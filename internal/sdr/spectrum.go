package sdr

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultSpecRate int64         = 2_048_000
	DefaultFFTSize  int           = 512
	SweepRefresh    time.Duration = 500 * time.Millisecond
	MaxTopSignals   int           = 50
)

// SpectrumConfig configures live rtl_sdr FFT streaming.
type SpectrumConfig struct {
	DeviceIndex int
	CenterHz    int64
	SampleRate  int64
	GainDB      int // -1 = Auto
	OutBins     int // e.g. 34..44 to match pane width
	FFTSize     int // power of 2, default 512
}

// SweepConfig configures a custom start..end frequency sweep refreshing every Interval.
type SweepConfig struct {
	DeviceIndex int
	StartHz     int64
	EndHz       int64
	GainDB      int // -1 = Auto
	OutBins     int
	Interval    time.Duration // defaults to 500ms
}

// SignalPeak represents one detected peak frequency and its measured power in dBFS.
type SignalPeak struct {
	FreqHz  int64
	PowerDB float64
}

// SpectrumFrame holds one computed power spectrum slice in dBFS and its strongest signals.
type SpectrumFrame struct {
	StartHz    int64
	EndHz      int64
	CenterHz   int64
	Bins       []float64    // downsampled dBFS values across [StartHz, EndHz]
	TopSignals []SignalPeak // up to 50 highest signals sorted descending by PowerDB
	PeakHz     int64
	PeakDB     float64
	FloorDB    float64
}

// BandPreset defines a wide-band sweep range for quick preset selection.
type BandPreset struct {
	Name        string
	StartMHz    float64
	EndMHz      float64
	StepKHz     int
	DefaultMode DemodMode
}

// BandPresets lists useful wideband scan ranges.
var BandPresets = []BandPreset{
	{Name: "FM Radio", StartMHz: 87.5, EndMHz: 108.0, StepKHz: 100, DefaultMode: ModeWBFM},
	{Name: "Airband", StartMHz: 118.0, EndMHz: 137.0, StepKHz: 25, DefaultMode: ModeAM},
	{Name: "2m Ham", StartMHz: 144.0, EndMHz: 148.0, StepKHz: 25, DefaultMode: ModeNFM},
	{Name: "NOAA WX", StartMHz: 162.35, EndMHz: 162.60, StepKHz: 12, DefaultMode: ModeNFM},
	{Name: "ISM 433", StartMHz: 433.05, EndMHz: 434.80, StepKHz: 25, DefaultMode: ModeNFM},
	{Name: "70cm Ham", StartMHz: 430.0, EndMHz: 450.0, StepKHz: 50, DefaultMode: ModeNFM},
}

// FFT computes an in-place radix-2 Cooley-Tukey discrete Fourier transform.
// len(re) and len(im) must be equal and a power of two.
func FFT(re, im []float64) {
	n := len(re)
	if n <= 1 || len(im) != n || (n&(n-1)) != 0 {
		return
	}
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		angle := -2.0 * math.Pi / float64(size)
		wStepRe := math.Cos(angle)
		wStepIm := math.Sin(angle)
		for start := 0; start < n; start += size {
			wRe, wIm := 1.0, 0.0
			for k := 0; k < half; k++ {
				i1 := start + k
				i2 := i1 + half
				tRe := wRe*re[i2] - wIm*im[i2]
				tIm := wRe*im[i2] + wIm*re[i2]
				re[i2] = re[i1] - tRe
				im[i2] = im[i1] - tIm
				re[i1] += tRe
				im[i1] += tIm
				nextRe := wRe*wStepRe - wIm*wStepIm
				wIm = wRe*wStepIm + wIm*wStepRe
				wRe = nextRe
			}
		}
	}
}

