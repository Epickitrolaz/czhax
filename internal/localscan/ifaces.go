package localscan

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type IfaceKind string

const (
	KindWiFi     IfaceKind = "WiFi"
	KindEthernet IfaceKind = "Eth"
	KindUSB      IfaceKind = "USB"
)

// Interface describes a local network interface and the network it is
// currently connected to (if any).
type Interface struct {
	Name        string
	Kind        IfaceKind
	Up          bool
	Carrier     bool
	OperState   string
	Connected   bool
	NetworkName string
	IPv4        string // e.g. "192.168.1.42/24"
	IP          string // e.g. "192.168.1.42"
	Subnet      string // actual interface IPv4 CIDR, e.g. "192.168.1.0/24"
	ScanTarget  string // target CIDR for nmap (clamped to /24 for huge subnets)
	MAC         string
	Driver      string
	Bus         string
	Product     string
}

// DisplayNetwork returns a concise description of the connected network.
func (i Interface) DisplayNetwork() string {
	if !i.Connected {
		if i.Up && !i.Carrier {
			return "no link"
		}
		if i.Up && i.Carrier && i.IPv4 == "" {
			return "no IP"
		}
		return "not connected"
	}
	name := strings.TrimSpace(i.NetworkName)
	sub := strings.TrimSpace(i.ScanTarget)
	switch {
	case name != "" && sub != "" && !strings.EqualFold(name, sub):
		return name + " · " + sub
	case name != "":
		return name
	case sub != "":
		return sub
	default:
		return "connected"
	}
}

type cmdRunner func(ctx context.Context, name string, args ...string) (string, error)
type netLookup func(name string) (addrs []string, flags net.Flags, mac string, ok bool)

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

func stdNetLookup(name string) ([]string, net.Flags, string, bool) {
	inf, err := net.InterfaceByName(name)
	if err != nil {
		return nil, 0, "", false
	}
	var addrs []string
	if raw, err := inf.Addrs(); err == nil {
		for _, a := range raw {
			addrs = append(addrs, a.String())
		}
	}
	return addrs, inf.Flags, inf.HardwareAddr.String(), true
}

// CollectInterfaces discovers WiFi, Ethernet, and USB tethering interfaces
// along with their connection state and scannable local subnet.
func CollectInterfaces() []Interface {
	return collectInterfacesWith(defaultCollector())
}

func collectInterfacesWith(c collector) []Interface {
	entries, err := os.ReadDir(c.netClassDir)
	if err != nil {
		return nil
	}

	nmConns := queryNMConnections(c.runCmd)
	var out []Interface

	for _, e := range entries {
		name := e.Name()
		if isIgnoredIfaceName(name) {
			continue
		}
		ifaceDir := filepath.Join(c.netClassDir, name)
		if isVirtualIface(c.netClassDir, ifaceDir, name) {
			continue
		}

		inf := Interface{Name: name}
		if st, ok := readTrimmed(filepath.Join(ifaceDir, "operstate")); ok {
			inf.OperState = st
		}
		if mac, ok := readTrimmed(filepath.Join(ifaceDir, "address")); ok && mac != "00:00:00:00:00:00" {
			inf.MAC = mac
		}
		if flagsStr, ok := readTrimmed(filepath.Join(ifaceDir, "flags")); ok {
			if fl, err := strconv.ParseUint(strings.TrimPrefix(flagsStr, "0x"), 16, 64); err == nil {
				inf.Up = fl&0x1 != 0
			}
		}

		// Carrier file may be absent on some virtual/gadget drivers or when down.
		hasCarrierFile := false
		if carStr, ok := readTrimmed(filepath.Join(ifaceDir, "carrier")); ok {
			hasCarrierFile = true
			inf.Carrier = carStr == "1"
		}

		if target, err := os.Readlink(filepath.Join(ifaceDir, "device", "driver")); err == nil {
			inf.Driver = filepath.Base(target)
		}
		if target, err := os.Readlink(filepath.Join(ifaceDir, "device", "subsystem")); err == nil {
			inf.Bus = filepath.Base(target)
		}
		inf.Product = readUSBProduct(filepath.Join(ifaceDir, "device"))

		var addrs []string
		if c.lookupNet != nil {
			var flags net.Flags
			var mac string
			var ok bool
			addrs, flags, mac, ok = c.lookupNet(name)
			if ok {
				if flags&net.FlagLoopback != 0 {
					continue
				}
				if flags&net.FlagUp != 0 {
					inf.Up = true
				}
				if inf.MAC == "" && mac != "" {
					inf.MAC = mac
				}
			}
		}

		isWifi := isWifiSysfs(ifaceDir)
		typStr, _ := readTrimmed(filepath.Join(ifaceDir, "type"))
		uev, _ := readTrimmed(filepath.Join(ifaceDir, "uevent"))
		if !isWifi && !isSupportedLinkType(name, typStr, uev, ifaceDir) {
			continue
		}

		inf.Kind = classifyIface(name, isWifi, inf.Driver, inf.Bus, uev, ifaceDir)
		if !hasCarrierFile {
			// Some USB tethering / gadget interfaces omit sysfs carrier; rely on IFF_UP + operstate != down.
			inf.Carrier = inf.Up && inf.OperState != "down" && inf.OperState != "lowerlayerdown"
		}

		// Monitor-mode WiFi interfaces cannot carry IP traffic.
		isMonitor := isWifi && (typStr == "801" || typStr == "802" || typStr == "803")

		if !isMonitor {
			if cidr, ipStr, subnet, target := pickIPv4Target(addrs); cidr != "" {
				inf.IPv4 = cidr
				inf.IP = ipStr
				inf.Subnet = subnet
				inf.ScanTarget = target
			}
		}

		// USB tethering (rndis_host / cdc_ether / ipheth) frequently reports
		// operstate="unknown" even while IFF_UP + carrier=1 + DHCP IP are active.
		inf.Connected = inf.Up && inf.Carrier && inf.ScanTarget != ""

		if inf.Connected {
			inf.NetworkName = resolveNetworkName(c.runCmd, inf, nmConns)
		}

		out = append(out, inf)
	}

	sort.Slice(out, func(i, j int) bool {
		return naturalLess(out[i].Name, out[j].Name)
	})
	return out
}

