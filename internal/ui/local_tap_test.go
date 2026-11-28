package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/czhax/czhax/internal/localscan"
	"github.com/czhax/czhax/internal/wifi"
)

type errFake struct{}

func (errFake) Error() string { return "boom" }

func tapTestIfaces() []localscan.Interface {
	return []localscan.Interface{
		{Name: "wlan0", Kind: localscan.KindWiFi, Up: true, Connected: true, IP: "192.168.1.9"},
		{Name: "eth0", Kind: localscan.KindEthernet, Up: true, Carrier: true, Connected: true, IP: "192.168.1.5", Subnet: "192.168.1.0/24"},
		{Name: "eth1", Kind: localscan.KindUSB, Up: true, Carrier: true, Connected: true, IP: "192.168.1.6", Subnet: "192.168.1.0/24"},
	}
}

// TestLocalTapTabBounds covers the new Local -> Tap subtab at every viewport.
func TestLocalTapTabBounds(t *testing.T) {
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
		m := newLocalTestApp(t, sz.w, sz.h)
		next, _ := m.Update(localIfacesMsg{ifaces: tapTestIfaces()})
		m = next.(AppModel)
		m.ActiveTabs[ViewLocal] = LocalTabTap
		m.Focus = FocusContent

		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/Tap")

		// WiFi must never be offered as a port.
		if m.LocalTap.PortA != "eth0" || m.LocalTap.PortB != "eth1" {
			t.Fatalf("[%s] ports = %s/%s, want eth0/eth1", sz.name, m.LocalTap.PortA, m.LocalTap.PortB)
		}

		// Every field, including the conditional Portal row.
		for i := 0; i < len(m.LocalTap.fields())+1; i++ {
			m = sendKey(m, "down")
			assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/Tap/Field")
		}
		m = sendKey(m, "d") // spoof on -> portal row appears
		if !m.LocalTap.Spoof {
			t.Fatalf("[%s] d did not enable spoof", sz.name)
		}
		if !strings.Contains(m.View(), "Portal") {
			t.Fatalf("[%s] portal row missing with spoof on:\n%s", sz.name, m.View())
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/Tap/Spoof")

		// Running state with counters and credential lines.
		m.LocalTap.Seq = 1
		m.LocalTap.Active = true
		m.LocalTap.Mode = tapModeRunning
		m.LocalTap.Started = time.Now().Add(-75 * time.Second)
		m.LocalTap.Elapsed = 75 * time.Second
		m.LocalTap.AppendLine(1, "tap cztap0: eth0 <-> eth1 (bridge, transparent L2)")
		m.LocalTap.AppendLine(1, "stats 1.2k pkt · 1.1 MB · 8 hosts")
		m.LocalTap.AppendLine(1, "proto dns 14 · http 3 · tls 9")
		m.LocalTap.AppendLine(1, "top 192.168.1.20")
		m.LocalTap.AppendLine(1, "dns 192.168.1.20 asks example.com")
		m.LocalTap.AppendLine(1, "auth 192.168.1.20 admin:secret")
		m.LocalTap.AppendLine(1, "portal: email,password from 192.168.1.20")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/Tap/Running")
		view := m.View()
		if !strings.Contains(view, "1.1 MB") {
			t.Fatalf("[%s] tap stats not shown:\n%s", sz.name, view)
		}
	}
}

