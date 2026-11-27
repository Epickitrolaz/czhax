package bluetooth

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const (
	dbusA2DPSinkPath  = dbus.ObjectPath("/com/czhax/bt/a2dp_sink")
	dbusHFPPath       = dbus.ObjectPath("/com/czhax/bt/hfp")
	uuidA2DPSink      = "0000110b-0000-1000-8000-00805f9b34fb"
	uuidHFPHandsFree  = "0000111e-0000-1000-8000-00805f9b34fb"
	bluezTransportIf  = "org.bluez.MediaTransport1"
	bluezMediaPlayer  = "org.bluez.MediaPlayer1"
	bluezMediaControl = "org.bluez.MediaControl1"
	sbcCodecID        = byte(0x00)
)

type a2dpSinkService struct {
	ctx         context.Context
	conn        *dbus.Conn
	adapterPath dbus.ObjectPath
	cfg         EmuConfig
	emit        func(EmuEvent)
	logf        func(string, ...any)

	mu            sync.Mutex
	registered    bool
	monitorOn     bool
	recording     bool
	activeTrans   dbus.ObjectPath
	transCancel   context.CancelFunc
	transActive   bool
	transStarting bool
	transGen      int
	sampleRate    int
	channels      int
	rxPackets     int
	wav           *pcmWAVWriter
	recordPath    string
	recordStarted time.Time
	player        *rawPCMPlayer
	closed        bool
}

func newA2DPSinkService(ctx context.Context, conn *dbus.Conn, adapterPath dbus.ObjectPath, cfg EmuConfig, emit func(EmuEvent), logf func(string, ...any)) (*a2dpSinkService, error) {
	s := &a2dpSinkService{
		ctx:         ctx,
		conn:        conn,
		adapterPath: adapterPath,
		cfg:         cfg,
		emit:        emit,
		logf:        logf,
		monitorOn:   cfg.MonitorAudio,
		recording:   cfg.RecordAudio,
	}

	if err := conn.Export(s, dbusA2DPSinkPath, "org.bluez.MediaEndpoint1"); err != nil {
		return nil, err
	}

	props := map[string]dbus.Variant{
		"UUID":         dbus.MakeVariant(uuidA2DPSink),
		"Codec":        dbus.MakeVariant(sbcCodecID),
		"Capabilities": dbus.MakeVariant([]byte{0xFF, 0xFF, 2, 53}),
	}
	adapterObj := conn.Object(bluezService, adapterPath)
	if err := adapterObj.Call(bluezMediaIface+".RegisterEndpoint", 0, dbusA2DPSinkPath, props).Err; err != nil {
		return nil, err
	}
	s.registered = true

	// Watch MediaTransport1 State changes.
	_ = conn.AddMatchSignal(
		dbus.WithMatchSender(bluezService),
		dbus.WithMatchInterface(dbusPropsIface),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchArg(0, bluezTransportIf),
	)
	go s.watchTransports()

	if s.recording {
		s.openRecorderLocked(44100, 2)
	}
	return s, nil
}

func (s *a2dpSinkService) close() {
	s.mu.Lock()
	s.closed = true
	if s.transCancel != nil {
		s.transCancel()
		s.transCancel = nil
	}
	s.closePlayerLocked()
	s.closeRecorderLocked()
	reg := s.registered
	s.registered = false
	s.mu.Unlock()

	if reg {
		adapterObj := s.conn.Object(bluezService, s.adapterPath)
		_ = adapterObj.Call(bluezMediaIface+".UnregisterEndpoint", 0, dbusA2DPSinkPath).Err
	}
}

func (s *a2dpSinkService) setMonitor(on bool) {
	s.mu.Lock()
	s.monitorOn = on
	if !on {
		s.closePlayerLocked()
	}
	rec := s.recording
	rpath := s.recordPath
	s.mu.Unlock()

	if s.logf != nil {
		if on {
			s.logf("Speaker monitor ON")
		} else {
			s.logf("Speaker monitor OFF")
		}
	}
	if s.emit != nil {
		s.emit(EmuEvent{Kind: "audio", MonitorOn: on, Recording: rec, RecordPath: rpath})
	}
}

