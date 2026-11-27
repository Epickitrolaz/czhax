package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/sdr"
	"github.com/czhax/czhax/internal/theme"
)

// SDRTab selects the active horizontal subtab inside ViewSDR.
type SDRTab int

const (
	SDRTabRadio SDRTab = iota
	SDRTabSpec
	SDRTabISM
)

type sdrRadioMode int

const (
	sdrRadioSetup sdrRadioMode = iota
	sdrRadioRunning
	sdrRadioPresets
	sdrRadioEditFreq
	sdrRadioSaveName
)

type sdrRadioField int

const (
	sdrFieldFreq sdrRadioField = iota
	sdrFieldStep
	sdrFieldMod
	sdrFieldGain
	sdrFieldSquelch
	sdrFieldPreset
	sdrFieldDevice
	sdrFieldStart
	sdrFieldSave
)

type sdrRunField int

const (
	sdrRunFreq sdrRunField = iota
	sdrRunStep
	sdrRunMute
	sdrRunRecord
	sdrRunStop
)

type sdrSpecKind int

const (
	sdrSpecLive sdrSpecKind = iota
	sdrSpecSweep
)

type sdrISMMode int

const (
	sdrISMSetup sdrISMMode = iota
	sdrISMList
	sdrISMDetail
)

type sdrISMField int

const (
	sdrISMBand sdrISMField = iota
	sdrISMGain
	sdrISMLog
	sdrISMStart
	sdrISMViewPkts
)

// SDRView implements the Radio, Spec (FFT/sweep), and 433 (rtl_433) tabs.
type SDRView struct {
	Tab           SDRTab
	WorkDir       string
	CaptureDir    string
	BookmarksPath string
	ASCII         bool

	Probe     sdr.ProbeStatus
	Probing   bool
	DeviceIdx int
	Note      string
	NoteIsErr bool
	Marquee   Marquee

	// Radio tuner state
	RadioMode    sdrRadioMode
	RadioFreqHz  int64
	RadioStepIdx int
	RadioMod     sdr.DemodMode
	RadioGainIdx int
	RadioSquelch int
	PresetIdx    int
	Presets      []sdr.Preset
	Bookmarks    []sdr.Preset
	RadioCursor  int
	RunCursor    int
	PresetCursor int
	Input        string
	InputErr     string

	RadioActive  bool
	RadioSeq     int
	RadioCancel  context.CancelFunc
	RadioCtrl    *sdr.RadioController
	RadioUpdate  chan sdr.RadioUpdate
	RadioLevel   float64
	RadioDBFS    float64
	RadioMuted   bool
	RadioRecPath string
	RadioStarted time.Time
	RadioElapsed time.Duration
	RadioErr     string

	// Spectrum / band sweep state
	SpecKind      sdrSpecKind
	SpecCenterHz  int64
	SpecBandIdx   int
	SpecActive    bool
	SpecSeq       int
	SpecCancel    context.CancelFunc
	SpecFrameCh   chan sdr.SpectrumFrame
	SpecFrame     sdr.SpectrumFrame
	SpecHasFrame  bool
	SpecMarker    int
	SpecStarted   time.Time
	SpecElapsed   time.Duration
	SpecErr       string
	SpecLastWidth int

	// 433 / ISM decoder state
	ISMMode         sdrISMMode
	ISMFreqIdx      int
	ISMGainIdx      int
	ISMLog          bool
	ISMCursor       int
	ISMPackets      []sdr.ISMPacket
	ISMPktCursor    int
	ISMDetailScroll int
	ISMActive       bool
	ISMSeq          int
	ISMCancel       context.CancelFunc
	ISMPktCh        chan sdr.ISMPacket
	ISMStarted      time.Time
	ISMElapsed      time.Duration
	ISMErr          string
}

func NewSDRView(workDir, captureDir, bookmarksPath string) SDRView {
	if bookmarksPath == "" {
		bookmarksPath = sdr.DefaultBookmarksPath()
	}
	bms, _ := sdr.LoadBookmarks(bookmarksPath)
	v := SDRView{
		WorkDir:       workDir,
		CaptureDir:    captureDir,
		BookmarksPath: bookmarksPath,
		RadioFreqHz:   98_500_000,
		RadioStepIdx:  3, // 100 kHz
		RadioMod:      sdr.ModeWBFM,
		RadioGainIdx:  0, // Auto
		SpecCenterHz:  98_500_000,
		SpecLastWidth: 34,
		Bookmarks:     bms,
		ISMLog:        true,
	}
	v.rebuildPresets()
	return v
}

func (v *SDRView) rebuildPresets() {
	out := make([]sdr.Preset, 0, len(v.Bookmarks)+len(sdr.DefaultPresets))
	out = append(out, v.Bookmarks...)
	out = append(out, sdr.DefaultPresets...)
	v.Presets = out
	v.PresetIdx = clampIndex(v.PresetIdx, len(v.Presets))
}

func (v SDRView) EffectiveDevice() int {
	if len(v.Probe.Devices) == 0 {
		return 0
	}
	idx := clampIndex(v.DeviceIdx, len(v.Probe.Devices))
	return v.Probe.Devices[idx].Index
}

func (v SDRView) CurrentStepHz() int64 {
	return sdr.FreqSteps[clampIndex(v.RadioStepIdx, len(sdr.FreqSteps))]
}

func (v SDRView) CurrentGainDB() int {
	return sdr.GainOptions[clampIndex(v.RadioGainIdx, len(sdr.GainOptions))]
}

