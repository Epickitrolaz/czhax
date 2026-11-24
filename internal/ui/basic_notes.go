package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/theme"
)

type NotesView struct {
	Lines   []string
	Cursor  int
	Adding  bool
	Draft   string
	Path    string
	LastErr string
}

func NewNotesView() NotesView {
	v := NotesView{Path: config.NotesPath()}
	v.Load()
	return v
}

func (v *NotesView) Load() {
	data, err := os.ReadFile(v.Path)
	if err != nil {
		if os.IsNotExist(err) {
			v.Lines = []string{"CZHax Phase 1 ready.", "Press 'a' to add a quick field note."}
			return
		}
		v.LastErr = err.Error()
		return
	}
	raw := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	v.Lines = nil
	for _, l := range raw {
		if strings.TrimSpace(l) != "" {
			v.Lines = append(v.Lines, l)
		}
	}
}

func (v *NotesView) Save() error {
	if err := os.MkdirAll(filepath.Dir(v.Path), 0o755); err != nil {
		v.LastErr = err.Error()
		return err
	}
	content := strings.Join(v.Lines, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(v.Path, []byte(content), 0o644); err != nil {
		v.LastErr = err.Error()
		return err
	}
	v.LastErr = ""
	return nil
}

func (v *NotesView) Move(delta int) {
	if len(v.Lines) == 0 {
		return
	}
	v.Cursor = (v.Cursor + delta + len(v.Lines)) % len(v.Lines)
}

func (v *NotesView) AddLine(text string) {
	t := strings.TrimSpace(text)
	if t == "" {
		return
	}
	v.Lines = append(v.Lines, t)
	v.Cursor = len(v.Lines) - 1
	_ = v.Save()
}

func (v *NotesView) DeleteCurrent() {
	if len(v.Lines) == 0 || v.Cursor < 0 || v.Cursor >= len(v.Lines) {
		return
	}
	v.Lines = append(v.Lines[:v.Cursor], v.Lines[v.Cursor+1:]...)
	if v.Cursor >= len(v.Lines) && v.Cursor > 0 {
		v.Cursor--
	}
	_ = v.Save()
}

func (v NotesView) Render(w, h int, pal theme.Palette) string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	var out []string
	out = append(out, accent.Render("QUICK NOTES")+" "+muted.Render(ScrollBadge(v.Cursor, len(v.Lines)))+" "+muted.Render("a:add d:del"))

	if v.Adding {
		out = append(out, accent.Render("New note (Enter=save Esc=cancel):"))
		out = append(out, sel.Width(w).Render("> "+v.Draft+"_"))
		return PadLines(out, w, h, "")
	}

	if len(v.Lines) == 0 {
		out = append(out, muted.Render("No notes yet. Press 'a' to add one."))
		return PadLines(out, w, h, "")
	}

	avail := max(1, h-1)
	start, end := ScrollWindow(len(v.Lines), v.Cursor, avail)
	for i := start; i < end; i++ {
		if i == v.Cursor {
			row := fmt.Sprintf("▸ %s", v.Lines[i])
			out = append(out, sel.Width(w).Render(Truncate(row, w)))
		} else {
			out = append(out, txt.Render(fmt.Sprintf("  %s", Truncate(v.Lines[i], max(4, w-2)))))
		}
	}
	return PadLines(out, w, h, "")
}
