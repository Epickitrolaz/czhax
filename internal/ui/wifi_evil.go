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

// WifiEvilMode is the evil-twin tab's screen state.
type WifiEvilMode int

const (
	evilModeSetup WifiEvilMode = iota
	evilModeEditSSID
	evilModeRunning
	// evilModeDNS is the DNS redirect rule editor overlay.
	evilModeDNS
	// evilModeDNSEdit types a new "host=ip" rule.
	evilModeDNSEdit
)

type wifiEvilField int

const (
	evilFieldSSID wifiEvilField = iota
	evilFieldChannel
	evilFieldAP
	evilFieldUplink
	evilFieldNAT
	evilFieldPortal
	evilFieldDNS
	evilFieldPMKID
	evilFieldDeauth
	evilFieldStart
)

// WifiEvilView is the WiFi -> Evil subtab: an evil twin with internet
// pass-through, captive portal, PMKID harvest and deauth of the original.
type WifiEvilView struct {
	WorkDir string
	// BaseDir is the CZHax working directory root (not the twin scratch dir);
	// custom portal pages are resolved against it.
	BaseDir    string
	CaptureDir string

	SSID        string
	Channel     int
	APIdx       int
	UplinkIdx   int
	NAT         bool
	PortalMode  wifi.PortalMode
	PortalFile  string
	DNSRules    []wifi.DNSRule
	DNSFilter   string
	PMKID       bool
	Deauth      bool
	TargetBSSID string

	Interfaces []wifi.Interface
	APCapable  map[string]bool
	// Uplinks are every non-wifi-capable route out: ethernet, USB tethering and
	// wifi interfaces that are not used as the AP.
	Uplinks []localscan.Interface

	Cursor   int
	Mode     WifiEvilMode
	Input    string
	InputErr string

	Active  bool
	Seq     int
	Lines   []string
	Err     string
	Cancel  context.CancelFunc
	Started time.Time
	Elapsed time.Duration
	Ch      chan string

	Marquee Marquee
}

func NewWifiEvilView() WifiEvilView {
	return WifiEvilView{
		Channel:    6,
		NAT:        true,
		PortalMode: wifi.PortalLogin,
		Mode:       evilModeSetup,
	}
}

// PortalFileDefault is where a custom portal page is looked up inside the
// working directory unless another path is configured.
const PortalFileDefault = "user/portal.html"

// PortalLabel describes the selected portal mode for the setup row.
func (v WifiEvilView) PortalLabel() string {
	switch v.PortalMode {
	case wifi.PortalOff:
		return "off"
	case wifi.PortalCustom:
		name := strings.TrimSpace(v.PortalFile)
		if name == "" {
			name = PortalFileDefault
		}
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		}
		return "custom · " + name
	default:
		return "login · logs creds"
	}
}

// DNSLabel summarises the redirect rules for the setup row.
func (v WifiEvilView) DNSLabel() string {
	if len(v.DNSRules) == 0 {
		return "all → " + wifi.EvilGateway
	}
	catchAll := wifi.EvilGateway
	specific := 0
	for _, r := range v.DNSRules {
		if r.Host == "" || r.Host == "*" {
			catchAll = r.IP
			continue
		}
		specific++
	}
	label := fmt.Sprintf("%d rule", specific)
	if specific != 1 {
		label += "s"
	}
	return label + " · all → " + catchAll
}

// portalFile resolves the custom portal page path (relative to the working
// directory when it is not absolute).
func (v WifiEvilView) portalFile() string {
	if v.PortalMode != wifi.PortalCustom {
		return ""
	}
	path := strings.TrimSpace(v.PortalFile)
	if path == "" {
		path = PortalFileDefault
	}
	base := strings.TrimSpace(v.BaseDir)
	if base == "" {
		base = strings.TrimSuffix(strings.TrimSpace(v.WorkDir), "/eviltwin")
	}
	if strings.HasPrefix(path, "/") || base == "" {
		return path
	}
	return strings.TrimSuffix(base, "/") + "/" + path
}

// FilteredDNSRules returns the rules matching the editor filter.
func (v WifiEvilView) FilteredDNSRules() []int {
	var out []int
	f := strings.ToLower(strings.TrimSpace(v.DNSFilter))
	for i, r := range v.DNSRules {
		if f == "" || strings.Contains(strings.ToLower(r.Host+" "+r.IP), f) {
			out = append(out, i)
		}
	}
	return out
}

