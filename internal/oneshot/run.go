package oneshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(stderr, "run oneshot: %v\n", err)
		return 1
	}
	return 0
}
