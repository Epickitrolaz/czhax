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
	// Leader (2) + 32 bits (64) + trailing stop mark (1) = 67 (odd for LIRC_MODE_PULSE).
	if len(sig.Timings) != 67 {
		t.Fatalf("NEC timings = %d, want 67", len(sig.Timings))
	}
	if err := sig.Validate(); err != nil {
		t.Fatalf("NEC Validate() = %v", err)
	}
	if sig.Timings[0] != 9000 || sig.Timings[1] != 4500 {
		t.Fatalf("NEC leader = %v", sig.Timings[:2])
	}
	if sig.Timings[len(sig.Timings)-1] != 560 {
		t.Fatalf("NEC trailing stop mark = %d, want 560", sig.Timings[len(sig.Timings)-1])
	}
	for i := 2; i < len(sig.Timings)-1; i += 2 {
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
	if sam.Timings[0] != 4500 || sam.Timings[1] != 4500 || len(sam.Timings) != 67 {
		t.Fatalf("Samsung leader/len = %v/%d", sam.Timings[:2], len(sam.Timings))
	}
	sirc, err := EncodeProtocol("SIRC", 0x01, 0x15)
	if err != nil {
		t.Fatal(err)
	}
	// 12 data bits + leader = 26 timings for repeats 1 & 2, 25 for final repeat (ends on mark).
	if len(sirc.Timings) != 77 {
		t.Fatalf("SIRC timings = %d, want 77", len(sirc.Timings))
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
		if len(sig.Timings) < 3 || len(sig.Timings)%2 != 1 {
			t.Fatalf("%s produced invalid odd/length timings (%d): %v", protocol, len(sig.Timings), sig.Timings)
		}
		if err := sig.Validate(); err != nil {
			t.Fatalf("%s failed Validate(): %v", protocol, err)
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
	if r.Signals[0].Name != "Power" || r.Signals[0].Protocol != "NEC" || len(r.Signals[0].Timings) != 67 {
		t.Fatalf("parsed signal = %+v", r.Signals[0])
	}
	if r.Signals[1].Name != "RawBtn" || len(r.Signals[1].Timings) != 7 {
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
	if len(sig.Timings) != 5 {
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
	if len(p.Bindings[0].Signal.Timings) != 67 {
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
	if !IsBindableKey("f8") || !IsBindableKey("p") || !IsBindableKey(" ") || IsBindableKey("q") {
		t.Fatal("unexpected bindable key set")
	}
	if NormalizeBindKey("P") != "p" || NormalizeBindKey(" ") != "space" {
		t.Fatalf("unexpected NormalizeBindKey result")
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
	got := normalizeTimings([]int{9000, 4500, 560, 40000})
	if len(got) != 3 || got[2] != 560 {
		t.Fatalf("normalizeTimings even = %v", got)
	}
	got = normalizeTimings([]int{9000, 4500, 560})
	if len(got) != 3 || got[2] != 560 {
		t.Fatalf("normalizeTimings odd = %v", got)
	}
	// Cumulative duration cap (Linux IR_MAX_DURATION = 500000 us): keeps repeats
	// that fit within 500ms and ends on an odd pulse.
	got = normalizeTimings([]int{9000, 4500, 560, 95000, 9000, 2250, 560, 400000, 9000, 2250, 560})
	if len(got) != 7 {
		t.Fatalf("normalizeTimings cumulative trim len = %d (%v)", len(got), got)
	}
	sum := 0
	for _, d := range got {
		sum += d
	}
	if sum > maxDurationUS {
		t.Fatalf("normalizeTimings cumulative sum = %d > %d", sum, maxDurationUS)
	}
	if err := (Signal{Timings: []int{9000, 4500, 560, 560}}).Validate(); err == nil {
		t.Fatal("expected Validate() to reject even timing count")
	}
	if err := (Signal{Timings: []int{9000, 495000, 560}}).Validate(); err == nil {
		t.Fatal("expected Validate() to reject cumulative duration > maxDurationUS")
	}
}

func TestStoreRebindAndRename(t *testing.T) {
	s := NewStore()
	pwr, _ := EncodeProtocol("NEC", 0x08, 0x05)
	pwr.Name = "Power"
	okSig, _ := EncodeProtocol("NEC", 0x08, 0x06)
	okSig.Name = "OK"
	oneSig, _ := EncodeProtocol("NEC", 0x08, 0x07)
	oneSig.Name = "1"

	prof := BindSignals("My TV", "flipper", "TV", "Acme", "X1", []Signal{pwr, okSig, oneSig})
	s.AddProfile(prof)

	// Remap OK (currently on enter) to free key 'k'.
	okIdx := -1
	for i, b := range s.Profiles[0].Bindings {
		if b.Name == "OK" {
			okIdx = i
		}
	}
	if okIdx < 0 {
		t.Fatal("missing OK binding")
	}
	swapped, newIdx, ok := s.RebindKey(0, okIdx, "K")
	if !ok || swapped != "" || s.Profiles[0].Bindings[newIdx].Key != "k" {
		t.Fatalf("RebindKey(OK->K) = swapped=%q newIdx=%d ok=%v bindings=%+v", swapped, newIdx, ok, s.Profiles[0].Bindings)
	}

	// Remap button "1" to occupied key '1' (Power) -> swaps keys cleanly.
	oneIdx := -1
	oldOneKey := ""
	for i, b := range s.Profiles[0].Bindings {
		if b.Name == "1" {
			oneIdx = i
			oldOneKey = b.Key
		}
	}
	swapped, newIdx, ok = s.RebindKey(0, oneIdx, "1")
	if !ok || swapped != "Power" || s.Profiles[0].Bindings[newIdx].Key != "1" || s.Profiles[0].Bindings[newIdx].Name != "1" {
		t.Fatalf("RebindKey(1->1 swap) = swapped=%q newIdx=%d ok=%v bindings=%+v", swapped, newIdx, ok, s.Profiles[0].Bindings)
	}
	// Power should now live on oldOneKey rather than being deleted.
	foundPower := false
	for _, b := range s.Profiles[0].Bindings {
		if b.Name == "Power" && b.Key == oldOneKey {
			foundPower = true
		}
	}
	if !foundPower {
		t.Fatalf("expected Power swapped to key %q, got %+v", oldOneKey, s.Profiles[0].Bindings)
	}

	// Rename binding
	if !s.RenameBinding(0, newIdx, "Ch 1") || s.Profiles[0].Bindings[newIdx].Name != "Ch 1" {
		t.Fatalf("RenameBinding failed: %+v", s.Profiles[0].Bindings[newIdx])
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
	if len(out) != 3 || out[2] != 3000 {
		t.Fatalf("odd capture = %v", out)
	}
	in = []int{1000, 2000, 3000, 500}
	out = trimCapturedTimings(in)
	if len(out) != 3 || out[2] != 3000 {
		t.Fatalf("even short gap = %v", out)
	}
	in = []int{1000, 2000, 3000, 50000}
	out = trimCapturedTimings(in)
	if len(out) != 3 || out[2] != 3000 {
		t.Fatalf("even trailing gap = %v", out)
	}
}

func TestBundledDatabasesAndSearch(t *testing.T) {
	roots := DefaultRoots()
	if !roots.Bundled {
		t.Fatal("expected DefaultRoots().Bundled to be true")
	}
	entries, err := LoadIndex(roots)
	if err != nil {
		t.Fatalf("LoadIndex(bundled): %v", err)
	}
	var flipperSample, miSample *Entry
	for i := range entries {
		e := &entries[i]
		if e.Archive == "flipper" && flipperSample == nil {
			flipperSample = e
		}
		if e.Archive == "mi" && miSample == nil {
			miSample = e
		}
		if flipperSample != nil && miSample != nil {
			break
		}
	}
	if flipperSample == nil || miSample == nil {
		t.Fatalf("expected both bundled flipper and mi entries (flipper=%v mi=%v, total=%d)", flipperSample != nil, miSample != nil, len(entries))
	}
	for _, e := range []*Entry{flipperSample, miSample} {
		r, err := LoadEntry(*e)
		if err != nil {
			t.Fatalf("LoadEntry(%s %s): %v", e.Provider, e.Path, err)
		}
		if len(r.Signals) == 0 {
			t.Fatalf("expected signals in %s", e.Path)
		}
		if err := r.Signals[0].Validate(); err != nil {
			t.Fatalf("bundled signal %s invalid: %v", e.Path, err)
		}
	}
	filtered := SearchEntries(entries, "mi", "", miSample.Brand)
	if len(filtered) == 0 {
		t.Fatalf("expected SearchEntries to match brand %q", miSample.Brand)
	}
}

func TestStoreMultipleRemotes(t *testing.T) {
	s := NewStore()
	idx0, err := s.CreateProfile("Living TV", "custom")
	if err != nil || idx0 != 0 {
		t.Fatalf("CreateProfile 0 = %d, %v", idx0, err)
	}
	if _, err := s.CreateProfile("living tv", "custom"); err == nil {
		t.Fatal("expected duplicate profile name error")
	}
	idx1, err := s.CreateProfile("Bedroom AC", "custom")
	if err != nil || idx1 != 1 || s.Active != 1 {
		t.Fatalf("CreateProfile 1 = %d, active=%d, err=%v", idx1, s.Active, err)
	}
	sig, _ := EncodeProtocol("NEC", 0x08, 0x05)
	s.AddBindingToActive(Binding{Key: "p", Name: "Power", Signal: sig})
	if p := s.ActiveProfile(); p == nil || p.Name != "Bedroom AC" || len(p.Bindings) != 1 {
		t.Fatalf("active profile = %+v", p)
	}
	if !s.SelectProfile(0) || s.Active != 0 {
		t.Fatal("expected SelectProfile(0) to succeed")
	}
	s.AddBindingToActive(Binding{Key: "1", Name: "Mute", Signal: sig})
	if p := s.ActiveProfile(); p == nil || p.Name != "Living TV" || len(p.Bindings) != 1 {
		t.Fatalf("living tv = %+v", p)
	}
	if !s.DeleteBinding(0, "1") || len(s.Profiles[0].Bindings) != 0 {
		t.Fatal("expected DeleteBinding to remove binding")
	}
	if !s.DeleteProfile(0) || len(s.Profiles) != 1 || s.ActiveProfile().Name != "Bedroom AC" {
		t.Fatalf("after DeleteProfile: %+v", s)
	}
}
