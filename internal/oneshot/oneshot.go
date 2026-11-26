// Package oneshot embeds OneShot-Extended (GPL-2.0) so CZHax can run WPS
// attacks without downloading the upstream project first.
//
// Upstream: https://github.com/chkndrp/OneShot-Extended
package oneshot

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed upstream
var files embed.FS

// Source describes an extracted OneShot-Extended tree.
type Source struct {
	Dir      string
	Script   string
	VulnList string
	Commit   string
	Embedded bool
}

// Commit returns the pinned upstream commit baked into this binary.
func Commit() string {
	raw, err := files.ReadFile("upstream/UPSTREAM-COMMIT")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(raw))
}

// ShortCommit is the first 12 characters of Commit (cache directory tag).
func ShortCommit() string {
	c := Commit()
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// Extract writes the embedded OneShot-Extended tree to the user cache
// directory (once per pinned commit) and returns its paths.
func Extract() (Source, error) {
	commit := ShortCommit()
	root, err := cacheRoot()
	if err != nil {
		return Source{}, err
	}
	dir := filepath.Join(root, "oneshot-"+commit)
	if ready(dir) {
		return sourceFor(dir, commit), nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Source{}, fmt.Errorf("create cache dir: %w", err)
	}
	tmp, err := os.MkdirTemp(root, "oneshot-"+commit+".tmp-")
	if err != nil {
		return Source{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	if _, err := ExtractAt(tmp); err != nil {
		return Source{}, err
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ready"), []byte(commit), 0o644); err != nil {
		return Source{}, err
	}
	if err := os.Rename(tmp, dir); err != nil {
		// Another process may have won the race; reuse its tree.
		if ready(dir) {
			return sourceFor(dir, commit), nil
		}
		return Source{}, fmt.Errorf("install OneShot-Extended to %s: %w", dir, err)
	}
	return sourceFor(dir, commit), nil
}

// ExtractAt writes the embedded OneShot-Extended tree into dir. It is used for
// private root-owned run directories where the user cache must not be trusted.
func ExtractAt(dir string) (Source, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Source{}, fmt.Errorf("create extract dir: %w", err)
	}
	err := fs.WalkDir(files, "upstream", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("upstream", path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		data, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if strings.HasSuffix(dst, "ose.py") {
			mode = 0o700
		}
		return os.WriteFile(dst, data, mode)
	})
	if err != nil {
		return Source{}, fmt.Errorf("extract OneShot-Extended: %w", err)
	}
	return sourceFor(dir, ShortCommit()), nil
}

func sourceFor(dir, commit string) Source {
	return Source{
		Dir:      dir,
		Script:   filepath.Join(dir, "ose.py"),
		VulnList: filepath.Join(dir, "vulnwsc.txt"),
		Commit:   commit,
		Embedded: true,
	}
}

func ready(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".ready"))
	return err == nil
}

func cacheRoot() (string, error) {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "czhax"), nil
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "czhax"), nil
	}
	return "", fmt.Errorf("no cache directory available")
}
