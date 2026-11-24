package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/modules"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
)

type HomeView struct {
	Cursor int
	Items  []modules.Item
}

func NewHomeView(pinnedIDs []string) HomeView {
	var items []modules.Item
	for _, id := range pinnedIDs {
		if it, ok := modules.FindByID(id); ok {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		items = modules.All()[:min(4, len(modules.All()))]
	}
	return HomeView{Items: items}
}

func (v *HomeView) RefreshPins(pinnedIDs []string) {
	var items []modules.Item
	for _, id := range pinnedIDs {
		if it, ok := modules.FindByID(id); ok {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		items = modules.All()[:min(4, len(modules.All()))]
	}
	v.Items = items
	if v.Cursor >= len(v.Items) {
		v.Cursor = max(0, len(v.Items)-1)
	}
}

func (v *HomeView) Move(delta int) {
	if len(v.Items) == 0 {
		return
	}
	v.Cursor = (v.Cursor + delta + len(v.Items)) % len(v.Items)
}

func (v HomeView) Selected() (modules.Item, bool) {
	if len(v.Items) == 0 || v.Cursor < 0 || v.Cursor >= len(v.Items) {
		return modules.Item{}, false
	}
	return v.Items[v.Cursor], true
}

func (v HomeView) Render(w, h int, pal theme.Palette, cfg config.Config, snap sysinfo.Snapshot) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	ascii := cfg.Border == config.BorderAscii

	var lines []string

	// Row 1: Identity & Network summary
	host := snap.Hostname
	ip := snap.PrimaryIP
	if ip == "" {
		ip = "offline"
	}
	if cfg.StealthMode {
		host = "stealth"
		ip = "masked"
	}
	hwTag := snap.Arch
	if snap.IsCardZero {
		hwTag = "CM0"
	}
	line1 := fmt.Sprintf("%s %s %s %s",
		accent.Render(Truncate(host, 12)),
		muted.Render("("+hwTag+")"),
		sec.Render(ip),
		muted.Render("up:"+sysinfo.FormatUptime(snap.Uptime)),
	)
	lines = append(lines, line1)

	// Row 2: Compact RAM & CPU/Temp bar
	var memRatio float64
	if snap.MemTotalMB > 0 {
		memRatio = float64(snap.MemUsedMB) / float64(snap.MemTotalMB)
	}
	barW := 8
	if w < 48 {
		barW = 6
	}
	memStr := fmt.Sprintf("MEM %s %d/%dM",
		accent.Render(MiniBar(memRatio, barW, ascii)),
		snap.MemUsedMB, snap.MemTotalMB,
	)
	rightStat := ""
	if snap.HasTemp {
		rightStat = fmt.Sprintf("%.0fC", snap.TempC)
	} else if snap.Load1 != "" {
		rightStat = "L:" + snap.Load1
	}
	if rightStat != "" {
		memStr += " " + muted.Render(rightStat)
	}
	lines = append(lines, memStr)

	// Row 3: Section header for Quick Launch
	if h >= 6 {
		hdr := accent.Render("PINNED") + " " + muted.Render(ScrollBadge(v.Cursor, len(v.Items))) +
			" " + muted.Render("/:find ?:help")
		lines = append(lines, hdr)
	}

	// Remaining rows: Pinned quick launch list
	availRows := h - len(lines)
	if availRows < 1 {
		availRows = 1
	}
	start, end := ScrollWindow(len(v.Items), v.Cursor, availRows)
	for i := start; i < end; i++ {
		it := v.Items[i]
		badge := "[" + it.Tag + "]"
		st := string(it.Status)
		if it.Status == modules.StatusReady {
			st = "RDY"
		}
		if i == v.Cursor {
			prefix := "▸ "
			if ascii {
				prefix = "> "
			}
			// Calculate middle name width
			nameW := max(6, w-len(prefix)-len(badge)-len(st)-3)
			row := fmt.Sprintf("%s%s %s %s", prefix, badge, FitLine(it.Name, nameW), st)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			nameW := max(6, w-2-len(badge)-len(st)-3)
			row := fmt.Sprintf("  %s %s %s",
				sec.Render(badge),
				txt.Render(FitLine(it.Name, nameW)),
				muted.Render(st),
			)
			lines = append(lines, row)
		}
	}

	// If extra space on wide/tall terminals, add a subtle quick-tip footer line
	if len(lines) < h && h >= 10 {
		lines = append(lines, muted.Render(strings.Repeat("─", min(w, 32))))
		if len(lines) < h {
			lines = append(lines, muted.Render("Tip: 1-4 switch tabs · p pins/unpins any tool"))
		}
	}

	return PadLines(lines, w, h, "")
}
