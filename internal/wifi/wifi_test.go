package wifi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectWithSysfsAndIW(t *testing.T) {
	root := t.TempDir()
	netDir := filepath.Join(root, "sys", "class", "net")
	devDir := filepath.Join(root, "sys", "devices")

	// Non-wifi interfaces (must be ignored)
	writeFile(t, filepath.Join(netDir, "lo", "type"), "772\n")
	writeFile(t, filepath.Join(netDir, "eth0", "type"), "1\n")
	writeFile(t, filepath.Join(netDir, "eth0", "address"), "00:11:22:33:44:55\n")

	// wlan0: onboard SDIO Broadcom
	w0Dev := filepath.Join(devDir, "mmc0", "mmc0:0001", "mmc0:0001:1")
	writeFile(t, filepath.Join(w0Dev, "vendor"), "0x02d0\n")
	writeFile(t, filepath.Join(w0Dev, "device"), "0xa9a6\n")
	writeFile(t, filepath.Join(devDir, "drivers", "brcmfmac", ".keep"), "")
	writeFile(t, filepath.Join(devDir, "bus", "sdio", ".keep"), "")
	if err := os.Symlink(filepath.Join(devDir, "drivers", "brcmfmac"), filepath.Join(w0Dev, "driver")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(devDir, "bus", "sdio"), filepath.Join(w0Dev, "subsystem")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(devDir, "ieee80211", "phy0", ".keep"), "")

	w0 := filepath.Join(netDir, "wlan0")
	writeFile(t, filepath.Join(w0, "wireless", ".keep"), "")
	writeFile(t, filepath.Join(w0, "address"), "b8:27:eb:aa:bb:cc\n")
	writeFile(t, filepath.Join(w0, "operstate"), "up\n")
	writeFile(t, filepath.Join(w0, "flags"), "0x1003\n")
	writeFile(t, filepath.Join(w0, "mtu"), "1500\n")
	writeFile(t, filepath.Join(w0, "type"), "1\n")
	if err := os.Symlink(w0Dev, filepath.Join(w0, "device")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(devDir, "ieee80211", "phy0"), filepath.Join(w0, "phy80211")); err != nil {
		t.Fatal(err)
	}

	// wlan1: external USB Realtek adapter in monitor mode
	w1UsbParent := filepath.Join(devDir, "usb1", "1-1")
	w1UsbIface := filepath.Join(w1UsbParent, "1-1:1.0")
	if err := os.MkdirAll(w1UsbIface, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(w1UsbParent, "idVendor"), "0bda\n")
	writeFile(t, filepath.Join(w1UsbParent, "idProduct"), "8812\n")
	writeFile(t, filepath.Join(w1UsbParent, "manufacturer"), "Realtek\n")
	writeFile(t, filepath.Join(w1UsbParent, "product"), "802.11ac NIC\n")
	writeFile(t, filepath.Join(devDir, "drivers", "rtl88XXau", ".keep"), "")
	writeFile(t, filepath.Join(devDir, "bus", "usb", ".keep"), "")
	if err := os.Symlink(filepath.Join(devDir, "drivers", "rtl88XXau"), filepath.Join(w1UsbIface, "driver")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(devDir, "bus", "usb"), filepath.Join(w1UsbIface, "subsystem")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(devDir, "ieee80211", "phy1", ".keep"), "")

	w1 := filepath.Join(netDir, "wlan1")
	writeFile(t, filepath.Join(w1, "uevent"), "DEVTYPE=wlan\nINTERFACE=wlan1\n")
	writeFile(t, filepath.Join(w1, "address"), "00:c0:ca:11:22:33\n")
	writeFile(t, filepath.Join(w1, "operstate"), "down\n")
	writeFile(t, filepath.Join(w1, "flags"), "0x1002\n")
	writeFile(t, filepath.Join(w1, "mtu"), "1500\n")
	writeFile(t, filepath.Join(w1, "type"), "803\n")
	if err := os.Symlink(w1UsbIface, filepath.Join(w1, "device")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(devDir, "ieee80211", "phy1"), filepath.Join(w1, "phy80211")); err != nil {
		t.Fatal(err)
	}

	c := collector{
		netClassDir: netDir,
		lookupNet: func(name string) ([]string, int, bool, string) {
			if name == "wlan0" {
				return []string{"192.168.1.42/24"}, 1500, true, "b8:27:eb:aa:bb:cc"
			}
			return nil, 1500, false, "00:c0:ca:11:22:33"
		},
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name != "iw" {
				return "", errors.New("not found")
			}
			key := args[0] + " " + args[1]
			switch key {
			case "dev wlan0":
				return "Interface wlan0\n\twiphy 0\n\tssid LabNet\n\ttype managed\n\tchannel 6 (2437 MHz), width: 20 MHz\n\ttxpower 20.00 dBm\n", nil
			case "dev wlan1":
				return "Interface wlan1\n\twiphy 1\n\ttype monitor\n\tchannel 36 (5180 MHz), width: 20 MHz\n\ttxpower 18.00 dBm\n", nil
			case "phy phy0":
				return "Wiphy phy0\n\tSupported interface modes:\n\t\t * IBSS\n\t\t * managed\n\t\t * AP\n\tBand 1:\n\t\tFrequencies:\n\t\t\t* 2412 MHz [1]\n", nil
			case "phy phy1":
				return "Wiphy phy1\n\tSupported interface modes:\n\t\t * managed\n\t\t * monitor\n\tBand 1:\n\t\tFrequencies:\n\t\t\t* 2412 MHz [1]\n\tBand 2:\n\t\tFrequencies:\n\t\t\t* 5180 MHz [36]\n", nil
			}
			return "", errors.New("unknown")
		},
	}

	got := collectWith(c)
	if len(got) != 2 {
		t.Fatalf("expected 2 wifi interfaces, got %d: %+v", len(got), got)
	}

	g0 := got[0]
	if g0.Name != "wlan0" || !g0.Up || g0.Mode != "managed" || g0.Manufacturer != "Broadcom" || g0.Driver != "brcmfmac" || g0.Bus != "sdio" || g0.Phy != "phy0" {
		t.Fatalf("unexpected wlan0: %+v", g0)
	}
	if g0.SSID != "LabNet" || g0.Channel != "ch 6 (2437 MHz)" || g0.MonitorCapable || !g0.HasMonCap || g0.Bands != "2.4G" {
		t.Fatalf("unexpected wlan0 iw details: %+v", g0)
	}

	g1 := got[1]
	if g1.Name != "wlan1" || g1.Up || g1.Mode != "monitor" || g1.Manufacturer != "Realtek" || g1.Product != "802.11ac NIC" || g1.Driver != "rtl88XXau" || g1.Bus != "usb" {
		t.Fatalf("unexpected wlan1: %+v", g1)
	}
	if !g1.MonitorCapable || !g1.HasMonCap || g1.Bands != "2.4G/5G" || g1.VendorID != "0bda" || g1.ProductID != "8812" {
		t.Fatalf("unexpected wlan1 caps/ids: %+v", g1)
	}
}

