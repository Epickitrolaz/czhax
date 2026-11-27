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

// mixerControl is one ALSA playback volume control plus its dB scale (when the
// driver exposes one), so the 20 UI levels can be mapped onto a perceptual
// (logarithmic) taper instead of a raw linear register value.
type mixerControl struct {
	name   string
	rawMax int
	dbMin  float64
	dbStep float64
	hasDB  bool
}

// VolumeController manages 20-level (5% step) system audio volume and mute
// state across PipeWire (wpctl), PulseAudio (pactl), ALSA (amixer, including
// Cardputer Zero's ES8389 DACL/DACR playback controls), or a software fallback.
type VolumeController struct {
	mu       sync.Mutex
	inited   bool
	level    int
	muted    bool
	backend  string
	controls []mixerControl // playback volume controls (may be several, e.g. DACL+DACR)
	swCtrl   string         // hardware playback switch control used for mute (optional)

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
		if ctrls, swCtrl, level, muted, ok := vc.detectAmixer(); ok {
			vc.backend = "amixer"
			vc.controls = ctrls
			vc.swCtrl = swCtrl
			vc.level = level
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
		if len(vc.controls) == 0 {
			return
		}
		ctrl := vc.controls[0]
		out, err := vc.run("amixer", "sget", ctrl.name)
		if err == nil {
			if level, ok := amixerRawLevelFrom(out, ctrl); ok {
				// While software-muting (no hardware switch) the raw value is 0;
				// keep the remembered level so unmute restores cleanly.
				if !(vc.swCtrl == "" && vc.muted && level == 0) {
					vc.level = level
				}
			}
		}
		if vc.swCtrl != "" {
			if out, err := vc.run("amixer", "sget", vc.swCtrl); err == nil {
				if m, ok := amixerSwitchState(out); ok {
					vc.muted = m
				}
			}
		}
	}
}

