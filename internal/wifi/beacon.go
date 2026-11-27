package wifi

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"
)

// BeaconConfig describes a beacon-flood run.
type BeaconConfig struct {
	Iface        string `json:"iface"`
	MonitorIface string `json:"monitor_iface,omitempty"`
	Channel      int    `json:"channel"`
	SSIDs        []string
	RandomMAC    bool `json:"random_mac"`
	IntervalMS   int  `json:"interval_ms"`
}

// MaxSSIDLen is the 802.11 SSID element limit.
const MaxSSIDLen = 32

// randomBSSID returns a locally administered unicast address, the shape every
// spoofed AP uses.
func randomBSSID() [6]byte {
	var b [6]byte
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	b[0] = (b[0] &^ 0x01) | 0x02
	if b[0] == 0x02 && b[1] == 0 && b[2] == 0 && b[3] == 0 && b[4] == 0 && b[5] == 0 {
		b[5] = 1
	}
	return b
}

// BuildBeaconFrame builds a radiotap-prefixed 802.11 beacon advertising ssid
// from bssid. The frame is what a spam tool injects on a monitor interface.
func BuildBeaconFrame(ssid string, bssid [6]byte, seq uint16, channel int) ([]byte, error) {
	if len([]rune(ssid)) > MaxSSIDLen {
		return nil, fmt.Errorf("ssid %q is longer than %d bytes", ssid, MaxSSIDLen)
	}
	if channel <= 0 || channel > 196 {
		channel = 6
	}

	ies := make([]byte, 0, 64)
	ies = append(ies, 0x00, byte(len(ssid))) // SSID
	ies = append(ies, []byte(ssid)...)
	// Supported rates: 1, 2, 5.5, 11, 6, 9, 12, 18 Mbps.
	ies = append(ies, 0x01, 0x08, 0x82, 0x84, 0x8b, 0x96, 0x0c, 0x12, 0x18, 0x24)
	ies = append(ies, 0x03, 0x01, byte(channel))                                  // DS parameter set
	ies = append(ies, 0x05, 0x04, 0x00, 0x01, 0x00, 0x00)                         // TIM
	ies = append(ies, 0x32, 0x04, 0x0c, 0x12, 0x18, 0x24)                         // Extended rates
	ies = append(ies, 0x7f, 0x08, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40) // ext capabilities

	// Radiotap: version 0, len 8, no present words (rate defaults to 1 Mbps).
	frame := make([]byte, 8, 8+24+12+len(ies))
	frame[2] = 8

	hdr := make([]byte, 24)
	hdr[0] = 0x80 // management: beacon
	copy(hdr[4:10], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	copy(hdr[10:16], bssid[:])
	copy(hdr[16:22], bssid[:])
	binary.LittleEndian.PutUint16(hdr[22:24], seq<<4)
	frame = append(frame, hdr...)

	fixed := make([]byte, 12)
	binary.LittleEndian.PutUint64(fixed[0:8], uint64(time.Now().UnixNano()/1000))
	binary.LittleEndian.PutUint16(fixed[8:10], 100)     // beacon interval (100 TU)
	binary.LittleEndian.PutUint16(fixed[10:12], 0x0411) // ESS | ShortPreamble | ShortSlot
	frame = append(frame, fixed...)
	frame = append(frame, ies...)
	return frame, nil
}

// ParseSSIDList turns a file body into a clean SSID list: one name per line,
// blank lines and #comments ignored, duplicates dropped, length clipped.
func ParseSSIDList(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		name := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		if len([]byte(name)) > MaxSSIDLen {
			name = string([]byte(name)[:MaxSSIDLen])
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// LoadSSIDFile reads an SSID list from disk.
func LoadSSIDFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	list := ParseSSIDList(string(data))
	if len(list) == 0 {
		return nil, errors.New("no SSIDs in file")
	}
	return list, nil
}

// NumberedSSIDs builds prefix1..prefixN (the "name with a number appended"
// mode). It keeps the result inside the 32-byte SSID limit.
func NumberedSSIDs(prefix string, count int) []string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "FreeWiFi"
	}
	if count < 1 {
		count = 1
	}
	out := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		suffix := fmt.Sprintf("%d", i)
		head := prefix
		if len([]byte(head))+len(suffix) > MaxSSIDLen {
			head = string([]byte(head)[:MaxSSIDLen-len(suffix)])
		}
		out = append(out, head+suffix)
	}
	return out
}

// BeaconStats is reported while a flood runs.
type BeaconStats struct {
	Frames  int
	SSIDs   int
	BSSIDs  int
	Elapsed time.Duration
}

// RunBeaconSpam injects a beacon for every configured SSID, optionally with a
// fresh random BSSID for every single frame.
func RunBeaconSpam(ctx context.Context, cfg BeaconConfig, onLine func(string)) error {
	emit := func(format string, args ...any) {
		if onLine != nil {
			onLine(fmt.Sprintf(format, args...))
		}
	}
	if len(cfg.SSIDs) == 0 {
		return errors.New("no SSIDs configured")
	}
	if cfg.Channel <= 0 {
		cfg.Channel = 6
	}
	interval := time.Duration(cfg.IntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = 15 * time.Millisecond
	}

	r := defaultPentestRunner()
	req := PentestRequest{ScanIface: cfg.Iface, MonitorIface: cfg.MonitorIface, Channel: cfg.Channel}
	mon, created, err := r.ensureMonitor(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = r.cleanupMonitor(cfg.Iface, mon, created) }()
	emit("Beacon spam on %s (channel %d)", mon, cfg.Channel)

	sock, err := openPacketSocket(mon)
	if err != nil {
		return fmt.Errorf("open %s: %w", mon, err)
	}
	defer sock.close()

	bssid := randomBSSID()
	var seq uint16
	stats := BeaconStats{SSIDs: len(cfg.SSIDs), BSSIDs: 1}
	start := time.Now()
	lastReport := start

	for ctx.Err() == nil {
		for _, ssid := range cfg.SSIDs {
			if ctx.Err() != nil {
				break
			}
			frame, ferr := BuildBeaconFrame(ssid, bssid, seq, cfg.Channel)
			seq++
			if ferr != nil {
				emit("skip %q: %v", ssid, ferr)
				continue
			}
			if werr := sock.write(frame); werr != nil {
				return fmt.Errorf("inject: %w", werr)
			}
			stats.Frames++
			if cfg.RandomMAC {
				bssid = randomBSSID()
				stats.BSSIDs++
			}
			select {
			case <-ctx.Done():
			case <-time.After(interval):
			}
		}
		if time.Since(lastReport) >= time.Second {
			stats.Elapsed = time.Since(start)
			emit("beacons %d · ssids %d · bssids %d · %s",
				stats.Frames, stats.SSIDs, stats.BSSIDs, stats.Elapsed.Round(time.Second))
			lastReport = time.Now()
		}
	}
	stats.Elapsed = time.Since(start)
	emit("stopped after %d beacons (%d bssids) in %s", stats.Frames, stats.BSSIDs, stats.Elapsed.Round(time.Second))
	return ctx.Err()
}
