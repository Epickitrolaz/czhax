package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
)

func RenderTopRow(sideW, mainW int, pal theme.Palette, cfg config.Config, activeView ViewID, activeTab int, tabsFocused bool, overlayTitle string, snap sysinfo.Snapshot) string {
	ascii := cfg.Border == config.BorderAscii
	vbar := "│"
	if ascii {
		vbar = "|"
	}

	brandStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.Accent)
	divStyle := lipgloss.NewStyle().Foreground(pal.Border)
	activeTabFocused := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	activeTabPassive := lipgloss.NewStyle().Bold(true).Underline(true).Foreground(pal.Accent)
	tabStyle := lipgloss.NewStyle().Foreground(pal.Muted)
	okStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warnStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	dangerStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)

	brandTxt := "CZHAX"
	if sideW >= 7 {
		brandTxt = " CZHAX "
	}
	leftCell := brandStyle.Render(FitLine(brandTxt, sideW))

	// Right status (battery + net + stealth)
	var stParts []string
	if cfg.StealthMode {
		stParts = append(stParts, warnStyle.Render("STL"))
	}
	if snap.HasBattery {
		pfx := ""
		if snap.Charging {
			pfx = "+"
		}
		st := okStyle
		if snap.BatteryPct <= 20 {
			st = dangerStyle
		} else if snap.BatteryPct <= 50 {
			st = warnStyle
		}
		stParts = append(stParts, st.Render(fmt.Sprintf("%s%d%%", pfx, snap.BatteryPct)))
	}
	if snap.NetUp {
		if snap.WifiUp {
			stParts = append(stParts, okStyle.Render("W"))
		} else {
			stParts = append(stParts, lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Render("N"))
		}
	}
	statusStr := strings.Join(stParts, " ")
	statusW := lipgloss.Width(statusStr)

	var tabArea string
	if overlayTitle != "" {
		tabArea = " " + activeTabFocused.Render(" "+overlayTitle+" ")
	} else {
		vd := TopViews[activeView]
		var tabStrs []string
		for i, lbl := range vd.Tabs {
			if i == activeTab {
				if tabsFocused {
					tabStrs = append(tabStrs, activeTabFocused.Render(" "+lbl+" "))
				} else {
					tabStrs = append(tabStrs, activeTabPassive.Render("["+lbl+"]"))
				}
			} else {
				tabStrs = append(tabStrs, tabStyle.Render(lbl))
			}
		}
		tabArea = " " + strings.Join(tabStrs, tabStyle.Render(" "))
	}

	availTabs := mainW - statusW - 1
	if statusW == 0 {
		availTabs = mainW
	}
	if availTabs < 4 {
		availTabs = mainW
		statusStr = ""
	}
	tabPart := FitLine(tabArea, availTabs)
	rightCell := tabPart
	if statusStr != "" {
		rightCell += " " + statusStr
	}

	return leftCell + divStyle.Render(vbar) + FitLine(rightCell, mainW)
}

func RenderDividerRow(sideW, mainW int, pal theme.Palette, cfg config.Config) string {
	ascii := cfg.Border == config.BorderAscii
	h, cross := "─", "┼"
	if ascii {
		h, cross = "-", "+"
	}
	divStyle := lipgloss.NewStyle().Foreground(pal.Border)
	return divStyle.Render(strings.Repeat(h, sideW) + cross + strings.Repeat(h, mainW))
}

func RenderSidebarColumn(sideW, bodyH int, pal theme.Palette, cfg config.Config, active ViewID, focused bool) []string {
	ascii := cfg.Border == config.BorderAscii
	marker := "▸"
	if ascii {
		marker = ">"
	}

	selFocused := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	selPassive := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	itemStyle := lipgloss.NewStyle().Foreground(pal.Text)

	start, end := ScrollWindow(len(TopViews), int(active), bodyH)
	lines := make([]string, 0, bodyH)
	for i := start; i < end; i++ {
		vd := TopViews[i]
		lbl := vd.Name
		if sideW < 7 {
			lbl = vd.Short3
		}
		if ViewID(i) == active {
			txt := FitLine(marker+lbl, sideW)
			if focused {
				lines = append(lines, selFocused.Render(txt))
			} else {
				lines = append(lines, selPassive.Render(txt))
			}
		} else {
			lines = append(lines, itemStyle.Render(FitLine(" "+lbl, sideW)))
		}
	}
	for len(lines) < bodyH {
		lines = append(lines, strings.Repeat(" ", sideW))
	}
	return lines
}

func JoinPaneColumns(sideLines []string, mainLines []string, sideW, mainW, bodyH int, pal theme.Palette, cfg config.Config) string {
	ascii := cfg.Border == config.BorderAscii
	vbar := "│"
	if ascii {
		vbar = "|"
	}
	div := lipgloss.NewStyle().Foreground(pal.Border).Render(vbar)

	paddedMain := SplitLinesPadded(mainLines, mainW, bodyH)
	rows := make([]string, 0, bodyH)
	for i := 0; i < bodyH; i++ {
		s := FitLine("", sideW)
		if i < len(sideLines) {
			s = sideLines[i]
		}
		rows = append(rows, s+div+paddedMain[i])
	}
	return strings.Join(rows, "\n")
}
