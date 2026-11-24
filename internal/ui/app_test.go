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
			// Move to view via F-key or down
			m.ActiveView = ViewID(vIdx)
			m.Focus = FocusSidebar
			assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/"+vd.Name+"/Sidebar")

			m = sendKey(m, "right")
			if m.Focus != FocusContent {
				t.Fatalf("expected FocusContent after right in %s", vd.Name)
			}
			for tIdx, tabName := range vd.Tabs {
				m.setTab(tIdx)
				assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/"+vd.Name+"/"+tabName)
			}
			m = sendKey(m, "esc")
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

func TestNavigationTabsAndOptions(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	m := NewAppModel(cfg, cfgPath)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 53, Height: 14})
	m = next.(AppModel)

	// Sidebar navigation
	m = sendKey(m, "down")
	if m.ActiveView != ViewRecon {
		t.Fatalf("expected ViewRecon, got %v", m.ActiveView)
	}
	m = sendKey(m, "tab")
	if m.ActiveTabs[ViewRecon] != 1 {
		t.Fatalf("expected horizontal tab 1 (BLE), got %d", m.ActiveTabs[ViewRecon])
	}

	// Pin/unpin in Recon
	m = sendKey(m, "right")
	origPins := len(m.Cfg.PinnedIDs)
	m = sendKey(m, "p")
	if len(m.Cfg.PinnedIDs) != origPins+1 {
		t.Fatalf("expected BLE module to be pinned, pins=%v", m.Cfg.PinnedIDs)
	}
	m = sendKey(m, "p")
	if len(m.Cfg.PinnedIDs) != origPins {
		t.Fatalf("expected BLE module to be unpinned, pins=%v", m.Cfg.PinnedIDs)
	}

	// Jump to Opts via F8 and change Theme
	m = sendKey(m, "f8")
	if m.ActiveView != ViewOpts {
		t.Fatalf("expected ViewOpts on F8, got %v", m.ActiveView)
	}
	m.Focus = FocusContent
	m = sendKey(m, "enter")
	if m.Cfg.Theme == config.Default().Theme {
		t.Fatalf("expected theme to change on Enter in Opts")
	}
	loaded, err := config.Load(cfgPath)
	if err != nil || loaded.Theme != m.Cfg.Theme {
		t.Fatalf("expected persisted theme %q, got %q (err=%v)", m.Cfg.Theme, loaded.Theme, err)
	}

	// Back to sidebar via left
	m = sendKey(m, "left")
	if m.Focus != FocusSidebar {
		t.Fatalf("expected FocusSidebar on left in Opts")
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
	for _, icon := range []string{"[#]", "((o", "<->", "{*}", ">$_", "[=]"} {
		if !strings.Contains(view, icon) {
			t.Fatalf("expected sidebar ASCII icon %q in view:\n%s", icon, view)
		}
	}
}
