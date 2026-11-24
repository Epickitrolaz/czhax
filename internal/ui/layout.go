package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/czhax/czhax/internal/config"
)

type Tab int

const (
	TabHome Tab = iota
	TabSec
	TabBasic
	TabOptions
)

func (t Tab) Label() string {
	switch t {
	case TabHome:
		return "Home"
	case TabSec:
		return "Sec"
	case TabBasic:
		return "Basic"
	case TabOptions:
		return "Opts"
	default:
		return "Home"
	}
}

func (t Tab) FullLabel() string {
	switch t {
	case TabHome:
		return "Home"
	case TabSec:
		return "SecTools"
	case TabBasic:
		return "Basic"
	case TabOptions:
		return "Options"
	default:
		return "Home"
	}
}

// Viewport computes the inner TUI width and height (and whether a desktop CZ bezel should be drawn).
func ResolveViewport(termW, termH int, mode config.ViewportMode) (w, h int, bezel bool) {
	if termW <= 0 {
		termW = 53
	}
	if termH <= 0 {
		termH = 14
	}

	switch mode {
	case config.ViewportCZ5314:
		if termW >= 57 && termH >= 18 {
			return 53, 14, true
		}
		return min(termW, 53), min(termH, 14), false
	case config.ViewportCZ4510:
		if termW >= 49 && termH >= 14 {
			return 45, 10, true
		}
		return min(termW, 45), min(termH, 10), false
	case config.ViewportWide:
		return max(40, termW), max(10, termH), false
	default: // Auto
		if termW <= 60 || termH <= 18 {
			return max(36, termW), max(10, termH), false
		}
		return termW, termH, false
	}
}

// FitLine ensures s fits strictly in maxW visible cells, truncating with "…" if needed and padding to maxW.
func FitLine(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	if w > maxW {
		if maxW == 1 {
			return ansi.Truncate(s, 1, "")
		}
		s = ansi.Truncate(s, maxW-1, "…")
		w = lipgloss.Width(s)
	}
	if w < maxW {
		s += strings.Repeat(" ", maxW-w)
	}
	return s
}

func Truncate(s string, maxW int) string {
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

func PadLines(lines []string, width, height int, emptyLine string) string {
	if emptyLine == "" {
		emptyLine = strings.Repeat(" ", max(0, width))
	}
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out = append(out, FitLine(lines[i], width))
		} else {
			out = append(out, FitLine(emptyLine, width))
		}
	}
	return strings.Join(out, "\n")
}

func MiniBar(ratio float64, width int, ascii bool) string {
	if width <= 0 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	fillCh, emptyCh := "█", "░"
	if ascii {
		fillCh, emptyCh = "#", "-"
	}
	return strings.Repeat(fillCh, filled) + strings.Repeat(emptyCh, width-filled)
}

func ScrollWindow(total, cursor, visible int) (start, end int) {
	if total <= 0 || visible <= 0 {
		return 0, 0
	}
	if total <= visible {
		return 0, total
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}
	half := visible / 2
	start = cursor - half
	if start < 0 {
		start = 0
	}
	end = start + visible
	if end > total {
		end = total
		start = end - visible
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func ScrollBadge(cursor, total int) string {
	if total <= 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d", cursor+1, total)
}
