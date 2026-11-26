package hwctl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrightness10Levels(t *testing.T) {
	// Perceptual CIE 1931 curve over 0..100: 0 is black, 100 is max, and
	// each 10% selector step maps monotonically onto raw PWM duty.
	wantRaw := []int{0, 1, 3, 6, 11, 18, 28, 41, 57, 76, 100}
	for lvl, raw := range wantRaw {
		gotRaw := BrightnessRawForLevel(lvl, 100)
		if gotRaw != raw {
			t.Fatalf("BrightnessRawForLevel(%d, 100) = %d, want %d", lvl, gotRaw, raw)
		}
		if gotLvl := BrightnessLevel(raw, 100); gotLvl != lvl {
			t.Fatalf("BrightnessLevel(%d, 100) = %d, want %d", raw, gotLvl, lvl)
		}
		if gotPct := BrightnessPercent(raw, 100); gotPct != lvl*10 {
			t.Fatalf("BrightnessPercent(%d, 100) = %d, want %d", raw, gotPct, lvl*10)
		}
	}
	// Non-zero values below level 1 still map to >= level 1.
	if got := BrightnessLevel(1, 255); got < 1 {
		t.Fatalf("expected non-zero raw to map to >=1, got %d", got)
	}
}

func TestReadAndStepBacklight(t *testing.T) {
	sysRoot := t.TempDir()
	blDir := filepath.Join(sysRoot, "class", "backlight", "backlight")
	if err := os.MkdirAll(blDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blDir, "max_brightness"), []byte("100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blDir, "brightness"), []byte("18\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, ok := ReadBacklight(sysRoot)
	if !ok || st.Level != 5 || st.MaxLevel != 10 || st.Percent != 50 {
		t.Fatalf("unexpected ReadBacklight: ok=%v st=%+v", ok, st)
	}

	st, ok = StepBacklight(sysRoot, 1)
	if !ok || st.Raw != 28 || st.Level != 6 || st.Percent != 60 {
		t.Fatalf("unexpected StepBacklight(+1): ok=%v st=%+v", ok, st)
	}
	st, ok = StepBacklight(sysRoot, -1)
	if !ok || st.Raw != 18 || st.Level != 5 || st.Percent != 50 {
		t.Fatalf("unexpected StepBacklight(-1): ok=%v st=%+v", ok, st)
	}

	// Stepping down must reach 0, and stepping up/down from legacy MIN=5 works cleanly.
	if err := os.WriteFile(filepath.Join(blDir, "brightness"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, ok = StepBacklight(sysRoot, -1)
	if !ok || st.Raw != 0 || st.Level != 0 || st.Percent != 0 {
		t.Fatalf("expected 1 -> 0 on StepBacklight(-1), got ok=%v st=%+v", ok, st)
	}
	st, ok = StepBacklight(sysRoot, -1)
	if !ok || st.Raw != 0 || st.Level != 0 {
		t.Fatalf("expected clamp at 0 on StepBacklight(-1), got ok=%v st=%+v", ok, st)
	}

	// Full round-trip must produce the perceptual curve 0..100 with 0%..100% steps.
	wantUp := []int{1, 3, 6, 11, 18, 28, 41, 57, 76, 100}
	var up []int
	for i := 0; i < 10; i++ {
		st, ok = StepBacklight(sysRoot, 1)
		if !ok {
			t.Fatal("StepBacklight(+1) failed")
		}
		if st.Percent != (i+1)*10 {
			t.Fatalf("up step %d percent = %d, want %d", i, st.Percent, (i+1)*10)
		}
		up = append(up, st.Raw)
	}
	for i, w := range wantUp {
		if up[i] != w {
			t.Fatalf("up step %d = %d, want %d (all=%v)", i, up[i], w, up)
		}
	}
	wantDn := []int{76, 57, 41, 28, 18, 11, 6, 3, 1, 0}
	var dn []int
	for i := 0; i < 10; i++ {
		st, ok = StepBacklight(sysRoot, -1)
		if !ok {
			t.Fatal("StepBacklight(-1) failed")
		}
		if st.Percent != (9-i)*10 {
			t.Fatalf("down step %d percent = %d, want %d", i, st.Percent, (9-i)*10)
		}
		dn = append(dn, st.Raw)
	}
	for i, w := range wantDn {
		if dn[i] != w {
			t.Fatalf("down step %d = %d, want %d (all=%v)", i, dn[i], w, dn)
		}
	}
}

func TestLegacyDaemonClampSnappedToZero(t *testing.T) {
	sysRoot := t.TempDir()
	blDir := filepath.Join(sysRoot, "class", "backlight", "backlight")
	if err := os.MkdirAll(blDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blDir, "max_brightness"), []byte("100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulate legacy cardputer-brightness.py clamping level 1 -> 5 on Fn+U.
	if err := os.WriteFile(filepath.Join(blDir, "brightness"), []byte("5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &Watcher{
		events:     make(chan Event, 4),
		stopCh:     make(chan struct{}),
		blKey:      make(chan int, 4),
		lastRaw:    BrightnessRawForLevel(1, 100),
		hasLastRaw: true,
	}
	w.handleBrightnessKeyAt(sysRoot, -1)
	ev := <-w.events
	if ev.Brightness.Raw != 0 || ev.Brightness.Level != 0 {
		t.Fatalf("expected legacy clamp snapped to 0, got %+v", ev.Brightness)
	}
	got, ok := ReadBacklight(sysRoot)
	if !ok || got.Raw != 0 {
		t.Fatalf("expected sysfs brightness written to 0, got ok=%v st=%+v", ok, got)
	}
}

func TestVolumeControllerES8389AndMute(t *testing.T) {
	daclPct := 75
	dacrPct := 75
	var calls []string

	vc := &VolumeController{
		hasBin: func(name string) bool { return name == "amixer" },
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name != "amixer" {
				return "", errors.New("missing")
			}
			calls = append(calls, strings.Join(args, " "))
			if args[0] == "sget" {
				switch args[1] {
				case "DACL Playback":
					return "Simple mixer control 'DACL Playback',0\n  Capabilities: volume volume-joined\n  Mono: Playback 191 [" + itoa(daclPct) + "%]\n", nil
				case "DACR Playback":
					return "Simple mixer control 'DACR Playback',0\n  Capabilities: volume volume-joined\n  Mono: Playback 191 [" + itoa(dacrPct) + "%]\n", nil
				default:
					return "", errors.New("no control")
				}
			}
			if args[0] == "-q" && args[1] == "sset" {
				pct := parsePctArg(args[3])
				if args[2] == "DACL Playback" {
					daclPct = pct
				} else if args[2] == "DACR Playback" {
					dacrPct = pct
				}
				return "", nil
			}
			return "", errors.New("unsupported")
		},
	}

	st := vc.State()
	if st.Backend != "amixer" || st.Level != 15 || st.MaxLevel != 20 || st.Percent != 75 || st.Muted {
		t.Fatalf("unexpected initial ES8389 state: %+v", st)
	}

	// Step up once: 15 -> 16 (80%) on both DACL and DACR.
	st = vc.Step(1)
	if st.Level != 16 || st.Percent != 80 || daclPct != 80 || dacrPct != 80 {
		t.Fatalf("expected 16/20 (80%%) on both channels, got st=%+v L=%d R=%d", st, daclPct, dacrPct)
	}

	// Mute remembers 16/20 while setting hardware to 0%, then unmute restores 80%.
	st = vc.ToggleMute()
	if !st.Muted || st.Level != 16 || daclPct != 0 || dacrPct != 0 {
		t.Fatalf("expected muted with saved level 16 and hw 0%%, got st=%+v L=%d R=%d", st, daclPct, dacrPct)
	}
	st = vc.ToggleMute()
	if st.Muted || st.Level != 16 || daclPct != 80 || dacrPct != 80 {
		t.Fatalf("expected unmuted back to 16/20 (80%%), got st=%+v L=%d R=%d", st, daclPct, dacrPct)
	}
}

func TestVolumeControllerSoftFallback20Levels(t *testing.T) {
	vc := &VolumeController{
		level:  10,
		hasBin: func(string) bool { return false },
	}
	for i := 0; i < 15; i++ {
		vc.Step(1)
	}
	st := vc.State()
	if st.Backend != "soft" || st.Level != 20 || st.Percent != 100 {
		t.Fatalf("expected clamped 20/20 (100%%), got %+v", st)
	}
	for i := 0; i < 25; i++ {
		vc.Step(-1)
	}
	st = vc.State()
	if st.Level != 0 || st.Percent != 0 {
		t.Fatalf("expected clamped 0/20 (0%%), got %+v", st)
	}
	st = vc.ToggleMute()
	if !st.Muted {
		t.Fatal("expected muted=true")
	}
	// Stepping up from muted unmutes.
	st = vc.Step(1)
	if st.Muted || st.Level != 1 || st.Percent != 5 {
		t.Fatalf("expected unmuted 1/20 (5%%), got %+v", st)
	}
}

func itoa(n int) string {
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}

func parsePctArg(s string) int {
	s = strings.TrimSuffix(s, "%")
	v := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			v = v*10 + int(c-'0')
		}
	}
	return v
}
