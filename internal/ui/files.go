package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/files"
	"github.com/czhax/czhax/internal/theme"
	"github.com/czhax/czhax/internal/workdir"
)

type FilesMode int

const (
	filesModeList FilesMode = iota
	filesModeText
	filesModeImage
	filesModeDelete
	filesModeInput
	filesModeRecord
)

type filesInputAction int

const (
	filesInputNewFile filesInputAction = iota
	filesInputNewDir
	filesInputRename
)

type FilesView struct {
	Root string

	Rel     string
	Entries []files.Entry
	Cursor  int

	Mode      FilesMode
	Note      string
	NoteIsErr bool
	Marquee   Marquee

	TextPath   string
	TextName   string
	TextLines  []string
	TextLine   int
	TextCol    int
	TextScroll int
	TextDirty  bool

	ImageName string
	Image     image.Image
	ImageErr  string

	DeleteEntry files.Entry
	InputAction filesInputAction
	Input       string
	InputPrompt string

	Recording     bool
	RecordSeq     int
	RecordStarted time.Time
	RecordElapsed time.Duration
	RecordPath    string
	RecordErr     string
	RecordCancel  context.CancelFunc
}

func NewFilesView(root string) FilesView {
	v := FilesView{Root: root}
	_ = workdir.Ensure(root)
	v.Reload()
	return v
}

func (v *FilesView) Reload() {
	entries, err := files.List(v.Root, v.Rel)
	if err != nil {
		v.Note = err.Error()
		v.NoteIsErr = true
		v.Entries = nil
		return
	}
	v.Entries = entries
	if v.Cursor >= len(entries) {
		v.Cursor = max(0, len(entries)-1)
	}
	if v.Cursor < 0 {
		v.Cursor = 0
	}
}

func (v *FilesView) Selected() (files.Entry, bool) {
	if v.Cursor < 0 || v.Cursor >= len(v.Entries) {
		return files.Entry{}, false
	}
	return v.Entries[v.Cursor], true
}

func (v *FilesView) Move(delta int) {
	if len(v.Entries) == 0 {
		v.Cursor = 0
		return
	}
	v.Cursor = (v.Cursor + delta + len(v.Entries)) % len(v.Entries)
	v.Note = ""
	v.NoteIsErr = false
}

func (v *FilesView) HandleEsc() bool {
	switch v.Mode {
	case filesModeText:
		if v.TextDirty {
			_ = v.SaveText()
		}
		v.Mode = filesModeList
		v.Reload()
		return true
	case filesModeImage:
		v.Mode = filesModeList
		v.Image = nil
		v.ImageErr = ""
		return true
	case filesModeDelete, filesModeInput:
		v.Mode = filesModeList
		v.Input = ""
		return true
	case filesModeRecord:
		if v.Recording {
			v.StopRecording()
			return true
		}
		v.Mode = filesModeList
		return true
	default:
		if v.Rel != "" {
			v.Rel = filepath.Dir(v.Rel)
			if v.Rel == "." {
				v.Rel = ""
			}
			v.Reload()
			v.Cursor = 0
			return true
		}
		return false
	}
}

// EnterSelected navigates directories, opens text/images, or returns a media
// kind so the caller can launch an external player.
func (v *FilesView) EnterSelected() (mediaKind, mediaPath string, err error) {
	e, ok := v.Selected()
	if !ok {
		return "", "", nil
	}
	if e.Dir {
		v.Rel = e.Rel
		v.Reload()
		v.Cursor = 0
		return "", "", nil
	}
	switch {
	case files.IsText(e.Name):
		if err := v.OpenText(e); err != nil {
			return "", "", err
		}
	case files.IsImage(e.Name):
		if err := v.OpenImage(e); err != nil {
			return "", "", err
		}
	case files.IsAudio(e.Name):
		return "audio", e.Path, nil
	case files.IsVideo(e.Name):
		return "video", e.Path, nil
	default:
		v.Note = "No viewer for " + filepath.Ext(e.Name)
		v.NoteIsErr = true
	}
	return "", "", nil
}

