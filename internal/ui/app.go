package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/modules"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
)

type FocusZone int

const (
	FocusSidebar FocusZone = iota
	FocusContent
)

type tickMsg time.Time
type clearStatusMsg struct{}

type AppModel struct {
	Cfg        config.Config
	ConfigPath string
	Snap       sysinfo.Snapshot

	TermW int
	TermH int

	ActiveView ViewID
	ActiveTabs [viewCount]int
	Focus      FocusZone

	Dash       DashView
	ModPanel   ModulePanelView
	SysMon     SysMonView
	NetIf      NetIfView
	Notes      NotesView
	Files      FilesView
	Options    OptionsView
	Search     SearchView
	Help       HelpView

	ShowSearch  bool
	ShowHelp    bool
	ConfirmExit bool
	StatusMsg   string
}

func NewAppModel(cfg config.Config, cfgPath string) AppModel {
	return AppModel{
		Cfg:        cfg,
		ConfigPath: cfgPath,
		Snap:       sysinfo.Collect(),
		TermW:      53,
		TermH:      14,
		ActiveView: ViewDash,
		Focus:      FocusSidebar,
		Dash:       NewDashView(cfg.PinnedIDs),
		ModPanel:   NewModulePanelView(),
		Notes:      NewNotesView(),
		Files:      NewFilesView(),
		Search:     NewSearchView(),
	}
}