func (s *a2dpSinkService) setRecording(on bool) {
	s.mu.Lock()
	s.recording = on
	if on {
		sr := s.sampleRate
		ch := s.channels
		if sr <= 0 {
			sr = 44100
		}
		if ch <= 0 {
			ch = 2
		}
		if s.wav == nil {
			s.openRecorderLocked(sr, ch)
		}
	} else {
		s.closeRecorderLocked()
	}
	mon := s.monitorOn
	rec := s.recording
	rpath := s.recordPath
	s.mu.Unlock()

	if s.emit != nil {
		s.emit(EmuEvent{Kind: "audio", MonitorOn: mon, Recording: rec, RecordPath: rpath})
	}
}

func (s *a2dpSinkService) openRecorderLocked(sampleRate, channels int) {
	if s.wav != nil {
		return
	}
	dir := strings.TrimSpace(s.cfg.RecordDir)
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "czhax-captures")
	}
	_ = os.MkdirAll(dir, 0o755)
	name := fmt.Sprintf("bt-speaker-%s.wav", time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	w, err := newPCMWAVWriter(path, sampleRate, channels)
	if err != nil {
		s.recording = false
		if s.logf != nil {
			s.logf("Record error: %v", err)
		}
		return
	}
	s.wav = w
	s.recordPath = path
	s.recordStarted = time.Now()
	ChownFile(path, s.cfg.UID, s.cfg.GID)
	if s.logf != nil {
		s.logf("Recording audio to %s", path)
	}
}

func (s *a2dpSinkService) closeRecorderLocked() {
	if s.wav == nil {
		return
	}
	path := s.recordPath
	_ = s.wav.Close()
	s.wav = nil
	ChownFile(path, s.cfg.UID, s.cfg.GID)
	if s.logf != nil {
		s.logf("Saved recording %s", filepath.Base(path))
	}
}

func (s *a2dpSinkService) closePlayerLocked() {
	if s.player != nil {
		s.player.Close()
		s.player = nil
	}
}

// --- org.bluez.MediaEndpoint1 methods ---

func (s *a2dpSinkService) SelectConfiguration(capabilities []byte) ([]byte, *dbus.Error) {
	cfg, err := selectSBCConfig(capabilities)
	if err != nil {
		return nil, dbus.MakeFailedError(err)
	}
	return cfg, nil
}

func (s *a2dpSinkService) SetConfiguration(transport dbus.ObjectPath, properties map[string]dbus.Variant) *dbus.Error {
	rate, ch := 44100, 2
	if v, ok := properties["Configuration"]; ok {
		if b, ok2 := v.Value().([]byte); ok2 && len(b) >= 4 {
			rate, ch = parseSBCConfig(b)
		}
	}
	state := ""
	if v, ok := properties["State"]; ok {
		state, _ = v.Value().(string)
	}

	s.mu.Lock()
	s.sampleRate = rate
	s.channels = ch
	s.activeTrans = transport
	s.mu.Unlock()

	if s.logf != nil {
		s.logf("A2DP configured: %dHz %dch SBC", rate, ch)
	}
	if s.emit != nil {
		s.emit(EmuEvent{
			Kind:       "audio",
			SampleRate: rate,
			Channels:   ch,
			MonitorOn:  s.monitorOn,
			Recording:  s.recording,
			RecordPath: s.recordPath,
		})
	}
	if state == "pending" || state == "active" {
		go s.startTransport(transport)
	}
	return nil
}

func (s *a2dpSinkService) ClearConfiguration(transport dbus.ObjectPath) *dbus.Error {
	s.mu.Lock()
	if s.activeTrans == transport {
		if s.transCancel != nil {
			s.transCancel()
			s.transCancel = nil
		}
		s.activeTrans = ""
		s.transActive = false
		s.transStarting = false
		s.closePlayerLocked()
	}
	s.mu.Unlock()
	if s.logf != nil {
		s.logf("A2DP transport cleared")
	}
	return nil
}

