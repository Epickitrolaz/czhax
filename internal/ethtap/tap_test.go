package ethtap

import (
	"encoding/binary"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// ethIPv4TCP builds a minimal Ethernet/IPv4/TCP frame carrying payload.
func ethIPv4TCP(srcMAC, dstMAC net.HardwareAddr, srcIP, dstIP net.IP, sport, dport uint16, payload []byte) []byte {
	tcpLen := 20 + len(payload)
	total := 20 + tcpLen
	frame := make([]byte, 14+total)
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], srcMAC)
	frame[12], frame[13] = 0x08, 0x00
	ip := frame[14 : 14+total]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(total))
	ip[8] = 64
	ip[9] = 6
	copy(ip[12:16], srcIP.To4())
	copy(ip[16:20], dstIP.To4())
	binary.BigEndian.PutUint16(ip[10:12], ipChecksum(ip[:20]))
	tcp := ip[20:]
	binary.BigEndian.PutUint16(tcp[0:2], sport)
	binary.BigEndian.PutUint16(tcp[2:4], dport)
	tcp[12] = 5 << 4
	copy(tcp[20:], payload)
	return frame
}

func TestDissectHTTPBasicAuth(t *testing.T) {
	src, _ := net.ParseMAC("02:11:22:33:44:55")
	dst, _ := net.ParseMAC("02:66:77:88:99:aa")
	req := "GET /admin/login?x=1 HTTP/1.1\r\nHost: router.test\r\nAuthorization: Basic YWRtaW46c2VjcmV0\r\n\r\n"
	frame := ethIPv4TCP(src, dst, net.IPv4(10, 0, 0, 5), net.IPv4(10, 0, 0, 1), 43210, 80, []byte(req))

	pkt, ok := Dissect(frame)
	if !ok {
		t.Fatal("frame not dissected")
	}
	if pkt.HTTPMethod != "GET" || pkt.HTTPHost != "router.test" || pkt.HTTPPath != "/admin/login?x=1" {
		t.Fatalf("unexpected http parse: %+v", pkt)
	}
	if pkt.HTTPAuth != "admin:secret" {
		t.Fatalf("basic auth = %q, want admin:secret", pkt.HTTPAuth)
	}
	if pkt.SrcIP.String() != "10.0.0.5" || pkt.DstPort != 80 {
		t.Fatalf("unexpected addresses: %+v", pkt)
	}
}

func TestDissectHTTPPostForm(t *testing.T) {
	src, _ := net.ParseMAC("02:11:22:33:44:55")
	dst, _ := net.ParseMAC("02:66:77:88:99:aa")
	req := "POST /login HTTP/1.1\r\nHost: nas.test\r\nContent-Type: application/x-www-form-urlencoded\r\n\r\nuser=bob&pass=hunter2"
	frame := ethIPv4TCP(src, dst, net.IPv4(10, 0, 0, 5), net.IPv4(10, 0, 0, 9), 5555, 80, []byte(req))
	pkt, _ := Dissect(frame)
	if pkt.HTTPForm != "user=bob&pass=hunter2" {
		t.Fatalf("form = %q", pkt.HTTPForm)
	}
}

// clientHelloSNI builds a TLS 1.2 ClientHello with only the server_name
// extension, enough to exercise the parser.
func clientHelloSNI(host string) []byte {
	ext := []byte{0x00, 0x00} // server_name
	body := append([]byte{0x00}, byte(len(host)+3))
	body = append(body, 0x00, byte(len(host)>>8), byte(len(host)))
	body = append(body, host...)
	ext = append(ext, byte(len(body)>>8), byte(len(body)))
	ext = append(ext, body...)

	hs := []byte{0x03, 0x03}             // version
	hs = append(hs, make([]byte, 32)...) // random
	hs = append(hs, 0x00)                // session id
	hs = append(hs, 0x00, 0x02, 0x13, 0x01)
	hs = append(hs, 0x01, 0x00) // compression
	hs = append(hs, byte(len(ext)>>8), byte(len(ext)))
	hs = append(hs, ext...)

	handshake := []byte{0x01, byte(len(hs) >> 16), byte(len(hs) >> 8), byte(len(hs))}
	handshake = append(handshake, hs...)

	rec := []byte{0x16, 0x03, 0x01, byte(len(handshake) >> 8), byte(len(handshake))}
	return append(rec, handshake...)
}

