package audio

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestConvertWAVNativeResamples(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.wav")
	out := filepath.Join(dir, "out.raw")
	const rate = 16000
	samples := make([]int16, rate/10) // 100 ms
	for i := range samples {
		samples[i] = int16(i % 1000)
	}
	b := make([]byte, 44)
	copy(b[0:4], "RIFF")
	binary.LittleEndian.PutUint32(b[4:8], uint32(36+len(samples)*2))
	copy(b[8:12], "WAVE")
	copy(b[12:16], "fmt ")
	binary.LittleEndian.PutUint32(b[16:20], 16)
	binary.LittleEndian.PutUint16(b[20:22], 1)
	binary.LittleEndian.PutUint16(b[22:24], 1)
	binary.LittleEndian.PutUint32(b[24:28], rate)
	binary.LittleEndian.PutUint32(b[28:32], rate*2)
	binary.LittleEndian.PutUint16(b[32:34], 2)
	binary.LittleEndian.PutUint16(b[34:36], 16)
	copy(b[36:40], "data")
	binary.LittleEndian.PutUint32(b[40:44], uint32(len(samples)*2))
	for _, s := range samples {
		var p [2]byte
		binary.LittleEndian.PutUint16(p[:], uint16(s))
		b = append(b, p[:]...)
	}
	if err := os.WriteFile(in, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := convertWAVNative(in, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(samples)*2/2 {
		t.Fatalf("resampled bytes = %d, want %d", len(data), len(samples)*2/2)
	}
}

func TestPlayAudioValidatesFile(t *testing.T) {
	if err := PlayAudio(context.Background(), filepath.Join(t.TempDir(), "missing.wav")); err == nil {
		t.Fatal("expected error for missing file")
	}
	empty := filepath.Join(t.TempDir(), "empty.wav")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PlayAudio(context.Background(), empty); err == nil {
		t.Fatal("expected error for empty file")
	}
}
