package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/config"
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
