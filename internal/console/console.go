package console

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	syslogActionConsoleOff = 6
	syslogActionConsoleOn  = 7
	printkPath             = "/proc/sys/kernel/printk"

	tioclSetKmsgRedirect byte = 11
	tioclGetKmsgRedirect byte = 17
	kmsgRedirectVT       byte = 12 // send kernel console logs to unused VT12 while TUI runs
)

// Guard suppresses kernel printk messages to the active VT console while the TUI runs
// (when running with root/CAP_SYS_ADMIN privileges) and returns a restore function.
type Guard struct {
	usedKlogctl   bool
	printkFile    string
	savedPrintk   string
	changedPrint  bool
	redirectedVT  bool
	savedRedirect byte
}

func SuppressKernelLogs() *Guard {
	g := &Guard{}
	g.suppressPrintk(printkPath)

	if _, err := unix.Klogctl(syslogActionConsoleOff, nil); err == nil {
		g.usedKlogctl = true
	}

	if isLinuxVT() {
		g.redirectKernelVT()
	}

	return g
}

func (g *Guard) suppressPrintk(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	orig := strings.TrimSpace(string(b))
	fields := strings.Fields(orig)
	if len(fields) < 4 {
		return
	}
	if fields[0] == "0" && fields[2] == "0" {
		return
	}
	fields[0] = "0"
	fields[2] = "0"
	if os.WriteFile(path, []byte(strings.Join(fields, " ")+"\n"), 0o644) == nil {
		g.printkFile = path
		g.savedPrintk = orig
		g.changedPrint = true
	}
}

func (g *Guard) redirectKernelVT() {
	fd := int(os.Stdout.Fd())
	getCmd := tioclGetKmsgRedirect
	r1, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCLINUX), uintptr(unsafe.Pointer(&getCmd)))
	if errno == 0 {
		g.savedRedirect = byte(r1)
	}
	setBuf := [2]byte{tioclSetKmsgRedirect, kmsgRedirectVT}
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCLINUX), uintptr(unsafe.Pointer(&setBuf[0]))); errno == 0 {
		g.redirectedVT = true
	}
}

func isLinuxVT() bool {
	if target, err := os.Readlink("/proc/self/fd/1"); err == nil {
		if strings.HasPrefix(target, "/dev/tty") && !strings.HasPrefix(target, "/dev/ttyS") && !strings.HasPrefix(target, "/dev/ttyAMA") && !strings.HasPrefix(target, "/dev/ttyUSB") {
			return true
		}
	}
	return os.Getenv("TERM") == "linux"
}

func (g *Guard) Restore() {
	if g == nil {
		return
	}
	if g.redirectedVT {
		fd := int(os.Stdout.Fd())
		setBuf := [2]byte{tioclSetKmsgRedirect, g.savedRedirect}
		_, _, _ = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCLINUX), uintptr(unsafe.Pointer(&setBuf[0])))
		g.redirectedVT = false
	}
	if g.usedKlogctl {
		_, _ = unix.Klogctl(syslogActionConsoleOn, nil)
		g.usedKlogctl = false
	}
	if g.changedPrint && g.savedPrintk != "" && g.printkFile != "" {
		_ = os.WriteFile(g.printkFile, []byte(g.savedPrintk+"\n"), 0o644)
		g.changedPrint = false
	}
}