func isIgnoredIfaceName(name string) bool {
	if name == "" || name == "lo" {
		return true
	}
	for _, pfx := range []string{
		"docker", "veth", "br-", "virbr", "tun", "tap",
		"wg", "tailscale", "zt", "sit", "ip6tnl", "gre",
		"dummy", "ifb", "bonding_masters",
	} {
		if strings.HasPrefix(name, pfx) {
			return true
		}
	}
	return false
}

func isVirtualIface(netClassDir, ifaceDir, name string) bool {
	if _, err := os.Stat(filepath.Join(ifaceDir, "bridge")); err == nil {
		return true
	}
	// If there is a hardware device symlink, it's a real NIC or USB tether.
	if _, err := os.Stat(filepath.Join(ifaceDir, "device")); err == nil {
		return false
	}
	if target, err := filepath.EvalSymlinks(ifaceDir); err == nil {
		if strings.Contains(target, "/devices/virtual/") {
			// Allow virtual-looking USB gadget names if explicitly usb/rndis.
			return !strings.HasPrefix(name, "usb") && !strings.HasPrefix(name, "rndis")
		}
	}
	_ = netClassDir
	return false
}

func isSupportedLinkType(name, typStr, uev, ifaceDir string) bool {
	if typStr == "" || typStr == "1" {
		return true
	}
	if strings.HasPrefix(name, "usb") || strings.HasPrefix(name, "rndis") || strings.HasPrefix(name, "wwan") {
		return true
	}
	for _, line := range strings.Split(uev, "\n") {
		line = strings.TrimSpace(line)
		if line == "DEVTYPE=gadget" || line == "DEVTYPE=wwan" {
			return true
		}
	}
	if _, err := os.Stat(filepath.Join(ifaceDir, "device")); err == nil {
		// USB tether / modem drivers sometimes use ARPHRD_NONE (65534) or RAWIP (519).
		return typStr == "65534" || typStr == "519"
	}
	return false
}

func classifyIface(name string, isWifi bool, driver, bus, uev, ifaceDir string) IfaceKind {
	if isWifi {
		return KindWiFi
	}
	if strings.HasPrefix(name, "usb") || strings.HasPrefix(name, "rndis") || strings.HasPrefix(name, "wwan") {
		return KindUSB
	}
	for _, line := range strings.Split(uev, "\n") {
		line = strings.TrimSpace(line)
		if line == "DEVTYPE=gadget" || line == "DEVTYPE=wwan" {
			return KindUSB
		}
	}
	switch strings.ToLower(driver) {
	case "rndis_host", "cdc_ether", "cdc_ncm", "cdc_eem", "ipheth",
		"cdc_mbim", "qmi_wwan", "huawei_ether", "g_ether",
		"r8152", "ax88179_178a", "smsc95xx", "asix", "lan78xx":
		return KindUSB
	}
	if strings.EqualFold(bus, "usb") || hasUSBParent(filepath.Join(ifaceDir, "device")) {
		return KindUSB
	}
	return KindEthernet
}

