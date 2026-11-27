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
	speakerOn := true
	var calls []string

	vc := &VolumeController{
		hasBin: func(name string) bool { return name == "amixer" },
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name != "amixer" {
				return "", errors.New("missing")
			}
			calls = append(calls, strings.Join(args, " "))
			if args[0] == "scontrols" {
				return "Simple mixer control 'Speaker',0\nSimple mixer control 'DACL',0\nSimple mixer control 'DACR',0\n", nil
			}
			if args[0] == "contents" {
				return "numid=16,iface=MIXER,name='DACL Playback Volume'\n  ; type=INTEGER,access=rw---R--,values=1,min=0,max=255,step=0\n  | dBscale-min=-95.50dB,step=0.50dB,mute=0\n" +
					"numid=17,iface=MIXER,name='DACR Playback Volume'\n  ; type=INTEGER,access=rw---R--,values=1,min=0,max=255,step=0\n  | dBscale-min=-95.50dB,step=0.50dB,mute=0\n", nil
			}
			if args[0] == "sget" {
				switch args[1] {
				case "DACL":
					return "Simple mixer control 'DACL',0\n  Capabilities: pvolume pvolume-joined\n  Playback channels: Mono\n  Limits: Playback 0 - 255\n  Mono: Playback 191 [" + itoa(daclPct) + "%] [0.00dB]\n", nil
				case "DACR":
					return "Simple mixer control 'DACR',0\n  Capabilities: pvolume pvolume-joined\n  Playback channels: Mono\n  Limits: Playback 0 - 255\n  Mono: Playback 191 [" + itoa(dacrPct) + "%] [0.00dB]\n", nil
				case "Speaker":
					state := "[on]"
					if !speakerOn {
						state = "[off]"
					}
					return "Simple mixer control 'Speaker',0\n  Capabilities: pswitch pswitch-joined\n  Playback channels: Mono\n  Mono: Playback " + state + "\n", nil
				default:
					return "", errors.New("no control")
				}
			}
			if args[0] == "-q" && args[1] == "sset" {
				if args[2] == "Speaker" {
					speakerOn = args[3] != "mute"
					return "", nil
				}
				pct := parsePctArg(args[3])
				if args[2] == "DACL" {
					daclPct = pct
				} else if args[2] == "DACR" {
					dacrPct = pct
				}
				return "", nil
			}
			return "", errors.New("unsupported")
		},
	}

	st := vc.State()
	if st.Backend != "amixer" || st.Level != 20 || st.MaxLevel != 20 || st.Percent != 100 || st.Muted {
		t.Fatalf("unexpected initial ES8389 state: %+v", st)
	}
	if len(vc.controls) != 2 || vc.controls[0].name != "DACL" || vc.controls[1].name != "DACR" {
		t.Fatalf("expected DACL+DACR controls, got %+v", vc.controls)
	}
	if vc.swCtrl != "Speaker" {
		t.Fatalf("expected Speaker mute switch, got %q", vc.swCtrl)
	}
	if !vc.controls[0].hasDB {
		t.Fatal("expected dB scale detected for DACL")
	}

	// Hardware mute uses the dedicated speaker switch.
	st = vc.ToggleMute()
	if !st.Muted || st.Level != 20 {
		t.Fatalf("expected muted while keeping level 20, got %+v", st)
	}
	seenSwitch := false
	for _, c := range calls {
		if c == "-q sset Speaker mute" {
			seenSwitch = true
		}
	}
	if !seenSwitch {
		t.Fatalf("expected Speaker mute switch in %v", calls)
	}
	st = vc.ToggleMute()
	if st.Muted {
		t.Fatalf("expected unmuted, got %+v", st)
	}

	// Stepping down writes both DAC channels with perceptual raw values.
	calls = nil
	st = vc.Step(-10)
	if st.Level != 10 || st.Percent != 50 {
		t.Fatalf("expected 10/20 (50%%), got %+v", st)
	}
	wroteL, wroteR := false, false
	for _, c := range calls {
		if strings.HasPrefix(c, "-q sset DACL ") {
			wroteL = true
			if strings.Contains(c, "%") {
				t.Fatalf("expected raw dB value for DACL, got %q", c)
			}
		}
		if strings.HasPrefix(c, "-q sset DACR ") {
			wroteR = true
		}
	}
	if !wroteL || !wroteR {
		t.Fatalf("expected both DACL and DACR writes, got %v", calls)
	}
}

