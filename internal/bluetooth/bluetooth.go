package bluetooth

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Adapter struct {
	Name         string
	Alias        string
	Address      string
	Up           bool
	Powered      bool
	StateKnown   bool
	RFKill       string // "", "unblocked", "soft-blocked", "hard-blocked"
	Bus          string // e.g. "UART", "USB", "SDIO", "VIRTUAL"
	Type         string // e.g. "Primary", "AMP"
	Mode         string // e.g. "BR/EDR + LE", "LE", "BR/EDR"
	HCIVersion   string // e.g. "5.0", "5.3"
	Manufacturer string
	Product      string
	Driver       string
	VendorID     string
	ProductID    string
	Discoverable bool
	Pairable     bool
	FlagsKnown   bool
	ScanMode     string
}

type cmdRunner func(ctx context.Context, name string, args ...string) (string, error)

type collector struct {
	btClassDir string
	rfkillDir  string
	runCmd     cmdRunner
}

func defaultCollector() collector {
	return collector{
		btClassDir: "/sys/class/bluetooth",
		rfkillDir:  "/sys/class/rfkill",
		runCmd:     execCmd,
	}
}

func execCmd(ctx context.Context, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	out, err := c.CombinedOutput()
	return string(out), err
}

// Collect discovers Bluetooth controller interfaces (hci0, hci1, ...) and their details.
func Collect() []Adapter {
	return collectWith(defaultCollector())
}

func collectWith(c collector) []Adapter {
	var out []Adapter

	entries, err := os.ReadDir(c.btClassDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "hci") || strings.Contains(name, ":") {
				continue
			}
			hciDir := filepath.Join(c.btClassDir, name)
			ad := Adapter{Name: name}

			if addr, ok := readTrimmed(filepath.Join(hciDir, "address")); ok && addr != "00:00:00:00:00:00" {
				ad.Address = strings.ToUpper(addr)
			}
			if alias, ok := readTrimmed(filepath.Join(hciDir, "name")); ok {
				ad.Alias = cleanText(alias)
			}
			if typ, ok := readTrimmed(filepath.Join(hciDir, "type")); ok {
				ad.Type = formatType(typ)
			}
			if bus, ok := readTrimmed(filepath.Join(hciDir, "bus")); ok {
				ad.Bus = formatBus(bus)
			}
			if target, err := os.Readlink(filepath.Join(hciDir, "device", "driver")); err == nil {
				ad.Driver = filepath.Base(target)
			}
			if ad.Bus == "" {
				if target, err := os.Readlink(filepath.Join(hciDir, "device", "subsystem")); err == nil {
					ad.Bus = formatBus(filepath.Base(target))
				}
			}

			hw := readHardwareInfo(filepath.Join(hciDir, "device"))
			ad.VendorID = hw.vendorID
			ad.ProductID = hw.productID
			ad.Manufacturer = hw.manufacturer
			ad.Product = hw.product
			if ad.Bus == "" && hw.bus != "" {
				ad.Bus = formatBus(hw.bus)
			}

			ad.RFKill = detectRFKill(hciDir, c.rfkillDir, name)
			if ad.RFKill == "soft-blocked" || ad.RFKill == "hard-blocked" {
				ad.Up = false
				ad.Powered = false
				ad.StateKnown = true
			}

			enrichFromCommands(&ad, c.runCmd)

			if ad.Manufacturer == "" {
				ad.Manufacturer = lookupVendor(ad.VendorID, ad.Driver)
			}

			out = append(out, ad)
		}
	}

	// Fallback: if sysfs returned nothing, try bluetoothctl list
	if len(out) == 0 && c.runCmd != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		listOut, err := c.runCmd(ctx, "bluetoothctl", "list")
		cancel()
		if err == nil {
			for i, ctrl := range parseBluetoothctlList(listOut) {
				ad := Adapter{
					Name:    "hci" + strconv.Itoa(i),
					Address: ctrl.addr,
					Alias:   ctrl.name,
				}
				enrichFromCommands(&ad, c.runCmd)
				if ad.Manufacturer == "" {
					ad.Manufacturer = lookupVendor(ad.VendorID, ad.Driver)
				}
				out = append(out, ad)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return naturalLess(out[i].Name, out[j].Name)
	})
	return out
}