// computeFullSpectrumDB computes all fftSize shifted dBFS bins from -Fs/2 to +Fs/2.
func computeFullSpectrumDB(iq []byte, fftSize int) []float64 {
	if fftSize < 16 || (fftSize&(fftSize-1)) != 0 {
		fftSize = DefaultFFTSize
	}
	bytesPerFrame := fftSize * 2
	frames := len(iq) / bytesPerFrame
	if frames == 0 {
		out := make([]float64, fftSize)
		for i := range out {
			out[i] = -75
		}
		return out
	}

	win := make([]float64, fftSize)
	var winSum float64
	for i := 0; i < fftSize; i++ {
		w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(fftSize-1)))
		win[i] = w
		winSum += w
	}
	norm := 1.0 / (winSum * 127.5)

	acc := make([]float64, fftSize)
	re := make([]float64, fftSize)
	im := make([]float64, fftSize)

	for f := 0; f < frames; f++ {
		base := f * bytesPerFrame
		var sumI, sumQ float64
		for i := 0; i < fftSize; i++ {
			sumI += float64(iq[base+i*2]) - 127.5
			sumQ += float64(iq[base+i*2+1]) - 127.5
		}
		meanI := sumI / float64(fftSize)
		meanQ := sumQ / float64(fftSize)

		for i := 0; i < fftSize; i++ {
			re[i] = ((float64(iq[base+i*2]) - 127.5) - meanI) * win[i]
			im[i] = ((float64(iq[base+i*2+1]) - 127.5) - meanQ) * win[i]
		}
		FFT(re, im)

		half := fftSize / 2
		for i := 0; i < fftSize; i++ {
			src := (i + half) % fftSize
			mag2 := (re[src]*re[src] + im[src]*im[src]) * norm * norm
			acc[i] += mag2
		}
	}

	for i := range acc {
		acc[i] /= float64(frames)
	}
	mid := fftSize / 2
	if mid > 1 && mid+1 < fftSize {
		acc[mid] = 0.5 * (acc[mid-1] + acc[mid+1])
	}

	out := make([]float64, fftSize)
	for i, p := range acc {
		db := -80.0
		if p > 1e-12 {
			db = 10.0 * math.Log10(p)
		}
		if db < -80.0 {
			db = -80.0
		}
		if db > 0.0 {
			db = 0.0
		}
		out[i] = db
	}
	return out
}

// ComputeSpectrum transforms interleaved unsigned 8-bit I/Q samples into outBins dBFS values
// ordered from -Fs/2 to +Fs/2, with center DC spike suppression and max-pooling.
func ComputeSpectrum(iq []byte, fftSize, outBins int) []float64 {
	full := computeFullSpectrumDB(iq, fftSize)
	return poolBinsMax(full, outBins)
}

func poolBinsMax(full []float64, outBins int) []float64 {
	if outBins <= 0 {
		outBins = 34
	}
	n := len(full)
	if n == 0 {
		out := make([]float64, outBins)
		for i := range out {
			out[i] = -75
		}
		return out
	}
	if n == outBins {
		cp := make([]float64, n)
		copy(cp, full)
		return cp
	}
	out := make([]float64, outBins)
	for b := 0; b < outBins; b++ {
		start := (b * n) / outBins
		end := ((b + 1) * n) / outBins
		if end <= start {
			end = start + 1
		}
		if end > n {
			end = n
		}
		peak := full[start]
		for k := start + 1; k < end; k++ {
			if full[k] > peak {
				peak = full[k]
			}
		}
		out[b] = peak
	}
	return out
}

// ExtractTopSignals extracts up to limit distinct signal peaks from high-resolution bins,
// preferring local maxima so wide stations do not crowd out weaker signals.
func ExtractTopSignals(startHz, endHz int64, hiBins []float64, limit int) []SignalPeak {
	n := len(hiBins)
	if n == 0 || endHz <= startHz || limit <= 0 {
		return nil
	}
	span := float64(endHz - startHz)
	binFreq := func(i int) int64 {
		return ClampFreq(startHz + int64((float64(i)+0.5)/float64(n)*span))
	}

	type idxPeak struct {
		idx int
		db  float64
	}
	var peaks []idxPeak
	used := make([]bool, n)

	// 1. Collect local maxima first.
	for i := 0; i < n; i++ {
		leftOK := i == 0 || hiBins[i] >= hiBins[i-1]
		rightOK := i == n-1 || hiBins[i] > hiBins[i+1]
		if i == n-1 && n > 1 {
			rightOK = hiBins[i] >= hiBins[i-1]
		}
		if leftOK && rightOK {
			peaks = append(peaks, idxPeak{idx: i, db: hiBins[i]})
			used[i] = true
		}
	}
	sort.Slice(peaks, func(i, j int) bool {
		if peaks[i].db != peaks[j].db {
			return peaks[i].db > peaks[j].db
		}
		return peaks[i].idx < peaks[j].idx
	})

	// 2. If fewer than limit local maxima exist on a small grid, fill from strongest remaining bins.
	if len(peaks) < limit && len(peaks) < n {
		var rest []idxPeak
		for i := 0; i < n; i++ {
			if !used[i] {
				rest = append(rest, idxPeak{idx: i, db: hiBins[i]})
			}
		}
		sort.Slice(rest, func(i, j int) bool {
			return rest[i].db > rest[j].db
		})
		for _, r := range rest {
			if len(peaks) >= limit {
				break
			}
			peaks = append(peaks, r)
		}
	}

	if len(peaks) > limit {
		peaks = peaks[:limit]
	}
	out := make([]SignalPeak, len(peaks))
	for i, p := range peaks {
		out[i] = SignalPeak{
			FreqHz:  binFreq(p.idx),
			PowerDB: p.db,
		}
	}
	return out
}

