package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

type WifiView struct {
	Selected   int
	Scroll     int
	Scanning   bool
	Interfaces []wifi.Interface
}

func NewWifiView() WifiView {
	return WifiView{}
}

func (v *WifiView) SetInterfaces(ifaces []wifi.Interface) {
	v.Interfaces = ifaces
	v.Scanning = false
	if len(v.Interfaces) == 0 {
		v.Selected = 0
		v.Scroll = 0
		return
	}
	if v.Selected >= len(v.Interfaces) {
		v.Selected = len(v.Interfaces) - 1
	}
	if v.Selected < 0 {
		v.Selected = 0
	}
}

func (v *WifiView) MoveInterface(delta int) {
	n := len(v.Interfaces)
	if n <= 1 {
		return
	}
	v.Selected = (v.Selected + delta + n) % n
	v.Scroll = 0
}

func (v *WifiView) MoveScroll(delta int, w, h int, pal theme.Palette) {
	if len(v.Interfaces) == 0 {
		v.Scroll = 0
		return
	}
	detailH := max(1, h-1)
	totalRows := len(v.BuildRows(w, pal, v.Interfaces[v.Selected]))
	maxScroll := max(0, totalRows-detailH)
	v.Scroll += delta
	if v.Scroll < 0 {
		v.Scroll = 0
	}
	if v.Scroll > maxScroll {
		v.Scroll = maxScroll
	}
}

func (v WifiView) BuildRows(w int, pal theme.Palette, inf wifi.Interface) []string {
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	valW := max(6, w-8)

	// State line
	var stateView string
	if inf.Up {
		stateView = ok.Render("UP")
		if len(inf.Addrs) > 0 {
			ip := strings.Split(inf.Addrs[0], "/")[0]
			stateView += " " + txt.Render(Truncate(ip, max(4, valW-3)))
		} else if inf.OperState != "" && inf.OperState != "up" {
			stateView += " " + muted.Render("("+Truncate(inf.OperState, max(4, valW-5))+")")
		} else {
			stateView += " " + muted.Render("(no IP)")
		}
	} else {
		stateView = danger.Render("DOWN")
		if inf.OperState != "" && inf.OperState != "down" {
			stateView += " " + muted.Render("("+Truncate(inf.OperState, max(4, valW-7))+")")
		}
	}

	// Mode line
	modeStr := inf.Mode
	if modeStr == "" {
		modeStr = "unknown"
	}
	modeStyled := txt.Render(modeStr)
	if strings.EqualFold(modeStr, "monitor") {
		modeStyled = warn.Render(modeStr)
	}
	if inf.HasMonCap {
		if inf.MonitorCapable {
			modeStyled += " " + ok.Render("(mon: yes)")
		} else {
			modeStyled += " " + muted.Render("(mon: no)")
		}
	}

	// Vendor / Product line
	vendorStr := joinNonEmpty(" ", inf.Manufacturer, inf.Product)
	if inf.Manufacturer != "" && inf.Product != "" && strings.Contains(strings.ToLower(inf.Product), strings.ToLower(inf.Manufacturer)) {
		vendorStr = inf.Product
	}
	vendorView := muted.Render("n/a")
	if vendorStr != "" {
		vendorView = txt.Render(Truncate(vendorStr, valW))
	}

	// Driver / Bus line
	driverView := muted.Render("n/a")
	if inf.Driver != "" && inf.Bus != "" {
		driverView = txt.Render(Truncate(inf.Driver+" ("+inf.Bus+")", valW))
	} else if inf.Driver != "" {
		driverView = txt.Render(Truncate(inf.Driver, valW))
	} else if inf.Bus != "" {
		driverView = txt.Render(Truncate(inf.Bus, valW))
	}

	// MAC line
	macView := muted.Render("n/a")
	if inf.MAC != "" {
		macView = txt.Render(Truncate(inf.MAC, valW))
	}

	rows := []string{
		" " + sec.Render("State  ") + stateView,
		" " + sec.Render("Mode   ") + modeStyled,
		" " + sec.Render("Vendor ") + vendorView,
		" " + sec.Render("Driver ") + driverView,
		" " + sec.Render("MAC    ") + macView,
	}

	if inf.Phy != "" || inf.Bands != "" {
		phyStr := joinNonEmpty(" · ", inf.Phy, inf.Bands)
		rows = append(rows, " "+sec.Render("Phy    ")+txt.Render(Truncate(phyStr, valW)))
	}
	if inf.SSID != "" {
		rows = append(rows, " "+sec.Render("SSID   ")+ok.Render(Truncate(inf.SSID, valW)))
	}
	if inf.Channel != "" {
		rows = append(rows, " "+sec.Render("Chan   ")+txt.Render(Truncate(inf.Channel, valW)))
	}
	if inf.TxPower != "" {
		rows = append(rows, " "+sec.Render("TxPwr  ")+txt.Render(Truncate(inf.TxPower, valW)))
	}
	if len(inf.Addrs) > 0 {
		rows = append(rows, " "+sec.Render("IP     ")+txt.Render(Truncate(strings.Join(inf.Addrs, ", "), valW)))
	}
	if inf.VendorID != "" || inf.ProductID != "" {
		hwID := strings.Trim(inf.VendorID+":"+inf.ProductID, ":")
		rows = append(rows, " "+sec.Render("HW ID  ")+muted.Render(Truncate(hwID, valW)))
	}
	if inf.MTU > 0 {
		rows = append(rows, " "+sec.Render("MTU    ")+muted.Render(strconv.Itoa(inf.MTU)))
	}

	return rows
}

