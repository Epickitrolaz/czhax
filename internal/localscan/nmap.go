package localscan

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type ScanMode int

const (
	ScanDiscovery ScanMode = iota
	ScanQuick
	ScanService
	ScanOS
	ScanTop1000
	ScanAllPorts
)

type ScanOption struct {
	Mode ScanMode
	Name string
	Flag string
	Desc string
	Warn bool
}

var ScanOptions = []ScanOption{
	{Mode: ScanDiscovery, Name: "Host Discovery", Flag: "-sn", Desc: "fast ping/ARP sweep"},
	{Mode: ScanQuick, Name: "Quick Ports", Flag: "-F", Desc: "top 100 TCP ports"},
	{Mode: ScanService, Name: "Service Scan", Flag: "-sV", Desc: "top 100 + versions"},
	{Mode: ScanOS, Name: "OS & Scripts", Flag: "-A", Desc: "OS, ver, scripts", Warn: true},
	{Mode: ScanTop1000, Name: "Top 1000 Ports", Flag: "1k", Desc: "default 1000 ports"},
	{Mode: ScanAllPorts, Name: "Full Port Scan", Flag: "-p-", Desc: "all 65535 TCP ports", Warn: true},
}

type Port struct {
	Number   int
	Protocol string
	State    string
	Service  string
	Product  string
	Version  string
}

func (p Port) Summary() string {
	svc := p.Service
	if svc == "" {
		svc = "?"
	}
	extra := strings.TrimSpace(strings.TrimSpace(p.Product) + " " + strings.TrimSpace(p.Version))
	if extra != "" {
		return fmt.Sprintf("%d/%s %s (%s)", p.Number, p.Protocol, svc, extra)
	}
	return fmt.Sprintf("%d/%s %s", p.Number, p.Protocol, svc)
}

type Host struct {
	IP       string
	Hostname string
	MAC      string
	Vendor   string
	OS       string
	Status   string
	Reason   string
	Ports    []Port
}

func (h Host) DisplayName() string {
	if h.Hostname != "" {
		return h.Hostname
	}
	if h.Vendor != "" {
		return h.Vendor
	}
	return h.IP
}

type ScanRequest struct {
	Iface  string
	Target string
	Mode   ScanMode
}

type ScanProgress struct {
	Task    string
	Percent float64
	HasPct  bool
	Line    string
	Hosts   []Host
}

type ScanResult struct {
	Target   string
	Mode     ScanMode
	Hosts    []Host
	Command  string
	Canceled bool
}

