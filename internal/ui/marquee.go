package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	marqueeDelay      = 2 * time.Second
	marqueeInterval   = 200 * time.Millisecond
	marqueeGap        = "   "
	marqueePauseTicks = 10 // 2.0s pause at start of each loop
)

// Marquee controls automatic horizontal scrolling for text that overflows its
// allocated display width after the user remains idle for marqueeDelay.
type Marquee struct {
	Step     int
	Overflow *bool
}

// Truncate keeps s within maxW visible cells. When Step > 0 and s overflows
// maxW, it scrolls horizontally across s + "   " + s, pausing at the start of
// each loop.
func (m Marquee) Truncate(s string, maxW int) string {
	return m.slice(s, maxW, marqueeGap, false)
}

// FitLine returns s scrolled (if overflowing and Step > 0) or truncated (if
// overflowing and Step <= 0), padded with spaces to exact maxW visible cells.
func (m Marquee) FitLine(s string, maxW int) string {
	return m.slice(s, maxW, marqueeGap, true)
}

// FitLineWithGap behaves like FitLine while using a custom gap string (for
// example styled spaces that preserve a footer background color).
func (m Marquee) FitLineWithGap(s string, maxW int, gap string) string {
	return m.slice(s, maxW, gap, true)
}

// SplitLinesPadded fits each line to width (scrolling any line that still
// overflows when Step > 0) and pads or slices to exact height.
func (m Marquee) SplitLinesPadded(lines []string, width, height int) []string {
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out = append(out, m.FitLine(lines[i], width))
		} else {
			out = append(out, strings.Repeat(" ", max(0, width)))
		}
	}
	return out
}

func (m Marquee) slice(s string, maxW int, gap string, pad bool) string {
	if maxW <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	if w <= maxW {
		if pad && w < maxW {
			return s + strings.Repeat(" ", maxW-w)
		}
		return s
	}
	if m.Overflow != nil {
		*m.Overflow = true
	}
	var out string
	if m.Step <= 0 {
		out = truncateStatic(s, maxW)
	} else {
		gapW := lipgloss.Width(gap)
		if gapW <= 0 {
			gap = marqueeGap
			gapW = 3
		}
		scrollSpan := w + gapW
		cycle := scrollSpan + marqueePauseTicks
		t := (m.Step - 1) % cycle
		pos := t + 1
		if pos >= scrollSpan {
			out = truncateStatic(s, maxW)
		} else {
			out = ansi.Cut(s+gap+s, pos, pos+maxW)
		}
	}
	if pad {
		if ow := lipgloss.Width(out); ow < maxW {
			out += strings.Repeat(" ", maxW-ow)
		}
	}
	return out
}

func truncateStatic(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW == 1 {
		return ansi.Truncate(s, 1, "")
	}
	return ansi.Truncate(s, maxW-1, "…")
}
