package bluetooth

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestParseFastPairPairingHandle(t *testing.T) {
	out := `handle: 0x0010, char properties: 0x02, char value handle: 0x0011, uuid: fe2c1234-8366-4814-8eb0-01de32100bea
handle: 0x0014, char properties: 0x02, char value handle: 0x0015, uuid: fe2c1233-8366-4814-8eb0-01de32100bea
`
	if got := parseFastPairPairingHandle(out); got != "0x0011" {
		t.Fatalf("pairing handle = %q", got)
	}
	if got := parseFastPairPairingHandle("handle: 0x1, char value handle: 0x2, uuid: 00001234-0000-1000-8000-00805f9b34fb"); got != "0x2" {
		t.Fatalf("deprecated pairing handle = %q", got)
	}
	if got := parseFastPairPairingHandle("nothing"); got != "" {
		t.Fatalf("expected no handle, got %q", got)
	}
}

func TestFastPairRawRequest(t *testing.T) {
	raw, err := fastPairRawRequest("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 16 || raw[0] != 0 || raw[1] != 0 {
		t.Fatalf("unexpected raw request %x", raw)
	}
	if hex.EncodeToString(raw[2:8]) != "aabbccddeeff" {
		t.Fatalf("address bytes = %x", raw[2:8])
	}
}

func TestWhisperPairKeys(t *testing.T) {
	keys := fastPairInvalidCurveKeys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 distinct invalid-curve keys, got %d", len(keys))
	}
	for _, k := range keys {
		if len(k) != 32 {
			t.Fatalf("key %q is not 16 bytes", k)
		}
	}
}

func TestAESECBEncrypt(t *testing.T) {
	key := make([]byte, 16)
	pt := make([]byte, 16)
	out := aesECBEncrypt(key, pt)
	want := "66e94bd4ef8a2c3b884cfa59ca342b2e"
	if hex.EncodeToString(out) != want {
		t.Fatalf("AES-128 zero vector = %x", out)
	}
}

func TestFastPairNotified(t *testing.T) {
	for _, out := range []string{
		"Notification handle = 0x0011 value: 0x01 0x02",
		"Notification: value: 0x01",
	} {
		if !fastPairNotified(out) {
			t.Fatalf("expected notification in %q", out)
		}
	}
	if fastPairNotified(strings.Repeat("nothing", 10)) {
		t.Fatal("unexpected notification")
	}
}
