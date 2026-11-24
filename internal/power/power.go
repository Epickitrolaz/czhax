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

	"github.com/czhax/czhax/internal/console"
	"golang.org/x/sys/unix"
)

const (
	evKey      uint16 = 1
	keyPower   uint16 = 116
	keySleep   uint16 = 142
	keyWakeup  uint16 = 143
	keyProg1   uint16 = 148
	keyProg2   uint16 = 149
	keyProg3   uint16 = 202
	keyProg4   uint16 = 203
	keySuspend uint16 = 205
	keyRestart uint16 = 408

	eviocgname256 = 0x80ff4506
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

// ProbeInputDevices lists /dev/input/event* nodes and whether CZHax watches them as wake buttons.
func ProbeInputDevices() []string {
	entries, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return nil
	}
	var out []string
	for _, devPath := range entries {
		base := filepath.Base(devPath)
		nameBytes, _ := os.ReadFile(filepath.Join("/sys/class/input", base, "device/name"))
		name := strings.TrimSpace(string(nameBytes))
		if name == "" {
			if fd, err := unix.Open(devPath, unix.O_RDONLY|unix.O_NONBLOCK, 0); err == nil {
				name = readEvdevName(fd, "")
				_ = unix.Close(fd)
			}
		}
		role := "wake-keys-only"
		if isWakeButtonDevice(name) {
			role = "wake-button"
		}
		out = append(out, fmt.Sprintf("%s: %q (%s)", devPath, name, role))
	}
	return out
}

// RunSleepCommand executes full power-save sleep from the CLI (`czhax power sleep`),
// guarantees hardware restore on wake or signal, and blocks until the hardware boot button
// wakes the unit.
func RunSleepCommand() error {
	guard := console.SuppressKernelLogs()
	defer guard.Restore()

	fmt.Println("Entering CZHax Sleep Mode...")
	fmt.Println("Press the boot button to wake.")
	time.Sleep(600 * time.Millisecond)

	saved := EnterSleep("")
	defer ExitSleep(saved)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	reason := WaitForWake(ctx, "/dev/input", nil)
	ExitSleep(saved)
	fmt.Printf("Woke up (%s).\n", reason)
	return nil
}

// WaitForWake blocks until a hardware wake button event (evdev or GPIO) is received.
func WaitForWake(ctx context.Context, inputDir string, _ io.Reader) string {
	wakeCh := make(chan string, 4)
	doneCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Watch Linux evdev & unclaimed GPIO button devices for boot / power button presses
	watchEvdevButtons(doneCtx, inputDir, wakeCh)
	watchGPIOButtons(doneCtx, wakeCh)

	select {
	case reason := <-wakeCh:
		return reason
	case <-ctx.Done():
		return "signal"
	}
}

func isWakeButtonDevice(name string) bool {
	low := strings.ToLower(name)
	if low == "" {
		return false
	}
	// Exclude known non-button peripherals (46-key QWERTY keypad, IMU, mice, HDMI CEC/audio, jack/IR)
	for _, skip := range []string{
		"tca8418", "bmi270", "bmm150", "imu",
		"mouse", "touchpad", "vc4-hdmi", "hdmi",
		"headphones", "es8389", "es8390", "gpio_ir", "ir_recv",
	} {
		if strings.Contains(low, skip) {
			return false
		}
	}
	return true
}

func isWakeKeyCode(code uint16) bool {
	switch code {
	case keyPower, keySleep, keyWakeup, keyProg1, keyProg2, keyProg3, keyProg4, keySuspend, keyRestart:
		return true
	default:
		return false
	}
}

func readEvdevName(fd int, sysName string) string {
	if sysName != "" {
		return sysName
	}
	var buf [256]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), eviocgname256, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return ""
	}
	return cString(buf[:])
}

func watchEvdevButtons(ctx context.Context, inputDir string, wakeCh chan<- string) {
	entries, err := filepath.Glob(filepath.Join(inputDir, "event*"))
	if err != nil {
		return
	}
	for _, devPath := range entries {
		base := filepath.Base(devPath)
		nameBytes, _ := os.ReadFile(filepath.Join("/sys/class/input", base, "device/name"))
		sysName := strings.TrimSpace(string(nameBytes))

		fd, err := unix.Open(devPath, unix.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		devName := readEvdevName(fd, sysName)
		btnDev := isWakeButtonDevice(devName)
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
