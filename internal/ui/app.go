package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/boot"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/ir"
	"github.com/czhax/czhax/internal/power"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/updater"
	"github.com/czhax/czhax/internal/wifi"
)

type FocusZone int

const (
	FocusTabs FocusZone = iota
	FocusContent
)

type tickMsg time.Time
type clearStatusMsg struct{}
type startupRedrawMsg struct{ step int }

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

type wifiPentestTickMsg struct{}

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

type bluetoothRefreshedMsg struct {
	adapters []bluetooth.Adapter
}

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

	Status    StatusView
	System    SystemView
	Wifi      WifiView
	WifiScan  WifiScanView
	IR        IRView
	Bluetooth BluetoothView
	Help      HelpView

	ShowHelp      bool
	ConfirmExit   bool
	RestartOnQuit bool
	StatusMsg     string
}

func NewAppModel(cfg config.Config, cfgPath, version string) AppModel {
	if version == "" {
		version = "dev"
	}
	m := AppModel{
		Cfg:        cfg,
		ConfigPath: cfgPath,
		Version:    version,
		Snap:       sysinfo.Collect(),
		TermW:      53,
		TermH:      14,
		ActiveView: ViewStatus,
		Focus:      FocusTabs,
		System:     NewSystemView(),
		Wifi:       NewWifiView(),
		WifiScan:   NewWifiScanView(),
		IR:         NewIRView(cfgPath),
		Bluetooth:  NewBluetoothView(),
	}
	if cfg.IRFlipperDir != "" {
		m.IR.Roots.Flipper = cfg.IRFlipperDir
	}
	if cfg.IRMiDir != "" {
		m.IR.Roots.Mi = cfg.IRMiDir
	}
	return m
}

func (m AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.scheduleTick(), collectWifiCmd(), collectBluetoothCmd()}
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

func runWifiPentestCmd(ctx context.Context, req wifi.PentestRequest) tea.Cmd {
	return func() tea.Msg {
		res, err := wifi.RunPentest(ctx, req)
		return wifiPentestDoneMsg{res: res, err: err}
	}
}

func wifiPentestTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return wifiPentestTickMsg{}
	})
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

