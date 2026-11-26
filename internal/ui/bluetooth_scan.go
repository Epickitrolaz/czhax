package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/files"
	"github.com/czhax/czhax/internal/oui"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/workdir"
)

type BTViewMode int

const (
	btModeList BTViewMode = iota
	btModeActions
	btModeDetail
	btModeRunning
	btModeCWMenu
	btModeCWAudio
	btModeCWRecord
	btModeCWTarget
)

type BTActionKind int

const (
	btActCarwhisperer BTActionKind = iota
	btActWhisperPair
	btActL2Flood
	btActDetail
)

type btActionItem struct {
	kind BTActionKind
	name string
	val  string
	warn bool
	ok   bool
}

type cwActionKind int

const (
	cwTap cwActionKind = iota
	cwPlayAudio
	cwRecordMessage
	cwTarget
	cwStart
	cwBack
)

type cwActionItem struct {
	kind cwActionKind
	name string
	val  string
	warn bool
	ok   bool
}

var cwActions = []cwActionItem{
	{kind: cwTap, name: "Tap (record mic)", val: "toggle"},
	{kind: cwPlayAudio, name: "Play audio", val: "select"},
	{kind: cwRecordMessage, name: "Record message", val: "toggle", warn: true},
	{kind: cwTarget, name: "Target", val: "edit"},
	{kind: cwStart, name: "Start", val: "run", ok: true},
	{kind: cwBack, name: "Back", val: "esc"},
}

// BTActionOptions controls one Carwhisperer run.
type BTActionOptions struct {
	Record   bool
	PlayPath string
}

var btActions = []btActionItem{
	{kind: btActCarwhisperer, name: "Carwhisperer", val: "audio", warn: true},
	{kind: btActWhisperPair, name: "WhisperPair", val: "fast pair"},
	{kind: btActL2Flood, name: "l2ping Flood", val: "flood", warn: true},
	{kind: btActDetail, name: "Device Details", val: "info", ok: true},
}

// BluetoothScanView is the Bluetooth equivalent of WifiScanView: it lists
// nearby peers, exposes a per-device action menu, and streams running tool
// output.
type BluetoothScanView struct {
	Mode         BTViewMode
	PrevMode     BTViewMode
	DetailReturn BTViewMode

	WorkDir string

	Adapters      []bluetooth.Adapter
	AdapterIdx    int
	AdapterManual bool
	ScanMode      bluetooth.ScanMode

	Scanning   bool
	HasScanned bool
	ScanErr    string
	Devices    []bluetooth.Device
	Cursor     int

	ActionCursor   int
	DetailScroll   int
	PendingInfoFor string

	CWReturn       BTViewMode
	CWCursor       int
	CWTap          bool
	CWPlay         bool
	CWRecordMsg    bool
	CWPlayPath     string
	CWPlayName     string
	CWStartTap     bool
	CWTarget       string
	CWTargetInput  string
	CWAudio        []files.Entry
	CWAudioCur     int
	CWRecord       bool
	CWRecordSeq    int
	CWRecordAt     time.Time
	CWRecordFor    time.Duration
	CWRecordPath   string
	CWRecordCancel context.CancelFunc
	CWRecordErr    string

	RunKind       BTActionKind
	RunDevice     bluetooth.Device
	RunActive     bool
	RunStopping   bool
	RunSeq        int
	RunStarted    time.Time
	RunElapsed    time.Duration
	RunLines      []string
	RunErr        string
	RunRecordPath string
	RunPlayPath   string
	RunProgressCh chan string
	RunCancel     context.CancelFunc

	Note      string
	NoteIsErr bool
	Marquee   Marquee
}

func NewBluetoothScanView() BluetoothScanView {
	return BluetoothScanView{ScanMode: bluetooth.ScanBoth}
}

func (v BluetoothScanView) IsTextInput() bool { return false }

func (v BluetoothScanView) EffectiveHCI() string {
	if len(v.Adapters) == 0 {
		return "hci0"
	}
	idx := clampIndex(v.AdapterIdx, len(v.Adapters))
	return v.Adapters[idx].Name
}

func (v BluetoothScanView) EffectiveAdapter() (bluetooth.Adapter, bool) {
	if len(v.Adapters) == 0 {
		return bluetooth.Adapter{}, false
	}
	return v.Adapters[clampIndex(v.AdapterIdx, len(v.Adapters))], true
}

func (v BluetoothScanView) EffectiveController() string {
	if a, ok := v.EffectiveAdapter(); ok {
		return strings.TrimSpace(a.Address)
	}
	return ""
}

