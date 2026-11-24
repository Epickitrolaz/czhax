package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type ViewportMode string

const (
	ViewportAuto   ViewportMode = "auto"
	ViewportCZ5314 ViewportMode = "cz53x14"
	ViewportCZ4510 ViewportMode = "cz45x10"
	ViewportWide   ViewportMode = "wide"
)

type BorderMode string

const (
	BorderCompact BorderMode = "compact"
	BorderMinimal BorderMode = "minimal"
	BorderAscii   BorderMode = "ascii"
)

type NavKeyMode string

const (
	NavArrowsVim NavKeyMode = "arrows_vim"
	NavArrows    NavKeyMode = "arrows"
	NavFnSoft    NavKeyMode = "fn_soft"
)

type Config struct {
	Theme          string       `json:"theme"`
	Viewport       ViewportMode `json:"viewport"`
	Border         BorderMode   `json:"border"`
	ShowKeyHints   bool         `json:"show_key_hints"`
	ShowFnBar      bool         `json:"show_fn_bar"`
	NavMode        NavKeyMode   `json:"nav_mode"`
	RefreshSeconds int          `json:"refresh_seconds"`
	StealthMode    bool         `json:"stealth_mode"`
	ConfirmQuit    bool         `json:"confirm_quit"`
	PinnedIDs      []string     `json:"pinned_ids"`
}

func Default() Config {
	return Config{
		Theme:          "cyber-green",
		Viewport:       ViewportAuto,
		Border:         BorderCompact,
		ShowKeyHints:   true,
		ShowFnBar:      false,
		NavMode:        NavArrowsVim,
		RefreshSeconds: 2,
		StealthMode:    false,
		ConfirmQuit:    true,
		PinnedIDs:      []string{"wifi-recon", "net-scan", "sys-mon", "net-if"},
	}
}

func DefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			base = "/tmp"
		} else {
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, "czhax", "config.json")
}

func NotesPath() string {
	return filepath.Join(filepath.Dir(DefaultPath()), "notes.txt")
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	cfg.normalize()
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath()
	}
	cfg.normalize()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c *Config) normalize() {
	if c.Theme == "" {
		c.Theme = "cyber-green"
	}
	switch c.Viewport {
	case ViewportAuto, ViewportCZ5314, ViewportCZ4510, ViewportWide:
	default:
		c.Viewport = ViewportAuto
	}
	switch c.Border {
	case BorderCompact, BorderMinimal, BorderAscii:
	default:
		c.Border = BorderCompact
	}
	switch c.NavMode {
	case NavArrowsVim, NavArrows, NavFnSoft:
	default:
		c.NavMode = NavArrowsVim
	}
	if c.RefreshSeconds < 0 {
		c.RefreshSeconds = 0
	}
	if c.RefreshSeconds > 60 {
		c.RefreshSeconds = 60
	}
	if len(c.PinnedIDs) == 0 {
		c.PinnedIDs = Default().PinnedIDs
	}
}
