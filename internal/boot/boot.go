package boot

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

const (
	serviceName = "czhax.service"
	servicePath = "/etc/systemd/system/czhax.service"
	profileBeg  = "# >>> czhax-autostart >>>"
	profileEnd  = "# <<< czhax-autostart <<<"
)

type Method string

const (
	MethodSystemd Method = "systemd"
	MethodProfile Method = "profile"
)

type State struct {
	Enabled bool
	Method  Method
}

func HasSystemd() bool {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func DetectState(homeDir string) State {
	if HasSystemd() {
		if _, err := os.Stat(servicePath); err == nil {
			out, err := exec.Command("systemctl", "is-enabled", serviceName).Output()
			if err == nil && strings.TrimSpace(string(out)) == "enabled" {
				return State{Enabled: true, Method: MethodSystemd}
			}
			// Also treat symlink presence as enabled if systemctl check is unavailable
			if _, err := os.Stat("/etc/systemd/system/multi-user.target.wants/" + serviceName); err == nil {
				return State{Enabled: true, Method: MethodSystemd}
			}
		}
		return State{Enabled: false, Method: MethodSystemd}
	}

	p := profilePath(homeDir)
	if data, err := os.ReadFile(p); err == nil {
		if strings.Contains(string(data), profileBeg) {
			return State{Enabled: true, Method: MethodProfile}
		}
	}
	return State{Enabled: false, Method: MethodProfile}
}

func NeedsRootForToggle() bool {
	return HasSystemd() && os.Geteuid() != 0
}

func RenderServiceUnit(username, homeDir, exePath string) string {
	if username == "" {
		username = "root"
	}
	if homeDir == "" {
		homeDir = "/root"
	}
	shellPath := detectUserShell(username)
	return fmt.Sprintf(`[Unit]
Description=CZHax TUI on tty1
After=systemd-user-sessions.service plymouth-quit-wait.service rc-local.service network.target
Before=getty.target
Conflicts=getty@tty1.service

[Service]
Type=idle
User=%s
WorkingDirectory=%s
Environment=TERM=linux
Environment=HOME=%s
Environment=USER=%s
Environment=LOGNAME=%s
Environment=SHELL=%s
ExecStartPre=+/bin/sh -c 'printf "0 4 0 7\n" >/proc/sys/kernel/printk 2>/dev/null || dmesg -n 1 >/dev/null 2>&1 || true; dmesg -D >/dev/null 2>&1 || true'
ExecStart=%s
ExecStopPost=+/bin/sh -c 'printf "4 4 1 7\n" >/proc/sys/kernel/printk 2>/dev/null || true; dmesg -E >/dev/null 2>&1 || true'
StandardInput=tty
StandardOutput=tty
StandardError=tty
TTYPath=/dev/tty1
TTYReset=yes
TTYVHangup=yes
TTYVTDisallocate=yes
Restart=always
RestartSec=1

[Install]
WantedBy=multi-user.target
`, username, homeDir, homeDir, username, username, shellPath, exePath)
}

func detectUserShell(username string) string {
	if data, err := os.ReadFile("/etc/passwd"); err == nil && username != "" {
		prefix := username + ":"
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, prefix) {
				fields := strings.Split(line, ":")
				if len(fields) >= 7 && strings.TrimSpace(fields[6]) != "" {
					return strings.TrimSpace(fields[6])
				}
				break
			}
		}
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

// SyncInstalledService refreshes czhax.service if systemd boot is already enabled
// (for example, during `sudo czhax update`).
func SyncInstalledService(exePath string) {
	if !HasSystemd() || os.Geteuid() != 0 {
		return
	}
	existing, err := os.ReadFile(servicePath)
	if err != nil {
		return
	}
	username := os.Getenv("SUDO_USER")
	homeDir := ""
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.HasPrefix(line, "User=") {
			username = strings.TrimSpace(strings.TrimPrefix(line, "User="))
		}
		if strings.HasPrefix(line, "Environment=HOME=") {
			homeDir = strings.TrimSpace(strings.TrimPrefix(line, "Environment=HOME="))
		}
	}
	if homeDir == "" && username != "" {
		if u, err := user.Lookup(username); err == nil {
			homeDir = u.HomeDir
		}
	}
	if exePath == "" {
		exePath, _ = os.Executable()
	}
	updated := RenderServiceUnit(username, homeDir, exePath)
	if string(existing) != updated {
		if os.WriteFile(servicePath, []byte(updated), 0o644) == nil {
			_ = exec.Command("systemctl", "daemon-reload").Run()
		}
	}
}

func Toggle(username, homeDir, exePath string) (State, error) {
	st := DetectState(homeDir)
	if st.Enabled {
		err := Disable(homeDir)
		return DetectState(homeDir), err
	}
	err := Enable(username, homeDir, exePath)
	return DetectState(homeDir), err
}

func Enable(username, homeDir, exePath string) error {
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return err
		}
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	if username == "" || homeDir == "" {
		u, err := user.Current()
		if err == nil {
			if username == "" {
				username = u.Username
			}
			if homeDir == "" {
				homeDir = u.HomeDir
			}
		}
	}

	if HasSystemd() {
		if os.Geteuid() != 0 {
			return fmt.Errorf("root required to enable %s (run with sudo)", serviceName)
		}
		unit := RenderServiceUnit(username, homeDir, exePath)
		if err := os.WriteFile(servicePath, []byte(unit), 0o644); err != nil {
			return err
		}
		if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		if out, err := exec.Command("systemctl", "enable", serviceName).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl enable: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	return enableProfileHook(profilePath(homeDir), exePath)
}

func Disable(homeDir string) error {
	if HasSystemd() {
		if os.Geteuid() != 0 {
			return fmt.Errorf("root required to disable %s (run with sudo)", serviceName)
		}
		_ = exec.Command("systemctl", "disable", serviceName).Run()
		_ = os.Remove(servicePath)
		_ = exec.Command("systemctl", "daemon-reload").Run()
		return nil
	}

	return disableProfileHook(profilePath(homeDir))
}

func profilePath(homeDir string) string {
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}
	return filepath.Join(homeDir, ".bash_profile")
}

func enableProfileHook(path, exePath string) error {
	existing := ""
	if b, err := os.ReadFile(path); err == nil {
		existing = string(b)
	} else if os.IsNotExist(err) {
		// Preserve ~/.bashrc sourcing when creating ~/.bash_profile from scratch
		existing = "[ -f ~/.bashrc ] && . ~/.bashrc\n"
	} else {
		return err
	}

	if strings.Contains(existing, profileBeg) {
		return nil
	}
	block := fmt.Sprintf("\n%s\nif [ -t 0 ] && [ -z \"${CZHAX_NESTED:-}\" ] && [ \"$(tty 2>/dev/null)\" = \"/dev/tty1\" ]; then\n  %s\nfi\n%s\n",
		profileBeg, exePath, profileEnd)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(existing+block), 0o644)
}

func disableProfileHook(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s := string(b)
	start := strings.Index(s, profileBeg)
	if start < 0 {
		return nil
	}
	end := strings.Index(s[start:], profileEnd)
	if end < 0 {
		return nil
	}
	endIdx := start + end + len(profileEnd)
	if endIdx < len(s) && s[endIdx] == '\n' {
		endIdx++
	}
	cleaned := strings.TrimRight(s[:start], "\n") + "\n" + strings.TrimLeft(s[endIdx:], "\n")
	if strings.TrimSpace(cleaned) == "" {
		cleaned = ""
	}
	return os.WriteFile(path, []byte(cleaned), 0o644)
}
