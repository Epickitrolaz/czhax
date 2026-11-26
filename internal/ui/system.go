package ui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/boot"
	"github.com/czhax/czhax/internal/theme"
)

type SystemOption int

const (
	SysOptUpdate SystemOption = iota
	SysOptRestart
	SysOptTerminal
	SysOptBoot
	SysOptSleep
	SysOptReboot
	SysOptShutdown
	sysOptCount
)

type SystemView struct {
	Cursor    int
	Updating  bool
	Confirm   string // "", "reboot", or "shutdown"
	Note      string
	BootState boot.State
	Marquee   Marquee
}

func NewSystemView() SystemView {
	return SystemView{
		BootState: boot.DetectState(""),
	}
}

func (v *SystemView) Move(delta int) {
	v.Cursor = (v.Cursor + delta + int(sysOptCount)) % int(sysOptCount)
	v.Confirm = ""
}

func (v *SystemView) RefreshBootState() {
	v.BootState = boot.DetectState("")
}

func shellName() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return "sh"
	}
	return filepath.Base(sh)
}

func (v SystemView) RenderLines(w, h int, pal theme.Palette, version string, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	verLbl := version
	if v.Updating {
		verLbl = "updating..."
	}

	bootLbl := "[OFF]"
	if v.BootState.Enabled {
		bootLbl = "[ON]"
	}

	type sysRow struct {
		opt    SystemOption
		isSep  bool
		name   string
		val    string
		ok     bool
		warn   bool
		danger bool
	}

	rows := []sysRow{
		{opt: SysOptUpdate, name: "Update CZHax", val: verLbl, warn: v.Updating},
		{opt: SysOptRestart, name: "Restart CZHax", val: "relaunch"},
		{opt: SysOptTerminal, name: "Terminal Shell", val: shellName()},
		{opt: SysOptBoot, name: "Start on Boot", val: bootLbl, ok: v.BootState.Enabled},
		{isSep: true},
		{opt: SysOptSleep, name: "Sleep Mode", val: "low pwr"},
		{opt: SysOptReboot, name: "Reboot", val: "restart", warn: v.Confirm == "reboot"},
		{opt: SysOptShutdown, name: "Shutdown", val: "poweroff", danger: true},
	}

	// Map option index -> display row index so ScrollWindow tracks across the separator cleanly.
	cursorRow := 0
	for i, r := range rows {
		if !r.isSep && int(r.opt) == v.Cursor {
			cursorRow = i
			break
		}
	}

	listH := h
	showBottom := h >= len(rows)+2
	if showBottom {
		listH = h - 1
	}

	start, end := ScrollWindow(len(rows), cursorRow, listH)
	var out []string
	for i := start; i < end; i++ {
		it := rows[i]
		if it.isSep {
			out = append(out, "")
			continue
		}
		selected := int(it.opt) == v.Cursor
		if selected && focused {
			nameW := max(8, w-len(it.val)-5)
			row := fmt.Sprintf(" ▸ %s %s", v.Marquee.FitLine(it.name, nameW), it.val)
			out = append(out, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if selected {
				pfx = accent.Render(" ▸ ")
			}
			valStyle := muted
			if it.ok {
				valStyle = ok
			} else if it.danger {
				valStyle = danger
			} else if it.warn {
				valStyle = warn
			}
			nameW := max(8, w-len(it.val)-5)
			out = append(out, fmt.Sprintf("%s%s %s", pfx, txt.Render(v.Marquee.FitLine(it.name, nameW)), valStyle.Render(it.val)))
		}
	}

	if showBottom {
		for len(out) < h-1 {
			out = append(out, "")
		}
		if v.Confirm != "" {
			out = append(out, " "+danger.Render(v.Marquee.Truncate("Confirm "+v.Confirm+"? [y/Enter] Yes [n/Esc] No", max(6, w-1))))
		} else if v.Note != "" {
			out = append(out, " "+warn.Render(v.Marquee.Truncate(v.Note, max(6, w-1))))
		} else {
			var hint string
			switch SystemOption(v.Cursor) {
			case SysOptUpdate:
				hint = "⏎ check & install latest release"
			case SysOptRestart:
				hint = "⏎ relaunch czhax (loads updates)"
			case SysOptTerminal:
				hint = "⏎ open shell (exit/^D returns)"
			case SysOptBoot:
				hint = fmt.Sprintf("⏎ toggle boot (%s)", v.BootState.Method)
			case SysOptSleep:
				hint = "⏎ screen/wifi off; Esc wakes"
			case SysOptReboot:
				hint = "⏎ reboot unit (asks confirm)"
			case SysOptShutdown:
				hint = "⏎ power off unit (asks confirm)"
			}
			out = append(out, " "+muted.Render(v.Marquee.Truncate(hint, max(6, w-1))))
		}
	}

	return v.Marquee.SplitLinesPadded(out, w, h)
}
