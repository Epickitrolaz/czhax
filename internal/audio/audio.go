package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// RecordMic captures mono 16-bit PCM from the default input into path (WAV).
func RecordMic(ctx context.Context, path string, onLine func(string)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	type candidate struct {
		bin  string
		args []string
	}
	var candidates []candidate
	if _, err := exec.LookPath("arecord"); err == nil {
		candidates = append(candidates, candidate{"arecord", []string{"-q", "-f", "S16_LE", "-r", "44100", "-c", "1", "-t", "wav", path}})
	}
	if _, err := exec.LookPath("parecord"); err == nil {
		candidates = append(candidates, candidate{"parecord", []string{"--format=s16le", "--rate=44100", "--channels=1", "--file-format=wav", path}})
	}
	if len(candidates) == 0 {
		return errors.New("no microphone recorder found (install alsa-utils or pulseaudio-utils)")
	}
	var lastErr error
	for _, c := range candidates {
		if onLine != nil {
			onLine("Recording microphone with " + c.bin)
		}
		err := runRecorder(ctx, c.bin, c.args)
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func runRecorder(ctx context.Context, bin string, args []string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// SIGINT lets arecord/parecord finalize the WAV header.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	}
	cmd.WaitDelay = 3 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s: %s", bin, msg)
		}
		return err
	}
	return nil
}

// ConvertToSCO converts a WAV/MP3/audio file to raw 8 kHz mono signed 16-bit
// little-endian PCM for the SCO audio path.
func ConvertToSCO(ctx context.Context, input, output string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(input); err != nil {
		return err
	}
	if bin, err := exec.LookPath("ffmpeg"); err == nil {
		args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", input, "-ac", "1", "-ar", "8000", "-f", "s16le", output}
		if out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}
	if bin, err := exec.LookPath("sox"); err == nil {
		args := []string{input, "-r", "8000", "-c", "1", "-b", "16", "-e", "signed-integer", "-t", "raw", output}
		if out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("sox: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}
	if strings.EqualFold(filepath.Ext(input), ".wav") {
		return convertWAVNative(input, output)
	}
	return errors.New("ffmpeg or sox is required to play compressed audio")
}

func convertWAVNative(input, output string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return errors.New("not a WAV file")
	}
	var (
		channels uint16
		rate     uint32
		bits     uint16
		format   uint16
		pcm      []byte
		haveFmt  bool
	)
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		body := off + 8
		if body+size > len(data) {
			size = len(data) - body
		}
		if id == "fmt " && size >= 16 {
			format = binary.LittleEndian.Uint16(data[body : body+2])
			channels = binary.LittleEndian.Uint16(data[body+2 : body+4])
			rate = binary.LittleEndian.Uint32(data[body+4 : body+8])
			bits = binary.LittleEndian.Uint16(data[body+14 : body+16])
			haveFmt = true
		} else if id == "data" {
			pcm = data[body : body+size]
		}
		off = body + size
		if size%2 == 1 {
			off++
		}
	}
	if !haveFmt || len(pcm) == 0 {
		return errors.New("WAV has no PCM data")
	}
	if format != 1 && format != 0xFFFE {
		return errors.New("unsupported WAV codec (PCM required)")
	}
	if bits != 16 || channels == 0 || rate == 0 {
		return errors.New("unsupported WAV format (16-bit PCM required)")
	}
	// Downmix to mono.
	mono := make([]int16, len(pcm)/2/int(channels))
	for i := range mono {
		var sum int32
		for c := 0; c < int(channels); c++ {
			idx := (i*int(channels) + c) * 2
			sum += int32(int16(binary.LittleEndian.Uint16(pcm[idx : idx+2])))
		}
		mono[i] = int16(sum / int32(channels))
	}
	// Linear resample to 8 kHz.
	const target = 8000
	outLen := int(float64(len(mono)) * target / float64(rate))
	if outLen <= 0 {
		return errors.New("WAV is too short")
	}
	var out bytes.Buffer
	for i := 0; i < outLen; i++ {
		pos := float64(i) * float64(rate) / target
		i0 := int(pos)
		frac := pos - float64(i0)
		if i0 >= len(mono) {
			i0 = len(mono) - 1
			frac = 0
		}
		i1 := i0 + 1
		if i1 >= len(mono) {
			i1 = i0
		}
		v := float64(mono[i0])*(1-frac) + float64(mono[i1])*frac
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], uint16(int16(v)))
		out.Write(b[:])
	}
	return os.WriteFile(output, out.Bytes(), 0o644)
}

// PlayAudio plays an audio file until completion or ctx cancellation.
// It validates readability up front, tries mpv / ffplay / aplay, and kills the
// process group cleanly when ctx is canceled.
func PlayAudio(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	info, statErr := f.Stat()
	_ = f.Close()
	if statErr == nil && info.Size() == 0 {
		return errors.New("audio file is empty")
	}

	type candidate struct {
		bin  string
		args []string
	}
	var candidates []candidate
	if _, err := exec.LookPath("mpv"); err == nil {
		candidates = append(candidates, candidate{"mpv", []string{"--really-quiet", "--no-video", "--no-terminal", path}})
	}
	if _, err := exec.LookPath("ffplay"); err == nil {
		candidates = append(candidates, candidate{"ffplay", []string{"-loglevel", "error", "-nodisp", "-autoexit", path}})
	}
	if strings.EqualFold(filepath.Ext(path), ".wav") {
		if _, err := exec.LookPath("aplay"); err == nil {
			candidates = append(candidates, candidate{"aplay", []string{"-q", path}})
		}
	}
	if len(candidates) == 0 {
		return errors.New("no audio player found (install mpv, ffmpeg, or alsa-utils)")
	}
	var lastErr error
	for _, c := range candidates {
		err := runPlayer(ctx, c.bin, c.args)
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func runPlayer(ctx context.Context, bin string, args []string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 2 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			// Take the last non-empty line so PipeWire noise doesn't hide the actual error.
			lines := strings.Split(msg, "\n")
			last := strings.TrimSpace(lines[len(lines)-1])
			if last != "" {
				return fmt.Errorf("%s: %s", bin, last)
			}
		}
		return fmt.Errorf("%s: %w", bin, err)
	}
	return nil
}

// PlayerCommand returns an installed external player for audio/video.
func PlayerCommand(path string, video bool) (string, []string, error) {
	if bin, err := exec.LookPath("mpv"); err == nil {
		args := []string{"--really-quiet"}
		if video {
			args = append(args, "--fs")
		} else {
			args = append(args, "--no-video")
		}
		return bin, append(args, path), nil
	}
	if bin, err := exec.LookPath("ffplay"); err == nil {
		args := []string{"-loglevel", "quiet", "-autoexit"}
		if video {
			args = append(args, "-fs")
		} else {
			args = append(args, "-nodisp")
		}
		return bin, append(args, path), nil
	}
	if !video && strings.EqualFold(filepath.Ext(path), ".wav") {
		if bin, err := exec.LookPath("aplay"); err == nil {
			return bin, []string{"-q", path}, nil
		}
	}
	return "", nil, errors.New("no media player found (install mpv or ffmpeg)")
}
