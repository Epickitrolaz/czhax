package wifi

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestBuildDHCPDiscover(t *testing.T) {
	mac, err := net.ParseMAC("02:11:22:33:44:55")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := BuildDHCPDiscover(mac, 0xdeadbeef, "czhax-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) < 14+20+8+236+10 {
		t.Fatalf("frame too short: %d", len(frame))
	}
	// Ethernet: broadcast destination, spoofed source, IPv4 ethertype.
	for i := 0; i < 6; i++ {
		if frame[i] != 0xff {
			t.Fatalf("dst not broadcast at %d: %#x", i, frame[i])
		}
	}
	if got := net.HardwareAddr(frame[6:12]).String(); got != mac.String() {
		t.Fatalf("src MAC = %s, want %s", got, mac)
	}
	if binary.BigEndian.Uint16(frame[12:14]) != 0x0800 {
		t.Fatal("ethertype is not IPv4")
	}

	ip := frame[14:]
	if ip[0] != 0x45 || ip[9] != 17 {
		t.Fatalf("bad IPv4 header: version/ihl=%#x proto=%d", ip[0], ip[9])
	}
	if got := net.IP(ip[16:20]).String(); got != "255.255.255.255" {
		t.Fatalf("dst IP = %s", got)
	}
	// The header checksum must verify to zero over the header.
	if ipChecksum(ip[:20]) != 0 {
		t.Fatalf("invalid IPv4 header checksum")
	}

	udp := ip[20:]
	if binary.BigEndian.Uint16(udp[0:2]) != 68 || binary.BigEndian.Uint16(udp[2:4]) != 67 {
		t.Fatalf("bad UDP ports: %d -> %d", binary.BigEndian.Uint16(udp[0:2]), binary.BigEndian.Uint16(udp[2:4]))
	}
	bootp := udp[8:]
	if bootp[0] != 1 || bootp[1] != 1 || bootp[2] != 6 {
		t.Fatalf("bad BOOTP header: %v", bootp[:3])
	}
	if binary.BigEndian.Uint32(bootp[4:8]) != 0xdeadbeef {
		t.Fatal("xid not embedded")
	}
	if net.HardwareAddr(bootp[28:34]).String() != mac.String() {
		t.Fatal("chaddr does not match the client MAC")
	}

	if string(bootp[236:240]) != "\x63\x82\x53\x63" {
		t.Fatalf("missing DHCP magic cookie: %v", bootp[236:240])
	}
	opts := bootp[240:]
	if opts[0] != 53 || opts[1] != 1 || opts[2] != dhcpDiscover {
		t.Fatalf("first option is not DHCPDISCOVER: %v", opts[:3])
	}
	if !strings.Contains(string(opts), "czhax-test") {
		t.Fatalf("hostname option missing")
	}
	if opts[len(opts)-1] != 255 {
		t.Fatalf("missing end option")
	}

	if _, err := BuildDHCPDiscover(mac[:5], 1, "x"); err == nil {
		t.Fatal("expected an error for a short MAC")
	}
}

func TestParseDHCPReply(t *testing.T) {
	// Build a synthetic OFFER for 192.168.1.77 / server 192.168.1.1.
	mac, _ := net.ParseMAC("02:aa:bb:cc:dd:ee")
	frame := make([]byte, 14+20+8+240+16)

	// Ethernet: unicast to the client, IPv4.
	copy(frame[0:6], mac)
	frame[12], frame[13] = 0x08, 0x00

	ip := frame[14:]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], 20+8+240+16)
	ip[8] = 64
	ip[9] = 17
	copy(ip[12:16], []byte{192, 168, 1, 1}) // src
	copy(ip[16:20], []byte{192, 168, 1, 77})
	binary.BigEndian.PutUint16(ip[10:12], ipChecksum(ip[:20]))

	udp := ip[20:]
	binary.BigEndian.PutUint16(udp[0:2], 67)
	binary.BigEndian.PutUint16(udp[2:4], 68)
	binary.BigEndian.PutUint16(udp[4:6], 8+240+16)

	bootp := udp[8:]
	bootp[0] = 2 // BOOTREPLY
	copy(bootp[16:20], []byte{192, 168, 1, 77})
	copy(bootp[28:34], mac)
	copy(bootp[236:240], []byte{99, 130, 83, 99})

	opts := bootp[240:]
	opts[0], opts[1], opts[2] = 53, 1, dhcpOffer
	opts[3], opts[4], opts[5], opts[6], opts[7], opts[8] = 54, 4, 192, 168, 1, 1
	opts[9] = 255

	gotIP, server, mtype, ok := ParseDHCPReply(frame)
	if !ok {
		t.Fatal("expected a valid OFFER")
	}
	if gotIP != "192.168.1.77" || server != "192.168.1.1" || mtype != dhcpOffer {
		t.Fatalf("parsed %s/%s/%d", gotIP, server, mtype)
	}

	// A DISCOVER (client -> server) must not parse as an offer.
	opts[2] = dhcpDiscover
	if _, _, _, ok := ParseDHCPReply(frame); ok {
		t.Fatal("client DISCOVER parsed as a server reply")
	}

	// Truncated frames are rejected rather than panicking.
	if _, _, _, ok := ParseDHCPReply(frame[:60]); ok {
		t.Fatal("truncated frame accepted")
	}
	if _, _, _, ok := ParseDHCPReply(nil); ok {
		t.Fatal("nil frame accepted")
	}
}

