package ethtap

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"
)

// Proto is the transport carried by a captured frame.
type Proto uint8

const (
	ProtoOther Proto = iota
	ProtoICMP
	ProtoTCP
	ProtoUDP
	ProtoARP
	ProtoIPv6
)

// Packet is the subset of a frame the tap understands. Decoded fields are
// best-effort and only filled in for plaintext protocols.
type Packet struct {
	SrcMAC, DstMAC net.HardwareAddr
	EtherType      uint16
	SrcIP, DstIP   net.IP
	SrcPort        uint16
	DstPort        uint16
	Proto          Proto
	Payload        []byte

	DNSName    string // first question of a DNS query
	HTTPMethod string
	HTTPHost   string
	HTTPPath   string
	HTTPAuth   string // decoded "user:pass" from Basic auth
	HTTPForm   string // urlencoded POST body
	SNI        string // TLS server name
}

// Dissect parses an Ethernet frame. It never panics on truncated or hostile
// input: anything malformed yields ok == false.
func Dissect(frame []byte) (Packet, bool) {
	if len(frame) < 14 {
		return Packet{}, false
	}
	p := Packet{
		DstMAC: net.HardwareAddr(frame[0:6]),
		SrcMAC: net.HardwareAddr(frame[6:12]),
	}
	off := 12
	p.EtherType = binary.BigEndian.Uint16(frame[off : off+2])
	off += 2
	// VLAN tags: the tapped switch may hand us tagged frames.
	for p.EtherType == 0x8100 || p.EtherType == 0x88a8 {
		if len(frame) < off+4 {
			return p, false
		}
		p.EtherType = binary.BigEndian.Uint16(frame[off+2 : off+4])
		off += 4
	}
	if p.EtherType == 0x0806 {
		p.Proto = ProtoARP
		return p, true
	}
	if p.EtherType != 0x0800 && p.EtherType != 0x86dd {
		return p, true
	}
	if p.EtherType == 0x86dd {
		p.Proto = ProtoIPv6
		return p, true
	}
	if len(frame) < off+20 {
		return p, false
	}
	ip := frame[off:]
	ihl := int(ip[0]&0x0f) * 4
	if ihl < 20 || len(ip) < ihl {
		return p, false
	}
	total := int(binary.BigEndian.Uint16(ip[2:4]))
	if total >= ihl && total <= len(ip) {
		ip = ip[:total]
	}
	p.SrcIP = net.IP(ip[12:16])
	p.DstIP = net.IP(ip[16:20])
	flagsFrag := binary.BigEndian.Uint16(ip[6:8])
	if flagsFrag&0x1fff != 0 { // non-first fragment: no transport header
		return p, true
	}
	l4 := ip[ihl:]
	switch ip[9] {
	case 1:
		p.Proto = ProtoICMP
		return p, true
	case 6:
		if len(l4) < 20 {
			return p, true
		}
		p.Proto = ProtoTCP
		p.SrcPort = binary.BigEndian.Uint16(l4[0:2])
		p.DstPort = binary.BigEndian.Uint16(l4[2:4])
		doff := int(l4[12]>>4) * 4
		if doff < 20 || len(l4) < doff {
			return p, true
		}
		p.Payload = l4[doff:]
	case 17:
		if len(l4) < 8 {
			return p, true
		}
		p.Proto = ProtoUDP
		p.SrcPort = binary.BigEndian.Uint16(l4[0:2])
		p.DstPort = binary.BigEndian.Uint16(l4[2:4])
		ulen := int(binary.BigEndian.Uint16(l4[4:6]))
		body := l4[8:]
		if ulen >= 8 && ulen-8 < len(body) {
			body = body[:ulen-8]
		}
		p.Payload = body
	default:
		p.Proto = ProtoOther
	}
	p.decode()
	return p, true
}

func (p *Packet) decode() {
	switch {
	case p.Proto == ProtoUDP && (p.DstPort == 53 || p.SrcPort == 53):
		if name, ok := dnsQuestionName(p.Payload); ok && p.DstPort == 53 {
			p.DNSName = name
		}
	case p.Proto == ProtoTCP && len(p.Payload) > 0:
		if host, method, path, auth, form, ok := parseHTTP(p.Payload); ok {
			p.HTTPHost, p.HTTPMethod, p.HTTPPath, p.HTTPAuth, p.HTTPForm = host, method, path, auth, form
			return
		}
		if sni, ok := parseTLSSNI(p.Payload); ok {
			p.SNI = sni
		}
	}
}

