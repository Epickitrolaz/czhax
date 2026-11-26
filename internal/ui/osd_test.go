package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/hwctl"
)

func TestOSDBrightnessAndVolumePopupsAllSizes(t *testing.T) {
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
		m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.4.1")
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(AppModel)

		// Brightness popup (10 levels).
		next, _ = m.Update(hwEventMsg{
			ev: hwctl.Event{
				Kind: hwctl.EventBrightness,
				Brightness: hwctl.BacklightState{
					Raw:      75,
					MaxRaw:   100,
					Level:    8,
					MaxLevel: 10,
					Percent:  75,
				},
			},
		})
		m = next.(AppModel)
		bView := m.View()
		assertFitsBounds(t, bView, sz.w, sz.h, sz.name+"/OSD/Brightness")
		if !strings.Contains(bView, "Brightness") || !strings.Contains(bView, "8/10") || !strings.Contains(bView, "75%") {
			t.Fatalf("[%s] expected Brightness 8/10 75%% in popup:\n%s", sz.name, bView)
		}
		firstSeq := m.OSD.Seq

		// Volume popup (20 levels) supersedes brightness and advances Seq.
		next, _ = m.Update(hwEventMsg{
			ev: hwctl.Event{
				Kind: hwctl.EventVolume,
				Volume: hwctl.VolumeState{
					Level:    14,
					MaxLevel: 20,
					Percent:  70,
				},
			},
		})
		m = next.(AppModel)
		vView := m.View()
		assertFitsBounds(t, vView, sz.w, sz.h, sz.name+"/OSD/Volume")
		if !strings.Contains(vView, "Volume") || !strings.Contains(vView, "14/20") || !strings.Contains(vView, "70%") {
			t.Fatalf("[%s] expected Volume 14/20 70%% in popup:\n%s", sz.name, vView)
		}

		// Stale hide timer for earlier Seq must NOT hide the newer popup.
		next, _ = m.Update(osdHideMsg{seq: firstSeq})
		m = next.(AppModel)
		if !m.OSD.Visible {
			t.Fatalf("[%s] stale osdHideMsg hid newer popup", sz.name)
		}

		// Muted volume popup (also verify ASCII border mode).
		m.Cfg.Border = config.BorderAscii
		next, _ = m.Update(hwEventMsg{
			ev: hwctl.Event{
				Kind: hwctl.EventVolume,
				Volume: hwctl.VolumeState{
					Level:    14,
					MaxLevel: 20,
					Percent:  70,
					Muted:    true,
				},
			},
		})
		m = next.(AppModel)
		muteView := m.View()
		assertFitsBounds(t, muteView, sz.w, sz.h, sz.name+"/OSD/MutedASCII")
		if !strings.Contains(muteView, "MUTED") || !strings.Contains(muteView, "MUTE") {
			t.Fatalf("[%s] expected MUTED state in popup:\n%s", sz.name, muteView)
		}

		// Matching hide timer dismisses popup.
		next, _ = m.Update(osdHideMsg{seq: m.OSD.Seq})
		m = next.(AppModel)
		if m.OSD.Visible {
			t.Fatalf("[%s] expected popup hidden on matching osdHideMsg", sz.name)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/OSD/Hidden")
	}
}
