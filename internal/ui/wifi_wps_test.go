package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

func newWPSTestModel(t *testing.T, w, h int) AppModel {
	t.Helper()
	m := newPentestTestModel(w, h)
	m.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	m.WifiScan.OneShot = wifi.OneShot{Dir: "/tmp/OneShot-Extended", Script: "/tmp/OneShot-Extended/ose.py", VulnList: "/tmp/vulnwsc.txt"}
	m.WifiScan.VulnList = []string{"RT-AC66U 1.0"}
	m.WifiScan.WPSByIface = map[string]map[string]wifi.WPSInfo{
		"wlan0": {
			"AA:BB:CC:DD:EE:FF": {WPS: true, Version: "1.0", Model: "RT-AC66U", ModelNumber: "1.0"},
		},
	}
	m.WifiScan.WPSFetched = map[string]time.Time{"wlan0": time.Now()}
	return m
}

func TestWPSMenuAndRunFlow(t *testing.T) {
	for _, sz := range []struct {
		name string
		w, h int
	}{{"CZ_40x10", 40, 10}, {"CZ_45x10", 45, 10}, {"CZ_53x14", 53, 14}, {"Wide_100x30", 100, 30}} {
		m := newWPSTestModel(t, sz.w, sz.h)

		// Actions menu -> WPS Attack.
		m = sendKey(m, "enter")
		for i := 0; i < int(wifiActWPS); i++ {
			m = sendKey(m, "down")
		}
		m = sendKey(m, "enter")
		if m.WifiScan.Mode != scanModeWPSMenu {
			t.Fatalf("[%s] expected WPS menu, got %v", sz.name, m.WifiScan.Mode)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/WPSMenu")
		menuView := m.View()
		if !strings.Contains(menuView, "Pixie") || !strings.Contains(menuView, "WPS 1.0") {
			t.Fatalf("[%s] expected WPS menu details:\n%s", sz.name, menuView)
		}

		// Default method must be Pixie Dust with -P -k -F.
		method, ok := m.WifiScan.SelectedWPSMethod()
		if !ok || method.Label != "Pixie Dust" || strings.Join(method.Args, " ") != "-P -k -F" {
			t.Fatalf("[%s] default method = %+v", sz.name, method)
		}
		m = sendKey(m, "enter")
		if !m.WifiScan.WPSActive || m.WifiScan.Mode != scanModeWPSRun {
			t.Fatalf("[%s] expected running OneShot, active=%v mode=%v", sz.name, m.WifiScan.WPSActive, m.WifiScan.Mode)
		}
		if strings.Join(m.WifiScan.WPSArgs, " ") != "-P -k -F" {
			t.Fatalf("[%s] run args = %v", sz.name, m.WifiScan.WPSArgs)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/WPSRun")

		// Stream some output, then complete.
		for _, line := range []string{"[*] Starting Pixie Dust", "[*] Sending WPS messages", "[+] Pin found: 12345670"} {
			next, cmd := m.Update(wpsRunLineMsg{seq: m.WifiScan.WPSSeq, line: line})
			m = next.(AppModel)
			if cmd == nil {
				t.Fatalf("[%s] expected continued output wait", sz.name)
			}
		}
		runView := m.View()
		assertFitsBounds(t, runView, sz.w, sz.h, sz.name+"/WiFi/WPSOutput")
		if !strings.Contains(runView, "Pin found") {
			t.Fatalf("[%s] expected streamed output in view:\n%s", sz.name, runView)
		}
		next, _ := m.Update(wpsRunDoneMsg{seq: m.WifiScan.WPSSeq})
		m = next.(AppModel)
		if m.WifiScan.WPSActive || m.WifiScan.WPSDone != "Finished" {
			t.Fatalf("[%s] expected finished OneShot, active=%v done=%q", sz.name, m.WifiScan.WPSActive, m.WifiScan.WPSDone)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/WPSDone")

		// Esc returns to the WPS menu, then to actions.
		m = sendKey(m, "esc")
		if m.WifiScan.Mode != scanModeWPSMenu {
			t.Fatalf("[%s] expected back to WPS menu, got %v", sz.name, m.WifiScan.Mode)
		}
		m = sendKey(m, "esc")
		if m.WifiScan.Mode != scanModeActions {
			t.Fatalf("[%s] expected back to actions, got %v", sz.name, m.WifiScan.Mode)
		}
	}
}

func TestWPSRunStopAndCustomPin(t *testing.T) {
	m := newWPSTestModel(t, 53, 14)
	m.WifiScan.Mode = scanModeWPSMenu
	// Custom PIN option is the 7th entry.
	for i := 0; i < 6; i++ {
		m = sendKey(m, "down")
	}
	m = sendKey(m, "enter")
	if m.WifiScan.Mode != scanModeWPSPin {
		t.Fatalf("expected PIN input, got %v", m.WifiScan.Mode)
	}
	assertFitsBounds(t, m.View(), 53, 14, "WiFi/WPSPin")
	for _, ch := range []string{"1", "2", "3", "4", "5", "6", "7", "0"} {
		m = sendKey(m, ch)
	}
	m = sendKey(m, "enter")
	if !m.WifiScan.WPSActive || strings.Join(m.WifiScan.WPSArgs, " ") != "-p 12345670 -k" {
		t.Fatalf("expected custom PIN run, active=%v args=%v", m.WifiScan.WPSActive, m.WifiScan.WPSArgs)
	}
	// q stops the run instead of quitting the app.
	m = sendKey(m, "q")
	if m.ConfirmExit || m.WifiScan.WPSActive {
		t.Fatalf("expected q to stop OneShot, confirmExit=%v active=%v", m.ConfirmExit, m.WifiScan.WPSActive)
	}
}

func TestWPSCustomPinRequiresInput(t *testing.T) {
	m := newWPSTestModel(t, 53, 14)
	m.WifiScan.Mode = scanModeWPSPin
	m = sendKey(m, "enter")
	if m.WifiScan.WPSActive || m.WifiScan.WPSNote == "" {
		t.Fatalf("expected empty PIN to be rejected with a note")
	}
}

func TestWPSMissingOneShotShowsNote(t *testing.T) {
	m := newWPSTestModel(t, 53, 14)
	m.WifiScan.OneShot = wifi.OneShot{Err: "OneShot-Extended not found"}
	m.WifiScan.Mode = scanModeWPSMenu
	m = sendKey(m, "enter")
	if m.WifiScan.WPSActive || m.WifiScan.Mode != scanModeWPSMenu {
		t.Fatalf("expected missing OneShot to stay in menu, active=%v mode=%v", m.WifiScan.WPSActive, m.WifiScan.Mode)
	}
	if !strings.Contains(m.WifiScan.WPSNote, "not found") {
		t.Fatalf("expected missing OneShot note, got %q", m.WifiScan.WPSNote)
	}
	assertFitsBounds(t, m.View(), 53, 14, "WiFi/WPSMissing")
}

func TestWiFiColorModes(t *testing.T) {
	pal := theme.HighContrast
	cases := map[string]string{
		"":          "open",
		"Open":      "open",
		"--":        "open",
		"WPA2":      "wpa2",
		"WPA1 WPA2": "wpa2",
		"WPA3":      "wpa3",
		"WPA2/3":    "wpa3",
		"SAE":       "wpa3",
		"WPA":       "wpa",
		"WEP":       "wep",
	}
	for sec, want := range cases {
		if got := SecurityColor(sec); got != want {
			t.Fatalf("SecurityColor(%q) = %q, want %q", sec, got, want)
		}
	}

	m := newWPSTestModel(t, 53, 14)
	m.WifiScan.ColorMode = config.WiFiColorSecurity
	open := wifi.Network{SSID: "Cafe", Security: "Open"}
	wpa3 := wifi.Network{SSID: "Vault", Security: "WPA3"}
	wpa2 := wifi.Network{SSID: "Home", Security: "WPA2"}
	if m.WifiScan.NetworkColor(open, pal) != pal.Success {
		t.Fatal("expected open network green")
	}
	if m.WifiScan.NetworkColor(wpa3, pal) != pal.Danger {
		t.Fatal("expected WPA3 red")
	}
	if m.WifiScan.NetworkColor(wpa2, pal) != pal.Orange {
		t.Fatal("expected WPA2 orange")
	}

	// Switch to OneShot mode via 'o'; vulnerable AP green, missing info muted.
	m.WifiScan.ColorMode = config.WiFiColorOneShot
	vuln := wifi.Network{SSID: "LabNet", BSSID: "AA:BB:CC:DD:EE:FF", Security: "WPA2"}
	unknown := wifi.Network{SSID: "Other", BSSID: "11:22:33:44:55:66", Security: "WPA2"}
	if m.WifiScan.NetworkColor(vuln, pal) != pal.Success {
		t.Fatal("expected OneShot-vulnerable AP green")
	}
	if m.WifiScan.NetworkColor(unknown, pal) != pal.Muted {
		t.Fatal("expected AP without WPS info muted")
	}
}

func TestColorModeTogglePersists(t *testing.T) {
	m := newWPSTestModel(t, 53, 14)
	m.WifiScan.ColorMode = config.WiFiColorSecurity
	if got := m.WifiScan.ColorModeLabel(); got != "SEC" {
		t.Fatalf("security tag = %q", got)
	}
	m = sendKey(m, "o")
	if m.WifiScan.ColorMode != config.WiFiColorOneShot || m.Cfg.WiFiColor != config.WiFiColorOneShot {
		t.Fatalf("expected oneshot mode, view=%q cfg=%q", m.WifiScan.ColorMode, m.Cfg.WiFiColor)
	}
	if got := m.WifiScan.ColorModeLabel(); got != "OSE" {
		t.Fatalf("oneshot tag = %q", got)
	}
	loaded, err := config.Load(m.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WiFiColor != config.WiFiColorOneShot {
		t.Fatalf("persisted color mode = %q", loaded.WiFiColor)
	}
	assertFitsBounds(t, m.View(), 53, 14, "WiFi/ColorOneShot")
	m = sendKey(m, "o")
	if m.WifiScan.ColorMode != config.WiFiColorSecurity {
		t.Fatalf("expected back to security mode, got %q", m.WifiScan.ColorMode)
	}
}

func TestWPSInfoRefreshAndVulnCount(t *testing.T) {
	m := newWPSTestModel(t, 53, 14)
	m.WifiScan.SetScanResult("wlan0", []wifi.Network{
		{SSID: "LabNet", BSSID: "AA:BB:CC:DD:EE:FF", Signal: 80, Security: "WPA2"},
		{SSID: "Guest", BSSID: "11:22:33:44:55:66", Signal: 60, Security: "Open"},
	}, nil)
	vuln, total := m.WifiScan.VulnerableCount()
	if vuln != 1 || total != 1 {
		t.Fatalf("VulnerableCount = %d/%d, want 1/1", vuln, total)
	}
	m.WifiScan.SetWPSInfo("wlan0", map[string]wifi.WPSInfo{
		"AA:BB:CC:DD:EE:FF": {WPS: true, Version: "2.0"},
	}, nil)
	if m.WifiScan.WPSInfoStale("wlan0") {
		t.Fatal("fresh WPS data must not be stale")
	}
	if _, ok := m.WifiScan.WPSInfoFor(wifi.Network{BSSID: "AA:BB:CC:DD:EE:FF"}); !ok {
		t.Fatal("expected WPS info for LabNet")
	}
}

func TestWPSRenderBoundsAllModes(t *testing.T) {
	for _, sz := range []struct {
		w, h int
	}{{40, 10}, {45, 10}, {53, 14}} {
		m := newWPSTestModel(t, sz.w, sz.h)
		for _, mode := range []WifiScanMode{scanModeWPSMenu, scanModeWPSPin, scanModeWPSRun} {
			m.WifiScan.Mode = mode
			if mode == scanModeWPSRun {
				m.WifiScan.WPSActive = true
				m.WifiScan.WPSElapsed = 12 * time.Second
				m.WifiScan.WPSLines = []string{"[*] line one", "[*] line two", "[*] line three", "[*] line four", "[*] line five", "[*] line six"}
			}
			assertFitsBounds(t, m.View(), sz.w, sz.h, "WiFi/WPSBounds")
		}
		m.WifiScan.WPSActive = false
		m.WifiScan.WPSErr = "oneshot exited 1"
		m.WifiScan.Mode = scanModeWPSRun
		assertFitsBounds(t, m.View(), sz.w, sz.h, "WiFi/WPSBoundsErr")
	}
}

func TestWPSRunLineBoundsAndSeq(t *testing.T) {
	m := newWPSTestModel(t, 40, 10)
	m.WifiScan.Mode = scanModeWPSMenu
	m.WifiScan.StartWPSRun("Pixie Dust", []string{"-P", "-k", "-F"})
	seq := m.WifiScan.WPSSeq
	long := strings.Repeat("very long oneshot output line ", 6)
	next, _ := m.Update(wpsRunLineMsg{seq: seq, line: long})
	m = next.(AppModel)
	if len(m.WifiScan.WPSLines) != 1 {
		t.Fatalf("expected one stored line, got %d", len(m.WifiScan.WPSLines))
	}
	assertFitsBounds(t, m.View(), 40, 10, "WiFi/WPSLongLine")
	// Stale sequence must be ignored.
	next, _ = m.Update(wpsRunLineMsg{seq: seq + 10, line: "stale"})
	m = next.(AppModel)
	if len(m.WifiScan.WPSLines) != 1 {
		t.Fatalf("expected stale seq ignored, got %d lines", len(m.WifiScan.WPSLines))
	}
	m.WifiScan.StopWPSRun()
	next, _ = m.Update(wpsRunDoneMsg{seq: seq, err: context.Canceled})
	m = next.(AppModel)
	if m.WifiScan.WPSActive {
		t.Fatal("expected WPS run inactive after done")
	}
}
