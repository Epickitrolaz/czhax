package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/config"
)

func TestFilesViewAllModesBounds(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"CZ_40x10", 40, 10},
		{"CZ_45x10", 45, 10},
		{"CZ_53x14", 53, 14},
		{"Wide_100x30", 100, 30},
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "music"), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 120, A: 255})
		}
	}
	imgPath := filepath.Join(root, "pic.png")
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	for _, sz := range sizes {
		cfg := config.Default()
		cfg.WorkDir = root
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.3")
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)
		m.ActiveView = ViewFiles
		m.Focus = FocusContent
		m.Files.Reload()

		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Files/List")

		// Text editor.
		for _, e := range m.Files.Entries {
			if e.Name == "note.txt" {
				if err := m.Files.OpenText(e); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Files/Text")
		// Printable keys must be handled by the editor, not global shortcuts.
		m = sendKey(m, "?")
		m = sendKey(m, "q")
		if !strings.Contains(strings.Join(m.Files.TextLines, "\n"), "?q") {
			t.Fatalf("[%s] editor did not receive printable keys: %+v", sz.name, m.Files.TextLines)
		}
		m.Files.Mode = filesModeList

		// Image viewer.
		for _, e := range m.Files.Entries {
			if e.Name == "pic.png" {
				if err := m.Files.OpenImage(e); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Files/Image")
		m.Files.Mode = filesModeList

		// Input and delete prompts.
		m.Files.BeginInput(filesInputNewFile)
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Files/Input")
		m.Files.Mode = filesModeList
		m.Files.Cursor = 0
		m.Files.BeginDelete()
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Files/Delete")
		m.Files.Mode = filesModeList

		// Microphone recording screen.
		ctx, _, _, ok := m.Files.StartRecording()
		if !ok {
			t.Fatalf("[%s] could not start recording", sz.name)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Files/Record")
		m.Files.StopRecording()
		_ = ctx
		m.Files.FinishRecording(m.Files.RecordSeq, nil)
	}
}

func TestBluetoothCarwhispererMenuBounds(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"CZ_40x10", 40, 10},
		{"CZ_45x10", 45, 10},
		{"CZ_53x14", 53, 14},
		{"Wide_100x30", 100, 30},
	}
	for _, sz := range sizes {
		cfg := config.Default()
		cfg.WorkDir = t.TempDir()
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.3")
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)
		m.ActiveView = ViewBluetooth
		m.Focus = FocusContent
		next, _ = m.Update(bluetoothRefreshedMsg{adapters: []bluetooth.Adapter{{Name: "hci0", Address: "AA:BB:CC:DD:EE:00"}}})
		m = next.(AppModel)
		next, _ = m.Update(btDevicesScannedMsg{hci: "hci0", devices: []bluetooth.Device{{Address: "AA:BB:CC:DD:EE:01", Name: "Headset", Kind: "Classic"}}})
		m = next.(AppModel)
		m.BluetoothScan.Cursor = 0
		m = sendKey(m, "enter") // actions
		m.BluetoothScan.ActionCursor = 0
		m = sendKey(m, "enter") // Carwhisperer menu
		if m.BluetoothScan.Mode != btModeCWMenu {
			t.Fatalf("[%s] expected CW menu, got %v", sz.name, m.BluetoothScan.Mode)
		}
		view := m.View()
		assertFitsBounds(t, view, sz.w, sz.h, sz.name+"/BT/CWMenu")
		for _, want := range []string{"Carwhisperer", "Tap", "Record message", "Play audio", "Start", "[ ]", "Target"} {
			if !strings.Contains(view, want) {
				t.Fatalf("[%s] expected %q in CW menu:\n%s", sz.name, want, view)
			}
		}
		m.BluetoothScan.CWTap = true
		tapView := m.View()
		if !strings.Contains(tapView, "[x] Tap") {
			t.Fatalf("[%s] expected checked Tap row:\n%s", sz.name, tapView)
		}
		m.BluetoothScan.CWTap = false
		m.BluetoothScan.StartCWTargetEdit()
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BT/CWTarget")
		m.BluetoothScan.CWTargetInput = "84:D3:52:E8:E8:FB"
		if !m.BluetoothScan.ConfirmCWTarget() || m.BluetoothScan.CWTarget != "84:D3:52:E8:E8:FB" {
			t.Fatalf("[%s] target address not confirmed", sz.name)
		}
		m.BluetoothScan.OpenCWAudio()
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BT/CWAudio")
		m.BluetoothScan.Mode = btModeCWMenu

		ctx, _, _, ok := m.BluetoothScan.StartCWRecord()
		if !ok {
			t.Fatalf("[%s] could not start CW mic recording", sz.name)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BT/CWRecord")
		m.BluetoothScan.StopCWRecord()
		_ = ctx
		m.BluetoothScan.FinishCWRecord(m.BluetoothScan.CWRecordSeq, nil)
	}
}
