package bluetooth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/czhax/czhax/internal/oui"
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
	AddrType    string // "public", "random", or ""
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
	for _, s := range []string{d.Name, d.Alias} {
		if t := cleanDeviceName(s); t != "" {
			return t
		}
	}
	if strings.TrimSpace(d.Address) != "" {
		return strings.TrimSpace(d.Address)
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
	scanCtx, cancel := context.WithTimeout(ctx, timeout*2+4*time.Second)
	defer cancel()

	out, err := runBluetoothctlScan(scanCtx, req, timeout)
	devices := ParseScanOutput(out, req.Mode)

	// Always run a br/edr-capable btmgmt discovery for Both/Classic. The
	// classic public address is required for RFCOMM even when the same device
	// also advertises a BLE random address. bluetoothctl can return only the
	// BLE entry, so don't skip this when names/RSSI are already complete.
	if req.Mode != ScanLE || needsEnrichment(devices) {
		if bdevs, _ := scanWithBtmgmt(ctx, req, timeout); len(bdevs) > 0 {
			if len(devices) > 0 {
				devices = mergeDeviceLists(devices, bdevs)
			} else {
				devices = bdevs
			}
		}
	}
	// If the dual pass produced no Classic entries, try a BR/EDR-only inquiry.
	// Some controllers/BlueZ builds don't return inquiry results in the
	// default "find" pass.
	if req.Mode == ScanBoth && !hasClassicDevice(devices) {
		creq := req
		creq.Mode = ScanClassic
		if bdevs, _ := scanWithBtmgmt(ctx, creq, timeout); len(bdevs) > 0 {
			devices = mergeDeviceLists(devices, bdevs)
		}
	}
	// btmgmt's BR/EDR records often omit the friendly name. hcitool's inquiry
	// returns it, so merge those names into the RSSI-bearing entries.
	if req.Mode != ScanLE && classicNamesMissing(devices) {
		nreq := req
		nreq.Mode = ScanClassic
		if named, _ := scanWithHCITool(ctx, nreq, timeout); len(named) > 0 {
			devices = mergeDeviceLists(devices, named)
		}
	}
	if len(devices) > 0 {
		if needsEnrichment(devices) {
			devices = enrichWithInfo(ctx, devices)
		}
		return applyOUI(devices), nil
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	devices, hciErr := scanWithHCITool(ctx, req, timeout)
	if len(devices) > 0 {
		return applyOUI(devices), nil
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

// runBluetoothctlScan uses an interactive bluetoothctl session with stdin held
// open for the scan window. BlueZ versions around 5.75 exit immediately from
// "--timeout N scan on" (they only set the discovery filter), which silently
// returns zero devices; keeping the session alive avoids that.
func runBluetoothctlScan(ctx context.Context, req ScanRequest, timeout time.Duration) (string, error) {
	if _, err := exec.LookPath("bluetoothctl"); err != nil {
		return "", err
	}
	prepareAdapter(ctx, req)
	out, err := interactiveBluetoothctlScan(ctx, req, timeout)
	if len(ParseScanOutput(out, req.Mode)) > 0 {
		return out, nil
	}
	// A PTY keeps bluetoothctl in interactive mode even on BlueZ versions
	// that otherwise exit right after "scan on".
	if _, lookErr := exec.LookPath("script"); lookErr == nil {
		if out2, err2 := ptyBluetoothctlScan(ctx, req, timeout); len(ParseScanOutput(out2, req.Mode)) > 0 {
			return out2, nil
		} else if strings.TrimSpace(out2) != "" && err2 == nil {
			out = out2
		}
	}

	secs := int(timeout / time.Second)
	if secs <= 0 {
		secs = 10
	}
	args := []string{"--timeout", strconv.Itoa(secs), "scan", req.Mode.Arg()}
	out2, err2 := commandOutput(ctx, "bluetoothctl", args...)
	if len(ParseScanOutput(out2, req.Mode)) > 0 {
		return out2, nil
	}
	if msg := scanFailure(out2); msg != "" {
		return out2, errors.New(msg)
	}
	if strings.TrimSpace(out2) != "" || err2 == nil {
		return out2, err2
	}
	if msg := scanFailure(out); msg != "" {
		return out, errors.New(msg)
	}
	return out, err
}

func scanFailure(out string) string {
	low := strings.ToLower(out)
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(line)
		for _, marker := range []string{
			"failed to start discovery",
			"no default controller available",
			"waiting to connect to bluetoothd",
			"org.bluez.error.notready",
			"org.bluez.error.inprogress",
		} {
			if strings.Contains(l, marker) {
				return strings.TrimSpace(stripANSI(line))
			}
		}
	}
	if strings.Contains(low, "failed to start discovery") {
		return "bluetoothctl: failed to start discovery"
	}
	return ""
}

// ScanRaw runs the same bluetoothctl discovery as ScanDevices and returns the
// raw session output for troubleshooting.
func ScanRaw(ctx context.Context, req ScanRequest) (string, error) {
	req.HCI = NormalizeHCI(req.HCI)
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	scanCtx, cancel := context.WithTimeout(ctx, timeout+3*time.Second)
	defer cancel()
	prepareAdapter(scanCtx, req)
	return interactiveBluetoothctlScan(scanCtx, req, timeout)
}

func prepareAdapter(ctx context.Context, req ScanRequest) {
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if _, err := exec.LookPath("systemctl"); err == nil {
		if exec.CommandContext(cctx, "systemctl", "is-active", "--quiet", "bluetooth").Run() != nil {
			_, _ = capturePrivileged(cctx, "systemctl", "start", "bluetooth")
		}
	}
	if _, err := exec.LookPath("rfkill"); err == nil {
		_, _ = capturePrivileged(cctx, "rfkill", "unblock", "bluetooth")
	}
	if _, err := exec.LookPath("hciconfig"); err == nil {
		_, _ = capturePrivileged(cctx, "hciconfig", req.HCI, "up")
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func interactiveBluetoothctlScan(ctx context.Context, req ScanRequest, timeout time.Duration) (string, error) {
	cmd := exec.CommandContext(ctx, "bluetoothctl")
	return runBluetoothctlSession(ctx, cmd, req, timeout)
}

func ptyBluetoothctlScan(ctx context.Context, req ScanRequest, timeout time.Duration) (string, error) {
	cmd := exec.CommandContext(ctx, "script", "-q", "-c", "bluetoothctl", "/dev/null")
	return runBluetoothctlSession(ctx, cmd, req, timeout)
}

func runBluetoothctlSession(ctx context.Context, cmd *exec.Cmd, req ScanRequest, timeout time.Duration) (string, error) {
	return runShellSession(ctx, cmd, func(write func(string)) {
		if req.Controller != "" {
			write("select " + strings.ToUpper(req.Controller))
			time.Sleep(150 * time.Millisecond)
		}
		write("power on")
		time.Sleep(350 * time.Millisecond)
		write("scan " + req.Mode.Arg())
	}, timeout, "scan off", "devices")
}

// runShellSession keeps an interactive BlueZ shell alive for the scan window.
// BlueZ 5.75+ stops discovery as soon as the client exits, so stdin must remain
// open until we explicitly stop it.
func runShellSession(ctx context.Context, cmd *exec.Cmd, setup func(write func(string)), timeout time.Duration, stop ...string) (string, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	var buf lockedBuffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return "", err
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()

	write := func(s string) {
		_, _ = stdin.Write([]byte(s + "\n"))
	}
	setup(write)

	timer := time.NewTimer(timeout)
	exited := false
	select {
	case <-ctx.Done():
	case <-timer.C:
	case <-wait:
		exited = true
	}
	timer.Stop()

	if !exited {
		for _, s := range stop {
			write(s)
		}
		write("quit")
		_ = stdin.Close()
		select {
		case <-wait:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-wait
		}
	}
	if ctx.Err() != nil {
		return buf.String(), ctx.Err()
	}
	return buf.String(), nil
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

// scanWithBtmgmt discovers devices through the kernel management socket. It
// provides RSSI for both BR/EDR and LE peers, unlike bluetoothctl.
func scanWithBtmgmt(ctx context.Context, req ScanRequest, timeout time.Duration) ([]Device, error) {
	if _, err := exec.LookPath("btmgmt"); err != nil {
		return nil, err
	}
	bin, prefix, ok := privilegedPrefix("btmgmt")
	if !ok {
		return nil, errors.New("root or passwordless sudo required for btmgmt")
	}
	if len(prefix) > 0 {
		prefix = append(prefix, "-i", req.HCI)
		bin = "sudo"
	} else {
		prefix = []string{"-i", req.HCI}
	}
	find := "find"
	switch req.Mode {
	case ScanLE:
		find += " -l"
	case ScanClassic:
		find += " -b"
	}
	run := func(cmd *exec.Cmd) (string, error) {
		return runShellSession(ctx, cmd, func(write func(string)) {
			write(find)
		}, timeout, "stop-find")
	}
	// A PTY keeps btmgmt from exiting its non-interactive stdin loop before
	// the scan window ends.
	if _, err := exec.LookPath("script"); err == nil {
		cmdStr := shellJoin(append([]string{bin}, prefix...))
		cmd := exec.CommandContext(ctx, "script", "-q", "-c", cmdStr, "/dev/null")
		if out, err := run(cmd); strings.TrimSpace(out) != "" {
			if devices := ParseBtmgmtOutput(out); len(devices) > 0 {
				return devices, nil
			}
			if err != nil && ctx.Err() == nil {
				// Fall through to the direct path.
			}
		}
	}
	cmd := exec.CommandContext(ctx, bin, prefix...)
	out, err := run(cmd)
	if err != nil && ctx.Err() != nil && strings.TrimSpace(out) != "" {
		err = nil
	}
	devices := ParseBtmgmtOutput(out)
	if len(devices) > 0 {
		return devices, nil
	}
	return devices, err
}

func shellJoin(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString("'")
		b.WriteString(strings.ReplaceAll(a, "'", "'\\''"))
		b.WriteString("'")
	}
	return b.String()
}

var reBtDevFound = regexp.MustCompile(`dev_found:\s*([0-9A-Fa-f:]{17})\s+type\s+(BR/EDR|LE\s+(?:Public|Random))\s+rssi\s+(-?\d+)`)

// ParseBtmgmtOutput parses `btmgmt find` output:
//
//	hci0 dev_found: AA:BB:CC:DD:EE:FF type BR/EDR rssi -79 flags 0x0000
//	AD flags 0x06
//	name Headset
func ParseBtmgmtOutput(out string) []Device {
	out = strings.ReplaceAll(out, "\r", "\n")
	var devices []Device
	lastIdx := -1
	for _, raw := range strings.Split(out, "\n") {
		line := stripANSI(strings.TrimSpace(raw))
		if line == "" {
			continue
		}
		if m := reBtDevFound.FindStringSubmatch(line); len(m) == 4 {
			d := Device{
				Address: NormalizeAddr(m[1]),
				Kind:    "Classic",
			}
			if strings.HasPrefix(m[2], "LE") {
				d.Kind = "BLE"
				if strings.HasSuffix(strings.ToLower(m[2]), "random") {
					d.AddrType = "random"
				} else {
					d.AddrType = "public"
				}
			}
			if n, err := strconv.Atoi(m[3]); err == nil {
				d.RSSI = n
				d.HasRSSI = true
			}
			devices = append(devices, d)
			lastIdx = len(devices) - 1
			continue
		}
		if lastIdx >= 0 && (strings.HasPrefix(line, "name ") || strings.HasPrefix(line, "short name ")) {
			name := strings.TrimPrefix(line, "short name ")
			name = strings.TrimPrefix(name, "name ")
			if n := cleanDeviceName(name); n != "" && devices[lastIdx].Name == "" {
				devices[lastIdx].Name = n
			}
		}
	}
	return SortDevices(dedupeDevices(devices))
}

func hasClassicDevice(devices []Device) bool {
	for _, d := range devices {
		if d.Kind == "Classic" {
			return true
		}
	}
	return false
}

func classicNamesMissing(devices []Device) bool {
	for _, d := range devices {
		if d.Kind != "Classic" {
			continue
		}
		if strings.TrimSpace(d.Name) == "" && strings.TrimSpace(d.Alias) == "" {
			return true
		}
	}
	return false
}

func applyOUI(devices []Device) []Device {
	for i := range devices {
		d := &devices[i]
		if d.Kind != "Classic" {
			continue
		}
		if strings.TrimSpace(d.Name) != "" || strings.TrimSpace(d.Alias) != "" {
			continue
		}
		if v := oui.Lookup(d.Address); v != "" {
			d.Name = v
		}
	}
	return SortDevices(devices)
}

func needsEnrichment(devices []Device) bool {
	for _, d := range devices {
		if !d.HasRSSI || strings.TrimSpace(d.Name) == "" {
			return true
		}
	}
	return false
}

// enrichWithInfo fills names/RSSI from bluetoothctl's cached Device1
// properties when btmgmt is unavailable. Queries run concurrently.
func enrichWithInfo(ctx context.Context, devices []Device) []Device {
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	sem := make(chan struct{}, 4)
	results := make(chan Device, len(devices))
	var wg sync.WaitGroup
	for _, d := range devices {
		if d.HasRSSI && strings.TrimSpace(d.Name) != "" {
			continue
		}
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-cctx.Done():
				return
			}
			info, err := QueryDeviceInfo(cctx, addr)
			if err == nil && info.Address != "" {
				results <- info
			}
		}(d.Address)
	}
	wg.Wait()
	close(results)
	var extra []Device
	for d := range results {
		extra = append(extra, d)
	}
	if len(extra) == 0 {
		return devices
	}
	return mergeDeviceLists(devices, extra)
}

// mergeDeviceLists adds non-empty names/RSSI from extra to matching addresses
// in base, and appends peers only present in extra.
func mergeDeviceLists(base, extra []Device) []Device {
	out := append([]Device(nil), base...)
	index := make(map[string]int, len(out))
	for i, d := range out {
		index[d.Address] = i
	}
	for _, e := range extra {
		if i, ok := index[e.Address]; ok {
			d := out[i]
			if e.Name != "" {
				d.Name = e.Name
			}
			if e.Alias != "" {
				d.Alias = e.Alias
			}
			if e.HasRSSI {
				d.RSSI = e.RSSI
				d.HasRSSI = true
			}
			if e.Kind != "" && (d.Kind == "" || d.Kind == "BT") {
				d.Kind = e.Kind
			}
			if e.AddrType != "" {
				d.AddrType = e.AddrType
			}
			if e.Class != "" {
				d.Class = e.Class
			}
			if e.Icon != "" {
				d.Icon = e.Icon
			}
			if len(e.UUIDs) > 0 {
				d.UUIDs = e.UUIDs
			}
			d.Paired = d.Paired || e.Paired
			d.Trusted = d.Trusted || e.Trusted
			d.Connected = d.Connected || e.Connected
			d.FastPair = d.FastPair || e.FastPair
			if e.HasModelID {
				d.ModelID = e.ModelID
				d.HasModelID = true
			}
			out[i] = d
			continue
		}
		index[e.Address] = len(out)
		out = append(out, e)
	}
	return SortDevices(out)
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
		line = stripANSI(line)
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

var reANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
var reMACName = regexp.MustCompile(`(?i)^([0-9a-f]{2}[:-]){5}[0-9a-f]{2}$|^[0-9a-f]{12}$`)

func stripANSI(s string) string {
	return reANSI.ReplaceAllString(s, "")
}

func isMACLikeName(s string) bool {
	return reMACName.MatchString(strings.TrimSpace(s))
}

// cleanDeviceName drops BlueZ's auto-generated "AA-BB-CC-DD-EE-FF" aliases so
// the UI can show the real address with colons instead.
func cleanDeviceName(s string) string {
	s = cleanName(s)
	if s == "" || isMACLikeName(s) {
		return ""
	}
	return s
}

// ParseScanOutput parses `bluetoothctl scan` output, merging repeated
// [NEW]/[CHG] records for the same address.
func ParseScanOutput(out string, mode ScanMode) []Device {
	out = strings.ReplaceAll(out, "\r", "\n")
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
		line = stripANSI(line)
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
		if (tag == "new" || tag == "") && !strings.Contains(attr, ":") && st.dev.Name == "" {
			st.dev.Name = cleanDeviceName(attr)
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
			if n := cleanDeviceName(strings.TrimPrefix(attr, "Name:")); n != "" {
				st.dev.Name = n
			}
		case strings.HasPrefix(attr, "Alias:"):
			if n := cleanDeviceName(strings.TrimPrefix(attr, "Alias:")); n != "" {
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
		d.Name = cleanDeviceName(d.Name)
		d.Alias = cleanDeviceName(d.Alias)
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
	out = strings.ReplaceAll(out, "\r", "\n")
	var d Device
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		line = stripANSI(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Device ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && IsBDAddr(fields[1]) {
				d.Address = NormalizeAddr(fields[1])
				if strings.Contains(line, "(Random)") {
					d.Kind = "BLE"
					d.AddrType = "random"
				} else if strings.Contains(line, "(Public)") {
					d.AddrType = "public"
				}
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "Name:"):
			d.Name = cleanDeviceName(strings.TrimPrefix(line, "Name:"))
		case strings.HasPrefix(line, "Alias:"):
			d.Alias = cleanDeviceName(strings.TrimPrefix(line, "Alias:"))
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
		// When the same physical device appears twice (BLE random + Classic
		// public), show the Classic public entry first so RFCOMM targets work.
		if strings.EqualFold(a.DisplayName(), b.DisplayName()) && a.DisplayName() != "" {
			aClassic := a.Kind == "Classic"
			bClassic := b.Kind == "Classic"
			if aClassic != bClassic {
				return aClassic
			}
		}
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
