package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/workdir"
)

type BTEmuMode int

const (
	btEmuSetup BTEmuMode = iota
	btEmuEditName
	btEmuEditCoD
	btEmuEditUUID
	btEmuActive
	btEmuPlayWAV
)

type BTEmuSubMode int

const (
	btEmuCtrlMenu BTEmuSubMode = iota
	btEmuCtrlLiveKB
	btEmuCtrlSendText
	btEmuCtrlMacros
	btEmuCtrlMouse
	btEmuCtrlGamepad
	btEmuCtrlMedia
	btEmuCtrlLog
)

type btEmuSetupField int

const (
	emuFieldProfile btEmuSetupField = iota
	emuFieldName
	emuFieldAdapter
	emuFieldCustomMode
	emuFieldClass
	emuFieldCustomUUID
	emuFieldMonitor
	emuFieldRecord
	emuFieldStart
	emuFieldPlayLast
)

type btEmuMenuAction int

const (
	emuActLiveKB btEmuMenuAction = iota
	emuActSendText
	emuActMacros
	emuActMouse
	emuActGamepad
	emuActMedia
	emuActMonitor
	emuActRecord
	emuActAVRCPPlay
	emuActAVRCPNext
	emuActAVRCPPrev
	emuActLog
	emuActStop
)

type btEmuMacro struct {
	Label string
	Op    string
	Key   string
	Code  uint8
	Mods  uint8
	Usage uint16
}

var btEmuMacros = []btEmuMacro{
	{Label: "Esc", Op: "key", Key: "esc"},
	{Label: "Win / Cmd", Op: "key", Mods: bluetooth.HIDModLGUI},
	{Label: "Win+R (Run)", Op: "key", Code: 0x15, Mods: bluetooth.HIDModLGUI},
	{Label: "Alt+Tab", Op: "key", Code: 0x2B, Mods: bluetooth.HIDModLAlt},
	{Label: "Ctrl+C (Copy)", Op: "key", Key: "ctrl+c"},
	{Label: "Ctrl+V (Paste)", Op: "key", Key: "ctrl+v"},
	{Label: "Ctrl+A (All)", Op: "key", Key: "ctrl+a"},
	{Label: "Ctrl+Alt+Del", Op: "key", Code: 0x4C, Mods: bluetooth.HIDModLCtrl | bluetooth.HIDModLAlt},
	{Label: "Enter", Op: "key", Key: "enter"},
	{Label: "Play / Pause", Op: "consumer", Usage: bluetooth.ConsumerPlayPause},
	{Label: "Volume Up", Op: "consumer", Usage: bluetooth.ConsumerVolUp},
	{Label: "Volume Down", Op: "consumer", Usage: bluetooth.ConsumerVolDown},
	{Label: "Mute", Op: "consumer", Usage: bluetooth.ConsumerMute},
}

type BluetoothEmuView struct {
	Mode    BTEmuMode
	SubMode BTEmuSubMode
	WorkDir string

	Adapters      []bluetooth.Adapter
	AdapterIdx    int
	AdapterManual bool

	ProfileIdx    int
	CustomName    string
	NameEdited    bool
	CustomModeIdx int
	CoDPresetIdx  int
	CustomCoDHex  string
	CustomUUID    string
	MonitorAudio  bool
	RecordAudio   bool

	SetupCursor int
	MenuCursor  int
	MacroCursor int
	LogScroll   int

	InputBuf string

	// Active emulation runtime state
	Active      bool
	Stopping    bool
	Seq         int
	Spec        bluetooth.EmuProfileSpec
	Ready       bool
	Connected   bool
	PeerPaired  bool
	PeerAddr    string
	PeerName    string
	Transport   string
	SampleRate  int
	Channels    int
	AudioLevel  int
	RecordPath  string
	RecordBytes int64
	RecordSec   int
	TXCount     int
	RXCount     int
	LEDs        uint8
	AVRCPPaused bool
	LastSent    string
	Logs        []string
	ErrMsg      string
	CmdCh       chan bluetooth.EmuCommand
	EventCh     chan bluetooth.EmuEvent
	Cancel      context.CancelFunc

	// Mouse state
	MouseStepIdx int
	MouseDrag    bool
	MouseDX      int
	MouseDY      int

	// Last saved audio recording playback
	LastRecordPath string
	PlaySeq        int
	PlayCancel     context.CancelFunc
	PlayStarted    time.Time
	PlayElapsed    time.Duration
	PlayErr        string

	Marquee Marquee
}