func (v *BluetoothScanView) SyncAdapters(adapters []bluetooth.Adapter) {
	prevHCI := v.EffectiveHCI()
	prev := ""
	if a, ok := v.EffectiveAdapter(); ok {
		prev = a.Name
	}
	v.Adapters = adapters
	if len(adapters) == 0 {
		v.AdapterIdx = 0
		return
	}
	found := -1
	if v.AdapterManual {
		for i, a := range adapters {
			if a.Name == prev {
				found = i
				break
			}
		}
	}
	if found < 0 {
		// Prefer a USB Bluetooth card for SCO/Carwhisperer; the onboard UART
		// controller on the Cardputer often cannot establish eSCO.
		for i, a := range adapters {
			if strings.EqualFold(a.Bus, "USB") && a.Up {
				found = i
				break
			}
		}
	}
	if found < 0 {
		for i, a := range adapters {
			if a.Name == "hci0" {
				found = i
				break
			}
		}
	}
	if found < 0 {
		found = 0
	}
	v.AdapterIdx = found
	if v.Scanning && v.EffectiveHCI() != prevHCI {
		// The first scan may have started before adapters were enumerated.
		v.Scanning = false
		v.Devices = nil
		v.HasScanned = false
	}
}

func (v *BluetoothScanView) CycleAdapter(delta int) {
	if len(v.Adapters) <= 1 {
		return
	}
	v.AdapterManual = true
	v.AdapterIdx = (clampIndex(v.AdapterIdx, len(v.Adapters)) + delta + len(v.Adapters)) % len(v.Adapters)
	v.Devices = nil
	v.Cursor = 0
	v.HasScanned = false
	v.ScanErr = ""
	v.Note = ""
	v.NoteIsErr = false
	if a, ok := v.EffectiveAdapter(); ok && strings.TrimSpace(a.Name) != "" {
		v.Note = "Adapter " + a.Name
	}
}

func (v *BluetoothScanView) CycleScanMode(delta int) {
	modes := []bluetooth.ScanMode{bluetooth.ScanBoth, bluetooth.ScanLE, bluetooth.ScanClassic}
	cur := 0
	for i, m := range modes {
		if m == v.ScanMode {
			cur = i
			break
		}
	}
	v.ScanMode = modes[(cur+delta+len(modes))%len(modes)]
	v.ScanErr = ""
	v.Note = "Scan mode: " + v.ScanMode.String()
	v.NoteIsErr = false
}

func (v *BluetoothScanView) StartScan() {
	v.Scanning = true
	v.ScanErr = ""
	v.Note = ""
	v.NoteIsErr = false
}

func (v *BluetoothScanView) SetScanResult(devices []bluetooth.Device, err error) {
	v.Scanning = false
	v.HasScanned = true
	if err != nil {
		v.ScanErr = err.Error()
		v.Note = err.Error()
		v.NoteIsErr = true
		return
	}
	v.ScanErr = ""
	devices = bluetooth.SortDevices(append([]bluetooth.Device(nil), devices...))
	prevAddr := ""
	if v.Cursor >= 0 && v.Cursor < len(v.Devices) {
		prevAddr = v.Devices[v.Cursor].Address
	}
	v.Devices = devices
	if len(devices) == 0 {
		v.Cursor = 0
		v.Note = "No devices on " + v.EffectiveHCI()
		v.NoteIsErr = false
		return
	}
	found := 0
	if prevAddr != "" {
		for i, d := range devices {
			if d.Address == prevAddr {
				found = i
				break
			}
		}
	}
	v.Cursor = found
	v.Note = fmt.Sprintf("%d devices on %s", len(devices), v.EffectiveHCI())
	v.NoteIsErr = false
}

func (v *BluetoothScanView) MoveDevice(delta int) {
	if len(v.Devices) == 0 {
		v.Cursor = 0
		return
	}
	v.Cursor = (v.Cursor + delta + len(v.Devices)) % len(v.Devices)
	v.Note = ""
	v.NoteIsErr = false
}

func (v BluetoothScanView) SelectedDevice() (bluetooth.Device, bool) {
	if v.Cursor < 0 || v.Cursor >= len(v.Devices) {
		return bluetooth.Device{}, false
	}
	return v.Devices[v.Cursor], true
}

func (v *BluetoothScanView) OpenActions() bool {
	if _, ok := v.SelectedDevice(); !ok {
		return false
	}
	v.Mode = btModeActions
	v.ActionCursor = 0
	v.Note = ""
	v.NoteIsErr = false
	return true
}

func (v *BluetoothScanView) MoveAction(delta int) {
	v.ActionCursor = (v.ActionCursor + delta + len(btActions)) % len(btActions)
	v.Note = ""
	v.NoteIsErr = false
}

func (v BluetoothScanView) SelectedAction() (BTActionKind, bool) {
	if v.ActionCursor < 0 || v.ActionCursor >= len(btActions) {
		return 0, false
	}
	return btActions[v.ActionCursor].kind, true
}

func (v *BluetoothScanView) OpenCWMenu() bool {
	dev, ok := v.SelectedDevice()
	if !ok {
		return false
	}
	v.CWReturn = v.Mode
	v.Mode = btModeCWMenu
	v.CWCursor = 0
	v.CWTap = false
	v.CWPlay = false
	v.CWRecordMsg = false
	v.CWPlayPath = ""
	v.CWPlayName = ""
	v.CWTarget = dev.Address
	v.CWTargetInput = dev.Address
	if dev.Kind != "Classic" {
		for _, other := range v.Devices {
			if other.Kind != "Classic" {
				continue
			}
			if sameSuffix(dev.Address, other.Address) || (dev.DisplayName() != "" && strings.EqualFold(dev.DisplayName(), other.DisplayName())) {
				v.CWTarget = other.Address
				v.CWTargetInput = other.Address
				break
			}
		}
	}
	v.Note = ""
	v.NoteIsErr = false
	if v.CWTarget != dev.Address {
		v.Note = "Using Classic address " + v.CWTarget
	} else if dev.Kind == "BLE" {
		v.Note = "BLE address: set Target to the public Classic address if RFCOMM fails"
	}
	return true
}

