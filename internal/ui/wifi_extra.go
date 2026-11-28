package ui

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/workdir"
)

type wifiSpamLineMsg struct {
	seq  int
	line string
}

type wifiSpamDoneMsg struct {
	seq int
	err error
}

type wifiEvilLineMsg struct {
	seq  int
	line string
}

type wifiEvilDoneMsg struct {
	seq int
	err error
}

type wifiHelperTickMsg struct{}

// streamPrivileged runs this binary (through sudo when needed) and forwards
// every output line, so the beacon / twin helpers can report progress.
func streamPrivileged(ctx context.Context, args []string, onLine func(string)) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		cmd = exec.CommandContext(ctx, exe, args...)
	} else {
		full := append([]string{"-n", exe}, args...)
		cmd = exec.CommandContext(ctx, "sudo", full...)
	}
	// Kill the whole helper process group so stopping a run also stops
	// hostapd/dnsmasq/hcxdumptool (and the sudo parent).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 3 * time.Second
	// Hold stdin open: a helper that reads it must never see EOF and cancel
	// itself the moment it starts.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && onLine != nil {
			onLine(line)
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && err.Error() == "exit status 1" {
		return err
	}
	return err
}

func runWifiSpamCmd(ctx context.Context, seq int, args []string, ch chan string) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		err := streamPrivileged(ctx, args, func(line string) {
			select {
			case ch <- line:
			default:
			}
		})
		return wifiSpamDoneMsg{seq: seq, err: err}
	}
}

func waitWifiSpamLineCmd(seq int, ch <-chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return wifiSpamLineMsg{seq: seq, line: line}
	}
}

func runWifiEvilCmd(ctx context.Context, seq int, args []string, ch chan string) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		err := streamPrivileged(ctx, args, func(line string) {
			select {
			case ch <- line:
			default:
			}
		})
		return wifiEvilDoneMsg{seq: seq, err: err}
	}
}

func waitWifiEvilLineCmd(seq int, ch <-chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return wifiEvilLineMsg{seq: seq, line: line}
	}
}

func wifiHelperTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return wifiHelperTickMsg{} })
}

// --- Beacon spam ---

func (m AppModel) startWifiSpam() (tea.Model, tea.Cmd) {
	tmp := workdir.Temp(workdir.Resolve(m.Cfg.WorkDir))
	ctx, seq, ssids, ch, err := m.WifiSpam.Start(tmp)
	if err != nil {
		return m, m.setStatus("Beacon spam: " + err.Error())
	}
	args := []string{"wifi", "spam",
		"--iface", m.WifiSpam.EffectiveIface(),
		"--channel", strconv.Itoa(m.WifiSpam.Channel),
		"--interval", strconv.Itoa(m.WifiSpam.IntervalMS),
	}
	if m.WifiSpam.RandomMAC {
		args = append(args, "--random-mac")
	}
	switch m.WifiSpam.Source {
	case SpamSourceFile:
		args = append(args, "--file", strings.TrimSpace(m.WifiSpam.FilePath))
	case SpamSourceNumbered:
		args = append(args, "--prefix", m.WifiSpam.Prefix, "--count", strconv.Itoa(len(ssids)))
	default:
		args = append(args, "--ssid", ssids[0])
	}
	return m, tea.Batch(
		m.setStatus("Beacon spam: "+strconv.Itoa(len(ssids))+" SSIDs"),
		runWifiSpamCmd(ctx, seq, args, ch),
		waitWifiSpamLineCmd(seq, ch),
		wifiHelperTickCmd(),
	)
}

func (m AppModel) handleWifiSpamKey(k string) (tea.Model, tea.Cmd) {
	v := &m.WifiSpam
	if v.Mode == spamModeRunning {
		if k == "esc" || k == "q" {
			v.Stop()
			return m, m.setStatus("Stopping beacon spam...")
		}
		return m, nil
	}
	switch {
	case m.isUp(k):
		v.MoveCursor(-1)
	case m.isDown(k):
		v.MoveCursor(1)
	case m.isLeft(k):
		v.Adjust(-1)
	case m.isRight(k):
		v.Adjust(1)
	case k == "enter":
		if v.BeginEdit() {
			return m, nil
		}
		if v.fields()[clampIndex(v.Cursor, len(v.fields()))] == spamFieldStart {
			return m.startWifiSpam()
		}
	}
	return m, nil
}

