package ui

import (
	"context"
	"fmt"
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

type sdrSrvLineMsg struct {
	seq  int
	line string
}

type sdrSrvDoneMsg struct {
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

func runSDRSweepCmd(ctx context.Context, seq int, cfg sdr.SweepConfig, ch chan sdr.SpectrumFrame) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		err := sdr.RunSweep(ctx, cfg, func(f sdr.SpectrumFrame) {
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

func runSDRServerCmd(ctx context.Context, seq int, cfg sdr.ServerConfig, ch chan string) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		err := sdr.RunServer(ctx, cfg, func(line string) {
			select {
			case ch <- line:
			default:
			}
		})
		return sdrSrvDoneMsg{seq: seq, err: err}
	}
}

func waitSDRServerLineCmd(seq int, ch <-chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return sdrSrvLineMsg{seq: seq, line: line}
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

func (m AppModel) startSDRSweep() (tea.Model, tea.Cmd) {
	_, mainW, _ := m.dimensions()
	ctx, seq, cfg, ch := m.SDR.StartSweep(mainW)
	return m, tea.Batch(
		m.setStatus("Sweep "+sdr.FormatFreqShort(cfg.StartHz)+"-"+sdr.FormatFreqShort(cfg.EndHz)),
		runSDRSweepCmd(ctx, seq, cfg, ch),
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

func (m AppModel) startSDRServer() (tea.Model, tea.Cmd) {
	ctx, seq, cfg, ch := m.SDR.StartServer()
	return m, tea.Batch(
		m.setStatus(fmt.Sprintf("rtl_tcp :%d", cfg.Port)),
		runSDRServerCmd(ctx, seq, cfg, ch),
		waitSDRServerLineCmd(seq, ch),
		sdrTickCmd(),
	)
}

// sdrTextInput routes printable keys into the active inline editor.
func (m AppModel) sdrTextInput(k string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.SDR
	switch k {
	case "esc":
		v.HandleEsc()
		return m, nil
	case "backspace", "ctrl+h":
		if len(v.Input) > 0 {
			_, sz := utf8.DecodeLastRuneInString(v.Input)
			v.Input = v.Input[:len(v.Input)-sz]
			v.InputErr = ""
		}
		return m, nil
	case "enter":
		switch {
		case v.Tab == SDRTabRadio && v.RadioMode == sdrRadioEditFreq:
			ok, wasRunning := v.CommitEditFreq()
			if !ok {
				return m, nil
			}
			if wasRunning {
				return m.startSDRRadio()
			}
			return m, m.setStatus("Tuned " + sdr.FormatFreqMHz(v.RadioFreqHz))
		case v.Tab == SDRTabRadio && v.RadioMode == sdrRadioSaveName:
			if err := v.CommitSaveBookmark(); err != nil {
				return m, nil
			}
			return m, m.setStatus(v.Note)
		case v.Tab == SDRTabSpec:
			if v.CommitEditSweep() {
				return m, m.setStatus("Sweep " + sdr.FormatFreqShort(v.SpecStartHz) + "-" + sdr.FormatFreqShort(v.SpecEndHz))
			}
			return m, nil
		case v.Tab == SDRTabServer:
			if v.CommitEditSrv() {
				return m, m.setStatus("Updated server settings")
			}
			return m, nil
		}
		return m, nil
	}
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

func (m AppModel) handleSDRKey(k string) (tea.Model, tea.Cmd) {
	v := &m.SDR
	v.Tab = SDRTab(m.ActiveTabs[ViewSDR])
	switch v.Tab {
	case SDRTabSpec:
		switch v.SpecActive {
		case true:
			switch {
			case k == "m":
				v.ToggleSpecView()
				return m, m.resetMarquee()
			case k == "c":
				v.CycleContrast(1)
				return m, m.setStatus("Contrast " + v.ContrastName())
			case k == "z" || k == "+" || k == "=":
				if v.SetZoom(1) {
					return m.startSDRSweep()
				}
				return m, nil
			case k == "x" || k == "-" || k == "_":
				if v.SetZoom(-1) {
					return m.startSDRSweep()
				}
				return m, nil
			case m.isLeft(k):
				v.MoveSpecMarker(-1)
			case m.isRight(k):
				v.MoveSpecMarker(1)
			case m.isUp(k):
				v.JumpSpecMarker(-1)
			case m.isDown(k):
				v.JumpSpecMarker(1)
			case k == "s" || k == " ":
				v.StopSpec()
				return m, m.setStatus("Stopping sweep...")
			case k == "enter":
				return m.tuneFromSweep()
			}
			return m, nil
		case false:
			if v.SpecHasFrame {
				// Results view kept after a stop: browse and tune the last frame.
				switch {
				case k == "m":
					v.ToggleSpecView()
					return m, m.resetMarquee()
				case k == "c":
					v.CycleContrast(1)
					return m, m.setStatus("Contrast " + v.ContrastName())
				case k == "z" || k == "+" || k == "=":
					if v.SetZoom(1) {
						return m.startSDRSweep()
					}
					return m, nil
				case k == "x" || k == "-" || k == "_":
					if v.SetZoom(-1) {
						return m.startSDRSweep()
					}
					return m, nil
				case m.isLeft(k):
					v.MoveSpecMarker(-1)
				case m.isRight(k):
					v.MoveSpecMarker(1)
				case m.isUp(k):
					v.JumpSpecMarker(-1)
				case m.isDown(k):
					v.JumpSpecMarker(1)
				case k == "s" || k == " ":
					return m.startSDRSweep()
				case k == "enter":
					return m.tuneFromSweep()
				}
				return m, nil
			}
		}
		switch {
		case k == "m":
			v.ToggleSpecView()
			return m, m.resetMarquee()
		case k == "c":
			v.CycleContrast(1)
			return m, m.setStatus("Contrast " + v.ContrastName())
		case m.isUp(k):
			v.MoveSpecCursor(-1)
		case m.isDown(k):
			v.MoveSpecCursor(1)
		case m.isLeft(k):
			v.AdjustSpec(-1)
		case m.isRight(k):
			v.AdjustSpec(1)
		case k == "enter" || k == " ":
			f := v.specFields()
			switch f[clampIndex(v.SpecCursor, len(f))] {
			case sdrSpecFStart:
				v.BeginEditSweep(sdrSpecEditStart)
			case sdrSpecFEnd:
				v.BeginEditSweep(sdrSpecEditEnd)
			case sdrSpecFZoom:
				v.SetZoom(1)
			case sdrSpecFContrast:
				v.CycleContrast(1)
			case sdrSpecFPreset:
				v.cycleSpecPreset(1)
			case sdrSpecFView:
				v.ToggleSpecView()
			case sdrSpecFGain:
				v.AdjustSpec(1)
			case sdrSpecFAction:
				return m.startSDRSweep()
			}
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

	case SDRTabServer:
		if v.SrvActive {
			if k == "esc" || k == "q" || k == "s" || k == " " {
				v.StopServer()
				return m, m.setStatus("Stopping rtl_tcp...")
			}
			return m, nil
		}
		switch {
		case m.isUp(k):
			v.MoveSrvCursor(-1)
		case m.isDown(k):
			v.MoveSrvCursor(1)
		case m.isLeft(k):
			v.AdjustSrv(-1)
		case m.isRight(k):
			v.AdjustSrv(1)
		case k == "enter" || k == " ":
			f := v.srvFields()
			switch f[clampIndex(v.SrvCursor, len(f))] {
			case sdrSrvPort:
				v.BeginEditSrv(sdrSrvEditPort)
			case sdrSrvFreq:
				v.BeginEditSrv(sdrSrvEditFreq)
			case sdrSrvAction:
				return m.startSDRServer()
			default:
				v.AdjustSrv(1)
			}
		}
		return m, nil

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
				if v.ToggleMute() {
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
				case sdrRunBW:
					v.AdjustRadio(1)
					return m.startSDRRadio()
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

// tuneFromSweep moves the marker frequency into the Radio tab and switches to it.
func (m AppModel) tuneFromSweep() (tea.Model, tea.Cmd) {
	v := &m.SDR
	freq, mode := v.MarkerFreqHz()
	v.StopSpec()
	v.RadioFreqHz = freq
	v.RadioMod = mode
	v.RadioBwHz = sdr.NormalizeBandwidth(mode, v.RadioBwHz)
	v.RadioMode = sdrRadioSetup
	m.ActiveTabs[ViewSDR] = int(SDRTabRadio)
	v.Tab = SDRTabRadio
	return m, m.setStatus("Tuned " + sdr.FormatFreqMHz(freq))
}

func (m AppModel) sdrHints() []Hint {
	v := m.SDR
	switch SDRTab(m.ActiveTabs[ViewSDR]) {
	case SDRTabSpec:
		if v.IsTextInput() {
			return []Hint{{Key: "⏎", Desc: "ok"}, {Key: "Esc", Desc: "cancel"}}
		}
		if v.SpecActive || v.SpecHasFrame {
			return []Hint{{Key: "←→", Desc: "sig"}, {Key: "⏎", Desc: "tune"}, {Key: "z/x", Desc: "zoom"}, {Key: "c", Desc: "cont"}, {Key: "Esc", Desc: "stop"}}
		}
		return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "←→", Desc: "chg"}, {Key: "⏎", Desc: "ok"}, {Key: "Esc", Desc: "tabs"}}

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

	case SDRTabServer:
		if v.IsTextInput() {
			return []Hint{{Key: "⏎", Desc: "ok"}, {Key: "Esc", Desc: "cancel"}}
		}
		if v.SrvActive {
			return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "q", Desc: "stop"}}
		}
		return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "←→", Desc: "chg"}, {Key: "⏎", Desc: "ok"}, {Key: "Esc", Desc: "tabs"}}

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