func (vc *VolumeController) applyLocked() {
	switch vc.backend {
	case "wpctl":
		pct := vc.level * 5
		if !vc.muted {
			_, _ = vc.run("wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", "0")
		}
		_, _ = vc.run("wpctl", "set-volume", "@DEFAULT_AUDIO_SINK@", fmt.Sprintf("%.2f", float64(pct)/100.0))
	case "pactl":
		pct := vc.level * 5
		if !vc.muted {
			_, _ = vc.run("pactl", "set-sink-mute", "@DEFAULT_SINK@", "0")
		}
		_, _ = vc.run("pactl", "set-sink-volume", "@DEFAULT_SINK@", fmt.Sprintf("%d%%", pct))
	case "amixer":
		targetPct := vc.level * 5
		if vc.swCtrl == "" && vc.muted {
			targetPct = 0
		}
		if vc.swCtrl != "" && !vc.muted {
			_, _ = vc.run("amixer", "-q", "sset", vc.swCtrl, "unmute")
		}
		for _, ctrl := range vc.controls {
			arg := fmt.Sprintf("%d%%", targetPct)
			if ctrl.hasDB {
				arg = strconv.Itoa(volumeLevelToRaw(vc.level, ctrl))
			}
			_, _ = vc.run("amixer", "-q", "sset", ctrl.name, arg)
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
		if vc.swCtrl != "" {
			sw := "unmute"
			if vc.muted {
				sw = "mute"
			}
			_, _ = vc.run("amixer", "-q", "sset", vc.swCtrl, sw)
			return
		}
		// Software mute for codecs without a dedicated playback switch.
		targetPct := vc.level * 5
		if vc.muted {
			targetPct = 0
		}
		for _, ctrl := range vc.controls {
			arg := fmt.Sprintf("%d%%", targetPct)
			if ctrl.hasDB {
				raw := vc.level
				if vc.muted {
					raw = 0
				}
				arg = strconv.Itoa(volumeLevelToRaw(raw, ctrl))
			}
			_, _ = vc.run("amixer", "-q", "sset", ctrl.name, arg)
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

// amixerPlaybackNames lists simple mixer control names in driver order.
func (vc *VolumeController) amixerPlaybackNames() []string {
	out, err := vc.run("amixer", "scontrols")
	if err != nil {
		return nil
	}
	var names []string
	for _, m := range reAmixerCtrl.FindAllStringSubmatch(out, -1) {
		names = append(names, m[1])
	}
	return names
}

// detectAmixer picks the real playback volume control(s). JSON-free heuristic:
// only controls that advertise a playback volume capability are eligible, so
// capture-only controls (such as the ES8389 "ADC OSR" or "ADCL") are never used.
func (vc *VolumeController) detectAmixer() (ctrls []mixerControl, swCtrl string, level int, muted bool, ok bool) {
	names := vc.amixerPlaybackNames()
	exists := make(map[string]bool, len(names))
	for _, n := range names {
		exists[n] = true
	}
	dbScales := vc.amixerDBScales()

	candidates := [][]string{
		{"Master"},
		{"PCM"},
		{"Speaker"},
		{"Headphone"},
		// Cardputer Zero ES8389 codec: DACL + DACR are the audible outputs.
		{"DACL", "DACR"},
		{"DACL Playback", "DACR Playback"},
		{"DAC", "DACR"},
		{"Digital"},
		{"Playback"},
	}
	for _, group := range candidates {
		var wav []string
		for _, name := range group {
			if exists[name] {
				wav = append(wav, name)
			}
		}
		if len(wav) == 0 {
			continue
		}
		var got []mixerControl
		for _, name := range wav {
			out, err := vc.run("amixer", "sget", name)
			if err != nil || !isPlaybackVolume(out) {
				continue
			}
			ctrl := mixerControl{name: name, rawMax: 100}
			if rawMax, parseOK := amixerRawMax(out); parseOK {
				ctrl.rawMax = rawMax
			}
			if sc, found := dbScales[normalizeMixerName(name)]; found {
				ctrl.dbMin, ctrl.dbStep, ctrl.hasDB = sc.dbMin, sc.dbStep, sc.dbStep > 0
			}
			got = append(got, ctrl)
		}
		if len(got) == 0 {
			continue
		}
		lvl, hasLvl := amixerRawLevelFrom(mustSget(vc, got[0].name), got[0])
		if !hasLvl {
			lvl = percentToVolumeLevel(mustPct(vc, got[0].name))
		}
		muted = mustMuted(vc, got[0].name)
		sw := vc.detectAmixerSwitch(names)
		if sw != "" {
			if out, err := vc.run("amixer", "sget", sw); err == nil {
				if m, okSw := amixerSwitchState(out); okSw {
					muted = m
				}
			}
		}
		return got, sw, lvl, muted, true
	}

	// Generic fallback: first control with playback volume capability.
	for _, name := range names {
		out, err := vc.run("amixer", "sget", name)
		if err != nil || !isPlaybackVolume(out) {
			continue
		}
		ctrl := mixerControl{name: name, rawMax: 100}
		if rawMax, parseOK := amixerRawMax(out); parseOK {
			ctrl.rawMax = rawMax
		}
		if sc, found := dbScales[normalizeMixerName(name)]; found {
			ctrl.dbMin, ctrl.dbStep, ctrl.hasDB = sc.dbMin, sc.dbStep, sc.dbStep > 0
		}
		lvl, hasLvl := amixerRawLevelFrom(out, ctrl)
		if !hasLvl {
			lvl = percentToVolumeLevel(mustPct(vc, name))
		}
		return []mixerControl{ctrl}, vc.detectAmixerSwitch(names), lvl, mustMuted(vc, name), true
	}
	return nil, "", 0, false, false
}

func mustSget(vc *VolumeController, name string) string {
	out, _ := vc.run("amixer", "sget", name)
	return out
}

func mustPct(vc *VolumeController, name string) int {
	pct, _, _, _ := parseAmixerSget(mustSget(vc, name))
	return pct
}

func mustMuted(vc *VolumeController, name string) bool {
	_, muted, _, _ := parseAmixerSget(mustSget(vc, name))
	return muted
}

// detectAmixerSwitch finds a dedicated playback mute switch, if the card has one.
func (vc *VolumeController) detectAmixerSwitch(names []string) string {
	preferred := []string{"Master", "Speaker", "Headphone", "PCM"}
	for _, want := range preferred {
		for _, name := range names {
			if !strings.EqualFold(name, want) {
				continue
			}
			if out, err := vc.run("amixer", "sget", name); err == nil && isPlaybackSwitch(out) {
				return name
			}
		}
	}
	return ""
}

// amixerDBScales maps normalized control names to their dB scale, parsed from
// `amixer contents` (which `amixer sget` does not print).
func (vc *VolumeController) amixerDBScales() map[string]struct {
	dbMin  float64
	dbStep float64
} {
	out, err := vc.run("amixer", "contents")
	if err != nil {
		return nil
	}
	res := make(map[string]struct {
		dbMin  float64
		dbStep float64
	})
	current := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "numid=") {
			current = ""
			if idx := strings.Index(line, "name='"); idx >= 0 {
				rest := line[idx+6:]
				if end := strings.Index(rest, "'"); end > 0 {
					current = normalizeMixerName(rest[:end])
				}
			}
			continue
		}
		if current == "" || !strings.Contains(line, "dBscale-min=") {
			continue
		}
		idx := strings.Index(line, "dBscale-min=")
		rest := line[idx+len("dBscale-min="):]
		parts := strings.SplitN(rest, ",", 2)
		if len(parts) != 2 {
			continue
		}
		dbMin, err1 := strconv.ParseFloat(strings.TrimSuffix(parts[0], "dB"), 64)
		stepPart := strings.TrimPrefix(strings.SplitN(parts[1], ",", 2)[0], "step=")
		dbStep, err2 := strconv.ParseFloat(strings.TrimSuffix(stepPart, "dB"), 64)
		if err1 != nil || err2 != nil || dbStep <= 0 {
			continue
		}
		res[current] = struct {
			dbMin  float64
			dbStep float64
		}{dbMin: dbMin, dbStep: dbStep}
	}
	return res
}

// normalizeMixerName strips ALSA suffixes so "DACL Playback Volume" and the
// simple control "DACL" match.
func normalizeMixerName(name string) string {
	n := strings.TrimSpace(name)
	for _, suffix := range []string{" Playback Volume", " Volume", " Playback", " Switch"} {
		n = strings.TrimSuffix(n, suffix)
	}
	return n
}

// volumeLevelToRaw maps a UI level (0..20) onto the control's raw register
// value using a perceptual (dB-linear) taper when the driver has a dB scale.
func volumeLevelToRaw(level int, ctrl mixerControl) int {
	if level <= 0 {
		return 0
	}
	if level > VolumeMaxLevel {
		level = VolumeMaxLevel
	}
	t := float64(level) / float64(VolumeMaxLevel)
	if !ctrl.hasDB || ctrl.rawMax <= 0 {
		return int(float64(ctrl.rawMax)*t + 0.5)
	}
	// 0 dB reference point (the maximum value before gain/boost).
	raw0dB := int((-ctrl.dbMin)/ctrl.dbStep + 0.5)
	if raw0dB <= 0 || raw0dB > ctrl.rawMax {
		raw0dB = ctrl.rawMax
	}
	if level >= VolumeMaxLevel {
		return raw0dB
	}
	const spanDB = 50.0 // usable attenuation range from the top step
	db := -spanDB * (1 - t)
	if db < ctrl.dbMin {
		db = ctrl.dbMin
	}
	raw := int((db-ctrl.dbMin)/ctrl.dbStep + 0.5)
	if raw < 1 {
		raw = 1
	}
	if raw > raw0dB {
		raw = raw0dB
	}
	return raw
}

// volumeRawToLevel is the inverse of volumeLevelToRaw.
func volumeRawToLevel(raw int, ctrl mixerControl) int {
	if raw <= 0 {
		return 0
	}
	if !ctrl.hasDB || ctrl.rawMax <= 0 {
		if ctrl.rawMax <= 0 {
			return 0
		}
		return percentToVolumeLevel(int(float64(raw) * 100 / float64(ctrl.rawMax)))
	}
	raw0dB := int((-ctrl.dbMin)/ctrl.dbStep + 0.5)
	if raw0dB <= 0 || raw0dB > ctrl.rawMax {
		raw0dB = ctrl.rawMax
	}
	if raw >= raw0dB {
		return VolumeMaxLevel
	}
	const spanDB = 50.0
	db := ctrl.dbMin + float64(raw)*ctrl.dbStep
	if db > 0 {
		return VolumeMaxLevel
	}
	t := 1 + db/spanDB
	if t <= 0 {
		return 1
	}
	lvl := int(t*float64(VolumeMaxLevel) + 0.5)
	if lvl < 1 {
		lvl = 1
	}
	if lvl > VolumeMaxLevel {
		lvl = VolumeMaxLevel
	}
	return lvl
}

func amixerRawLevelFrom(out string, ctrl mixerControl) (int, bool) {
	raw, ok := amixerRawValue(out)
	if !ok {
		return 0, false
	}
	return volumeRawToLevel(raw, ctrl), true
}

// amixerRawValue extracts the numeric playback register value, e.g.
// "Mono: Playback 191 [75%] [0.00dB]" -> 191.
func amixerRawValue(out string) (int, bool) {
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Limits:") || strings.HasPrefix(trimmed, "Capabilities:") {
			continue
		}
		idx := strings.Index(trimmed, "Playback")
		if idx <= 0 {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(trimmed[idx+len("Playback"):]), ":"))
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		return v, true
	}
	return 0, false
}

