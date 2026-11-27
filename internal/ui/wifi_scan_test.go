package ui

import (
	"testing"

	"github.com/czhax/czhax/internal/wifi"
)

// TestConnectReusesScanIface is the reported bug: connecting asked which wlan to
// use even though the interface had already been chosen when scanning.
func TestConnectReusesScanIface(t *testing.T) {
	var v WifiScanView
	v.Interfaces = []wifi.Interface{{Name: "wlan0"}, {Name: "wlan1"}}
	v.ScanIface = "wlan1"
	v.Networks = []wifi.Network{{SSID: "Home", BSSID: "AA:BB:CC:DD:EE:FF", Security: "WPA2"}}

	if start := v.BeginConnect(scanModeList); start {
		t.Fatal("a WPA2 network must prompt for the passphrase, not connect directly")
	}
	if v.Mode != scanModePassword {
		t.Fatalf("expected the passphrase prompt, got mode %v", v.Mode)
	}
	if v.ConnectIface != "wlan1" {
		t.Fatalf("expected the scan interface wlan1, got %q", v.ConnectIface)
	}

	// Esc returns to where the flow started, never to an interface picker.
	if !v.HandleEsc() {
		t.Fatal("esc should be handled from the password prompt")
	}
	if v.Mode != scanModeList {
		t.Fatalf("expected back to the list, got mode %v", v.Mode)
	}

	// After switching the scan interface the next connect follows it.
	v.ScanIface = "wlan0"
	if start := v.BeginConnect(scanModeDetail); start {
		t.Fatal("expected a passphrase prompt")
	}
	if v.ConnectIface != "wlan0" {
		t.Fatalf("expected wlan0 after switching the scan interface, got %q", v.ConnectIface)
	}
	if v.Mode != scanModePassword {
		t.Fatalf("expected the passphrase prompt, got mode %v", v.Mode)
	}
}

// An open network connects straight away on the scan interface.
func TestConnectOpenNetworkStartsImmediately(t *testing.T) {
	var v WifiScanView
	v.Interfaces = []wifi.Interface{{Name: "wlan0"}}
	v.ScanIface = "wlan0"
	v.Networks = []wifi.Network{{SSID: "FreeSpot", BSSID: "AA:BB:CC:DD:EE:FF", Security: "Open"}}

	if start := v.BeginConnect(scanModeList); !start {
		t.Fatal("an open network should connect immediately")
	}
	if !v.Connecting || v.ConnectIface != "wlan0" {
		t.Fatalf("connecting=%v iface=%q", v.Connecting, v.ConnectIface)
	}
}
