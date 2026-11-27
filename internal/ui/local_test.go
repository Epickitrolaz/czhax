package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/localscan"
)

func newLocalTestModel(t *testing.T, w, h int) AppModel {
	t.Helper()
	cfg := config.Default()
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.4.0")
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(AppModel)
	m.ActiveView = ViewLocal
	m.Focus = FocusContent
	m.Local.NmapPath = "/usr/bin/nmap"
	return m
}

func sampleLocalIfaces() []localscan.Interface {
	return []localscan.Interface{
		{
			Name:        "eth0",
			Kind:        localscan.KindEthernet,
			Up:          true,
			Carrier:     true,
			Connected:   true,
			NetworkName: "LabLAN",
			IPv4:        "10.0.0.12/24",
			IP:          "10.0.0.12",
			Subnet:      "10.0.0.0/24",
			ScanTarget:  "10.0.0.0/24",
		},
		{
			Name:      "wlan0",
			Kind:      localscan.KindWiFi,
			Up:        true,
			Carrier:   false,
			Connected: false,
		},
		{
			Name:        "usb0",
			Kind:        localscan.KindUSB,
			Up:          true,
			Carrier:     true,
			OperState:   "unknown",
			Connected:   true,
			NetworkName: "USB · Pixel 8",
			IPv4:        "192.168.42.129/24",
			IP:          "192.168.42.129",
			Subnet:      "192.168.42.0/24",
			ScanTarget:  "192.168.42.0/24",
		},
		{
			Name:        "wlan1",
			Kind:        localscan.KindWiFi,
			Up:          true,
			Carrier:     true,
			Connected:   true,
			NetworkName: "HomeWiFi",
			IPv4:        "192.168.1.50/24",
			IP:          "192.168.1.50",
			Subnet:      "192.168.1.0/24",
			ScanTarget:  "192.168.1.0/24",
		},
	}
}

func TestLocalDisconnectedUnselectable(t *testing.T) {
	m := newLocalTestModel(t, 53, 14)
	next, _ := m.Update(localIfacesMsg{ifaces: sampleLocalIfaces()})
	m = next.(AppModel)

	// Starts on first connected interface (eth0, index 0).
	if m.Local.IfaceCursor != 0 {
		t.Fatalf("expected cursor 0 (eth0), got %d", m.Local.IfaceCursor)
	}
	// Moving down must skip disconnected wlan0 (index 1) and land on usb0 (index 2).
	m = sendKey(m, "down")
	if m.Local.IfaceCursor != 2 {
		t.Fatalf("expected cursor to skip disconnected wlan0 and land on usb0 (2), got %d", m.Local.IfaceCursor)
	}
	// Moving down again lands on wlan1 (index 3).
	m = sendKey(m, "down")
	if m.Local.IfaceCursor != 3 {
		t.Fatalf("expected cursor on wlan1 (3), got %d", m.Local.IfaceCursor)
	}
	// Moving up skips wlan0 again and returns to usb0 (2) -> eth0 (0).
	m = sendKey(m, "up")
	if m.Local.IfaceCursor != 2 {
		t.Fatalf("expected cursor on usb0 (2), got %d", m.Local.IfaceCursor)
	}
	m = sendKey(m, "up")
	if m.Local.IfaceCursor != 0 {
		t.Fatalf("expected cursor on eth0 (0), got %d", m.Local.IfaceCursor)
	}

	// If all interfaces are disconnected, none are selectable and Enter stays on ifaces.
	next, _ = m.Update(localIfacesMsg{ifaces: []localscan.Interface{
		{Name: "wlan0", Kind: localscan.KindWiFi, Connected: false},
		{Name: "eth0", Kind: localscan.KindEthernet, Connected: false},
	}})
	m = next.(AppModel)
	if m.Local.IfaceCursor != -1 {
		t.Fatalf("expected cursor -1 when all disconnected, got %d", m.Local.IfaceCursor)
	}
	m = sendKey(m, "down")
	m = sendKey(m, "enter")
	if m.Local.Mode != localModeIfaces {
		t.Fatalf("expected to remain in localModeIfaces when nothing is connected, got %v", m.Local.Mode)
	}
}