func NewBluetoothEmuView() BluetoothEmuView {
	return BluetoothEmuView{
		CustomName:   bluetooth.EmuProfiles[0].DefaultName,
		MonitorAudio: true,
		MouseStepIdx: 1,
	}
}

func (v BluetoothEmuView) IsTextInput() bool {
	if v.Mode == btEmuEditName || v.Mode == btEmuEditCoD || v.Mode == btEmuEditUUID {
		return true
	}
	if v.Mode == btEmuActive && (v.SubMode == btEmuCtrlLiveKB || v.SubMode == btEmuCtrlSendText) {
		return true
	}
	return false
}

// CapturesDirectKeys reports whether keys like r, q, ?, w/a/s/d must go directly
// to the emulator rather than global TUI shortcuts.
func (v BluetoothEmuView) CapturesDirectKeys() bool {
	if v.IsTextInput() {
		return true
	}
	if v.Mode == btEmuActive {
		switch v.SubMode {
		case btEmuCtrlMouse, btEmuCtrlGamepad, btEmuCtrlMedia:
			return true
		}
	}
	return false
}

func (v BluetoothEmuView) EffectiveHCI() string {
	if len(v.Adapters) == 0 {
		return "hci0"
	}
	return v.Adapters[clampIndex(v.AdapterIdx, len(v.Adapters))].Name
}

func (v BluetoothEmuView) EffectiveAdapter() (bluetooth.Adapter, bool) {
	if len(v.Adapters) == 0 {
		return bluetooth.Adapter{}, false
	}
	return v.Adapters[clampIndex(v.AdapterIdx, len(v.Adapters))], true
}

func (v *BluetoothEmuView) SyncAdapters(adapters []bluetooth.Adapter) {
	prev := ""
	if a, ok := v.EffectiveAdapter(); ok {
		prev = a.Name
	}
	v.Adapters = adapters
	if len(adapters) == 0 {
		v.AdapterIdx = 0
		return
	}
	if v.AdapterManual && prev != "" {
		for i, a := range adapters {
			if a.Name == prev {
				v.AdapterIdx = i
				return
			}
		}
	}
	for i, a := range adapters {
		if strings.EqualFold(a.Bus, "USB") && a.Up {
			v.AdapterIdx = i
			return
		}
	}
	for i, a := range adapters {
		if a.Name == "hci0" {
			v.AdapterIdx = i
			return
		}
	}
	v.AdapterIdx = 0
}

func (v *BluetoothEmuView) CycleAdapter(delta int) {
	if len(v.Adapters) <= 1 {
		return
	}
	v.AdapterIdx = (v.AdapterIdx + delta + len(v.Adapters)) % len(v.Adapters)
	v.AdapterManual = true
}

func (v BluetoothEmuView) currentProfile() bluetooth.EmuProfileSpec {
	return bluetooth.EmuProfiles[clampIndex(v.ProfileIdx, len(bluetooth.EmuProfiles))]
}

func (v BluetoothEmuView) currentCustomMode() bluetooth.EmuCustomMode {
	modes := []bluetooth.EmuCustomMode{bluetooth.CustomHID, bluetooth.CustomSpeaker, bluetooth.CustomAdvertise}
	return modes[clampIndex(v.CustomModeIdx, len(modes))]
}

func (v BluetoothEmuView) BuildConfig() bluetooth.EmuConfig {
	p := v.currentProfile()
	name := strings.TrimSpace(v.CustomName)
	if name == "" {
		name = p.DefaultName
	}
	cod := p.CoD
	app := p.Appearance
	if p.Kind == bluetooth.EmuCustom {
		preset := bluetooth.EmuCoDPresets[clampIndex(v.CoDPresetIdx, len(bluetooth.EmuCoDPresets))]
		cod = preset.CoD
		app = preset.Appearance
		if v.CustomCoDHex != "" {
			if parsed, err := bluetooth.ParseHexUint32(v.CustomCoDHex); err == nil {
				cod = parsed
			}
		}
	}
	root := workdir.Resolve(v.WorkDir)
	recDir := workdir.Captures(root)
	return bluetooth.EmuConfig{
		HCI:          v.EffectiveHCI(),
		Profile:      p.Kind,
		Name:         name,
		CoD:          cod,
		Appearance:   app,
		CustomMode:   v.currentCustomMode(),
		CustomUUID:   strings.TrimSpace(v.CustomUUID),
		RecordDir:    recDir,
		TempDir:      workdir.Temp(root),
		MonitorAudio: v.MonitorAudio,
		RecordAudio:  v.RecordAudio,
	}
}

