package wifi

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Interface struct {
	Name           string
	Up             bool
	OperState      string
	MAC            string
	MTU            int
	Addrs          []string
	Phy            string
	Mode           string
	MonitorCapable bool
	HasMonCap      bool
	Bands          string
	Manufacturer   string
	Product        string
	Driver         string
	Bus            string
	VendorID       string
	ProductID      string
	Channel        string
	SSID           string
	TxPower        string
}

type cmdRunner func(ctx context.Context, name string, args ...string) (string, error)
type netLookup func(name string) (addrs []string, mtu int, up bool, mac string)

type collector struct {
	netClassDir string
	runCmd      cmdRunner
	lookupNet   netLookup
}

func defaultCollector() collector {
	return collector{
		netClassDir: "/sys/class/net",
		runCmd:      execCmd,
		lookupNet:   stdNetLookup,
	}
}

func execCmd(ctx context.Context, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func stdNetLookup(name string) ([]string, int, bool, string) {
	inf, err := net.InterfaceByName(name)
	if err != nil {
		return nil, 0, false, ""
	}
	var addrs []string
	if raw, err := inf.Addrs(); err == nil {
		// Put IPv4 addresses before IPv6 for compact display
		var v4, v6 []string
		for _, a := range raw {
			s := a.String()
			if strings.Contains(s, ":") {
				v6 = append(v6, s)
			} else {
				v4 = append(v4, s)
			}
		}
		addrs = append(v4, v6...)
	}
	return addrs, inf.MTU, inf.Flags&net.FlagUp != 0, inf.HardwareAddr.String()
}

// Collect discovers available WiFi network interfaces and their hardware/mode details.
func Collect() []Interface {
	return collectWith(defaultCollector())
}

func collectWith(c collector) []Interface {
	entries, err := os.ReadDir(c.netClassDir)
	if err != nil {
		return nil
	}

	type phyCaps struct {
		hasMonCap bool
		monCap    bool
		bands     string
	}
	phyCache := make(map[string]phyCaps)

	var out []Interface
	for _, e := range entries {
		name := e.Name()
		ifaceDir := filepath.Join(c.netClassDir, name)
		if !isWifiSysfs(ifaceDir) {
			continue
		}

		inf := Interface{
			Name: name,
			Mode: "managed",
		}

		if mac, ok := readTrimmed(filepath.Join(ifaceDir, "address")); ok && mac != "00:00:00:00:00:00" {
			inf.MAC = mac
		}
		if st, ok := readTrimmed(filepath.Join(ifaceDir, "operstate")); ok {
			inf.OperState = st
		}
		if mtuStr, ok := readTrimmed(filepath.Join(ifaceDir, "mtu")); ok {
			if mtu, err := strconv.Atoi(mtuStr); err == nil {
				inf.MTU = mtu
			}
		}
		if flagsStr, ok := readTrimmed(filepath.Join(ifaceDir, "flags")); ok {
			if fl, err := strconv.ParseUint(strings.TrimPrefix(flagsStr, "0x"), 16, 64); err == nil {
				inf.Up = fl&0x1 != 0
			}
		}
		if typStr, ok := readTrimmed(filepath.Join(ifaceDir, "type")); ok {
			// ARPHRD_IEEE80211 (801), PRISM (802), RADIOTAP (803)
			if typStr == "801" || typStr == "802" || typStr == "803" {
				inf.Mode = "monitor"
			}
		}

		if target, err := os.Readlink(filepath.Join(ifaceDir, "phy80211")); err == nil {
			inf.Phy = filepath.Base(target)
		}
		if target, err := os.Readlink(filepath.Join(ifaceDir, "device", "driver")); err == nil {
			inf.Driver = filepath.Base(target)
		}
		if target, err := os.Readlink(filepath.Join(ifaceDir, "device", "subsystem")); err == nil {
			inf.Bus = filepath.Base(target)
		}

		if c.lookupNet != nil {
			addrs, mtu, up, mac := c.lookupNet(name)
			if len(addrs) > 0 {
				inf.Addrs = addrs
			}
			if inf.MTU == 0 && mtu > 0 {
				inf.MTU = mtu
			}
			if !inf.Up && up {
				inf.Up = true
			}
			if inf.MAC == "" && mac != "" {
				inf.MAC = mac
			}
		}

		hw := readHardwareInfo(filepath.Join(ifaceDir, "device"))
		inf.VendorID = hw.vendorID
		inf.ProductID = hw.productID
		inf.Manufacturer = hw.manufacturer
		inf.Product = hw.product
		if inf.Bus == "" && hw.bus != "" {
			inf.Bus = hw.bus
		}
		if inf.Manufacturer == "" {
			inf.Manufacturer = lookupVendor(inf.VendorID, inf.Driver)
		}

		// Enrich with `iw dev <name> info`
		iwOk := false
		if c.runCmd != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
			iwOut, err := c.runCmd(ctx, "iw", "dev", name, "info")
			cancel()
			if err == nil && strings.TrimSpace(iwOut) != "" {
				iwOk = true
				applyIWDevInfo(&inf, iwOut)
			}

			if !iwOk {
				ctx2, cancel2 := context.WithTimeout(context.Background(), 800*time.Millisecond)
				cfgOut, err2 := c.runCmd(ctx2, "iwconfig", name)
				cancel2()
				if err2 == nil && strings.TrimSpace(cfgOut) != "" {
					applyIWConfig(&inf, cfgOut)
				}
			}

			if inf.Phy != "" {
				caps, cached := phyCache[inf.Phy]
				if !cached {
					ctx3, cancel3 := context.WithTimeout(context.Background(), 800*time.Millisecond)
					phyOut, err3 := c.runCmd(ctx3, "iw", "phy", inf.Phy, "info")
					cancel3()
					if err3 == nil && strings.TrimSpace(phyOut) != "" {
						caps.hasMonCap, caps.monCap, caps.bands = parseIWPhyInfo(phyOut)
					}
					phyCache[inf.Phy] = caps
				}
				if caps.hasMonCap {
					inf.HasMonCap = true
					inf.MonitorCapable = caps.monCap
				}
				if caps.bands != "" {
					inf.Bands = caps.bands
				}
			}
		}

		out = append(out, inf)
	}

	sort.Slice(out, func(i, j int) bool {
		return naturalLess(out[i].Name, out[j].Name)
	})
	return out
}

