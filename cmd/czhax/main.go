package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/boot"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/console"
	"github.com/czhax/czhax/internal/ir"
	"github.com/czhax/czhax/internal/power"
	"github.com/czhax/czhax/internal/ui"
	"github.com/czhax/czhax/internal/updater"
	"github.com/czhax/czhax/internal/wifi"
)

var version = "0.2.0"

func valueOrNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

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
		if exePath, err := updater.ExecutablePath(); err == nil {
			boot.SyncInstalledService(exePath)
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

	if len(os.Args) > 1 && os.Args[1] == "wifi" {
		if len(os.Args) > 2 && os.Args[2] == "pentest" {
			fs := flag.NewFlagSet("wifi pentest", flag.ExitOnError)
			scanIface := fs.String("scan-iface", "wlan0", "Managed/scan interface")
			monIface := fs.String("mon-iface", "", "Pre-existing monitor interface")
			bssid := fs.String("bssid", "", "Target AP BSSID")
			ssid := fs.String("ssid", "", "Target AP SSID")
			channel := fs.Int("channel", 0, "Target AP channel")
			modeStr := fs.String("mode", "handshake", "Pentest mode: handshake|deauth|both")
			duration := fs.Duration("duration", 30*time.Second, "Run duration")
			saveDir := fs.String("save-dir", "", "Capture output directory")
			uid := fs.Int("uid", -1, "Owner UID for capture files")
			gid := fs.Int("gid", -1, "Owner GID for capture files")
			_ = fs.Parse(os.Args[3:])

			mode, err := wifi.ParsePentestMode(*modeStr)
			if err != nil {
				out, _ := json.Marshal(wifi.PentestHelperOutcome{Error: err.Error()})
				fmt.Println(string(out))
				return
			}
			req := wifi.PentestRequest{
				ScanIface:    *scanIface,
				MonitorIface: *monIface,
				BSSID:        *bssid,
				SSID:         *ssid,
				Channel:      *channel,
				Mode:         mode,
				Duration:     *duration,
				SaveDir:      *saveDir,
			}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			wifi.WatchStdinCancel(os.Stdin, cancel)

			res, err := wifi.RunPentestDirect(ctx, req)
			wifi.ChownCapture(req.SaveDir, res.CapturePath, *uid, *gid)
			outcome := wifi.PentestHelperOutcome{
				Result:   res,
				Canceled: errors.Is(ctx.Err(), context.Canceled) && err == nil,
			}
			if err != nil {
				outcome.Error = err.Error()
			}
			out, _ := json.Marshal(outcome)
			fmt.Println(string(out))
			return
		}
		fmt.Fprintln(os.Stderr, "usage: czhax wifi pentest [flags]")
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "ir" {
		hw := ir.DiscoverHardware()
		fmt.Printf("IR receiver: %s\n", valueOrNone(hw.RXPath))
		fmt.Printf("IR transmitter: %s\n", valueOrNone(hw.TXPath))
		for _, d := range hw.Paths {
			fmt.Printf("  %s features=%#08x rx=%v tx=%v", d.Path, d.Features, d.Receiver, d.Transmitter)
			if d.Err != "" {
				fmt.Printf(" error=%s", d.Err)
			}
			fmt.Println()
		}
		if hw.Err != "" {
			fmt.Fprintln(os.Stderr, "IR: "+hw.Err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "power" {
		sub := ""
		if len(os.Args) > 2 {
			sub = os.Args[2]
		}
		switch sub {
		case "sleep":
			if err := power.RunSleepCommand(); err != nil {
				fmt.Fprintf(os.Stderr, "sleep error: %v\n", err)
				os.Exit(1)
			}
		case "shutdown":
			if err := power.Shutdown(); err != nil {
				fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
				os.Exit(1)
			}
		case "reboot":
			if err := power.Reboot(); err != nil {
				fmt.Fprintf(os.Stderr, "reboot error: %v\n", err)
				os.Exit(1)
			}
		case "probe":
			fmt.Println("Input devices:")
			for _, line := range power.ProbeInputDevices() {
				fmt.Println(" ", line)
			}
			fmt.Println("GPIO wake lines:")
			for _, line := range power.ProbeGPIOLines() {
				fmt.Println(" ", line)
			}
		default:
			fmt.Fprintln(os.Stderr, "usage: czhax power <sleep|reboot|shutdown|probe>")
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
		if exePath, err := updater.ExecutablePath(); err == nil {
			boot.SyncInstalledService(exePath)
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

	guard := console.SuppressKernelLogs()

	model := ui.NewAppModel(cfg, cfgPath, version)
	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	guard.Restore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "czhax error: %v\n", err)
		os.Exit(1)
	}

	if app, ok := finalModel.(ui.AppModel); ok && app.RestartOnQuit {
		exePath, err := updater.ExecutablePath()
		if err != nil {
			exePath = os.Args[0]
		}
		if err := syscall.Exec(exePath, os.Args, os.Environ()); err != nil {
			fmt.Fprintf(os.Stderr, "restart error: %v\n", err)
			os.Exit(1)
		}
	}
}
