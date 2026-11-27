package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

// WifiSpamSource selects how the SSID list is produced.
type WifiSpamSource int

const (
	SpamSourceSingle WifiSpamSource = iota
	SpamSourceNumbered
	SpamSourceFile
)

var spamSourceLabels = []string{"Single", "Numbered", "File"}

// WifiSpamMode is the spam tab's screen state.
type WifiSpamMode int

const (
	spamModeSetup WifiSpamMode = iota
	spamModeEdit
	spamModeRunning
)

type wifiSpamField int

const (
	spamFieldSource wifiSpamField = iota
	spamFieldValue
	spamFieldCount
	spamFieldChannel
	spamFieldInterval
	spamFieldRandomMAC
	spamFieldRadio
	spamFieldStart
)

// WifiSpamView is the WiFi -> Spam subtab: a beacon flood driven by a single
// SSID, numbered names or a file.
type WifiSpamView struct {
	WorkDir string

	Source     WifiSpamSource
	SingleName string
	Prefix     string
	FilePath   string
	Count      int
	Channel    int
	IntervalMS int
	RandomMAC  bool
	IfaceIdx   int
	Interfaces []wifi.Interface

	Cursor int
	Mode   WifiSpamMode
	Input  string

	Active  bool
	Seq     int
	Lines   []string
	Err     string
	Stats   string
	Cancel  context.CancelFunc
	Started time.Time
	Elapsed time.Duration
	Ch      chan string

	Marquee Marquee
}

func NewWifiSpamView() WifiSpamView {
	return WifiSpamView{
		SingleName: "FreeWiFi",
		Prefix:     "FreeWiFi",
		Count:      25,
		Channel:    6,
		IntervalMS: 15,
		RandomMAC:  true,
		Mode:       spamModeSetup,
	}
}

func (v WifiSpamView) EffectiveIface() string {
	if len(v.Interfaces) == 0 {
		return "wlan0"
	}
	return v.Interfaces[clampIndex(v.IfaceIdx, len(v.Interfaces))].Name
}

func (v WifiSpamView) IsTextInput() bool { return v.Mode == spamModeEdit }

func (v *WifiSpamView) SyncInterfaces(ifaces []wifi.Interface) {
	prev := v.EffectiveIface()
	v.Interfaces = ifaces
	if len(ifaces) == 0 {
		v.IfaceIdx = 0
		return
	}
	for i, inf := range ifaces {
		if inf.Name == prev {
			v.IfaceIdx = i
			return
		}
	}
	v.IfaceIdx = clampIndex(v.IfaceIdx, len(ifaces))
}

// SSIDs resolves the configured source into the list that will be advertised.
func (v WifiSpamView) SSIDs() ([]string, error) {
	switch v.Source {
	case SpamSourceNumbered:
		return wifi.NumberedSSIDs(v.Prefix, v.Count), nil
	case SpamSourceFile:
		path := strings.TrimSpace(v.FilePath)
		if path == "" {
			return nil, fmt.Errorf("no SSID file selected")
		}
		return wifi.LoadSSIDFile(path)
	default:
		name := strings.TrimSpace(v.SingleName)
		if name == "" {
			return nil, fmt.Errorf("enter an SSID")
		}
		return []string{name}, nil
	}
}

func (v WifiSpamView) fields() []wifiSpamField {
	out := []wifiSpamField{spamFieldSource, spamFieldValue}
	if v.Source == SpamSourceNumbered {
		out = append(out, spamFieldCount)
	}
	out = append(out, spamFieldChannel, spamFieldInterval, spamFieldRandomMAC, spamFieldRadio, spamFieldStart)
	return out
}

func (v *WifiSpamView) MoveCursor(delta int) {
	f := v.fields()
	v.Cursor = (v.Cursor + delta + len(f)) % len(f)
}

func (v WifiSpamView) valueField() (label, text string) {
	switch v.Source {
	case SpamSourceNumbered:
		return "Prefix", v.Prefix
	case SpamSourceFile:
		return "File", v.FilePath
	default:
		return "SSID", v.SingleName
	}
}

// Adjust changes the highlighted numeric/enum field.
func (v *WifiSpamView) Adjust(delta int) {
	f := v.fields()
	cur := f[clampIndex(v.Cursor, len(f))]
	switch cur {
	case spamFieldSource:
		n := len(spamSourceLabels)
		v.Source = WifiSpamSource((int(v.Source) + delta + n) % n)
	case spamFieldCount:
		v.Count = clampInt(v.Count+delta*5, 1, 250)
	case spamFieldChannel:
		v.Channel = (v.Channel + delta + 14) % 14
		if v.Channel == 0 {
			v.Channel = 1
		}
	case spamFieldInterval:
		v.IntervalMS = clampInt(v.IntervalMS+delta*5, 2, 200)
	case spamFieldRandomMAC:
		v.RandomMAC = !v.RandomMAC
	case spamFieldRadio:
		if len(v.Interfaces) > 0 {
			v.IfaceIdx = (v.IfaceIdx + delta + len(v.Interfaces)) % len(v.Interfaces)
		}
	}
}

