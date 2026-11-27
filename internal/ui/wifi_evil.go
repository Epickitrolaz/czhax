package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

// WifiEvilMode is the evil-twin tab's screen state.
type WifiEvilMode int

const (
	evilModeSetup WifiEvilMode = iota
	evilModeEditSSID
	evilModeRunning
)

type wifiEvilField int

const (
	evilFieldSSID wifiEvilField = iota
	evilFieldChannel
	evilFieldAP
	evilFieldUplink
	evilFieldNAT
	evilFieldPortal
	evilFieldPMKID
	evilFieldDeauth
	evilFieldStart
)

// WifiEvilView is the WiFi -> Evil subtab: an evil twin with internet
// pass-through, captive portal, PMKID harvest and deauth of the original.
type WifiEvilView struct {
	WorkDir    string
	CaptureDir string

	SSID        string
	Channel     int
	APIdx       int
	UplinkIdx   int
	NAT         bool
	Portal      bool
	PMKID       bool
	Deauth      bool
	TargetBSSID string

	Interfaces []wifi.Interface

	Cursor int
	Mode   WifiEvilMode
	Input  string

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
		Channel: 6,
		NAT:     true,
		Portal:  true,
		Mode:    evilModeSetup,
	}
}

func (v WifiEvilView) EffectiveAP() string {
	if len(v.Interfaces) == 0 {
		return "wlan0"
	}
	return v.Interfaces[clampIndex(v.APIdx, len(v.Interfaces))].Name
}

// UplinkIface returns the selected uplink, excluding the AP interface.
func (v WifiEvilView) UplinkIface() string {
	if len(v.Interfaces) < 2 {
		return ""
	}
	ap := v.EffectiveAP()
	cands := make([]wifi.Interface, 0, len(v.Interfaces))
	for _, inf := range v.Interfaces {
		if inf.Name != ap {
			cands = append(cands, inf)
		}
	}
	if len(cands) == 0 {
		return ""
	}
	return cands[clampIndex(v.UplinkIdx, len(cands))].Name
}

func (v WifiEvilView) IsTextInput() bool { return v.Mode == evilModeEditSSID }

func (v *WifiEvilView) SyncInterfaces(ifaces []wifi.Interface) {
	prev := v.EffectiveAP()
	v.Interfaces = ifaces
	if len(ifaces) == 0 {
		v.APIdx, v.UplinkIdx = 0, 0
		return
	}
	for i, inf := range ifaces {
		if inf.Name == prev {
			v.APIdx = i
			break
		}
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
		evilFieldNAT, evilFieldPortal, evilFieldPMKID, evilFieldDeauth, evilFieldStart,
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
		v.Channel = (v.Channel + delta + 14) % 14
		if v.Channel == 0 {
			v.Channel = 1
		}
	case evilFieldAP:
		if len(v.Interfaces) > 0 {
			v.APIdx = (v.APIdx + delta + len(v.Interfaces)) % len(v.Interfaces)
		}
	case evilFieldUplink:
		n := max(1, len(v.Interfaces)-1)
		v.UplinkIdx = (v.UplinkIdx + delta + n) % n
	case evilFieldNAT:
		v.NAT = !v.NAT
	case evilFieldPortal:
		v.Portal = !v.Portal
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
		Channel:     clampInt(v.Channel, 1, 14),
		NAT:         v.NAT,
		Portal:      v.Portal,
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
	v.Lines = append(v.Lines, strings.TrimSpace(line))
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
			lbl, val = "AP", v.EffectiveAP()
		case evilFieldUplink:
			lbl = "Uplink"
			if up := v.UplinkIface(); up != "" {
				val = up
			} else {
				val, style = "none", bad
			}
		case evilFieldNAT:
			lbl = "Internet"
			val, style = toggle(v.NAT && v.UplinkIface() != "", "NAT via uplink", "[OFF]")
		case evilFieldPortal:
			lbl = "Portal"
			val, style = toggle(v.Portal, "captive portal", "[OFF]")
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
	if v.Portal {
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
