package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

type WifiScanMode int

const (
	scanModeList WifiScanMode = iota
	scanModeActions
	scanModeDetail
	scanModePickIface
	scanModeSSID
	scanModePassword
	scanModePentest
)

type WifiScanAction int

const (
	wifiActConnect WifiScanAction = iota
	wifiActBoth
	wifiActHandshake
	wifiActDeauth
	wifiActDetail
	wifiActCount
)

type WifiScanView struct {
	Mode         WifiScanMode
	PrevMode     WifiScanMode
	DetailReturn WifiScanMode
	Interfaces   []wifi.Interface
	ScanIface    string
	Scanning     bool
	HasScanned   bool
	ScanErr      string
	Networks     []wifi.Network
	Cursor       int
	ActionCursor int
	DetailScroll int

	IfaceCursor  int
	ConnectIface string
	SSIDInput    string
	Password     string
	ShowPassword bool
	Connecting   bool
	ConnectErr   string

	PentestMode       wifi.PentestMode
	PentestReturn     WifiScanMode
	PentestActive     bool
	PentestStopping   bool
	PentestStarted    time.Time
	PentestDuration   time.Duration // listen window between deauth bursts (Both mode)
	PentestElapsed    time.Duration
	PentestResult     wifi.PentestResult
	PentestErr        string
	PentestCancel     context.CancelFunc
	PentestProgressCh chan wifi.PentestResult
	PentestTriggerCh  chan struct{}

	Note      string
	NoteIsErr bool
	Marquee   Marquee

	// Connected tracks the active link per interface so each adapter shows only
	// its own associated network.
	Connected map[string]wifi.Interface
}

// ActiveLink reports the network associated on iface (if any).
func (v WifiScanView) ActiveLink(iface string) (wifi.Interface, bool) {
	if v.Connected == nil {
		return wifi.Interface{}, false
	}
	inf, ok := v.Connected[iface]
	if !ok || strings.TrimSpace(inf.SSID) == "" {
		return wifi.Interface{}, false
	}
	return inf, true
}

// NetworkInUse reports whether n is the network the selected interface is
// associated with. Falls back to scan-provided data when the association for
// this interface is unknown.
func (v WifiScanView) NetworkInUse(n wifi.Network) bool {
	inf, ok := v.ActiveLink(v.EffectiveScanIface())
	if !ok {
		return n.InUse
	}
	if strings.TrimSpace(inf.SSID) != "" && strings.EqualFold(strings.TrimSpace(inf.SSID), strings.TrimSpace(n.SSID)) {
		return true
	}
	return false
}

func NewWifiScanView() WifiScanView {
	return WifiScanView{
		ScanIface:    "wlan0",
		ShowPassword: true,
	}
}

func (v WifiScanView) IsTextInput() bool {
	return v.Mode == scanModePassword || v.Mode == scanModeSSID
}

func (v WifiScanView) EffectiveScanIface() string {
	if strings.TrimSpace(v.ScanIface) != "" {
		return v.ScanIface
	}
	return "wlan0"
}

func (v WifiScanView) AvailableIfaces() []wifi.Interface {
	if len(v.Interfaces) > 0 {
		return v.Interfaces
	}
	return []wifi.Interface{{Name: v.EffectiveScanIface(), Mode: "managed"}}
}

func (v *WifiScanView) SyncInterfaces(ifaces []wifi.Interface) {
	v.Interfaces = ifaces
	if v.Connected == nil {
		v.Connected = make(map[string]wifi.Interface, len(ifaces))
	}
	present := make(map[string]bool, len(ifaces))
	for _, inf := range ifaces {
		present[inf.Name] = true
		v.Connected[inf.Name] = inf
	}
	for name := range v.Connected {
		if !present[name] {
			delete(v.Connected, name)
		}
	}
	if len(ifaces) == 0 {
		if strings.TrimSpace(v.ScanIface) == "" {
			v.ScanIface = "wlan0"
		}
		v.IfaceCursor = 0
		return
	}
	foundScan := false
	hasWlan0 := false
	for _, inf := range ifaces {
		if inf.Name == v.ScanIface {
			foundScan = true
		}
		if inf.Name == "wlan0" {
			hasWlan0 = true
		}
	}
	if !foundScan {
		if hasWlan0 {
			v.ScanIface = "wlan0"
		} else {
			v.ScanIface = ifaces[0].Name
		}
	}
	v.IfaceCursor = clampIndex(v.IfaceCursor, len(ifaces))
}

func (v *WifiScanView) CycleScanIface(delta int) {
	ifaces := v.AvailableIfaces()
	if len(ifaces) <= 1 {
		return
	}
	cur := 0
	for i, inf := range ifaces {
		if inf.Name == v.EffectiveScanIface() {
			cur = i
			break
		}
	}
	next := (cur + delta + len(ifaces)) % len(ifaces)
	v.ScanIface = ifaces[next].Name
	v.ScanErr = ""
	v.Note = ""
	v.NoteIsErr = false
	v.recomputeInUse()
}

