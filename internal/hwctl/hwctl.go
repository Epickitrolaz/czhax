package hwctl

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	BrightnessMaxLevel = 10
	VolumeMaxLevel     = 20

	evKey             uint16 = 1
	keyMute           uint16 = 113
	keyVolumeDown     uint16 = 114
	keyVolumeUp       uint16 = 115
	keyBrightnessDown uint16 = 224
	keyBrightnessUp   uint16 = 225

	eviocgname256 = 0x80ff4506
)

type BacklightState struct {
	Raw      int
	MaxRaw   int
	Level    int // 0..10
	MaxLevel int // 10
	Percent  int // 0..100
}

type EventKind int

const (
	EventBrightness EventKind = iota
	EventVolume
)

type Event struct {
	Kind       EventKind
	Brightness BacklightState
	Volume     VolumeState
}

// BrightnessRawForLevel maps a perceptual selector level in [0..10] to a raw
// hardware backlight value in [0..maxB] using CIE 1931 lightness (L*).
// Level 0 is always 0 (black), level 10 is maxB, and each step produces an
// equal perceived brightness change across the full range.
func BrightnessRawForLevel(level, maxB int) int {
	if level <= 0 || maxB <= 0 {
		return 0
	}
	if level >= BrightnessMaxLevel {
		return maxB
	}
	lStar := float64(level) * 100.0 / float64(BrightnessMaxLevel)
	var y float64
	if lStar <= 8.0 {
		y = lStar / 903.3
	} else {
		t := (lStar + 16.0) / 116.0
		y = t * t * t
	}
	raw := int(math.Round(y * float64(maxB)))
	if raw < 1 {
		raw = 1
	}
	if raw > maxB {
		raw = maxB
	}
	return raw
}

// BrightnessPercent maps a raw backlight value in [0..maxB] to a perceptually
// linear percentage in [0..100] (matching CIE 1931 lightness L*).
func BrightnessPercent(cur, maxB int) int {
	if cur <= 0 || maxB <= 0 {
		return 0
	}
	if cur >= maxB {
		return 100
	}
	for lvl := 1; lvl < BrightnessMaxLevel; lvl++ {
		if cur == BrightnessRawForLevel(lvl, maxB) {
			return (lvl * 100) / BrightnessMaxLevel
		}
	}
	y := float64(cur) / float64(maxB)
	var lStar float64
	if y <= 0.008856 {
		lStar = y * 903.3
	} else {
		lStar = 116.0*math.Cbrt(y) - 16.0
	}
	pct := int(math.Round(lStar))
	if pct < 1 {
		pct = 1
	}
	if pct > 99 {
		pct = 99
	}
	return pct
}

// BrightnessLevel maps a raw backlight value in [0..maxB] to 10 perceptually
// linear discrete levels. Non-zero values always map to at least level 1.
func BrightnessLevel(cur, maxB int) int {
	if cur <= 0 || maxB <= 0 {
		return 0
	}
	if cur >= maxB {
		return BrightnessMaxLevel
	}
	for lvl := 1; lvl < BrightnessMaxLevel; lvl++ {
		if cur == BrightnessRawForLevel(lvl, maxB) {
			return lvl
		}
	}
	pct := BrightnessPercent(cur, maxB)
	lvl := (pct + 5) / 10
	if lvl < 1 {
		lvl = 1
	}
	if lvl > BrightnessMaxLevel {
		lvl = BrightnessMaxLevel
	}
	return lvl
}

// ReadBacklight reads the first backlight device under sysRoot (default "/sys").
func ReadBacklight(sysRoot string) (BacklightState, bool) {
	bPath, mPath, ok := findBacklightPaths(sysRoot)
	if !ok {
		return BacklightState{}, false
	}
	cur, ok1 := readIntFile(bPath)
	maxB, ok2 := readIntFile(mPath)
	if !ok1 || !ok2 || maxB <= 0 {
		return BacklightState{}, false
	}
	if cur < 0 {
		cur = 0
	}
	if cur > maxB {
		cur = maxB
	}
	lvl := BrightnessLevel(cur, maxB)
	pct := BrightnessPercent(cur, maxB)
	return BacklightState{
		Raw:      cur,
		MaxRaw:   maxB,
		Level:    lvl,
		MaxLevel: BrightnessMaxLevel,
		Percent:  pct,
	}, true
}