func enrichFromCommands(ad *Adapter, run cmdRunner) {
	if run == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	hciOut, err := run(ctx, "hciconfig", "-a", ad.Name)
	cancel()
	if err == nil && strings.TrimSpace(hciOut) != "" {
		applyHCIConfig(ad, hciOut)
	}

	arg := ad.Address
	if arg == "" {
		return
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 800*time.Millisecond)
	btOut, err2 := run(ctx2, "bluetoothctl", "show", arg)
	cancel2()
	if err2 == nil && strings.TrimSpace(btOut) != "" {
		applyBluetoothctlShow(ad, btOut)
	}
}

func detectRFKill(hciDir, rfkillDir, hciName string) string {
	// First check inside /sys/class/bluetooth/hciX/rfkill*
	if entries, err := os.ReadDir(hciDir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "rfkill") {
				if st := readRFKillDir(filepath.Join(hciDir, e.Name())); st != "" {
					return st
				}
			}
		}
	}
	// Fallback: check /sys/class/rfkill/rfkill* matching name == hciName
	if rfkillDir != "" {
		if entries, err := os.ReadDir(rfkillDir); err == nil {
			for _, e := range entries {
				dir := filepath.Join(rfkillDir, e.Name())
				if n, ok := readTrimmed(filepath.Join(dir, "name")); ok && n == hciName {
					if st := readRFKillDir(dir); st != "" {
						return st
					}
				}
			}
		}
	}
	return ""
}

func readRFKillDir(dir string) string {
	if hard, ok := readTrimmed(filepath.Join(dir, "hard")); ok && hard == "1" {
		return "hard-blocked"
	}
	if soft, ok := readTrimmed(filepath.Join(dir, "soft")); ok && soft == "1" {
		return "soft-blocked"
	}
	if state, ok := readTrimmed(filepath.Join(dir, "state")); ok {
		switch state {
		case "0":
			return "soft-blocked"
		case "1":
			return "unblocked"
		case "2":
			return "hard-blocked"
		}
	}
	return ""
}

type hwInfo struct {
	vendorID     string
	productID    string
	manufacturer string
	product      string
	bus          string
}

var (
	reModaliasUSB = regexp.MustCompile(`^usb:v([0-9A-Fa-f]{4})p([0-9A-Fa-f]{4})`)
	reModaliasPCI = regexp.MustCompile(`^pci:v[0-9A-Fa-f]{4}([0-9A-Fa-f]{4})d[0-9A-Fa-f]{4}([0-9A-Fa-f]{4})`)
)

