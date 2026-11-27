package audio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Player is a controllable audio player. When mpv is available it drives mpv
// over its JSON IPC socket, which allows live pause, seeking and volume changes.
// Other players (ffplay/aplay) are used as a fire-and-forget fallback.
type Player struct {
	Path   string
	Volume int

	mu       sync.Mutex
	cmd      *exec.Cmd
	sock     string
	conn     net.Conn
	reader   *bufio.Reader
	reqID    int
	mpv      bool
	closed   bool
	waitOnce sync.Once
	waitErr  error
	waitDone chan struct{}
}

// StartPlayer starts playback of path. volume is a percentage (0-100); pass a
// negative value to leave the player's own volume untouched.
func StartPlayer(ctx context.Context, path string, volume int) (*Player, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	p := &Player{Path: path, Volume: volume, waitDone: make(chan struct{})}

	if bin, err := exec.LookPath("mpv"); err == nil {
		p.sock = filepath.Join(os.TempDir(), fmt.Sprintf("czhax-mpv-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
		_ = os.Remove(p.sock)
		args := []string{
			"--really-quiet", "--no-terminal", "--no-video", "--no-input-terminal",
			"--input-ipc-server=" + p.sock,
		}
		if volume >= 0 {
			args = append(args, fmt.Sprintf("--volume=%d", clampPct(volume)))
		}
		args = append(args, "--", path)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("mpv: %w", err)
		}
		p.cmd = cmd
		p.mpv = true
		return p, nil
	}

	if bin, err := exec.LookPath("ffplay"); err == nil {
		args := []string{"-loglevel", "quiet", "-nodisp", "-autoexit"}
		if volume >= 0 {
			args = append(args, "-volume", strconv.Itoa(clampPct(volume)))
		}
		args = append(args, path)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("ffplay: %w", err)
		}
		p.cmd = cmd
		return p, nil
	}

	if bin, err := exec.LookPath("aplay"); err == nil && strings.EqualFold(filepath.Ext(path), ".wav") {
		cmd := exec.CommandContext(ctx, bin, "-q", path)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("aplay: %w", err)
		}
		p.cmd = cmd
		return p, nil
	}
	return nil, errors.New("no audio player found (install mpv, ffmpeg, or alsa-utils)")
}

func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// Controllable reports whether pause/seek/volume commands are supported.
func (p *Player) Controllable() bool { return p != nil && p.mpv }

// Wait blocks until playback finishes and returns the process error.
func (p *Player) Wait() error {
	if p == nil || p.cmd == nil {
		return nil
	}
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		close(p.waitDone)
	})
	<-p.waitDone
	if p.waitErr != nil {
		if ee, ok := p.waitErr.(*exec.ExitError); ok {
			// mpv/ffplay exit with 1 or on SIGTERM when stopped from the UI.
			if code := ee.ExitCode(); code == 0 || code == 1 || code == -1 {
				return nil
			}
		}
		return p.waitErr
	}
	return nil
}

// Close stops playback and releases resources.
func (p *Player) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	closed := p.closed
	p.closed = true
	conn := p.conn
	p.conn = nil
	sock := p.sock
	cmd := p.cmd
	p.mu.Unlock()
	if closed {
		return
	}
	if conn != nil {
		_ = conn.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	if sock != "" {
		_ = os.Remove(sock)
	}
}

// connect dials the mpv IPC socket, retrying while mpv starts up.
func (p *Player) connect() (net.Conn, *bufio.Reader, error) {
	if !p.mpv {
		return nil, nil, errors.New("player does not support remote control")
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, nil, errors.New("player closed")
	}
	if p.conn != nil {
		conn, rd := p.conn, p.reader
		p.mu.Unlock()
		return conn, rd, nil
	}
	sock := p.sock
	p.mu.Unlock()

	deadline := time.Now().Add(1500 * time.Millisecond)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", sock, 200*time.Millisecond)
		if err == nil {
			rd := bufio.NewReader(conn)
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				_ = conn.Close()
				return nil, nil, errors.New("player closed")
			}
			p.conn, p.reader = conn, rd
			p.mu.Unlock()
			return conn, rd, nil
		}
		lastErr = err
		time.Sleep(40 * time.Millisecond)
	}
	return nil, nil, fmt.Errorf("mpv ipc: %w", lastErr)
}

// command sends one JSON IPC command and returns the decoded result.
func (p *Player) command(args ...any) (any, error) {
	conn, rd, err := p.connect()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.reqID++
	id := p.reqID
	p.mu.Unlock()

	msg := map[string]any{"command": args, "request_id": id}
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := conn.Write(append(b, '\n')); err != nil {
		p.dropConn()
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(700 * time.Millisecond))
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			p.dropConn()
			return nil, err
		}
		var resp struct {
			RequestID *int `json:"request_id"`
			Error     string
			Data      any
		}
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.RequestID == nil || *resp.RequestID != id {
			continue // event or stale reply
		}
		if resp.Error != "" && resp.Error != "success" {
			return nil, errors.New(resp.Error)
		}
		return resp.Data, nil
	}
}

func (p *Player) dropConn() {
	p.mu.Lock()
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.conn = nil
	p.reader = nil
	p.mu.Unlock()
}

// TogglePause flips the pause state and reports the new value.
func (p *Player) TogglePause() (bool, error) {
	data, err := p.command("get_property", "pause")
	if err != nil {
		return false, err
	}
	paused, _ := data.(bool)
	next := !paused
	if _, err := p.command("set_property", "pause", next); err != nil {
		return paused, err
	}
	return next, nil
}

// SetPaused sets the pause state explicitly.
func (p *Player) SetPaused(paused bool) error {
	_, err := p.command("set_property", "pause", paused)
	return err
}

// Seek moves the playback position by delta seconds.
func (p *Player) Seek(delta float64) error {
	_, err := p.command("seek", delta, "relative")
	return err
}

// SetVolume sets the player volume percentage.
func (p *Player) SetVolume(pct int) error {
	p.mu.Lock()
	p.Volume = clampPct(pct)
	p.mu.Unlock()
	_, err := p.command("set_property", "volume", clampPct(pct))
	return err
}

// Status returns position, duration and pause state in seconds.
func (p *Player) Status() (pos, dur float64, paused bool, err error) {
	if !p.mpv {
		return 0, 0, false, errors.New("not controllable")
	}
	if v, err := p.command("get_property", "time-pos"); err == nil {
		pos, _ = v.(float64)
	} else {
		return 0, 0, false, err
	}
	if v, err := p.command("get_property", "duration"); err == nil {
		dur, _ = v.(float64)
	}
	if v, err := p.command("get_property", "pause"); err == nil {
		paused, _ = v.(bool)
	}
	return pos, dur, paused, nil
}
