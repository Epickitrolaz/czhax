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
	yel := lipgloss.NewStyle().Bold(true).Foreground(pal.Yellow)
	cyan := lipgloss.NewStyle().Bold(true).Foreground(pal.Cyan)
	mag := lipgloss.NewStyle().Bold(true).Foreground(pal.Magenta)
	grn := lipgloss.NewStyle().Bold(true).Foreground(pal.Green)
	org := lipgloss.NewStyle().Bold(true).Foreground(pal.Orange)
	pur := lipgloss.NewStyle().Bold(true).Foreground(pal.Purple)
	red := lipgloss.NewStyle().Bold(true).Foreground(pal.Red)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	rows := []string{
		" " + yel.Render("↑↓/Fn+F,X ") + txt.Render("Move / scroll"),
		" " + cyan.Render("←→/Fn+Z,C ") + txt.Render("Sidebar ↔ Content"),
		" " + mag.Render("Tab / 1-3 ") + txt.Render("Top horizontal tabs"),
		" " + grn.Render("F4 - F8   ") + txt.Render("Dsh/Rcn/Net/Sys/Opt"),
		" " + org.Render("Enter/Spc ") + txt.Render("Open / toggle"),
		" " + pur.Render("/         ") + txt.Render("Quick find module"),
		" " + cyan.Render("p / t / s ") + txt.Render("Pin / Theme / Stl"),
		" " + red.Render("r / q     ") + txt.Render("Refresh / Quit"),
	}

	maxScroll := max(0, len(rows)-h)
	scroll := min(v.Scroll, maxScroll)
	end := min(len(rows), scroll+h)
	return SplitLinesPadded(rows[scroll:end], w, h)
}
