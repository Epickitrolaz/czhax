package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/wifi"
)

func sendKey(m AppModel, key string) AppModel {
	var msg tea.KeyMsg
	switch key {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := m.Update(msg)
	return next.(AppModel)
}

func assertFitsBounds(t *testing.T, view string, maxW, exactH int, label string) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != exactH {
		t.Fatalf("[%s] expected %d lines, got %d:\n%s", label, exactH, len(lines), view)
	}
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w > maxW {
			t.Fatalf("[%s] line %d exceeds width %d (got %d): %q", label, i+1, maxW, w, line)
		}
	}
}

func TestAllViewsAndSizes(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"CZ_53x14", 53, 14},
		{"CZ_45x10", 45, 10},
		{"CZ_40x10", 40, 10},
		{"CZ_40x21", 40, 21},
		{"Wide_100x30", 100, 30},
	}

	for _, sz := range sizes {
		cfg := config.Default()
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.2.0")
		m.Snap.HasBattery = true
		m.Snap.BatteryPct = 78
		m.Snap.HasBatTime = true
		m.Snap.BatTimeLeft = 2*time.Hour + 15*time.Minute

		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)

		// Status view (Device tab)
		view := m.View()
		assertFitsBounds(t, view, sz.w, sz.h, sz.name+"/Status/Tabs")
		if !strings.Contains(view, "Device") || !strings.Contains(view, "2h15m left") {
			t.Fatalf("[%s] expected Device tab and 2h15m left in:\n%s", sz.name, view)
		}

		// Cycle to Status -> WiFi tab (empty state)
		m = sendKey(m, "right")
		if m.ActiveTabs[ViewStatus] != StatusTabWiFi {
			t.Fatalf("expected StatusTabWiFi on right, got %d", m.ActiveTabs[ViewStatus])
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Status/WiFi/Empty")

		// Populate multiple WiFi interfaces and test FocusTabs + FocusContent navigation
		m.Wifi.SetInterfaces([]wifi.Interface{
			{
				Name:           "wlan0",
				Up:             true,
				OperState:      "up",
				MAC:            "b8:27:eb:aa:bb:cc",
				MTU:            1500,
				Addrs:          []string{"192.168.1.42/24"},
				Phy:            "phy0",
				Mode:           "managed",
				HasMonCap:      true,
				MonitorCapable: false,
				Bands:          "2.4G",
				Manufacturer:   "Broadcom",
				Driver:         "brcmfmac",
				Bus:            "sdio",
				Channel:        "ch 6 (2437 MHz)",
				SSID:           "LabNet",
				TxPower:        "20.00 dBm",
			},
			{
				Name:           "wlan1",
				Up:             false,
				OperState:      "down",
				MAC:            "00:c0:ca:11:22:33",
				MTU:            1500,
				Phy:            "phy1",
				Mode:           "monitor",
				HasMonCap:      true,
				MonitorCapable: true,
				Bands:          "2.4G/5G",
				Manufacturer:   "Realtek",
				Product:        "802.11ac NIC",
				Driver:         "rtl88XXau",
				Bus:            "usb",
				VendorID:       "0bda",
				ProductID:      "8812",
			},
		})
		wifiTabsView := m.View()
		assertFitsBounds(t, wifiTabsView, sz.w, sz.h, sz.name+"/Status/WiFi/Tabs")
		for _, want := range []string{"wlan0", "wlan1", "Broadcom", "managed"} {
			if !strings.Contains(wifiTabsView, want) {
				t.Fatalf("[%s] expected %q in WiFi view:\n%s", sz.name, want, wifiTabsView)
			}
		}
		if strings.Contains(wifiTabsView, "(mon") {
			t.Fatalf("[%s] expected bare Mode value, got monitor capability suffix:\n%s", sz.name, wifiTabsView)
		}

		// Enter WiFi content; right switches to wlan1, left returns to wlan0 (does not exit FocusContent)
		m = sendKey(m, "enter")
		if m.Focus != FocusContent {
			t.Fatalf("expected FocusContent on WiFi enter")
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Status/WiFi/Content0")
		m = sendKey(m, "right")
		if m.Wifi.Selected != 1 || m.Focus != FocusContent {
			t.Fatalf("expected Selected=1 and FocusContent on right, got %d / %v", m.Wifi.Selected, m.Focus)
		}
		wifiContent1 := m.View()
		assertFitsBounds(t, wifiContent1, sz.w, sz.h, sz.name+"/Status/WiFi/Content1")
		for _, want := range []string{"monitor", "Realtek"} {
			if !strings.Contains(wifiContent1, want) {
				t.Fatalf("[%s] expected %q in wlan1 view:\n%s", sz.name, want, wifiContent1)
			}
		}
		if strings.Contains(wifiContent1, "(mon") {
			t.Fatalf("[%s] expected bare monitor mode, got capability suffix:\n%s", sz.name, wifiContent1)
		}
		m = sendKey(m, "down")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Status/WiFi/ScrollDown")
		m = sendKey(m, "left")
		if m.Wifi.Selected != 0 || m.Focus != FocusContent {
			t.Fatalf("expected Selected=0 and FocusContent on left, got %d / %v", m.Wifi.Selected, m.Focus)
		}
		m = sendKey(m, "esc")
		if m.Focus != FocusTabs {
			t.Fatalf("expected FocusTabs on esc from WiFi")
		}

		// Cycle to Status -> BT tab (empty state)
		m = sendKey(m, "right")
		if m.ActiveTabs[ViewStatus] != StatusTabBT {
			t.Fatalf("expected StatusTabBT on right, got %d", m.ActiveTabs[ViewStatus])
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Status/BT/Empty")

		// Populate multiple Bluetooth adapters and test FocusTabs + FocusContent navigation
		m.Bluetooth.SetAdapters([]bluetooth.Adapter{
			{
				Name:         "hci0",
				Alias:        "cardzero",
				Address:      "B8:27:EB:11:22:33",
				Up:           true,
				Powered:      true,
				StateKnown:   true,
				RFKill:       "unblocked",
				Bus:          "UART",
				Type:         "Primary",
				Mode:         "BR/EDR + LE",
				HCIVersion:   "5.0",
				Manufacturer: "Broadcom",
				Driver:       "hci_uart",
				Pairable:     true,
				FlagsKnown:   true,
				ScanMode:     "PSCAN",
			},
			{
				Name:         "hci1",
				Address:      "00:E0:4C:AA:BB:CC",
				Up:           false,
				StateKnown:   true,
				RFKill:       "soft-blocked",
				Bus:          "USB",
				Type:         "Primary",
				Manufacturer: "Realtek",
				Product:      "Bluetooth 5.0 Radio",
				Driver:       "btusb",
				VendorID:     "0bda",
				ProductID:    "8771",
			},
		})
		btTabsView := m.View()
		assertFitsBounds(t, btTabsView, sz.w, sz.h, sz.name+"/Status/BT/Tabs")
		for _, want := range []string{"hci0", "hci1", "Broadcom", "B8:27:EB:11:22:33"} {
			if !strings.Contains(btTabsView, want) {
				t.Fatalf("[%s] expected %q in BT view:\n%s", sz.name, want, btTabsView)
			}
		}

		m = sendKey(m, "enter")
		if m.Focus != FocusContent {
			t.Fatalf("expected FocusContent on BT enter")
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Status/BT/Content0")
		m = sendKey(m, "right")
		if m.Bluetooth.Selected != 1 || m.Focus != FocusContent {
			t.Fatalf("expected Selected=1 and FocusContent on BT right, got %d / %v", m.Bluetooth.Selected, m.Focus)
		}
		btContent1 := m.View()
		assertFitsBounds(t, btContent1, sz.w, sz.h, sz.name+"/Status/BT/Content1")
		if !strings.Contains(btContent1, "BLOCKED") || !strings.Contains(btContent1, "Realtek") {
			t.Fatalf("[%s] expected BLOCKED and Realtek in hci1 view:\n%s", sz.name, btContent1)
		}
		m = sendKey(m, "down")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Status/BT/ScrollDown")
		m = sendKey(m, "esc")
		if m.Focus != FocusTabs {
			t.Fatalf("expected FocusTabs on esc from BT")
		}

		// Down moves to WiFi view (Scan tab)
		m = sendKey(m, "down")
		if m.ActiveView != ViewWiFi {
			t.Fatalf("expected ViewWiFi on down, got %v", m.ActiveView)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/Tabs")

		// Enter WiFi -> Scan content (auto-starts scan if not scanned yet)
		m = sendKey(m, "enter")
		if m.Focus != FocusContent || !m.WifiScan.Scanning {
			t.Fatalf("expected FocusContent and initial scanning on WiFi enter, got focus=%v scanning=%v", m.Focus, m.WifiScan.Scanning)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/Scanning")

		// Sync interfaces (wlan0 + wlan1) and deliver scan results
		nextM, _ := m.Update(wifiRefreshedMsg{ifaces: []wifi.Interface{
			{Name: "wlan0", Up: true, Mode: "managed", SSID: "LabNet"},
			{Name: "wlan1", Up: false, Mode: "managed"},
		}})
		m = nextM.(AppModel)
		nextM, _ = m.Update(wifiNetworksScannedMsg{
			iface: "wlan0",
			nets: []wifi.Network{
				{SSID: "LabNet", BSSID: "5C:7B:5C:46:E7:A8", Signal: 76, RSSI: -62, HasRSSI: true, Channel: "1", Freq: "2412 MHz", Rate: "540 Mbit/s", Mode: "Infra", Security: "WPA1 WPA2", InUse: true},
				{SSID: "FastSAE", BSSID: "EC:81:9C:1C:5C:44", Signal: 52, Channel: "3", Freq: "2422 MHz", Security: "WPA2 WPA3"},
				{SSID: "", BSSID: "5C:7B:5C:46:E7:AB", Signal: 48, Channel: "1", Security: "WPA2"},
				{SSID: "CafeGuest", BSSID: "AA:BB:CC:DD:EE:FF", Signal: 25, Channel: "6", Security: "Open"},
			},
		})
		m = nextM.(AppModel)
		scanListView := m.View()
		assertFitsBounds(t, scanListView, sz.w, sz.h, sz.name+"/WiFi/List")
		if !strings.Contains(scanListView, "LabNet") || !strings.Contains(scanListView, "wlan0") {
			t.Fatalf("[%s] expected LabNet and wlan0 in scan list:\n%s", sz.name, scanListView)
		}

		// Right cycles scan interface to wlan1, left returns to wlan0 (stays in FocusContent)
		m = sendKey(m, "right")
		if m.WifiScan.EffectiveScanIface() != "wlan1" || m.Focus != FocusContent {
			t.Fatalf("expected scan iface wlan1 and FocusContent, got %q / %v", m.WifiScan.EffectiveScanIface(), m.Focus)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/IfaceWlan1")
		m = sendKey(m, "left")
		if m.WifiScan.EffectiveScanIface() != "wlan0" {
			t.Fatalf("expected scan iface wlan0, got %q", m.WifiScan.EffectiveScanIface())
		}

		// Down moves to FastSAE, Enter opens Details, Esc returns to List (still FocusContent)
		m = sendKey(m, "down")
		m = sendKey(m, "enter")
		if m.WifiScan.Mode != scanModeDetail || m.Focus != FocusContent {
			t.Fatalf("expected scanModeDetail and FocusContent, got mode=%v focus=%v", m.WifiScan.Mode, m.Focus)
		}
		detailView := m.View()
		assertFitsBounds(t, detailView, sz.w, sz.h, sz.name+"/WiFi/Detail")
		if !strings.Contains(detailView, "FastSAE") || !strings.Contains(detailView, "EC:81:9C:1C:5C:44") {
			t.Fatalf("[%s] expected network details in:\n%s", sz.name, detailView)
		}
		m = sendKey(m, "down")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/DetailScroll")

		// Enter from Detail opens Connect Interface Picker; pick wlan1 while scan iface is wlan0
		m = sendKey(m, "enter")
		if m.WifiScan.Mode != scanModePickIface {
			t.Fatalf("expected scanModePickIface, got %v", m.WifiScan.Mode)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/PickIface")
		m = sendKey(m, "down") // wlan1
		m = sendKey(m, "enter")
		if m.WifiScan.Mode != scanModePassword || m.WifiScan.ConnectIface != "wlan1" {
			t.Fatalf("expected scanModePassword on wlan1, got mode=%v iface=%q", m.WifiScan.Mode, m.WifiScan.ConnectIface)
		}
		// Typing q/r/? must go to password input, not global handlers
		for _, ch := range []string{"q", "r", "?", " ", "x"} {
			m = sendKey(m, ch)
		}
		if m.WifiScan.Password != "qr? x" || m.ShowHelp || m.ConfirmExit {
			t.Fatalf("expected password 'qr? x' without triggering global keys, got %q help=%v quit=%v", m.WifiScan.Password, m.ShowHelp, m.ConfirmExit)
		}
		m = sendKey(m, "backspace")
		if m.WifiScan.Password != "qr? " {
			t.Fatalf("expected backspace to drop x, got %q", m.WifiScan.Password)
		}
		if !m.WifiScan.ShowPassword {
			t.Fatalf("expected ShowPassword to default to true")
		}
		pwVisView := m.View()
		assertFitsBounds(t, pwVisView, sz.w, sz.h, sz.name+"/WiFi/PasswordVisible")
		if !strings.Contains(pwVisView, "qr? _") {
			t.Fatalf("[%s] expected visible passphrase 'qr? _' by default in:\n%s", sz.name, pwVisView)
		}
		m = sendKey(m, "tab")
		if m.WifiScan.ShowPassword {
			t.Fatalf("expected Tab to hide password")
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/WiFi/PasswordMasked")
		// Esc steps back: Password -> PickIface -> Detail -> List -> FocusTabs
		m = sendKey(m, "esc")
		if m.WifiScan.Mode != scanModePickIface || m.Focus != FocusContent {
			t.Fatalf("expected back to PickIface, got mode=%v focus=%v", m.WifiScan.Mode, m.Focus)
		}
		m = sendKey(m, "esc")
		if m.WifiScan.Mode != scanModeDetail || m.Focus != FocusContent {
			t.Fatalf("expected back to Detail, got mode=%v focus=%v", m.WifiScan.Mode, m.Focus)
		}
		m = sendKey(m, "esc")
		if m.WifiScan.Mode != scanModeList || m.Focus != FocusContent {
			t.Fatalf("expected back to List, got mode=%v focus=%v", m.WifiScan.Mode, m.Focus)
		}
		m = sendKey(m, "esc")
		if m.Focus != FocusTabs {
			t.Fatalf("expected FocusTabs on esc from WiFi List")
		}

		// Down moves to IR view, then System view
		m = sendKey(m, "down")
		if m.ActiveView != ViewIR {
			t.Fatalf("expected ViewIR on down, got %v", m.ActiveView)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Tabs")
		m = sendKey(m, "enter")
		if m.Focus != FocusContent {
			t.Fatalf("expected FocusContent on IR enter")
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/RemoteEmpty")
		m = sendKey(m, "esc")
		if m.Focus != FocusTabs {
			t.Fatalf("expected FocusTabs on esc from IR")
		}
		m = sendKey(m, "down")
		if m.ActiveView != ViewSystem {
			t.Fatalf("expected ViewSystem on down, got %v", m.ActiveView)
		}
		sysView := m.View()
		assertFitsBounds(t, sysView, sz.w, sz.h, sz.name+"/System/Tabs")
		for _, want := range []string{"Update CZHax", "Restart CZHax", "Terminal Shell", "Start on Boot"} {
			if !strings.Contains(sysView, want) {
				t.Fatalf("[%s] expected %q in System view:\n%s", sz.name, want, sysView)
			}
		}

		// Enter enters System content, arrows navigate all options (skipping separator), Esc returns
		m = sendKey(m, "enter")
		if m.Focus != FocusContent {
			t.Fatalf("expected FocusContent on enter")
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptRestart) {
			t.Fatalf("expected cursor at SysOptRestart, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptTerminal) {
			t.Fatalf("expected cursor at SysOptTerminal, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptBoot) {
			t.Fatalf("expected cursor at SysOptBoot, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptSleep) {
			t.Fatalf("expected cursor at SysOptSleep, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptReboot) {
			t.Fatalf("expected cursor at SysOptReboot, got %d", m.System.Cursor)
		}
		m = sendKey(m, "down")
		if m.System.Cursor != int(SysOptShutdown) {
			t.Fatalf("expected cursor at SysOptShutdown, got %d", m.System.Cursor)
		}
		// Trigger shutdown confirm and cancel cleanly with 'n'
		m = sendKey(m, "enter")
		if m.System.Confirm != "shutdown" {
			t.Fatalf("expected shutdown confirmation prompt, got %q", m.System.Confirm)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/System/ConfirmShutdown")
		m = sendKey(m, "n")
		if m.System.Confirm != "" {
			t.Fatalf("expected confirmation cleared on 'n'")
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/System/Content")

		m = sendKey(m, "esc")
		if m.Focus != FocusTabs {
			t.Fatalf("expected FocusTabs on esc")
		}

		// Help overlay
		m = sendKey(m, "?")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Help")
		m = sendKey(m, "esc")
	}
}

func TestStartupRedrawSchedule(t *testing.T) {
	var prev time.Duration
	for i, d := range startupRedrawDelays {
		if d <= prev {
			t.Fatalf("startup redraw delay %d (%s) is not increasing", i, d)
		}
		prev = d
		if scheduleStartupRedraw(i) == nil {
			t.Fatalf("expected a redraw command for step %d", i)
		}
	}
	if scheduleStartupRedraw(len(startupRedrawDelays)) != nil {
		t.Fatal("expected no redraw command past the end of the schedule")
	}
}

func TestSidebarLabelsMaxSixChars(t *testing.T) {
	for _, vd := range TopViews {
		if got := lipgloss.Width(SidebarLabel(vd.Name)); got > SidebarLabelMax {
			t.Fatalf("sidebar label for %q is %d cells, max is %d", vd.Name, got, SidebarLabelMax)
		}
	}
	if got := SidebarLabel("VeryLongViewName"); got != "Very…" {
		t.Fatalf("expected truncated sidebar label %q, got %q", "Very…", got)
	}
	lines := RenderSidebarColumn(8, 10, theme.HighContrast, config.Default(), ViewStatus, true)
	for i, line := range lines {
		if got := lipgloss.Width(line); got != 8 {
			t.Fatalf("sidebar line %d has width %d, want 8: %q", i, got, line)
		}
	}
}

func TestManyInterfacesSelectorOverflowBounds(t *testing.T) {
	cfg := config.Default()
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.2.0")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m = next.(AppModel)
	m = sendKey(m, "right") // StatusTabWiFi
	m = sendKey(m, "enter") // FocusContent

	var ifaces []wifi.Interface
	for i := 0; i < 6; i++ {
		ifaces = append(ifaces, wifi.Interface{
			Name:         "wlan" + string(rune('0'+i)),
			Up:           true,
			Mode:         "managed",
			Manufacturer: "VeryLongManufacturerNameCorporation",
			Product:      "Dual-Band Wireless Adapter Pro Max",
			Driver:       "rtl88XXau",
			Bus:          "usb",
		})
	}
	m.Wifi.SetInterfaces(ifaces)
	for i := 0; i < 6; i++ {
		view := m.View()
		assertFitsBounds(t, view, 40, 10, "40x10/ManyWifi/"+ifaces[m.Wifi.Selected].Name)
		if !strings.Contains(view, ifaces[m.Wifi.Selected].Name) {
			t.Fatalf("expected selected interface %s to remain visible in:\n%s", ifaces[m.Wifi.Selected].Name, view)
		}
		m = sendKey(m, "right")
	}
}

func TestTTYExecRoutesNonTTYSysderrToStdout(t *testing.T) {
	cmd := buildInteractiveShellCmd()
	var outBuf bytes.Buffer
	rPipe, wPipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rPipe.Close()
	defer wPipe.Close()

	cmd.SetStdout(&outBuf)
	cmd.SetStderr(wPipe) // simulate non-TTY journald stderr
	if cmd.Cmd.Stderr != &outBuf {
		t.Fatalf("expected non-TTY stderr to route to tty stdout writer")
	}
	hasNested := false
	for _, e := range cmd.Cmd.Env {
		if e == "CZHAX_NESTED=1" {
			hasNested = true
		}
	}
	if !hasNested {
		t.Fatalf("expected CZHAX_NESTED=1 in shell command env")
	}
}

func TestWifiScanConnectHiddenOpenAndErrors(t *testing.T) {
	cfg := config.Default()
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.2.0")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m = next.(AppModel)

	m = sendKey(m, "down")  // ViewWiFi
	m = sendKey(m, "enter") // FocusContent

	nextM, _ := m.Update(wifiRefreshedMsg{ifaces: []wifi.Interface{
		{Name: "wlan0", Up: true, Mode: "managed", SSID: "LabNet"},
		{Name: "wlan1", Up: false, Mode: "managed"},
	}})
	m = nextM.(AppModel)

	// Scan error bounds
	nextM, _ = m.Update(wifiNetworksScannedMsg{iface: "wlan0", err: os.ErrPermission})
	m = nextM.(AppModel)
	assertFitsBounds(t, m.View(), 40, 10, "40x10/WiFi/ScanError")

	// Deliver hidden + open networks
	nextM, _ = m.Update(wifiNetworksScannedMsg{
		iface: "wlan0",
		nets: []wifi.Network{
			{SSID: "", BSSID: "11:22:33:44:55:66", Signal: 70, Security: "WPA2"},
			{SSID: "FreeSpot", BSSID: "AA:BB:CC:DD:EE:FF", Signal: 60, Security: "Open"},
		},
	})
	m = nextM.(AppModel)

	// Connect to hidden network via 'c' shortcut -> PickIface -> SSID -> Password
	m = sendKey(m, "c")
	if m.WifiScan.Mode != scanModePickIface {
		t.Fatalf("expected scanModePickIface on 'c', got %v", m.WifiScan.Mode)
	}
	m = sendKey(m, "down")  // select wlan1
	m = sendKey(m, "enter") // confirm wlan1 -> hidden SSID prompt
	if m.WifiScan.Mode != scanModeSSID {
		t.Fatalf("expected scanModeSSID for hidden network, got %v", m.WifiScan.Mode)
	}
	assertFitsBounds(t, m.View(), 40, 10, "40x10/WiFi/HiddenSSID")
	for _, ch := range []string{"S", "e", "c", "r", "e", "t"} {
		m = sendKey(m, ch)
	}
	m = sendKey(m, "enter")
	if m.WifiScan.Mode != scanModePassword {
		t.Fatalf("expected scanModePassword after SSID, got %v", m.WifiScan.Mode)
	}
	for _, ch := range []string{"p", "a", "s", "s"} {
		m = sendKey(m, ch)
	}
	req := m.WifiScan.BuildConnectRequest()
	if req.Iface != "wlan1" || req.SSID != "Secret" || req.Password != "pass" || !req.Hidden {
		t.Fatalf("unexpected hidden connect request: %+v", req)
	}
	m = sendKey(m, "enter")
	if !m.WifiScan.Connecting {
		t.Fatalf("expected Connecting=true on password submit")
	}
	assertFitsBounds(t, m.View(), 40, 10, "40x10/WiFi/Connecting")

	// Simulate connect failure then success
	nextM, _ = m.Update(wifiConnectDoneMsg{req: req, err: os.ErrInvalid})
	m = nextM.(AppModel)
	if m.WifiScan.Connecting || m.WifiScan.ConnectErr == "" {
		t.Fatalf("expected ConnectErr after failure")
	}
	assertFitsBounds(t, m.View(), 40, 10, "40x10/WiFi/ConnectError")

	nextM, _ = m.Update(wifiConnectDoneMsg{req: req, err: nil})
	m = nextM.(AppModel)
	if m.WifiScan.Mode != scanModeList || m.WifiScan.NoteIsErr {
		t.Fatalf("expected scanModeList and success note, got mode=%v note=%q", m.WifiScan.Mode, m.WifiScan.Note)
	}
	assertFitsBounds(t, m.View(), 40, 10, "40x10/WiFi/ConnectSuccess")

	// Open network skips password and immediately enters Connecting=true
	m = sendKey(m, "down") // FreeSpot
	m = sendKey(m, "c")    // PickIface
	m = sendKey(m, "enter")
	if !m.WifiScan.Connecting {
		t.Fatalf("expected Open network to start connecting immediately after interface pick")
	}
	openReq := m.WifiScan.BuildConnectRequest()
	if openReq.SSID != "FreeSpot" || openReq.Iface != "wlan0" || openReq.Hidden {
		t.Fatalf("unexpected open connect request: %+v", openReq)
	}
}
