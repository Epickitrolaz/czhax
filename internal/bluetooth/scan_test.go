package bluetooth

import "testing"

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