// TestLocalTapConfig pins the helper configuration and port swapping.
func TestLocalTapConfig(t *testing.T) {
	v := NewLocalTapView()
	v.SyncInterfaces(tapTestIfaces())
	if v.PortA != "eth0" || v.PortB != "eth1" {
		t.Fatalf("ports = %s/%s", v.PortA, v.PortB)
	}
	if got := v.Subnet(); got != "192.168.1.0/24" {
		t.Fatalf("subnet = %q", got)
	}
	if !v.Record {
		t.Fatal("recording should default on")
	}
	if v.Portal != wifi.PortalLogin {
		t.Fatalf("portal = %q", v.Portal)
	}

	cfg := v.Config("/home/pi/czhax/captures")
	if cfg.PortA != "eth0" || cfg.PortB != "eth1" || cfg.CaptureDir != "/home/pi/czhax/captures" {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.Spoof || cfg.Portal != string(wifi.PortalOff) {
		t.Fatalf("spoof must be off and portal disabled by default: %+v", cfg)
	}
	if !strings.HasSuffix(cfg.PortalDoc, "czhax/user/portal.html") {
		t.Fatalf("portal page = %q", cfg.PortalDoc)
	}

	v.Spoof = true
	v.Portal = wifi.PortalCustom
	cfg = v.Config("/home/pi/czhax/captures")
	if !cfg.Spoof || cfg.Portal != string(wifi.PortalCustom) || cfg.Subnet != "192.168.1.0/24" {
		t.Fatalf("spoof config = %+v", cfg)
	}

	// Two links: → swaps the pair instead of getting stuck.
	v.Cursor = 0
	v.Adjust(1)
	if v.PortA != "eth1" || v.PortB != "eth0" {
		t.Fatalf("ports did not swap: %s/%s", v.PortA, v.PortB)
	}
}

// TestLocalTapStartValidation checks the guards before a privileged run.
func TestLocalTapStartValidation(t *testing.T) {
	v := NewLocalTapView()
	v.SyncInterfaces(tapTestIfaces())
	if _, _, _, _, err := v.Start(t.TempDir()); err != nil {
		t.Fatalf("valid setup rejected: %v", err)
	}
	v.Stop()

	same := NewLocalTapView()
	same.SyncInterfaces(tapTestIfaces())
	same.PortB = same.PortA
	if _, _, _, _, err := same.Start(t.TempDir()); err == nil {
		t.Fatal("identical ports must be rejected")
	}

	noSubnet := NewLocalTapView()
	noSubnet.SyncInterfaces([]localscan.Interface{
		{Name: "eth0", Kind: localscan.KindEthernet, Up: true},
		{Name: "eth1", Kind: localscan.KindUSB, Up: true},
	})
	noSubnet.Spoof = true
	if _, _, _, _, err := noSubnet.Start(t.TempDir()); err == nil {
		t.Fatal("spoof without a subnet must be rejected")
	}

	one := NewLocalTapView()
	one.SyncInterfaces([]localscan.Interface{{Name: "eth0", Kind: localscan.KindEthernet, Up: true}})
	if _, _, _, _, err := one.Start(t.TempDir()); err == nil {
		t.Fatal("a single port must be rejected")
	}
}

// TestLocalTapAppendLine covers the counter rows and log ring.
func TestLocalTapAppendLine(t *testing.T) {
	v := NewLocalTapView()
	v.Seq = 3
	v.AppendLine(2, "stats 1 pkt") // stale run: ignored
	if v.Stats != "" {
		t.Fatalf("stale line accepted: %q", v.Stats)
	}
	v.AppendLine(3, "stats 12 pkt · 4 kB · 2 hosts")
	v.AppendLine(3, "proto dns 2 · http 1 · tls 0")
	v.AppendLine(3, "top 10.0.0.2")
	v.AppendLine(3, "dns 10.0.0.2 asks x.test")
	if v.Stats != "12 pkt · 4 kB · 2 hosts" || !strings.Contains(v.Proto, "dns 2") || v.Top != "10.0.0.2" {
		t.Fatalf("counter rows = %q / %q / %q", v.Stats, v.Proto, v.Top)
	}
	if len(v.Lines) != 1 {
		t.Fatalf("log lines = %v", v.Lines)
	}
	for i := 0; i < 200; i++ {
		v.AppendLine(3, "line")
	}
	if len(v.Lines) != 160 {
		t.Fatalf("log ring not capped: %d", len(v.Lines))
	}
}

// TestLocalTapEscStopsRun makes sure Esc stops the tap instead of leaving it.
func TestLocalTapEscStopsRun(t *testing.T) {
	m := newLocalTestApp(t, 53, 14)
	next, _ := m.Update(localIfacesMsg{ifaces: tapTestIfaces()})
	m = next.(AppModel)
	m.ActiveTabs[ViewLocal] = LocalTabTap
	m.Focus = FocusContent
	m = sendKey(m, "enter") // focus content
	for i := 0; i < len(m.LocalTap.fields())-1; i++ {
		m = sendKey(m, "down")
	}
	m = sendKey(m, "enter") // Action
	if !m.LocalTap.Active || m.LocalTap.Mode != tapModeRunning {
		t.Fatalf("tap did not start: active=%v mode=%v err=%q", m.LocalTap.Active, m.LocalTap.Mode, m.LocalTap.Err)
	}
	m = sendKey(m, "esc")
	if m.LocalTap.Cancel != nil {
		t.Fatal("esc did not cancel the tap run")
	}
	if m.Focus != FocusContent {
		t.Fatal("esc must not leave the tab while a run is active")
	}
	// The helper's exit arrives asynchronously and returns to the setup screen.
	m.LocalTap.Finish(m.LocalTap.Seq, nil)
	if m.LocalTap.Active || m.LocalTap.Mode != tapModeSetup {
		t.Fatal("finishing the run must return to setup")
	}
	// A failed run must not trap the user in the running screen.
	m.LocalTap.Mode = tapModeRunning
	m.LocalTap.Finish(m.LocalTap.Seq+1, errFake{})
	m.LocalTap.Active = false
	if !m.LocalTap.HandleEsc() || m.LocalTap.Mode != tapModeSetup {
		t.Fatal("esc must dismiss a failed run")
	}
}
