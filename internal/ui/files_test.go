package ui

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/theme"
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
	if err := os.WriteFile(filepath.Join(root, "clip.wav"), []byte("RIFF0000WAVE"), 0o644); err != nil {
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

		// Audio playback popup (state set directly: no external player in CI).
		m.Files.Mode = filesModePlay
		m.Files.Playing = true
		m.Files.PlayName = "clip.wav"
		m.Files.PlayStarted = time.Now()
		m.Files.PlayElapsed = 3 * time.Second
		m.Files.PlayHasPos = true
		m.Files.PlayPos = 12.5
		m.Files.PlayDur = 180
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Files/Play")
		m.Files.PlayPaused = true
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Files/PlayPaused")
		m.Files.PlayPaused = false
		m.Files.PlayHasPos = false
		m.Files.Mode = filesModeList
		m.Files.Playing = false
	}
}

func TestFilesShortNamesDoNotTriggerMarquee(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "user", "audio"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "user", "audio", "clip.mp3"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, sz := range []struct {
		name string
		w, h int
	}{{"CZ_40x10", 40, 10}, {"CZ_45x10", 45, 10}, {"CZ_53x14", 53, 14}} {
		cfg := config.Default()
		cfg.WorkDir = root
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.22")
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)
		_, mainW, bodyH := m.dimensions()
		pal := theme.Resolve(m.Cfg.Theme, false)

		var overflow bool
		fv := m.Files
		fv.Marquee = Marquee{Overflow: &overflow}
		fv.Reload()
		lines := fv.RenderLines(mainW, bodyH, pal, true)
		if overflow {
			t.Fatalf("[%s] short filenames in Files view triggered marquee overflow:\n%s", sz.name, strings.Join(lines, "\n"))
		}

		// Also verify Carwhisperer audio picker does not scroll short filenames.
		var btOverflow bool
		bv := m.BluetoothScan
		bv.Marquee = Marquee{Overflow: &btOverflow}
		bv.Devices = []bluetooth.Device{{Address: "84:D3:52:E8:E8:FB", Name: "SRS-XB100", Kind: "Classic"}}
		bv.OpenCWAudio()
		btLines := bv.RenderLines(mainW, bodyH, pal, true)
		if btOverflow {
			t.Fatalf("[%s] short filenames in CW audio picker triggered marquee overflow:\n%s", sz.name, strings.Join(btLines, "\n"))
		}
	}
}

