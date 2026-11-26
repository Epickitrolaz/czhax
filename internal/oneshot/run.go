package oneshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// FindPython returns the python3/python interpreter path (with python3
// preferred), or an error when none is installed.
func FindPython() (string, error) {
	for _, cand := range []string{"python3", "python"} {
		if p, err := exec.LookPath(cand); err == nil {
			return p, nil
		}
	}
	return "", errors.New("python3 not found (OneShot-Extended needs Python 3.11+)")
}

// RunAsRoot executes the embedded OneShot-Extended snapshot from a private
// directory. It is intended for the privileged helper invoked through sudo, so
// it never executes from the user-writable cache. Returns the process exit
// code.
func RunAsRoot(ctx context.Context, iface, bssid string, attackArgs []string, stdout, stderr io.Writer) int {
	py, err := FindPython()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if filepath.IsAbs(py) == false {
		fmt.Fprintln(stderr, "python path is not absolute")
		return 1
	}
	base := "/run/czhax"
	if err := os.MkdirAll(base, 0o700); err != nil {
		base = os.TempDir()
	}
	dir, err := os.MkdirTemp(base, "oneshot-")
	if err != nil {
		fmt.Fprintf(stderr, "create run dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)
	src, err := ExtractAt(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	args := append([]string{src.Script, "-i", iface, "-b", bssid}, attackArgs...)
	cmd := exec.CommandContext(ctx, py, args...)
	cmd.Dir = src.Dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	restoreNetwork(iface)
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(stderr, "run oneshot: %v\n", runErr)
		return 1
	}
	return 0
}

// RestoreNetwork restores networking after OneShot's --kill. It is used by the
// privileged `czhax oneshot restore` helper.
func RestoreNetwork(iface string) { restoreNetwork(iface) }

// restoreNetwork runs with root privileges from the helper and undoes the
// effects of OneShot's --kill.
func restoreNetwork(iface string) {
	run := func(timeout time.Duration, name string, args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		c := exec.CommandContext(ctx, name, args...)
		return c.Run()
	}
	_ = run(20*time.Second, "systemctl", "start", "NetworkManager")
	if _, err := exec.LookPath("nmcli"); err == nil {
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			if err := run(2*time.Second, "nmcli", "-t", "general", "status"); err == nil {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		_ = run(10*time.Second, "nmcli", "networking", "on")
		if iface != "" {
			_ = run(10*time.Second, "nmcli", "device", "set", iface, "managed", "yes")
		}
	}
	if iface != "" {
		_ = run(10*time.Second, "ip", "link", "set", iface, "up")
	}
	_ = run(10*time.Second, "rfkill", "unblock", "wifi")
}
