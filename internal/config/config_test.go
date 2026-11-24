package config

import (
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "czhax", "config.json")

	cfg := Default()
	cfg.Viewport = ViewportCZ5314
	cfg.ShowFnBar = true
	cfg.StealthMode = true
	cfg.RefreshSeconds = 5

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got.Theme != "mono-hi" || got.Viewport != ViewportCZ5314 || !got.ShowFnBar || !got.StealthMode || got.RefreshSeconds != 5 {
		t.Fatalf("loaded config mismatch: %+v", got)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	got, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Theme != Default().Theme {
		t.Fatalf("expected default theme %q, got %q", Default().Theme, got.Theme)
	}
}
