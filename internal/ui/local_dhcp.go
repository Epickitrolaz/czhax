package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/localscan"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

// LocalDHCPMode is the DHCP starvation tab's screen state.
type LocalDHCPMode int

const (
	dhcpModeSetup LocalDHCPMode = iota
	dhcpModeRunning
)

type localDHCPField int

const (
	dhcpFieldIface localDHCPField = iota
	dhcpFieldRate
	dhcpFieldCount
	dhcpFieldRandomMAC
	dhcpFieldStart
)

// LocalDHCPView drives DHCP pool starvation from the Local view: fresh spoofed
// client MACs, a configurable request rate, and live offer accounting.
type LocalDHCPView struct {
	Mode       LocalDHCPMode
	Interfaces []localscan.Interface
	IfaceIdx   int

	Rate      int  // DISCOVERs per second
	Count     int  // requests per probe run
	Probe     bool // stop after Count instead of running until stopped
	RandomMAC bool

	Cursor int

	Active  bool
	Seq     int
	Cancel  context.CancelFunc
	Lines   []string
	Ch      chan string
	Stats   string
	Err     string
	Started time.Time
	Elapsed time.Duration

	Note      string
	NoteIsErr bool
	Marquee   Marquee
}

func NewLocalDHCPView() LocalDHCPView {
	return LocalDHCPView{
		Rate:      25,
		Count:     500,
		Probe:     true,
		RandomMAC: true,
	}
}

// SyncInterfaces keeps a connected interface selected, preferring wired and
// tethered links (raw DHCP frames are rejected by some WiFi client drivers).
func (v *LocalDHCPView) SyncInterfaces(ifaces []localscan.Interface) {
	prev := v.EffectiveIface()
	v.Interfaces = ifaces
	if len(ifaces) == 0 {
		v.IfaceIdx = 0
		return
	}
	if prev != "" {
		for i, inf := range ifaces {
			if inf.Name == prev {
				v.IfaceIdx = i
				return
			}
		}
	}
	for i, inf := range ifaces {
		if inf.Connected && inf.Kind != localscan.KindWiFi {
			v.IfaceIdx = i
			return
		}
	}
	for i, inf := range ifaces {
		if inf.Connected {
			v.IfaceIdx = i
			return
		}
	}
	v.IfaceIdx = clampIndex(v.IfaceIdx, len(ifaces))
}

func (v LocalDHCPView) EffectiveIface() string {
	if len(v.Interfaces) == 0 {
		return ""
	}
	return v.Interfaces[clampIndex(v.IfaceIdx, len(v.Interfaces))].Name
}

func (v LocalDHCPView) ifaceLabel() string {
	if len(v.Interfaces) == 0 {
		return "none"
	}
	inf := v.Interfaces[clampIndex(v.IfaceIdx, len(v.Interfaces))]
	label := inf.Name
	if inf.Kind != "" {
		label += " · " + string(inf.Kind)
	}
	if !inf.Connected {
		label += " (down)"
	}
	return label
}

func (v LocalDHCPView) fields() []localDHCPField {
	out := []localDHCPField{dhcpFieldIface, dhcpFieldRate}
	if v.Probe {
		out = append(out, dhcpFieldCount)
	}
	out = append(out, dhcpFieldRandomMAC, dhcpFieldStart)
	return out
}

func (v *LocalDHCPView) MoveCursor(delta int) {
	f := v.fields()
	v.Cursor = (v.Cursor + delta + len(f)) % len(f)
}

func (v *LocalDHCPView) Adjust(delta int) {
	f := v.fields()
	switch f[clampIndex(v.Cursor, len(f))] {
	case dhcpFieldIface:
		if len(v.Interfaces) > 0 {
			v.IfaceIdx = (v.IfaceIdx + delta + len(v.Interfaces)) % len(v.Interfaces)
		}
	case dhcpFieldRate:
		v.Rate = clampInt(v.Rate+delta*5, 1, 250)
	case dhcpFieldCount:
		v.Count = clampInt(v.Count+delta*100, 10, 20000)
	case dhcpFieldRandomMAC:
		v.RandomMAC = !v.RandomMAC
	}
}

// ToggleMode switches between a bounded probe and a continuous flood.
func (v *LocalDHCPView) ToggleMode() {
	v.Probe = !v.Probe
	v.Cursor = clampIndex(v.Cursor, len(v.fields()))
}

func (v LocalDHCPView) Config() wifi.DHCPStarvationConfig {
	cfg := wifi.DHCPStarvationConfig{
		Iface:     v.EffectiveIface(),
		Rate:      v.Rate,
		RandomMAC: v.RandomMAC,
	}
	if v.Probe {
		cfg.Count = v.Count
	}
	return cfg
}

