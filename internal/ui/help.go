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
		" " + sec.Render("↑↓/Fn+F,X ") + txt.Render("Vertical views / list"),
		" " + sec.Render("←→/Fn+Z,C ") + txt.Render("Top tabs / adjust"),
		" " + sec.Render("Enter     ") + txt.Render("Enter tab content"),
		" " + sec.Render("Esc       ") + txt.Render("Back to tab select"),
		" " + sec.Render("F4 - F8   ") + txt.Render("Jump top views"),
		" " + sec.Render("/  or  ?  ") + txt.Render("Quick find / Help"),
		" " + sec.Render("p / s / r ") + txt.Render("Pin / Stealth / Ref"),
		" " + sec.Render("q         ") + txt.Render("Quit CZHax"),
	}

	maxScroll := max(0, len(rows)-h)
	scroll := min(v.Scroll, maxScroll)
	end := min(len(rows), scroll+h)
	return SplitLinesPadded(rows[scroll:end], w, h)
}
