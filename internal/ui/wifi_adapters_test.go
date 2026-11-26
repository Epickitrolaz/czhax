package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/czhax/czhax/internal/wifi"
)

func contextCanceled() error { return context.Canceled }

func newTwoAdapterModel(t *testing.T) AppModel {
	t.Helper()
	m := newPentestTestModel(53, 14)
	m.WifiScan.SyncInterfaces([]wifi.Interface{
		{Name: "wlan0", Up: true, Mode: "managed", SSID: "HomeNet"},
		{Name: "wlan1", Up: true, Mode: "managed", SSID: "OfficeNet"},
	})
	m.WifiScan.SetScanResult("wlan0", []wifi.Network{
		{SSID: "HomeNet", BSSID: "AA:BB:CC:DD:EE:01", Signal: 80, Channel: "1", Security: "WPA2"},
		{SSID: "OfficeNet", BSSID: "AA:BB:CC:DD:EE:02", Signal: 70, Channel: "6", Security: "WPA2"},
		{SSID: "GuestOpen", BSSID: "AA:BB:CC:DD:EE:03", Signal: 40, Channel: "11", Security: "Open"},
	}, nil)
	return m
}

func TestWifiInUseFollowsSelectedAdapter(t *testing.T) {
	m := newTwoAdapterModel(t)
	if !m.WifiScan.NetworkInUse(wifi.Network{SSID: "HomeNet", BSSID: "AA:BB:CC:DD:EE:01"}) {
		t.Fatal("wlan0 must report HomeNet as connected")
	}
	if m.WifiScan.NetworkInUse(wifi.Network{SSID: "OfficeNet", BSSID: "AA:BB:CC:DD:EE:02"}) {
		t.Fatal("wlan0 must not report OfficeNet as connected")
	}

	// Switch scan adapter to wlan1: OfficeNet becomes the connected network.
	m.WifiScan.CycleScanIface(1)
	if m.WifiScan.EffectiveScanIface() != "wlan1" {
		t.Fatalf("expected wlan1, got %q", m.WifiScan.EffectiveScanIface())
	}
	if !m.WifiScan.NetworkInUse(wifi.Network{SSID: "OfficeNet", BSSID: "AA:BB:CC:DD:EE:02"}) {
		t.Fatal("wlan1 must report OfficeNet as connected")
	}
	if m.WifiScan.NetworkInUse(wifi.Network{SSID: "HomeNet", BSSID: "AA:BB:CC:DD:EE:01"}) {
		t.Fatal("wlan1 must not report HomeNet as connected")
	}

	// Connecting wlan1 must not steal wlan0's association marker.
	m.WifiScan.SetConnectResult(wifi.ConnectRequest{Iface: "wlan1", SSID: "OfficeNet"}, nil)
	m.WifiScan.CycleScanIface(-1)
	if !m.WifiScan.NetworkInUse(wifi.Network{SSID: "HomeNet", BSSID: "AA:BB:CC:DD:EE:01"}) {
		t.Fatal("wlan0 association was lost after connecting wlan1")
	}
	if m.WifiScan.NetworkInUse(wifi.Network{SSID: "OfficeNet", BSSID: "AA:BB:CC:DD:EE:02"}) {
		t.Fatal("wlan0 must not show wlan1's network as connected")
	}

	// The rendered list marks exactly one row per adapter.
	view := m.View()
	starCount := strings.Count(view, "*")
	if starCount != 1 {
		t.Fatalf("expected a single in-use marker, got %d:\n%s", starCount, view)
	}
}

func TestWifiPentestTriggerAndCycleDisplay(t *testing.T) {
	m := newTwoAdapterModel(t)
	// Deauth-only: no timer, key triggers a burst.
	m.WifiScan.SetScanResult("wlan0", []wifi.Network{{SSID: "HomeNet", BSSID: "AA:BB:CC:DD:EE:01", Signal: 80, Channel: "1", Security: "WPA2"}}, nil)
	ctx, req, ok := m.WifiScan.StartPentest(wifi.PentestDeauth)
	if !ok {
		t.Fatal("expected deauth pentest to start")
	}
	defer ctx.Done()
	if req.Duration != 0 {
		t.Fatalf("deauth must not run on a timer, duration=%s", req.Duration)
	}
	if req.DeauthTrigger == nil {
		t.Fatal("expected deauth trigger channel")
	}
	m.WifiScan.Mode = scanModePentest
	if !m.WifiScan.TriggerDeauth() {
		t.Fatal("expected trigger to be queued")
	}
	m.WifiScan.StopPentest()
	m.WifiScan.SetPentestResult(wifi.PentestResult{}, contextCanceled())

	// Both mode listens for 30s then bursts; verify the countdown renders.
	ctx2, req2, ok := m.WifiScan.StartPentest(wifi.PentestBoth)
	if !ok {
		t.Fatal("expected both pentest to start")
	}
	defer ctx2.Done()
	if req2.Duration != 30*time.Second {
		t.Fatalf("both listen window = %s, want 30s", req2.Duration)
	}
	m.WifiScan.PentestElapsed = 12 * time.Second
	m.WifiScan.PentestResult = wifi.PentestResult{Mode: wifi.PentestBoth, Channel: 1}
	assertFitsBounds(t, m.View(), 53, 14, "WiFi/Pentest/BothCountdown")
	if view := m.View(); !strings.Contains(view, "deauth in 18s") {
		t.Fatalf("expected deauth countdown in view:\n%s", view)
	}

	// Handshake-only runs until captured.
	m.WifiScan.StopPentest()
	m.WifiScan.SetPentestResult(wifi.PentestResult{}, contextCanceled())
	ctx3, req3, ok := m.WifiScan.StartPentest(wifi.PentestHandshake)
	if !ok {
		t.Fatal("expected handshake pentest to start")
	}
	defer ctx3.Done()
	if req3.Duration != 0 {
		t.Fatalf("handshake must be indefinite, duration=%s", req3.Duration)
	}
	m.WifiScan.PentestElapsed = 10 * time.Minute
	if view := m.View(); !strings.Contains(view, "until handshake") {
		t.Fatalf("expected indefinite handshake label:\n%s", view)
	}
}

func TestWifiListBoundsWithMultipleAdapters(t *testing.T) {
	m := newTwoAdapterModel(t)
	for _, sz := range []struct {
		w, h int
	}{{40, 10}, {45, 10}, {53, 14}} {
		m.TermW, m.TermH = sz.w, sz.h
		m.WifiScan.ScanIface = "wlan1"
		assertFitsBounds(t, m.View(), sz.w, sz.h, "WiFi/TwoAdapters")
	}
}
