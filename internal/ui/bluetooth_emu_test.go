package ui

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/config"
)

func TestBluetoothEmuBoundsAllModesAndSizes(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"CZ_53x14", 53, 14},
		{"CZ_45x10", 45, 10},
		{"CZ_40x10", 40, 10},
		{"Wide_100x30", 100, 30},
	}

	adapters := []bluetooth.Adapter{
		{Name: "hci0", Address: "F0:40:AF:90:59:A5", Bus: "UART", Up: true},
		{Name: "hci1", Address: "00:E0:03:00:01:74", Bus: "USB", Up: true},
	}

	for _, sz := range sizes {
		cfg := config.Default()
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.24")
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)
		m.Bluetooth.SetAdapters(adapters)
		m.BluetoothEmu.SyncAdapters(adapters)

		// Navigate to BT -> Emu tab and enter content.
		m.ActiveView = ViewBluetooth
		m.ActiveTabs[ViewBluetooth] = BTTabEmu
		m.Focus = FocusContent

		// Setup: Keyboard
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Setup/Keyboard")

		// Setup: Custom (extra Class/Stack/UUID fields)
		m.BluetoothEmu.ProfileIdx = len(bluetooth.EmuProfiles) - 1
		m.BluetoothEmu.ErrMsg = "test error banner"
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Setup/Custom")

		// Setup: Speaker with LastRecordPath
		m.BluetoothEmu.ErrMsg = ""
		m.BluetoothEmu.ProfileIdx = 5 // Speaker
		m.BluetoothEmu.LastRecordPath = "/tmp/bt-speaker-test.wav"
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Setup/Speaker")

		// Text prompts
		m.BluetoothEmu.Mode = btEmuEditName
		m.BluetoothEmu.InputBuf = "My Speaker Pro"
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/EditName")

		// Playback mode
		m.BluetoothEmu.Mode = btEmuPlayWAV
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/PlayWAV")

		// Active modes
		m.BluetoothEmu.Mode = btEmuActive
		m.BluetoothEmu.Active = true
		m.BluetoothEmu.Ready = true
		m.BluetoothEmu.Connected = true
		m.BluetoothEmu.PeerName = "MacBook Pro"

		// Speaker active menu
		m.BluetoothEmu.Spec = bluetooth.FindEmuProfile(bluetooth.EmuSpeaker)
		m.BluetoothEmu.AudioLevel = 65
		m.BluetoothEmu.SampleRate = 44100
		m.BluetoothEmu.RecordAudio = true
		m.BluetoothEmu.RecordSec = 14
		m.BluetoothEmu.SubMode = btEmuCtrlMenu
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Active/SpeakerMenu")

		// Combo HID active modes
		m.BluetoothEmu.Spec = bluetooth.FindEmuProfile(bluetooth.EmuCombo)
		m.BluetoothEmu.SubMode = btEmuCtrlMenu
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Active/HIDMenu")

		m.BluetoothEmu.SubMode = btEmuCtrlLiveKB
		m.BluetoothEmu.LEDs = 0x02
		m.BluetoothEmu.LastSent = "ctrl+c"
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Active/LiveKB")

		m.BluetoothEmu.SubMode = btEmuCtrlSendText
		m.BluetoothEmu.InputBuf = "echo hello world"
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Active/SendText")

		m.BluetoothEmu.SubMode = btEmuCtrlMacros
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Active/Macros")

		m.BluetoothEmu.SubMode = btEmuCtrlMouse
		m.BluetoothEmu.MouseDrag = true
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Active/Mouse")

		m.BluetoothEmu.SubMode = btEmuCtrlGamepad
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Active/Gamepad")

		m.BluetoothEmu.SubMode = btEmuCtrlMedia
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Active/Media")

		m.BluetoothEmu.SubMode = btEmuCtrlLog
		m.BluetoothEmu.Logs = []string{"GATT HID service active", "Connected: MacBook Pro"}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BTEmu/Active/Log")
	}
}

