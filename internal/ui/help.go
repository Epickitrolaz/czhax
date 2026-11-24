package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/theme"
)

type HelpView struct {
	Scroll int
}

func (v *HelpView) Move(delta int) {
	v.Scroll += delta
	if v.Scroll < 0 {
		v.Scroll = 0
	}
}

func (v HelpView) Render(w, h int, pal theme.Palette) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	rows := []string{
		accent.Render("CZHAX KEYMAP (Cardputer Zero)") + " " + muted.Render("Esc:close"),
		sec.Render("↑↓ / Fn+F,X ") + txt.Render("Move cursor / scroll"),
		sec.Render("←→ / Fn+Z,C ") + txt.Render("Back / adjust option"),
		sec.Render("h/j/k/l     ") + txt.Render("Vim nav (when enabled)"),
		sec.Render("1 - 4 / Tab ") + txt.Render("Switch Home/Sec/Basic/Opts"),
		sec.Render("F4 - F8     ") + txt.Render("CZ Fn+4..8 softkeys"),
		sec.Render("Enter/Space ") + txt.Render("Open or toggle selected"),
		sec.Render("/           ") + txt.Render("Quick find any tool"),
		sec.Render("p           ") + txt.Render("Pin/unpin tool to Home"),
		sec.Render("s           ") + txt.Render("Toggle Stealth mode"),
		sec.Render("t           ") + txt.Render("Cycle color theme"),
		sec.Render("r           ") + txt.Render("Refresh telemetry"),
		sec.Render("q / Ctrl+C  ") + txt.Render("Quit CZHax"),
	}

	maxScroll := max(0, len(rows)-h)
	scroll := min(v.Scroll, maxScroll)
	end := min(len(rows), scroll+h)
	return PadLines(rows[scroll:end], w, h, "")
}
