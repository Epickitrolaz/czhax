package ir

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux lirc uapi values (linux/lirc.h).
const (
	lircGetFeatures         = 0x80046900
	lircSetSendMode         = 0x40046911
	lircSetRecMode          = 0x40046912
	lircSetSendCarrier      = 0x40046913
	lircSetSendDutyCycle    = 0x40046915
	lircSetRecTimeout       = 0x40046918
	lircSetRecTimeoutReport = 0x40046919

	lircCanSendPulse      = 0x00000002
	lircCanSetSendCarrier = 0x00000100
	lircCanSetSendDuty    = 0x00000200
	lircCanRecMode2       = 0x00040000

	lircModePulse = 0x00000002
	lircModeMode2 = 0x00000004

	lircMode2Pulse    = 0x01000000
	lircValueMask     = 0x00ffffff
	lircMode2Mask     = 0xff000000
	lircMode2Timeout  = 0x03000000
	lircMode2Overflow = 0x04000000
	lircMode2Freq     = 0x02000000
)

const (
	lircCapturePoll     = 50 * time.Millisecond
	lircFrameGap        = 12000 * time.Microsecond
	lircSilenceTimeout  = 700 * time.Millisecond
	lircDefaultCapture  = 5 * time.Second
	lircMaxCaptureBytes = 8192
)

// Device is one /dev/lirc* node and its advertised capabilities.
type Device struct {
	Path        string
	Features    uint32
	Receiver    bool
	Transmitter bool
	Err         string
}

// Hardware is the discovered RX/TX pair. Err is user-facing when discovery
// could not find a usable pair.
type Hardware struct {
	RXPath string
	TXPath string
	Paths  []Device
	Err    string
}

func DiscoverHardware() Hardware {
	paths, _ := filepath.Glob("/dev/lirc*")
	if len(paths) == 0 {
		if _, err := os.Stat("/dev/lirc"); err == nil {
			paths = []string{"/dev/lirc"}
		}
	}
	sort.Slice(paths, func(i, j int) bool { return lircIndex(paths[i]) < lircIndex(paths[j]) })

	hw := Hardware{}
	permissionErr := ""
	openErr := ""
	for _, path := range paths {
		dev := Device{Path: path}
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
		if err != nil {
			fd, err = unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK, 0)
		}
		if err != nil {
			dev.Err = err.Error()
			if openErr == "" {
				openErr = err.Error()
			}
			if permissionErr == "" && errors.Is(err, unix.EACCES) {
				permissionErr = err.Error()
			}
			hw.Paths = append(hw.Paths, dev)
			continue
		}
		features, err := ioctlGet(fd, lircGetFeatures)
		_ = unix.Close(fd)
		if err != nil {
			dev.Err = err.Error()
			if openErr == "" {
				openErr = err.Error()
			}
			if permissionErr == "" && errors.Is(err, unix.EACCES) {
				permissionErr = err.Error()
			}
			hw.Paths = append(hw.Paths, dev)
			continue
		}
		dev.Features = features
		dev.Receiver = features&lircCanRecMode2 != 0
		dev.Transmitter = features&lircCanSendPulse != 0
		if dev.Receiver && hw.RXPath == "" {
			hw.RXPath = path
		}
		if dev.Transmitter && hw.TXPath == "" {
			hw.TXPath = path
		}
		hw.Paths = append(hw.Paths, dev)
	}

	switch {
	case len(paths) == 0:
		hw.Err = "no /dev/lirc* found; enable dtoverlay=gpio-ir,gpio_pin=13,gpio_pull=up and dtoverlay=gpio-ir-tx,gpio_pin=12"
	case hw.RXPath == "" && hw.TXPath == "" && openErr != "":
		hw.Err = "cannot open /dev/lirc* (" + openErr + ")"
		if permissionErr != "" {
			hw.Err += "; add the user to the video group"
		}
	case hw.RXPath == "" && hw.TXPath == "":
		hw.Err = "found /dev/lirc but no receiver or transmitter capability"
	case hw.RXPath == "":
		hw.Err = "IR transmitter found, no raw receiver (check dtoverlay=gpio-ir,gpio_pin=13,gpio_pull=up)"
	case hw.TXPath == "":
		hw.Err = "IR receiver found, no raw transmitter (check dtoverlay=gpio-ir-tx,gpio_pin=12)"
	}
	return hw
}

