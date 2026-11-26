package ui

import (
	"fmt"
	"strings"

	"github.com/czhax/czhax/internal/config"
)

type ViewID int

const (
	ViewStatus ViewID = iota
	ViewWiFi
	ViewIR
	ViewSystem
	viewCount
)

const (
	StatusTabDevice = iota
	StatusTabWiFi
	StatusTabBT
)

const (
	WiFiTabScan = iota
)

// SidebarLabelMax is the widest label a left sidebar entry may render.
// Full names are truncated to this width; compact widths use Short3.
const SidebarLabelMax = 6

type ViewDef struct {
	ID     ViewID
	Name   string
	Short3 string
	Tabs   []string
}

// SidebarLabel returns the name that fits the left sidebar width budget.
func SidebarLabel(name string) string {
	return Truncate(strings.TrimSpace(name), SidebarLabelMax)
}

var TopViews = []ViewDef{
	{ID: ViewStatus, Name: "Status", Short3: "Sts", Tabs: []string{"Device", "WiFi", "BT"}},
	{ID: ViewWiFi, Name: "WiFi", Short3: "WiF", Tabs: []string{"Scan"}},
	{ID: ViewIR, Name: "IR", Short3: "IR", Tabs: []string{"Remote", "Learn", "Browse"}},
	{ID: ViewSystem, Name: "System", Short3: "Sys", Tabs: []string{"Tools"}},
}

// ResolveViewport computes inner TUI width and height (and whether a desktop CZ bezel should be drawn).
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
	case config.ViewportCZ4010:
		if termW >= 44 && termH >= 14 {
			return 40, 10, true
		}
		return min(termW, 40), min(termH, 10), false
	case config.ViewportWide:
		return max(36, termW), max(10, termH), false
	default: // Auto
		if termW <= 60 || termH <= 18 {
			return max(34, termW), max(9, termH), false
		}
		return termW, termH, false
	}
}

func ResolveSidebarWidth(totalW int, mode config.SidebarMode) int {
	switch mode {
	case config.SidebarSlim:
		return 5
	case config.SidebarWide:
		return 8
	default:
		if totalW < 50 {
			return 5
		}
		return 8
	}
}

// FitLine ensures s fits strictly in maxW visible cells, truncating with "…" if needed and padding to maxW.
func FitLine(s string, maxW int) string {
	return (Marquee{}).FitLine(s, maxW)
}

func Truncate(s string, maxW int) string {
	return truncateStatic(s, maxW)
}

func SplitLinesPadded(lines []string, width, height int) []string {
	return (Marquee{}).SplitLinesPadded(lines, width, height)
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
