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

	keyColors := []lipgloss.Color{pal.Yellow, pal.Cyan, pal.Magenta, pal.Green, pal.Orange}

	descStyle := lipgloss.NewStyle().
		Foreground(pal.Text).
		Background(pal.FooterBg)

	msgStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Yellow).
		Background(pal.FooterBg)

	if statusMsg != "" {
		return barStyle.Width(w).Render(FitLine(msgStyle.Render(statusMsg), w))
	}

	if cfg.ShowFnBar {
		fnHints := []Hint{
			{Key: "F4", Desc: "Dsh"},
			{Key: "F5", Desc: "Rcn"},
			{Key: "F6", Desc: "Net"},
			{Key: "F7", Desc: "Sys"},
			{Key: "F8", Desc: "Opt"},
		}
		var parts []string
		for i, h := range fnHints {
			ks := lipgloss.NewStyle().Bold(true).Foreground(keyColors[i%len(keyColors)]).Background(pal.FooterBg)
			parts = append(parts, ks.Render(h.Key)+descStyle.Render(":"+h.Desc))
		}
		line := strings.Join(parts, barStyle.Render(" "))
		return barStyle.Width(w).Render(FitLine(line, w))
	}

	var parts []string
	used := 0
	for i, h := range hints {
		ks := lipgloss.NewStyle().Bold(true).Foreground(keyColors[i%len(keyColors)]).Background(pal.FooterBg)
		chunk := ks.Render(h.Key) + descStyle.Render(":"+h.Desc)
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
