package bluetooth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// EmuProfileKind identifies the emulated Bluetooth device role.
type EmuProfileKind string

const (
	EmuKeyboard EmuProfileKind = "keyboard"
	EmuMouse    EmuProfileKind = "mouse"
	EmuCombo    EmuProfileKind = "combo"
	EmuGamepad  EmuProfileKind = "gamepad"
	EmuMedia    EmuProfileKind = "media"
	EmuSpeaker  EmuProfileKind = "speaker"
	EmuHeadset  EmuProfileKind = "headset"
	EmuCustom   EmuProfileKind = "custom"
)

// EmuCustomMode controls which active protocol stack Custom profile runs.
type EmuCustomMode string

const (
	CustomHID       EmuCustomMode = "hid"
	CustomSpeaker   EmuCustomMode = "speaker"
	CustomAdvertise EmuCustomMode = "advertise"
)

// EmuProfileSpec describes a built-in device emulation profile.
type EmuProfileSpec struct {
	Kind        EmuProfileKind
	Label       string
	DefaultName string
	CoD         uint32
	Appearance  uint16
	IsHID       bool
	HasKB       bool
	HasMouse    bool
	HasConsumer bool
	HasGamepad  bool
	IsAudio     bool
	HasHFP      bool
}

var EmuProfiles = []EmuProfileSpec{
	{
		Kind: EmuKeyboard, Label: "Keyboard", DefaultName: "CZHax Keyboard",
		CoD: 0x002540, Appearance: 0x03C1,
		IsHID: true, HasKB: true, HasConsumer: true,
	},
	{
		Kind: EmuMouse, Label: "Mouse", DefaultName: "CZHax Mouse",
		CoD: 0x002580, Appearance: 0x03C2,
		IsHID: true, HasMouse: true,
	},
	{
		Kind: EmuCombo, Label: "KB + Mouse", DefaultName: "CZHax Input",
		CoD: 0x0025C0, Appearance: 0x03C0,
		IsHID: true, HasKB: true, HasMouse: true, HasConsumer: true,
	},
	{
		Kind: EmuGamepad, Label: "Gamepad", DefaultName: "CZHax Pad",
		CoD: 0x002508, Appearance: 0x03C4,
		IsHID: true, HasGamepad: true, HasConsumer: true,
	},
	{
		Kind: EmuMedia, Label: "Media Remote", DefaultName: "CZHax Media",
		CoD: 0x00250C, Appearance: 0x0180,
		IsHID: true, HasConsumer: true,
	},
	{
		Kind: EmuSpeaker, Label: "Speaker (A2DP)", DefaultName: "CZHax Speaker",
		CoD: 0x240414, Appearance: 0x0841,
		IsAudio: true,
	},
	{
		Kind: EmuHeadset, Label: "Headset (A2DP)", DefaultName: "CZHax Headset",
		CoD: 0x240404, Appearance: 0x0421,
		IsAudio: true, HasHFP: true,
	},
	{
		Kind: EmuCustom, Label: "Custom", DefaultName: "CZHax Device",
		CoD: 0x0025C0, Appearance: 0x0000,
		IsHID: true, HasKB: true, HasMouse: true, HasConsumer: true,
	},
}

// EmuCoDPreset describes selectable Class-of-Device presets for Custom mode.
type EmuCoDPreset struct {
	Label      string
	CoD        uint32
	Appearance uint16
}

var EmuCoDPresets = []EmuCoDPreset{
	{Label: "KB+Mouse (0x0025C0)", CoD: 0x0025C0, Appearance: 0x03C0},
	{Label: "Keyboard (0x002540)", CoD: 0x002540, Appearance: 0x03C1},
	{Label: "Mouse (0x002580)", CoD: 0x002580, Appearance: 0x03C2},
	{Label: "Gamepad (0x002508)", CoD: 0x002508, Appearance: 0x03C4},
	{Label: "Speaker (0x240414)", CoD: 0x240414, Appearance: 0x0841},
	{Label: "Headset (0x240404)", CoD: 0x240404, Appearance: 0x0421},
	{Label: "Car Kit (0x200420)", CoD: 0x200420, Appearance: 0x0421},
	{Label: "Phone (0x5A020C)", CoD: 0x5A020C, Appearance: 0x0040},
	{Label: "Laptop (0x02010C)", CoD: 0x02010C, Appearance: 0x0080},
	{Label: "Smart TV (0x04043C)", CoD: 0x04043C, Appearance: 0x0340},
	{Label: "Watch (0x070404)", CoD: 0x070404, Appearance: 0x00C0},
	{Label: "Printer (0x040680)", CoD: 0x040680, Appearance: 0x0000},
	{Label: "Beacon (0x000000)", CoD: 0x000000, Appearance: 0x0000},
}