func (v SDRView) CurrentISMFreqHz() int64 {
	return sdr.ISMFreqs[clampIndex(v.ISMFreqIdx, len(sdr.ISMFreqs))]
}

func (v SDRView) CurrentISMGainDB() int {
	return sdr.GainOptions[clampIndex(v.ISMGainIdx, len(sdr.GainOptions))]
}

func (v SDRView) CurrentBand() sdr.BandPreset {
	return sdr.BandPresets[clampIndex(v.SpecBandIdx, len(sdr.BandPresets))]
}

func (v SDRView) IsTextInput() bool {
	return v.Tab == SDRTabRadio && (v.RadioMode == sdrRadioEditFreq || v.RadioMode == sdrRadioSaveName)
}

func (v SDRView) AnyActive() bool {
	return v.RadioActive || v.SpecActive || v.ISMActive
}

func (v *SDRView) StopRadio() {
	if v.RadioCancel != nil {
		v.RadioCancel()
		v.RadioCancel = nil
	}
	if v.RadioCtrl != nil {
		v.RadioCtrl.StopRecording()
	}
}

func (v *SDRView) StopSpec() {
	if v.SpecCancel != nil {
		v.SpecCancel()
		v.SpecCancel = nil
	}
}

func (v *SDRView) StopISM() {
	if v.ISMCancel != nil {
		v.ISMCancel()
		v.ISMCancel = nil
	}
}

func (v *SDRView) StopAll() {
	v.StopRadio()
	v.StopSpec()
	v.StopISM()
}

func (v *SDRView) SetProbeResult(st sdr.ProbeStatus) {
	v.Probe = st
	v.Probing = false
	if len(st.Devices) > 0 {
		v.DeviceIdx = clampIndex(v.DeviceIdx, len(st.Devices))
	} else {
		v.DeviceIdx = 0
	}
}

func (v *SDRView) HandleEsc() bool {
	switch v.Tab {
	case SDRTabRadio:
		switch v.RadioMode {
		case sdrRadioEditFreq, sdrRadioSaveName:
			if v.RadioActive {
				v.RadioMode = sdrRadioRunning
			} else {
				v.RadioMode = sdrRadioSetup
			}
			v.Input = ""
			v.InputErr = ""
			return true
		case sdrRadioPresets:
			if v.RadioActive {
				v.RadioMode = sdrRadioRunning
			} else {
				v.RadioMode = sdrRadioSetup
			}
			return true
		case sdrRadioRunning:
			v.StopRadio()
			return true
		}
	case SDRTabSpec:
		if v.SpecActive {
			v.StopSpec()
			return true
		}
	case SDRTabISM:
		switch v.ISMMode {
		case sdrISMDetail:
			v.ISMMode = sdrISMList
			return true
		case sdrISMList:
			if v.ISMActive {
				v.StopISM()
				return true
			}
			v.ISMMode = sdrISMSetup
			return true
		}
	}
	return false
}

// --- Radio Tab state transitions ---

func (v SDRView) radioFields() []sdrRadioField {
	out := []sdrRadioField{
		sdrFieldFreq,
		sdrFieldStep,
		sdrFieldMod,
		sdrFieldGain,
		sdrFieldSquelch,
		sdrFieldPreset,
	}
	if len(v.Probe.Devices) > 1 {
		out = append(out, sdrFieldDevice)
	}
	out = append(out, sdrFieldStart, sdrFieldSave)
	return out
}

func (v SDRView) runFields() []sdrRunField {
	return []sdrRunField{sdrRunFreq, sdrRunStep, sdrRunMute, sdrRunRecord, sdrRunStop}
}

func (v *SDRView) MoveRadioCursor(delta int) {
	if v.RadioMode == sdrRadioRunning {
		f := v.runFields()
		v.RunCursor = (v.RunCursor + delta + len(f)) % len(f)
		return
	}
	if v.RadioMode == sdrRadioPresets {
		if len(v.Presets) > 0 {
			v.PresetCursor = (v.PresetCursor + delta + len(v.Presets)) % len(v.Presets)
		}
		return
	}
	f := v.radioFields()
	v.RadioCursor = (v.RadioCursor + delta + len(f)) % len(f)
}

// AdjustRadio modifies the selected enum/numeric field and reports whether a live retune is needed.
func (v *SDRView) AdjustRadio(delta int) (retune bool) {
	if v.RadioMode == sdrRadioRunning {
		f := v.runFields()
		switch f[clampIndex(v.RunCursor, len(f))] {
		case sdrRunFreq:
			v.RadioFreqHz = sdr.ClampFreq(v.RadioFreqHz + int64(delta)*v.CurrentStepHz())
			v.SpecCenterHz = v.RadioFreqHz
			return true
		case sdrRunStep:
			v.RadioStepIdx = (v.RadioStepIdx + delta + len(sdr.FreqSteps)) % len(sdr.FreqSteps)
		case sdrRunMute:
			v.ToggleMute()
		case sdrRunRecord:
			v.ToggleRecord()
		}
		return false
	}

	f := v.radioFields()
	switch f[clampIndex(v.RadioCursor, len(f))] {
	case sdrFieldFreq:
		v.RadioFreqHz = sdr.ClampFreq(v.RadioFreqHz + int64(delta)*v.CurrentStepHz())
		v.SpecCenterHz = v.RadioFreqHz
	case sdrFieldStep:
		v.RadioStepIdx = (v.RadioStepIdx + delta + len(sdr.FreqSteps)) % len(sdr.FreqSteps)
	case sdrFieldMod:
		v.RadioMod = sdr.CycleDemodMode(v.RadioMod, delta)
	case sdrFieldGain:
		v.RadioGainIdx = (v.RadioGainIdx + delta + len(sdr.GainOptions)) % len(sdr.GainOptions)
	case sdrFieldSquelch:
		v.RadioSquelch = clampInt(v.RadioSquelch+delta*5, 0, 100)
	case sdrFieldPreset:
		if len(v.Presets) > 0 {
			v.PresetIdx = (v.PresetIdx + delta + len(v.Presets)) % len(v.Presets)
			p := v.Presets[v.PresetIdx]
			v.applyPreset(p)
		}
	case sdrFieldDevice:
		if len(v.Probe.Devices) > 0 {
			v.DeviceIdx = (v.DeviceIdx + delta + len(v.Probe.Devices)) % len(v.Probe.Devices)
		}
	}
	return false
}