func (m AppModel) handleWifiSpamTextKey(k string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.WifiSpam
	switch k {
	case "esc":
		v.Mode = spamModeSetup
		return m, nil
	case "enter":
		v.CommitEdit()
		return m, nil
	case "backspace":
		rs := []rune(v.Input)
		if len(rs) > 0 {
			v.Input = string(rs[:len(rs)-1])
		}
		return m, nil
	}
	if msg.Type == tea.KeySpace {
		v.Input += " "
	} else if len(msg.Runes) > 0 {
		v.Input += string(msg.Runes)
	}
	return m, nil
}

// --- Evil twin ---

// startWifiEvil launches the twin from the Evil tab.
func (m AppModel) startWifiEvil() (tea.Model, tea.Cmd) {
	return m.launchWifiEvil()
}

// openWifiEvilFromScan carries the selected network over to the Evil tab with
// everything else left at its default, so the user starts it themselves.
func (m AppModel) openWifiEvilFromScan() (tea.Model, tea.Cmd) {
	net, ok := m.WifiScan.SelectedNetwork()
	if !ok {
		return m, m.setStatus("Select a network first")
	}
	if strings.TrimSpace(net.SSID) == "" {
		return m, m.setStatus("Hidden network: no SSID to clone")
	}
	m.WifiEvil.Prefill(net)
	m.ActiveTabs[ViewWiFi] = WiFiTabEvil
	m.Focus = FocusContent
	m.WifiEvil.Cursor = 0
	return m, m.setStatus("Twin ready: " + net.SSID)
}

func (m AppModel) launchWifiEvil() (tea.Model, tea.Cmd) {
	cfg := m.WifiEvil.Config()
	tmp := workdir.Temp(workdir.Resolve(m.Cfg.WorkDir))
	caps := workdir.Captures(workdir.Resolve(m.Cfg.WorkDir))
	ctx, seq, cfg, ch, err := m.WifiEvil.Start(tmp, caps)
	if err != nil {
		return m, m.setStatus("Evil twin: " + err.Error())
	}
	args := []string{"wifi", "evil",
		"--iface", cfg.Iface,
		"--ssid", cfg.SSID,
		"--channel", strconv.Itoa(cfg.Channel),
		"--work-dir", tmp,
		"--capture-dir", caps,
		"--uid", strconv.Itoa(os.Getuid()),
		"--gid", strconv.Itoa(os.Getgid()),
	}
	if cfg.UplinkIface != "" {
		args = append(args, "--uplink", cfg.UplinkIface)
	}
	if cfg.NAT {
		args = append(args, "--nat")
	}
	if cfg.Portal {
		args = append(args, "--portal")
	}
	if cfg.PMKID {
		args = append(args, "--pmkid")
	}
	if cfg.DeauthBSSID != "" {
		args = append(args, "--deauth", cfg.DeauthBSSID)
	}
	return m, tea.Batch(
		m.setStatus("Evil twin: "+cfg.SSID),
		runWifiEvilCmd(ctx, seq, args, ch),
		waitWifiEvilLineCmd(seq, ch),
		wifiHelperTickCmd(),
	)
}

