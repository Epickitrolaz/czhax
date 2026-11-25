package ui

import (
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
	return AppModel{
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
		Bluetooth:  NewBluetoothView(),
	}
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
		return m, nil

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

	switch k {
	case "?":
		m.ShowHelp = true
		m.Help.Scroll = 0
		return m, nil
	case "r":
		m.Snap = sysinfo.Collect()
		m.System.RefreshBootState()
		if len(m.Wifi.Interfaces) == 0 {
			m.Wifi.Scanning = true
		}
		if len(m.Bluetooth.Adapters) == 0 {
			m.Bluetooth.Scanning = true
		}
		return m, tea.Batch(m.setStatus("Refreshed"), collectWifiCmd(), collectBluetoothCmd())
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

func (m AppModel) handleContentKey(k string) (tea.Model, tea.Cmd) {
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
		m.Focus = FocusTabs
		return m, nil
	}

	switch m.ActiveView {
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
