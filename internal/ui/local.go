package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/localscan"
	"github.com/czhax/czhax/internal/theme"
)

type LocalMode int

const (
	localModeIfaces LocalMode = iota
	localModeOptions
	localModeRunning
	localModeResults
	localModeHostDetail
)

type LocalView struct {
	Mode          LocalMode
	Interfaces    []localscan.Interface
	LoadingIfaces bool
	IfaceCursor   int
	SelectedIface localscan.Interface

	OptionCursor int
	TargetHost   string // non-empty when scanning a single host from HostDetail
	NmapPath     string

	Scanning       bool
	ScanSeq        int
	ScanCancel     context.CancelFunc
	ScanProgressCh chan localscan.ScanProgress
	ScanStarted    time.Time
	ScanElapsed    time.Duration
	RunMode        localscan.ScanMode
	RunOptionName  string
	RunTarget      string
	RunTask        string
	RunPct         float64
	RunHasPct      bool
	RunLines       []string

	Result           localscan.ScanResult
	SubnetHosts      []localscan.Host
	SubnetTarget     string
	LastSubnetMode   localscan.ScanMode
	RunSingleHost    bool
	ScanErr          string
	HostCursor       int
	HostDetailScroll int

	Note      string
	NoteIsErr bool
	Marquee   Marquee
}

func NewLocalView() LocalView {
	return LocalView{
		NmapPath: localscan.FindNmap(),
	}
}

func (v *LocalView) SyncInterfaces(ifaces []localscan.Interface) {
	prevName := ""
	if v.IfaceCursor >= 0 && v.IfaceCursor < len(v.Interfaces) {
		prevName = v.Interfaces[v.IfaceCursor].Name
	}

	v.Interfaces = ifaces
	v.LoadingIfaces = false

	// Keep the previously highlighted interface if it is still connected.
	if prevName != "" {
		for i, inf := range ifaces {
			if inf.Name == prevName && inf.Connected {
				v.IfaceCursor = i
				if v.SelectedIface.Name == inf.Name {
					v.SelectedIface = inf
				}
				return
			}
		}
	}

	// Otherwise snap to the first connected interface (or -1 if none).
	v.IfaceCursor = v.firstConnectedIndex()
}

func (v LocalView) firstConnectedIndex() int {
	for i, inf := range v.Interfaces {
		if inf.Connected {
			return i
		}
	}
	return -1
}

func (v LocalView) ConnectedCount() int {
	n := 0
	for _, inf := range v.Interfaces {
		if inf.Connected {
			n++
		}
	}
	return n
}

func (v LocalView) HighlightedIface() (localscan.Interface, bool) {
	if v.IfaceCursor < 0 || v.IfaceCursor >= len(v.Interfaces) {
		return localscan.Interface{}, false
	}
	inf := v.Interfaces[v.IfaceCursor]
	if !inf.Connected {
		return localscan.Interface{}, false
	}
	return inf, true
}

