package ui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type localDHCPLineMsg struct {
	seq  int
	line string
}

type localDHCPDoneMsg struct {
	seq int
	err error
}

type localDHCPTickMsg struct{}

func runLocalDHCPCmd(ctx context.Context, seq int, args []string, ch chan string) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		err := streamPrivileged(ctx, args, func(line string) {
			select {
			case ch <- line:
			default:
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- line:
				default:
				}
			}
		})
		return localDHCPDoneMsg{seq: seq, err: err}
	}
}

func waitLocalDHCPLineCmd(seq int, ch <-chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return localDHCPLineMsg{seq: seq, line: line}
	}
}

func localDHCPTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return localDHCPTickMsg{} })
}

func (m AppModel) startLocalDHCP() (tea.Model, tea.Cmd) {
	ctx, seq, cfg, ch, err := m.LocalDHCP.Start()
	if err != nil {
		return m, m.setStatus("DHCP: " + err.Error())
	}
	args := []string{"local", "dhcp",
		"--iface", cfg.Iface,
		"--rate", strconv.Itoa(cfg.Rate),
		"--count", strconv.Itoa(cfg.Count),
	}
	if cfg.RandomMAC {
		args = append(args, "--random-mac")
	}
	label := "probe"
	if cfg.Count == 0 {
		label = "flood"
	}
	return m, tea.Batch(
		m.setStatus("DHCP "+label+" on "+cfg.Iface),
		runLocalDHCPCmd(ctx, seq, args, ch),
		waitLocalDHCPLineCmd(seq, ch),
		localDHCPTickCmd(),
	)
}

func (m AppModel) handleLocalDHCPKey(k string) (tea.Model, tea.Cmd) {
	v := &m.LocalDHCP
	if v.Mode == dhcpModeRunning {
		if k == "esc" || k == "q" {
			v.Stop()
			return m, m.setStatus("Stopping DHCP run...")
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
	case k == "m":
		v.ToggleMode()
		return m, m.resetMarquee()
	case k == "enter" || k == " ":
		f := v.fields()
		switch f[clampIndex(v.Cursor, len(f))] {
		case dhcpFieldIface:
			v.Adjust(1)
		case dhcpFieldStart:
			return m.startLocalDHCP()
		default:
			v.Adjust(1)
		}
	}
	return m, nil
}

func (m AppModel) localDHCPHints() []Hint {
	v := m.LocalDHCP
	if v.Mode == dhcpModeRunning {
		return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "q", Desc: "stop"}}
	}
	return []Hint{
		{Key: "↑↓", Desc: "sel"},
		{Key: "←→", Desc: "chg"},
		{Key: "m", Desc: "probe/flood"},
		{Key: "⏎", Desc: "run"},
		{Key: "Esc", Desc: "tabs"},
	}
}

// localDHCPNoteText is a short summary used in the status line after a run.
func localDHCPNoteText(line string) string {
	return strings.TrimSpace(line)
}
