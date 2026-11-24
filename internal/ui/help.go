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

func (v HelpView) RenderLines(w, h int, pal theme.Palette) []string {
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	rows := []string{
		" " + sec.Render("↑↓/Fn+F,X ") + txt.Render("Move / scroll"),
		" " + sec.Render("←→/Fn+Z,C ") + txt.Render("Sidebar ↔ Content"),
		" " + sec.Render("Tab / 1-3 ") + txt.Render("Top horizontal tabs"),
		" " + sec.Render("F4 - F8   ") + txt.Render("Dsh/Rcn/Net/Sys/Opt"),
		" " + sec.Render("Enter/Spc ") + txt.Render("Open / toggle"),
		" " + sec.Render("/         ") + txt.Render("Quick find module"),
		" " + sec.Render("p / t / s ") + txt.Render("Pin / Theme / Stl"),
		" " + sec.Render("r / q     ") + txt.Render("Refresh / Quit"),
	}

	maxScroll := max(0, len(rows)-h)
	scroll := min(v.Scroll, maxScroll)
	end := min(len(rows), scroll+h)
	return SplitLinesPadded(rows[scroll:end], w, h)
}
