package wifi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDNSRule(t *testing.T) {
	cases := []struct {
		in   string
		host string
		ip   string
	}{
		{"portal.test=192.168.66.1", "portal.test", "192.168.66.1"},
		{"login.example.com 10.0.0.5", "login.example.com", "10.0.0.5"},
		{"192.168.1.10", "*", "192.168.1.10"},
		{"*=10.0.0.2", "*", "10.0.0.2"},
	}
	for _, tc := range cases {
		rule, err := ParseDNSRule(tc.in)
		if err != nil {
			t.Fatalf("ParseDNSRule(%q): %v", tc.in, err)
		}
		if rule.Host != tc.host || rule.IP != tc.ip {
			t.Fatalf("ParseDNSRule(%q) = %+v, want %s/%s", tc.in, rule, tc.host, tc.ip)
		}
	}
	for _, bad := range []string{"", "host=not-an-ip", "host=2001:db8::1", "a b=1.2.3.4"} {
		if _, err := ParseDNSRule(bad); err == nil {
			t.Fatalf("ParseDNSRule(%q) should fail", bad)
		}
	}

	rules := ParseDNSRules("a.test=10.0.0.1,b.test=10.0.0.2;c.test=10.0.0.3")
	if len(rules) != 3 {
		t.Fatalf("parsed %d rules, want 3", len(rules))
	}
	if got := FormatDNSRule(DNSRule{Host: "*", IP: "1.2.3.4"}); got != "all → 1.2.3.4" {
		t.Fatalf("FormatDNSRule = %q", got)
	}
}

func TestDNSSection(t *testing.T) {
	// Default: everything to the gateway so the portal catches all names.
	lines := DNSSection(EvilGateway, nil)
	if len(lines) != 1 || lines[0] != "address=/#/192.168.66.1" {
		t.Fatalf("default section = %v", lines)
	}

	// Specific rules come first, catch-all stays for the portal.
	lines = DNSSection(EvilGateway, []DNSRule{
		{Host: "portal.test", IP: "192.168.66.1"},
		{Host: "files.test", IP: "192.168.66.50"},
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "address=/portal.test/192.168.66.1") {
		t.Fatalf("missing portal.test rule:\n%s", joined)
	}
	if !strings.Contains(joined, "address=/files.test/192.168.66.50") {
		t.Fatalf("missing files.test rule:\n%s", joined)
	}
	if !strings.Contains(joined, "address=/#/192.168.66.1") {
		t.Fatalf("missing catch-all:\n%s", joined)
	}

	// A "*" rule moves the catch-all target.
	lines = DNSSection(EvilGateway, []DNSRule{{Host: "*", IP: "10.9.9.9"}})
	if len(lines) != 1 || lines[0] != "address=/#/10.9.9.9" {
		t.Fatalf("wildcard override = %v", lines)
	}

	// dnsmasq.conf carries the rules.
	conf := DnsmasqConfig("wlan0", EvilGateway, DNSRule{Host: "login.test", IP: "192.168.66.7"})
	if !strings.Contains(conf, "address=/login.test/192.168.66.7") {
		t.Fatalf("DnsmasqConfig lost the custom rule:\n%s", conf)
	}
	if !strings.Contains(conf, "dhcp-option=6,192.168.66.1") {
		t.Fatalf("dnsmasq.conf lost the DNS dhcp option:\n%s", conf)
	}
}

func TestPortalModeParse(t *testing.T) {
	cases := map[string]PortalMode{
		"":         PortalLogin,
		"login":    PortalLogin,
		"custom":   PortalCustom,
		"html":     PortalCustom,
		"off":      PortalOff,
		"none":     PortalOff,
		"whatever": PortalLogin,
	}
	for in, want := range cases {
		if got := ParsePortalMode(in); got != want {
			t.Fatalf("ParsePortalMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPortalCapturesCredentials(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "portal.ndjson")
	var events []string
	h := NewPortalHandler(PortalConfig{
		Mode:    PortalLogin,
		LogPath: logPath,
		OnEvent: func(line string) { events = append(events, line) },
	})

	// Captive-portal detection gets redirected (no credential logging).
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/generate_204", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("generate_204 status = %d, want 302", rec.Code)
	}

	// The login page is served for other GETs.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portal", nil))
	if !strings.Contains(rec.Body.String(), "<form") {
		t.Fatalf("login page missing a form:\n%s", rec.Body.String())
	}

	// A POST submission is logged and answered with the success page.
	body := "email=victim%40example.com&password=hunter2&url=https%3A%2F%2Fexample.com%2F"
	req := httptest.NewRequest(http.MethodPost, "/portal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "CaptiveBrowser/1.0")
	req.RemoteAddr = "192.168.66.23:5555"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "You are connected") {
		t.Fatalf("POST response = %d, body=%q", rec.Code, rec.Body.String())
	}

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("portal log missing: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			lines = append(lines, sc.Text())
		}
	}
	if len(lines) != 1 {
		t.Fatalf("logged %d lines, want 1: %v", len(lines), lines)
	}
	var cap PortalCapture
	if err := json.Unmarshal([]byte(lines[0]), &cap); err != nil {
		t.Fatalf("bad JSON line: %v", err)
	}
	if cap.Fields["email"] != "victim@example.com" || cap.Fields["password"] != "hunter2" {
		t.Fatalf("captured fields = %v", cap.Fields)
	}
	if cap.ClientIP != "192.168.66.23" || cap.Method != http.MethodPost {
		t.Fatalf("capture metadata = %+v", cap)
	}
	if cap.Target != "https://example.com/" {
		t.Fatalf("target URL = %q", cap.Target)
	}
	if len(events) == 0 || !strings.Contains(events[0], "portal:") {
		t.Fatalf("no capture event emitted: %v", events)
	}

	// A GET-based submission is captured too.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/portal?username=bob&password=pw", nil))
	f2, _ := os.ReadFile(logPath)
	if strings.Count(string(f2), "\n") != 2 {
		t.Fatalf("GET submission not logged:\n%s", string(f2))
	}
}

func TestPortalCustomPage(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "portal.html")
	if err := os.WriteFile(page, []byte("<html><body>MY CUSTOM PORTAL</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewPortalHandler(PortalConfig{Mode: PortalCustom, File: page, LogPath: filepath.Join(dir, "log.ndjson")})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if !strings.Contains(rec.Body.String(), "MY CUSTOM PORTAL") {
		t.Fatalf("custom page not served:\n%s", rec.Body.String())
	}

	// A missing file falls back to the built-in page instead of failing.
	h = NewPortalHandler(PortalConfig{Mode: PortalCustom, File: filepath.Join(dir, "nope.html")})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "<form") {
		t.Fatalf("fallback login page missing:\n%s", rec.Body.String())
	}
}
