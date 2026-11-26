package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

// wpsMethod is one OneShot-Extended attack preset offered in the WPS menu.
type wpsMethod struct {
	Label  string
	Args   []string
	Custom bool
	Back   bool
}

var wpsMethods = []wpsMethod{
	{Label: "Pixie Dust", Args: wifi.SplitAttackArgs(wifi.OneShotDefaultAttack)},
	{Label: "Pixie (no force)", Args: []string{"-P", "-k"}},
	{Label: "Pixie force only", Args: []string{"-P", "-F"}},
	{Label: "Null PIN", Args: []string{"-N", "-k"}},
	{Label: "Push button", Args: []string{"--pbc", "-k"}},
	{Label: "Bruteforce", Args: []string{"-B", "-k"}},
	{Label: "Custom PIN…", Custom: true},
	{Label: "Back", Back: true},
}

// SetOneShotRefresh reloads OneShot discovery and the vulnerable-device list.
func (v *WifiScanView) SetOneShotRefresh(dir string) {
	v.OneShot = wifi.FindOneShot(dir)
	v.VulnList = wifi.LoadVulnList(v.OneShot.VulnList)
}

// SetWPSInfo stores WPS scan results for one interface.
func (v *WifiScanView) SetWPSInfo(iface string, infos map[string]wifi.WPSInfo, err error) {
	if v.WPSByIface == nil {
		v.WPSByIface = make(map[string]map[string]wifi.WPSInfo)
	}
	if v.WPSFetched == nil {
		v.WPSFetched = make(map[string]time.Time)
	}
	if v.WPSLoading == nil {
		v.WPSLoading = make(map[string]bool)
	}
	v.WPSLoading[iface] = false
	if err != nil {
		// Do not mark the fetch as fresh: allow an immediate retry.
		v.WPSNote = "WPS scan: " + err.Error()
		return
	}
	v.WPSNote = ""
	v.WPSFetched[iface] = time.Now()
	v.WPSByIface[iface] = infos
}

func (v WifiScanView) WPSLoadingFor(iface string) bool {
	return v.WPSLoading != nil && v.WPSLoading[iface]
}

// WPSInfoFor returns WPS data for n on the selected interface.
func (v WifiScanView) WPSInfoFor(n wifi.Network) (wifi.WPSInfo, bool) {
	if v.WPSByIface == nil {
		return wifi.WPSInfo{}, false
	}
	infos, ok := v.WPSByIface[v.EffectiveScanIface()]
	if !ok {
		return wifi.WPSInfo{}, false
	}
	info, ok := infos[strings.ToUpper(strings.TrimSpace(n.BSSID))]
	return info, ok
}

// WPSInfoStale reports whether WPS data for iface should be refreshed.
func (v WifiScanView) WPSInfoStale(iface string) bool {
	if v.WPSFetched == nil {
		return true
	}
	at, ok := v.WPSFetched[iface]
	return !ok || time.Since(at) > 2*time.Minute
}

// DrainWPSLines applies any buffered OneShot output before finishing a run.
func (v *WifiScanView) DrainWPSLines(seq int) {
	if v.WPSProgressCh == nil {
		return
	}
	for {
		select {
		case line, ok := <-v.WPSProgressCh:
			if !ok {
				return
			}
			v.ApplyWPSLine(seq, line)
		default:
			return
		}
	}
}

// VulnerableCount counts OneShot-vulnerable APs on the selected interface.
func (v WifiScanView) VulnerableCount() (vulnerable, total int) {
	for _, n := range v.Networks {
		info, ok := v.WPSInfoFor(n)
		if !ok {
			continue
		}
		if !info.WPS {
			continue
		}
		total++
		if info.Vulnerable(v.VulnList) {
			vulnerable++
		}
	}
	return vulnerable, total
}

// SecurityColor maps a network's security to the requested color scheme:
// red WPA3, orange WPA2, yellow mixed/WPA/WEP, green Open.
func SecurityColor(sec string) string {
	s := strings.ToUpper(strings.TrimSpace(sec))
	switch {
	case s == "" || s == "--" || s == "OPEN":
		return "open"
	case strings.Contains(s, "WPA3") || strings.Contains(s, "SAE") || strings.Contains(s, "WPA2/3"):
		return "wpa3"
	case strings.Contains(s, "WPA2"):
		return "wpa2"
	case strings.Contains(s, "WPA"):
		return "wpa"
	case strings.Contains(s, "WEP"):
		return "wep"
	default:
		return "other"
	}
}