func TestLocalAllSizesAndFullScanFlow(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"CZ_40x10", 40, 10},
		{"CZ_45x10", 45, 10},
		{"CZ_53x14", 53, 14},
		{"Wide_100x30", 100, 30},
	}

	for _, sz := range sizes {
		m := newLocalTestModel(t, sz.w, sz.h)
		next, _ := m.Update(localIfacesMsg{ifaces: sampleLocalIfaces()})
		m = next.(AppModel)

		ifView := m.View()
		assertFitsBounds(t, ifView, sz.w, sz.h, sz.name+"/Local/Ifaces")
		for _, want := range []string{"eth0", "wlan0", "usb0", "wlan1"} {
			if !strings.Contains(ifView, want) {
				t.Fatalf("[%s] expected %q in interface list:\n%s", sz.name, want, ifView)
			}
		}

		// Select usb0 (USB tethering) and open Nmap scan options.
		m = sendKey(m, "down")
		if m.Local.IfaceCursor != 2 {
			t.Fatalf("[%s] expected usb0 selected, got %d", sz.name, m.Local.IfaceCursor)
		}
		m = sendKey(m, "enter")
		if m.Local.Mode != localModeOptions || m.Local.SelectedIface.Name != "usb0" {
			t.Fatalf("[%s] expected options for usb0, got mode=%v iface=%q", sz.name, m.Local.Mode, m.Local.SelectedIface.Name)
		}
		optView := m.View()
		assertFitsBounds(t, optView, sz.w, sz.h, sz.name+"/Local/Options")
		if !strings.Contains(optView, "192.168.42.0/24") || !strings.Contains(optView, "Discover Hosts") {
			t.Fatalf("[%s] expected target and options in:\n%s", sz.name, optView)
		}

		// Start Host Discovery (-sn) scan (transitions to running).
		m = sendKey(m, "enter")
		if m.Local.Mode != localModeRunning || !m.Local.Scanning {
			t.Fatalf("[%s] expected running mode, got mode=%v scanning=%v", sz.name, m.Local.Mode, m.Local.Scanning)
		}

		// Feed streaming progress and completion.
		next, _ = m.Update(localScanProgressMsg{
			seq: m.Local.ScanSeq,
			prog: localscan.ScanProgress{
				Task:    "ARP Ping Scan",
				Percent: 50,
				HasPct:  true,
				Line:    "192.168.42.1 · Pixel 8",
			},
		})
		m = next.(AppModel)
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/Running")

		next, _ = m.Update(localScanDoneMsg{
			seq: m.Local.ScanSeq,
			res: localscan.ScanResult{
				Target: "192.168.42.0/24",
				Mode:   localscan.ScanDiscovery,
				Hosts: []localscan.Host{
					{
						IP:       "192.168.42.1",
						Hostname: "pixel.local",
						MAC:      "02:11:22:33:44:55",
						Vendor:   "Google",
						Status:   "up",
						Reason:   "arp-response",
						Ports: []localscan.Port{
							{Number: 53, Protocol: "tcp", State: "open", Service: "domain"},
						},
					},
					{
						IP:     "192.168.42.129",
						Status: "up",
						Reason: "localhost-response",
					},
				},
			},
		})
		m = next.(AppModel)
		if m.Local.Mode != localModeResults || m.Local.Scanning {
			t.Fatalf("[%s] expected results mode, got mode=%v scanning=%v", sz.name, m.Local.Mode, m.Local.Scanning)
		}
		resView := m.View()
		assertFitsBounds(t, resView, sz.w, sz.h, sz.name+"/Local/Results")
		if !strings.Contains(resView, "192.168.42.1") {
			t.Fatalf("[%s] expected host IP in results:\n%s", sz.name, resView)
		}

		// Enter opens Host Detail; 's' scopes Nmap options to that single host.
		m = sendKey(m, "enter")
		if m.Local.Mode != localModeHostDetail {
			t.Fatalf("[%s] expected host detail mode, got %v", sz.name, m.Local.Mode)
		}
		detView := m.View()
		assertFitsBounds(t, detView, sz.w, sz.h, sz.name+"/Local/HostDetail")
		if !strings.Contains(detView, "53/tcp") {
			t.Fatalf("[%s] expected open port in host detail:\n%s", sz.name, detView)
		}

		m = sendKey(m, "s")
		if m.Local.Mode != localModeOptions || m.Local.TargetHost != "192.168.42.1" {
			t.Fatalf("[%s] expected single-host options, got mode=%v target=%q", sz.name, m.Local.Mode, m.Local.TargetHost)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/SingleHostOptions")

		// Esc unwinds cleanly: SingleHostOptions -> HostDetail -> Results -> Options -> Ifaces -> FocusTabs.
		m = sendKey(m, "esc")
		if m.Local.Mode != localModeHostDetail || m.Local.TargetHost != "" {
			t.Fatalf("[%s] expected back to HostDetail, got mode=%v target=%q", sz.name, m.Local.Mode, m.Local.TargetHost)
		}
		m = sendKey(m, "esc")
		if m.Local.Mode != localModeResults {
			t.Fatalf("[%s] expected back to Results, got %v", sz.name, m.Local.Mode)
		}
		m = sendKey(m, "esc")
		if m.Local.Mode != localModeOptions {
			t.Fatalf("[%s] expected back to Options, got %v", sz.name, m.Local.Mode)
		}
		m = sendKey(m, "esc")
		if m.Local.Mode != localModeIfaces || m.Focus != FocusContent {
			t.Fatalf("[%s] expected back to Ifaces in FocusContent, got mode=%v focus=%v", sz.name, m.Local.Mode, m.Focus)
		}
		m = sendKey(m, "esc")
		if m.Focus != FocusTabs {
			t.Fatalf("[%s] expected FocusTabs on final Esc, got %v", sz.name, m.Focus)
		}
	}
}

