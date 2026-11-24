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
		Foreground(pal.Accent).
		Background(pal.FooterBg)

	descStyle := lipgloss.NewStyle().
		Foreground(pal.FooterFg).
		Background(pal.FooterBg)

	msgStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Warning).
		Background(pal.FooterBg)

	if statusMsg != "" {
		return barStyle.Width(w).Render(FitLine(msgStyle.Render(statusMsg), w))
	}

	// If Fn Softkeys bar is enabled, render F4..F8 hardware strip matching Cardputer Zero keys below display
	if cfg.ShowFnBar {
		fnHints := []Hint{
			{Key: "F4", Desc: "Hm"},
			{Key: "F5", Desc: "Sc"},
			{Key: "F6", Desc: "Bs"},
			{Key: "F7", Desc: "Op"},
			{Key: "F8", Desc: "?"},
		}
		var parts []string
		for _, h := range fnHints {
			parts = append(parts, keyStyle.Render(h.Key)+descStyle.Render(":"+h.Desc))
		}
		line := strings.Join(parts, barStyle.Render(" "))
		return barStyle.Width(w).Render(FitLine(line, w))
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
