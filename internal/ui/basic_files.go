package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/theme"
)

type FileEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

type FilesView struct {
	Dir         string
	Entries     []FileEntry
	Cursor      int
	PreviewPath string
	PreviewData []string
	PreviewOff  int
	LastErr     string
}

func NewFilesView() FilesView {
	dir, err := os.Getwd()
	if err != nil || dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if dir == "" {
		dir = "/"
	}
	v := FilesView{Dir: dir}
	v.Reload()
	return v
}

func (v *FilesView) Reload() {
	v.LastErr = ""
	entries, err := os.ReadDir(v.Dir)
	if err != nil {
		v.LastErr = err.Error()
		v.Entries = nil
		return
	}
	var dirs, files []FileEntry
	if v.Dir != "/" {
		dirs = append(dirs, FileEntry{Name: "..", IsDir: true})
	}
	for _, e := range entries {
		var sz int64
		if info, err := e.Info(); err == nil {
			sz = info.Size()
		}
		fe := FileEntry{Name: e.Name(), IsDir: e.IsDir(), Size: sz}
		if e.IsDir() {
			dirs = append(dirs, fe)
		} else {
			files = append(files, fe)
		}
	}
	sort.Slice(dirs[min(1, len(dirs)):], func(i, j int) bool {
		off := min(1, len(dirs))
		return strings.ToLower(dirs[off+i].Name) < strings.ToLower(dirs[off+j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	v.Entries = append(dirs, files...)
	if v.Cursor >= len(v.Entries) {
		v.Cursor = max(0, len(v.Entries)-1)
	}
}

func (v *FilesView) Move(delta int) {
	if v.PreviewPath != "" {
		v.PreviewOff += delta
		if v.PreviewOff < 0 {
			v.PreviewOff = 0
		}
		return
	}
	if len(v.Entries) == 0 {
		return
	}
	v.Cursor = (v.Cursor + delta + len(v.Entries)) % len(v.Entries)
}

func (v *FilesView) Open() {
	if v.PreviewPath != "" || len(v.Entries) == 0 {
		return
	}
	ent := v.Entries[v.Cursor]
	if ent.Name == ".." {
		v.Dir = filepath.Dir(v.Dir)
		v.Cursor = 0
		v.Reload()
		return
	}
	target := filepath.Join(v.Dir, ent.Name)
	if ent.IsDir {
		v.Dir = target
		v.Cursor = 0
		v.Reload()
		return
	}
	data, err := os.ReadFile(target)
	if err != nil {
		v.LastErr = err.Error()
		return
	}
	if len(data) > 64*1024 {
		data = data[:64*1024]
	}
	for i := 0; i < min(256, len(data)); i++ {
		if data[i] == 0 {
			v.LastErr = "binary file"
			return
		}
	}
	v.PreviewPath = target
	v.PreviewOff = 0
	v.PreviewData = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
}

func (v *FilesView) Back() bool {
	if v.PreviewPath != "" {
		v.PreviewPath = ""
		v.PreviewData = nil
		v.PreviewOff = 0
		return true
	}
	if v.Dir != "/" {
		v.Dir = filepath.Dir(v.Dir)
		v.Cursor = 0
		v.Reload()
		return true
	}
	return false
}

func (v FilesView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	warn := lipgloss.NewStyle().Foreground(pal.Warning)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	if v.PreviewPath != "" {
		var out []string
		out = append(out, " "+accent.Render(Truncate(filepath.Base(v.PreviewPath), max(6, w-10)))+" "+muted.Render("Esc:back"))
		avail := max(1, h-1)
		maxOff := max(0, len(v.PreviewData)-avail)
		off := min(v.PreviewOff, maxOff)
		end := min(len(v.PreviewData), off+avail)
		for _, line := range v.PreviewData[off:end] {
			out = append(out, " "+txt.Render(Truncate(line, max(4, w-1))))
		}
		return SplitLinesPadded(out, w, h)
	}

	var out []string
	out = append(out, " "+muted.Render(Truncate(v.Dir, max(6, w-2))))
	if v.LastErr != "" {
		out = append(out, " "+warn.Render(Truncate(v.LastErr, max(6, w-2))))
	}

	avail := max(1, h-len(out))
	start, end := ScrollWindow(len(v.Entries), v.Cursor, avail)
	for i := start; i < end; i++ {
		e := v.Entries[i]
		tag := formatSize(e.Size)
		name := e.Name
		if e.IsDir {
			tag = "DIR"
			if name != ".." {
				name += "/"
			}
		}
		if i == v.Cursor && focused {
			nameW := max(6, w-len(tag)-4)
			row := fmt.Sprintf(" ▸ %s %s", FitLine(name, nameW), tag)
			out = append(out, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if i == v.Cursor {
				pfx = " ▸ "
			}
			nameW := max(6, w-len(tag)-4)
			st := txt
			if e.IsDir {
				st = sec
			}
			out = append(out, fmt.Sprintf("%s%s %s", accent.Render(pfx), st.Render(FitLine(name, nameW)), muted.Render(tag)))
		}
	}
	return SplitLinesPadded(out, w, h)
}

func formatSize(sz int64) string {
	if sz < 1024 {
		return fmt.Sprintf("%3dB", sz)
	}
	if sz < 1024*1024 {
		return fmt.Sprintf("%2dKB", sz/1024)
	}
	return fmt.Sprintf("%2dMB", sz/(1024*1024))
}