// BeginDNS opens the rule editor.
func (v *WifiEvilView) BeginDNS() {
	v.Mode = evilModeDNS
	v.DNSFilter = ""
	v.Cursor = 0
}

// BeginDNSEdit opens the "new rule" input.
func (v *WifiEvilView) BeginDNSEdit() {
	v.Input = ""
	v.InputErr = ""
	v.Mode = evilModeDNSEdit
}

// CommitDNSEdit parses and appends a rule; returns false when invalid.
func (v *WifiEvilView) CommitDNSEdit() bool {
	rule, err := wifi.ParseDNSRule(v.Input)
	if err != nil {
		v.InputErr = err.Error()
		return false
	}
	// Replace an existing rule for the same host.
	for i, existing := range v.DNSRules {
		if existing.Host == rule.Host {
			v.DNSRules[i] = rule
			v.Input = ""
			v.InputErr = ""
			v.Mode = evilModeDNS
			return true
		}
	}
	v.DNSRules = append(v.DNSRules, rule)
	v.Input = ""
	v.InputErr = ""
	v.Mode = evilModeDNS
	return true
}

// DeleteDNSRule removes the selected rule.
func (v *WifiEvilView) DeleteDNSRule() bool {
	idx := v.FilteredDNSRules()
	if len(idx) == 0 {
		return false
	}
	pos := clampIndex(v.Cursor, len(idx))
	real := idx[pos]
	v.DNSRules = append(v.DNSRules[:real], v.DNSRules[real+1:]...)
	v.Cursor = clampIndex(pos, max(1, len(v.FilteredDNSRules())))
	return true
}

func (v WifiEvilView) EffectiveAP() string {
	if len(v.Interfaces) == 0 {
		return "wlan0"
	}
	return v.Interfaces[clampIndex(v.APIdx, len(v.Interfaces))].Name
}

// apUsable reports whether the selected AP interface can actually host a
// hotspot (its driver must advertise AP mode).
func (v WifiEvilView) apUsable(name string) bool {
	if v.APCapable == nil {
		return true
	}
	ok, known := v.APCapable[name]
	return !known || ok
}

// apIndexes lists the positions of interfaces that can host an AP.
func (v WifiEvilView) apIndexes() []int {
	var out []int
	for i, inf := range v.Interfaces {
		if v.apUsable(inf.Name) {
			out = append(out, i)
		}
	}
	return out
}

// uplinkCandidates lists every interface that can carry the uplink, with the AP
// interface excluded: ethernet ports, USB tethering gadgets and spare wifi.
func (v WifiEvilView) uplinkCandidates() []localscan.Interface {
	ap := v.EffectiveAP()
	if len(v.Uplinks) > 0 {
		out := make([]localscan.Interface, 0, len(v.Uplinks))
		for _, inf := range v.Uplinks {
			if inf.Name == ap {
				continue
			}
			out = append(out, inf)
		}
		return out
	}
	// Fallback when the broader discovery has not run yet.
	out := make([]localscan.Interface, 0, len(v.Interfaces))
	for _, inf := range v.Interfaces {
		if inf.Name == ap {
			continue
		}
		out = append(out, localscan.Interface{Name: inf.Name, Kind: localscan.KindWiFi, Up: inf.Up, Connected: inf.Up})
	}
	return out
}

// UplinkIface returns the selected uplink name, or "" when none is available.
func (v WifiEvilView) UplinkIface() string {
	cands := v.uplinkCandidates()
	if len(cands) == 0 {
		return ""
	}
	return cands[clampIndex(v.UplinkIdx, len(cands))].Name
}

// uplinkLabel renders the selected uplink with its kind ("eth0 · Eth").
func (v WifiEvilView) uplinkLabel() (string, bool) {
	cands := v.uplinkCandidates()
	if len(cands) == 0 {
		return "", false
	}
	inf := cands[clampIndex(v.UplinkIdx, len(cands))]
	label := inf.Name
	if inf.Kind != "" {
		label += " · " + string(inf.Kind)
	}
	return label, true
}

// SyncUplinks keeps the broader interface discovery (ethernet, USB tethering,
// wifi) available for the uplink picker.
func (v *WifiEvilView) SyncUplinks(ifaces []localscan.Interface) {
	prev := v.UplinkIface()
	v.Uplinks = ifaces
	if len(ifaces) == 0 {
		v.UplinkIdx = 0
		return
	}
	cands := v.uplinkCandidates()
	if len(cands) == 0 {
		v.UplinkIdx = 0
		return
	}
	for i, inf := range cands {
		if inf.Name == prev {
			v.UplinkIdx = i
			return
		}
	}
	// Prefer a connected wired/tethered uplink over wifi.
	for i, inf := range cands {
		if inf.Kind != localscan.KindWiFi && inf.Connected {
			v.UplinkIdx = i
			return
		}
	}
	v.UplinkIdx = 0
}

