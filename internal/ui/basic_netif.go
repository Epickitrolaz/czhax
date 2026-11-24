package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
)

type NetIfView struct {
	Cursor int
}

func (v *NetIfView) Move(delta int, count int) {
	if count <= 0 {
		return
	}
	v.Cursor = (v.Cursor + delta + count) % count
}

func (v NetIfView) RenderLines(w, h int, pal theme.Palette, cfg config.Config, snap sysinfo.Snapshot, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	ifaces := snap.Interfaces
	if len(ifaces) == 0 {
		return SplitLinesPadded([]string{" " + muted.Render("No interfaces found")}, w, h)
	}

	cursor := v.Cursor
	if cursor >= len(ifaces) {
		cursor = len(ifaces) - 1
	}

	var lines []string
	listH := max(1, h-2)
	start, end := ScrollWindow(len(ifaces), cursor, listH)
	for i := start; i < end; i++ {
		inf := ifaces[i]
		state := "DN"
		if inf.Up {
			state = "UP"
		}
		addr := "-"
		if len(inf.Addrs) > 0 {
			addr = strings.Split(inf.Addrs[0], "/")[0]
			if cfg.StealthMode {
				addr = "[masked]"
			}
		}
		if i == cursor && focused {
			row := fmt.Sprintf(" ▸ %-6s %s %s", Truncate(inf.Name, 6), state, addr)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "  "
			if i == cursor {
				pfx = "▸ "
			}
			st := muted.Render(state)
			if inf.Up {
				st = ok.Render(state)
			}
			row := fmt.Sprintf("%s%-6s %s %s", accent.Render(pfx), txt.Render(Truncate(inf.Name, 6)), st, txt.Render(addr))
			lines = append(lines, row)
		}
	}

	for len(lines) < h-2 {
		lines = append(lines, "")
	}

	cur := ifaces[cursor]
	mac := cur.MAC
	if mac == "" {
		mac = "none"
	}
	if cfg.StealthMode && mac != "none" {
		mac = "xx:xx:xx:xx:xx:xx"
	}
	if h >= 3 {
		lines = append(lines, " "+muted.Render(fmt.Sprintf("MAC %s  MTU %d", mac, cur.MTU)))
	}
	if h >= 4 {
		if len(cur.Addrs) > 0 && !cfg.StealthMode {
			lines = append(lines, " "+muted.Render("IP  "+strings.Join(cur.Addrs, ", ")))
		} else {
			lines = append(lines, " "+muted.Render("r:refresh  ← sidebar"))
		}
	}

	return SplitLinesPadded(lines, w, h)
}
