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

func (v SearchView) RenderLines(w, h int, pal theme.Palette) []string {
	cyan := lipgloss.NewStyle().Bold(true).Foreground(pal.Cyan)
	grn := lipgloss.NewStyle().Bold(true).Foreground(pal.Green)
	org := lipgloss.NewStyle().Bold(true).Foreground(pal.Orange)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#000000")).Background(pal.Cyan)

	var lines []string
	prompt := fmt.Sprintf("/ %s_", v.Query)
	lines = append(lines, " "+cyan.Render(Truncate(prompt, max(6, w-1))))

	if len(v.Results) == 0 {
		lines = append(lines, " "+muted.Render("No match. Esc to close."))
		return SplitLinesPadded(lines, w, h)
	}

	avail := max(1, h-1)
	start, end := ScrollWindow(len(v.Results), v.Cursor, avail)
	for i := start; i < end; i++ {
		it := v.Results[i]
		st := "PH2"
		if it.Status == modules.StatusReady {
			st = "RDY"
		}
		if i == v.Cursor {
			nameW := max(6, w-len(st)-5)
			row := fmt.Sprintf(" ▸ %s %s", FitLine(it.Name, nameW), st)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			nameW := max(6, w-len(st)-5)
			stView := org.Render(st)
			if it.Status == modules.StatusReady {
				stView = grn.Render(st)
			}
			vCol := lipgloss.NewStyle().Bold(true).Foreground(theme.ViewColor(pal, it.Target.View))
			row := fmt.Sprintf(" %s %s %s", vCol.Render("•"), txt.Render(FitLine(it.Name, nameW)), stView)
			lines = append(lines, row)
		}
	}
	return SplitLinesPadded(lines, w, h)
}
