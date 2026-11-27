package ui

import (
	"context"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/sdr"
)

type sdrProbeDoneMsg struct {
	st sdr.ProbeStatus
}

type sdrRadioUpdateMsg struct {
	seq int
	u   sdr.RadioUpdate
}

type sdrRadioDoneMsg struct {
	seq int
	err error
}

type sdrSpecFrameMsg struct {
	seq   int
	frame sdr.SpectrumFrame
}

type sdrSpecDoneMsg struct {
	seq      int
	frame    sdr.SpectrumFrame
	hasFrame bool
	err      error
}

type sdrISMPacketMsg struct {
	seq int
	pkt sdr.ISMPacket
}

type sdrISMDoneMsg struct {
	seq int
	err error
}

type sdrTickMsg struct{}

func probeSDRCmd(skipRTLTest bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return sdrProbeDoneMsg{st: sdr.Probe(ctx, skipRTLTest)}
	}
}

func runSDRRadioCmd(ctx context.Context, seq int, cfg sdr.RadioConfig, ctrl *sdr.RadioController, ch chan sdr.RadioUpdate) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		err := sdr.RunRadio(ctx, cfg, ctrl, func(u sdr.RadioUpdate) {
			select {
			case ch <- u:
			default:
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- u:
				default:
				}
			}
		})
		return sdrRadioDoneMsg{seq: seq, err: err}
	}
}

func waitSDRRadioUpdateCmd(seq int, ch <-chan sdr.RadioUpdate) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		u, ok := <-ch
		if !ok {
			return nil
		}
		return sdrRadioUpdateMsg{seq: seq, u: u}
	}
}

func runSDRSpecLiveCmd(ctx context.Context, seq int, cfg sdr.SpectrumConfig, ch chan sdr.SpectrumFrame) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		err := sdr.RunSpectrum(ctx, cfg, func(f sdr.SpectrumFrame) {
			select {
			case ch <- f:
			default:
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- f:
				default:
				}
			}
		})
		return sdrSpecDoneMsg{seq: seq, err: err}
	}
}

func waitSDRSpecFrameCmd(seq int, ch <-chan sdr.SpectrumFrame) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		f, ok := <-ch
		if !ok {
			return nil
		}
		return sdrSpecFrameMsg{seq: seq, frame: f}
	}
}

func runSDRSpecSweepCmd(ctx context.Context, seq, devIdx int, band sdr.BandPreset, gainDB, outBins int) tea.Cmd {
	return func() tea.Msg {
		frame, err := sdr.RunBandSweep(ctx, devIdx, band, gainDB, outBins)
		return sdrSpecDoneMsg{seq: seq, frame: frame, hasFrame: err == nil, err: err}
	}
}

func runSDRISMCmd(ctx context.Context, seq int, cfg sdr.ISMConfig, ch chan sdr.ISMPacket) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		err := sdr.RunISM(ctx, cfg, func(pkt sdr.ISMPacket) {
			select {
			case ch <- pkt:
			default:
			}
		}, nil)
		return sdrISMDoneMsg{seq: seq, err: err}
	}
}

func waitSDRISMPacketCmd(seq int, ch <-chan sdr.ISMPacket) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		pkt, ok := <-ch
		if !ok {
			return nil
		}
		return sdrISMPacketMsg{seq: seq, pkt: pkt}
	}
}

func sdrTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return sdrTickMsg{} })
}

func (m AppModel) startSDRRadio() (tea.Model, tea.Cmd) {
	ctx, seq, cfg, ctrl, ch := m.SDR.StartRadio()
	return m, tea.Batch(
		m.setStatus("SDR "+sdr.FormatFreqShort(cfg.FreqHz)+" "+cfg.Mode.String()),
		runSDRRadioCmd(ctx, seq, cfg, ctrl, ch),
		waitSDRRadioUpdateCmd(seq, ch),
		sdrTickCmd(),
	)
}

func (m AppModel) startSDRSpec() (tea.Model, tea.Cmd) {
	_, mainW, _ := m.dimensions()
	if m.SDR.SpecKind == sdrSpecSweep {
		ctx, seq, devIdx, band, gain, width := m.SDR.StartSpecSweep(mainW)
		return m, tea.Batch(
			m.setStatus("Sweeping "+band.Name+"..."),
			runSDRSpecSweepCmd(ctx, seq, devIdx, band, gain, width),
			sdrTickCmd(),
		)
	}
	ctx, seq, cfg, ch := m.SDR.StartSpecLive(mainW)
	return m, tea.Batch(
		m.setStatus("Spectrum "+sdr.FormatFreqShort(cfg.CenterHz)),
		runSDRSpecLiveCmd(ctx, seq, cfg, ch),
		waitSDRSpecFrameCmd(seq, ch),
		sdrTickCmd(),
	)
}

