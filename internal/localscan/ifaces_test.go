package localscan

import (
	"context"
	"errors"
	"net"
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

func TestCollectInterfacesWiFiEthUSB(t *testing.T) {
	root := t.TempDir()
	netDir := filepath.Join(root, "sys", "class", "net")
	devDir := filepath.Join(root, "sys", "devices")

	// Loopback and docker must be excluded.
	writeFile(t, filepath.Join(netDir, "lo", "type"), "772\n")
	writeFile(t, filepath.Join(netDir, "docker0", "type"), "1\n")
	writeFile(t, filepath.Join(netDir, "docker0", "bridge", ".keep"), "")

	// wlan0: connected WiFi
	w0Dev := filepath.Join(devDir, "mmc0:0001")
	writeFile(t, filepath.Join(w0Dev, ".keep"), "")
	w0 := filepath.Join(netDir, "wlan0")
	writeFile(t, filepath.Join(w0, "wireless", ".keep"), "")
	writeFile(t, filepath.Join(w0, "type"), "1\n")
	writeFile(t, filepath.Join(w0, "flags"), "0x1003\n")
	writeFile(t, filepath.Join(w0, "operstate"), "up\n")
	writeFile(t, filepath.Join(w0, "carrier"), "1\n")
	writeFile(t, filepath.Join(w0, "address"), "b8:27:eb:aa:bb:cc\n")
	if err := os.Symlink(w0Dev, filepath.Join(w0, "device")); err != nil {
		t.Fatal(err)
	}

	// wlan1: disconnected WiFi (greyed out, unselectable)
	w1Dev := filepath.Join(devDir, "usb1", "1-1", "1-1:1.0")
	writeFile(t, filepath.Join(w1Dev, ".keep"), "")
	w1 := filepath.Join(netDir, "wlan1")
	writeFile(t, filepath.Join(w1, "uevent"), "DEVTYPE=wlan\n")
	writeFile(t, filepath.Join(w1, "type"), "1\n")
	writeFile(t, filepath.Join(w1, "flags"), "0x1003\n")
	writeFile(t, filepath.Join(w1, "operstate"), "down\n")
	writeFile(t, filepath.Join(w1, "carrier"), "0\n")
	if err := os.Symlink(w1Dev, filepath.Join(w1, "device")); err != nil {
		t.Fatal(err)
	}

	// eth0: connected Ethernet
	e0Dev := filepath.Join(devDir, "platform", "eth0")
	writeFile(t, filepath.Join(e0Dev, ".keep"), "")
	e0 := filepath.Join(netDir, "eth0")
	writeFile(t, filepath.Join(e0, "type"), "1\n")
	writeFile(t, filepath.Join(e0, "flags"), "0x1003\n")
	writeFile(t, filepath.Join(e0, "operstate"), "up\n")
	writeFile(t, filepath.Join(e0, "carrier"), "1\n")
	if err := os.Symlink(e0Dev, filepath.Join(e0, "device")); err != nil {
		t.Fatal(err)
	}

	// usb0: USB tethering with operstate=unknown, carrier=1, rndis_host driver
	usbParent := filepath.Join(devDir, "usb1", "1-2")
	usbIface := filepath.Join(usbParent, "1-2:1.0")
	if err := os.MkdirAll(usbIface, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(usbParent, "idVendor"), "18d1\n")
	writeFile(t, filepath.Join(usbParent, "product"), "Pixel 8\n")
	writeFile(t, filepath.Join(devDir, "drivers", "rndis_host", ".keep"), "")
	writeFile(t, filepath.Join(devDir, "bus", "usb", ".keep"), "")
	if err := os.Symlink(filepath.Join(devDir, "drivers", "rndis_host"), filepath.Join(usbIface, "driver")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(devDir, "bus", "usb"), filepath.Join(usbIface, "subsystem")); err != nil {
		t.Fatal(err)
	}
	u0 := filepath.Join(netDir, "usb0")
	writeFile(t, filepath.Join(u0, "type"), "1\n")
	writeFile(t, filepath.Join(u0, "flags"), "0x1003\n")
	writeFile(t, filepath.Join(u0, "operstate"), "unknown\n")
	writeFile(t, filepath.Join(u0, "carrier"), "1\n")
	if err := os.Symlink(usbIface, filepath.Join(u0, "device")); err != nil {
		t.Fatal(err)
	}

	// enx001122: iPhone USB tether via ipheth driver, unplugged (carrier=0)
	ipParent := filepath.Join(devDir, "usb1", "1-3")
	ipIface := filepath.Join(ipParent, "1-3:4.2")
	if err := os.MkdirAll(ipIface, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(ipParent, "idVendor"), "05ac\n")
	writeFile(t, filepath.Join(ipParent, "product"), "iPhone\n")
	writeFile(t, filepath.Join(devDir, "drivers", "ipheth", ".keep"), "")
	if err := os.Symlink(filepath.Join(devDir, "drivers", "ipheth"), filepath.Join(ipIface, "driver")); err != nil {
		t.Fatal(err)
	}
	enx := filepath.Join(netDir, "enx001122")
	writeFile(t, filepath.Join(enx, "type"), "1\n")
	writeFile(t, filepath.Join(enx, "flags"), "0x1003\n")
	writeFile(t, filepath.Join(enx, "operstate"), "down\n")
	writeFile(t, filepath.Join(enx, "carrier"), "0\n")
	if err := os.Symlink(ipIface, filepath.Join(enx, "device")); err != nil {
		t.Fatal(err)
	}

	c := collector{
		netClassDir: netDir,
		lookupNet: func(name string) ([]string, net.Flags, string, bool) {
			switch name {
			case "wlan0":
				return []string{"192.168.1.42/24"}, net.FlagUp | net.FlagMulticast, "b8:27:eb:aa:bb:cc", true
			case "wlan1":
				return nil, net.FlagUp, "00:c0:ca:11:22:33", true
			case "eth0":
				return []string{"10.0.0.15/8"}, net.FlagUp, "dc:a6:32:00:00:01", true
			case "usb0":
				return []string{"192.168.42.129/24"}, net.FlagUp, "02:11:22:33:44:55", true
			case "enx001122":
				return nil, net.FlagUp, "02:aa:bb:cc:dd:ee", true
			}
			return nil, 0, "", false
		},
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name == "nmcli" {
				return "wlan0:connected:HomeWiFi\neth0:connected:LabSwitch\nusb0:connected:Wired connection 1\n", nil
			}
			if name == "iw" && len(args) >= 3 && args[1] == "wlan0" {
				return "Connected to aa:bb:cc:dd:ee:ff (on wlan0)\n\tSSID: HomeWiFi\n", nil
			}
			return "", errors.New("unsupported")
		},
	}

	got := collectInterfacesWith(c)
	byName := make(map[string]Interface)
	for _, inf := range got {
		byName[inf.Name] = inf
	}

	if _, bad := byName["lo"]; bad {
		t.Fatal("lo must be excluded")
	}
	if _, bad := byName["docker0"]; bad {
		t.Fatal("docker0 bridge must be excluded")
	}

	w0Inf := byName["wlan0"]
	if !w0Inf.Connected || w0Inf.Kind != KindWiFi || w0Inf.NetworkName != "HomeWiFi" || w0Inf.ScanTarget != "192.168.1.0/24" {
		t.Fatalf("unexpected wlan0: %+v", w0Inf)
	}

	w1Inf := byName["wlan1"]
	if w1Inf.Connected || w1Inf.Kind != KindWiFi {
		t.Fatalf("expected wlan1 disconnected, got %+v", w1Inf)
	}

	e0Inf := byName["eth0"]
	if !e0Inf.Connected || e0Inf.Kind != KindEthernet || e0Inf.NetworkName != "LabSwitch" || e0Inf.ScanTarget != "10.0.0.0/24" {
		t.Fatalf("unexpected eth0 (expected /8 clamped to /24): %+v", e0Inf)
	}

	u0Inf := byName["usb0"]
	if !u0Inf.Connected || u0Inf.Kind != KindUSB || u0Inf.ScanTarget != "192.168.42.0/24" || u0Inf.NetworkName != "USB · Pixel 8" {
		t.Fatalf("unexpected usb0: %+v", u0Inf)
	}

	enxInf := byName["enx001122"]
	if enxInf.Connected || enxInf.Kind != KindUSB {
		t.Fatalf("expected unplugged ipheth disconnected USB, got %+v", enxInf)
	}
}
