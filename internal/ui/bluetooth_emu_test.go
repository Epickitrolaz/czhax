package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/theme"
)

func renderEmuActive(t *testing.T, v BluetoothEmuView, w, h int) []string {
	t.Helper()
	lines := v.RenderLines(w, h, theme.HighContrast, true)
	if len(lines) != h {
		t.Fatalf("[%dx%d] expected %d lines, got %d: %q", w, h, h, len(lines), lines)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got > w {
			t.Fatalf("[%dx%d] line %d is %d cells wide: %q", w, h, i+1, got, line)
		}
	}
	return lines
}

// TestEmuActiveShowsLinkState makes sure a live link is reported as such instead
// of leaving the "pair from host" advertising hint on screen.
func TestEmuActiveShowsLinkState(t *testing.T) {
	sizes := []struct{ w, h int }{{40, 10}, {45, 10}, {53, 14}, {100, 30}}

	for _, sz := range sizes {
		base := BluetoothEmuView{
			Mode:    btEmuActive,
			SubMode: btEmuCtrlMenu,
			Spec:    bluetooth.FindEmuProfile(bluetooth.EmuCombo),
			Ready:   true,
		}

		advertising := renderEmuActive(t, base, sz.w, sz.h)
		joined := strings.Join(advertising, "\n")
		if !strings.Contains(joined, "Advertising") {
			t.Fatalf("[%dx%d] expected an advertising hint while unconnected:\n%s", sz.w, sz.h, joined)
		}

		linked := base
		linked.Connected = true
		linked.PeerName = "DESKTOP-ABCDEFG"
		linked.PeerPaired = true
		linked.TXCount = 7
		out := renderEmuActive(t, linked, sz.w, sz.h)
		joined = strings.Join(out, "\n")
		if strings.Contains(joined, "Advertising") {
			t.Fatalf("[%dx%d] still advertising after connect:\n%s", sz.w, sz.h, joined)
		}
		if !strings.Contains(joined, "paired") {
			t.Fatalf("[%dx%d] expected the paired state on screen:\n%s", sz.w, sz.h, joined)
		}

		// Unpaired but linked is a distinct state (host connected, no bond).
		unpaired := linked
		unpaired.PeerPaired = false
		out = renderEmuActive(t, unpaired, sz.w, sz.h)
		if joined = strings.Join(out, "\n"); !strings.Contains(joined, "connected") {
			t.Fatalf("[%dx%d] expected the connected state on screen:\n%s", sz.w, sz.h, joined)
		}
	}
}
