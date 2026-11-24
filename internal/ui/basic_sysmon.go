package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
)

type SysMonView struct {
	Scroll int
}

func (v *SysMonView) Move(delta int) {
	v.Scroll += delta
	if v.Scroll < 0 {
		v.Scroll = 0
	}
}

func (v SysMonView) RenderLines(w, h int, pal theme.Palette, cfg config.Config, snap sysinfo.Snapshot) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ascii := cfg.Border == config.BorderAscii

	host := snap.Hostname
	if cfg.StealthMode {
		host = "[masked]"
	}
	hw := "Linux " + snap.Arch
	if snap.IsCardZero {
		hw = "RPi CM0"
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

	rows := []string{
		" " + sec.Render("Host ") + txt.Render(host+" ("+hw+")"),
		" " + sec.Render("Kern ") + txt.Render(Truncate(snap.Kernel, max(6, w-6))),
		" " + sec.Render("Up   ") + txt.Render(sysinfo.FormatUptime(snap.Uptime)+"  Load "+snap.Load1),
		fmt.Sprintf(" %s %s %d/%dM",
			sec.Render("RAM "),
			accent.Render(MiniBar(memRatio, barW, ascii)),
			snap.MemUsedMB, snap.MemTotalMB,
		),
		fmt.Sprintf(" %s %s %.1f/%.1fG",
			sec.Render("Disk"),
			accent.Render(MiniBar(diskRatio, barW, ascii)),
			snap.DiskFreeGB, snap.DiskTotGB,
		),
	}

	if snap.HasTemp {
		rows = append(rows, " "+sec.Render("Temp ")+txt.Render(fmt.Sprintf("%.1f°C", snap.TempC)))
	} else {
		rows = append(rows, " "+sec.Render("Temp ")+muted.Render("n/a"))
	}

	if snap.HasBattery {
		chg := "discharging"
		if snap.Charging {
			chg = "charging"
		}
		rows = append(rows, " "+sec.Render("Bat  ")+txt.Render(fmt.Sprintf("%d%% (%s)", snap.BatteryPct, chg)))
	} else {
		rows = append(rows, " "+sec.Render("Bat  ")+muted.Render("AC power"))
	}

	maxScroll := max(0, len(rows)-h)
	scroll := min(v.Scroll, maxScroll)
	end := min(len(rows), scroll+h)
	return SplitLinesPadded(rows[scroll:end], w, h)
}