func (v BluetoothEmuView) setupFields() []btEmuSetupField {
	p := v.currentProfile()
	fields := []btEmuSetupField{
		emuFieldProfile,
		emuFieldName,
		emuFieldAdapter,
	}
	if p.Kind == bluetooth.EmuCustom {
		fields = append(fields, emuFieldCustomMode, emuFieldClass, emuFieldCustomUUID)
	}
	isAudio := p.IsAudio || (p.Kind == bluetooth.EmuCustom && v.currentCustomMode() == bluetooth.CustomSpeaker)
	if isAudio {
		fields = append(fields, emuFieldMonitor, emuFieldRecord)
	}
	fields = append(fields, emuFieldStart)
	if strings.TrimSpace(v.LastRecordPath) != "" {
		fields = append(fields, emuFieldPlayLast)
	}
	return fields
}

func (v *BluetoothEmuView) MoveSetup(delta int) {
	fields := v.setupFields()
	if len(fields) == 0 {
		return
	}
	v.SetupCursor = (v.SetupCursor + delta + len(fields)) % len(fields)
}

func (v *BluetoothEmuView) CycleProfile(delta int) {
	oldDef := v.currentProfile().DefaultName
	n := len(bluetooth.EmuProfiles)
	v.ProfileIdx = (v.ProfileIdx + delta + n) % n
	if !v.NameEdited || strings.TrimSpace(v.CustomName) == "" || v.CustomName == oldDef {
		v.CustomName = v.currentProfile().DefaultName
		v.NameEdited = false
	}
	fields := v.setupFields()
	v.SetupCursor = clampIndex(v.SetupCursor, len(fields))
}

func (v *BluetoothEmuView) CycleCustomMode(delta int) {
	v.CustomModeIdx = (v.CustomModeIdx + delta + 3) % 3
	fields := v.setupFields()
	v.SetupCursor = clampIndex(v.SetupCursor, len(fields))
}

func (v *BluetoothEmuView) CycleCoDPreset(delta int) {
	n := len(bluetooth.EmuCoDPresets)
	v.CoDPresetIdx = (v.CoDPresetIdx + delta + n) % n
	v.CustomCoDHex = ""
}

func (v *BluetoothEmuView) StartEmulation() (context.Context, int, bluetooth.EmuConfig, chan bluetooth.EmuCommand, chan bluetooth.EmuEvent) {
	v.StopEmulation()
	v.Seq++
	cfg := v.BuildConfig()
	v.Spec = cfg.EffectiveSpec()
	v.Mode = btEmuActive
	v.SubMode = btEmuCtrlMenu
	if v.Spec.Kind == bluetooth.EmuMouse {
		v.SubMode = btEmuCtrlMouse
	} else if v.Spec.Kind == bluetooth.EmuGamepad {
		v.SubMode = btEmuCtrlGamepad
	} else if v.Spec.Kind == bluetooth.EmuMedia {
		v.SubMode = btEmuCtrlMedia
	}
	v.Active = true
	v.Stopping = false
	v.Ready = false
	v.Connected = false
	v.PeerPaired = false
	v.PeerAddr = ""
	v.PeerName = ""
	v.Transport = ""
	v.SampleRate = 0
	v.Channels = 0
	v.AudioLevel = 0
	v.RecordPath = ""
	v.RecordBytes = 0
	v.RecordSec = 0
	v.TXCount = 0
	v.RXCount = 0
	v.LEDs = 0
	v.AVRCPPaused = false
	v.LastSent = ""
	v.ErrMsg = ""
	v.Logs = []string{fmt.Sprintf("Starting %s on %s...", v.Spec.Label, cfg.HCI)}
	v.MenuCursor = 0
	v.MacroCursor = 0
	v.LogScroll = 0

	ctx, cancel := context.WithCancel(context.Background())
	v.Cancel = cancel
	v.CmdCh = make(chan bluetooth.EmuCommand, 32)
	v.EventCh = make(chan bluetooth.EmuEvent, 64)
	return ctx, v.Seq, cfg, v.CmdCh, v.EventCh
}

func (v *BluetoothEmuView) StopEmulation() {
	if v.CmdCh != nil {
		select {
		case v.CmdCh <- bluetooth.EmuCommand{Op: "stop"}:
		default:
		}
	}
	if v.Cancel != nil {
		v.Cancel()
		v.Cancel = nil
	}
	v.Stopping = v.Active
}

