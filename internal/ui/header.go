package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
)

func RenderHeader(w int, pal theme.Palette, cfg config.Config, active Tab, breadcrumb string, snap sysinfo.Snapshot) string {
	brandStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.SelectFg).
		Background(pal.Accent).
		Padding(0, 1)

	barStyle := lipgloss.NewStyle().
		Foreground(pal.HeaderFg).
		Background(pal.HeaderBg)

	activeTabStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Accent).
		Background(pal.HeaderBg)

	tabStyle := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Background(pal.HeaderBg)

	mutedStyle := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Background(pal.HeaderBg)

	okStyle := lipgloss.NewStyle().
		Foreground(pal.Success).
		Background(pal.HeaderBg)

	warnStyle := lipgloss.NewStyle().
		Foreground(pal.Warning).
		Background(pal.HeaderBg)

	// Right status telemetry (compact for 45-53 cols)
	var statusParts []string
	if cfg.StealthMode {
		statusParts = append(statusParts, warnStyle.Render("STL"))
	}
	if snap.HasBattery {
		batPrefix := "B"
		if snap.Charging {
			batPrefix = "+"
		}
		st := okStyle
		if snap.BatteryPct <= 20 {
			st = warnStyle
		}
		statusParts = append(statusParts, st.Render(fmt.Sprintf("%s%d%%", batPrefix, snap.BatteryPct)))
	}
	if snap.NetUp {
		if snap.WifiUp {
			statusParts = append(statusParts, okStyle.Render("W"))
		} else {
			statusParts = append(statusParts, okStyle.Render("N"))
		}
	} else {
		statusParts = append(statusParts, mutedStyle.Render("-"))
	}
	if w >= 48 {
		statusParts = append(statusParts, mutedStyle.Render(time.Now().Format("15:04")))
	}
	right := strings.Join(statusParts, barStyle.Render(" "))
	rightW := lipgloss.Width(right)

	// Left: brand + either breadcrumb or compact tabs
	brand := "CZ"
	if w >= 52 && breadcrumb == "" {
		brand = "CZH"
	}
	left := brandStyle.Render(brand)

	if breadcrumb != "" {
		bcMax := w - lipgloss.Width(left) - rightW - 2
		bc := Truncate(breadcrumb, max(4, bcMax))
		left += barStyle.Render(" ") + activeTabStyle.Render("◂ "+bc)
	} else {
		tabs := []Tab{TabHome, TabSec, TabBasic, TabOptions}
		var tabStrs []string
		compact := w < 50
		for i, t := range tabs {
			lbl := t.Label()
			if compact {
				switch t {
				case TabHome:
					lbl = "Hm"
				case TabSec:
					lbl = "Sc"
				case TabBasic:
					lbl = "Bs"
				case TabOptions:
					lbl = "Op"
				}
			}
			txt := fmt.Sprintf("%d:%s", i+1, lbl)
			if t == active {
				tabStrs = append(tabStrs, activeTabStyle.Render("["+txt+"]"))
			} else {
				tabStrs = append(tabStrs, tabStyle.Render(txt))
			}
		}
		left += barStyle.Render(" ") + strings.Join(tabStrs, barStyle.Render(" "))
	}

	leftW := lipgloss.Width(left)
	gap := w - leftW - rightW
	if gap < 1 {
		// Trim left slightly if needed
		avail := w - rightW - 1
		if avail > 0 {
			left = FitLine(left, avail)
			gap = 1
		} else {
			gap = 0
		}
	}
	row := left + barStyle.Render(strings.Repeat(" ", max(0, gap))) + right
	return FitLine(row, w)
}
