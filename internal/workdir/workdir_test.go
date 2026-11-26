package workdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAndEnsure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "czhax")
	if err := Ensure(root); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, Captures(root), BT(root), Recordings(root)} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("missing %s: %v", dir, err)
		}
	}
	if got := Resolve("~/x"); !strings.HasSuffix(got, string(os.PathSeparator)+"x") {
		t.Fatalf("Resolve(~/x) = %q", got)
	}
	if got := Resolve(""); got == "" {
		t.Fatal("Resolve(\"\") is empty")
	}
}
