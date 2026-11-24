package ui

import (
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// ttyExecCommand wraps *exec.Cmd for tea.Exec so that stderr is routed to the
// active TTY (stdout) whenever the parent process's os.Stderr is not a terminal
// (e.g. when launched by systemd with StandardError=journal).
type ttyExecCommand struct {
	*exec.Cmd
	ttyOut io.Writer
}

func (c *ttyExecCommand) SetStdin(r io.Reader) {
	c.Cmd.Stdin = r
}

func (c *ttyExecCommand) SetStdout(w io.Writer) {
	c.ttyOut = w
	c.Cmd.Stdout = w
	if c.Cmd.Stderr == nil {
		c.Cmd.Stderr = w
	}
}

func (c *ttyExecCommand) SetStderr(w io.Writer) {
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		c.Cmd.Stderr = w
		return
	}
	if c.ttyOut != nil {
		c.Cmd.Stderr = c.ttyOut
	} else {
		c.Cmd.Stderr = w
	}
}

func resolveLoginShell() string {
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		if st, err := os.Stat(sh); err == nil && !st.IsDir() {
			return sh
		}
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		if data, err := os.ReadFile("/etc/passwd"); err == nil {
			prefix := u.Username + ":"
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, prefix) {
					fields := strings.Split(line, ":")
					if len(fields) >= 7 && strings.TrimSpace(fields[6]) != "" {
						sh := strings.TrimSpace(fields[6])
						if st, err := os.Stat(sh); err == nil && !st.IsDir() {
							return sh
						}
					}
					break
				}
			}
		}
	}
	for _, candidate := range []string{"/bin/bash", "/usr/bin/bash", "/bin/sh"} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return "/bin/sh"
}

func buildInteractiveShellCmd() *ttyExecCommand {
	sh := resolveLoginShell()
	base := filepath.Base(sh)
	execLine := `printf "\n[CZHax] Terminal shell — type 'exit' or press Ctrl+D to return.\n\n"; stty sane 2>/dev/null || true; exec "$1" -il`
	if base == "sh" || base == "dash" {
		execLine = `printf "\n[CZHax] Terminal shell — type 'exit' or press Ctrl+D to return.\n\n"; stty sane 2>/dev/null || true; exec "$1" -i`
	}

	c := exec.Command("/bin/sh", "-c", execLine, "_", sh)
	env := append([]string{}, os.Environ()...)
	env = append(env, "CZHAX_NESTED=1", "SHELL="+sh)
	if os.Getenv("TERM") == "" {
		env = append(env, "TERM=linux")
	}
	if u, err := user.Current(); err == nil {
		if os.Getenv("HOME") == "" && u.HomeDir != "" {
			env = append(env, "HOME="+u.HomeDir)
			c.Dir = u.HomeDir
		}
		if os.Getenv("USER") == "" && u.Username != "" {
			env = append(env, "USER="+u.Username, "LOGNAME="+u.Username)
		}
	}
	if c.Dir == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			c.Dir = home
		}
	}
	c.Env = env
	return &ttyExecCommand{Cmd: c}
}
