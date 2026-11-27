package sdr

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DemodMode selects the rtl_fm demodulator.
type DemodMode int

const (
	ModeWBFM DemodMode = iota
	ModeNFM
	ModeAM
	ModeUSB
	ModeLSB
	modeCount
)

func (m DemodMode) String() string {
	switch m {
	case ModeWBFM:
		return "WBFM"
	case ModeNFM:
		return "NFM"
	case ModeAM:
		return "AM"
	case ModeUSB:
		return "USB"
	case ModeLSB:
		return "LSB"
	default:
		return "NFM"
	}
}

// RTLFMArg returns the -M flag value expected by rtl_fm.
func (m DemodMode) RTLFMArg() string {
	switch m {
	case ModeWBFM:
		return "wbfm"
	case ModeNFM:
		return "fm"
	case ModeAM:
		return "am"
	case ModeUSB:
		return "usb"
	case ModeLSB:
		return "lsb"
	default:
		return "fm"
	}
}

// CycleDemodMode steps through demodulation modes.
func CycleDemodMode(cur DemodMode, delta int) DemodMode {
	return DemodMode((int(cur) + delta + int(modeCount)*4) % int(modeCount))
}

// FreqSteps are the tuning step sizes in Hz.
var FreqSteps = []int64{
	5_000,     // 5 kHz
	12_500,    // 12.5 kHz
	25_000,    // 25 kHz
	100_000,   // 100 kHz
	1_000_000, // 1 MHz
}

// FormatStep formats a tuning step cleanly (e.g. "100 kHz", "12.5 kHz").
func FormatStep(hz int64) string {
	switch {
	case hz >= 1_000_000 && hz%1_000_000 == 0:
		return fmt.Sprintf("%d MHz", hz/1_000_000)
	case hz%1_000 == 0:
		return fmt.Sprintf("%d kHz", hz/1_000)
	default:
		return fmt.Sprintf("%.1f kHz", float64(hz)/1_000.0)
	}
}

// GainOptions are tuner gain presets in tenths of dB (-1 = Auto AGC).
var GainOptions = []int{-1, 0, 9, 14, 20, 28, 34, 40, 49}

// FormatGain returns "Auto" or "<N> dB".
func FormatGain(db int) string {
	if db < 0 {
		return "Auto"
	}
	return fmt.Sprintf("%d dB", db)
}

const (
	MinFreqHz int64 = 100_000       // 0.1 MHz (Blog V4 direct sampling / HF)
	MaxFreqHz int64 = 1_800_000_000 // 1.8 GHz
)

// ClampFreq keeps freqHz inside realistic RTL-SDR bounds.
func ClampFreq(hz int64) int64 {
	if hz < MinFreqHz {
		return MinFreqHz
	}
	if hz > MaxFreqHz {
		return MaxFreqHz
	}
	return hz
}

// FormatFreqMHz renders frequency in MHz with 3 decimal places (e.g. "98.500 MHz").
func FormatFreqMHz(hz int64) string {
	return fmt.Sprintf("%.3f MHz", float64(hz)/1_000_000.0)
}

// FormatFreqShort renders a compact MHz label (e.g. "98.50M").
func FormatFreqShort(hz int64) string {
	return fmt.Sprintf("%.2fM", float64(hz)/1_000_000.0)
}

// ParseFreqMHz parses user input like "98.5", "162.550 MHz", "433920k", or "146520000".
// Bare numbers without units are interpreted as MHz unless >= 1_000_000 (Hz).
func ParseFreqMHz(raw string) (int64, error) {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return 0, errors.New("enter a frequency")
	}
	s = strings.ReplaceAll(s, " ", "")
	mult := 1_000_000.0
	switch {
	case strings.HasSuffix(s, "ghz"):
		s = strings.TrimSuffix(s, "ghz")
		mult = 1_000_000_000.0
	case strings.HasSuffix(s, "g"):
		s = strings.TrimSuffix(s, "g")
		mult = 1_000_000_000.0
	case strings.HasSuffix(s, "mhz"):
		s = strings.TrimSuffix(s, "mhz")
		mult = 1_000_000.0
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSuffix(s, "m")
		mult = 1_000_000.0
	case strings.HasSuffix(s, "khz"):
		s = strings.TrimSuffix(s, "khz")
		mult = 1_000.0
	case strings.HasSuffix(s, "k"):
		s = strings.TrimSuffix(s, "k")
		mult = 1_000.0
	case strings.HasSuffix(s, "hz"):
		s = strings.TrimSuffix(s, "hz")
		mult = 1.0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, errors.New("invalid frequency")
	}
	if mult == 1_000_000.0 && v >= 1_000_000 {
		// User typed raw Hz (e.g. "433920000").
		mult = 1.0
	}
	hz := int64(math.Round(v * mult))
	if hz < MinFreqHz || hz > MaxFreqHz {
		return 0, fmt.Errorf("range: 0.1–1800 MHz")
	}
	return hz, nil
}