func (v *BluetoothEmuView) SendCmd(cmd bluetooth.EmuCommand) bool {
	if !v.Active || v.CmdCh == nil {
		return false
	}
	select {
	case v.CmdCh <- cmd:
		return true
	default:
		return false
	}
}

func (v *BluetoothEmuView) ApplyEvent(seq int, ev bluetooth.EmuEvent) {
	if seq != v.Seq {
		return
	}
	switch ev.Kind {
	case "log":
		if ev.Text != "" {
			v.appendLog(ev.Text)
		}
	case "error":
		if ev.Text != "" {
			v.ErrMsg = ev.Text
			v.appendLog("Error: " + ev.Text)
		}
	case "state":
		if ev.Ready {
			v.Ready = true
		}
		if ev.Transport != "" {
			v.Transport = ev.Transport
		}
		if ev.Ready {
			// Only a real state (re)announcement carries the audio toggles; LED
			// updates arrive as their own event kind so they cannot clobber them.
			v.MonitorAudio = ev.MonitorOn
			v.RecordAudio = ev.Recording
		}
	case "led":
		v.LEDs = ev.LEDs
	case "peer":
		v.Connected = ev.Connected
		v.PeerPaired = ev.Connected && ev.PeerPaired
		v.PeerAddr = ev.PeerAddr
		v.PeerName = ev.PeerName
	case "audio":
		if ev.SampleRate > 0 {
			v.SampleRate = ev.SampleRate
		}
		if ev.Channels > 0 {
			v.Channels = ev.Channels
		}
		v.AudioLevel = ev.AudioLevel
		v.MonitorAudio = ev.MonitorOn
		v.RecordAudio = ev.Recording
		if ev.RecordPath != "" {
			v.RecordPath = ev.RecordPath
			v.LastRecordPath = ev.RecordPath
		}
		v.RecordBytes = ev.RecordBytes
		v.RecordSec = ev.RecordSec
		if ev.RXCount > 0 {
			v.RXCount = ev.RXCount
		}
	case "stats":
		if ev.TXCount > 0 {
			v.TXCount = ev.TXCount
		}
		if ev.RXCount > 0 {
			v.RXCount = ev.RXCount
		}
	}
}

func (v *BluetoothEmuView) DrainEvents(seq int) {
	if seq != v.Seq || v.EventCh == nil {
		return
	}
	for {
		select {
		case ev, ok := <-v.EventCh:
			if !ok {
				return
			}
			v.ApplyEvent(seq, ev)
		default:
			return
		}
	}
}

func (v *BluetoothEmuView) FinishEmulation(seq int, err error) {
	if seq != v.Seq {
		return
	}
	v.DrainEvents(seq)
	v.Active = false
	v.Stopping = false
	v.Cancel = nil
	v.CmdCh = nil
	v.EventCh = nil
	if v.RecordPath != "" {
		if _, stErr := os.Stat(v.RecordPath); stErr == nil {
			v.LastRecordPath = v.RecordPath
		}
	}
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		v.ErrMsg = err.Error()
		v.appendLog("Stopped: " + err.Error())
	} else {
		v.Mode = btEmuSetup
	}
}

func (v *BluetoothEmuView) appendLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	v.Logs = append(v.Logs, line)
	if len(v.Logs) > 80 {
		v.Logs = v.Logs[len(v.Logs)-80:]
	}
}

func (v BluetoothEmuView) menuItems() []btEmuMenuAction {
	var out []btEmuMenuAction
	if v.Spec.HasKB {
		out = append(out, emuActLiveKB, emuActSendText, emuActMacros)
	}
	if v.Spec.HasMouse {
		out = append(out, emuActMouse)
	}
	if v.Spec.HasGamepad {
		out = append(out, emuActGamepad)
	}
	if v.Spec.HasConsumer && !v.Spec.HasKB && !v.Spec.HasGamepad {
		out = append(out, emuActMedia)
	}
	if v.Spec.IsAudio {
		out = append(out, emuActMonitor, emuActRecord, emuActAVRCPPlay, emuActAVRCPNext, emuActAVRCPPrev)
	}
	out = append(out, emuActLog, emuActStop)
	return out
}

func (v *BluetoothEmuView) MoveMenu(delta int) {
	items := v.menuItems()
	if len(items) == 0 {
		return
	}
	v.MenuCursor = (v.MenuCursor + delta + len(items)) % len(items)
}

func (v *BluetoothEmuView) MoveMacro(delta int) {
	n := len(btEmuMacros)
	if n == 0 {
		return
	}
	v.MacroCursor = (v.MacroCursor + delta + n) % n
}

