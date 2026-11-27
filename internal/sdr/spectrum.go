package sdr

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultSpecRate int64 = 2_048_000
	DefaultFFTSize  int   = 256
)

// SpectrumConfig configures live rtl_sdr FFT streaming.
type SpectrumConfig struct {
	DeviceIndex int
	CenterHz    int64
	SampleRate  int64
	GainDB      int // -1 = Auto
	OutBins     int // e.g. 34..44 to match pane width
	FFTSize     int // power of 2, default 256
}

// SpectrumFrame holds one computed power spectrum slice in dBFS.
type SpectrumFrame struct {
	StartHz  int64
	EndHz    int64
	CenterHz int64
	Bins     []float64 // dBFS values across [StartHz, EndHz]
	PeakHz   int64
	PeakDB   float64
	FloorDB  float64
}

// BandPreset defines a wide-band sweep range for rtl_power.
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
	// Bit-reversal permutation.
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
	// Butterfly stages.
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

// ComputeSpectrum transforms interleaved unsigned 8-bit I/Q samples into outBins dBFS values
// ordered from -Fs/2 to +Fs/2, with center DC spike suppression and max-pooling.
func ComputeSpectrum(iq []byte, fftSize, outBins int) []float64 {
	if fftSize < 16 || (fftSize&(fftSize-1)) != 0 {
		fftSize = DefaultFFTSize
	}
	if outBins <= 0 {
		outBins = 34
	}
	bytesPerFrame := fftSize * 2
	frames := len(iq) / bytesPerFrame
	if frames == 0 {
		out := make([]float64, outBins)
		for i := range out {
			out[i] = -75
		}
		return out
	}

	// Precompute Hann window.
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
		// Subtract chunk mean to reduce static DC offset before windowing.
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

		// FFT-shift into acc[0..fftSize-1] from -Fs/2 to +Fs/2.
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
	// Suppress residual DC spike at center bin.
	mid := fftSize / 2
	if mid > 1 && mid+1 < fftSize {
		acc[mid] = 0.5 * (acc[mid-1] + acc[mid+1])
	}

	// Max-pool fftSize bins into outBins columns so narrow peaks stay visible.
	out := make([]float64, outBins)
	for b := 0; b < outBins; b++ {
		start := (b * fftSize) / outBins
		end := ((b + 1) * fftSize) / outBins
		if end <= start {
			end = start + 1
		}
		if end > fftSize {
			end = fftSize
		}
		peak := acc[start]
		for k := start + 1; k < end; k++ {
			if acc[k] > peak {
				peak = acc[k]
			}
		}
		db := -80.0
		if peak > 1e-12 {
			db = 10.0 * math.Log10(peak)
		}
		if db < -80.0 {
			db = -80.0
		}
		if db > 0.0 {
			db = 0.0
		}
		out[b] = db
	}
	return out
}

// BuildSpectrumFrame summarizes bin powers and locates the strongest peak frequency.
func BuildSpectrumFrame(startHz, endHz int64, bins []float64) SpectrumFrame {
	if len(bins) == 0 {
		return SpectrumFrame{StartHz: startHz, EndHz: endHz, CenterHz: (startHz + endHz) / 2}
	}
	peakIdx := 0
	peakDB := bins[0]
	sumDB := 0.0
	for i, v := range bins {
		if v > peakDB {
			peakDB = v
			peakIdx = i
		}
		sumDB += v
	}
	span := float64(endHz - startHz)
	peakHz := startHz + int64((float64(peakIdx)+0.5)/float64(len(bins))*span)
	return SpectrumFrame{
		StartHz:  startHz,
		EndHz:    endHz,
		CenterHz: (startHz + endHz) / 2,
		Bins:     bins,
		PeakHz:   ClampFreq(peakHz),
		PeakDB:   peakDB,
		FloorDB:  sumDB / float64(len(bins)),
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
	rtlsdr := FindTool("rtl_sdr")
	if rtlsdr == "" {
		return errors.New("rtl_sdr not installed (apt install rtl-sdr)")
	}
	rate := cfg.SampleRate
	if rate <= 0 {
		rate = DefaultSpecRate
	}
	fftSize := cfg.FFTSize
	if fftSize <= 0 {
		fftSize = DefaultFFTSize
	}
	outBins := cfg.OutBins
	if outBins <= 0 {
		outBins = 34
	}
	center := ClampFreq(cfg.CenterHz)

	args := []string{
		"-d", strconv.Itoa(max(0, cfg.DeviceIndex)),
		"-f", strconv.FormatInt(center, 10),
		"-s", strconv.FormatInt(rate, 10),
	}
	if cfg.GainDB >= 0 {
		args = append(args, "-g", strconv.Itoa(cfg.GainDB))
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

	// 16 averaged FFT blocks per read (~8 KB at fftSize=256).
	iqChunk := make([]byte, fftSize*2*16)
	discard := make([]byte, 32*1024)
	startHz := center - rate/2
	endHz := center + rate/2
	lastEmit := time.Time{}

	for {
		if _, err := io.ReadFull(stdout, iqChunk); err != nil {
			break
		}
		now := time.Now()
		if lastEmit.IsZero() || now.Sub(lastEmit) >= 95*time.Millisecond {
			bins := ComputeSpectrum(iqChunk, fftSize, outBins)
			frame := BuildSpectrumFrame(startHz, endHz, bins)
			if onFrame != nil {
				onFrame(frame)
			}
			lastEmit = now
		} else {
			// Drain at most one small chunk to keep stream moving without CPU spike.
			_, _ = stdout.Read(discard[:len(iqChunk)])
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

// ParseRTLPowerCSV parses one rtl_power -1 CSV sweep into a unified outBins frame.
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
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, ",")
		if len(cols) < 7 {
			continue
		}
		lo, err1 := strconv.ParseFloat(strings.TrimSpace(cols[2]), 64)
		hi, err2 := strconv.ParseFloat(strings.TrimSpace(cols[3]), 64)
		if err1 != nil || err2 != nil || hi <= lo {
			continue
		}
		vals := make([]float64, 0, len(cols)-6)
		for _, c := range cols[6:] {
			v, err := strconv.ParseFloat(strings.TrimSpace(c), 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			vals = append(vals, v)
		}
		if len(vals) == 0 {
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

	bins := make([]float64, outBins)
	hasVal := make([]bool, outBins)
	for _, s := range segs {
		step := (s.hi - s.lo) / float64(len(s.vals))
		for i, v := range s.vals {
			f := s.lo + (float64(i)+0.5)*step
			pos := int(((f - minHz) / (maxHz - minHz)) * float64(outBins))
			if pos < 0 {
				pos = 0
			}
			if pos >= outBins {
				pos = outBins - 1
			}
			if !hasVal[pos] || v > bins[pos] {
				bins[pos] = v
				hasVal[pos] = true
			}
		}
	}
	// Fill any unpopulated gap bins by linear neighbor carry.
	last := -75.0
	for i := range bins {
		if hasVal[i] {
			last = bins[i]
		} else {
			bins[i] = last
		}
	}
	return BuildSpectrumFrame(int64(math.Round(minHz)), int64(math.Round(maxHz)), bins), nil
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
