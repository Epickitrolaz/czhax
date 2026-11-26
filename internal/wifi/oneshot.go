package wifi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/czhax/czhax/internal/oneshot"
)

// OneShotDefaultAttack is the default OneShot-Extended argument set:
// Pixie Dust + kill interfering processes.
const OneShotDefaultAttack = "-P -k"

// WPSInfo is the WPS information element data parsed from an iw scan.
type WPSInfo struct {
	WPS         bool
	Version     string
	Locked      bool
	Model       string
	ModelNumber string
	DeviceName  string
}

// Vulnerable reports whether OneShot-Extended considers this AP a likely
// Pixie Dust target: WPS 1.0, or a WSC model/device name present in
// vulnwsc.txt.
func (w WPSInfo) Vulnerable(vulnList []string) bool {
	if !w.WPS {
		return false
	}
	if w.Version == "1.0" || w.Version == "" {
		return true
	}
	if len(vulnList) == 0 {
		return false
	}
	candidates := []string{}
	model := strings.TrimSpace(w.Model + " " + w.ModelNumber)
	if model != "" {
		candidates = append(candidates, model)
	}
	if strings.TrimSpace(w.Model) != "" {
		candidates = append(candidates, strings.TrimSpace(w.Model))
	}
	if strings.TrimSpace(w.DeviceName) != "" {
		candidates = append(candidates, strings.TrimSpace(w.DeviceName))
	}
	for _, c := range candidates {
		for _, v := range vulnList {
			if strings.EqualFold(strings.TrimSpace(v), c) {
				return true
			}
		}
	}
	return false
}

// Label summarises WPS state for the UI.
func (w WPSInfo) Label() string {
	if !w.WPS {
		return "no WPS"
	}
	parts := []string{"WPS " + w.Version}
	if w.Locked {
		parts = append(parts, "locked")
	}
	if w.DeviceName != "" {
		parts = append(parts, w.DeviceName)
	}
	return strings.Join(parts, " · ")
}

// ParseWPSScan extracts WPS information per BSSID from `iw dev <iface> scan`.
func ParseWPSScan(out string) map[string]WPSInfo {
	infos := make(map[string]WPSInfo)
	var curBSSID string
	var cur WPSInfo
	inWPS := false
	flush := func() {
		if curBSSID != "" && cur.WPS {
			infos[curBSSID] = cur
		}
	}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "\t"))
		if strings.HasPrefix(trimmed, "BSS ") {
			flush()
			fields := strings.Fields(strings.TrimPrefix(trimmed, "BSS "))
			if len(fields) == 0 {
				curBSSID = ""
				continue
			}
			curBSSID = strings.ToUpper(fields[0])
			if idx := strings.Index(curBSSID, "("); idx >= 0 {
				curBSSID = curBSSID[:idx]
			}
			cur = WPSInfo{Version: "1.0"}
			inWPS = false
			continue
		}
		if curBSSID == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "WPS:") {
			inWPS = true
			cur.WPS = true
			if v := extractWPSVersion(trimmed); v != "" {
				cur.Version = v
			}
			continue
		}
		if !inWPS {
			continue
		}
		switch {
		case strings.Contains(trimmed, "AP setup locked:"):
			flag := strings.TrimSpace(strings.TrimPrefix(trimmed, "* AP setup locked:"))
			flag = strings.TrimPrefix(strings.ToLower(flag), "0x")
			if n, err := strconv.ParseUint(flag, 16, 32); err == nil {
				cur.Locked = n != 0
			}
		case strings.Contains(trimmed, "Model Number:"):
			cur.ModelNumber = strings.TrimSpace(strings.TrimPrefix(trimmed, "* Model Number:"))
		case strings.Contains(trimmed, "Model:"):
			cur.Model = strings.TrimSpace(strings.TrimPrefix(trimmed, "* Model:"))
		case strings.Contains(trimmed, "Device name:"):
			cur.DeviceName = strings.TrimSpace(strings.TrimPrefix(trimmed, "* Device name:"))
		case strings.Contains(trimmed, "Version2:"):
			cur.Version = "2.0"
		case strings.Contains(trimmed, "Version:"):
			if v := extractWPSVersion(trimmed); v != "" {
				cur.Version = v
			}
		}
	}
	flush()
	return infos
}

func extractWPSVersion(s string) string {
	idx := strings.Index(s, "Version:")
	if idx < 0 {
		return ""
	}
	v := strings.TrimSpace(s[idx+len("Version:"):])
	v = strings.TrimPrefix(v, "2:")
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "2") {
		return "2.0"
	}
	if strings.HasPrefix(v, "1") {
		return "1.0"
	}
	return ""
}