// Preset is a named frequency + demodulation mode.
type Preset struct {
	Name        string    `json:"name"`
	FreqHz      int64     `json:"freq_hz"`
	Mode        DemodMode `json:"mode"`
	BandwidthHz int       `json:"bandwidth_hz,omitempty"`
	GainDB      int       `json:"gain_db,omitempty"`
	User        bool      `json:"-"`
}

// BandwidthsForMode returns the selectable channel bandwidths (Hz) for a mode.
func BandwidthsForMode(m DemodMode) []int {
	switch m {
	case ModeWBFM:
		return []int{120_000, 170_000, 200_000, 240_000}
	case ModeNFM:
		return []int{8_000, 12_000, 16_000, 24_000, 32_000, 48_000}
	case ModeAM:
		return []int{6_000, 8_000, 12_000, 16_000, 24_000}
	case ModeUSB, ModeLSB:
		return []int{3_000, 4_000, 6_000, 8_000, 12_000}
	default:
		return []int{12_000, 24_000}
	}
}

// DefaultBandwidth returns the standard IF bandwidth (Hz) for a mode.
func DefaultBandwidth(m DemodMode) int {
	switch m {
	case ModeWBFM:
		return 170_000
	case ModeNFM:
		return 24_000
	case ModeAM:
		return 8_000
	case ModeUSB, ModeLSB:
		return 3_000
	default:
		return 24_000
	}
}

// NormalizeBandwidth snaps bwHz to a valid bandwidth for mode m (or its default).
func NormalizeBandwidth(m DemodMode, bwHz int) int {
	opts := BandwidthsForMode(m)
	for _, o := range opts {
		if o == bwHz {
			return o
		}
	}
	return DefaultBandwidth(m)
}

// CycleBandwidth steps to the next/previous bandwidth option for mode m.
func CycleBandwidth(m DemodMode, curHz, delta int) int {
	opts := BandwidthsForMode(m)
	idx := 0
	found := false
	for i, o := range opts {
		if o == curHz {
			idx = i
			found = true
			break
		}
	}
	if !found {
		def := DefaultBandwidth(m)
		for i, o := range opts {
			if o == def {
				idx = i
				break
			}
		}
	}
	next := (idx + delta + len(opts)*4) % len(opts)
	return opts[next]
}

// FormatBandwidth formats a channel bandwidth cleanly (e.g. "170 kHz", "3 kHz").
func FormatBandwidth(hz int) string {
	if hz <= 0 {
		return "auto"
	}
	if hz%1000 == 0 {
		return fmt.Sprintf("%d kHz", hz/1000)
	}
	return fmt.Sprintf("%.1f kHz", float64(hz)/1000.0)
}