func (s *a2dpSinkService) Release() *dbus.Error {
	return nil
}

func (s *a2dpSinkService) watchTransports() {
	ch := make(chan *dbus.Signal, 32)
	s.conn.Signal(ch)
	defer s.conn.RemoveSignal(ch)

	for {
		select {
		case <-s.ctx.Done():
			return
		case sig, ok := <-ch:
			if !ok {
				return
			}
			if len(sig.Body) < 2 {
				continue
			}
			iface, _ := sig.Body[0].(string)
			if iface != bluezTransportIf {
				continue
			}
			changed, ok := sig.Body[1].(map[string]dbus.Variant)
			if !ok {
				continue
			}
			stVar, ok := changed["State"]
			if !ok {
				continue
			}
			state, _ := stVar.Value().(string)
			switch state {
			case "pending", "active":
				go s.startTransport(sig.Path)
			case "idle":
				s.mu.Lock()
				if s.activeTrans == sig.Path && s.transCancel != nil {
					s.transCancel()
					s.transCancel = nil
				}
				if s.activeTrans == sig.Path {
					s.transActive = false
					s.transStarting = false
				}
				s.closePlayerLocked()
				s.mu.Unlock()
			}
		}
	}
}

// transportBusyLocked reports whether a reader is already attached to (or
// acquiring) this transport. Callers must hold s.mu.
func (s *a2dpSinkService) transportBusyLocked(transport dbus.ObjectPath) bool {
	return s.activeTrans == transport && (s.transActive || s.transStarting)
}

func (s *a2dpSinkService) startTransport(transport dbus.ObjectPath) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	// SetConfiguration and the MediaTransport1 State change both land here, and
	// a second Acquire is rejected by BlueZ with NotAuthorized. Restarting also
	// cancelled the reader that had just acquired the stream, which is why
	// nothing was ever decoded.
	if s.transportBusyLocked(transport) {
		s.mu.Unlock()
		return
	}
	if s.transCancel != nil {
		s.transCancel()
		s.transCancel = nil
	}
	tctx, tcancel := context.WithCancel(s.ctx)
	s.transCancel = tcancel
	s.activeTrans = transport
	s.transActive = false
	s.transStarting = true
	s.transGen++
	gen := s.transGen
	s.mu.Unlock()

	obj := s.conn.Object(bluezService, transport)
	var (
		ufd      dbus.UnixFD
		readMTU  uint16
		writeMTU uint16
	)
	if err := obj.Call(bluezTransportIf+".Acquire", 0).Store(&ufd, &readMTU, &writeMTU); err != nil {
		s.mu.Lock()
		if s.activeTrans == transport && s.transGen == gen {
			s.transStarting = false
		}
		s.mu.Unlock()
		if s.logf != nil {
			s.logf("A2DP acquire failed: %v", err)
		}
		return
	}
	fd := int(ufd)
	s.mu.Lock()
	if s.activeTrans != transport || s.transGen != gen {
		// A newer transport took over while we were acquiring.
		s.mu.Unlock()
		_ = unix.Close(fd)
		_ = obj.Call(bluezTransportIf+".Release", 0).Err
		return
	}
	s.transActive = true
	s.transStarting = false
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.activeTrans == transport {
			s.transActive = false
			s.transStarting = false
		}
		s.mu.Unlock()
		_ = unix.Close(fd)
		_ = obj.Call(bluezTransportIf+".Release", 0).Err
	}()

	if readMTU < 512 {
		readMTU = 1024
	}
	buf := make([]byte, int(readMTU)+64)
	var dec SBCDecoder
	lastEmit := time.Now()
	peak := 0

	if s.logf != nil {
		s.logf("A2DP streaming (MTU %d)", readMTU)
	}

	for {
		if tctx.Err() != nil {
			return
		}
		pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		nReady, err := unix.Poll(pfd, 250)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return
		}
		if nReady == 0 {
			continue
		}
		if (pfd[0].Revents & (unix.POLLERR | unix.POLLHUP | unix.POLLNVAL)) != 0 {
			return
		}
		n, err := unix.Read(fd, buf)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			return
		}
		if n <= 0 {
			continue
		}
		pcm, info, err := dec.DecodeRTP(buf[:n])
		if err != nil || len(pcm) == 0 {
			continue
		}

		if lvl := pcmPeakPercent(pcm); lvl > peak {
			peak = lvl
		}

		s.mu.Lock()
		s.rxPackets++
		rx := s.rxPackets
		if info.SampleRate > 0 && (s.sampleRate != info.SampleRate || s.channels != info.Channels) {
			s.sampleRate = info.SampleRate
			s.channels = info.Channels
			s.closePlayerLocked()
			if s.recording && s.wav == nil {
				s.openRecorderLocked(info.SampleRate, info.Channels)
			}
		}
		if s.recording {
			if s.wav == nil {
				s.openRecorderLocked(info.SampleRate, info.Channels)
			}
			if s.wav != nil {
				_, _ = s.wav.Write(pcm)
			}
		}
		if s.monitorOn {
			if s.player == nil || s.player.rate != info.SampleRate || s.player.channels != info.Channels {
				s.closePlayerLocked()
				if p, perr := startRawPCMPlayer(s.ctx, info.SampleRate, info.Channels); perr == nil {
					s.player = p
				}
			}
			if s.player != nil {
				_, _ = s.player.Write(pcm)
			}
		}
		mon := s.monitorOn
		rec := s.recording
		rpath := s.recordPath
		var rbytes int64
		var rsec int
		if s.wav != nil {
			rbytes = s.wav.dataBytes
			if !s.recordStarted.IsZero() {
				rsec = int(time.Since(s.recordStarted).Seconds())
			}
		}
		s.mu.Unlock()

		if time.Since(lastEmit) >= 180*time.Millisecond {
			lastEmit = time.Now()
			if s.emit != nil {
				s.emit(EmuEvent{
					Kind:        "audio",
					SampleRate:  info.SampleRate,
					Channels:    info.Channels,
					AudioLevel:  peak,
					MonitorOn:   mon,
					Recording:   rec,
					RecordPath:  rpath,
					RecordBytes: rbytes,
					RecordSec:   rsec,
					RXCount:     rx,
				})
			}
			peak = 0
		}
	}
}