func TestVolumeLevelRawTaper(t *testing.T) {
	ctrl := mixerControl{name: "DACL", rawMax: 255, dbMin: -95.5, dbStep: 0.5, hasDB: true}
	if raw := volumeLevelToRaw(20, ctrl); raw != 191 {
		t.Fatalf("level 20 raw = %d, want 191 (0 dB)", raw)
	}
	if raw := volumeLevelToRaw(0, ctrl); raw != 0 {
		t.Fatalf("level 0 raw = %d, want 0", raw)
	}
	prev := 0
	for lvl := 0; lvl <= 20; lvl++ {
		raw := volumeLevelToRaw(lvl, ctrl)
		if raw < prev {
			t.Fatalf("taper not monotonic at level %d: %d < %d", lvl, raw, prev)
		}
		if lvl > 0 && volumeRawToLevel(raw, ctrl) != lvl {
			t.Fatalf("round trip failed at level %d: raw %d -> %d", lvl, raw, volumeRawToLevel(raw, ctrl))
		}
		prev = raw
	}
	// Mid steps should be clearly quieter than max but not silent.
	if raw := volumeLevelToRaw(10, ctrl); raw >= 191 || raw < 20 {
		t.Fatalf("level 10 raw = %d, want a mid-range attenuation", raw)
	}
}

func TestVolumeControllerSkipsCaptureControls(t *testing.T) {
	// Regression: the ES8389 exposes capture volumes (ADC OSR / ADCL / ADCR)
	// before DACL/DACR in scontrols order. They must never be selected.
	sets := map[string]string{}
	vc := &VolumeController{
		hasBin: func(name string) bool { return name == "amixer" },
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name != "amixer" {
				return "", errors.New("missing")
			}
			switch args[0] {
			case "scontrols":
				return "Simple mixer control 'Speaker',0\nSimple mixer control 'ADC OSR',0\nSimple mixer control 'ADCL',0\nSimple mixer control 'DACL',0\nSimple mixer control 'DACR',0\n", nil
			case "contents":
				return "numid=16,iface=MIXER,name='DACL Playback Volume'\n  | dBscale-min=-95.50dB,step=0.50dB,mute=0\n", nil
			case "sget":
				switch args[1] {
				case "ADC OSR":
					return "Simple mixer control 'ADC OSR',0\n  Capabilities: cvolume cvolume-joined\n  Capture channels: Mono\n  Limits: Capture 0 - 255\n  Mono: Capture 178 [70%]\n", nil
				case "ADCL":
					return "Simple mixer control 'ADCL',0\n  Capabilities: cvolume cvolume-joined\n  Capture channels: Mono\n  Limits: Capture 0 - 255\n  Mono: Capture 191 [75%]\n", nil
				case "DACL":
					return "Simple mixer control 'DACL',0\n  Capabilities: pvolume pvolume-joined\n  Playback channels: Mono\n  Limits: Playback 0 - 255\n  Mono: Playback 191 [75%] [0.00dB]\n", nil
				case "DACR":
					return "Simple mixer control 'DACR',0\n  Capabilities: pvolume pvolume-joined\n  Playback channels: Mono\n  Limits: Playback 0 - 255\n  Mono: Playback 191 [75%] [0.00dB]\n", nil
				case "Speaker":
					return "Simple mixer control 'Speaker',0\n  Capabilities: pswitch pswitch-joined\n  Mono: Playback [on]\n", nil
				}
				return "", errors.New("no control")
			case "-q":
				sets[args[2]] = args[3]
				return "", nil
			}
			return "", errors.New("unsupported")
		},
	}
	st := vc.State()
	if st.Backend != "amixer" || st.Level != 20 {
		t.Fatalf("unexpected state: %+v", st)
	}
	for _, c := range vc.controls {
		if c.name == "ADC OSR" || c.name == "ADCL" {
			t.Fatalf("capture control selected: %+v", vc.controls)
		}
	}
	vc.Step(-5)
	if _, bad := sets["ADC OSR"]; bad {
		t.Fatal("volume step wrote to capture control ADC OSR")
	}
	if sets["DACL"] == "" || sets["DACR"] == "" {
		t.Fatalf("expected DACL+DACR writes, got %v", sets)
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
