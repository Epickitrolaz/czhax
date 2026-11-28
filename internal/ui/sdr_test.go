package ui

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/sdr"
)

func newTestSDRApp(t *testing.T, w, h int) AppModel {
	t.Helper()
	cfg := config.Default()
	tmp := t.TempDir()
	cfg.WorkDir = tmp
	m := NewAppModel(cfg, filepath.Join(tmp, "config.json"), "v0.5.42")
	m.SDR = NewSDRView(tmp, filepath.Join(tmp, "captures"), filepath.Join(tmp, "sdr.json"))
	m.SDR.SetProbeResult(sdr.ProbeStatus{
		Devices: []sdr.USBDevice{{
			Index:     0,
			VendorID:  "0bda",
			ProductID: "2838",
			Product:   "RTL2838UHIDIR",
			Tuner:     "Rafael Micro R820T",
		}},
		Tools: sdr.Tools{
			RTLFM:    "/usr/bin/rtl_fm",
			RTLSDR:   "/usr/bin/rtl_sdr",
			RTLPower: "/usr/bin/rtl_power",
			RTLTest:  "/usr/bin/rtl_test",
			RTL433:   "/usr/bin/rtl_433",
			Player:   "/usr/bin/aplay",
		},
	})
	m.ActiveView = ViewSDR
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(AppModel)
}

