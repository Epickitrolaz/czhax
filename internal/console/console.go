package console

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	syslogActionConsoleOff = 6
	syslogActionConsoleOn  = 7
	printkPath             = "/proc/sys/kernel/printk"
)

// Guard suppresses kernel printk messages to the active VT console while the TUI runs
// (when running with root/CAP_SYS_ADMIN privileges) and returns a restore function.
type Guard struct {
	usedKlogctl  bool
	savedPrintk  string
	changedPrint bool
}

func SuppressKernelLogs() *Guard {
	g := &Guard{}

	// Try /proc/sys/kernel/printk first so we can lower console loglevel to 1 (emerg only)
	// and restore the exact original levels on exit.
	if b, err := os.ReadFile(printkPath); err == nil {
		orig := strings.TrimSpace(string(b))
		fields := strings.Fields(orig)
		if len(fields) >= 4 && fields[0] != "1" {
			fields[0] = "1"
			if os.WriteFile(printkPath, []byte(strings.Join(fields, " ")+"\n"), 0o644) == nil {
				g.savedPrintk = orig
				g.changedPrint = true
			}
		}
	}

	if _, err := unix.Klogctl(syslogActionConsoleOff, nil); err == nil {
		g.usedKlogctl = true
	}

	return g
}

func (g *Guard) Restore() {
	if g == nil {
		return
	}
	if g.usedKlogctl {
		_, _ = unix.Klogctl(syslogActionConsoleOn, nil)
		g.usedKlogctl = false
	}
	if g.changedPrint && g.savedPrintk != "" {
		_ = os.WriteFile(printkPath, []byte(g.savedPrintk+"\n"), 0o644)
		g.changedPrint = false
	}
}