func isWifiSysfs(ifaceDir string) bool {
	if _, err := os.Stat(filepath.Join(ifaceDir, "wireless")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(ifaceDir, "phy80211")); err == nil {
		return true
	}
	if uev, ok := readTrimmed(filepath.Join(ifaceDir, "uevent")); ok {
		for _, line := range strings.Split(uev, "\n") {
			if strings.TrimSpace(line) == "DEVTYPE=wlan" {
				return true
			}
		}
	}
	return false
}

type hwInfo struct {
	vendorID     string
	productID    string
	manufacturer string
	product      string
	bus          string
}

var (
	reModaliasUSB  = regexp.MustCompile(`^usb:v([0-9A-Fa-f]{4})p([0-9A-Fa-f]{4})`)
	reModaliasSDIO = regexp.MustCompile(`^sdio:c[0-9A-Fa-f]+v([0-9A-Fa-f]{4})d([0-9A-Fa-f]{4})`)
	reModaliasPCI  = regexp.MustCompile(`^pci:v[0-9A-Fa-f]{4}([0-9A-Fa-f]{4})d[0-9A-Fa-f]{4}([0-9A-Fa-f]{4})`)
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
	if m := reModaliasSDIO.FindStringSubmatch(mod); len(m) == 3 {
		hw.vendorID = strings.ToLower(m[1])
		hw.productID = strings.ToLower(m[2])
		if hw.bus == "" {
			hw.bus = "sdio"
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

func applyIWDevInfo(inf *Interface, out string) {
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "type "):
			inf.Mode = strings.TrimSpace(strings.TrimPrefix(line, "type "))
		case strings.HasPrefix(line, "ssid "):
			inf.SSID = strings.TrimSpace(strings.TrimPrefix(line, "ssid "))
		case strings.HasPrefix(line, "addr "):
			if inf.MAC == "" {
				inf.MAC = strings.TrimSpace(strings.TrimPrefix(line, "addr "))
			}
		case strings.HasPrefix(line, "wiphy "):
			if inf.Phy == "" {
				inf.Phy = "phy" + strings.TrimSpace(strings.TrimPrefix(line, "wiphy "))
			}
		case strings.HasPrefix(line, "channel "):
			rest := strings.TrimPrefix(line, "channel ")
			if idx := strings.Index(rest, ","); idx >= 0 {
				rest = rest[:idx]
			}
			inf.Channel = "ch " + strings.TrimSpace(rest)
		case strings.HasPrefix(line, "txpower "):
			inf.TxPower = strings.TrimSpace(strings.TrimPrefix(line, "txpower "))
		}
	}
}

