package power

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	evKey     uint16 = 1
	keyPower  uint16 = 116
	keySleep  uint16 = 142
	keyWakeup uint16 = 143
	keyProg1  uint16 = 148
	keyRestart uint16 = 408
)

type SavedFile struct {
	Path  string
	Value string
}

type SavedState struct {
	Files       []SavedFile
	BlockedWifi bool
	BlockedBT   bool
}

func NeedsSudo() bool {
	return os.Geteuid() != 0
}

func EnterSleep(sysRoot string) *SavedState {
	if sysRoot == "" {
		sysRoot = "/sys"
	}
	st := &SavedState{}

	// 1. Save & turn off LCD backlight(s)
	st.Files = append(st.Files, writeGlobSaved(filepath.Join(sysRoot, "class/backlight/*/brightness"), "0")...)

	// 2. Save & blank framebuffer(s)
	st.Files = append(st.Files, writeGlobSaved(filepath.Join(sysRoot, "class/graphics/fb*/blank"), "1")...)

	// 3. Save & set USB bus devices to runtime autosuspend
	st.Files = append(st.Files, writeGlobSaved(filepath.Join(sysRoot, "bus/usb/devices/*/power/control"), "auto")...)

	// 4. Save & set CPU scaling governors to powersave
	st.Files = append(st.Files, writeGlobSaved(filepath.Join(sysRoot, "devices/system/cpu/cpu*/cpufreq/scaling_governor"), "powersave")...)

	// 5. Save & turn off keypad/expansion LEDs
	for _, led := range []string{"capslock-led", "fn-led", "tables-sel-led", "ext_5v_out", "grove_5v_out"} {
		st.Files = append(st.Files, writeGlobSaved(filepath.Join(sysRoot, "class/leds", led, "brightness"), "0")...)
	}

	// 6. Soft-block Wi-Fi & Bluetooth if rfkill is available and not already blocked
	if _, err := exec.LookPath("rfkill"); err == nil {
		if !isRfkillBlocked("wifi") {
			if exec.Command("rfkill", "block", "wifi").Run() == nil {
				st.BlockedWifi = true
			}
		}
		if !isRfkillBlocked("bluetooth") {
			if exec.Command("rfkill", "block", "bluetooth").Run() == nil {
				st.BlockedBT = true
			}
		}
	}

	return st
}

func ExitSleep(st *SavedState) {
	if st == nil {
		return
	}
	if st.BlockedWifi {
		_ = exec.Command("rfkill", "unblock", "wifi").Run()
	}
	if st.BlockedBT {
		_ = exec.Command("rfkill", "unblock", "bluetooth").Run()
	}
	// Restore in reverse order so framebuffers unblank before backlight turns back on
	for i := len(st.Files) - 1; i >= 0; i-- {
		sf := st.Files[i]
		_ = os.WriteFile(sf.Path, []byte(sf.Value+"\n"), 0o644)
	}
}

func writeGlobSaved(pattern, newVal string) []SavedFile {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	var saved []SavedFile
	for _, p := range matches {
		origBytes, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		orig := strings.TrimSpace(string(origBytes))
		if orig == newVal {
			continue
		}
		if err := os.WriteFile(p, []byte(newVal+"\n"), 0o644); err == nil {
			saved = append(saved, SavedFile{Path: p, Value: orig})
		}
	}
	return saved
}

func isRfkillBlocked(kind string) bool {
	out, err := exec.Command("rfkill", "list", kind).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Soft blocked: yes")
}

// RunSleepCommand executes full power-save sleep from the CLI (`czhax power sleep`),
// guarantees hardware restore on wake or signal, and blocks until the side boot button
// (or fallback Enter/Esc key) wakes the unit.
func RunSleepCommand() error {
	fmt.Println("Entering CZHax Sleep Mode...")
	fmt.Println("Press the side boot button (or Enter / Esc) to wake.")
	time.Sleep(600 * time.Millisecond)

	saved := EnterSleep("")
	defer ExitSleep(saved)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	reason := WaitForWake(ctx, "/dev/input", os.Stdin)
	ExitSleep(saved)
	fmt.Printf("Woke up (%s).\n", reason)
	return nil
}

// WaitForWake blocks until a hardware wake button event or fallback terminal key is received.
func WaitForWake(ctx context.Context, inputDir string, stdin io.Reader) string {
	wakeCh := make(chan string, 4)
	doneCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Watch Linux evdev devices for side boot / power button presses
	watchEvdevButtons(doneCtx, inputDir, wakeCh)

	// Watch stdin for Enter/Esc/Space fallback if running interactively
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		oldState, err := term.MakeRaw(int(f.Fd()))
		if err == nil {
			defer func() { _ = term.Restore(int(f.Fd()), oldState) }()
		}
		go watchStdinWake(doneCtx, f, wakeCh)
	}

	select {
	case reason := <-wakeCh:
		return reason
	case <-ctx.Done():
		return "signal"
	}
}

