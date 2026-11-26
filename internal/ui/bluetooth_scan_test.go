package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/config"
)

func TestBluetoothScanTabAllSizes(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"CZ_40x10", 40, 10},
		{"CZ_45x10", 45, 10},
		{"CZ_53x14", 53, 14},
		{"Wide_100x30", 100, 30},
	}

	devices := []bluetooth.Device{
		{
			Address:   "AA:BB:CC:DD:EE:01",
			Name:      "Headset",
			RSSI:      -54,
			HasRSSI:   true,
			Kind:      "Classic",
			Class:     "0x240404",
			Icon:      "audio-card",
			FastPair:  false,
			UUIDs:     []string{"0000110b-0000-1000-8000-00805f9b34fb"},
			Paired:    true,
			Connected: true,
		},
		{
			Address:     "AA:BB:CC:DD:EE:02",
			Name:        "Buds",
			RSSI:        -72,
			HasRSSI:     true,
			Kind:        "BLE",
			FastPair:    true,
			ModelID:     0x345678,
			HasModelID:  true,
			ServiceData: []byte{0x78, 0x56, 0x34},
		},
	}

	for _, sz := range sizes {
		cfg := config.Default()
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.0")
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)
		m.ActiveView = ViewBluetooth
		m.Focus = FocusContent

		next, _ = m.Update(bluetoothRefreshedMsg{adapters: []bluetooth.Adapter{{Name: "hci0", Address: "AA:BB:CC:DD:EE:00"}}})
		m = next.(AppModel)
		next, _ = m.Update(btDevicesScannedMsg{hci: "hci0", devices: devices})
		m = next.(AppModel)

		view := m.View()
		assertFitsBounds(t, view, sz.w, sz.h, sz.name+"/BT/List")
		if !strings.Contains(view, "Buds") || !strings.Contains(view, "FP") {
			t.Fatalf("[%s] expected Fast Pair device in list:\n%s", sz.name, view)
		}

		// Scan mode cycling: Both -> BLE -> Classic.
		m = sendKey(m, "m")
		if m.BluetoothScan.ScanMode != bluetooth.ScanLE {
			t.Fatalf("[%s] expected BLE scan mode, got %v", sz.name, m.BluetoothScan.ScanMode)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BT/ModeBLE")
		m = sendKey(m, "m")
		if m.BluetoothScan.ScanMode != bluetooth.ScanClassic {
			t.Fatalf("[%s] expected Classic scan mode, got %v", sz.name, m.BluetoothScan.ScanMode)
		}
		m = sendKey(m, "m")

		// Actions menu.
		m.BluetoothScan.Cursor = 0
		m = sendKey(m, "enter")
		if m.BluetoothScan.Mode != btModeActions {
			t.Fatalf("[%s] expected actions mode, got %v", sz.name, m.BluetoothScan.Mode)
		}
		actions := m.View()
		assertFitsBounds(t, actions, sz.w, sz.h, sz.name+"/BT/Actions")
		for _, want := range []string{"Carwhisperer", "WhisperPair", "l2ping Flood"} {
			if !strings.Contains(actions, want) {
				t.Fatalf("[%s] expected %q in actions:\n%s", sz.name, want, actions)
			}
		}

		// Device details (last row).
		m = sendKey(m, "up") // wrap to Device Details
		m = sendKey(m, "enter")
		if m.BluetoothScan.Mode != btModeDetail {
			t.Fatalf("[%s] expected detail mode, got %v", sz.name, m.BluetoothScan.Mode)
		}
		detail := m.View()
		assertFitsBounds(t, detail, sz.w, sz.h, sz.name+"/BT/Detail")
		if !strings.Contains(detail, "Address") || !strings.Contains(detail, "Buds") {
			t.Fatalf("[%s] expected device detail fields:\n%s", sz.name, detail)
		}

		// Back to actions, then start an l2ping flood and stream output.
		m = sendKey(m, "esc")
		if m.BluetoothScan.Mode != btModeActions {
			t.Fatalf("[%s] expected back to actions, got %v", sz.name, m.BluetoothScan.Mode)
		}
		m.BluetoothScan.ActionCursor = 2 // l2ping Flood
		ctx, req, seq, _, ok := m.BluetoothScan.StartAttack(btActL2Flood)
		if !ok || req.Kind != bluetooth.AttackL2Flood {
			t.Fatalf("[%s] expected l2ping attack request, ok=%v req=%+v", sz.name, ok, req)
		}
		defer ctx.Done()
		next, _ = m.Update(btAttackLineMsg{seq: seq, line: "reply from AA:BB:CC:DD:EE:01"})
		m = next.(AppModel)
		m.BluetoothScan.RunElapsed = 2 * time.Second
		running := m.View()
		assertFitsBounds(t, running, sz.w, sz.h, sz.name+"/BT/Running")
		if !strings.Contains(strings.ToLower(running), "l2ping flood") || !strings.Contains(running, "reply from") {
			t.Fatalf("[%s] expected running output:\n%s", sz.name, running)
		}
		next, _ = m.Update(btAttackDoneMsg{seq: seq, kind: req.Kind})
		m = next.(AppModel)
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BT/Done")
		if m.BluetoothScan.RunActive {
			t.Fatalf("[%s] expected attack to finish", sz.name)
		}

		// Esc from the finished run returns to actions, then the list.
		m = sendKey(m, "esc")
		if m.BluetoothScan.Mode != btModeActions {
			t.Fatalf("[%s] expected actions after run, got %v", sz.name, m.BluetoothScan.Mode)
		}
		m = sendKey(m, "esc")
		if m.BluetoothScan.Mode != btModeList {
			t.Fatalf("[%s] expected list after actions, got %v", sz.name, m.BluetoothScan.Mode)
		}
		m = sendKey(m, "esc")
		if m.Focus != FocusTabs {
			t.Fatalf("[%s] expected FocusTabs on esc from list", sz.name)
		}
	}
}

func TestBluetoothScanAdapterCycle(t *testing.T) {
	m := NewAppModel(config.Default(), filepath.Join(t.TempDir(), "config.json"), "v0.5.0")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 53, Height: 14})
	m = next.(AppModel)
	m.ActiveView = ViewBluetooth
	m.Focus = FocusContent
	next, _ = m.Update(bluetoothRefreshedMsg{adapters: []bluetooth.Adapter{
		{Name: "hci0", Address: "AA:BB:CC:DD:EE:00"},
		{Name: "hci1", Address: "AA:BB:CC:DD:EE:01"},
	}})
	m = next.(AppModel)
	if got := m.BluetoothScan.EffectiveHCI(); got != "hci0" {
		t.Fatalf("expected hci0, got %q", got)
	}
	m = sendKey(m, "right")
	if got := m.BluetoothScan.EffectiveHCI(); got != "hci1" {
		t.Fatalf("expected hci1 after right, got %q", got)
	}
	m = sendKey(m, "left")
	if got := m.BluetoothScan.EffectiveHCI(); got != "hci0" {
		t.Fatalf("expected hci0 after left, got %q", got)
	}
}