func hasUSBParent(devPath string) bool {
	resolved, err := filepath.EvalSymlinks(devPath)
	if err != nil {
		resolved = devPath
	}
	if strings.Contains(resolved, "/usb") {
		return true
	}
	cur := resolved
	for i := 0; i < 4; i++ {
		if _, err := os.Stat(filepath.Join(cur, "idVendor")); err == nil {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur || parent == "/" || parent == "." {
			break
		}
		cur = parent
	}
	return false
}

func readUSBProduct(devPath string) string {
	resolved, err := filepath.EvalSymlinks(devPath)
	if err != nil {
		resolved = devPath
	}
	cur := resolved
	for i := 0; i < 4; i++ {
		if p, ok := readTrimmed(filepath.Join(cur, "product")); ok {
			p = strings.TrimSpace(strings.ReplaceAll(p, "\x00", ""))
			if p != "" {
				return p
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur || parent == "/" || parent == "." {
			break
		}
		cur = parent
	}
	return ""
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

func pickIPv4Target(addrs []string) (cidr, ipStr, subnet, scanTarget string) {
	for _, raw := range addrs {
		ip, ipNet, err := net.ParseCIDR(strings.TrimSpace(raw))
		if err != nil || ip == nil {
			continue
		}
		v4 := ip.To4()
		if v4 == nil || v4.IsLoopback() || v4.IsLinkLocalUnicast() || v4.IsMulticast() {
			continue
		}
		cidr = raw
		ipStr = v4.String()
		subnet = ipNet.String()
		ones, _ := ipNet.Mask.Size()
		switch {
		case ones == 32:
			scanTarget = ipStr + "/32"
		case ones < 22:
			// Clamp huge networks (e.g. /8 or /16) to the local /24 so
			// handheld nmap scans complete in reasonable time.
			mask24 := net.CIDRMask(24, 32)
			net24 := &net.IPNet{IP: v4.Mask(mask24), Mask: mask24}
			scanTarget = net24.String()
		default:
			scanTarget = subnet
		}
		return cidr, ipStr, subnet, scanTarget
	}
	return "", "", "", ""
}

func queryNMConnections(run cmdRunner) map[string]string {
	out := make(map[string]string)
	if run == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	raw, err := run(ctx, "nmcli", "-t", "-f", "DEVICE,STATE,CONNECTION", "dev", "status")
	cancel()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// nmcli -t escapes ':' inside fields as '\:'
		fields := splitNmcliLine(line)
		if len(fields) < 3 {
			continue
		}
		dev := strings.TrimSpace(fields[0])
		state := strings.ToLower(strings.TrimSpace(fields[1]))
		conn := strings.TrimSpace(fields[2])
		if dev != "" && strings.HasPrefix(state, "connected") && conn != "" && conn != "--" {
			out[dev] = conn
		}
	}
	return out
}

func splitNmcliLine(line string) []string {
	var fields []string
	var cur strings.Builder
	esc := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if esc {
			cur.WriteByte(ch)
			esc = false
			continue
		}
		if ch == '\\' {
			esc = true
			continue
		}
		if ch == ':' {
			fields = append(fields, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	fields = append(fields, cur.String())
	return fields
}

func resolveNetworkName(run cmdRunner, inf Interface, nmConns map[string]string) string {
	if inf.Kind == KindWiFi && run != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		iwOut, err := run(ctx, "iw", "dev", inf.Name, "link")
		cancel()
		if err == nil && !strings.Contains(iwOut, "Not connected") {
			for _, raw := range strings.Split(iwOut, "\n") {
				line := strings.TrimSpace(raw)
				if strings.HasPrefix(line, "SSID:") {
					if ssid := strings.TrimSpace(strings.TrimPrefix(line, "SSID:")); ssid != "" {
						return ssid
					}
				}
			}
		}
	}

	nm := strings.TrimSpace(nmConns[inf.Name])
	if inf.Kind == KindUSB {
		if nm != "" && !isGenericNMName(nm) {
			return nm
		}
		if inf.Product != "" {
			return "USB · " + inf.Product
		}
		return "USB tether"
	}
	if nm != "" {
		return nm
	}
	if inf.Kind == KindWiFi {
		return "WiFi"
	}
	return "Ethernet"
}

func isGenericNMName(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(low, "wired connection") ||
		strings.HasPrefix(low, "ethernet") ||
		strings.HasPrefix(low, "netplan-")
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
