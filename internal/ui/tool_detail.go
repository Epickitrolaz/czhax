package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/modules"
	"github.com/czhax/czhax/internal/theme"
)

type ToolDetailView struct {
	Item   modules.Item
	Scroll int
}

func NewToolDetailView(it modules.Item) ToolDetailView {
	return ToolDetailView{Item: it}
}

func (v *ToolDetailView) Move(delta int) {
	v.Scroll += delta
	if v.Scroll < 0 {
		v.Scroll = 0
	}
}

func (v ToolDetailView) Render(w, h int, pal theme.Palette, cfg config.Config) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	isPinned := false
	for _, id := range cfg.PinnedIDs {
		if id == v.Item.ID {
			isPinned = true
			break
		}
	}
	pinLbl := "p:pin"
	if isPinned {
		pinLbl = "p:unpin"
	}

	var allLines []string
	allLines = append(allLines,
		accent.Render("["+v.Item.Tag+"] "+v.Item.Name)+" "+warn.Render("[PHASE 2]"),
	)
	allLines = append(allLines, txt.Render(Truncate(v.Item.Summary, w)))
	allLines = append(allLines, sec.Render("HW: ")+txt.Render(Truncate(v.Item.Hardware, max(4, w-4))))
	allLines = append(allLines, sec.Render("Priv: ")+txt.Render(Truncate(v.Item.Privileges, max(4, w-6))))

	if len(v.Item.PlannedOpts) > 0 {
		allLines = append(allLines, accent.Render("Sub-modes (Phase 2):"))
		for i, opt := range v.Item.PlannedOpts {
			allLines = append(allLines, fmt.Sprintf("  %s %s", muted.Render(fmt.Sprintf("%d.", i+1)), txt.Render(opt)))
		}
	}
	allLines = append(allLines, muted.Render("Action: "+pinLbl+" · Esc:back"))

	maxScroll := max(0, len(allLines)-h)
	scroll := min(v.Scroll, maxScroll)
	end := min(len(allLines), scroll+h)
	return PadLines(allLines[scroll:end], w, h, "")
}