func (v *SDRView) applyPreset(p sdr.Preset) {
	v.RadioFreqHz = sdr.ClampFreq(p.FreqHz)
	v.SpecCenterHz = v.RadioFreqHz
	v.RadioMod = p.Mode
	if p.GainDB != 0 || p.User {
		for i, g := range sdr.GainOptions {
			if g == p.GainDB {
				v.RadioGainIdx = i
				break
			}
		}
	}
}

func (v *SDRView) BeginEditFreq() {
	v.Input = fmt.Sprintf("%.3f", float64(v.RadioFreqHz)/1e6)
	v.InputErr = ""
	v.RadioMode = sdrRadioEditFreq
}

func (v *SDRView) CommitEditFreq() (ok bool, wasRunning bool) {
	hz, err := sdr.ParseFreqMHz(v.Input)
	if err != nil {
		v.InputErr = err.Error()
		return false, false
	}
	v.RadioFreqHz = hz
	v.SpecCenterHz = hz
	v.Input = ""
	v.InputErr = ""
	if v.RadioActive {
		v.RadioMode = sdrRadioRunning
		return true, true
	}
	v.RadioMode = sdrRadioSetup
	return true, false
}

func (v *SDRView) BeginSaveBookmark() {
	v.Input = strings.TrimSpace(sdr.FormatFreqShort(v.RadioFreqHz) + " " + v.RadioMod.String())
	v.InputErr = ""
	v.RadioMode = sdrRadioSaveName
}

func (v *SDRView) CommitSaveBookmark() error {
	name := strings.TrimSpace(v.Input)
	if name == "" {
		v.InputErr = "enter a name"
		return fmt.Errorf("empty bookmark name")
	}
	bm := sdr.Preset{
		Name:   name,
		FreqHz: v.RadioFreqHz,
		Mode:   v.RadioMod,
		GainDB: v.CurrentGainDB(),
		User:   true,
	}
	v.Bookmarks = append([]sdr.Preset{bm}, v.Bookmarks...)
	v.rebuildPresets()
	v.PresetIdx = 0
	v.Input = ""
	v.InputErr = ""
	v.RadioMode = sdrRadioSetup
	if err := sdr.SaveBookmarks(v.BookmarksPath, v.Bookmarks); err != nil {
		v.Note = err.Error()
		v.NoteIsErr = true
		return err
	}
	v.Note = "Saved " + name
	v.NoteIsErr = false
	return nil
}

func (v *SDRView) DeleteSelectedBookmark() bool {
	if v.RadioMode != sdrRadioPresets || len(v.Presets) == 0 {
		return false
	}
	idx := clampIndex(v.PresetCursor, len(v.Presets))
	if !v.Presets[idx].User || idx >= len(v.Bookmarks) {
		return false
	}
	name := v.Bookmarks[idx].Name
	v.Bookmarks = append(v.Bookmarks[:idx], v.Bookmarks[idx+1:]...)
	v.rebuildPresets()
	v.PresetCursor = clampIndex(idx, max(1, len(v.Presets)))
	_ = sdr.SaveBookmarks(v.BookmarksPath, v.Bookmarks)
	v.Note = "Deleted " + name
	v.NoteIsErr = false
	return true
}

func (v *SDRView) SelectPresetFromList() {
	if len(v.Presets) == 0 {
		v.RadioMode = sdrRadioSetup
		return
	}
	idx := clampIndex(v.PresetCursor, len(v.Presets))
	v.PresetIdx = idx
	v.applyPreset(v.Presets[idx])
	v.RadioMode = sdrRadioSetup
}

func (v *SDRView) ToggleMute() bool {
	v.RadioMuted = !v.RadioMuted
	if v.RadioCtrl != nil {
		v.RadioCtrl.SetMuted(v.RadioMuted)
	}
	return v.RadioMuted
}

func (v *SDRView) ToggleRecord() (string, error) {
	if v.RadioCtrl == nil {
		return "", fmt.Errorf("radio not running")
	}
	if v.RadioCtrl.RecordingPath() != "" {
		path := v.RadioCtrl.StopRecording()
		v.RadioRecPath = ""
		return path, nil
	}
	path, err := v.RadioCtrl.StartRecording(v.CaptureDir, v.RadioFreqHz, v.RadioMod)
	if err != nil {
		v.RadioErr = err.Error()
		return "", err
	}
	v.RadioRecPath = path
	return path, nil
}

