package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/theme"
)

type Hint struct {
	Key  string
	Desc string
}

func RenderFooter(w int, pal theme.Palette, cfg config.Config, hints []Hint, statusMsg string, mq ...Marquee) string {
	var m Marquee
	if len(mq) > 0 {
		m = mq[0]
	}
	barStyle := lipgloss.NewStyle().
		Foreground(pal.FooterFg).
		Background(pal.FooterBg)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Secondary).
		Background(pal.FooterBg)

	descStyle := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Background(pal.FooterBg)

	msgStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Warning).
		Background(pal.FooterBg)

	gap := barStyle.Render("   ")
	if statusMsg != "" {
		return barStyle.Width(w).Render(m.FitLineWithGap(msgStyle.Render(statusMsg), w, gap))
	}

	var parts []string
	for _, h := range hints {
		parts = append(parts, keyStyle.Render(h.Key)+descStyle.Render(":"+h.Desc))
	}

	line := strings.Join(parts, barStyle.Render(" "))
	return barStyle.Width(w).Render(m.FitLineWithGap(line, w, gap))
}