// FindEmuProfile returns the spec for kind (defaulting to EmuKeyboard).
func FindEmuProfile(kind EmuProfileKind) EmuProfileSpec {
	for _, p := range EmuProfiles {
		if p.Kind == kind {
			return p
		}
	}
	return EmuProfiles[0]
}

// ParseEmuProfile parses a profile string.
func ParseEmuProfile(s string) (EmuProfileKind, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, p := range EmuProfiles {
		if string(p.Kind) == s {
			return p.Kind, nil
		}
	}
	return "", fmt.Errorf("unknown bluetooth emulator profile %q", s)
}

// EmuConfig configures an emulated Bluetooth device session.
type EmuConfig struct {
	HCI          string         `json:"hci"`
	Profile      EmuProfileKind `json:"profile"`
	Name         string         `json:"name"`
	CoD          uint32         `json:"cod"`
	Appearance   uint16         `json:"appearance"`
	CustomMode   EmuCustomMode  `json:"custom_mode,omitempty"`
	CustomUUID   string         `json:"custom_uuid,omitempty"`
	RecordDir    string         `json:"record_dir,omitempty"`
	MonitorAudio bool           `json:"monitor_audio,omitempty"`
	RecordAudio  bool           `json:"record_audio,omitempty"`
	UID          int            `json:"uid,omitempty"`
	GID          int            `json:"gid,omitempty"`
}

// EffectiveSpec resolves the effective capability flags for cfg.
func (cfg EmuConfig) EffectiveSpec() EmuProfileSpec {
	spec := FindEmuProfile(cfg.Profile)
	if cfg.Name != "" {
		spec.DefaultName = cfg.Name
	}
	if cfg.CoD != 0 {
		spec.CoD = cfg.CoD
	}
	if cfg.Appearance != 0 {
		spec.Appearance = cfg.Appearance
	}
	if cfg.Profile == EmuCustom {
		switch cfg.CustomMode {
		case CustomSpeaker:
			spec.IsHID = false
			spec.HasKB = false
			spec.HasMouse = false
			spec.HasConsumer = false
			spec.HasGamepad = false
			spec.IsAudio = true
		case CustomAdvertise:
			spec.IsHID = false
			spec.HasKB = false
			spec.HasMouse = false
			spec.HasConsumer = false
			spec.HasGamepad = false
			spec.IsAudio = false
		default:
			spec.IsHID = true
			spec.HasKB = true
			spec.HasMouse = true
			spec.HasConsumer = true
			spec.IsAudio = false
		}
	}
	return spec
}

