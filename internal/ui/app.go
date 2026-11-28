package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/audio"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/boot"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/hwctl"
	"github.com/czhax/czhax/internal/ir"
	"github.com/czhax/czhax/internal/localscan"
	"github.com/czhax/czhax/internal/power"
	"github.com/czhax/czhax/internal/sdr"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/updater"
	"github.com/czhax/czhax/internal/wifi"
	"github.com/czhax/czhax/internal/workdir"
)

type FocusZone int

const (
	FocusTabs FocusZone = iota
	FocusContent
)

type tickMsg time.Time
type clearStatusMsg struct{}
type startupRedrawMsg struct{ step int }
type marqueeStartMsg struct{ gen int }
type marqueeTickMsg struct{ gen int }

// startupRedrawDelays schedules full repaints while the system is still
// settling after boot. Kernel/systemd console messages write straight to the
// VT and shift the TUI; a clear+repaint restores it until things go quiet.
var startupRedrawDelays = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	9 * time.Second,
	14 * time.Second,
	20 * time.Second,
	30 * time.Second,
	45 * time.Second,
	60 * time.Second,
}

func scheduleStartupRedraw(step int) tea.Cmd {
	if step < 0 || step >= len(startupRedrawDelays) {
		return nil
	}
	return tea.Tick(startupRedrawDelays[step], func(time.Time) tea.Msg {
		return startupRedrawMsg{step: step}
	})
}

type updateDoneMsg struct {
	res updater.Result
	err error
}

type shellExitedMsg struct {
	err error
}

type sudoUpdateDoneMsg struct {
	err error
}

type bootToggledMsg struct {
	state boot.State
	err   error
}

type sleepDoneMsg struct {
	err error
}

type powerActionDoneMsg struct {
	action string
	err    error
}

type wifiRefreshedMsg struct {
	ifaces []wifi.Interface
}

type wifiNetworksScannedMsg struct {
	iface string
	nets  []wifi.Network
	err   error
}

type wifiConnectDoneMsg struct {
	req wifi.ConnectRequest
	err error
}

type wifiPentestDoneMsg struct {
	res wifi.PentestResult
	err error
}

type wifiPentestProgressMsg struct {
	res wifi.PentestResult
}

type wifiPentestTickMsg struct{}

type wpsScanDoneMsg struct {
	iface string
	infos map[string]wifi.WPSInfo
	err   error
}

type wpsRunLineMsg struct {
	seq  int
	line string
}

type wpsRunDoneMsg struct {
	seq int
	err error
}

type irCaptureDoneMsg struct {
	seq int
	sig ir.Signal
	err error
}

type irSendDoneMsg struct {
	name string
	err  error
}

type irIndexDoneMsg struct {
	entries []ir.Entry
	err     error
}

type irRemoteLoadedMsg struct {
	remote ir.Remote
	err    error
}

type irLearnTickMsg struct{}

type irMatchCandidatesMsg struct {
	cands []ir.MatchCandidate
	err   error
}

type irMatchSavedMsg struct {
	profile ir.Profile
	err     error
}

type irUniversalProgressMsg struct {
	seq  int
	prog ir.UniversalProgress
}

type irUniversalDoneMsg struct {
	seq        int
	target     string
	uniqueSent int
	lastBrand  string
	err        error
}

type irUnivTickMsg struct{}

type bluetoothRefreshedMsg struct {
	adapters []bluetooth.Adapter
}

type btDevicesScannedMsg struct {
	hci     string
	mode    bluetooth.ScanMode
	devices []bluetooth.Device
	err     error
}

type btDeviceInfoMsg struct {
	addr string
	info bluetooth.Device
	err  error
}

type btAttackLineMsg struct {
	seq  int
	line string
}

type btAttackDoneMsg struct {
	seq  int
	kind bluetooth.AttackKind
	err  error
}

type btAttackTickMsg struct{}

type filesPlayDoneMsg struct {
	err error
}

type filesAudioDoneMsg struct {
	seq int
	err error
}

type filesPlayTickMsg struct {
	seq    int
	pos    float64
	dur    float64
	paused bool
	err    error
}

type filesRecordDoneMsg struct {
	seq int
	err error
}

type filesRecordTickMsg struct{}

type btMicDoneMsg struct {
	seq  int
	path string
	err  error
}

type btMicTickMsg struct{}

type btConvertDoneMsg struct {
	path string
	err  error
}

type btSudoAuthMsg struct {
	kind BTActionKind
	opts BTActionOptions
	err  error
}

type btEmuEventMsg struct {
	seq int
	ev  bluetooth.EmuEvent
}

type btEmuDoneMsg struct {
	seq int
	err error
}

type btEmuSudoAuthMsg struct {
	err error
}

type btEmuPlayDoneMsg struct {
	seq int
	err error
}

type btEmuPlayTickMsg struct{}

type localIfacesMsg struct {
	ifaces []localscan.Interface
}

type localScanProgressMsg struct {
	seq  int
	prog localscan.ScanProgress
}

type localScanDoneMsg struct {
	seq int
	res localscan.ScanResult
	err error
}

type localScanTickMsg struct{}

type hwEventMsg struct {
	ev hwctl.Event
	ch <-chan hwctl.Event
}

type osdHideMsg struct{ seq int }

type AppModel struct {
	Cfg        config.Config
	ConfigPath string
	Version    string
	Snap       sysinfo.Snapshot

	TermW int
	TermH int

	ActiveView ViewID
	ActiveTabs [viewCount]int
	Focus      FocusZone

	Status        StatusView
	System        SystemView
	Wifi          WifiView
	WifiScan      WifiScanView
	WifiSpam      WifiSpamView
	WifiEvil      WifiEvilView
	Local         LocalView
	IR            IRView
	SDR           SDRView
	Bluetooth     BluetoothView
	BluetoothScan BluetoothScanView
	BluetoothEmu  BluetoothEmuView
	Files         FilesView
	Help          HelpView
	WorkDir       string

	ShowHelp      bool
	ConfirmExit   bool
	RestartOnQuit bool
	StatusMsg     string

	MarqueeGen   int
	MarqueeStep  int
	OSD          OSDState
	OSDMuteUntil time.Time
}

func NewAppModel(cfg config.Config, cfgPath, version string) AppModel {
	if version == "" {
		version = "dev"
	}
	root := workdir.Resolve(cfg.WorkDir)
	_ = workdir.Ensure(root)
	m := AppModel{
		Cfg:           cfg,
		ConfigPath:    cfgPath,
		Version:       version,
		Snap:          sysinfo.Collect(),
		TermW:         53,
		TermH:         14,
		ActiveView:    ViewStatus,
		Focus:         FocusTabs,
		System:        NewSystemView(),
		Wifi:          NewWifiView(),
		WifiScan:      NewWifiScanView(),
		WifiSpam:      NewWifiSpamView(),
		WifiEvil:      NewWifiEvilView(),
		Local:         NewLocalView(),
		IR:            NewIRView(cfgPath),
		SDR:           NewSDRView(root, workdir.Captures(root), sdr.DefaultBookmarksPath()),
		Bluetooth:     NewBluetoothView(),
		BluetoothScan: NewBluetoothScanView(),
		BluetoothEmu:  NewBluetoothEmuView(),
		Files:         NewFilesView(root),
		WorkDir:       root,
	}
	m.BluetoothScan.WorkDir = root
	m.BluetoothEmu.WorkDir = root
	m.WifiScan.ColorMode = cfg.WiFiColor
	m.WifiScan.SetOneShotRefresh(cfg.OneShotDir)
	if cfg.IRFlipperDir != "" {
		m.IR.Roots.Flipper = cfg.IRFlipperDir
	}
	if cfg.IRMiDir != "" {
		m.IR.Roots.Mi = cfg.IRMiDir
	}
	return m
}

func marqueeStartCmd(gen int) tea.Cmd {
	return tea.Tick(marqueeDelay, func(time.Time) tea.Msg {
		return marqueeStartMsg{gen: gen}
	})
}

func marqueeTickCmd(gen int) tea.Cmd {
	return tea.Tick(marqueeInterval, func(time.Time) tea.Msg {
		return marqueeTickMsg{gen: gen}
	})
}

func (m *AppModel) resetMarquee() tea.Cmd {
	m.MarqueeStep = 0
	m.MarqueeGen++
	return marqueeStartCmd(m.MarqueeGen)
}

func (m AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.scheduleTick(), collectWifiCmd(), collectBluetoothCmd(), collectLocalIfacesCmd(), probeSDRCmd(false), startHWWatcherCmd(), marqueeStartCmd(m.MarqueeGen)}
	if isLinuxConsole() {
		cmds = append(cmds, scheduleStartupRedraw(0))
	}
	return tea.Batch(cmds...)
}

func isLinuxConsole() bool {
	return os.Getenv("TERM") == "linux"
}

func collectWifiCmd() tea.Cmd {
	return func() tea.Msg {
		return wifiRefreshedMsg{ifaces: wifi.Collect()}
	}
}

func scanWifiNetworksCmd(iface string) tea.Cmd {
	return func() tea.Msg {
		nets, err := wifi.ScanNetworks(iface)
		return wifiNetworksScannedMsg{iface: iface, nets: nets, err: err}
	}
}

func connectWifiNetworkCmd(req wifi.ConnectRequest) tea.Cmd {
	return func() tea.Msg {
		err := wifi.ConnectNetwork(req)
		return wifiConnectDoneMsg{req: req, err: err}
	}
}

func runWifiPentestCmd(ctx context.Context, req wifi.PentestRequest, progressCh chan wifi.PentestResult) tea.Cmd {
	return func() tea.Msg {
		defer func() {
			if progressCh != nil {
				close(progressCh)
			}
		}()
		res, err := wifi.RunPentestWithProgress(ctx, req, func(prog wifi.PentestResult) {
			if progressCh == nil {
				return
			}
			select {
			case progressCh <- prog:
			default:
				select {
				case <-progressCh:
				default:
				}
				select {
				case progressCh <- prog:
				default:
				}
			}
		})
		return wifiPentestDoneMsg{res: res, err: err}
	}
}

func waitWifiPentestProgressCmd(ch <-chan wifi.PentestResult) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		res, ok := <-ch
		if !ok {
			return nil
		}
		return wifiPentestProgressMsg{res: res}
	}
}

func wifiPentestTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return wifiPentestTickMsg{}
	})
}

func wpsScanCmd(iface string) tea.Cmd {
	return func() tea.Msg {
		infos, err := wifi.ScanWPS(iface)
		return wpsScanDoneMsg{iface: iface, infos: infos, err: err}
	}
}

func wpsRunCmd(ctx context.Context, seq int, o wifi.OneShot, iface, bssid string, args []string, ch chan string) tea.Cmd {
	return func() tea.Msg {
		defer func() {
			if ch != nil {
				close(ch)
			}
		}()
		err := wifi.RunOneShot(ctx, o, iface, bssid, args, func(line string) {
			if ch == nil {
				return
			}
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
		return wpsRunDoneMsg{seq: seq, err: err}
	}
}

func waitWPSLineCmd(seq int, ch <-chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return wpsRunLineMsg{seq: seq, line: line}
	}
}

func irCaptureCmd(ctx context.Context, path string, timeout time.Duration, seq int) tea.Cmd {
	return func() tea.Msg {
		sig, err := ir.CaptureOnce(ctx, path, timeout)
		return irCaptureDoneMsg{seq: seq, sig: sig, err: err}
	}
}

func irSendCmd(path string, sig ir.Signal, name string) tea.Cmd {
	return func() tea.Msg {
		return irSendDoneMsg{name: name, err: ir.SendOnce(path, sig)}
	}
}

func irIndexCmd(roots ir.Roots) tea.Cmd {
	return func() tea.Msg {
		entries, err := ir.LoadIndex(roots)
		return irIndexDoneMsg{entries: entries, err: err}
	}
}

func irLoadCmd(entry ir.Entry) tea.Cmd {
	return func() tea.Msg {
		remote, err := ir.LoadEntry(entry)
		return irRemoteLoadedMsg{remote: remote, err: err}
	}
}

func irLearnTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return irLearnTickMsg{}
	})
}

func irMatchCandidatesCmd(entries []ir.Entry, category, brand string) tea.Cmd {
	return func() tea.Msg {
		cands, err := ir.CollectBrandPowerCandidates(entries, category, brand)
		return irMatchCandidatesMsg{cands: cands, err: err}
	}
}

func irMatchSaveCmd(cand ir.MatchCandidate) tea.Cmd {
	return func() tea.Msg {
		prof, err := ir.BindMatchedRemote(cand)
		return irMatchSavedMsg{profile: prof, err: err}
	}
}

func irUniversalBlastCmd(ctx context.Context, seq int, txPath string, entries []ir.Entry, target string, progressCh chan ir.UniversalProgress) tea.Cmd {
	return func() tea.Msg {
		defer func() {
			if progressCh != nil {
				close(progressCh)
			}
		}()
		var last ir.UniversalProgress
		err := ir.BlastUniversal(ctx, txPath, entries, target, func(prog ir.UniversalProgress) {
			last = prog
			if progressCh == nil {
				return
			}
			select {
			case progressCh <- prog:
			default:
				select {
				case <-progressCh:
				default:
				}
				select {
				case progressCh <- prog:
				default:
				}
			}
		})
		return irUniversalDoneMsg{
			seq:        seq,
			target:     target,
			uniqueSent: last.UniqueSent,
			lastBrand:  last.Current.Brand,
			err:        err,
		}
	}
}

func waitIRUniversalProgressCmd(seq int, ch <-chan ir.UniversalProgress) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		prog, ok := <-ch
		if !ok {
			return nil
		}
		return irUniversalProgressMsg{seq: seq, prog: prog}
	}
}

func irUnivTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return irUnivTickMsg{}
	})
}

func collectBluetoothCmd() tea.Cmd {
	return func() tea.Msg {
		return bluetoothRefreshedMsg{adapters: bluetooth.Collect()}
	}
}

func scanBluetoothDevicesCmd(hci, controller string, mode bluetooth.ScanMode) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		devices, err := bluetooth.ScanDevices(ctx, bluetooth.ScanRequest{
			HCI:        hci,
			Controller: controller,
			Mode:       mode,
			Timeout:    10 * time.Second,
		})
		return btDevicesScannedMsg{hci: hci, mode: mode, devices: devices, err: err}
	}
}

func bluetoothDeviceInfoCmd(addr string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		info, err := bluetooth.QueryDeviceInfo(ctx, addr)
		return btDeviceInfoMsg{addr: addr, info: info, err: err}
	}
}

func runBluetoothAttackCmd(ctx context.Context, seq int, req bluetooth.AttackRequest, ch chan string) tea.Cmd {
	return func() tea.Msg {
		defer func() {
			if ch != nil {
				close(ch)
			}
		}()
		err := bluetooth.RunAttack(ctx, req, func(line string) {
			if ch == nil {
				return
			}
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
		return btAttackDoneMsg{seq: seq, kind: req.Kind, err: err}
	}
}

func waitBluetoothLineCmd(seq int, ch <-chan string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return btAttackLineMsg{seq: seq, line: line}
	}
}

func btAttackTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return btAttackTickMsg{}
	})
}

func playMediaCmd(path string, video bool) tea.Cmd {
	bin, args, err := audio.PlayerCommand(path, video)
	if err != nil {
		return func() tea.Msg { return filesPlayDoneMsg{err: err} }
	}
	c := exec.Command(bin, args...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return filesPlayDoneMsg{err: err}
	})
}

func playAudioCmd(ctx context.Context, seq int, path string) tea.Cmd {
	return func() tea.Msg {
		return filesAudioDoneMsg{seq: seq, err: audio.PlayAudio(ctx, path)}
	}
}

func waitAudioPlayerCmd(seq int, p *audio.Player) tea.Cmd {
	return func() tea.Msg {
		if p == nil {
			return nil
		}
		return filesAudioDoneMsg{seq: seq, err: p.Wait()}
	}
}

func filesPlayTickCmd(seq int, p *audio.Player) tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg {
		msg := filesPlayTickMsg{seq: seq}
		if p != nil && p.Controllable() {
			pos, dur, paused, err := p.Status()
			msg.pos, msg.dur, msg.paused, msg.err = pos, dur, paused, err
		}
		return msg
	})
}

func recordMicCmd(ctx context.Context, seq int, path string) tea.Cmd {
	return func() tea.Msg {
		return filesRecordDoneMsg{seq: seq, err: audio.RecordMic(ctx, path, nil)}
	}
}

func filesRecordTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return filesRecordTickMsg{}
	})
}

func btMicRecordCmd(ctx context.Context, seq int, path string) tea.Cmd {
	return func() tea.Msg {
		return btMicDoneMsg{seq: seq, path: path, err: audio.RecordMic(ctx, path, nil)}
	}
}

func btMicTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return btMicTickMsg{}
	})
}

func btConvertCmd(ctx context.Context, input, output string) tea.Cmd {
	return func() tea.Msg {
		return btConvertDoneMsg{path: output, err: audio.ConvertToSCO(ctx, input, output)}
	}
}

func collectLocalIfacesCmd() tea.Cmd {
	return func() tea.Msg {
		return localIfacesMsg{ifaces: localscan.CollectInterfaces()}
	}
}

func runLocalScanCmd(ctx context.Context, seq int, req localscan.ScanRequest, ch chan localscan.ScanProgress) tea.Cmd {
	return func() tea.Msg {
		defer func() {
			if ch != nil {
				close(ch)
			}
		}()
		res, err := localscan.RunScan(ctx, req, func(prog localscan.ScanProgress) {
			if ch == nil {
				return
			}
			select {
			case ch <- prog:
			default:
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- prog:
				default:
				}
			}
		})
		return localScanDoneMsg{seq: seq, res: res, err: err}
	}
}

func waitLocalScanProgressCmd(seq int, ch <-chan localscan.ScanProgress) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		prog, ok := <-ch
		if !ok {
			return nil
		}
		return localScanProgressMsg{seq: seq, prog: prog}
	}
}

func localScanTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return localScanTickMsg{}
	})
}

func startHWWatcherCmd() tea.Cmd {
	return func() tea.Msg {
		w := hwctl.StartWatcher()
		ev, ok := <-w.Events()
		if !ok {
			return nil
		}
		return hwEventMsg{ev: ev, ch: w.Events()}
	}
}

