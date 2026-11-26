package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/hwctl"
	"github.com/czhax/czhax/internal/theme"
)

const osdDisplayDuration = 1600 * time.Millisecond

type OSDKind int

const (
	OSDBrightness OSDKind = iota
	OSDVolume
)

type OSDState struct {
	Visible  bool
	Kind     OSDKind
	Level    int
	MaxLevel int
	Percent  int
	Muted    bool
	Seq      int
}

func (o *OSDState) ShowBrightness(st hwctl.BacklightState) {
	o.Seq++
	o.Visible = true
	o.Kind = OSDBrightness
	o.Level = st.Level
	o.MaxLevel = hwctl.BrightnessMaxLevel
	o.Percent = st.Percent
	o.Muted = false
}

func (o *OSDState) ShowVolume(st hwctl.VolumeState) {
	o.Seq++
	o.Visible = true
	o.Kind = OSDVolume
	o.Level = st.Level
	o.MaxLevel = hwctl.VolumeMaxLevel
	o.Percent = st.Percent
	o.Muted = st.Muted
}

func (o *OSDState) Hide(seq int) {
	if seq == 0 || seq == o.Seq {
		o.Visible = false
	}
}

// Overlay composites the brightness/volume popup centered over screen while
// keeping exact line count (h) and maximum visible width (w).
func (o OSDState) Overlay(screen string, w, h int, pal theme.Palette, cfg config.Config) string {
	if !o.Visible || w < 20 || h < 5 {
		return screen
	}

	boxW := min(max(24, w-6), 30)
	if boxW > w-2 {
		boxW = w - 2
	}
	if boxW < 18 {
		return screen
	}
	innerW := boxW - 4 // border + 1 space pad each side
	ascii := cfg.Border == config.BorderAscii

	tl, tr, bl, br, hz, vt := "╭", "╮", "╰", "╯", "─", "│"
	if ascii {
		tl, tr, bl, br, hz, vt = "+", "+", "+", "+", "-", "|"
	}

	bg := pal.HeaderBg
	bdrSty := lipgloss.NewStyle().Foreground(pal.Accent).Background(bg)
	padSty := lipgloss.NewStyle().Background(bg)
	titleSty := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent).Background(bg)
	secSty := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary).Background(bg)
	mutedSty := lipgloss.NewStyle().Foreground(pal.Muted).Background(bg)
	dangerSty := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger).Background(bg)

	var titlePlain, titleStyled string
	if o.Kind == OSDBrightness {
		titlePlain = "Brightness"
		titleStyled = titleSty.Render(titlePlain)
	} else {
		titlePlain = "Volume"
		titleStyled = titleSty.Render(titlePlain)
		if o.Muted {
			titlePlain += " MUTED"
			titleStyled += padSty.Render(" ") + dangerSty.Render("MUTED")
		}
	}

	maxLvl := o.MaxLevel
	if maxLvl <= 0 {
		if o.Kind == OSDBrightness {
			maxLvl = hwctl.BrightnessMaxLevel
		} else {
			maxLvl = hwctl.VolumeMaxLevel
		}
	}
	lvlStr := fmt.Sprintf("%d/%d", o.Level, maxLvl)
	lvlSty := secSty
	if o.Kind == OSDVolume && o.Muted {
		lvlSty = mutedSty
	}

	gap1 := innerW - lipgloss.Width(titlePlain) - len(lvlStr)
	if gap1 < 1 {
		gap1 = 1
	}
	row1Inner := titleStyled + padSty.Render(strings.Repeat(" ", gap1)) + lvlSty.Render(lvlStr)
	row1Inner = FitLineWithBg(row1Inner, innerW, bg)

	var rightVal string
	var rightSty lipgloss.Style
	if o.Kind == OSDVolume && o.Muted {
		rightVal = "MUTE"
		rightSty = dangerSty
	} else {
		rightVal = fmt.Sprintf("%3d%%", o.Percent)
		rightSty = secSty
	}

	barW := min(20, max(6, innerW-len(rightVal)-1))
	ratio := 0.0
	if maxLvl > 0 && !(o.Kind == OSDVolume && o.Muted) {
		ratio = float64(o.Level) / float64(maxLvl)
	}
	barColor := pal.Warning
	if o.Kind == OSDVolume {
		if o.Muted {
			barColor = pal.Muted
		} else if o.Percent >= 85 {
			barColor = pal.Warning
		} else {
			barColor = pal.Success
		}
	}
	barSty := lipgloss.NewStyle().Bold(true).Foreground(barColor).Background(bg)
	barStr := barSty.Render(MiniBar(ratio, barW, ascii))
	gap2 := innerW - barW - len(rightVal)
	if gap2 < 1 {
		gap2 = 1
	}
	row2Inner := barStr + padSty.Render(strings.Repeat(" ", gap2)) + rightSty.Render(rightVal)
	row2Inner = FitLineWithBg(row2Inner, innerW, bg)

	boxLines := []string{
		bdrSty.Render(tl + strings.Repeat(hz, boxW-2) + tr),
		bdrSty.Render(vt) + padSty.Render(" ") + row1Inner + padSty.Render(" ") + bdrSty.Render(vt),
		bdrSty.Render(vt) + padSty.Render(" ") + row2Inner + padSty.Render(" ") + bdrSty.Render(vt),
		bdrSty.Render(bl + strings.Repeat(hz, boxW-2) + br),
	}

	lines := strings.Split(screen, "\n")
	if len(lines) < h {
		for len(lines) < h {
			lines = append(lines, strings.Repeat(" ", w))
		}
	}

	boxX := (w - boxW) / 2
	boxY := (h - len(boxLines)) / 2
	if boxY < 0 {
		boxY = 0
	}

	for i, bLine := range boxLines {
		row := boxY + i
		if row >= len(lines) || row >= h {
			break
		}
		bgLine := lines[row]
		left := ansi.Cut(bgLine, 0, boxX)
		if lw := lipgloss.Width(left); lw < boxX {
			left += strings.Repeat(" ", boxX-lw)
		}
		right := ansi.Cut(bgLine, boxX+boxW, w)
		lines[row] = Truncate(left+"\x1b[0m"+bLine+"\x1b[0m"+right, w)
	}

	return strings.Join(lines[:h], "\n")
}

func FitLineWithBg(s string, maxW int, bg lipgloss.Color) string {
	w := lipgloss.Width(s)
	if w > maxW {
		return ansi.Truncate(s, maxW, "")
	}
	if w < maxW {
		return s + lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", maxW-w))
	}
	return s
}
