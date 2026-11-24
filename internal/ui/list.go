package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/modules"
	"github.com/czhax/czhax/internal/theme"
)

type ModuleListView struct {
	Title  string
	Items  []modules.Item
	Cursor int
}

func NewModuleListView(title string, items []modules.Item) ModuleListView {
	return ModuleListView{
		Title: title,
		Items: items,
	}
}

func (v *ModuleListView) Move(delta int) {
	if len(v.Items) == 0 {
		return
	}
	v.Cursor = (v.Cursor + delta + len(v.Items)) % len(v.Items)
}

func (v ModuleListView) Selected() (modules.Item, bool) {
	if len(v.Items) == 0 || v.Cursor < 0 || v.Cursor >= len(v.Items) {
		return modules.Item{}, false
	}
	return v.Items[v.Cursor], true
}

func (v ModuleListView) Render(w, h int, pal theme.Palette, cfg config.Config) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Foreground(pal.Success)
	warn := lipgloss.NewStyle().Foreground(pal.Warning)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	ascii := cfg.Border == config.BorderAscii

	var lines []string

	// Row 1: Section title + scroll counter
	hdr := accent.Render(v.Title) + " " + muted.Render(ScrollBadge(v.Cursor, len(v.Items)))
	lines = append(lines, hdr)

	// Reserve 1 bottom row for selected item summary if h >= 5
	showSummary := h >= 5 && len(v.Items) > 0
	listRows := h - 1
	if showSummary {
		listRows--
	}
	if listRows < 1 {
		listRows = 1
	}

	pinnedSet := map[string]bool{}
	for _, id := range cfg.PinnedIDs {
		pinnedSet[id] = true
	}

	start, end := ScrollWindow(len(v.Items), v.Cursor, listRows)
	for i := start; i < end; i++ {
		it := v.Items[i]
		badge := fmt.Sprintf("[%-4s]", Truncate(it.Tag, 4))
		pin := " "
		if pinnedSet[it.ID] {
			pin = "*"
			if !ascii {
				pin = "★"
			}
		}
		st := "PH2"
		if it.Status == modules.StatusReady {
			st = "RDY"
		}

		if i == v.Cursor {
			prefix := "▸"
			if ascii {
				prefix = ">"
			}
			nameW := max(6, w-len(prefix)-len(badge)-len(st)-5)
			row := fmt.Sprintf("%s%s %s %s %s", prefix, badge, FitLine(it.Name, nameW), pin, st)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			stStyled := warn.Render(st)
			if it.Status == modules.StatusReady {
				stStyled = ok.Render(st)
			}
			nameW := max(6, w-1-len(badge)-len(st)-5)
			row := fmt.Sprintf(" %s %s %s %s",
				sec.Render(badge),
				txt.Render(FitLine(it.Name, nameW)),
				accent.Render(pin),
				stStyled,
			)
			lines = append(lines, row)
		}
	}

	for len(lines) < h-1 && showSummary {
		lines = append(lines, "")
	}
	if showSummary {
		if selItem, ok := v.Selected(); ok {
			lines = append(lines, muted.Render(Truncate(selItem.Summary, w)))
		}
	}

	return PadLines(lines, w, h, "")
}
