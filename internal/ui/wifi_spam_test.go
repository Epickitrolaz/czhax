package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/localscan"
	"github.com/czhax/czhax/internal/wifi"
)

// TestWifiSpamAndEvilTabsBounds walks both new WiFi subtabs at every supported
// size and asserts they render strictly inside the viewport.
func TestWifiSpamAndEvilTabsBounds(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"CZ_40x10", 40, 10},
		{"CZ_45x10", 45, 10},
		{"CZ_53x14", 53, 14},
		{"Wide_100x30", 100, 30},
	}
	for _, sz := range sizes {
		cfg := config.Default()
		cfg.WorkDir = t.TempDir()
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.36")
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)
		m.ActiveView = ViewWiFi
		m.Focus = FocusContent
		m.WifiSpam.SyncInterfaces([]wifi.Interface{{Name: "wlan0"}, {Name: "wlan1"}})
		m.WifiEvil.SyncInterfaces([]wifi.Interface{{Name: "wlan0"}, {Name: "wlan1"}})

		// Spam tab.
		m.ActiveTabs[ViewWiFi] = WiFiTabSpam
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/Spam")
		if !strings.Contains(m.View(), "Beacon spam") {
			t.Fatalf("[%s] expected the beacon spam screen:\n%s", sz.name, m.View())
		}
		// Every field variant of the source selector must fit too.
		for _, src := range []WifiSpamSource{SpamSourceSingle, SpamSourceNumbered, SpamSourceFile} {
			m.WifiSpam.Source = src
			m.WifiSpam.Cursor = 0
			assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/Spam/src")
		}
		m.WifiSpam.Mode = spamModeEdit
		m.WifiSpam.Input = "a-very-long-ssid-name-for-marquee"
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/Spam/edit")
		m.WifiSpam.Mode = spamModeRunning
		m.WifiSpam.Active = true
		m.WifiSpam.Seq = 1
		m.WifiSpam.Stats = "25 SSIDs"
		m.WifiSpam.Lines = []string{"beacons 1200 · ssids 25 · bssids 3900 · 1m20s"}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/Spam/running")
		m.WifiSpam.Mode = spamModeSetup
		m.WifiSpam.Active = false

		// Evil tab.
		m.ActiveTabs[ViewWiFi] = WiFiTabEvil
		m.WifiEvil.SSID = "CoffeeShop-Free-WiFi"
		m.WifiEvil.TargetBSSID = "EC:81:9C:1C:5C:44"
		m.WifiEvil.Deauth = true
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/Evil")
		if !strings.Contains(m.View(), "Evil twin") {
			t.Fatalf("[%s] expected the evil twin screen:\n%s", sz.name, m.View())
		}
		m.WifiEvil.Mode = evilModeEditSSID
		m.WifiEvil.Input = "CoffeeShop-Free-WiFi"
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/Evil/edit")
		m.WifiEvil.Mode = evilModeRunning
		m.WifiEvil.Active = true
		m.WifiEvil.Seq = 1
		m.WifiEvil.Lines = []string{"Twin AP \"CoffeeShop-Free-WiFi\" up on wlan0 (channel 6)", "Captive portal on http://192.168.66.1"}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/Evil/running")
	}
}

// TestWifiTabNavigationReachesNewTabs checks the horizontal tab bar exposes the
// two new subtabs.
func TestWifiTabNavigationReachesNewTabs(t *testing.T) {
	cfg := config.Default()
	cfg.WorkDir = t.TempDir()
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.36")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 53, Height: 14})
	m = next.(AppModel)
	m.ActiveView = ViewWiFi
	m.Focus = FocusTabs

	m = sendKey(m, "right")
	if m.ActiveTabs[ViewWiFi] != WiFiTabSpam {
		t.Fatalf("expected the Spam tab, got %d", m.ActiveTabs[ViewWiFi])
	}
	m = sendKey(m, "right")
	if m.ActiveTabs[ViewWiFi] != WiFiTabEvil {
		t.Fatalf("expected the Evil tab, got %d", m.ActiveTabs[ViewWiFi])
	}
	m = sendKey(m, "enter")
	if m.Focus != FocusContent {
		t.Fatal("expected content focus on the Evil tab")
	}
}

