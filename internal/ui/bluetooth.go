package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/theme"
)

type BluetoothView struct {
	Selected int
	Scroll   int
	Scanning bool
	Adapters []bluetooth.Adapter
}

func NewBluetoothView() BluetoothView {
	return BluetoothView{}
}

func (v *BluetoothView) SetAdapters(adapters []bluetooth.Adapter) {
	v.Adapters = adapters
	v.Scanning = false
	if len(v.Adapters) == 0 {
		v.Selected = 0
		v.Scroll = 0
		return
	}
	if v.Selected >= len(v.Adapters) {
		v.Selected = len(v.Adapters) - 1
	}
	if v.Selected < 0 {
		v.Selected = 0
	}
}

func (v *BluetoothView) MoveAdapter(delta int) {
	n := len(v.Adapters)
	if n <= 1 {
		return
	}
	v.Selected = (v.Selected + delta + n) % n
	v.Scroll = 0
}

func (v *BluetoothView) MoveScroll(delta int, w, h int, pal theme.Palette) {
	if len(v.Adapters) == 0 {
		v.Scroll = 0
		return
	}
	detailH := max(1, h-1)
	totalRows := len(v.BuildRows(w, pal, v.Adapters[v.Selected]))
	maxScroll := max(0, totalRows-detailH)
	v.Scroll += delta
	if v.Scroll < 0 {
		v.Scroll = 0
	}
	if v.Scroll > maxScroll {
		v.Scroll = maxScroll
	}
}

func (v BluetoothView) BuildRows(w int, pal theme.Palette, ad bluetooth.Adapter) []string {
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	valW := max(6, w-8)

	// State line
	stateView := muted.Render("unknown")
	switch {
	case ad.RFKill == "hard-blocked" || ad.RFKill == "soft-blocked":
		stateView = danger.Render("BLOCKED") + " " + muted.Render("("+Truncate(ad.RFKill, max(4, valW-10))+")")
	case ad.StateKnown && (ad.Up || ad.Powered):
		stateView = ok.Render("UP") + " " + txt.Render("powered")
	case ad.StateKnown:
		stateView = danger.Render("DOWN") + " " + muted.Render("(off)")
	}

	// Vendor / Product line
	vendorStr := joinNonEmpty(" ", ad.Manufacturer, ad.Product)
	if ad.Manufacturer != "" && ad.Product != "" && strings.Contains(strings.ToLower(ad.Product), strings.ToLower(ad.Manufacturer)) {
		vendorStr = ad.Product
	}
	vendorView := muted.Render("n/a")
	if vendorStr != "" {
		vendorView = txt.Render(Truncate(vendorStr, valW))
	}

	// Driver / Bus line
	driverView := muted.Render("n/a")
	if ad.Driver != "" && ad.Bus != "" {
		driverView = txt.Render(Truncate(ad.Driver+" ("+ad.Bus+")", valW))
	} else if ad.Driver != "" {
		driverView = txt.Render(Truncate(ad.Driver, valW))
	} else if ad.Bus != "" {
		driverView = txt.Render(Truncate(ad.Bus, valW))
	}

	// BD_ADDR line
	addrView := muted.Render("n/a")
	if ad.Address != "" {
		addrView = txt.Render(Truncate(ad.Address, valW))
	}

	var rows []string
	rows = append(rows, " "+sec.Render("State  ")+stateView)
	if ad.Alias != "" {
		rows = append(rows, " "+sec.Render("Alias  ")+txt.Render(Truncate(ad.Alias, valW)))
	}
	rows = append(rows,
		" "+sec.Render("Vendor ")+vendorView,
		" "+sec.Render("Driver ")+driverView,
		" "+sec.Render("Addr   ")+addrView,
	)

	if ad.Mode != "" || ad.HCIVersion != "" {
		hciPart := ""
		if ad.HCIVersion != "" {
			hciPart = "HCI " + ad.HCIVersion
		}
		modeStr := joinNonEmpty(" · ", ad.Mode, hciPart)
		rows = append(rows, " "+sec.Render("Mode   ")+txt.Render(Truncate(modeStr, valW)))
	}

	if ad.FlagsKnown {
		var flags []string
		if ad.Discoverable {
			flags = append(flags, "discov")
		}
		if ad.Pairable {
			flags = append(flags, "pairable")
		}
		flagView := muted.Render("non-discov")
		if len(flags) > 0 {
			flagStyle := txt
			if ad.Discoverable {
				flagStyle = warn
			}
			flagView = flagStyle.Render(Truncate(strings.Join(flags, " · "), valW))
		}
		rows = append(rows, " "+sec.Render("Flags  ")+flagView)
	}

	if ad.RFKill != "" {
		rfStyle := ok
		if ad.RFKill != "unblocked" {
			rfStyle = danger
		}
		rows = append(rows, " "+sec.Render("RFKill ")+rfStyle.Render(Truncate(ad.RFKill, valW)))
	}

	if ad.Type != "" {
		rows = append(rows, " "+sec.Render("Type   ")+muted.Render(Truncate(ad.Type, valW)))
	}

	if ad.VendorID != "" || ad.ProductID != "" {
		hwID := strings.Trim(ad.VendorID+":"+ad.ProductID, ":")
		rows = append(rows, " "+sec.Render("HW ID  ")+muted.Render(Truncate(hwID, valW)))
	}

	if ad.ScanMode != "" {
		rows = append(rows, " "+sec.Render("Scan   ")+muted.Render(Truncate(ad.ScanMode, valW)))
	}

	return rows
}

func (v BluetoothView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	muted := lipgloss.NewStyle().Foreground(pal.Muted)

	if len(v.Adapters) == 0 {
		var out []string
		if v.Scanning {
			out = []string{" " + muted.Render("Scanning Bluetooth adapters...")}
		} else {
			out = []string{
				" " + muted.Render("No Bluetooth adapters found"),
				" " + muted.Render("Press r to rescan"),
			}
		}
		return SplitLinesPadded(out, w, h)
	}

	selIdx := clampIndex(v.Selected, len(v.Adapters))
	names := make([]string, len(v.Adapters))
	for i, ad := range v.Adapters {
		names[i] = ad.Name
	}

	header := renderSelectorBar(names, selIdx, w, pal, focused)
	if h <= 1 {
		return SplitLinesPadded([]string{header}, w, h)
	}

	rows := v.BuildRows(w, pal, v.Adapters[selIdx])
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
