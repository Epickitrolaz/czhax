package ir

import (
	"fmt"
	"strings"
)

// EncodeProtocol converts a Flipper parsed entry into raw timings.
func EncodeProtocol(protocol string, address, command uint32) (Signal, error) {
	name := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(protocol), " ", ""))
	sig := Signal{Protocol: protocol, Address: address, Command: command}
	var err error
	switch name {
	case "NEC":
		sig.Timings = encodeNEC(address, command)
	case "NECEXT":
		sig.Timings = encodeNECext(address, command)
	case "NEC42":
		sig.Timings = encodeNEC42(address, command)
	case "NEC42EXT":
		sig.Timings = encodeNEC42ext(address, command)
	case "SAMSUNG", "SAMSUNG32":
		protocol = "Samsung32"
		sig.Timings = encodeSamsung32(address, command)
	case "RC5":
		sig.Timings = encodeRC5(address, command, false)
		sig.Frequency = 36000
	case "RC5X":
		sig.Timings = encodeRC5(address, command, true)
		sig.Frequency = 36000
	case "RC6":
		sig.Timings = encodeRC6(address, command)
		sig.Frequency = 36000
	case "SIRC", "SIRC15", "SIRC20":
		sig.Timings = encodeSIRC(address, command, name)
		sig.Frequency = 40000
	case "RCA":
		sig.Timings = encodeRCA(address, command)
	case "KASEIKYO":
		sig.Timings = encodeKaseikyo(address, command)
	default:
		err = fmt.Errorf("unsupported IR protocol %q", protocol)
	}
	if err != nil {
		return Signal{}, err
	}
	sig.Frequency = defaultIfZero(sig.Frequency, DefaultFrequency)
	sig.DutyCycle = DefaultDutyCycle
	sig.Timings = normalizeTimings(sig.Timings)
	return sig, nil
}

func defaultIfZero(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func bitsLSB(v uint64, n int) []int {
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[i] = int((v >> i) & 1)
	}
	return out
}

func bitsMSB(v uint64, n int) []int {
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[i] = int((v >> (n - 1 - i)) & 1)
	}
	return out
}

// pdwmFrame builds a pulse-distance frame with a trailing stop mark so every
// bit's space is preserved and the frame ends on a pulse for LIRC_MODE_PULSE.
func pdwmFrame(preMark, preSpace, oneMark, zeroMark, oneSpace, zeroSpace int, bits []int) []int {
	out := make([]int, 0, 3+len(bits)*2)
	out = append(out, preMark, preSpace)
	for _, b := range bits {
		mark := zeroMark
		space := zeroSpace
		if b == 1 {
			mark = oneMark
			space = oneSpace
		}
		out = append(out, mark, space)
	}
	out = append(out, zeroMark)
	return out
}

func encodeNEC(address, command uint32) []int {
	addr := uint8(address)
	cmd := uint8(command)
	data := uint32(addr) |
		uint32(^addr)<<8 |
		uint32(cmd)<<16 |
		uint32(^cmd)<<24
	return pdwmFrame(9000, 4500, 560, 560, 1690, 560, bitsLSB(uint64(data), 32))
}

func encodeNECext(address, command uint32) []int {
	data := uint64(address&0xffff) | uint64(command&0xffff)<<16
	return pdwmFrame(9000, 4500, 560, 560, 1690, 560, bitsLSB(data, 32))
}

func encodeNEC42(address, command uint32) []int {
	addr := address & 0x1fff
	cmd := command & 0xff
	data1 := addr | (^addr&0x1fff)<<13 | (cmd&0x3f)<<26
	data2 := (cmd&0xc0)>>6 | (^cmd&0xff)<<2
	bits := append(bitsLSB(uint64(data1), 32), bitsLSB(uint64(data2), 10)...)
	return pdwmFrame(9000, 4500, 560, 560, 1690, 560, bits)
}

func encodeNEC42ext(address, command uint32) []int {
	bits := append(bitsLSB(uint64(address&0x3ffffff), 26), bitsLSB(uint64(command&0xffff), 16)...)
	return pdwmFrame(9000, 4500, 560, 560, 1690, 560, bits)
}

func encodeSamsung32(address, command uint32) []int {
	addr := uint8(address)
	cmd := uint8(command)
	data := uint32(addr) | uint32(addr)<<8 | uint32(cmd)<<16 | uint32(^cmd)<<24
	return pdwmFrame(4500, 4500, 550, 550, 1650, 550, bitsLSB(uint64(data), 32))
}