func TestSDRAllSizesAndModes(t *testing.T) {
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
		m := newTestSDRApp(t, sz.w, sz.h)

		// Radio tab: FocusTabs & FocusContent
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Radio/Tabs")
		m = sendKey(m, "enter")
		if m.Focus != FocusContent {
			t.Fatalf("[%s] expected FocusContent on SDR Radio enter", sz.name)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Radio/Setup")

		// Left/right steps frequency and stays in FocusContent.
		beforeFreq := m.SDR.RadioFreqHz
		m = sendKey(m, "right")
		if m.Focus != FocusContent || m.SDR.RadioFreqHz <= beforeFreq {
			t.Fatalf("[%s] expected freq step up inside FocusContent, got focus=%v freq=%d", sz.name, m.Focus, m.SDR.RadioFreqHz)
		}
		m = sendKey(m, "left")
		if m.SDR.RadioFreqHz != beforeFreq {
			t.Fatalf("[%s] expected freq step back to %d, got %d", sz.name, beforeFreq, m.SDR.RadioFreqHz)
		}

		// Presets overlay
		m = sendKey(m, "p")
		if m.SDR.RadioMode != sdrRadioPresets {
			t.Fatalf("[%s] expected sdrRadioPresets", sz.name)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Radio/Presets")
		m = sendKey(m, "esc")
		if m.SDR.RadioMode != sdrRadioSetup || m.Focus != FocusContent {
			t.Fatalf("[%s] expected esc from presets to return to setup in FocusContent", sz.name)
		}

		// Edit frequency input
		m = sendKey(m, "enter")
		if m.SDR.RadioMode != sdrRadioEditFreq {
			t.Fatalf("[%s] expected sdrRadioEditFreq", sz.name)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Radio/EditFreq")
		m = sendKey(m, "esc")

		// Simulated running state with meter + REC + MUTE
		m.SDR.RadioMode = sdrRadioRunning
		m.SDR.RadioActive = true
		m.SDR.RadioLevel = 0.65
		m.SDR.RadioDBFS = -18
		m.SDR.RadioRecPath = "/tmp/test.wav"
		m.SDR.RadioElapsed = 12 * time.Second
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Radio/Running")
		m.SDR.RadioActive = false
		m.SDR.RadioMode = sdrRadioSetup

		// Spec tab: config, bars, and top-50 list
		m = sendKey(m, "esc") // back to FocusTabs
		m = sendKey(m, "right")
		if m.ActiveTabs[ViewSDR] != int(SDRTabSpec) {
			t.Fatalf("[%s] expected SDRTabSpec, got %d", sz.name, m.ActiveTabs[ViewSDR])
		}
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Spec/Config")

		m = sendKey(m, "m")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Spec/ConfigList")
		m = sendKey(m, "m")

		bins := make([]float64, 34)
		for i := range bins {
			bins[i] = -60 + float64(i%7)*5
		}
		bins[17] = -12
		m.SDR.SpecSeq = 1
		m.SDR.SpecHasFrame = true
		m.SDR.ApplySpecFrame(1, sdr.BuildSpectrumFrame(97_500_000, 99_548_000, bins))
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Spec/Bars")

		m = sendKey(m, "m")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Spec/Top50")
		m = sendKey(m, "m")

		// 433 / ISM tab: setup, packet list, packet detail
		m = sendKey(m, "esc") // results -> sweep config
		m = sendKey(m, "esc") // config -> tabs
		m = sendKey(m, "right")
		if m.ActiveTabs[ViewSDR] != int(SDRTabISM) {
			t.Fatalf("[%s] expected SDRTabISM, got %d", sz.name, m.ActiveTabs[ViewSDR])
		}
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/ISM/Setup")

		m.SDR.ISMSeq = 1
		m.SDR.ISMMode = sdrISMList
		m.SDR.ISMActive = true
		m.SDR.AppendISMPacket(1, sdr.ISMPacket{
			Time:    "14:22:05",
			Model:   "Acurite-Tower",
			Summary: "21.5C 48% BAT:OK",
			Fields: []sdr.ISMField{
				{Key: "id", Value: "1234"},
				{Key: "temperature_C", Value: "21.5"},
				{Key: "humidity", Value: "48"},
			},
		})
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/ISM/List")

		m = sendKey(m, "enter")
		if m.SDR.ISMMode != sdrISMDetail {
			t.Fatalf("[%s] expected sdrISMDetail", sz.name)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/ISM/Detail")

		// rtl_tcp server tab: setup and running
		m = sendKey(m, "esc") // detail -> list
		m = sendKey(m, "esc") // stop the decoder
		nextModel, _ := m.Update(sdrISMDoneMsg{seq: 1, err: nil})
		m = nextModel.(AppModel)
		m = sendKey(m, "esc") // list -> setup
		m = sendKey(m, "esc") // setup -> tabs
		m = sendKey(m, "right")
		if m.ActiveTabs[ViewSDR] != int(SDRTabServer) {
			t.Fatalf("[%s] expected SDRTabServer, got %d", sz.name, m.ActiveTabs[ViewSDR])
		}
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Srv/Setup")
		m.SDR.SrvSeq = 1
		m.SDR.SrvActive = true
		m.SDR.SrvStarted = time.Now()
		m.SDR.AppendSrvLine(1, "client 1 connected")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Srv/Running")
		m.SDR.SrvActive = false
	}
}

func TestSDRBookmarksAndTuneFromSpec(t *testing.T) {
	m := newTestSDRApp(t, 53, 14)
	m = sendKey(m, "enter") // FocusContent on Radio

	// Edit frequency to 162.55
	m = sendKey(m, "enter")
	m.SDR.Input = "162.55"
	m = sendKey(m, "enter")
	if m.SDR.RadioFreqHz != 162_550_000 {
		t.Fatalf("RadioFreqHz = %d, want 162550000", m.SDR.RadioFreqHz)
	}

	// Save bookmark via Save field
	for i := 0; i < 10; i++ {
		f := m.SDR.radioFields()
		if f[m.SDR.RadioCursor] == sdrFieldSave {
			break
		}
		m = sendKey(m, "down")
	}
	m = sendKey(m, "enter")
	if m.SDR.RadioMode != sdrRadioSaveName {
		t.Fatalf("expected sdrRadioSaveName")
	}
	m.SDR.Input = "My WX"
	m = sendKey(m, "enter")
	if len(m.SDR.Bookmarks) != 1 || m.SDR.Bookmarks[0].Name != "My WX" {
		t.Fatalf("bookmark not saved: %+v", m.SDR.Bookmarks)
	}

	// Delete bookmark from preset list
	m = sendKey(m, "p")
	m.SDR.PresetCursor = 0
	m = sendKey(m, "x")
	if len(m.SDR.Bookmarks) != 0 {
		t.Fatalf("expected bookmark deleted, got %+v", m.SDR.Bookmarks)
	}
	m = sendKey(m, "esc")
	m = sendKey(m, "esc") // back to FocusTabs

	// Switch to Spec, populate sweep frame, tune Radio via Enter
	m = sendKey(m, "right")
	m = sendKey(m, "enter")
	m.SDR.SpecPresetIdx = 2 // Airband (AM)
	m.SDR.cycleSpecPreset(0)
	bins := []float64{-60, -55, -15, -58}
	m.SDR.SpecSeq = 2
	m.SDR.SpecHasFrame = true
	m.SDR.SpecActive = true
	m.SDR.ApplySpecFrame(2, sdr.BuildSpectrumFrame(118_000_000, 137_000_000, bins))
	m = sendKey(m, "enter")

	if m.ActiveTabs[ViewSDR] != int(SDRTabRadio) {
		t.Fatalf("expected switch back to Radio tab on Spec tune")
	}
	if m.SDR.RadioMod != sdr.ModeAM {
		t.Fatalf("expected ModeAM for Airband sweep, got %v", m.SDR.RadioMod)
	}
	if m.SDR.RadioFreqHz <= 118_000_000 || m.SDR.RadioFreqHz >= 137_000_000 {
		t.Fatalf("unexpected tuned frequency: %d", m.SDR.RadioFreqHz)
	}
	_ = bins
}

// TestSDRCustomSweepRange pins the user-editable start/end sweep range and the
// Bars / Top-50 view switch.
func TestSDRCustomSweepRange(t *testing.T) {
	m := newTestSDRApp(t, 53, 14)
	m.ActiveView = ViewSDR
	m.ActiveTabs[ViewSDR] = int(SDRTabSpec)
	m.SDR.Tab = SDRTabSpec
	m.Focus = FocusContent

	if m.SDR.SpecStartHz != 87_500_000 || m.SDR.SpecEndHz != 108_000_000 {
		t.Fatalf("unexpected default range %d-%d", m.SDR.SpecStartHz, m.SDR.SpecEndHz)
	}

	// End must be above start: set the end first, then move the start up.
	m.SDR.SpecCursor = int(sdrSpecFEnd)
	m = sendKey(m, "enter")
	m.SDR.Input = "440"
	m = sendKey(m, "enter")
	if m.SDR.SpecEndHz != 440_000_000 {
		t.Fatalf("end = %d, want 440000000", m.SDR.SpecEndHz)
	}

	m.SDR.SpecCursor = int(sdrSpecFStart)
	m = sendKey(m, "enter")
	if m.SDR.SpecEdit != sdrSpecEditStart {
		t.Fatalf("expected sdrSpecEditStart, got %v", m.SDR.SpecEdit)
	}
	assertFitsBounds(t, m.View(), 53, 14, "Spec/EditStart")
	m.SDR.Input = "430"
	m = sendKey(m, "enter")
	if m.SDR.SpecStartHz != 430_000_000 {
		t.Fatalf("start = %d, want 430000000", m.SDR.SpecStartHz)
	}
	// A start at or above the end must be rejected.
	m = sendKey(m, "enter")
	m.SDR.Input = "500"
	m = sendKey(m, "enter")
	if m.SDR.SpecEdit != sdrSpecEditStart || m.SDR.InputErr == "" {
		t.Fatalf("expected start edit rejected, edit=%v err=%q", m.SDR.SpecEdit, m.SDR.InputErr)
	}
	m = sendKey(m, "esc")

	if m.SDR.ExplicitPreset() {
		t.Fatalf("hand-edited range should be Custom, preset=%d", m.SDR.SpecPresetIdx)
	}
	assertFitsBounds(t, m.View(), 53, 14, "Spec/CustomConfig")

	// View toggle and a live frame in both presentations.
	if m.SDR.SpecView != sdrSpecViewChart {
		t.Fatalf("expected chart view by default")
	}
	m = sendKey(m, "m")
	if m.SDR.SpecView != sdrSpecViewList {
		t.Fatalf("expected list view after m")
	}
	m = sendKey(m, "m")

	hi := make([]float64, 200)
	for i := range hi {
		hi[i] = -70
	}
	hi[40] = -20
	hi[120] = -35
	m.SDR.SpecSeq = 3
	m.SDR.SpecHasFrame = true
	m.SDR.ApplySpecFrame(3, sdr.BuildSpectrumFrame(430_000_000, 440_000_000, hi))
	assertFitsBounds(t, m.View(), 53, 14, "Spec/Chart")

	m = sendKey(m, "m")
	if m.SDR.SpecView != sdrSpecViewList {
		t.Fatalf("expected list view")
	}
	if len(m.SDR.SpecFrame.TopSignals) == 0 {
		t.Fatalf("expected top signals in frame")
	}
	assertFitsBounds(t, m.View(), 53, 14, "Spec/TopList")
	m = sendKey(m, "down")
	assertFitsBounds(t, m.View(), 53, 14, "Spec/TopList/Next")

	// Tuning from the ranked list uses the selected signal frequency.
	want, _ := m.SDR.MarkerFreqHz()
	m = sendKey(m, "enter")
	if m.ActiveTabs[ViewSDR] != int(SDRTabRadio) {
		t.Fatalf("expected Radio tab after list tune")
	}
	if m.SDR.RadioFreqHz != want {
		t.Fatalf("tuned %d, want %d", m.SDR.RadioFreqHz, want)
	}
}

// TestSDRBandwidthAcrossModes verifies every demod mode exposes a settable bandwidth.
func TestSDRBandwidthAcrossModes(t *testing.T) {
	m := newTestSDRApp(t, 53, 14)
	m.ActiveView = ViewSDR
	m.ActiveTabs[ViewSDR] = int(SDRTabRadio)
	m.SDR.Tab = SDRTabRadio
	m.Focus = FocusContent

	for _, mode := range []sdr.DemodMode{sdr.ModeWBFM, sdr.ModeNFM, sdr.ModeAM, sdr.ModeUSB, sdr.ModeLSB} {
		m.SDR.RadioMod = mode
		m.SDR.RadioBwHz = sdr.DefaultBandwidth(mode)
		before := m.SDR.RadioBwHz
		m.SDR.RadioCursor = 0
		for i := 0; i < len(m.SDR.radioFields()); i++ {
			if m.SDR.radioFields()[i] == sdrFieldBW {
				m.SDR.RadioCursor = i
				break
			}
		}
		m = sendKey(m, "right")
		if m.SDR.RadioBwHz == before {
			t.Fatalf("mode %v: bandwidth did not change from %d", mode, before)
		}
		if got := sdr.NormalizeBandwidth(mode, m.SDR.RadioBwHz); got != m.SDR.RadioBwHz {
			t.Fatalf("mode %v: bandwidth %d not in mode's option set", mode, m.SDR.RadioBwHz)
		}
		assertStr := m.View()
		if !strings.Contains(assertStr, "BW") {
			t.Fatalf("mode %v: BW row missing in:\n%s", mode, assertStr)
		}
	}
}

// TestSDRServerTab covers the rtl_tcp subtab: settings, edits, and running view.
func TestSDRServerTab(t *testing.T) {
	m := newTestSDRApp(t, 53, 14)
	m.ActiveView = ViewSDR
	m.ActiveTabs[ViewSDR] = int(SDRTabServer)
	m.SDR.Tab = SDRTabServer
	m.Focus = FocusContent

	assertFitsBounds(t, m.View(), 53, 14, "Srv/Setup")

	// Port editor accepts a valid port and rejects garbage.
	m.SDR.SrvCursor = int(sdrSrvPort)
	m = sendKey(m, "enter")
	if m.SDR.SrvEdit != sdrSrvEditPort {
		t.Fatalf("expected port editor, got %v", m.SDR.SrvEdit)
	}
	assertFitsBounds(t, m.View(), 53, 14, "Srv/EditPort")
	m.SDR.Input = "5555"
	m = sendKey(m, "enter")
	if m.SDR.SrvPort != 5555 {
		t.Fatalf("port = %d, want 5555", m.SDR.SrvPort)
	}

	// Start frequency editor.
	m.SDR.SrvCursor = int(sdrSrvFreq)
	m = sendKey(m, "enter")
	m.SDR.Input = "433.92"
	m = sendKey(m, "enter")
	if m.SDR.SrvFreqHz != 433_920_000 {
		t.Fatalf("srv freq = %d, want 433920000", m.SDR.SrvFreqHz)
	}

	// Bind + rate adjust.
	m.SDR.SrvCursor = int(sdrSrvBind)
	m = sendKey(m, "right")
	if m.SDR.SrvBind() != "127.0.0.1" {
		t.Fatalf("bind = %q, want 127.0.0.1", m.SDR.SrvBind())
	}
	rate := m.SDR.SrvRate()
	m.SDR.SrvCursor = int(sdrSrvRate)
	m = sendKey(m, "right")
	if m.SDR.SrvRate() == rate {
		t.Fatalf("sample rate did not change")
	}

	// Simulated running state with log lines.
	m.SDR.SrvSeq = 1
	m.SDR.SrvActive = true
	m.SDR.SrvStarted = time.Now()
	m.SDR.AppendSrvLine(1, "client 1 connected")
	m.SDR.AppendSrvLine(1, "listening on 0.0.0.0:5555")
	assertFitsBounds(t, m.View(), 53, 14, "Srv/Running")
	if m.SDR.SrvClients != 1 {
		t.Fatalf("clients = %d, want 1", m.SDR.SrvClients)
	}

	// Esc stops the server and returns to setup.
	m = sendKey(m, "esc")
	nextModel, _ := m.Update(sdrSrvDoneMsg{seq: 1, err: nil})
	m = nextModel.(AppModel)
	if m.SDR.SrvActive {
		t.Fatalf("expected server stopped")
	}
}

// TestSDRSpectrumZoom pins the zoom feature: the window shrinks around the
// cursor, stays inside the configured range, and never inverts.
func TestSDRSpectrumZoom(t *testing.T) {
	m := newTestSDRApp(t, 53, 14)
	m.ActiveView = ViewSDR
	m.ActiveTabs[ViewSDR] = int(SDRTabSpec)
	m.SDR.Tab = SDRTabSpec
	m.Focus = FocusContent

	m.SDR.SpecStartHz = 87_500_000
	m.SDR.SpecEndHz = 108_000_000

	if got := m.SDR.ZoomedSpanHz(); got != 20_500_000 {
		t.Fatalf("zoom 0 span = %d, want 20500000", got)
	}

	// Zoom in twice from the config screen: 20.5M -> 5.125M, centred on the range.
	m.SDR.SpecCursor = int(sdrSpecFZoom)
	m = sendKey(m, "right")
	m = sendKey(m, "right")
	if m.SDR.SpecZoom != 2 {
		t.Fatalf("zoom = %d, want 2", m.SDR.SpecZoom)
	}
	lo, hi := m.SDR.EffectiveSweepRange()
	if span := hi - lo; span != 20_500_000/4 {
		t.Fatalf("zoomed span = %d, want %d", span, 20_500_000/4)
	}
	if lo < m.SDR.SpecStartHz || hi > m.SDR.SpecEndHz {
		t.Fatalf("zoomed window %d-%d escaped %d-%d", lo, hi, m.SDR.SpecStartHz, m.SDR.SpecEndHz)
	}
	if mid := (lo + hi) / 2; mid < 97_000_000 || mid > 98_000_000 {
		t.Fatalf("zoomed centre = %d, want near 97.75M", mid)
	}
	assertFitsBounds(t, m.View(), 53, 14, "Spec/ZoomConfig")

	// The zoom row and header must show the factor.
	if !strings.Contains(m.View(), "4x") {
		t.Fatalf("expected 4x in zoom row:\n%s", m.View())
	}

	// Zooming out returns to the full range and does not go negative.
	for i := 0; i < 6; i++ {
		m = sendKey(m, "left")
	}
	if m.SDR.SpecZoom != 0 {
		t.Fatalf("zoom = %d, want 0 after zooming out", m.SDR.SpecZoom)
	}

	// Zoom anchored on the marker: with a live sweep, zoom uses the peak.
	m.SDR.SpecSeq = 4
	m.SDR.SpecActive = true
	m.SDR.SpecHasFrame = true
	hiBins := make([]float64, 128)
	for i := range hiBins {
		hiBins[i] = -70
	}
	hiBins[100] = -10 // near the top of the band
	m.SDR.ApplySpecFrame(4, sdr.BuildSpectrumFrame(87_500_000, 108_000_000, hiBins))
	peak := m.SDR.SpecFrame.BinFreqHz(100)
	m.SDR.SpecCursorHz = peak

	m = sendKey(m, "z")
	if m.SDR.SpecZoom != 1 {
		t.Fatalf("z: zoom = %d, want 1", m.SDR.SpecZoom)
	}
	lo, hi = m.SDR.EffectiveSweepRange()
	if peak < lo || peak > hi {
		t.Fatalf("zoomed window %d-%d does not contain marker %d", lo, hi, peak)
	}
	if span := hi - lo; span != 20_500_000/2 {
		t.Fatalf("zoomed span = %d, want %d", span, 20_500_000/2)
	}
	assertFitsBounds(t, m.View(), 53, 14, "Spec/ZoomLive")
}

// TestSDRContrastAutoscale verifies the contrast stretch lifts the strongest
// signal to full height and pushes weak signals down further at higher levels.
func TestSDRContrastAutoscale(t *testing.T) {
	bins := []float64{-72, -70, -69, -68, -66, -60, -12}
	lo, hi, g := contrastWindow(bins, 0)
	if lo != -70 || hi != -5 || g != 1 {
		t.Fatalf("level 0 window = %v/%v/%v, want -70/-5/1", lo, hi, g)
	}

	norm := func(level int, db float64) float64 {
		wl, wh, wg := contrastWindow(bins, level)
		v := (db - wl) / (wh - wl)
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		return math.Pow(v, wg)
	}

	for level := 1; level <= len(sdrContrastNames)-1; level++ {
		strong := norm(level, -12)
		weak := norm(level, -68)
		if strong < 0.9 {
			t.Fatalf("level %d: strongest maps to %.2f, want ~1.0", level, strong)
		}
		if weak > 0.35 {
			t.Fatalf("level %d: weak signal maps to %.2f, want near 0", level, weak)
		}
		if weak >= norm(level, -60) {
			t.Fatalf("level %d: ordering broken (weak >= mid)", level)
		}
	}
	// A higher contrast level must push the same weak signal lower.
	if norm(4, -66) >= norm(1, -66) {
		t.Fatalf("max contrast did not lower the noise floor")
	}

	// Cycling wraps and is reflected in the label.
	m := newTestSDRApp(t, 40, 10)
	m.ActiveView = ViewSDR
	m.ActiveTabs[ViewSDR] = int(SDRTabSpec)
	m.SDR.Tab = SDRTabSpec
	m.Focus = FocusContent
	for i := 1; i < len(sdrContrastNames); i++ {
		m = sendKey(m, "c")
		if m.SDR.SpecContrast != i {
			t.Fatalf("contrast = %d, want %d", m.SDR.SpecContrast, i)
		}
	}
	if m.SDR.ContrastName() != "Max" {
		t.Fatalf("contrast name = %q, want Max", m.SDR.ContrastName())
	}
	m = sendKey(m, "c")
	if m.SDR.SpecContrast != 0 {
		t.Fatalf("contrast did not wrap to 0: %d", m.SDR.SpecContrast)
	}
	assertFitsBounds(t, m.View(), 40, 10, "Spec/Contrast")
}

// TestSDRSpectrumCursorMovesImmediately is the "selector only updates when the
// spectrum updates" fix: the cursor is a frequency in the model, so every key
// press moves it right away - no waiting for the next 0.5s frame (and it works
// even before the first frame arrives).
func TestSDRSpectrumCursorMovesImmediately(t *testing.T) {
	m := newTestSDRApp(t, 53, 14)
	m.ActiveView = ViewSDR
	m.ActiveTabs[ViewSDR] = int(SDRTabSpec)
	m.SDR.Tab = SDRTabSpec
	m.Focus = FocusContent

	// Before any frame: the cursor must still move.
	m.SDR.SpecSeq = 1
	m.SDR.SpecActive = true
	startHz := m.SDR.SpecCursorHz
	m = sendKey(m, "right")
	m = sendKey(m, "right")
	if m.SDR.SpecCursorHz <= startHz {
		t.Fatalf("cursor did not move before the first frame: %d -> %d", startHz, m.SDR.SpecCursorHz)
	}
	noFrameHz := m.SDR.SpecCursorHz

	// With a frame, the rendered marker column tracks the cursor immediately.
	bins := make([]float64, 44)
	for i := range bins {
		bins[i] = -55
	}
	bins[20] = -15
	m.SDR.ApplySpecFrame(1, sdr.BuildSpectrumFrame(98_000_000, 99_000_000, bins))
	m.SDR.SpecCursorHz = 98_100_000
	before := markerColumn(m.View())
	beforeHz := m.SDR.SpecCursorHz

	for i := 0; i < 4; i++ {
		m = sendKey(m, "right")
	}
	if m.SDR.SpecCursorHz <= beforeHz {
		t.Fatalf("cursor did not advance: %d -> %d", beforeHz, m.SDR.SpecCursorHz)
	}
	after := markerColumn(m.View())
	if after <= before {
		t.Fatalf("marker column did not move right: %d -> %d\n%s", before, after, m.View())
	}
	// Jump keys move further per press.
	m.SDR.SpecCursorHz = 98_500_000
	upHz := m.SDR.SpecCursorHz
	m = sendKey(m, "down")
	if m.SDR.SpecCursorHz <= upHz+int64(m.SDR.specBinWidthHz()) {
		t.Fatalf("jump did not move ~5 bins: %d -> %d", upHz, m.SDR.SpecCursorHz)
	}
	// The cursor never leaves the displayed window.
	m.SDR.SpecCursorHz = 99_000_000
	for i := 0; i < 10; i++ {
		m = sendKey(m, "right")
	}
	if m.SDR.SpecCursorHz > m.SDR.SpecFrame.EndHz {
		t.Fatalf("cursor escaped the window: %d > %d", m.SDR.SpecCursorHz, m.SDR.SpecFrame.EndHz)
	}
	_ = noFrameHz
}

// markerColumn returns the 0-based column of the "▲" cursor row, or -1.
func markerColumn(view string) int {
	for _, line := range strings.Split(view, "\n") {
		if idx := strings.Index(line, "▲"); idx >= 0 {
			// Strip the gutter ("│") and sidebar so the column is comparable.
			return idx
		}
	}
	return -1
}

// TestSDRRadioRunningHasSquelchNotMute pins the requested control swap.
func TestSDRRadioRunningHasSquelchNotMute(t *testing.T) {
	m := newTestSDRApp(t, 40, 10)
	m.ActiveView = ViewSDR
	m.ActiveTabs[ViewSDR] = int(SDRTabRadio)
	m.SDR.Tab = SDRTabRadio
	m.SDR.RadioMode = sdrRadioRunning
	m.SDR.RadioActive = true
	m.Focus = FocusContent

	fields := m.SDR.runFields()
	hasSquelch := false
	for _, f := range fields {
		if f == sdrRunSquelch {
			hasSquelch = true
		}
	}
	if !hasSquelch {
		t.Fatalf("running radio fields have no squelch: %v", fields)
	}
	view := m.View()
	if !strings.Contains(view, "Squelch") {
		t.Fatalf("squelch row missing:\n%s", view)
	}
	if strings.Contains(view, "Mute") || strings.Contains(view, "MUTED") {
		t.Fatalf("mute row still present:\n%s", view)
	}

	// Squelch is adjustable while running and survives the restart.
	m.SDR.RunCursor = int(sdrRunSquelch)
	before := m.SDR.RadioSquelch
	m = sendKey(m, "right")
	if m.SDR.RadioSquelch <= before {
		t.Fatalf("squelch did not increase: %d -> %d", before, m.SDR.RadioSquelch)
	}
	if got := m.SDR.RadioSquelch; got > 100 {
		t.Fatalf("squelch out of range: %d", got)
	}
	assertFitsBounds(t, m.View(), 40, 10, "Radio/Squelch")
}

// TestSDRSpecLiveRepaintTick pins the 10fps refresh that keeps the cursor in
// step with the keyboard, independent of the 0.5s sweep frames.
func TestSDRSpecLiveRepaintTick(t *testing.T) {
	m := newTestSDRApp(t, 53, 14)
	m.ActiveView = ViewSDR
	m.ActiveTabs[ViewSDR] = int(SDRTabSpec)
	m.SDR.Tab = SDRTabSpec
	m.Focus = FocusContent

	// Not sweeping: the tick stops.
	next, cmd := m.Update(sdrRepaintMsg{})
	if cmd != nil {
		t.Fatalf("repaint tick should stop when nothing is shown")
	}
	m = next.(AppModel)

	// Sweeping: the tick keeps running so the cursor refreshes at ~10fps.
	m.SDR.SpecSeq = 1
	m.SDR.SpecActive = true
	m.SDR.SpecStarted = time.Now().Add(-2 * time.Second)
	next, cmd = m.Update(sdrRepaintMsg{})
	if cmd == nil {
		t.Fatalf("repaint tick should continue while sweeping")
	}
	m = next.(AppModel)
	if m.SDR.SpecElapsed < 1500*time.Millisecond {
		t.Fatalf("elapsed not refreshed: %v", m.SDR.SpecElapsed)
	}

	// Switching away from the Spec tab stops it.
	m.ActiveTabs[ViewSDR] = int(SDRTabRadio)
	if _, cmd := m.Update(sdrRepaintMsg{}); cmd != nil {
		t.Fatalf("repaint tick should stop off the Spec tab")
	}
}

// TestSDRRunningGainSelector checks the gain row exists and adjusts while playing.
func TestSDRRunningGainSelector(t *testing.T) {
	m := newTestSDRApp(t, 40, 10)
	m.ActiveView = ViewSDR
	m.ActiveTabs[ViewSDR] = int(SDRTabRadio)
	m.SDR.Tab = SDRTabRadio
	m.SDR.RadioMode = sdrRadioRunning
	m.SDR.RadioActive = true
	m.Focus = FocusContent

	if !strings.Contains(m.View(), "Gain") {
		t.Fatalf("gain row missing while running:\n%s", m.View())
	}
	m.SDR.RunCursor = int(sdrRunGain)
	before := m.SDR.CurrentGainDB()
	m = sendKey(m, "right")
	if m.SDR.CurrentGainDB() == before {
		t.Fatalf("gain did not change from %d", before)
	}
	m.SDR.RunCursor = int(sdrRunGain)
	m = sendKey(m, "left")
	if m.SDR.CurrentGainDB() != before {
		t.Fatalf("gain did not return to %d (got %d)", before, m.SDR.CurrentGainDB())
	}
	m.SDR.RadioActive = false
	m.SDR.RadioMode = sdrRadioSetup
}

// TestSDRNoDongleAndDVBStatus runs the probe status paths.
func TestSDRNoDongleAndDVBStatus(t *testing.T) {
	m := newTestSDRApp(t, 40, 10)
	m.SDR.SetProbeResult(sdr.ProbeStatus{})
	outMissingTools := m.View()
	if !strings.Contains(outMissingTools, "rtl-sdr not installed") {
		t.Fatalf("expected rtl-sdr not installed status in:\n%s", outMissingTools)
	}
	assertFitsBounds(t, outMissingTools, 40, 10, "MissingTools")

	m.SDR.SetProbeResult(sdr.ProbeStatus{
		Tools: sdr.Tools{RTLFM: "/usr/bin/rtl_fm"},
	})
	out := m.View()
	if !strings.Contains(out, "No RTL-SDR") {
		t.Fatalf("expected No RTL-SDR status in:\n%s", out)
	}
	assertFitsBounds(t, out, 40, 10, "NoDongle")

	m.SDR.SetProbeResult(sdr.ProbeStatus{
		Devices: []sdr.USBDevice{{
			Index:     0,
			VendorID:  "0bda",
			ProductID: "2838",
			Product:   "RTL2838UHIDIR",
			DVBDriver: true,
		}},
		Tools: sdr.Tools{RTLFM: "/usr/bin/rtl_fm"},
	})
	out2 := m.View()
	if !strings.Contains(out2, "DVB busy") {
		t.Fatalf("expected DVB busy warning in:\n%s", out2)
	}
	assertFitsBounds(t, out2, 40, 10, "DVBBusy")
}
