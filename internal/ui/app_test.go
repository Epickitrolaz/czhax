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

		// Down moves to System view
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
