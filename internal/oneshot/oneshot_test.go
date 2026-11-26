package oneshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractEmbeddedTree(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	src, err := Extract()
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !src.Embedded {
		t.Fatal("expected embedded source")
	}
	if !strings.HasPrefix(src.Dir, cache) {
		t.Fatalf("extracted dir %q not under %q", src.Dir, cache)
	}
	for _, path := range []string{
		src.Script,
		src.VulnList,
		filepath.Join(src.Dir, "src", "wps", "connection.py"),
		filepath.Join(src.Dir, "LICENSE"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing extracted file %s: %v", path, err)
		}
	}
	if data, err := os.ReadFile(src.VulnList); err != nil || len(data) < 100 {
		t.Fatalf("vulnwsc.txt not extracted correctly: %v (%d bytes)", err, len(data))
	}
	if src.Commit == "" || src.Commit == "unknown" {
		t.Fatalf("missing upstream commit, got %q", src.Commit)
	}

	// A second call must reuse the same directory (idempotent).
	again, err := Extract()
	if err != nil {
		t.Fatalf("second Extract: %v", err)
	}
	if again.Dir != src.Dir {
		t.Fatalf("expected cached dir %q, got %q", src.Dir, again.Dir)
	}
}

func TestCommitMetadata(t *testing.T) {
	if len(Commit()) < 7 {
		t.Fatalf("Commit() = %q", Commit())
	}
	if len(ShortCommit()) > 12 || ShortCommit() == "" {
		t.Fatalf("ShortCommit() = %q", ShortCommit())
	}
}