// EmuCommand is sent from the UI to the live emulator loop.
type EmuCommand struct {
	Op      string `json:"op"`
	Key     string `json:"key,omitempty"`
	Code    uint8  `json:"code,omitempty"`
	Mods    uint8  `json:"mods,omitempty"`
	Text    string `json:"text,omitempty"`
	DX      int8   `json:"dx,omitempty"`
	DY      int8   `json:"dy,omitempty"`
	Wheel   int8   `json:"wheel,omitempty"`
	Buttons uint8  `json:"buttons,omitempty"`
	GBtn    uint16 `json:"gbtn,omitempty"`
	Usage   uint16 `json:"usage,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
	Action  string `json:"action,omitempty"`
}

// EmuEvent is emitted from the emulator loop back to the UI.
type EmuEvent struct {
	Kind        string `json:"kind"`
	Text        string `json:"text,omitempty"`
	Ready       bool   `json:"ready,omitempty"`
	Connected   bool   `json:"connected,omitempty"`
	PeerAddr    string `json:"peer_addr,omitempty"`
	PeerName    string `json:"peer_name,omitempty"`
	PeerPaired  bool   `json:"peer_paired,omitempty"`
	Transport   string `json:"transport,omitempty"`
	SampleRate  int    `json:"sample_rate,omitempty"`
	Channels    int    `json:"channels,omitempty"`
	AudioLevel  int    `json:"audio_level,omitempty"`
	MonitorOn   bool   `json:"monitor_on,omitempty"`
	Recording   bool   `json:"recording,omitempty"`
	RecordPath  string `json:"record_path,omitempty"`
	RecordBytes int64  `json:"record_bytes,omitempty"`
	RecordSec   int    `json:"record_sec,omitempty"`
	TXCount     int    `json:"tx_count,omitempty"`
	RXCount     int    `json:"rx_count,omitempty"`
	LEDs        uint8  `json:"leds,omitempty"`
}

// RunEmulator starts Bluetooth device emulation. When not root, it delegates
// to `sudo -n czhax bt emulate` using JSON lines over stdin/stdout so the TUI
// can send real-time HID/audio/AVRCP commands while keeping D-Bus root access.
func RunEmulator(ctx context.Context, cfg EmuConfig, cmdCh <-chan EmuCommand, emit func(EmuEvent)) error {
	if os.Geteuid() == 0 {
		return RunEmulatorDirect(ctx, cfg, cmdCh, emit)
	}
	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("resolve czhax executable: %w", err)
	}
	full, prefix, ok := privilegedPrefix(exe)
	if !ok {
		return errors.New("root or passwordless sudo required")
	}

	args := []string{"bt", "emulate",
		"--hci", cfg.HCI,
		"--profile", string(cfg.Profile),
		"--name", cfg.Name,
		"--cod", fmt.Sprintf("0x%06x", cfg.CoD),
		"--appearance", fmt.Sprintf("0x%04x", cfg.Appearance),
		"--custom-mode", string(cfg.CustomMode),
		"--custom-uuid", cfg.CustomUUID,
		"--record-dir", cfg.RecordDir,
		"--uid", strconv.Itoa(os.Getuid()),
		"--gid", strconv.Itoa(os.Getgid()),
	}
	if cfg.MonitorAudio {
		args = append(args, "--monitor")
	}
	if cfg.RecordAudio {
		args = append(args, "--record")
	}
	bin := full
	if len(prefix) > 0 {
		args = append(prefix, args...)
		bin = "sudo"
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 3 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var stdinMu sync.Mutex
	sendCmd := func(c EmuCommand) {
		b, err := json.Marshal(c)
		if err != nil {
			return
		}
		stdinMu.Lock()
		_, _ = stdin.Write(append(b, '\n'))
		stdinMu.Unlock()
	}

	stopWriter := make(chan struct{})
	go func() {
		defer close(stopWriter)
		for {
			select {
			case <-ctx.Done():
				sendCmd(EmuCommand{Op: "stop"})
				stdinMu.Lock()
				_ = stdin.Close()
				stdinMu.Unlock()
				return
			case c, ok := <-cmdCh:
				if !ok {
					sendCmd(EmuCommand{Op: "stop"})
					stdinMu.Lock()
					_ = stdin.Close()
					stdinMu.Unlock()
					return
				}
				sendCmd(c)
			}
		}
	}()

	var wg sync.WaitGroup
	var lastErrLine string
	var errMu sync.Mutex

	wg.Add(2)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 4096), 256*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var ev EmuEvent
			if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Kind != "" {
				if ev.Kind == "error" && ev.Text != "" {
					errMu.Lock()
					lastErrLine = ev.Text
					errMu.Unlock()
				}
				if emit != nil {
					emit(ev)
				}
			} else if emit != nil {
				emit(EmuEvent{Kind: "log", Text: line})
			}
		}
	}()
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 4096), 64*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			errMu.Lock()
			lastErrLine = line
			errMu.Unlock()
			if emit != nil {
				emit(EmuEvent{Kind: "log", Text: line})
			}
		}
	}()

	waitErr := cmd.Wait()
	wg.Wait()
	<-stopWriter

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		errMu.Lock()
		tail := lastErrLine
		errMu.Unlock()
		if tail != "" {
			return fmt.Errorf("%s: %w", tail, waitErr)
		}
		return waitErr
	}
	return nil
}

// RunEmulatorCLI runs the root-privileged helper loop reading JSON commands
// from r and writing JSON EmuEvents to w.
func RunEmulatorCLI(ctx context.Context, cfg EmuConfig, r io.Reader, w io.Writer) error {
	cmdCh := make(chan EmuCommand, 32)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 4096), 64*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var c EmuCommand
			if err := json.Unmarshal([]byte(line), &c); err != nil {
				continue
			}
			if c.Op == "stop" {
				cancel()
				return
			}
			select {
			case cmdCh <- c:
			case <-ctx.Done():
				return
			}
		}
		cancel()
	}()

	var writeMu sync.Mutex
	emit := func(ev EmuEvent) {
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		writeMu.Lock()
		_, _ = w.Write(append(b, '\n'))
		writeMu.Unlock()
	}

	err := RunEmulatorDirect(ctx, cfg, cmdCh, emit)
	if err != nil && !errors.Is(err, context.Canceled) {
		emit(EmuEvent{Kind: "error", Text: err.Error()})
	}
	return err
}

// ParseHexUint32 parses "0x002540" or "2540" into a uint32.
func ParseHexUint32(s string) (uint32, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	if s == "" {
		return 0, errors.New("empty hex value")
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
}

// ChownFile sets ownership of path (when running under sudo with uid/gid).
func ChownFile(path string, uid, gid int) {
	if strings.TrimSpace(path) == "" || uid < 0 || gid < 0 {
		return
	}
	_ = os.Chmod(path, 0o644)
	_ = os.Chown(path, uid, gid)
	_ = os.Chown(filepath.Dir(path), uid, gid)
}