func lircIndex(path string) int {
	base := filepath.Base(path)
	base = strings.TrimPrefix(base, "lirc")
	n, err := strconv.Atoi(base)
	if err != nil {
		return 1 << 30
	}
	return n
}

func ioctlGet(fd int, req uint) (uint32, error) {
	var value uint32
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return 0, errno
	}
	return value, nil
}

func ioctlSet(fd int, req uint, value uint32) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return errno
	}
	return nil
}

// Receiver captures raw IR in LIRC_MODE2.
type Receiver struct {
	f    *os.File
	path string
}

func OpenReceiver(path string) (*Receiver, error) {
	f, err := os.OpenFile(path, os.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		f, err = os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("open receiver %s: %w", path, err)
	}
	fd := int(f.Fd())
	features, err := ioctlGet(fd, lircGetFeatures)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("query %s: %w", path, err)
	}
	if features&lircCanRecMode2 == 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%s has no LIRC_MODE2 receiver", path)
	}
	if err := ioctlSet(fd, lircSetRecMode, lircModeMode2); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("set %s receiver mode: %w", path, err)
	}
	// Best effort: ask the driver for a timeout packet at the end of a frame.
	_ = ioctlSet(fd, lircSetRecTimeout, 50000)
	_ = ioctlSet(fd, lircSetRecTimeoutReport, 1)
	return &Receiver{f: f, path: path}, nil
}

func (r *Receiver) Path() string { return r.path }

func (r *Receiver) Close() error {
	if r == nil || r.f == nil {
		return nil
	}
	return r.f.Close()
}

// Capture waits for one IR frame. It returns as soon as a frame-sized gap is
// seen, or when timeout expires with at least a partial frame.
func (r *Receiver) Capture(ctx context.Context, timeout time.Duration) (Signal, error) {
	if timeout <= 0 {
		timeout = lircDefaultCapture
	}
	fd := int(r.f.Fd())
	r.drain(fd)

	deadline := time.Now().Add(timeout)
	var timings []int
	var lastData time.Time
	var measuredFreq int
	var pending []byte
	buf := make([]byte, 512)

	finish := func() (Signal, bool) {
		trimmed := trimCapturedTimings(timings)
		if len(trimmed) < 5 {
			return Signal{}, false
		}
		freq := measuredFreq
		if freq <= 0 || freq > maxDurationUS {
			freq = DefaultFrequency
		}
		sig := Signal{
			Name:      "Learned",
			Protocol:  "raw",
			Frequency: freq,
			DutyCycle: DefaultDutyCycle,
			Timings:   normalizeTimings(trimmed),
			Source:    "learned",
		}
		return sig, true
	}

	for {
		if err := ctx.Err(); err != nil {
			return Signal{}, err
		}
		if time.Now().After(deadline) {
			break
		}
		pfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(pfds, int(lircCapturePoll/time.Millisecond))
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return Signal{}, fmt.Errorf("poll %s: %w", r.path, err)
		}
		if n == 0 || pfds[0].Revents&unix.POLLIN == 0 {
			if len(timings) >= 5 && !lastData.IsZero() && time.Since(lastData) > lircSilenceTimeout {
				if sig, ok := finish(); ok {
					return sig, nil
				}
			}
			continue
		}

		got, err := unix.Read(fd, buf)
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			continue
		}
		if err != nil {
			return Signal{}, fmt.Errorf("read %s: %w", r.path, err)
		}
		if got <= 0 {
			continue
		}
		lastData = time.Now()
		pending = append(pending, buf[:got]...)
		if len(pending) > lircMaxCaptureBytes*4 {
			return Signal{}, errors.New("IR signal is too long")
		}
		for len(pending) >= 4 {
			v := binary.LittleEndian.Uint32(pending[:4])
			pending = pending[4:]
			switch v & lircMode2Mask {
			case lircMode2Pulse, 0:
				d := int(v & lircValueMask)
				if d <= 0 {
					continue
				}
				isPulse := (v & lircMode2Mask) == lircMode2Pulse
				if len(timings) == 0 && !isPulse {
					// A frame starts with a mark; ignore idle spaces.
					continue
				}
				if !isPulse && d > int(lircFrameGap/time.Microsecond) && len(timings) >= 5 {
					if sig, ok := finish(); ok {
						return sig, nil
					}
				}
				if len(timings) > 0 && isPulse == (len(timings)%2 == 1) {
					// Merge consecutive samples of identical polarity to preserve mark/space alternation.
					sum := timings[len(timings)-1] + d
					if sum > maxDurationUS {
						sum = maxDurationUS
					}
					timings[len(timings)-1] = sum
				} else {
					timings = append(timings, d)
				}
				if len(timings) >= MaxTimings {
					if sig, ok := finish(); ok {
						return sig, nil
					}
					return Signal{}, errors.New("IR signal is too long")
				}
			case lircMode2Freq:
				measuredFreq = int(v & lircValueMask)
			case lircMode2Timeout, lircMode2Overflow:
				if sig, ok := finish(); ok {
					return sig, nil
				}
				timings = timings[:0]
			}
		}
	}
	if sig, ok := finish(); ok {
		return sig, nil
	}
	if ctx.Err() != nil {
		return Signal{}, ctx.Err()
	}
	return Signal{}, errors.New("no IR signal detected (point a remote at the receiver and try again)")
}