// BeginEdit opens the text editor for the highlighted value field.
func (v *WifiSpamView) BeginEdit() bool {
	f := v.fields()
	if f[clampIndex(v.Cursor, len(f))] != spamFieldValue {
		return false
	}
	_, v.Input = v.valueField()
	v.Mode = spamModeEdit
	return true
}

func (v *WifiSpamView) CommitEdit() {
	switch v.Source {
	case SpamSourceNumbered:
		v.Prefix = strings.TrimSpace(v.Input)
		if v.Prefix == "" {
			v.Prefix = "FreeWiFi"
		}
	case SpamSourceFile:
		v.FilePath = strings.TrimSpace(v.Input)
	default:
		v.SingleName = strings.TrimSpace(v.Input)
	}
	v.Mode = spamModeSetup
}

func (v *WifiSpamView) Start(workDir string) (context.Context, int, []string, chan string, error) {
	ssids, err := v.SSIDs()
	if err != nil {
		v.Err = err.Error()
		return nil, 0, nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.Seq++
	v.Active = true
	v.Cancel = cancel
	v.Err = ""
	v.Lines = nil
	v.Channel = clampInt(v.Channel, 1, 14)
	v.Stats = fmt.Sprintf("%d SSIDs", len(ssids))
	v.Started = time.Now()
	v.Elapsed = 0
	v.Ch = make(chan string, 64)
	v.Mode = spamModeRunning
	v.WorkDir = workDir
	return ctx, v.Seq, ssids, v.Ch, nil
}

func (v *WifiSpamView) Stop() {
	if v.Cancel != nil {
		v.Cancel()
		v.Cancel = nil
	}
}

func (v *WifiSpamView) AppendLine(seq int, line string) {
	if seq != v.Seq {
		return
	}
	v.Lines = append(v.Lines, strings.TrimSpace(line))
	if len(v.Lines) > 120 {
		v.Lines = v.Lines[len(v.Lines)-120:]
	}
}

func (v *WifiSpamView) Finish(seq int, err error) {
	if seq != v.Seq {
		return
	}
	v.Active = false
	v.Cancel = nil
	v.Mode = spamModeSetup
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		v.Err = err.Error()
	}
}

// RenderLines draws the spam tab strictly inside w x h.
func (v WifiSpamView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)

	if v.Mode == spamModeRunning {
		lines := []string{
			hdr.Render(v.Marquee.FitLine("Beacon spam · "+v.EffectiveIface(), w)),
			warn.Render(v.Marquee.FitLine(fmt.Sprintf("%s · ch %d · %dms · %s", v.Stats, v.Channel, v.IntervalMS, formatClock(int(v.Elapsed.Seconds()))), w)),
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

	lines := []string{hdr.Render(v.Marquee.FitLine("Beacon spam · "+v.EffectiveIface(), w))}
	fields := v.fields()
	cur := clampIndex(v.Cursor, len(fields))
	visH := max(1, h-1)
	if v.Err != "" && visH > 1 {
		visH--
	}
	start, end := ScrollWindow(len(fields), cur, visH)
	labelW := min(9, max(6, w/4))
	valW := max(8, w-labelW-4)

	name, value := v.valueField()
	for i := start; i < end; i++ {
		f := fields[i]
		var lbl, val string
		style := txt
		switch f {
		case spamFieldSource:
			lbl, val = "Mode", "< "+spamSourceLabels[clampIndex(int(v.Source), len(spamSourceLabels))]+" >"
			style = lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
		case spamFieldValue:
			lbl, val = name, value
			if lbl == "File" && val != "" {
				val = filepath.Base(val)
			}
		case spamFieldCount:
			lbl, val = "Count", fmt.Sprintf("%d SSIDs", v.Count)
		case spamFieldChannel:
			lbl, val = "Channel", fmt.Sprintf("%d", v.Channel)
		case spamFieldInterval:
			lbl, val = "Delay", fmt.Sprintf("%d ms", v.IntervalMS)
		case spamFieldRandomMAC:
			lbl = "BSSID"
			if v.RandomMAC {
				val, style = "random each beacon", ok
			} else {
				val, style = "fixed", mut
			}
		case spamFieldRadio:
			lbl, val = "Radio", v.EffectiveIface()
		case spamFieldStart:
			lbl, val = "Action", "Start spam"
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
	if v.Mode == spamModeEdit {
		lines = append(lines, renderEmuInputBox(v.Input, w, pal, focused))
	} else if v.Err != "" && len(lines) < h {
		lines = append(lines, bad.Render(v.Marquee.FitLine("Err: "+v.Err, w)))
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
