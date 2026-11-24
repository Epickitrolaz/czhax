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
			v.Lines = []string{"CZHax field notes.", "Press 'a' to add note."}
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

func (v NotesView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	var out []string
	if v.Adding {
		out = append(out, " "+accent.Render("New note (Enter save, Esc cancel):"))
		out = append(out, sel.Width(w).Render(" > "+v.Draft+"_"))
		return SplitLinesPadded(out, w, h)
	}

	if len(v.Lines) == 0 {
		out = append(out, " "+muted.Render("No notes. Press 'a' to add."))
		return SplitLinesPadded(out, w, h)
	}

	avail := max(1, h-1)
	start, end := ScrollWindow(len(v.Lines), v.Cursor, avail)
	for i := start; i < end; i++ {
		if i == v.Cursor && focused {
			row := fmt.Sprintf(" ▸ %s", v.Lines[i])
			out = append(out, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if i == v.Cursor {
				pfx = " ▸ "
			}
			out = append(out, txt.Render(Truncate(pfx+v.Lines[i], w)))
		}
	}
	for len(out) < h-1 {
		out = append(out, "")
	}
	out = append(out, " "+muted.Render("a:add  d:del  Esc:tabs"))
	return SplitLinesPadded(out, w, h)
}