func selectSBCConfig(cap []byte) ([]byte, error) {
	if len(cap) < 4 {
		return nil, errors.New("short SBC capabilities")
	}
	out := make([]byte, 4)
	// Frequency (prefer 44.1kHz or 48kHz for best compatibility).
	var freq byte
	switch {
	case cap[0]&0x20 != 0:
		freq = 0x20 // 44.1 kHz
	case cap[0]&0x10 != 0:
		freq = 0x10 // 48 kHz
	case cap[0]&0x40 != 0:
		freq = 0x40 // 32 kHz
	case cap[0]&0x80 != 0:
		freq = 0x80 // 16 kHz
	default:
		freq = 0x20
	}
	// Channel mode.
	var mode byte
	switch {
	case cap[0]&0x01 != 0:
		mode = 0x01 // Joint Stereo
	case cap[0]&0x02 != 0:
		mode = 0x02 // Stereo
	case cap[0]&0x04 != 0:
		mode = 0x04 // Dual Channel
	default:
		mode = 0x08 // Mono
	}
	out[0] = freq | mode

	// Blocks.
	var blk byte
	switch {
	case cap[1]&0x10 != 0:
		blk = 0x10 // 16
	case cap[1]&0x20 != 0:
		blk = 0x20 // 12
	case cap[1]&0x40 != 0:
		blk = 0x40 // 8
	default:
		blk = 0x80 // 4
	}
	// Subbands.
	sb := byte(0x04) // 8 subbands
	if cap[1]&0x04 == 0 && cap[1]&0x08 != 0 {
		sb = 0x08
	}
	// Allocation.
	alloc := byte(0x01) // Loudness
	if cap[1]&0x01 == 0 && cap[1]&0x02 != 0 {
		alloc = 0x02
	}
	out[1] = blk | sb | alloc

	minBP := cap[2]
	if minBP < 2 {
		minBP = 2
	}
	maxBP := cap[3]
	if maxBP > 53 || maxBP < minBP {
		maxBP = 53
	}
	out[2] = minBP
	out[3] = maxBP
	return out, nil
}