func (v *BluetoothScanView) MoveCW(delta int) {
	v.CWCursor = (v.CWCursor + delta + len(cwActions)) % len(cwActions)
	v.Note = ""
	v.NoteIsErr = false
}

func (v BluetoothScanView) SelectedCW() (cwActionKind, bool) {
	if v.CWCursor < 0 || v.CWCursor >= len(cwActions) {
		return 0, false
	}
	return cwActions[v.CWCursor].kind, true
}

func (v *BluetoothScanView) StartCWTargetEdit() {
	v.CWTargetInput = v.CWTarget
	v.Mode = btModeCWTarget
	v.Note = ""
	v.NoteIsErr = false
}

func (v *BluetoothScanView) HandleCWTargetKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		v.CWTargetInput = dropLastRune(v.CWTargetInput)
	case tea.KeyCtrlU:
		v.CWTargetInput = ""
	case tea.KeySpace:
		v.CWTargetInput += " "
	case tea.KeyRunes:
		v.CWTargetInput += string(msg.Runes)
	}
}

func (v *BluetoothScanView) ConfirmCWTarget() bool {
	addr := strings.ToUpper(strings.TrimSpace(v.CWTargetInput))
	addr = strings.ReplaceAll(addr, "-", ":")
	if !bluetooth.IsBDAddr(addr) {
		v.Note = "Enter a valid AA:BB:CC:DD:EE:FF address"
		v.NoteIsErr = true
		return false
	}
	// If the user pasted the BLE random address (no IEEE OUI), prefer a
	// visible Classic peer with the same last-5 octets.
	if oui.Lookup(addr) == "" {
		for _, other := range v.Devices {
			if other.Kind == "Classic" && sameSuffix(addr, other.Address) {
				addr = other.Address
				break
			}
		}
	}
	v.CWTarget = addr
	v.Mode = btModeCWMenu
	v.Note = "Target: " + addr
	if oui.Lookup(addr) == "" {
		v.Note += " (not a public OUI)"
		v.NoteIsErr = true
	}
	return true
}

func (v *BluetoothScanView) OpenCWAudio() bool {
	root := v.WorkDir
	if strings.TrimSpace(root) == "" {
		root = workdir.Resolve("")
	}
	entries, err := files.ListAudio(root)
	if err != nil {
		v.Note = err.Error()
		v.NoteIsErr = true
		return false
	}
	v.CWAudio = entries
	v.CWAudioCur = 0
	v.Mode = btModeCWAudio
	v.Note = ""
	v.NoteIsErr = false
	return true
}

func (v *BluetoothScanView) MoveCWAudio(delta int) {
	if len(v.CWAudio) == 0 {
		v.CWAudioCur = 0
		return
	}
	v.CWAudioCur = (v.CWAudioCur + delta + len(v.CWAudio)) % len(v.CWAudio)
}

func (v BluetoothScanView) SelectedCWAudio() (files.Entry, bool) {
	if v.CWAudioCur < 0 || v.CWAudioCur >= len(v.CWAudio) {
		return files.Entry{}, false
	}
	return v.CWAudio[v.CWAudioCur], true
}

func (v *BluetoothScanView) SetCWAudio(e files.Entry) {
	v.CWPlay = true
	v.CWPlayPath = e.Path
	v.CWPlayName = e.Rel
	v.Mode = btModeCWMenu
	v.Note = "Audio: " + e.Rel
	v.NoteIsErr = false
}

func (v *BluetoothScanView) ToggleCW(kind cwActionKind) {
	switch kind {
	case cwTap:
		v.CWTap = !v.CWTap
	case cwPlayAudio:
		v.CWPlay = !v.CWPlay
		if v.CWPlay {
			v.CWRecordMsg = false
		} else {
			v.CWPlayPath = ""
			v.CWPlayName = ""
		}
	case cwRecordMessage:
		v.CWRecordMsg = !v.CWRecordMsg
		if v.CWRecordMsg {
			v.CWPlay = false
			v.CWPlayPath = ""
			v.CWPlayName = ""
		}
	}
}

func (v BluetoothScanView) CWOptions() BTActionOptions {
	return BTActionOptions{
		Record:   v.CWTap,
		PlayPath: v.CWPlayPath,
	}
}

