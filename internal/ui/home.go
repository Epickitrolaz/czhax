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
	case 1: // Pinned
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

func (v DashView) RenderLines(tab, w, h int, pal theme.Palette, cfg config.Config, snap sysinfo.Snapshot, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
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
		netStr := "Offline"
		if snap.NetUp {
			netStr = "Online (" + snap.PrimaryIP + ")"
			if cfg.StealthMode {
				netStr = "Online (masked)"
			}
		}
		rows := []string{
			" " + sec.Render("HW   ") + txt.Render(hw),
			" " + sec.Render("Kern ") + txt.Render(Truncate(snap.Kernel, max(6, w-6))),
			" " + sec.Render("Net  ") + txt.Render(Truncate(netStr, max(6, w-6))),
			" " + sec.Render("Load ") + txt.Render(snap.Load1+"  Up "+sysinfo.FormatUptime(snap.Uptime)),
			" " + sec.Render("Disk ") + txt.Render(fmt.Sprintf("%.1fG / %.1fG free", snap.DiskFreeGB, snap.DiskTotGB)),
		}
		if snap.HasBattery {
			rows = append(rows, " "+sec.Render("Bat  ")+txt.Render(fmt.Sprintf("%d%%", snap.BatteryPct)))
		}
		maxS := max(0, len(rows)-h)
		sc := min(v.Scroll, maxS)
		end := min(len(rows), sc+h)
		return SplitLinesPadded(rows[sc:end], w, h)

	default: // Pulse (default high-readability dashboard)
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
		tempStr := "n/a"
		if snap.HasTemp {
			tempStr = fmt.Sprintf("%.0f°C", snap.TempC)
		}
		batStr := "AC"
		if snap.HasBattery {
			batStr = fmt.Sprintf("%d%%", snap.BatteryPct)
			if snap.Charging {
				batStr += "+"
			}
		}

		rows := []string{
			" " + sec.Render("Host ") + txt.Render(Truncate(host, max(6, w-6))),
			" " + sec.Render("IP   ") + accent.Render(Truncate(ip, max(6, w-6))),
			fmt.Sprintf(" %s %s %d/%dM", sec.Render("RAM "), accent.Render(MiniBar(memRatio, barW, ascii)), snap.MemUsedMB, snap.MemTotalMB),
			" " + sec.Render("Up   ") + txt.Render(sysinfo.FormatUptime(snap.Uptime)+"  Load "+snap.Load1),
			" " + sec.Render("Temp ") + txt.Render(tempStr) + "  " + sec.Render("Bat ") + txt.Render(batStr),
		}
		if h >= 7 {
			rows = append(rows, "")
			rows = append(rows, " "+muted.Render("→ focus  Tab tabs  / find"))
		}
		maxS := max(0, len(rows)-h)
		sc := min(v.Scroll, maxS)
		end := min(len(rows), sc+h)
		return SplitLinesPadded(rows[sc:end], w, h)
	}
}