// DefaultPresets lists common frequencies for quick testing out of the box.
var DefaultPresets = []Preset{
	{Name: "FM 98.5", FreqHz: 98_500_000, Mode: ModeWBFM, GainDB: -1},
	{Name: "FM 101.1", FreqHz: 101_100_000, Mode: ModeWBFM, GainDB: -1},
	{Name: "NOAA WX1", FreqHz: 162_550_000, Mode: ModeNFM, GainDB: -1},
	{Name: "NOAA WX2", FreqHz: 162_400_000, Mode: ModeNFM, GainDB: -1},
	{Name: "NOAA WX3", FreqHz: 162_475_000, Mode: ModeNFM, GainDB: -1},
	{Name: "Air Guard", FreqHz: 121_500_000, Mode: ModeAM, GainDB: -1},
	{Name: "Air Tower", FreqHz: 118_000_000, Mode: ModeAM, GainDB: -1},
	{Name: "Marine 16", FreqHz: 156_800_000, Mode: ModeNFM, GainDB: -1},
	{Name: "PMR446 Ch1", FreqHz: 446_006_250, Mode: ModeNFM, GainDB: -1},
	{Name: "FRS/GMRS 1", FreqHz: 462_562_500, Mode: ModeNFM, GainDB: -1},
	{Name: "2m Calling", FreqHz: 146_520_000, Mode: ModeNFM, GainDB: -1},
	{Name: "2m APRS US", FreqHz: 144_390_000, Mode: ModeNFM, GainDB: -1},
	{Name: "2m APRS EU", FreqHz: 144_800_000, Mode: ModeNFM, GainDB: -1},
	{Name: "70cm Call", FreqHz: 446_000_000, Mode: ModeNFM, GainDB: -1},
	{Name: "ISM 433.92", FreqHz: 433_920_000, Mode: ModeNFM, GainDB: -1},
	{Name: "ISM 868.3", FreqHz: 868_300_000, Mode: ModeNFM, GainDB: -1},
	{Name: "ISM 915.0", FreqHz: 915_000_000, Mode: ModeNFM, GainDB: -1},
}

// DefaultBookmarksPath returns ~/.config/czhax/sdr.json.
func DefaultBookmarksPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			base = "/tmp"
		} else {
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, "czhax", "sdr.json")
}

