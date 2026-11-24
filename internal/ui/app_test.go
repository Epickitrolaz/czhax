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

func TestSmallScreenGeometryAllScreens(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"CZ_53x14", 53, 14},
		{"CZ_45x10", 45, 10},
		{"Wide_100x30", 100, 30},
	}

	for _, sz := range sizes {
		cfg := config.Default()
		cfgPath := filepath.Join(t.TempDir(), "config.json")
		m := NewAppModel(cfg, cfgPath)
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)

		// Home
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Home")

		// SecTools + Tool Detail
		m = sendKey(m, "2")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SecList")
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/ToolDetail")
		m = sendKey(m, "esc")

		// Basic + each sub-screen
		m = sendKey(m, "3")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BasicList")

		// SysMon
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SysMon")
		m = sendKey(m, "esc")

		// NetIF
		m = sendKey(m, "down")
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/NetIF")
		m = sendKey(m, "esc")

		// Notes
		m = sendKey(m, "down")
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Notes")
		m = sendKey(m, "esc")

		// Files
		m = sendKey(m, "down")
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Files")
		for m.Sub == SubFiles {
			m = sendKey(m, "esc")
		}

		// Options
		m = sendKey(m, "4")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Options")

		// Search overlay
		m = sendKey(m, "/")
		m = sendKey(m, "w")
		m = sendKey(m, "i")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Search")
		m = sendKey(m, "esc")

		// Help overlay
		m = sendKey(m, "?")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Help")
		m = sendKey(m, "esc")
	}
}

func TestNavigationAndOptionsBehavior(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	m := NewAppModel(cfg, cfgPath)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 53, Height: 14})
	m = next.(AppModel)

	// Fn softkey tabs F5 -> Sec, F6 -> Basic, F7 -> Options, F4 -> Home
	m = sendKey(m, "f5")
	if m.ActiveTab != TabSec {
		t.Fatalf("expected TabSec on F5, got %v", m.ActiveTab)
	}
	m = sendKey(m, "f6")
	if m.ActiveTab != TabBasic {
		t.Fatalf("expected TabBasic on F6, got %v", m.ActiveTab)
	}
	m = sendKey(m, "f7")
	if m.ActiveTab != TabOptions {
		t.Fatalf("expected TabOptions on F7, got %v", m.ActiveTab)
	}
	m = sendKey(m, "f8")
	if !m.ShowHelp {
		t.Fatalf("expected Help overlay on F8")
	}
	m = sendKey(m, "esc")

	// Adjust Theme in Options and verify persistence
	m = sendKey(m, "right")
	if m.Cfg.Theme == config.Default().Theme {
		t.Fatalf("expected theme to change on right key in Options")
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("failed loading saved config: %v", err)
	}
	if loaded.Theme != m.Cfg.Theme {
		t.Fatalf("expected saved theme %q, got %q", m.Cfg.Theme, loaded.Theme)
	}

	// Toggle Stealth via 's'
	m = sendKey(m, "s")
	if !m.Cfg.StealthMode {
		t.Fatalf("expected StealthMode ON after 's'")
	}

	// Pin/unpin on SecTools
	m = sendKey(m, "f5")
	origPins := len(m.Cfg.PinnedIDs)
	m = sendKey(m, "p") // wifi-recon is initially pinned -> unpins it
	if len(m.Cfg.PinnedIDs) != origPins-1 {
		t.Fatalf("expected pin toggle to remove item, pins=%v", m.Cfg.PinnedIDs)
	}
	m = sendKey(m, "p") // re-pin
	if len(m.Cfg.PinnedIDs) != origPins {
		t.Fatalf("expected pin toggle to restore item, pins=%v", m.Cfg.PinnedIDs)
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
}