func waitHWEventCmd(ch <-chan hwctl.Event) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return hwEventMsg{ev: ev, ch: ch}
	}
}

func osdHideCmd(seq int) tea.Cmd {
	return tea.Tick(osdDisplayDuration, func(time.Time) tea.Msg {
		return osdHideMsg{seq: seq}
	})
}

func (m AppModel) scheduleTick() tea.Cmd {
	if m.Cfg.RefreshSeconds <= 0 {
		return nil
	}
	d := time.Duration(m.Cfg.RefreshSeconds) * time.Second
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *AppModel) setStatus(msg string) tea.Cmd {
	m.StatusMsg = msg
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.TermW = msg.Width
		m.TermH = msg.Height
		return m, m.resetMarquee()

	case marqueeStartMsg:
		if msg.gen != m.MarqueeGen {
			return m, nil
		}
		_, overflow := m.renderScreen(0)
		if !overflow {
			return m, nil
		}
		m.MarqueeStep = 1
		return m, marqueeTickCmd(m.MarqueeGen)

	case marqueeTickMsg:
		if msg.gen != m.MarqueeGen {
			return m, nil
		}
		m.MarqueeStep++
		return m, marqueeTickCmd(m.MarqueeGen)

	case tickMsg:
		m.Snap = sysinfo.Collect()
		cmds := []tea.Cmd{m.scheduleTick()}
		if m.ActiveView == ViewStatus {
			switch m.ActiveTabs[ViewStatus] {
			case StatusTabWiFi:
				cmds = append(cmds, collectWifiCmd())
			case StatusTabBT:
				cmds = append(cmds, collectBluetoothCmd())
			}
		}
		if m.ActiveView == ViewBluetooth {
			cmds = append(cmds, collectBluetoothCmd())
		}
		if m.ActiveView == ViewWiFi {
			cmds = append(cmds, collectWifiCmd(), collectLocalIfacesCmd())
		}
		if m.ActiveView == ViewSDR && !m.SDR.AnyActive() && !m.SDR.Probing {
			cmds = append(cmds, probeSDRCmd(true))
		}
		return m, tea.Batch(cmds...)

	case wifiRefreshedMsg:
		m.Wifi.SetInterfaces(msg.ifaces)
		m.WifiScan.SyncInterfaces(msg.ifaces)
		m.WifiSpam.SyncInterfaces(msg.ifaces)
		m.WifiEvil.SyncInterfaces(msg.ifaces)
		return m, nil

	case wifiNetworksScannedMsg:
		m.WifiScan.SetScanResult(msg.iface, msg.nets, msg.err)
		wpsCmd := m.maybeWPSScanCmd()
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("Scan failed"), m.resetMarquee(), wpsCmd)
		}
		return m, tea.Batch(m.setStatus(fmt.Sprintf("%d networks on %s", len(msg.nets), msg.iface)), m.resetMarquee(), wpsCmd)

	case wifiConnectDoneMsg:
		m.WifiScan.SetConnectResult(msg.req, msg.err)
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("Connect failed"), m.resetMarquee())
		}
		m.Snap = sysinfo.Collect()
		scanIface := m.WifiScan.EffectiveScanIface()
		return m, tea.Batch(m.setStatus("Connected "+msg.req.Iface), collectWifiCmd(), collectLocalIfacesCmd(), scanWifiNetworksCmd(scanIface), m.resetMarquee())

	case wifiPentestDoneMsg:
		m.WifiScan.SetPentestResult(msg.res, msg.err)
		status := "Pentest done"
		switch {
		case msg.err != nil:
			status = "Pentest failed"
		case msg.res.HandshakeComplete:
			status = "Handshake captured"
		case msg.res.EAPOLFrames > 0:
			status = fmt.Sprintf("Captured %d EAPOL", msg.res.EAPOLFrames)
		case msg.res.DeauthFrames > 0:
			status = fmt.Sprintf("Sent %d deauth", msg.res.DeauthFrames)
		}
		return m, tea.Batch(m.setStatus(status), collectWifiCmd(), m.resetMarquee())

	case wifiPentestProgressMsg:
		if !m.WifiScan.PentestActive {
			return m, nil
		}
		m.WifiScan.SetPentestProgress(msg.res)
		return m, waitWifiPentestProgressCmd(m.WifiScan.PentestProgressCh)

	case wifiPentestTickMsg:
		if !m.WifiScan.PentestActive {
			return m, nil
		}
		if !m.WifiScan.PentestStarted.IsZero() {
			m.WifiScan.PentestElapsed = time.Since(m.WifiScan.PentestStarted)
		}
		return m, wifiPentestTickCmd()

	case wpsScanDoneMsg:
		m.WifiScan.SetWPSInfo(msg.iface, msg.infos, msg.err)
		return m, nil

	case wpsRunLineMsg:
		if !m.WifiScan.WPSActive || msg.seq != m.WifiScan.WPSSeq {
			return m, nil
		}
		m.WifiScan.ApplyWPSLine(msg.seq, msg.line)
		return m, waitWPSLineCmd(msg.seq, m.WifiScan.WPSProgressCh)

	case wpsRunDoneMsg:
		m.WifiScan.DrainWPSLines(msg.seq)
		m.WifiScan.FinishWPSRun(msg.seq, msg.err)
		switch {
		case msg.err == nil:
			return m, tea.Batch(m.setStatus("OneShot finished"), m.resetMarquee(), collectWifiCmd(), collectLocalIfacesCmd())
		case strings.Contains(msg.err.Error(), "canceled"):
			return m, tea.Batch(m.setStatus("OneShot stopped"), m.resetMarquee(), collectWifiCmd(), collectLocalIfacesCmd())
		default:
			return m, tea.Batch(m.setStatus("OneShot failed"), m.resetMarquee(), collectWifiCmd(), collectLocalIfacesCmd())
		}

	case irCaptureDoneMsg:
		m.IR.ApplyCaptureResult(msg.seq, msg.sig, msg.err)
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("IR capture failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus("IR signal captured"), m.resetMarquee())

	case irSendDoneMsg:
		if msg.err != nil {
			m.IR.Note = "Send failed: " + msg.err.Error()
			m.IR.NoteIsErr = true
			return m, tea.Batch(m.setStatus("IR send failed"), m.resetMarquee())
		}
		m.IR.Note = "Sent " + msg.name
		m.IR.NoteIsErr = false
		return m, tea.Batch(m.setStatus("IR sent"), m.resetMarquee())

	case irIndexDoneMsg:
		m.IR.SetIndex(msg.entries, msg.err)
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("IR index failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus(fmt.Sprintf("%d IR remotes", len(msg.entries))), m.resetMarquee())

	case irRemoteLoadedMsg:
		if msg.err != nil {
			m.IR.RemoteLoading = false
			m.IR.RemoteErr = msg.err.Error()
			return m, tea.Batch(m.setStatus("IR load failed"), m.resetMarquee())
		}
		m.IR.SetRemote(msg.remote)
		return m, tea.Batch(m.setStatus(msg.remote.ProfileName()), m.resetMarquee())

	case irLearnTickMsg:
		if !m.IR.Capturing {
			return m, nil
		}
		m.IR.CaptureElapsed = time.Since(m.IR.CaptureStarted)
		return m, irLearnTickCmd()

	case irMatchCandidatesMsg:
		m.IR.SetMatchCandidates(msg.cands, msg.err)
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("Match load failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus(fmt.Sprintf("%d Power codes", len(msg.cands))), m.resetMarquee())

	case irMatchSavedMsg:
		if !m.IR.AcceptMatchedProfile(msg.profile, msg.err) {
			return m, tea.Batch(m.setStatus("Save remote failed"), m.resetMarquee())
		}
		m.ActiveTabs[ViewIR] = int(IRTabRemote)
		if m.IR.NoteIsErr {
			return m, tea.Batch(m.setStatus("IR save failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus("Saved "+msg.profile.Name), m.resetMarquee())

	case irUniversalProgressMsg:
		if !m.IR.UnivBlasting || msg.seq != m.IR.UnivSeq {
			return m, nil
		}
		m.IR.ApplyUniversalProgress(msg.seq, msg.prog)
		return m, waitIRUniversalProgressCmd(msg.seq, m.IR.UnivProgressCh)

	case irUniversalDoneMsg:
		m.IR.ApplyUniversalDone(msg.seq, msg.target, msg.uniqueSent, msg.lastBrand, msg.err)
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("Universal stopped"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus(fmt.Sprintf("Sent %d codes", msg.uniqueSent)), m.resetMarquee())

	case irUnivTickMsg:
		if !m.IR.UnivBlasting {
			return m, nil
		}
		if !m.IR.UnivBlastStarted.IsZero() {
			m.IR.UnivBlastElapsed = time.Since(m.IR.UnivBlastStarted)
		}
		return m, irUnivTickCmd()

	case bluetoothRefreshedMsg:
		m.Bluetooth.SetAdapters(msg.adapters)
		m.BluetoothScan.SyncAdapters(msg.adapters)
		m.BluetoothEmu.SyncAdapters(msg.adapters)
		return m, nil

	case btDevicesScannedMsg:
		if msg.hci != m.BluetoothScan.EffectiveHCI() {
			m.BluetoothScan.Scanning = false
			return m, nil
		}
		m.BluetoothScan.SetScanResult(msg.devices, msg.err)
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("BT scan failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus(fmt.Sprintf("%d BT devices on %s", len(msg.devices), msg.hci)), m.resetMarquee())

	case btDeviceInfoMsg:
		if msg.err == nil {
			m.BluetoothScan.MergeDeviceInfo(msg.info)
		}
		return m, nil

	case btAttackLineMsg:
		if !m.BluetoothScan.RunActive || msg.seq != m.BluetoothScan.RunSeq {
			return m, nil
		}
		m.BluetoothScan.ApplyAttackLine(msg.seq, msg.line)
		return m, waitBluetoothLineCmd(msg.seq, m.BluetoothScan.RunProgressCh)

	case btAttackDoneMsg:
		m.BluetoothScan.DrainRunLines(msg.seq)
		m.BluetoothScan.FinishAttack(msg.seq, msg.err)
		if msg.kind == bluetooth.AttackCarwhisperer {
			m.BluetoothScan.CleanupCWMessage()
		}
		switch {
		case msg.err == nil:
			return m, tea.Batch(m.setStatus(msg.kind.String()+" done"), m.resetMarquee())
		case strings.Contains(msg.err.Error(), "canceled"):
			return m, tea.Batch(m.setStatus(msg.kind.String()+" stopped"), m.resetMarquee())
		default:
			return m, tea.Batch(m.setStatus(msg.kind.String()+" failed"), m.resetMarquee())
		}

	case btAttackTickMsg:
		if !m.BluetoothScan.RunActive {
			return m, nil
		}
		if !m.BluetoothScan.RunStarted.IsZero() {
			m.BluetoothScan.RunElapsed = time.Since(m.BluetoothScan.RunStarted)
		}
		return m, btAttackTickCmd()

	case filesPlayDoneMsg:
		if msg.err != nil {
			m.Files.Note = msg.err.Error()
			m.Files.NoteIsErr = true
			return m, tea.Batch(tea.ClearScreen, m.setStatus("Playback failed"), m.resetMarquee())
		}
		return m, tea.Batch(tea.ClearScreen, m.setStatus("Playback finished"), m.resetMarquee())

	case filesAudioDoneMsg:
		if errors.Is(msg.err, context.Canceled) {
			m.Files.FinishPlayback(msg.seq, nil)
			return m, tea.Batch(m.setStatus("Playback stopped"), m.resetMarquee())
		}
		m.Files.FinishPlayback(msg.seq, msg.err)
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("Playback failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus("Playback finished"), m.resetMarquee())

	case filesPlayTickMsg:
		if !m.Files.Playing || msg.seq != m.Files.PlaySeq {
			return m, nil
		}
		m.Files.ApplyPlayStatus(msg.seq, msg.pos, msg.dur, msg.paused, msg.err)
		return m, filesPlayTickCmd(msg.seq, m.Files.Player)

	case filesRecordDoneMsg:
		if errors.Is(msg.err, context.Canceled) {
			m.Files.FinishRecording(msg.seq, nil)
			return m, tea.Batch(m.setStatus("Recording stopped"), m.resetMarquee())
		}
		m.Files.FinishRecording(msg.seq, msg.err)
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("Recording failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus("Recording saved"), m.resetMarquee())

	case filesRecordTickMsg:
		if !m.Files.Recording {
			return m, nil
		}
		if !m.Files.RecordStarted.IsZero() {
			m.Files.RecordElapsed = time.Since(m.Files.RecordStarted)
		}
		return m, filesRecordTickCmd()

	case btMicDoneMsg:
		canceled := errors.Is(msg.err, context.Canceled)
		m.BluetoothScan.FinishCWRecord(msg.seq, nil)
		discard := m.BluetoothScan.CWDiscardMsg
		m.BluetoothScan.CWDiscardMsg = false
		if msg.err != nil && !canceled {
			m.BluetoothScan.CleanupCWMessage()
			return m, tea.Batch(m.setStatus("Mic recording failed"), m.resetMarquee())
		}
		if discard || msg.path == "" {
			m.BluetoothScan.CleanupCWMessage()
			return m, m.setStatus("Message discarded")
		}
		// Recording stopped (Enter) -> convert and drive the SCO link with it.
		m.BluetoothScan.CWStartTap = m.BluetoothScan.CWTap
		return m, tea.Batch(
			m.setStatus("Sending message..."),
			btConvertCmd(context.Background(), msg.path, m.BluetoothScan.ConvertPath()),
		)

	case btMicTickMsg:
		if !m.BluetoothScan.CWRecord {
			return m, nil
		}
		if !m.BluetoothScan.CWRecordAt.IsZero() {
			m.BluetoothScan.CWRecordFor = time.Since(m.BluetoothScan.CWRecordAt)
		}
		return m, btMicTickCmd()

	case btConvertDoneMsg:
		if msg.err != nil {
			m.BluetoothScan.CleanupCWMessage()
			return m, tea.Batch(m.setStatus("Audio convert failed"), m.resetMarquee())
		}
		m.BluetoothScan.Mode = btModeCWMenu
		return m.startBluetoothActionWithOptions(btActCarwhisperer, BTActionOptions{
			Record:   m.BluetoothScan.CWStartTap,
			PlayPath: msg.path,
		})

	case btSudoAuthMsg:
		if msg.err != nil {
			return m, tea.Batch(tea.ClearScreen, m.setStatus("sudo cancelled"), m.resetMarquee())
		}
		if bluetooth.NeedsSudo() {
			return m, tea.Batch(tea.ClearScreen, m.setStatus("sudo unavailable"), m.resetMarquee())
		}
		next, cmd := m.startBluetoothActionWithOptions(msg.kind, msg.opts)
		return next, tea.Batch(tea.ClearScreen, cmd)

	case btEmuEventMsg:
		if !m.BluetoothEmu.Active || msg.seq != m.BluetoothEmu.Seq {
			return m, nil
		}
		prevConn := m.BluetoothEmu.Connected
		m.BluetoothEmu.ApplyEvent(msg.seq, msg.ev)
		cmds := []tea.Cmd{waitBluetoothEmuEventCmd(msg.seq, m.BluetoothEmu.EventCh)}
		if msg.ev.Kind == "peer" && msg.ev.Connected != prevConn {
			if msg.ev.Connected {
				label := "BT connected: " + msg.ev.PeerName
				if msg.ev.PeerPaired {
					label = "BT paired: " + msg.ev.PeerName
				}
				cmds = append(cmds, m.setStatus(label))
			} else {
				cmds = append(cmds, m.setStatus("BT disconnected"))
			}
		}
		return m, tea.Batch(cmds...)

	case btEmuDoneMsg:
		m.BluetoothEmu.FinishEmulation(msg.seq, msg.err)
		switch {
		case msg.err == nil:
			return m, tea.Batch(m.setStatus("Emulator stopped"), m.resetMarquee())
		case strings.Contains(msg.err.Error(), "canceled"):
			return m, tea.Batch(m.setStatus("Emulator stopped"), m.resetMarquee())
		default:
			return m, tea.Batch(m.setStatus("Emulator failed"), m.resetMarquee())
		}

	case btEmuSudoAuthMsg:
		if msg.err != nil {
			return m, tea.Batch(tea.ClearScreen, m.setStatus("sudo cancelled"), m.resetMarquee())
		}
		if bluetooth.NeedsSudo() {
			return m, tea.Batch(tea.ClearScreen, m.setStatus("sudo unavailable"), m.resetMarquee())
		}
		next, cmd := m.startBluetoothEmu()
		return next, tea.Batch(tea.ClearScreen, cmd)

	case btEmuPlayDoneMsg:
		if errors.Is(msg.err, context.Canceled) {
			m.BluetoothEmu.FinishPlayLast(msg.seq, nil)
			return m, tea.Batch(m.setStatus("Playback stopped"), m.resetMarquee())
		}
		m.BluetoothEmu.FinishPlayLast(msg.seq, msg.err)
		if msg.err != nil {
			return m, tea.Batch(m.setStatus("Playback failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus("Playback finished"), m.resetMarquee())

	case btEmuPlayTickMsg:
		if m.BluetoothEmu.Mode != btEmuPlayWAV {
			return m, nil
		}
		if !m.BluetoothEmu.PlayStarted.IsZero() {
			m.BluetoothEmu.PlayElapsed = time.Since(m.BluetoothEmu.PlayStarted)
		}
		return m, btEmuPlayTickCmd()

	case wifiSpamLineMsg:
		if msg.seq != m.WifiSpam.Seq || !m.WifiSpam.Active {
			return m, nil
		}
		m.WifiSpam.AppendLine(msg.seq, msg.line)
		return m, waitWifiSpamLineCmd(msg.seq, m.WifiSpam.Ch)

	case wifiSpamDoneMsg:
		m.WifiSpam.Finish(msg.seq, msg.err)
		if msg.err != nil && !strings.Contains(strings.ToLower(msg.err.Error()), "canceled") {
			return m, m.setStatus("Beacon spam failed")
		}
		return m, m.setStatus("Beacon spam stopped")

	case wifiEvilLineMsg:
		if msg.seq != m.WifiEvil.Seq || !m.WifiEvil.Active {
			return m, nil
		}
		m.WifiEvil.AppendLine(msg.seq, msg.line)
		return m, waitWifiEvilLineCmd(msg.seq, m.WifiEvil.Ch)

	case wifiEvilDoneMsg:
		m.WifiEvil.Finish(msg.seq, msg.err)
		if msg.err != nil && !strings.Contains(strings.ToLower(msg.err.Error()), "canceled") {
			return m, m.setStatus("Evil twin failed")
		}
		return m, m.setStatus("Evil twin stopped")

	case wifiHelperTickMsg:
		if m.WifiSpam.Active {
			m.WifiSpam.Elapsed = time.Since(m.WifiSpam.Started)
		}
		if m.WifiEvil.Active {
			m.WifiEvil.Elapsed = time.Since(m.WifiEvil.Started)
		}
		if m.WifiSpam.Active || m.WifiEvil.Active {
			return m, wifiHelperTickCmd()
		}
		return m, nil

	case localIfacesMsg:
		m.Local.SyncInterfaces(msg.ifaces)
		m.WifiEvil.SyncUplinks(msg.ifaces)
		return m, nil

	case localScanProgressMsg:
		if !m.Local.Scanning || msg.seq != m.Local.ScanSeq {
			return m, nil
		}
		m.Local.ApplyProgress(msg.seq, msg.prog)
		return m, waitLocalScanProgressCmd(msg.seq, m.Local.ScanProgressCh)

	case localScanDoneMsg:
		m.Local.FinishScan(msg.seq, msg.res, msg.err)
		switch {
		case msg.err != nil && !m.Local.Result.Canceled:
			return m, tea.Batch(m.setStatus("Nmap failed"), m.resetMarquee())
		case m.Local.Result.Canceled:
			return m, tea.Batch(m.setStatus("Nmap stopped"), m.resetMarquee())
		case m.Local.Mode == localModeHostDetail:
			if h, ok := m.Local.SelectedHost(); ok {
				return m, tea.Batch(m.setStatus(fmt.Sprintf("Nmap: %d open", len(h.Ports))), m.resetMarquee())
			}
			return m, tea.Batch(m.setStatus("Nmap done"), m.resetMarquee())
		default:
			return m, tea.Batch(m.setStatus(fmt.Sprintf("Nmap: %d hosts", len(m.Local.Result.Hosts))), m.resetMarquee())
		}

	case localScanTickMsg:
		if !m.Local.Scanning {
			return m, nil
		}
		if !m.Local.ScanStarted.IsZero() {
			m.Local.ScanElapsed = time.Since(m.Local.ScanStarted)
		}
		return m, localScanTickCmd()

	case sdrProbeDoneMsg:
		m.SDR.SetProbeResult(msg.st)
		return m, nil

	case sdrRadioUpdateMsg:
		if !m.SDR.RadioActive || msg.seq != m.SDR.RadioSeq {
			return m, nil
		}
		m.SDR.ApplyRadioUpdate(msg.seq, msg.u)
		return m, waitSDRRadioUpdateCmd(msg.seq, m.SDR.RadioUpdate)

	case sdrRadioDoneMsg:
		m.SDR.FinishRadio(msg.seq, msg.err)
		if msg.err != nil && !isSdrCanceled(msg.err) {
			return m, tea.Batch(m.setStatus("SDR radio failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus("SDR radio stopped"), m.resetMarquee())

	case sdrSpecFrameMsg:
		if !m.SDR.SpecActive || msg.seq != m.SDR.SpecSeq {
			return m, nil
		}
		m.SDR.ApplySpecFrame(msg.seq, msg.frame)
		if m.SDR.SpecActive && !m.SDR.SpecStarted.IsZero() {
			m.SDR.SpecElapsed = time.Since(m.SDR.SpecStarted)
		}
		return m, waitSDRSpecFrameCmd(msg.seq, m.SDR.SpecFrameCh)

	case sdrSpecDoneMsg:
		m.SDR.FinishSpec(msg.seq, msg.frame, msg.hasFrame, msg.err)
		if msg.err != nil && !isSdrCanceled(msg.err) {
			return m, tea.Batch(m.setStatus("Sweep failed"), m.resetMarquee())
		}
		if msg.hasFrame {
			return m, tea.Batch(m.setStatus("Peak "+sdr.FormatFreqShort(msg.frame.PeakHz)), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus("Sweep stopped"), m.resetMarquee())

	case sdrSrvLineMsg:
		if !m.SDR.SrvActive || msg.seq != m.SDR.SrvSeq {
			return m, nil
		}
		m.SDR.AppendSrvLine(msg.seq, msg.line)
		return m, waitSDRServerLineCmd(msg.seq, m.SDR.SrvLineCh)

	case sdrSrvDoneMsg:
		m.SDR.FinishServer(msg.seq, msg.err)
		if msg.err != nil && !isSdrCanceled(msg.err) {
			return m, tea.Batch(m.setStatus("rtl_tcp failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus("rtl_tcp stopped"), m.resetMarquee())

	case sdrISMPacketMsg:
		if !m.SDR.ISMActive || msg.seq != m.SDR.ISMSeq {
			return m, nil
		}
		m.SDR.AppendISMPacket(msg.seq, msg.pkt)
		return m, waitSDRISMPacketCmd(msg.seq, m.SDR.ISMPktCh)

	case sdrISMDoneMsg:
		m.SDR.FinishISM(msg.seq, msg.err)
		if msg.err != nil && !isSdrCanceled(msg.err) {
			return m, tea.Batch(m.setStatus("ISM decoder failed"), m.resetMarquee())
		}
		return m, tea.Batch(m.setStatus("ISM decoder stopped"), m.resetMarquee())

	case sdrTickMsg:
		if m.SDR.RadioActive && !m.SDR.RadioStarted.IsZero() {
			m.SDR.RadioElapsed = time.Since(m.SDR.RadioStarted)
		}
		if m.SDR.SpecActive && !m.SDR.SpecStarted.IsZero() {
			m.SDR.SpecElapsed = time.Since(m.SDR.SpecStarted)
		}
		if m.SDR.ISMActive && !m.SDR.ISMStarted.IsZero() {
			m.SDR.ISMElapsed = time.Since(m.SDR.ISMStarted)
		}
		if m.SDR.SrvActive && !m.SDR.SrvStarted.IsZero() {
			m.SDR.SrvElapsed = time.Since(m.SDR.SrvStarted)
		}
		if m.SDR.AnyActive() {
			return m, sdrTickCmd()
		}
		return m, nil

	case hwEventMsg:
		nextCmd := waitHWEventCmd(msg.ch)
		if !m.OSDMuteUntil.IsZero() && time.Now().Before(m.OSDMuteUntil) {
			return m, nextCmd
		}
		switch msg.ev.Kind {
		case hwctl.EventBrightness:
			m.OSD.ShowBrightness(msg.ev.Brightness)
		case hwctl.EventVolume:
			m.OSD.ShowVolume(msg.ev.Volume)
		}
		return m, tea.Batch(nextCmd, osdHideCmd(m.OSD.Seq))

	case osdHideMsg:
		m.OSD.Hide(msg.seq)
		return m, nil

	case clearStatusMsg:
		m.StatusMsg = ""
		return m, m.resetMarquee()

	case startupRedrawMsg:
		cmds := []tea.Cmd{tea.ClearScreen}
		if next := scheduleStartupRedraw(msg.step + 1); next != nil {
			cmds = append(cmds, next)
		}
		return m, tea.Batch(cmds...)

	case updateDoneMsg:
		m.System.Updating = false
		if msg.err != nil {
			m.System.Note = msg.err.Error()
			return m, tea.Batch(m.setStatus("Update failed"), m.resetMarquee())
		}
		if !msg.res.Updated {
			m.System.Note = "Already up to date (" + msg.res.CurrentVersion + ")"
			return m, tea.Batch(m.setStatus("Up to date"), m.resetMarquee())
		}
		m.Version = msg.res.LatestVersion
		m.System.Note = "Done! Select Restart CZHax"
		return m, tea.Batch(m.setStatus("Updated: "+msg.res.LatestVersion), m.resetMarquee())

	case sudoUpdateDoneMsg:
		m.System.Updating = false
		if msg.err != nil {
			m.System.Note = "Update canceled or failed"
			return m, tea.Batch(tea.ClearScreen, m.setStatus("Update failed"), m.resetMarquee())
		}
		m.System.Note = "Done! Select Restart CZHax"
		return m, tea.Batch(tea.ClearScreen, m.setStatus("Update finished"), m.resetMarquee())

	case shellExitedMsg:
		m.Snap = sysinfo.Collect()
		m.System.RefreshBootState()
		m.OSD.Hide(0)
		m.OSDMuteUntil = time.Now().Add(500 * time.Millisecond)
		return m, tea.Batch(tea.ClearScreen, m.setStatus("Returned from shell"), collectWifiCmd(), collectBluetoothCmd(), collectLocalIfacesCmd(), m.resetMarquee())

	case bootToggledMsg:
		m.System.BootState = msg.state
		if msg.err != nil {
			m.System.Note = msg.err.Error()
			return m, tea.Batch(tea.ClearScreen, m.setStatus("Boot toggle failed"), m.resetMarquee())
		}
		if msg.state.Enabled {
			m.System.Note = "Start on boot: ON (" + string(msg.state.Method) + ")"
			return m, tea.Batch(tea.ClearScreen, m.setStatus("Boot: ON"), m.resetMarquee())
		}
		m.System.Note = "Start on boot: OFF"
		return m, tea.Batch(tea.ClearScreen, m.setStatus("Boot: OFF"), m.resetMarquee())

	case sleepDoneMsg:
		m.Snap = sysinfo.Collect()
		m.OSD.Hide(0)
		m.OSDMuteUntil = time.Now().Add(600 * time.Millisecond)
		if msg.err != nil {
			m.System.Note = msg.err.Error()
			return m, tea.Batch(tea.ClearScreen, m.setStatus("Sleep error"), collectWifiCmd(), collectBluetoothCmd(), collectLocalIfacesCmd(), m.resetMarquee())
		}
		m.System.Note = "Woke from sleep"
		return m, tea.Batch(tea.ClearScreen, m.setStatus("Awake"), collectWifiCmd(), collectBluetoothCmd(), collectLocalIfacesCmd(), m.resetMarquee())

	case powerActionDoneMsg:
		if msg.err != nil {
			m.System.Note = msg.err.Error()
			return m, tea.Batch(m.setStatus(msg.action+" failed"), m.resetMarquee())
		}
		return m, nil

	case tea.KeyMsg:
		nextModel, cmd := m.handleKey(msg)
		nm, ok := nextModel.(AppModel)
		if !ok {
			return nextModel, cmd
		}
		if msg.String() == "ctrl+c" {
			return nm, cmd
		}
		return nm, tea.Batch(cmd, nm.resetMarquee())
	}
	return m, nil
}

func (m AppModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if k == "ctrl+l" {
		return m, tea.ClearScreen
	}

	if m.ConfirmExit {
		switch k {
		case "y", "Y", "enter", "q":
			return m, tea.Quit
		default:
			m.ConfirmExit = false
			m.StatusMsg = ""
			return m, nil
		}
	}

	if m.ShowHelp {
		switch {
		case k == "esc" || k == "?" || k == "q" || k == "enter":
			m.ShowHelp = false
		case m.isUp(k):
			m.Help.Move(-1)
		case m.isDown(k):
			m.Help.Move(1)
		}
		return m, nil
	}

	// Text/name editing in the Files view must receive printable keys before
	// the global q/r/? shortcuts.
	if m.Focus == FocusContent && m.ActiveView == ViewFiles {
		switch m.Files.Mode {
		case filesModeText:
			if k == "esc" {
				m.Files.HandleEsc()
				return m, nil
			}
			m.Files.TextKey(msg)
			return m, m.resetMarquee()
		case filesModeInput:
			if k == "esc" {
				m.Files.HandleEsc()
				return m, nil
			}
			if k == "enter" {
				if err := m.Files.ConfirmInput(); err != nil {
					return m, m.setStatus(err.Error())
				}
				return m, m.resetMarquee()
			}
			m.Files.InputKey(msg)
			return m, nil
		}
	}
	if m.Focus == FocusContent && m.ActiveView == ViewBluetooth && m.BluetoothScan.Mode == btModeCWTarget {
		if k == "esc" {
			m.BluetoothScan.HandleEsc()
			return m, nil
		}
		if k == "enter" {
			m.BluetoothScan.ConfirmCWTarget()
			return m, m.resetMarquee()
		}
		m.BluetoothScan.HandleCWTargetKey(msg)
		return m, m.resetMarquee()
	}

	// Never leave a monitor interface behind: stop a run before quitting.
	if m.WifiScan.PentestActive && (k == "ctrl+c" || k == "q") {
		m.WifiScan.StopPentest()
		return m, m.setStatus("Stopping pentest...")
	}
	if m.IR.Capturing && (k == "ctrl+c" || k == "q") {
		m.IR.StopCapture()
		return m, m.setStatus("IR capture stopped")
	}
	if m.IR.UnivBlasting && (k == "ctrl+c" || k == "q") {
		m.IR.StopUniversalBlast()
		return m, m.setStatus("IR blast stopped")
	}
	if m.WifiScan.WPSActive && (k == "ctrl+c" || k == "q") {
		m.WifiScan.StopWPSRun()
		return m, m.setStatus("OneShot stopped")
	}
	if m.Local.Scanning && (k == "ctrl+c" || k == "q") {
		m.Local.StopScan()
		return m, m.setStatus("Stopping nmap...")
	}
	if m.BluetoothScan.RunActive && (k == "ctrl+c" || k == "q") {
		m.BluetoothScan.StopAttack()
		return m, m.setStatus("Stopping Bluetooth action...")
	}
	if m.Files.Recording && (k == "ctrl+c" || k == "q") {
		m.Files.StopRecording()
		return m, m.setStatus("Stopping recording...")
	}
	if m.Files.Playing && (k == "ctrl+c" || k == "q") {
		m.Files.StopPlayback()
		return m, m.setStatus("Stopping playback...")
	}
	if m.BluetoothScan.CWRecord && (k == "ctrl+c" || k == "q") {
		m.BluetoothScan.StopCWRecord()
		return m, m.setStatus("Stopping mic recording...")
	}
	if m.BluetoothEmu.Active && k == "ctrl+c" {
		m.BluetoothEmu.StopEmulation()
		return m, m.setStatus("Stopping Bluetooth emulator...")
	}
	if m.SDR.AnyActive() && (k == "ctrl+c" || k == "q") {
		m.SDR.StopAll()
		return m, m.setStatus("Stopping SDR...")
	}
	if m.Focus == FocusContent && m.ActiveView == ViewSDR {
		m.SDR.Tab = SDRTab(m.ActiveTabs[ViewSDR])
		if m.SDR.IsTextInput() {
			return m.sdrTextInput(k, msg)
		}
	}
	if m.Focus == FocusContent && m.ActiveView == ViewBluetooth && m.ActiveTabs[ViewBluetooth] == BTTabEmu && m.BluetoothEmu.CapturesDirectKeys() {
		return m.handleBluetoothEmuDirectKey(msg)
	}

	if m.Focus == FocusContent && m.ActiveView == ViewWiFi && m.ActiveTabs[ViewWiFi] == WiFiTabSpam && m.WifiSpam.IsTextInput() {
		return m.handleWifiSpamTextKey(k, msg)
	}
	if m.Focus == FocusContent && m.ActiveView == ViewWiFi && m.ActiveTabs[ViewWiFi] == WiFiTabEvil && m.WifiEvil.IsTextInput() {
		return m.handleWifiEvilTextKey(k, msg)
	}

	if m.Focus == FocusContent && m.ActiveView == ViewWiFi && m.ActiveTabs[ViewWiFi] == WiFiTabScan && m.WifiScan.IsTextInput() {
		if k == "esc" {
			m.WifiScan.HandleEsc()
			return m, nil
		}
		if submit, handled := m.WifiScan.HandleTextKey(msg); handled {
			if submit {
				if m.WifiScan.Mode == scanModeWPSPin {
					pin := strings.TrimSpace(m.WifiScan.WPSPinInput)
					return m.launchWPS("Custom PIN", []string{"-p", pin, "-k"})
				}
				req := m.WifiScan.BuildConnectRequest()
				return m, tea.Batch(m.setStatus("Connecting "+req.Iface+"..."), connectWifiNetworkCmd(req))
			}
			return m, nil
		}
	}

	if m.Focus == FocusContent && m.ActiveView == ViewIR {
		m.IR.Tab = IRTab(m.ActiveTabs[ViewIR])
		if m.IR.IsTextInput() {
			if k == "esc" {
				m.IR.HandleEsc()
				return m, nil
			}
			m.IR.HandleTextKey(msg)
			return m, nil
		}
		if m.IR.IsKeyCaptureActive() {
			if k == "esc" {
				m.IR.HandleEsc()
				return m, nil
			}
			bk := ir.NormalizeBindKey(k)
			if ir.IsBindableKey(bk) && m.IR.StartBindLabel(bk) {
				return m, nil
			}
			m.IR.LearnErr = "Use a-z, 0-9, F1-F8, Enter, Bksp"
			return m, nil
		}
		if m.IR.IsRemapKeyCaptureActive() {
			if k == "esc" {
				m.IR.HandleEsc()
				return m, nil
			}
			bk := ir.NormalizeBindKey(k)
			if ir.IsBindableKey(bk) && m.IR.ConfirmRemapKey(bk) {
				if m.IR.Note != "" {
					return m, m.setStatus(m.IR.Note)
				}
				return m, nil
			}
			m.IR.Note = "Use a-z, 0-9, F1-F8, Enter, Bksp"
			m.IR.NoteIsErr = true
			return m, nil
		}
	}

	switch k {
	case "?":
		m.ShowHelp = true
		m.Help.Scroll = 0
		return m, nil
	case "r":
		if m.WifiScan.PentestActive {
			return m, m.setStatus("Pentest running")
		}
		if m.Local.Scanning {
			return m, m.setStatus("Nmap scan running")
		}
		if m.BluetoothScan.RunActive {
			return m, m.setStatus("Bluetooth action running")
		}
		if m.ActiveView == ViewBluetooth && m.Focus == FocusContent {
			if m.ActiveTabs[ViewBluetooth] == BTTabEmu {
				if m.BluetoothEmu.Active && m.BluetoothEmu.Spec.IsAudio && m.BluetoothEmu.SubMode == btEmuCtrlMenu {
					m.BluetoothEmu.RecordAudio = !m.BluetoothEmu.RecordAudio
					m.BluetoothEmu.SendCmd(bluetooth.EmuCommand{Op: "record", Enabled: m.BluetoothEmu.RecordAudio})
					return m, m.resetMarquee()
				}
				return m, collectBluetoothCmd()
			}
			switch m.BluetoothScan.Mode {
			case btModeList:
				if !m.BluetoothScan.Scanning {
					hci := m.BluetoothScan.EffectiveHCI()
					m.BluetoothScan.StartScan()
					return m, tea.Batch(m.setStatus("Scanning "+hci+"..."), collectBluetoothCmd(), scanBluetoothDevicesCmd(hci, m.BluetoothScan.EffectiveController(), m.BluetoothScan.ScanMode))
				}
			case btModeDetail:
				if dev, ok := m.BluetoothScan.SelectedDevice(); ok {
					return m, bluetoothDeviceInfoCmd(dev.Address)
				}
			}
			return m, collectBluetoothCmd()
		}
		if m.ActiveView == ViewFiles && m.Focus == FocusContent && m.Files.Mode == filesModeList {
			m.Files.BeginInput(filesInputRename)
			return m, nil
		}
		if m.ActiveView == ViewLocal && m.Focus == FocusContent {
			switch m.Local.Mode {
			case localModeResults:
				m.Local.TargetHost = ""
				for _, opt := range localscan.ScanOptions {
					if opt.Mode == m.Local.LastSubnetMode {
						return m.launchLocalScan(opt)
					}
				}
			case localModeIfaces:
				m.Local.LoadingIfaces = true
				return m, tea.Batch(m.setStatus("Refreshing interfaces..."), collectLocalIfacesCmd())
			}
		}
		if m.ActiveView == ViewIR && m.Focus == FocusContent {
			if m.IR.Capturing {
				return m, m.setStatus("IR capture running")
			}
			if m.IR.UnivBlasting {
				return m, m.setStatus("IR blast running")
			}
			if m.IR.Tab == IRTabRemote && m.IR.RemoteMode == remoteModeUse {
				if b, ok := m.IR.BindingForKey(k); ok {
					return m.sendIR(b.Signal, b.Name)
				}
			}
			if m.IR.Tab == IRTabBrowse && m.IR.Remote != nil && m.IR.GridMode {
				if sig, name, ok := m.IR.gridSignal(k); ok {
					m.IR.advanceGrid(k)
					return m.sendIR(sig, name)
				}
			}
			if m.IR.Tab == IRTabBrowse && m.IR.Remote == nil && m.IR.BrowseMode != browseModeList {
				if m.IR.BrowseMode == browseModeMatchTest {
					if cand, ok := m.IR.CurrentMatchCandidate(); ok {
						return m.sendIR(cand.Signal, fmt.Sprintf("Power #%d", m.IR.MatchCursor+1))
					}
				}
				m.IR.RefreshHardware()
				return m, m.setStatus("IR refreshed")
			}
			m.IR.RefreshHardware()
			if IRTab(m.ActiveTabs[ViewIR]) == IRTabBrowse {
				m.IR.Remote = nil
				m.IR.RemoteErr = ""
				m.IR.IndexLoading = true
				m.IR.IndexDone = false
				return m, tea.Batch(m.setStatus("Indexing IR databases..."), irIndexCmd(m.IR.Roots))
			}
			return m, m.setStatus("IR refreshed")
		}
		if m.ActiveView == ViewSDR && m.Focus == FocusContent {
			if m.SDR.AnyActive() {
				return m, m.setStatus("SDR capture active")
			}
			m.SDR.Probing = true
			return m, tea.Batch(m.setStatus("Probing RTL-SDR..."), probeSDRCmd(false))
		}
		m.Snap = sysinfo.Collect()
		m.System.RefreshBootState()
		if len(m.Wifi.Interfaces) == 0 {
			m.Wifi.Scanning = true
		}
		if len(m.Bluetooth.Adapters) == 0 {
			m.Bluetooth.Scanning = true
		}
		cmds := []tea.Cmd{m.setStatus("Refreshed"), collectWifiCmd(), collectBluetoothCmd(), collectLocalIfacesCmd()}
		if m.Focus == FocusContent && m.ActiveView == ViewWiFi && m.WifiScan.Mode == scanModeList && !m.WifiScan.Scanning {
			iface := m.WifiScan.EffectiveScanIface()
			m.WifiScan.StartScan()
			cmds = append(cmds, scanWifiNetworksCmd(iface))
		}
		return m, tea.Batch(cmds...)
	case "q":
		if m.Cfg.ConfirmQuit {
			m.ConfirmExit = true
			return m, nil
		}
		return m, tea.Quit
	}

	if m.Focus == FocusTabs {
		switch {
		case m.isUp(k):
			m.ActiveView = ViewID((int(m.ActiveView) - 1 + int(viewCount)) % int(viewCount))
		case m.isDown(k):
			m.ActiveView = ViewID((int(m.ActiveView) + 1) % int(viewCount))
		case m.isLeft(k):
			m.cycleTab(-1)
		case m.isRight(k):
			m.cycleTab(1)
		case k == "enter":
			m.Focus = FocusContent
			if m.ActiveView == ViewSystem {
				m.System.RefreshBootState()
			} else if m.ActiveView == ViewWiFi {
				m.WifiScan.SyncInterfaces(m.Wifi.Interfaces)
				if !m.WifiScan.HasScanned && !m.WifiScan.Scanning {
					iface := m.WifiScan.EffectiveScanIface()
					m.WifiScan.StartScan()
					return m, tea.Batch(collectWifiCmd(), scanWifiNetworksCmd(iface), m.setStatus("Scanning "+iface+"..."), m.maybeWPSScanCmd())
				}
				return m, tea.Batch(collectWifiCmd(), m.maybeWPSScanCmd())
			} else if m.ActiveView == ViewLocal {
				if len(m.Local.Interfaces) == 0 {
					m.Local.LoadingIfaces = true
				}
				return m, collectLocalIfacesCmd()
			} else if m.ActiveView == ViewBluetooth {
				if m.ActiveTabs[ViewBluetooth] == BTTabEmu {
					m.BluetoothEmu.SyncAdapters(m.Bluetooth.Adapters)
					return m, collectBluetoothCmd()
				}
				m.BluetoothScan.SyncAdapters(m.Bluetooth.Adapters)
				if !m.BluetoothScan.HasScanned && !m.BluetoothScan.Scanning {
					hci := m.BluetoothScan.EffectiveHCI()
					m.BluetoothScan.StartScan()
					return m, tea.Batch(collectBluetoothCmd(), scanBluetoothDevicesCmd(hci, m.BluetoothScan.EffectiveController(), m.BluetoothScan.ScanMode), m.setStatus("Scanning "+hci+"..."))
				}
				return m, collectBluetoothCmd()
			} else if m.ActiveView == ViewFiles {
				m.Files.Reload()
				return m, nil
			} else if m.ActiveView == ViewIR {
				m.IR.Tab = IRTab(m.ActiveTabs[ViewIR])
				m.IR.RefreshHardware()
				if m.IR.Tab == IRTabBrowse && !m.IR.IndexDone && !m.IR.IndexLoading {
					m.IR.RemoteErr = ""
					m.IR.IndexLoading = true
					return m, tea.Batch(m.setStatus("Indexing IR databases..."), irIndexCmd(m.IR.Roots))
				}
				return m, nil
			} else if m.ActiveView == ViewSDR {
				m.SDR.Tab = SDRTab(m.ActiveTabs[ViewSDR])
				if !m.SDR.AnyActive() && !m.SDR.Probing {
					m.SDR.Probing = true
					return m, probeSDRCmd(false)
				}
				return m, nil
			} else if m.ActiveView == ViewStatus {
				switch m.ActiveTabs[ViewStatus] {
				case StatusTabWiFi:
					if len(m.Wifi.Interfaces) == 0 {
						m.Wifi.Scanning = true
					}
					return m, collectWifiCmd()
				case StatusTabBT:
					if len(m.Bluetooth.Adapters) == 0 {
						m.Bluetooth.Scanning = true
					}
					return m, collectBluetoothCmd()
				}
			}
		case k == "esc":
			if m.Cfg.ConfirmQuit {
				m.ConfirmExit = true
			}
		}
		if m.ActiveView == ViewIR && IRTab(m.ActiveTabs[ViewIR]) == IRTabBrowse && !m.IR.IndexDone && !m.IR.IndexLoading {
			m.IR.RemoteErr = ""
			m.IR.IndexLoading = true
			return m, tea.Batch(m.setStatus("Indexing IR databases..."), irIndexCmd(m.IR.Roots))
		}
		if m.ActiveView == ViewSDR {
			m.SDR.Tab = SDRTab(m.ActiveTabs[ViewSDR])
			if !m.SDR.AnyActive() && !m.SDR.Probing {
				return m, probeSDRCmd(true)
			}
			return m, nil
		}
		if m.ActiveView == ViewLocal {
			return m, collectLocalIfacesCmd()
		}
		if m.ActiveView == ViewBluetooth {
			return m, collectBluetoothCmd()
		}
		if m.ActiveView == ViewFiles {
			m.Files.Reload()
			return m, nil
		}
		if m.ActiveView == ViewStatus {
			switch m.ActiveTabs[ViewStatus] {
			case StatusTabWiFi:
				return m, collectWifiCmd()
			case StatusTabBT:
				return m, collectBluetoothCmd()
			}
		}
		return m, nil
	}

	return m.handleContentKey(k)
}

func (m *AppModel) maybeWPSScanCmd() tea.Cmd {
	if m.Cfg.WiFiColor != config.WiFiColorOneShot {
		return nil
	}
	return m.wpsScanForcedCmd()
}

// wpsScanForcedCmd refreshes WPS data regardless of color mode (used by the
// WPS menu and network details).
func (m *AppModel) wpsScanForcedCmd() tea.Cmd {
	iface := m.WifiScan.EffectiveScanIface()
	if !m.WifiScan.WPSInfoStale(iface) || m.WifiScan.WPSLoadingFor(iface) {
		return nil
	}
	if m.WifiScan.WPSLoading == nil {
		m.WifiScan.WPSLoading = make(map[string]bool)
	}
	m.WifiScan.WPSLoading[iface] = true
	return wpsScanCmd(iface)
}

func (m AppModel) launchWPS(label string, args []string) (tea.Model, tea.Cmd) {
	ctx, seq, ch, ok := m.WifiScan.StartWPSRun(label, args)
	if !ok {
		note := m.WifiScan.WPSNote
		if note == "" {
			note = "WPS attack unavailable"
		}
		return m, m.setStatus(note)
	}
	iface := m.WifiScan.EffectiveScanIface()
	return m, tea.Batch(
		m.setStatus("OneShot: "+label),
		wpsRunCmd(ctx, seq, m.WifiScan.OneShot, iface, m.WifiScan.TargetBSSID(), args, ch),
		waitWPSLineCmd(seq, ch),
	)
}

// launchWPSRetry reuses a recovered PIN, keeping it visible for further retries.
func (m AppModel) launchWPSRetry(label string, args []string, pin string) (tea.Model, tea.Cmd) {
	next, cmd := m.launchWPS(label, args)
	if nm, ok := next.(AppModel); ok && pin != "" {
		nm.WifiScan.WPSFoundPIN = pin
		return nm, cmd
	}
	return next, cmd
}

func (m AppModel) launchLocalScan(opt localscan.ScanOption) (tea.Model, tea.Cmd) {
	if m.Local.NmapPath == "" {
		m.Local.NmapPath = localscan.FindNmap()
	}
	if m.Local.NmapPath == "" {
		m.Local.ScanErr = "nmap not installed (install nmap)"
		return m, m.setStatus("nmap not found")
	}
	ctx, seq, req, ch, ok := m.Local.StartScan(opt)
	if !ok {
		msg := m.Local.ScanErr
		if msg == "" {
			msg = "Scan unavailable"
		}
		return m, m.setStatus(msg)
	}
	return m, tea.Batch(
		m.setStatus("Nmap: "+opt.Name),
		runLocalScanCmd(ctx, seq, req, ch),
		waitLocalScanProgressCmd(seq, ch),
		localScanTickCmd(),
	)
}

func (m AppModel) startBluetoothEmu() (tea.Model, tea.Cmd) {
	if bluetooth.NeedsSudo() {
		c := exec.Command("sudo", "-v")
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return btEmuSudoAuthMsg{err: err}
		})
	}
	ctx, seq, cfg, cmdCh, evCh := m.BluetoothEmu.StartEmulation()
	return m, tea.Batch(
		m.setStatus("BT Emu: "+cfg.Name),
		runBluetoothEmuCmd(ctx, seq, cfg, cmdCh, evCh),
		waitBluetoothEmuEventCmd(seq, evCh),
	)
}

func playBTEmuAudioCmd(ctx context.Context, seq int, path string) tea.Cmd {
	return func() tea.Msg {
		return btEmuPlayDoneMsg{seq: seq, err: audio.PlayAudio(ctx, path)}
	}
}

func btEmuPlayTickCmd() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return btEmuPlayTickMsg{}
	})
}

func (m AppModel) startBluetoothAction(kind BTActionKind) (tea.Model, tea.Cmd) {
	return m.startBluetoothActionWithOptions(kind, BTActionOptions{})
}

func (m AppModel) startBluetoothActionWithPlay(kind BTActionKind, playPath string) (tea.Model, tea.Cmd) {
	return m.startBluetoothActionWithOptions(kind, BTActionOptions{PlayPath: playPath})
}

func (m AppModel) startBluetoothActionWithOptions(kind BTActionKind, opts BTActionOptions) (tea.Model, tea.Cmd) {
	if (kind == btActCarwhisperer || kind == btActL2Flood) && bluetooth.NeedsSudo() {
		c := exec.Command("sudo", "-v")
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return btSudoAuthMsg{kind: kind, opts: opts, err: err}
		})
	}
	ctx, req, seq, ch, ok := m.BluetoothScan.StartAttack(kind, opts)
	if !ok {
		return m, m.setStatus("Select a device first")
	}
	return m, tea.Batch(
		m.setStatus("BT: "+req.Name),
		runBluetoothAttackCmd(ctx, seq, req, ch),
		waitBluetoothLineCmd(seq, ch),
		btAttackTickCmd(),
	)
}

func (m AppModel) startCW() (tea.Model, tea.Cmd) {
	v := &m.BluetoothScan
	if v.CWRecordMsg {
		ctx, seq, path, ok := v.StartCWRecord()
		if ok {
			return m, tea.Batch(m.setStatus("Recording message..."), btMicRecordCmd(ctx, seq, path), btMicTickCmd())
		}
		return m, nil
	}
	if v.CWPlay {
		if strings.TrimSpace(v.CWPlayPath) == "" {
			return m, m.setStatus("Choose an audio file")
		}
		v.CWStartTap = v.CWTap
		return m, tea.Batch(
			m.setStatus("Converting audio..."),
			btConvertCmd(context.Background(), v.CWPlayPath, v.ConvertPath()),
		)
	}
	if v.CWTap {
		return m.startBluetoothActionWithOptions(btActCarwhisperer, BTActionOptions{Record: true})
	}
	return m, m.setStatus("Select Tap or Play audio")
}

// startWifiConnect launches the connection request the view just prepared.
func (m AppModel) startWifiConnect() tea.Cmd {
	req := m.WifiScan.BuildConnectRequest()
	return tea.Batch(m.setStatus("Connecting "+req.Iface+"..."), connectWifiNetworkCmd(req))
}

func (m AppModel) startWifiPentest(mode wifi.PentestMode) (tea.Model, tea.Cmd) {
	if m.WifiScan.PentestActive {
		return m, nil
	}
	net, ok := m.WifiScan.SelectedNetwork()
	if !ok {
		return m, m.setStatus("Select an AP first")
	}
	if wifi.ChannelNumber(net) <= 0 {
		return m, m.setStatus("Unknown AP channel, rescan")
	}
	ctx, req, ok := m.WifiScan.StartPentest(mode)
	if !ok {
		return m, m.setStatus("Select an AP first")
	}
	return m, tea.Batch(
		m.setStatus(mode.String()+"..."),
		runWifiPentestCmd(ctx, req, m.WifiScan.PentestProgressCh),
		waitWifiPentestProgressCmd(m.WifiScan.PentestProgressCh),
		wifiPentestTickCmd(),
	)
}

func (m AppModel) sendIR(sig ir.Signal, name string) (tea.Model, tea.Cmd) {
	if m.IR.HW.TXPath == "" {
		m.IR.Note = "No IR transmitter: " + m.IR.HW.Err
		m.IR.NoteIsErr = true
		return m, m.setStatus("No IR transmitter")
	}
	return m, irSendCmd(m.IR.HW.TXPath, sig, name)
}

func (m AppModel) irProbe(target string) (tea.Model, tea.Cmd) {
	if m.IR.HW.TXPath == "" {
		m.IR.Note = "No IR transmitter: " + m.IR.HW.Err
		m.IR.NoteIsErr = true
		return m, m.setStatus("No IR transmitter")
	}
	sig, ok := m.IR.NextProbe(target)
	if !ok {
		m.IR.Note = "No candidates for " + target
		m.IR.NoteIsErr = true
		return m, nil
	}
	return m.sendIR(sig, target)
}

func (m AppModel) startIRUniversalBlast(btn ir.UniversalButton) (tea.Model, tea.Cmd) {
	ctx, seq, ch, ok := m.IR.StartUniversalBlast(btn.Target)
	if !ok {
		if m.IR.UnivBlastErr != "" {
			return m, m.setStatus("No IR transmitter")
		}
		return m, nil
	}
	entries := ir.EntriesForCategory(m.IR.Index, m.IR.UnivCategory, m.IR.UnivLastBrand)
	m.IR.UnivProgress.TotalEntries = len(entries)
	if len(entries) > 0 {
		m.IR.UnivProgress.Current.Provider = entries[0].Provider
	}
	return m, tea.Batch(
		m.setStatus("Blasting "+btn.Label+"..."),
		irUniversalBlastCmd(ctx, seq, m.IR.HW.TXPath, entries, btn.Target, ch),
		waitIRUniversalProgressCmd(seq, ch),
		irUnivTickCmd(),
	)
}

func (m AppModel) startIRCapture() (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	_, seq, ok := m.IR.StartCapture(ctx)
	if !ok {
		cancel()
		return m, nil
	}
	m.IR.SetCaptureCancel(cancel)
	return m, tea.Batch(irCaptureCmd(ctx, m.IR.HW.RXPath, 5*time.Second, seq), irLearnTickCmd(), m.setStatus("IR listening..."))
}

func (m AppModel) handleContentKey(k string) (tea.Model, tea.Cmd) {
	if m.ActiveView == ViewIR {
		m.IR.Tab = IRTab(m.ActiveTabs[ViewIR])
	}
	if m.ActiveView == ViewSDR {
		m.SDR.Tab = SDRTab(m.ActiveTabs[ViewSDR])
	}
	if m.ActiveView == ViewSystem && m.System.Confirm != "" {
		action := m.System.Confirm
		switch k {
		case "y", "Y", "enter":
			m.System.Confirm = ""
			if action == "shutdown" {
				m.System.Note = "Shutting down..."
				return m, func() tea.Msg {
					return powerActionDoneMsg{action: "shutdown", err: power.Shutdown()}
				}
			}
			m.System.Note = "Rebooting..."
			return m, func() tea.Msg {
				return powerActionDoneMsg{action: "reboot", err: power.Reboot()}
			}
		case "n", "N", "esc":
			m.System.Confirm = ""
			return m, nil
		default:
			m.System.Confirm = ""
			return m, nil
		}
	}

	if k == "esc" {
		if m.ActiveView == ViewWiFi && m.ActiveTabs[ViewWiFi] == WiFiTabSpam {
			if m.WifiSpam.Mode == spamModeRunning {
				m.WifiSpam.Stop()
				return m, m.setStatus("Stopping beacon spam...")
			}
			m.Focus = FocusTabs
			return m, nil
		}
		if m.ActiveView == ViewWiFi && m.ActiveTabs[ViewWiFi] == WiFiTabEvil {
			if m.WifiEvil.Mode == evilModeRunning {
				m.WifiEvil.Stop()
				return m, m.setStatus("Stopping evil twin...")
			}
			m.Focus = FocusTabs
			return m, nil
		}
		if m.ActiveView == ViewWiFi && m.WifiScan.HandleEsc() {
			return m, nil
		}
		if m.ActiveView == ViewLocal && m.Local.HandleEsc() {
			return m, nil
		}
		if m.ActiveView == ViewBluetooth {
			if m.ActiveTabs[ViewBluetooth] == BTTabEmu {
				if m.BluetoothEmu.HandleEsc() {
					return m, nil
				}
			} else if m.BluetoothScan.HandleEsc() {
				return m, nil
			}
		}
		if m.ActiveView == ViewFiles && m.Files.HandleEsc() {
			return m, nil
		}
		if m.ActiveView == ViewIR && m.IR.HandleEsc() {
			return m, nil
		}
		if m.ActiveView == ViewSDR && m.SDR.HandleEsc() {
			return m, nil
		}
		m.Focus = FocusTabs
		return m, nil
	}

	switch m.ActiveView {
	case ViewWiFi:
		switch m.ActiveTabs[ViewWiFi] {
		case WiFiTabSpam:
			return m.handleWifiSpamKey(k)
		case WiFiTabEvil:
			return m.handleWifiEvilKey(k)
		}
		_, mainW, bodyH := m.dimensions()
		pal := theme.Resolve(m.Cfg.Theme, false)
		switch m.WifiScan.Mode {
		case scanModeList:
			switch {
			case m.isUp(k):
				m.WifiScan.MoveNetwork(-1)
			case m.isDown(k):
				m.WifiScan.MoveNetwork(1)
			case m.isLeft(k):
				m.WifiScan.CycleScanIface(-1)
				return m, tea.Batch(m.resetMarquee(), m.maybeWPSScanCmd())
			case m.isRight(k) || k == "i":
				m.WifiScan.CycleScanIface(1)
				return m, tea.Batch(m.resetMarquee(), m.maybeWPSScanCmd())
			case k == "o":
				mode := m.WifiScan.ToggleColorMode()
				m.Cfg.WiFiColor = mode
				_ = config.Save(m.ConfigPath, m.Cfg)
				cmd := m.maybeWPSScanCmd()
				label := "Security colors"
				if mode == config.WiFiColorOneShot {
					label = "OneShot colors"
				}
				return m, tea.Batch(m.setStatus(label), cmd)
			case k == "s":
				if !m.WifiScan.Scanning {
					iface := m.WifiScan.EffectiveScanIface()
					m.WifiScan.StartScan()
					return m, tea.Batch(collectWifiCmd(), scanWifiNetworksCmd(iface), m.setStatus("Scanning "+iface+"..."))
				}
			case k == "enter":
				if len(m.WifiScan.Networks) == 0 {
					if !m.WifiScan.Scanning {
						iface := m.WifiScan.EffectiveScanIface()
						m.WifiScan.StartScan()
						return m, tea.Batch(collectWifiCmd(), scanWifiNetworksCmd(iface), m.setStatus("Scanning "+iface+"..."))
					}
				} else {
					m.WifiScan.OpenActions()
				}
			case k == "c":
				if m.WifiScan.BeginConnect(scanModeList) {
					return m, m.startWifiConnect()
				}
			}
			return m, nil

		case scanModeActions:
			switch {
			case m.isUp(k):
				m.WifiScan.MoveAction(-1)
			case m.isDown(k):
				m.WifiScan.MoveAction(1)
			case k == "enter" || k == " ":
				act, ok := m.WifiScan.SelectedAction()
				if !ok {
					return m, nil
				}
				switch act {
				case wifiActConnect:
					if m.WifiScan.BeginConnect(scanModeActions) {
						return m, m.startWifiConnect()
					}
				case wifiActBoth:
					return m.startWifiPentest(wifi.PentestBoth)
				case wifiActHandshake:
					return m.startWifiPentest(wifi.PentestHandshake)
				case wifiActDeauth:
					return m.startWifiPentest(wifi.PentestDeauth)
				case wifiActEvil:
					return m.openWifiEvilFromScan()
				case wifiActWPS:
					m.WifiScan.BeginWPSMenu(scanModeActions)
					return m, tea.Batch(m.resetMarquee(), m.wpsScanForcedCmd())
				case wifiActDetail:
					m.WifiScan.OpenDetail(scanModeActions)
					return m, m.wpsScanForcedCmd()
				}
			}
			return m, nil

		case scanModeWPSMenu:
			switch {
			case m.isUp(k):
				m.WifiScan.MoveWPSMenu(-1)
			case m.isDown(k):
				m.WifiScan.MoveWPSMenu(1)
			case k == "enter" || k == " ":
				method, ok := m.WifiScan.SelectedWPSMethod()
				if !ok {
					return m, nil
				}
				switch {
				case method.Back:
					m.WifiScan.HandleEsc()
				case method.Custom:
					m.WifiScan.StartWPSPin()
				default:
					if pin, ok := m.WifiScan.RetryWithPINArgs(); ok && strings.Join(pin, " ") == strings.Join(method.Args, " ") {
						return m.launchWPSRetry(method.Label, method.Args, m.WifiScan.WPSFoundPIN)
					}
					return m.launchWPS(method.Label, method.Args)
				}
			}
			return m, nil

		case scanModeWPSRun:
			if m.WifiScan.WPSActive {
				return m, nil
			}
			switch k {
			case "c":
				if m.WifiScan.BeginWPSConnect() {
					return m, m.startWifiConnect()
				}
				switch m.WifiScan.Mode {
				case scanModePassword, scanModeSSID:
					return m, m.setStatus("Enter the network details")
				}
				return m, m.setStatus("No PSK recovered")
			case "p":
				if args, ok := m.WifiScan.RetryWithPINArgs(); ok {
					return m.launchWPSRetry("PIN retry", args, m.WifiScan.WPSFoundPIN)
				}
				return m, m.setStatus("No PIN recovered")
			}
			return m, nil

		case scanModeDetail:
			switch {
			case m.isUp(k):
				m.WifiScan.MoveDetailScroll(-1, mainW, bodyH, pal)
			case m.isDown(k):
				m.WifiScan.MoveDetailScroll(1, mainW, bodyH, pal)
			case k == "enter" || k == "c":
				if m.WifiScan.BeginConnect(scanModeDetail) {
					return m, m.startWifiConnect()
				}
			}
			return m, nil

		case scanModePentest:
			// Deauth-only mode injects one burst per key press and keeps
			// injecting while a key is held (terminal key repeat).
			if m.WifiScan.PentestActive && m.WifiScan.PentestMode == wifi.PentestDeauth {
				m.WifiScan.TriggerDeauth()
			}
			return m, nil

		case scanModePickIface:
			if m.WifiScan.Connecting {
				return m, nil
			}
			switch {
			case m.isUp(k):
				m.WifiScan.MoveIfaceCursor(-1)
			case m.isDown(k):
				m.WifiScan.MoveIfaceCursor(1)
			case k == "enter":
				if m.WifiScan.ConfirmConnectIface() {
					req := m.WifiScan.BuildConnectRequest()
					return m, tea.Batch(m.setStatus("Connecting "+req.Iface+"..."), connectWifiNetworkCmd(req))
				}
			}
			return m, nil
		}
		return m, nil

	case ViewLocal:
		_, mainW, bodyH := m.dimensions()
		pal := theme.Resolve(m.Cfg.Theme, false)
		switch m.Local.Mode {
		case localModeIfaces:
			switch {
			case m.isUp(k):
				m.Local.MoveIface(-1)
			case m.isDown(k):
				m.Local.MoveIface(1)
			case k == "enter" || k == " ":
				if !m.Local.ConfirmIface() {
					return m, m.setStatus("No connected interface")
				}
			}
			return m, nil

		case localModeOptions:
			switch {
			case m.isUp(k):
				m.Local.MoveOption(-1)
			case m.isDown(k):
				m.Local.MoveOption(1)
			case k == "enter" || k == " ":
				if m.Local.IsBackOption() {
					m.Local.HandleEsc()
					return m, nil
				}
				if opt, ok := m.Local.SelectedOption(); ok {
					return m.launchLocalScan(opt)
				}
			}
			return m, nil

		case localModeRunning:
			return m, nil

		case localModeResults:
			switch {
			case m.isUp(k):
				m.Local.MoveHost(-1)
			case m.isDown(k):
				m.Local.MoveHost(1)
			case k == "s":
				m.Local.ScanSelectedHost()
			case k == "enter" || k == " ":
				if len(m.Local.Result.Hosts) > 0 {
					m.Local.OpenHostDetail()
					return m, nil
				}
				m.Local.TargetHost = ""
				for _, opt := range localscan.ScanOptions {
					if opt.Mode == m.Local.LastSubnetMode {
						return m.launchLocalScan(opt)
					}
				}
			}
			return m, nil

		case localModeHostDetail:
			switch {
			case m.isUp(k):
				m.Local.MoveHostDetailScroll(-1, mainW, bodyH, pal)
			case m.isDown(k):
				m.Local.MoveHostDetailScroll(1, mainW, bodyH, pal)
			case k == "s" || k == "enter":
				m.Local.ScanSelectedHost()
			}
			return m, nil
		}
		return m, nil

	case ViewBluetooth:
		if m.ActiveTabs[ViewBluetooth] == BTTabEmu {
			return m.handleBluetoothEmuKey(k)
		}
		_, mainW, bodyH := m.dimensions()
		pal := theme.Resolve(m.Cfg.Theme, false)
		switch m.BluetoothScan.Mode {
		case btModeList:
			switch {
			case m.isUp(k):
				m.BluetoothScan.MoveDevice(-1)
			case m.isDown(k):
				m.BluetoothScan.MoveDevice(1)
			case m.isLeft(k):
				m.BluetoothScan.CycleAdapter(-1)
				return m, collectBluetoothCmd()
			case m.isRight(k) || k == "i":
				m.BluetoothScan.CycleAdapter(1)
				return m, collectBluetoothCmd()
			case k == "m":
				m.BluetoothScan.CycleScanMode(1)
				return m, m.resetMarquee()
			case k == "s":
				if !m.BluetoothScan.Scanning {
					hci := m.BluetoothScan.EffectiveHCI()
					m.BluetoothScan.StartScan()
					return m, tea.Batch(m.setStatus("Scanning "+hci+"..."), collectBluetoothCmd(), scanBluetoothDevicesCmd(hci, m.BluetoothScan.EffectiveController(), m.BluetoothScan.ScanMode))
				}
			case k == "enter":
				if len(m.BluetoothScan.Devices) == 0 {
					if !m.BluetoothScan.Scanning {
						hci := m.BluetoothScan.EffectiveHCI()
						m.BluetoothScan.StartScan()
						return m, tea.Batch(m.setStatus("Scanning "+hci+"..."), collectBluetoothCmd(), scanBluetoothDevicesCmd(hci, m.BluetoothScan.EffectiveController(), m.BluetoothScan.ScanMode))
					}
				} else {
					m.BluetoothScan.OpenActions()
				}
			}
			return m, nil

		case btModeActions:
			switch {
			case m.isUp(k):
				m.BluetoothScan.MoveAction(-1)
			case m.isDown(k):
				m.BluetoothScan.MoveAction(1)
			case k == "enter" || k == " ":
				act, ok := m.BluetoothScan.SelectedAction()
				if !ok {
					return m, nil
				}
				if act == btActDetail {
					if m.BluetoothScan.OpenDetail(btModeActions) {
						if dev, ok := m.BluetoothScan.SelectedDevice(); ok {
							return m, bluetoothDeviceInfoCmd(dev.Address)
						}
					}
					return m, nil
				}
				if act == btActCarwhisperer {
					m.BluetoothScan.OpenCWMenu()
					return m, nil
				}
				return m.startBluetoothAction(act)
			}
			return m, nil

		case btModeCWMenu:
			switch {
			case m.isUp(k):
				m.BluetoothScan.MoveCW(-1)
			case m.isDown(k):
				m.BluetoothScan.MoveCW(1)
			case k == "enter" || k == " ":
				act, ok := m.BluetoothScan.SelectedCW()
				if !ok {
					return m, nil
				}
				switch act {
				case cwBack:
					m.BluetoothScan.HandleEsc()
				case cwTap, cwRecordMessage:
					m.BluetoothScan.ToggleCW(act)
				case cwPlayAudio:
					wasOn := m.BluetoothScan.CWPlay
					m.BluetoothScan.ToggleCW(cwPlayAudio)
					if !wasOn {
						m.BluetoothScan.OpenCWAudio()
					}
				case cwTarget:
					m.BluetoothScan.StartCWTargetEdit()
				case cwStart:
					return m.startCW()
				}
			}
			return m, nil

		case btModeCWAudio:
			switch {
			case m.isUp(k):
				m.BluetoothScan.MoveCWAudio(-1)
			case m.isDown(k):
				m.BluetoothScan.MoveCWAudio(1)
			case k == "enter" || k == " ":
				if e, ok := m.BluetoothScan.SelectedCWAudio(); ok {
					m.BluetoothScan.SetCWAudio(e)
				}
			}
			return m, nil

		case btModeCWRecord:
			if k == "enter" || k == " " {
				m.BluetoothScan.StopCWRecord()
			}
			return m, nil

		case btModeDetail:
			switch {
			case m.isUp(k):
				m.BluetoothScan.MoveDetailScroll(-1, mainW, bodyH, pal)
			case m.isDown(k):
				m.BluetoothScan.MoveDetailScroll(1, mainW, bodyH, pal)
			}
			return m, nil

		case btModeRunning:
			return m, nil
		}
		return m, nil

	case ViewFiles:
		switch m.Files.Mode {
		case filesModeList:
			switch {
			case m.isUp(k):
				m.Files.Move(-1)
			case m.isDown(k):
				m.Files.Move(1)
			case k == "enter" || k == " ":
				kind, path, err := m.Files.EnterSelected()
				if err != nil {
					return m, m.setStatus(err.Error())
				}
				if kind == "audio" {
					_, seq, _, ok := m.Files.StartPlayback(path)
					if ok {
						return m, tea.Batch(m.setStatus("Playing "+m.Files.PlayName), waitAudioPlayerCmd(seq, m.Files.Player), filesPlayTickCmd(seq, m.Files.Player))
					}
					if m.Files.PlayErr != "" {
						return m, m.setStatus(m.Files.PlayErr)
					}
				} else if kind != "" {
					return m, playMediaCmd(path, kind == "video")
				}
			case k == "n":
				m.Files.BeginInput(filesInputNewFile)
			case k == "m":
				m.Files.BeginInput(filesInputNewDir)
			case k == "x" || k == "delete" || k == "backspace":
				m.Files.BeginDelete()
			case k == "a":
				ctx, seq, path, ok := m.Files.StartRecording()
				if ok {
					return m, tea.Batch(m.setStatus("Recording..."), recordMicCmd(ctx, seq, path), filesRecordTickCmd())
				}
			}
			return m, nil

		case filesModeDelete:
			switch k {
			case "y", "Y", "enter":
				if err := m.Files.ConfirmDelete(); err != nil {
					return m, m.setStatus("Delete failed")
				}
				return m, m.setStatus("Deleted")
			case "n", "N":
				m.Files.Mode = filesModeList
			}
			return m, nil

		case filesModeRecord:
			if k == "enter" || k == " " {
				m.Files.StopRecording()
			}
			return m, nil

		case filesModePlay:
			switch {
			case k == "enter" || k == " ":
				if paused, err := m.Files.TogglePause(); err == nil {
					if paused {
						return m, m.setStatus("Paused")
					}
					return m, m.setStatus("Playing")
				}
				// Player without remote control: stop instead.
				m.Files.StopPlayback()
			case m.isLeft(k):
				if err := m.Files.Seek(-5); err == nil {
					return m, tea.Batch(m.setStatus("−5s"), filesPlayTickCmd(m.Files.PlaySeq, m.Files.Player))
				}
			case m.isRight(k):
				if err := m.Files.Seek(5); err == nil {
					return m, tea.Batch(m.setStatus("+5s"), filesPlayTickCmd(m.Files.PlaySeq, m.Files.Player))
				}
			}
			return m, nil

		case filesModeImage:
			return m, nil
		}
		return m, nil

	case ViewIR:
		_, mainW, bodyH := m.dimensions()
		pal := theme.Resolve(m.Cfg.Theme, false)
		_ = mainW
		_ = bodyH
		_ = pal
		m.IR.Tab = IRTab(m.ActiveTabs[ViewIR])
		switch m.IR.Tab {
		case IRTabRemote:
			if m.IR.RemoteMode == remoteModeRemap {
				switch {
				case m.isUp(k):
					m.IR.MoveRemap(-1)
				case m.isDown(k):
					m.IR.MoveRemap(1)
				case k == "enter" || k == " ":
					m.IR.BeginRemapKey()
				case k == "n":
					m.IR.BeginRemapLabel()
				case k == "t":
					if b, ok := m.IR.SelectedRemapBinding(); ok {
						return m.sendIR(b.Signal, b.Name)
					}
				case k == "x" || k == "backspace" || k == "delete":
					if m.IR.DeleteRemapBinding() && m.IR.Note != "" {
						return m, m.setStatus(m.IR.Note)
					}
				}
				return m, nil
			}
			if m.IR.RemoteMode == remoteModeList {
				switch {
				case m.isUp(k):
					m.IR.MoveRemoteList(-1)
				case m.isDown(k):
					m.IR.MoveRemoteList(1)
				case k == "enter" || k == " ":
					if m.IR.RemoteListCursor < len(m.IR.Store.Profiles) {
						m.IR.SelectRemoteFromList(m.IR.RemoteListCursor)
					} else {
						m.IR.BeginNewRemote(false)
					}
				case k == "e" || k == "m":
					if m.IR.RemoteListCursor < len(m.IR.Store.Profiles) {
						m.IR.OpenRemap(m.IR.RemoteListCursor, remoteModeList)
					}
				case k == "l":
					if m.IR.RemoteListCursor < len(m.IR.Store.Profiles) {
						m.IR.SelectRemoteFromList(m.IR.RemoteListCursor)
						m.ActiveTabs[ViewIR] = int(IRTabLearn)
						m.IR.Tab = IRTabLearn
					}
				case k == "x" || k == "backspace" || k == "delete":
					if m.IR.RemoteListCursor < len(m.IR.Store.Profiles) {
						m.IR.DeleteRemoteFromList(m.IR.RemoteListCursor)
					}
				}
				return m, nil
			}
			if len(m.IR.Store.Profiles) == 0 {
				switch {
				case m.isUp(k):
					m.IR.MoveEmptyMenu(-1)
				case m.isDown(k):
					m.IR.MoveEmptyMenu(1)
				case k == "enter" || k == " ":
					switch m.IR.EmptyMenuCursor {
					case 0:
						m.IR.BeginNewRemote(false)
					case 1:
						m.ActiveTabs[ViewIR] = int(IRTabLearn)
						m.IR.Tab = IRTabLearn
					case 2:
						m.ActiveTabs[ViewIR] = int(IRTabBrowse)
						m.IR.Tab = IRTabBrowse
						m.IR.BrowseMode = browseModeList
						if !m.IR.IndexDone && !m.IR.IndexLoading {
							m.IR.RemoteErr = ""
							m.IR.IndexLoading = true
							return m, tea.Batch(m.setStatus("Indexing IR databases..."), irIndexCmd(m.IR.Roots))
						}
					case 3:
						m.ActiveTabs[ViewIR] = int(IRTabBrowse)
						m.IR.Tab = IRTabBrowse
						m.IR.StartMatchWizard()
						if !m.IR.IndexDone && !m.IR.IndexLoading {
							m.IR.RemoteErr = ""
							m.IR.IndexLoading = true
							return m, tea.Batch(m.setStatus("Indexing IR databases..."), irIndexCmd(m.IR.Roots))
						}
					case 4:
						m.ActiveTabs[ViewIR] = int(IRTabBrowse)
						m.IR.Tab = IRTabBrowse
						m.IR.StartUniversalWizard()
						if !m.IR.IndexDone && !m.IR.IndexLoading {
							m.IR.RemoteErr = ""
							m.IR.IndexLoading = true
							return m, tea.Batch(m.setStatus("Indexing IR databases..."), irIndexCmd(m.IR.Roots))
						}
					}
				}
				return m, nil
			}
			if b, ok := m.IR.BindingForKey(k); ok {
				return m.sendIR(b.Signal, b.Name)
			}
			switch {
			case k == "=":
				m.IR.OpenRemap(m.IR.Store.Active, remoteModeUse)
			case k == "tab":
				m.IR.OpenRemoteList()
			case m.isUp(k):
				if m.IR.RemoteScroll > 0 {
					m.IR.RemoteScroll--
				}
			case m.isDown(k):
				m.IR.RemoteScroll++
			case m.isLeft(k) || k == "[":
				m.IR.CycleProfile(-1)
			case m.isRight(k) || k == "]":
				m.IR.CycleProfile(1)
			case k == "enter":
				if p := m.IR.Store.ActiveProfile(); p != nil && len(p.Bindings) == 0 {
					m.ActiveTabs[ViewIR] = int(IRTabLearn)
					m.IR.Tab = IRTabLearn
					return m, nil
				}
			}
			if p := m.IR.Store.ActiveProfile(); p != nil {
				maxScroll := max(0, len(p.Bindings)-max(1, bodyH-2))
				if m.IR.RemoteScroll > maxScroll {
					m.IR.RemoteScroll = maxScroll
				}
			}
			return m, nil

		case IRTabLearn:
			switch {
			case m.IR.Capturing:
				return m, nil
			case m.IR.AwaitBinding && m.IR.Learned != nil:
				switch {
				case m.isUp(k):
					m.IR.MoveCapturedMenu(-1)
				case m.isDown(k):
					m.IR.MoveCapturedMenu(1)
				case k == "t":
					return m.sendIR(*m.IR.Learned, "Learned")
				case k == "l":
					return m.startIRCapture()
				case k == "enter" || k == " ":
					switch learnCapturedOption(m.IR.CapturedCursor) {
					case learnCapBind:
						m.IR.BeginBindKey()
					case learnCapTest:
						return m.sendIR(*m.IR.Learned, "Learned")
					case learnCapAgain:
						return m.startIRCapture()
					case learnCapDiscard:
						m.IR.DiscardLearned()
					}
				}
			default:
				switch {
				case m.isUp(k):
					m.IR.MoveLearnMenu(-1)
				case m.isDown(k):
					m.IR.MoveLearnMenu(1)
				case m.isLeft(k):
					if learnMenuOption(m.IR.LearnCursor) == learnOptProfile {
						m.IR.CycleProfile(-1)
					}
				case m.isRight(k):
					if learnMenuOption(m.IR.LearnCursor) == learnOptProfile {
						m.IR.CycleProfile(1)
					}
				case k == "l":
					return m.startIRCapture()
				case k == "enter" || k == " ":
					switch learnMenuOption(m.IR.LearnCursor) {
					case learnOptListen:
						return m.startIRCapture()
					case learnOptProfile:
						if len(m.IR.Store.Profiles) > 1 {
							m.IR.CycleProfile(1)
						} else if len(m.IR.Store.Profiles) == 0 {
							m.IR.BeginNewRemote(true)
						}
					case learnOptNewRemote:
						m.IR.BeginNewRemote(true)
					}
				}
			}
			return m, nil

		case IRTabBrowse:
			if m.IR.Remote == nil {
				if m.IR.IndexLoading {
					return m, nil
				}
				switch m.IR.BrowseMode {
				case browseModeMatchType:
					switch {
					case m.isUp(k):
						m.IR.MoveType(-1)
					case m.isDown(k):
						m.IR.MoveType(1)
					case k == "enter" || k == " ":
						m.IR.ConfirmMatchType()
					}
					return m, nil

				case browseModeMatchBrand:
					switch {
					case m.isUp(k):
						m.IR.MoveMatchBrand(-1)
					case m.isDown(k):
						m.IR.MoveMatchBrand(1)
					case k == "[":
						m.IR.JumpMatchBrandLetter(-1)
					case k == "]":
						m.IR.JumpMatchBrandLetter(1)
					case k == "/":
						m.IR.MatchBrandSearching = true
					case k == "enter" || k == " ":
						if cat, brand, ok := m.IR.BeginMatchCandidates(); ok {
							return m, tea.Batch(m.setStatus("Loading "+brand+"..."), irMatchCandidatesCmd(m.IR.Index, cat, brand))
						}
					}
					return m, nil

				case browseModeMatchTest:
					if m.IR.MatchLoading || m.IR.MatchSaving {
						return m, nil
					}
					switch {
					case m.isUp(k):
						m.IR.MoveMatchAction(-1)
					case m.isDown(k):
						m.IR.MoveMatchAction(1)
					case m.isLeft(k):
						if cand, ok := m.IR.StepMatchCandidate(-1); ok {
							return m.sendIR(cand.Signal, fmt.Sprintf("Power #%d", m.IR.MatchCursor+1))
						}
					case m.isRight(k) || k == "n":
						if cand, ok := m.IR.StepMatchCandidate(1); ok {
							return m.sendIR(cand.Signal, fmt.Sprintf("Power #%d", m.IR.MatchCursor+1))
						}
					case k == "p":
						if cand, ok := m.IR.CurrentMatchCandidate(); ok {
							return m.sendIR(cand.Signal, fmt.Sprintf("Power #%d", m.IR.MatchCursor+1))
						}
					case k == "y" || k == "s" || k == "a":
						if cand, ok := m.IR.CurrentMatchCandidate(); ok {
							m.IR.MatchSaving = true
							return m, tea.Batch(m.setStatus("Saving remote..."), irMatchSaveCmd(cand))
						}
					case k == "enter" || k == " ":
						switch m.IR.MatchActionCursor {
						case 0:
							if cand, ok := m.IR.CurrentMatchCandidate(); ok {
								return m.sendIR(cand.Signal, fmt.Sprintf("Power #%d", m.IR.MatchCursor+1))
							}
						case 1:
							if cand, ok := m.IR.StepMatchCandidate(1); ok {
								return m.sendIR(cand.Signal, fmt.Sprintf("Power #%d", m.IR.MatchCursor+1))
							}
						case 2:
							if cand, ok := m.IR.CurrentMatchCandidate(); ok {
								m.IR.MatchSaving = true
								return m, tea.Batch(m.setStatus("Saving remote..."), irMatchSaveCmd(cand))
							}
						}
					}
					return m, nil

				case browseModeUnivType:
					switch {
					case m.isUp(k):
						m.IR.MoveType(-1)
					case m.isDown(k):
						m.IR.MoveType(1)
					case k == "enter" || k == " ":
						m.IR.ConfirmUnivType()
					}
					return m, nil

				case browseModeUnivRemote:
					if m.IR.UnivBlasting {
						if k == " " || k == "enter" || k == "s" {
							m.IR.StopUniversalBlast()
							return m, m.setStatus("IR blast stopped")
						}
						return m, nil
					}
					switch {
					case m.isUp(k):
						m.IR.MoveUnivButton(-1)
					case m.isDown(k):
						m.IR.MoveUnivButton(1)
					case k == "enter" || k == " ":
						if btn, ok := m.IR.SelectedUnivButton(); ok {
							return m.startIRUniversalBlast(btn)
						}
					default:
						if btn, idx, ok := m.IR.UnivButtonForKey(k); ok {
							m.IR.UnivCursor = idx
							return m.startIRUniversalBlast(btn)
						}
					}
					return m, nil
				}

				switch {
				case m.isUp(k):
					m.IR.MoveBrowse(-1)
				case m.isDown(k):
					m.IR.MoveBrowse(1)
				case m.isLeft(k):
					m.IR.CycleCategory(-1)
				case m.isRight(k):
					m.IR.CycleCategory(1)
				case k == "[":
					m.IR.JumpBrand(-1)
				case k == "]":
					m.IR.JumpBrand(1)
				case k == "/":
					m.IR.BrowseSearching = true
				case k == "p":
					m.IR.CycleProvider(1)
				case k == "m":
					m.IR.StartMatchWizard()
				case k == "u":
					m.IR.StartUniversalWizard()
				case k == "enter":
					if e, ok := m.IR.SelectedEntry(); ok {
						m.IR.RemoteLoading = true
						return m, irLoadCmd(e)
					}
				}
				return m, nil
			}
			switch {
			case k == "g":
				m.IR.GridMode = !m.IR.GridMode
			case k == "a":
				if m.IR.AcceptRemote() {
					m.ActiveTabs[ViewIR] = int(IRTabRemote)
					if m.IR.NoteIsErr {
						return m, m.setStatus("IR save failed")
					}
					return m, m.setStatus("IR profile saved")
				}
				return m, m.setStatus("No IR signals to save")
			case !m.IR.GridMode && k == "p":
				return m.irProbe("Power")
			case !m.IR.GridMode && k == "v":
				return m.irProbe("Vol_up")
			case !m.IR.GridMode && k == "m":
				return m.irProbe("Mute")
			case m.IR.GridMode && ir.IsBindableKey(k):
				if m.IR.HW.TXPath == "" {
					m.IR.Note = "No IR transmitter: " + m.IR.HW.Err
					m.IR.NoteIsErr = true
					return m, m.setStatus("No IR transmitter")
				}
				if sig, name, ok := m.IR.gridSignal(k); ok {
					m.IR.advanceGrid(k)
					return m.sendIR(sig, name)
				}
			}
			return m, nil
		}
		return m, nil

	case ViewSDR:
		return m.handleSDRKey(k)

	case ViewStatus:
		_, mainW, bodyH := m.dimensions()
		pal := theme.Resolve(m.Cfg.Theme, false)
		switch m.ActiveTabs[ViewStatus] {
		case StatusTabWiFi:
			switch {
			case m.isLeft(k):
				m.Wifi.MoveInterface(-1)
			case m.isRight(k):
				m.Wifi.MoveInterface(1)
			case m.isUp(k):
				m.Wifi.MoveScroll(-1, mainW, bodyH, pal)
			case m.isDown(k):
				m.Wifi.MoveScroll(1, mainW, bodyH, pal)
			case k == "enter" || k == " ":
				if len(m.Wifi.Interfaces) == 0 {
					m.Wifi.Scanning = true
				}
				return m, tea.Batch(m.setStatus("Scanning WiFi..."), collectWifiCmd())
			}
			return m, nil

		case StatusTabBT:
			switch {
			case m.isLeft(k):
				m.Bluetooth.MoveAdapter(-1)
			case m.isRight(k):
				m.Bluetooth.MoveAdapter(1)
			case m.isUp(k):
				m.Bluetooth.MoveScroll(-1, mainW, bodyH, pal)
			case m.isDown(k):
				m.Bluetooth.MoveScroll(1, mainW, bodyH, pal)
			case k == "enter" || k == " ":
				if len(m.Bluetooth.Adapters) == 0 {
					m.Bluetooth.Scanning = true
				}
				return m, tea.Batch(m.setStatus("Scanning BT..."), collectBluetoothCmd())
			}
			return m, nil

		default:
			totalRows := len(m.Status.BuildRows(mainW, pal, m.Cfg, m.Snap))
			switch {
			case m.isUp(k):
				m.Status.Move(-1, totalRows, bodyH)
			case m.isDown(k):
				m.Status.Move(1, totalRows, bodyH)
			}
			return m, nil
		}

	case ViewSystem:
		switch {
		case m.isUp(k):
			m.System.Move(-1)
			m.System.Note = ""
		case m.isDown(k):
			m.System.Move(1)
			m.System.Note = ""
		case k == "enter" || k == " ":
			return m.activateSystemOption()
		}
		return m, nil
	}

	return m, nil
}

func (m AppModel) activateSystemOption() (tea.Model, tea.Cmd) {
	switch SystemOption(m.System.Cursor) {
	case SysOptUpdate:
		if m.System.Updating {
			return m, nil
		}
		m.System.Updating = true
		m.System.Note = "Checking GitHub releases..."
		if updater.NeedsSudo() {
			exePath, err := updater.ExecutablePath()
			if err != nil {
				m.System.Updating = false
				m.System.Note = err.Error()
				return m, nil
			}
			c := exec.Command("sh", "-c", `sudo "$1" update; printf "\nPress Enter to return to CZHax..."; read -r _`, "_", exePath)
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return sudoUpdateDoneMsg{err: err}
			})
		}
		ver := m.Version
		return m, func() tea.Msg {
			res, err := updater.CheckAndUpdate(ver, false)
			return updateDoneMsg{res: res, err: err}
		}

	case SysOptRestart:
		m.RestartOnQuit = true
		return m, tea.Quit

	case SysOptTerminal:
		return m, tea.Exec(buildInteractiveShellCmd(), func(err error) tea.Msg {
			return shellExitedMsg{err: err}
		})

	case SysOptBoot:
		exePath, _ := updater.ExecutablePath()
		username := ""
		homeDir := ""
		if u, err := user.Current(); err == nil {
			username = u.Username
			homeDir = u.HomeDir
		}
		if boot.NeedsRootForToggle() {
			c := exec.Command("sudo", exePath, "boot", "toggle", username, homeDir)
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return bootToggledMsg{state: boot.DetectState(homeDir), err: err}
			})
		}
		return m, func() tea.Msg {
			st, err := boot.Toggle(username, homeDir, exePath)
			return bootToggledMsg{state: st, err: err}
		}

	case SysOptSleep:
		exePath, err := updater.ExecutablePath()
		if err != nil {
			m.System.Note = err.Error()
			return m, nil
		}
		var c *exec.Cmd
		if power.NeedsSudo() {
			c = exec.Command("sudo", exePath, "power", "sleep")
		} else {
			c = exec.Command(exePath, "power", "sleep")
		}
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return sleepDoneMsg{err: err}
		})

	case SysOptReboot:
		m.System.Confirm = "reboot"
		return m, nil

	case SysOptShutdown:
		m.System.Confirm = "shutdown"
		return m, nil
	}
	return m, nil
}

func (m *AppModel) cycleTab(delta int) {
	tabs := TopViews[m.ActiveView].Tabs
	if len(tabs) == 0 {
		return
	}
	cur := m.ActiveTabs[m.ActiveView]
	m.ActiveTabs[m.ActiveView] = (cur + delta + len(tabs)) % len(tabs)
	if m.ActiveView == ViewIR {
		m.IR.Tab = IRTab(m.ActiveTabs[ViewIR])
	}
	if m.ActiveView == ViewSDR {
		m.SDR.Tab = SDRTab(m.ActiveTabs[ViewSDR])
	}
}

func (m AppModel) isUp(k string) bool {
	if k == "up" {
		return true
	}
	return m.Cfg.NavMode == config.NavArrowsVim && k == "k"
}

func (m AppModel) isDown(k string) bool {
	if k == "down" {
		return true
	}
	return m.Cfg.NavMode == config.NavArrowsVim && k == "j"
}

func (m AppModel) isLeft(k string) bool {
	if k == "left" {
		return true
	}
	return m.Cfg.NavMode == config.NavArrowsVim && k == "h"
}

func (m AppModel) isRight(k string) bool {
	if k == "right" {
		return true
	}
	return m.Cfg.NavMode == config.NavArrowsVim && k == "l"
}

func (m AppModel) dimensions() (sideW, mainW, bodyH int) {
	w, h, _ := ResolveViewport(m.TermW, m.TermH, m.Cfg.Viewport)
	sideW = ResolveSidebarWidth(w, m.Cfg.Sidebar)
	mainW = max(16, w-sideW-1)
	if sideW+1+mainW > w {
		sideW = max(3, w-mainW-1)
	}
	showDivider := h >= 12
	showFooter := m.Cfg.ShowKeyHints || m.ConfirmExit || m.StatusMsg != ""
	bodyH = h - 1
	if showDivider {
		bodyH--
	}
	if showFooter {
		bodyH--
	}
	if bodyH < 1 {
		bodyH = 1
	}
	return sideW, mainW, bodyH
}

func (m AppModel) View() string {
	out, _ := m.renderScreen(m.MarqueeStep)
	return out
}

func (m AppModel) renderScreen(marqueeStep int) (string, bool) {
	w, h, bezel := ResolveViewport(m.TermW, m.TermH, m.Cfg.Viewport)
	pal := theme.Resolve(m.Cfg.Theme, false)

	sideW, mainW, bodyH := m.dimensions()
	showDivider := h >= 12
	showFooter := m.Cfg.ShowKeyHints || m.ConfirmExit || m.StatusMsg != ""

	var overflow bool
	mq := Marquee{Step: marqueeStep, Overflow: &overflow}

	overlayTitle := ""
	if m.ShowHelp {
		overlayTitle = "HELP"
	}

	tabsFocused := m.Focus == FocusTabs && !m.ShowHelp
	topRow := RenderTopRow(sideW, mainW, pal, m.Cfg, m.ActiveView, m.ActiveTabs[m.ActiveView], tabsFocused, overlayTitle, m.Snap, mq)
	sideLines := RenderSidebarColumn(sideW, bodyH, pal, m.Cfg, m.ActiveView, tabsFocused)

	var mainLines []string
	switch {
	case m.ShowHelp:
		helpView := m.Help
		helpView.Marquee = mq
		mainLines = helpView.RenderLines(mainW, bodyH, pal)
	case m.ActiveView == ViewWiFi:
		switch m.ActiveTabs[ViewWiFi] {
		case WiFiTabSpam:
			spamView := m.WifiSpam
			spamView.Marquee = mq
			mainLines = spamView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
		case WiFiTabEvil:
			evilView := m.WifiEvil
			evilView.Marquee = mq
			mainLines = evilView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
		default:
			scanView := m.WifiScan
			scanView.Marquee = mq
			mainLines = scanView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
		}
	case m.ActiveView == ViewLocal:
		localView := m.Local
		localView.Marquee = mq
		mainLines = localView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
	case m.ActiveView == ViewBluetooth:
		if m.ActiveTabs[ViewBluetooth] == BTTabEmu {
			btEmuView := m.BluetoothEmu
			btEmuView.Marquee = mq
			mainLines = btEmuView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
		} else {
			btScanView := m.BluetoothScan
			btScanView.Marquee = mq
			mainLines = btScanView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
		}
	case m.ActiveView == ViewFiles:
		filesView := m.Files
		filesView.Marquee = mq
		mainLines = filesView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
	case m.ActiveView == ViewIR:
		irView := m.IR
		irView.Tab = IRTab(m.ActiveTabs[ViewIR])
		irView.ASCII = m.Cfg.Border == config.BorderAscii
		irView.Marquee = mq
		mainLines = irView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
	case m.ActiveView == ViewSDR:
		sdrView := m.SDR
		sdrView.Tab = SDRTab(m.ActiveTabs[ViewSDR])
		sdrView.ASCII = m.Cfg.Border == config.BorderAscii
		sdrView.Marquee = mq
		mainLines = sdrView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
	case m.ActiveView == ViewSystem:
		sysView := m.System
		sysView.Marquee = mq
		mainLines = sysView.RenderLines(mainW, bodyH, pal, m.Version, m.Focus == FocusContent)
	default:
		switch m.ActiveTabs[ViewStatus] {
		case StatusTabWiFi:
			wifiView := m.Wifi
			wifiView.Marquee = mq
			mainLines = wifiView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
		case StatusTabBT:
			btView := m.Bluetooth
			btView.Marquee = mq
			mainLines = btView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
		default:
			statusView := m.Status
			statusView.Marquee = mq
			mainLines = statusView.RenderLines(mainW, bodyH, pal, m.Cfg, m.Snap, m.Focus == FocusContent)
		}
	}
	bodyBlock := JoinPaneColumns(sideLines, mainLines, sideW, mainW, bodyH, pal, m.Cfg)

	parts := []string{topRow}
	if showDivider {
		parts = append(parts, RenderDividerRow(sideW, mainW, pal, m.Cfg))
	}
	parts = append(parts, bodyBlock)

	if showFooter {
		status := m.StatusMsg
		if m.ConfirmExit {
			status = "Quit CZHax? [y/Enter] Yes  [n/Esc] No"
		} else if m.ActiveView == ViewSystem && m.System.Confirm != "" {
			status = "Confirm " + m.System.Confirm + "? [y/Enter] Yes  [n/Esc] No"
		}
		parts = append(parts, RenderFooter(w, pal, m.Cfg, m.currentHints(), status, mq))
	}

	screen := strings.Join(parts, "\n")
	if m.OSD.Visible {
		screen = m.OSD.Overlay(screen, w, h, pal, m.Cfg)
	}

	if !bezel {
		return screen, overflow
	}

	bdr := lipgloss.RoundedBorder()
	if m.Cfg.Border == config.BorderAscii {
		bdr = lipgloss.ASCIIBorder()
	}
	frame := lipgloss.NewStyle().
		Border(bdr).
		BorderForeground(pal.Accent).
		Render(screen)

	label := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render(fmt.Sprintf("M5Stack Cardputer Zero · %dx%d (320x170 ST7789)", w, h))
	fnLegend := lipgloss.NewStyle().
		Foreground(pal.Muted).
		Render("↑↓ view · ←→ tab · ⏎ enter · Esc tabs · r refresh · ? help")

	composed := lipgloss.JoinVertical(lipgloss.Center, label, frame, fnLegend)
	return lipgloss.Place(m.TermW, m.TermH, lipgloss.Center, lipgloss.Center, composed), overflow
}

// mouseStepScale maps the modifier state of a mouse-pad keypress onto the
// movement multiplier: Ctrl = fine (1x), no modifier = 3x, Shift = 9x.
func mouseStepScale(k string) int {
	switch {
	case strings.HasPrefix(k, "ctrl+"):
		return 1
	case strings.HasPrefix(k, "shift+"):
		return 9
	}
	if rs := []rune(k); len(rs) == 1 && unicode.IsUpper(rs[0]) {
		return 9 // shift+letter arrives as an uppercase rune
	}
	return 3
}

// unmodifiedKey lowercases and strips ctrl/shift/alt prefixes so one switch can
// serve every modifier combination.
func unmodifiedKey(k string) string {
	for _, p := range []string{"ctrl+", "shift+", "alt+"} {
		for strings.HasPrefix(k, p) {
			k = strings.TrimPrefix(k, p)
		}
	}
	// Bubble Tea keeps the shift case ("Right", "D"), so normalise the name too.
	return strings.ToLower(k)
}

// clampMouseDelta keeps a scaled step inside the 8-bit HID relative range.
func clampMouseDelta(v int) int8 {
	if v > 127 {
		return 127
	}
	if v < -127 {
		return -127
	}
	return int8(v)
}

func (m AppModel) currentHints() []Hint {
	if m.ShowHelp {
		return []Hint{{Key: "↑↓", Desc: "scroll"}, {Key: "Esc", Desc: "close"}}
	}
	if m.Focus == FocusTabs {
		return []Hint{
			{Key: "↑↓", Desc: "view"},
			{Key: "←→", Desc: "tab"},
			{Key: "⏎", Desc: "enter"},
			{Key: "r", Desc: "ref"},
			{Key: "?", Desc: "help"},
			{Key: "q", Desc: "quit"},
		}
	}
	if m.ActiveView == ViewWiFi {
		if h := m.wifiTabHints(); h != nil {
			return h
		}
		switch m.WifiScan.Mode {
		case scanModeActions:
			return []Hint{
				{Key: "↑↓", Desc: "sel"},
				{Key: "⏎", Desc: "run"},
				{Key: "Esc", Desc: "back"},
			}
		case scanModeDetail:
			return []Hint{
				{Key: "↑↓", Desc: "scr"},
				{Key: "⏎", Desc: "conn"},
				{Key: "Esc", Desc: "back"},
			}
		case scanModePickIface:
			return []Hint{
				{Key: "↑↓", Desc: "if"},
				{Key: "⏎", Desc: "use"},
				{Key: "Esc", Desc: "back"},
				{Key: "q", Desc: "quit"},
			}
		case scanModeSSID:
			return []Hint{
				{Key: "⏎", Desc: "next"},
				{Key: "Esc", Desc: "back"},
			}
		case scanModePassword:
			return []Hint{
				{Key: "⏎", Desc: "conn"},
				{Key: "Tab", Desc: "vis"},
				{Key: "Esc", Desc: "back"},
			}
		case scanModePentest:
			if m.WifiScan.PentestActive {
				if m.WifiScan.PentestMode == wifi.PentestDeauth {
					return []Hint{{Key: "key", Desc: "deauth"}, {Key: "Esc", Desc: "stop"}}
				}
				return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "q", Desc: "stop"}}
			}
			return []Hint{{Key: "Esc", Desc: "back"}, {Key: "q", Desc: "quit"}}
		case scanModeWPSMenu:
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "run"}, {Key: "Esc", Desc: "back"}}
		case scanModeWPSPin:
			return []Hint{{Key: "⏎", Desc: "run"}, {Key: "Esc", Desc: "back"}}
		case scanModeWPSRun:
			if m.WifiScan.WPSActive {
				return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "q", Desc: "stop"}}
			}
			if _, psk := m.WifiScan.HasWPSCredentials(); psk {
				return []Hint{{Key: "c", Desc: "connect"}, {Key: "Esc", Desc: "back"}}
			}
			if pin, _ := m.WifiScan.HasWPSCredentials(); pin {
				return []Hint{{Key: "p", Desc: "retry psk"}, {Key: "Esc", Desc: "back"}}
			}
			return []Hint{{Key: "Esc", Desc: "back"}}
		default:
			return []Hint{
				{Key: "↑↓", Desc: "sel"},
				{Key: "←→", Desc: "if"},
				{Key: "s", Desc: "scan"},
				{Key: "o", Desc: "color"},
				{Key: "⏎", Desc: "act"},
				{Key: "Esc", Desc: "tabs"},
			}
		}
	}
	if m.ActiveView == ViewLocal {
		switch m.Local.Mode {
		case localModeOptions:
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "scan"}, {Key: "Esc", Desc: "back"}}
		case localModeRunning:
			return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "q", Desc: "stop"}}
		case localModeResults:
			if len(m.Local.Result.Hosts) > 0 {
				return []Hint{{Key: "↑↓", Desc: "host"}, {Key: "⏎", Desc: "ports"}, {Key: "s", Desc: "scan"}, {Key: "Esc", Desc: "opts"}}
			}
			return []Hint{{Key: "⏎", Desc: "rescan"}, {Key: "Esc", Desc: "back"}}
		case localModeHostDetail:
			return []Hint{{Key: "↑↓", Desc: "scr"}, {Key: "s", Desc: "scan"}, {Key: "Esc", Desc: "back"}}
		default:
			return []Hint{{Key: "↑↓", Desc: "if"}, {Key: "⏎", Desc: "next"}, {Key: "r", Desc: "ref"}, {Key: "Esc", Desc: "tabs"}}
		}
	}
	if m.ActiveView == ViewBluetooth {
		if m.ActiveTabs[ViewBluetooth] == BTTabEmu {
			switch m.BluetoothEmu.Mode {
			case btEmuEditName, btEmuEditCoD, btEmuEditUUID:
				return []Hint{{Key: "⏎", Desc: "save"}, {Key: "Esc", Desc: "cancel"}}
			case btEmuPlayWAV:
				return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "⏎", Desc: "stop"}}
			case btEmuActive:
				switch m.BluetoothEmu.SubMode {
				case btEmuCtrlLiveKB:
					return []Hint{{Key: "key", Desc: "type"}, {Key: "Esc", Desc: "menu"}}
				case btEmuCtrlSendText:
					return []Hint{{Key: "⏎", Desc: "send"}, {Key: "Esc", Desc: "menu"}}
				case btEmuCtrlMacros:
					return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "send"}, {Key: "Esc", Desc: "menu"}}
				case btEmuCtrlMouse:
					return []Hint{{Key: "↑↓←→", Desc: "move"}, {Key: "⏎/r", Desc: "click"}, {Key: "+/-", Desc: "spd"}, {Key: "Esc", Desc: "menu"}}
				case btEmuCtrlGamepad:
					return []Hint{{Key: "↑↓←→", Desc: "pad"}, {Key: "zxcv", Desc: "btn"}, {Key: "Esc", Desc: "menu"}}
				case btEmuCtrlMedia:
					return []Hint{{Key: "⏎", Desc: "play"}, {Key: "←→", Desc: "trk"}, {Key: "↑↓", Desc: "vol"}, {Key: "Esc", Desc: "menu"}}
				case btEmuCtrlLog:
					return []Hint{{Key: "↑↓", Desc: "scr"}, {Key: "Esc", Desc: "menu"}}
				default:
					return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "run"}, {Key: "Esc", Desc: "stop"}}
				}
			default:
				return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "←→", Desc: "chg"}, {Key: "⏎", Desc: "ok"}, {Key: "Esc", Desc: "tabs"}}
			}
		}
		switch m.BluetoothScan.Mode {
		case btModeActions:
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "run"}, {Key: "Esc", Desc: "back"}}
		case btModeCWMenu:
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "run"}, {Key: "Esc", Desc: "back"}}
		case btModeCWAudio:
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "play"}, {Key: "Esc", Desc: "back"}}
		case btModeCWRecord:
			return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "⏎", Desc: "stop"}}
		case btModeCWTarget:
			return []Hint{{Key: "⏎", Desc: "save"}, {Key: "Esc", Desc: "cancel"}}
		case btModeDetail:
			return []Hint{{Key: "↑↓", Desc: "scr"}, {Key: "Esc", Desc: "back"}}
		case btModeRunning:
			if m.BluetoothScan.RunActive {
				return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "q", Desc: "stop"}}
			}
			return []Hint{{Key: "Esc", Desc: "back"}}
		default:
			return []Hint{
				{Key: "↑↓", Desc: "sel"},
				{Key: "←→", Desc: "if"},
				{Key: "s", Desc: "scan"},
				{Key: "m", Desc: "mode"},
				{Key: "⏎", Desc: "act"},
				{Key: "Esc", Desc: "tabs"},
			}
		}
	}
	if m.ActiveView == ViewFiles {
		switch m.Files.Mode {
		case filesModeText:
			return []Hint{{Key: "Ctrl+S", Desc: "save"}, {Key: "Esc", Desc: "back"}}
		case filesModeInput:
			return []Hint{{Key: "⏎", Desc: "ok"}, {Key: "Esc", Desc: "cancel"}}
		case filesModeDelete:
			return []Hint{{Key: "y", Desc: "delete"}, {Key: "n", Desc: "cancel"}}
		case filesModeImage:
			return []Hint{{Key: "Esc", Desc: "back"}}
		case filesModeRecord:
			return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "⏎", Desc: "stop"}}
		case filesModePlay:
			return []Hint{{Key: "⏎", Desc: "pause"}, {Key: "←→", Desc: "±5s"}, {Key: "Esc", Desc: "stop"}}
		default:
			return []Hint{
				{Key: "⏎", Desc: "open"},
				{Key: "n", Desc: "file"},
				{Key: "m", Desc: "dir"},
				{Key: "a", Desc: "rec"},
				{Key: "x", Desc: "del"},
				{Key: "r", Desc: "ren"},
				{Key: "Esc", Desc: "up"},
			}
		}
	}
	if m.ActiveView == ViewSDR {
		return m.sdrHints()
	}
	if m.ActiveView == ViewIR {
		switch m.IR.Tab {
		case IRTabLearn:
			if m.IR.NewRemoteInput || m.IR.AwaitBindLabel {
				return []Hint{{Key: "⏎", Desc: "save"}, {Key: "Esc", Desc: "back"}}
			}
			if m.IR.Capturing {
				return []Hint{{Key: "Esc", Desc: "cancel"}, {Key: "q", Desc: "stop"}}
			}
			if m.IR.IsKeyCaptureActive() {
				return []Hint{{Key: "key", Desc: "bind"}, {Key: "Esc", Desc: "back"}}
			}
			if m.IR.AwaitBinding {
				return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "run"}, {Key: "Esc", Desc: "discard"}}
			}
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "run"}, {Key: "Esc", Desc: "tabs"}}
		case IRTabBrowse:
			if m.IR.Remote != nil {
				if m.IR.GridMode {
					return []Hint{{Key: "key", Desc: "send/next"}, {Key: "a", Desc: "accept"}, {Key: "g", Desc: "probe"}, {Key: "Esc", Desc: "back"}}
				}
				return []Hint{{Key: "p/v/m", Desc: "try"}, {Key: "a", Desc: "accept"}, {Key: "g", Desc: "grid"}, {Key: "Esc", Desc: "back"}}
			}
			switch m.IR.BrowseMode {
			case browseModeMatchType:
				return []Hint{{Key: "↑↓", Desc: "type"}, {Key: "⏎", Desc: "brands"}, {Key: "Esc", Desc: "back"}}
			case browseModeMatchBrand:
				if m.IR.MatchBrandSearching {
					return []Hint{{Key: "⏎", Desc: "done"}, {Key: "Esc", Desc: "clear"}}
				}
				return []Hint{{Key: "↑↓", Desc: "brand"}, {Key: "/", Desc: "find"}, {Key: "⏎", Desc: "test"}, {Key: "Esc", Desc: "back"}}
			case browseModeMatchTest:
				return []Hint{{Key: "⏎", Desc: "run"}, {Key: "←→", Desc: "step"}, {Key: "y", Desc: "save"}, {Key: "Esc", Desc: "back"}}
			case browseModeUnivType:
				return []Hint{{Key: "↑↓", Desc: "type"}, {Key: "⏎", Desc: "open"}, {Key: "Esc", Desc: "back"}}
			case browseModeUnivRemote:
				if m.IR.UnivBlasting {
					return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "Space", Desc: "stop"}}
				}
				return []Hint{{Key: "key/⏎", Desc: "blast"}, {Key: "↑↓", Desc: "sel"}, {Key: "Esc", Desc: "back"}}
			}
			if m.IR.BrowseSearching {
				return []Hint{{Key: "⏎", Desc: "done"}, {Key: "Esc", Desc: "clear"}}
			}
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "m", Desc: "match"}, {Key: "u", Desc: "univ"}, {Key: "/", Desc: "find"}, {Key: "⏎", Desc: "open"}}
		default:
			if m.IR.RemoteMode == remoteModeRemap {
				if m.IR.RemapAwaitLabel {
					return []Hint{{Key: "⏎", Desc: "save"}, {Key: "Esc", Desc: "cancel"}}
				}
				if m.IR.RemapAwaitKey {
					return []Hint{{Key: "key", Desc: "bind"}, {Key: "Esc", Desc: "cancel"}}
				}
				return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "remap"}, {Key: "n", Desc: "name"}, {Key: "x", Desc: "del"}, {Key: "Esc", Desc: "done"}}
			}
			if m.IR.RemoteMode == remoteModeNew {
				return []Hint{{Key: "⏎", Desc: "create"}, {Key: "Esc", Desc: "cancel"}}
			}
			if m.IR.RemoteMode == remoteModeList {
				return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "use"}, {Key: "e", Desc: "map"}, {Key: "l", Desc: "learn"}, {Key: "x", Desc: "del"}}
			}
			if len(m.IR.Store.Profiles) == 0 {
				return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "open"}, {Key: "Esc", Desc: "tabs"}}
			}
			return []Hint{{Key: "key", Desc: "send"}, {Key: "←→", Desc: "rem"}, {Key: "=", Desc: "remap"}, {Key: "Tab", Desc: "list"}}
		}
	}
	if m.ActiveView == ViewSystem {
		return []Hint{
			{Key: "↑↓", Desc: "sel"},
			{Key: "⏎", Desc: "run"},
			{Key: "Esc", Desc: "tabs"},
			{Key: "q", Desc: "quit"},
		}
	}
	if m.ActiveTabs[ViewStatus] != StatusTabDevice {
		return []Hint{
			{Key: "←→", Desc: "if"},
			{Key: "↑↓", Desc: "scr"},
			{Key: "r", Desc: "ref"},
			{Key: "Esc", Desc: "tabs"},
			{Key: "q", Desc: "quit"},
		}
	}
	return []Hint{
		{Key: "↑↓", Desc: "scroll"},
		{Key: "r", Desc: "ref"},
		{Key: "Esc", Desc: "tabs"},
		{Key: "q", Desc: "quit"},
	}
}

func (m AppModel) handleBluetoothEmuDirectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	v := &m.BluetoothEmu

	if k == "esc" {
		v.HandleEsc()
		return m, m.resetMarquee()
	}

	switch v.Mode {
	case btEmuEditName, btEmuEditCoD, btEmuEditUUID:
		switch k {
		case "enter":
			val := strings.TrimSpace(v.InputBuf)
			switch v.Mode {
			case btEmuEditName:
				if val != "" {
					v.CustomName = val
					v.NameEdited = true
				}
			case btEmuEditCoD:
				if val == "" {
					v.CustomCoDHex = ""
				} else if parsed, err := bluetooth.ParseHexUint32(val); err == nil {
					v.CustomCoDHex = fmt.Sprintf("0x%06X", parsed)
				} else {
					v.ErrMsg = "Invalid hex CoD"
				}
			case btEmuEditUUID:
				v.CustomUUID = val
			}
			v.Mode = btEmuSetup
			v.InputBuf = ""
			return m, m.resetMarquee()
		case "backspace":
			if len(v.InputBuf) > 0 {
				rs := []rune(v.InputBuf)
				v.InputBuf = string(rs[:len(rs)-1])
			}
		default:
			if msg.Type == tea.KeySpace {
				v.InputBuf += " "
			} else if len(msg.Runes) > 0 {
				v.InputBuf += string(msg.Runes)
			}
		}
		return m, nil
	}

	if v.Mode != btEmuActive {
		return m, nil
	}

	switch v.SubMode {
	case btEmuCtrlLiveKB:
		sendKey := k
		if msg.Type == tea.KeySpace {
			sendKey = "space"
		} else if len(msg.Runes) == 1 && !strings.HasPrefix(k, "ctrl+") && !strings.HasPrefix(k, "alt+") {
			sendKey = string(msg.Runes)
		}
		if code, mods, ok := bluetooth.KeyToHID(sendKey); ok {
			v.SendCmd(bluetooth.EmuCommand{Op: "key", Code: code, Mods: mods})
			v.LastSent = sendKey
		} else {
			v.LastSent = "unmapped: " + k
		}
		return m, m.resetMarquee()

	case btEmuCtrlSendText:
		switch k {
		case "enter":
			txt := v.InputBuf
			v.InputBuf = ""
			if txt != "" {
				v.SendCmd(bluetooth.EmuCommand{Op: "text", Text: txt})
				v.LastSent = txt
			}
			v.SubMode = btEmuCtrlMenu
			return m, m.resetMarquee()
		case "backspace":
			if len(v.InputBuf) > 0 {
				rs := []rune(v.InputBuf)
				v.InputBuf = string(rs[:len(rs)-1])
			}
		default:
			if msg.Type == tea.KeySpace {
				v.InputBuf += " "
			} else if len(msg.Runes) > 0 {
				v.InputBuf += string(msg.Runes)
			}
		}
		return m, nil

	case btEmuCtrlMouse:
		// Ctrl keeps the fine step, a bare arrow/WASD press is 3x that, and
		// Shift is 3x again so the cursor can cross a screen in a few presses.
		step := clampMouseDelta(int(v.MouseStep()) * mouseStepScale(k))
		key := unmodifiedKey(k)
		var dragBtn uint8
		if v.MouseDrag {
			dragBtn = 0x01
		}
		switch key {
		case "up", "w":
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse", Buttons: dragBtn, DY: -step})
			v.LastSent = fmt.Sprintf("move 0,-%d", step)
		case "down", "s":
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse", Buttons: dragBtn, DY: step})
			v.LastSent = fmt.Sprintf("move 0,+%d", step)
		case "left", "a":
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse", Buttons: dragBtn, DX: -step})
			v.LastSent = fmt.Sprintf("move -%d,0", step)
		case "right", "d":
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse", Buttons: dragBtn, DX: step})
			v.LastSent = fmt.Sprintf("move +%d,0", step)
		case "g":
			v.MouseDrag = !v.MouseDrag
			var btn uint8
			if v.MouseDrag {
				btn = 0x01
				v.LastSent = "drag HOLD"
			} else {
				v.LastSent = "drag OFF"
			}
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse", Buttons: btn})
		case "enter", " ", "l", "1":
			v.MouseDrag = false
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse_click", Buttons: 0x01})
			v.LastSent = "left click"
		case "r", "2":
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse_click", Buttons: 0x02})
			v.LastSent = "right click"
		case "m", "3":
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse_click", Buttons: 0x04})
			v.LastSent = "middle click"
		case "u", "[":
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse", Buttons: dragBtn, Wheel: 2})
			v.LastSent = "wheel up"
		case "j", "]":
			v.SendCmd(bluetooth.EmuCommand{Op: "mouse", Buttons: dragBtn, Wheel: -2})
			v.LastSent = "wheel down"
		case "+", "=":
			v.MouseStepIdx = min(2, v.MouseStepIdx+1)
			v.LastSent = "speed " + v.MouseStepLabel()
		case "-", "_":
			v.MouseStepIdx = max(0, v.MouseStepIdx-1)
			v.LastSent = "speed " + v.MouseStepLabel()
		}
		return m, m.resetMarquee()

	case btEmuCtrlGamepad:
		switch k {
		case "up":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", DY: -127})
			v.LastSent = "pad up"
		case "down":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", DY: 127})
			v.LastSent = "pad down"
		case "left":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", DX: -127})
			v.LastSent = "pad left"
		case "right":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", DX: 127})
			v.LastSent = "pad right"
		case "z", "a":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", GBtn: 1 << 0})
			v.LastSent = "btn A"
		case "x", "b":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", GBtn: 1 << 1})
			v.LastSent = "btn B"
		case "c":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", GBtn: 1 << 2})
			v.LastSent = "btn X"
		case "v":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", GBtn: 1 << 3})
			v.LastSent = "btn Y"
		case "q":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", GBtn: 1 << 4})
			v.LastSent = "btn L1"
		case "e":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", GBtn: 1 << 5})
			v.LastSent = "btn R1"
		case "enter":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", GBtn: 1 << 7})
			v.LastSent = "Start"
		case " ":
			v.SendCmd(bluetooth.EmuCommand{Op: "gamepad", GBtn: 1 << 6})
			v.LastSent = "Select"
		}
		return m, m.resetMarquee()

	case btEmuCtrlMedia:
		switch k {
		case "enter", " ":
			v.SendCmd(bluetooth.EmuCommand{Op: "consumer", Usage: bluetooth.ConsumerPlayPause})
			v.LastSent = "Play/Pause"
		case "left":
			v.SendCmd(bluetooth.EmuCommand{Op: "consumer", Usage: bluetooth.ConsumerPrevTrack})
			v.LastSent = "Prev Track"
		case "right":
			v.SendCmd(bluetooth.EmuCommand{Op: "consumer", Usage: bluetooth.ConsumerNextTrack})
			v.LastSent = "Next Track"
		case "up":
			v.SendCmd(bluetooth.EmuCommand{Op: "consumer", Usage: bluetooth.ConsumerVolUp})
			v.LastSent = "Volume +"
		case "down":
			v.SendCmd(bluetooth.EmuCommand{Op: "consumer", Usage: bluetooth.ConsumerVolDown})
			v.LastSent = "Volume -"
		case "m":
			v.SendCmd(bluetooth.EmuCommand{Op: "consumer", Usage: bluetooth.ConsumerMute})
			v.LastSent = "Mute"
		case "h":
			v.SendCmd(bluetooth.EmuCommand{Op: "consumer", Usage: bluetooth.ConsumerHome})
			v.LastSent = "Home"
		}
		return m, m.resetMarquee()
	}

	return m, nil
}

