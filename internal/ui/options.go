package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/theme"
)

type OptionKey int

const (
	OptTheme OptionKey = iota
	OptViewport
	OptBorder
	OptKeyHints
	OptFnBar
	OptNavMode
	OptRefresh
	OptStealth
	OptConfirmQuit
	OptReset
	optCount
)

type OptionsView struct {
	Cursor int
	Dirty  bool
}

func (v *OptionsView) Move(delta int) {
	v.Cursor = (v.Cursor + delta + int(optCount)) % int(optCount)
}

func (v *OptionsView) Adjust(delta int, cfg *config.Config) (action string) {
	key := OptionKey(v.Cursor)
	switch key {
	case OptTheme:
		if delta >= 0 {
			cfg.Theme = theme.NextID(cfg.Theme)
		} else {
			cfg.Theme = theme.PrevID(cfg.Theme)
		}
	case OptViewport:
		modes := []config.ViewportMode{
			config.ViewportAuto,
			config.ViewportCZ5314,
			config.ViewportCZ4510,
			config.ViewportWide,
		}
		cfg.Viewport = cycleSlice(modes, cfg.Viewport, delta)
	case OptBorder:
		modes := []config.BorderMode{
			config.BorderCompact,
			config.BorderMinimal,
			config.BorderAscii,
		}
		cfg.Border = cycleSlice(modes, cfg.Border, delta)
	case OptKeyHints:
		cfg.ShowKeyHints = !cfg.ShowKeyHints
	case OptFnBar:
		cfg.ShowFnBar = !cfg.ShowFnBar
	case OptNavMode:
		modes := []config.NavKeyMode{
			config.NavArrowsVim,
			config.NavArrows,
			config.NavFnSoft,
		}
		cfg.NavMode = cycleSlice(modes, cfg.NavMode, delta)
	case OptRefresh:
		vals := []int{0, 1, 2, 5, 10}
		cfg.RefreshSeconds = cycleSlice(vals, cfg.RefreshSeconds, delta)
	case OptStealth:
		cfg.StealthMode = !cfg.StealthMode
	case OptConfirmQuit:
		cfg.ConfirmQuit = !cfg.ConfirmQuit
	case OptReset:
		if delta == 0 {
			*cfg = config.Default()
			v.Dirty = true
			return "reset"
		}
		return ""
	}
	v.Dirty = true
	return "changed"
}

func cycleSlice[T comparable](items []T, current T, delta int) T {
	idx := 0
	for i, it := range items {
		if it == current {
			idx = i
			break
		}
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	return items[(idx+step+len(items))%len(items)]
}

func optionRow(key OptionKey, cfg config.Config) (label, val, hint string) {
	switch key {
	case OptTheme:
		p := theme.Resolve(cfg.Theme, false)
		return "Theme", p.Name, "Color palette tuned for 1.9\" ST7789 LCD"
	case OptViewport:
		var lbl string
		switch cfg.Viewport {
		case config.ViewportAuto:
			lbl = "Auto"
		case config.ViewportCZ5314:
			lbl = "CZ 53x14"
		case config.ViewportCZ4510:
			lbl = "CZ 45x10"
		case config.ViewportWide:
			lbl = "Full Term"
		}
		return "Viewport", lbl, "Constrain UI to exact Cardputer Zero dimensions"
	case OptBorder:
		return "Style", string(cfg.Border), "compact / minimal / pure ASCII symbols"
	case OptKeyHints:
		return "Footer Hints", onOff(cfg.ShowKeyHints), "Hide footer to gain +1 row on small screens"
	case OptFnBar:
		return "Fn4-8 Strip", onOff(cfg.ShowFnBar), "Show Cardputer Zero Fn+4..8 soft-key bar"
	case OptNavMode:
		switch cfg.NavMode {
		case config.NavArrowsVim:
			return "Nav Keys", "Arrows+hjkl", "Support arrows (Fn+F/X/Z/C) and h/j/k/l"
		case config.NavArrows:
			return "Nav Keys", "Arrows only", "Use arrow keys / Fn+F/X/Z/C only"
		case config.NavFnSoft:
			return "Nav Keys", "Fn+Arrows", "Prioritize F4-F8 softkeys + arrows"
		}
	case OptRefresh:
		if cfg.RefreshSeconds == 0 {
			return "Telemetry", "Manual", "Press 'r' to refresh sysinfo manually"
		}
		return "Telemetry", fmt.Sprintf("%ds", cfg.RefreshSeconds), "System stats polling interval"
	case OptStealth:
		return "Stealth Mode", onOff(cfg.StealthMode), "Dim accents & mask host/IP/MAC in UI"
	case OptConfirmQuit:
		return "Confirm Quit", onOff(cfg.ConfirmQuit), "Prompt before exiting CZHax"
	case OptReset:
		return "Reset Defaults", "[Enter]", "Restore default Phase 1 options"
	}
	return "", "", ""
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func (v OptionsView) Render(w, h int, pal theme.Palette, cfg config.Config) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	var lines []string
	saveTag := ""
	if v.Dirty {
		saveTag = " *"
	}
	lines = append(lines, accent.Render("OPTIONS"+saveTag)+" "+muted.Render(ScrollBadge(v.Cursor, int(optCount)))+" "+muted.Render("←→/⏎:edit"))

	showDesc := h >= 5
	avail := h - 1
	if showDesc {
		avail--
	}
	if avail < 1 {
		avail = 1
	}

	start, end := ScrollWindow(int(optCount), v.Cursor, avail)
	for i := start; i < end; i++ {
		lbl, val, _ := optionRow(OptionKey(i), cfg)
		if i == v.Cursor {
			lblW := max(8, w-len(val)-6)
			row := fmt.Sprintf("▸ %s <%s>", FitLine(lbl, lblW), val)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			lblW := max(8, w-len(val)-4)
			row := fmt.Sprintf("  %s %s", txt.Render(FitLine(lbl, lblW)), sec.Render(val))
			lines = append(lines, row)
		}
	}

	for len(lines) < h-1 && showDesc {
		lines = append(lines, "")
	}
	if showDesc {
		_, _, hint := optionRow(OptionKey(v.Cursor), cfg)
		lines = append(lines, muted.Render(Truncate(hint, w)))
	}

	return PadLines(lines, w, h, "")
}
