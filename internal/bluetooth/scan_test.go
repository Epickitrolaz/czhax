package bluetooth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInteractiveScanKeepsSessionOpenForResults(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    "scan on"|"scan le"|"scan bredr") echo "[NEW] Device AA:BB:CC:DD:EE:09 Scripted";;
    quit*) exit 0;;
  esac
done
`
	path := filepath.Join(dir, "bluetoothctl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	out, err := interactiveBluetoothctlScan(context.Background(), ScanRequest{Mode: ScanBoth}, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("interactive scan: %v", err)
	}
	devices := ParseScanOutput(out, ScanBoth)
	if len(devices) != 1 || devices[0].Name != "Scripted" {
		t.Fatalf("expected scripted device, got %+v (raw=%q)", devices, out)
	}
}

func TestScanFailureMessages(t *testing.T) {
	for _, out := range []string{
		"Failed to start discovery: org.bluez.Error.InProgress",
		"Waiting to connect to bluetoothd...",
		"No default controller available",
	} {
		if scanFailure(out) == "" {
			t.Fatalf("expected scan failure for %q", out)
		}
	}
	if scanFailure("[NEW] Device AA:BB:CC:DD:EE:01 ok") != "" {
		t.Fatal("did not expect failure for normal scan output")
	}
}

func TestParseBtmgmtOutput(t *testing.T) {
	out := `hci0 dev_found: AA:BB:CC:DD:EE:10 type BR/EDR rssi -79 flags 0x0000
AD flags 0x06
name 60-08-84-D5-E2-9D
hci0 dev_found: AA:BB:CC:DD:EE:11 type LE Public rssi -60 flags 0x0000
AD flags 0x1a
name Proximity
`
	devices := ParseBtmgmtOutput(out)
	if len(devices) != 2 {
		t.Fatalf("expected 2 btmgmt devices, got %d: %+v", len(devices), devices)
	}
	byAddr := map[string]Device{}
	for _, d := range devices {
		byAddr[d.Address] = d
	}
	classic := byAddr["AA:BB:CC:DD:EE:10"]
	if classic.Kind != "Classic" || classic.RSSI != -79 || !classic.HasRSSI || classic.Name != "" {
		t.Fatalf("unexpected classic btmgmt device: %+v", classic)
	}
	le := byAddr["AA:BB:CC:DD:EE:11"]
	if le.Kind != "BLE" || le.RSSI != -60 || le.Name != "Proximity" {
		t.Fatalf("unexpected LE btmgmt device: %+v", le)
	}
}

func TestCleanDeviceNameDropsMACAlias(t *testing.T) {
	for _, name := range []string{"60-08-84-D5-E2-9D", "60:08:84:D5:E2:9D", "600884D5E29D"} {
		if got := cleanDeviceName(name); got != "" {
			t.Fatalf("cleanDeviceName(%q) = %q, want empty", name, got)
		}
	}
	if got := cleanDeviceName("Proximity"); got != "Proximity" {
		t.Fatalf("cleanDeviceName(Proximity) = %q", got)
	}
	d := Device{Name: "60-08-84-D5-E2-9D", Address: "60:08:84:D5:E2:9D"}
	if got := d.DisplayName(); got != "60:08:84:D5:E2:9D" {
		t.Fatalf("DisplayName with MAC alias = %q", got)
	}
}

func TestMergeDeviceListsEnrichesRSSIAndName(t *testing.T) {
	base := []Device{{Address: "AA:BB:CC:DD:EE:12", Name: ""}}
	extra := []Device{{Address: "AA:BB:CC:DD:EE:12", Name: "Speaker", RSSI: -55, HasRSSI: true, Kind: "Classic"}}
	got := mergeDeviceLists(base, extra)
	if len(got) != 1 || got[0].Name != "Speaker" || got[0].RSSI != -55 || !got[0].HasRSSI {
		t.Fatalf("unexpected merged device: %+v", got)
	}
}

func TestBtmgmtSessionAddsRSSIAndName(t *testing.T) {
	if !canSudo() {
		t.Skip("btmgmt needs root or passwordless sudo")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    find*) echo "hci0 dev_found: AA:BB:CC:DD:EE:20 type BR/EDR rssi -70 flags 0x0000"; echo "AD flags 0x06"; echo "name Car Kit";;
    quit*) exit 0;;
  esac
done
`
	path := filepath.Join(dir, "btmgmt")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	devices, err := scanWithBtmgmt(context.Background(), ScanRequest{HCI: "hci0", Mode: ScanBoth}, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("btmgmt session: %v", err)
	}
	if len(devices) != 1 || devices[0].Name != "Car Kit" || devices[0].RSSI != -70 || !devices[0].HasRSSI {
		t.Fatalf("unexpected btmgmt devices: %+v", devices)
	}
}

