package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/modules"
	"github.com/czhax/czhax/internal/theme"
)

type ModulePanelView struct {
	Cursors map[string]int
}

func NewModulePanelView() ModulePanelView {
	return ModulePanelView{Cursors: map[string]int{}}
}

func (v *ModulePanelView) Move(it modules.Item, delta int) {
	if len(it.PlannedOpts) == 0 {
		return
	}
	cur := v.Cursors[it.ID]
	cur = (cur + delta + len(it.PlannedOpts)) % len(it.PlannedOpts)
	v.Cursors[it.ID] = cur
}

func (v ModulePanelView) RenderLines(it modules.Item, w, h int, pal theme.Palette, cfg config.Config, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	pinned := false
	for _, id := range cfg.PinnedIDs {
		if id == it.ID {
			pinned = true
			break
		}
	}
	pinTxt := muted.Render("p:pin")
	if pinned {
		pinTxt = sec.Render("★") + muted.Render(" p:unpin")
	}

	var rows []string
	rows = append(rows, " "+accent.Render(it.Name)+" "+warn.Render("[PH2]"))
	rows = append(rows, " "+txt.Render(Truncate(it.Summary, max(6, w-1))))
	if h >= 6 {
		rows = append(rows, " "+sec.Render("HW   ")+muted.Render(Truncate(it.Hardware, max(6, w-6))))
	}
	if h >= 7 {
		rows = append(rows, " "+sec.Render("Priv ")+muted.Render(Truncate(it.Privileges, max(6, w-6))))
	}

	cur := v.Cursors[it.ID]
	if cur >= len(it.PlannedOpts) {
		cur = 0
	}

	avail := h - len(rows)
	if h >= 8 && avail > 1 {
		avail--
	}
	if avail > 0 && len(it.PlannedOpts) > 0 {
		start, end := ScrollWindow(len(it.PlannedOpts), cur, avail)
		for i := start; i < end; i++ {
			opt := it.PlannedOpts[i]
			if i == cur && focused {
				row := fmt.Sprintf(" ▸ %s", opt)
				rows = append(rows, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "   "
				if i == cur {
					pfx = accent.Render(" ▸ ")
				}
				rows = append(rows, pfx+txt.Render(Truncate(opt, max(4, w-3))))
			}
		}
	}

	for len(rows) < h-1 {
		rows = append(rows, "")
	}
	if len(rows) < h {
		rows = append(rows, " "+pinTxt+muted.Render(" · ← side"))
	}

	return SplitLinesPadded(rows, w, h)
}