func TestRandomClientMAC(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		mac := RandomClientMAC()
		if len(mac) != 6 {
			t.Fatalf("bad MAC length %d", len(mac))
		}
		if mac[0]&0x01 != 0 {
			t.Fatalf("multicast bit set: %s", mac)
		}
		if mac[0]&0x02 == 0 {
			t.Fatalf("not locally administered: %s", mac)
		}
		seen[mac.String()] = true
	}
	if len(seen) < 60 {
		t.Fatalf("MACs are not random enough: %d unique", len(seen))
	}
}

func TestDHCPStarvationStatsLine(t *testing.T) {
	line := DHCPStarvationStatsLine(DHCPStarvationStats{Sent: 120, Offers: 41, Unique: 39, LastOffer: "10.0.0.9"})
	if line != "sent=120 offers=41 unique=39 last=10.0.0.9" {
		t.Fatalf("unexpected line: %q", line)
	}
}

// TestDHCPDiscoverUDPChecksum guards against the zero-checksum bug: Linux drops
// DHCP frames whose UDP checksum is 0, so servers never see them.
func TestDHCPDiscoverUDPChecksum(t *testing.T) {
	mac, _ := net.ParseMAC("02:aa:bb:cc:dd:ee")
	frame, err := BuildDHCPDiscover(mac, 42, "czhax")
	if err != nil {
		t.Fatal(err)
	}
	ip := frame[14:]
	udp := ip[20:]
	if binary.BigEndian.Uint16(udp[6:8]) == 0 {
		t.Fatal("UDP checksum is zero; Linux will drop this frame")
	}
	// Validating a datagram that already carries its checksum yields 0xFFFF
	// (udpChecksum returns the complement, clamped from 0 for transmission).
	if got := udpChecksum(ip[12:16], ip[16:20], udp); got != 0xffff {
		t.Fatalf("checksum does not verify: %#x", got)
	}
}

// TestDHCPDiscoverChecksumGolden pins the UDP checksum against a value verified
// with tcpdump/Python: a wrong checksum makes the kernel drop every DISCOVER, so
// the flood silently does nothing.
func TestDHCPDiscoverChecksumGolden(t *testing.T) {
	mac, _ := net.ParseMAC("de:b7:f1:0e:f3:c8")
	frame, err := BuildDHCPDiscover(mac, 0x90b1a034, "czhax")
	if err != nil {
		t.Fatal(err)
	}
	ip := frame[14:]
	udp := ip[20:]
	if got := binary.BigEndian.Uint16(udp[6:8]); got != 0xf236 {
		t.Fatalf("UDP checksum = %#04x, want 0xf236", got)
	}
	// And the header must validate: complement of the sum is zero.
	cp := append([]byte(nil), udp...)
	cp[6], cp[7] = 0, 0
	if got := udpChecksum(ip[12:16], ip[16:20], cp); got != 0xf236 {
		t.Fatalf("udpChecksum = %#04x, want 0xf236", got)
	}
}

// fakeDHCPLink feeds synthetic frames and can simulate a full driver queue.
type fakeDHCPLink struct {
	writes   int
	failFrom int // first write number that returns EAGAIN (0 = never)
	failTo   int // last write number that returns EAGAIN
	replies  [][]byte
	closed   bool
}