func (r *Receiver) drain(fd int) {
	buf := make([]byte, 512)
	for {
		n, err := unix.Read(fd, buf)
		if err != nil || n <= 0 {
			return
		}
	}
}

func trimCapturedTimings(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	// Linux LIRC_MODE_PULSE requires an odd number of durations (ending on a
	// pulse; the transmitter turns the carrier off afterwards).
	if len(in)%2 == 0 {
		in = in[:len(in)-1]
	}
	return in
}

// Transmitter sends raw LIRC_MODE_PULSE timings.
type Transmitter struct {
	f        *os.File
	path     string
	features uint32
}

func OpenTransmitter(path string) (*Transmitter, error) {
	f, err := os.OpenFile(path, os.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		f, err = os.OpenFile(path, os.O_WRONLY|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("open transmitter %s: %w", path, err)
	}
	fd := int(f.Fd())
	features, err := ioctlGet(fd, lircGetFeatures)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("query %s: %w", path, err)
	}
	if features&lircCanSendPulse == 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%s has no LIRC_MODE_PULSE transmitter", path)
	}
	if err := ioctlSet(fd, lircSetSendMode, lircModePulse); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("set %s send mode: %w", path, err)
	}
	return &Transmitter{f: f, path: path, features: features}, nil
}

func (t *Transmitter) Path() string { return t.path }

func (t *Transmitter) Close() error {
	if t == nil || t.f == nil {
		return nil
	}
	return t.f.Close()
}

func (t *Transmitter) Send(sig Signal) error {
	sig = sig.Normalized()
	if err := sig.Validate(); err != nil {
		return err
	}
	if sig.Frequency <= 0 || sig.Frequency > maxDurationUS {
		sig.Frequency = DefaultFrequency
	}
	fd := int(t.f.Fd())
	if t.features&lircCanSetSendCarrier != 0 {
		if err := ioctlSet(fd, lircSetSendCarrier, uint32(sig.Frequency)); err != nil {
			return fmt.Errorf("set carrier %d Hz: %w", sig.Frequency, err)
		}
	}
	if t.features&lircCanSetSendDuty != 0 {
		duty := int(sig.DutyCycle*100 + 0.5)
		if duty < 1 {
			duty = 1
		}
		if duty > 99 {
			duty = 99
		}
		// Not all raw transmitters implement duty-cycle control.
		_ = ioctlSet(fd, lircSetSendDutyCycle, uint32(duty))
	}

	data := make([]byte, len(sig.Timings)*4)
	for i, d := range sig.Timings {
		binary.LittleEndian.PutUint32(data[i*4:], uint32(d))
	}
	written := 0
	for written < len(data) {
		n, err := unix.Write(fd, data[written:])
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			pfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
			if _, perr := unix.Poll(pfds, 200); perr != nil {
				return fmt.Errorf("poll transmitter: %w", perr)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("write %s: %w", t.path, err)
		}
		if n <= 0 {
			return fmt.Errorf("write %s: short write", t.path)
		}
		written += n
	}
	return nil
}

// SendOnce opens the transmitter, sends one signal and closes it. This keeps
// the device free when the TUI is idle.
func SendOnce(path string, sig Signal) error {
	tx, err := OpenTransmitter(path)
	if err != nil {
		return err
	}
	defer tx.Close()
	return tx.Send(sig)
}

// CaptureOnce opens the receiver, captures one frame and closes it.
func CaptureOnce(ctx context.Context, path string, timeout time.Duration) (Signal, error) {
	rx, err := OpenReceiver(path)
	if err != nil {
		return Signal{}, err
	}
	defer rx.Close()
	return rx.Capture(ctx, timeout)
}
