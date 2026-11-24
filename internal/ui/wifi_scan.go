package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

type WifiScanMode int

const (
	scanModeList WifiScanMode = iota
	scanModeDetail
	scanModePickIface
	scanModeSSID
	scanModePassword
)

type WifiScanView struct {
	Mode         WifiScanMode
	PrevMode     WifiScanMode
	Interfaces   []wifi.Interface
	ScanIface    string
	Scanning     bool
	HasScanned   bool
	ScanErr      string
	Networks     []wifi.Network
	Cursor       int
	DetailScroll int

	IfaceCursor  int
	ConnectIface string
	SSIDInput    string
	Password     string
	ShowPassword bool
	Connecting   bool
	ConnectErr   string

	Note      string
	NoteIsErr bool
}

func NewWifiScanView() WifiScanView {
	return WifiScanView{
		ScanIface: "wlan0",
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

func (v *WifiScanView) MoveNetwork(delta int) {
	if len(v.Networks) == 0 {
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

func (v *WifiScanView) OpenDetail() bool {
	if _, ok := v.SelectedNetwork(); !ok {
		return false
	}
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
	v.ShowPassword = false
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

	for i := range v.Networks {
		if (req.BSSID != "" && v.Networks[i].BSSID == req.BSSID) || (req.SSID != "" && v.Networks[i].SSID == req.SSID) {
			v.Networks[i].InUse = true
		} else if req.Iface == v.EffectiveScanIface() {
			v.Networks[i].InUse = false
		}
	}

	if v.PrevMode == scanModeDetail {
		v.Mode = scanModeDetail
	} else {
		v.Mode = scanModeList
	}
}

func (v *WifiScanView) HandleEsc() bool {
	switch v.Mode {
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
		v.Mode = scanModeList
		return true
	default:
		return false
	}
}

func (v WifiScanView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	switch v.Mode {
	case scanModeDetail:
		return v.renderDetail(w, h, pal, focused)
	case scanModePickIface:
		return v.renderPickIface(w, h, pal, focused)
	case scanModeSSID:
		return v.renderSSIDInput(w, h, pal)
	case scanModePassword:
		return v.renderPasswordInput(w, h, pal)
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
	ifacePart := sec.Render("If:") + accent.Render(Truncate(ifaceName, max(5, w-18)))
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
	leftMax := max(6, w-rightW-2)
	header := " " + FitLine(ifacePart, leftMax) + " " + rightPart
	if h <= 1 {
		return SplitLinesPadded([]string{header}, w, h)
	}

	showBottom := (h >= 11 || v.Note != "") && h >= 4
	listH := h - 1
	if showBottom {
		listH = h - 2
	}

	lines := []string{header}

	switch {
	case v.Scanning && len(v.Networks) == 0:
		lines = append(lines, " "+warn.Render(Truncate("Scanning "+ifaceName+"...", max(6, w-1))))

	case v.ScanErr != "" && len(v.Networks) == 0:
		lines = append(lines, " "+danger.Render("Scan failed:"))
		lines = append(lines, " "+muted.Render(Truncate(v.ScanErr, max(6, w-1))))
		lines = append(lines, " "+muted.Render("Press s to retry"))

	case len(v.Networks) == 0:
		if !v.HasScanned {
			lines = append(lines, " "+muted.Render("No scan yet"))
			lines = append(lines, " "+muted.Render(Truncate("Press s or ⏎ to scan "+ifaceName, max(6, w-1))))
		} else {
			lines = append(lines, " "+muted.Render(Truncate("No networks on "+ifaceName, max(6, w-1))))
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
			ssidW := max(4, w-2-1-rightPlainW)
			ssidTxt := Truncate(n.DisplaySSID(), ssidW)

			if selected && focused {
				mark := " "
				if n.InUse {
					mark = "*"
				}
				row := fmt.Sprintf("▸%s%s %s", mark, FitLine(ssidTxt, ssidW), rightPlain)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := " "
				if selected {
					pfx = accent.Render("▸")
				}
				mark := " "
				if n.InUse {
					mark = ok.Render("*")
				}
				ssidStyled := txt.Render(FitLine(ssidTxt, ssidW))
				if strings.TrimSpace(n.SSID) == "" {
					ssidStyled = muted.Render(FitLine(ssidTxt, ssidW))
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
			lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-1))))
		} else {
			lines = append(lines, " "+muted.Render(Truncate("⏎:info  c:conn  ←→:if  s:scan", max(6, w-1))))
		}
	}

	return SplitLinesPadded(lines, w, h)
}

func (v WifiScanView) buildDetailRows(w int, pal theme.Palette, net wifi.Network) []string {
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	valW := max(6, w-8)

	ssidVal := txt.Render(Truncate(net.DisplaySSID(), valW))
	if strings.TrimSpace(net.SSID) == "" {
		ssidVal = muted.Render("<hidden>")
	}

	bssidVal := muted.Render("n/a")
	if net.BSSID != "" {
		bssidVal = txt.Render(Truncate(net.BSSID, valW))
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
	if net.InUse {
		stateVal = ok.Render("Connected")
	}

	rows := []string{
		" " + sec.Render("SSID   ") + ssidVal,
		" " + sec.Render("BSSID  ") + bssidVal,
		" " + sec.Render("Signal ") + sigStyle.Render(Truncate(sigStr, valW)),
		" " + sec.Render("Sec    ") + txt.Render(Truncate(secStr, valW)),
		" " + sec.Render("State  ") + stateVal,
	}

	chanStr := joinNonEmpty(" · ", prefixIfNonEmpty("ch ", net.Channel), net.Freq)
	if chanStr != "" {
		rows = append(rows, " "+sec.Render("Chan   ")+txt.Render(Truncate(chanStr, valW)))
	}
	if net.Rate != "" {
		rows = append(rows, " "+sec.Render("Rate   ")+txt.Render(Truncate(net.Rate, valW)))
	}
	if net.Mode != "" {
		rows = append(rows, " "+sec.Render("Mode   ")+txt.Render(Truncate(net.Mode, valW)))
	}
	rows = append(rows, " "+sec.Render("ScanIf ")+muted.Render(Truncate(v.EffectiveScanIface(), valW)))

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
	titleW := max(6, w-actionW-2)
	header := " " + FitLine(muted.Render("◂ ")+accent.Render(Truncate(net.DisplaySSID(), max(4, titleW-2))), titleW) + " " + action
	if h <= 1 {
		return SplitLinesPadded([]string{header}, w, h)
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
			lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-1))))
		} else {
			lines = append(lines, " "+muted.Render(Truncate("⏎/c:connect  ↑↓:scroll  Esc:list", max(6, w-1))))
		}
	}

	return SplitLinesPadded(lines, w, h)
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
		" " + accent.Render(Truncate("Connect: "+net.DisplaySSID(), max(6, w-1))),
	}
	if h <= 1 {
		return SplitLinesPadded(lines, w, h)
	}

	if v.Connecting {
		lines = append(lines, " "+warn.Render(Truncate("Connecting via "+v.ConnectIface+"...", max(6, w-1))))
		return SplitLinesPadded(lines, w, h)
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

		maxStW := max(4, w/2)
		stPlain = Truncate(stPlain, maxStW)
		nameW := max(5, w-4-len([]rune(stPlain)))
		nameTxt := Truncate(inf.Name, nameW)

		if selected && focused {
			row := fmt.Sprintf(" ▸ %s %s", FitLine(nameTxt, nameW), stPlain)
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
			lines = append(lines, pfx+txt.Render(FitLine(nameTxt, nameW))+" "+stStyle.Render(stPlain))
		}
	}

	if showBottom {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		if v.ConnectErr != "" {
			lines = append(lines, " "+danger.Render(Truncate(v.ConnectErr, max(6, w-1))))
		} else {
			lines = append(lines, " "+muted.Render(Truncate("⏎:choose  Esc:back", max(6, w-1))))
		}
	}

	return SplitLinesPadded(lines, w, h)
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
		" " + accent.Render(Truncate("Hidden AP ("+v.ConnectIface+")", max(6, w-1))),
	}
	if v.Connecting {
		lines = append(lines, " "+warn.Render(Truncate("Connecting via "+v.ConnectIface+"...", max(6, w-1))))
		return SplitLinesPadded(lines, w, h)
	}

	lines = append(lines, " "+sec.Render("Enter SSID:"))
	fieldW := max(6, w-4)
	lines = append(lines, " > "+txt.Render(tailWindow(v.SSIDInput, fieldW-1)+"_"))
	if net.BSSID != "" {
		lines = append(lines, " "+muted.Render(Truncate("BSSID: "+net.BSSID, max(6, w-1))))
	}
	if v.ConnectErr != "" {
		lines = append(lines, " "+danger.Render(Truncate(v.ConnectErr, max(6, w-1))))
	}
	if h >= 6 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+muted.Render(Truncate("⏎:next  Esc:back", max(6, w-1))))
	}
	return SplitLinesPadded(lines, w, h)
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
		" " + accent.Render(Truncate(ssid+" ("+v.ConnectIface+")", max(6, w-1))),
	}
	if v.Connecting {
		lines = append(lines, " "+warn.Render(Truncate("Connecting "+ssid+"...", max(6, w-1))))
		lines = append(lines, " "+muted.Render(Truncate("Interface: "+v.ConnectIface, max(6, w-1))))
		return SplitLinesPadded(lines, w, h)
	}

	lines = append(lines, " "+sec.Render(Truncate("Password (blank=saved):", max(6, w-1))))

	disp := v.Password
	if !v.ShowPassword {
		disp = strings.Repeat("•", utf8.RuneCountInString(v.Password))
	}
	fieldW := max(6, w-4)
	lines = append(lines, " > "+txt.Render(tailWindow(disp, fieldW-1)+"_"))
	lines = append(lines, " "+muted.Render(Truncate("Sec: "+net.ShortSecurity(), max(6, w-1))))

	if v.ConnectErr != "" {
		lines = append(lines, " "+danger.Render(Truncate(v.ConnectErr, max(6, w-1))))
	}

	if h >= 6 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		visLbl := "Tab:show"
		if v.ShowPassword {
			visLbl = "Tab:hide"
		}
		lines = append(lines, " "+muted.Render(Truncate("⏎:conn  "+visLbl+"  Esc:back", max(6, w-1))))
	}

	return SplitLinesPadded(lines, w, h)
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