func (v WifiEvilView) IsTextInput() bool {
	return v.Mode == evilModeEditSSID || v.Mode == evilModeDNSEdit
}

func (v *WifiEvilView) SyncInterfaces(ifaces []wifi.Interface) {
	prev := v.EffectiveAP()
	v.Interfaces = ifaces
	if len(ifaces) == 0 {
		v.APIdx, v.UplinkIdx = 0, 0
		return
	}
	if v.APCapable == nil {
		v.APCapable = make(map[string]bool, len(ifaces))
	}
	for _, inf := range ifaces {
		v.APCapable[inf.Name] = wifi.APCapable(inf.Name)
	}
	// Prefer keeping the previous choice when it can still host an AP.
	if v.apUsable(prev) {
		for i, inf := range ifaces {
			if inf.Name == prev {
				v.APIdx = i
				break
			}
		}
	} else if idx := v.apIndexes(); len(idx) > 0 {
		v.APIdx = idx[0]
	}
	v.APIdx = clampIndex(v.APIdx, len(ifaces))
	v.UplinkIdx = clampIndex(v.UplinkIdx, max(1, len(ifaces)-1))
}

// Prefill configures the twin from a scanned network (the Scan tab's clone
// actions) without starting it.
func (v *WifiEvilView) Prefill(net wifi.Network) {
	v.SSID = net.SSID
	if ch := wifi.ChannelNumber(net); ch > 0 {
		v.Channel = ch
	}
	v.TargetBSSID = net.BSSID
	v.Mode = evilModeSetup
}

func (v WifiEvilView) fields() []wifiEvilField {
	return []wifiEvilField{
		evilFieldSSID, evilFieldChannel, evilFieldAP, evilFieldUplink,
		evilFieldNAT, evilFieldPortal, evilFieldDNS, evilFieldPMKID,
		evilFieldDeauth, evilFieldStart,
	}
}

func (v *WifiEvilView) MoveCursor(delta int) {
	f := v.fields()
	v.Cursor = (v.Cursor + delta + len(f)) % len(f)
}

func (v *WifiEvilView) Adjust(delta int) {
	f := v.fields()
	switch f[clampIndex(v.Cursor, len(f))] {
	case evilFieldChannel:
		v.Channel = (v.Channel + delta + 13) % 13
		if v.Channel == 0 {
			v.Channel = 1
		}
	case evilFieldAP:
		// Only cycle through interfaces whose driver supports AP mode.
		if idx := v.apIndexes(); len(idx) > 0 {
			cur := 0
			for i, pos := range idx {
				if pos == v.APIdx {
					cur = i
				}
			}
			next := (cur + delta + len(idx)) % len(idx)
			v.APIdx = idx[next]
		}
	case evilFieldUplink:
		if n := len(v.uplinkCandidates()); n > 0 {
			v.UplinkIdx = (v.UplinkIdx + delta + n) % n
		}
	case evilFieldNAT:
		v.NAT = !v.NAT
	case evilFieldPortal:
		switch v.PortalMode {
		case wifi.PortalOff:
			v.PortalMode = wifi.PortalLogin
		case wifi.PortalLogin:
			v.PortalMode = wifi.PortalCustom
		default:
			v.PortalMode = wifi.PortalOff
		}
	case evilFieldPMKID:
		v.PMKID = !v.PMKID
	case evilFieldDeauth:
		v.Deauth = !v.Deauth
	}
}

func (v *WifiEvilView) BeginEdit() bool {
	f := v.fields()
	if f[clampIndex(v.Cursor, len(f))] != evilFieldSSID {
		return false
	}
	v.Input = v.SSID
	v.Mode = evilModeEditSSID
	return true
}

func (v *WifiEvilView) CommitEdit() {
	v.SSID = strings.TrimSpace(v.Input)
	v.Mode = evilModeSetup
}

