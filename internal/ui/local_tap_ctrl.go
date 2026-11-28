package ui

import (
	"context"
	"os"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/workdir"
)

type localTapLineMsg struct {
	seq  int
	line string
}

type localTapDoneMsg struct {
	seq int
	err error
}

func runLocalTapCmd(ctx context.Context, seq int, args []string, ch chan string) tea.Cmd {
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
		return localTapDoneMsg{seq: seq, err: err}
	}
}

func waitLocalTapLineCmd(seq int, ch <-chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return localTapLineMsg{seq: seq, line: line}
	}
}

func (m AppModel) startLocalTap() (tea.Model, tea.Cmd) {
	ctx, seq, cfg, ch, err := m.LocalTap.Start(workdir.Captures(m.WorkDir))
	if err != nil {
		return m, m.setStatus("Tap: " + err.Error())
	}
	args := []string{"local", "tap",
		"--port-a", cfg.PortA,
		"--port-b", cfg.PortB,
		"--captures", cfg.CaptureDir,
		"--uid", strconv.Itoa(os.Getuid()),
		"--gid", strconv.Itoa(os.Getgid()),
	}
	if cfg.Record {
		args = append(args, "--record")
	}
	if cfg.Spoof {
		args = append(args, "--spoof", "--portal", cfg.Portal, "--subnet", cfg.Subnet, "--portal-file", cfg.PortalDoc)
	}
	return m, tea.Batch(
		m.setStatus("Tap "+cfg.PortA+" <-> "+cfg.PortB),
		runLocalTapCmd(ctx, seq, args, ch),
		waitLocalTapLineCmd(seq, ch),
		wifiHelperTickCmd(),
	)
}

func (m AppModel) handleLocalTapKey(k string) (tea.Model, tea.Cmd) {
	v := &m.LocalTap
	if v.Mode == tapModeRunning {
		if k == "esc" || k == "q" {
			v.Stop()
			return m, m.setStatus("Stopping tap...")
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
	case k == "p":
		v.Record = !v.Record
	case k == "d":
		v.Spoof = !v.Spoof
		v.Cursor = clampIndex(v.Cursor, len(v.fields()))
	case k == "enter" || k == " ":
		f := v.fields()
		if f[clampIndex(v.Cursor, len(f))] == tapFieldStart {
			return m.startLocalTap()
		}
		v.Adjust(1)
	}
	return m, nil
}

func (m AppModel) localTapHints() []Hint {
	if m.LocalTap.Mode == tapModeRunning {
		return []Hint{{Key: "Esc", Desc: "stop"}}
	}
	return []Hint{
		{Key: "↑↓", Desc: "sel"},
		{Key: "←→", Desc: "chg"},
		{Key: "⏎", Desc: "start"},
		{Key: "Esc", Desc: "tabs"},
	}
}