// BuildSpectrumFrame summarizes bin powers, locates the strongest peak, and populates TopSignals.
func BuildSpectrumFrame(startHz, endHz int64, bins []float64) SpectrumFrame {
	return BuildSpectrumFrameHiRes(startHz, endHz, bins, bins)
}

// BuildSpectrumFrameHiRes computes TopSignals from hiBins (for pinpoint frequency accuracy)
// while keeping downsampled dispBins for rendering.
func BuildSpectrumFrameHiRes(startHz, endHz int64, dispBins, hiBins []float64) SpectrumFrame {
	if len(dispBins) == 0 {
		return SpectrumFrame{StartHz: startHz, EndHz: endHz, CenterHz: (startHz + endHz) / 2}
	}
	if len(hiBins) == 0 {
		hiBins = dispBins
	}
	top := ExtractTopSignals(startHz, endHz, hiBins, MaxTopSignals)
	var peakHz int64
	var peakDB float64
	if len(top) > 0 {
		peakHz = top[0].FreqHz
		peakDB = top[0].PowerDB
	} else {
		peakHz = (startHz + endHz) / 2
		peakDB = dispBins[0]
	}
	sumDB := 0.0
	for _, v := range dispBins {
		sumDB += v
	}
	return SpectrumFrame{
		StartHz:    startHz,
		EndHz:      endHz,
		CenterHz:   (startHz + endHz) / 2,
		Bins:       dispBins,
		TopSignals: top,
		PeakHz:     ClampFreq(peakHz),
		PeakDB:     peakDB,
		FloorDB:    sumDB / float64(len(dispBins)),
	}
}

