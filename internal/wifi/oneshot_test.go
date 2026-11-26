package wifi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testIWScanWPS = `
BSS 5c:7b:5c:46:e7:a8(on wlan0)
	freq: 2412.0
	signal: -62.00 dBm
	SSID: LabNet
	capability: ESS Privacy (0x0411)
	RSN:	 * Version: 1
		 * Authentication suites: PSK
	WPS:	 * Version: 1.0
		 * AP setup locked: 0x00
		 * Model: RT-AC66U
		 * Model Number: 1.0
		 * Device name: ASUS Router

BSS 11:22:33:44:55:66(on wlan0)
	freq: 2437.0
	signal: -70.00 dBm
	SSID: OfficeAP
	capability: ESS Privacy (0x0411)
	RSN:	 * Version: 1
		 * Authentication suites: PSK SAE
	WPS:	 * Version: 2.0
		 * AP setup locked: 0x01
		 * Device name: Unknown Box
`

func TestParseWPSScan(t *testing.T) {
	infos := ParseWPSScan(testIWScanWPS)
	if len(infos) != 2 {
		t.Fatalf("expected 2 WPS APs, got %d: %+v", len(infos), infos)
	}
	vuln := []string{"RT-AC66U 1.0", "Some Other Router"}
	a, ok := infos["5C:7B:5C:46:E7:A8"]
	if !ok || !a.WPS || a.Version != "1.0" || a.Locked {
		t.Fatalf("unexpected WPS 1.0 info: %+v", a)
	}
	if a.Model != "RT-AC66U" || a.ModelNumber != "1.0" || a.DeviceName != "ASUS Router" {
		t.Fatalf("model fields = %+v", a)
	}
	if !a.Vulnerable(vuln) {
		t.Fatal("WPS 1.0 AP must be considered vulnerable")
	}

	b := infos["11:22:33:44:55:66"]
	if b.Version != "2.0" || !b.Locked {
		t.Fatalf("unexpected WPS 2.0 info: %+v", b)
	}
	if b.Vulnerable(nil) {
		t.Fatal("WPS 2.0 AP without a DB match must not be considered vulnerable")
	}
	if !b.Vulnerable([]string{"Unknown Box"}) {
		t.Fatal("expected OneShot DB device-name match to mark the AP vulnerable")
	}
}

func TestWPSInfoVulnerableMatchesDB(t *testing.T) {
	info := WPSInfo{WPS: true, Version: "2.0", Model: "Fritz!Box", ModelNumber: "7390"}
	if !info.Vulnerable([]string{"Fritz!Box 7390"}) {
		t.Fatal("expected model+number DB match to be vulnerable")
	}
	if info.Vulnerable([]string{"Fritz!Box 7490"}) {
		t.Fatal("expected non-matching model to be safe")
	}
	if (WPSInfo{}).Vulnerable([]string{"anything"}) {
		t.Fatal("AP without WPS must never be vulnerable")
	}
}

func TestFindOneShotAndVulnList(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "ose.py")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env python3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	vulnPath := filepath.Join(dir, "vulnwsc.txt")
	if err := os.WriteFile(vulnPath, []byte("# comment\nRT-AC66U 1.0\n\nFritz!Box 7390\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := FindOneShot(dir)
	if !o.Found() || o.Script != script || o.Dir != dir {
		t.Fatalf("FindOneShot = %+v", o)
	}
	if o.VulnList != vulnPath {
		t.Fatalf("VulnList = %q, want %q", o.VulnList, vulnPath)
	}
	list := LoadVulnList(vulnPath)
	if len(list) != 2 || list[0] != "RT-AC66U 1.0" || list[1] != "Fritz!Box 7390" {
		t.Fatalf("LoadVulnList = %v", list)
	}
	if got := LoadVulnList(filepath.Join(dir, "missing.txt")); len(got) != 0 {
		t.Fatalf("expected empty list for missing DB, got %v", got)
	}

	// A path pointing directly at ose.py also resolves.
	if o2 := FindOneShot(script); !o2.Found() || o2.Script != script {
		t.Fatalf("FindOneShot(script) = %+v", o2)
	}
}

func TestSplitAttackArgs(t *testing.T) {
	got := SplitAttackArgs(" -P  -k   -F ")
	if strings.Join(got, " ") != "-P -k -F" || len(got) != 3 {
		t.Fatalf("SplitAttackArgs = %#v", got)
	}
}

func TestDefaultOneShotAttack(t *testing.T) {
	if OneShotDefaultAttack != "-P -k" {
		t.Fatalf("default OneShot attack = %q, want -P -k", OneShotDefaultAttack)
	}
	if strings.Contains(OneShotDefaultAttack, "-F") {
		t.Fatal("plain Pixie Dust must not force pixiewps by default")
	}
}

func TestEmbeddedOneShotIsDefault(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CZHAX_ONESHOT_DIR", "")

	o := FindOneShot("")
	if !o.Found() {
		t.Fatalf("expected embedded OneShot, got %+v", o)
	}
	if !o.Embedded {
		t.Fatalf("expected Embedded=true, got %+v", o)
	}
	if _, err := os.Stat(o.Script); err != nil {
		t.Fatalf("embedded ose.py missing at %s: %v", o.Script, err)
	}
	if _, err := os.Stat(o.VulnList); err != nil {
		t.Fatalf("embedded vulnwsc.txt missing at %s: %v", o.VulnList, err)
	}
	if len(LoadVulnList(o.VulnList)) < 50 {
		t.Fatalf("embedded vuln list looks truncated: %d entries", len(LoadVulnList(o.VulnList)))
	}
}

func TestOneShotOverrideWins(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CZHAX_ONESHOT_DIR", "")

	dir := t.TempDir()
	script := filepath.Join(dir, "ose.py")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env python3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := FindOneShot(dir)
	if !o.Found() || o.Embedded || o.Script != script {
		t.Fatalf("expected local override, got %+v", o)
	}

	// A bad explicit path must not silently fall back to the embedded copy.
	bad := FindOneShot(filepath.Join(dir, "missing"))
	if bad.Found() || !strings.Contains(bad.Err, "unset oneshot_dir") {
		t.Fatalf("expected explicit-path error, got %+v", bad)
	}
}

func TestRestoreAfterOneShot(t *testing.T) {
	var calls []string
	s := scanner{
		euid:   func() int { return 0 },
		hasBin: func(string) bool { return true },
		runCmd: func(_ context.Context, name string, args ...string) (string, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return "connected\n", nil
		},
	}
	restoreAfterOneShot(s, "wlan0")
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"systemctl start NetworkManager",
		"nmcli networking on",
		"nmcli device set wlan0 managed yes",
		"ip link set wlan0 up",
		"rfkill unblock wifi",
		"nmcli -t general status",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing restore command %q in:\n%s", want, joined)
		}
	}

	calls = nil
	restoreAfterOneShot(s, "")
	joined = strings.Join(calls, "\n")
	if strings.Contains(joined, "device set") || strings.Contains(joined, "ip link set") {
		t.Fatalf("empty iface must skip interface-specific restore:\n%s", joined)
	}
}