// parseHTTP extracts the request line, Host header, Basic credentials and a
// urlencoded POST body from a cleartext HTTP request.
func parseHTTP(b []byte) (host, method, path, auth, form string, ok bool) {
	if len(b) < 16 {
		return
	}
	line := b
	if i := bytes.Index(b, []byte("\r\n")); i >= 0 {
		line = b[:i]
	}
	parts := strings.Fields(string(line))
	if len(parts) != 3 || !validMethod(parts[0]) || !strings.HasPrefix(parts[2], "HTTP/") {
		return
	}
	method, path = parts[0], parts[1]
	head, body := b, []byte(nil)
	if i := bytes.Index(b, []byte("\r\n\r\n")); i >= 0 {
		head = b[:i]
		if len(b) > i+4 {
			body = b[i+4:]
		}
	}
	contentType := ""
	for _, raw := range strings.Split(string(head), "\r\n") {
		key, val, found := strings.Cut(raw, ":")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "host":
			if host == "" {
				host = val
			}
		case "authorization":
			if len(val) > 6 && strings.EqualFold(val[:6], "basic ") {
				if decoded, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(val[6:])); derr == nil {
					auth = printable(decoded)
				}
			}
		case "content-type":
			contentType = val
		}
	}
	if method == "POST" && strings.Contains(strings.ToLower(contentType), "x-www-form-urlencoded") && len(body) > 0 {
		form = printable(trimN(body, 400))
	}
	return host, method, path, auth, form, path != ""
}

func validMethod(m string) bool {
	switch m {
	case "GET", "POST", "HEAD", "PUT", "DELETE", "OPTIONS", "PATCH", "PROPFIND":
		return true
	}
	return false
}

// printable makes captured text safe for a one-line log entry.
func printable(b []byte) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		switch {
		case c >= 32 && c < 127:
			out = append(out, c)
		case c == '\t':
			out = append(out, ' ')
		default:
			out = append(out, '.')
		}
	}
	return strings.TrimSpace(string(out))
}

func trimN(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

// parseTLSSNI reads the server_name extension of a TLS ClientHello.
func parseTLSSNI(b []byte) (string, bool) {
	// TLS record: type(1) version(2) length(2)
	if len(b) < 9 || b[0] != 0x16 || b[1] != 0x03 {
		return "", false
	}
	if int(binary.BigEndian.Uint16(b[3:5])) > len(b)-5 {
		return "", false
	}
	hs := b[5:]
	if hs[0] != 1 { // ClientHello
		return "", false
	}
	// handshake length(3) version(2) random(32)
	if len(hs) < 38 {
		return "", false
	}
	i := 4 + 2 + 32
	if i >= len(hs) {
		return "", false
	}
	sidLen := int(hs[i])
	i += 1 + sidLen
	if i+2 > len(hs) {
		return "", false
	}
	csLen := int(binary.BigEndian.Uint16(hs[i : i+2]))
	i += 2 + csLen
	if i >= len(hs) {
		return "", false
	}
	compLen := int(hs[i])
	i += 1 + compLen
	if i+2 > len(hs) {
		return "", false
	}
	extTotal := int(binary.BigEndian.Uint16(hs[i : i+2]))
	i += 2
	end := i + extTotal
	if end > len(hs) {
		end = len(hs)
	}
	for i+4 <= end {
		typ := binary.BigEndian.Uint16(hs[i : i+2])
		size := int(binary.BigEndian.Uint16(hs[i+2 : i+4]))
		i += 4
		if i+size > end {
			return "", false
		}
		if typ == 0 { // server_name
			ext := hs[i : i+size]
			if len(ext) < 5 {
				return "", false
			}
			nameLen := int(binary.BigEndian.Uint16(ext[3:5]))
			if 5+nameLen > len(ext) {
				return "", false
			}
			return printable(ext[5 : 5+nameLen]), true
		}
		i += size
	}
	return "", false
}

// pcapWriter records frames in classic pcap format so Wireshark can open the
// capture directly. It is written from Go, so no dumpcap/tcpdump is needed.
type pcapWriter struct {
	f  *os.File
	bw *bufio.Writer
}

func newPcapWriter(path string) (*pcapWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	var hdr [24]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(hdr[4:6], 2)
	binary.LittleEndian.PutUint16(hdr[6:8], 4)
	binary.LittleEndian.PutUint32(hdr[16:20], 262144) // snaplen
	binary.LittleEndian.PutUint32(hdr[20:24], 1)      // LINKTYPE_ETHERNET
	bw := bufio.NewWriterSize(f, 64<<10)
	if _, err := bw.Write(hdr[:]); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &pcapWriter{f: f, bw: bw}, nil
}

func (w *pcapWriter) write(frame []byte, ts time.Time) error {
	if w == nil || w.bw == nil {
		return nil
	}
	var hdr [16]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(ts.Unix()))
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(ts.Nanosecond()/1000))
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(len(frame)))
	binary.LittleEndian.PutUint32(hdr[12:16], uint32(len(frame)))
	if _, err := w.bw.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.bw.Write(frame)
	return err
}