func (m AppModel) handleBluetoothEmuKey(k string) (tea.Model, tea.Cmd) {
	v := &m.BluetoothEmu
	switch v.Mode {
	case btEmuPlayWAV:
		if k == "enter" || k == " " {
			v.StopPlayLast()
			v.Mode = btEmuSetup
		}
		return m, nil

	case btEmuActive:
		switch v.SubMode {
		case btEmuCtrlMacros:
			switch {
			case m.isUp(k):
				v.MoveMacro(-1)
			case m.isDown(k):
				v.MoveMacro(1)
			case k == "enter" || k == " ":
				mc := btEmuMacros[clampIndex(v.MacroCursor, len(btEmuMacros))]
				v.SendCmd(bluetooth.EmuCommand{Op: mc.Op, Key: mc.Key, Code: mc.Code, Mods: mc.Mods, Usage: mc.Usage})
				v.LastSent = mc.Label
				return m, tea.Batch(m.setStatus("Sent "+mc.Label), m.resetMarquee())
			}
			return m, nil

		case btEmuCtrlLog:
			_, _, bodyH := m.dimensions()
			slots := max(1, bodyH-2)
			maxScr := max(0, len(v.Logs)-slots)
			switch {
			case m.isUp(k):
				v.LogScroll = max(0, v.LogScroll-1)
			case m.isDown(k):
				v.LogScroll = min(maxScr, v.LogScroll+1)
			}
			return m, nil

		default: // btEmuCtrlMenu
			items := v.menuItems()
			switch {
			case m.isUp(k):
				v.MoveMenu(-1)
			case m.isDown(k):
				v.MoveMenu(1)
			case k == "m" && v.Spec.IsAudio:
				v.MonitorAudio = !v.MonitorAudio
				v.SendCmd(bluetooth.EmuCommand{Op: "monitor", Enabled: v.MonitorAudio})
			case k == "p" && v.Spec.IsAudio:
				act := "pause"
				if v.AVRCPPaused {
					act = "play"
				}
				v.AVRCPPaused = !v.AVRCPPaused
				v.SendCmd(bluetooth.EmuCommand{Op: "avrcp", Action: act})
			case k == "n" && v.Spec.IsAudio:
				v.SendCmd(bluetooth.EmuCommand{Op: "avrcp", Action: "next"})
			case k == "b" && v.Spec.IsAudio:
				v.SendCmd(bluetooth.EmuCommand{Op: "avrcp", Action: "prev"})
			case k == "enter" || k == " ":
				if len(items) == 0 {
					return m, nil
				}
				act := items[clampIndex(v.MenuCursor, len(items))]
				switch act {
				case emuActLiveKB:
					v.SubMode = btEmuCtrlLiveKB
				case emuActSendText:
					v.InputBuf = ""
					v.SubMode = btEmuCtrlSendText
				case emuActMacros:
					v.SubMode = btEmuCtrlMacros
				case emuActMouse:
					v.SubMode = btEmuCtrlMouse
				case emuActGamepad:
					v.SubMode = btEmuCtrlGamepad
				case emuActMedia:
					v.SubMode = btEmuCtrlMedia
				case emuActMonitor:
					v.MonitorAudio = !v.MonitorAudio
					v.SendCmd(bluetooth.EmuCommand{Op: "monitor", Enabled: v.MonitorAudio})
				case emuActRecord:
					v.RecordAudio = !v.RecordAudio
					v.SendCmd(bluetooth.EmuCommand{Op: "record", Enabled: v.RecordAudio})
				case emuActAVRCPPlay:
					action := "pause"
					if v.AVRCPPaused {
						action = "play"
					}
					v.AVRCPPaused = !v.AVRCPPaused
					v.SendCmd(bluetooth.EmuCommand{Op: "avrcp", Action: action})
				case emuActAVRCPNext:
					v.SendCmd(bluetooth.EmuCommand{Op: "avrcp", Action: "next"})
				case emuActAVRCPPrev:
					v.SendCmd(bluetooth.EmuCommand{Op: "avrcp", Action: "prev"})
				case emuActLog:
					_, _, bodyH := m.dimensions()
					slots := max(1, bodyH-2)
					v.LogScroll = max(0, len(v.Logs)-slots)
					v.SubMode = btEmuCtrlLog
				case emuActStop:
					v.StopEmulation()
					if !v.Active {
						v.Mode = btEmuSetup
					}
				}
			}
			return m, m.resetMarquee()
		}
	}

	// btEmuSetup
	fields := v.setupFields()
	if len(fields) == 0 {
		return m, nil
	}
	cur := clampIndex(v.SetupCursor, len(fields))
	f := fields[cur]

	switch {
	case m.isUp(k):
		v.MoveSetup(-1)
	case m.isDown(k):
		v.MoveSetup(1)
	case m.isLeft(k):
		switch f {
		case emuFieldProfile:
			v.CycleProfile(-1)
		case emuFieldAdapter:
			v.CycleAdapter(-1)
			return m, collectBluetoothCmd()
		case emuFieldCustomMode:
			v.CycleCustomMode(-1)
		case emuFieldClass:
			v.CycleCoDPreset(-1)
		case emuFieldMonitor:
			v.MonitorAudio = !v.MonitorAudio
		case emuFieldRecord:
			v.RecordAudio = !v.RecordAudio
		}
	case m.isRight(k):
		switch f {
		case emuFieldProfile:
			v.CycleProfile(1)
		case emuFieldAdapter:
			v.CycleAdapter(1)
			return m, collectBluetoothCmd()
		case emuFieldCustomMode:
			v.CycleCustomMode(1)
		case emuFieldClass:
			v.CycleCoDPreset(1)
		case emuFieldMonitor:
			v.MonitorAudio = !v.MonitorAudio
		case emuFieldRecord:
			v.RecordAudio = !v.RecordAudio
		}
	case k == "enter" || k == " ":
		switch f {
		case emuFieldProfile:
			v.CycleProfile(1)
		case emuFieldName:
			v.InputBuf = v.CustomName
			v.Mode = btEmuEditName
		case emuFieldAdapter:
			v.CycleAdapter(1)
			return m, collectBluetoothCmd()
		case emuFieldCustomMode:
			v.CycleCustomMode(1)
		case emuFieldClass:
			if v.CustomCoDHex != "" {
				v.InputBuf = v.CustomCoDHex
			} else {
				preset := bluetooth.EmuCoDPresets[clampIndex(v.CoDPresetIdx, len(bluetooth.EmuCoDPresets))]
				v.InputBuf = fmt.Sprintf("0x%06X", preset.CoD)
			}
			v.Mode = btEmuEditCoD
		case emuFieldCustomUUID:
			v.InputBuf = v.CustomUUID
			v.Mode = btEmuEditUUID
		case emuFieldMonitor:
			v.MonitorAudio = !v.MonitorAudio
		case emuFieldRecord:
			v.RecordAudio = !v.RecordAudio
		case emuFieldStart:
			return m.startBluetoothEmu()
		case emuFieldPlayLast:
			ctx, seq, path, ok := v.StartPlayLast()
			if ok {
				return m, tea.Batch(m.setStatus("Playing capture..."), playBTEmuAudioCmd(ctx, seq, path), btEmuPlayTickCmd())
			}
		}
	}
	return m, m.resetMarquee()
}