func (v *WifiScanView) StartScan() {
	v.Scanning = true
	v.ScanErr = ""
	v.Note = ""
	v.NoteIsErr = false
}

func (v *WifiScanView) SetScanResult(iface string, nets []wifi.Network, err error) {
	v.Scanning = false
	v.HasScanned = true
	if err != nil {
		v.ScanErr = err.Error()
		v.Note = err.Error()
		v.NoteIsErr = true
		return
	}
	v.ScanErr = ""
	prevBSSID := ""
	prevSSID := ""
	if v.Cursor >= 0 && v.Cursor < len(v.Networks) {
		prevBSSID = v.Networks[v.Cursor].BSSID
		prevSSID = v.Networks[v.Cursor].SSID
	}
	v.Networks = nets
	v.recomputeInUse()
	v.Cursor = 0
	if prevBSSID != "" || prevSSID != "" {
		for i, n := range nets {
			if (prevBSSID != "" && n.BSSID == prevBSSID) || (prevBSSID == "" && n.SSID == prevSSID) {
				v.Cursor = i
				break
			}
		}
	}
	v.DetailScroll = 0
}

// recomputeInUse re-marks the current list against the selected interface's
// association.
func (v *WifiScanView) recomputeInUse() {
	for i := range v.Networks {
		v.Networks[i].InUse = v.NetworkInUse(v.Networks[i])
	}
}

func (v *WifiScanView) MoveNetwork(delta int) {	if len(v.Networks) == 0 {
		v.Cursor = 0
		return
	}
	v.Cursor = (v.Cursor + delta + len(v.Networks)) % len(v.Networks)
	v.Note = ""
	v.NoteIsErr = false
}

func (v WifiScanView) SelectedNetwork() (wifi.Network, bool) {
	if v.Cursor < 0 || v.Cursor >= len(v.Networks) {
		return wifi.Network{}, false
	}
	return v.Networks[v.Cursor], true
}

