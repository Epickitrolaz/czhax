package workdir

import (
	"os"
	"path/filepath"
	"strings"
)

// DirName is the default visible working directory under the user's home.
const DirName = "czhax"

// Default returns ~/czhax (or /tmp/czhax when no home directory is available).
func Default() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, DirName)
	}
	return filepath.Join(os.TempDir(), DirName)
}

// Resolve expands a configured path (including a leading ~) or returns Default.
func Resolve(configured string) string {
	configured = ExpandHome(strings.TrimSpace(configured))
	if configured == "" {
		return Default()
	}
	return configured
}

// ExpandHome expands a leading ~ or ~/ to the current user's home.
func ExpandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// Ensure creates the working directory and its standard subdirectories.
func Ensure(root string) error {
	for _, dir := range []string{root, Captures(root), BT(root), Recordings(root)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func Captures(root string) string   { return filepath.Join(root, "captures") }
func BT(root string) string         { return filepath.Join(root, "bt") }
func Recordings(root string) string { return filepath.Join(root, "recordings") }
