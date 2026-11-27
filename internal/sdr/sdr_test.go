package sdr

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverUSBDevicesFrom(t *testing.T) {
	root := t.TempDir()

	// Real RTL2838 dongle with dvb_usb_rtl28xxu driver bound on interface 1-1:1.0.
	dev1 := filepath.Join(root, "1-1")
	if err := os.MkdirAll(dev1, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dev1, "idVendor"), []byte("0bda\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dev1, "idProduct"), []byte("2838\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dev1, "manufacturer"), []byte("Realtek\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dev1, "product"), []byte("RTL2838UHIDIR\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dev1, "serial"), []byte("00000001\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dev1, "busnum"), []byte("1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dev1, "devnum"), []byte("3\n"), 0o644)

	iface1 := filepath.Join(root, "1-1:1.0")
	_ = os.MkdirAll(iface1, 0o755)
	_ = os.Symlink("/sys/bus/usb/drivers/dvb_usb_rtl28xxu", filepath.Join(iface1, "driver"))

	// Unrelated USB hub should be ignored.
	hub := filepath.Join(root, "usb1")
	_ = os.MkdirAll(hub, 0o755)
	_ = os.WriteFile(filepath.Join(hub, "idVendor"), []byte("1d6b\n"), 0o644)
	_ = os.WriteFile(filepath.Join(hub, "idProduct"), []byte("0002\n"), 0o644)

	devs := DiscoverUSBDevicesFrom(root)
	if len(devs) != 1 {
		t.Fatalf("got %d devices, want 1", len(devs))
	}
	if devs[0].VendorID != "0bda" || devs[0].ProductID != "2838" {
		t.Fatalf("unexpected vid:pid %s:%s", devs[0].VendorID, devs[0].ProductID)
	}
	if !devs[0].DVBDriver {
		t.Fatalf("expected DVBDriver=true")
	}
	if devs[0].Label() == "" {
		t.Fatalf("expected non-empty label")
	}
}

func TestParseRTLTestOutput(t *testing.T) {
	out := `Found 1 device(s):
  0:  Realtek, RTL2838UHIDIR, SN: 00000001

Using device 0: Generic RTL2832U OEM
Found Rafael Micro R820T tuner
Supported gain values (29): 0.0 0.9 1.4 ...
`
	entries := ParseRTLTestOutput(out)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Tuner != "Rafael Micro R820T" {
		t.Fatalf("tuner = %q, want Rafael Micro R820T", entries[0].Tuner)
	}
	if entries[0].Serial != "00000001" {
		t.Fatalf("serial = %q, want 00000001", entries[0].Serial)
	}

	busyOut := `Found 1 device(s):
  0:  Realtek, RTL2838UHIDIR, SN: 00000001

Using device 0: Generic RTL2832U OEM
usb_claim_interface error -6
`
	busy := ParseRTLTestOutput(busyOut)
	if len(busy) != 1 || !strings.Contains(busy[0].Err, "busy") {
		t.Fatalf("expected busy claim error, got %+v", busy)
	}
}

func TestParseFreqMHz(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"98.5", 98_500_000},
		{"162.550 MHz", 162_550_000},
		{"433.92M", 433_920_000},
		{"433920k", 433_920_000},
		{"433920000", 433_920_000},
		{"1.09G", 1_090_000_000},
	}
	for _, tc := range cases {
		got, err := ParseFreqMHz(tc.in)
		if err != nil {
			t.Fatalf("ParseFreqMHz(%q) unexpected err: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseFreqMHz(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
	if _, err := ParseFreqMHz("abc"); err == nil {
		t.Fatalf("expected error for invalid freq")
	}
	if _, err := ParseFreqMHz("3000"); err == nil {
		t.Fatalf("expected error for out-of-range freq")
	}
}

func TestBuildRTLFMArgs(t *testing.T) {
	args, rate := BuildRTLFMArgs(RadioConfig{
		DeviceIndex: 0,
		FreqHz:      98_500_000,
		Mode:        ModeWBFM,
		GainDB:      -1,
		Squelch:     0,
	})
	if rate != 32000 {
		t.Fatalf("WBFM rate = %d, want 32000", rate)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-M wbfm") || !strings.Contains(joined, "-E deemp") {
		t.Fatalf("unexpected WBFM args: %s", joined)
	}

	args2, rate2 := BuildRTLFMArgs(RadioConfig{
		DeviceIndex: 1,
		FreqHz:      121_500_000,
		Mode:        ModeAM,
		GainDB:      28,
		Squelch:     15,
	})
	if rate2 != 24000 {
		t.Fatalf("AM rate = %d, want 24000", rate2)
	}
	joined2 := strings.Join(args2, " ")
	if !strings.Contains(joined2, "-d 1") || !strings.Contains(joined2, "-M am") || !strings.Contains(joined2, "-g 28") || !strings.Contains(joined2, "-l 15") {
		t.Fatalf("unexpected AM args: %s", joined2)
	}
}

func TestWAVHeaderFinalize(t *testing.T) {
	dir := t.TempDir()
	ctrl := NewRadioController(24000)
	path, err := ctrl.StartRecording(dir, 146_520_000, ModeNFM)
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]byte, 480)
	ctrl.writeRec(pcm)
	gotPath := ctrl.StopRecording()
	if gotPath != path {
		t.Fatalf("StopRecording = %q, want %q", gotPath, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 44+480 {
		t.Fatalf("file size = %d, want %d", len(b), 44+480)
	}
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatalf("invalid RIFF/WAVE header")
	}
	riffSize := binary.LittleEndian.Uint32(b[4:8])
	dataSize := binary.LittleEndian.Uint32(b[40:44])
	if riffSize != 36+480 || dataSize != 480 {
		t.Fatalf("unexpected sizes riff=%d data=%d", riffSize, dataSize)
	}
}

func TestBookmarksPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sdr.json")
	in := []Preset{{Name: "Local Repeater", FreqHz: 146_940_000, Mode: ModeNFM, GainDB: 28}}
	if err := SaveBookmarks(path, in); err != nil {
		t.Fatal(err)
	}
	got, err := LoadBookmarks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Local Repeater" || !got[0].User {
		t.Fatalf("unexpected bookmarks: %+v", got)
	}
}

