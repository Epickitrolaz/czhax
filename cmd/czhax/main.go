package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/boot"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/console"
	"github.com/czhax/czhax/internal/ir"
	"github.com/czhax/czhax/internal/oneshot"
	"github.com/czhax/czhax/internal/power"
	"github.com/czhax/czhax/internal/sdr"
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

// parseRateOr parses an IQ rate like "2.048M" or "1024000", falling back to def.
func parseRateOr(raw string, def int64) int64 {
	s := strings.TrimSpace(strings.ToLower(raw))
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSuffix(s, "m")
		mult = 1_000_000
	case strings.HasSuffix(s, "k"):
		s = strings.TrimSuffix(s, "k")
		mult = 1_000
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return def
	}
	rate := int64(v * mult)
	if rate < 225_000 {
		rate = 225_000
	}
	if rate > 3_200_000 {
		rate = 3_200_000
	}
	return rate
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

	if len(os.Args) > 1 && os.Args[1] == "oneshot" {
		if len(os.Args) > 2 && os.Args[2] == "run" {
			fs := flag.NewFlagSet("oneshot run", flag.ExitOnError)
			iface := fs.String("iface", "wlan0", "Wireless interface")
			bssid := fs.String("bssid", "", "Target AP BSSID")
			attack := fs.String("attack", "-P -k", "OneShot attack arguments")
			_ = fs.Parse(os.Args[3:])
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			code := oneshot.RunAsRoot(ctx, *iface, *bssid, strings.Fields(*attack), os.Stdout, os.Stderr)
			os.Exit(code)
		}
		if len(os.Args) > 2 && os.Args[2] == "restore" {
			fs := flag.NewFlagSet("oneshot restore", flag.ExitOnError)
			iface := fs.String("iface", "", "Wireless interface")
			_ = fs.Parse(os.Args[3:])
			oneshot.RestoreNetwork(*iface)
			return
		}
		fmt.Fprintln(os.Stderr, "usage: czhax oneshot run --iface wlan0 --bssid AA:BB:.. [--attack \"-P -k\"]")
		os.Exit(1)
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
			req.DeauthTrigger = wifi.WatchPentestStdin(os.Stdin, cancel)

			res, err := wifi.RunPentestDirectWithProgress(ctx, req, func(prog wifi.PentestResult) {
				out, _ := json.Marshal(wifi.PentestHelperOutcome{
					Result:   prog,
					Progress: true,
				})
				fmt.Println(string(out))
			})
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
		if len(os.Args) > 2 && os.Args[2] == "spam" {
			fs := flag.NewFlagSet("wifi spam", flag.ExitOnError)
			iface := fs.String("iface", "wlan0", "Managed interface used for the monitor")
			monIface := fs.String("monitor", "", "Pre-existing monitor interface")
			channel := fs.Int("channel", 6, "Channel to flood")
			ssid := fs.String("ssid", "", "Single SSID to advertise")
			prefix := fs.String("prefix", "", "SSID prefix for numbered names")
			count := fs.Int("count", 0, "How many numbered SSIDs to generate")
			file := fs.String("file", "", "File with one SSID per line")
			randomMAC := fs.Bool("random-mac", false, "New random BSSID for every beacon")
			interval := fs.Int("interval", 15, "Delay between beacons (ms)")
			_ = fs.Parse(os.Args[3:])

			var ssids []string
			switch {
			case strings.TrimSpace(*file) != "":
				list, err := wifi.LoadSSIDFile(*file)
				if err != nil {
					fmt.Fprintf(os.Stderr, "wifi spam: %v\n", err)
					os.Exit(1)
				}
				ssids = list
			case strings.TrimSpace(*prefix) != "" && *count > 0:
				ssids = wifi.NumberedSSIDs(*prefix, *count)
			case strings.TrimSpace(*ssid) != "":
				ssids = []string{*ssid}
			default:
				fmt.Fprintln(os.Stderr, "wifi spam: give --ssid, --file or --prefix with --count")
				os.Exit(1)
			}
			bcfg := wifi.BeaconConfig{
				Iface:        *iface,
				MonitorIface: *monIface,
				Channel:      *channel,
				SSIDs:        ssids,
				RandomMAC:    *randomMAC,
				IntervalMS:   *interval,
			}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			if err := wifi.RunBeaconSpam(ctx, bcfg, func(line string) { fmt.Println(line) }); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "wifi spam error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if len(os.Args) > 2 && os.Args[2] == "evil" {
			fs := flag.NewFlagSet("wifi evil", flag.ExitOnError)
			iface := fs.String("iface", "wlan0", "AP interface")
			uplink := fs.String("uplink", "", "Uplink interface for internet pass-through")
			ssid := fs.String("ssid", "", "SSID to clone")
			channel := fs.Int("channel", 6, "Channel")
			nat := fs.Bool("nat", false, "NAT clients to the uplink")
			portal := fs.Bool("portal", false, "Serve a captive portal")
			portalMode := fs.String("portal-mode", "login", "Portal page: login|custom|off")
			portalFile := fs.String("portal-file", "", "Custom portal HTML page")
			dnsRules := fs.String("dns", "", "DNS redirects: \"host=ip,host2=ip\" (* = all)")
			pmkid := fs.Bool("pmkid", false, "Harvest PMKIDs with hcxdumptool")
			deauth := fs.String("deauth", "", "Original BSSID to deauth")
			workDir := fs.String("work-dir", "", "Directory for generated configs")
			caps := fs.String("capture-dir", "", "Directory for captures")
			uid := fs.Int("uid", -1, "Owner UID for captures")
			gid := fs.Int("gid", -1, "Owner GID for captures")
			_ = fs.Parse(os.Args[3:])

			mode := wifi.ParsePortalMode(*portalMode)
			ecfg := wifi.EvilConfig{
				Iface:       *iface,
				UplinkIface: *uplink,
				SSID:        *ssid,
				Channel:     *channel,
				NAT:         *nat,
				Portal:      *portal || mode != wifi.PortalOff,
				PortalMode:  string(mode),
				PortalFile:  *portalFile,
				DNSRules:    wifi.ParseDNSRules(*dnsRules),
				PMKID:       *pmkid,
				DeauthBSSID: *deauth,
				WorkDir:     *workDir,
				CaptureDir:  *caps,
				UID:         *uid,
				GID:         *gid,
			}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			if err := wifi.RunEvilTwin(ctx, ecfg, func(line string) { fmt.Println(line) }); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "wifi evil error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprintln(os.Stderr, "usage: czhax wifi pentest|spam|evil [flags]")
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

	if len(os.Args) > 1 && os.Args[1] == "local" {
		if len(os.Args) > 2 && os.Args[2] == "dhcp" {
			fs := flag.NewFlagSet("local dhcp", flag.ExitOnError)
			iface := fs.String("iface", "", "Interface on the target segment")
			rate := fs.Int("rate", 25, "DHCPDISCOVER packets per second")
			count := fs.Int("count", 0, "Stop after this many requests (0 = flood)")
			randomMAC := fs.Bool("random-mac", true, "New random client MAC per request")
			_ = fs.Parse(os.Args[3:])
			if strings.TrimSpace(*iface) == "" {
				fmt.Fprintln(os.Stderr, wifi.DHCPStarvationUsage)
				os.Exit(1)
			}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			cfg := wifi.DHCPStarvationConfig{
				Iface:     *iface,
				Rate:      *rate,
				Count:     *count,
				RandomMAC: *randomMAC,
			}
			if err := wifi.RunDHCPStarvation(ctx, cfg, func(line string) { fmt.Println(line) }); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "local dhcp error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprintln(os.Stderr, wifi.DHCPStarvationUsage)
		os.Exit(1)
	}

	if len(os.Args) > 1 && os.Args[1] == "sdr" {
		if len(os.Args) > 2 && os.Args[2] == "serve" {
			fs := flag.NewFlagSet("sdr serve", flag.ExitOnError)
			dev := fs.Int("device", 0, "RTL-SDR device index")
			bind := fs.String("bind", "0.0.0.0", "Listen address")
			port := fs.Int("port", 1234, "rtl_tcp port")
			freq := fs.String("freq", "100M", "Initial center frequency")
			rate := fs.String("rate", "2.048M", "IQ sample rate")
			gain := fs.Int("gain", -1, "Tuner gain in dB (-1 = auto)")
			_ = fs.Parse(os.Args[3:])

			hz, err := sdr.ParseFreqMHz(*freq)
			if err != nil {
				fmt.Fprintf(os.Stderr, "sdr serve: %v\n", err)
				os.Exit(1)
			}
			cfg := sdr.ServerConfig{
				DeviceIndex: *dev,
				ListenAddr:  *bind,
				Port:        *port,
				FreqHz:      hz,
				SampleRate:  parseRateOr(*rate, 2_048_000),
				GainDB:      *gain,
			}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			fmt.Printf("rtl_tcp on %s:%d · %s · %s IQ\n", cfg.ListenAddr, cfg.Port,
				sdr.FormatFreqMHz(cfg.FreqHz), sdr.FormatSampleRate(cfg.SampleRate))
			if err := sdr.RunServer(ctx, cfg, func(line string) { fmt.Println(line) }); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "sdr serve error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		st := sdr.Probe(ctx, false)
		fmt.Printf("RTL-SDR devices: %d\n", len(st.Devices))
		for _, d := range st.Devices {
			fmt.Printf("  #%d %s:%s %s (tuner=%s dvb=%v)\n",
				d.Index, d.VendorID, d.ProductID, d.Label(), valueOrNone(d.Tuner), d.DVBDriver)
		}
		fmt.Printf("Tools: rtl_fm=%s rtl_sdr=%s rtl_power=%s rtl_tcp=%s rtl_433=%s player=%s\n",
			valueOrNone(st.Tools.RTLFM), valueOrNone(st.Tools.RTLSDR),
			valueOrNone(st.Tools.RTLPower), valueOrNone(st.Tools.RTLTCP),
			valueOrNone(st.Tools.RTL433), valueOrNone(st.Tools.Player))
		if st.Err != "" {
			fmt.Fprintln(os.Stderr, "SDR: "+st.Err)
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

	if len(os.Args) > 1 && os.Args[1] == "bt" {
		if len(os.Args) > 2 && os.Args[2] == "scan" {
			fs := flag.NewFlagSet("bt scan", flag.ExitOnError)
			hci := fs.String("hci", "hci0", "HCI device (hci0)")
			controller := fs.String("controller", "", "Controller address")
			modeStr := fs.String("mode", "both", "Scan mode: both|le|classic")
			timeout := fs.Duration("timeout", 10*time.Second, "Scan duration")
			raw := fs.Bool("raw", false, "Print raw bluetoothctl output")
			_ = fs.Parse(os.Args[3:])
			mode := bluetooth.ScanBoth
			switch strings.ToLower(*modeStr) {
			case "le", "ble":
				mode = bluetooth.ScanLE
			case "classic", "bredr":
				mode = bluetooth.ScanClassic
			}
			req := bluetooth.ScanRequest{HCI: *hci, Controller: *controller, Mode: mode, Timeout: *timeout}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			var devices []bluetooth.Device
			if *raw {
				out, err := bluetooth.ScanRaw(ctx, req)
				fmt.Print(out)
				if err != nil {
					fmt.Fprintf(os.Stderr, "raw scan error: %v\n", err)
				}
				devices = bluetooth.ParseScanOutput(out, mode)
			} else {
				var err error
				devices, err = bluetooth.ScanDevices(ctx, req)
				if err != nil {
					fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
					os.Exit(1)
				}
			}
			fmt.Printf("%d devices\n", len(devices))
			for _, d := range devices {
				fmt.Printf("%s  %-16s  %-16s  %s\n", d.Address, d.SignalLabel(), d.Kind, d.DisplayName())
			}
			return
		}
		if len(os.Args) > 2 && os.Args[2] == "carwhisperer" {
			fs := flag.NewFlagSet("bt carwhisperer", flag.ExitOnError)
			hci := fs.String("hci", "hci0", "HCI device (hci0)")
			addr := fs.String("addr", "", "Target Bluetooth address")
			record := fs.String("record", "", "Raw audio output path")
			play := fs.String("play", "", "Optional raw audio to inject")
			noRecord := fs.Bool("no-record", false, "Do not save captured audio")
			uid := fs.Int("uid", -1, "Owner UID for capture file")
			gid := fs.Int("gid", -1, "Owner GID for capture file")
			_ = fs.Parse(os.Args[3:])
			if strings.TrimSpace(*addr) == "" {
				fmt.Fprintln(os.Stderr, "bt carwhisperer: --addr is required")
				os.Exit(1)
			}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			err := bluetooth.Carwhisperer(ctx, *hci, *addr, *record, *play, !*noRecord, func(line string) {
				fmt.Println(line)
			})
			if !*noRecord {
				bluetooth.ChownCapture(*record, *uid, *gid)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "bt carwhisperer error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if len(os.Args) > 2 && os.Args[2] == "emulate" {
			fs := flag.NewFlagSet("bt emulate", flag.ExitOnError)
			hci := fs.String("hci", "hci0", "HCI device (hci0)")
			profileStr := fs.String("profile", "keyboard", "Profile: keyboard|mouse|combo|gamepad|media|speaker|headset|custom")
			name := fs.String("name", "", "Advertised Bluetooth device name")
			codStr := fs.String("cod", "", "Hex Class of Device (e.g. 0x002540)")
			appStr := fs.String("appearance", "", "Hex BLE Appearance (e.g. 0x03c1)")
			customMode := fs.String("custom-mode", "hid", "Custom mode: hid|speaker|advertise")
			customUUID := fs.String("custom-uuid", "", "Custom BLE service UUID")
			recordDir := fs.String("record-dir", "", "Directory for speaker WAV captures")
			tempDir := fs.String("temp-dir", "", "Directory for the emulator debug log")
			monitor := fs.Bool("monitor", false, "Play received speaker audio locally")
			record := fs.Bool("record", false, "Record received speaker audio to WAV")
			uid := fs.Int("uid", -1, "Owner UID for capture files")
			gid := fs.Int("gid", -1, "Owner GID for capture files")
			_ = fs.Parse(os.Args[3:])

			prof, err := bluetooth.ParseEmuProfile(*profileStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bt emulate: %v\n", err)
				os.Exit(1)
			}
			var cod uint32
			if strings.TrimSpace(*codStr) != "" {
				if v, perr := bluetooth.ParseHexUint32(*codStr); perr == nil {
					cod = v
				}
			}
			var app uint16
			if strings.TrimSpace(*appStr) != "" {
				if v, perr := bluetooth.ParseHexUint32(*appStr); perr == nil {
					app = uint16(v)
				}
			}
			cfg := bluetooth.EmuConfig{
				HCI:          *hci,
				Profile:      prof,
				Name:         *name,
				CoD:          cod,
				Appearance:   app,
				CustomMode:   bluetooth.EmuCustomMode(*customMode),
				CustomUUID:   *customUUID,
				RecordDir:    *recordDir,
				TempDir:      *tempDir,
				MonitorAudio: *monitor,
				RecordAudio:  *record,
				UID:          *uid,
				GID:          *gid,
			}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			if err := bluetooth.RunEmulatorCLI(ctx, cfg, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "bt emulate error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprintln(os.Stderr, "usage: czhax bt scan [--hci hci0] [--mode both|le|classic] [--raw]")
		fmt.Fprintln(os.Stderr, "       czhax bt carwhisperer --hci hci0 --addr AA:BB:.. --record out.wav [--play playback.raw]")
		fmt.Fprintln(os.Stderr, "       czhax bt emulate [--hci hci0] [--profile keyboard|mouse|combo|gamepad|media|speaker|headset|custom]")
		os.Exit(1)
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
