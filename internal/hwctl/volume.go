package hwctl

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type VolumeState struct {
	Level    int // 0..20
	MaxLevel int // 20
	Percent  int // 0..100
	Muted    bool
	Backend  string // "wpctl", "pactl", "amixer", or "soft"
}

type cmdRunner func(ctx context.Context, name string, args ...string) (string, error)
type binCheck func(name string) bool

// VolumeController manages 20-level (5% step) system audio volume and mute
// state across PipeWire (wpctl), PulseAudio (pactl), ALSA (amixer, including
// Cardputer Zero's ES8389 DACL/DACR Playback controls), or a software fallback.
type VolumeController struct {
	mu       sync.Mutex
	inited   bool
	level    int
	muted    bool
	backend  string
	controls []string // ALSA mixer control names
	hasSw    bool     // whether ALSA control has hardware playback switch

	runCmd cmdRunner
	hasBin binCheck
}

func NewVolumeController() *VolumeController {
	return &VolumeController{
		level:  10, // 50% default until probed
		runCmd: execCmd,
		hasBin: defaultHasBin,
	}
}

func defaultHasBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func execCmd(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

func (vc *VolumeController) ensureInit() {
	if vc.inited {
		return
	}
	vc.inited = true
	vc.detectLocked()
}

func (vc *VolumeController) State() VolumeState {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.ensureInit()
	return vc.snapshotLocked()
}

func (vc *VolumeController) snapshotLocked() VolumeState {
	return VolumeState{
		Level:    vc.level,
		MaxLevel: VolumeMaxLevel,
		Percent:  vc.level * 5,
		Muted:    vc.muted,
		Backend:  vc.backend,
	}
}

// Step changes the volume by delta levels (out of 20) and returns the new state.
func (vc *VolumeController) Step(delta int) VolumeState {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.ensureInit()

	// Refresh current level from external mixer if possible before stepping.
	vc.refreshLocked()

	if delta > 0 && vc.muted {
		vc.muted = false
	}
	next := vc.level + delta
	if next < 0 {
		next = 0
	}
	if next > VolumeMaxLevel {
		next = VolumeMaxLevel
	}
	vc.level = next
	vc.applyLocked()
	return vc.snapshotLocked()
}

// ToggleMute toggles mute state and returns the new state.
func (vc *VolumeController) ToggleMute() VolumeState {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.ensureInit()
	vc.refreshLocked()

	vc.muted = !vc.muted
	vc.applyMuteLocked()
	return vc.snapshotLocked()
}

func (vc *VolumeController) run(name string, args ...string) (string, error) {
	if vc.runCmd == nil {
		return "", fmt.Errorf("no command runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	return vc.runCmd(ctx, name, args...)
}

func (vc *VolumeController) detectLocked() {
	if vc.hasBin != nil && vc.hasBin("wpctl") {
		if pct, muted, ok := vc.readWpctl(); ok {
			vc.backend = "wpctl"
			vc.level = percentToVolumeLevel(pct)
			vc.muted = muted
			return
		}
	}
	if vc.hasBin != nil && vc.hasBin("pactl") {
		if pct, muted, ok := vc.readPactl(); ok {
			vc.backend = "pactl"
			vc.level = percentToVolumeLevel(pct)
			vc.muted = muted
			return
		}
	}
	if vc.hasBin != nil && vc.hasBin("amixer") {
		if ctrls, pct, muted, hasSw, ok := vc.detectAmixer(); ok {
			vc.backend = "amixer"
			vc.controls = ctrls
			vc.hasSw = hasSw
			vc.level = percentToVolumeLevel(pct)
			vc.muted = muted
			return
		}
	}
	vc.backend = "soft"
}

func (vc *VolumeController) refreshLocked() {
	switch vc.backend {
	case "wpctl":
		if pct, muted, ok := vc.readWpctl(); ok {
			vc.level = percentToVolumeLevel(pct)
			vc.muted = muted
		}
	case "pactl":
		if pct, muted, ok := vc.readPactl(); ok {
			vc.level = percentToVolumeLevel(pct)
			vc.muted = muted
		}
	case "amixer":
		if len(vc.controls) > 0 {
			if out, err := vc.run("amixer", "sget", vc.controls[0]); err == nil {
				if pct, muted, _, ok := parseAmixerSget(out); ok {
					// When software-muting (no hardware switch), keep remembered
					// non-zero level while muted so unmute restores cleanly.
					if !vc.hasSw && vc.muted && pct == 0 {
						return
					}
					vc.level = percentToVolumeLevel(pct)
					if vc.hasSw {
						vc.muted = muted
					}
				}
			}
		}
	}
}

func (vc *VolumeController) applyLocked() {
	pct := vc.level * 5
	switch vc.backend {
	case "wpctl":
		if !vc.muted {
			_, _ = vc.run("wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", "0")
		}
		_, _ = vc.run("wpctl", "set-volume", "@DEFAULT_AUDIO_SINK@", fmt.Sprintf("%.2f", float64(pct)/100.0))
	case "pactl":
		if !vc.muted {
			_, _ = vc.run("pactl", "set-sink-mute", "@DEFAULT_SINK@", "0")
		}
		_, _ = vc.run("pactl", "set-sink-volume", "@DEFAULT_SINK@", fmt.Sprintf("%d%%", pct))
	case "amixer":
		targetPct := pct
		if !vc.hasSw && vc.muted {
			targetPct = 0
		}
		for _, ctrl := range vc.controls {
			if vc.hasSw && !vc.muted {
				_, _ = vc.run("amixer", "-q", "sset", ctrl, fmt.Sprintf("%d%%", targetPct), "unmute")
			} else {
				_, _ = vc.run("amixer", "-q", "sset", ctrl, fmt.Sprintf("%d%%", targetPct))
			}
		}
	}
}

func (vc *VolumeController) applyMuteLocked() {
	switch vc.backend {
	case "wpctl":
		val := "0"
		if vc.muted {
			val = "1"
		}
		_, _ = vc.run("wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", val)
	case "pactl":
		val := "0"
		if vc.muted {
			val = "1"
		}
		_, _ = vc.run("pactl", "set-sink-mute", "@DEFAULT_SINK@", val)
	case "amixer":
		if vc.hasSw {
			sw := "unmute"
			if vc.muted {
				sw = "mute"
			}
			for _, ctrl := range vc.controls {
				_, _ = vc.run("amixer", "-q", "sset", ctrl, sw)
			}
			return
		}
		// Software mute for codecs without a playback mute switch (e.g. ES8389 DACL/DACR).
		targetPct := vc.level * 5
		if vc.muted {
			targetPct = 0
		}
		for _, ctrl := range vc.controls {
			_, _ = vc.run("amixer", "-q", "sset", ctrl, fmt.Sprintf("%d%%", targetPct))
		}
	}
}

var (
	reWpctlVol   = regexp.MustCompile(`Volume:\s*([0-9]+(?:\.[0-9]+)?)`)
	rePct        = regexp.MustCompile(`(\d+)%`)
	reAmixerCtrl = regexp.MustCompile(`Simple mixer control '([^']+)'`)
)

func (vc *VolumeController) readWpctl() (pct int, muted bool, ok bool) {
	out, err := vc.run("wpctl", "get-volume", "@DEFAULT_AUDIO_SINK@")
	if err != nil {
		return 0, false, false
	}
	m := reWpctlVol.FindStringSubmatch(out)
	if len(m) != 2 {
		return 0, false, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false, false
	}
	pct = int(f*100 + 0.5)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	muted = strings.Contains(strings.ToUpper(out), "MUTED")
	return pct, muted, true
}

func (vc *VolumeController) readPactl() (pct int, muted bool, ok bool) {
	out, err := vc.run("pactl", "get-sink-volume", "@DEFAULT_SINK@")
	if err != nil {
		return 0, false, false
	}
	m := rePct.FindStringSubmatch(out)
	if len(m) != 2 {
		return 0, false, false
	}
	pct, err = strconv.Atoi(m[1])
	if err != nil {
		return 0, false, false
	}
	if pct > 100 {
		pct = 100
	}
	if mOut, mErr := vc.run("pactl", "get-sink-mute", "@DEFAULT_SINK@"); mErr == nil {
		muted = strings.Contains(strings.ToLower(mOut), "yes")
	}
	return pct, muted, true
}

func (vc *VolumeController) detectAmixer() (ctrls []string, pct int, muted bool, hasSw bool, ok bool) {
	// Prefer standard mixer controls first, then Cardputer Zero ES8389 DACL/DACR pair.
	candidates := [][]string{
		{"Master"},
		{"PCM"},
		{"Speaker"},
		{"Headphone"},
		{"DACL Playback", "DACR Playback"},
		{"Playback"},
	}
	for _, group := range candidates {
		out, err := vc.run("amixer", "sget", group[0])
		if err != nil {
			continue
		}
		p, m, sw, parsed := parseAmixerSget(out)
		if !parsed {
			continue
		}
		var active []string
		active = append(active, group[0])
		for _, extra := range group[1:] {
			if _, err := vc.run("amixer", "sget", extra); err == nil {
				active = append(active, extra)
			}
		}
		return active, p, m, sw, true
	}

	// Fallback: inspect `amixer scontrols` for any playback volume control.
	listOut, err := vc.run("amixer", "scontrols")
	if err != nil {
		return nil, 0, false, false, false
	}
	for _, match := range reAmixerCtrl.FindAllStringSubmatch(listOut, -1) {
		name := match[1]
		out, err := vc.run("amixer", "sget", name)
		if err != nil || !strings.Contains(out, "Playback") {
			continue
		}
		if p, m, sw, parsed := parseAmixerSget(out); parsed {
			return []string{name}, p, m, sw, true
		}
	}
	return nil, 0, false, false, false
}

func parseAmixerSget(out string) (pct int, muted bool, hasSwitch bool, ok bool) {
	if strings.Contains(out, "pswitch") {
		hasSwitch = true
	}
	if m := rePct.FindStringSubmatch(out); len(m) == 2 {
		v, err := strconv.Atoi(m[1])
		if err == nil {
			pct = v
			ok = true
		}
	}
	if strings.Contains(out, "[off]") {
		hasSwitch = true
		muted = true
	} else if strings.Contains(out, "[on]") {
		hasSwitch = true
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, muted, hasSwitch, ok
}

func percentToVolumeLevel(pct int) int {
	if pct <= 0 {
		return 0
	}
	if pct >= 100 {
		return VolumeMaxLevel
	}
	lvl := (pct + 2) / 5 // round to nearest 5% step across 0..20
	if lvl < 0 {
		lvl = 0
	}
	if lvl > VolumeMaxLevel {
		lvl = VolumeMaxLevel
	}
	return lvl
}