func (v *BluetoothScanView) StartCWRecord() (context.Context, int, string, bool) {
	if v.CWRecord {
		return nil, 0, "", false
	}
	root := v.WorkDir
	if strings.TrimSpace(root) == "" {
		root = workdir.Resolve("")
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.CWRecordSeq++
	v.CWStartTap = v.CWTap
	v.CWRecordPath = filepath.Join(workdir.Recordings(root), fmt.Sprintf("message-%s.wav", time.Now().Format("20060102-150405")))
	v.CWRecord = true
	v.CWRecordAt = time.Now()
	v.CWRecordFor = 0
	v.CWRecordErr = ""
	v.CWRecordCancel = cancel
	v.Mode = btModeCWRecord
	return ctx, v.CWRecordSeq, v.CWRecordPath, true
}

func (v *BluetoothScanView) StopCWRecord() {
	if v.CWRecordCancel != nil {
		v.CWRecordCancel()
	}
}

func (v *BluetoothScanView) FinishCWRecord(seq int, err error) {
	if seq != v.CWRecordSeq {
		return
	}
	v.CWRecord = false
	v.CWRecordCancel = nil
	if !v.CWRecordAt.IsZero() {
		v.CWRecordFor = time.Since(v.CWRecordAt)
	}
	if err != nil {
		v.CWRecordErr = err.Error()
		v.Note = "Mic recording failed"
		v.NoteIsErr = true
		v.Mode = btModeCWMenu
		return
	}
	v.Mode = btModeCWMenu
}

func (v *BluetoothScanView) ConvertPath() string {
	root := v.WorkDir
	if strings.TrimSpace(root) == "" {
		root = workdir.Resolve("")
	}
	return filepath.Join(workdir.BT(root), "playback.raw")
}

func (v *BluetoothScanView) OpenDetail(fromMode BTViewMode) bool {
	dev, ok := v.SelectedDevice()
	if !ok {
		return false
	}
	v.DetailReturn = fromMode
	v.Mode = btModeDetail
	v.DetailScroll = 0
	v.PendingInfoFor = dev.Address
	v.Note = ""
	v.NoteIsErr = false
	return true
}

func (v *BluetoothScanView) MoveDetailScroll(delta, w, h int, pal theme.Palette) {
	dev, ok := v.SelectedDevice()
	if !ok {
		v.DetailScroll = 0
		return
	}
	detailH := max(1, h-1)
	if h >= 10 {
		detailH = max(1, h-2)
	}
	total := len(v.buildDetailRows(w, pal, dev))
	maxScroll := max(0, total-detailH)
	v.DetailScroll += delta
	if v.DetailScroll < 0 {
		v.DetailScroll = 0
	}
	if v.DetailScroll > maxScroll {
		v.DetailScroll = maxScroll
	}
}

func (v *BluetoothScanView) MergeDeviceInfo(info bluetooth.Device) {
	if strings.TrimSpace(info.Address) == "" {
		return
	}
	for i := range v.Devices {
		if !strings.EqualFold(v.Devices[i].Address, info.Address) {
			continue
		}
		d := v.Devices[i]
		if info.Name != "" {
			d.Name = info.Name
		}
		if info.Alias != "" {
			d.Alias = info.Alias
		}
		if info.HasRSSI {
			d.RSSI = info.RSSI
			d.HasRSSI = true
		}
		if info.Kind != "" && d.Kind == "" {
			d.Kind = info.Kind
		}
		if info.Class != "" {
			d.Class = info.Class
		}
		if info.Icon != "" {
			d.Icon = info.Icon
		}
		if len(info.UUIDs) > 0 {
			d.UUIDs = info.UUIDs
		}
		d.Paired = d.Paired || info.Paired
		d.Trusted = d.Trusted || info.Trusted
		d.Connected = d.Connected || info.Connected
		d.FastPair = d.FastPair || info.FastPair
		v.Devices[i] = d
		return
	}
}

func (v *BluetoothScanView) StartAttack(kind BTActionKind, opts ...BTActionOptions) (context.Context, bluetooth.AttackRequest, int, chan string, bool) {
	dev, ok := v.SelectedDevice()
	if !ok || v.RunActive {
		return nil, bluetooth.AttackRequest{}, 0, nil, false
	}
	opt := BTActionOptions{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	var backendKind bluetooth.AttackKind
	switch kind {
	case btActCarwhisperer:
		backendKind = bluetooth.AttackCarwhisperer
	case btActWhisperPair:
		backendKind = bluetooth.AttackWhisperPair
	case btActL2Flood:
		backendKind = bluetooth.AttackL2Flood
	default:
		return nil, bluetooth.AttackRequest{}, 0, nil, false
	}

	ctx, cancel := context.WithCancel(context.Background())
	v.PrevMode = v.Mode
	v.Mode = btModeRunning
	v.RunKind = kind
	v.RunDevice = dev
	if kind == btActCarwhisperer && bluetooth.IsBDAddr(v.CWTarget) {
		v.RunDevice.Address = strings.ToUpper(strings.TrimSpace(v.CWTarget))
	}
	v.RunActive = true
	v.RunStopping = false
	v.RunSeq++
	v.RunStarted = time.Now()
	v.RunElapsed = 0
	v.RunLines = nil
	v.RunErr = ""
	v.RunRecordPath = ""
	v.RunPlayPath = ""
	if kind == btActCarwhisperer {
		root := v.WorkDir
		if strings.TrimSpace(root) == "" {
			root = workdir.Resolve("")
		}
		if opt.Record {
			v.RunRecordPath = bluetooth.CarwhispererRecordPathIn(root, dev.Address)
		}
		v.RunPlayPath = opt.PlayPath
	}
	v.RunProgressCh = make(chan string, 64)
	v.RunCancel = cancel
	v.Note = ""
	v.NoteIsErr = false

	req := bluetooth.AttackRequest{
		Kind:       backendKind,
		HCI:        v.EffectiveHCI(),
		Address:    v.RunDevice.Address,
		AddrType:   dev.AddrType,
		Name:       dev.DisplayName(),
		Record:     opt.Record,
		RecordPath: v.RunRecordPath,
		PlayPath:   v.RunPlayPath,
		FastPair:   dev.FastPair,
		ModelID:    dev.ModelID,
	}
	return ctx, req, v.RunSeq, v.RunProgressCh, true
}

func (v *BluetoothScanView) ApplyAttackLine(seq int, line string) {
	if !v.RunActive || seq != v.RunSeq {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	v.RunLines = append(v.RunLines, line)
	if len(v.RunLines) > 500 {
		v.RunLines = append([]string(nil), v.RunLines[len(v.RunLines)-500:]...)
	}
	if !v.RunStarted.IsZero() {
		v.RunElapsed = time.Since(v.RunStarted)
	}
}

func (v *BluetoothScanView) DrainRunLines(seq int) {
	ch := v.RunProgressCh
	if ch == nil {
		return
	}
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return
			}
			v.ApplyAttackLine(seq, line)
		default:
			return
		}
	}
}