// ScanWPS scans iface and returns WPS data per BSSID.
func ScanWPS(iface string) (map[string]WPSInfo, error) {
	iface = strings.TrimSpace(iface)
	if iface == "" {
		iface = "wlan0"
	}
	s := defaultScanner()
	out, err := runPrivileged(s, 14*time.Second, "iw", "dev", iface, "scan")
	if err != nil {
		return nil, fmt.Errorf("iw scan: %s", cleanCmdError(out, err))
	}
	return ParseWPSScan(out), nil
}

// OneShot describes a discovered OneShot-Extended installation.
type OneShot struct {
	Dir      string
	Script   string
	VulnList string
	Err      string
	Embedded bool
}

// Found reports whether the ose.py entrypoint was located.
func (o OneShot) Found() bool { return strings.TrimSpace(o.Script) != "" }

// FindOneShot resolves OneShot-Extended. An explicit directory (config
// oneshot_dir or CZHAX_ONESHOT_DIR) overrides the copy embedded in CZHax.
func FindOneShot(preferred string) OneShot {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		preferred = strings.TrimSpace(os.Getenv("CZHAX_ONESHOT_DIR"))
	}
	if preferred != "" {
		if o := oneShotFromPath(preferred); o.Found() {
			return o.finish()
		}
		return OneShot{Err: "OneShot-Extended not found at " + preferred + " (unset oneshot_dir to use the embedded copy)"}
	}

	// Default: the pinned upstream tree baked into the binary.
	if src, err := oneshot.Extract(); err == nil {
		return OneShot{
			Dir:      src.Dir,
			Script:   src.Script,
			VulnList: src.VulnList,
			Embedded: true,
		}
	} else {
		extractErr := err
		// Fall back to a system/library copy if extraction is impossible
		// (for example a read-only home directory).
		home, _ := os.UserHomeDir()
		candidates := []string{
			filepath.Join(home, "OneShot-Extended"),
			filepath.Join(home, "oneshot-extended"),
			filepath.Join(home, "OneShot"),
			filepath.Join(home, "tools", "OneShot-Extended"),
			"/opt/OneShot-Extended",
			"/opt/oneshot-extended",
			"/usr/local/share/OneShot-Extended",
			"/usr/local/share/oneshot-extended",
			"/usr/share/oneshot-extended",
		}
		for _, c := range candidates {
			if c == "" {
				continue
			}
			if found := oneShotFromPath(c); found.Found() {
				return found.finish()
			}
		}
		if p, err := exec.LookPath("ose.py"); err == nil {
			return OneShot{Dir: filepath.Dir(p), Script: p}.finish()
		}
		return OneShot{Err: "embedded OneShot-Extended unavailable (" + extractErr.Error() + ")"}
	}
}

func oneShotFromPath(path string) OneShot {
	path = strings.TrimSpace(path)
	if path == "" {
		return OneShot{}
	}
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		return OneShot{Dir: filepath.Dir(path), Script: path}
	}
	script := filepath.Join(path, "ose.py")
	if _, err := os.Stat(script); err == nil {
		return OneShot{Dir: path, Script: script}
	}
	return OneShot{}
}

func (o OneShot) finish() OneShot {
	if o.VulnList == "" {
		o.VulnList = DefaultVulnListPath(o.Dir)
	}
	return o
}

// EmbeddedOneShotCommit reports the pinned upstream snapshot baked into CZHax.
func EmbeddedOneShotCommit() string { return oneshot.ShortCommit() }