func (m AppModel) Init() tea.Cmd {
	return m.scheduleTick()
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
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
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
		return m, m.scheduleTick()

	case clearStatusMsg:
		m.StatusMsg = ""
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

	// Search overlay captures text input
	if m.ShowSearch {
		switch k {
		case "esc":
			m.ShowSearch = false
			return m, nil
		case "up", "ctrl+p":
			m.Search.Move(-1)
			return m, nil
		case "down", "ctrl+n":
			m.Search.Move(1)
			return m, nil
		case "enter":
			if it, ok := m.Search.Selected(); ok {
				m.ShowSearch = false
				m.jumpToModule(it)
			}
			return m, nil
		case "backspace":
			if len(m.Search.Query) > 0 {
				r := []rune(m.Search.Query)
				m.Search.SetQuery(string(r[:len(r)-1]))
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
				m.Search.SetQuery(m.Search.Query + string(msg.Runes))
			} else if k == " " {
				m.Search.SetQuery(m.Search.Query + " ")
			}
			return m, nil
		}
	}

	// Notes inline editor captures text input
	if m.ActiveView == ViewSys && m.ActiveTabs[ViewSys] == 1 && m.Focus == FocusContent && m.Notes.Adding {
		switch k {
		case "esc":
			m.Notes.Adding = false
			m.Notes.Draft = ""
			return m, nil
		case "enter":
			m.Notes.AddLine(m.Notes.Draft)
			m.Notes.Adding = false
			m.Notes.Draft = ""
			return m, m.setStatus("Note saved")
		case "backspace":
			if len(m.Notes.Draft) > 0 {
				r := []rune(m.Notes.Draft)
				m.Notes.Draft = string(r[:len(r)-1])
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
				m.Notes.Draft += string(msg.Runes)
			} else if k == " " {
				m.Notes.Draft += " "
			}
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

	// Global shortcuts
	switch k {
	case "?":
		m.ShowHelp = true
		m.Help.Scroll = 0
		return m, nil
	case "/":
		m.ShowSearch = true
		m.Search.SetQuery("")
		return m, nil
	case "f4":
		m.ActiveView = ViewDash
		return m, nil
	case "f5":
		m.ActiveView = ViewRecon
		return m, nil
	case "f6":
		m.ActiveView = ViewNet
		return m, nil
	case "f7":
		m.ActiveView = ViewSys
		return m, nil
	case "f8":
		m.ActiveView = ViewOpts
		return m, nil
	case "tab", "]":
		m.cycleTab(1)
		return m, nil
	case "shift+tab", "[":
		m.cycleTab(-1)
		return m, nil
	case "1":
		m.setTab(0)
		return m, nil
	case "2":
		m.setTab(1)
		return m, nil
	case "3":
		m.setTab(2)
		return m, nil
	case "r":
		m.Snap = sysinfo.Collect()
		if m.ActiveView == ViewSys && m.ActiveTabs[ViewSys] == 2 {
			m.Files.Reload()
		}
		return m, m.setStatus("Refreshed")
	case "t":
		m.Cfg.Theme = theme.NextID(m.Cfg.Theme)
		_ = config.Save(m.ConfigPath, m.Cfg)
		pal := theme.Resolve(m.Cfg.Theme, m.Cfg.StealthMode)
		return m, m.setStatus("Theme: " + pal.Name)
	case "s":
		m.Cfg.StealthMode = !m.Cfg.StealthMode
		_ = config.Save(m.ConfigPath, m.Cfg)
		state := "OFF"
		if m.Cfg.StealthMode {
			state = "ON"
		}
		return m, m.setStatus("Stealth: " + state)
	case "q":
		if m.Cfg.ConfirmQuit {
			m.ConfirmExit = true
			return m, nil
		}
		return m, tea.Quit
	}

	// Left Sidebar focus
	if m.Focus == FocusSidebar {
		switch {
		case m.isUp(k):
			m.ActiveView = ViewID((int(m.ActiveView) - 1 + int(viewCount)) % int(viewCount))
		case m.isDown(k):
			m.ActiveView = ViewID((int(m.ActiveView) + 1) % int(viewCount))
		case k == "enter" || m.isRight(k):
			m.Focus = FocusContent
		case k == "esc":
			if m.Cfg.ConfirmQuit {
				m.ConfirmExit = true
			}
		}
		return m, nil
	}

	// Right Content focus
	return m.handleContentKey(k)
}

func (m AppModel) handleContentKey(k string) (tea.Model, tea.Cmd) {
	tab := m.ActiveTabs[m.ActiveView]

	// Handle per-view content actions
	switch m.ActiveView {
	case ViewDash:
		switch {
		case k == "esc" || m.isLeft(k):
			m.Focus = FocusSidebar
		case m.isUp(k):
			m.Dash.Move(tab, -1)
		case m.isDown(k):
			m.Dash.Move(tab, 1)
		case tab == 1 && (k == "enter" || k == " "):
			if it, ok := m.Dash.SelectedPin(); ok {
				m.jumpToModule(it)
			}
		case tab == 1 && k == "p":
			if it, ok := m.Dash.SelectedPin(); ok {
				return m, m.togglePin(it)
			}
		}
		return m, nil

	case ViewRecon, ViewLab:
		it, _ := modules.FindByTarget(int(m.ActiveView), tab)
		switch {
		case k == "esc" || m.isLeft(k):
			m.Focus = FocusSidebar
		case m.isUp(k):
			m.ModPanel.Move(it, -1)
		case m.isDown(k):
			m.ModPanel.Move(it, 1)
		case k == "p":
			return m, m.togglePin(it)
		}
		return m, nil

	case ViewNet:
		if tab == 0 { // Live Net Interfaces
			switch {
			case k == "esc" || m.isLeft(k):
				m.Focus = FocusSidebar
			case m.isUp(k):
				m.NetIf.Move(-1, len(m.Snap.Interfaces))
			case m.isDown(k):
				m.NetIf.Move(1, len(m.Snap.Interfaces))
			case k == "p":
				if it, ok := modules.FindByTarget(int(ViewNet), 0); ok {
					return m, m.togglePin(it)
				}
			}
			return m, nil
		}
		it, _ := modules.FindByTarget(int(ViewNet), tab)
		switch {
		case k == "esc" || m.isLeft(k):
			m.Focus = FocusSidebar
		case m.isUp(k):
			m.ModPanel.Move(it, -1)
		case m.isDown(k):
			m.ModPanel.Move(it, 1)
		case k == "p":
			return m, m.togglePin(it)
		}
		return m, nil

	case ViewSys:
		switch tab {
		case 0: // SysMon
			switch {
			case k == "esc" || m.isLeft(k):
				m.Focus = FocusSidebar
			case m.isUp(k):
				m.SysMon.Move(-1)
			case m.isDown(k):
				m.SysMon.Move(1)
			case k == "p":
				if it, ok := modules.FindByID("sys-mon"); ok {
					return m, m.togglePin(it)
				}
			}
		case 1: // Notes
			switch {
			case k == "esc" || m.isLeft(k):
				m.Focus = FocusSidebar
			case m.isUp(k):
				m.Notes.Move(-1)
			case m.isDown(k):
				m.Notes.Move(1)
			case k == "a":
				m.Notes.Adding = true
				m.Notes.Draft = ""
			case k == "d":
				m.Notes.DeleteCurrent()
				return m, m.setStatus("Note deleted")
			}
		case 2: // Files
			switch {
			case k == "esc" || k == "backspace":
				if !m.Files.Back() {
					m.Focus = FocusSidebar
				}
			case m.isLeft(k):
				if !m.Files.Back() {
					m.Focus = FocusSidebar
				}
			case m.isUp(k):
				m.Files.Move(-1)
			case m.isDown(k):
				m.Files.Move(1)
			case k == "enter" || m.isRight(k):
				m.Files.Open()
			}
		}
		return m, nil

	case ViewOpts:
		switch {
		case k == "esc" || m.isLeft(k):
			m.Focus = FocusSidebar
		case m.isUp(k):
			m.Options.Move(tab, -1)
		case m.isDown(k):
			m.Options.Move(tab, 1)
		case k == "enter" || k == " " || k == "+" || k == "=" || m.isRight(k):
			return m, m.applyOption(tab, 1)
		case k == "-":
			return m, m.applyOption(tab, -1)
		}
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

func (m *AppModel) setTab(idx int) {
	tabs := TopViews[m.ActiveView].Tabs
	if idx >= 0 && idx < len(tabs) {
		m.ActiveTabs[m.ActiveView] = idx
	}
}

func (m *AppModel) jumpToModule(it modules.Item) {
	m.ActiveView = ViewID(it.Target.View)
	m.setTab(it.Target.Tab)
	m.Focus = FocusContent
	if it.ID == "sys-mon" || it.ID == "net-if" {
		m.Snap = sysinfo.Collect()
	}
	if it.ID == "files" {
		m.Files.Reload()
	}
}

func (m *AppModel) applyOption(tab, delta int) tea.Cmd {
	prevRefresh := m.Cfg.RefreshSeconds
	action := m.Options.Adjust(tab, delta, &m.Cfg)
	if action == "" {
		return nil
	}
	if err := config.Save(m.ConfigPath, m.Cfg); err != nil {
		return m.setStatus("Save err: " + err.Error())
	}
	m.Dash.RefreshPins(m.Cfg.PinnedIDs)

	var cmds []tea.Cmd
	if action == "reset" {
		cmds = append(cmds, m.setStatus("Options reset"))
	} else {
		cmds = append(cmds, m.setStatus("Saved"))
	}
	if prevRefresh != m.Cfg.RefreshSeconds {
		cmds = append(cmds, m.scheduleTick())
	}
	return tea.Batch(cmds...)
}

func (m *AppModel) togglePin(it modules.Item) tea.Cmd {
	idx := -1
	for i, id := range m.Cfg.PinnedIDs {
		if id == it.ID {
			idx = i
			break
		}
	}
	var msg string
	if idx >= 0 {
		m.Cfg.PinnedIDs = append(m.Cfg.PinnedIDs[:idx], m.Cfg.PinnedIDs[idx+1:]...)
		msg = "Unpinned: " + it.ShortName
	} else {
		m.Cfg.PinnedIDs = append(m.Cfg.PinnedIDs, it.ID)
		msg = "Pinned: " + it.ShortName
	}
	_ = config.Save(m.ConfigPath, m.Cfg)
	m.Dash.RefreshPins(m.Cfg.PinnedIDs)
	return m.setStatus(msg)
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

func (m AppModel) View() string {
	w, h, bezel := ResolveViewport(m.TermW, m.TermH, m.Cfg.Viewport)
	pal := theme.Resolve(m.Cfg.Theme, m.Cfg.StealthMode)

	sideW := ResolveSidebarWidth(w, m.Cfg.Sidebar)
	mainW := max(16, w-sideW-1)
	if sideW+1+mainW > w {
		sideW = max(3, w-mainW-1)
	}

	overlayTitle := ""
	if m.ShowSearch {
		overlayTitle = "FIND"
	} else if m.ShowHelp {
		overlayTitle = "HELP"
	}

	topRow := RenderTopRow(sideW, mainW, pal, m.Cfg, m.ActiveView, m.ActiveTabs[m.ActiveView], overlayTitle, m.Snap)

	showDivider := h >= 12
	showFooter := m.Cfg.ShowKeyHints || m.ConfirmExit || m.StatusMsg != ""

	bodyH := h - 1
	if showDivider {
		bodyH--
	}
	if showFooter {
		bodyH--
	}
	if bodyH < 1 {
		bodyH = 1
	}

	sideLines := RenderSidebarColumn(sideW, bodyH, pal, m.Cfg, m.ActiveView, m.Focus == FocusSidebar && !m.ShowSearch && !m.ShowHelp)
	mainLines := m.renderMainLines(mainW, bodyH, pal)
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
		Render("↑↓ view/item · ←→ sidebar/pane · Tab tabs · / find · ? help")

	composed := lipgloss.JoinVertical(lipgloss.Center, label, frame, fnLegend)
	return lipgloss.Place(m.TermW, m.TermH, lipgloss.Center, lipgloss.Center, composed)
}

func (m AppModel) renderMainLines(w, h int, pal theme.Palette) []string {
	if m.ShowSearch {
		return m.Search.RenderLines(w, h, pal)
	}
	if m.ShowHelp {
		return m.Help.RenderLines(w, h, pal)
	}

	tab := m.ActiveTabs[m.ActiveView]
	focused := m.Focus == FocusContent

	switch m.ActiveView {
	case ViewDash:
		return m.Dash.RenderLines(tab, w, h, pal, m.Cfg, m.Snap, focused)
	case ViewRecon, ViewLab:
		if it, ok := modules.FindByTarget(int(m.ActiveView), tab); ok {
			return m.ModPanel.RenderLines(it, w, h, pal, m.Cfg, focused)
		}
	case ViewNet:
		if tab == 0 {
			return m.NetIf.RenderLines(w, h, pal, m.Cfg, m.Snap, focused)
		}
		if it, ok := modules.FindByTarget(int(ViewNet), tab); ok {
			return m.ModPanel.RenderLines(it, w, h, pal, m.Cfg, focused)
		}
	case ViewSys:
		switch tab {
		case 0:
			return m.SysMon.RenderLines(w, h, pal, m.Cfg, m.Snap)
		case 1:
			return m.Notes.RenderLines(w, h, pal, focused)
		case 2:
			return m.Files.RenderLines(w, h, pal, focused)
		}
	case ViewOpts:
		return m.Options.RenderLines(tab, w, h, pal, m.Cfg, focused)
	}
	return SplitLinesPadded(nil, w, h)
}

func (m AppModel) currentHints() []Hint {
	if m.ShowSearch {
		return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "jump"}, {Key: "Esc", Desc: "close"}}
	}
	if m.ShowHelp {
		return []Hint{{Key: "↑↓", Desc: "scroll"}, {Key: "Esc", Desc: "close"}}
	}
	if m.Focus == FocusSidebar {
		return []Hint{
			{Key: "↑↓", Desc: "view"},
			{Key: "→", Desc: "open"},
			{Key: "Tab", Desc: "tab"},
			{Key: "/", Desc: "find"},
			{Key: "?", Desc: "help"},
		}
	}
	if m.ActiveView == ViewOpts {
		return []Hint{
			{Key: "↑↓", Desc: "opt"},
			{Key: "⏎", Desc: "set"},
			{Key: "←", Desc: "side"},
			{Key: "Tab", Desc: "tab"},
		}
	}
	if m.ActiveView == ViewSys && m.ActiveTabs[ViewSys] == 1 && m.Notes.Adding {
		return []Hint{{Key: "⏎", Desc: "save"}, {Key: "Esc", Desc: "cancel"}}
	}
	return []Hint{
		{Key: "↑↓", Desc: "nav"},
		{Key: "←", Desc: "side"},
		{Key: "Tab", Desc: "tab"},
		{Key: "/", Desc: "find"},
		{Key: "?", Desc: "help"},
	}
}