func readHardwareInfo(devPath string) hwInfo {
	var hw hwInfo
	resolved, err := filepath.EvalSymlinks(devPath)
	if err != nil {
		resolved = devPath
	}

	cur := resolved
	for depth := 0; depth < 4; depth++ {
		if cur == "" || cur == "/" || cur == "." {
			break
		}
		if hw.manufacturer == "" {
			if m, ok := readTrimmed(filepath.Join(cur, "manufacturer")); ok {
				hw.manufacturer = cleanText(m)
			}
		}
		if hw.product == "" {
			if p, ok := readTrimmed(filepath.Join(cur, "product")); ok {
				hw.product = cleanText(p)
			}
		}
		if hw.vendorID == "" {
			if v, ok := readTrimmed(filepath.Join(cur, "idVendor")); ok {
				hw.vendorID = normHexID(v)
				if hw.bus == "" {
					hw.bus = "usb"
				}
			} else if v, ok := readTrimmed(filepath.Join(cur, "vendor")); ok {
				hw.vendorID = normHexID(v)
			}
		}
		if hw.productID == "" {
			if p, ok := readTrimmed(filepath.Join(cur, "idProduct")); ok {
				hw.productID = normHexID(p)
			} else if p, ok := readTrimmed(filepath.Join(cur, "device")); ok {
				if !strings.Contains(p, "/") {
					hw.productID = normHexID(p)
				}
			}
		}
		if hw.vendorID == "" {
			if mod, ok := readTrimmed(filepath.Join(cur, "modalias")); ok {
				parseModalias(mod, &hw)
			}
		}
		if hw.manufacturer == "" {
			if comp, ok := readTrimmed(filepath.Join(cur, "of_node", "compatible")); ok {
				applyCompatible(comp, &hw)
			}
		}
		if hw.vendorID != "" && hw.manufacturer != "" {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return hw
}

func parseModalias(mod string, hw *hwInfo) {
	if m := reModaliasUSB.FindStringSubmatch(mod); len(m) == 3 {
		hw.vendorID = strings.ToLower(m[1])
		hw.productID = strings.ToLower(m[2])
		if hw.bus == "" {
			hw.bus = "usb"
		}
		return
	}
	if m := reModaliasPCI.FindStringSubmatch(mod); len(m) == 3 {
		hw.vendorID = strings.ToLower(m[1])
		hw.productID = strings.ToLower(m[2])
		if hw.bus == "" {
			hw.bus = "pci"
		}
	}
}

func applyCompatible(comp string, hw *hwInfo) {
	low := strings.ToLower(comp)
	switch {
	case strings.Contains(low, "brcm,") || strings.Contains(low, "broadcom") || strings.Contains(low, "cypress"):
		hw.manufacturer = "Broadcom"
	case strings.Contains(low, "realtek") || strings.Contains(low, "rtl"):
		hw.manufacturer = "Realtek"
	case strings.Contains(low, "qcom,") || strings.Contains(low, "atheros"):
		hw.manufacturer = "Qualcomm"
	case strings.Contains(low, "mediatek") || strings.Contains(low, "mtk"):
		hw.manufacturer = "MediaTek"
	case strings.Contains(low, "ti,"):
		hw.manufacturer = "Texas Instruments"
	}
	if hw.product == "" {
		for _, part := range strings.Split(comp, "\x00") {
			part = strings.TrimSpace(part)
			if idx := strings.Index(part, ","); idx >= 0 && idx+1 < len(part) {
				hw.product = strings.TrimSuffix(part[idx+1:], "-bt")
				break
			}
		}
	}
}

var (
	reHCITypeBus = regexp.MustCompile(`Type:\s*(\S+)\s+Bus:\s*(\S+)`)
	reHCIAddr    = regexp.MustCompile(`BD Address:\s*([0-9A-Fa-f:]{17})`)
	reHCIName    = regexp.MustCompile(`Name:\s*'([^']*)'`)
	reHCIVer     = regexp.MustCompile(`HCI Version:\s*([0-9.]+)`)
	reHCIMfg     = regexp.MustCompile(`Manufacturer:\s*(.+?)\s*\((\d+)\)`)
)

func applyHCIConfig(ad *Adapter, out string) {
	if m := reHCITypeBus.FindStringSubmatch(out); len(m) == 3 {
		if ad.Type == "" {
			ad.Type = formatType(m[1])
		}
		if ad.Bus == "" {
			ad.Bus = formatBus(m[2])
		}
	}
	if m := reHCIAddr.FindStringSubmatch(out); len(m) == 2 && ad.Address == "" {
		ad.Address = strings.ToUpper(m[1])
	}
	if m := reHCIName.FindStringSubmatch(out); len(m) == 2 && m[1] != "" {
		ad.Alias = m[1]
	}
	if m := reHCIVer.FindStringSubmatch(out); len(m) == 2 {
		ad.HCIVersion = m[1]
	}
	if m := reHCIMfg.FindStringSubmatch(out); len(m) == 3 {
		if id, err := strconv.Atoi(m[2]); err == nil {
			if name, ok := btCompanyIDs[id]; ok {
				if ad.Manufacturer == "" {
					ad.Manufacturer = name
				}
			} else if ad.Manufacturer == "" {
				ad.Manufacturer = strings.TrimSpace(m[1])
			}
		}
	}

	hasLE := strings.Contains(out, "LE Supported")
	hasBREDR := strings.Contains(out, "LMP Version:") || strings.Contains(out, "BREDR")
	switch {
	case hasLE && hasBREDR:
		ad.Mode = "BR/EDR + LE"
	case hasLE:
		ad.Mode = "LE"
	case hasBREDR:
		ad.Mode = "BR/EDR"
	}

	for _, raw := range strings.Split(out, "\n") {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "UP":
			ad.Up = true
			ad.Powered = true
			ad.StateKnown = true
			var scans []string
			for _, f := range fields {
				if f == "PSCAN" || f == "ISCAN" {
					scans = append(scans, f)
				}
				if f == "ISCAN" {
					ad.Discoverable = true
					ad.FlagsKnown = true
				}
			}
			if len(scans) > 0 {
				ad.ScanMode = strings.Join(scans, " ")
			}
		case "DOWN":
			ad.Up = false
			ad.Powered = false
			ad.StateKnown = true
		}
	}
}

func applyBluetoothctlShow(ad *Adapter, out string) {
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "Alias: "):
			val := strings.TrimSpace(strings.TrimPrefix(line, "Alias: "))
			if val != "" {
				ad.Alias = val
			}
		case strings.HasPrefix(line, "Name: "):
			val := strings.TrimSpace(strings.TrimPrefix(line, "Name: "))
			if ad.Alias == "" && val != "" {
				ad.Alias = val
			}
		case strings.HasPrefix(line, "Powered: "):
			v := strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, "Powered: ")), "yes")
			ad.Powered = v
			ad.Up = v
			ad.StateKnown = true
		case strings.HasPrefix(line, "Discoverable: "):
			ad.Discoverable = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, "Discoverable: ")), "yes")
			ad.FlagsKnown = true
		case strings.HasPrefix(line, "Pairable: "):
			ad.Pairable = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, "Pairable: ")), "yes")
			ad.FlagsKnown = true
		}
	}
}

