package power

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	gpioGetChipInfoIOCTL   = 0x8044B401
	gpioGetLineInfoIOCTL   = 0xC048B402
	gpioGetLineHandleIOCTL = 0xC16CB403
	gpioGetLineValuesIOCTL = 0xC040B408

	gpioV2GetLineInfoIOCTL   = 0xC100B405
	gpioV2GetLineIOCTL       = 0xC250B407
	gpioV2LineGetValuesIOCTL = 0xC010B40E

	gpioLineFlagKernel = 1 << 0
	gpioLineFlagIsOut  = 1 << 1
	gpioHandleInput    = 1 << 0

	gpioV2LineFlagUsed   = 1 << 0
	gpioV2LineFlagInput  = 1 << 2
	gpioV2LineFlagOutput = 1 << 3
)

type gpioChipInfo struct {
	Name  [32]byte
	Label [32]byte
	Lines uint32
}

type gpioLineInfo struct {
	LineOffset uint32
	Flags      uint32
	Name       [32]byte
	Consumer   [32]byte
}

type gpioHandleRequest struct {
	LineOffsets   [64]uint32
	Flags         uint32
	DefaultValues [64]uint8
	ConsumerLabel [32]byte
	Lines         uint32
	Fd            int32
}

type gpioHandleData struct {
	Values [64]uint8
}

type gpioV2LineAttribute struct {
	ID      uint32
	Padding uint32
	Data    uint64
}

type gpioV2LineInfo struct {
	Name     [32]byte
	Consumer [32]byte
	Offset   uint32
	NumAttrs uint32
	Flags    uint64
	Attrs    [10]gpioV2LineAttribute
	Padding  [4]uint32
}

type gpioV2LineConfigAttribute struct {
	Attr gpioV2LineAttribute
	Mask uint64
}

type gpioV2LineConfig struct {
	Flags    uint64
	NumAttrs uint32
	Padding  [5]uint32
	Attrs    [10]gpioV2LineConfigAttribute
}

type gpioV2LineRequest struct {
	Offsets         [64]uint32
	Consumer        [32]byte
	Config          gpioV2LineConfig
	NumLines        uint32
	EventBufferSize uint32
	Padding         [5]uint32
	Fd              int32
}

type gpioV2LineValues struct {
	Bits uint64
	Mask uint64
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

func isWakeGPIOLine(name, consumer string) bool {
	s := strings.ToLower(name + " " + consumer)
	for _, kw := range []string{"boot", "btn", "button", "pwr", "power", "wake", "sw_det", "emmc_reenable", "emmc_disable", "rpiboot"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

type gpioCandidate struct {
	offset   uint32
	name     string
	consumer string
	flags    uint64
	claimed  bool
	isOutput bool
	v2       bool
}

func inspectChipCandidates(chipFd int, lines uint32) []gpioCandidate {
	var out []gpioCandidate
	for off := uint32(0); off < lines; off++ {
		var li gpioLineInfo
		li.LineOffset = off
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(chipFd), gpioGetLineInfoIOCTL, uintptr(unsafe.Pointer(&li)))
		if errno == 0 {
			name := cString(li.Name[:])
			cons := cString(li.Consumer[:])
			if isWakeGPIOLine(name, cons) {
				out = append(out, gpioCandidate{
					offset:   off,
					name:     name,
					consumer: cons,
					flags:    uint64(li.Flags),
					claimed:  (li.Flags & gpioLineFlagKernel) != 0,
					isOutput: (li.Flags & gpioLineFlagIsOut) != 0,
					v2:       false,
				})
			}
			continue
		}

		// Fallback to GPIO v2 line info ioctl if v1 is unavailable
		var li2 gpioV2LineInfo
		li2.Offset = off
		_, _, errno2 := unix.Syscall(unix.SYS_IOCTL, uintptr(chipFd), gpioV2GetLineInfoIOCTL, uintptr(unsafe.Pointer(&li2)))
		if errno2 == 0 {
			name := cString(li2.Name[:])
			cons := cString(li2.Consumer[:])
			if isWakeGPIOLine(name, cons) {
				out = append(out, gpioCandidate{
					offset:   off,
					name:     name,
					consumer: cons,
					flags:    li2.Flags,
					claimed:  (li2.Flags & gpioV2LineFlagUsed) != 0,
					isOutput: (li2.Flags & gpioV2LineFlagOutput) != 0,
					v2:       true,
				})
			}
		}
	}
	return out
}

// ProbeGPIOLines inspects /dev/gpiochip* lines matching wake/button keywords.
func ProbeGPIOLines() []string {
	chips, err := filepath.Glob("/dev/gpiochip*")
	if err != nil {
		return nil
	}
	var out []string
	for _, chip := range chips {
		fd, err := unix.Open(chip, unix.O_RDONLY, 0)
		if err != nil {
			continue
		}
		var ci gpioChipInfo
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), gpioGetChipInfoIOCTL, uintptr(unsafe.Pointer(&ci)))
		if errno != 0 {
			_ = unix.Close(fd)
			continue
		}
		for _, cand := range inspectChipCandidates(fd, ci.Lines) {
			out = append(out, fmt.Sprintf("%s[%d] (%s): name=%q consumer=%q flags=0x%x",
				chip, cand.offset, cString(ci.Label[:]), cand.name, cand.consumer, cand.flags))
		}
		_ = unix.Close(fd)
	}
	return out
}