// TestSpamViewSSIDSources covers the three input modes the request describes.
func TestSpamViewSSIDSources(t *testing.T) {
	v := NewWifiSpamView()
	v.SingleName = "TestNet"
	got, err := v.SSIDs()
	if err != nil || len(got) != 1 || got[0] != "TestNet" {
		t.Fatalf("single source = %q, %v", got, err)
	}

	v = NewWifiSpamView()
	v.Source = SpamSourceNumbered
	v.Prefix = "Cafe"
	v.Count = 4
	got, err = v.SSIDs()
	if err != nil || len(got) != 4 || got[0] != "Cafe1" || got[3] != "Cafe4" {
		t.Fatalf("numbered source = %q, %v", got, err)
	}

	v = NewWifiSpamView()
	v.Source = SpamSourceFile
	if _, err := v.SSIDs(); err == nil {
		t.Fatal("expected an error without a file")
	}
}

// TestSanitizeLogLine covers the report that hcxdumptool's full-screen status
// display scribbled over the TUI: escape sequences and carriage-return redraws
// must never reach the renderer.
func TestSanitizeLogLine(t *testing.T) {
	cases := map[string]string{
		"\x1b[?25lInitialize main scan loop...": "Initialize main scan loop...",
		"CHA   LAST\x1b[2J\x1b[H ESSID":         "CHA   LAST ESSID",
		"progress 50%\rprogress 100%":           "progress 100%",
		"  \x1b[1;32mSaved capture\x1b[0m  ":    "Saved capture",
		"tab\tseparated":                        "tab separated",
		"\x1b]0;title\x07done":                  "done",
	}
	for in, want := range cases {
		if got := sanitizeLogLine(in); got != want {
			t.Errorf("sanitizeLogLine(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("x", 400)
	if got := sanitizeLogLine(long); len(got) != maxLogLineLen {
		t.Errorf("long line not capped: %d", len(got))
	}
}

// TestSpamLimits covers the requested range: 1000 SSIDs and a 0 ms delay that
// always stays on the 5 ms grid.
func TestSpamLimits(t *testing.T) {
	v := NewWifiSpamView()
	v.Count = 990
	v.Cursor = 0
	// Count field is index 2 in numbered mode, 1 in single; drive it directly.
	v.Source = SpamSourceNumbered
	for i, f := range v.fields() {
		if f == spamFieldCount {
			v.Cursor = i
		}
	}
	for i := 0; i < 10; i++ {
		v.Adjust(1)
	}
	if v.Count != 1000 {
		t.Fatalf("count clamped to %d, want 1000", v.Count)
	}
	for i, f := range v.fields() {
		if f == spamFieldInterval {
			v.Cursor = i
		}
	}
	v.IntervalMS = 5
	v.Adjust(-1)
	v.Adjust(-1)
	if v.IntervalMS != 0 {
		t.Fatalf("delay = %d, want 0", v.IntervalMS)
	}
	v.Adjust(-1)
	if v.IntervalMS != 0 {
		t.Fatalf("delay went below zero: %d", v.IntervalMS)
	}
	ctx, _, _, _, err := v.Start(t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if v.IntervalMS != 0 {
		t.Fatalf("start normalised delay to %d, want 0", v.IntervalMS)
	}
	v.Stop()
	_ = ctx

	// A value off the grid is snapped down to a multiple of five.
	v.IntervalMS = 13
	ctx2, _, _, _, err := v.Start(t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if v.IntervalMS != 10 {
		t.Fatalf("13 ms snapped to %d, want 10", v.IntervalMS)
	}
	v.Stop()
	_ = ctx2
}

// TestEvilFromScanPrefillsWithoutStarting is the "one button, prefilled" request.
func TestEvilFromScanPrefillsWithoutStarting(t *testing.T) {
	cfg := config.Default()
	cfg.WorkDir = t.TempDir()
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.38")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 53, Height: 14})
	m = next.(AppModel)
	m.WifiScan.SyncInterfaces([]wifi.Interface{{Name: "wlan0"}})
	m.WifiEvil.SyncInterfaces([]wifi.Interface{{Name: "wlan0"}})
	m.WifiScan.SetScanResult("wlan0", []wifi.Network{{
		SSID: "CafeFree", BSSID: "AA:BB:CC:DD:EE:FF", Channel: "6", Security: "WPA2",
	}}, nil)
	m.WifiScan.Cursor = 0

	next, _ = m.openWifiEvilFromScan()
	m = next.(AppModel)

	if m.ActiveTabs[ViewWiFi] != WiFiTabEvil {
		t.Fatalf("expected the Evil tab, got %d", m.ActiveTabs[ViewWiFi])
	}
	if m.WifiEvil.SSID != "CafeFree" || m.WifiEvil.Channel != 6 || m.WifiEvil.TargetBSSID != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("twin not prefilled: %+v", m.WifiEvil)
	}
	if m.WifiEvil.Active || m.WifiEvil.Mode == evilModeRunning {
		t.Fatal("the twin must not start by itself")
	}
	if m.WifiEvil.Deauth {
		t.Fatal("deauth must stay off by default")
	}
	if !m.WifiEvil.Portal || !m.WifiEvil.NAT {
		t.Fatal("the remaining options must keep their defaults")
	}
}

// TestEvilUplinkSupportsEthernetAndUSB is the "uplink can be eth0 or USB
// tethering" request: the picker must offer wired and tethered interfaces, not
// only spare wifi, and exclude the AP interface itself.
func TestEvilUplinkSupportsEthernetAndUSB(t *testing.T) {
	v := NewWifiEvilView()
	v.SyncInterfaces([]wifi.Interface{{Name: "wlan0"}, {Name: "wlan1"}})
	v.SyncUplinks([]localscan.Interface{
		{Name: "wlan0", Kind: localscan.KindWiFi, Up: true, Connected: true},
		{Name: "eth0", Kind: localscan.KindEthernet, Up: true, Connected: true},
		{Name: "usb0", Kind: localscan.KindUSB, Up: true, Connected: true},
	})

	if got := v.EffectiveAP(); got != "wlan0" {
		t.Fatalf("AP = %q, want wlan0", got)
	}
	// A connected wired/tethered uplink is preferred over wifi.
	if got := v.UplinkIface(); got != "eth0" {
		t.Fatalf("uplink = %q, want eth0", got)
	}
	if label, ok := v.uplinkLabel(); !ok || label != "eth0 · Eth" {
		t.Fatalf("uplink label = %q (%v), want \"eth0 · Eth\"", label, ok)
	}

	// Cycling reaches the USB tether and never the AP interface.
	for i, f := range v.fields() {
		if f == evilFieldUplink {
			v.Cursor = i
		}
	}
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		seen[v.UplinkIface()] = true
		v.Adjust(1)
	}
	if seen["wlan0"] {
		t.Fatalf("AP interface offered as uplink: %v", seen)
	}
	if !seen["usb0"] || !seen["eth0"] {
		t.Fatalf("expected eth0 and usb0 in the uplink list, saw %v", seen)
	}

	// NAT passes the chosen uplink to the helper.
	cfg := v.Config()
	if cfg.UplinkIface == "" || !cfg.NAT {
		t.Fatalf("config lost the uplink: %+v", cfg)
	}
}

// TestEvilUplinkFallsBackToWifi keeps the single-adapter case working.
func TestEvilUplinkFallsBackToWifi(t *testing.T) {
	v := NewWifiEvilView()
	v.SyncInterfaces([]wifi.Interface{{Name: "wlan0"}, {Name: "wlan1"}})
	if got := v.UplinkIface(); got != "wlan1" {
		t.Fatalf("uplink = %q, want wlan1", got)
	}
	v.SyncInterfaces([]wifi.Interface{{Name: "wlan0"}})
	if got := v.UplinkIface(); got != "" {
		t.Fatalf("uplink = %q, want none with a single radio", got)
	}
}