func (v *SDRView) StartRadio() (context.Context, int, sdr.RadioConfig, *sdr.RadioController, chan sdr.RadioUpdate) {
	v.StopAll()
	ctx, cancel := context.WithCancel(context.Background())
	v.RadioSeq++
	v.RadioActive = true
	v.RadioCancel = cancel
	cfg := sdr.RadioConfig{
		DeviceIndex: v.EffectiveDevice(),
		FreqHz:      v.RadioFreqHz,
		Mode:        v.RadioMod,
		GainDB:      v.CurrentGainDB(),
		Squelch:     v.RadioSquelch,
		CaptureDir:  v.CaptureDir,
	}
	_, rate := sdr.BuildRTLFMArgs(cfg)
	ctrl := sdr.NewRadioController(rate)
	ctrl.SetMuted(v.RadioMuted)
	v.RadioCtrl = ctrl
	v.RadioUpdate = make(chan sdr.RadioUpdate, 16)
	v.RadioLevel = 0
	v.RadioDBFS = -60
	v.RadioRecPath = ""
	v.RadioErr = ""
	v.RadioStarted = time.Now()
	v.RadioElapsed = 0
	v.RadioMode = sdrRadioRunning
	return ctx, v.RadioSeq, cfg, ctrl, v.RadioUpdate
}

func (v *SDRView) ApplyRadioUpdate(seq int, u sdr.RadioUpdate) {
	if seq != v.RadioSeq {
		return
	}
	v.RadioLevel = u.Level
	v.RadioDBFS = u.DBFS
	v.RadioMuted = u.Muted
	v.RadioRecPath = u.RecPath
}

func (v *SDRView) FinishRadio(seq int, err error) {
	if seq != v.RadioSeq {
		return
	}
	v.RadioActive = false
	v.RadioCancel = nil
	if v.RadioCtrl != nil {
		v.RadioCtrl.StopRecording()
		v.RadioCtrl = nil
	}
	v.RadioRecPath = ""
	v.RadioMode = sdrRadioSetup
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		v.RadioErr = err.Error()
	}
}

// --- Spec Tab state transitions ---

func (v *SDRView) ToggleSpecKind() {
	if v.SpecActive {
		v.StopSpec()
	}
	if v.SpecKind == sdrSpecLive {
		v.SpecKind = sdrSpecSweep
	} else {
		v.SpecKind = sdrSpecLive
	}
	v.SpecHasFrame = false
	v.SpecErr = ""
}

func (v *SDRView) MoveSpecMarker(delta int) {
	n := len(v.SpecFrame.Bins)
	if n <= 0 {
		if v.SpecKind == sdrSpecSweep {
			v.SpecBandIdx = (v.SpecBandIdx + delta + len(sdr.BandPresets)) % len(sdr.BandPresets)
		} else {
			v.SpecCenterHz = sdr.ClampFreq(v.SpecCenterHz + int64(delta)*v.CurrentStepHz())
		}
		return
	}
	v.SpecMarker = (v.SpecMarker + delta + n) % n
}

func (v *SDRView) StepSpecSpan(delta int) (restart bool) {
	if v.SpecKind == sdrSpecSweep {
		v.SpecBandIdx = (v.SpecBandIdx + delta + len(sdr.BandPresets)) % len(sdr.BandPresets)
		return v.SpecActive
	}
	v.SpecCenterHz = sdr.ClampFreq(v.SpecCenterHz + int64(delta)*1_000_000)
	return v.SpecActive
}

func (v *SDRView) MarkerFreqHz() (int64, sdr.DemodMode) {
	mode := v.RadioMod
	if v.SpecKind == sdrSpecSweep {
		mode = v.CurrentBand().DefaultMode
	}
	if v.SpecHasFrame && len(v.SpecFrame.Bins) > 0 {
		return v.SpecFrame.BinFreqHz(v.SpecMarker), mode
	}
	return v.SpecCenterHz, mode
}

func (v *SDRView) StartSpecLive(width int) (context.Context, int, sdr.SpectrumConfig, chan sdr.SpectrumFrame) {
	v.StopAll()
	if width <= 0 {
		width = 34
	}
	v.SpecLastWidth = width
	ctx, cancel := context.WithCancel(context.Background())
	v.SpecSeq++
	v.SpecActive = true
	v.SpecCancel = cancel
	v.SpecErr = ""
	v.SpecStarted = time.Now()
	v.SpecElapsed = 0
	v.SpecFrameCh = make(chan sdr.SpectrumFrame, 4)
	cfg := sdr.SpectrumConfig{
		DeviceIndex: v.EffectiveDevice(),
		CenterHz:    v.SpecCenterHz,
		SampleRate:  sdr.DefaultSpecRate,
		GainDB:      v.CurrentGainDB(),
		OutBins:     width,
		FFTSize:     sdr.DefaultFFTSize,
	}
	return ctx, v.SpecSeq, cfg, v.SpecFrameCh
}

func (v *SDRView) StartSpecSweep(width int) (context.Context, int, int, sdr.BandPreset, int, int) {
	v.StopAll()
	if width <= 0 {
		width = 34
	}
	v.SpecLastWidth = width
	ctx, cancel := context.WithCancel(context.Background())
	v.SpecSeq++
	v.SpecActive = true
	v.SpecCancel = cancel
	v.SpecErr = ""
	v.SpecStarted = time.Now()
	v.SpecElapsed = 0
	return ctx, v.SpecSeq, v.EffectiveDevice(), v.CurrentBand(), v.CurrentGainDB(), width
}

func (v *SDRView) ApplySpecFrame(seq int, frame sdr.SpectrumFrame) {
	if seq != v.SpecSeq {
		return
	}
	first := !v.SpecHasFrame
	v.SpecFrame = frame
	v.SpecHasFrame = true
	if len(frame.Bins) > 0 {
		if first {
			// Snap marker to peak on first frame.
			peakIdx := 0
			for i, b := range frame.Bins {
				if b > frame.Bins[peakIdx] {
					peakIdx = i
				}
			}
			v.SpecMarker = peakIdx
		} else {
			v.SpecMarker = clampIndex(v.SpecMarker, len(frame.Bins))
		}
	}
}