// NetworkColor returns the lipgloss color for a scan row according to the
// active color mode.
func (v WifiScanView) NetworkColor(n wifi.Network, pal theme.Palette) lipgloss.Color {
	if v.ColorMode == config.WiFiColorOneShot {
		infos, loaded := v.WPSByIface[v.EffectiveScanIface()]
		if !loaded {
			// WPS data not fetched yet: show security colors until it arrives.
			return securityColor(n, pal)
		}
		info, ok := infos[strings.ToUpper(strings.TrimSpace(n.BSSID))]
		if !ok || !info.WPS {
			return pal.Muted
		}
		if info.Locked {
			return pal.Danger
		}
		if info.Vulnerable(v.VulnList) {
			return pal.Success
		}
		return pal.Secondary
	}
	return securityColor(n, pal)
}

func securityColor(n wifi.Network, pal theme.Palette) lipgloss.Color {
	switch SecurityColor(n.Security) {
	case "wpa3":
		return pal.Danger
	case "wpa2":
		return pal.Orange
	case "wpa":
		return pal.Warning
	case "wep":
		return pal.Warning
	case "open":
		return pal.Success
	default:
		return pal.Muted
	}
}

// ColorModeLabel is the short tag shown in the list header.
func (v WifiScanView) ColorModeLabel() string {
	if v.ColorMode == config.WiFiColorOneShot {
		return "OSE"
	}
	return "SEC"
}

// ToggleColorMode switches between security and OneShot vulnerability coloring.
func (v *WifiScanView) ToggleColorMode() string {
	if v.ColorMode == config.WiFiColorOneShot {
		v.ColorMode = config.WiFiColorSecurity
	} else {
		v.ColorMode = config.WiFiColorOneShot
	}
	return v.ColorMode
}

// BeginWPSMenu opens the OneShot method picker for the selected AP.
func (v *WifiScanView) BeginWPSMenu(returnMode WifiScanMode) bool {
	if _, ok := v.SelectedNetwork(); !ok {
		return false
	}
	v.WPSMenuReturn = returnMode
	v.Mode = scanModeWPSMenu
	v.WPSMenuCursor = 0
	v.WPSPinInput = ""
	v.WPSNote = ""
	return true
}

func (v *WifiScanView) MoveWPSMenu(delta int) {
	v.WPSMenuCursor = (v.WPSMenuCursor + delta + len(wpsMethods)) % len(wpsMethods)
	v.WPSNote = ""
}

func (v WifiScanView) SelectedWPSMethod() (wpsMethod, bool) {
	if v.WPSMenuCursor < 0 || v.WPSMenuCursor >= len(wpsMethods) {
		return wpsMethod{}, false
	}
	return wpsMethods[v.WPSMenuCursor], true
}

// StartWPSPin enters custom PIN input.
func (v *WifiScanView) StartWPSPin() {
	v.Mode = scanModeWPSPin
	v.WPSPinInput = ""
	v.WPSNote = ""
}

