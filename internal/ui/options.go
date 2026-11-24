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
	OptSidebar
	OptBorder
	OptKeyHints
	OptFnBar
	OptStealth

	OptNavMode
	OptRefresh
	OptConfirmQuit
	OptReset
)

var displayKeys = []OptionKey{
	OptTheme,
	OptViewport,
	OptSidebar,
	OptBorder,
	OptKeyHints,
	OptFnBar,
	OptStealth,
}

var controlKeys = []OptionKey{
	OptNavMode,
	OptRefresh,
	OptConfirmQuit,
	OptReset,
}

type OptionsView struct {
	Cursors [3]int // per horizontal tab: 0=Display, 1=Ctrls, 2=Keys (scroll)
}

func (v *OptionsView) Move(tab, delta int) {
	switch tab {
	case 0:
		v.Cursors[0] = (v.Cursors[0] + delta + len(displayKeys)) % len(displayKeys)
	case 1:
		v.Cursors[1] = (v.Cursors[1] + delta + len(controlKeys)) % len(controlKeys)
	case 2:
		v.Cursors[2] += delta
		if v.Cursors[2] < 0 {
			v.Cursors[2] = 0
		}
	}
}

func (v *OptionsView) Adjust(tab, delta int, cfg *config.Config) string {
	var keys []OptionKey
	switch tab {
	case 0:
		keys = displayKeys
	case 1:
		keys = controlKeys
	default:
		return ""
	}
	cur := v.Cursors[tab]
	if cur < 0 || cur >= len(keys) {
		return ""
	}
	key := keys[cur]

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
			config.ViewportCZ4010,
			config.ViewportWide,
		}
		cfg.Viewport = cycleSlice(modes, cfg.Viewport, delta)
	case OptSidebar:
		modes := []config.SidebarMode{
			config.SidebarAuto,
			config.SidebarWide,
			config.SidebarSlim,
		}
		cfg.Sidebar = cycleSlice(modes, cfg.Sidebar, delta)
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
	case OptStealth:
		cfg.StealthMode = !cfg.StealthMode
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
	case OptConfirmQuit:
		cfg.ConfirmQuit = !cfg.ConfirmQuit
	case OptReset:
		if delta >= 0 {
			*cfg = config.Default()
			return "reset"
		}
		return ""
	}
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

func optionRow(key OptionKey, cfg config.Config) (label, val string) {
	switch key {
	case OptTheme:
		return "Theme", theme.Resolve(cfg.Theme, false).Name
	case OptViewport:
		switch cfg.Viewport {
		case config.ViewportAuto:
			return "Viewport", "Auto"
		case config.ViewportCZ5314:
			return "Viewport", "CZ 53x14"
		case config.ViewportCZ4510:
			return "Viewport", "CZ 45x10"
		case config.ViewportCZ4010:
			return "Viewport", "CZ 40x10"
		case config.ViewportWide:
			return "Viewport", "Full"
		}
	case OptSidebar:
		switch cfg.Sidebar {
		case config.SidebarAuto:
			return "Sidebar", "Auto"
		case config.SidebarWide:
			return "Sidebar", "Wide 7c"
		case config.SidebarSlim:
			return "Sidebar", "Slim 3c"
		}
	case OptBorder:
		return "Style", string(cfg.Border)
	case OptKeyHints:
		return "Footer", onOff(cfg.ShowKeyHints)
	case OptFnBar:
		return "Fn4-8 Bar", onOff(cfg.ShowFnBar)
	case OptStealth:
		return "Stealth", onOff(cfg.StealthMode)
	case OptNavMode:
		switch cfg.NavMode {
		case config.NavArrowsVim:
			return "Nav Keys", "Arr+hjkl"
		case config.NavArrows:
			return "Nav Keys", "Arrows"
		case config.NavFnSoft:
			return "Nav Keys", "Fn+Arr"
		}
	case OptRefresh:
		if cfg.RefreshSeconds == 0 {
			return "Refresh", "Manual"
		}
		return "Refresh", fmt.Sprintf("%ds", cfg.RefreshSeconds)
	case OptConfirmQuit:
		return "Ask Quit", onOff(cfg.ConfirmQuit)
	case OptReset:
		return "Reset", "[Enter]"
	}
	return "", ""
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func (v OptionsView) RenderLines(tab, w, h int, pal theme.Palette, cfg config.Config, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	if tab == 2 { // Keys reference tab
		rows := []string{
			" " + sec.Render("↑↓/Fn+F,X ") + txt.Render("Move up / down"),
			" " + sec.Render("←→/Fn+Z,C ") + txt.Render("Sidebar ↔ Pane"),
			" " + sec.Render("Tab / 1-3 ") + txt.Render("Switch top tabs"),
			" " + sec.Render("F4 - F8   ") + txt.Render("Jump top views"),
			" " + sec.Render("Enter/Spc ") + txt.Render("Open / toggle opt"),
			" " + sec.Render("/  or  ?  ") + txt.Render("Find / Help"),
			" " + sec.Render("p / t / s ") + txt.Render("Pin / Theme / Stl"),
			" " + sec.Render("q         ") + txt.Render("Quit CZHax"),
		}
		maxS := max(0, len(rows)-h)
		sc := min(v.Cursors[2], maxS)
		end := min(len(rows), sc+h)
		return SplitLinesPadded(rows[sc:end], w, h)
	}

	keys := displayKeys
	if tab == 1 {
		keys = controlKeys
	}
	cur := v.Cursors[tab]
	if cur >= len(keys) {
		cur = len(keys) - 1
	}

	avail := max(1, h-1)
	if h <= 5 {
		avail = h
	}
	start, end := ScrollWindow(len(keys), cur, avail)
	var out []string
	for i := start; i < end; i++ {
		lbl, val := optionRow(keys[i], cfg)
		if i == cur && focused {
			lblW := max(7, w-len(val)-6)
			row := fmt.Sprintf(" ▸ %s <%s>", FitLine(lbl, lblW), val)
			out = append(out, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if i == cur {
				pfx = " ▸ "
			}
			lblW := max(7, w-len(val)-5)
			out = append(out, fmt.Sprintf("%s%s %s",
				accent.Render(pfx),
				txt.Render(FitLine(lbl, lblW)),
				sec.Render(val),
			))
		}
	}
	if len(out) < h {
		for len(out) < h-1 {
			out = append(out, "")
		}
		out = append(out, " "+muted.Render("Enter/Space:change · ← sidebar"))
	}
	return SplitLinesPadded(out, w, h)
}