func (v *SDRView) FinishSpec(seq int, frame sdr.SpectrumFrame, hasFrame bool, err error) {
	if seq != v.SpecSeq {
		return
	}
	v.SpecActive = false
	v.SpecCancel = nil
	if hasFrame {
		v.ApplySpecFrame(seq, frame)
	}
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		v.SpecErr = err.Error()
	}
}

// --- 433 / ISM Tab state transitions ---

func (v SDRView) ismFields() []sdrISMField {
	out := []sdrISMField{sdrISMBand, sdrISMGain, sdrISMLog, sdrISMStart}
	if len(v.ISMPackets) > 0 {
		out = append(out, sdrISMViewPkts)
	}
	return out
}

func (v *SDRView) MoveISMCursor(delta int) {
	switch v.ISMMode {
	case sdrISMSetup:
		f := v.ismFields()
		v.ISMCursor = (v.ISMCursor + delta + len(f)) % len(f)
	case sdrISMList:
		if len(v.ISMPackets) > 0 {
			v.ISMPktCursor = (v.ISMPktCursor + delta + len(v.ISMPackets)) % len(v.ISMPackets)
		}
	case sdrISMDetail:
		v.ISMDetailScroll = max(0, v.ISMDetailScroll+delta)
	}
}

func (v *SDRView) AdjustISM(delta int) {
	if v.ISMMode != sdrISMSetup {
		return
	}
	f := v.ismFields()
	switch f[clampIndex(v.ISMCursor, len(f))] {
	case sdrISMBand:
		v.ISMFreqIdx = (v.ISMFreqIdx + delta + len(sdr.ISMFreqs)) % len(sdr.ISMFreqs)
	case sdrISMGain:
		v.ISMGainIdx = (v.ISMGainIdx + delta + len(sdr.GainOptions)) % len(sdr.GainOptions)
	case sdrISMLog:
		v.ISMLog = !v.ISMLog
	}
}

func (v *SDRView) StartISM() (context.Context, int, sdr.ISMConfig, chan sdr.ISMPacket) {
	v.StopAll()
	ctx, cancel := context.WithCancel(context.Background())
	v.ISMSeq++
	v.ISMActive = true
	v.ISMCancel = cancel
	v.ISMErr = ""
	v.ISMPktCh = make(chan sdr.ISMPacket, 32)
	v.ISMStarted = time.Now()
	v.ISMElapsed = 0
	v.ISMPktCursor = 0
	v.ISMMode = sdrISMList
	cfg := sdr.ISMConfig{
		DeviceIndex: v.EffectiveDevice(),
		FreqHz:      v.CurrentISMFreqHz(),
		GainDB:      v.CurrentISMGainDB(),
		LogToFile:   v.ISMLog,
		CaptureDir:  v.CaptureDir,
	}
	return ctx, v.ISMSeq, cfg, v.ISMPktCh
}

func (v *SDRView) AppendISMPacket(seq int, pkt sdr.ISMPacket) {
	if seq != v.ISMSeq {
		return
	}
	v.ISMPackets = append([]sdr.ISMPacket{pkt}, v.ISMPackets...)
	if len(v.ISMPackets) > 100 {
		v.ISMPackets = v.ISMPackets[:100]
	}
}

func (v *SDRView) FinishISM(seq int, err error) {
	if seq != v.ISMSeq {
		return
	}
	v.ISMActive = false
	v.ISMCancel = nil
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		v.ISMErr = err.Error()
		if len(v.ISMPackets) == 0 {
			v.ISMMode = sdrISMSetup
		}
	}
}

// --- Rendering ---

func (v SDRView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	switch v.Tab {
	case SDRTabSpec:
		return v.renderSpec(w, h, pal, focused)
	case SDRTabISM:
		return v.renderISM(w, h, pal, focused)
	default:
		return v.renderRadio(w, h, pal, focused)
	}
}

func (v SDRView) dongleHeader(w int, pal theme.Palette) string {
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)

	if v.Probing && len(v.Probe.Devices) == 0 {
		return warn.Render(v.Marquee.FitLine("Probing RTL-SDR...", w))
	}
	if len(v.Probe.Devices) == 0 {
		msg := "No RTL-SDR USB dongle"
		if v.Probe.Tools.RTLFM == "" && v.Probe.Tools.RTLSDR == "" {
			msg = "rtl-sdr not installed"
		}
		return bad.Render(v.Marquee.FitLine(msg, w))
	}
	dev := v.Probe.Devices[clampIndex(v.DeviceIdx, len(v.Probe.Devices))]
	if v.Probe.Err != "" && dev.ClaimErr != "" {
		return bad.Render(v.Marquee.FitLine(v.Probe.Err, w))
	}
	if dev.DVBDriver {
		return warn.Render(v.Marquee.FitLine(dev.Label()+" (DVB busy)", w))
	}
	if v.Probe.Tools.RTLFM == "" {
		return mut.Render(v.Marquee.FitLine(dev.Label()+" (no rtl_fm)", w))
	}
	return ok.Render(v.Marquee.FitLine(dev.Label(), w))
}

