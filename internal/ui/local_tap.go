package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/ethtap"
	"github.com/czhax/czhax/internal/localscan"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

// LocalTapMode is the Ethernet tap tab's screen state.
type LocalTapMode int

const (
	tapModeSetup LocalTapMode = iota
	tapModeRunning
)

type localTapField int

const (
	tapFieldPortA localTapField = iota
	tapFieldPortB
	tapFieldRecord
	tapFieldSpoof
	tapFieldPortal
	tapFieldStart
)

// LocalTapView configures a transparent inline Ethernet tap: two wired ports
// are bridged, every frame is forwarded untouched, and the tab exposes live
// counters, pcap recording and an optional DNS-spoof portal.
type LocalTapView struct {
	Mode       LocalTapMode
	Interfaces []localscan.Interface

	PortA  string
	PortB  string
	Record bool
	Spoof  bool
	Portal wifi.PortalMode

	Cursor int

	Active  bool
	Seq     int
	Cancel  context.CancelFunc
	Lines   []string
	Ch      chan string
	Stats   string
	Proto   string
	Top     string
	Err     string
	Started time.Time
	Elapsed time.Duration

	Marquee Marquee
}

func NewLocalTapView() LocalTapView {
	return LocalTapView{Record: true, Portal: wifi.PortalLogin}
}

// tapCandidates lists wired links that can be enslaved to a bridge. WiFi and
// point-to-point tether interfaces have no Ethernet header, so they are not
// offered.
func tapCandidates(ifaces []localscan.Interface) []localscan.Interface {
	out := make([]localscan.Interface, 0, len(ifaces))
	for _, inf := range ifaces {
		if inf.Kind == localscan.KindWiFi {
			continue
		}
		out = append(out, inf)
	}
	return out
}

// SyncInterfaces keeps both port selections valid as the interface list
// changes, and never lets both ports point at the same link.
func (v *LocalTapView) SyncInterfaces(ifaces []localscan.Interface) {
	v.Interfaces = ifaces
	cands := tapCandidates(ifaces)
	if len(cands) == 0 {
		v.PortA, v.PortB = "", ""
		return
	}
	names := make(map[string]bool, len(cands))
	for _, inf := range cands {
		names[inf.Name] = true
	}
	if !names[v.PortA] {
		v.PortA = ""
	}
	if !names[v.PortB] || v.PortB == v.PortA {
		v.PortB = ""
	}
	if v.PortA == "" {
		v.PortA = cands[0].Name
	}
	if v.PortB == "" {
		for _, inf := range cands {
			if inf.Name != v.PortA {
				v.PortB = inf.Name
				break
			}
		}
	}
}

func (v LocalTapView) findIface(name string) (localscan.Interface, bool) {
	for _, inf := range v.Interfaces {
		if inf.Name == name {
			return inf, true
		}
	}
	return localscan.Interface{}, false
}

func (v LocalTapView) ifaceLabel(name string) string {
	if name == "" {
		return "none"
	}
	inf, ok := v.findIface(name)
	if !ok {
		return name
	}
	label := inf.Name
	if inf.Connected && inf.IP != "" {
		label += " · " + inf.IP
	} else if !inf.Carrier && inf.Up {
		label += " · no link"
	} else if !inf.Up {
		label += " · down"
	}
	return label
}

// EffectivePorts returns the selected pair, or the best available pair when the
// view was populated before interfaces arrived.
func (v LocalTapView) EffectivePorts() (string, string) {
	return v.PortA, v.PortB
}

func (v LocalTapView) fields() []localTapField {
	out := []localTapField{tapFieldPortA, tapFieldPortB, tapFieldRecord, tapFieldSpoof}
	if v.Spoof {
		out = append(out, tapFieldPortal)
	}
	return append(out, tapFieldStart)
}

func (v *LocalTapView) MoveCursor(delta int) {
	f := v.fields()
	v.Cursor = (v.Cursor + delta + len(f)) % len(f)
}

