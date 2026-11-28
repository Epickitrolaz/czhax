package ethtap

import (
	"errors"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

var errPacketTimeout = errors.New("packet timeout")

// packetSocket is an AF_PACKET capture socket bound to one interface. Unlike a
// plain sniffer it enables promiscuous mode, because frames forwarded by a
// Linux bridge are not addressed to the bridge and would otherwise be dropped
// before reaching us.
type packetSocket struct {
	fd  int
	mac net.HardwareAddr
}

func openTapSocket(iface string) (*packetSocket, error) {
	inf, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_ALL)))
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	sll := &unix.SockaddrLinklayer{Protocol: htons(unix.ETH_P_ALL), Ifindex: inf.Index}
	if err := unix.Bind(fd, sll); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	mreq := &unix.PacketMreq{Ifindex: int32(inf.Index), Type: unix.PACKET_MR_PROMISC}
	if err := unix.SetsockoptPacketMreq(fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, mreq); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 8<<20)
	return &packetSocket{fd: fd, mac: inf.HardwareAddr}, nil
}

// readFrame returns one frame and whether it was emitted by this host
// (PACKET_OUTGOING). Our own injected replies are filtered by the caller so
// they are not counted or captured twice.
func (p *packetSocket) readFrame(buf []byte, timeout time.Duration) (int, bool, error) {
	pfds := []unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLIN}}
	ms := int(timeout / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	n, err := unix.Poll(pfds, ms)
	if err == unix.EINTR {
		return 0, false, errPacketTimeout
	}
	if err != nil {
		return 0, false, err
	}
	if n == 0 || pfds[0].Revents&unix.POLLIN == 0 {
		return 0, false, errPacketTimeout
	}
	got, from, err := unix.Recvfrom(p.fd, buf, 0)
	if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
		return 0, false, errPacketTimeout
	}
	if err != nil {
		return 0, false, err
	}
	outgoing := false
	if ll, ok := from.(*unix.SockaddrLinklayer); ok {
		outgoing = ll.Pkttype == unix.PACKET_OUTGOING
	}
	return got, outgoing, nil
}

func (p *packetSocket) write(frame []byte) error {
	_, err := unix.Write(p.fd, frame)
	return err
}

func (p *packetSocket) close() {
	if p != nil && p.fd >= 0 {
		_ = unix.Close(p.fd)
		p.fd = -1
	}
}

func htons(v uint16) uint16 {
	return v<<8 | v>>8
}