// ExpectedBrightnessStep returns the target raw brightness on the 10-level
// perceptually linear grid when stepping from cur by delta.
func ExpectedBrightnessStep(cur, maxB, delta int) int {
	if maxB <= 0 {
		return 0
	}
	if cur < 0 {
		cur = 0
	}
	if cur > maxB {
		cur = maxB
	}
	curLvl := BrightnessLevel(cur, maxB)
	if delta > 0 {
		if cur >= maxB {
			return maxB
		}
		target := min(BrightnessMaxLevel, curLvl+1)
		raw := BrightnessRawForLevel(target, maxB)
		for raw <= cur && target < BrightnessMaxLevel {
			target++
			raw = BrightnessRawForLevel(target, maxB)
		}
		if raw <= cur {
			return min(maxB, cur+1)
		}
		return raw
	}
	if cur <= 0 {
		return 0
	}
	target := max(0, curLvl-1)
	raw := BrightnessRawForLevel(target, maxB)
	for raw >= cur && target > 0 {
		target--
		raw = BrightnessRawForLevel(target, maxB)
	}
	if raw >= cur {
		return max(0, cur-1)
	}
	return raw
}

// StepBacklight adjusts sysfs brightness by delta levels (out of 10).
// Used when no external brightness daemon is managing the backlight or to snap
// legacy daemon clamps (such as MIN_BRIGHTNESS=5) onto the perceptual grid.
func StepBacklight(sysRoot string, delta int) (BacklightState, bool) {
	bPath, mPath, ok := findBacklightPaths(sysRoot)
	if !ok {
		return BacklightState{}, false
	}
	cur, ok1 := readIntFile(bPath)
	maxB, ok2 := readIntFile(mPath)
	if !ok1 || !ok2 || maxB <= 0 {
		return BacklightState{}, false
	}
	next := ExpectedBrightnessStep(cur, maxB, delta)
	return writeBacklightRaw(bPath, next, maxB)
}

func writeBacklightRaw(bPath string, next, maxB int) (BacklightState, bool) {
	if err := os.WriteFile(bPath, []byte(strconv.Itoa(next)+"\n"), 0o644); err != nil {
		return BacklightState{}, false
	}
	return BacklightState{
		Raw:      next,
		MaxRaw:   maxB,
		Level:    BrightnessLevel(next, maxB),
		MaxLevel: BrightnessMaxLevel,
		Percent:  BrightnessPercent(next, maxB),
	}, true
}

func findBacklightPaths(sysRoot string) (brightnessPath, maxPath string, ok bool) {
	if sysRoot == "" {
		sysRoot = "/sys"
	}
	dirs, err := filepath.Glob(filepath.Join(sysRoot, "class/backlight/*"))
	if err != nil || len(dirs) == 0 {
		return "", "", false
	}
	sort.Strings(dirs)
	return filepath.Join(dirs[0], "brightness"), filepath.Join(dirs[0], "max_brightness"), true
}

func readIntFile(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, false
	}
	return v, true
}

// Watcher monitors Linux backlight sysfs changes and TCA8418 / keyboard evdev
// brightness, volume, and mute keys.
type Watcher struct {
	events chan Event
	stopCh chan struct{}
	blKey  chan int
	once   sync.Once

	mu            sync.Mutex
	lastRaw       int
	hasLastRaw    bool
	holdPollUntil time.Time
	vol           *VolumeController
}

// StartWatcher launches background hardware listeners and returns a Watcher.
func StartWatcher() *Watcher {
	w := &Watcher{
		events: make(chan Event, 16),
		stopCh: make(chan struct{}),
		blKey:  make(chan int, 16),
		vol:    NewVolumeController(),
	}
	if st, ok := ReadBacklight("/sys"); ok {
		w.lastRaw = st.Raw
		w.hasLastRaw = true
	}
	go w.brightnessKeyLoop()
	go w.pollBacklight("/sys")
	go w.watchEvdev("/dev/input")
	return w
}

func (w *Watcher) Events() <-chan Event {
	return w.events
}

func (w *Watcher) Close() {
	w.once.Do(func() {
		close(w.stopCh)
	})
}

func (w *Watcher) emit(ev Event) {
	select {
	case w.events <- ev:
	default:
		// Drop oldest unread event so the newest popup always wins.
		select {
		case <-w.events:
		default:
		}
		select {
		case w.events <- ev:
		default:
		}
	}
}

func (w *Watcher) pollBacklight(sysRoot string) {
	ticker := time.NewTicker(90 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			held := !w.holdPollUntil.IsZero() && time.Now().Before(w.holdPollUntil)
			w.mu.Unlock()
			if held {
				continue
			}
			st, ok := ReadBacklight(sysRoot)
			if !ok {
				continue
			}
			w.mu.Lock()
			changed := !w.hasLastRaw || st.Raw != w.lastRaw
			if changed {
				w.lastRaw = st.Raw
				w.hasLastRaw = true
			}
			w.mu.Unlock()
			if changed {
				w.emit(Event{Kind: EventBrightness, Brightness: st})
			}
		}
	}
}

func (w *Watcher) triggerBrightnessKey(delta int) {
	w.mu.Lock()
	w.holdPollUntil = time.Now().Add(150 * time.Millisecond)
	w.mu.Unlock()
	select {
	case w.blKey <- delta:
	default:
	}
}

