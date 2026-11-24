package boot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderServiceUnit(t *testing.T) {
	unit := RenderServiceUnit("pi", "/home/pi", "/usr/local/bin/czhax")
	for _, want := range []string{
		"Type=idle",
		"After=systemd-user-sessions.service plymouth-quit-wait.service rc-local.service network.target",
		"Before=getty.target",
		"User=pi",
		"WorkingDirectory=/home/pi",
		"Environment=HOME=/home/pi",
		"Environment=SHELL=",
		"ExecStartPre=+",
		"ExecStart=/usr/local/bin/czhax",
		"StandardError=tty",
		"Conflicts=getty@tty1.service",
		"TTYPath=/dev/tty1",
		"TTYVTDisallocate=yes",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("expected %q in unit:\n%s", want, unit)
		}
	}
}

func TestProfileHookRoundTrip(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".bash_profile")

	if err := enableProfileHook(p, "/usr/local/bin/czhax"); err != nil {
		t.Fatalf("enableProfileHook failed: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), profileBeg) || !strings.Contains(string(data), "/usr/local/bin/czhax") {
		t.Fatalf("expected autostart block in %s:\n%s", p, string(data))
	}

	if err := disableProfileHook(p); err != nil {
		t.Fatalf("disableProfileHook failed: %v", err)
	}
	data2, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data2), profileBeg) {
		t.Fatalf("expected autostart block removed, got:\n%s", string(data2))
	}
	if !strings.Contains(string(data2), ".bashrc") {
		t.Fatalf("expected .bashrc sourcing preserved, got:\n%s", string(data2))
	}
}
