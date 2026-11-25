package wifi

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseNmcliScan(t *testing.T) {
	raw := strings.Join([]string{
		`*:M&P\:24:5C\:7B\:5C\:46\:E7\:A8:Infra:1:2412 MHz:540 Mbit/s:62:WPA1 WPA2`,
		` ::5C\:7B\:5C\:46\:E7\:AB:Infra:1:2412 MHz:540 Mbit/s:50:WPA2`,
		` :NETIASPOT-E6j5:EC\:81\:9C\:1C\:5C\:44:Infra:3:2422 MHz:130 Mbit/s:32:WPA2 WPA3`,
		` :CafeOpen:AA\:BB\:CC\:DD\:EE\:FF:Infra:6:2437 MHz:54 Mbit/s:85:--`,
		// Duplicate SSID+BSSID with lower signal should not overwrite higher one
		` :CafeOpen:AA\:BB\:CC\:DD\:EE\:FF:Infra:6:2437 MHz:54 Mbit/s:40:--`,
	}, "\n")

	nets := parseNmcliScan(raw)
	sortNetworks(nets)
	if len(nets) != 4 {
		t.Fatalf("expected 4 networks, got %d: %+v", len(nets), nets)
	}

	// InUse sorts first
	if !nets[0].InUse || nets[0].SSID != "M&P:24" || nets[0].BSSID != "5C:7B:5C:46:E7:A8" || nets[0].Signal != 62 {
		t.Fatalf("unexpected first network: %+v", nets[0])
	}
	// Next highest signal: CafeOpen (85%)
	if nets[1].SSID != "CafeOpen" || nets[1].Signal != 85 || nets[1].ShortSecurity() != "Open" {
		t.Fatalf("unexpected second network: %+v", nets[1])
	}
	// Hidden network
	if nets[2].DisplaySSID() != "<hidden>" || nets[2].Signal != 50 {
		t.Fatalf("unexpected hidden network: %+v", nets[2])
	}
	// WPA2 WPA3
	if nets[3].ShortSecurity() != "WPA2/3" {
		t.Fatalf("expected WPA2/3, got %q", nets[3].ShortSecurity())
	}
}

func TestParseIWScan(t *testing.T) {
	raw := `BSS 5c:7b:5c:46:e7:a8(on wlan0) -- associated
	freq: 2412.0
	capability: ESS Privacy ShortPreamble (0x1031)
	signal: -62.00 dBm
	SSID: LabNet
	DS Parameter set: channel 1
	RSN:	 * Version: 1
		 * Authentication suites: PSK
BSS 11:22:33:44:55:66(on wlan0)
	freq: 5180.0
	capability: ESS (0x0401)
	signal: -80.00 dBm
	SSID: GuestOpen
	DS Parameter set: channel 36
BSS aa:bb:cc:dd:ee:ff(on wlan0)
	freq: 2437.0
	capability: ESS Privacy (0x0411)
	signal: -45.00 dBm
	SSID: FastSAE
	RSN:	 * Version: 1
		 * Authentication suites: SAE
`
	nets := parseIWScan(raw)
	sortNetworks(nets)
	if len(nets) != 3 {
		t.Fatalf("expected 3 networks, got %d", len(nets))
	}
	if !nets[0].InUse || nets[0].SSID != "LabNet" || !nets[0].HasRSSI || nets[0].RSSI != -62 || nets[0].Signal != 76 || nets[0].ShortSecurity() != "WPA2" {
		t.Fatalf("unexpected associated iw net: %+v", nets[0])
	}
	if nets[1].SSID != "FastSAE" || nets[1].Signal != 100 || nets[1].ShortSecurity() != "WPA3" {
		t.Fatalf("unexpected SAE net: %+v", nets[1])
	}
	if nets[2].SSID != "GuestOpen" || nets[2].Signal != 40 || nets[2].ShortSecurity() != "Open" {
		t.Fatalf("unexpected open net: %+v", nets[2])
	}
}

func TestScanWithSudoAndFallback(t *testing.T) {
	var calls [][]string
	s := scanner{
		euid:   func() int { return 1000 },
		hasBin: func(string) bool { return true },
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			calls = append(calls, append([]string{name}, args...))
			if name == "sudo" && len(args) >= 2 && filepath.Base(args[1]) == "nmcli" {
				return " :TestAP:11\\:22\\:33\\:44\\:55\\:66:Infra:6:2437 MHz:130 Mbit/s:77:WPA2\n", nil
			}
			return "", errors.New("fail")
		},
	}

	nets, err := scanWith(s, "")
	if err != nil {
		t.Fatalf("unexpected scan err: %v", err)
	}
	if len(nets) != 1 || nets[0].SSID != "TestAP" {
		t.Fatalf("unexpected scan result: %+v", nets)
	}
	if len(calls) != 1 || calls[0][0] != "sudo" || calls[0][len(calls[0])-1] != "wlan0" {
		t.Fatalf("expected sudo nmcli on default wlan0, got %+v", calls)
	}

	// Fallback to iw when nmcli fails and sudo needs password
	s2 := scanner{
		euid:   func() int { return 1000 },
		hasBin: func(string) bool { return true },
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name == "sudo" {
				return "sudo: a password is required", errors.New("exit 1")
			}
			if name == "nmcli" {
				return "Error: unmanaged device", errors.New("exit 10")
			}
			if name == "iw" && len(args) == 3 && args[0] == "dev" && args[1] == "wlan1" && args[2] == "scan" {
				return "BSS aa:bb:cc:dd:ee:ff(on wlan1)\n\tSSID: USBNet\n\tsignal: -55.00 dBm\n", nil
			}
			return "", errors.New("unexpected")
		},
	}

	nets2, err := scanWith(s2, "wlan1")
	if err != nil {
		t.Fatalf("unexpected fallback err: %v", err)
	}
	if len(nets2) != 1 || nets2[0].SSID != "USBNet" || nets2[0].Signal != 90 {
		t.Fatalf("unexpected iw fallback result: %+v", nets2)
	}
}

