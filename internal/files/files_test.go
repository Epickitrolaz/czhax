package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListWriteRenameDelete(t *testing.T) {
	root := t.TempDir()
	if err := CreateText(root, "note.txt"); err != nil {
		t.Fatal(err)
	}
	if err := Mkdir(root, "folder"); err != nil {
		t.Fatal(err)
	}
	entries, err := List(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].Dir || entries[0].Name != "folder" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if err := WriteText(root, "note.txt", []string{"hello", "world"}); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadText(root, "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[1] != "world" {
		t.Fatalf("unexpected text: %+v", lines)
	}
	if _, err := Rename(root, "note.txt", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	if err := Delete(root, "folder"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "folder")); !os.IsNotExist(err) {
		t.Fatalf("folder still exists: %v", err)
	}
}

func TestJoinRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../x", "/etc/passwd", "a/../../x"} {
		if _, err := Join(root, rel); err == nil {
			t.Fatalf("expected %q to be rejected", rel)
		}
	}
	if _, err := Join(root, "a/b.txt"); err != nil {
		t.Fatalf("expected nested path to be accepted: %v", err)
	}
}
