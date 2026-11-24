package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/ui"
)

var version = "0.1.0-phase1"

func main() {
	czMode := flag.Bool("cz", false, "Render inside a 53x14 Cardputer Zero viewport frame")
	cz45 := flag.Bool("cz45", false, "Render inside a compact 45x10 Cardputer Zero viewport frame")
	themeFlag := flag.String("theme", "", "Override theme (cyber-green, purple-hax, amber-ops, ice-cyan, mono-hi)")
	cfgFlag := flag.String("config", "", "Custom config file path")
	verFlag := flag.Bool("version", false, "Print CZHax version and exit")
	flag.Parse()

	if *verFlag {
		fmt.Printf("czhax %s\n", version)
		return
	}

	cfgPath := *cfgFlag
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load config (%v), using defaults\n", err)
	}

	if *czMode {
		cfg.Viewport = config.ViewportCZ5314
	}
	if *cz45 {
		cfg.Viewport = config.ViewportCZ4510
	}
	if *themeFlag != "" {
		cfg.Theme = *themeFlag
	}

	model := ui.NewAppModel(cfg, cfgPath)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "czhax error: %v\n", err)
		os.Exit(1)
	}
}
