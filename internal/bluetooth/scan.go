package bluetooth

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ScanMode selects which Bluetooth transports are queried.
type ScanMode int

const (
	ScanBoth ScanMode = iota
	ScanClassic
	ScanLE
)

func (m ScanMode) String() string {
	switch m {
	case ScanClassic:
		return "Classic"
	case ScanLE:
		return "BLE"
	default:
		return "BLE+Classic"
	}
}

func (m ScanMode) Short() string {
	switch m {
	case ScanClassic:
		return "CL"
	case ScanLE:
		return "LE"
	default:
		return "Both"
	}
}

// Arg maps the mode to bluetoothctl's scan argument.
func (m ScanMode) Arg() string {
	switch m {
	case ScanClassic:
		return "bredr"
	case ScanLE:
		return "le"
	default:
		return "on"
	}
}

// Device is one Bluetooth peer discovered by a scan.
type Device struct {
	Address     string
	Name        string
	Alias       string
	RSSI        int
	HasRSSI     bool
	Kind        string // "Classic", "BLE", or "BT"
	Class       string
	Icon        string
	UUIDs       []string
	FastPair    bool
	ModelID     uint32
	HasModelID  bool
	ServiceData []byte
	Paired      bool
	Trusted     bool
	Connected   bool
}

func (d Device) DisplayName() string {
	for _, s := range []string{d.Name, d.Alias, d.Address} {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return "unknown"
}

func (d Device) SignalLabel() string {
	if !d.HasRSSI {
		return "--"
	}
	return fmt.Sprintf("%d", d.RSSI)
}

func (d Device) ModelIDString() string {
	if !d.HasModelID {
		return "n/a"
	}
	return fmt.Sprintf("0x%06X", d.ModelID)
}

func (d Device) ShortKind() string {
	switch d.Kind {
	case "Classic":
		return "CL"
	case "BLE":
		return "LE"
	default:
		return "BT"
	}
}

// ScanRequest configures a one-shot Bluetooth discovery run.
type ScanRequest struct {
	HCI        string
	Controller string // controller BD address (optional)
	Mode       ScanMode
	Timeout    time.Duration
}

// NormalizeHCI accepts "0", "hci0", or "" and returns a hciX name.
func NormalizeHCI(hci string) string {
	hci = strings.TrimSpace(strings.ToLower(hci))
	if hci == "" {
		return "hci0"
	}
	if strings.HasPrefix(hci, "hci") {
		return hci
	}
	if _, err := strconv.Atoi(hci); err == nil {
		return "hci" + hci
	}
	return hci
}

func hciNumber(hci string) (int, string, error) {
	name := NormalizeHCI(hci)
	n, err := strconv.Atoi(strings.TrimPrefix(name, "hci"))
	if err != nil || n < 0 {
		return 0, name, fmt.Errorf("invalid HCI device %q", hci)
	}
	return n, name, nil
}

// ScanDevices discovers nearby Classic and/or BLE devices. bluetoothctl is
// preferred because it exposes RSSI, service data and Fast Pair advertisements;
// hcitool is used as a fallback on older BlueZ installations.
func ScanDevices(ctx context.Context, req ScanRequest) ([]Device, error) {
	req.HCI = NormalizeHCI(req.HCI)
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	req.Timeout = timeout
	scanCtx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()

	tryPowerOn(scanCtx, req)

	out, err := runBluetoothctlScan(scanCtx, req)
	if err == nil || strings.TrimSpace(out) != "" {
		if devices := ParseScanOutput(out, req.Mode); len(devices) > 0 {
			return devices, nil
		}
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	devices, hciErr := scanWithHCITool(ctx, req, timeout)
	if len(devices) > 0 {
		return devices, nil
	}
	if hciErr != nil {
		// bluetoothctl ran fine but found nothing; a missing hcitool is not an
		// error in that case.
		if err == nil {
			return nil, nil
		}
		return nil, hciErr
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) &&
		!strings.Contains(strings.ToLower(err.Error()), "killed") {
		return nil, err
	}
	return nil, nil
}

func tryPowerOn(ctx context.Context, req ScanRequest) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if req.Controller != "" {
		script := "select " + strings.ToUpper(req.Controller) + "\npower on\n"
		_, _ = commandOutputInput(cctx, "bluetoothctl", script, "--timeout", "3")
		return
	}
	_, _ = commandOutput(cctx, "bluetoothctl", "power", "on")
}

func runBluetoothctlScan(ctx context.Context, req ScanRequest) (string, error) {
	if _, err := exec.LookPath("bluetoothctl"); err != nil {
		return "", err
	}
	secs := int(req.Timeout / time.Second)
	if secs <= 0 {
		secs = 10
	}

	if req.Controller != "" {
		script := "select " + strings.ToUpper(req.Controller) + "\nscan " + req.Mode.Arg() + "\n"
		out, err := commandOutputInput(ctx, "bluetoothctl", script, "--timeout", strconv.Itoa(secs))
		if err == nil || strings.TrimSpace(out) != "" {
			return out, err
		}
	}

	args := []string{"--timeout", strconv.Itoa(secs), "scan", req.Mode.Arg()}
	out, err := commandOutput(ctx, "bluetoothctl", args...)
	if err == nil && strings.TrimSpace(out) != "" {
		return out, nil
	}
	// Older bluetoothctl releases only understand "scan on".
	if req.Mode != ScanBoth {
		fallback := []string{"--timeout", strconv.Itoa(secs), "scan", "on"}
		if out2, err2 := commandOutput(ctx, "bluetoothctl", fallback...); strings.TrimSpace(out2) != "" || err2 == nil {
			if strings.TrimSpace(out2) != "" {
				return out2, nil
			}
		}
	}
	return out, err
}

func scanWithHCITool(ctx context.Context, req ScanRequest, timeout time.Duration) ([]Device, error) {
	if _, err := exec.LookPath("hcitool"); err != nil {
		return nil, err
	}
	run := func(kind string) ([]Device, error) {
		args := []string{"-i", req.HCI}
		switch kind {
		case "classic":
			args = append(args, "scan", "--flush")
		default:
			args = append(args, "lescan", "--duplicates")
		}
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		out, err := capturePrivileged(cctx, "hcitool", args...)
		if cctx.Err() != nil && strings.TrimSpace(out) != "" {
			err = nil
		}
		return ParseHCIToolOutput(out, kind), err
	}

	switch req.Mode {
	case ScanClassic:
		return run("classic")
	case ScanLE:
		return run("le")
	default:
		var wg sync.WaitGroup
		var classic, le []Device
		var cErr, lErr error
		wg.Add(2)
		go func() { defer wg.Done(); classic, cErr = run("classic") }()
		go func() { defer wg.Done(); le, lErr = run("le") }()
		wg.Wait()
		merged := mergeDevices(classic, le)
		if len(merged) > 0 {
			return merged, nil
		}
		if cErr != nil {
			return nil, cErr
		}
		return nil, lErr
	}
}

// ParseHCIToolOutput parses `hcitool scan` / `hcitool lescan` output.
func ParseHCIToolOutput(out, kind string) []Device {
	label := "Classic"
	if kind == "le" {
		label = "BLE"
	}
	var devices []Device
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "Scanning") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || !IsBDAddr(fields[0]) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		devices = append(devices, Device{
			Address: NormalizeAddr(fields[0]),
			Name:    cleanName(name),
			Kind:    label,
		})
	}
	return dedupeDevices(devices)
}

