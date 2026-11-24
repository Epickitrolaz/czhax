package console

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSuppressAndRestorePrintk(t *testing.T) {
	p := filepath.Join(t.TempDir(), "printk")
	if err := os.WriteFile(p, []byte("7\t4\t1\t7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := &Guard{}
	g.suppressPrintk(p)

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "0 4 0 7\n" {
		t.Fatalf("suppressed printk = %q, want %q", got, "0 4 0 7\n")
	}

	g.Restore()
	b2, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b2); got != "7\t4\t1\t7\n" {
		t.Fatalf("restored printk = %q, want %q", got, "7\t4\t1\t7\n")
	}
}