func (v *BluetoothScanView) FinishAttack(seq int, err error) {
	if seq != v.RunSeq {
		return
	}
	v.RunActive = false
	v.RunStopping = false
	v.RunProgressCh = nil
	if v.RunCancel != nil {
		v.RunCancel()
		v.RunCancel = nil
	}
	if !v.RunStarted.IsZero() {
		v.RunElapsed = time.Since(v.RunStarted)
	}
	switch {
	case err != nil && errors.Is(err, context.Canceled):
		v.RunErr = ""
		v.Note = "Stopped"
		v.NoteIsErr = false
	case err != nil:
		v.RunErr = err.Error()
		v.Note = "Attack failed"
		v.NoteIsErr = true
	default:
		v.RunErr = ""
		v.Note = v.attackLabel() + " done"
		v.NoteIsErr = false
	}
}

func (v *BluetoothScanView) StopAttack() {
	if v.RunCancel != nil {
		v.RunCancel()
	}
	v.RunStopping = true
}

func (v BluetoothScanView) attackLabel() string {
	if v.RunKind < 0 || int(v.RunKind) >= len(btActions) {
		return "Attack"
	}
	return btActions[v.RunKind].name
}

func (v *BluetoothScanView) HandleEsc() bool {
	switch v.Mode {
	case btModeCWTarget:
		v.Mode = btModeCWMenu
		return true
	case btModeCWRecord:
		if v.CWRecord {
			v.StopCWRecord()
			return true
		}
		v.Mode = btModeCWMenu
		return true
	case btModeCWAudio:
		v.Mode = btModeCWMenu
		return true
	case btModeCWMenu:
		v.Mode = v.CWReturn
		return true
	case btModeRunning:
		if v.RunActive {
			v.StopAttack()
			return true
		}
		v.RunErr = ""
		v.Mode = v.PrevMode
		return true
	case btModeDetail:
		if v.DetailReturn != btModeList {
			v.Mode = v.DetailReturn
		} else {
			v.Mode = btModeList
		}
		return true
	case btModeActions:
		v.Note = ""
		v.NoteIsErr = false
		v.Mode = btModeList
		return true
	default:
		return false
	}
}

func (v BluetoothScanView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	switch v.Mode {
	case btModeActions:
		return v.renderActions(w, h, pal, focused)
	case btModeDetail:
		return v.renderDetail(w, h, pal, focused)
	case btModeRunning:
		return v.renderRunning(w, h, pal)
	case btModeCWMenu:
		return v.renderCWMenu(w, h, pal, focused)
	case btModeCWAudio:
		return v.renderCWAudio(w, h, pal, focused)
	case btModeCWRecord:
		return v.renderCWRecord(w, h, pal)
	case btModeCWTarget:
		return v.renderCWTarget(w, h, pal)
	default:
		return v.renderList(w, h, pal, focused)
	}
}

