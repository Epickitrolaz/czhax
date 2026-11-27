package bluetooth

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEmuProfileAndEffectiveSpec(t *testing.T) {
	for _, p := range EmuProfiles {
		got, err := ParseEmuProfile(string(p.Kind))
		if err != nil {
			t.Fatalf("ParseEmuProfile(%q): %v", p.Kind, err)
		}
		if got != p.Kind {
			t.Fatalf("ParseEmuProfile(%q) = %q", p.Kind, got)
		}
	}
	if _, err := ParseEmuProfile("invalid-device"); err == nil {
		t.Fatal("expected error for invalid profile")
	}

	cfg := EmuConfig{
		Profile:    EmuCustom,
		Name:       "Desk Box",
		CoD:        0x240414,
		CustomMode: CustomSpeaker,
	}
	spec := cfg.EffectiveSpec()
	if !spec.IsAudio || spec.IsHID || spec.DefaultName != "Desk Box" || spec.CoD != 0x240414 {
		t.Fatalf("unexpected custom speaker spec: %+v", spec)
	}

	cfgAdv := EmuConfig{
		Profile:    EmuCustom,
		CustomMode: CustomAdvertise,
	}
	specAdv := cfgAdv.EffectiveSpec()
	if specAdv.IsAudio || specAdv.IsHID {
		t.Fatalf("expected advertise-only custom mode, got %+v", specAdv)
	}
}

func TestParseHexUint32(t *testing.T) {
	v, err := ParseHexUint32("0x002540")
	if err != nil || v != 0x002540 {
		t.Fatalf("ParseHexUint32(0x002540) = (%#x, %v)", v, err)
	}
	v, err = ParseHexUint32("240414")
	if err != nil || v != 0x240414 {
		t.Fatalf("ParseHexUint32(240414) = (%#x, %v)", v, err)
	}
	if _, err := ParseHexUint32("xyz"); err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestSelectSBCConfigAndPeak(t *testing.T) {
	sel, err := selectSBCConfig([]byte{0xFF, 0xFF, 2, 53})
	if err != nil {
		t.Fatalf("selectSBCConfig: %v", err)
	}
	rate, ch := parseSBCConfig(sel)
	if rate != 44100 || ch != 2 {
		t.Fatalf("unexpected SBC config rate=%d ch=%d (sel=%x)", rate, ch, sel)
	}

	pcm := make([]byte, 4)
	s0, s1 := int16(16384), int16(-8192)
	binary.LittleEndian.PutUint16(pcm[0:2], uint16(s0))
	binary.LittleEndian.PutUint16(pcm[2:4], uint16(s1))
	if lvl := pcmPeakPercent(pcm); lvl < 49 || lvl > 51 {
		t.Fatalf("pcmPeakPercent = %d, want ~50", lvl)
	}
}

func TestPCMWAVWriterHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.wav")
	w, err := newPCMWAVWriter(path, 48000, 2)
	if err != nil {
		t.Fatalf("newPCMWAVWriter: %v", err)
	}
	payload := make([]byte, 16)
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(raw) != 44+16 {
		t.Fatalf("WAV len = %d, want %d", len(raw), 44+16)
	}
	if string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		t.Fatalf("invalid WAV header")
	}
	if got := binary.LittleEndian.Uint32(raw[4:8]); got != 36+16 {
		t.Fatalf("RIFF size = %d, want %d", got, 36+16)
	}
	if got := binary.LittleEndian.Uint32(raw[40:44]); got != 16 {
		t.Fatalf("data size = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(raw[24:28]); got != 48000 {
		t.Fatalf("sample rate = %d, want 48000", got)
	}
}

func TestRunEmulatorCLICancelsOnStop(t *testing.T) {
	// Ensure RunEmulatorCLI exits cleanly on "stop" command without needing D-Bus
	// if canceled immediately; we test the JSON scanner/cancel path cleanly.
	var in bytes.Buffer
	b, _ := json.Marshal(EmuCommand{Op: "stop"})
	in.Write(append(b, '\n'))
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunEmulatorCLI(ctx, EmuConfig{HCI: "hci0", Profile: EmuKeyboard}, &in, &out)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") && !strings.Contains(strings.ToLower(err.Error()), "dbus") && !strings.Contains(strings.ToLower(err.Error()), "org.bluez") {
		t.Fatalf("unexpected RunEmulatorCLI error: %v", err)
	}
}