func (m AppModel) startSDRISM() (tea.Model, tea.Cmd) {
	ctx, seq, cfg, ch := m.SDR.StartISM()
	return m, tea.Batch(
		m.setStatus("ISM "+sdr.FormatFreqShort(cfg.FreqHz)),
		runSDRISMCmd(ctx, seq, cfg, ch),
		waitSDRISMPacketCmd(seq, ch),
		sdrTickCmd(),
	)
}

func (m AppModel) handleSDRTextKey(k string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.SDR
	switch k {
	case "esc":
		v.HandleEsc()
		return m, nil
	case "enter":
		if v.RadioMode == sdrRadioEditFreq {
			ok, wasRunning := v.CommitEditFreq()
			if !ok {
				return m, nil
			}
			if wasRunning {
				return m.startSDRRadio()
			}
			return m, m.setStatus("Tuned " + sdr.FormatFreqMHz(v.RadioFreqHz))
		}
		if err := v.CommitSaveBookmark(); err != nil {
			return m, nil
		}
		return m, m.setStatus(v.Note)
	case "backspace", "ctrl+h":
		if len(v.Input) > 0 {
			_, sz := utf8.DecodeLastRuneInString(v.Input)
			v.Input = v.Input[:len(v.Input)-sz]
			v.InputErr = ""
		}
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				if r >= ' ' && len(v.Input) < 32 {
					v.Input += string(r)
				}
			}
			v.InputErr = ""
		}
		return m, nil
	}
}

func (m AppModel) handleSDRKey(k string) (tea.Model, tea.Cmd) {
	v := &m.SDR
	v.Tab = SDRTab(m.ActiveTabs[ViewSDR])
	switch v.Tab {
	case SDRTabSpec:
		switch {
		case k == "m":
			v.ToggleSpecKind()
			return m, m.resetMarquee()
		case k == "s" || k == " ":
			if v.SpecActive {
				v.StopSpec()
				return m, m.setStatus("Stopping spectrum...")
			}
			return m.startSDRSpec()
		case m.isLeft(k):
			v.MoveSpecMarker(-1)
		case m.isRight(k):
			v.MoveSpecMarker(1)
		case m.isUp(k):
			delta := 1
			if v.SpecKind == sdrSpecSweep {
				delta = -1
			}
			if v.StepSpecSpan(delta) {
				return m.startSDRSpec()
			}
		case m.isDown(k):
			delta := -1
			if v.SpecKind == sdrSpecSweep {
				delta = 1
			}
			if v.StepSpecSpan(delta) {
				return m.startSDRSpec()
			}
		case k == "enter":
			if !v.SpecActive && !v.SpecHasFrame {
				return m.startSDRSpec()
			}
			freq, mode := v.MarkerFreqHz()
			v.StopSpec()
			v.RadioFreqHz = freq
			v.RadioMod = mode
			v.RadioMode = sdrRadioSetup
			m.ActiveTabs[ViewSDR] = int(SDRTabRadio)
			v.Tab = SDRTabRadio
			return m, m.setStatus("Tuned " + sdr.FormatFreqMHz(freq))
		}
		return m, nil

	case SDRTabISM:
		switch v.ISMMode {
		case sdrISMDetail:
			switch {
			case m.isUp(k):
				v.MoveISMCursor(-1)
			case m.isDown(k):
				v.MoveISMCursor(1)
			}
			return m, nil
		case sdrISMList:
			switch {
			case m.isUp(k):
				v.MoveISMCursor(-1)
			case m.isDown(k):
				v.MoveISMCursor(1)
			case k == "s":
				if v.ISMActive {
					v.StopISM()
					return m, m.setStatus("Stopping decoder...")
				}
				return m.startSDRISM()
			case k == "enter" || k == " ":
				if len(v.ISMPackets) > 0 {
					v.ISMDetailScroll = 0
					v.ISMMode = sdrISMDetail
				}
			}
			return m, nil
		default:
			switch {
			case m.isUp(k):
				v.MoveISMCursor(-1)
			case m.isDown(k):
				v.MoveISMCursor(1)
			case m.isLeft(k):
				v.AdjustISM(-1)
			case m.isRight(k):
				v.AdjustISM(1)
			case k == "enter" || k == " ":
				f := v.ismFields()
				switch f[clampIndex(v.ISMCursor, len(f))] {
				case sdrISMStart:
					return m.startSDRISM()
				case sdrISMViewPkts:
					v.ISMMode = sdrISMList
				default:
					v.AdjustISM(1)
				}
			}
			return m, nil
		}

	default: // SDRTabRadio
		switch v.RadioMode {
		case sdrRadioPresets:
			switch {
			case m.isUp(k):
				v.MoveRadioCursor(-1)
			case m.isDown(k):
				v.MoveRadioCursor(1)
			case k == "enter" || k == " ":
				v.SelectPresetFromList()
				return m, m.setStatus("Loaded " + sdr.FormatFreqShort(v.RadioFreqHz))
			case k == "x" || k == "delete" || k == "backspace":
				if v.DeleteSelectedBookmark() {
					return m, m.setStatus(v.Note)
				}
			}
			return m, nil

		case sdrRadioRunning:
			switch {
			case m.isUp(k):
				v.MoveRadioCursor(-1)
			case m.isDown(k):
				v.MoveRadioCursor(1)
			case m.isLeft(k):
				if v.AdjustRadio(-1) {
					return m.startSDRRadio()
				}
			case m.isRight(k):
				if v.AdjustRadio(1) {
					return m.startSDRRadio()
				}
			case k == "m":
				muted := v.ToggleMute()
				if muted {
					return m, m.setStatus("Muted")
				}
				return m, m.setStatus("Unmuted")
			case k == "enter" || k == " ":
				f := v.runFields()
				switch f[clampIndex(v.RunCursor, len(f))] {
				case sdrRunFreq:
					v.BeginEditFreq()
				case sdrRunStep:
					v.AdjustRadio(1)
				case sdrRunMute:
					if v.ToggleMute() {
						return m, m.setStatus("Muted")
					}
					return m, m.setStatus("Unmuted")
				case sdrRunRecord:
					wasRec := v.RadioRecPath != ""
					path, err := v.ToggleRecord()
					if err != nil {
						return m, m.setStatus("Record failed")
					}
					if wasRec {
						return m, m.setStatus("Saved " + filepath.Base(path))
					}
					return m, m.setStatus("Recording WAV...")
				case sdrRunStop:
					v.StopRadio()
					return m, m.setStatus("Stopping radio...")
				}
			}
			return m, nil

		default: // sdrRadioSetup
			switch {
			case m.isUp(k):
				v.MoveRadioCursor(-1)
			case m.isDown(k):
				v.MoveRadioCursor(1)
			case m.isLeft(k):
				v.AdjustRadio(-1)
			case m.isRight(k):
				v.AdjustRadio(1)
			case k == "p":
				v.PresetCursor = clampIndex(v.PresetIdx, max(1, len(v.Presets)))
				v.RadioMode = sdrRadioPresets
			case k == "enter" || k == " ":
				f := v.radioFields()
				switch f[clampIndex(v.RadioCursor, len(f))] {
				case sdrFieldFreq:
					v.BeginEditFreq()
				case sdrFieldPreset:
					v.PresetCursor = clampIndex(v.PresetIdx, max(1, len(v.Presets)))
					v.RadioMode = sdrRadioPresets
				case sdrFieldStart:
					return m.startSDRRadio()
				case sdrFieldSave:
					v.BeginSaveBookmark()
				default:
					v.AdjustRadio(1)
				}
			}
			return m, nil
		}
	}
}

