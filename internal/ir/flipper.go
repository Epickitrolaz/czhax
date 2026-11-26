package ir

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Remote is one .ir file or one Mi Remote export.
type Remote struct {
	Name        string
	Provider    string
	Category    string
	Brand       string
	Model       string
	Path        string
	Signals     []Signal
	Unsupported int
}

// ParseFlipperFile reads a Flipper IR signals/library file.
func ParseFlipperFile(path string) (Remote, error) {
	f, err := os.Open(path)
	if err != nil {
		return Remote{}, err
	}
	defer f.Close()
	return ParseFlipperReader(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), path, f)
}

// ParseFlipperReader parses a Flipper IR signals stream.
func ParseFlipperReader(name, path string, rd io.Reader) (Remote, error) {
	r := Remote{
		Name:     name,
		Provider: "flipper",
		Path:     path,
	}
	var cur map[string]string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		sig, ok := parseFlipperEntry(cur, r.Provider)
		if ok {
			r.Signals = append(r.Signals, sig)
		} else if strings.TrimSpace(cur["name"]) != "" {
			r.Unsupported++
		}
		cur = nil
	}

	sc := bufio.NewScanner(rd)
	// IR files are small, but keep a generous token limit for long raw data.
	sc.Buffer(make([]byte, 1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "name" {
			flush()
			cur = map[string]string{}
		}
		if cur == nil {
			cur = map[string]string{}
		}
		cur[key] = value
	}
	flush()
	if err := sc.Err(); err != nil {
		return Remote{}, err
	}
	if len(r.Signals) == 0 {
		return r, fmt.Errorf("no usable IR signals in %s", filepath.Base(path))
	}
	return r, nil
}

func parseFlipperEntry(fields map[string]string, source string) (Signal, bool) {
	name := strings.TrimSpace(fields["name"])
	if name == "" {
		return Signal{}, false
	}
	switch strings.ToLower(strings.TrimSpace(fields["type"])) {
	case "raw":
		freq := parseIntDefault(fields["frequency"], DefaultFrequency)
		duty := parseFloatDefault(fields["duty_cycle"], DefaultDutyCycle)
		timings := parseTimingList(fields["data"])
		if len(timings) < 3 {
			return Signal{}, false
		}
		return Signal{
			Name:      name,
			Protocol:  "raw",
			Frequency: freq,
			DutyCycle: duty,
			Timings:   normalizeTimings(timings),
			Source:    source,
		}, true
	case "parsed":
		protocol := strings.TrimSpace(fields["protocol"])
		addr, err := parseFlipperHex(fields["address"])
		if err != nil {
			return Signal{}, false
		}
		cmd, err := parseFlipperHex(fields["command"])
		if err != nil {
			return Signal{}, false
		}
		sig, err := EncodeProtocol(protocol, addr, cmd)
		if err != nil {
			return Signal{}, false
		}
		sig.Name = name
		sig.Source = source
		return sig, true
	}
	return Signal{}, false
}

// Flipper stores address/command as 4 little-endian hex bytes.
func parseFlipperHex(s string) (uint32, error) {
	parts := strings.Fields(strings.TrimSpace(s))
	if len(parts) == 0 || len(parts) > 4 {
		return 0, fmt.Errorf("invalid hex value %q", s)
	}
	var out uint32
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return 0, err
		}
		out |= uint32(v) << (8 * i)
	}
	return out, nil
}

func parseTimingList(s string) []int {
	parts := strings.Fields(s)
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v <= 0 {
			continue
		}
		out = append(out, v)
	}
	return out
}

func parseIntDefault(s string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func parseFloatDefault(s string, def float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v <= 0 || v > 1 {
		return def
	}
	return v
}

func (r Remote) WithMeta(provider, category, brand, model string) Remote {
	r.Provider = provider
	r.Category = category
	r.Brand = brand
	r.Model = model
	if r.Name == "" {
		r.Name = model
	}
	return r
}

func (r Remote) BindableSignals() []Signal {
	out := make([]Signal, 0, len(r.Signals))
	for _, s := range r.Signals {
		if len(s.Timings) >= 3 {
			out = append(out, s)
		}
	}
	return out
}

func (r Remote) ProfileName() string {
	parts := []string{}
	for _, p := range []string{r.Brand, r.Model} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, strings.TrimSpace(p))
		}
	}
	if len(parts) == 0 {
		return r.Name
	}
	return strings.Join(parts, " ")
}

func (r Remote) ProfileSource() string {
	if r.Provider == "" {
		return "library"
	}
	return r.Provider
}
