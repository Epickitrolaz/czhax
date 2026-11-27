package sdr

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// knownIDs lists the USB VID:PID pairs supported by librtlsdr (RTL2832U + tuners).
var knownIDs = map[string]string{
	"0bda:2832": "Generic RTL2832U",
	"0bda:2838": "RTL2832U OEM",
	"0413:6680": "Leadtek WinFast DTV Dongle mini",
	"0413:6f0f": "Leadtek WinFast DTV Dongle mini D",
	"0458:707f": "Genius TVGo DVB-T03",
	"0ccd:00a9": "Terratec Cinergy T Stick Black",
	"0ccd:00b3": "Terratec NOXON DAB Stick",
	"0ccd:00b4": "Terratec Deutschlandradio DAB Stick",
	"0ccd:00b5": "Terratec NOXON DAB Stick (rev 2)",
	"0ccd:00b7": "Terratec Media Broadcast DAB Stick",
	"0ccd:00b8": "Terratec BR DAB Stick",
	"0ccd:00b9": "Terratec WDR DAB Stick",
	"0ccd:00c0": "Terratec MuellerVerlag DAB Stick",
	"0ccd:00c6": "Terratec Fraunhofer DAB Stick",
	"0ccd:00d3": "Terratec Cinergy T Stick RC (Rev.3)",
	"0ccd:00d7": "Terratec T Stick PLUS",
	"0ccd:00e0": "Terratec NOXON DAB Stick (rev 3)",
	"15f4:0131": "Astrometa DVB-T/T2/C",
	"15f4:0133": "HanfTek DAB+FM+DVB-T",
	"185b:0620": "Compro Videomate U620F",
	"185b:0650": "Compro Videomate U650F",
	"185b:0680": "Compro Videomate U680F",
	"1b80:d393": "GIGABYTE GT-U7300",
	"1b80:d394": "DIKOM USB-DVBT HD",
	"1b80:d395": "Peak 102569AGPK",
	"1b80:d397": "KWorld KW-UB450-T",
	"1b80:d398": "Zaapa ZT-MINDVBZP",
	"1b80:d39d": "SVEON STV20",
	"1b80:d3a4": "Twintech UT-40",
	"1b80:d3a8": "ASUS U3100MINI_PLUS_V2",
	"1b80:d3af": "SVEON STV27",
	"1b80:d3b0": "SVEON STV21",
	"1d19:1101": "Dexatek DK DVB-T Dongle",
	"1d19:1102": "Dexatek DK DVB-T Dongle (Logilink VG0002A)",
	"1d19:1103": "Dexatek Technology Ltd. DK 5217 DVB-T Dongle",
	"1d19:1104": "MSI DigiVox Micro HD",
	"1f4d:b803": "GTek T803",
	"1f4d:c803": "Lifeview LV5TDeluxe",
	"1f4d:d286": "MyGica TD312",
	"1f4d:d803": "PROlectrix DV107669",
}

// USBDevice describes a detected RTL-SDR USB dongle.
type USBDevice struct {
	Index        int
	VendorID     string
	ProductID    string
	Manufacturer string
	Product      string
	Serial       string
	BusNum       string
	DevNum       string
	Tuner        string
	DVBDriver    bool
	ClaimErr     string
}

// Label returns a concise single-line label for small Cardputer screens.
func (d USBDevice) Label() string {
	name := strings.TrimSpace(d.Product)
	if name == "" {
		if known, ok := knownIDs[d.VendorID+":"+d.ProductID]; ok {
			name = known
		} else {
			name = "RTL-SDR"
		}
	}
	// Strip verbose Realtek boilerplate so the tuner chip remains visible.
	name = strings.TrimSpace(strings.TrimPrefix(name, "Realtek"))
	name = strings.TrimSpace(strings.TrimPrefix(name, "Generic"))
	if name == "" {
		name = "RTL2838"
	}
	if d.Tuner != "" {
		return name + " · " + shortTuner(d.Tuner)
	}
	return name
}

func shortTuner(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "Rafael Micro ")
	t = strings.TrimPrefix(t, "Elonics ")
	t = strings.TrimPrefix(t, "Fitipower ")
	t = strings.TrimPrefix(t, "Maxim ")
	return t
}

// Tools holds resolved paths to RTL-SDR CLI utilities and audio output players.
type Tools struct {
	RTLFM    string
	RTLSDR   string
	RTLPower string
	RTLTest  string
	RTL433   string
	Player   string
}

// ProbeStatus summarizes connected hardware and installed CLI dependencies.
type ProbeStatus struct {
	Devices []USBDevice
	Tools   Tools
	Err     string
}