func (m AppModel) sdrHints() []Hint {
	v := m.SDR
	switch SDRTab(m.ActiveTabs[ViewSDR]) {
	case SDRTabSpec:
		if v.SpecActive {
			return []Hint{{Key: "←→", Desc: "mark"}, {Key: "⏎", Desc: "tune"}, {Key: "m", Desc: "mode"}, {Key: "Esc", Desc: "stop"}}
		}
		if v.SpecHasFrame {
			return []Hint{{Key: "←→", Desc: "mark"}, {Key: "⏎", Desc: "tune"}, {Key: "s", Desc: "scan"}, {Key: "m", Desc: "mode"}, {Key: "Esc", Desc: "tabs"}}
		}
		return []Hint{{Key: "⏎", Desc: "start"}, {Key: "m", Desc: "mode"}, {Key: "↑↓", Desc: "span"}, {Key: "Esc", Desc: "tabs"}}

	case SDRTabISM:
		switch v.ISMMode {
		case sdrISMDetail:
			return []Hint{{Key: "↑↓", Desc: "scr"}, {Key: "Esc", Desc: "back"}}
		case sdrISMList:
			if v.ISMActive {
				return []Hint{{Key: "↑↓", Desc: "pkt"}, {Key: "⏎", Desc: "info"}, {Key: "Esc", Desc: "stop"}}
			}
			return []Hint{{Key: "↑↓", Desc: "pkt"}, {Key: "⏎", Desc: "info"}, {Key: "s", Desc: "start"}, {Key: "Esc", Desc: "back"}}
		default:
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "←→", Desc: "chg"}, {Key: "⏎", Desc: "ok"}, {Key: "Esc", Desc: "tabs"}}
		}

	default:
		switch v.RadioMode {
		case sdrRadioEditFreq, sdrRadioSaveName:
			return []Hint{{Key: "⏎", Desc: "ok"}, {Key: "Esc", Desc: "cancel"}}
		case sdrRadioPresets:
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "use"}, {Key: "x", Desc: "del"}, {Key: "Esc", Desc: "back"}}
		case sdrRadioRunning:
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "←→", Desc: "tune"}, {Key: "m", Desc: "mute"}, {Key: "Esc", Desc: "stop"}}
		default:
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "←→", Desc: "chg"}, {Key: "p", Desc: "pre"}, {Key: "⏎", Desc: "ok"}, {Key: "Esc", Desc: "tabs"}}
		}
	}
}

func isSdrCanceled(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "canceled")
}