func TestDissectTLSSNI(t *testing.T) {
	src, _ := net.ParseMAC("02:11:22:33:44:55")
	dst, _ := net.ParseMAC("02:66:77:88:99:aa")
	frame := ethIPv4TCP(src, dst, net.IPv4(10, 0, 0, 5), net.IPv4(1, 1, 1, 1), 40000, 443, clientHelloSNI("mail.example.com"))
	pkt, ok := Dissect(frame)
	if !ok || pkt.SNI != "mail.example.com" {
		t.Fatalf("sni = %q (ok=%v)", pkt.SNI, ok)
	}
}

func dnsQuery(name string, qtype uint16) []byte {
	msg := make([]byte, 12)
	msg[0], msg[1] = 0x12, 0x34
	msg[2] = 0x01 // RD
	binary.BigEndian.PutUint16(msg[4:6], 1)
	for _, label := range strings.Split(name, ".") {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	return binary.BigEndian.AppendUint16(msg, 1) // QCLASS IN
}

func TestBuildDNSSpoof(t *testing.T) {
	query := dnsQuery("example.com", 1)
	resp, name, ok := BuildDNSSpoof(query, net.IPv4(192, 168, 1, 250), 30)
	if !ok || name != "example.com" {
		t.Fatalf("spoof failed: ok=%v name=%q", ok, name)
	}
	if resp[2]&0x80 == 0 {
		t.Fatal("QR bit not set")
	}
	if got := binary.BigEndian.Uint16(resp[6:8]); got != 1 {
		t.Fatalf("ancount = %d, want 1", got)
	}
	if binary.BigEndian.Uint16(resp[2:4])&0x000f != 0 {
		t.Fatal("rcode should be 0")
	}
	// The answer is the last 16 bytes.
	ans := resp[len(resp)-16:]
	if ip := net.IP(ans[12:16]); ip.String() != "192.168.1.250" {
		t.Fatalf("answer ip = %s", ip)
	}
	if binary.BigEndian.Uint16(ans[2:4]) != 1 || binary.BigEndian.Uint16(ans[4:6]) != 1 {
		t.Fatalf("bad answer type/class: %v", ans[:8])
	}
	// AAAA queries get NODATA so clients fall back to IPv4.
	resp6, _, ok := BuildDNSSpoof(dnsQuery("example.com", 28), net.IPv4(192, 168, 1, 250), 30)
	if !ok || binary.BigEndian.Uint16(resp6[6:8]) != 0 {
		t.Fatalf("AAAA should be NODATA: ok=%v ancount=%d", ok, binary.BigEndian.Uint16(resp6[6:8]))
	}
	// Responses must not be re-spoofed (loop protection).
	if _, _, ok := BuildDNSSpoof(resp, net.IPv4(192, 168, 1, 250), 30); ok {
		t.Fatal("a response was spoofed again")
	}
}

func TestDNSReplyFrameWiringAndChecksum(t *testing.T) {
	src, _ := net.ParseMAC("02:11:22:33:44:55") // client
	gw, _ := net.ParseMAC("02:66:77:88:99:aa")  // router
	query := dnsQuery("portal.test", 1)
	frame := ethIPv4TCP(src, gw, net.IPv4(10, 0, 0, 5), net.IPv4(10, 0, 0, 1), 5555, 53, query)
	// ethIPv4TCP built TCP; fix the protocol to UDP by rebuilding the l4 header.
	ip := frame[14:]
	ip[9] = 17
	udp := ip[20:]
	binary.BigEndian.PutUint16(udp[0:2], 5555)
	binary.BigEndian.PutUint16(udp[2:4], 53)
	binary.BigEndian.PutUint16(udp[4:6], uint16(8+len(query)))
	copy(udp[8:], query)
	binary.BigEndian.PutUint16(ip[2:4], uint16(20+8+len(query)))
	frame = frame[:14+20+8+len(query)]

	pkt, ok := Dissect(frame)
	if !ok || pkt.DNSName != "portal.test" || pkt.DstPort != 53 {
		t.Fatalf("query not dissected: %+v ok=%v", pkt, ok)
	}
	payload, _, ok := BuildDNSSpoof(pkt.Payload, net.IPv4(10, 0, 0, 250), 30)
	if !ok {
		t.Fatal("spoof build failed")
	}
	reply, err := DNSReplyFrame(pkt, payload)
	if err != nil {
		t.Fatal(err)
	}
	out, ok := Dissect(reply)
	if !ok {
		t.Fatal("reply not dissectable")
	}
	if out.SrcIP.String() != "10.0.0.1" || out.DstIP.String() != "10.0.0.5" {
		t.Fatalf("reply ip swap wrong: %s -> %s", out.SrcIP, out.DstIP)
	}
	if out.SrcPort != 53 || out.DstPort != 5555 {
		t.Fatalf("reply ports wrong: %d -> %d", out.SrcPort, out.DstPort)
	}
	if out.SrcMAC.String() != gw.String() || out.DstMAC.String() != src.String() {
		t.Fatalf("reply macs wrong: %s -> %s", out.SrcMAC, out.DstMAC)
	}
	// A correct UDP checksum validates to zero over the pseudo-header.
	ipPart := reply[14:]
	udpPart := ipPart[20:]
	if got := udpChecksum(ipPart[12:16], ipPart[16:20], udpPart); got != 0 {
		t.Fatalf("udp checksum invalid: %#04x", got)
	}
}

func TestPortalCandidates(t *testing.T) {
	got := portalCandidates("192.168.1.0/24", 3)
	if len(got) != 3 {
		t.Fatalf("candidates = %v", got)
	}
	want := []string{"192.168.1.254", "192.168.1.253", "192.168.1.252"}
	for i, w := range want {
		if got[i].String() != w {
			t.Fatalf("candidate %d = %s, want %s", i, got[i], w)
		}
	}
	if c := portalCandidates("10.0.0.0/31", 2); len(c) != 0 {
		t.Fatalf("/31 must have no host candidates: %v", c)
	}
	if c := portalCandidates("10.0.0.0/16", 2); len(c) != 2 || c[0].String() != "10.0.255.254" {
		t.Fatalf("unexpected /16 candidates: %v", c)
	}
}

func TestStatsTrackerTopHost(t *testing.T) {
	s := newStatsTracker()
	frame := ethIPv4TCP(
		net.HardwareAddr{2, 0, 0, 0, 0, 2}, net.HardwareAddr{2, 0, 0, 0, 0, 1},
		net.IPv4(10, 0, 0, 9), net.IPv4(10, 0, 0, 1), 1000, 80,
		[]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"),
	)
	pkt, _ := Dissect(frame)
	for i := 0; i < 5; i++ {
		s.add(pkt, len(frame))
	}
	snap := s.snapshot()
	if snap.Packets != 5 || snap.Hosts != 2 || snap.HTTP != 5 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.Top != "10.0.0.9" && snap.Top != "10.0.0.1" {
		t.Fatalf("top host = %q", snap.Top)
	}
	if !strings.Contains(snap.Line(), "hosts") {
		t.Fatalf("stats line = %q", snap.Line())
	}
}

func TestDissectTruncatedFramesNeverPanics(t *testing.T) {
	full := ethIPv4TCP(
		net.HardwareAddr{2, 0, 0, 0, 0, 2}, net.HardwareAddr{2, 0, 0, 0, 0, 1},
		net.IPv4(10, 0, 0, 9), net.IPv4(10, 0, 0, 1), 1234, 443,
		clientHelloSNI("a.test"),
	)
	for i := 0; i < len(full); i++ {
		_, _ = Dissect(full[:i])
	}
	// Hostile headers: huge ihl / zero lengths.
	bad := append([]byte{}, full...)
	bad[14] = 0xf0
	_, _ = Dissect(bad)
}

func TestPcapWriter(t *testing.T) {
	path := t.TempDir() + "/x.pcap"
	w, err := newPcapWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	frame := []byte{1, 2, 3, 4}
	if err := w.write(frame, time.Unix(100, 5000)); err != nil {
		t.Fatal(err)
	}
	_ = w.close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 24+16+len(frame) {
		t.Fatalf("pcap size = %d", len(raw))
	}
	if binary.LittleEndian.Uint32(raw[0:4]) != 0xa1b2c3d4 {
		t.Fatal("bad magic")
	}
	if binary.LittleEndian.Uint32(raw[20:24]) != 1 {
		t.Fatal("bad linktype")
	}
	if binary.LittleEndian.Uint32(raw[24+8:24+12]) != uint32(len(frame)) {
		t.Fatal("bad incl_len")
	}
}

func TestARPRoundTrip(t *testing.T) {
	mac, _ := net.ParseMAC("02:aa:bb:cc:dd:ee")
	frame := arpRequestFrame(mac, net.IPv4(10, 0, 0, 250), net.IPv4(10, 0, 0, 250))
	if frame == nil {
		t.Fatal("no arp frame")
	}
	// Turn it into a reply and check the sender address parser.
	binary.BigEndian.PutUint16(frame[14+6:14+8], 2)
	copy(frame[0:6], mac)
	copy(frame[14+8:14+14], mac)
	ip, ok := parseARPReply(frame)
	if !ok || ip.String() != "10.0.0.250" {
		t.Fatalf("arp reply parse = %v %v", ip, ok)
	}
}
