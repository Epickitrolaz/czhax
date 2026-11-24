package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/modules"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
)

type DashView struct {
	Cursor int
	Scroll int
	Items  []modules.Item
}

func NewDashView(pinnedIDs []string) DashView {
	v := DashView{}
	v.RefreshPins(pinnedIDs)
	return v
}

func (v *DashView) RefreshPins(pinnedIDs []string) {
	var items []modules.Item
	for _, id := range pinnedIDs {
		if it, ok := modules.FindByID(id); ok {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		all := modules.All()
		items = all[:min(4, len(all))]
	}
	v.Items = items
	if v.Cursor >= len(v.Items) {
		v.Cursor = max(0, len(v.Items)-1)
	}
}

func (v *DashView) Move(tab int, delta int) {
	switch tab {
	case 1:
		if len(v.Items) == 0 {
			return
		}
		v.Cursor = (v.Cursor + delta + len(v.Items)) % len(v.Items)
	default:
		v.Scroll += delta
		if v.Scroll < 0 {
			v.Scroll = 0
		}
	}
}

func (v DashView) SelectedPin() (modules.Item, bool) {
	if len(v.Items) == 0 || v.Cursor < 0 || v.Cursor >= len(v.Items) {
		return modules.Item{}, false
	}
	return v.Items[v.Cursor], true
}

func barColor(pal theme.Palette, ratio float64) lipgloss.Color {
	if ratio >= 0.85 {
		return pal.Danger
	}
	if ratio >= 0.60 {
		return pal.Warning
	}
	return pal.Success
}

func tempColor(pal theme.Palette, c float64) lipgloss.Color {
	if c >= 75 {
		return pal.Danger
	}
	if c >= 55 {
		return pal.Warning
	}
	return pal.Success
}

func (v DashView) RenderLines(tab, w, h int, pal theme.Palette, cfg config.Config, snap sysinfo.Snapshot, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	ascii := cfg.Border == config.BorderAscii

	switch tab {
	case 1: // Pinned quick launch
		var out []string
		start, end := ScrollWindow(len(v.Items), v.Cursor, h)
		for i := start; i < end; i++ {
			it := v.Items[i]
			st := "PH2"
			if it.Status == modules.StatusReady {
				st = "RDY"
			}
			if i == v.Cursor && focused {
				nameW := max(6, w-len(st)-4)
				row := fmt.Sprintf(" ▸ %s %s", FitLine(it.Name, nameW), st)
				out = append(out, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "  "
				if i == v.Cursor {
					pfx = "▸ "
				}
				nameW := max(6, w-len(st)-4)
				stView := warn.Render(st)
				if it.Status == modules.StatusReady {
					stView = ok.Render(st)
				}
				out = append(out, fmt.Sprintf("%s%s %s",
					accent.Render(pfx),
					txt.Render(FitLine(it.Name, nameW)),
					stView,
				))
			}
		}
		return SplitLinesPadded(out, w, h)

	case 2: // System hardware summary
		hw := "Linux " + snap.Arch
		if snap.IsCardZero {
			hw = "Cardputer Zero CM0"
		}
		netView := danger.Render("Offline")
		if snap.NetUp {
			if cfg.StealthMode {
				netView = ok.Render("Online (masked)")
			} else {
				netView = ok.Render("Online ") + txt.Render(snap.PrimaryIP)
			}
		}
		rows := []string{
			" " + sec.Render("HW   ") + txt.Render(hw),
			" " + sec.Render("Kern ") + txt.Render(Truncate(snap.Kernel, max(6, w-6))),
			" " + sec.Render("Net  ") + netView,
			" " + sec.Render("Load ") + txt.Render(snap.Load1+"  Up "+sysinfo.FormatUptime(snap.Uptime)),
			" " + sec.Render("Disk ") + txt.Render(fmt.Sprintf("%.1fG / %.1fG free", snap.DiskFreeGB, snap.DiskTotGB)),
		}
		if snap.HasBattery {
			bCol := pal.Success
			if snap.BatteryPct <= 20 {
				bCol = pal.Danger
			} else if snap.BatteryPct <= 50 {
				bCol = pal.Warning
			}
			rows = append(rows, " "+sec.Render("Bat  ")+lipgloss.NewStyle().Bold(true).Foreground(bCol).Render(fmt.Sprintf("%d%%", snap.BatteryPct)))
		}
		maxS := max(0, len(rows)-h)
		sc := min(v.Scroll, maxS)
		end := min(len(rows), sc+h)
		return SplitLinesPadded(rows[sc:end], w, h)

	default: // Pulse
		host := snap.Hostname
		ip := snap.PrimaryIP
		if ip == "" {
			ip = "offline"
		}
		if cfg.StealthMode {
			host = "stealth"
			ip = "masked"
		}
		var memRatio float64
		if snap.MemTotalMB > 0 {
			memRatio = float64(snap.MemUsedMB) / float64(snap.MemTotalMB)
		}
		barW := 8
		if w < 34 {
			barW = 5
		}
		memBar := lipgloss.NewStyle().Bold(true).Foreground(barColor(pal, memRatio)).Render(MiniBar(memRatio, barW, ascii))

		tempView := muted.Render("n/a")
		if snap.HasTemp {
			tempView = lipgloss.NewStyle().Bold(true).Foreground(tempColor(pal, snap.TempC)).Render(fmt.Sprintf("%.0f°C", snap.TempC))
		}
		batView := txt.Render("AC")
		if snap.HasBattery {
			bCol := pal.Success
			if snap.BatteryPct <= 20 {
				bCol = pal.Danger
			} else if snap.BatteryPct <= 50 {
				bCol = pal.Warning
			}
			bTxt := fmt.Sprintf("%d%%", snap.BatteryPct)
			if snap.Charging {
				bTxt += "+"
			}
			batView = lipgloss.NewStyle().Bold(true).Foreground(bCol).Render(bTxt)
		}

		ipView := accent.Render(Truncate(ip, max(6, w-6)))
		if ip == "offline" {
			ipView = danger.Render(ip)
		}

		rows := []string{
			" " + sec.Render("Host ") + txt.Render(Truncate(host, max(6, w-6))),
			" " + sec.Render("IP   ") + ipView,
			fmt.Sprintf(" %s %s %s", sec.Render("RAM "), memBar, txt.Render(fmt.Sprintf("%d/%dM", snap.MemUsedMB, snap.MemTotalMB))),
			" " + sec.Render("Up   ") + txt.Render(sysinfo.FormatUptime(snap.Uptime)) + "  " + sec.Render("Ld ") + txt.Render(snap.Load1),
			" " + sec.Render("Temp ") + tempView + "  " + sec.Render("Bat ") + batView,
		}
		if h >= 7 {
			rows = append(rows, "")
			rows = append(rows, " "+sec.Render("→")+muted.Render(" open  ")+sec.Render("Tab")+muted.Render(" tabs  ")+sec.Render("/")+muted.Render(" find"))
		}
		maxS := max(0, len(rows)-h)
		sc := min(v.Scroll, maxS)
		end := min(len(rows), sc+h)
		return SplitLinesPadded(rows[sc:end], w, h)
	}
}