func watchStdinWake(ctx context.Context, f *os.File, wakeCh chan<- string) {
	fd := int(f.Fd())
	buf := make([]byte, 16)
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := unix.Poll(pfd, 200)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if n <= 0 || (pfd[0].Revents&unix.POLLIN) == 0 {
			continue
		}
		nr, err := unix.Read(fd, buf)
		if err != nil || nr <= 0 {
			return
		}
		for i := 0; i < nr; i++ {
			b := buf[i]
			if b == '\r' || b == '\n' || b == ' ' || b == 0x1b || b == 0x03 {
				select {
				case wakeCh <- "key":
				default:
				}
				return
			}
		}
	}
}

func isWakeButtonDevice(name string) bool {
	low := strings.ToLower(name)
	if strings.Contains(low, "tca8418") || strings.Contains(low, "mouse") || strings.Contains(low, "imu") {
		return false
	}
	return strings.Contains(low, "gpio") ||
		strings.Contains(low, "btn") ||
		strings.Contains(low, "button") ||
		strings.Contains(low, "pwr") ||
		strings.Contains(low, "power") ||
		strings.Contains(low, "boot") ||
		strings.Contains(low, "pmic")
}

func isWakeKeyCode(code uint16) bool {
	switch code {
	case keyPower, keySleep, keyWakeup, keyProg1, keyRestart:
		return true
	default:
		return false
	}
}

func watchEvdevButtons(ctx context.Context, inputDir string, wakeCh chan<- string) {
	entries, err := filepath.Glob(filepath.Join(inputDir, "event*"))
	if err != nil {
		return
	}
	for _, devPath := range entries {
		base := filepath.Base(devPath)
		nameBytes, _ := os.ReadFile(filepath.Join("/sys/class/input", base, "device/name"))
		devName := strings.TrimSpace(string(nameBytes))
		btnDev := isWakeButtonDevice(devName)

		fd, err := unix.Open(devPath, unix.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		go func(fd int, anyKey bool, label string) {
			defer unix.Close(fd)
			evSize := int(unsafe.Sizeof(unix.Timeval{})) + 8
			tvSize := int(unsafe.Sizeof(unix.Timeval{}))
			buf := make([]byte, evSize*8)
			pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				ready, err := unix.Poll(pfd, 250)
				if err != nil {
					if err == unix.EINTR {
						continue
					}
					return
				}
				if ready <= 0 || (pfd[0].Revents&unix.POLLIN) == 0 {
					continue
				}
				n, err := unix.Read(fd, buf)
				if err != nil {
					if err == unix.EAGAIN || err == unix.EINTR {
						continue
					}
					return
				}
				for off := 0; off+evSize <= n; off += evSize {
					evType := binary.LittleEndian.Uint16(buf[off+tvSize : off+tvSize+2])
					evCode := binary.LittleEndian.Uint16(buf[off+tvSize+2 : off+tvSize+4])
					evVal := int32(binary.LittleEndian.Uint32(buf[off+tvSize+4 : off+tvSize+8]))
					if evType == evKey && evVal == 1 && (anyKey || isWakeKeyCode(evCode)) {
						select {
						case wakeCh <- "button:" + label:
						default:
						}
						return
					}
				}
			}
		}(fd, btnDev, devName)
	}
}

func Shutdown() error {
	if _, err := exec.LookPath("systemctl"); err == nil {
		if out, err := exec.Command("systemctl", "poweroff").CombinedOutput(); err == nil {
			return nil
		} else if os.Geteuid() != 0 {
			if err2 := exec.Command("sudo", "systemctl", "poweroff").Run(); err2 == nil {
				return nil
			}
			return fmt.Errorf("poweroff failed: %v (%s)", err, strings.TrimSpace(string(out)))
		} else {
			return fmt.Errorf("systemctl poweroff: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	return exec.Command("poweroff").Run()
}

func Reboot() error {
	if _, err := exec.LookPath("systemctl"); err == nil {
		if out, err := exec.Command("systemctl", "reboot").CombinedOutput(); err == nil {
			return nil
		} else if os.Geteuid() != 0 {
			if err2 := exec.Command("sudo", "systemctl", "reboot").Run(); err2 == nil {
				return nil
			}
			return fmt.Errorf("reboot failed: %v (%s)", err, strings.TrimSpace(string(out)))
		} else {
			return fmt.Errorf("systemctl reboot: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	return exec.Command("reboot").Run()
}