// LoadBookmarks reads user bookmarks from disk (returning nil slice if missing).
func LoadBookmarks(path string) ([]Preset, error) {
	if path == "" {
		path = DefaultBookmarksPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []Preset
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	for i := range list {
		list[i].User = true
		list[i].FreqHz = ClampFreq(list[i].FreqHz)
	}
	return list, nil
}

// SaveBookmarks persists user bookmarks to disk.
func SaveBookmarks(path string, list []Preset) error {
	if path == "" {
		path = DefaultBookmarksPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// RadioConfig holds parameters for a live rtl_fm audio session.
type RadioConfig struct {
	DeviceIndex int
	FreqHz      int64
	Mode        DemodMode
	BandwidthHz int // 0 = mode default
	GainDB      int // -1 = Auto
	Squelch     int // 0..100
	PPM         int
	CaptureDir  string
}

// BuildRTLFMArgs builds the rtl_fm arguments and returns the rtl_fm output PCM rate
// and the downstream audio player rate (Hz). When bandwidth < playerRate, RunRadio
// upsamples in Go so narrow SSB/AM/NFM bandwidths play cleanly on every audio backend.
func BuildRTLFMArgs(cfg RadioConfig) (args []string, rtlOutRate int, playerRate int) {
	freq := ClampFreq(cfg.FreqHz)
	bw := NormalizeBandwidth(cfg.Mode, cfg.BandwidthHz)
	playerRate = 24000
	if cfg.Mode == ModeWBFM {
		playerRate = 32000
	}

	args = []string{
		"-d", strconv.Itoa(max(0, cfg.DeviceIndex)),
		"-f", strconv.FormatInt(freq, 10),
		"-M", cfg.Mode.RTLFMArg(),
		"-s", strconv.Itoa(bw),
	}
	if bw >= playerRate {
		rtlOutRate = playerRate
		args = append(args, "-r", strconv.Itoa(playerRate))
	} else {
		// rtl_fm only downsamples with -r; emit native bw rate and upsample in Go.
		rtlOutRate = bw
	}
	args = append(args, "-A", "fast")
	if cfg.Mode == ModeWBFM {
		args = append(args, "-F", "9", "-E", "deemp")
	}
	if cfg.GainDB >= 0 {
		args = append(args, "-g", strconv.Itoa(cfg.GainDB))
	}
	if cfg.Squelch > 0 {
		args = append(args, "-l", strconv.Itoa(cfg.Squelch))
	}
	if cfg.PPM != 0 {
		args = append(args, "-p", strconv.Itoa(cfg.PPM))
	}
	args = append(args, "-")
	return args, rtlOutRate, playerRate
}

// AudioPlayerCommand picks an installed raw PCM player reading 16-bit LE mono from stdin.
func AudioPlayerCommand(rate int) (string, []string, error) {
	rateStr := strconv.Itoa(rate)
	if bin := FindTool("aplay"); bin != "" {
		return bin, []string{"-q", "-t", "raw", "-f", "S16_LE", "-r", rateStr, "-c", "1", "-"}, nil
	}
	if bin := FindTool("pw-play"); bin != "" {
		return bin, []string{"--raw", "--format=s16", "--rate=" + rateStr, "--channels=1", "-"}, nil
	}
	if bin := FindTool("ffplay"); bin != "" {
		return bin, []string{"-loglevel", "quiet", "-nodisp", "-autoexit", "-f", "s16le", "-ar", rateStr, "-ac", "1", "-i", "pipe:0"}, nil
	}
	if bin := FindTool("mpv"); bin != "" {
		return bin, []string{
			"--really-quiet", "--no-terminal", "--no-video",
			"--demuxer=rawaudio", "--demuxer-rawaudio-rate=" + rateStr,
			"--demuxer-rawaudio-channels=1", "--demuxer-rawaudio-format=s16le", "-",
		}, nil
	}
	return "", nil, errors.New("no raw audio player found (install alsa-utils, ffmpeg, or mpv)")
}

// RadioUpdate carries live signal/audio telemetry to the TUI.
type RadioUpdate struct {
	Level     float64 // 0..1 normalized meter
	DBFS      float64 // -60..0
	Muted     bool
	RecPath   string
	BytesRead int64
}

// RadioController allows toggling mute and WAV recording while RunRadio is running.
type RadioController struct {
	muted   atomic.Bool
	mu      sync.Mutex
	recFile *os.File
	recPath string
	recSize uint32
	rate    int
}

// NewRadioController creates a live session controller for audioRate Hz mono S16_LE.
func NewRadioController(audioRate int) *RadioController {
	return &RadioController{rate: audioRate}
}

func (c *RadioController) SetMuted(m bool) {
	if c != nil {
		c.muted.Store(m)
	}
}

func (c *RadioController) Muted() bool {
	if c == nil {
		return false
	}
	return c.muted.Load()
}

func (c *RadioController) RecordingPath() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recPath
}

// StartRecording opens a WAV capture file in dir.
func (c *RadioController) StartRecording(dir string, freqHz int64, mode DemodMode) (string, error) {
	if c == nil {
		return "", errors.New("radio not active")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.recFile != nil {
		return c.recPath, nil
	}
	if strings.TrimSpace(dir) == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("sdr_%s_%s_%s.wav",
		strings.ReplaceAll(fmt.Sprintf("%.3fM", float64(freqHz)/1e6), ".", "_"),
		strings.ToLower(mode.String()),
		time.Now().Format("20060102_150405"),
	)
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	rate := c.rate
	if rate <= 0 {
		rate = 24000
	}
	hdr := buildWAVHeader(uint32(rate), 1, 16, 0)
	if _, err := f.Write(hdr); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	c.recFile = f
	c.recPath = path
	c.recSize = 0
	return path, nil
}

// StopRecording finalizes the WAV header and closes the capture file.
func (c *RadioController) StopRecording() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.recFile == nil {
		return ""
	}
	path := c.recPath
	finalizeWAVFile(c.recFile, c.recSize)
	_ = c.recFile.Close()
	c.recFile = nil
	c.recPath = ""
	c.recSize = 0
	return path
}

func (c *RadioController) writeRec(pcm []byte) {
	c.mu.Lock()
	if c.recFile != nil && len(pcm) > 0 {
		if n, err := c.recFile.Write(pcm); err == nil {
			c.recSize += uint32(n)
		}
	}
	c.mu.Unlock()
}

func buildWAVHeader(sampleRate uint32, channels, bits uint16, dataSize uint32) []byte {
	var hdr [44]byte
	byteRate := sampleRate * uint32(channels) * uint32(bits/8)
	blockAlign := channels * (bits / 8)
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], 36+dataSize)
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], channels)
	binary.LittleEndian.PutUint32(hdr[24:28], sampleRate)
	binary.LittleEndian.PutUint32(hdr[28:32], byteRate)
	binary.LittleEndian.PutUint16(hdr[32:34], blockAlign)
	binary.LittleEndian.PutUint16(hdr[34:36], bits)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], dataSize)
	return hdr[:]
}