func TestCollectSysfsOnlyAndIWConfigFallback(t *testing.T) {
	root := t.TempDir()
	netDir := filepath.Join(root, "net")

	w0 := filepath.Join(netDir, "wlan0")
	writeFile(t, filepath.Join(w0, "wireless", ".keep"), "")
	writeFile(t, filepath.Join(w0, "type"), "1\n")
	writeFile(t, filepath.Join(w0, "flags"), "0x1003\n")
	writeFile(t, filepath.Join(w0, "device", "modalias"), "usb:v148Fp5370d0101dc00dsc00dp00icFFiscFFipFFin00\n")

	w1 := filepath.Join(netDir, "wlan10")
	writeFile(t, filepath.Join(w1, "phy80211", ".keep"), "")
	writeFile(t, filepath.Join(w1, "type"), "803\n") // ARPHRD_IEEE80211_RADIOTAP -> monitor without iw
	writeFile(t, filepath.Join(w1, "flags"), "0x1002\n")

	w2 := filepath.Join(netDir, "wlan2")
	writeFile(t, filepath.Join(w2, "phy80211", ".keep"), "")
	writeFile(t, filepath.Join(w2, "type"), "1\n")

	c := collector{
		netClassDir: netDir,
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name == "iwconfig" && len(args) == 1 && args[0] == "wlan0" {
				return `wlan0     IEEE 802.11  ESSID:"TestNet"  Mode:Managed  Frequency:2.437 GHz  Tx-Power=20 dBm`, nil
			}
			return "", errors.New("unavailable")
		},
	}

	got := collectWith(c)
	if len(got) != 3 {
		t.Fatalf("expected 3 interfaces, got %d", len(got))
	}
	if got[0].Name != "wlan0" || got[1].Name != "wlan2" || got[2].Name != "wlan10" {
		t.Fatalf("unexpected natural sort order: %s, %s, %s", got[0].Name, got[1].Name, got[2].Name)
	}
	if got[0].Manufacturer != "Ralink" || got[0].SSID != "TestNet" || got[0].Mode != "managed" {
		t.Fatalf("unexpected wlan0 iwconfig/modalias parse: %+v", got[0])
	}
	if got[2].Mode != "monitor" {
		t.Fatalf("expected radiotap type 803 to default to monitor mode, got %q", got[2].Mode)
	}
}
