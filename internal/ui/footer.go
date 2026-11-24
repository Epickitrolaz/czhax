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

func RenderFooter(w int, pal theme.Palette, cfg config.Config, hints []Hint, statusMsg string) string {
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

	if statusMsg != "" {
		return barStyle.Width(w).Render(FitLine(msgStyle.Render(statusMsg), w))
	}

	var parts []string
	used := 0
	for i, h := range hints {
		chunk := keyStyle.Render(h.Key) + descStyle.Render(":"+h.Desc)
		chunkW := lipgloss.Width(chunk)
		sepW := 0
		if i > 0 {
			sepW = 1
		}
		if used+sepW+chunkW > w {
			break
		}
		parts = append(parts, chunk)
		used += sepW + chunkW
	}

	line := strings.Join(parts, barStyle.Render(" "))
	return barStyle.Width(w).Render(FitLine(line, w))
}