// FindTool locates an executable on PATH or standard system sbin/bin directories.
func FindTool(name string) string {
	if strings.ContainsRune(name, '/') {
		if st, err := os.Stat(name); err == nil && !st.IsDir() {
			return name
		}
		return ""
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/usr/bin", "/bin", "/usr/local/bin", "/usr/sbin", "/sbin", "/usr/local/sbin"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// DiscoverTools finds installed RTL-SDR utilities and audio playback binaries.
func DiscoverTools() Tools {
	t := Tools{
		RTLFM:    FindTool("rtl_fm"),
		RTLSDR:   FindTool("rtl_sdr"),
		RTLPower: FindTool("rtl_power"),
		RTLTest:  FindTool("rtl_test"),
		RTL433:   FindTool("rtl_433"),
	}
	for _, p := range []string{"aplay", "pw-play", "ffplay", "mpv"} {
		if path := FindTool(p); path != "" {
			t.Player = path
			break
		}
	}
	return t
}

// IsKnownRTLVIDPID reports whether vid:pid matches a known RTL2832U SDR dongle.
func IsKnownRTLVIDPID(vid, pid string) bool {
	key := strings.ToLower(strings.TrimSpace(vid)) + ":" + strings.ToLower(strings.TrimSpace(pid))
	_, ok := knownIDs[key]
	return ok
}

// DiscoverUSBDevices scans /sys/bus/usb/devices for connected RTL-SDR dongles.
func DiscoverUSBDevices() []USBDevice {
	return DiscoverUSBDevicesFrom("/sys/bus/usb/devices")
}

// DiscoverUSBDevicesFrom scans a sysfs USB device directory for RTL-SDR dongles.
func DiscoverUSBDevicesFrom(sysUSBDir string) []USBDevice {
	entries, err := os.ReadDir(sysUSBDir)
	if err != nil {
		return nil
	}
	var devs []USBDevice
	for _, e := range entries {
		name := e.Name()
		// Skip USB interface entries like "1-1:1.0" and root hubs when scanning devices.
		if strings.Contains(name, ":") {
			continue
		}
		dir := filepath.Join(sysUSBDir, name)
		vid := strings.ToLower(readTrim(filepath.Join(dir, "idVendor")))
		pid := strings.ToLower(readTrim(filepath.Join(dir, "idProduct")))
		if !IsKnownRTLVIDPID(vid, pid) {
			continue
		}
		d := USBDevice{
			Index:        len(devs),
			VendorID:     vid,
			ProductID:    pid,
			Manufacturer: readTrim(filepath.Join(dir, "manufacturer")),
			Product:      readTrim(filepath.Join(dir, "product")),
			Serial:       readTrim(filepath.Join(dir, "serial")),
			BusNum:       readTrim(filepath.Join(dir, "busnum")),
			DevNum:       readTrim(filepath.Join(dir, "devnum")),
			DVBDriver:    hasDVBDriver(sysUSBDir, name),
		}
		devs = append(devs, d)
	}
	sort.Slice(devs, func(i, j int) bool {
		if devs[i].BusNum != devs[j].BusNum {
			return devs[i].BusNum < devs[j].BusNum
		}
		return devs[i].DevNum < devs[j].DevNum
	})
	for i := range devs {
		devs[i].Index = i
	}
	return devs
}

func hasDVBDriver(sysUSBDir, devName string) bool {
	matches, err := filepath.Glob(filepath.Join(sysUSBDir, devName+":*", "driver"))
	if err != nil {
		return false
	}
	for _, m := range matches {
		if target, err := os.Readlink(m); err == nil {
			base := filepath.Base(target)
			if strings.Contains(base, "dvb_usb_rtl28xxu") || strings.Contains(base, "rtl283") {
				return true
			}
		}
	}
	return false
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// RTLTestEntry represents one device line parsed from rtl_test output.
type RTLTestEntry struct {
	Index        int
	Manufacturer string
	Product      string
	Serial       string
	Tuner        string
	Err          string
}

// ParseRTLTestOutput extracts device list, tuner model, and claim errors from rtl_test.
func ParseRTLTestOutput(out string) []RTLTestEntry {
	var entries []RTLTestEntry
	var activeIdx = -1
	var lastErr string

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// e.g. "  0:  Realtek, RTL2838UHIDIR, SN: 00000001"
		if idx, rest, ok := parseDeviceIndexLine(line); ok {
			parts := strings.Split(rest, ",")
			ent := RTLTestEntry{Index: idx}
			if len(parts) >= 1 {
				ent.Manufacturer = strings.TrimSpace(parts[0])
			}
			if len(parts) >= 2 {
				ent.Product = strings.TrimSpace(parts[1])
			}
			if len(parts) >= 3 {
				sn := strings.TrimSpace(parts[2])
				sn = strings.TrimPrefix(sn, "SN:")
				ent.Serial = strings.TrimSpace(sn)
			}
			entries = append(entries, ent)
			continue
		}
		if strings.HasPrefix(line, "Using device ") {
			numStr := strings.TrimPrefix(line, "Using device ")
			if colon := strings.IndexByte(numStr, ':'); colon >= 0 {
				numStr = numStr[:colon]
			}
			if n, err := strconv.Atoi(strings.TrimSpace(numStr)); err == nil {
				activeIdx = n
			}
			continue
		}
		if strings.HasPrefix(line, "Found ") && strings.HasSuffix(line, " tuner") {
			tuner := strings.TrimPrefix(line, "Found ")
			tuner = strings.TrimSuffix(tuner, " tuner")
			tuner = strings.TrimSpace(tuner)
			for i := range entries {
				if entries[i].Index == activeIdx || (activeIdx < 0 && i == 0) {
					entries[i].Tuner = tuner
				}
			}
			continue
		}
		if strings.Contains(line, "usb_claim_interface error") {
			if strings.Contains(line, "-6") {
				lastErr = "USB busy (DVB driver?)"
			} else if strings.Contains(line, "-3") {
				lastErr = "USB permission denied"
			} else {
				lastErr = line
			}
		} else if strings.Contains(line, "usb_open error") {
			if strings.Contains(line, "-3") {
				lastErr = "USB permission denied"
			} else {
				lastErr = line
			}
		}
	}
	if lastErr != "" && len(entries) > 0 {
		for i := range entries {
			if entries[i].Index == activeIdx || (activeIdx < 0 && i == 0) {
				entries[i].Err = lastErr
			}
		}
	}
	return entries
}

func parseDeviceIndexLine(line string) (int, string, bool) {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 || colon > 3 {
		return 0, "", false
	}
	idx, err := strconv.Atoi(strings.TrimSpace(line[:colon]))
	if err != nil || idx < 0 {
		return 0, "", false
	}
	rest := strings.TrimSpace(line[colon+1:])
	if rest == "" || strings.HasPrefix(rest, "//") {
		return 0, "", false
	}
	return idx, rest, true
}

var usbSem = make(chan struct{}, 1)

// acquireUSB serializes exclusive librtlsdr USB access across rtl_test,
// rtl_fm, rtl_sdr, rtl_power, and rtl_433 so fast retuning or tab switching
// waits for the prior process to release the USB interface.
func acquireUSB(ctx context.Context) (func(), error) {
	select {
	case usbSem <- struct{}{}:
		return func() { <-usbSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Probe discovers connected RTL-SDR devices and installed CLI tooling.
// Set skipRTLTest when an SDR capture is already streaming to avoid USB contention.
func Probe(ctx context.Context, skipRTLTest bool) ProbeStatus {
	tools := DiscoverTools()
	devs := DiscoverUSBDevices()
	st := ProbeStatus{
		Devices: devs,
		Tools:   tools,
	}

	if !skipRTLTest && tools.RTLTest != "" && len(devs) > 0 {
		tctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if rel, err := acquireUSB(tctx); err == nil {
			cmd := exec.CommandContext(tctx, tools.RTLTest, "-t")
			out, _ := cmd.CombinedOutput()
			rel()
			parsed := ParseRTLTestOutput(string(out))
			for _, p := range parsed {
				matched := false
				for i := range st.Devices {
					if st.Devices[i].Index == p.Index {
						if p.Tuner != "" {
							st.Devices[i].Tuner = p.Tuner
						}
						if p.Product != "" && st.Devices[i].Product == "" {
							st.Devices[i].Product = p.Product
						}
						if p.Serial != "" && st.Devices[i].Serial == "" {
							st.Devices[i].Serial = p.Serial
						}
						if p.Err != "" {
							st.Devices[i].ClaimErr = p.Err
						}
						matched = true
						break
					}
				}
				if !matched {
					st.Devices = append(st.Devices, USBDevice{
						Index:        p.Index,
						Manufacturer: p.Manufacturer,
						Product:      p.Product,
						Serial:       p.Serial,
						Tuner:        p.Tuner,
						ClaimErr:     p.Err,
					})
				}
			}
		}
	}

	if len(st.Devices) == 0 {
		st.Err = "No RTL-SDR USB dongle detected"
	} else if st.Devices[0].ClaimErr != "" {
		st.Err = st.Devices[0].ClaimErr
	} else if st.Devices[0].DVBDriver {
		st.Err = "DVB kernel driver bound (sudo modprobe -r dvb_usb_rtl28xxu)"
	} else if tools.RTLFM == "" && tools.RTLSDR == "" {
		st.Err = "rtl-sdr tools missing (apt install rtl-sdr)"
	}

	return st
}

// DetachDVBDriverIfNeeded best-effort unloads dvb_usb_rtl28xxu when sysfs
// shows it claiming an RTL-SDR dongle and passwordless sudo is available.
func DetachDVBDriverIfNeeded(devs []USBDevice) {
	need := false
	for _, d := range devs {
		if d.DVBDriver {
			need = true
			break
		}
	}
	if !need {
		return
	}
	if modprobe := FindTool("modprobe"); modprobe != "" {
		if os.Geteuid() == 0 {
			_ = exec.Command(modprobe, "-r", "dvb_usb_rtl28xxu").Run()
			return
		}
		_ = exec.Command("sudo", "-n", modprobe, "-r", "dvb_usb_rtl28xxu").Run()
	}
}
