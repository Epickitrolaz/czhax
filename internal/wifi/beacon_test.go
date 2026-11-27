package wifi

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestBuildBeaconFrame(t *testing.T) {
	bssid := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	frame, err := BuildBeaconFrame("FreeWiFi", bssid, 7, 11)
	if err != nil {
		t.Fatalf("BuildBeaconFrame: %v", err)
	}
	rtLen := int(binary.LittleEndian.Uint16(frame[2:4]))
	if rtLen != 8 {
		t.Fatalf("radiotap length = %d, want 8", rtLen)
	}
	dot11 := frame[rtLen:]
	if dot11[0] != 0x80 {
		t.Fatalf("frame control = %#x, want 0x80 (beacon)", dot11[0])
	}
	if got := dot11[4:10]; string(got) != "\xff\xff\xff\xff\xff\xff" {
		t.Fatalf("addr1 = % x, want broadcast", got)
	}
	if string(dot11[10:16]) != string(bssid[:]) || string(dot11[16:22]) != string(bssid[:]) {
		t.Fatalf("addr2/addr3 must carry the BSSID")
	}
	// Fixed fields: timestamp(8) interval(2) capability(2), then IEs.
	ies := dot11[24+12:]
	if ies[0] != 0x00 || int(ies[1]) != len("FreeWiFi") || string(ies[2:2+len("FreeWiFi")]) != "FreeWiFi" {
		t.Fatalf("first IE must be the SSID, got % x", ies[:12])
	}
	// DS parameter set follows the rates IE and carries the channel.
	if !strings.Contains(string(ies), "\x03\x01\x0b") {
		t.Fatalf("missing DS parameter IE for channel 11: % x", ies)
	}
	if _, err := BuildBeaconFrame(strings.Repeat("x", 33), bssid, 0, 6); err == nil {
		t.Fatal("expected an error for an oversized SSID")
	}
}

func TestParseSSIDListAndNumbering(t *testing.T) {
	raw := "Home\n\n# comment\r\nCafe  \nHome\n" + strings.Repeat("y", 40) + "\n"
	list := ParseSSIDList(raw)
	want := []string{"Home", "Cafe", strings.Repeat("y", MaxSSIDLen)}
	if len(list) != len(want) {
		t.Fatalf("ParseSSIDList = %q, want %q", list, want)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("entry %d = %q, want %q", i, list[i], want[i])
		}
	}

	numbered := NumberedSSIDs("CZHax", 3)
	if len(numbered) != 3 || numbered[0] != "CZHax1" || numbered[2] != "CZHax3" {
		t.Fatalf("NumberedSSIDs = %q", numbered)
	}
	long := NumberedSSIDs(strings.Repeat("z", 40), 1)
	if len([]byte(long[0])) > MaxSSIDLen {
		t.Fatalf("numbered SSID exceeds 32 bytes: %q", long[0])
	}
}

func TestEvilConfigs(t *testing.T) {
	hap := HostapdConfig(EvilConfig{Iface: "wlan0", SSID: "CafeFree", Channel: 11})
	for _, want := range []string{"interface=wlan0", "ssid=CafeFree", "channel=11", "hw_mode=g"} {
		if !strings.Contains(hap, want) {
			t.Fatalf("hostapd config missing %q:\n%s", want, hap)
		}
	}
	dns := DnsmasqConfig("wlan0", EvilGateway)
	for _, want := range []string{
		"interface=wlan0",
		"dhcp-range=" + EvilSubnet,
		"address=/#/" + EvilGateway,
		"dhcp-option=6," + EvilGateway,
	} {
		if !strings.Contains(dns, want) {
			t.Fatalf("dnsmasq config missing %q:\n%s", want, dns)
		}
	}
	if !strings.Contains(PortalPage, "<form") {
		t.Fatal("portal page needs a sign-in form")
	}
}

func TestEvilTwinRejectsEmptySSID(t *testing.T) {
	err := RunEvilTwin(nil, EvilConfig{Iface: "wlan0"}, nil)
	if err == nil || !strings.Contains(err.Error(), "SSID") {
		t.Fatalf("expected an SSID error, got %v", err)
	}
}