func (v BluetoothScanView) renderList(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	hci := v.EffectiveHCI()
	ifacePart := sec.Render("If:") + accent.Render(v.Marquee.Truncate(hci, max(5, w-22)))
	if len(v.Adapters) > 1 {
		ifacePart += muted.Render("◂▸")
	}
	ifacePart += muted.Render("·") + ok.Render(v.ScanMode.Short())

	var rightPart string
	switch {
	case v.Scanning:
		rightPart = warn.Render("scanning…")
	case v.ScanErr != "":
		rightPart = danger.Render("err ") + sec.Render("[s]") + muted.Render("Scan")
	case v.HasScanned:
		rightPart = muted.Render(fmt.Sprintf("%d devs ", len(v.Devices))) + sec.Render("[s]") + muted.Render("Scan")
	default:
		rightPart = sec.Render("[s]") + muted.Render("Scan")
	}

	rightW := lipgloss.Width(rightPart)
	leftMax := max(6, w-rightW-3)
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
	case v.Scanning && len(v.Devices) == 0:
		lines = append(lines, " "+warn.Render(v.Marquee.Truncate("Scanning "+hci+" ("+v.ScanMode.String()+")...", max(6, w-2))))

	case v.ScanErr != "" && len(v.Devices) == 0:
		lines = append(lines, " "+danger.Render("Scan failed:"))
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate(v.ScanErr, max(6, w-2))))
		lines = append(lines, " "+muted.Render("Press s to retry"))

	case len(v.Devices) == 0:
		if !v.HasScanned {
			lines = append(lines, " "+muted.Render("No scan yet"))
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("Press s or ⏎ to scan "+hci, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("No devices on "+hci, max(6, w-2))))
			lines = append(lines, " "+muted.Render("Press s to rescan"))
		}

	default:
		start, end := ScrollWindow(len(v.Devices), v.Cursor, listH)
		for i := start; i < end; i++ {
			d := v.Devices[i]
			selected := i == v.Cursor

			kind := d.ShortKind()
			signal := d.SignalLabel()
			mark := " "
			if d.Connected {
				mark = "*"
			}
			rightPlain := fmt.Sprintf("%4s %s%s", signal, kind, mark)
			rightW := lipgloss.Width(rightPlain)
			nameW := max(4, w-2-1-rightW-1)
			nameTxt := v.Marquee.FitLine(d.DisplayName(), nameW)
			if d.FastPair {
				kind = "FP"
				rightPlain = fmt.Sprintf("%4s FP%s", signal, mark)
				rightW = lipgloss.Width(rightPlain)
				nameW = max(4, w-2-1-rightW-1)
				nameTxt = v.Marquee.FitLine(d.DisplayName(), nameW)
			}

			if selected && focused {
				row := fmt.Sprintf("▸%s%s %s", mark, nameTxt, rightPlain)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := " "
				if selected {
					pfx = accent.Render("▸")
				}
				sigStyle := danger
				if d.HasRSSI {
					if d.RSSI >= -60 {
						sigStyle = ok
					} else if d.RSSI >= -80 {
						sigStyle = warn
					}
				} else {
					sigStyle = muted
				}
				kindStyle := muted
				switch {
				case d.FastPair:
					kindStyle = warn
				case d.Kind == "BLE":
					kindStyle = ok
				}
				markStyle := muted
				if d.Connected {
					markStyle = ok
				}
				row := pfx + markStyle.Render(mark) + nameTxt + " " + sigStyle.Render(fmt.Sprintf("%4s", signal)) + " " + kindStyle.Render(kind) + " "
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
		} else if v.ScanErr != "" {
			lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.ScanErr, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("⏎:act s:scan m:mode Esc:tabs", max(6, w-2))))
		}
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v BluetoothScanView) renderActions(w, h int, pal theme.Palette, focused bool) []string {
	dev, okDev := v.SelectedDevice()
	if !okDev {
		return v.renderList(w, h, pal, focused)
	}

	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	rightPlain := sec.Render("Esc")
	titleW := max(6, w-lipgloss.Width(rightPlain)-4)
	header := " " + FitLine(muted.Render("◂ ")+accent.Render(v.Marquee.Truncate(dev.DisplayName(), max(4, titleW-2))), titleW) + " " + rightPlain + " "
	if h <= 1 {
		return v.Marquee.SplitLinesPadded([]string{header}, w, h)
	}

	lines := []string{header}
	showSummary := h >= 8
	if showSummary {
		sig := "n/a"
		if dev.HasRSSI {
			sig = fmt.Sprintf("%d dBm", dev.RSSI)
		}
		sub := joinNonEmpty(" · ", dev.Address, sig, dev.Kind)
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate(sub, max(6, w-2))))
	}

	items := btActions
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
				valStyle = ok
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
			st := ok
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

