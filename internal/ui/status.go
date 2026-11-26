package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
)

type StatusView struct {
	Scroll  int
	Marquee Marquee
}

func (v *StatusView) Move(delta int, totalRows, visibleH int) {
	maxScroll := max(0, totalRows-visibleH)
	v.Scroll += delta
	if v.Scroll < 0 {
		v.Scroll = 0
	}
	if v.Scroll > maxScroll {
		v.Scroll = maxScroll
	}
}

func (v StatusView) BuildRows(w int, pal theme.Palette, cfg config.Config, snap sysinfo.Snapshot) []string {
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ascii := cfg.Border == config.BorderAscii

	hw := "Linux " + snap.Arch
	if snap.IsCardZero {
		hw = "CardZero CM0"
	}

	ipView := danger.Render("offline")
	if snap.NetUp && snap.PrimaryIP != "" {
		ipView = ok.Render(v.Marquee.Truncate(snap.PrimaryIP, max(6, w-7)))
	}

	var memRatio, diskRatio float64
	if snap.MemTotalMB > 0 {
		memRatio = float64(snap.MemUsedMB) / float64(snap.MemTotalMB)
	}
	if snap.DiskTotGB > 0 {
		diskRatio = (snap.DiskTotGB - snap.DiskFreeGB) / snap.DiskTotGB
	}

	barW := 8
	if w < 34 {
		barW = 5
	}
	memBar := lipgloss.NewStyle().Bold(true).Foreground(barColor(pal, memRatio)).Render(MiniBar(memRatio, barW, ascii))
	diskBar := lipgloss.NewStyle().Bold(true).Foreground(barColor(pal, diskRatio)).Render(MiniBar(diskRatio, barW, ascii))

	tempView := muted.Render("n/a")
	if snap.HasTemp {
		tempView = lipgloss.NewStyle().Bold(true).Foreground(tempColor(pal, snap.TempC)).Render(fmt.Sprintf("%.1fC", snap.TempC))
	}

	batView := muted.Render("AC power")
	batLeftView := muted.Render("AC power")
	if snap.HasBattery {
		bStyle := ok
		if snap.BatteryPct <= 20 {
			bStyle = danger
		} else if snap.BatteryPct <= 50 {
			bStyle = warn
		}
		state := "disch"
		if snap.Charging {
			state = "chg"
		}
		batView = bStyle.Render(fmt.Sprintf("%d%%", snap.BatteryPct)) + " " + muted.Render("("+state+")")

		switch {
		case snap.Charging && snap.HasFullTime:
			batLeftView = ok.Render("full in " + sysinfo.FormatDuration(snap.BatTimeToFull))
		case snap.Charging:
			batLeftView = ok.Render("charging")
		case snap.HasBatTime:
			tStyle := ok
			if snap.BatTimeLeft <= 30*60*1e9 {
				tStyle = danger
			} else if snap.BatTimeLeft <= 90*60*1e9 {
				tStyle = warn
			}
			batLeftView = tStyle.Render(sysinfo.FormatDuration(snap.BatTimeLeft) + " left")
		default:
			batLeftView = muted.Render("estimating...")
		}
	}

	return []string{
		" " + sec.Render("Host  ") + txt.Render(v.Marquee.Truncate(snap.Hostname, max(6, w-14))) + " " + muted.Render(hw),
		" " + sec.Render("IP    ") + ipView,
		fmt.Sprintf(" %s %s %s", sec.Render("RAM  "), memBar, txt.Render(fmt.Sprintf("%d/%dM", snap.MemUsedMB, snap.MemTotalMB))),
		fmt.Sprintf(" %s %s %s", sec.Render("Disk "), diskBar, txt.Render(fmt.Sprintf("%.1f/%.1fG", snap.DiskFreeGB, snap.DiskTotGB))),
		" " + sec.Render("Bat   ") + batView,
		" " + sec.Render("Left  ") + batLeftView,
		" " + sec.Render("Temp  ") + tempView + "  " + sec.Render("Ld ") + txt.Render(snap.Load1),
		" " + sec.Render("Up    ") + txt.Render(sysinfo.FormatDuration(snap.Uptime)),
	}
}

func (v StatusView) RenderLines(w, h int, pal theme.Palette, cfg config.Config, snap sysinfo.Snapshot, focused bool) []string {
	rows := v.BuildRows(w, pal, cfg, snap)
	maxS := max(0, len(rows)-h)
	sc := min(v.Scroll, maxS)
	end := min(len(rows), sc+h)
	visible := rows[sc:end]
	if focused && len(visible) > 0 && maxS > 0 {
		// Show compact scroll position on last visible line when scrollable
		muted := lipgloss.NewStyle().Foreground(pal.Muted)
		indicator := muted.Render(fmt.Sprintf(" %d-%d/%d", sc+1, end, len(rows)))
		if lipgloss.Width(visible[len(visible)-1])+lipgloss.Width(indicator) <= w {
			visible[len(visible)-1] += indicator
		}
	}
	return v.Marquee.SplitLinesPadded(visible, w, h)
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