func parseSBCConfig(cfg []byte) (rate, channels int) {
	rate, channels = 44100, 2
	if len(cfg) < 1 {
		return rate, channels
	}
	switch {
	case cfg[0]&0x80 != 0:
		rate = 16000
	case cfg[0]&0x40 != 0:
		rate = 32000
	case cfg[0]&0x20 != 0:
		rate = 44100
	case cfg[0]&0x10 != 0:
		rate = 48000
	}
	if cfg[0]&0x08 != 0 {
		channels = 1
	} else {
		channels = 2
	}
	return rate, channels
}

func pcmPeakPercent(pcm []byte) int {
	maxAmp := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int(int16(binary.LittleEndian.Uint16(pcm[i : i+2])))
		if v < 0 {
			v = -v
		}
		if v > maxAmp {
			maxAmp = v
		}
	}
	pct := (maxAmp * 100) / 32767
	if pct > 100 {
		pct = 100
	}
	return pct
}

// --- Dynamic PCM WAV Writer ---

type pcmWAVWriter struct {
	f          *os.File
	sampleRate int
	channels   int
	dataBytes  int64
}

func newPCMWAVWriter(path string, sampleRate, channels int) (*pcmWAVWriter, error) {
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	if channels <= 0 {
		channels = 2
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	w := &pcmWAVWriter{f: f, sampleRate: sampleRate, channels: channels}
	if err := w.writeHeader(0); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

func (w *pcmWAVWriter) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	w.dataBytes += int64(n)
	return n, err
}

func (w *pcmWAVWriter) Close() error {
	if w.f == nil {
		return nil
	}
	if _, err := w.f.Seek(0, io.SeekStart); err == nil {
		_ = w.writeHeader(uint32(w.dataBytes))
	}
	err := w.f.Close()
	w.f = nil
	return err
}

func (w *pcmWAVWriter) writeHeader(dataSize uint32) error {
	var hdr [44]byte
	byteRate := uint32(w.sampleRate * w.channels * 2)
	blockAlign := uint16(w.channels * 2)
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], 36+dataSize)
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1)
	binary.LittleEndian.PutUint16(hdr[22:24], uint16(w.channels))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(w.sampleRate))
	binary.LittleEndian.PutUint32(hdr[28:32], byteRate)
	binary.LittleEndian.PutUint16(hdr[32:34], blockAlign)
	binary.LittleEndian.PutUint16(hdr[34:36], 16)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], dataSize)
	_, err := w.f.Write(hdr[:])
	return err
}

// --- Live PCM Audio Monitor (aplay / mpv) ---

type rawPCMPlayer struct {
	rate     int
	channels int
	cmd      *exec.Cmd
	stdin    io.WriteCloser
}

