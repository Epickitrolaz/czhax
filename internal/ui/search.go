package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/modules"
	"github.com/czhax/czhax/internal/theme"
)

type SearchView struct {
	Query   string
	Results []modules.Item
	Cursor  int
}

func NewSearchView() SearchView {
	return SearchView{
		Results: modules.All(),
	}
}

func (v *SearchView) SetQuery(q string) {
	v.Query = q
	v.Results = modules.Search(q)
	if v.Cursor >= len(v.Results) {
		v.Cursor = max(0, len(v.Results)-1)
	}
}

func (v *SearchView) Move(delta int) {
	if len(v.Results) == 0 {
		return
	}
	v.Cursor = (v.Cursor + delta + len(v.Results)) % len(v.Results)
}

func (v SearchView) Selected() (modules.Item, bool) {
	if len(v.Results) == 0 || v.Cursor < 0 || v.Cursor >= len(v.Results) {
		return modules.Item{}, false
	}
	return v.Results[v.Cursor], true
}

func (v SearchView) Render(w, h int, pal theme.Palette) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	var lines []string
	prompt := fmt.Sprintf("/ %s_", v.Query)
	lines = append(lines, accent.Render("FIND: ")+txt.Render(Truncate(prompt, max(8, w-6))))

	if len(v.Results) == 0 {
		lines = append(lines, muted.Render("No matching modules. Esc to close."))
		return PadLines(lines, w, h, "")
	}

	avail := max(1, h-1)
	start, end := ScrollWindow(len(v.Results), v.Cursor, avail)
	for i := start; i < end; i++ {
		it := v.Results[i]
		badge := "[" + it.Tag + "]"
		st := "PH2"
		if it.Status == modules.StatusReady {
			st = "RDY"
		}
		if i == v.Cursor {
			nameW := max(6, w-len(badge)-len(st)-5)
			row := fmt.Sprintf("▸ %s %s %s", badge, FitLine(it.Name, nameW), st)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			nameW := max(6, w-len(badge)-len(st)-5)
			row := fmt.Sprintf("  %s %s %s", sec.Render(badge), txt.Render(FitLine(it.Name, nameW)), muted.Render(st))
			lines = append(lines, row)
		}
	}
	return PadLines(lines, w, h, "")
}
