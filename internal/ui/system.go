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

	items := []struct {
		name   string
		val    string
		ok     bool
		warn   bool
		danger bool
	}{
		{name: "Update CZHax", val: verLbl, warn: v.Updating},
		{name: "Terminal Shell", val: shellName()},
		{name: "Start on Boot", val: bootLbl, ok: v.BootState.Enabled},
		{name: "Sleep Mode", val: "low pwr"},
		{name: "Reboot", val: "restart", warn: v.Confirm == "reboot"},
		{name: "Shutdown", val: "poweroff", danger: true},
	}

	listH := h
	showBottom := h >= len(items)+2
	if showBottom {
		listH = h - 1
	}

	start, end := ScrollWindow(len(items), v.Cursor, listH)
	var out []string
	for i := start; i < end; i++ {
		it := items[i]
		if i == v.Cursor && focused {
			nameW := max(8, w-len(it.val)-5)
			row := fmt.Sprintf(" ▸ %s %s", FitLine(it.name, nameW), it.val)
			out = append(out, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if i == v.Cursor {
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
			out = append(out, fmt.Sprintf("%s%s %s", pfx, txt.Render(FitLine(it.name, nameW)), valStyle.Render(it.val)))
		}
	}

	if showBottom {
		for len(out) < h-1 {
			out = append(out, "")
		}
		if v.Confirm != "" {
			out = append(out, " "+danger.Render(Truncate("Confirm "+v.Confirm+"? [y/Enter] Yes [n/Esc] No", max(6, w-1))))
		} else if v.Note != "" {
			out = append(out, " "+warn.Render(Truncate(v.Note, max(6, w-1))))
		} else {
			var hint string
			switch SystemOption(v.Cursor) {
			case SysOptUpdate:
				hint = "⏎ check & install latest release"
			case SysOptTerminal:
				hint = "⏎ open shell (exit/^D returns)"
			case SysOptBoot:
				hint = fmt.Sprintf("⏎ toggle boot (%s)", v.BootState.Method)
			case SysOptSleep:
				hint = "⏎ screen/wifi off; side btn wakes"
			case SysOptReboot:
				hint = "⏎ reboot unit (asks confirm)"
			case SysOptShutdown:
				hint = "⏎ power off unit (asks confirm)"
			}
			out = append(out, " "+muted.Render(Truncate(hint, max(6, w-1))))
		}
	}

	return SplitLinesPadded(out, w, h)
}