func TestBluetoothEmuNavigationAndDirectKeys(t *testing.T) {
	cfg := config.Default()
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.24")
	m.Bluetooth.SetAdapters([]bluetooth.Adapter{
		{Name: "hci0", Bus: "UART", Up: true},
		{Name: "hci1", Bus: "USB", Up: true},
	})
	m.BluetoothEmu.SyncAdapters(m.Bluetooth.Adapters)
	if m.BluetoothEmu.EffectiveHCI() != "hci1" {
		t.Fatalf("expected USB adapter hci1 preferred, got %s", m.BluetoothEmu.EffectiveHCI())
	}

	// Navigate FocusTabs to ViewBluetooth -> Emu tab -> FocusContent
	m.ActiveView = ViewBluetooth
	m = sendKey(m, "right")
	if m.ActiveTabs[ViewBluetooth] != BTTabEmu {
		t.Fatalf("expected BTTabEmu, got %d", m.ActiveTabs[ViewBluetooth])
	}
	m = sendKey(m, "enter")
	if m.Focus != FocusContent {
		t.Fatalf("expected FocusContent, got %v", m.Focus)
	}

	// Cycle profile Keyboard -> Mouse
	m = sendKey(m, "right")
	if m.BluetoothEmu.BuildConfig().Profile != bluetooth.EmuMouse {
		t.Fatalf("expected EmuMouse, got %s", m.BluetoothEmu.BuildConfig().Profile)
	}

	// Edit device name (move down to Name, press enter, type, enter)
	m = sendKey(m, "down")
	m = sendKey(m, "enter")
	if m.BluetoothEmu.Mode != btEmuEditName {
		t.Fatalf("expected btEmuEditName, got %v", m.BluetoothEmu.Mode)
	}
	for range "CZHax Mouse" {
		m = sendKey(m, "backspace")
	}
	for _, r := range "Desk Rodent" {
		m = sendKey(m, string(r))
	}
	m = sendKey(m, "enter")
	if m.BluetoothEmu.Mode != btEmuSetup || m.BluetoothEmu.BuildConfig().Name != "Desk Rodent" {
		t.Fatalf("expected saved name Desk Rodent, got mode=%v name=%q", m.BluetoothEmu.Mode, m.BluetoothEmu.BuildConfig().Name)
	}

	// Simulate active Combo HID session and verify LiveKB and Mouse forward keys without quitting/refreshing.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmdCh := make(chan bluetooth.EmuCommand, 16)
	m.BluetoothEmu.Mode = btEmuActive
	m.BluetoothEmu.Active = true
	m.BluetoothEmu.Spec = bluetooth.FindEmuProfile(bluetooth.EmuCombo)
	m.BluetoothEmu.SubMode = btEmuCtrlLiveKB
	m.BluetoothEmu.CmdCh = cmdCh
	m.BluetoothEmu.Cancel = cancel
	_ = ctx

	// 'q' and 'r' in LiveKB must send HID key commands, NOT trigger quit/refresh!
	m = sendKey(m, "q")
	m = sendKey(m, "r")
	m = sendKey(m, " ")
	if len(cmdCh) != 3 {
		t.Fatalf("expected 3 HID key commands queued, got %d", len(cmdCh))
	}
	for i := 0; i < 3; i++ {
		c := <-cmdCh
		if c.Op != "key" {
			t.Fatalf("expected key op, got %+v", c)
		}
	}

	// Esc exits LiveKB back to btEmuCtrlMenu
	m = sendKey(m, "esc")
	if m.BluetoothEmu.SubMode != btEmuCtrlMenu {
		t.Fatalf("expected btEmuCtrlMenu after Esc, got %v", m.BluetoothEmu.SubMode)
	}

	// Mouse pad direct keys ('r' = right click, 'd' = move right, 'g' = drag toggle)
	m.BluetoothEmu.SubMode = btEmuCtrlMouse
	m = sendKey(m, "r")
	m = sendKey(m, "d")
	m = sendKey(m, "g")
	if len(cmdCh) != 3 {
		t.Fatalf("expected 3 mouse commands queued, got %d", len(cmdCh))
	}
	c1 := <-cmdCh
	c2 := <-cmdCh
	c3 := <-cmdCh
	if c1.Op != "mouse_click" || c1.Buttons != 0x02 {
		t.Fatalf("expected right click, got %+v", c1)
	}
	if c2.Op != "mouse" || c2.DX <= 0 {
		t.Fatalf("expected move right, got %+v", c2)
	}
	if c3.Op != "mouse" || c3.Buttons != 0x01 {
		t.Fatalf("expected drag hold, got %+v", c3)
	}
}