// flush pushes buffered records to disk; the capture loop calls it once a
// second so a crash loses at most a second of traffic.
func (w *pcapWriter) flush() {
	if w == nil || w.bw == nil {
		return
	}
	_ = w.bw.Flush()
}

func (w *pcapWriter) close() error {
	if w == nil || w.f == nil {
		return nil
	}
	if w.bw != nil {
		_ = w.bw.Flush()
	}
	return w.f.Close()
}

// Stats is a snapshot of the tap counters.
type Stats struct {
	Packets int64
	Bytes   int64
	Hosts   int
	ARP     int64
	DNS     int64
	HTTP    int64
	TLS     int64
	IPv6    int64
	Top     string
}

type statsTracker struct {
	packets, bytes            int64
	arp, dns, http, tls, ipv6 int64
	hostBytes                 map[string]int64
}

func newStatsTracker() *statsTracker {
	return &statsTracker{hostBytes: make(map[string]int64, 64)}
}

func (s *statsTracker) add(p Packet, size int) {
	s.packets++
	s.bytes += int64(size)
	switch p.Proto {
	case ProtoARP:
		s.arp++
	case ProtoIPv6:
		s.ipv6++
	}
	if p.DNSName != "" {
		s.dns++
	}
	if p.HTTPHost != "" || p.HTTPMethod != "" {
		s.http++
	}
	if p.SNI != "" {
		s.tls++
	}
	if p.SrcIP != nil {
		s.hostBytes[p.SrcIP.String()] += int64(size)
	}
	if p.DstIP != nil {
		s.hostBytes[p.DstIP.String()] += int64(size)
	}
}

func (s *statsTracker) snapshot() Stats {
	out := Stats{
		Packets: s.packets,
		Bytes:   s.bytes,
		Hosts:   len(s.hostBytes),
		ARP:     s.arp,
		DNS:     s.dns,
		HTTP:    s.http,
		TLS:     s.tls,
		IPv6:    s.ipv6,
	}
	var best int64
	for host, n := range s.hostBytes {
		if n > best || (n == best && host < out.Top) {
			best, out.Top = n, host
		}
	}
	return out
}

// Line renders the one-line counter summary shown under the running header.
func (s Stats) Line() string {
	return fmt.Sprintf("stats %s pkt · %s · %d hosts",
		humanCount(s.Packets), humanBytes(s.Bytes), s.Hosts)
}

// Summary is the same counters without the machine-readable prefix.
func (s Stats) Summary() string {
	return fmt.Sprintf("%s pkt · %s · %d hosts · dns %d http %d tls %d",
		humanCount(s.Packets), humanBytes(s.Bytes), s.Hosts, s.DNS, s.HTTP, s.TLS)
}

// ProtoLine summarises the application protocols seen so far.
func (s Stats) ProtoLine() string {
	return fmt.Sprintf("proto dns %d · http %d · tls %d", s.DNS, s.HTTP, s.TLS)
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.0fk", float64(n)/1_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// sortedKeys is used by tests and callers that want deterministic host lists.
func sortedKeys(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