// BinFreqHz maps bin index idx in [0, totalBins-1] to its center frequency in Hz.
func (f SpectrumFrame) BinFreqHz(idx int) int64 {
	if len(f.Bins) == 0 || f.EndHz <= f.StartHz {
		return f.CenterHz
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(f.Bins) {
		idx = len(f.Bins) - 1
	}
	span := float64(f.EndHz - f.StartHz)
	hz := f.StartHz + int64((float64(idx)+0.5)/float64(len(f.Bins))*span)
	return ClampFreq(hz)
}

// RunSpectrum streams raw IQ from rtl_sdr and emits FFT frames until ctx is canceled.
func RunSpectrum(ctx context.Context, cfg SpectrumConfig, onFrame func(SpectrumFrame)) error {
	rate := cfg.SampleRate
	if rate <= 0 {
		rate = DefaultSpecRate
	}
	center := ClampFreq(cfg.CenterHz)
	startHz := ClampFreq(center - rate/2)
	endHz := ClampFreq(center + rate/2)
	return runNarrowFFTSweep(ctx, cfg.DeviceIndex, startHz, endHz, rate, cfg.GainDB, cfg.OutBins, cfg.FFTSize, SweepRefresh, onFrame)
}

// RunSweep streams a continuous frequency sweep across [StartHz, EndHz], emitting updated
// SpectrumFrame values (both bargraph bins and top 50 signal peaks) every 0.5s.
func RunSweep(ctx context.Context, cfg SweepConfig, onFrame func(SpectrumFrame)) error {
	startHz := ClampFreq(cfg.StartHz)
	endHz := ClampFreq(cfg.EndHz)
	if endHz <= startHz {
		return errors.New("end frequency must exceed start frequency")
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = SweepRefresh
	}
	outBins := cfg.OutBins
	if outBins <= 0 {
		outBins = 34
	}
	span := endHz - startHz
	if span <= 2_400_000 {
		rate := int64(2_048_000)
		if span > 2_048_000 {
			rate = 2_400_000
		}
		return runNarrowFFTSweep(ctx, cfg.DeviceIndex, startHz, endHz, rate, cfg.GainDB, outBins, DefaultFFTSize, interval, onFrame)
	}
	return runWidePowerSweep(ctx, cfg.DeviceIndex, startHz, endHz, cfg.GainDB, outBins, interval, onFrame)
}

func runNarrowFFTSweep(ctx context.Context, devIdx int, startHz, endHz, rate int64, gainDB, outBins, fftSize int, interval time.Duration, onFrame func(SpectrumFrame)) error {
	rtlsdr := FindTool("rtl_sdr")
	if rtlsdr == "" {
		return errors.New("rtl_sdr not installed (apt install rtl-sdr)")
	}
	if fftSize <= 0 {
		fftSize = DefaultFFTSize
	}
	if outBins <= 0 {
		outBins = 34
	}
	if interval <= 0 {
		interval = SweepRefresh
	}
	center := ClampFreq((startHz + endHz) / 2)

	args := []string{
		"-d", strconv.Itoa(max(0, devIdx)),
		"-f", strconv.FormatInt(center, 10),
		"-s", strconv.FormatInt(rate, 10),
	}
	if gainDB >= 0 {
		args = append(args, "-g", strconv.Itoa(gainDB))
	}
	args = append(args, "-")

	release, err := acquireUSB(ctx)
	if err != nil {
		return err
	}
	defer release()

	DetachDVBDriverIfNeeded(DiscoverUSBDevices())

	cmd := exec.CommandContext(ctx, rtlsdr, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	cmd.WaitDelay = 1500 * time.Millisecond

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	var errMu sync.Mutex
	var lastErrLine string
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rtl_sdr: %w", err)
	}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			errMu.Lock()
			lastErrLine = line
			errMu.Unlock()
		}
	}()

	iqChunk := make([]byte, fftSize*2*16)
	// Buffer the last ~64 chunks of IQ (≈65 KB) so each frame averages a few
	// hundred FFTs while staying cheap on the CM0.
	maxAcc := fftSize * 2 * 64
	acc := make([]byte, 0, maxAcc)
	fullStart := center - rate/2
	fullEnd := center + rate/2
	lastEmit := time.Time{}

	for {
		if _, err := io.ReadFull(stdout, iqChunk); err != nil {
			break
		}
		acc = append(acc, iqChunk...)
		if len(acc) > maxAcc {
			keep := append([]byte(nil), acc[len(acc)-maxAcc:]...)
			acc = keep
		}
		now := time.Now()
		if lastEmit.IsZero() || now.Sub(lastEmit) >= interval {
			full := computeFullSpectrumDB(acc, fftSize)
			hiBins := cropBins(full, fullStart, fullEnd, startHz, endHz)
			disp := poolBinsMax(hiBins, outBins)
			frame := BuildSpectrumFrameHiRes(startHz, endHz, disp, hiBins)
			if onFrame != nil {
				onFrame(frame)
			}
			lastEmit = now
		}
		if ctx.Err() != nil {
			break
		}
	}

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		errMu.Lock()
		msg := lastErrLine
		errMu.Unlock()
		if msg != "" {
			return errors.New(humanizeRTLErr(msg))
		}
		return waitErr
	}
	return nil
}

func cropBins(full []float64, fullStart, fullEnd, wantStart, wantEnd int64) []float64 {
	n := len(full)
	if n == 0 || fullEnd <= fullStart {
		return full
	}
	if wantStart <= fullStart && wantEnd >= fullEnd {
		return full
	}
	lo := int(float64(wantStart-fullStart) / float64(fullEnd-fullStart) * float64(n))
	hi := int(math.Ceil(float64(wantEnd-fullStart) / float64(fullEnd-fullStart) * float64(n)))
	if lo < 0 {
		lo = 0
	}
	if hi > n {
		hi = n
	}
	if hi <= lo {
		hi = min(n, lo+1)
	}
	return full[lo:hi]
}

// sweepStepKHz picks a sensible bin width for rtl_power so sweeps stay responsive.
func sweepStepKHz(spanHz int64) int {
	switch {
	case spanHz <= 5_000_000:
		return 15
	case spanHz <= 25_000_000:
		return 50
	case spanHz <= 100_000_000:
		return 100
	default:
		return 250
	}
}

