package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
)

func sendKey(m AppModel, key string) AppModel {
	var msg tea.KeyMsg
	switch key {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "f4":
		msg = tea.KeyMsg{Type: tea.KeyF4}
	case "f5":
		msg = tea.KeyMsg{Type: tea.KeyF5}
	case "f6":
		msg = tea.KeyMsg{Type: tea.KeyF6}
	case "f7":
		msg = tea.KeyMsg{Type: tea.KeyF7}
	case "f8":
		msg = tea.KeyMsg{Type: tea.KeyF8}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := m.Update(msg)
	return next.(AppModel)
}

func assertFitsBounds(t *testing.T, view string, maxW, exactH int, label string) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != exactH {
		t.Fatalf("[%s] expected %d lines, got %d:\n%s", label, exactH, len(lines), view)
	}
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w > maxW {
			t.Fatalf("[%s] line %d exceeds width %d (got %d): %q", label, i+1, maxW, w, line)
		}
	}
}

func TestPineappleLayoutAllViewsAndSizes(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"CZ_53x14", 53, 14},
		{"CZ_45x10", 45, 10},
		{"CZ_40x10", 40, 10},
		{"Wide_100x30", 100, 30},
	}

	for _, sz := range sizes {
		cfg := config.Default()
		cfgPath := filepath.Join(t.TempDir(), "config.json")
		m := NewAppModel(cfg, cfgPath)
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)

		for vIdx, vd := range TopViews {
			m.ActiveView = ViewID(vIdx)
			m.Focus = FocusTabs
			assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/"+vd.Name+"/Tabs")

			m = sendKey(m, "enter")
			if m.Focus != FocusContent {
				t.Fatalf("expected FocusContent after enter in %s", vd.Name)
			}
			for tIdx, tabName := range vd.Tabs {
				m.setTab(tIdx)
				assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/"+vd.Name+"/"+tabName)
			}
			m = sendKey(m, "esc")
			if m.Focus != FocusTabs {
				t.Fatalf("expected FocusTabs after esc in %s", vd.Name)
			}
		}

		// Search overlay
		m = sendKey(m, "/")
		m = sendKey(m, "w")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Search")
		m = sendKey(m, "esc")

		// Help overlay
		m = sendKey(m, "?")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Help")
		m = sendKey(m, "esc")
	}
}

func TestTabAndContentArrowNavigation(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	m := NewAppModel(cfg, cfgPath)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 53, Height: 14})
	m = next.(AppModel)

	// In tab selection mode: Up/Down moves vertical views, Left/Right moves horizontal tabs
	if m.Focus != FocusTabs {
		t.Fatalf("expected initial FocusTabs")
	}
	m = sendKey(m, "down")
	if m.ActiveView != ViewRecon {
		t.Fatalf("expected ViewRecon on down, got %v", m.ActiveView)
	}
	m = sendKey(m, "right")
	if m.ActiveTabs[ViewRecon] != 1 {
		t.Fatalf("expected horizontal tab 1 (BLE) on right, got %d", m.ActiveTabs[ViewRecon])
	}
	m = sendKey(m, "left")
	if m.ActiveTabs[ViewRecon] != 0 {
		t.Fatalf("expected horizontal tab 0 (WiFi) on left, got %d", m.ActiveTabs[ViewRecon])
	}

	// Enter goes into content
	m = sendKey(m, "enter")
	if m.Focus != FocusContent {
		t.Fatalf("expected FocusContent on enter")
	}
	origPins := len(m.Cfg.PinnedIDs)
	m = sendKey(m, "p")
	if len(m.Cfg.PinnedIDs) != origPins-1 {
		t.Fatalf("expected WiFi module to be unpinned, pins=%v", m.Cfg.PinnedIDs)
	}
	m = sendKey(m, "p")
	if len(m.Cfg.PinnedIDs) != origPins {
		t.Fatalf("expected WiFi module to be re-pinned, pins=%v", m.Cfg.PinnedIDs)
	}

	// Esc returns to tab selection view
	m = sendKey(m, "esc")
	if m.Focus != FocusTabs {
		t.Fatalf("expected FocusTabs on esc")
	}

	// Jump to Opts, enter content, and use Left/Right arrows to adjust Viewport (first option, since Theme is removed)
	m = sendKey(m, "f8")
	if m.ActiveView != ViewOpts {
		t.Fatalf("expected ViewOpts on F8, got %v", m.ActiveView)
	}
	m = sendKey(m, "enter")
	if m.Focus != FocusContent {
		t.Fatalf("expected FocusContent on enter in Opts")
	}
	m = sendKey(m, "right")
	if m.Focus != FocusContent {
		t.Fatalf("left/right inside Opts content must stay in FocusContent")
	}
	if m.Cfg.Viewport != config.ViewportCZ5314 {
		t.Fatalf("expected ViewportCZ5314 on right in Opts, got %v", m.Cfg.Viewport)
	}
	m = sendKey(m, "left")
	if m.Cfg.Viewport != config.ViewportAuto {
		t.Fatalf("expected ViewportAuto on left in Opts, got %v", m.Cfg.Viewport)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil || loaded.Theme != "mono-hi" {
		t.Fatalf("expected persisted theme mono-hi, got %q (err=%v)", loaded.Theme, err)
	}

	// Esc returns to tab selection view
	m = sendKey(m, "esc")
	if m.Focus != FocusTabs {
		t.Fatalf("expected FocusTabs on esc from Opts")
	}
}

func TestDesktopBezelMode(t *testing.T) {
	cfg := config.Default()
	cfg.Viewport = config.ViewportCZ5314
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(AppModel)

	view := m.View()
	if !strings.Contains(view, "M5Stack Cardputer Zero") {
		t.Fatalf("expected desktop bezel label in CZ5314 mode on 100x30 terminal")
	}
	for _, lbl := range []string{"Dash", "Recon", "Net", "Lab", "Sys", "Opts"} {
		if !strings.Contains(view, lbl) {
			t.Fatalf("expected sidebar label %q in 53x14 view:\n%s", lbl, view)
		}
	}

	m.Cfg.Viewport = config.ViewportCZ4510
	compactView := m.View()
	for _, lbl := range []string{"Dsh", "Rcn", "Net", "Lab", "Sys", "Opt"} {
		if !strings.Contains(compactView, lbl) {
			t.Fatalf("expected 3-char sidebar label %q in 45x10 view:\n%s", lbl, compactView)
		}
	}
}