// cyclePorts moves one port to the next wired link, skipping the other port.
// With only two links there is nothing else to pick, so the pair swaps instead
// (otherwise ←/→ would look broken on a two-port Pi).
func (v *LocalTapView) cyclePorts(primary bool, delta int) {
	cands := tapCandidates(v.Interfaces)
	if len(cands) < 2 {
		return
	}
	cur, other := v.PortA, v.PortB
	if !primary {
		cur, other = v.PortB, v.PortA
	}
	idx := 0
	for i, inf := range cands {
		if inf.Name == cur {
			idx = i
			break
		}
	}
	for step := 1; step <= len(cands); step++ {
		i := ((idx+step*delta)%len(cands) + len(cands)) % len(cands)
		name := cands[i].Name
		if name == other || name == cur {
			continue
		}
		if primary {
			v.PortA = name
		} else {
			v.PortB = name
		}
		return
	}
	// Two links only: swap so the key still does something visible.
	v.PortA, v.PortB = other, cur
}

func (v *LocalTapView) Adjust(delta int) {
	f := v.fields()
	switch f[clampIndex(v.Cursor, len(f))] {
	case tapFieldPortA:
		v.cyclePorts(true, delta)
	case tapFieldPortB:
		v.cyclePorts(false, delta)
	case tapFieldRecord:
		v.Record = !v.Record
	case tapFieldSpoof:
		v.Spoof = !v.Spoof
		v.Cursor = clampIndex(v.Cursor, len(v.fields()))
	case tapFieldPortal:
		switch v.Portal {
		case wifi.PortalLogin:
			v.Portal = wifi.PortalCustom
		case wifi.PortalCustom:
			v.Portal = wifi.PortalOff
		default:
			v.Portal = wifi.PortalLogin
		}
	}
}

// Subnet is the tapped network used to pick the portal address.
func (v LocalTapView) Subnet() string {
	for _, name := range []string{v.PortA, v.PortB} {
		if inf, ok := v.findIface(name); ok && strings.TrimSpace(inf.Subnet) != "" {
			return inf.Subnet
		}
	}
	return ""
}

// Config builds the helper configuration for a run.
func (v LocalTapView) Config(captureDir string) ethtap.Config {
	portal := v.Portal
	if !v.Spoof {
		portal = wifi.PortalOff
	}
	root := filepath.Dir(captureDir)
	return ethtap.Config{
		PortA:      v.PortA,
		PortB:      v.PortB,
		Record:     v.Record,
		CaptureDir: captureDir,
		Spoof:      v.Spoof,
		Subnet:     v.Subnet(),
		Portal:     string(portal),
		PortalDoc:  filepath.Join(root, PortalFileDefault),
		WorkDir:    root,
	}
}

// Start launches a run and returns its context/sequence plus the line channel.
func (v *LocalTapView) Start(captureDir string) (context.Context, int, ethtap.Config, chan string, error) {
	a, b := v.EffectivePorts()
	if a == "" || b == "" {
		v.Err = "select two Ethernet ports"
		return nil, 0, ethtap.Config{}, nil, fmt.Errorf("select two Ethernet ports")
	}
	if a == b {
		v.Err = "the two ports must be different"
		return nil, 0, ethtap.Config{}, nil, fmt.Errorf("same port twice")
	}
	if v.Spoof && strings.TrimSpace(v.Subnet()) == "" {
		v.Err = "DNS spoof needs an IPv4 subnet on a port"
		return nil, 0, ethtap.Config{}, nil, fmt.Errorf("no subnet")
	}
	cfg := v.Config(captureDir)
	ctx, cancel := context.WithCancel(context.Background())
	v.Seq++
	v.Active = true
	v.Cancel = cancel
	v.Err = ""
	v.Lines = nil
	v.Stats, v.Proto, v.Top = "", "", ""
	v.Started = time.Now()
	v.Elapsed = 0
	v.Ch = make(chan string, 64)
	v.Mode = tapModeRunning
	return ctx, v.Seq, cfg, v.Ch, nil
}

func (v *LocalTapView) Stop() {
	if v.Cancel != nil {
		v.Cancel()
		v.Cancel = nil
	}
}

// AppendLine routes helper output: counters have their own rows, everything
// else goes to the scrolling log.
func (v *LocalTapView) AppendLine(seq int, line string) {
	if seq != v.Seq {
		return
	}
	line = sanitizeLogLine(line)
	if line == "" {
		return
	}
	switch {
	case strings.HasPrefix(line, "stats "):
		v.Stats = strings.TrimSpace(strings.TrimPrefix(line, "stats "))
		return
	case strings.HasPrefix(line, "proto "):
		v.Proto = strings.TrimSpace(strings.TrimPrefix(line, "proto "))
		return
	case strings.HasPrefix(line, "top "):
		v.Top = strings.TrimSpace(strings.TrimPrefix(line, "top "))
		return
	}
	v.Lines = append(v.Lines, line)
	if len(v.Lines) > 160 {
		v.Lines = v.Lines[len(v.Lines)-160:]
	}
}

