package bluetooth

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

func TestCollectWithSysfsAndTools(t *testing.T) {
	root := t.TempDir()
	btDir := filepath.Join(root, "sys", "class", "bluetooth")
	rfDir := filepath.Join(root, "sys", "class", "rfkill")
	devDir := filepath.Join(root, "sys", "devices")

	// Connection node (must be ignored)
	writeFile(t, filepath.Join(btDir, "hci0:11", "type"), "ACL\n")

	// hci0: onboard Broadcom UART
	h0Dev := filepath.Join(devDir, "serial0", "serial0-0")
	writeFile(t, filepath.Join(h0Dev, "of_node", "compatible"), "brcm,bcm43438-bt\x00")
	writeFile(t, filepath.Join(devDir, "drivers", "hci_uart", ".keep"), "")
	if err := os.Symlink(filepath.Join(devDir, "drivers", "hci_uart"), filepath.Join(h0Dev, "driver")); err != nil {
		t.Fatal(err)
	}

	h0 := filepath.Join(btDir, "hci0")
	writeFile(t, filepath.Join(h0, "address"), "b8:27:eb:11:22:33\n")
	writeFile(t, filepath.Join(h0, "type"), "BR/EDR\n")
	writeFile(t, filepath.Join(h0, "bus"), "UART\n")
	writeFile(t, filepath.Join(h0, "rfkill0", "state"), "1\n")
	if err := os.Symlink(h0Dev, filepath.Join(h0, "device")); err != nil {
		t.Fatal(err)
	}

	// hci1: USB dongle soft-blocked via rfkill dir
	h1UsbParent := filepath.Join(devDir, "usb1", "1-2")
	h1UsbIface := filepath.Join(h1UsbParent, "1-2:1.0")
	if err := os.MkdirAll(h1UsbIface, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h1UsbParent, "idVendor"), "0bda\n")
	writeFile(t, filepath.Join(h1UsbParent, "idProduct"), "8771\n")
	writeFile(t, filepath.Join(h1UsbParent, "manufacturer"), "Realtek\n")
	writeFile(t, filepath.Join(h1UsbParent, "product"), "Bluetooth 5.0 Radio\n")
	writeFile(t, filepath.Join(devDir, "drivers", "btusb", ".keep"), "")
	if err := os.Symlink(filepath.Join(devDir, "drivers", "btusb"), filepath.Join(h1UsbIface, "driver")); err != nil {
		t.Fatal(err)
	}

	h1 := filepath.Join(btDir, "hci1")
	writeFile(t, filepath.Join(h1, "address"), "00:e0:4c:aa:bb:cc\n")
	writeFile(t, filepath.Join(h1, "type"), "BR/EDR\n")
	writeFile(t, filepath.Join(h1, "bus"), "USB\n")
	if err := os.Symlink(h1UsbIface, filepath.Join(h1, "device")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(rfDir, "rfkill1", "name"), "hci1\n")
	writeFile(t, filepath.Join(rfDir, "rfkill1", "soft"), "1\n")

	c := collector{
		btClassDir: btDir,
		rfkillDir:  rfDir,
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name == "hciconfig" && len(args) == 2 && args[1] == "hci0" {
				return `hci0:	Type: Primary  Bus: UART
	BD Address: B8:27:EB:11:22:33  ACL MTU: 1021:8  SCO MTU: 64:1
	UP RUNNING PSCAN
	Name: 'cardzero'
	HCI Version: 5.0 (0x9)  Revision: 0x0
	LMP Version: 5.0 (0x9)  Subversion: 0x6119
	Manufacturer: Broadcom Corporation (15)
	Features: 0xbf 0xfe 0xcf 0xfe 0xdb 0xff 0x7b 0x87
		<3-slot packets> <LE Supported (Controller)>`, nil
			}
			if name == "bluetoothctl" && len(args) == 2 && args[0] == "show" && args[1] == "B8:27:EB:11:22:33" {
				return `Controller B8:27:EB:11:22:33 (public)
	Name: cardzero
	Alias: cardzero
	Powered: yes
	Discoverable: no
	Pairable: yes`, nil
			}
			return "", errors.New("unavailable")
		},
	}

	got := collectWith(c)
	if len(got) != 2 {
		t.Fatalf("expected 2 adapters, got %d: %+v", len(got), got)
	}

	g0 := got[0]
	if g0.Name != "hci0" || !g0.Up || !g0.Powered || g0.Alias != "cardzero" || g0.Manufacturer != "Broadcom" || g0.Driver != "hci_uart" || g0.Bus != "UART" {
		t.Fatalf("unexpected hci0: %+v", g0)
	}
	if g0.HCIVersion != "5.0" || g0.Mode != "BR/EDR + LE" || g0.RFKill != "unblocked" || !g0.Pairable || g0.Discoverable {
		t.Fatalf("unexpected hci0 details: %+v", g0)
	}

	g1 := got[1]
	if g1.Name != "hci1" || g1.Up || g1.RFKill != "soft-blocked" || g1.Manufacturer != "Realtek" || g1.Product != "Bluetooth 5.0 Radio" || g1.Bus != "USB" || g1.Driver != "btusb" {
		t.Fatalf("unexpected hci1: %+v", g1)
	}
	if g1.VendorID != "0bda" || g1.ProductID != "8771" {
		t.Fatalf("unexpected hci1 ids: %+v", g1)
	}
}

func TestCollectBluetoothctlFallbackAndSort(t *testing.T) {
	c := collector{
		btClassDir: filepath.Join(t.TempDir(), "missing"),
		rfkillDir:  filepath.Join(t.TempDir(), "missing-rf"),
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			if name == "bluetoothctl" && len(args) == 1 && args[0] == "list" {
				return "Controller AA:BB:CC:DD:EE:01 cz-bt [default]\nController AA:BB:CC:DD:EE:02 usb-bt\n", nil
			}
			if name == "bluetoothctl" && len(args) == 2 && args[0] == "show" {
				return "Name: cz-bt\nPowered: yes\nDiscoverable: yes\nPairable: no\n", nil
			}
			if name == "hciconfig" {
				return "", errors.New("no hciconfig")
			}
			return "", errors.New("unknown")
		},
	}

	got := collectWith(c)
	if len(got) != 2 {
		t.Fatalf("expected 2 fallback adapters, got %d", len(got))
	}
	if got[0].Name != "hci0" || !got[0].Powered || !got[0].Discoverable || got[0].Pairable {
		t.Fatalf("unexpected fallback adapter: %+v", got[0])
	}
}