func (v WifiView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	muted := lipgloss.NewStyle().Foreground(pal.Muted)

	if len(v.Interfaces) == 0 {
		var out []string
		if v.Scanning {
			out = []string{" " + muted.Render("Scanning WiFi interfaces...")}
		} else {
			out = []string{
				" " + muted.Render("No WiFi interfaces found"),
				" " + muted.Render("Press r to rescan"),
			}
		}
		return SplitLinesPadded(out, w, h)
	}

	selIdx := clampIndex(v.Selected, len(v.Interfaces))
	names := make([]string, len(v.Interfaces))
	for i, inf := range v.Interfaces {
		names[i] = inf.Name
	}

	header := renderSelectorBar(names, selIdx, w, pal, focused)
	if h <= 1 {
		return SplitLinesPadded([]string{header}, w, h)
	}

	rows := v.BuildRows(w, pal, v.Interfaces[selIdx])
	detailH := h - 1
	maxS := max(0, len(rows)-detailH)
	sc := min(v.Scroll, maxS)
	end := min(len(rows), sc+detailH)
	visible := append([]string(nil), rows[sc:end]...)

	if focused && len(visible) > 0 && maxS > 0 {
		indicator := muted.Render(fmt.Sprintf(" %d-%d/%d", sc+1, end, len(rows)))
		if lipgloss.Width(visible[len(visible)-1])+lipgloss.Width(indicator) <= w {
			visible[len(visible)-1] += indicator
		}
	}

	lines := make([]string, 0, h)
	lines = append(lines, header)
	lines = append(lines, visible...)
	return SplitLinesPadded(lines, w, h)
}

func renderSelectorBar(names []string, selected, w int, pal theme.Palette, focused bool) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	badge := fmt.Sprintf("%d/%d", selected+1, len(names))
	badgeStr := muted.Render(badge)
	badgeW := lipgloss.Width(badgeStr)

	avail := w - 1 - badgeW - 1
	if avail < 8 {
		avail = w - 1
		badgeStr = ""
	}

	renderPill := func(i int) string {
		if i == selected {
			if focused {
				return sel.Render(" " + names[i] + " ")
			}
			return accent.Render("[" + names[i] + "]")
		}
		return muted.Render(names[i])
	}

	// Ensure selected pill is visible within avail width
	start := 0
	for start < selected {
		width := 0
		for i := start; i <= selected; i++ {
			if i > start {
				width++
			}
			width += lipgloss.Width(renderPill(i))
		}
		if width <= avail {
			break
		}
		start++
	}

	var parts []string
	used := 0
	if start > 0 {
		parts = append(parts, muted.Render("‹"))
		used += 2
	}
	for i := start; i < len(names); i++ {
		p := renderPill(i)
		pw := lipgloss.Width(p)
		sep := 0
		if len(parts) > 0 {
			sep = 1
		}
		if used+sep+pw > avail && i > selected {
			break
		}
		parts = append(parts, p)
		used += sep + pw
	}

	left := " " + strings.Join(parts, " ")
	if badgeStr == "" {
		return FitLine(left, w)
	}
	return FitLine(left, w-badgeW-1) + " " + badgeStr
}

func clampIndex(idx, n int) int {
	if n <= 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
