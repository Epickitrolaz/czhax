package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/localscan"
	"github.com/czhax/czhax/internal/wifi"
)

// newTestApp builds a plain app model at the given terminal size.
func newTestApp(t *testing.T, w, h int) AppModel {
	t.Helper()
	cfg := config.Default()
	tmp := t.TempDir()
	cfg.WorkDir = tmp
	m := NewAppModel(cfg, filepath.Join(tmp, "config.json"), "v0.5")
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(AppModel)
}

func newLocalTestApp(t *testing.T, w, h int) AppModel {
	t.Helper()
	cfg := config.Default()
	tmp := t.TempDir()
	cfg.WorkDir = tmp
	m := NewAppModel(cfg, filepath.Join(tmp, "config.json"), "v0.5")
	m.ActiveView = ViewLocal
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(AppModel)
}

// TestLocalDHCPTabBounds covers the new Local -> DHCP subtab at every viewport.
func TestLocalDHCPTabBounds(t *testing.T) {
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
		ifaces := []localscan.Interface{
			{Name: "wlan0", Kind: localscan.KindWiFi, Up: true, Connected: true, IP: "192.168.1.5"},
			{Name: "eth0", Kind: localscan.KindEthernet, Up: true, Connected: true, IP: "10.0.0.7"},
		}
		next, _ := m.Update(localIfacesMsg{ifaces: ifaces})
		m = next.(AppModel)

		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/Scan")

		m.ActiveTabs[ViewLocal] = LocalTabDHCP
		m.Focus = FocusContent
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/DHCP")

		// Wired interface preferred over WiFi for raw DHCP frames.
		if got := m.LocalDHCP.EffectiveIface(); got != "eth0" {
			t.Fatalf("[%s] DHCP interface = %q, want eth0", sz.name, got)
		}

		// Cycle every field and confirm the layout stays inside the viewport.
		for i := 0; i < len(m.LocalDHCP.fields())+1; i++ {
			m = sendKey(m, "down")
			assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/DHCP/Field")
		}
		m = sendKey(m, "right")
		m = sendKey(m, "m") // toggle flood mode
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/DHCP/Flood")

		// Running state with log lines.
		m.LocalDHCP.Seq = 1
		m.LocalDHCP.Active = true
		m.LocalDHCP.Mode = dhcpModeRunning
		m.LocalDHCP.Started = time.Now().Add(-12 * time.Second)
		m.LocalDHCP.Elapsed = 12 * time.Second
		m.LocalDHCP.AppendLine(1, "DHCP starvation on eth0 (25 req/s, 500 requests, MACs random)")
		m.LocalDHCP.AppendLine(1, "sent=25 offers=9 unique=9 last=10.0.0.42")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/Local/DHCP/Running")
		if !strings.Contains(m.View(), "unique=9") {
			t.Fatalf("[%s] DHCP stats not shown:\n%s", sz.name, m.View())
		}
	}
}

