package bluetooth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func resolveBin(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

// NeedsSudo reports whether a privileged action would currently need an
// interactive sudo password (no root and no cached passwordless sudo).
func NeedsSudo() bool {
	return os.Geteuid() != 0 && !canSudo()
}

// canSudo reports whether passwordless sudo is usable without prompting.
func canSudo() bool {
	if os.Geteuid() == 0 {
		return true
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return false
	}
	return exec.Command("sudo", "-n", "true").Run() == nil
}

// privilegedPrefix returns the command prefix needed to run a privileged
// helper. The boolean is false when no usable root path exists.
func privilegedPrefix(bin string) (string, []string, bool) {
	if os.Geteuid() == 0 {
		return resolveBin(bin), nil, true
	}
	if !canSudo() {
		return "", nil, false
	}
	return "sudo", []string{"-n", resolveBin(bin)}, true
}

func commandOutput(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func commandOutputInput(ctx context.Context, bin, input string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// streamCommand runs bin and forwards stdout/stderr line by line to onLine.
// Cancelling ctx terminates the whole process group.
func streamCommand(ctx context.Context, bin string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 3 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var mu sync.Mutex
	var tail string
	emit := func(line string) {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			return
		}
		mu.Lock()
		tail = line
		mu.Unlock()
		if onLine != nil {
			onLine(line)
		}
	}

	var wg sync.WaitGroup
	drain := func(r interface{ Read([]byte) (int, error) }) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 4096), 256*1024)
		for sc.Scan() {
			emit(sc.Text())
		}
	}
	wg.Add(2)
	go drain(stdout)
	go drain(stderr)

	waitErr := cmd.Wait()
	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr == nil {
		return nil
	}
	mu.Lock()
	last := tail
	mu.Unlock()
	low := strings.ToLower(last)
	if strings.Contains(low, "password is required") || strings.Contains(low, "not in the sudoers") {
		return fmt.Errorf("root or passwordless sudo required: %s", last)
	}
	if last != "" && !errors.Is(waitErr, exec.ErrNotFound) {
		return fmt.Errorf("%s: %w", last, waitErr)
	}
	return waitErr
}

// streamPrivileged runs bin with sudo -n when needed and available.
func streamPrivileged(ctx context.Context, bin string, args []string, onLine func(string)) error {
	full, prefix, ok := privilegedPrefix(bin)
	if !ok {
		return errors.New("root or passwordless sudo required")
	}
	if len(prefix) > 0 {
		args = append(prefix, args...)
		bin = "sudo"
	} else {
		bin = full
	}
	return streamCommand(ctx, bin, args, onLine)
}

// capturePrivileged runs a short-lived privileged command and returns its
// combined output.
func capturePrivileged(ctx context.Context, bin string, args ...string) (string, error) {
	full, prefix, ok := privilegedPrefix(bin)
	if !ok {
		return "", errors.New("root or passwordless sudo required")
	}
	if len(prefix) > 0 {
		args = append(prefix, args...)
		bin = "sudo"
	} else {
		bin = full
	}
	return commandOutput(ctx, bin, args...)
}

// executablePath resolves the running CZHax binary for privileged re-exec.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		return resolved, nil
	}
	return exe, nil
}
