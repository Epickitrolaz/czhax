package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/ui"
	"github.com/czhax/czhax/internal/updater"
)

var version = "0.2.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "update" {
		force := false
		for _, a := range os.Args[2:] {
			if a == "--force" || a == "-f" {
				force = true
			}
		}
		if err := updater.Run(version, force); err != nil {
			fmt.Fprintf(os.Stderr, "update error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	czMode := flag.Bool("cz", false, "Render inside a 53x14 Cardputer Zero viewport frame")
	cz45 := flag.Bool("cz45", false, "Render inside a compact 45x10 Cardputer Zero viewport frame")
	updateFlag := flag.Bool("update", false, "Update czhax to the latest GitHub release")
	forceFlag := flag.Bool("force", false, "Force reinstall when running --update")
	cfgFlag := flag.String("config", "", "Custom config file path")
	verFlag := flag.Bool("version", false, "Print CZHax version and exit")
	flag.Parse()

	if *verFlag {
		fmt.Printf("czhax %s\n", version)
		return
	}

	if *updateFlag {
		if err := updater.Run(version, *forceFlag); err != nil {
			fmt.Fprintf(os.Stderr, "update error: %v\n", err)
			os.Exit(1)
		}
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

	model := ui.NewAppModel(cfg, cfgPath)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "czhax error: %v\n", err)
		os.Exit(1)
	}
}