// MoveIface moves the interface cursor across connected interfaces only,
// skipping greyed-out disconnected interfaces.
func (v *LocalView) MoveIface(delta int) {
	n := len(v.Interfaces)
	if n == 0 || v.ConnectedCount() == 0 {
		v.IfaceCursor = -1
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	cur := v.IfaceCursor
	if cur < 0 || cur >= n {
		cur = 0
		if step < 0 {
			cur = n - 1
		}
		if v.Interfaces[cur].Connected {
			v.IfaceCursor = cur
			return
		}
	}
	for tried := 0; tried < n; tried++ {
		cur = (cur + step + n) % n
		if v.Interfaces[cur].Connected {
			v.IfaceCursor = cur
			v.Note = ""
			return
		}
	}
	v.IfaceCursor = -1
}

func (v *LocalView) ConfirmIface() bool {
	inf, ok := v.HighlightedIface()
	if !ok {
		return false
	}
	v.SelectedIface = inf
	v.TargetHost = ""
	v.OptionCursor = 0
	v.ScanErr = ""
	v.Note = ""
	if v.NmapPath == "" {
		v.NmapPath = localscan.FindNmap()
	}
	v.Mode = localModeOptions
	return true
}

func (v LocalView) activeOptions() []localscan.ScanOption {
	if strings.TrimSpace(v.TargetHost) != "" {
		return localscan.HostScanOptions
	}
	return localscan.ScanOptions
}

func (v LocalView) optionCount() int {
	return len(v.activeOptions()) + 1 // +1 for Back
}

func (v *LocalView) MoveOption(delta int) {
	n := v.optionCount()
	v.OptionCursor = (v.OptionCursor + delta + n) % n
}

func (v LocalView) SelectedOption() (localscan.ScanOption, bool) {
	opts := v.activeOptions()
	if v.OptionCursor < 0 || v.OptionCursor >= len(opts) {
		return localscan.ScanOption{}, false
	}
	return opts[v.OptionCursor], true
}

func (v LocalView) IsBackOption() bool {
	return v.OptionCursor == len(v.activeOptions())
}

func (v LocalView) ActiveTarget() string {
	if strings.TrimSpace(v.TargetHost) != "" {
		return strings.TrimSpace(v.TargetHost)
	}
	return strings.TrimSpace(v.SelectedIface.ScanTarget)
}

func (v LocalView) ActiveTargetLabel() string {
	if strings.TrimSpace(v.TargetHost) != "" {
		return strings.TrimSpace(v.TargetHost) + " (host)"
	}
	t := strings.TrimSpace(v.SelectedIface.ScanTarget)
	n := strings.TrimSpace(v.SelectedIface.NetworkName)
	if n != "" && t != "" && !strings.EqualFold(n, t) {
		return t + " · " + n
	}
	if t != "" {
		return t
	}
	return "unknown"
}

func (v *LocalView) StartScan(opt localscan.ScanOption) (context.Context, int, localscan.ScanRequest, chan localscan.ScanProgress, bool) {
	if v.Scanning {
		return nil, 0, localscan.ScanRequest{}, nil, false
	}
	target := v.ActiveTarget()
	if target == "" {
		v.ScanErr = "No target subnet on interface"
		return nil, 0, localscan.ScanRequest{}, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.ScanSeq++
	v.Scanning = true
	v.ScanCancel = cancel
	v.ScanProgressCh = make(chan localscan.ScanProgress, 64)
	v.ScanStarted = time.Now()
	v.ScanElapsed = 0
	v.RunMode = opt.Mode
	v.RunOptionName = opt.Name
	v.RunTarget = target
	v.RunSingleHost = strings.TrimSpace(v.TargetHost) != ""
	if !v.RunSingleHost {
		v.LastSubnetMode = opt.Mode
	}
	v.RunTask = ""
	v.RunPct = 0
	v.RunHasPct = false
	v.RunLines = nil
	v.ScanErr = ""
	v.Result = localscan.ScanResult{Target: target, Mode: opt.Mode}
	v.HostCursor = 0
	v.HostDetailScroll = 0
	v.Mode = localModeRunning

	req := localscan.ScanRequest{
		Iface:      v.SelectedIface.Name,
		Target:     target,
		Mode:       opt.Mode,
		SingleHost: v.RunSingleHost,
	}
	return ctx, v.ScanSeq, req, v.ScanProgressCh, true
}

func (v *LocalView) StopScan() {
	if v.ScanCancel != nil {
		v.ScanCancel()
	}
}

func (v *LocalView) ApplyProgress(seq int, prog localscan.ScanProgress) {
	if !v.Scanning || seq != v.ScanSeq {
		return
	}
	if !v.ScanStarted.IsZero() {
		v.ScanElapsed = time.Since(v.ScanStarted)
	}
	if prog.Task != "" {
		v.RunTask = prog.Task
	}
	if prog.HasPct {
		v.RunPct = prog.Percent
		v.RunHasPct = true
	}
	if len(prog.Hosts) > 0 {
		v.Result.Hosts = prog.Hosts
	}
	if prog.Line != "" {
		v.RunLines = append(v.RunLines, prog.Line)
		if len(v.RunLines) > 100 {
			v.RunLines = v.RunLines[len(v.RunLines)-100:]
		}
	}
}

func (v *LocalView) FinishScan(seq int, res localscan.ScanResult, err error) {
	if seq != v.ScanSeq {
		return
	}
	v.Scanning = false
	v.ScanCancel = nil
	v.ScanProgressCh = nil
	if !v.ScanStarted.IsZero() {
		v.ScanElapsed = time.Since(v.ScanStarted)
	}
	if res.Target != "" {
		v.Result = res
	}
	if err != nil {
		if strings.Contains(err.Error(), "canceled") || res.Canceled {
			v.Result.Canceled = true
			v.ScanErr = ""
		} else {
			v.ScanErr = err.Error()
		}
	} else {
		v.ScanErr = ""
	}

	if v.RunMode != localscan.ScanDiscovery {
		for i := range v.Result.Hosts {
			v.Result.Hosts[i].PortsScanned = true
		}
	}

	if v.RunSingleHost {
		targetIP := strings.TrimSpace(v.RunTarget)
		v.TargetHost = ""
		v.RunSingleHost = false
		if len(v.Result.Hosts) > 0 {
			for _, scanned := range v.Result.Hosts {
				v.SubnetHosts = upsertHost(v.SubnetHosts, scanned)
			}
		} else if len(v.SubnetHosts) > 0 && targetIP != "" && v.ScanErr == "" && !v.Result.Canceled {
			// Single-host scan completed with 0 open ports and no <host> block: still mark PortsScanned.
			for i := range v.SubnetHosts {
				if v.SubnetHosts[i].IP == targetIP {
					v.SubnetHosts[i].PortsScanned = true
				}
			}
		}
		if len(v.SubnetHosts) > 0 {
			v.Result.Hosts = v.SubnetHosts
			if v.SubnetTarget != "" {
				v.Result.Target = v.SubnetTarget
			}
		}
		v.HostCursor = findHostIndex(v.Result.Hosts, targetIP)
		v.HostDetailScroll = 0
		if len(v.Result.Hosts) > 0 {
			v.Mode = localModeHostDetail
			return
		}
		v.Mode = localModeResults
		return
	}

	v.SubnetHosts = append([]localscan.Host(nil), v.Result.Hosts...)
	if v.Result.Target != "" {
		v.SubnetTarget = v.Result.Target
	}
	v.HostCursor = 0
	v.HostDetailScroll = 0
	v.Mode = localModeResults
}

func upsertHost(hosts []localscan.Host, scanned localscan.Host) []localscan.Host {
	for i := range hosts {
		if hosts[i].IP == scanned.IP {
			merged := hosts[i]
			if scanned.Hostname != "" {
				merged.Hostname = scanned.Hostname
			}
			if scanned.MAC != "" {
				merged.MAC = scanned.MAC
			}
			if scanned.Vendor != "" {
				merged.Vendor = scanned.Vendor
			}
			if scanned.OS != "" {
				merged.OS = scanned.OS
			}
			if scanned.Status != "" {
				merged.Status = scanned.Status
			}
			if scanned.Reason != "" {
				merged.Reason = scanned.Reason
			}
			if scanned.PortsScanned || len(scanned.Ports) > 0 {
				merged.PortsScanned = true
				merged.Ports = append([]localscan.Port(nil), scanned.Ports...)
			}
			hosts[i] = merged
			return hosts
		}
	}
	return append(hosts, scanned)
}

func findHostIndex(hosts []localscan.Host, ip string) int {
	for i, h := range hosts {
		if h.IP == ip {
			return i
		}
	}
	return 0
}

func (v *LocalView) MoveHost(delta int) {
	n := len(v.Result.Hosts)
	if n == 0 {
		v.HostCursor = 0
		return
	}
	v.HostCursor = (v.HostCursor + delta + n) % n
}

func (v LocalView) SelectedHost() (localscan.Host, bool) {
	if v.HostCursor < 0 || v.HostCursor >= len(v.Result.Hosts) {
		return localscan.Host{}, false
	}
	return v.Result.Hosts[v.HostCursor], true
}

func (v *LocalView) OpenHostDetail() bool {
	if _, ok := v.SelectedHost(); !ok {
		return false
	}
	v.HostDetailScroll = 0
	v.Mode = localModeHostDetail
	return true
}

func (v *LocalView) ScanSelectedHost() bool {
	h, ok := v.SelectedHost()
	if !ok {
		return false
	}
	v.TargetHost = h.IP
	v.OptionCursor = 0 // Quick Port Scan (-F) is first in HostScanOptions
	v.ScanErr = ""
	v.Mode = localModeOptions
	return true
}

func (v *LocalView) MoveHostDetailScroll(delta, w, h int, pal theme.Palette) {
	host, ok := v.SelectedHost()
	if !ok {
		v.HostDetailScroll = 0
		return
	}
	rows := v.buildHostRows(w, pal, host)
	bodyH := max(1, h-2)
	maxScroll := max(0, len(rows)-bodyH)
	v.HostDetailScroll += delta
	if v.HostDetailScroll < 0 {
		v.HostDetailScroll = 0
	}
	if v.HostDetailScroll > maxScroll {
		v.HostDetailScroll = maxScroll
	}
}

func (v *LocalView) HandleEsc() bool {
	switch v.Mode {
	case localModeRunning:
		if v.Scanning {
			v.StopScan()
		} else {
			v.Mode = localModeOptions
		}
		return true
	case localModeHostDetail:
		v.Mode = localModeResults
		return true
	case localModeResults:
		v.ScanErr = ""
		v.Mode = localModeOptions
		return true
	case localModeOptions:
		v.ScanErr = ""
		if v.TargetHost != "" {
			v.TargetHost = ""
			v.Mode = localModeHostDetail
		} else {
			v.Mode = localModeIfaces
		}
		return true
	default:
		return false
	}
}

func (v LocalView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	switch v.Mode {
	case localModeOptions:
		return v.renderOptions(w, h, pal, focused)
	case localModeRunning:
		return v.renderRunning(w, h, pal)
	case localModeResults:
		return v.renderResults(w, h, pal, focused)
	case localModeHostDetail:
		return v.renderHostDetail(w, h, pal, focused)
	default:
		return v.renderIfaces(w, h, pal, focused)
	}
}

func (v LocalView) renderIfaces(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	var badge string
	switch {
	case v.LoadingIfaces && len(v.Interfaces) == 0:
		badge = warn.Render("loading…")
	case len(v.Interfaces) > 0:
		badge = muted.Render(fmt.Sprintf("%d/%d up", v.ConnectedCount(), len(v.Interfaces)))
	default:
		badge = sec.Render("[r]") + muted.Render("Ref")
	}
	titleW := max(6, w-lipgloss.Width(badge)-3)
	header := " " + v.Marquee.FitLine(accent.Render("Select Interface"), titleW) + " " + badge + " "
	if h <= 1 {
		return v.Marquee.SplitLinesPadded([]string{header}, w, h)
	}

	showBottom := h >= 5
	listH := h - 1
	if showBottom {
		listH = h - 2
	}

	lines := []string{header}

	switch {
	case v.LoadingIfaces && len(v.Interfaces) == 0:
		lines = append(lines, " "+warn.Render(v.Marquee.Truncate("Checking interfaces...", max(6, w-2))))

	case len(v.Interfaces) == 0:
		lines = append(lines, " "+muted.Render("No interfaces found"))
		lines = append(lines, " "+muted.Render("Press r to refresh"))

	default:
		scrollIdx := v.IfaceCursor
		if scrollIdx < 0 {
			scrollIdx = 0
		}
		start, end := ScrollWindow(len(v.Interfaces), scrollIdx, listH)
		nameW := 7
		if w >= 44 {
			nameW = 9
		}
		netW := max(6, w-nameW-5)

		for i := start; i < end; i++ {
			inf := v.Interfaces[i]
			netDesc := inf.DisplayNetwork()

			// Disconnected interfaces are greyed out and strictly unselectable.
			if !inf.Connected {
				row := "   " + FitLine(inf.Name, nameW) + " " + v.Marquee.Truncate(netDesc, netW)
				lines = append(lines, muted.Render(Truncate(row, w)))
				continue
			}

			selected := i == v.IfaceCursor
			if selected && focused {
				row := " ▸ " + FitLine(inf.Name, nameW) + " " + v.Marquee.FitLine(netDesc, netW)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "   "
				if selected {
					pfx = accent.Render(" ▸ ")
				}
				lines = append(lines, pfx+txt.Render(FitLine(inf.Name, nameW))+" "+ok.Render(v.Marquee.Truncate(netDesc, netW)))
			}
		}
	}

	if showBottom {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		switch {
		case v.Note != "":
			st := ok
			if v.NoteIsErr {
				st = danger
			}
			lines = append(lines, " "+st.Render(v.Marquee.Truncate(v.Note, max(6, w-2))))
		case len(v.Interfaces) > 0 && v.ConnectedCount() == 0:
			lines = append(lines, " "+warn.Render(v.Marquee.Truncate("No connected interface · r:ref", max(6, w-2))))
		default:
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("↑↓:if ⏎:scan r:ref Esc:tabs", max(6, w-2))))
		}
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v LocalView) renderOptions(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	ifName := v.SelectedIface.Name
	if ifName == "" {
		ifName = "iface"
	}
	title := "Scan · " + ifName
	if strings.TrimSpace(v.TargetHost) != "" {
		title = "Port Scan · " + strings.TrimSpace(v.TargetHost)
	}
	lines := []string{
		" " + accent.Render(v.Marquee.Truncate(title, max(6, w-2))),
		" " + sec.Render("Net ") + txt.Render(v.Marquee.Truncate(v.ActiveTargetLabel(), max(6, w-6))),
	}

	hasErr := v.ScanErr != "" || v.NmapPath == ""
	listH := max(1, h-3)
	if hasErr {
		listH = max(1, h-4)
	}

	opts := v.activeOptions()
	total := v.optionCount()
	cursor := clampIndex(v.OptionCursor, total)
	start, end := ScrollWindow(total, cursor, listH)
	for i := start; i < end; i++ {
		var label, val string
		var isBack, isWarn bool
		if i < len(opts) {
			opt := opts[i]
			label = opt.Name
			val = opt.Flag
			isWarn = opt.Warn
		} else {
			label = "Back"
			val = "esc"
			isBack = true
		}
		nameW := max(8, w-len(val)-5)
		if i == cursor && focused {
			row := " ▸ " + FitLine(label, nameW) + " " + val
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if i == cursor {
				pfx = accent.Render(" ▸ ")
			}
			vSty := muted
			switch {
			case isBack:
				vSty = muted
			case i == 0:
				vSty = ok
			case isWarn:
				vSty = warn
			}
			lines = append(lines, pfx+txt.Render(FitLine(label, nameW))+" "+vSty.Render(val))
		}
	}

	if v.ScanErr != "" {
		lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.ScanErr, max(6, w-2))))
	} else if v.NmapPath == "" {
		lines = append(lines, " "+warn.Render(v.Marquee.Truncate("nmap not installed", max(6, w-2))))
	}

	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+muted.Render(v.Marquee.Truncate("↑↓:sel ⏎:run Esc:back", max(6, w-2))))
	if len(lines) > h && h >= 2 {
		lines = append(lines[:h-1], lines[len(lines)-1])
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v LocalView) renderRunning(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	elapsed := v.ScanElapsed
	if v.Scanning && !v.ScanStarted.IsZero() {
		elapsed = time.Since(v.ScanStarted)
	}
	title := v.RunOptionName
	if title == "" {
		title = "Nmap Scan"
	}
	status := fmt.Sprintf("%ds", int(elapsed.Seconds()))
	if v.RunHasPct {
		status += fmt.Sprintf(" · %.0f%%", v.RunPct)
	}
	if len(v.Result.Hosts) > 0 {
		status += fmt.Sprintf(" · %d up", len(v.Result.Hosts))
	}

	lines := []string{
		" " + accent.Render(v.Marquee.Truncate("Nmap · "+title, max(6, w-2))),
		" " + sec.Render("Target ") + txt.Render(v.Marquee.Truncate(v.RunTarget+" ("+v.SelectedIface.Name+")", max(6, w-9))),
		" " + warn.Render("scanning") + " " + muted.Render(v.Marquee.Truncate(status, max(6, w-11))),
	}

	avail := h - len(lines) - 1
	if avail < 0 {
		avail = 0
	}
	tail := v.RunLines
	if len(tail) > avail {
		tail = tail[len(tail)-avail:]
	}
	for _, ln := range tail {
		lines = append(lines, " "+txt.Render(v.Marquee.Truncate(ln, max(6, w-2))))
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+muted.Render(v.Marquee.Truncate("Esc:stop  q:stop", max(6, w-2))))
	if len(lines) > h && h >= 2 {
		lines = append(lines[:h-1], lines[len(lines)-1])
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v LocalView) renderResults(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	var badge string
	switch {
	case v.ScanErr != "":
		badge = danger.Render("err")
	case v.Result.Canceled:
		badge = muted.Render(fmt.Sprintf("%d up·stop", len(v.Result.Hosts)))
	default:
		badge = ok.Render(fmt.Sprintf("%d up", len(v.Result.Hosts)))
	}

	target := v.Result.Target
	if target == "" {
		target = v.ActiveTarget()
	}
	titleW := max(6, w-lipgloss.Width(badge)-3)
	lines := []string{
		" " + v.Marquee.FitLine(accent.Render("Hosts · "+target), titleW) + " " + badge + " ",
	}
	if h <= 1 {
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	showBottom := h >= 4
	listH := h - 1
	if showBottom {
		listH = h - 2
	}

	switch {
	case v.ScanErr != "" && len(v.Result.Hosts) == 0:
		lines = append(lines, " "+danger.Render("Scan failed:"))
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate(v.ScanErr, max(6, w-2))))
		lines = append(lines, " "+muted.Render("Press ⏎ to retry, Esc back"))

	case len(v.Result.Hosts) == 0:
		msg := "No live hosts found"
		if v.Result.Canceled {
			msg = "Scan stopped (0 hosts)"
		}
		lines = append(lines, " "+muted.Render(msg))
		lines = append(lines, " "+muted.Render("Press ⏎ to rescan, Esc back"))

	default:
		cursor := clampIndex(v.HostCursor, len(v.Result.Hosts))
		start, end := ScrollWindow(len(v.Result.Hosts), cursor, listH)
		ipW := 15
		if w < 30 {
			ipW = 12
		}
		infoW := max(4, w-ipW-5)

		for i := start; i < end; i++ {
			host := v.Result.Hosts[i]
			info := hostResultSummary(host)
			selected := i == cursor

			if selected && focused {
				row := " ▸ " + FitLine(host.IP, ipW) + " " + v.Marquee.FitLine(info, infoW)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "   "
				if selected {
					pfx = accent.Render(" ▸ ")
				}
				ipSty := txt
				infoSty := muted
				if len(host.Ports) > 0 {
					ipSty = ok
					infoSty = warn
				}
				lines = append(lines, pfx+ipSty.Render(FitLine(host.IP, ipW))+" "+infoSty.Render(v.Marquee.Truncate(info, infoW)))
			}
		}
	}

	if showBottom {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		hint := "⏎:rescan Esc:options"
		if len(v.Result.Hosts) > 0 {
			hint = "↑↓:host ⏎:ports s:scan Esc:opts"
		}
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate(hint, max(6, w-2))))
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func hostResultSummary(h localscan.Host) string {
	var parts []string
	if len(h.Ports) > 0 {
		parts = append(parts, fmt.Sprintf("%dp:%s", len(h.Ports), formatPortList(h.Ports)))
	} else if h.PortsScanned {
		parts = append(parts, "0p")
	}
	if h.Hostname != "" {
		parts = append(parts, h.Hostname)
	} else if h.Vendor != "" {
		parts = append(parts, h.Vendor)
	}
	if len(parts) == 0 {
		return "up"
	}
	return strings.Join(parts, " ")
}