// DefaultVulnListPath locates OneShot's vulnwsc.txt vulnerable device list.
func DefaultVulnListPath(oneShotDir string) string {
	if env := strings.TrimSpace(os.Getenv("CZHAX_ONESHOT_VULN")); env != "" {
		return env
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(oneShotDir, "vulnwsc.txt"),
		filepath.Join(home, ".OneShot-Extended", "vulnwsc.txt"),
		"/usr/share/oneshot-extended/vulnwsc.txt",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// LoadVulnList reads a OneShot vulnerable-device list. Missing files return an
// empty list so color mode still works via WPS version detection.
func LoadVulnList(path string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// SplitAttackArgs splits a stored argument string such as "-P -k -F".
func SplitAttackArgs(s string) []string {
	return strings.Fields(strings.TrimSpace(s))
}

// RunOneShot executes OneShot-Extended against one AP, streaming cleaned output
// lines to onLine. It needs root; when not running as root it uses sudo -n.
func RunOneShot(ctx context.Context, o OneShot, iface, bssid string, attackArgs []string, onLine func(string)) error {
	if !o.Found() {
		if o.Err != "" {
			return errors.New(o.Err)
		}
		return errors.New("OneShot-Extended not found")
	}

	var bin string
	var args []string
	workDir := ""

	if o.Embedded {
		// Execute via the privileged helper, which re-extracts the embedded
		// snapshot into a root-owned private directory. This keeps sudo from
		// running Python files from a user-writable cache.
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("locate czhax binary: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
			exe = resolved
		}
		runArgs := []string{
			"oneshot", "run",
			"--iface", iface,
			"--bssid", bssid,
			"--attack", strings.Join(attackArgs, " "),
		}
		if os.Geteuid() != 0 {
			bin = "sudo"
			args = append([]string{"-n", exe}, runArgs...)
		} else {
			bin = exe
			args = runArgs
		}
	} else {
		py, err := oneshot.FindPython()
		if err != nil {
			return err
		}
		workDir = o.Dir
		args = []string{o.Script, "-i", iface, "-b", bssid}
		args = append(args, attackArgs...)
		if strings.TrimSpace(o.VulnList) != "" {
			args = append(args, "--vuln-list", o.VulnList)
		}
		bin = py
		if os.Geteuid() != 0 {
			args = append([]string{"-n", py}, args...)
			bin = "sudo"
		}
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 3 * time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	stream := func(f interface{ Read([]byte) (int, error) }) {
		defer wg.Done()
		buf := make([]byte, 4096)
		var pending string
		for {
			n, err := f.Read(buf)
			if n > 0 {
				pending += strings.ReplaceAll(string(buf[:n]), "\r", "\n")
				for {
					idx := strings.IndexByte(pending, '\n')
					if idx < 0 {
						break
					}
					line := pending[:idx]
					pending = pending[idx+1:]
					if s := cleanOneShotLine(line); s != "" && onLine != nil {
						onLine(s)
					}
				}
				if len(pending) > 4096 {
					pending = pending[len(pending)-4096:]
				}
			}
			if err != nil {
				break
			}
		}
		if s := cleanOneShotLine(pending); s != "" && onLine != nil {
			onLine(s)
		}
	}
	wg.Add(2)
	go stream(pipe)
	go stream(errPipe)
	waitErr := cmd.Wait()
	wg.Wait()
	if onLine != nil {
		onLine("[czhax] restoring NetworkManager…")
	}
	restoreViaHelper(iface)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code := exitErr.ExitCode()
			if code == 130 || code == 143 || code == -1 {
				return context.Canceled
			}
			return fmt.Errorf("oneshot exited %d", code)
		}
		return waitErr
	}
	return nil
}

// RestoreAfterOneShot undoes the side effects of OneShot's --kill: starts
// NetworkManager again, re-enables networking, re-adopts the interface and
// unblocks rfkill so the next CZHax scan succeeds.
func RestoreAfterOneShot(iface string) {
	restoreAfterOneShot(defaultScanner(), iface)
}

func restoreAfterOneShot(s scanner, iface string) {
	iface = strings.TrimSpace(iface)
	hasNM := s.hasBin == nil || s.hasBin("nmcli")
	if hasNM {
		// --kill may have terminated NetworkManager; starting an active unit is
		// a no-op, so always ask systemd to ensure it runs.
		_, _ = runPrivileged(s, 20*time.Second, "systemctl", "start", "NetworkManager")
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, err := s.runCmd(ctx, "nmcli", "-t", "general", "status")
			cancel()
			if err == nil {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		_, _ = runPrivileged(s, 10*time.Second, "nmcli", "networking", "on")
		if iface != "" {
			_, _ = runPrivileged(s, 10*time.Second, "nmcli", "device", "set", iface, "managed", "yes")
		}
	}
	if iface != "" {
		_, _ = runPrivileged(s, 10*time.Second, "ip", "link", "set", iface, "up")
	}
	_, _ = runPrivileged(s, 10*time.Second, "rfkill", "unblock", "wifi")
}

// restoreViaHelper restores networking through `czhax oneshot restore`, which
// works under the same sudo rule as the WPS run and also covers canceled runs
// where the privileged helper was killed. Falls back to best-effort commands.
func restoreViaHelper(iface string) {
	exe, err := os.Executable()
	if err != nil {
		RestoreAfterOneShot(iface)
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	args := []string{"oneshot", "restore", "--iface", iface}
	bin := exe
	if os.Geteuid() != 0 {
		bin = "sudo"
		args = append([]string{"-n", exe}, args...)
	}
	if err := exec.CommandContext(ctx, bin, args...).Run(); err != nil {
		RestoreAfterOneShot(iface)
	}
}

func cleanOneShotLine(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = ansi.Strip(s)
	s = strings.TrimRight(s, " \t")
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}