// StartWPSRun launches OneShot for method with the given arguments.
func (v *WifiScanView) StartWPSRun(method string, args []string) (context.Context, int, chan string, bool) {
	if v.WPSActive {
		return nil, 0, nil, false
	}
	net, ok := v.SelectedNetwork()
	if !ok {
		return nil, 0, nil, false
	}
	if !v.OneShot.Found() {
		if v.OneShot.Err != "" {
			v.WPSNote = v.OneShot.Err
		} else {
			v.WPSNote = "OneShot-Extended not found"
		}
		return nil, 0, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.WPSSeq++
	v.WPSActive = true
	v.WPSCancel = cancel
	v.WPSMethod = method
	v.WPSArgs = args
	v.WPSScript = v.OneShot.Script
	v.WPSLines = nil
	v.WPSErr = ""
	v.WPSDone = ""
	v.WPSStarted = time.Now()
	v.WPSElapsed = 0
	v.WPSProgressCh = make(chan string, 64)
	v.Mode = scanModeWPSRun
	v.WPSNote = ""
	_ = net
	return ctx, v.WPSSeq, v.WPSProgressCh, true
}

// TargetBSSID returns the selected AP's BSSID.
func (v WifiScanView) TargetBSSID() string {
	net, ok := v.SelectedNetwork()
	if !ok {
		return ""
	}
	return net.BSSID
}

func (v *WifiScanView) ApplyWPSLine(seq int, line string) {
	if !v.WPSActive || seq != v.WPSSeq {
		return
	}
	v.WPSElapsed = time.Since(v.WPSStarted)
	v.WPSLines = append(v.WPSLines, line)
	if len(v.WPSLines) > 200 {
		v.WPSLines = v.WPSLines[len(v.WPSLines)-200:]
	}
	v.WPSOutput = line
}

func (v *WifiScanView) StopWPSRun() {
	if !v.WPSActive {
		return
	}
	if v.WPSCancel != nil {
		v.WPSCancel()
		v.WPSCancel = nil
	}
	v.WPSActive = false
	v.WPSProgressCh = nil
	v.WPSDone = "Stopped"
}

func (v *WifiScanView) FinishWPSRun(seq int, err error) {
	if seq != v.WPSSeq {
		return
	}
	v.WPSActive = false
	v.WPSCancel = nil
	v.WPSProgressCh = nil
	v.WPSElapsed = time.Since(v.WPSStarted)
	switch {
	case err == nil:
		v.WPSDone = "Finished"
	case strings.Contains(err.Error(), "canceled"):
		v.WPSDone = "Stopped"
	default:
		v.WPSErr = err.Error()
	}
}

func (v WifiScanView) renderWPSMenu(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	net, _ := v.SelectedNetwork()
	title := "WPS · " + net.DisplaySSID()
	lines := []string{
		" " + accent.Render(v.Marquee.Truncate(title, max(6, w-2))),
	}
	status := "OneShot"
	if v.OneShot.Found() {
		status = "OneShot · ready"
	} else {
		status = "OneShot · missing"
	}
	if info, ok := v.WPSInfoFor(net); ok && info.WPS {
		status = info.Label()
	}
	lines = append(lines, " "+muted.Render(v.Marquee.Truncate(status, max(6, w-2))))

	listH := max(1, h-3)
	if v.WPSNote != "" {
		listH = max(1, h-4)
	}
	cursor := clampIndex(v.WPSMenuCursor, len(wpsMethods))
	start, end := ScrollWindow(len(wpsMethods), cursor, listH)
	for i := start; i < end; i++ {
		m := wpsMethods[i]
		val := strings.Join(m.Args, " ")
		if m.Custom {
			val = "pin"
		} else if m.Back {
			val = "esc"
		}
		nameW := max(8, w-len(val)-5)
		if i == cursor && focused {
			row := " ▸ " + FitLine(m.Label, nameW) + " " + val
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if i == cursor {
				pfx = accent.Render(" ▸ ")
			}
			vSty := muted
			switch {
			case m.Back:
				vSty = muted
			case i == 0:
				vSty = ok
			case m.Custom:
				vSty = warn
			}
			lines = append(lines, pfx+txt.Render(FitLine(m.Label, nameW))+" "+vSty.Render(val))
		}
	}
	if v.WPSNote != "" {
		lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.WPSNote, max(6, w-2))))
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

func (v WifiScanView) renderWPSPin(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	net, _ := v.SelectedNetwork()
	lines := []string{
		" " + accent.Render(v.Marquee.Truncate("WPS PIN · "+net.DisplaySSID(), max(6, w-2))),
		" " + sec.Render("Enter WPS PIN (4/8 digits):"),
	}
	fieldW := max(6, w-5)
	lines = append(lines, " > "+txt.Render(tailWindow(v.WPSPinInput, fieldW-1)+"_"))
	if v.WPSNote != "" {
		lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.WPSNote, max(6, w-2))))
	}
	if h >= 5 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate("⏎:run  Esc:back", max(6, w-2))))
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v WifiScanView) renderWPSRun(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	net, _ := v.SelectedNetwork()
	state := "running"
	stStyle := warn
	if !v.WPSActive {
		switch {
		case v.WPSErr != "":
			state, stStyle = "failed", danger
		case strings.Contains(v.WPSDone, "Stopped"):
			state, stStyle = "stopped", muted
		default:
			state, stStyle = "done", ok
		}
	}
	elapsed := v.WPSElapsed
	if v.WPSActive && v.WPSStarted != (time.Time{}) {
		elapsed = time.Since(v.WPSStarted)
	}
	title := v.WPSMethod
	if title == "" {
		title = "OneShot"
	}
	lines := []string{
		" " + accent.Render(v.Marquee.Truncate("WPS · "+net.DisplaySSID(), max(6, w-2))),
		" " + sec.Render("Mode ") + txt.Render(v.Marquee.Truncate(title+" · "+strings.Join(v.WPSArgs, " "), max(6, w-5))),
		" " + stStyle.Render(state) + " " + muted.Render(fmt.Sprintf("%ds", int(elapsed.Seconds()))),
	}
	if v.WPSErr != "" {
		lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.WPSErr, max(6, w-2))))
	}
	// Output tail fills the remaining rows above the hint.
	avail := h - len(lines) - 1
	if avail < 0 {
		avail = 0
	}
	tail := v.WPSLines
	if len(tail) > avail {
		tail = tail[len(tail)-avail:]
	}
	for _, ln := range tail {
		lines = append(lines, " "+txt.Render(v.Marquee.Truncate(ln, max(6, w-2))))
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	hint := "Esc:stop"
	if !v.WPSActive {
		hint = "Esc:back"
	}
	lines = append(lines, " "+muted.Render(v.Marquee.Truncate(hint, max(6, w-2))))
	if len(lines) > h && h >= 2 {
		lines = append(lines[:h-1], lines[len(lines)-1])
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}
