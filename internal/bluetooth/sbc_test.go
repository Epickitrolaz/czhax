package bluetooth

import (
	"testing"
)

func TestBuildHIDReportMapAndKeyMapping(t *testing.T) {
	kb := BuildHIDReportMap(true, false, false, false)
	if len(kb) < 40 {
		t.Fatalf("unexpected short keyboard report map: %d", len(kb))
	}
	combo := BuildHIDReportMap(true, true, true, false)
	if len(combo) <= len(kb) {
		t.Fatalf("expected combo report map > keyboard (%d <= %d)", len(combo), len(kb))
	}

	c, m, ok := KeyToHID("a")
	if !ok || c != 0x04 || m != 0 {
		t.Fatalf("KeyToHID('a') = (%#x, %#x, %v)", c, m, ok)
	}
	c, m, ok = KeyToHID("A")
	if !ok || c != 0x04 || m != HIDModLShift {
		t.Fatalf("KeyToHID('A') = (%#x, %#x, %v)", c, m, ok)
	}
	c, m, ok = KeyToHID("ctrl+c")
	if !ok || c != 0x06 || m != HIDModLCtrl {
		t.Fatalf("KeyToHID('ctrl+c') = (%#x, %#x, %v)", c, m, ok)
	}
	c, m, ok = KeyToHID("enter")
	if !ok || c != 0x28 || m != 0 {
		t.Fatalf("KeyToHID('enter') = (%#x, %#x, %v)", c, m, ok)
	}
}

func TestSBCDecoderSilentFrame(t *testing.T) {
	// Construct a valid 44.1kHz, 4-block, Mono, Loudness, 4-subband, bitpool=16 SBC frame
	// with zero scale factors (valid silence frame).
	// Header: sync=0x9C, flags: freq=2(44.1k)->0x80, blocks=0(4)->0x00, mode=0(mono)->0x00, alloc=0(loud)->0x00, sb=0(4)->0x00 => 0x80
	// bitpool=16 (0x10).
	// Total bits = 32 + 4*4*1 + 4*1*16 = 32 + 16 + 64 = 112 bits = 14 bytes.
	frame := make([]byte, 14)
	frame[0] = 0x9C
	frame[1] = 0x80
	frame[2] = 0x10
	frame[3] = sbcCRC8(frame[1:], 16+4*4)

	var dec SBCDecoder
	pcm, info, err := dec.DecodeFrame(frame)
	if err != nil {
		t.Fatalf("DecodeFrame failed: %v", err)
	}
	if info.SampleRate != 44100 || info.Channels != 1 || info.Blocks != 4 || info.Subbands != 4 || info.FrameBytes != 14 {
		t.Fatalf("unexpected SBCFrameInfo: %+v", info)
	}
	if len(pcm) != 4*4*1*2 {
		t.Fatalf("pcm bytes = %d, want %d", len(pcm), 4*4*1*2)
	}

	// Wrap in RTP packet (12-byte RTP + 1-byte SBC payload header = 13 bytes)
	pkt := make([]byte, 13+len(frame))
	pkt[0] = 0x80 // RTP v2
	pkt[1] = 96
	pkt[12] = 0x01 // 1 SBC frame
	copy(pkt[13:], frame)

	rpcm, rinfo, err := dec.DecodeRTP(pkt)
	if err != nil {
		t.Fatalf("DecodeRTP failed: %v", err)
	}
	if rinfo.SampleRate != 44100 || len(rpcm) != len(pcm) {
		t.Fatalf("unexpected RTP decode result: info=%+v len=%d", rinfo, len(rpcm))
	}
}
