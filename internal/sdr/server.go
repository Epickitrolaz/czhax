package sdr

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ServerRates lists common IQ sample rates for rtl_tcp streaming over LAN/Wi-Fi.
var ServerRates = []int64{
	1_024_000,
	2_048_000,
	2_400_000,
}

// ServerConfig configures an rtl_tcp network IQ server.
type ServerConfig struct {
	DeviceIndex int
	ListenAddr  string // "0.0.0.0" or "127.0.0.1"
	Port        int    // default 1234
	FreqHz      int64  // initial center frequency
	SampleRate  int64  // default 2_048_000
	GainDB      int    // -1 = Auto
}

// BuildRTLTCPArgs constructs the rtl_tcp command arguments.
func BuildRTLTCPArgs(cfg ServerConfig) []string {
	addr := strings.TrimSpace(cfg.ListenAddr)
	if addr == "" {
		addr = "0.0.0.0"
	}
	port := cfg.Port
	if port <= 0 || port > 65535 {
		port = 1234
	}
	freq := ClampFreq(cfg.FreqHz)
	if cfg.FreqHz <= 0 {
		freq = 100_000_000
	}
	rate := cfg.SampleRate
	if rate <= 0 {
		rate = 2_048_000
	}
	args := []string{
		"-d", strconv.Itoa(max(0, cfg.DeviceIndex)),
		"-a", addr,
		"-p", strconv.Itoa(port),
		"-f", strconv.FormatInt(freq, 10),
		"-s", strconv.FormatInt(rate, 10),
	}
	if cfg.GainDB >= 0 {
		args = append(args, "-g", strconv.Itoa(cfg.GainDB))
	}
	return args
}

// LocalIPHint returns the first non-loopback IPv4 address for display, or "127.0.0.1".
func LocalIPHint() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP == nil || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return "127.0.0.1"
}

// FormatSampleRate formats an IQ rate cleanly (e.g. "2.048M", "1.024M").
func FormatSampleRate(hz int64) string {
	return fmt.Sprintf("%.3fM", float64(hz)/1_000_000.0)
}

// RunServer starts rtl_tcp and streams log lines until ctx is canceled or the server exits.
func RunServer(ctx context.Context, cfg ServerConfig, onLine func(string)) error {
	bin := FindTool("rtl_tcp")
	if bin == "" {
		return errors.New("rtl_tcp not installed (apt install rtl-sdr)")
	}
	release, err := acquireUSB(ctx)
	if err != nil {
		return err
	}
	defer release()

	DetachDVBDriverIfNeeded(DiscoverUSBDevices())

	args := BuildRTLTCPArgs(cfg)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	cmd.WaitDelay = 1500 * time.Millisecond

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rtl_tcp: %w", err)
	}

	var mu sync.Mutex
	var lastLine string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		mu.Lock()
		lastLine = line
		mu.Unlock()
		if onLine != nil {
			onLine(line)
		}
	}

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		mu.Lock()
		msg := lastLine
		mu.Unlock()
		if msg != "" {
			return errors.New(humanizeRTLErr(msg))
		}
		return waitErr
	}
	return nil
}
