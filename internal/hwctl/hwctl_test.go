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
	// Cardputer-brightness.py values (MIN=5, STEP=10, MAX=100) and clean 10s.
	cases := []struct {
		raw  int
		max  int
		want int
	}{
		{0, 100, 0},
		{5, 100, 1},
		{10, 100, 1},
		{15, 100, 2},
		{20, 100, 2},
		{25, 100, 3},
		{35, 100, 4},
		{45, 100, 5},
		{55, 100, 6},
		{65, 100, 7},
		{75, 100, 8},
		{85, 100, 9},
		{90, 100, 9},
		{95, 100, 10},
		{100, 100, 10},
	}
	for _, tc := range cases {
		got := BrightnessLevel(tc.raw, tc.max)
		if got != tc.want {
			t.Fatalf("BrightnessLevel(%d, %d) = %d, want %d", tc.raw, tc.max, got, tc.want)
		}
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
	if err := os.WriteFile(filepath.Join(blDir, "brightness"), []byte("50\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, ok := ReadBacklight(sysRoot)
	if !ok || st.Level != 5 || st.MaxLevel != 10 || st.Percent != 50 {
		t.Fatalf("unexpected ReadBacklight: ok=%v st=%+v", ok, st)
	}

	st, ok = StepBacklight(sysRoot, 1)
	if !ok || st.Raw != 60 || st.Level != 6 {
		t.Fatalf("unexpected StepBacklight(+1): ok=%v st=%+v", ok, st)
	}
	st, ok = StepBacklight(sysRoot, -1)
	if !ok || st.Raw != 50 || st.Level != 5 {
		t.Fatalf("unexpected StepBacklight(-1): ok=%v st=%+v", ok, st)
	}

	// Stepping down must reach 0, and stepping up from 5 or 0 snaps onto clean 10-level grid.
	if err := os.WriteFile(filepath.Join(blDir, "brightness"), []byte("5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, ok = StepBacklight(sysRoot, 1)
	if !ok || st.Raw != 10 || st.Level != 1 {
		t.Fatalf("expected 5 -> 10 on StepBacklight(+1), got ok=%v st=%+v", ok, st)
	}
	st, ok = StepBacklight(sysRoot, -1)
	if !ok || st.Raw != 0 || st.Level != 0 {
		t.Fatalf("expected 10 -> 0 on StepBacklight(-1), got ok=%v st=%+v", ok, st)
	}
	st, ok = StepBacklight(sysRoot, -1)
	if !ok || st.Raw != 0 || st.Level != 0 {
		t.Fatalf("expected clamp at 0 on StepBacklight(-1), got ok=%v st=%+v", ok, st)
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