func (v *WifiEvilView) Config() wifi.EvilConfig {
	cfg := wifi.EvilConfig{
		Iface:       v.EffectiveAP(),
		UplinkIface: v.UplinkIface(),
		SSID:        strings.TrimSpace(v.SSID),
		Channel:     clampInt(v.Channel, 1, 13),
		NAT:         v.NAT,
		Portal:      v.PortalMode != wifi.PortalOff,
		PortalMode:  string(v.PortalMode),
		PortalFile:  v.portalFile(),
		DNSRules:    append([]wifi.DNSRule(nil), v.DNSRules...),
		PMKID:       v.PMKID,
		WorkDir:     strings.TrimSpace(v.WorkDir) + "/eviltwin",
		CaptureDir:  v.CaptureDir,
	}
	if v.Deauth && strings.TrimSpace(v.TargetBSSID) != "" {
		cfg.DeauthBSSID = v.TargetBSSID
	}
	if cfg.UplinkIface == "" {
		cfg.NAT = false
	}
	return cfg
}

func (v *WifiEvilView) Start(workDir, captureDir string) (context.Context, int, wifi.EvilConfig, chan string, error) {
	v.WorkDir = workDir + "/eviltwin"
	v.CaptureDir = captureDir
	cfg := v.Config()
	if cfg.SSID == "" {
		v.Err = "enter an SSID to clone"
		return nil, 0, cfg, nil, fmt.Errorf("no SSID")
	}
	if !v.apUsable(cfg.Iface) {
		v.Err = cfg.Iface + " cannot host an AP - pick another radio"
		return nil, 0, cfg, nil, fmt.Errorf("%s has no AP mode", cfg.Iface)
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.Seq++
	v.Active = true
	v.Cancel = cancel
	v.Err = ""
	v.Lines = nil
	v.Started = time.Now()
	v.Elapsed = 0
	v.Ch = make(chan string, 64)
	v.Mode = evilModeRunning
	return ctx, v.Seq, cfg, v.Ch, nil
}

func (v *WifiEvilView) Stop() {
	if v.Cancel != nil {
		v.Cancel()
		v.Cancel = nil
	}
}

func (v *WifiEvilView) AppendLine(seq int, line string) {
	if seq != v.Seq {
		return
	}
	if line = sanitizeLogLine(line); line == "" {
		return
	}
	v.Lines = append(v.Lines, line)
	if len(v.Lines) > 120 {
		v.Lines = v.Lines[len(v.Lines)-120:]
	}
}

func (v *WifiEvilView) Finish(seq int, err error) {
	if seq != v.Seq {
		return
	}
	v.Active = false
	v.Cancel = nil
	v.Mode = evilModeSetup
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		v.Err = err.Error()
	}
}