func (w *Watcher) brightnessKeyLoop() {
	for {
		select {
		case <-w.stopCh:
			return
		case delta := <-w.blKey:
			w.handleBrightnessKey(delta)
		}
	}
}

func (w *Watcher) handleBrightnessKey(delta int) {
	w.handleBrightnessKeyAt("/sys", delta)
}

func (w *Watcher) handleBrightnessKeyAt(sysRoot string, delta int) {
	w.mu.Lock()
	before := w.lastRaw
	hasBefore := w.hasLastRaw
	w.holdPollUntil = time.Now().Add(150 * time.Millisecond)
	w.mu.Unlock()

	// Give cardputer-brightness.service a brief window to apply its sysfs write
	// so we display the updated value without racing.
	time.Sleep(45 * time.Millisecond)

	st, ok := ReadBacklight(sysRoot)
	if !ok {
		return
	}
	base := st.Raw
	if hasBefore {
		base = before
	}
	want := ExpectedBrightnessStep(base, st.MaxRaw, delta)
	// Snap legacy daemon steps (e.g. 10 -> 5 or 5 -> 15) onto the clean
	// 0, 10, 20, ..., 100 grid when sysfs is writable.
	if st.Raw != want {
		if bPath, _, bOk := findBacklightPaths(sysRoot); bOk {
			if snapped, sOk := writeBacklightRaw(bPath, want, st.MaxRaw); sOk {
				st = snapped
			}
		}
	}

	w.mu.Lock()
	w.lastRaw = st.Raw
	w.hasLastRaw = true
	if len(w.blKey) == 0 {
		w.holdPollUntil = time.Time{}
	}
	w.mu.Unlock()
	w.emit(Event{Kind: EventBrightness, Brightness: st})
}

func (w *Watcher) handleVolumeKey(code uint16, repeat bool) {
	if w.vol == nil {
		return
	}
	var st VolumeState
	switch code {
	case keyMute:
		if repeat {
			return
		}
		st = w.vol.ToggleMute()
	case keyVolumeDown:
		st = w.vol.Step(-1)
	case keyVolumeUp:
		st = w.vol.Step(1)
	default:
		return
	}
	w.emit(Event{Kind: EventVolume, Volume: st})
}

func (w *Watcher) watchEvdev(inputDir string) {
	entries, err := filepath.Glob(filepath.Join(inputDir, "event*"))
	if err != nil || len(entries) == 0 {
		return
	}
	for _, devPath := range entries {
		base := filepath.Base(devPath)
		nameBytes, _ := os.ReadFile(filepath.Join("/sys/class/input", base, "device/name"))
		sysName := strings.TrimSpace(string(nameBytes))

		fd, err := unix.Open(devPath, unix.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		devName := readEvdevName(fd, sysName)
		if !isMediaKeyDevice(devName) {
			_ = unix.Close(fd)
			continue
		}
		go w.readEvdevLoop(fd)
	}
}

func (w *Watcher) readEvdevLoop(fd int) {
	defer unix.Close(fd)
	evSize := int(unsafe.Sizeof(unix.Timeval{})) + 8
	tvSize := int(unsafe.Sizeof(unix.Timeval{}))
	buf := make([]byte, evSize*8)
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}

	for {
		select {
		case <-w.stopCh:
			return
		default:
		}
		n, err := unix.Poll(pfd, 250)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if n <= 0 || (pfd[0].Revents&unix.POLLIN) == 0 {
			continue
		}
		nr, err := unix.Read(fd, buf)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EINTR {
				continue
			}
			return
		}
		for off := 0; off+evSize <= nr; off += evSize {
			evType := *(*uint16)(unsafe.Pointer(&buf[off+tvSize]))
			code := *(*uint16)(unsafe.Pointer(&buf[off+tvSize+2]))
			val := *(*int32)(unsafe.Pointer(&buf[off+tvSize+4]))
			if evType != evKey || (val != 1 && val != 2) {
				continue
			}
			repeat := val == 2
			switch code {
			case keyBrightnessDown:
				w.triggerBrightnessKey(-1)
			case keyBrightnessUp:
				w.triggerBrightnessKey(1)
			case keyMute, keyVolumeDown, keyVolumeUp:
				w.handleVolumeKey(code, repeat)
			}
		}
	}
}

func isMediaKeyDevice(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	if low == "" {
		return true
	}
	for _, skip := range []string{
		"bmi270", "bmm150", "imu", "mouse", "touchpad",
		"vc4-hdmi", "hdmi", "headphones", "es8389", "es8390",
		"gpio_ir", "ir_recv",
	} {
		if strings.Contains(low, skip) {
			return false
		}
	}
	return true
}

func readEvdevName(fd int, sysName string) string {
	if sysName != "" {
		return sysName
	}
	var buf [256]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), eviocgname256, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf[:])
}