// TestLocalDHCPConfig pins the helper arguments and safety defaults.
func TestLocalDHCPConfig(t *testing.T) {
	v := NewLocalDHCPView()
	v.SyncInterfaces([]localscan.Interface{
		{Name: "wlan1", Kind: localscan.KindWiFi, Connected: true},
		{Name: "usb0", Kind: localscan.KindUSB, Connected: true},
	})
	if got := v.EffectiveIface(); got != "usb0" {
		t.Fatalf("interface = %q, want usb0 (wired/tethered preferred)", got)
	}
	cfg := v.Config()
	if cfg.Rate != 25 || cfg.Count != 500 || !cfg.RandomMAC {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	v.Probe = false
	if cfg = v.Config(); cfg.Count != 0 {
		t.Fatalf("flood mode must not cap the request count: %+v", cfg)
	}
	v.Rate = 1000
	v.Cursor = 1 // rate field
	v.Adjust(0)
	if v.Rate != 250 {
		t.Fatalf("rate not clamped: %d", v.Rate)
	}
}

// TestEvilPortalAndDNSEditor covers the custom portal modes and DNS redirect
// editor added to the Evil tab.
func TestEvilPortalAndDNSEditor(t *testing.T) {
	m := newTestApp(t, 53, 14)
	m.ActiveView = ViewWiFi
	m.ActiveTabs[ViewWiFi] = WiFiTabEvil
	m.Focus = FocusContent
	m.WifiEvil.SyncInterfaces([]wifi.Interface{{Name: "wlan0"}, {Name: "wlan1"}})

	if m.WifiEvil.PortalMode != wifi.PortalLogin {
		t.Fatalf("default portal mode = %q, want login", m.WifiEvil.PortalMode)
	}

	// Cycle Portal off -> login -> custom -> off.
	m.WifiEvil.Cursor = indexOfEvilField(m.WifiEvil, evilFieldPortal)
	m = sendKey(m, "right")
	if m.WifiEvil.PortalMode != wifi.PortalCustom {
		t.Fatalf("portal mode = %q, want custom", m.WifiEvil.PortalMode)
	}
	view := m.View()
	if !strings.Contains(view, "Portal") || !strings.Contains(view, "custom") {
		t.Fatalf("portal row missing:\n%s", view)
	}
	m = sendKey(m, "right")
	if m.WifiEvil.PortalMode != wifi.PortalOff {
		t.Fatalf("portal mode = %q, want off", m.WifiEvil.PortalMode)
	}
	m = sendKey(m, "right")
	if m.WifiEvil.PortalMode != wifi.PortalLogin {
		t.Fatalf("portal mode = %q, want login again", m.WifiEvil.PortalMode)
	}

	// Open the DNS editor and add a redirect rule by typing.
	m.WifiEvil.Cursor = indexOfEvilField(m.WifiEvil, evilFieldDNS)
	m = sendKey(m, "enter")
	if m.WifiEvil.Mode != evilModeDNS {
		t.Fatalf("expected the DNS editor, got %v", m.WifiEvil.Mode)
	}
	assertFitsBounds(t, m.View(), 53, 14, "Evil/DNS/Empty")

	m = sendKey(m, "a")
	if m.WifiEvil.Mode != evilModeDNSEdit {
		t.Fatalf("expected the rule input, got %v", m.WifiEvil.Mode)
	}
	if !m.WifiEvil.IsTextInput() {
		t.Fatal("rule input must capture text keys")
	}
	for _, r := range "portal.test=192.168.66.80" {
		m = sendKey(m, string(r))
	}
	assertFitsBounds(t, m.View(), 53, 14, "Evil/DNS/Typing")
	m = sendKey(m, "enter")
	if len(m.WifiEvil.DNSRules) != 1 {
		t.Fatalf("rule not saved: %+v", m.WifiEvil.DNSRules)
	}
	if got := m.WifiEvil.DNSRules[0]; got.Host != "portal.test" || got.IP != "192.168.66.80" {
		t.Fatalf("unexpected rule: %+v", got)
	}
	assertFitsBounds(t, m.View(), 53, 14, "Evil/DNS/List")

	// Invalid rules are rejected without leaving the input.
	m = sendKey(m, "a")
	for _, r := range "bad=not-an-ip" {
		m = sendKey(m, string(r))
	}
	m = sendKey(m, "enter")
	if m.WifiEvil.Mode != evilModeDNSEdit || m.WifiEvil.InputErr == "" {
		t.Fatalf("invalid rule accepted: mode=%v err=%q", m.WifiEvil.Mode, m.WifiEvil.InputErr)
	}
	m = sendKey(m, "esc")
	if m.WifiEvil.Mode != evilModeDNS {
		t.Fatalf("esc from input = %v, want DNS list", m.WifiEvil.Mode)
	}

	// Deleting works and the config carries the rules + portal mode.
	m = sendKey(m, "x")
	if len(m.WifiEvil.DNSRules) != 0 {
		t.Fatalf("rule not deleted: %+v", m.WifiEvil.DNSRules)
	}
	m.WifiEvil.DNSRules = []wifi.DNSRule{{Host: "login.test", IP: "192.168.66.9"}}
	cfg := m.WifiEvil.Config()
	if !cfg.Portal || cfg.PortalMode != string(wifi.PortalLogin) {
		t.Fatalf("portal not carried into the config: %+v", cfg)
	}
	if len(cfg.DNSRules) != 1 || cfg.DNSRules[0].Host != "login.test" {
		t.Fatalf("DNS rules not carried into the config: %+v", cfg.DNSRules)
	}
	if got := wifi.DnsmasqConfig("wlan0", wifi.EvilGateway, cfg.DNSRules...); !strings.Contains(got, "address=/login.test/192.168.66.9") {
		t.Fatalf("dnsmasq.conf missing the rule:\n%s", got)
	}

	// Esc closes the editor back to the setup screen (not to the tabs).
	m = sendKey(m, "esc")
	if m.WifiEvil.Mode != evilModeSetup || m.Focus != FocusContent {
		t.Fatalf("esc = mode %v focus %v, want setup/content", m.WifiEvil.Mode, m.Focus)
	}
}

func indexOfEvilField(v WifiEvilView, want wifiEvilField) int {
	for i, f := range v.fields() {
		if f == want {
			return i
		}
	}
	return 0
}

// TestEvilPortalFileResolution checks the custom page path handling.
func TestEvilPortalFileResolution(t *testing.T) {
	v := NewWifiEvilView()
	v.BaseDir = "/home/pi/czhax"

	v.PortalMode = wifi.PortalLogin
	if got := v.portalFile(); got != "" {
		t.Fatalf("login mode should not use a file, got %q", got)
	}
	v.PortalMode = wifi.PortalCustom
	if got := v.portalFile(); got != "/home/pi/czhax/user/portal.html" {
		t.Fatalf("default custom page = %q", got)
	}
	v.PortalFile = "/tmp/my.html"
	if got := v.portalFile(); got != "/tmp/my.html" {
		t.Fatalf("absolute page = %q", got)
	}
	v.PortalFile = "pages/alt.html"
	if got := v.portalFile(); got != "/home/pi/czhax/pages/alt.html" {
		t.Fatalf("relative page = %q", got)
	}
}
