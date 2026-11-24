package sysinfo

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type NetIface struct {
	Name   string
	Up     bool
	MAC    string
	MTU    int
	Addrs  []string
	IsWifi bool
}

type Snapshot struct {
	Hostname   string
	Arch       string
	Kernel     string
	Uptime     time.Duration
	Load1      string
	MemUsedMB  uint64
	MemTotalMB uint64
	DiskFreeGB float64
	DiskTotGB  float64
	TempC      float64
	HasTemp    bool
	BatteryPct int
	HasBattery bool
	Charging   bool
	PrimaryIP  string
	NetUp      bool
	WifiUp     bool
	Interfaces []NetIface
	IsCardZero bool
	Collected  time.Time
}

func Collect() Snapshot {
	s := Snapshot{
		Arch:      runtime.GOARCH,
		Collected: time.Now(),
	}

	if h, err := os.Hostname(); err == nil {
		s.Hostname = h
	} else {
		s.Hostname = "czhax"
	}

	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		s.Kernel = strings.TrimSpace(string(data))
	}

	if model, err := os.ReadFile("/proc/device-tree/model"); err == nil {
		m := strings.ToLower(string(model))
		if strings.Contains(m, "raspberry pi") || strings.Contains(m, "compute module 0") || strings.Contains(m, "cardputer") {
			s.IsCardZero = true
		}
	}

	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if secs, err := strconv.ParseFloat(fields[0], 64); err == nil {
				s.Uptime = time.Duration(secs) * time.Second
			}
		}
	}

	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			s.Load1 = fields[0]
		}
	}

	s.MemUsedMB, s.MemTotalMB = readMemMB()
	s.DiskFreeGB, s.DiskTotGB = readDiskGB("/")
	s.TempC, s.HasTemp = readTempC()
	s.BatteryPct, s.Charging, s.HasBattery = readBattery()
	s.Interfaces = readInterfaces()

	for _, iface := range s.Interfaces {
		if iface.Name == "lo" || !iface.Up {
			continue
		}
		if len(iface.Addrs) > 0 {
			s.NetUp = true
			if iface.IsWifi {
				s.WifiUp = true
			}
			if s.PrimaryIP == "" {
				s.PrimaryIP = strings.Split(iface.Addrs[0], "/")[0]
			}
		}
	}

	return s
}

func readMemMB() (used uint64, total uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var totalKB, availKB uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalKB = val
		case "MemAvailable:":
			availKB = val
		}
	}
	if totalKB > 0 {
		total = totalKB / 1024
		if availKB <= totalKB {
			used = (totalKB - availKB) / 1024
		}
	}
	return used, total
}

func readDiskGB(path string) (free float64, total float64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	totBytes := float64(st.Blocks) * float64(st.Bsize)
	freeBytes := float64(st.Bavail) * float64(st.Bsize)
	const gb = 1024 * 1024 * 1024
	return freeBytes / gb, totBytes / gb
}

func readTempC() (float64, bool) {
	paths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/class/hwmon/hwmon0/temp1_input",
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		if v > 200 {
			v = v / 1000.0
		}
		return v, true
	}
	return 0, false
}

func readBattery() (pct int, charging bool, ok bool) {
	entries, err := os.ReadDir("/sys/class/power_supply")
	if err != nil {
		return 0, false, false
	}
	for _, e := range entries {
		base := filepath.Join("/sys/class/power_supply", e.Name())
		capBytes, err := os.ReadFile(filepath.Join(base, "capacity"))
		if err != nil {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(string(capBytes)))
		if err != nil {
			continue
		}
		if stBytes, err := os.ReadFile(filepath.Join(base, "status")); err == nil {
			st := strings.ToLower(strings.TrimSpace(string(stBytes)))
			charging = st == "charging" || st == "full"
		}
		return v, charging, true
	}
	return 0, false, false
}

func readInterfaces() []NetIface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]NetIface, 0, len(ifaces))
	for _, inf := range ifaces {
		ni := NetIface{
			Name:   inf.Name,
			Up:     inf.Flags&net.FlagUp != 0,
			MAC:    inf.HardwareAddr.String(),
			MTU:    inf.MTU,
			IsWifi: strings.HasPrefix(inf.Name, "wl") || strings.HasPrefix(inf.Name, "wlan"),
		}
		if addrs, err := inf.Addrs(); err == nil {
			for _, a := range addrs {
				ni.Addrs = append(ni.Addrs, a.String())
			}
		}
		out = append(out, ni)
	}
	return out
}

func FormatUptime(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h >= 24 {
		return fmt.Sprintf("%dd%dh", h/24, h%24)
	}
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