// manchester builds a bi-phase mark/space sequence, merging adjacent levels
// when two bits produce the same level back to back. The first bit must be 1
// (RC5 and RC6 start with a mark).
func manchester(bits []int, half int, doubleAt int) []int {
	type segment struct {
		mark bool
		dur  int
	}
	var segs []segment
	add := func(mark bool, dur int) {
		if dur <= 0 {
			return
		}
		if n := len(segs); n > 0 && segs[n-1].mark == mark {
			segs[n-1].dur += dur
			return
		}
		segs = append(segs, segment{mark: mark, dur: dur})
	}
	for i, b := range bits {
		d := half
		if i == doubleAt {
			d = half * 2
		}
		if b == 1 {
			add(true, d)
			add(false, d)
		} else {
			add(false, d)
			add(true, d)
		}
	}
	if len(segs) == 0 || !segs[0].mark {
		return nil
	}
	if !segs[len(segs)-1].mark {
		// End on a pulse for LIRC_MODE_PULSE; the transmitter idles off afterwards.
		segs = segs[:len(segs)-1]
	}
	if len(segs) == 0 {
		return nil
	}
	out := make([]int, 0, len(segs))
	for _, s := range segs {
		out = append(out, s.dur)
	}
	return out
}

func encodeRC5(address, command uint32, extended bool) []int {
	bits := []int{1}
	if extended {
		bits = append(bits, int((^command>>6)&1))
	} else {
		bits = append(bits, 1)
	}
	bits = append(bits, 0) // toggle bit, fixed for a single press
	bits = append(bits, bitsMSB(uint64(address&0x1f), 5)...)
	bits = append(bits, bitsMSB(uint64(command&0x3f), 6)...)
	return manchester(bits, 888, -1)
}

func encodeRC6(address, command uint32) []int {
	bits := []int{1}             // start
	bits = append(bits, 0, 0, 0) // mode 0
	bits = append(bits, 0)       // toggle bit
	bits = append(bits, bitsMSB(uint64(address&0xff), 8)...)
	bits = append(bits, bitsMSB(uint64(command&0xff), 8)...)
	frame := manchester(bits, 444, 4)
	if len(frame) == 0 {
		return nil
	}
	return append([]int{2666, 889}, frame...)
}

func encodeSIRC(address, command uint32, variant string) []int {
	addrBits := 5
	switch variant {
	case "SIRC15":
		addrBits = 8
	case "SIRC20":
		addrBits = 13
	}
	bits := append(bitsLSB(uint64(command&0x7f), 7), bitsLSB(uint64(address)&(1<<addrBits-1), addrBits)...)
	frame := make([]int, 0, 2+len(bits)*2)
	frame = append(frame, 2400, 600)
	for _, b := range bits {
		mark := 600
		if b == 1 {
			mark = 1200
		}
		frame = append(frame, mark, 600)
	}
	// Sony devices expect a minimum of three repeats separated by a ~25ms gap,
	// ending on the final bit's mark for LIRC_MODE_PULSE.
	out := make([]int, 0, len(frame)*3-1)
	for i := 0; i < 3; i++ {
		if i < 2 {
			out = append(out, frame[:len(frame)-1]...)
			out = append(out, 25000)
		} else {
			out = append(out, frame[:len(frame)-1]...)
		}
	}
	return out
}

func encodeRCA(address, command uint32) []int {
	addr := uint8(address) & 0xf
	cmd := uint8(command)
	data := uint64(addr) | uint64(cmd)<<4 | uint64(^addr&0xf)<<12 | uint64(^cmd)<<16
	return pdwmFrame(4000, 4000, 500, 500, 2000, 1000, bitsLSB(data, 24))
}

func encodeKaseikyo(address, command uint32) []int {
	id := (address >> 24) & 3
	vendor := (address >> 8) & 0xffff
	genre1 := (address >> 4) & 0xf
	genre2 := address & 0xf
	b0 := uint8(vendor & 0xff)
	b1 := uint8(vendor >> 8)
	parity := (b0 ^ b1)
	parity = (parity & 0xf) ^ (parity >> 4)
	b2 := (parity & 0xf) | uint8(genre1<<4)
	b3 := uint8(genre2&0xf) | uint8((command&0xf)<<4)
	b4 := uint8(id<<6) | uint8((command>>4)&0x3f)
	b5 := b2 ^ b3 ^ b4
	value := uint64(b0) | uint64(b1)<<8 | uint64(b2)<<16 | uint64(b3)<<24 | uint64(b4)<<32 | uint64(b5)<<40
	return pdwmFrame(3456, 1728, 432, 432, 1296, 432, bitsLSB(value, 48))
}