func runWidePowerSweep(ctx context.Context, devIdx int, startHz, endHz int64, gainDB, outBins int, interval time.Duration, onFrame func(SpectrumFrame)) error {
	rtlpow := FindTool("rtl_power")
	if rtlpow == "" {
		return errors.New("rtl_power not installed (apt install rtl-sdr)")
	}
	if interval <= 0 {
		interval = SweepRefresh
	}
	if outBins <= 0 {
		outBins = 34
	}

	release, err := acquireUSB(ctx)
	if err != nil {
		return err
	}
	defer release()

	DetachDVBDriverIfNeeded(DiscoverUSBDevices())

	stepK := sweepStepKHz(endHz - startHz)
	freqSpec := fmt.Sprintf("%.3fM:%.3fM:%dk", float64(startHz)/1e6, float64(endHz)/1e6, stepK)
	rtlArgs := []string{
		"-d", strconv.Itoa(max(0, devIdx)),
		"-f", freqSpec,
		"-i", "1",
	}
	if gainDB >= 0 {
		rtlArgs = append(rtlArgs, "-g", strconv.Itoa(gainDB))
	}
	rtlArgs = append(rtlArgs, "-")

	binPath := rtlpow
	cmdArgs := rtlArgs
	if stdbuf := FindTool("stdbuf"); stdbuf != "" {
		binPath = stdbuf
		cmdArgs = append([]string{"-oL", rtlpow}, rtlArgs...)
	}

	cmd := exec.CommandContext(ctx, binPath, cmdArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	cmd.WaitDelay = 1500 * time.Millisecond

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	var errMu sync.Mutex
	var lastErrLine string
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rtl_power: %w", err)
	}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			errMu.Lock()
			lastErrLine = line
			errMu.Unlock()
		}
	}()

	// Maintain a high-resolution grid across [startHz, endHz] updated as hop rows arrive.
	hiCount := 512
	hiBins := make([]float64, hiCount)
	hasVal := make([]bool, hiCount)
	for i := range hiBins {
		hiBins[i] = -75.0
	}
	var gridMu sync.Mutex
	dirty := false

	// Emit updated frames every 0.5s while rtl_power streams hop rows.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	doneScan := make(chan struct{})

	go func() {
		defer close(doneScan)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 16*1024), 512*1024)
		for sc.Scan() {
			lo, hi, vals, ok := parseRTLPowerRow(sc.Text())
			if !ok {
				continue
			}
			gridMu.Lock()
			mergePowerRow(hiBins, hasVal, float64(startHz), float64(endHz), lo, hi, vals)
			dirty = true
			gridMu.Unlock()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			_ = cmd.Wait()
			return ctx.Err()
		case <-doneScan:
			gridMu.Lock()
			if dirty {
				filled := fillPowerGaps(hiBins, hasVal)
				disp := poolBinsMax(filled, outBins)
				if onFrame != nil {
					onFrame(BuildSpectrumFrameHiRes(startHz, endHz, disp, filled))
				}
			}
			gridMu.Unlock()
			waitErr := cmd.Wait()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if waitErr != nil {
				errMu.Lock()
				msg := lastErrLine
				errMu.Unlock()
				if msg != "" {
					return errors.New(humanizeRTLErr(msg))
				}
				return waitErr
			}
			return nil
		case <-ticker.C:
			gridMu.Lock()
			if dirty {
				filled := fillPowerGaps(hiBins, hasVal)
				disp := poolBinsMax(filled, outBins)
				frame := BuildSpectrumFrameHiRes(startHz, endHz, disp, filled)
				dirty = false
				gridMu.Unlock()
				if onFrame != nil {
					onFrame(frame)
				}
			} else {
				gridMu.Unlock()
			}
		}
	}
}

func parseRTLPowerRow(raw string) (lo, hi float64, vals []float64, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return 0, 0, nil, false
	}
	cols := strings.Split(line, ",")
	if len(cols) < 7 {
		return 0, 0, nil, false
	}
	lo, err1 := strconv.ParseFloat(strings.TrimSpace(cols[2]), 64)
	hi, err2 := strconv.ParseFloat(strings.TrimSpace(cols[3]), 64)
	if err1 != nil || err2 != nil || hi <= lo {
		return 0, 0, nil, false
	}
	vals = make([]float64, 0, len(cols)-6)
	for _, c := range cols[6:] {
		v, err := strconv.ParseFloat(strings.TrimSpace(c), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		vals = append(vals, v)
	}
	if len(vals) == 0 {
		return 0, 0, nil, false
	}
	return lo, hi, vals, true
}