func TestComputeSpectrumDetectsTone(t *testing.T) {
	const fftSize = 256
	const bins = 34
	const rate = 2_048_000.0
	// Inject a strong tone at +Fs/4 = +512 kHz -> should land at 3/4 of the shifted spectrum (~bin 25).
	toneFreq := rate / 4.0
	nSamples := fftSize * 4
	iq := make([]byte, nSamples*2)
	for i := 0; i < nSamples; i++ {
		ph := 2.0 * math.Pi * toneFreq * float64(i) / rate
		iVal := 127.5 + 100.0*math.Cos(ph)
		qVal := 127.5 + 100.0*math.Sin(ph)
		iq[i*2] = byte(iVal)
		iq[i*2+1] = byte(qVal)
	}

	out := ComputeSpectrum(iq, fftSize, bins)
	if len(out) != bins {
		t.Fatalf("len = %d, want %d", len(out), bins)
	}
	frame := BuildSpectrumFrame(98_000_000-1_024_000, 98_000_000+1_024_000, out)
	// Peak should be close to 98.512 MHz (within ±100 kHz).
	wantHz := int64(98_512_000)
	if diff := frame.PeakHz - wantHz; diff < -120_000 || diff > 120_000 {
		t.Fatalf("PeakHz = %d, want ~%d (diff=%d)", frame.PeakHz, wantHz, diff)
	}
	if frame.PeakDB <= frame.FloorDB+15 {
		t.Fatalf("expected clear peak above floor: peak=%.1f floor=%.1f", frame.PeakDB, frame.FloorDB)
	}
}

func TestParseRTLPowerCSV(t *testing.T) {
	csv := `2026-11-27, 12:00:00, 88000000, 90000000, 500000, 100, -50.0, -48.0, -12.0, -49.0
2026-11-27, 12:00:00, 90000000, 92000000, 500000, 100, -52.0, -51.0, -50.0, -49.0
`
	frame, err := ParseRTLPowerCSV(csv, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Bins) != 16 {
		t.Fatalf("bins = %d, want 16", len(frame.Bins))
	}
	if frame.StartHz != 88_000_000 || frame.EndHz != 92_000_000 {
		t.Fatalf("range = %d..%d", frame.StartHz, frame.EndHz)
	}
	if frame.PeakDB < -13.0 {
		t.Fatalf("PeakDB = %.1f, want ~-12.0", frame.PeakDB)
	}
}

func TestParseRTL433Line(t *testing.T) {
	raw := `{"time":"2026-11-27 14:22:05","model":"Acurite-Tower","id":1234,"channel":"A","battery_ok":1,"temperature_C":21.5,"humidity":48}`
	pkt, ok := ParseRTL433Line(raw)
	if !ok {
		t.Fatalf("expected valid packet")
	}
	if pkt.Time != "14:22:05" || pkt.Model != "Acurite-Tower" {
		t.Fatalf("unexpected packet header: %+v", pkt)
	}
	if !strings.Contains(pkt.Summary, "21.5C") || !strings.Contains(pkt.Summary, "48%") {
		t.Fatalf("unexpected summary: %q", pkt.Summary)
	}
	if _, ok := ParseRTL433Line("not json"); ok {
		t.Fatalf("expected false for non-json")
	}
}