func (v *BluetoothEmuView) StartPlayLast() (context.Context, int, string, bool) {
	if strings.TrimSpace(v.LastRecordPath) == "" {
		return nil, 0, "", false
	}
	v.StopPlayLast()
	v.PlaySeq++
	v.Mode = btEmuPlayWAV
	v.PlayStarted = time.Now()
	v.PlayElapsed = 0
	v.PlayErr = ""
	ctx, cancel := context.WithCancel(context.Background())
	v.PlayCancel = cancel
	return ctx, v.PlaySeq, v.LastRecordPath, true
}

func (v *BluetoothEmuView) StopPlayLast() {
	if v.PlayCancel != nil {
		v.PlayCancel()
		v.PlayCancel = nil
	}
}

func (v *BluetoothEmuView) FinishPlayLast(seq int, err error) {
	if seq != v.PlaySeq {
		return
	}
	v.PlayCancel = nil
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		v.PlayErr = err.Error()
		return
	}
	v.Mode = btEmuSetup
}

func (v *BluetoothEmuView) HandleEsc() bool {
	switch v.Mode {
	case btEmuEditName, btEmuEditCoD, btEmuEditUUID:
		v.Mode = btEmuSetup
		v.InputBuf = ""
		return true
	case btEmuPlayWAV:
		v.StopPlayLast()
		v.Mode = btEmuSetup
		return true
	case btEmuActive:
		if v.SubMode != btEmuCtrlMenu {
			if v.MouseDrag {
				v.MouseDrag = false
				v.SendCmd(bluetooth.EmuCommand{Op: "mouse", Buttons: 0})
			}
			v.SubMode = btEmuCtrlMenu
			return true
		}
		v.StopEmulation()
		if !v.Active {
			v.Mode = btEmuSetup
		}
		return true
	}
	return false
}

func (v BluetoothEmuView) MouseStep() int8 {
	steps := []int8{6, 18, 40}
	return steps[clampIndex(v.MouseStepIdx, len(steps))]
}

func (v BluetoothEmuView) MouseStepLabel() string {
	labels := []string{"Slow", "Med", "Fast"}
	return labels[clampIndex(v.MouseStepIdx, len(labels))]
}

// RenderLines renders BT -> Emu strictly inside w x h.
func (v BluetoothEmuView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	switch v.Mode {
	case btEmuEditName:
		return v.renderTextPrompt(w, h, pal, "Device Name", "Name shown to phones & PCs:", v.InputBuf, focused)
	case btEmuEditCoD:
		return v.renderTextPrompt(w, h, pal, "Class of Device", "24-bit hex CoD (e.g. 0x002540):", v.InputBuf, focused)
	case btEmuEditUUID:
		return v.renderTextPrompt(w, h, pal, "Custom Service UUID", "16-bit/128-bit UUID (blank=none):", v.InputBuf, focused)
	case btEmuPlayWAV:
		return v.renderPlayWAV(w, h, pal)
	case btEmuActive:
		return v.renderActive(w, h, pal, focused)
	default:
		return v.renderSetup(w, h, pal, focused)
	}
}

func renderEmuInputBox(buf string, w int, pal theme.Palette, focused bool) string {
	txt := lipgloss.NewStyle().Bold(true).Foreground(pal.Text)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	if focused {
		txt = lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	}
	fieldW := max(6, w-4)
	content := tailWindow(buf, fieldW-1) + "_"
	return mut.Render("> ") + txt.Render(FitLine(content, fieldW))
}

