package sdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ISMFreqs are common ISM / SRD center frequencies in Hz.
var ISMFreqs = []int64{
	433_920_000,
	315_000_000,
	868_300_000,
	915_000_000,
}

// ISMConfig configures an rtl_433 capture session.
type ISMConfig struct {
	DeviceIndex int
	FreqHz      int64
	GainDB      int // -1 = Auto
	LogToFile   bool
	CaptureDir  string
}

// ISMPacket holds one decoded packet from rtl_433 -F json.
type ISMPacket struct {
	Time    string
	Model   string
	ID      string
	Channel string
	Summary string
	Fields  []ISMField
	RawJSON string
}

// ISMField is a single decoded key-value pair for the detail view.
type ISMField struct {
	Key   string
	Value string
}

// ParseRTL433Line parses one JSON line emitted by `rtl_433 -F json`.
func ParseRTL433Line(line string) (ISMPacket, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return ISMPacket{}, false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return ISMPacket{}, false
	}
	model := strVal(m["model"])
	if model == "" {
		return ISMPacket{}, false
	}

	pkt := ISMPacket{
		Time:    shortTime(strVal(m["time"])),
		Model:   model,
		ID:      strVal(m["id"]),
		Channel: strVal(m["channel"]),
		RawJSON: line,
	}

	// Build a crisp summary for 34-44 col screens.
	var parts []string
	if v, ok := numVal(m["temperature_C"]); ok {
		parts = append(parts, fmt.Sprintf("%.1fC", v))
	} else if v, ok := numVal(m["temperature_F"]); ok {
		parts = append(parts, fmt.Sprintf("%.1fF", v))
	}
	if v, ok := numVal(m["humidity"]); ok {
		parts = append(parts, fmt.Sprintf("%.0f%%", v))
	}
	if v, ok := numVal(m["pressure_kPa"]); ok {
		parts = append(parts, fmt.Sprintf("%.0fkPa", v))
	} else if v, ok := numVal(m["pressure_PSI"]); ok {
		parts = append(parts, fmt.Sprintf("%.1fPSI", v))
	}
	if s := strVal(m["state"]); s != "" {
		parts = append(parts, s)
	}
	if s := strVal(m["cmd"]); s != "" {
		parts = append(parts, "cmd:"+s)
	}
	if s := strVal(m["data"]); s != "" {
		parts = append(parts, s)
	}
	if b, ok := numVal(m["battery_ok"]); ok {
		if b == 0 {
			parts = append(parts, "BAT:LOW")
		} else {
			parts = append(parts, "BAT:OK")
		}
	}
	if len(parts) == 0 && pkt.ID != "" {
		parts = append(parts, "id:"+pkt.ID)
	}
	pkt.Summary = strings.Join(parts, " ")

	keys := make([]string, 0, len(m))
	for k := range m {
		if k == "time" || k == "model" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s := strVal(m[k]); s != "" {
			pkt.Fields = append(pkt.Fields, ISMField{Key: k, Value: s})
		}
	}
	return pkt, true
}

func strVal(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func numVal(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func shortTime(ts string) string {
	ts = strings.TrimSpace(ts)
	if idx := strings.IndexAny(ts, " T"); idx >= 0 && len(ts) > idx+1 {
		rest := ts[idx+1:]
		if len(rest) >= 8 {
			return rest[:8]
		}
		return rest
	}
	if len(ts) >= 8 {
		return ts
	}
	return time.Now().Format("15:04:05")
}

// RunISM runs rtl_433 until ctx is canceled, emitting decoded packets and optional status lines.
func RunISM(ctx context.Context, cfg ISMConfig, onPacket func(ISMPacket), onStatus func(string)) error {
	bin := FindTool("rtl_433")
	if bin == "" {
		return errors.New("rtl_433 not installed (apt install rtl-433)")
	}
	freq := cfg.FreqHz
	if freq <= 0 {
		freq = 433_920_000
	}

	args := []string{
		"-d", strconv.Itoa(max(0, cfg.DeviceIndex)),
		"-f", strconv.FormatInt(freq, 10),
		"-F", "json",
		"-M", "time:iso",
	}
	if cfg.GainDB >= 0 {
		args = append(args, "-g", strconv.Itoa(cfg.GainDB))
	}

	var logFile *os.File
	if cfg.LogToFile && strings.TrimSpace(cfg.CaptureDir) != "" {
		if err := os.MkdirAll(cfg.CaptureDir, 0o755); err == nil {
			name := fmt.Sprintf("sdr_ism_%s_%s.jsonl",
				strings.ReplaceAll(fmt.Sprintf("%.2fM", float64(freq)/1e6), ".", "_"),
				time.Now().Format("20060102_150405"),
			)
			path := filepath.Join(cfg.CaptureDir, name)
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
				logFile = f
				if onStatus != nil {
					onStatus("Log: " + name)
				}
			}
		}
	}
	if logFile != nil {
		defer logFile.Close()
	}

	release, err := acquireUSB(ctx)
	if err != nil {
		return err
	}
	defer release()

	DetachDVBDriverIfNeeded(DiscoverUSBDevices())

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
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	var errMu sync.Mutex
	var lastErrLine string
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rtl_433: %w", err)
	}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			errMu.Lock()
			lastErrLine = line
			errMu.Unlock()
		}
	}()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 16*1024), 256*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if pkt, ok := ParseRTL433Line(line); ok {
			if logFile != nil {
				_, _ = logFile.WriteString(line + "\n")
			}
			if onPacket != nil {
				onPacket(pkt)
			}
		}
	}

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		errMu.Lock()
		msg := lastErrLine
		errMu.Unlock()
		if msg != "" {
			return errors.New(humanizeRTLErr(msg))
		}
		return waitErr
	}
	return nil
}