func TestParseScanOutputMergesClassicAndBLE(t *testing.T) {
	out := `[NEW] Device AA:BB:CC:DD:EE:01 Headset
[CHG] Device AA:BB:CC:DD:EE:01 RSSI: -54
[CHG] Device AA:BB:CC:DD:EE:01 Class: 0x240404
[CHG] Device AA:BB:CC:DD:EE:01 Icon: audio-card
[CHG] Device AA:BB:CC:DD:EE:01 UUIDs: 0000110b-0000-1000-8000-00805f9b34fb
[NEW] Device AA:BB:CC:DD:EE:02
[CHG] Device AA:BB:CC:DD:EE:02 Name: Buds
[CHG] Device AA:BB:CC:DD:EE:02 RSSI: -72
[CHG] Device AA:BB:CC:DD:EE:02 ServiceData Key: 0000fe2c-0000-1000-8000-00805f9b34fb
[CHG] Device AA:BB:CC:DD:EE:02 ServiceData Value: 0x78
[CHG] Device AA:BB:CC:DD:EE:02 ServiceData Value: 0x56
[CHG] Device AA:BB:CC:DD:EE:02 ServiceData Value: 0x34
[CHG] Device AA:BB:CC:DD:EE:02 Paired: yes
[CHG] Device AA:BB:CC:DD:EE:02 Connected: yes
`
	devices := ParseScanOutput(out, ScanBoth)
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d: %+v", len(devices), devices)
	}
	headset := devices[1]
	if headset.Address != "AA:BB:CC:DD:EE:01" || headset.Name != "Headset" || headset.RSSI != -54 || headset.Kind != "Classic" {
		t.Fatalf("unexpected classic device: %+v", headset)
	}
	buds := devices[0] // Fast Pair sorts first
	if buds.Name != "Buds" || buds.RSSI != -72 || !buds.FastPair || !buds.HasModelID || buds.ModelID != 0x345678 {
		t.Fatalf("unexpected Fast Pair device: %+v", buds)
	}
	if buds.Kind != "BLE" || !buds.Paired || !buds.Connected {
		t.Fatalf("unexpected Fast Pair flags: %+v", buds)
	}
}

func TestParseScanOutputModeKinds(t *testing.T) {
	out := "[NEW] Device AA:BB:CC:DD:EE:03 Mystery\n[CHG] Device AA:BB:CC:DD:EE:03 RSSI: -40\n"
	for _, tc := range []struct {
		mode ScanMode
		want string
	}{
		{ScanLE, "BLE"},
		{ScanClassic, "Classic"},
		{ScanBoth, "BT"},
	} {
		devices := ParseScanOutput(out, tc.mode)
		if len(devices) != 1 || devices[0].Kind != tc.want {
			t.Fatalf("mode %v: expected kind %q, got %+v", tc.mode, tc.want, devices)
		}
	}
}

func TestParseScanOutputANSIAndCachedDevices(t *testing.T) {
	out := "\x1b[0;94m[NEW]\x1b[0m Device AA:BB:CC:DD:EE:06 Speaker\r" +
		"\x1b[0;94m[CHG]\x1b[0m Device AA:BB:CC:DD:EE:06 RSSI: -61\r" +
		"[bluetooth]# Device AA:BB:CC:DD:EE:07 Cached Phone\r"
	devices := ParseScanOutput(out, ScanBoth)
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices from ANSI/CR output, got %d: %+v", len(devices), devices)
	}
	byAddr := map[string]Device{}
	for _, d := range devices {
		byAddr[d.Address] = d
	}
	if byAddr["AA:BB:CC:DD:EE:06"].Name != "Speaker" || byAddr["AA:BB:CC:DD:EE:06"].RSSI != -61 {
		t.Fatalf("unexpected ANSI scan parse: %+v", byAddr["AA:BB:CC:DD:EE:06"])
	}
	if byAddr["AA:BB:CC:DD:EE:07"].DisplayName() != "Cached Phone" {
		t.Fatalf("cached device name not parsed: %+v", byAddr["AA:BB:CC:DD:EE:07"])
	}
}

func TestParseHCIToolOutput(t *testing.T) {
	devices := ParseHCIToolOutput("Scanning ...\nAA:BB:CC:DD:EE:04 Phone\n", "classic")
	if len(devices) != 1 || devices[0].Name != "Phone" || devices[0].Kind != "Classic" {
		t.Fatalf("unexpected hcitool parse: %+v", devices)
	}
}

func TestParseDeviceInfo(t *testing.T) {
	out := `Device AA:BB:CC:DD:EE:05 (Public)
	Name: Phone
	Alias: My Phone
	Class: 0x5a020c
	Icon: phone
	Paired: yes
	Trusted: no
	Connected: yes
	RSSI: -62
	UUID: 0000fe2c-0000-1000-8000-00805f9b34fb
	UUID: 0000110b-0000-1000-8000-00805f9b34fb
`
	d := ParseDeviceInfo(out)
	if d.Address != "AA:BB:CC:DD:EE:05" || d.Name != "Phone" || d.Alias != "My Phone" || !d.Paired || d.Trusted || !d.Connected || d.RSSI != -62 {
		t.Fatalf("unexpected info: %+v", d)
	}
	if !d.FastPair || len(d.UUIDs) != 2 {
		t.Fatalf("expected Fast Pair UUID parsing: %+v", d)
	}
}

func TestParseBDAddrRoundTrip(t *testing.T) {
	const want = "AA:BB:CC:DD:EE:FF"
	addr, err := ParseBDAddr(want)
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatBDAddr(addr); got != want {
		t.Fatalf("round trip = %q, want %q", got, want)
	}
	if addr[0] != 0xFF || addr[5] != 0xAA {
		t.Fatalf("address byte order = %v", addr)
	}
	if _, err := ParseBDAddr("nope"); err == nil {
		t.Fatal("expected invalid address error")
	}
}

func TestScanModeLabels(t *testing.T) {
	cases := []struct {
		mode ScanMode
		arg  string
		str  string
	}{
		{ScanBoth, "on", "BLE+Classic"},
		{ScanClassic, "bredr", "Classic"},
		{ScanLE, "le", "BLE"},
	}
	for _, tc := range cases {
		if tc.mode.Arg() != tc.arg || tc.mode.String() != tc.str {
			t.Fatalf("mode %v: arg=%q str=%q", tc.mode, tc.mode.Arg(), tc.mode.String())
		}
	}
}
