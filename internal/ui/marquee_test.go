package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/ir"
	"github.com/czhax/czhax/internal/wifi"
)

func TestMarqueeSliceAndPause(t *testing.T) {
	text := "Samsung_Smart_TV_UltraLongName"
	var overflow bool
	m0 := Marquee{Step: 0, Overflow: &overflow}
	got0 := m0.Truncate(text, 12)
	if !overflow {
		t.Fatal("expected overflow=true")
	}
	if lipgloss.Width(got0) > 12 || !strings.HasSuffix(got0, "…") {
		t.Fatalf("step 0 = %q", got0)
	}

	// Scrolling reveals the tail of the string within exact maxW.
	seenTail := false
	for step := 1; step <= 40; step++ {
		ms := Marquee{Step: step}
		got := ms.FitLine(text, 12)
		if lipgloss.Width(got) != 12 {
			t.Fatalf("step %d FitLine width = %d, want 12 (%q)", step, lipgloss.Width(got), got)
		}
		if strings.Contains(got, "LongName") {
			seenTail = true
		}
	}
	if !seenTail {
		t.Fatal("expected marquee scroll to reveal tail 'LongName'")
	}

	// Non-overflowing string never marks overflow or scrolls.
	overflow = false
	short := (Marquee{Step: 5, Overflow: &overflow}).Truncate("Power", 12)
	if overflow || short != "Power" {
		t.Fatalf("short string = %q overflow=%v", short, overflow)
	}
}

func TestAppModelMarqueeLifecycleAndBounds(t *testing.T) {
	sig := irTestSignal(t)
	longName := "SuperExtraLongDeviceModelName_WithExtraCharacters_ToOverflowEvenWideScreens_XYZ_9000_Plus"

	for _, sz := range []struct {
		name string
		w, h int
	}{{"CZ_40x10", 40, 10}, {"CZ_45x10", 45, 10}, {"CZ_53x14", 53, 14}, {"Wide_100x30", 100, 30}} {
		cfg := config.Default()
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.3.2")
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)

		// Put a long SSID in WiFi Scan and enter content.
		m.ActiveView = ViewWiFi
		m.Focus = FocusContent
		m.WifiScan.SetScanResult("wlan0", []wifi.Network{
			{SSID: longName, BSSID: "aa:bb:cc:dd:ee:ff", Signal: 85, Security: "WPA2"},
		}, nil)

		// Before 2s: MarqueeStep == 0, tail is not yet revealed on small screens.
		if m.MarqueeStep != 0 {
			t.Fatalf("[%s] expected MarqueeStep 0 initially", sz.name)
		}

		// Simulate 2s idle timer firing.
		next, cmd := m.Update(marqueeStartMsg{gen: m.MarqueeGen})
		m = next.(AppModel)
		if m.MarqueeStep != 1 || cmd == nil {
			t.Fatalf("[%s] expected marquee to start on overflow (step=%d cmd=%v)", sz.name, m.MarqueeStep, cmd != nil)
		}

		// Advance ticks and verify bounds hold AND tail becomes visible.
		revealed := false
		for i := 0; i < 100; i++ {
			next, cmd = m.Update(marqueeTickMsg{gen: m.MarqueeGen})
			m = next.(AppModel)
			if cmd == nil {
				t.Fatalf("[%s] expected continued tick cmd", sz.name)
			}
			view := m.View()
			assertFitsBounds(t, view, sz.w, sz.h, sz.name+"/Marquee/WiFi")
			if strings.Contains(ansi.Strip(view), "XYZ_9000") {
				revealed = true
			}
		}
		if !revealed {
			t.Fatalf("[%s] expected marquee ticks to reveal tail 'XYZ_9000' in view:\n%s", sz.name, ansi.Strip(m.View()))
		}

		// Also verify IR Browse / Remote scrolling bounds.
		m.ActiveView = ViewIR
		m.ActiveTabs[ViewIR] = int(IRTabRemote)
		m.IR.Tab = IRTabRemote
		m.IR.Store.AddBinding(longName, ir.Binding{Key: "1", Name: longName, Signal: sig})
		m.MarqueeStep = 15
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Marquee/IRRemote")

		// Pressing a key resets MarqueeStep to 0 and bumps MarqueeGen.
		prevGen := m.MarqueeGen
		m = sendKey(m, "down")
		if m.MarqueeStep != 0 || m.MarqueeGen == prevGen {
			t.Fatalf("[%s] expected key press to reset marquee (step=%d gen=%d prev=%d)", sz.name, m.MarqueeStep, m.MarqueeGen, prevGen)
		}

		// Stale tick from previous generation is ignored.
		next, _ = m.Update(marqueeTickMsg{gen: prevGen})
		m = next.(AppModel)
		if m.MarqueeStep != 0 {
			t.Fatalf("[%s] expected stale marquee tick ignored, got step=%d", sz.name, m.MarqueeStep)
		}
	}

	// Screen with zero overflowing text should NOT start the marquee ticker.
	cfg := config.Default()
	cfg.ShowKeyHints = false
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.3.2")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(AppModel)
	m.Snap.Hostname = "cz"
	m.Snap.PrimaryIP = "10.0.0.1"
	m.ActiveView = ViewSystem
	m.Focus = FocusContent
	next, cmd := m.Update(marqueeStartMsg{gen: m.MarqueeGen})
	m = next.(AppModel)
	if m.MarqueeStep != 0 || cmd != nil {
		t.Fatalf("expected no marquee ticks when nothing overflows on 100x30 (step=%d cmd=%v)", m.MarqueeStep, cmd != nil)
	}
}
