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
	cyan := lipgloss.NewStyle().Bold(true).Foreground(pal.Cyan)
	pur := lipgloss.NewStyle().Bold(true).Foreground(pal.Purple)
	blu := lipgloss.NewStyle().Bold(true).Foreground(pal.Blue)
	mag := lipgloss.NewStyle().Bold(true).Foreground(pal.Magenta)
	org := lipgloss.NewStyle().Bold(true).Foreground(pal.Orange)
	yel := lipgloss.NewStyle().Bold(true).Foreground(pal.Yellow)
	grn := lipgloss.NewStyle().Bold(true).Foreground(pal.Green)
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

	memBar := lipgloss.NewStyle().Bold(true).Foreground(barColor(pal, memRatio)).Render(MiniBar(memRatio, barW, ascii))
	diskBar := lipgloss.NewStyle().Bold(true).Foreground(barColor(pal, diskRatio)).Render(MiniBar(diskRatio, barW, ascii))

	rows := []string{
		" " + cyan.Render("Host ") + txt.Render(host) + " " + pur.Render("("+hw+")"),
		" " + pur.Render("Kern ") + blu.Render(Truncate(snap.Kernel, max(6, w-6))),
		" " + blu.Render("Up   ") + txt.Render(sysinfo.FormatUptime(snap.Uptime)) + "  " + yel.Render("Ld ") + txt.Render(snap.Load1),
		fmt.Sprintf(" %s %s %s", mag.Render("RAM "), memBar, txt.Render(fmt.Sprintf("%d/%dM", snap.MemUsedMB, snap.MemTotalMB))),
		fmt.Sprintf(" %s %s %s", org.Render("Disk"), diskBar, txt.Render(fmt.Sprintf("%.1f/%.1fG", snap.DiskFreeGB, snap.DiskTotGB))),
	}

	if snap.HasTemp {
		tCol := tempColor(pal, snap.TempC)
		rows = append(rows, " "+org.Render("Temp ")+lipgloss.NewStyle().Bold(true).Foreground(tCol).Render(fmt.Sprintf("%.1f°C", snap.TempC)))
	} else {
		rows = append(rows, " "+org.Render("Temp ")+muted.Render("n/a"))
	}

	if snap.HasBattery {
		bCol := pal.Green
		if snap.BatteryPct <= 20 {
			bCol = pal.Red
		} else if snap.BatteryPct <= 50 {
			bCol = pal.Yellow
		}
		chg := "discharging"
		if snap.Charging {
			chg = "charging"
		}
		rows = append(rows, " "+grn.Render("Bat  ")+lipgloss.NewStyle().Bold(true).Foreground(bCol).Render(fmt.Sprintf("%d%% (%s)", snap.BatteryPct, chg)))
	} else {
		rows = append(rows, " "+grn.Render("Bat  ")+blu.Render("AC power"))
	}

	maxScroll := max(0, len(rows)-h)
	scroll := min(v.Scroll, maxScroll)
	end := min(len(rows), scroll+h)
	return SplitLinesPadded(rows[scroll:end], w, h)
}