// ParseScanOutput parses `bluetoothctl scan` output, merging repeated
// [NEW]/[CHG] records for the same address.
func ParseScanOutput(out string, mode ScanMode) []Device {
	type state struct {
		dev        *Device
		sdActive   bool
		sdLastLine bool
	}
	states := make(map[string]*state)
	order := []string{}

	defaultKind := func() string {
		switch mode {
		case ScanClassic:
			return "Classic"
		case ScanLE:
			return "BLE"
		default:
			return ""
		}
	}

	get := func(addr string) *state {
		addr = NormalizeAddr(addr)
		st, ok := states[addr]
		if !ok {
			st = &state{dev: &Device{Address: addr, Kind: defaultKind()}}
			states[addr] = st
			order = append(order, addr)
		}
		return st
	}

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "[bluetooth]# ")
		tag := ""
		switch {
		case strings.HasPrefix(line, "[NEW] "):
			tag = "new"
			line = strings.TrimPrefix(line, "[NEW] ")
		case strings.HasPrefix(line, "[CHG] "):
			tag = "chg"
			line = strings.TrimPrefix(line, "[CHG] ")
		case strings.HasPrefix(line, "[DEL] "):
			tag = "del"
			line = strings.TrimPrefix(line, "[DEL] ")
		}
		if strings.HasPrefix(line, "Controller ") || strings.HasPrefix(line, "Discovery started") ||
			strings.HasPrefix(line, "Discovery stopped") || strings.HasPrefix(line, "SetDiscoveryFilter") {
			continue
		}
		if !strings.HasPrefix(line, "Device ") {
			continue
		}

		rest := strings.TrimPrefix(line, "Device ")
		fields := strings.SplitN(rest, " ", 2)
		if len(fields) == 0 || !IsBDAddr(fields[0]) {
			continue
		}
		addr := NormalizeAddr(fields[0])
		if tag == "del" {
			delete(states, addr)
			continue
		}
		st := get(addr)
		attr := ""
		if len(fields) > 1 {
			attr = strings.TrimSpace(fields[1])
		}
		if attr == "" {
			continue
		}
		// "[NEW] Device AA:BB:.. Headset" carries the initial name without a
		// "Name:" prefix.
		if tag == "new" && !strings.Contains(attr, ":") && st.dev.Name == "" {
			st.dev.Name = cleanName(attr)
		}

		// ServiceData values arrive one byte per line and belong to the last
		// ServiceData Key seen for this address.
		isSDValue := strings.HasPrefix(attr, "ServiceData Value:")
		if st.sdLastLine && !isSDValue {
			st.sdActive = false
		}
		st.sdLastLine = isSDValue

		switch {
		case isSDValue:
			if st.sdActive {
				if b, ok := parseHexByte(strings.TrimSpace(strings.TrimPrefix(attr, "ServiceData Value:"))); ok {
					st.dev.ServiceData = append(st.dev.ServiceData, b)
					st.dev.FastPair = true
					if len(st.dev.ServiceData) >= 3 && !st.dev.HasModelID {
						st.dev.ModelID = uint32(st.dev.ServiceData[0]) |
							uint32(st.dev.ServiceData[1])<<8 |
							uint32(st.dev.ServiceData[2])<<16
						st.dev.HasModelID = true
					}
				}
			}
		case strings.HasPrefix(attr, "ServiceData Key:"):
			key := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(attr, "ServiceData Key:")))
			st.dev.ServiceData = nil
			st.sdActive = strings.Contains(key, "fe2c")
			if st.sdActive {
				st.dev.FastPair = true
			}
		case strings.HasPrefix(attr, "RSSI:"):
			if n, ok := parseRSSI(strings.TrimSpace(strings.TrimPrefix(attr, "RSSI:"))); ok {
				st.dev.RSSI = n
				st.dev.HasRSSI = true
			}
		case strings.HasPrefix(attr, "Name:"):
			if n := cleanName(strings.TrimPrefix(attr, "Name:")); n != "" {
				st.dev.Name = n
			}
		case strings.HasPrefix(attr, "Alias:"):
			if n := cleanName(strings.TrimPrefix(attr, "Alias:")); n != "" {
				st.dev.Alias = n
			}
		case strings.HasPrefix(attr, "Class:"):
			st.dev.Class = strings.TrimSpace(strings.TrimPrefix(attr, "Class:"))
		case strings.HasPrefix(attr, "Icon:"):
			st.dev.Icon = strings.TrimSpace(strings.TrimPrefix(attr, "Icon:"))
		case strings.HasPrefix(attr, "UUIDs:"):
			st.dev.UUIDs = splitUUIDs(strings.TrimSpace(strings.TrimPrefix(attr, "UUIDs:")))
			for _, u := range st.dev.UUIDs {
				if strings.Contains(strings.ToLower(u), "fe2c") {
					st.dev.FastPair = true
				}
			}
		case strings.HasPrefix(attr, "Paired:"):
			st.dev.Paired = parseYesNo(attr)
		case strings.HasPrefix(attr, "Trusted:"):
			st.dev.Trusted = parseYesNo(attr)
		case strings.HasPrefix(attr, "Connected:"):
			st.dev.Connected = parseYesNo(attr)
		}
	}

	devices := make([]Device, 0, len(states))
	for _, addr := range order {
		st, ok := states[addr]
		if !ok || st.dev == nil {
			continue
		}
		d := *st.dev
		if d.Kind == "" {
			switch {
			case d.FastPair:
				d.Kind = "BLE"
			case d.Class != "":
				d.Kind = "Classic"
			default:
				d.Kind = "BT"
			}
		}
		devices = append(devices, d)
	}
	return SortDevices(devices)
}

