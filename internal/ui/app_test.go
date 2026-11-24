package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestStatusViewAllSizes(t *testing.T) {
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
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"))
		m.Snap.HasBattery = true
		m.Snap.BatteryPct = 78
		m.Snap.HasBatTime = true
		m.Snap.BatTimeLeft = 2*time.Hour + 15*time.Minute

		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)

		view := m.View()
		assertFitsBounds(t, view, sz.w, sz.h, sz.name+"/Tabs")
		if !strings.Contains(view, "Device") {
			t.Fatalf("[%s] expected horizontal tab Device in:\n%s", sz.name, view)
		}
		if !strings.Contains(view, "2h15m left") {
			t.Fatalf("[%s] expected battery time left in:\n%s", sz.name, view)
		}

		// Enter content, scroll down/up, return with Esc
		m = sendKey(m, "enter")
		if m.Focus != FocusContent {
			t.Fatalf("expected FocusContent on enter")
		}
		m = sendKey(m, "down")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/ContentScrolled")
		m = sendKey(m, "esc")
		if m.Focus != FocusTabs {
			t.Fatalf("expected FocusTabs on esc")
		}

		// Help overlay
		m = sendKey(m, "?")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Help")
		m = sendKey(m, "esc")
	}
}

func TestSingleStatusTabAndLabels(t *testing.T) {
	if len(TopViews) != 1 || TopViews[0].Name != "Status" || TopViews[0].Short3 != "Sts" {
		t.Fatalf("expected single Status vertical view, got %+v", TopViews)
	}
	if len(TopViews[0].Tabs) != 1 || TopViews[0].Tabs[0] != "Device" {
		t.Fatalf("expected single Device horizontal tab, got %+v", TopViews[0].Tabs)
	}
}