func collectBluetoothCmd() tea.Cmd {
	return func() tea.Msg {
		return bluetoothRefreshedMsg{adapters: bluetooth.Collect()}
	}
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
		return m, nil

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
		return m, tea.Batch(cmds...)

	case wifiRefreshedMsg:
		m.Wifi.SetInterfaces(msg.ifaces)
		m.WifiScan.SyncInterfaces(msg.ifaces)
		return m, nil

	case wifiNetworksScannedMsg:
		m.WifiScan.SetScanResult(msg.iface, msg.nets, msg.err)
		if msg.err != nil {
			return m, m.setStatus("Scan failed")
		}
		return m, m.setStatus(fmt.Sprintf("%d networks on %s", len(msg.nets), msg.iface))

	case wifiConnectDoneMsg:
		m.WifiScan.SetConnectResult(msg.req, msg.err)
		if msg.err != nil {
			return m, m.setStatus("Connect failed")
		}
		m.Snap = sysinfo.Collect()
		scanIface := m.WifiScan.EffectiveScanIface()
		return m, tea.Batch(m.setStatus("Connected "+msg.req.Iface), collectWifiCmd(), scanWifiNetworksCmd(scanIface))

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
		return m, tea.Batch(m.setStatus(status), collectWifiCmd())

	case wifiPentestTickMsg:
		if !m.WifiScan.PentestActive {
			return m, nil
		}
		if !m.WifiScan.PentestStarted.IsZero() {
			m.WifiScan.PentestElapsed = time.Since(m.WifiScan.PentestStarted)
		}
		return m, wifiPentestTickCmd()

	case irCaptureDoneMsg:
		m.IR.ApplyCaptureResult(msg.seq, msg.sig, msg.err)
		if msg.err != nil {
			return m, m.setStatus("IR capture failed")
		}
		return m, m.setStatus("IR signal captured")

	case irSendDoneMsg:
		if msg.err != nil {
			m.IR.Note = "Send failed: " + msg.err.Error()
			m.IR.NoteIsErr = true
			return m, m.setStatus("IR send failed")
		}
		m.IR.Note = "Sent " + msg.name
		m.IR.NoteIsErr = false
		return m, m.setStatus("IR sent")

	case irIndexDoneMsg:
		m.IR.SetIndex(msg.entries, msg.err)
		if msg.err != nil {
			return m, m.setStatus("IR index failed")
		}
		return m, m.setStatus(fmt.Sprintf("%d IR remotes", len(msg.entries)))

	case irRemoteLoadedMsg:
		if msg.err != nil {
			m.IR.RemoteLoading = false
			m.IR.RemoteErr = msg.err.Error()
			return m, m.setStatus("IR load failed")
		}
		m.IR.SetRemote(msg.remote)
		return m, m.setStatus(msg.remote.ProfileName())

	case irLearnTickMsg:
		if !m.IR.Capturing {
			return m, nil
		}
		m.IR.CaptureElapsed = time.Since(m.IR.CaptureStarted)
		return m, irLearnTickCmd()

	case bluetoothRefreshedMsg:
		m.Bluetooth.SetAdapters(msg.adapters)
		return m, nil

	case clearStatusMsg:
		m.StatusMsg = ""
		return m, nil

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
			return m, m.setStatus("Update failed")
		}
		if !msg.res.Updated {
			m.System.Note = "Already up to date (" + msg.res.CurrentVersion + ")"
			return m, m.setStatus("Up to date")
		}
		m.Version = msg.res.LatestVersion
		m.System.Note = "Done! Select Restart CZHax"
		return m, m.setStatus("Updated: " + msg.res.LatestVersion)

	case sudoUpdateDoneMsg:
		m.System.Updating = false
		if msg.err != nil {
			m.System.Note = "Update canceled or failed"
			return m, tea.Batch(tea.ClearScreen, m.setStatus("Update failed"))
		}
		m.System.Note = "Done! Select Restart CZHax"
		return m, tea.Batch(tea.ClearScreen, m.setStatus("Update finished"))

	case shellExitedMsg:
		m.Snap = sysinfo.Collect()
		m.System.RefreshBootState()
		return m, tea.Batch(tea.ClearScreen, m.setStatus("Returned from shell"), collectWifiCmd(), collectBluetoothCmd())

	case bootToggledMsg:
		m.System.BootState = msg.state
		if msg.err != nil {
			m.System.Note = msg.err.Error()
			return m, tea.Batch(tea.ClearScreen, m.setStatus("Boot toggle failed"))
		}
		if msg.state.Enabled {
			m.System.Note = "Start on boot: ON (" + string(msg.state.Method) + ")"
			return m, tea.Batch(tea.ClearScreen, m.setStatus("Boot: ON"))
		}
		m.System.Note = "Start on boot: OFF"
		return m, tea.Batch(tea.ClearScreen, m.setStatus("Boot: OFF"))

	case sleepDoneMsg:
		m.Snap = sysinfo.Collect()
		if msg.err != nil {
			m.System.Note = msg.err.Error()
			return m, tea.Batch(tea.ClearScreen, m.setStatus("Sleep error"), collectWifiCmd(), collectBluetoothCmd())
		}
		m.System.Note = "Woke from sleep"
		return m, tea.Batch(tea.ClearScreen, m.setStatus("Awake"), collectWifiCmd(), collectBluetoothCmd())

	case powerActionDoneMsg:
		if msg.err != nil {
			m.System.Note = msg.err.Error()
			return m, m.setStatus(msg.action + " failed")
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
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

	// Never leave a monitor interface behind: stop a run before quitting.
	if m.WifiScan.PentestActive && (k == "ctrl+c" || k == "q") {
		m.WifiScan.StopPentest()
		return m, m.setStatus("Stopping pentest...")
	}
	if m.IR.Capturing && (k == "ctrl+c" || k == "q") {
		m.IR.StopCapture()
		return m, m.setStatus("IR capture stopped")
	}

	if m.Focus == FocusContent && m.ActiveView == ViewWiFi && m.WifiScan.IsTextInput() {
		if k == "esc" {
			m.WifiScan.HandleEsc()
			return m, nil
		}
		if submit, handled := m.WifiScan.HandleTextKey(msg); handled {
			if submit {
				req := m.WifiScan.BuildConnectRequest()
				return m, tea.Batch(m.setStatus("Connecting "+req.Iface+"..."), connectWifiNetworkCmd(req))
			}
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
		if m.ActiveView == ViewIR && m.Focus == FocusContent {
			if m.IR.Capturing {
				return m, m.setStatus("IR capture running")
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
		m.Snap = sysinfo.Collect()
		m.System.RefreshBootState()
		if len(m.Wifi.Interfaces) == 0 {
			m.Wifi.Scanning = true
		}
		if len(m.Bluetooth.Adapters) == 0 {
			m.Bluetooth.Scanning = true
		}
		cmds := []tea.Cmd{m.setStatus("Refreshed"), collectWifiCmd(), collectBluetoothCmd()}
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
					return m, tea.Batch(collectWifiCmd(), scanWifiNetworksCmd(iface), m.setStatus("Scanning "+iface+"..."))
				}
				return m, collectWifiCmd()
			} else if m.ActiveView == ViewIR {
				m.IR.Tab = IRTab(m.ActiveTabs[ViewIR])
				m.IR.RefreshHardware()
				if m.IR.Tab == IRTabBrowse && !m.IR.IndexDone && !m.IR.IndexLoading {
					m.IR.RemoteErr = ""
					m.IR.IndexLoading = true
					return m, tea.Batch(m.setStatus("Indexing IR databases..."), irIndexCmd(m.IR.Roots))
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
		runWifiPentestCmd(ctx, req),
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
		if m.ActiveView == ViewWiFi && m.WifiScan.HandleEsc() {
			return m, nil
		}
		if m.ActiveView == ViewIR && m.IR.HandleEsc() {
			return m, nil
		}
		m.Focus = FocusTabs
		return m, nil
	}

	switch m.ActiveView {
	case ViewWiFi:
		_, mainW, bodyH := m.dimensions()
		pal := theme.Resolve(m.Cfg.Theme, false)
		switch m.WifiScan.Mode {
		case scanModeList:
			switch {
			case k == "w":
				return m.startWifiPentest(wifi.PentestHandshake)
			case k == "d":
				return m.startWifiPentest(wifi.PentestDeauth)
			case k == "b":
				return m.startWifiPentest(wifi.PentestBoth)
			case m.isUp(k):
				m.WifiScan.MoveNetwork(-1)
			case m.isDown(k):
				m.WifiScan.MoveNetwork(1)
			case m.isLeft(k):
				m.WifiScan.CycleScanIface(-1)
			case m.isRight(k) || k == "i":
				m.WifiScan.CycleScanIface(1)
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
					m.WifiScan.OpenDetail()
				}
			case k == "c":
				m.WifiScan.BeginConnect(scanModeList)
			}
			return m, nil

		case scanModeDetail:
			switch {
			case k == "w":
				return m.startWifiPentest(wifi.PentestHandshake)
			case k == "d":
				return m.startWifiPentest(wifi.PentestDeauth)
			case k == "b":
				return m.startWifiPentest(wifi.PentestBoth)
			case m.isUp(k):
				m.WifiScan.MoveDetailScroll(-1, mainW, bodyH, pal)
			case m.isDown(k):
				m.WifiScan.MoveDetailScroll(1, mainW, bodyH, pal)
			case k == "enter" || k == "c":
				m.WifiScan.BeginConnect(scanModeDetail)
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

	case ViewIR:
		_, mainW, bodyH := m.dimensions()
		pal := theme.Resolve(m.Cfg.Theme, false)
		_ = mainW
		_ = bodyH
		_ = pal
		m.IR.Tab = IRTab(m.ActiveTabs[ViewIR])
		switch m.IR.Tab {
		case IRTabRemote:
			switch {
			case m.isUp(k):
				if m.IR.RemoteScroll > 0 {
					m.IR.RemoteScroll--
				}
			case m.isDown(k):
				m.IR.RemoteScroll++
			case k == "[":
				m.IR.CycleProfile(-1)
			case k == "]":
				m.IR.CycleProfile(1)
			default:
				if b, ok := m.IR.BindingForKey(k); ok {
					return m.sendIR(b.Signal, b.Name)
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
			case k == "l":
				return m.startIRCapture()
			case k == "t" && m.IR.Learned != nil:
				return m.sendIR(*m.IR.Learned, "Learned")
			default:
				if m.IR.AwaitBinding && ir.IsBindableKey(k) {
					if m.IR.BindLearned(k) {
						if m.IR.NoteIsErr {
							return m, m.setStatus("IR save failed")
						}
						return m, m.setStatus("Bound " + ir.KeyLabel(k))
					}
				}
			}
			return m, nil

		case IRTabBrowse:
			if m.IR.Remote == nil {
				switch {
				case m.isUp(k):
					m.IR.MoveBrowse(-1)
				case m.isDown(k):
					m.IR.MoveBrowse(1)
				case m.isLeft(k):
					m.IR.CycleCategory(-1)
				case m.isRight(k):
					m.IR.CycleCategory(1)
				case k == "p":
					m.IR.CycleProvider(1)
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
	w, h, bezel := ResolveViewport(m.TermW, m.TermH, m.Cfg.Viewport)
	pal := theme.Resolve(m.Cfg.Theme, false)

	sideW, mainW, bodyH := m.dimensions()
	showDivider := h >= 12
	showFooter := m.Cfg.ShowKeyHints || m.ConfirmExit || m.StatusMsg != ""

	overlayTitle := ""
	if m.ShowHelp {
		overlayTitle = "HELP"
	}

	tabsFocused := m.Focus == FocusTabs && !m.ShowHelp
	topRow := RenderTopRow(sideW, mainW, pal, m.Cfg, m.ActiveView, m.ActiveTabs[m.ActiveView], tabsFocused, overlayTitle, m.Snap)
	sideLines := RenderSidebarColumn(sideW, bodyH, pal, m.Cfg, m.ActiveView, tabsFocused)

	var mainLines []string
	switch {
	case m.ShowHelp:
		mainLines = m.Help.RenderLines(mainW, bodyH, pal)
	case m.ActiveView == ViewWiFi:
		mainLines = m.WifiScan.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
	case m.ActiveView == ViewIR:
		irView := m.IR
		irView.Tab = IRTab(m.ActiveTabs[ViewIR])
		mainLines = irView.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
	case m.ActiveView == ViewSystem:
		mainLines = m.System.RenderLines(mainW, bodyH, pal, m.Version, m.Focus == FocusContent)
	default:
		switch m.ActiveTabs[ViewStatus] {
		case StatusTabWiFi:
			mainLines = m.Wifi.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
		case StatusTabBT:
			mainLines = m.Bluetooth.RenderLines(mainW, bodyH, pal, m.Focus == FocusContent)
		default:
			mainLines = m.Status.RenderLines(mainW, bodyH, pal, m.Cfg, m.Snap, m.Focus == FocusContent)
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
		parts = append(parts, RenderFooter(w, pal, m.Cfg, m.currentHints(), status))
	}

	screen := strings.Join(parts, "\n")

	if !bezel {
		return screen
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
	return lipgloss.Place(m.TermW, m.TermH, lipgloss.Center, lipgloss.Center, composed)
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
		switch m.WifiScan.Mode {
		case scanModeDetail:
			return []Hint{
				{Key: "⏎", Desc: "conn"},
				{Key: "b", Desc: "both"},
				{Key: "w", Desc: "hs"},
				{Key: "d", Desc: "deauth"},
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
				return []Hint{{Key: "Esc", Desc: "stop"}, {Key: "q", Desc: "stop"}}
			}
			return []Hint{{Key: "Esc", Desc: "back"}, {Key: "q", Desc: "quit"}}
		default:
			return []Hint{
				{Key: "⏎", Desc: "info"},
				{Key: "s", Desc: "scan"},
				{Key: "c", Desc: "conn"},
				{Key: "b", Desc: "both"},
				{Key: "w", Desc: "hs"},
				{Key: "d", Desc: "deauth"},
				{Key: "Esc", Desc: "tabs"},
			}
		}
	}
	if m.ActiveView == ViewIR {
		switch m.IR.Tab {
		case IRTabLearn:
			if m.IR.Capturing {
				return []Hint{{Key: "Esc", Desc: "cancel"}, {Key: "q", Desc: "stop"}}
			}
			if m.IR.AwaitBinding {
				return []Hint{{Key: "1-0", Desc: "bind"}, {Key: "t", Desc: "test"}, {Key: "Esc", Desc: "cancel"}}
			}
			return []Hint{{Key: "l", Desc: "listen"}, {Key: "Esc", Desc: "tabs"}}
		case IRTabBrowse:
			if m.IR.Remote != nil {
				if m.IR.GridMode {
					return []Hint{{Key: "key", Desc: "send/next"}, {Key: "a", Desc: "accept"}, {Key: "g", Desc: "probe"}, {Key: "Esc", Desc: "back"}}
				}
				return []Hint{{Key: "p/v/m", Desc: "try"}, {Key: "a", Desc: "accept"}, {Key: "g", Desc: "grid"}, {Key: "Esc", Desc: "back"}}
			}
			return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "←→", Desc: "type"}, {Key: "p", Desc: "src"}, {Key: "⏎", Desc: "open"}, {Key: "r", Desc: "index"}}
		default:
			return []Hint{{Key: "key", Desc: "send"}, {Key: "[ ]", Desc: "profile"}, {Key: "Esc", Desc: "tabs"}}
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