func (f *fakeDHCPLink) writeWait(frame []byte, _ time.Duration) error {
	f.writes++
	if f.failFrom > 0 && f.writes >= f.failFrom && f.writes <= f.failTo {
		return unix.EAGAIN
	}
	return nil
}

func (f *fakeDHCPLink) read(buf []byte, _ time.Duration) (int, error) {
	if len(f.replies) == 0 {
		return 0, errPacketTimeout
	}
	n := copy(buf, f.replies[0])
	f.replies = f.replies[1:]
	return n, nil
}

func (f *fakeDHCPLink) close() { f.closed = true }

func offerFrame(mac net.HardwareAddr, ip [4]byte) []byte {
	frame := make([]byte, 14+20+8+240+16)
	copy(frame[0:6], mac)
	frame[12], frame[13] = 0x08, 0x00
	ipH := frame[14:]
	ipH[0] = 0x45
	binary.BigEndian.PutUint16(ipH[2:4], 20+8+240+16)
	ipH[8], ipH[9] = 64, 17
	binary.BigEndian.PutUint16(ipH[10:12], ipChecksum(ipH[:20]))
	udp := ipH[20:]
	binary.BigEndian.PutUint16(udp[0:2], 67)
	binary.BigEndian.PutUint16(udp[2:4], 68)
	binary.BigEndian.PutUint16(udp[4:6], 8+240+16)
	bootp := udp[8:]
	bootp[0] = 2
	copy(bootp[16:20], ip[:])
	copy(bootp[28:34], mac)
	copy(bootp[236:240], []byte{99, 130, 83, 99})
	opts := bootp[240:]
	opts[0], opts[1], opts[2] = 53, 1, dhcpOffer
	opts[3], opts[4], opts[5], opts[6], opts[7], opts[8] = 54, 4, 10, 0, 0, 1
	opts[9] = 255
	return frame
}

// TestDHCPStarvationSurvivesBusyLink is the "send: resource temporarily
// unavailable" fix: a full driver queue must throttle the flood, not abort it.
func TestDHCPStarvationSurvivesBusyLink(t *testing.T) {
	mac, _ := net.ParseMAC("02:aa:bb:cc:dd:ee")
	// The probe (write 1) succeeds, then the driver queue is busy for a while.
	link := &fakeDHCPLink{
		failFrom: 2,
		failTo:   6,
		replies: [][]byte{
			offerFrame(mac, [4]byte{10, 0, 0, 7}),
			offerFrame(mac, [4]byte{10, 0, 0, 8}),
			offerFrame(mac, [4]byte{10, 0, 0, 7}), // duplicate address
		},
	}

	inf := &net.Interface{Name: "test0", Flags: net.FlagUp, HardwareAddr: mac}
	cfg := DHCPStarvationConfig{Iface: "test0", Rate: 200, Count: 12, RandomMAC: true}

	var lines []string
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runDHCPStarvation(ctx, inf, cfg, link, func(f string, a ...any) {
			lines = append(lines, fmt.Sprintf(f, a...))
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run aborted: %v (lines: %v)", err, lines)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("run did not finish")
	}

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "dropped") {
		t.Fatalf("busy-queue drops were not reported:\n%s", joined)
	}
	if !strings.Contains(joined, "finished 12 requests") {
		t.Fatalf("run did not complete its quota:\n%s", joined)
	}
	if !strings.Contains(joined, "drops=") {
		t.Fatalf("stats line missing drops:\n%s", joined)
	}
	_ = link.closed
}

// TestDHCPStarvationReportsNoOffers checks the hint when a server never answers.
func TestDHCPStarvationReportsNoOffers(t *testing.T) {
	link := &fakeDHCPLink{}
	inf := &net.Interface{Name: "test0", Flags: net.FlagUp}
	cfg := DHCPStarvationConfig{Iface: "test0", Rate: 250, Count: 60}

	var mu sync.Mutex
	var lines []string
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runDHCPStarvation(ctx, inf, cfg, link, func(f string, a ...any) {
		mu.Lock()
		lines = append(lines, fmt.Sprintf(f, a...))
		mu.Unlock()
	}); err != nil {
		t.Fatalf("run error: %v", err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "no offers seen") {
		t.Fatalf("missing no-offer hint:\n%s", joined)
	}
}
