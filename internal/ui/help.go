package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/theme"
)

type HelpView struct {
	Scroll  int
	Marquee Marquee
}

func (v *HelpView) Move(delta int) {
	v.Scroll += delta
	if v.Scroll < 0 {
		v.Scroll = 0
	}
}

func (v HelpView) RenderLines(w, h int, pal theme.Palette) []string {
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	descW := max(6, w-11)

	rows := []string{
		" " + sec.Render("↑↓/Fn+F,X ") + txt.Render(v.Marquee.Truncate("Vertical tabs / scroll", descW)),
		" " + sec.Render("←→/Fn+Z,C ") + txt.Render(v.Marquee.Truncate("Horizontal tabs", descW)),
		" " + sec.Render("Enter     ") + txt.Render(v.Marquee.Truncate("Enter tab content", descW)),
		" " + sec.Render("Esc       ") + txt.Render(v.Marquee.Truncate("Return to tabs", descW)),
		" " + sec.Render("r         ") + txt.Render(v.Marquee.Truncate("Refresh / rescan", descW)),
		" " + sec.Render("s / ⏎     ") + txt.Render(v.Marquee.Truncate("WiFi scan / AP actions", descW)),
		" " + sec.Render("pcap      ") + txt.Render(v.Marquee.Truncate("Saved ~/czhax/captures", descW)),
		" " + sec.Render("BT Scan   ") + txt.Render(v.Marquee.Truncate("s:scan m:mode ⏎:actions", descW)),
		" " + sec.Render("BT Emu    ") + txt.Render(v.Marquee.Truncate("emulate HID/speaker/custom", descW)),
		" " + sec.Render("Files     ") + txt.Render(v.Marquee.Truncate("⏎:open n:file m:dir a:rec x:del r:ren", descW)),
		" " + sec.Render("IR Remote ") + txt.Render(v.Marquee.Truncate("keys:send ←→:rem Tab:list =:remap", descW)),
		" " + sec.Render("IR Learn  ") + txt.Render(v.Marquee.Truncate("⏎:listen/remote/bind key", descW)),
		" " + sec.Render("IR Browse ") + txt.Render(v.Marquee.Truncate("m:match u:univ /:find [/]:brand", descW)),
		" " + sec.Render("SDR Radio ") + txt.Render(v.Marquee.Truncate("←→:tune gain/sq live p:presets ⏎:go", descW)),
		" " + sec.Render("SDR Spec  ") + txt.Render(v.Marquee.Truncate("start/end z/x:zoom c:cont m:top50", descW)),
		" " + sec.Render("SDR 433   ") + txt.Render(v.Marquee.Truncate("rtl_433 ISM decoder + JSONL log", descW)),
		" " + sec.Render("SDR Srv   ") + txt.Render(v.Marquee.Truncate("rtl_tcp IQ server for LAN clients", descW)),
		" " + sec.Render("WiFi List ") + txt.Render(v.Marquee.Truncate("o:color ⏎:actions (WPS)", descW)),
		" " + sec.Render("Local     ") + txt.Render(v.Marquee.Truncate("pick if → nmap scan → hosts", descW)),
		" " + sec.Render("?         ") + txt.Render(v.Marquee.Truncate("Toggle help", descW)),
		" " + sec.Render("q / ^C    ") + txt.Render(v.Marquee.Truncate("Quit CZHax", descW)),
	}

	maxScroll := max(0, len(rows)-h)
	scroll := min(v.Scroll, maxScroll)
	end := min(len(rows), scroll+h)
	return v.Marquee.SplitLinesPadded(rows[scroll:end], w, h)
}
