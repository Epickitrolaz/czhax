package wifi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// findTool resolves a binary even when sbin is missing from PATH, which is the
// case for unprivileged shells on the Cardputer (iw, iptables, hostapd and
// dnsmasq all live in /usr/sbin).
func findTool(name string) string {
	if strings.ContainsRune(name, '/') {
		if st, err := os.Stat(name); err == nil && !st.IsDir() {
			return name
		}
		return ""
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/usr/sbin", "/sbin", "/usr/local/sbin", "/usr/bin", "/bin", "/usr/local/bin"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// phyOf returns the wiphy name backing an interface (e.g. "phy5"), or "".
func phyOf(iface string) string {
	if link, err := os.Readlink(filepath.Join("/sys/class/net", iface, "phy80211")); err == nil {
		return filepath.Base(link)
	}
	return ""
}

// APCapable reports whether the interface's driver advertises AP mode. Knowing
// this up front avoids hostapd failing with a bare "Unable to setup interface"
// on adapters that simply cannot host a soft AP (rtw88 USB, for instance).
func APCapable(iface string) bool {
	iw := findTool("iw")
	phy := phyOf(iface)
	if iw == "" || phy == "" {
		// Unknown: do not block the user.
		return true
	}
	out, err := exec.Command(iw, "phy", phy, "info").Output()
	if err != nil {
		return true
	}
	return strings.Contains(string(out), "* AP")
}

// APInterfaces filters ifaces down to the ones that can host a soft AP.
func APInterfaces(ifaces []Interface) []Interface {
	out := make([]Interface, 0, len(ifaces))
	for _, inf := range ifaces {
		if APCapable(inf.Name) {
			out = append(out, inf)
		}
	}
	return out
}