func formatPortList(ports []localscan.Port) string {
	var nums []string
	for i, p := range ports {
		if i >= 3 {
			nums = append(nums, "…")
			break
		}
		nums = append(nums, strconv.Itoa(p.Number))
	}
	return strings.Join(nums, ",")
}

func (v LocalView) buildHostRows(w int, pal theme.Palette, host localscan.Host) []string {
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	valW := max(6, w-9)
	var rows []string

	if v.ScanErr != "" {
		rows = append(rows, " "+danger.Render("Err    ")+danger.Render(v.Marquee.Truncate(v.ScanErr, valW)))
	}
	if host.Hostname != "" {
		rows = append(rows, " "+sec.Render("Host   ")+txt.Render(v.Marquee.Truncate(host.Hostname, valW)))
	}
	if host.MAC != "" {
		rows = append(rows, " "+sec.Render("MAC    ")+txt.Render(v.Marquee.Truncate(host.MAC, valW)))
	}
	if host.Vendor != "" {
		rows = append(rows, " "+sec.Render("Vendor ")+txt.Render(v.Marquee.Truncate(host.Vendor, valW)))
	}
	if host.OS != "" {
		rows = append(rows, " "+sec.Render("OS     ")+warn.Render(v.Marquee.Truncate(host.OS, valW)))
	}
	if host.Reason != "" {
		rows = append(rows, " "+sec.Render("State  ")+ok.Render(host.Status)+" "+muted.Render(v.Marquee.Truncate("("+host.Reason+")", max(4, valW-len(host.Status)-1))))
	}
	switch {
	case len(host.Ports) > 0:
		rows = append(rows, " "+sec.Render("Ports  ")+ok.Render(fmt.Sprintf("%d open", len(host.Ports))))
		for _, p := range host.Ports {
			rows = append(rows, "  "+txt.Render(v.Marquee.Truncate(p.Summary(), max(6, w-3))))
		}
	case host.PortsScanned:
		rows = append(rows, " "+sec.Render("Ports  ")+muted.Render("0 open (scanned)"))
	default:
		rows = append(rows, " "+sec.Render("Ports  ")+muted.Render("not scanned (⏎/s:scan)"))
	}
	return rows
}

func (v LocalView) renderHostDetail(w, h int, pal theme.Palette, focused bool) []string {
	host, ok := v.SelectedHost()
	if !ok {
		return v.renderResults(w, h, pal, focused)
	}
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	okSty := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)

	rightPill := sec.Render("[s]") + okSty.Render("Scan")
	titleW := max(6, w-lipgloss.Width(rightPill)-3)
	lines := []string{
		" " + v.Marquee.FitLine(accent.Render("Host · "+host.IP), titleW) + " " + rightPill + " ",
	}
	if h <= 1 {
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	rows := v.buildHostRows(w, pal, host)
	bodyH := max(1, h-2)
	maxS := max(0, len(rows)-bodyH)
	sc := min(v.HostDetailScroll, maxS)
	end := min(len(rows), sc+bodyH)
	lines = append(lines, rows[sc:end]...)

	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+muted.Render(v.Marquee.Truncate("↑↓:scr s/⏎:scan ports Esc:back", max(6, w-2))))
	if len(lines) > h && h >= 2 {
		lines = append(lines[:h-1], lines[len(lines)-1])
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}