var (
	reIWConfigMode = regexp.MustCompile(`Mode:([A-Za-z-]+)`)
	reIWConfigSSID = regexp.MustCompile(`ESSID:"([^"]*)"`)
	reIWConfigFreq = regexp.MustCompile(`Frequency:([0-9.]+\s*[GM]Hz)`)
	reIWConfigTxP  = regexp.MustCompile(`Tx-Power=([0-9]+\s*dBm)`)
)

func applyIWConfig(inf *Interface, out string) {
	if m := reIWConfigMode.FindStringSubmatch(out); len(m) == 2 {
		mode := strings.ToLower(m[1])
		if mode == "master" {
			mode = "AP"
		}
		inf.Mode = mode
	}
	if inf.SSID == "" {
		if m := reIWConfigSSID.FindStringSubmatch(out); len(m) == 2 && m[1] != "" && m[1] != "off/any" {
			inf.SSID = m[1]
		}
	}
	if inf.Channel == "" {
		if m := reIWConfigFreq.FindStringSubmatch(out); len(m) == 2 {
			inf.Channel = m[1]
		}
	}
	if inf.TxPower == "" {
		if m := reIWConfigTxP.FindStringSubmatch(out); len(m) == 2 {
			inf.TxPower = m[1]
		}
	}
}

var reFreqMHz = regexp.MustCompile(`\*\s+(\d+(?:\.\d+)?)\s+MHz`)

func parseIWPhyInfo(out string) (hasMonCap, monCap bool, bands string) {
	inModes := false
	has24, has5, has6 := false, false, false

	for _, raw := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "Supported interface modes:") {
			inModes = true
			hasMonCap = true
			continue
		}
		if inModes {
			if strings.HasPrefix(trimmed, "* ") {
				mode := strings.TrimSpace(strings.TrimPrefix(trimmed, "* "))
				if mode == "monitor" {
					monCap = true
				}
				continue
			}
			inModes = false
		}
		if m := reFreqMHz.FindStringSubmatch(trimmed); len(m) == 2 {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				switch {
				case f >= 2400 && f < 2500:
					has24 = true
				case f >= 4900 && f < 5900:
					has5 = true
				case f >= 5925:
					has6 = true
				}
			}
		}
	}

	var b []string
	if has24 {
		b = append(b, "2.4G")
	}
	if has5 {
		b = append(b, "5G")
	}
	if has6 {
		b = append(b, "6G")
	}
	return hasMonCap, monCap, strings.Join(b, "/")
}

var wifiVendorIDs = map[string]string{
	"02d0": "Broadcom",
	"14e4": "Broadcom",
	"0a5c": "Broadcom",
	"0bda": "Realtek",
	"10ec": "Realtek",
	"0cf3": "Qualcomm Atheros",
	"168c": "Qualcomm Atheros",
	"0e8d": "MediaTek",
	"148f": "Ralink",
	"8086": "Intel",
	"2357": "TP-Link",
	"7392": "Edimax",
	"0b05": "ASUS",
	"13b1": "Linksys",
	"0846": "Netgear",
	"050d": "Belkin",
	"2001": "D-Link",
	"0411": "Buffalo",
	"1286": "Marvell",
	"11ab": "Marvell",
	"303a": "Espressif",
	"1d6b": "Linux",
	"04e8": "Samsung",
	"0451": "Texas Instruments",
	"0fe6": "Kontron",
	"1485": "Edison",
	"2c4e": "Mercury",
	"2c02": "Planex",
	"0586": "ZyXEL",
	"07b8": "AboCom",
	"0df6": "Sitecom",
}

func lookupVendor(vendorID, driver string) string {
	if vendorID != "" {
		if name, ok := wifiVendorIDs[strings.ToLower(vendorID)]; ok {
			return name
		}
	}
	d := strings.ToLower(driver)
	switch {
	case strings.HasPrefix(d, "brcm") || strings.HasPrefix(d, "wl"):
		return "Broadcom"
	case strings.HasPrefix(d, "rtl") || strings.HasPrefix(d, "rtw") || strings.Contains(d, "88xx"):
		return "Realtek"
	case strings.HasPrefix(d, "ath") || strings.HasPrefix(d, "carl9170"):
		return "Qualcomm Atheros"
	case strings.HasPrefix(d, "mt76") || strings.HasPrefix(d, "mt79"):
		return "MediaTek"
	case strings.HasPrefix(d, "rt2"):
		return "Ralink"
	case strings.HasPrefix(d, "iwl"):
		return "Intel"
	case strings.HasPrefix(d, "esp"):
		return "Espressif"
	case strings.HasPrefix(d, "mwifiex") || strings.HasPrefix(d, "mwl"):
		return "Marvell"
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
