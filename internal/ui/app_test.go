package ui

import (
	"bytes"
	"os"
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

func TestAllViewsAndSizes(t *testing.T) {
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
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.2.0")
		m.Snap.HasBattery = true
		m.Snap.BatteryPct = 78
		m.Snap.HasBatTime = true
		m.Snap.BatTimeLeft = 2*time.Hour + 15*time.Minute

		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)

		// Status view
		view := m.View()
		assertFitsBounds(t, view, sz.w, sz.h, sz.name+"/Status/Tabs")
		if !strings.Contains(view, "Device") || !strings.Contains(view, "2h15m left") {
			t.Fatalf("[%s] expected Device tab and 2h15m left in:\n%s", sz.name, view)
		}

		// Down moves to System view
		m = sendKey(m, "down")
		if m.ActiveView != ViewSystem {
			t.Fatalf("expected ViewSystem on down, got %v", m.ActiveView)
		}
		sysView := m.View()
		assertFitsBounds(t, sysView, sz.w, sz.h, sz.name+"/System/Tabs")
		for _, want := range []string{"Update CZHax", "Restart CZHax", "Terminal Shell", "Start on Boot"} {
			if !strings.Contains(sysView, want) {
				t.Fatalf("[%s] expected %q in System view:\n%s", sz.name, want, sysView)
			}
		}

		// Enter enters System content, arrows navigate all options (skipping separator), Esc returns
		m = sendKey(m, "enter")
		if m.Focus != FocusContent {
			t.Fatalf("expected FocusContent on enter")
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptRestart) {
			t.Fatalf("expected cursor at SysOptRestart, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptTerminal) {
			t.Fatalf("expected cursor at SysOptTerminal, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptBoot) {
			t.Fatalf("expected cursor at SysOptBoot, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptSleep) {
			t.Fatalf("expected cursor at SysOptSleep, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptReboot) {
			t.Fatalf("expected cursor at SysOptReboot, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptShutdown) {
			t.Fatalf("expected cursor at SysOptShutdown, got %d", m.System.Cursor)
		}
		// Trigger shutdown confirm and cancel cleanly with 'n'
		m = sendKey(m, "enter")
		if m.System.Confirm != "shutdown" {
			t.Fatalf("expected shutdown confirmation prompt, got %q", m.System.Confirm)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/System/ConfirmShutdown")
		m = sendKey(m, "n")
		if m.System.Confirm != "" {
			t.Fatalf("expected confirmation cleared on 'n'")
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/System/Content")

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

func TestTTYExecRoutesNonTTYSysderrToStdout(t *testing.T) {
	cmd := buildInteractiveShellCmd()
	var outBuf bytes.Buffer
	rPipe, wPipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rPipe.Close()
	defer wPipe.Close()

	cmd.SetStdout(&outBuf)
	cmd.SetStderr(wPipe) // simulate non-TTY journald stderr
	if cmd.Cmd.Stderr != &outBuf {
		t.Fatalf("expected non-TTY stderr to route to tty stdout writer")
	}
	hasNested := false
	for _, e := range cmd.Cmd.Env {
		if e == "CZHAX_NESTED=1" {
			hasNested = true
		}
	}
	if !hasNested {
		t.Fatalf("expected CZHAX_NESTED=1 in shell command env")
	}
}