func startRawPCMPlayer(ctx context.Context, rate, channels int) (*rawPCMPlayer, error) {
	var bin string
	var args []string
	if p, err := exec.LookPath("aplay"); err == nil {
		bin = p
		args = []string{"-q", "-f", "S16_LE", "-r", fmt.Sprintf("%d", rate), "-c", fmt.Sprintf("%d", channels), "-t", "raw", "-"}
	} else if p, err := exec.LookPath("mpv"); err == nil {
		bin = p
		args = []string{
			"--really-quiet", "--no-terminal", "--no-video",
			"--demuxer=rawaudio",
			fmt.Sprintf("--demuxer-rawaudio-rate=%d", rate),
			fmt.Sprintf("--demuxer-rawaudio-channels=%d", channels),
			"--demuxer-rawaudio-format=s16le",
			"-",
		}
	} else {
		return nil, errors.New("no raw audio player found (aplay/mpv)")
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	return &rawPCMPlayer{rate: rate, channels: channels, cmd: cmd, stdin: stdin}, nil
}

func (p *rawPCMPlayer) Write(b []byte) (int, error) {
	if p.stdin == nil {
		return 0, io.ErrClosedPipe
	}
	return p.stdin.Write(b)
}

func (p *rawPCMPlayer) Close() {
	if p.stdin != nil {
		_ = p.stdin.Close()
		p.stdin = nil
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		_ = p.cmd.Wait()
	}
}

// --- AVRCP remote playback control ---

func sendAVRCPCommand(conn *dbus.Conn, adapterPath dbus.ObjectPath, action string) error {
	root := conn.Object(bluezService, "/")
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := root.Call(dbusObjMgrIface+".GetManagedObjects", 0).Store(&objs); err != nil {
		return err
	}
	prefix := string(adapterPath) + "/dev_"
	method := ""
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "play":
		method = "Play"
	case "pause":
		method = "Pause"
	case "next":
		method = "Next"
	case "prev", "previous":
		method = "Previous"
	case "stop":
		method = "Stop"
	default:
		return fmt.Errorf("unknown AVRCP action %q", action)
	}

	for path, ifaces := range objs {
		if !strings.HasPrefix(string(path), prefix) {
			continue
		}
		if _, ok := ifaces[bluezMediaPlayer]; ok {
			if err := conn.Object(bluezService, path).Call(bluezMediaPlayer+"."+method, 0).Err; err == nil {
				return nil
			}
		}
	}
	for path, ifaces := range objs {
		if !strings.HasPrefix(string(path), prefix) {
			continue
		}
		if _, ok := ifaces[bluezMediaControl]; ok {
			if err := conn.Object(bluezService, path).Call(bluezMediaControl+"."+method, 0).Err; err == nil {
				return nil
			}
		}
	}
	return errors.New("no connected AVRCP player")
}

// --- Minimal HFP Hands-Free Profile (0x111E) ---

type hfpProfileService struct {
	conn *dbus.Conn
	logf func(string, ...any)
}

func newHFPProfileService(conn *dbus.Conn, logf func(string, ...any)) *hfpProfileService {
	return &hfpProfileService{conn: conn, logf: logf}
}

func (h *hfpProfileService) register() error {
	if err := h.conn.Export(h, dbusHFPPath, "org.bluez.Profile1"); err != nil {
		return err
	}
	mgr := h.conn.Object(bluezService, "/org/bluez")
	opts := map[string]dbus.Variant{
		"Role":     dbus.MakeVariant("client"),
		"Features": dbus.MakeVariant(uint16(0x0010)),
	}
	return mgr.Call(bluezProfileMgrIface+".RegisterProfile", 0, dbusHFPPath, uuidHFPHandsFree, opts).Err
}

func (h *hfpProfileService) unregister() {
	mgr := h.conn.Object(bluezService, "/org/bluez")
	_ = mgr.Call(bluezProfileMgrIface+".UnregisterProfile", 0, dbusHFPPath).Err
}

func (h *hfpProfileService) Release() *dbus.Error { return nil }

func (h *hfpProfileService) RequestDisconnection(device dbus.ObjectPath) *dbus.Error {
	return nil
}

func (h *hfpProfileService) NewConnection(device dbus.ObjectPath, ufd dbus.UnixFD, props map[string]dbus.Variant) *dbus.Error {
	f := os.NewFile(uintptr(ufd), "hfp-rfcomm")
	if f == nil {
		return nil
	}
	go func() {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			switch {
			case strings.HasPrefix(line, "AT+BRSF="):
				_, _ = f.WriteString("\r\n+BRSF: 0\r\n\r\nOK\r\n")
			case line == "AT+CIND=?":
				_, _ = f.WriteString("\r\n+CIND: (\"service\",(0,1)),(\"call\",(0,1)),(\"callsetup\",(0-3)),(\"signal\",(0-5))\r\n\r\nOK\r\n")
			case line == "AT+CIND?":
				_, _ = f.WriteString("\r\n+CIND: 1,0,0,5\r\n\r\nOK\r\n")
			default:
				_, _ = f.WriteString("\r\nOK\r\n")
			}
		}
	}()
	return nil
}