func amixerRawMax(out string) (int, bool) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "Limits:") {
			continue
		}
		idx := strings.Index(line, "-")
		if idx < 0 {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(line[idx+1:]))
		if err != nil || v <= 0 {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// isPlaybackVolume reports whether amixer output describes a playback volume
// control (and not a capture-only volume or a switch).
func isPlaybackVolume(out string) bool {
	caps := strings.ToLower(capabilitiesLine(out))
	if caps == "" {
		return false
	}
	if strings.Contains(caps, "cvolume") || strings.Contains(caps, "capture") {
		return false
	}
	return strings.Contains(caps, "pvolume") || strings.Contains(caps, "volume")
}

func isPlaybackSwitch(out string) bool {
	caps := strings.ToLower(capabilitiesLine(out))
	if caps == "" || strings.Contains(caps, "cswitch") {
		return false
	}
	return strings.Contains(caps, "pswitch") || strings.Contains(caps, "switch")
}

func capabilitiesLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Capabilities:") {
			_, rest, found := strings.Cut(line, "Capabilities:")
			if !found {
				return ""
			}
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func amixerSwitchState(out string) (muted bool, ok bool) {
	switch {
	case strings.Contains(out, "[off]"):
		return true, true
	case strings.Contains(out, "[on]"):
		return false, true
	}
	return false, false
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

var (
	sharedVolOnce sync.Once
	sharedVol     *VolumeController
)

// SharedVolume returns the process-wide volume controller used by the hardware
// watcher and any UI surface that needs to read or set the system volume.
func SharedVolume() *VolumeController {
	sharedVolOnce.Do(func() {
		sharedVol = NewVolumeController()
	})
	return sharedVol
}