// ParseDeviceInfo parses `bluetoothctl info <address>` output.
func ParseDeviceInfo(out string) Device {
	var d Device
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Device ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && IsBDAddr(fields[1]) {
				d.Address = NormalizeAddr(fields[1])
				if strings.Contains(line, "(Random)") {
					d.Kind = "BLE"
				}
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "Name:"):
			d.Name = cleanName(strings.TrimPrefix(line, "Name:"))
		case strings.HasPrefix(line, "Alias:"):
			d.Alias = cleanName(strings.TrimPrefix(line, "Alias:"))
		case strings.HasPrefix(line, "Class:"):
			d.Class = strings.TrimSpace(strings.TrimPrefix(line, "Class:"))
		case strings.HasPrefix(line, "Icon:"):
			d.Icon = strings.TrimSpace(strings.TrimPrefix(line, "Icon:"))
		case strings.HasPrefix(line, "RSSI:"):
			if n, ok := parseRSSI(strings.TrimSpace(strings.TrimPrefix(line, "RSSI:"))); ok {
				d.RSSI = n
				d.HasRSSI = true
			}
		case strings.HasPrefix(line, "Paired:"):
			d.Paired = parseYesNo(line)
		case strings.HasPrefix(line, "Trusted:"):
			d.Trusted = parseYesNo(line)
		case strings.HasPrefix(line, "Connected:"):
			d.Connected = parseYesNo(line)
		case strings.HasPrefix(line, "UUID:"):
			u := strings.TrimSpace(strings.TrimPrefix(line, "UUID:"))
			if u != "" {
				d.UUIDs = append(d.UUIDs, u)
				if strings.Contains(strings.ToLower(u), "fe2c") {
					d.FastPair = true
				}
			}
		}
	}
	if d.Kind == "" {
		if d.FastPair {
			d.Kind = "BLE"
		} else if d.Class != "" {
			d.Kind = "Classic"
		} else {
			d.Kind = "BT"
		}
	}
	return d
}