func TestConnectWithArgs(t *testing.T) {
	var calls [][]string
	s := scanner{
		euid:   func() int { return 0 },
		hasBin: func(string) bool { return true },
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name != "nmcli" {
				return "", errors.New("unexpected binary")
			}
			calls = append(calls, append([]string(nil), args...))
			if len(args) >= 2 && args[0] == "-t" && args[len(args)-1] == "show" {
				return "1111-2222:czhax-wlan1-TargetNet:802-11-wireless\n3333-4444:netplan-wlan0-M&P24:802-11-wireless\n", nil
			}
			if len(args) >= 2 && args[0] == "-g" {
				if args[len(args)-1] == "1111-2222" {
					return "TargetNet\nwlan1\n", nil
				}
				return "M&P24\nwlan0\n", nil
			}
			return "ok\n", nil
		},
	}

	err := connectWith(s, ConnectRequest{
		Iface:    "wlan1",
		SSID:     "TargetNet",
		Password: "secret",
		Security: "WPA2",
	})
	if err != nil {
		t.Fatalf("unexpected connect err: %v", err)
	}
	wantDel := []string{"con", "delete", "uuid", "1111-2222"}
	wantAdd := []string{"con", "add", "type", "wifi", "con-name", "czhax-wlan1-TargetNet", "ifname", "wlan1", "ssid", "TargetNet", "wifi-sec.key-mgmt", "wpa-psk", "wifi-sec.psk", "secret"}
	wantUp := []string{"con", "up", "id", "czhax-wlan1-TargetNet", "ifname", "wlan1"}
	if len(calls) != 4 || !reflect.DeepEqual(calls[1], wantDel) || !reflect.DeepEqual(calls[2], wantAdd) || !reflect.DeepEqual(calls[3], wantUp) {
		t.Fatalf("unexpected calls for WPA2 connect: %+v", calls)
	}

	// Open hidden network omits wifi-sec and sets 802-11-wireless.hidden yes
	calls = nil
	err = connectWith(s, ConnectRequest{
		Iface:    "",
		SSID:     "HiddenOpen",
		Password: "ignored",
		Security: "Open",
		Hidden:   true,
	})
	if err != nil {
		t.Fatalf("unexpected open hidden err: %v", err)
	}
	wantOpenAdd := []string{"con", "add", "type", "wifi", "con-name", "czhax-wlan0-HiddenOpen", "ifname", "wlan0", "ssid", "HiddenOpen", "802-11-wireless.hidden", "yes"}
	wantOpenUp := []string{"con", "up", "id", "czhax-wlan0-HiddenOpen", "ifname", "wlan0"}
	if len(calls) != 3 || !reflect.DeepEqual(calls[1], wantOpenAdd) || !reflect.DeepEqual(calls[2], wantOpenUp) {
		t.Fatalf("unexpected calls for open hidden: %+v", calls)
	}

	// Blank password on secured network activates saved profile via con up uuid
	calls = nil
	err = connectWith(s, ConnectRequest{
		Iface:    "wlan0",
		SSID:     "M&P24",
		Password: "",
		Security: "WPA1 WPA2",
	})
	if err != nil {
		t.Fatalf("unexpected saved profile connect err: %v", err)
	}
	wantSavedUp := []string{"con", "up", "uuid", "3333-4444", "ifname", "wlan0"}
	if len(calls) != 4 || !reflect.DeepEqual(calls[3], wantSavedUp) {
		t.Fatalf("expected saved profile con up, got %+v", calls)
	}

	// Failed con up cleans up the temporary czhax profile
	calls = nil
	sFail := scanner{
		euid:   func() int { return 0 },
		hasBin: func(string) bool { return true },
		runCmd: func(_ context.Context, _ string, args ...string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			if len(args) >= 2 && args[0] == "con" && args[1] == "up" {
				return "Error: Secrets were required, but not provided.\n", errors.New("exit 4")
			}
			return "", nil
		},
	}
	err = connectWith(sFail, ConnectRequest{
		Iface:    "wlan0",
		SSID:     "WrongPassNet",
		Password: "bad",
		Security: "WPA3",
	})
	if err == nil || !strings.Contains(err.Error(), "Secrets were required") {
		t.Fatalf("expected Secrets error, got %v", err)
	}
	lastCall := calls[len(calls)-1]
	wantCleanup := []string{"con", "delete", "id", "czhax-wlan0-WrongPassNet"}
	if !reflect.DeepEqual(lastCall, wantCleanup) {
		t.Fatalf("expected cleanup delete call %v, got %v", wantCleanup, lastCall)
	}
}