func execCmd(ctx context.Context, name string, args ...string) (string, error) {
	bin := resolveBin(name)
	c := exec.CommandContext(ctx, bin, args...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func resolveBin(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/sbin", "/usr/sbin", "/usr/bin", "/bin"} {
		cand := filepath.Join(dir, name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return name
}

// FindNmap locates the nmap binary on the system.
func FindNmap() string {
	if p, err := exec.LookPath("nmap"); err == nil {
		return p
	}
	for _, dir := range []string{"/usr/bin", "/usr/local/bin", "/bin", "/sbin", "/usr/sbin"} {
		cand := filepath.Join(dir, "nmap")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

func hasPasswordlessSudo() bool {
	if os.Geteuid() == 0 {
		return true
	}
	sudoBin := resolveBin("sudo")
	if sudoBin == "sudo" {
		if _, err := exec.LookPath("sudo"); err != nil {
			return false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, sudoBin, "-n", "true").Run() == nil
}

// BuildNmapArgs constructs the nmap CLI argument list for req.
func BuildNmapArgs(req ScanRequest, privileged bool) []string {
	var args []string
	switch req.Mode {
	case ScanDiscovery:
		args = append(args, "-sn")
	case ScanQuick:
		args = append(args, "-F", "-T4")
	case ScanService:
		args = append(args, "-sV", "--version-light", "-F", "-T4")
	case ScanOS:
		if privileged {
			args = append(args, "-A", "-T4", "--top-ports", "100")
		} else {
			// -O and traceroute require root; fall back to version + default scripts.
			args = append(args, "-sV", "-sC", "-T4", "--top-ports", "100")
		}
	case ScanTop1000:
		args = append(args, "-T4")
	case ScanAllPorts:
		args = append(args, "-p-", "-T4")
	default:
		args = append(args, "-sn")
	}

	if privileged && strings.TrimSpace(req.Iface) != "" {
		args = append(args, "-e", strings.TrimSpace(req.Iface))
	}
	args = append(args, "--stats-every", "1s", "-oX", "-", strings.TrimSpace(req.Target))
	return args
}

// RunScan executes nmap against req.Target and streams live progress updates.
func RunScan(ctx context.Context, req ScanRequest, onProgress func(ScanProgress)) (ScanResult, error) {
	nmapBin := FindNmap()
	if nmapBin == "" {
		return ScanResult{}, errors.New("nmap not installed (run: sudo apt install nmap)")
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return ScanResult{}, errors.New("no scan target")
	}

	priv := hasPasswordlessSudo()
	nmapArgs := BuildNmapArgs(req, priv)

	bin := nmapBin
	args := nmapArgs
	if os.Geteuid() != 0 && priv {
		bin = resolveBin("sudo")
		args = append([]string{"-n", nmapBin}, nmapArgs...)
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 3 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ScanResult{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ScanResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return ScanResult{}, err
	}

	var mu sync.Mutex
	var xmlBuf bytes.Buffer
	var errBuf bytes.Buffer
	var streamedHosts []Host
	var lastTask string
	var lastPct float64
	var hasPct bool

	emit := func(line string) {
		if onProgress == nil {
			return
		}
		mu.Lock()
		hostsCopy := append([]Host(nil), streamedHosts...)
		task := lastTask
		pct := lastPct
		hp := hasPct
		mu.Unlock()
		onProgress(ScanProgress{
			Task:    task,
			Percent: pct,
			HasPct:  hp,
			Line:    line,
			Hosts:   hostsCopy,
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Read stdout XML stream incrementally, extracting <taskprogress> and <host>...</host>.
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		var pending string
		for {
			n, rerr := stdout.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				mu.Lock()
				xmlBuf.WriteString(chunk)
				mu.Unlock()

				pending += chunk
				for {
					idx := strings.Index(pending, "<taskprogress ")
					if idx < 0 {
						break
					}
					end := strings.Index(pending[idx:], "/>")
					if end < 0 {
						break
					}
					tag := pending[idx : idx+end+2]
					pending = pending[idx+end+2:]
					if task, pct, ok := parseTaskProgressTag(tag); ok {
						mu.Lock()
						lastTask = task
						lastPct = pct
						hasPct = true
						mu.Unlock()
						emit(fmt.Sprintf("%s %.0f%%", task, pct))
					}
				}
				for {
					start := strings.Index(pending, "<host ")
					if start < 0 {
						start = strings.Index(pending, "<host>")
					}
					if start < 0 {
						break
					}
					end := strings.Index(pending[start:], "</host>")
					if end < 0 {
						break
					}
					hostXML := pending[start : start+end+len("</host>")]
					pending = pending[start+end+len("</host>"):]
					if h, ok := parseSingleHostXML(hostXML); ok {
						mu.Lock()
						streamedHosts = append(streamedHosts, h)
						mu.Unlock()
						line := h.IP
						if extra := hostBrief(h); extra != "" {
							line += " · " + extra
						}
						emit(line)
					}
				}
				if len(pending) > 65536 {
					pending = pending[len(pending)-32768:]
				}
			}
			if rerr != nil {
				break
			}
		}
	}()

	go func() {
		defer wg.Done()
		buf := make([]byte, 2048)
		var pending string
		for {
			n, rerr := stderr.Read(buf)
			if n > 0 {
				s := string(buf[:n])
				mu.Lock()
				errBuf.WriteString(s)
				mu.Unlock()
				pending += strings.ReplaceAll(s, "\r", "\n")
				for {
					idx := strings.IndexByte(pending, '\n')
					if idx < 0 {
						break
					}
					line := strings.TrimSpace(pending[:idx])
					pending = pending[idx+1:]
					if line != "" {
						emit(line)
					}
				}
			}
			if rerr != nil {
				break
			}
		}
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	res := ScanResult{
		Target:  target,
		Mode:    req.Mode,
		Command: "nmap " + strings.Join(nmapArgs, " "),
	}

	hosts, parseErr := ParseNmapXML(xmlBuf.Bytes())
	if parseErr == nil && len(hosts) > 0 {
		res.Hosts = hosts
	} else {
		mu.Lock()
		res.Hosts = append([]Host(nil), streamedHosts...)
		mu.Unlock()
		sortHosts(res.Hosts)
	}

	if ctx.Err() != nil {
		res.Canceled = true
		return res, ctx.Err()
	}
	if waitErr != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg != "" {
			lines := strings.Split(msg, "\n")
			msg = strings.TrimSpace(lines[len(lines)-1])
		}
		if msg == "" {
			msg = waitErr.Error()
		}
		return res, fmt.Errorf("nmap: %s", msg)
	}
	return res, nil
}

func hostBrief(h Host) string {
	if len(h.Ports) > 0 {
		return fmt.Sprintf("%d open", len(h.Ports))
	}
	if h.Hostname != "" {
		return h.Hostname
	}
	if h.Vendor != "" {
		return h.Vendor
	}
	return ""
}

type xmlNmapRun struct {
	Hosts []xmlHost `xml:"host"`
}

type xmlHost struct {
	Status    xmlStatus     `xml:"status"`
	Addresses []xmlAddress  `xml:"address"`
	Hostnames []xmlHostname `xml:"hostnames>hostname"`
	Ports     []xmlPort     `xml:"ports>port"`
	OSMatches []xmlOSMatch  `xml:"os>osmatch"`
}

type xmlStatus struct {
	State  string `xml:"state,attr"`
	Reason string `xml:"reason,attr"`
}

type xmlAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
	Vendor   string `xml:"vendor,attr"`
}

type xmlHostname struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

type xmlPort struct {
	Protocol string     `xml:"protocol,attr"`
	PortID   int        `xml:"portid,attr"`
	State    xmlStatus  `xml:"state"`
	Service  xmlService `xml:"service"`
}

type xmlService struct {
	Name    string `xml:"name,attr"`
	Product string `xml:"product,attr"`
	Version string `xml:"version,attr"`
}

type xmlOSMatch struct {
	Name     string `xml:"name,attr"`
	Accuracy string `xml:"accuracy,attr"`
}

type xmlTaskProgress struct {
	Task    string `xml:"task,attr"`
	Percent string `xml:"percent,attr"`
}

func parseTaskProgressTag(tag string) (string, float64, bool) {
	var tp xmlTaskProgress
	if err := xml.Unmarshal([]byte(tag), &tp); err != nil {
		return "", 0, false
	}
	pct, err := strconv.ParseFloat(strings.TrimSpace(tp.Percent), 64)
	if err != nil {
		return "", 0, false
	}
	task := strings.TrimSpace(tp.Task)
	if task == "" {
		task = "Scan"
	}
	return task, pct, true
}

func parseSingleHostXML(raw string) (Host, bool) {
	var xh xmlHost
	if err := xml.Unmarshal([]byte(raw), &xh); err != nil {
		return Host{}, false
	}
	return convertXMLHost(xh)
}

// ParseNmapXML extracts live hosts and their ports/OS/MAC details from nmap `-oX -` output.
func ParseNmapXML(data []byte) ([]Host, error) {
	var run xmlNmapRun
	if err := xml.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	out := make([]Host, 0, len(run.Hosts))
	for _, xh := range run.Hosts {
		if h, ok := convertXMLHost(xh); ok {
			out = append(out, h)
		}
	}
	sortHosts(out)
	return out, nil
}

func convertXMLHost(xh xmlHost) (Host, bool) {
	if xh.Status.State != "" && !strings.EqualFold(xh.Status.State, "up") {
		return Host{}, false
	}
	h := Host{
		Status: xh.Status.State,
		Reason: xh.Status.Reason,
	}
	if h.Status == "" {
		h.Status = "up"
	}
	for _, a := range xh.Addresses {
		switch strings.ToLower(a.AddrType) {
		case "ipv4":
			if h.IP == "" {
				h.IP = a.Addr
			}
		case "ipv6":
			if h.IP == "" {
				h.IP = a.Addr
			}
		case "mac":
			h.MAC = strings.ToUpper(a.Addr)
			if a.Vendor != "" {
				h.Vendor = a.Vendor
			}
		}
	}
	if h.IP == "" {
		return Host{}, false
	}
	for _, hn := range xh.Hostnames {
		if hn.Name != "" {
			h.Hostname = hn.Name
			break
		}
	}
	for _, p := range xh.Ports {
		if !strings.EqualFold(p.State.State, "open") {
			continue
		}
		h.Ports = append(h.Ports, Port{
			Number:   p.PortID,
			Protocol: p.Protocol,
			State:    p.State.State,
			Service:  p.Service.Name,
			Product:  p.Service.Product,
			Version:  p.Service.Version,
		})
	}
	sort.Slice(h.Ports, func(i, j int) bool {
		if h.Ports[i].Number != h.Ports[j].Number {
			return h.Ports[i].Number < h.Ports[j].Number
		}
		return h.Ports[i].Protocol < h.Ports[j].Protocol
	})
	if len(xh.OSMatches) > 0 {
		h.OS = xh.OSMatches[0].Name
	}
	return h, true
}

func sortHosts(hosts []Host) {
	sort.Slice(hosts, func(i, j int) bool {
		a := net.ParseIP(hosts[i].IP)
		b := net.ParseIP(hosts[j].IP)
		if a4, b4 := a.To4(), b.To4(); a4 != nil && b4 != nil {
			return bytes.Compare(a4, b4) < 0
		}
		return hosts[i].IP < hosts[j].IP
	})
}