func (v SDRView) renderRadio(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)

	if v.RadioMode == sdrRadioEditFreq || v.RadioMode == sdrRadioSaveName {
		title := "Tune frequency (MHz)"
		hint := "e.g. 98.5, 162.55, 433.92M"
		if v.RadioMode == sdrRadioSaveName {
			title = "Bookmark name"
			hint = sdr.FormatFreqMHz(v.RadioFreqHz) + " · " + v.RadioMod.String()
		}
		lines := []string{
			hdr.Render(v.Marquee.FitLine(title, w)),
			mut.Render(v.Marquee.FitLine(hint, w)),
			txt.Render(v.Marquee.FitLine("> "+v.Input+"_", w)),
		}
		if v.InputErr != "" {
			lines = append(lines, bad.Render(v.Marquee.FitLine(v.InputErr, w)))
		}
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	if v.RadioMode == sdrRadioPresets {
		lines := []string{hdr.Render(v.Marquee.FitLine(fmt.Sprintf("Presets & Bookmarks (%d)", len(v.Presets)), w))}
		body := max(1, h-1)
		start, end := ScrollWindow(len(v.Presets), v.PresetCursor, body)
		for i := start; i < end; i++ {
			p := v.Presets[i]
			tag := ""
			if p.User {
				tag = "*"
			}
			right := fmt.Sprintf("%.2fM %s", float64(p.FreqHz)/1e6, p.Mode.String())
			nameW := max(6, w-len(right)-4)
			row := fmt.Sprintf(" %s%-*s %s", tag, nameW-len(tag), Truncate(p.Name, max(4, nameW-len(tag))), right)
			if i == v.PresetCursor && focused {
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else if p.User {
				lines = append(lines, warn.Render(v.Marquee.FitLine(row, w)))
			} else {
				lines = append(lines, txt.Render(v.Marquee.FitLine(row, w)))
			}
		}
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	if v.RadioMode == sdrRadioRunning {
		flags := v.RadioMod.String()
		if v.RadioRecPath != "" {
			flags += " REC"
		}
		if v.RadioMuted {
			flags += " MUTE"
		}
		head := fmt.Sprintf("● %s %s %s", sdr.FormatFreqShort(v.RadioFreqHz), flags, formatClock(int(v.RadioElapsed.Seconds())))
		barW := max(6, w-12)
		lvlRow := fmt.Sprintf("Lvl %s %3.0fdB", MiniBar(v.RadioLevel, barW, v.ASCII), v.RadioDBFS)
		lines := []string{
			ok.Render(v.Marquee.FitLine(head, w)),
			sec.Render(v.Marquee.FitLine(lvlRow, w)),
		}
		fields := v.runFields()
		body := max(1, h-len(lines))
		start, end := ScrollWindow(len(fields), v.RunCursor, body)
		labelW := 8
		valW := max(8, w-labelW-3)
		for i := start; i < end; i++ {
			f := fields[i]
			var lbl, val string
			style := txt
			switch f {
			case sdrRunFreq:
				lbl, val = "Freq", sdr.FormatFreqMHz(v.RadioFreqHz)
			case sdrRunStep:
				lbl, val = "Step", sdr.FormatStep(v.CurrentStepHz())
			case sdrRunMute:
				lbl = "Mute"
				if v.RadioMuted {
					val, style = "[MUTED]", warn
				} else {
					val = "[OFF]"
				}
			case sdrRunRecord:
				lbl = "Record"
				if v.RadioRecPath != "" {
					val, style = "[REC WAV]", bad
				} else {
					val = "[OFF]"
				}
			case sdrRunStop:
				lbl, val, style = "Action", "Stop radio", warn
			}
			active := i == v.RunCursor && focused
			pfx := "  "
			if active {
				pfx = "> "
			}
			row := fmt.Sprintf("%s%-*s %s", pfx, labelW, lbl, Truncate(val, valW))
			if active {
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				lines = append(lines, fmt.Sprintf("%s%s %s", mut.Render(pfx), sec.Render(fmt.Sprintf("%-*s", labelW, lbl)), style.Render(Truncate(val, valW))))
			}
		}
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	// Setup mode.
	lines := []string{v.dongleHeader(w, pal)}
	if v.RadioErr != "" {
		lines = append(lines, bad.Render(v.Marquee.FitLine(v.RadioErr, w)))
	} else if v.Note != "" {
		st := ok
		if v.NoteIsErr {
			st = bad
		}
		lines = append(lines, st.Render(v.Marquee.FitLine(v.Note, w)))
	}

	fields := v.radioFields()
	body := max(1, h-len(lines))
	start, end := ScrollWindow(len(fields), v.RadioCursor, body)
	labelW := 8
	valW := max(8, w-labelW-3)
	for i := start; i < end; i++ {
		f := fields[i]
		var lbl, val string
		style := txt
		switch f {
		case sdrFieldFreq:
			lbl, val = "Freq", sdr.FormatFreqMHz(v.RadioFreqHz)
		case sdrFieldStep:
			lbl, val = "Step", sdr.FormatStep(v.CurrentStepHz())
		case sdrFieldMod:
			lbl, val = "Mode", v.RadioMod.String()
		case sdrFieldGain:
			lbl, val = "Gain", sdr.FormatGain(v.CurrentGainDB())
		case sdrFieldSquelch:
			lbl, val = "Squelch", fmt.Sprintf("%d", v.RadioSquelch)
		case sdrFieldPreset:
			lbl = "Preset"
			if len(v.Presets) > 0 {
				val = v.Presets[clampIndex(v.PresetIdx, len(v.Presets))].Name
			} else {
				val = "none"
			}
		case sdrFieldDevice:
			lbl = "Dongle"
			if len(v.Probe.Devices) > 0 {
				d := v.Probe.Devices[clampIndex(v.DeviceIdx, len(v.Probe.Devices))]
				val = fmt.Sprintf("#%d %s", d.Index, d.Label())
			}
		case sdrFieldStart:
			lbl, val, style = "Action", "Start radio", ok
		case sdrFieldSave:
			lbl, val, style = "Bookmark", "Save current", sec
		}
		active := i == v.RadioCursor && focused
		pfx := "  "
		if active {
			pfx = "> "
		}
		row := fmt.Sprintf("%s%-*s %s", pfx, labelW, lbl, Truncate(val, valW))
		if active {
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			lines = append(lines, fmt.Sprintf("%s%s %s", mut.Render(pfx), sec.Render(fmt.Sprintf("%-*s", labelW, lbl)), style.Render(Truncate(val, valW))))
		}
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v SDRView) renderSpec(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	var head string
	headStyle := hdr
	switch {
	case v.SpecActive && v.SpecKind == sdrSpecSweep:
		head = fmt.Sprintf("● Sweep %s (%ds)", v.CurrentBand().Name, int(v.SpecElapsed.Seconds()))
		headStyle = ok
	case v.SpecActive:
		pk := ""
		if v.SpecHasFrame {
			pk = " pk " + sdr.FormatFreqShort(v.SpecFrame.PeakHz)
		}
		head = fmt.Sprintf("● %s ±1M%s", sdr.FormatFreqShort(v.SpecCenterHz), pk)
		headStyle = ok
	case v.SpecHasFrame && v.SpecKind == sdrSpecSweep:
		head = fmt.Sprintf("%s · pk %s", v.CurrentBand().Name, sdr.FormatFreqShort(v.SpecFrame.PeakHz))
	case v.SpecHasFrame:
		head = fmt.Sprintf("%s ±1M · pk %s", sdr.FormatFreqShort(v.SpecCenterHz), sdr.FormatFreqShort(v.SpecFrame.PeakHz))
	case v.SpecKind == sdrSpecSweep:
		b := v.CurrentBand()
		head = fmt.Sprintf("Sweep · %s %.0f-%.0fM", b.Name, b.StartMHz, b.EndMHz)
	default:
		head = "Spec · " + sdr.FormatFreqMHz(v.SpecCenterHz)
	}

	lines := []string{headStyle.Render(v.Marquee.FitLine(head, w))}
	barH := max(2, h-3)

	if !v.SpecHasFrame {
		if v.SpecErr != "" {
			lines = append(lines, bad.Render(v.Marquee.FitLine(v.SpecErr, w)))
		} else if v.SpecActive {
			lines = append(lines, warn.Render(v.Marquee.FitLine("Capturing spectrum...", w)))
		} else {
			lines = append(lines, v.dongleHeader(w, pal))
			if v.SpecKind == sdrSpecSweep {
				b := v.CurrentBand()
				lines = append(lines, txt.Render(v.Marquee.FitLine(fmt.Sprintf("Band: %s (%.1f-%.1fM)", b.Name, b.StartMHz, b.EndMHz), w)))
				lines = append(lines, mut.Render(v.Marquee.FitLine("↑↓:band  m:Live/Sweep  ⏎:sweep", w)))
			} else {
				lines = append(lines, txt.Render(v.Marquee.FitLine("Span: 2.048 MHz FFT", w)))
				lines = append(lines, mut.Render(v.Marquee.FitLine("←→:freq  m:Live/Sweep  ⏎:start", w)))
			}
		}
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	// Render vertical spectrum bars across w columns.
	bins := resampleBins(v.SpecFrame.Bins, w)
	marker := clampIndex(v.SpecMarker, len(bins))
	peakCol := 0
	for i, b := range bins {
		if b > bins[peakCol] {
			peakCol = i
		}
	}

	blocks := []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	if v.ASCII {
		blocks = []string{" ", ".", ".", ":", ":", "|", "|", "#", "#"}
	}

	const minDB = -70.0
	const maxDB = -5.0
	for row := barH - 1; row >= 0; row-- {
		var sb strings.Builder
		for col := 0; col < w; col++ {
			norm := (bins[col] - minDB) / (maxDB - minDB)
			if norm < 0 {
				norm = 0
			}
			if norm > 1 {
				norm = 1
			}
			totalSteps := norm * float64(barH*8)
			rowStart := float64(row * 8)
			level := int(totalSteps - rowStart + 0.5)
			if level < 0 {
				level = 0
			}
			if level > 8 {
				level = 8
			}
			ch := blocks[level]
			switch {
			case col == marker && focused:
				sb.WriteString(sel.Render(ch))
			case col == marker:
				sb.WriteString(hdr.Render(ch))
			case col == peakCol:
				sb.WriteString(warn.Render(ch))
			default:
				sb.WriteString(ok.Render(ch))
			}
		}
		lines = append(lines, sb.String())
	}

	// Marker track row.
	markChar := "▲"
	if v.ASCII {
		markChar = "^"
	}
	track := strings.Repeat(" ", marker) + hdr.Render(markChar)
	lines = append(lines, FitLine(track, w))

	// Bottom frequency/dB readout.
	mf := v.SpecFrame.BinFreqHz(marker)
	mdb := bins[marker]
	modeTag := "Live"
	if v.SpecKind == sdrSpecSweep {
		modeTag = "Swp"
	}
	readout := fmt.Sprintf("%s %.0fdB [%s]", sdr.FormatFreqMHz(mf), mdb, modeTag)
	if v.SpecErr != "" {
		readout = v.SpecErr
		lines = append(lines, bad.Render(v.Marquee.FitLine(readout, w)))
	} else {
		lines = append(lines, sec.Render(v.Marquee.FitLine(readout, w)))
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func resampleBins(bins []float64, w int) []float64 {
	if w <= 0 {
		return nil
	}
	if len(bins) == w {
		return bins
	}
	if len(bins) == 0 {
		out := make([]float64, w)
		for i := range out {
			out[i] = -70
		}
		return out
	}
	out := make([]float64, w)
	for i := 0; i < w; i++ {
		s := (i * len(bins)) / w
		e := ((i + 1) * len(bins)) / w
		if e <= s {
			e = s + 1
		}
		if e > len(bins) {
			e = len(bins)
		}
		peak := bins[s]
		for k := s + 1; k < e; k++ {
			if bins[k] > peak {
				peak = bins[k]
			}
		}
		out[i] = peak
	}
	return out
}

func (v SDRView) renderISM(w, h int, pal theme.Palette, focused bool) []string {
	hdr := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	mut := lipgloss.NewStyle().Foreground(pal.Muted)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	bad := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)

	if v.ISMMode == sdrISMDetail && len(v.ISMPackets) > 0 {
		pkt := v.ISMPackets[clampIndex(v.ISMPktCursor, len(v.ISMPackets))]
		lines := []string{hdr.Render(v.Marquee.FitLine(pkt.Model+" · "+pkt.Time, w))}
		var rows []string
		for _, f := range pkt.Fields {
			rows = append(rows, sec.Render(Truncate(f.Key, 12)+": ")+txt.Render(f.Value))
		}
		if len(rows) == 0 {
			rows = append(rows, mut.Render(pkt.RawJSON))
		}
		body := max(1, h-1)
		maxScroll := max(0, len(rows)-body)
		scr := min(v.ISMDetailScroll, maxScroll)
		end := min(len(rows), scr+body)
		for _, r := range rows[scr:end] {
			lines = append(lines, v.Marquee.FitLine(r, w))
		}
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	if v.ISMMode == sdrISMList {
		freq := sdr.FormatFreqShort(v.CurrentISMFreqHz())
		var head string
		headStyle := hdr
		if v.ISMActive {
			head = fmt.Sprintf("● %s · %d pkts · %s", freq, len(v.ISMPackets), formatClock(int(v.ISMElapsed.Seconds())))
			headStyle = ok
		} else {
			head = fmt.Sprintf("%s · %d pkts · idle", freq, len(v.ISMPackets))
		}
		lines := []string{headStyle.Render(v.Marquee.FitLine(head, w))}
		if v.ISMErr != "" {
			lines = append(lines, bad.Render(v.Marquee.FitLine(v.ISMErr, w)))
		}
		if len(v.ISMPackets) == 0 {
			if v.ISMActive {
				lines = append(lines, mut.Render(v.Marquee.FitLine("Listening for ISM signals...", w)))
			} else {
				lines = append(lines, mut.Render(v.Marquee.FitLine("No packets decoded", w)))
			}
			return v.Marquee.SplitLinesPadded(lines, w, h)
		}
		body := max(1, h-len(lines))
		start, end := ScrollWindow(len(v.ISMPackets), v.ISMPktCursor, body)
		for i := start; i < end; i++ {
			p := v.ISMPackets[i]
			row := fmt.Sprintf("%s %s", p.Time, p.Model)
			if p.Summary != "" {
				row += " · " + p.Summary
			}
			if i == v.ISMPktCursor && focused {
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				lines = append(lines, txt.Render(v.Marquee.FitLine(row, w)))
			}
		}
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	// Setup mode.
	var head string
	headStyle := hdr
	switch {
	case v.Probe.Tools.RTL433 == "":
		head = "rtl_433 missing (apt rtl-433)"
		headStyle = bad
	case len(v.Probe.Devices) == 0:
		head = "No RTL-SDR USB dongle"
		headStyle = bad
	default:
		head = "ISM 433/315/868/915 (rtl_433)"
	}
	lines := []string{headStyle.Render(v.Marquee.FitLine(head, w))}
	if v.ISMErr != "" {
		lines = append(lines, bad.Render(v.Marquee.FitLine(v.ISMErr, w)))
	}

	fields := v.ismFields()
	body := max(1, h-len(lines))
	start, end := ScrollWindow(len(fields), v.ISMCursor, body)
	labelW := 8
	valW := max(8, w-labelW-3)
	for i := start; i < end; i++ {
		f := fields[i]
		var lbl, val string
		style := txt
		switch f {
		case sdrISMBand:
			lbl, val = "Band", sdr.FormatFreqMHz(v.CurrentISMFreqHz())
		case sdrISMGain:
			lbl, val = "Gain", sdr.FormatGain(v.CurrentISMGainDB())
		case sdrISMLog:
			lbl = "Log"
			if v.ISMLog {
				val, style = "[ON jsonl]", ok
			} else {
				val = "[OFF]"
			}
		case sdrISMStart:
			lbl, val, style = "Action", "Start decoder", ok
			if v.Probe.Tools.RTL433 == "" {
				style = warn
			}
		case sdrISMViewPkts:
			lbl, val, style = "Packets", fmt.Sprintf("View %d decoded", len(v.ISMPackets)), sec
		}
		active := i == v.ISMCursor && focused
		pfx := "  "
		if active {
			pfx = "> "
		}
		row := fmt.Sprintf("%s%-*s %s", pfx, labelW, lbl, Truncate(val, valW))
		if active {
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			lines = append(lines, fmt.Sprintf("%s%s %s", mut.Render(pfx), sec.Render(fmt.Sprintf("%-*s", labelW, lbl)), style.Render(Truncate(val, valW))))
		}
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}