// StartPentest begins a handshake capture and/or deauth run against the
// selected AP. The returned context is cancelled by StopPentest or Esc.
func (v *WifiScanView) StartPentest(mode wifi.PentestMode) (context.Context, wifi.PentestRequest, bool) {
	net, ok := v.SelectedNetwork()
	if !ok || v.PentestActive || strings.TrimSpace(net.BSSID) == "" {
		return nil, wifi.PentestRequest{}, false
	}
	// Both mode listens for 30s between deauth bursts; the other modes run
	// until the handshake is captured or the user stops them.
	duration := 30 * time.Second
	if mode != wifi.PentestBoth {
		duration = 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.PentestCancel = cancel
	v.PentestReturn = v.Mode
	v.Mode = scanModePentest
	v.PentestMode = mode
	v.PentestActive = true
	v.PentestStopping = false
	v.PentestStarted = time.Now()
	v.PentestDuration = duration
	v.PentestElapsed = 0
	v.PentestResult = wifi.PentestResult{}
	v.PentestErr = ""
	v.PentestProgressCh = make(chan wifi.PentestResult, 1)
	v.PentestTriggerCh = make(chan struct{}, 8)
	v.Note = ""
	v.NoteIsErr = false
	req := wifi.PentestRequest{
		ScanIface:     v.EffectiveScanIface(),
		BSSID:         net.BSSID,
		SSID:          net.SSID,
		Channel:       wifi.ChannelNumber(net),
		Mode:          mode,
		Duration:      duration,
		DeauthTrigger: v.PentestTriggerCh,
	}
	return ctx, req, true
}

// TriggerDeauth asks a running deauth-mode pentest to inject one burst. Keys
// held down generate repeated key events, so holding a key keeps deauthing.
func (v *WifiScanView) TriggerDeauth() bool {
	if !v.PentestActive || v.PentestMode != wifi.PentestDeauth || v.PentestTriggerCh == nil {
		return false
	}
	select {
	case v.PentestTriggerCh <- struct{}{}:
		return true
	default:
		return false
	}
}

func (v *WifiScanView) StopPentest() {
	if v.PentestCancel != nil {
		v.PentestCancel()
	}
	v.PentestStopping = true
}

func (v *WifiScanView) SetPentestProgress(res wifi.PentestResult) {
	if !v.PentestActive {
		return
	}
	v.PentestResult = res
	if !v.PentestStarted.IsZero() {
		v.PentestElapsed = time.Since(v.PentestStarted)
	}
}

func (v *WifiScanView) SetPentestResult(res wifi.PentestResult, err error) {
	v.PentestActive = false
	v.PentestStopping = false
	v.PentestProgressCh = nil
	v.PentestTriggerCh = nil
	if v.PentestCancel != nil {
		v.PentestCancel()
		v.PentestCancel = nil
	}
	v.PentestResult = res
	if !v.PentestStarted.IsZero() {
		v.PentestElapsed = time.Since(v.PentestStarted)
	}
	switch {
	case err != nil && errors.Is(err, context.Canceled):
		v.PentestErr = ""
		v.Note = "Stopped"
		v.NoteIsErr = false
	case err != nil:
		v.PentestErr = err.Error()
		v.Note = "Pentest failed"
		v.NoteIsErr = true
	case res.HandshakeComplete:
		v.Note = "Handshake captured"
		v.NoteIsErr = false
	case res.EAPOLFrames > 0:
		v.Note = fmt.Sprintf("Captured %d EAPOL frames", res.EAPOLFrames)
		v.NoteIsErr = false
	case res.DeauthFrames > 0:
		v.Note = fmt.Sprintf("Sent %d deauth frames", res.DeauthFrames)
		v.NoteIsErr = false
	default:
		v.Note = "No handshake captured"
		v.NoteIsErr = true
	}
}

func (v *WifiScanView) OpenActions() bool {
	if _, ok := v.SelectedNetwork(); !ok {
		return false
	}
	v.Mode = scanModeActions
	v.ActionCursor = 0
	v.ConnectErr = ""
	v.Note = ""
	v.NoteIsErr = false
	return true
}

func (v *WifiScanView) MoveAction(delta int) {
	v.ActionCursor = (v.ActionCursor + delta + int(wifiActCount)) % int(wifiActCount)
	v.Note = ""
	v.NoteIsErr = false
}

func (v *WifiScanView) OpenDetail(fromMode WifiScanMode) bool {
	if _, ok := v.SelectedNetwork(); !ok {
		return false
	}
	v.DetailReturn = fromMode
	v.Mode = scanModeDetail
	v.DetailScroll = 0
	v.ConnectErr = ""
	v.Note = ""
	v.NoteIsErr = false
	return true
}

func (v *WifiScanView) MoveDetailScroll(delta, w, h int, pal theme.Palette) {
	net, ok := v.SelectedNetwork()
	if !ok {
		v.DetailScroll = 0
		return
	}
	detailH := max(1, h-1)
	if h >= 10 {
		detailH = max(1, h-2)
	}
	total := len(v.buildDetailRows(w, pal, net))
	maxScroll := max(0, total-detailH)
	v.DetailScroll += delta
	if v.DetailScroll < 0 {
		v.DetailScroll = 0
	}
	if v.DetailScroll > maxScroll {
		v.DetailScroll = maxScroll
	}
}

func (v *WifiScanView) BeginConnect(fromMode WifiScanMode) bool {
	if _, ok := v.SelectedNetwork(); !ok || v.Connecting {
		return false
	}
	v.PrevMode = fromMode
	v.Mode = scanModePickIface
	v.ConnectErr = ""
	v.Password = ""
	v.SSIDInput = ""
	v.ShowPassword = true
	v.Note = ""
	v.NoteIsErr = false

	ifaces := v.AvailableIfaces()
	v.IfaceCursor = 0
	target := v.EffectiveScanIface()
	for i, inf := range ifaces {
		if inf.Name == target {
			v.IfaceCursor = i
			break
		}
	}
	return true
}

func (v *WifiScanView) MoveIfaceCursor(delta int) {
	ifaces := v.AvailableIfaces()
	if len(ifaces) == 0 {
		v.IfaceCursor = 0
		return
	}
	v.IfaceCursor = (v.IfaceCursor + delta + len(ifaces)) % len(ifaces)
}

func (v *WifiScanView) ConfirmConnectIface() (startConnect bool) {
	ifaces := v.AvailableIfaces()
	idx := clampIndex(v.IfaceCursor, len(ifaces))
	v.ConnectIface = ifaces[idx].Name
	v.ConnectErr = ""

	net, ok := v.SelectedNetwork()
	if !ok {
		v.Mode = scanModeList
		return false
	}
	if strings.TrimSpace(net.SSID) == "" {
		v.Mode = scanModeSSID
		v.SSIDInput = ""
		return false
	}
	if strings.EqualFold(net.ShortSecurity(), "Open") {
		v.Connecting = true
		return true
	}
	v.Mode = scanModePassword
	v.Password = ""
	return false
}

func (v *WifiScanView) HandleTextKey(msg tea.KeyMsg) (submit bool, handled bool) {
	if !v.IsTextInput() || v.Connecting {
		return false, false
	}

	switch msg.Type {
	case tea.KeyEnter:
		if v.Mode == scanModeSSID {
			if strings.TrimSpace(v.SSIDInput) == "" {
				v.ConnectErr = "Enter an SSID first"
				return false, true
			}
			v.ConnectErr = ""
			net, _ := v.SelectedNetwork()
			if strings.EqualFold(net.ShortSecurity(), "Open") {
				v.Connecting = true
				return true, true
			}
			v.Mode = scanModePassword
			return false, true
		}
		v.Connecting = true
		v.ConnectErr = ""
		return true, true

	case tea.KeyTab:
		if v.Mode == scanModePassword {
			v.ShowPassword = !v.ShowPassword
		}
		return false, true

	case tea.KeyBackspace, tea.KeyDelete:
		if v.Mode == scanModeSSID {
			v.SSIDInput = dropLastRune(v.SSIDInput)
		} else {
			v.Password = dropLastRune(v.Password)
		}
		v.ConnectErr = ""
		return false, true

	case tea.KeyCtrlU:
		if v.Mode == scanModeSSID {
			v.SSIDInput = ""
		} else {
			v.Password = ""
		}
		v.ConnectErr = ""
		return false, true

	case tea.KeySpace:
		if v.Mode == scanModeSSID {
			v.SSIDInput += " "
		} else {
			v.Password += " "
		}
		v.ConnectErr = ""
		return false, true

	case tea.KeyRunes:
		s := string(msg.Runes)
		if v.Mode == scanModeSSID {
			v.SSIDInput += s
		} else {
			v.Password += s
		}
		v.ConnectErr = ""
		return false, true
	}

	return false, true
}

func (v WifiScanView) BuildConnectRequest() wifi.ConnectRequest {
	net, _ := v.SelectedNetwork()
	ssid := net.SSID
	hidden := false
	if strings.TrimSpace(ssid) == "" {
		ssid = strings.TrimSpace(v.SSIDInput)
		hidden = true
	}
	iface := strings.TrimSpace(v.ConnectIface)
	if iface == "" {
		iface = v.EffectiveScanIface()
	}
	return wifi.ConnectRequest{
		Iface:    iface,
		SSID:     ssid,
		BSSID:    net.BSSID,
		Password: v.Password,
		Security: net.Security,
		Hidden:   hidden,
	}
}

func (v *WifiScanView) SetConnectResult(req wifi.ConnectRequest, err error) {
	v.Connecting = false
	if err != nil {
		v.ConnectErr = err.Error()
		v.Note = err.Error()
		v.NoteIsErr = true
		return
	}
	v.Password = ""
	v.SSIDInput = ""
	v.ConnectErr = ""
	name := req.SSID
	if name == "" {
		name = req.BSSID
	}
	v.Note = fmt.Sprintf("Connected %s (%s)", name, req.Iface)
	v.NoteIsErr = false

	// Remember the association for this interface only: another adapter must
	// keep showing its own network as the connected one.
	if v.Connected == nil {
		v.Connected = make(map[string]wifi.Interface)
	}
	inf := v.Connected[req.Iface]
	inf.Name = req.Iface
	inf.SSID = req.SSID
	v.Connected[req.Iface] = inf
	for i := range v.Networks {
		v.Networks[i].InUse = v.NetworkInUse(v.Networks[i])
	}

	if v.PrevMode == scanModeDetail {
		v.Mode = scanModeDetail
	} else {
		v.Mode = scanModeList
	}
}

func (v *WifiScanView) HandleEsc() bool {
	switch v.Mode {
	case scanModePentest:
		if v.PentestActive {
			v.StopPentest()
			return true
		}
		v.PentestErr = ""
		v.Mode = v.PentestReturn
		return true
	case scanModePassword:
		v.Password = ""
		v.ConnectErr = ""
		v.Connecting = false
		if net, ok := v.SelectedNetwork(); ok && strings.TrimSpace(net.SSID) == "" {
			v.Mode = scanModeSSID
		} else {
			v.Mode = scanModePickIface
		}
		return true
	case scanModeSSID:
		v.SSIDInput = ""
		v.ConnectErr = ""
		v.Connecting = false
		v.Mode = scanModePickIface
		return true
	case scanModePickIface:
		v.ConnectErr = ""
		v.Connecting = false
		v.Mode = v.PrevMode
		return true
	case scanModeDetail:
		v.ConnectErr = ""
		if v.DetailReturn != scanModeList {
			v.Mode = v.DetailReturn
		} else {
			v.Mode = scanModeList
		}
		return true
	case scanModeActions:
		v.ConnectErr = ""
		v.Note = ""
		v.NoteIsErr = false
		v.Mode = scanModeList
		return true
	default:
		return false
	}
}

func (v WifiScanView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	switch v.Mode {
	case scanModeActions:
		return v.renderActions(w, h, pal, focused)
	case scanModeDetail:
		return v.renderDetail(w, h, pal, focused)
	case scanModePickIface:
		return v.renderPickIface(w, h, pal, focused)
	case scanModeSSID:
		return v.renderSSIDInput(w, h, pal)
	case scanModePassword:
		return v.renderPasswordInput(w, h, pal)
	case scanModePentest:
		return v.renderPentest(w, h, pal)
	default:
		return v.renderList(w, h, pal, focused)
	}
}

func (v WifiScanView) renderList(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	ifaces := v.AvailableIfaces()
	ifaceName := v.EffectiveScanIface()
	ifaceBands := ""
	for _, inf := range ifaces {
		if inf.Name == ifaceName {
			ifaceBands = inf.Bands
			break
		}
	}
	ifacePart := sec.Render("If:") + accent.Render(v.Marquee.Truncate(ifaceName, max(5, w-18)))
	if len(ifaces) > 1 {
		ifacePart += muted.Render("◂▸")
	}

	var rightPart string
	switch {
	case v.Connecting:
		rightPart = warn.Render("conn…")
	case v.Scanning:
		rightPart = warn.Render("scanning…")
	case v.ScanErr != "":
		rightPart = danger.Render("err ") + sec.Render("[s]") + muted.Render("Scan")
	case v.HasScanned:
		rightPart = muted.Render(fmt.Sprintf("%d APs ", len(v.Networks))) + sec.Render("[s]") + muted.Render("Scan")
	default:
		rightPart = sec.Render("[s]") + muted.Render("Scan")
	}

	rightW := lipgloss.Width(rightPart)
	leftMax := max(6, w-rightW-3)
	if ifaceBands != "" && lipgloss.Width(ifacePart)+1+len(ifaceBands) <= leftMax {
		ifacePart += " " + muted.Render(ifaceBands)
	}
	header := " " + v.Marquee.FitLine(ifacePart, leftMax) + " " + rightPart + " "
	if h <= 1 {
		return v.Marquee.SplitLinesPadded([]string{header}, w, h)
	}

	showBottom := (h >= 11 || v.Note != "") && h >= 4
	listH := h - 1
	if showBottom {
		listH = h - 2
	}

	lines := []string{header}

	switch {
	case v.Scanning && len(v.Networks) == 0:
		lines = append(lines, " "+warn.Render(v.Marquee.Truncate("Scanning "+ifaceName+"...", max(6, w-2))))

	case v.ScanErr != "" && len(v.Networks) == 0:
		lines = append(lines, " "+danger.Render("Scan failed:"))
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate(v.ScanErr, max(6, w-2))))
		lines = append(lines, " "+muted.Render("Press s to retry"))

	case len(v.Networks) == 0:
		if !v.HasScanned {
			lines = append(lines, " "+muted.Render("No scan yet"))
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("Press s or ⏎ to scan "+ifaceName, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("No networks on "+ifaceName, max(6, w-2))))
			lines = append(lines, " "+muted.Render("Press s to rescan"))
		}

	default:
		start, end := ScrollWindow(len(v.Networks), v.Cursor, listH)
		for i := start; i < end; i++ {
			n := v.Networks[i]
			selected := i == v.Cursor

			secLbl := compactSec(n.ShortSecurity(), w)
			sigTxt := fmt.Sprintf("%3d%%", n.Signal)
			rightPlain := sigTxt + " " + secLbl
			rightPlainW := lipgloss.Width(rightPlain)
			ssidW := max(4, w-2-1-rightPlainW-1)
			ssidTxt := v.Marquee.FitLine(n.DisplaySSID(), ssidW)
			inUse := v.NetworkInUse(n)

			if selected && focused {
				mark := " "
				if inUse {
					mark = "*"
				}
				row := fmt.Sprintf("▸%s%s %s ", mark, ssidTxt, rightPlain)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := " "
				if selected {
					pfx = accent.Render("▸")
				}
				mark := " "
				if inUse {
					mark = ok.Render("*")
				}
				ssidStyled := txt.Render(ssidTxt)
				if strings.TrimSpace(n.SSID) == "" {
					ssidStyled = muted.Render(ssidTxt)
				}
				sigStyle := danger
				if n.Signal >= 60 {
					sigStyle = ok
				} else if n.Signal >= 30 {
					sigStyle = warn
				}
				row := pfx + mark + ssidStyled + " " + sigStyle.Render(sigTxt) + " " + muted.Render(secLbl)
				lines = append(lines, row)
			}
		}
	}

	if showBottom {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		if v.Note != "" {
			st := ok
			if v.NoteIsErr {
				st = danger
			}
			lines = append(lines, " "+st.Render(v.Marquee.Truncate(v.Note, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("⏎:actions  s:scan  Esc:tabs", max(6, w-2))))
		}
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v WifiScanView) renderActions(w, h int, pal theme.Palette, focused bool) []string {
	net, ok := v.SelectedNetwork()
	if !ok {
		return v.renderList(w, h, pal, focused)
	}

	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	okStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	rightPlain := fmt.Sprintf("%d%% %s", net.Signal, compactSec(net.ShortSecurity(), w))
	titleW := max(6, w-len([]rune(rightPlain))-3)
	sigStyle := danger
	if net.Signal >= 60 {
		sigStyle = okStyle
	} else if net.Signal >= 30 {
		sigStyle = warn
	}
	header := " " + FitLine(muted.Render("◂ ")+accent.Render(v.Marquee.Truncate(net.DisplaySSID(), max(4, titleW-2))), titleW) +
		" " + sigStyle.Render(fmt.Sprintf("%d%%", net.Signal)) + " " + muted.Render(compactSec(net.ShortSecurity(), w)) + " "
	if h <= 1 {
		return v.Marquee.SplitLinesPadded([]string{header}, w, h)
	}

	lines := []string{header}
	showSummary := h >= 8
	if showSummary {
		sub := joinNonEmpty(" · ", net.BSSID, prefixIfNonEmpty("ch ", net.Channel))
		if sub == "" {
			sub = net.ShortSecurity()
		}
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate(sub, max(6, w-2))))
	}

	type actItem struct {
		act  WifiScanAction
		name string
		val  string
		warn bool
		ok   bool
	}
	items := []actItem{
		{act: wifiActConnect, name: "Connect", val: "join", ok: true},
		{act: wifiActBoth, name: "Deauth + Capture", val: "loop", warn: true},
		{act: wifiActHandshake, name: "Handshake Capture", val: "until HS"},
		{act: wifiActDeauth, name: "Deauth AP", val: "manual", warn: true},
		{act: wifiActDetail, name: "Network Details", val: "info"},
	}

	showBottom := (h >= 8 || v.Note != "") && h >= 4
	listH := h - len(lines)
	if showBottom {
		listH--
	}
	if listH < 1 {
		listH = 1
	}

	cursor := clampIndex(v.ActionCursor, len(items))
	start, end := ScrollWindow(len(items), cursor, listH)
	for i := start; i < end; i++ {
		it := items[i]
		selected := i == cursor
		nameW := max(8, w-len(it.val)-5)
		if selected && focused {
			row := fmt.Sprintf(" ▸ %s %s", v.Marquee.FitLine(it.name, nameW), it.val)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if selected {
				pfx = accent.Render(" ▸ ")
			}
			valStyle := muted
			if it.ok {
				valStyle = okStyle
			} else if it.warn {
				valStyle = warn
			}
			lines = append(lines, fmt.Sprintf("%s%s %s", pfx, txt.Render(v.Marquee.FitLine(it.name, nameW)), valStyle.Render(it.val)))
		}
	}

	if showBottom {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		if v.Note != "" {
			st := okStyle
			if v.NoteIsErr {
				st = danger
			}
			lines = append(lines, " "+st.Render(v.Marquee.Truncate(v.Note, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("↑↓:sel  ⏎:run  Esc:back", max(6, w-2))))
		}
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v WifiScanView) buildDetailRows(w int, pal theme.Palette, net wifi.Network) []string {
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	valW := max(6, w-8)

	ssidVal := txt.Render(v.Marquee.Truncate(net.DisplaySSID(), valW))
	if strings.TrimSpace(net.SSID) == "" {
		ssidVal = muted.Render("<hidden>")
	}

	bssidVal := muted.Render("n/a")
	if net.BSSID != "" {
		bssidVal = txt.Render(v.Marquee.Truncate(net.BSSID, valW))
	}

	sigStr := fmt.Sprintf("%d%%", net.Signal)
	if net.HasRSSI {
		sigStr = fmt.Sprintf("%d%% (%d dBm)", net.Signal, net.RSSI)
	}
	sigStyle := danger
	if net.Signal >= 60 {
		sigStyle = ok
	} else if net.Signal >= 30 {
		sigStyle = warn
	}

	secStr := net.Security
	if strings.TrimSpace(secStr) == "" {
		secStr = "Open"
	}

	stateVal := muted.Render("Available")
	if v.NetworkInUse(net) {
		stateVal = ok.Render("Connected")
	}

	rows := []string{
		" " + sec.Render("SSID   ") + ssidVal,
		" " + sec.Render("BSSID  ") + bssidVal,
		" " + sec.Render("Signal ") + sigStyle.Render(v.Marquee.Truncate(sigStr, valW)),
		" " + sec.Render("Sec    ") + txt.Render(v.Marquee.Truncate(secStr, valW)),
		" " + sec.Render("State  ") + stateVal,
	}

	chanStr := joinNonEmpty(" · ", prefixIfNonEmpty("ch ", net.Channel), net.Freq)
	if chanStr != "" {
		rows = append(rows, " "+sec.Render("Chan   ")+txt.Render(v.Marquee.Truncate(chanStr, valW)))
	}
	if net.Rate != "" {
		rows = append(rows, " "+sec.Render("Rate   ")+txt.Render(v.Marquee.Truncate(net.Rate, valW)))
	}
	if net.Mode != "" {
		rows = append(rows, " "+sec.Render("Mode   ")+txt.Render(v.Marquee.Truncate(net.Mode, valW)))
	}
	rows = append(rows, " "+sec.Render("ScanIf ")+muted.Render(v.Marquee.Truncate(v.EffectiveScanIface(), valW)))

	return rows
}

func (v WifiScanView) renderDetail(w, h int, pal theme.Palette, focused bool) []string {
	net, ok := v.SelectedNetwork()
	if !ok {
		return v.renderList(w, h, pal, focused)
	}

	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	okStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)

	action := sec.Render("[⏎]") + okStyle.Render("Conn")
	actionW := lipgloss.Width(action)
	titleW := max(6, w-actionW-3)
	header := " " + FitLine(muted.Render("◂ ")+accent.Render(v.Marquee.Truncate(net.DisplaySSID(), max(4, titleW-2))), titleW) + " " + action + " "
	if h <= 1 {
		return v.Marquee.SplitLinesPadded([]string{header}, w, h)
	}

	showBottom := (h >= 10 || v.Note != "") && h >= 4
	detailH := h - 1
	if showBottom {
		detailH = h - 2
	}

	rows := v.buildDetailRows(w, pal, net)
	maxS := max(0, len(rows)-detailH)
	sc := min(v.DetailScroll, maxS)
	end := min(len(rows), sc+detailH)

	lines := []string{header}
	lines = append(lines, rows[sc:end]...)

	if showBottom {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		if v.Note != "" {
			st := okStyle
			if v.NoteIsErr {
				st = danger
			}
			lines = append(lines, " "+st.Render(v.Marquee.Truncate(v.Note, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("↑↓:scroll  ⏎:conn  Esc:back", max(6, w-2))))
		}
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v WifiScanView) renderPickIface(w, h int, pal theme.Palette, focused bool) []string {
	net, _ := v.SelectedNetwork()
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	lines := []string{
		" " + accent.Render(v.Marquee.Truncate("Connect: "+net.DisplaySSID(), max(6, w-2))),
	}
	if h <= 1 {
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	if v.Connecting {
		lines = append(lines, " "+warn.Render(v.Marquee.Truncate("Connecting via "+v.ConnectIface+"...", max(6, w-2))))
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	lines = append(lines, " "+sec.Render("Use which interface?"))
	if h <= 2 {
		return SplitLinesPadded(lines, w, h)
	}

	showBottom := h >= 5
	listH := h - 2
	if showBottom {
		listH = h - 3
	}

	ifaces := v.AvailableIfaces()
	cursor := clampIndex(v.IfaceCursor, len(ifaces))
	start, end := ScrollWindow(len(ifaces), cursor, max(1, listH))
	for i := start; i < end; i++ {
		inf := ifaces[i]
		selected := i == cursor

		stPlain := "DOWN"
		if inf.Up {
			if inf.SSID != "" {
				stPlain = "UP:" + inf.SSID
			} else {
				stPlain = "UP"
			}
		} else if strings.EqualFold(inf.Mode, "monitor") {
			stPlain = "MON"
		}

		maxStW := max(4, w/2-1)
		stPlain = v.Marquee.Truncate(stPlain, maxStW)
		nameW := max(5, w-5-lipgloss.Width(stPlain))
		nameTxt := v.Marquee.FitLine(inf.Name, nameW)

		if selected && focused {
			row := fmt.Sprintf(" ▸ %s %s ", nameTxt, stPlain)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if selected {
				pfx = accent.Render(" ▸ ")
			}
			stStyle := danger
			if inf.Up {
				stStyle = ok
			} else if strings.EqualFold(inf.Mode, "monitor") {
				stStyle = warn
			}
			lines = append(lines, pfx+txt.Render(nameTxt)+" "+stStyle.Render(stPlain))
		}
	}

	if showBottom {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		if v.ConnectErr != "" {
			lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.ConnectErr, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("⏎:choose  Esc:back", max(6, w-2))))
		}
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v WifiScanView) renderSSIDInput(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	net, _ := v.SelectedNetwork()
	lines := []string{
		" " + accent.Render(v.Marquee.Truncate("Hidden AP ("+v.ConnectIface+")", max(6, w-2))),
	}
	if v.Connecting {
		lines = append(lines, " "+warn.Render(v.Marquee.Truncate("Connecting via "+v.ConnectIface+"...", max(6, w-2))))
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	lines = append(lines, " "+sec.Render("Enter SSID:"))
	fieldW := max(6, w-5)
	lines = append(lines, " > "+txt.Render(tailWindow(v.SSIDInput, fieldW-1)+"_"))
	if net.BSSID != "" {
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate("BSSID: "+net.BSSID, max(6, w-2))))
	}
	if v.ConnectErr != "" {
		lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.ConnectErr, max(6, w-2))))
	}
	if h >= 6 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate("⏎:next  Esc:back", max(6, w-2))))
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v WifiScanView) renderPasswordInput(w, h int, pal theme.Palette) []string {
	net, _ := v.SelectedNetwork()
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	ssid := net.SSID
	if strings.TrimSpace(ssid) == "" {
		ssid = v.SSIDInput
	}

	lines := []string{
		" " + accent.Render(v.Marquee.Truncate(ssid+" ("+v.ConnectIface+")", max(6, w-2))),
	}
	if v.Connecting {
		lines = append(lines, " "+warn.Render(v.Marquee.Truncate("Connecting "+ssid+"...", max(6, w-2))))
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate("Interface: "+v.ConnectIface, max(6, w-2))))
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	lines = append(lines, " "+sec.Render(v.Marquee.Truncate("Password (blank=saved):", max(6, w-2))))

	disp := v.Password
	if !v.ShowPassword {
		disp = strings.Repeat("•", utf8.RuneCountInString(v.Password))
	}
	fieldW := max(6, w-5)
	lines = append(lines, " > "+txt.Render(tailWindow(disp, fieldW-1)+"_"))
	lines = append(lines, " "+muted.Render(v.Marquee.Truncate("Sec: "+net.ShortSecurity(), max(6, w-2))))

	if v.ConnectErr != "" {
		lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.ConnectErr, max(6, w-2))))
	}

	if h >= 6 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		visLbl := "Tab:show"
		if v.ShowPassword {
			visLbl = "Tab:hide"
		}
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate("⏎:conn  "+visLbl+"  Esc:back", max(6, w-2))))
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v WifiScanView) renderPentest(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	net, _ := v.SelectedNetwork()
	res := v.PentestResult

	lines := []string{
		" " + accent.Render(v.Marquee.Truncate("Pentest · "+v.PentestMode.String(), max(6, w-2))),
	}
	ssid := net.DisplaySSID()
	if strings.TrimSpace(net.SSID) == "" {
		ssid = "<hidden>"
	}
	lines = append(lines, " "+sec.Render("AP     ")+txt.Render(v.Marquee.Truncate(ssid, max(6, w-8))))

	if h >= 4 {
		iface := res.MonitorIface
		if iface == "" {
			iface = v.EffectiveScanIface()
		}
		ch := res.Channel
		if ch == 0 {
			ch = wifi.ChannelNumber(net)
		}
		radio := iface
		if ch > 0 {
			radio += fmt.Sprintf(" · ch %d", ch)
		}
		lines = append(lines, " "+sec.Render("Radio  ")+txt.Render(v.Marquee.Truncate(radio, max(6, w-8))))
	}

	if h >= 5 {
		state := muted.Render("done")
		phase := ""
		switch {
		case v.PentestActive && v.PentestStopping:
			state = warn.Render("stopping…")
		case v.PentestActive:
			elapsed := v.PentestElapsed
			if elapsed <= 0 && !v.PentestStarted.IsZero() {
				elapsed = time.Since(v.PentestStarted)
			}
			switch v.PentestMode {
			case wifi.PentestDeauth:
				state = warn.Render("ready")
				phase = "any key = deauth"
			case wifi.PentestBoth:
				state = warn.Render("listening")
				window := v.PentestDuration
				if window <= 0 {
					window = 30 * time.Second
				}
				next := window - (elapsed % window)
				phase = fmt.Sprintf("deauth in %ds", int(next.Seconds()+0.5))
			default:
				state = warn.Render("listening")
				phase = "until handshake"
			}
			phase = fmt.Sprintf("%ds · %s", int(elapsed.Seconds()), phase)
		case v.PentestErr != "":
			state = danger.Render("failed")
		case res.HandshakeComplete:
			state = ok.Render("handshake")
		case res.EAPOLFrames > 0:
			state = ok.Render("eapol")
		case res.DeauthFrames > 0:
			state = ok.Render("sent")
		case v.Note == "Stopped":
			state = muted.Render("stopped")
		case v.Note == "No handshake captured":
			state = warn.Render("no capture")
		}
		line := " " + state
		if phase != "" {
			line += " " + muted.Render(v.Marquee.Truncate(phase, max(6, w-4-lipgloss.Width(state))))
		}
		lines = append(lines, line)
	}

	if h >= 6 {
		parts := []string{sec.Render("EAPOL ") + txt.Render(fmt.Sprintf("%d", res.EAPOLFrames))}
		if v.PentestMode != wifi.PentestHandshake || res.DeauthFrames > 0 {
			parts = append(parts, sec.Render("Deauth ")+txt.Render(fmt.Sprintf("%d", res.DeauthFrames)))
		}
		lines = append(lines, " "+strings.Join(parts, muted.Render(" · ")))
	}

	if v.PentestErr != "" {
		lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.PentestErr, max(6, w-2))))
	} else if res.CapturePath != "" && !v.PentestActive {
		lines = append(lines, " "+ok.Render("Saved ")+muted.Render(v.Marquee.Truncate(filepath.Base(res.CapturePath), max(6, w-8))))
	}

	if h >= 4 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		hint := "Esc:back"
		if v.PentestActive {
			hint = "Esc:stop"
			if v.PentestMode == wifi.PentestDeauth {
				hint = "any key:deauth  Esc:stop"
			}
		}
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate(hint, max(6, w-2))))
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func compactSec(s string, w int) string {
	if s == "WPA2/3" && w < 40 {
		return "W2/3"
	}
	return Truncate(s, 6)
}

func prefixIfNonEmpty(prefix, s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return prefix + s
}

func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[:len(r)-1])
}

func tailWindow(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[len(r)-maxRunes:])
}