func (v *FilesView) OpenText(e files.Entry) error {
	lines, err := files.ReadText(v.Root, e.Rel)
	if err != nil {
		return err
	}
	v.TextPath = e.Rel
	v.TextName = e.Name
	v.TextLines = lines
	v.TextLine = 0
	v.TextCol = 0
	v.TextScroll = 0
	v.TextDirty = false
	v.Mode = filesModeText
	return nil
}

func (v *FilesView) SaveText() error {
	if len(v.TextLines) == 0 {
		v.TextLines = []string{""}
	}
	if err := files.WriteText(v.Root, v.TextPath, v.TextLines); err != nil {
		v.Note = err.Error()
		v.NoteIsErr = true
		return err
	}
	v.TextDirty = false
	v.Note = "Saved " + v.TextName
	v.NoteIsErr = false
	return nil
}

func (v *FilesView) OpenImage(e files.Entry) error {
	f, err := os.Open(e.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	v.Image = img
	v.ImageName = e.Name
	v.ImageErr = ""
	v.Mode = filesModeImage
	return nil
}

func (v *FilesView) BeginInput(action filesInputAction) {
	e, _ := v.Selected()
	v.Mode = filesModeInput
	v.InputAction = action
	v.Input = ""
	switch action {
	case filesInputNewFile:
		v.InputPrompt = "New file name:"
	case filesInputNewDir:
		v.InputPrompt = "New folder name:"
	case filesInputRename:
		v.InputPrompt = "Rename " + e.Name + ":"
		v.Input = e.Name
	}
}

func (v *FilesView) ConfirmInput() error {
	name := strings.TrimSpace(v.Input)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		v.Note = "Enter a simple file name"
		v.NoteIsErr = true
		return nil
	}
	switch v.InputAction {
	case filesInputNewFile:
		rel := filepath.Join(v.Rel, name)
		if err := files.CreateText(v.Root, rel); err != nil {
			return err
		}
		e := files.Entry{Name: name, Rel: rel, Path: filepath.Join(v.Root, rel)}
		v.Mode = filesModeList
		v.Reload()
		v.selectName(name)
		return v.OpenText(e)
	case filesInputNewDir:
		rel := filepath.Join(v.Rel, name)
		if err := files.Mkdir(v.Root, rel); err != nil {
			return err
		}
		v.Mode = filesModeList
		v.Reload()
		v.selectName(name)
	case filesInputRename:
		e, ok := v.Selected()
		if !ok {
			v.Mode = filesModeList
			return nil
		}
		if _, err := files.Rename(v.Root, e.Rel, name); err != nil {
			return err
		}
		v.Mode = filesModeList
		v.Reload()
		v.selectName(name)
	}
	v.Input = ""
	return nil
}

func (v *FilesView) selectName(name string) {
	for i, e := range v.Entries {
		if e.Name == name {
			v.Cursor = i
			return
		}
	}
}

func (v *FilesView) BeginDelete() bool {
	e, ok := v.Selected()
	if !ok {
		return false
	}
	v.DeleteEntry = e
	v.Mode = filesModeDelete
	return true
}

func (v *FilesView) ConfirmDelete() error {
	err := files.Delete(v.Root, v.DeleteEntry.Rel)
	v.Mode = filesModeList
	v.Reload()
	if err != nil {
		v.Note = err.Error()
		v.NoteIsErr = true
		return err
	}
	return nil
}

