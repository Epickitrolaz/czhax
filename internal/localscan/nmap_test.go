package localscan

import (
	"strings"
	"testing"
)

func TestParseNmapXML(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<nmaprun scanner="nmap">
  <host>
    <status state="down" reason="no-response"/>
    <address addr="192.168.1.99" addrtype="ipv4"/>
  </host>
  <host>
    <status state="up" reason="syn-ack"/>
    <address addr="192.168.1.20" addrtype="ipv4"/>
    <address addr="aa:bb:cc:dd:ee:02" addrtype="mac" vendor="Raspberry Pi"/>
    <hostnames><hostname name="pi.local" type="PTR"/></hostnames>
    <ports>
      <port protocol="tcp" portid="80"><state state="closed"/><service name="http"/></port>
      <port protocol="tcp" portid="22"><state state="open"/><service name="ssh" product="OpenSSH" version="9.2"/></port>
    </ports>
    <os><osmatch name="Linux 6.6" accuracy="99"/></os>
  </host>
  <host>
    <status state="up" reason="arp-response"/>
    <address addr="192.168.1.2" addrtype="ipv4"/>
    <hostnames><hostname name="gw.local" type="PTR"/></hostnames>
  </host>
</nmaprun>`)

	hosts, err := ParseNmapXML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 live hosts, got %d: %+v", len(hosts), hosts)
	}
	// Numeric IPv4 sorting: .2 before .20
	if hosts[0].IP != "192.168.1.2" || hosts[0].Hostname != "gw.local" {
		t.Fatalf("unexpected first host: %+v", hosts[0])
	}
	if hosts[1].IP != "192.168.1.20" || hosts[1].MAC != "AA:BB:CC:DD:EE:02" || hosts[1].Vendor != "Raspberry Pi" || hosts[1].OS != "Linux 6.6" {
		t.Fatalf("unexpected second host: %+v", hosts[1])
	}
	if len(hosts[1].Ports) != 1 || hosts[1].Ports[0].Number != 22 || hosts[1].Ports[0].Service != "ssh" {
		t.Fatalf("expected only open port 22, got %+v", hosts[1].Ports)
	}
}

func TestParseTaskProgressTag(t *testing.T) {
	task, pct, ok := parseTaskProgressTag(`<taskprogress task="SYN Stealth Scan" time="1700000000" percent="63.40" remaining="4"/>`)
	if !ok || task != "SYN Stealth Scan" || pct < 63.3 || pct > 63.5 {
		t.Fatalf("unexpected progress parse: ok=%v task=%q pct=%v", ok, task, pct)
	}
}

func TestBuildNmapArgs(t *testing.T) {
	req := ScanRequest{Iface: "usb0", Target: "192.168.42.0/24", Mode: ScanOS}
	priv := strings.Join(BuildNmapArgs(req, true), " ")
	if !strings.Contains(priv, "-A") || strings.Contains(priv, "-e usb0") || !strings.Contains(priv, "-oX -") {
		t.Fatalf("unexpected privileged args: %q", priv)
	}
	unpriv := strings.Join(BuildNmapArgs(req, false), " ")
	if strings.Contains(unpriv, " -A ") || !strings.Contains(unpriv, "-sV -sC") {
		t.Fatalf("unexpected unprivileged args: %q", unpriv)
	}

	single := strings.Join(BuildNmapArgs(ScanRequest{Target: "192.168.42.1", Mode: ScanQuick, SingleHost: true}, true), " ")
	if !strings.Contains(single, "-F") || !strings.Contains(single, "-Pn") || !strings.Contains(single, "-n") {
		t.Fatalf("expected -Pn -n for single-host port scan, got %q", single)
	}
}
