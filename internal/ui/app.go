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

type SubScreen int

const (
	SubNone SubScreen = iota
	SubToolDetail
	SubSysMon
	SubNetIf
	SubNotes
	SubFiles
)

type tickMsg time.Time
type clearStatusMsg struct{}

type AppModel struct {
	Cfg        config.Config
	ConfigPath string
	Snap       sysinfo.Snapshot

	TermW int
	TermH int

	ActiveTab Tab
	Sub       SubScreen

	Home       HomeView
	SecList    ModuleListView
	BasicList  ModuleListView
	Options    OptionsView
	ToolDetail ToolDetailView
	SysMon     SysMonView
	NetIf      NetIfView
	Notes      NotesView
	Files      FilesView
	Search     SearchView
	Help       HelpView

	ShowSearch  bool
	ShowHelp    bool
	ConfirmExit bool
	StatusMsg   string
}

func NewAppModel(cfg config.Config, cfgPath string) AppModel {
	snap := sysinfo.Collect()
	return AppModel{
		Cfg:        cfg,
		ConfigPath: cfgPath,
		Snap:       snap,
		TermW:      53,
		TermH:      14,
		ActiveTab:  TabHome,
		Home:       NewHomeView(cfg.PinnedIDs),
		SecList:    NewModuleListView("SECTOOLS", modules.ByCategory(modules.CatSec)),
		BasicList:  NewModuleListView("BASIC TOOLS", modules.ByCategory(modules.CatBasic)),
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

	// Confirm exit prompt takes priority
	if m.ConfirmExit {
		switch k {
		case "y", "Y", "enter", "q":
			return m, tea.Quit
		case "n", "N", "esc":
			m.ConfirmExit = false
			m.StatusMsg = ""
			return m, nil
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
				m.openModule(it)
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

	// Notes text input mode
	if m.Sub == SubNotes && m.Notes.Adding {
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

	// Help overlay
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

	// Global overlays & shortcuts
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
		m.switchTab(TabHome)
		return m, nil
	case "f5":
		m.switchTab(TabSec)
		return m, nil
	case "f6":
		m.switchTab(TabBasic)
		return m, nil
	case "f7":
		m.switchTab(TabOptions)
		return m, nil
	case "f8":
		m.ShowHelp = true
		m.Help.Scroll = 0
		return m, nil
	case "1":
		m.switchTab(TabHome)
		return m, nil
	case "2":
		m.switchTab(TabSec)
		return m, nil
	case "3":
		m.switchTab(TabBasic)
		return m, nil
	case "4":
		m.switchTab(TabOptions)
		return m, nil
	case "tab", "]":
		m.switchTab(Tab((int(m.ActiveTab) + 1) % 4))
		return m, nil
	case "shift+tab", "[":
		m.switchTab(Tab((int(m.ActiveTab) + 3) % 4))
		return m, nil
	case "r":
		m.Snap = sysinfo.Collect()
		if m.Sub == SubFiles {
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

	// Sub-screen routing
	if m.Sub != SubNone {
		return m.handleSubScreenKey(k)
	}

	// Main tab routing
	switch m.ActiveTab {
	case TabHome:
		switch {
		case m.isUp(k):
			m.Home.Move(-1)
		case m.isDown(k):
			m.Home.Move(1)
		case k == "enter" || k == " " || m.isRight(k):
			if it, ok := m.Home.Selected(); ok {
				m.openModule(it)
			}
		case k == "p":
			if it, ok := m.Home.Selected(); ok {
				return m, m.togglePin(it)
			}
		case k == "esc":
			if m.Cfg.ConfirmQuit {
				m.ConfirmExit = true
			}
		}
		return m, nil

	case TabSec:
		switch {
		case m.isUp(k):
			m.SecList.Move(-1)
		case m.isDown(k):
			m.SecList.Move(1)
		case k == "enter" || k == " " || m.isRight(k):
			if it, ok := m.SecList.Selected(); ok {
				m.openModule(it)
			}
		case k == "p":
			if it, ok := m.SecList.Selected(); ok {
				return m, m.togglePin(it)
			}
		case k == "esc":
			m.switchTab(TabHome)
		}
		return m, nil

	case TabBasic:
		switch {
		case m.isUp(k):
			m.BasicList.Move(-1)
		case m.isDown(k):
			m.BasicList.Move(1)
		case k == "enter" || k == " " || m.isRight(k):
			if it, ok := m.BasicList.Selected(); ok {
				m.openModule(it)
			}
		case k == "p":
			if it, ok := m.BasicList.Selected(); ok {
				return m, m.togglePin(it)
			}
		case k == "esc":
			m.switchTab(TabHome)
		}
		return m, nil

	case TabOptions:
		switch {
		case m.isUp(k):
			m.Options.Move(-1)
		case m.isDown(k):
			m.Options.Move(1)
		case m.isRight(k):
			return m, m.applyOption(1)
		case m.isLeft(k):
			return m, m.applyOption(-1)
		case k == "enter" || k == " ":
			return m, m.applyOption(0)
		case k == "esc":
			m.switchTab(TabHome)
		}
		return m, nil
	}

	return m, nil
}

func (m *AppModel) applyOption(delta int) tea.Cmd {
	prevRefresh := m.Cfg.RefreshSeconds
	action := m.Options.Adjust(delta, &m.Cfg)
	if action == "" {
		return nil
	}
	if err := config.Save(m.ConfigPath, m.Cfg); err != nil {
		return m.setStatus("Save err: " + err.Error())
	}
	m.Options.Dirty = false
	m.Home.RefreshPins(m.Cfg.PinnedIDs)

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

func (m AppModel) handleSubScreenKey(k string) (tea.Model, tea.Cmd) {
	switch m.Sub {
	case SubToolDetail:
		switch {
		case k == "esc" || k == "backspace" || m.isLeft(k):
			m.Sub = SubNone
		case m.isUp(k):
			m.ToolDetail.Move(-1)
		case m.isDown(k):
			m.ToolDetail.Move(1)
		case k == "p":
			return m, m.togglePin(m.ToolDetail.Item)
		}
		return m, nil

	case SubSysMon:
		switch {
		case k == "esc" || k == "backspace" || m.isLeft(k):
			m.Sub = SubNone
		case m.isUp(k):
			m.SysMon.Move(-1)
		case m.isDown(k):
			m.SysMon.Move(1)
		}
		return m, nil

	case SubNetIf:
		switch {
		case k == "esc" || k == "backspace" || m.isLeft(k):
			m.Sub = SubNone
		case m.isUp(k):
			m.NetIf.Move(-1, len(m.Snap.Interfaces))
		case m.isDown(k):
			m.NetIf.Move(1, len(m.Snap.Interfaces))
		}
		return m, nil

	case SubNotes:
		switch {
		case k == "esc" || k == "backspace" || m.isLeft(k):
			m.Sub = SubNone
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
		return m, nil

	case SubFiles:
		switch {
		case k == "esc" || k == "backspace" || m.isLeft(k):
			if !m.Files.Back() {
				m.Sub = SubNone
			}
		case m.isUp(k):
			m.Files.Move(-1)
		case m.isDown(k):
			m.Files.Move(1)
		case k == "enter" || m.isRight(k):
			m.Files.Open()
		}
		return m, nil
	}
	return m, nil
}

func (m *AppModel) switchTab(t Tab) {
	m.ActiveTab = t
	m.Sub = SubNone
}

func (m *AppModel) openModule(it modules.Item) {
	switch it.ID {
	case "sys-mon":
		m.ActiveTab = TabBasic
		m.Sub = SubSysMon
		m.SysMon.Scroll = 0
		m.Snap = sysinfo.Collect()
	case "net-if":
		m.ActiveTab = TabBasic
		m.Sub = SubNetIf
		m.NetIf.Cursor = 0
		m.Snap = sysinfo.Collect()
	case "notes":
		m.ActiveTab = TabBasic
		m.Sub = SubNotes
	case "files":
		m.ActiveTab = TabBasic
		m.Sub = SubFiles
		m.Files.Reload()
	default:
		if it.Category == modules.CatSec {
			m.ActiveTab = TabSec
		} else {
			m.ActiveTab = TabBasic
		}
		m.Sub = SubToolDetail
		m.ToolDetail = NewToolDetailView(it)
	}
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
	m.Home.RefreshPins(m.Cfg.PinnedIDs)
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

	breadcrumb := m.currentBreadcrumb()
	header := RenderHeader(w, pal, m.Cfg, m.ActiveTab, breadcrumb, m.Snap)

	showFooter := m.Cfg.ShowKeyHints || m.ConfirmExit || m.StatusMsg != ""
	bodyH := h - 1
	if showFooter {
		bodyH--
	}
	if bodyH < 1 {
		bodyH = 1
	}

	var body string
	switch {
	case m.ShowSearch:
		body = m.Search.Render(w, bodyH, pal)
	case m.ShowHelp:
		body = m.Help.Render(w, bodyH, pal)
	case m.Sub == SubToolDetail:
		body = m.ToolDetail.Render(w, bodyH, pal, m.Cfg)
	case m.Sub == SubSysMon:
		body = m.SysMon.Render(w, bodyH, pal, m.Cfg, m.Snap)
	case m.Sub == SubNetIf:
		body = m.NetIf.Render(w, bodyH, pal, m.Cfg, m.Snap)
	case m.Sub == SubNotes:
		body = m.Notes.Render(w, bodyH, pal)
	case m.Sub == SubFiles:
		body = m.Files.Render(w, bodyH, pal)
	default:
		switch m.ActiveTab {
		case TabHome:
			body = m.Home.Render(w, bodyH, pal, m.Cfg, m.Snap)
		case TabSec:
			body = m.SecList.Render(w, bodyH, pal, m.Cfg)
		case TabBasic:
			body = m.BasicList.Render(w, bodyH, pal, m.Cfg)
		case TabOptions:
			body = m.Options.Render(w, bodyH, pal, m.Cfg)
		}
	}

	parts := []string{header, body}
	if showFooter {
		status := m.StatusMsg
		if m.ConfirmExit {
			status = "Quit CZHax? [y/Enter] Yes  [n/Esc] No"
		}
		footer := RenderFooter(w, pal, m.Cfg, m.currentHints(), status)
		parts = append(parts, footer)
	}

	screen := strings.Join(parts, "\n")

	if !bezel {
		return screen
	}

	// Render centered Cardputer Zero hardware simulator bezel when desktop terminal is large
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
		Render("Keys: ↑↓/Fn+FX nav · F4-F8 softkeys · / find · ? help")

	composed := lipgloss.JoinVertical(lipgloss.Center, label, frame, fnLegend)
	return lipgloss.Place(m.TermW, m.TermH, lipgloss.Center, lipgloss.Center, composed)
}

func (m AppModel) currentBreadcrumb() string {
	if m.ShowSearch {
		return "Find"
	}
	if m.ShowHelp {
		return "Help"
	}
	switch m.Sub {
	case SubToolDetail:
		return m.ActiveTab.FullLabel() + "/" + m.ToolDetail.Item.ShortName
	case SubSysMon:
		return "Basic/SysMon"
	case SubNetIf:
		return "Basic/NetIF"
	case SubNotes:
		return "Basic/Notes"
	case SubFiles:
		return "Basic/Files"
	}
	return ""
}

func (m AppModel) currentHints() []Hint {
	if m.ShowSearch {
		return []Hint{{Key: "↑↓", Desc: "sel"}, {Key: "⏎", Desc: "open"}, {Key: "Esc", Desc: "close"}}
	}
	if m.ShowHelp {
		return []Hint{{Key: "↑↓", Desc: "scroll"}, {Key: "Esc", Desc: "close"}}
	}
	if m.Sub != SubNone {
		switch m.Sub {
		case SubNotes:
			if m.Notes.Adding {
				return []Hint{{Key: "⏎", Desc: "save"}, {Key: "Esc", Desc: "cancel"}}
			}
			return []Hint{{Key: "a", Desc: "add"}, {Key: "d", Desc: "del"}, {Key: "Esc", Desc: "back"}}
		case SubFiles:
			return []Hint{{Key: "↑↓", Desc: "nav"}, {Key: "⏎", Desc: "open"}, {Key: "Esc", Desc: "back"}}
		case SubToolDetail:
			return []Hint{{Key: "p", Desc: "pin"}, {Key: "↑↓", Desc: "scroll"}, {Key: "Esc", Desc: "back"}}
		default:
			return []Hint{{Key: "↑↓", Desc: "scroll"}, {Key: "r", Desc: "ref"}, {Key: "Esc", Desc: "back"}}
		}
	}

	switch m.ActiveTab {
	case TabOptions:
		return []Hint{
			{Key: "↑↓", Desc: "opt"},
			{Key: "←→", Desc: "set"},
			{Key: "Tab", Desc: "view"},
			{Key: "?", Desc: "help"},
		}
	case TabSec, TabBasic:
		return []Hint{
			{Key: "↑↓", Desc: "nav"},
			{Key: "⏎", Desc: "open"},
			{Key: "p", Desc: "pin"},
			{Key: "/", Desc: "find"},
			{Key: "q", Desc: "quit"},
		}
	default:
		return []Hint{
			{Key: "↑↓", Desc: "nav"},
			{Key: "⏎", Desc: "open"},
			{Key: "Tab", Desc: "tab"},
			{Key: "/", Desc: "find"},
			{Key: "?", Desc: "help"},
			{Key: "q", Desc: "quit"},
		}
	}
}