func watchGPIOButtons(ctx context.Context, wakeCh chan<- string) {
	chips, err := filepath.Glob("/dev/gpiochip*")
	if err != nil {
		return
	}
	for _, chip := range chips {
		fd, err := unix.Open(chip, unix.O_RDONLY, 0)
		if err != nil {
			continue
		}
		var ci gpioChipInfo
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), gpioGetChipInfoIOCTL, uintptr(unsafe.Pointer(&ci)))
		if errno != 0 {
			_ = unix.Close(fd)
			continue
		}

		cands := inspectChipCandidates(fd, ci.Lines)
		for _, cand := range cands {
			if cand.claimed || cand.isOutput {
				continue
			}
			label := fmt.Sprintf("%s[%d]:%s", chip, cand.offset, cand.name)
			if !cand.v2 && watchGPIOLineV1(ctx, fd, cand.offset, label, wakeCh) {
				continue
			}
			watchGPIOLineV2(ctx, fd, cand.offset, label, wakeCh)
		}
		_ = unix.Close(fd)
	}
}

func watchGPIOLineV1(ctx context.Context, chipFd int, offset uint32, label string, wakeCh chan<- string) bool {
	var req gpioHandleRequest
	req.LineOffsets[0] = offset
	req.Flags = gpioHandleInput
	copy(req.ConsumerLabel[:], "czhax-wake")
	req.Lines = 1

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(chipFd), gpioGetLineHandleIOCTL, uintptr(unsafe.Pointer(&req)))
	if errno != 0 || req.Fd <= 0 {
		return false
	}

	lineFd := int(req.Fd)
	var initData gpioHandleData
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(lineFd), gpioGetLineValuesIOCTL, uintptr(unsafe.Pointer(&initData)))
	if errno != 0 {
		_ = unix.Close(lineFd)
		return false
	}

	go func() {
		defer unix.Close(lineFd)
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var cur gpioHandleData
				_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(lineFd), gpioGetLineValuesIOCTL, uintptr(unsafe.Pointer(&cur)))
				if errno != 0 {
					return
				}
				if cur.Values[0] != initData.Values[0] {
					select {
					case wakeCh <- "gpio:" + label:
					default:
					}
					return
				}
			}
		}
	}()
	return true
}

func watchGPIOLineV2(ctx context.Context, chipFd int, offset uint32, label string, wakeCh chan<- string) bool {
	var req gpioV2LineRequest
	req.Offsets[0] = offset
	copy(req.Consumer[:], "czhax-wake")
	req.Config.Flags = gpioV2LineFlagInput
	req.NumLines = 1

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(chipFd), gpioV2GetLineIOCTL, uintptr(unsafe.Pointer(&req)))
	if errno != 0 || req.Fd <= 0 {
		return false
	}

	lineFd := int(req.Fd)
	initVals := gpioV2LineValues{Mask: 1}
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(lineFd), gpioV2LineGetValuesIOCTL, uintptr(unsafe.Pointer(&initVals)))
	if errno != 0 {
		_ = unix.Close(lineFd)
		return false
	}

	go func() {
		defer unix.Close(lineFd)
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cur := gpioV2LineValues{Mask: 1}
				_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(lineFd), gpioV2LineGetValuesIOCTL, uintptr(unsafe.Pointer(&cur)))
				if errno != 0 {
					return
				}
				if (cur.Bits & 1) != (initVals.Bits & 1) {
					select {
					case wakeCh <- "gpio:" + label:
					default:
					}
					return
				}
			}
		}
	}()
	return true
}
