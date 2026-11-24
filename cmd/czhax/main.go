package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/boot"
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

	if len(os.Args) > 1 && os.Args[1] == "boot" {
		sub := ""
		if len(os.Args) > 2 {
			sub = os.Args[2]
		}
		username := os.Getenv("SUDO_USER")
		homeDir := ""
		if len(os.Args) > 3 {
			username = os.Args[3]
		}
		if len(os.Args) > 4 {
			homeDir = os.Args[4]
		}
		exePath, _ := updater.ExecutablePath()
		switch sub {
		case "enable":
			if err := boot.Enable(username, homeDir, exePath); err != nil {
				fmt.Fprintf(os.Stderr, "boot enable error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Start on boot enabled.")
		case "disable":
			if err := boot.Disable(homeDir); err != nil {
				fmt.Fprintf(os.Stderr, "boot disable error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Start on boot disabled.")
		case "toggle":
			st, err := boot.Toggle(username, homeDir, exePath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "boot toggle error: %v\n", err)
				os.Exit(1)
			}
			if st.Enabled {
				fmt.Printf("Start on boot: ON (%s)\n", st.Method)
			} else {
				fmt.Printf("Start on boot: OFF (%s)\n", st.Method)
			}
		default:
			st := boot.DetectState(homeDir)
			fmt.Printf("enabled=%v method=%s\n", st.Enabled, st.Method)
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

	model := ui.NewAppModel(cfg, cfgPath, version)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "czhax error: %v\n", err)
		os.Exit(1)
	}
}