func (v *FilesView) StartRecording() (context.Context, int, string, bool) {
	if v.Recording {
		return nil, 0, "", false
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.RecordSeq++
	v.RecordPath = filepath.Join(workdir.Recordings(v.Root), fmt.Sprintf("mic-%s.wav", time.Now().Format("20060102-150405")))
	v.Recording = true
	v.RecordStarted = time.Now()
	v.RecordElapsed = 0
	v.RecordErr = ""
	v.RecordCancel = cancel
	v.Mode = filesModeRecord
	v.Note = ""
	v.NoteIsErr = false
	return ctx, v.RecordSeq, v.RecordPath, true
}

func (v *FilesView) StopRecording() {
	if v.RecordCancel != nil {
		v.RecordCancel()
	}
}

func (v *FilesView) FinishRecording(seq int, err error) {
	if seq != v.RecordSeq {
		return
	}
	v.Recording = false
	v.RecordCancel = nil
	if !v.RecordStarted.IsZero() {
		v.RecordElapsed = time.Since(v.RecordStarted)
	}
	v.Mode = filesModeList
	v.Reload()
	if err != nil {
		v.RecordErr = err.Error()
		v.Note = "Recording failed"
		v.NoteIsErr = true
		return
	}
	v.Note = "Saved " + filepath.Base(v.RecordPath)
	v.NoteIsErr = false
}

func (v *FilesView) TextKey(msg tea.KeyMsg) bool {
	if v.Mode != filesModeText {
		return false
	}
	if len(v.TextLines) == 0 {
		v.TextLines = []string{""}
	}
	line := clampIndex(v.TextLine, len(v.TextLines))
	runes := []rune(v.TextLines[line])
	v.TextCol = min(max(0, v.TextCol), len(runes))
	changed := false
	switch msg.Type {
	case tea.KeyUp:
		v.TextLine = max(0, v.TextLine-1)
		v.TextCol = min(v.TextCol, len([]rune(v.TextLines[v.TextLine])))
	case tea.KeyDown:
		v.TextLine = min(len(v.TextLines)-1, v.TextLine+1)
		v.TextCol = min(v.TextCol, len([]rune(v.TextLines[v.TextLine])))
	case tea.KeyLeft:
		if v.TextCol > 0 {
			v.TextCol--
		} else if v.TextLine > 0 {
			v.TextLine--
			v.TextCol = len([]rune(v.TextLines[v.TextLine]))
		}
	case tea.KeyRight:
		if v.TextCol < len(runes) {
			v.TextCol++
		} else if v.TextLine < len(v.TextLines)-1 {
			v.TextLine++
			v.TextCol = 0
		}
	case tea.KeyHome:
		v.TextCol = 0
	case tea.KeyEnd:
		v.TextCol = len(runes)
	case tea.KeyCtrlS:
		_ = v.SaveText()
		return true
	case tea.KeyCtrlU:
		v.TextLines[v.TextLine] = ""
		v.TextCol = 0
		changed = true
	case tea.KeyEnter:
		left := string(runes[:v.TextCol])
		right := string(runes[v.TextCol:])
		v.TextLines = append(v.TextLines[:v.TextLine], append([]string{left, right}, v.TextLines[v.TextLine+1:]...)...)
		v.TextLine++
		v.TextCol = 0
		changed = true
	case tea.KeyBackspace:
		if v.TextCol > 0 {
			v.TextLines[v.TextLine] = string(runes[:v.TextCol-1]) + string(runes[v.TextCol:])
			v.TextCol--
			changed = true
		} else if v.TextLine > 0 {
			prev := []rune(v.TextLines[v.TextLine-1])
			v.TextLines[v.TextLine-1] = string(prev) + string(runes)
			v.TextLines = append(v.TextLines[:v.TextLine], v.TextLines[v.TextLine+1:]...)
			v.TextLine--
			v.TextCol = len(prev)
			changed = true
		}
	case tea.KeyDelete:
		if v.TextCol < len(runes) {
			v.TextLines[v.TextLine] = string(runes[:v.TextCol]) + string(runes[v.TextCol+1:])
			changed = true
		}
	case tea.KeySpace:
		v.TextLines[v.TextLine] = string(runes[:v.TextCol]) + " " + string(runes[v.TextCol:])
		v.TextCol++
		changed = true
	case tea.KeyTab:
		v.TextLines[v.TextLine] = string(runes[:v.TextCol]) + "  " + string(runes[v.TextCol:])
		v.TextCol += 2
		changed = true
	case tea.KeyRunes:
		s := string(msg.Runes)
		v.TextLines[v.TextLine] = string(runes[:v.TextCol]) + s + string(runes[v.TextCol:])
		v.TextCol += len(msg.Runes)
		changed = true
	}
	if changed {
		v.TextDirty = true
		v.Note = ""
	}
	// Keep the cursor line visible.
	if v.TextLine < v.TextScroll {
		v.TextScroll = v.TextLine
	}
	return true
}

func (v *FilesView) InputKey(msg tea.KeyMsg) bool {
	if v.Mode != filesModeInput {
		return false
	}
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		v.Input = dropLastRune(v.Input)
	case tea.KeyCtrlU:
		v.Input = ""
	case tea.KeySpace:
		v.Input += " "
	case tea.KeyRunes:
		v.Input += string(msg.Runes)
	}
	return true
}