func (v BluetoothScanView) buildDetailRows(w int, pal theme.Palette, d bluetooth.Device) []string {
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	valW := max(6, w-9)
	val := func(s string) string {
		if strings.TrimSpace(s) == "" {
			return muted.Render("n/a")
		}
		return txt.Render(v.Marquee.Truncate(s, valW))
	}
	kind := d.Kind
	if kind == "" {
		kind = "BT"
	}

	rows := []string{
		" " + sec.Render("Name   ") + val(d.DisplayName()),
		" " + sec.Render("Address") + val(d.Address),
	}
	sig := "n/a"
	if d.HasRSSI {
		sig = fmt.Sprintf("%d dBm", d.RSSI)
	}
	rows = append(rows,
		" "+sec.Render("Signal ")+val(sig),
		" "+sec.Render("Kind   ")+val(kind),
	)
	if d.Class != "" {
		rows = append(rows, " "+sec.Render("Class  ")+val(d.Class))
	}
	if d.Icon != "" {
		rows = append(rows, " "+sec.Render("Icon   ")+val(d.Icon))
	}
	fp := "no"
	if d.FastPair {
		fp = "yes"
		if d.HasModelID {
			fp = "yes · " + d.ModelIDString()
		}
	}
	fpStyle := muted
	if d.FastPair {
		fpStyle = warn
	}
	rows = append(rows, " "+sec.Render("FastPr ")+fpStyle.Render(v.Marquee.Truncate(fp, valW)))
	if len(d.ServiceData) > 0 {
		rows = append(rows, " "+sec.Render("SvcData")+muted.Render(fmt.Sprintf("%d bytes", len(d.ServiceData))))
	}
	var flags []string
	if d.Paired {
		flags = append(flags, "paired")
	}
	if d.Trusted {
		flags = append(flags, "trusted")
	}
	if d.Connected {
		flags = append(flags, "connected")
	}
	flagStyle := muted
	if len(flags) > 0 {
		flagStyle = ok
	}
	rows = append(rows, " "+sec.Render("Flags  ")+flagStyle.Render(v.Marquee.Truncate(joinNonEmpty(" · ", flags...), valW)))
	rows = append(rows, " "+sec.Render("HCI    ")+val(v.EffectiveHCI()))
	rows = append(rows, " "+sec.Render("Mode   ")+val(v.ScanMode.String()))
	for _, u := range d.UUIDs {
		rows = append(rows, " "+sec.Render("UUID   ")+muted.Render(v.Marquee.Truncate(u, valW)))
	}
	return rows
}