// Start launches a run and returns its context/sequence plus the line channel.
func (v *LocalDHCPView) Start() (context.Context, int, wifi.DHCPStarvationConfig, chan string, error) {
	cfg := v.Config()
	if cfg.Iface == "" {
		v.Err = "no interface - connect one first"
		return nil, 0, cfg, nil, fmt.Errorf("no interface")
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.Seq++
	v.Active = true
	v.Cancel = cancel
	v.Err = ""
	v.Lines = nil
	v.Stats = ""
	v.Started = time.Now()
	v.Elapsed = 0
	v.Ch = make(chan string, 64)
	v.Mode = dhcpModeRunning
	return ctx, v.Seq, cfg, v.Ch, nil
}

func (v *LocalDHCPView) Stop() {
	if v.Cancel != nil {
		v.Cancel()
		v.Cancel = nil
	}
}

func (v *LocalDHCPView) AppendLine(seq int, line string) {
	if seq != v.Seq {
		return
	}
	if line = sanitizeLogLine(line); line == "" {
		return
	}
	if strings.HasPrefix(line, "sent=") || strings.HasPrefix(line, "finished") || strings.HasPrefix(line, "stopped") {
		v.Stats = line
	}
	v.Lines = append(v.Lines, line)
	if len(v.Lines) > 200 {
		v.Lines = v.Lines[len(v.Lines)-200:]
	}
}

func (v *LocalDHCPView) Finish(seq int, err error) {
	if seq != v.Seq {
		return
	}
	v.Active = false
	v.Cancel = nil
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		v.Err = err.Error()
	} else {
		v.Mode = dhcpModeSetup
	}
}

func (v LocalDHCPView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)

	if v.Mode == dhcpModeRunning {
		kind := "Probe"
		if !v.Probe {
			kind = "Flood"
		}
		head := fmt.Sprintf("● %s %s %d/s %s", kind, v.EffectiveIface(), v.Rate, formatClock(int(v.Elapsed.Seconds())))
		lines := []string{
			ok.Render(v.Marquee.FitLine(head, w)),
			sec.Render(v.Marquee.FitLine(v.Stats, w)),
		}
		if v.Err != "" {
			lines = append(lines, bad.Render(v.Marquee.FitLine(v.Err, w)))
		}
		body := max(1, h-len(lines))
		tail := v.Lines
		if len(tail) > body {
			tail = tail[len(tail)-body:]
		}
		for _, l := range tail {
			lines = append(lines, mut.Render(v.Marquee.FitLine(l, w)))
		}
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	head := "DHCP starvation"
	if v.Note != "" {
		st := ok
		if v.NoteIsErr {
			st = bad
		}
		lines := []string{
			hdr.Render(v.Marquee.FitLine(head, w)),
			st.Render(v.Marquee.FitLine(v.Note, w)),
		}
		return v.appendDHCPFields(lines, w, h, pal, focused, txt, sec, mut, sel, ok, warn)
	}
	lines := []string{hdr.Render(v.Marquee.FitLine(head, w))}
	if v.Err != "" {
		lines = append(lines, bad.Render(v.Marquee.FitLine(v.Err, w)))
	}
	return v.appendDHCPFields(lines, w, h, pal, focused, txt, sec, mut, sel, ok, warn)
}

func (v LocalDHCPView) appendDHCPFields(lines []string, w, h int, pal theme.Palette, focused bool,
	txt, sec, mut, sel, ok, warn lipgloss.Style) []string {

	body := max(1, h-len(lines))
	fields := v.fields()
	start, end := ScrollWindow(len(fields), v.Cursor, body)
	for i := start; i < end; i++ {
		var lbl, val string
		style := txt
		switch fields[i] {
		case dhcpFieldIface:
			lbl, val = "Iface", v.ifaceLabel()
			if len(v.Interfaces) == 0 {
				style = warn
			}
		case dhcpFieldRate:
			lbl, val = "Rate", fmt.Sprintf("%d req/s", v.Rate)
		case dhcpFieldCount:
			lbl, val = "Count", fmt.Sprintf("%d requests", v.Count)
		case dhcpFieldRandomMAC:
			lbl = "MACs"
			if v.RandomMAC {
				val = "random"
			} else {
				val = "fixed"
			}
		case dhcpFieldStart:
			lbl, val, style = "Action", "Start probe", ok
			if !v.Probe {
				val, style = "Start flood", warn
			}
		}
		lines = append(lines, fieldRow(w, focused, i == v.Cursor, lbl, val, style, sec, mut, sel))
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

// HandleEsc stops a run or leaves the tab.
func (v *LocalDHCPView) HandleEsc() bool {
	if v.Mode == dhcpModeRunning {
		v.Stop()
		return true
	}
	return false
}