func TestCWAudioOnlyListsUserAudioDir(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{
		filepath.Join(root, "captures", "carwhisperer-112233445566-1.wav"),
		filepath.Join(root, "temp", "conv.wav"),
		filepath.Join(root, "user", "recordings", "mic-1.wav"),
		filepath.Join(root, "user", "audio", "rick.mp3"),
		filepath.Join(root, "user", "audio", "sub", "horn.wav"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("RIFF"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v := NewBluetoothScanView()
	v.WorkDir = root
	if !v.OpenCWAudio() {
		t.Fatal("expected OpenCWAudio to succeed")
	}
	if len(v.CWAudio) != 3 {
		t.Fatalf("expected 3 files (2 audio + 1 recording), got %d: %+v", len(v.CWAudio), v.CWAudio)
	}
	// user/audio entries come first (on top), user/recordings after.
	for i, e := range v.CWAudio[:2] {
		if !strings.Contains(e.Path, filepath.Join("user", "audio")) {
			t.Fatalf("entry %d is not user/audio: %q", i, e.Path)
		}
		if !strings.HasPrefix(e.Rel, "audio/") {
			t.Fatalf("entry %d missing audio/ label: %q", i, e.Rel)
		}
	}
	if !strings.Contains(v.CWAudio[2].Path, filepath.Join("user", "recordings")) {
		t.Fatalf("expected recording last, got %q", v.CWAudio[2].Path)
	}
	if !strings.HasPrefix(v.CWAudio[2].Rel, "rec/") {
		t.Fatalf("expected rec/ label, got %q", v.CWAudio[2].Rel)
	}
	if got := v.ConvertPath(); got != filepath.Join(root, "temp", "playback.raw") {
		t.Fatalf("ConvertPath() = %q, want temp/playback.raw", got)
	}
	if got := bluetooth.CarwhispererRecordPathIn(root, "84:D3:52:E8:E8:FB"); !strings.HasPrefix(got, filepath.Join(root, "captures")) {
		t.Fatalf("CarwhispererRecordPathIn() = %q, want under captures/", got)
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
		// Seed both libraries so the picker shows prefixed labels at every size.
		for _, p := range []string{
			filepath.Join(cfg.WorkDir, "user", "audio", "a-very-long-audio-file-name-for-marquee.mp3"),
			filepath.Join(cfg.WorkDir, "user", "recordings", "mic-20260101-120000.wav"),
		} {
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			_ = os.WriteFile(p, []byte("RIFF"), 0o644)
		}
		m.BluetoothScan.OpenCWAudio()
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/BT/CWAudio")
		if !strings.Contains(m.View(), "audio/") {
			t.Fatalf("[%s] expected audio/ label in picker:\n%s", sz.name, m.View())
		}
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

func TestCWRecordUsesTempAndSendsOnStop(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.WorkDir = root
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.5.24")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 53, Height: 14})
	m = next.(AppModel)
	m.ActiveView = ViewBluetooth
	m.Focus = FocusContent
	next, _ = m.Update(bluetoothRefreshedMsg{adapters: []bluetooth.Adapter{{Name: "hci0", Address: "AA:BB:CC:DD:EE:00"}}})
	m = next.(AppModel)
	next, _ = m.Update(btDevicesScannedMsg{hci: "hci0", devices: []bluetooth.Device{{Address: "AA:BB:CC:DD:EE:01", Name: "Headset", Kind: "Classic"}}})
	m = next.(AppModel)
	m.BluetoothScan.Cursor = 0
	m = sendKey(m, "enter")
	m.BluetoothScan.ActionCursor = 0
	m = sendKey(m, "enter")
	if m.BluetoothScan.Mode != btModeCWMenu {
		t.Fatalf("expected CW menu, got %v", m.BluetoothScan.Mode)
	}

	// Record message goes to temp/, never user/recordings.
	ctx, seq, path, ok := m.BluetoothScan.StartCWRecord()
	if !ok {
		t.Fatal("expected recording to start")
	}
	_ = ctx
	if !strings.HasPrefix(path, filepath.Join(root, "temp")) {
		t.Fatalf("record path %q should live under temp/", path)
	}
	if strings.Contains(path, "recordings") {
		t.Fatalf("record path %q must not use user/recordings", path)
	}
	if err := os.WriteFile(path, []byte("RIFFfake"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Enter stops the take: cancellation is the normal stop path and must still
	// queue the conversion + Carwhisperer run (regression: it used to just stop).
	next, cmd := m.Update(btMicDoneMsg{seq: seq, path: path, err: context.Canceled})
	m = next.(AppModel)
	if cmd == nil {
		t.Fatal("expected a follow-up command after stopping the recording")
	}
	if m.BluetoothScan.Mode == btModeCWRecord {
		t.Fatal("recording screen should be left after stop")
	}

	// Esc-discard removes the temp take instead of sending it.
	_, seq2, path2, ok2 := m.BluetoothScan.StartCWRecord()
	if !ok2 {
		t.Fatal("expected second recording to start")
	}
	if err := os.WriteFile(path2, []byte("RIFFfake2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.BluetoothScan.CWDiscardMsg = true
	next, _ = m.Update(btMicDoneMsg{seq: seq2, path: path2, err: context.Canceled})
	m = next.(AppModel)
	if _, err := os.Stat(path2); !os.IsNotExist(err) {
		t.Fatalf("discarded take should be removed, stat err=%v", err)
	}

	// Cleanup removes both the take and the converted PCM.
	m.BluetoothScan.CWRecordPath = filepath.Join(root, "temp", "cw-message.wav")
	_ = os.WriteFile(m.BluetoothScan.CWRecordPath, []byte("x"), 0o644)
	_ = os.WriteFile(m.BluetoothScan.ConvertPath(), []byte("x"), 0o644)
	m.BluetoothScan.CleanupCWMessage()
	if _, err := os.Stat(filepath.Join(root, "temp", "cw-message.wav")); !os.IsNotExist(err) {
		t.Fatal("expected temp take to be deleted")
	}
	if _, err := os.Stat(m.BluetoothScan.ConvertPath()); !os.IsNotExist(err) {
		t.Fatal("expected converted PCM to be deleted")
	}
}