func (v *LocalTapView) Finish(seq int, err error) {
	if seq != v.Seq {
		return
	}
	v.Active = false
	v.Cancel = nil
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		v.Err = err.Error()
		return
	}
	v.Mode = tapModeSetup
}

// HandleEsc stops a running tap, dismisses a finished/error screen, or leaves
// the tab. It always consumes the key while the tab is in its running screen so
// a failed run never traps the user there.
func (v *LocalTapView) HandleEsc() bool {
	if v.Mode == tapModeRunning {
		if v.Active {
			v.Stop()
			return true
		}
		v.Mode = tapModeSetup
		return true
	}
	return false
}

func (v LocalTapView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)

	if v.Mode == tapModeRunning {
		head := fmt.Sprintf("● Tap %s ↔ %s %s", v.PortA, v.PortB, formatClock(int(v.Elapsed.Seconds())))
		lines := []string{ok.Render(v.Marquee.FitLine(head, w))}
		if v.Err != "" {
			lines = append(lines, bad.Render(v.Marquee.FitLine(v.Err, w)))
		}
		for _, row := range []string{v.Stats, v.Proto} {
			if row != "" {
				lines = append(lines, sec.Render(v.Marquee.FitLine(row, w)))
			}
		}
		if v.Top != "" {
			lines = append(lines, sec.Render(v.Marquee.FitLine("top "+v.Top, w)))
		}
		body := max(1, h-len(lines))
		tail := v.Lines
		if len(tail) > body {
			tail = tail[len(tail)-body:]
		}
		for _, l := range tail {
			st := mut
			switch {
			case strings.HasPrefix(l, "auth "), strings.HasPrefix(l, "form "), strings.HasPrefix(l, "portal:"):
				st = warn
			case strings.HasPrefix(l, "spoof "), strings.HasPrefix(l, "tap "):
				st = ok
			case strings.HasPrefix(l, "warning"), strings.Contains(l, "failed"):
				st = bad
			}
			lines = append(lines, st.Render(v.Marquee.FitLine(l, w)))
		}
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	lines := []string{hdr.Render(v.Marquee.FitLine("Ethernet tap", w))}
	if v.Err != "" {
		lines = append(lines, bad.Render(v.Marquee.FitLine(v.Err, w)))
	} else if len(tapCandidates(v.Interfaces)) < 2 {
		lines = append(lines, warn.Render(v.Marquee.FitLine("need two wired ports", w)))
	}
	return v.appendTapFields(lines, w, h, focused, txt, sec, mut, sel, ok, warn)
}

func (v LocalTapView) appendTapFields(lines []string, w, h int, focused bool,
	txt, sec, mut, sel, ok, warn lipgloss.Style) []string {

	body := max(1, h-len(lines))
	fields := v.fields()
	start, end := ScrollWindow(len(fields), v.Cursor, body)
	for i := start; i < end; i++ {
		var lbl, val string
		style := txt
		switch fields[i] {
		case tapFieldPortA:
			lbl, val = "Port A", v.ifaceLabel(v.PortA)
			if v.PortA == "" {
				style = warn
			}
		case tapFieldPortB:
			lbl, val = "Port B", v.ifaceLabel(v.PortB)
			if v.PortB == "" || v.PortB == v.PortA {
				style = warn
			}
		case tapFieldRecord:
			lbl = "Record"
			val = "pcap off"
			if v.Record {
				val = "pcap on"
			}
		case tapFieldSpoof:
			lbl = "DNS spoof"
			val = "off"
			if v.Spoof {
				val = "portal"
				style = warn
			}
		case tapFieldPortal:
			lbl, val = "Portal", string(v.Portal)
			if v.Portal == wifi.PortalCustom {
				val = "custom page"
			}
		case tapFieldStart:
			lbl, val, style = "Action", "Start tap", ok
		}
		lines = append(lines, fieldRow(w, focused, i == v.Cursor, lbl, val, style, sec, mut, sel))
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}