type btCtrl struct {
	addr string
	name string
}

var reCtrlList = regexp.MustCompile(`Controller\s+([0-9A-Fa-f:]{17})\s+(.*)`)

func parseBluetoothctlList(out string) []btCtrl {
	var res []btCtrl
	for _, raw := range strings.Split(out, "\n") {
		if m := reCtrlList.FindStringSubmatch(raw); len(m) == 3 {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[2]), "[default]"))
			res = append(res, btCtrl{
				addr: strings.ToUpper(m[1]),
				name: strings.TrimSpace(name),
			})
		}
	}
	return res
}

func formatBus(b string) string {
	b = strings.TrimSpace(b)
	switch strings.ToLower(b) {
	case "usb":
		return "USB"
	case "uart", "tty", "serial":
		return "UART"
	case "sdio":
		return "SDIO"
	case "spi":
		return "SPI"
	case "pci":
		return "PCI"
	case "virtual":
		return "VIRTUAL"
	default:
		return strings.ToUpper(b)
	}
}

func formatType(t string) string {
	t = strings.TrimSpace(t)
	switch strings.ToLower(t) {
	case "br/edr", "primary", "bredr":
		return "Primary"
	case "amp":
		return "AMP"
	default:
		return t
	}
}

func normHexID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "0x")
	if len(s) > 4 {
		s = s[len(s)-4:]
	}
	if len(s) > 0 && len(s) < 4 {
		s = strings.Repeat("0", 4-len(s)) + s
	}
	return s
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}

var btCompanyIDs = map[int]string{
	2:   "Intel",
	10:  "Qualcomm (CSR)",
	13:  "Texas Instruments",
	15:  "Broadcom",
	29:  "Qualcomm",
	48:  "STMicro",
	70:  "MediaTek",
	72:  "Marvell",
	76:  "Apple",
	89:  "Nordic Semi",
	93:  "Realtek",
	135: "Espressif",
	305: "Cypress",
}

var btVendorIDs = map[string]string{
	"0a12": "Cambridge Silicon Radio",
	"0a5c": "Broadcom",
	"8087": "Intel",
	"0bda": "Realtek",
	"0cf3": "Qualcomm Atheros",
	"0e8d": "MediaTek",
	"13d3": "IMC Networks",
	"0489": "Foxconn",
	"0b05": "ASUS",
	"2357": "TP-Link",
	"050d": "Belkin",
	"1d6b": "Linux",
	"303a": "Espressif",
	"04ca": "Lite-On",
	"413c": "Dell",
}

func lookupVendor(vendorID, driver string) string {
	if vendorID != "" {
		if name, ok := btVendorIDs[strings.ToLower(vendorID)]; ok {
			return name
		}
	}
	d := strings.ToLower(driver)
	switch {
	case strings.Contains(d, "bcm"):
		return "Broadcom"
	case strings.Contains(d, "intel"):
		return "Intel"
	case strings.Contains(d, "rtl"):
		return "Realtek"
	case strings.Contains(d, "ath") || strings.Contains(d, "qca"):
		return "Qualcomm"
	case strings.Contains(d, "mtk"):
		return "MediaTek"
	}
	return ""
}

func readTrimmed(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func naturalLess(a, b string) bool {
	pa, na, okA := splitTrailingNum(a)
	pb, nb, okB := splitTrailingNum(b)
	if okA && okB && pa == pb && na != nb {
		return na < nb
	}
	return a < b
}

func splitTrailingNum(s string) (prefix string, num int, ok bool) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return s, 0, false
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return s, 0, false
	}
	return s[:i], n, true
}