func (v BluetoothScanView) renderCWMenu(w, h int, pal theme.Palette, focused bool) []string {
	dev, okDev := v.SelectedDevice()
	if !okDev {
		return v.renderList(w, h, pal, focused)
	}
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	header := " " + muted.Render("◂ ") + accent.Render(v.Marquee.Truncate("Carwhisperer · "+dev.DisplayName(), max(6, w-4)))
	if h <= 1 {
		return v.Marquee.SplitLinesPadded([]string{header}, w, h)
	}
	lines := []string{header}
	if h >= 8 {
		lines = append(lines, " "+sec.Render("Radio  ")+txt.Render(v.Marquee.Truncate(v.EffectiveHCI()+" · "+v.CWTarget, max(6, w-9))))
	}
	showBottom := (h >= 8 || v.Note != "") && h >= 4
	listH := h - len(lines)
	if showBottom {
		listH--
	}
	if listH < 1 {
		listH = 1
	}
	cursor := clampIndex(v.CWCursor, len(cwActions))
	start, end := ScrollWindow(len(cwActions), cursor, listH)
	for i := start; i < end; i++ {
		it := cwActions[i]
		selected := i == cursor
		check := ""
		switch it.kind {
		case cwTap:
			check = checkbox(v.CWTap)
		case cwPlayAudio:
			check = checkbox(v.CWPlay)
		case cwRecordMessage:
			check = checkbox(v.CWRecordMsg)
		}
		name := check + it.name
		val := it.val
		if it.kind == cwPlayAudio && v.CWPlay && v.CWPlayName != "" {
			val = filepath.Base(v.CWPlayName)
		}
		if it.kind == cwTarget && v.CWTarget != "" {
			val = v.CWTarget
		}
		nameW := max(8, w-len(val)-5)
		if selected && focused {
			lines = append(lines, sel.Width(w).Render(Truncate(fmt.Sprintf(" ▸ %s %s", v.Marquee.FitLine(name, nameW), val), w)))
		} else {
			pfx := "   "
			if selected {
				pfx = accent.Render(" ▸ ")
			}
			valStyle := muted
			if it.ok {
				valStyle = ok
			} else if it.warn {
				valStyle = warn
			}
			lines = append(lines, pfx+txt.Render(v.Marquee.FitLine(name, nameW))+" "+valStyle.Render(val))
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
			lines = append(lines, " "+muted.Render("↑↓:sel  ⏎:run  Esc:back"))
		}
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func sameSuffix(a, b string) bool {
	pa := strings.Split(strings.ToUpper(a), ":")
	pb := strings.Split(strings.ToUpper(b), ":")
	if len(pa) != 6 || len(pb) != 6 {
		return false
	}
	return strings.Join(pa[1:], ":") == strings.Join(pb[1:], ":")
}

func checkbox(on bool) string {
	if on {
		return "[x] "
	}
	return "[ ] "
}

func (v BluetoothScanView) renderCWAudio(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	lines := []string{" " + accent.Render("Play audio")}
	if h <= 1 {
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}
	showBottom := (h >= 8 || v.Note != "") && h >= 4
	listH := h - 1
	if showBottom {
		listH--
	}
	if len(v.CWAudio) == 0 {
		lines = append(lines, " "+muted.Render("No audio files in the workdir"))
		lines = append(lines, " "+muted.Render("Put .wav/.mp3 in "+v.WorkDir))
	} else {
		cursor := clampIndex(v.CWAudioCur, len(v.CWAudio))
		start, end := ScrollWindow(len(v.CWAudio), cursor, max(1, listH))
		for i := start; i < end; i++ {
			e := v.CWAudio[i]
			selected := i == cursor
			right := files.HumanSize(e.Size)
			nameW := max(4, w-2-len(right)-1)
			name := v.Marquee.FitLine(e.Rel, nameW)
			if selected && focused {
				lines = append(lines, sel.Width(w).Render(Truncate(fmt.Sprintf(" ▸ %s %s", name, right), w)))
			} else {
				pfx := "   "
				if selected {
					pfx = accent.Render(" ▸ ")
				}
				lines = append(lines, pfx+txt.Render(name)+" "+muted.Render(right))
			}
		}
	}
	if showBottom {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		if v.Note != "" {
			lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.Note, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render("↑↓:sel  ⏎:play  Esc:back"))
		}
	}
	_ = sec
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v BluetoothScanView) renderCWTarget(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	lines := []string{" " + accent.Render("Carwhisperer target address")}
	lines = append(lines, " "+sec.Render("AA:BB:CC:DD:EE:FF"))
	fieldW := max(6, w-5)
	lines = append(lines, " > "+txt.Render(tailWindow(v.CWTargetInput, fieldW-1)+"_"))
	if v.Note != "" {
		st := muted
		if v.NoteIsErr {
			st = danger
		}
		lines = append(lines, " "+st.Render(v.Marquee.Truncate(v.Note, max(6, w-2))))
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+muted.Render("⏎:save  Esc:cancel"))
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v BluetoothScanView) renderCWRecord(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	lines := []string{" " + accent.Render("Record message")}
	lines = append(lines, " "+sec.Render("File  ")+txt.Render(v.Marquee.Truncate(filepath.Base(v.CWRecordPath), max(6, w-8))))
	lines = append(lines, " "+warn.Render(fmt.Sprintf("● %ds", int(v.CWRecordFor.Seconds()))))
	if v.CWRecordErr != "" {
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate(v.CWRecordErr, max(6, w-2))))
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+muted.Render("Esc/⏎:stop"))
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v BluetoothScanView) renderDetail(w, h int, pal theme.Palette, focused bool) []string {
	dev, ok := v.SelectedDevice()
	if !ok {
		return v.renderList(w, h, pal, focused)
	}

	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	okStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)

	action := muted.Render("[Esc]") + okStyle.Render("Back")
	actionW := lipgloss.Width(action)
	titleW := max(6, w-actionW-3)
	header := " " + FitLine(muted.Render("◂ ")+accent.Render(v.Marquee.Truncate(dev.DisplayName(), max(4, titleW-2))), titleW) + " " + action + " "
	if h <= 1 {
		return v.Marquee.SplitLinesPadded([]string{header}, w, h)
	}

	showBottom := (h >= 10 || v.Note != "") && h >= 4
	detailH := h - 1
	if showBottom {
		detailH = h - 2
	}

	rows := v.buildDetailRows(w, pal, dev)
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
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("↑↓:scroll  Esc:back", max(6, w-2))))
		}
	}

	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v BluetoothScanView) renderRunning(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	label := v.attackLabel()
	lines := []string{
		" " + accent.Render(v.Marquee.Truncate("Attack · "+label, max(6, w-2))),
	}
	target := v.RunDevice.DisplayName() + " · " + v.RunDevice.Address
	lines = append(lines, " "+sec.Render("Target ")+txt.Render(v.Marquee.Truncate(target, max(6, w-9))))

	if h >= 4 {
		state := muted.Render("done")
		phase := ""
		switch {
		case v.RunActive && v.RunStopping:
			state = warn.Render("stopping…")
		case v.RunActive:
			state = warn.Render("running")
			phase = fmt.Sprintf("%ds", int(v.RunElapsed.Seconds()))
		case v.RunErr != "":
			state = danger.Render("failed")
		case v.Note == "Stopped":
			state = muted.Render("stopped")
		default:
			state = ok.Render("done")
		}
		line := " " + state
		if phase != "" {
			line += " " + muted.Render(v.Marquee.Truncate(phase, max(4, w-4-lipgloss.Width(state))))
		}
		lines = append(lines, line)
	}

	if v.RunRecordPath != "" && h >= 5 {
		lines = append(lines, " "+sec.Render("Record ")+muted.Render(v.Marquee.Truncate(v.RunRecordPath, max(6, w-9))))
	}
	if v.RunPlayPath != "" && h >= 6 {
		lines = append(lines, " "+sec.Render("Play   ")+muted.Render(v.Marquee.Truncate(filepath.Base(v.RunPlayPath), max(6, w-9))))
	}
	if v.RunErr != "" {
		lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.RunErr, max(6, w-2))))
	}

	outH := h - len(lines) - 1
	if outH > 0 {
		if len(v.RunLines) == 0 {
			lines = append(lines, " "+muted.Render("Starting…"))
		} else {
			start := max(0, len(v.RunLines)-outH)
			for _, raw := range v.RunLines[start:] {
				lines = append(lines, " "+muted.Render(v.Marquee.Truncate(raw, max(6, w-2))))
			}
		}
	}

	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	hint := "Esc:back"
	if v.RunActive {
		hint = "Esc:stop"
	}
	lines = append(lines, " "+muted.Render(v.Marquee.Truncate(hint, max(6, w-2))))

	return v.Marquee.SplitLinesPadded(lines, w, h)
}
