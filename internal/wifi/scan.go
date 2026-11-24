package wifi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Network struct {
	SSID     string
	BSSID    string
	Signal   int // 0..100 percentage
	RSSI     int // dBm when available
	HasRSSI  bool
	Channel  string
	Freq     string
	Mode     string
	Rate     string
	Security string
	InUse    bool
}

func (n Network) DisplaySSID() string {
	if strings.TrimSpace(n.SSID) == "" {
		return "<hidden>"
	}
	return n.SSID
}

func (n Network) ShortSecurity() string {
	s := strings.ToUpper(strings.TrimSpace(n.Security))
	switch {
	case s == "" || s == "--" || s == "OPEN":
		return "Open"
	case strings.Contains(s, "SAE") || strings.Contains(s, "WPA3"):
		if strings.Contains(s, "WPA2") {
			return "WPA2/3"
		}
		return "WPA3"
	case strings.Contains(s, "WPA2"):
		return "WPA2"
	case strings.Contains(s, "WPA"):
		return "WPA"
	case strings.Contains(s, "WEP"):
		return "WEP"
	default:
		return n.Security
	}
}

type ConnectRequest struct {
	Iface    string
	SSID     string
	BSSID    string
	Password string
	Security string
	Hidden   bool
}

type scanner struct {
	runCmd cmdRunner
	euid   func() int
	hasBin func(name string) bool
}

func defaultScanner() scanner {
	return scanner{
		runCmd: execCmd,
		euid:   os.Geteuid,
		hasBin: func(name string) bool {
			bin := resolveBin(name)
			_, err := os.Stat(bin)
			if err == nil {
				return true
			}
			return bin != name
		},
	}
}

// ScanNetworks scans surrounding WiFi networks using the specified interface (default wlan0).
func ScanNetworks(iface string) ([]Network, error) {
	return scanWith(defaultScanner(), iface)
}

func scanWith(s scanner, iface string) ([]Network, error) {
	iface = strings.TrimSpace(iface)
	if iface == "" {
		iface = "wlan0"
	}

	var nmErr error
	var nmNets []Network
	nmOk := false
	if s.hasBin == nil || s.hasBin("nmcli") {
		args := []string{
			"-t", "--escape", "yes",
			"-f", "IN-USE,SSID,BSSID,MODE,CHAN,FREQ,RATE,SIGNAL,SECURITY",
			"dev", "wifi", "list", "--rescan", "yes", "ifname", iface,
		}
		out, err := runPrivileged(s, 14*time.Second, "nmcli", args...)
		if err == nil {
			nmNets = parseNmcliScan(out)
			if len(nmNets) > 0 {
				sortNetworks(nmNets)
				return nmNets, nil
			}
			nmOk = true
		} else {
			nmErr = fmt.Errorf("nmcli: %s", cleanCmdError(out, err))
		}
	}

	if s.hasBin == nil || s.hasBin("iw") {
		out, err := runPrivileged(s, 14*time.Second, "iw", "dev", iface, "scan")
		if err == nil {
			nets := parseIWScan(out)
			sortNetworks(nets)
			return nets, nil
		}
		if nmOk {
			return nmNets, nil
		}
		iwErr := fmt.Errorf("iw: %s", cleanCmdError(out, err))
		if nmErr != nil {
			return nil, fmt.Errorf("%v; %v", nmErr, iwErr)
		}
		return nil, iwErr
	}

	if nmOk {
		return nmNets, nil
	}
	if nmErr != nil {
		return nil, nmErr
	}
	return nil, errors.New("no wifi scanner available (need nmcli or iw)")
}

// ConnectNetwork connects the specified interface to the target network via NetworkManager.
func ConnectNetwork(req ConnectRequest) error {
	return connectWith(defaultScanner(), req)
}

func connectWith(s scanner, req ConnectRequest) error {
	iface := strings.TrimSpace(req.Iface)
	if iface == "" {
		iface = "wlan0"
	}
	target := strings.TrimSpace(req.SSID)
	if target == "" {
		target = strings.TrimSpace(req.BSSID)
	}
	if target == "" {
		return errors.New("missing network SSID/BSSID")
	}

	if s.hasBin != nil && !s.hasBin("nmcli") {
		return errors.New("nmcli not found")
	}

	args := []string{"dev", "wifi", "connect", target}
	if req.Password != "" && !strings.EqualFold(strings.TrimSpace(req.Security), "Open") {
		args = append(args, "password", req.Password)
	}
	if req.Hidden {
		args = append(args, "hidden", "yes")
	}
	args = append(args, "ifname", iface)

	out, err := runPrivileged(s, 25*time.Second, "nmcli", args...)
	if err != nil {
		return errors.New(cleanCmdError(out, err))
	}
	return nil
}