func TestLocalSingleHostPortScanMergesAndOpensDetail(t *testing.T) {
	m := newLocalTestModel(t, 53, 14)
	next, _ := m.Update(localIfacesMsg{ifaces: sampleLocalIfaces()})
	m = next.(AppModel)
	m = sendKey(m, "enter") // options
	m = sendKey(m, "enter") // Discover Hosts
	next, _ = m.Update(localScanDoneMsg{
		seq: m.Local.ScanSeq,
		res: localscan.ScanResult{
			Target: "10.0.0.0/24",
			Mode:   localscan.ScanDiscovery,
			Hosts: []localscan.Host{
				{IP: "10.0.0.1", Hostname: "gw", Status: "up", Reason: "arp-response"},
				{IP: "10.0.0.2", Hostname: "nas", Status: "up", Reason: "arp-response"},
			},
		},
	})
	m = next.(AppModel)

	// Select second host and press 's' directly from results.
	m = sendKey(m, "down")
	m = sendKey(m, "s")
	if m.Local.Mode != localModeOptions || m.Local.TargetHost != "10.0.0.2" {
		t.Fatalf("expected single-host options for 10.0.0.2, got mode=%v target=%q", m.Local.Mode, m.Local.TargetHost)
	}
	optView := m.View()
	if strings.Contains(optView, "Discover Hosts") || !strings.Contains(optView, "Quick Port Scan") {
		t.Fatalf("single-host menu should show port scan options only:\n%s", optView)
	}

	// Start Quick Port Scan (-F).
	m = sendKey(m, "enter")
	if !m.Local.RunSingleHost || m.Local.RunMode != localscan.ScanQuick {
		t.Fatalf("expected RunSingleHost=true RunMode=Quick, got %v %v", m.Local.RunSingleHost, m.Local.RunMode)
	}

	// Complete scan with open ports: must land directly in HostDetail showing ports.
	next, _ = m.Update(localScanDoneMsg{
		seq: m.Local.ScanSeq,
		res: localscan.ScanResult{
			Target: "10.0.0.2",
			Mode:   localscan.ScanQuick,
			Hosts: []localscan.Host{
				{
					IP:           "10.0.0.2",
					Status:       "up",
					Reason:       "user-set",
					PortsScanned: true,
					Ports: []localscan.Port{
						{Number: 22, Protocol: "tcp", State: "open", Service: "ssh"},
						{Number: 80, Protocol: "tcp", State: "open", Service: "http"},
					},
				},
			},
		},
	})
	m = next.(AppModel)
	if m.Local.Mode != localModeHostDetail {
		t.Fatalf("expected single-host scan to open HostDetail directly, got %v", m.Local.Mode)
	}
	detView := m.View()
	if !strings.Contains(detView, "22/tcp") || !strings.Contains(detView, "80/tcp") {
		t.Fatalf("expected open ports in HostDetail:\n%s", detView)
	}

	// Esc returns to the full subnet host list with both hosts preserved and port summary visible.
	m = sendKey(m, "esc")
	if m.Local.Mode != localModeResults || len(m.Local.Result.Hosts) != 2 {
		t.Fatalf("expected 2 preserved hosts in Results, got mode=%v count=%d", m.Local.Mode, len(m.Local.Result.Hosts))
	}
	resView := m.View()
	if !strings.Contains(resView, "10.0.0.1") || !strings.Contains(resView, "2p:22,80") {
		t.Fatalf("expected preserved subnet hosts and port summary:\n%s", resView)
	}

	// Host scanned with 0 open ports distinguishes scanned vs not scanned.
	m.Local.HostCursor = 0
	m = sendKey(m, "s")
	m = sendKey(m, "enter")
	next, _ = m.Update(localScanDoneMsg{
		seq: m.Local.ScanSeq,
		res: localscan.ScanResult{
			Target: "10.0.0.1",
			Mode:   localscan.ScanQuick,
			Hosts:  []localscan.Host{{IP: "10.0.0.1", Status: "up", PortsScanned: true}},
		},
	})
	m = next.(AppModel)
	if !strings.Contains(m.View(), "0 open (scanned)") {
		t.Fatalf("expected '0 open (scanned)' in HostDetail:\n%s", m.View())
	}
}