func (m AppModel) handleWifiEvilKey(k string) (tea.Model, tea.Cmd) {
	v := &m.WifiEvil
	if v.Mode == evilModeRunning {
		if k == "esc" || k == "q" {
			v.Stop()
			return m, m.setStatus("Stopping evil twin...")
		}
		return m, nil
	}
	if v.Mode == evilModeDNS {
		switch {
		case m.isUp(k):
			if n := len(v.FilteredDNSRules()); n > 0 {
				v.Cursor = (v.Cursor - 1 + n) % n
			}
		case m.isDown(k):
			if n := len(v.FilteredDNSRules()); n > 0 {
				v.Cursor = (v.Cursor + 1) % n
			}
		case k == "a" || k == "n":
			v.BeginDNSEdit()
			return m, nil
		case k == "x" || k == "delete" || k == "backspace":
			if v.DeleteDNSRule() {
				return m, m.setStatus("DNS rule deleted")
			}
		case k == "enter":
			v.BeginDNSEdit()
			return m, nil
		}
		return m, nil
	}
	switch {
	case m.isUp(k):
		v.MoveCursor(-1)
	case m.isDown(k):
		v.MoveCursor(1)
	case m.isLeft(k):
		v.Adjust(-1)
	case m.isRight(k):
		v.Adjust(1)
	case k == "enter":
		if v.BeginEdit() {
			return m, nil
		}
		switch v.fields()[clampIndex(v.Cursor, len(v.fields()))] {
		case evilFieldStart:
			return m.startWifiEvil()
		case evilFieldDNS:
			v.BeginDNS()
			return m, nil
		case evilFieldPortal:
			v.Adjust(1)
			return m, m.resetMarquee()
		}
	}
	return m, nil
}

func (m AppModel) handleWifiEvilTextKey(k string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.WifiEvil
	if v.Mode == evilModeDNSEdit {
		switch k {
		case "esc":
			v.Mode = evilModeDNS
			v.Input = ""
			v.InputErr = ""
			return m, nil
		case "enter":
			if v.CommitDNSEdit() {
				return m, m.setStatus("DNS rule saved")
			}
			return m, nil
		case "backspace":
			rs := []rune(v.Input)
			if len(rs) > 0 {
				v.Input = string(rs[:len(rs)-1])
				v.InputErr = ""
			}
			return m, nil
		}
		if len(msg.Runes) > 0 {
			v.Input += string(msg.Runes)
			v.InputErr = ""
		}
		return m, nil
	}
	switch k {
	case "esc":
		v.Mode = evilModeSetup
		return m, nil
	case "enter":
		v.CommitEdit()
		return m, nil
	case "backspace":
		rs := []rune(v.Input)
		if len(rs) > 0 {
			v.Input = string(rs[:len(rs)-1])
		}
		return m, nil
	}
	if msg.Type == tea.KeySpace {
		v.Input += " "
	} else if len(msg.Runes) > 0 {
		v.Input += string(msg.Runes)
	}
	return m, nil
}

// wifiTabHints returns the footer hints for the Spam / Evil subtabs.
func (m AppModel) wifiTabHints() []Hint {
	switch m.ActiveTabs[ViewWiFi] {
	case WiFiTabSpam:
		if m.WifiSpam.Mode == spamModeRunning {
			return []Hint{{Key: "Esc", Desc: "stop"}}
		}
		if m.WifiSpam.IsTextInput() {
			return []Hint{{Key: "⏎", Desc: "save"}, {Key: "Esc", Desc: "cancel"}}
		}
		return []Hint{
			{Key: "↑↓", Desc: "sel"},
			{Key: "←→", Desc: "chg"},
			{Key: "⏎", Desc: "ok"},
			{Key: "Esc", Desc: "tabs"},
		}
	case WiFiTabEvil:
		if m.WifiEvil.Mode == evilModeRunning {
			return []Hint{{Key: "Esc", Desc: "stop"}}
		}
		if m.WifiEvil.IsTextInput() {
			return []Hint{{Key: "⏎", Desc: "save"}, {Key: "Esc", Desc: "cancel"}}
		}
		if m.WifiEvil.Mode == evilModeDNS {
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "a", Desc: "add"}, {Key: "x", Desc: "del"}, {Key: "Esc", Desc: "back"}}
		}
		return []Hint{
			{Key: "↑↓", Desc: "sel"},
			{Key: "←→", Desc: "chg"},
			{Key: "⏎", Desc: "ok"},
			{Key: "Esc", Desc: "tabs"},
		}
	}
	return nil
}