func runPrivileged(s scanner, timeout time.Duration, bin string, args ...string) (string, error) {
	euid := 0
	if s.euid != nil {
		euid = s.euid()
	}
	if euid != 0 {
		sudoArgs := append([]string{"-n", resolveBin(bin)}, args...)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		out, err := s.runCmd(ctx, "sudo", sudoArgs...)
		cancel()
		if err == nil {
			return out, nil
		}
		// If sudo failed due to missing password/binary, fall back to direct execution;
		// otherwise if direct execution succeeds, prefer it, else keep the sudo error.
		ctx2, cancel2 := context.WithTimeout(context.Background(), timeout)
		out2, err2 := s.runCmd(ctx2, bin, args...)
		cancel2()
		if err2 == nil {
			return out2, nil
		}
		low := strings.ToLower(out)
		if strings.Contains(low, "password is required") || strings.Contains(low, "not found") || strings.Contains(low, "not in the sudoers") {
			return out2, err2
		}
		return out, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	out, err := s.runCmd(ctx, bin, args...)
	cancel()
	return out, err
}

func parseNmcliScan(out string) []Network {
	var nets []Network
	seen := make(map[string]int)

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := splitNmcliTerseLine(line)
		if len(fields) < 9 {
			continue
		}
		sig, _ := strconv.Atoi(strings.TrimSpace(fields[7]))
		sec := strings.TrimSpace(fields[8])
		if sec == "" || sec == "--" {
			sec = "Open"
		}
		n := Network{
			InUse:    strings.TrimSpace(fields[0]) == "*",
			SSID:     fields[1],
			BSSID:    strings.ToUpper(strings.TrimSpace(fields[2])),
			Mode:     strings.TrimSpace(fields[3]),
			Channel:  strings.TrimSpace(fields[4]),
			Freq:     strings.TrimSpace(fields[5]),
			Rate:     strings.TrimSpace(fields[6]),
			Signal:   clampSignal(sig),
			Security: sec,
		}
		key := n.SSID + "\x00" + n.BSSID
		if idx, ok := seen[key]; ok {
			if n.InUse || n.Signal > nets[idx].Signal {
				nets[idx] = n
			}
			continue
		}
		seen[key] = len(nets)
		nets = append(nets, n)
	}
	return nets
}

func splitNmcliTerseLine(line string) []string {
	var fields []string
	var cur strings.Builder
	escaped := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		if escaped {
			cur.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == ':' {
			fields = append(fields, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	if escaped {
		cur.WriteByte('\\')
	}
	fields = append(fields, cur.String())
	return fields
}

func parseIWScan(out string) []Network {
	var nets []Network
	var cur *Network
	hasPrivacy := false
	hasWPA := false
	hasRSN := false
	hasPSK := false
	hasSAE := false

	flush := func() {
		if cur == nil {
			return
		}
		switch {
		case hasSAE && hasPSK:
			cur.Security = "WPA2/WPA3"
		case hasSAE:
			cur.Security = "WPA3"
		case hasRSN && hasWPA:
			cur.Security = "WPA1 WPA2"
		case hasRSN:
			cur.Security = "WPA2"
		case hasWPA:
			cur.Security = "WPA"
		case hasPrivacy:
			cur.Security = "WEP"
		default:
			cur.Security = "Open"
		}
		if cur.Mode == "" {
			cur.Mode = "Infra"
		}
		nets = append(nets, *cur)
		cur = nil
		hasPrivacy, hasWPA, hasRSN, hasPSK, hasSAE = false, false, false, false, false
	}

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "BSS ") {
			flush()
			rest := strings.TrimPrefix(line, "BSS ")
			bssid := rest
			if idx := strings.Index(rest, "("); idx >= 0 {
				bssid = rest[:idx]
			}
			cur = &Network{
				BSSID: strings.ToUpper(strings.TrimSpace(bssid)),
				InUse: strings.Contains(line, "-- associated"),
			}
			continue
		}
		if cur == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "SSID:"):
			cur.SSID = strings.TrimSpace(strings.TrimPrefix(trimmed, "SSID:"))
		case strings.HasPrefix(trimmed, "freq:"):
			freqStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "freq:"))
			if f, err := strconv.ParseFloat(freqStr, 64); err == nil {
				cur.Freq = fmt.Sprintf("%.0f MHz", f)
			} else {
				cur.Freq = freqStr
			}
		case strings.HasPrefix(trimmed, "signal:"):
			sigStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "signal:"))
			sigStr = strings.TrimSpace(strings.TrimSuffix(sigStr, "dBm"))
			if dbm, err := strconv.ParseFloat(sigStr, 64); err == nil {
				cur.RSSI = int(math.Round(dbm))
				cur.HasRSSI = true
				cur.Signal = dbmToPct(cur.RSSI)
			}
		case strings.HasPrefix(trimmed, "DS Parameter set: channel "):
			cur.Channel = strings.TrimSpace(strings.TrimPrefix(trimmed, "DS Parameter set: channel "))
		case strings.HasPrefix(trimmed, "capability:") && strings.Contains(trimmed, "Privacy"):
			hasPrivacy = true
		case strings.HasPrefix(trimmed, "RSN:"):
			hasRSN = true
		case strings.HasPrefix(trimmed, "WPA:"):
			hasWPA = true
		case strings.Contains(trimmed, "Authentication suites:"):
			if strings.Contains(trimmed, "SAE") {
				hasSAE = true
			}
			if strings.Contains(trimmed, "PSK") {
				hasPSK = true
			}
		}
	}
	flush()
	return nets
}

func dbmToPct(dbm int) int {
	if dbm <= -100 {
		return 0
	}
	if dbm >= -50 {
		return 100
	}
	return 2 * (dbm + 100)
}

func clampSignal(s int) int {
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}

func sortNetworks(nets []Network) {
	sort.Slice(nets, func(i, j int) bool {
		if nets[i].InUse != nets[j].InUse {
			return nets[i].InUse
		}
		if nets[i].Signal != nets[j].Signal {
			return nets[i].Signal > nets[j].Signal
		}
		if nets[i].SSID != nets[j].SSID {
			return strings.ToLower(nets[i].SSID) < strings.ToLower(nets[j].SSID)
		}
		return nets[i].BSSID < nets[j].BSSID
	})
}

func cleanCmdError(out string, err error) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "Error: ")
		return line
	}
	if err != nil {
		return err.Error()
	}
	return "command failed"
}
