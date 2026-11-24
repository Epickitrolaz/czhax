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

	gpioLineFlagKernel = 1 << 0
	gpioHandleInput    = 1 << 0
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
	DefaultValues [64]uint32
	ConsumerLabel [32]byte
	Lines         uint32
	Fd            int32
}

type gpioHandleData struct {
	Values [64]uint8
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

func isWakeGPIOLine(name, consumer string) bool {
	s := strings.ToLower(name + " " + consumer)
	for _, kw := range []string{"boot", "btn", "button", "pwr", "power", "wake", "sw_det"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
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
		for off := uint32(0); off < ci.Lines; off++ {
			var li gpioLineInfo
			li.LineOffset = off
			_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), gpioGetLineInfoIOCTL, uintptr(unsafe.Pointer(&li)))
			if errno != 0 {
				continue
			}
			name := cString(li.Name[:])
			cons := cString(li.Consumer[:])
			if isWakeGPIOLine(name, cons) {
				out = append(out, fmt.Sprintf("%s[%d] (%s): name=%q consumer=%q flags=0x%x",
					chip, off, cString(ci.Label[:]), name, cons, li.Flags))
			}
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

		var watchOffsets []uint32
		for off := uint32(0); off < ci.Lines && len(watchOffsets) < 64; off++ {
			var li gpioLineInfo
			li.LineOffset = off
			_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), gpioGetLineInfoIOCTL, uintptr(unsafe.Pointer(&li)))
			if errno != 0 {
				continue
			}
			if (li.Flags & gpioLineFlagKernel) != 0 {
				continue // already claimed by kernel (e.g. gpio-keys evdev handles it)
			}
			if isWakeGPIOLine(cString(li.Name[:]), cString(li.Consumer[:])) {
				watchOffsets = append(watchOffsets, off)
			}
		}
		if len(watchOffsets) == 0 {
			_ = unix.Close(fd)
			continue
		}

		var req gpioHandleRequest
		for i, off := range watchOffsets {
			req.LineOffsets[i] = off
		}
		req.Flags = gpioHandleInput
		copy(req.ConsumerLabel[:], "czhax-wake")
		req.Lines = uint32(len(watchOffsets))

		_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), gpioGetLineHandleIOCTL, uintptr(unsafe.Pointer(&req)))
		_ = unix.Close(fd)
		if errno != 0 || req.Fd <= 0 {
			continue
		}

		go func(lineFd int, count int, chipPath string) {
			defer unix.Close(lineFd)
			var initData gpioHandleData
			_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(lineFd), gpioGetLineValuesIOCTL, uintptr(unsafe.Pointer(&initData)))
			if errno != 0 {
				return
			}
			ticker := time.NewTicker(60 * time.Millisecond)
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
					for i := 0; i < count; i++ {
						if cur.Values[i] != initData.Values[i] {
							select {
							case wakeCh <- "gpio:" + chipPath:
							default:
							}
							return
						}
					}
				}
			}
		}(int(req.Fd), len(watchOffsets), chip)
	}
}