func finalizeWAVFile(f *os.File, dataSize uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], 36+dataSize)
	_, _ = f.WriteAt(b[:], 4)
	binary.LittleEndian.PutUint32(b[:], dataSize)
	_, _ = f.WriteAt(b[:], 40)
}

// RunRadio runs rtl_fm piped into the system audio player until ctx is canceled or an error occurs.
func RunRadio(ctx context.Context, cfg RadioConfig, ctrl *RadioController, onUpdate func(RadioUpdate)) error {
	rtlfm := FindTool("rtl_fm")
	if rtlfm == "" {
		return errors.New("rtl_fm not installed (apt install rtl-sdr)")
	}
	args, rtlOutRate, playerRate := BuildRTLFMArgs(cfg)
	playerBin, playerArgs, err := AudioPlayerCommand(playerRate)
	if err != nil {
		return err
	}

	if ctrl == nil {
		ctrl = NewRadioController(playerRate)
	} else {
		ctrl.mu.Lock()
		ctrl.rate = playerRate
		ctrl.mu.Unlock()
	}
	defer ctrl.StopRecording()

	release, err := acquireUSB(ctx)
	if err != nil {
		return err
	}
	defer release()

	DetachDVBDriverIfNeeded(DiscoverUSBDevices())

	rctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rtlCmd := exec.CommandContext(rctx, rtlfm, args...)
	rtlCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	rtlCmd.Cancel = func() error {
		if rtlCmd.Process != nil {
			return syscall.Kill(-rtlCmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	rtlCmd.WaitDelay = 1500 * time.Millisecond

	rtlOut, err := rtlCmd.StdoutPipe()
	if err != nil {
		return err
	}
	rtlErrPipe, err := rtlCmd.StderrPipe()
	if err != nil {
		return err
	}

	playCmd := exec.CommandContext(rctx, playerBin, playerArgs...)
	playCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	playCmd.Cancel = func() error {
		if playCmd.Process != nil {
			return syscall.Kill(-playCmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	playCmd.WaitDelay = 1500 * time.Millisecond
	playIn, err := playCmd.StdinPipe()
	if err != nil {
		return err
	}

	var stderrMu sync.Mutex
	var lastErrLine string
	captureErr := func(r io.Reader) {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			stderrMu.Lock()
			lastErrLine = line
			stderrMu.Unlock()
		}
	}

	if err := rtlCmd.Start(); err != nil {
		return fmt.Errorf("rtl_fm: %w", err)
	}
	go captureErr(rtlErrPipe)

	if err := playCmd.Start(); err != nil {
		cancel()
		_ = rtlCmd.Wait()
		return fmt.Errorf("%s: %w", filepath.Base(playerBin), err)
	}

	buf := make([]byte, 4096)
	var rem [1]byte
	hasRem := false
	var totalBytes int64
	lastTick := time.Now()
	var peakRMS float64

	for {
		n, rerr := rtlOut.Read(buf)
		if n > 0 {
			raw := buf[:n]
			if hasRem {
				merged := make([]byte, 1+n)
				merged[0] = rem[0]
				copy(merged[1:], raw)
				raw = merged
				hasRem = false
			}
			if len(raw)%2 == 1 {
				rem[0] = raw[len(raw)-1]
				hasRem = true
				raw = raw[:len(raw)-1]
			}
			if len(raw) == 0 {
				if rerr != nil {
					break
				}
				continue
			}

			chunk := resampleS16LE(raw, rtlOutRate, playerRate)
			if gain := demodGain(cfg.Mode, rtlOutRate); gain > 1 {
				chunk = applyGainS16LE(chunk, gain)
			}
			totalBytes += int64(len(chunk))
			ctrl.writeRec(chunk)

			rms := pcmRMS(chunk)
			if rms > peakRMS {
				peakRMS = rms
			}

			outChunk := chunk
			if ctrl.Muted() {
				outChunk = make([]byte, len(chunk))
			}
			if _, werr := playIn.Write(outChunk); werr != nil && rctx.Err() == nil {
				cancel()
				break
			}

			if onUpdate != nil && time.Since(lastTick) >= 100*time.Millisecond {
				dbfs := -60.0
				if peakRMS > 1e-4 {
					dbfs = 20 * math.Log10(peakRMS)
					if dbfs < -60 {
						dbfs = -60
					}
					if dbfs > 0 {
						dbfs = 0
					}
				}
				lvl := (dbfs + 60.0) / 60.0
				onUpdate(RadioUpdate{
					Level:     lvl,
					DBFS:      dbfs,
					Muted:     ctrl.Muted(),
					RecPath:   ctrl.RecordingPath(),
					BytesRead: totalBytes,
				})
				peakRMS = 0
				lastTick = time.Now()
			}
		}
		if rerr != nil {
			break
		}
	}

	_ = playIn.Close()
	rtlWaitErr := rtlCmd.Wait()
	_ = playCmd.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if rtlWaitErr != nil {
		stderrMu.Lock()
		msg := lastErrLine
		stderrMu.Unlock()
		if msg != "" {
			return fmt.Errorf("%s", humanizeRTLErr(msg))
		}
		return rtlWaitErr
	}
	return nil
}

// demodGain compensates rtl_fm's output_scale, which shrinks AM/SSB amplitude
// as the demod rate drops, so narrow bandwidths are not 10-20 dB quieter.
func demodGain(mode DemodMode, demodRate int) int {
	if mode != ModeAM && mode != ModeUSB && mode != ModeLSB {
		return 1
	}
	if demodRate <= 0 {
		return 1
	}
	gain := 24000 / demodRate
	if gain < 1 {
		gain = 1
	}
	if gain > 16 {
		gain = 16
	}
	return gain
}

func applyGainS16LE(in []byte, gain int) []byte {
	if gain <= 1 || len(in) < 2 {
		return in
	}
	out := make([]byte, len(in)-len(in)%2)
	for i := 0; i+1 < len(in); i += 2 {
		v := int32(int16(binary.LittleEndian.Uint16(in[i:i+2]))) * int32(gain)
		if v > 32767 {
			v = 32767
		}
		if v < -32768 {
			v = -32768
		}
		binary.LittleEndian.PutUint16(out[i:i+2], uint16(int16(v)))
	}
	return out
}

func resampleS16LE(in []byte, inRate, outRate int) []byte {
	if inRate <= 0 || outRate <= 0 || inRate == outRate || len(in) < 4 {
		return in
	}
	inSamples := len(in) / 2
	outSamples := int(float64(inSamples) * float64(outRate) / float64(inRate))
	if outSamples <= 0 {
		return in
	}
	out := make([]byte, outSamples*2)
	for i := 0; i < outSamples; i++ {
		pos := float64(i) * float64(inRate) / float64(outRate)
		i0 := int(pos)
		frac := pos - float64(i0)
		if i0 >= inSamples-1 {
			i0 = inSamples - 1
			frac = 0
		}
		i1 := i0 + 1
		if i1 >= inSamples {
			i1 = i0
		}
		s0 := float64(int16(binary.LittleEndian.Uint16(in[i0*2 : i0*2+2])))
		s1 := float64(int16(binary.LittleEndian.Uint16(in[i1*2 : i1*2+2])))
		v := int16(s0*(1-frac) + s1*frac)
		binary.LittleEndian.PutUint16(out[i*2:i*2+2], uint16(v))
	}
	return out
}

func pcmRMS(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		s := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:i*2+2]))) / 32768.0
		sum += s * s
	}
	return math.Sqrt(sum / float64(n))
}

func humanizeRTLErr(msg string) string {
	switch {
	case strings.Contains(msg, "No supported devices found"):
		return "No RTL-SDR USB dongle found"
	case strings.Contains(msg, "usb_claim_interface error -6"):
		return "USB busy (run: sudo modprobe -r dvb_usb_rtl28xxu)"
	case strings.Contains(msg, "usb_open error -3") || strings.Contains(msg, "usb_claim_interface error -3"):
		return "USB permission denied (check plugdev / udev rules)"
	default:
		return msg
	}
}