func (v FilesView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	switch v.Mode {
	case filesModeText:
		return v.renderText(w, h, pal)
	case filesModeImage:
		return v.renderImage(w, h, pal)
	case filesModeDelete:
		return v.renderDelete(w, h, pal)
	case filesModeInput:
		return v.renderInput(w, h, pal)
	case filesModeRecord:
		return v.renderRecord(w, h, pal)
	default:
		return v.renderList(w, h, pal, focused)
	}
}

func (v FilesView) renderList(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	where := v.Rel
	if where == "" {
		where = filepath.Base(v.Root)
	}
	header := " " + sec.Render("Dir:") + accent.Render(v.Marquee.Truncate(where, max(6, w-12))) + muted.Render(fmt.Sprintf(" %d", len(v.Entries)))
	if h <= 1 {
		return v.Marquee.SplitLinesPadded([]string{header}, w, h)
	}

	showBottom := (h >= 11 || v.Note != "") && h >= 4
	listH := h - 1
	if showBottom {
		listH = h - 2
	}
	lines := []string{header}
	if len(v.Entries) == 0 {
		lines = append(lines, " "+muted.Render("Empty folder"))
		lines = append(lines, " "+muted.Render("n:new file  m:new dir"))
	} else {
		start, end := ScrollWindow(len(v.Entries), v.Cursor, listH)
		for i := start; i < end; i++ {
			e := v.Entries[i]
			selected := i == v.Cursor
			name := e.Name
			if e.Dir {
				name += "/"
			}
			right := ""
			if !e.Dir {
				right = files.HumanSize(e.Size)
			}
			nameW := max(4, w-2-lipgloss.Width(right)-1)
			nameTxt := v.Marquee.FitLine(name, nameW)
			if selected && focused {
				row := fmt.Sprintf(" ▸ %s %s", nameTxt, right)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "   "
				if selected {
					pfx = accent.Render(" ▸ ")
				}
				style := txt
				if e.Dir {
					style = sec
				}
				lines = append(lines, pfx+style.Render(nameTxt)+" "+muted.Render(right))
			}
		}
	}

	if showBottom {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		if v.Note != "" {
			st := ok
			if v.NoteIsErr {
				st = danger
			}
			lines = append(lines, " "+st.Render(v.Marquee.Truncate(v.Note, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(v.Marquee.Truncate("⏎:open n:file m:dir x:del r:ren Esc:up", max(6, w-2))))
		}
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v FilesView) renderText(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	cur := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)

	name := v.TextName
	if v.TextDirty {
		name += "*"
	}
	lines := []string{" " + sec.Render("Edit ") + accent.Render(v.Marquee.Truncate(name, max(6, w-8)))}
	if h <= 1 {
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}

	bodyH := h - 2
	if bodyH < 1 {
		bodyH = 1
	}
	scroll := v.TextScroll
	if v.TextLine < scroll {
		scroll = v.TextLine
	}
	if v.TextLine >= scroll+bodyH {
		scroll = v.TextLine - bodyH + 1
	}
	if scroll < 0 {
		scroll = 0
	}
	for i := 0; i < bodyH; i++ {
		idx := scroll + i
		if idx >= len(v.TextLines) {
			lines = append(lines, "")
			continue
		}
		raw := []rune(v.TextLines[idx])
		if idx == v.TextLine {
			col := min(max(0, v.TextCol), len(raw))
			left := string(raw[:col])
			cursor := "▏"
			if col < len(raw) {
				cursor = string(raw[col])
			}
			right := ""
			if col+1 < len(raw) {
				right = string(raw[col+1:])
			}
			lines = append(lines, " "+cur.Render(v.Marquee.Truncate(">"+left+cursor+right, max(6, w-2))))
		} else {
			lines = append(lines, " "+txt.Render(v.Marquee.Truncate(string(raw), max(6, w-2))))
		}
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+muted.Render(v.Marquee.Truncate("Ctrl+S:save ↑↓←→:move Esc:back", max(6, w-2))))
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v FilesView) renderImage(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)

	lines := []string{" " + accent.Render(v.Marquee.Truncate("Image · "+v.ImageName, max(6, w-2)))}
	if h <= 1 {
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}
	if v.ImageErr != "" {
		lines = append(lines, " "+danger.Render(v.Marquee.Truncate(v.ImageErr, max(6, w-2))))
		return v.Marquee.SplitLinesPadded(lines, w, h)
	}
	bodyH := h - 2
	if bodyH < 1 {
		bodyH = 1
	}
	if v.Image != nil {
		lines = append(lines, renderImageHalfBlocks(v.Image, w, bodyH)...)
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+muted.Render(v.Marquee.Truncate("Esc:back", max(6, w-2))))
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v FilesView) renderDelete(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	lines := []string{
		" " + accent.Render("Delete?"),
		" " + warn.Render(v.Marquee.Truncate(v.DeleteEntry.Name, max(6, w-2))),
		" " + muted.Render("y:delete  n/Esc:cancel"),
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v FilesView) renderInput(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	lines := []string{
		" " + accent.Render(v.Marquee.Truncate(v.InputPrompt, max(6, w-2))),
	}
	fieldW := max(6, w-5)
	lines = append(lines, " > "+txt.Render(tailWindow(v.Input, fieldW-1)+"_"))
	if h >= 5 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+muted.Render("⏎:ok  Esc:cancel"))
	}
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func (v FilesView) renderRecord(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	lines := []string{" " + accent.Render("Record microphone")}
	lines = append(lines, " "+sec.Render("File  ")+txt.Render(v.Marquee.Truncate(filepath.Base(v.RecordPath), max(6, w-8))))
	lines = append(lines, " "+warn.Render(fmt.Sprintf("● %ds", int(v.RecordElapsed.Seconds()))))
	if v.RecordErr != "" {
		lines = append(lines, " "+muted.Render(v.Marquee.Truncate(v.RecordErr, max(6, w-2))))
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+muted.Render("Esc/⏎:stop"))
	return v.Marquee.SplitLinesPadded(lines, w, h)
}

func renderImageHalfBlocks(img image.Image, w, h int) []string {
	b := img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw <= 0 || ih <= 0 || w <= 0 || h <= 0 {
		return nil
	}
	cellH := h * 2
	scale := float64(w) / float64(iw)
	if s := float64(cellH) / float64(ih); s < scale {
		scale = s
	}
	tw := int(float64(iw) * scale)
	th := int(float64(ih) * scale)
	if tw < 1 {
		tw = 1
	}
	if th < 2 {
		th = 2
	}
	th = (th / 2) * 2
	if th < 2 {
		th = 2
	}
	xoff := (w - tw) / 2
	yoff := (h*2 - th) / 2
	lines := make([]string, 0, h)
	for cy := 0; cy < h; cy++ {
		var sb strings.Builder
		for cx := 0; cx < w; cx++ {
			px := cx - xoff
			py := cy*2 - yoff
			if px < 0 || px >= tw || py < 0 || py+1 >= th {
				sb.WriteByte(' ')
				continue
			}
			sx := b.Min.X + px*iw/tw
			sy0 := b.Min.Y + py*ih/th
			sy1 := b.Min.Y + (py+1)*ih/th
			r0, g0, b0 := rgba8(img.At(sx, sy0))
			r1, g1, b1 := rgba8(img.At(sx, sy1))
			sb.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", r0, g0, b0, r1, g1, b1))
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}
	return lines
}

func rgba8(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}