func (v BluetoothEmuView) renderTextPrompt(w, h int, pal theme.Palette, title, subtitle, buf string, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	lines := []string{
		hdr.Render(v.Marquee.FitLine("BT Emu · "+title, w)),
		sec.Render(v.Marquee.FitLine(subtitle, w)),
		renderEmuInputBox(buf, w, pal, focused),
		mut.Render(v.Marquee.FitLine("Enter save · Esc cancel", w)),
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v BluetoothEmuView) renderPlayWAV(w, h int, pal theme.Palette) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)

	lines := []string{
		hdr.Render(v.Marquee.FitLine("Speaker Capture Playback", w)),
		mut.Render(v.Marquee.FitLine("File: "+filepath.Base(v.LastRecordPath), w)),
	}
	if v.PlayErr != "" {
		lines = append(lines, bad.Render(v.Marquee.FitLine(v.PlayErr, w)))
	} else {
		lines = append(lines, ok.Render(v.Marquee.FitLine(fmt.Sprintf("Playing %s", formatDur(v.PlayElapsed)), w)))
	}
	lines = append(lines, mut.Render(v.Marquee.FitLine("Enter / Esc to stop", w)))
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v BluetoothEmuView) renderSetup(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	hci := v.EffectiveHCI()
	headRight := ""
	if a, okA := v.EffectiveAdapter(); okA {
		if a.Bus != "" {
			headRight = a.Bus
		}
	}
	headText := "BT Emu · " + hci
	if headRight != "" {
		headText += " (" + headRight + ")"
	}
	lines := []string{hdr.Render(v.Marquee.FitLine(headText, w))}

	fields := v.setupFields()
	p := v.currentProfile()
	cur := clampIndex(v.SetupCursor, len(fields))
	visH := max(1, h-1)
	if v.ErrMsg != "" && visH > 2 {
		visH--
	}
	start, end := ScrollWindow(len(fields), cur, visH)

	labelW := min(9, max(6, w/4))
	valW := max(8, w-labelW-4)

	for i := start; i < end; i++ {
		f := fields[i]
		isSel := i == cur && focused
		var lbl, val string
		var valStyle = lipgloss.NewStyle().Foreground(pal.Text)

		switch f {
		case emuFieldProfile:
			lbl = "Type"
			val = "< " + p.Label + " >"
			valStyle = lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
		case emuFieldName:
			lbl = "Name"
			name := strings.TrimSpace(v.CustomName)
			if name == "" {
				name = p.DefaultName
			}
			val = name
		case emuFieldAdapter:
			lbl = "Radio"
			if a, okA := v.EffectiveAdapter(); okA {
				st := "DN"
				if a.Up {
					st = "UP"
				}
				val = fmt.Sprintf("%s %s %s", a.Name, a.Bus, st)
			} else {
				val = "none"
				valStyle = bad
			}
		case emuFieldCustomMode:
			lbl = "Stack"
			val = "< " + strings.ToUpper(string(v.currentCustomMode())) + " >"
		case emuFieldClass:
			lbl = "Class"
			if v.CustomCoDHex != "" {
				val = v.CustomCoDHex
			} else {
				val = bluetooth.EmuCoDPresets[clampIndex(v.CoDPresetIdx, len(bluetooth.EmuCoDPresets))].Label
			}
		case emuFieldCustomUUID:
			lbl = "UUID"
			if strings.TrimSpace(v.CustomUUID) == "" {
				val = "(auto)"
				valStyle = mut
			} else {
				val = v.CustomUUID
			}
		case emuFieldMonitor:
			lbl = "Play"
			if v.MonitorAudio {
				val = "[ON] local spk"
				valStyle = ok
			} else {
				val = "[OFF]"
				valStyle = mut
			}
		case emuFieldRecord:
			lbl = "Record"
			if v.RecordAudio {
				val = "[ON] save WAV"
				valStyle = warn
			} else {
				val = "[OFF]"
				valStyle = mut
			}
		case emuFieldStart:
			lbl = "Action"
			val = "Start Emulation"
			valStyle = ok
		case emuFieldPlayLast:
			lbl = "Audio"
			val = "Play " + filepath.Base(v.LastRecordPath)
			valStyle = lipgloss.NewStyle().Foreground(pal.Accent)
		}

		prefix := "  "
		if i == cur {
			prefix = "> "
		}
		rowText := fmt.Sprintf("%s%-*s %s", prefix, labelW, Truncate(lbl, labelW), Truncate(val, valW))
		if isSel {
			lines = append(lines, sel.Render(v.Marquee.FitLine(rowText, w)))
		} else {
			lines = append(lines, sec.Render(fmt.Sprintf("%s%-*s ", prefix, labelW, Truncate(lbl, labelW)))+valStyle.Render(Truncate(val, max(1, w-labelW-3))))
		}
	}

	if v.ErrMsg != "" && len(lines) < h {
		lines = append(lines, bad.Render(v.Marquee.FitLine("Err: "+v.ErrMsg, w)))
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v BluetoothEmuView) renderActive(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	dot := warn.Render("◌")
	if v.Connected {
		dot = ok.Render("●")
	}
	title := fmt.Sprintf(" %s · %s", v.Spec.Label, v.BuildConfig().Name)
	head := dot + hdr.Render(Truncate(title, max(1, w-1)))

	sub := ""
	if v.Stopping {
		sub = warn.Render(v.Marquee.FitLine("Stopping emulator...", w))
	} else if v.ErrMsg != "" {
		sub = lipgloss.NewStyle().Bold(true).Foreground(pal.Danger).Render(v.Marquee.FitLine(v.ErrMsg, w))
	} else if v.Spec.IsAudio {
		peer := "Waiting audio host..."
		if v.Connected {
			peer = v.PeerName
			if peer == "" {
				peer = v.PeerAddr
			}
		}
		barW := max(4, min(10, w/4))
		bar := MiniBar(float64(v.AudioLevel)/100.0, barW, false)
		info := ""
		if v.SampleRate > 0 {
			info = fmt.Sprintf(" %dk", v.SampleRate/1000)
		}
		if v.RecordAudio {
			info += fmt.Sprintf(" REC %s", formatClock(v.RecordSec))
		}
		sub = txt.Render(v.Marquee.FitLine(fmt.Sprintf("%s %s%s", bar, Truncate(peer, max(6, w-barW-len(info)-2)), info), w))
	} else if v.Connected {
		peer := v.PeerName
		if peer == "" {
			peer = v.PeerAddr
		}
		state := "connected"
		if v.PeerPaired {
			state = "paired"
		}
		sub = ok.Render(v.Marquee.FitLine(fmt.Sprintf("%s: %s · TX %d", state, peer, v.TXCount), w))
	} else if v.Ready {
		sub = warn.Render(v.Marquee.FitLine(fmt.Sprintf("Advertising (%s) · pair from host", v.EffectiveHCI()), w))
	} else {
		sub = mut.Render(v.Marquee.FitLine("Initializing BlueZ D-Bus...", w))
	}

	lines := []string{head, sub}
	bodySlots := max(1, h-2)

	switch v.SubMode {
	case btEmuCtrlLiveKB:
		ledStr := ""
		if v.LEDs&0x02 != 0 {
			ledStr += " [CAPS]"
		}
		if v.LEDs&0x01 != 0 {
			ledStr += " [NUM]"
		}
		lines = append(lines, ok.Render(v.Marquee.FitLine("LIVE TYPING ON"+ledStr, w)))
		lines = append(lines, txt.Render(v.Marquee.FitLine("Type any key -> sent to host", w)))
		last := v.LastSent
		if last == "" {
			last = "(waiting for keypress)"
		}
		lines = append(lines, hdr.Render(v.Marquee.FitLine("Last: "+last, w)))
		lines = append(lines, mut.Render(v.Marquee.FitLine("Esc exits live typing", w)))

	case btEmuCtrlSendText:
		lines = append(lines, hdr.Render(v.Marquee.FitLine("Send Text Burst", w)))
		lines = append(lines, renderEmuInputBox(v.InputBuf, w, pal, focused))
		lines = append(lines, mut.Render(v.Marquee.FitLine("Enter types text · Esc back", w)))

	case btEmuCtrlMacros:
		cur := clampIndex(v.MacroCursor, len(btEmuMacros))
		start, end := ScrollWindow(len(btEmuMacros), cur, bodySlots)
		for i := start; i < end; i++ {
			m := btEmuMacros[i]
			prefix := "  "
			if i == cur {
				prefix = "> "
			}
			row := prefix + m.Label
			if i == cur && focused {
				lines = append(lines, sel.Render(v.Marquee.FitLine(row, w)))
			} else {
				lines = append(lines, txt.Render(v.Marquee.FitLine(row, w)))
			}
		}

	case btEmuCtrlMouse:
		drag := "OFF"
		if v.MouseDrag {
			drag = "HOLD"
		}
		lines = append(lines, hdr.Render(v.Marquee.FitLine(fmt.Sprintf("Mouse Pad · Spd:%s Drag:%s", v.MouseStepLabel(), drag), w)))
		lines = append(lines, txt.Render(v.Marquee.FitLine("Arrows/WASD move · Enter/L LClick", w)))
		lines = append(lines, txt.Render(v.Marquee.FitLine("R RClick · M Mid · U/J Wheel", w)))
		lines = append(lines, mut.Render(v.Marquee.FitLine("+/- Spd · G Drag · Esc Menu", w)))
		if v.LastSent != "" {
			lines = append(lines, ok.Render(v.Marquee.FitLine("Last: "+v.LastSent, w)))
		}

	case btEmuCtrlGamepad:
		lines = append(lines, hdr.Render(v.Marquee.FitLine("Gamepad Controller", w)))
		lines = append(lines, txt.Render(v.Marquee.FitLine("Arrows: D-Pad / Stick", w)))
		lines = append(lines, txt.Render(v.Marquee.FitLine("Z:A  X:B  C:X  V:Y  Q:L1  E:R1", w)))
		lines = append(lines, mut.Render(v.Marquee.FitLine("Enter:Start Space:Sel Esc:Menu", w)))
		if v.LastSent != "" {
			lines = append(lines, ok.Render(v.Marquee.FitLine("Last: "+v.LastSent, w)))
		}

	case btEmuCtrlMedia:
		lines = append(lines, hdr.Render(v.Marquee.FitLine("Media Remote", w)))
		lines = append(lines, txt.Render(v.Marquee.FitLine("Space/Enter: Play/Pause", w)))
		lines = append(lines, txt.Render(v.Marquee.FitLine("Left/Right: Prev/Next Track", w)))
		lines = append(lines, txt.Render(v.Marquee.FitLine("Up/Down: Vol+/- · M: Mute", w)))
		if v.LastSent != "" {
			lines = append(lines, ok.Render(v.Marquee.FitLine("Last: "+v.LastSent, w)))
		}

	case btEmuCtrlLog:
		if len(v.Logs) == 0 {
			lines = append(lines, mut.Render(v.Marquee.FitLine("No events yet.", w)))
		} else {
			maxScr := max(0, len(v.Logs)-bodySlots)
			scr := clampIndex(v.LogScroll, maxScr+1)
			end := min(len(v.Logs), scr+bodySlots)
			for i := scr; i < end; i++ {
				lines = append(lines, mut.Render(v.Marquee.FitLine(v.Logs[i], w)))
			}
		}

	default: // btEmuCtrlMenu
		items := v.menuItems()
		cur := clampIndex(v.MenuCursor, len(items))
		start, end := ScrollWindow(len(items), cur, bodySlots)
		for i := start; i < end; i++ {
			act := items[i]
			label := ""
			style := txt
			switch act {
			case emuActLiveKB:
				label = "Live Keyboard (passthrough)"
				style = ok
			case emuActSendText:
				label = "Send Text String"
			case emuActMacros:
				label = "Quick Keys & Combos"
			case emuActMouse:
				label = "Mouse Pad"
			case emuActGamepad:
				label = "Gamepad Controls"
			case emuActMedia:
				label = "Media Controls"
			case emuActMonitor:
				if v.MonitorAudio {
					label = "Monitor Speaker: ON"
					style = ok
				} else {
					label = "Monitor Speaker: OFF"
					style = mut
				}
			case emuActRecord:
				if v.RecordAudio {
					label = fmt.Sprintf("Record Audio: REC (%s)", formatClock(v.RecordSec))
					style = warn
				} else {
					label = "Record Audio: OFF"
				}
			case emuActAVRCPPlay:
				if v.AVRCPPaused {
					label = "AVRCP: Play"
				} else {
					label = "AVRCP: Play / Pause"
				}
			case emuActAVRCPNext:
				label = "AVRCP: Next Track"
			case emuActAVRCPPrev:
				label = "AVRCP: Prev Track"
			case emuActLog:
				label = fmt.Sprintf("Event Log (%d)", len(v.Logs))
			case emuActStop:
				label = "Stop Emulation"
				style = lipgloss.NewStyle().Foreground(pal.Danger)
			}
			prefix := "  "
			if i == cur {
				prefix = "> "
			}
			row := prefix + label
			if i == cur && focused {
				lines = append(lines, sel.Render(v.Marquee.FitLine(row, w)))
			} else {
				lines = append(lines, style.Render(v.Marquee.FitLine(row, w)))
			}
		}
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func formatClock(sec int) string {
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("%02d:%02d", sec/60, sec%60)
}

func formatDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return formatClock(int(d.Seconds()))
}

// runBluetoothEmuCmd executes the emulator loop in a Bubble Tea cmd.
func runBluetoothEmuCmd(ctx context.Context, seq int, cfg bluetooth.EmuConfig, cmdCh <-chan bluetooth.EmuCommand, evCh chan<- bluetooth.EmuEvent) tea.Cmd {
	return func() tea.Msg {
		err := bluetooth.RunEmulator(ctx, cfg, cmdCh, func(ev bluetooth.EmuEvent) {
			select {
			case evCh <- ev:
			default:
			}
		})
		close(evCh)
		return btEmuDoneMsg{seq: seq, err: err}
	}
}

func waitBluetoothEmuEventCmd(seq int, ch <-chan bluetooth.EmuEvent) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return btEmuEventMsg{seq: seq, ev: ev}
	}
}