func mergePowerRow(grid []float64, hasVal []bool, minHz, maxHz, lo, hi float64, vals []float64) {
	n := len(grid)
	if n == 0 || maxHz <= minHz {
		return
	}
	// Reset bins inside [lo, hi] for this new hop reading so values refresh rather than latching forever.
	loPos := int(((lo - minHz) / (maxHz - minHz)) * float64(n))
	hiPos := int(math.Ceil(((hi - minHz) / (maxHz - minHz)) * float64(n)))
	if loPos < 0 {
		loPos = 0
	}
	if hiPos > n {
		hiPos = n
	}
	for p := loPos; p < hiPos; p++ {
		hasVal[p] = false
	}

	step := (hi - lo) / float64(len(vals))
	for i, v := range vals {
		f := lo + (float64(i)+0.5)*step
		if f < minHz || f > maxHz {
			continue
		}
		pos := int(((f - minHz) / (maxHz - minHz)) * float64(n))
		if pos < 0 {
			pos = 0
		}
		if pos >= n {
			pos = n - 1
		}
		if !hasVal[pos] || v > grid[pos] {
			grid[pos] = v
			hasVal[pos] = true
		}
	}
}

func fillPowerGaps(grid []float64, hasVal []bool) []float64 {
	out := make([]float64, len(grid))
	last := -75.0
	for i, v := range grid {
		if hasVal[i] {
			last = v
		}
		out[i] = last
	}
	return out
}

// ParseRTLPowerCSV parses one rtl_power CSV sweep into a unified frame with TopSignals.
func ParseRTLPowerCSV(csvText string, outBins int) (SpectrumFrame, error) {
	if outBins <= 0 {
		outBins = 34
	}
	type seg struct {
		lo, hi float64
		vals   []float64
	}
	var segs []seg
	minHz := math.MaxFloat64
	maxHz := 0.0

	for _, raw := range strings.Split(csvText, "\n") {
		lo, hi, vals, ok := parseRTLPowerRow(raw)
		if !ok {
			continue
		}
		if lo < minHz {
			minHz = lo
		}
		if hi > maxHz {
			maxHz = hi
		}
		segs = append(segs, seg{lo: lo, hi: hi, vals: vals})
	}
	if len(segs) == 0 || maxHz <= minHz {
		return SpectrumFrame{}, errors.New("no spectrum data from rtl_power")
	}

	hiCount := 512
	hiBins := make([]float64, hiCount)
	hasVal := make([]bool, hiCount)
	for _, s := range segs {
		mergePowerRow(hiBins, hasVal, minHz, maxHz, s.lo, s.hi, s.vals)
	}
	filled := fillPowerGaps(hiBins, hasVal)
	disp := poolBinsMax(filled, outBins)
	return BuildSpectrumFrameHiRes(int64(math.Round(minHz)), int64(math.Round(maxHz)), disp, filled), nil
}

// RunBandSweep executes a single-shot rtl_power sweep for the given band preset.
func RunBandSweep(ctx context.Context, devIdx int, band BandPreset, gainDB, outBins int) (SpectrumFrame, error) {
	rtlpow := FindTool("rtl_power")
	if rtlpow == "" {
		return SpectrumFrame{}, errors.New("rtl_power not installed (apt install rtl-sdr)")
	}
	release, err := acquireUSB(ctx)
	if err != nil {
		return SpectrumFrame{}, err
	}
	defer release()

	DetachDVBDriverIfNeeded(DiscoverUSBDevices())

	freqSpec := fmt.Sprintf("%.3fM:%.3fM:%dk", band.StartMHz, band.EndMHz, max(5, band.StepKHz))
	args := []string{
		"-d", strconv.Itoa(max(0, devIdx)),
		"-f", freqSpec,
		"-i", "1",
		"-1",
	}
	if gainDB >= 0 {
		args = append(args, "-g", strconv.Itoa(gainDB))
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, rtlpow, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return SpectrumFrame{}, ctx.Err()
	}
	frame, perr := ParseRTLPowerCSV(string(out), outBins)
	if perr == nil {
		return frame, nil
	}
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			return SpectrumFrame{}, errors.New(humanizeRTLErr(strings.TrimSpace(lines[len(lines)-1])))
		}
		return SpectrumFrame{}, err
	}
	return SpectrumFrame{}, perr
}