// QueryDeviceInfo returns bluetoothctl's cached information for one address.
func QueryDeviceInfo(ctx context.Context, address string) (Device, error) {
	if _, err := exec.LookPath("bluetoothctl"); err != nil {
		return Device{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := commandOutput(cctx, "bluetoothctl", "info", NormalizeAddr(address))
	info := ParseDeviceInfo(out)
	if err != nil && strings.TrimSpace(out) == "" {
		return info, err
	}
	return info, nil
}

// IsBDAddr reports whether s looks like AA:BB:CC:DD:EE:FF.
func IsBDAddr(s string) bool {
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return false
	}
	for _, p := range parts {
		if len(p) != 2 {
			return false
		}
		if _, err := strconv.ParseUint(p, 16, 8); err != nil {
			return false
		}
	}
	return true
}

func NormalizeAddr(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}

func parseYesNo(line string) bool {
	_, v, ok := strings.Cut(line, ":")
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(v), "yes")
}

func parseHexByte(s string) (byte, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if idx := strings.IndexAny(s, " \t("); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 16, 8)
	if err != nil {
		return 0, false
	}
	return byte(n), true
}

func parseRSSI(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	// Some BlueZ versions render "0xffffffb4 (-76)".
	if idx := strings.IndexByte(s, '('); idx >= 0 {
		if end := strings.IndexByte(s[idx:], ')'); end > 0 {
			if n, err := strconv.Atoi(strings.TrimSpace(s[idx+1 : idx+end])); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func splitUUIDs(s string) []string {
	s = strings.TrimSpace(strings.Trim(s, "[]"))
	if s == "" {
		return nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
	var out []string
	for _, f := range fields {
		f = strings.TrimSpace(strings.Trim(f, "\"'"))
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func dedupeDevices(in []Device) []Device {
	seen := make(map[string]int)
	var out []Device
	for _, d := range in {
		d.Address = NormalizeAddr(d.Address)
		if idx, ok := seen[d.Address]; ok {
			if d.Name != "" {
				out[idx].Name = d.Name
			}
			if d.HasRSSI {
				out[idx].RSSI = d.RSSI
				out[idx].HasRSSI = true
			}
			if d.Kind != "" {
				out[idx].Kind = d.Kind
			}
			continue
		}
		seen[d.Address] = len(out)
		out = append(out, d)
	}
	return out
}

func mergeDevices(lists ...[]Device) []Device {
	var all []Device
	for _, l := range lists {
		all = append(all, l...)
	}
	return SortDevices(dedupeDevices(all))
}

// SortDevices puts Fast Pair and strongest-signal devices first.
func SortDevices(devices []Device) []Device {
	sort.SliceStable(devices, func(i, j int) bool {
		a, b := devices[i], devices[j]
		if a.FastPair != b.FastPair {
			return a.FastPair
		}
		if a.HasRSSI != b.HasRSSI {
			return a.HasRSSI
		}
		if a.HasRSSI && b.HasRSSI && a.RSSI != b.RSSI {
			return a.RSSI > b.RSSI
		}
		an, bn := strings.ToLower(a.DisplayName()), strings.ToLower(b.DisplayName())
		if an != bn {
			return an < bn
		}
		return a.Address < b.Address
	})
	return devices
}