// RenderLines draws the evil tab strictly inside w x h.
func (v WifiEvilView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)

	if v.Mode == evilModeDNS || v.Mode == evilModeDNSEdit {
		return v.renderDNS(w, h, pal, focused)
	}

	if v.Mode == evilModeRunning {
		title := fmt.Sprintf("Evil twin %q · %s", v.SSID, v.EffectiveAP())
		lines := []string{
			hdr.Render(v.Marquee.FitLine(title, w)),
			warn.Render(v.Marquee.FitLine(fmt.Sprintf("ch %d · %s · %s", v.Channel, evilFlags(v), formatClock(int(v.Elapsed.Seconds()))), w)),
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

	lines := []string{hdr.Render(v.Marquee.FitLine("Evil twin · "+v.EffectiveAP(), w))}
	fields := v.fields()
	cur := clampIndex(v.Cursor, len(fields))
	visH := max(1, h-1)
	if v.Err != "" && visH > 1 {
		visH--
	}
	start, end := ScrollWindow(len(fields), cur, visH)
	labelW := min(9, max(6, w/4))
	valW := max(8, w-labelW-4)

	toggle := func(on bool, yes, no string) (string, lipgloss.Style) {
		if on {
			return yes, ok
		}
		return no, mut
	}

	for i := start; i < end; i++ {
		f := fields[i]
		var lbl, val string
		style := txt
		switch f {
		case evilFieldSSID:
			lbl = "SSID"
			val = v.SSID
			if val == "" {
				val, style = "(clone target)", mut
			}
		case evilFieldChannel:
			lbl, val = "Channel", fmt.Sprintf("%d", v.Channel)
		case evilFieldAP:
			lbl = "AP"
			if name := v.EffectiveAP(); v.apUsable(name) {
				val = name
			} else {
				val, style = name+" (no AP)", bad
			}
		case evilFieldUplink:
			lbl = "Uplink"
			if label, ok := v.uplinkLabel(); ok {
				val = label
			} else {
				val, style = "none", bad
			}
		case evilFieldNAT:
			lbl = "Internet"
			val, style = toggle(v.NAT && v.UplinkIface() != "", "NAT via uplink", "[OFF]")
		case evilFieldPortal:
			lbl = "Portal"
			switch v.PortalMode {
			case wifi.PortalOff:
				val, style = "[OFF]", mut
			case wifi.PortalCustom:
				val, style = v.PortalLabel(), warn
			default:
				val, style = v.PortalLabel(), ok
			}
		case evilFieldDNS:
			lbl = "DNS"
			val, style = v.DNSLabel(), ok
			if len(v.DNSRules) == 0 {
				style = mut
			}
		case evilFieldPMKID:
			lbl = "PMKID"
			val, style = toggle(v.PMKID, "hcxdumptool", "[OFF]")
		case evilFieldDeauth:
			lbl = "Deauth"
			if v.Deauth {
				if v.TargetBSSID == "" {
					val, style = "needs target", warn
				} else {
					val, style = "original "+v.TargetBSSID, ok
				}
			} else {
				val, style = "[OFF]", mut
			}
		case evilFieldStart:
			lbl, val = "Action", "Start twin"
			style = ok
		}
		prefix := "  "
		if i == cur {
			prefix = "> "
		}
		row := fmt.Sprintf("%s%-*s %s", prefix, labelW, Truncate(lbl, labelW), Truncate(val, valW))
		if i == cur && focused {
			lines = append(lines, sel.Render(v.Marquee.FitLine(row, w)))
		} else {
			lines = append(lines, sec.Render(fmt.Sprintf("%s%-*s ", prefix, labelW, Truncate(lbl, labelW)))+style.Render(Truncate(val, max(1, w-labelW-3))))
		}
	}
	if v.Mode == evilModeEditSSID {
		lines = append(lines, renderEmuInputBox(v.Input, w, pal, focused))
	} else if v.Err != "" && len(lines) < h {
		lines = append(lines, bad.Render(v.Marquee.FitLine("Err: "+v.Err, w)))
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func evilFlags(v WifiEvilView) string {
	flags := make([]string, 0, 4)
	if v.NAT && v.UplinkIface() != "" {
		flags = append(flags, "NAT")
	}
	if v.PortalMode != wifi.PortalOff {
		flags = append(flags, "portal")
	}
	if v.PMKID {
		flags = append(flags, "pmkid")
	}
	if v.Deauth && v.TargetBSSID != "" {
		flags = append(flags, "deauth")
	}
	if len(flags) == 0 {
		return "plain"
	}
	return strings.Join(flags, "+")
}

// renderDNS draws the DNS redirect editor: one line per rule plus an input box
// while a new rule is being typed.
func (v WifiEvilView) renderDNS(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)

	wild := wifi.EvilGateway
	lines := []string{hdr.Render(v.Marquee.FitLine(fmt.Sprintf("DNS redirects (%d)", len(v.DNSRules)), w))}
	lines = append(lines, mut.Render(v.Marquee.FitLine("catch-all → "+wild+" (portal)", w)))

	idxs := v.FilteredDNSRules()
	body := max(1, h-len(lines)-1)
	if len(idxs) == 0 {
		lines = append(lines, mut.Render(v.Marquee.FitLine("(no rules) a:add", w)))
	} else {
		cur := clampIndex(v.Cursor, len(idxs))
		start, end := ScrollWindow(len(idxs), cur, body)
		for i := start; i < end; i++ {
			rule := v.DNSRules[idxs[i]]
			row := " " + wifi.FormatDNSRule(rule)
			if i == cur && focused {
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				lines = append(lines, sec.Render(v.Marquee.FitLine(row, w)))
			}
		}
	}

	if v.Mode == evilModeDNSEdit {
		lines = append(lines, renderEmuInputBox(v.Input, w, pal, focused))
		if v.InputErr != "" {
			lines = append(lines, bad.Render(v.Marquee.FitLine(v.InputErr, w)))
		}
		lines = append(lines, mut.Render(v.Marquee.FitLine("host=ip · ⏎ save · Esc cancel", w)))
	} else if v.Err != "" {
		lines = append(lines, bad.Render(v.Marquee.FitLine(v.Err, w)))
	} else {
		lines = append(lines, mut.Render(v.Marquee.FitLine("a:add x:del Esc:back", w)))
	}
	_ = ok
	_ = txt
	return v.Marquee.SplitLinesPadded(lines, w, h)
}
