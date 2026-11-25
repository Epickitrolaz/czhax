package ir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeNEC(t *testing.T) {
	sig, err := EncodeProtocol("NEC", 0x08, 0x05)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig.Timings) != 66 {
		t.Fatalf("NEC timings = %d, want 66", len(sig.Timings))
	}
	if sig.Timings[0] != 9000 || sig.Timings[1] != 4500 {
		t.Fatalf("NEC leader = %v", sig.Timings[:2])
	}
	if sig.Timings[len(sig.Timings)-1] != frameGap {
		t.Fatalf("NEC trailing gap = %d, want %d", sig.Timings[len(sig.Timings)-1], frameGap)
	}
	for i := 2; i < len(sig.Timings)-2; i += 2 {
		if sig.Timings[i] != 560 {
			t.Fatalf("NEC mark at %d = %d, want 560", i, sig.Timings[i])
		}
		if s := sig.Timings[i+1]; s != 560 && s != 1690 {
			t.Fatalf("NEC space at %d = %d", i+1, s)
		}
	}
}

func TestEncodeSamsungAndSIRC(t *testing.T) {
	sam, err := EncodeProtocol("Samsung32", 0x07, 0x02)
	if err != nil {
		t.Fatal(err)
	}
	if sam.Timings[0] != 4500 || sam.Timings[1] != 4500 || len(sam.Timings) != 66 {
		t.Fatalf("Samsung leader/len = %v/%d", sam.Timings[:2], len(sam.Timings))
	}
	sirc, err := EncodeProtocol("SIRC", 0x01, 0x15)
	if err != nil {
		t.Fatal(err)
	}
	// 12 data bits + leader = 26 timings, sent three times.
	if len(sirc.Timings) != 78 {
		t.Fatalf("SIRC timings = %d, want 78", len(sirc.Timings))
	}
	if sirc.Frequency != 40000 {
		t.Fatalf("SIRC frequency = %d", sirc.Frequency)
	}
}

func TestEncodeRC5RC6RCA(t *testing.T) {
	for _, protocol := range []string{"RC5", "RC5X", "RC6", "RCA", "Kaseikyo", "NECext"} {
		sig, err := EncodeProtocol(protocol, 0x01, 0x15)
		if err != nil {
			t.Fatalf("%s: %v", protocol, err)
		}
		if len(sig.Timings) < 4 || len(sig.Timings)%2 != 0 {
			t.Fatalf("%s produced invalid timings: %v", protocol, sig.Timings)
		}
		if sig.Timings[len(sig.Timings)-1] != frameGap {
			t.Fatalf("%s does not end in a frame gap: %d", protocol, sig.Timings[len(sig.Timings)-1])
		}
	}
}

func TestParseFlipperFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tv.ir")
	content := `Filetype: IR signals file
Version: 1
#
name: Power
type: parsed
protocol: NEC
address: 08 00 00 00
command: 05 00 00 00
#
name: RawBtn
type: raw
frequency: 38000
duty_cycle: 0.330000
data: 9000 4500 560 560 560 1690 560 560
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := ParseFlipperFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Signals) != 2 {
		t.Fatalf("signals = %d, want 2", len(r.Signals))
	}
	if r.Signals[0].Name != "Power" || r.Signals[0].Protocol != "NEC" || len(r.Signals[0].Timings) != 66 {
		t.Fatalf("parsed signal = %+v", r.Signals[0])
	}
	if r.Signals[1].Name != "RawBtn" || len(r.Signals[1].Timings) < 4 {
		t.Fatalf("raw signal = %+v", r.Signals[1])
	}
}

func TestParseMiRemoteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Xiaomi_TV.json")
	content := `[{"designation":"Xiaomi Projector","patterns":[
		{"comment":"kk 111_6667","frequency":37960,"pattern":[341,171,20,22,20,22]}
	]}]`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := ParseMiRemoteJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Provider != "mi" || len(r.Signals) != 1 {
		t.Fatalf("remote = %+v", r)
	}
	sig := r.Signals[0]
	if sig.Name != "Power" || sig.Frequency != 37960 {
		t.Fatalf("signal = %+v", sig)
	}
	if len(sig.Timings) != 6 {
		t.Fatalf("timings = %v", sig.Timings)
	}
	if sig.Timings[0] < 8900 || sig.Timings[0] > 9050 {
		t.Fatalf("first timing = %d, want ~8983us", sig.Timings[0])
	}
	if !strings.Contains(sig.Comment, "Xiaomi Projector") {
		t.Fatalf("comment = %q", sig.Comment)
	}
}

func TestStoreRoundTripAndBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ir.json")
	s := NewStore()
	sig, _ := EncodeProtocol("NEC", 0x08, 0x05)
	sig.Name = "Power"
	b := Binding{Key: "1", Name: "Power", Signal: sig}
	s.AddBinding("My TV", b)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	p := loaded.ActiveProfile()
	if p == nil || p.Name != "My TV" || len(p.Bindings) != 1 || p.Bindings[0].Key != "1" {
		t.Fatalf("loaded profile = %+v", p)
	}
	if len(p.Bindings[0].Signal.Timings) != 66 {
		t.Fatalf("binding timings = %d", len(p.Bindings[0].Signal.Timings))
	}
}

func TestKeyMapping(t *testing.T) {
	cases := map[string]string{
		"Power":   "1",
		"VOL+":    "2",
		"Vol_dn":  "3",
		"Mute":    "4",
		"Ch_next": "5",
		"Up":      "7",
		"OK":      "enter",
		"Back":    "backspace",
	}
	for name, want := range cases {
		if got := KeyForButton(name); got != want {
			t.Fatalf("KeyForButton(%q) = %q, want %q", name, got, want)
		}
	}
	if !IsBindableKey("f8") || IsBindableKey("q") {
		t.Fatal("unexpected bindable key set")
	}
}

func TestBindSignals(t *testing.T) {
	sig, _ := EncodeProtocol("NEC", 1, 2)
	sig.Name = "Power"
	p := BindSignals("TV", "flipper", "TV", "Acme", "X1", []Signal{sig, sig})
	if len(p.Bindings) != 2 {
		t.Fatalf("bindings = %d", len(p.Bindings))
	}
	if p.Bindings[0].Key != "1" || p.Bindings[1].Key == "1" {
		t.Fatalf("duplicate key mapping: %+v", p.Bindings)
	}
}

func TestLibraryIndex(t *testing.T) {
	root := t.TempDir()
	flipperRoot := filepath.Join(root, "flipper")
	miRoot := filepath.Join(root, "mi")
	for _, dir := range []string{
		filepath.Join(flipperRoot, "TVs", "Acme"),
		filepath.Join(miRoot, "TV", "Acme"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	irFile := "Filetype: IR signals file\nVersion: 1\n#\nname: Power\ntype: parsed\nprotocol: NEC\naddress: 01 00 00 00\ncommand: 02 00 00 00\n"
	for _, p := range []string{
		filepath.Join(flipperRoot, "TVs", "Acme", "Model.ir"),
		filepath.Join(miRoot, "TV", "Acme", "Model.ir"),
	} {
		if err := os.WriteFile(p, []byte(irFile), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := LoadIndex(Roots{Flipper: flipperRoot, Mi: miRoot})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Category != "TV" || e.Brand != "Acme" || e.Model != "Model" {
			t.Fatalf("entry metadata = %+v", e)
		}
	}
}

func TestNormalizeTimings(t *testing.T) {
	got := normalizeTimings([]int{9000, 4500, 560})
	if len(got) != 4 || got[3] != frameGap {
		t.Fatalf("normalizeTimings = %v", got)
	}
}

func TestParseFlipperHex(t *testing.T) {
	v, err := parseFlipperHex("EE 87 00 00")
	if err != nil || v != 0x87ee {
		t.Fatalf("parseFlipperHex = %#x, %v", v, err)
	}
	if _, err := parseFlipperHex("nope"); err == nil {
		t.Fatal("expected invalid hex to fail")
	}
}

func TestLircUAPIConstants(t *testing.T) {
	// Values from linux/lirc.h on the Cardputer's arm64 kernel.
	if lircGetFeatures != 0x80046900 || lircSetRecMode != 0x40046912 ||
		lircSetSendMode != 0x40046911 || lircSetSendCarrier != 0x40046913 ||
		lircSetSendDutyCycle != 0x40046915 {
		t.Fatal("unexpected lirc ioctl encoding")
	}
	if lircCanRecMode2 != 0x00040000 || lircCanSendPulse != 0x00000002 {
		t.Fatal("unexpected lirc capability masks")
	}
	if lircMode2Pulse != 0x01000000 || lircMode2Timeout != 0x03000000 || lircMode2Overflow != 0x04000000 {
		t.Fatal("unexpected lirc mode2 packet flags")
	}
}

func TestTrimCapturedTimings(t *testing.T) {
	in := []int{1000, 2000, 3000}
	out := trimCapturedTimings(in)
	if len(out) != 4 || out[3] != frameGap {
		t.Fatalf("odd capture = %v", out)
	}
	in = []int{1000, 2000, 3000, 500}
	out = trimCapturedTimings(in)
	if len(out) != 4 || out[3] != frameGap {
		t.Fatalf("short gap = %v", out)
	}
	in = []int{1000, 2000, 3000, 50000}
	out = trimCapturedTimings(in)
	if out[3] != 50000 {
		t.Fatalf("long gap was truncated: %v", out)
	}
}
