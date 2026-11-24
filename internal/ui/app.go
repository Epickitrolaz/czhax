package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/sysinfo"
	"github.com/czhax/czhax/internal/theme"
)

type FocusZone int

const (
	FocusTabs FocusZone = iota
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

	Status StatusView
	Help   HelpView

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
		ActiveView: ViewStatus,
		Focus:      FocusTabs,
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
		return m, m.setStatus("Refreshed")
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
		case k == "esc":
			if m.Cfg.ConfirmQuit {
				m.ConfirmExit = true
			}
		}
		return m, nil
	}

	// Inside tab content: arrows belong to content, Esc returns to tab selection
	_, mainW, bodyH := m.dimensions()
	pal := theme.Resolve(m.Cfg.Theme, false)
	totalRows := len(m.Status.BuildRows(mainW, pal, m.Cfg, m.Snap))

	switch {
	case k == "esc":
		m.Focus = FocusTabs
	case m.isUp(k):
		m.Status.Move(-1, totalRows, bodyH)
	case m.isDown(k):
		m.Status.Move(1, totalRows, bodyH)
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
	if m.ShowHelp {
		mainLines = m.Help.RenderLines(mainW, bodyH, pal)
	} else {
		mainLines = m.Status.RenderLines(mainW, bodyH, pal, m.Cfg, m.Snap, m.Focus == FocusContent)
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
	return []Hint{
		{Key: "↑↓", Desc: "scroll"},
		{Key: "r", Desc: "ref"},
		{Key: "Esc", Desc: "tabs"},
		{Key: "q", Desc: "quit"},
	}
}
