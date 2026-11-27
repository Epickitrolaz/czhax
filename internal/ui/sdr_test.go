package ui

import (
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

		// Spec tab: idle, sweep idle, and populated FFT frame
		m = sendKey(m, "esc") // back to FocusTabs
		m = sendKey(m, "right")
		if m.ActiveTabs[ViewSDR] != int(SDRTabSpec) {
			t.Fatalf("[%s] expected SDRTabSpec, got %d", sz.name, m.ActiveTabs[ViewSDR])
		}
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Spec/IdleLive")

		m = sendKey(m, "m")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Spec/IdleSweep")
		m = sendKey(m, "m")

		bins := make([]float64, 34)
		for i := range bins {
			bins[i] = -60 + float64(i%7)*5
		}
		bins[17] = -12
		m.SDR.SpecSeq = 1
		m.SDR.ApplySpecFrame(1, sdr.BuildSpectrumFrame(97_500_000, 99_548_000, bins))
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/SDR/Spec/Frame")

		// 433 / ISM tab: setup, packet list, packet detail
		m = sendKey(m, "esc")
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
	m.SDR.SpecKind = sdrSpecSweep
	m.SDR.SpecBandIdx = 1 // Airband (AM)
	bins := []float64{-60, -55, -15, -58}
	m.SDR.SpecSeq = 2
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
}

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
