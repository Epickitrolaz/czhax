package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/czhax/czhax/internal/ir"
	"github.com/czhax/czhax/internal/theme"
)

type IRTab int

const (
	IRTabRemote IRTab = iota
	IRTabLearn
	IRTabBrowse
)

type irGridButton struct {
	Name    string
	Key     string
	Signals []ir.Signal
	Index   int
	Last    int
}

type IRView struct {
	Tab       IRTab
	Store     ir.Store
	StorePath string
	Roots     ir.Roots
	HW        ir.Hardware

	Note      string
	NoteIsErr bool

	RemoteScroll int

	Capturing      bool
	CaptureSeq     int
	CaptureCancel  context.CancelFunc
	CaptureStarted time.Time
	CaptureElapsed time.Duration
	Learned        *ir.Signal
	AwaitBinding   bool
	LearnErr       string

	Index        []ir.Entry
	IndexLoading bool
	IndexDone    bool
	IndexErr     string
	ProviderIdx  int
	CategoryIdx  int
	Categories   []string
	Filtered     []ir.Entry
	BrowseCursor int

	Remote          *ir.Remote
	RemoteLoading   bool
	RemoteErr       string
	GridMode        bool
	ProbeTarget     string
	ProbeIndex      int
	ProbeLast       ir.Signal
	ProbeLastTarget string
	GridButtons     []irGridButton
}

var irProviders = []struct {
	ID    string
	Label string
}{
	{"", "All"},
	{"flipper", "Flipper"},
	{"mi", "Mi Remote"},
}

func NewIRView(configPath string) IRView {
	storePath := ir.DefaultStorePath(configPath)
	store, err := ir.LoadStore(storePath)
	v := IRView{
		Store:     store,
		StorePath: storePath,
		Roots:     ir.DefaultRoots(),
		HW:        ir.DiscoverHardware(),
	}
	if err != nil {
		v.Note = "IR store: " + err.Error()
		v.NoteIsErr = true
	}
	return v
}

func (v *IRView) RefreshHardware() {
	v.HW = ir.DiscoverHardware()
}

func (v IRView) ProviderLabel() string {
	if v.ProviderIdx < 0 || v.ProviderIdx >= len(irProviders) {
		return "All"
	}
	return irProviders[v.ProviderIdx].Label
}

func (v IRView) ProviderID() string {
	if v.ProviderIdx < 0 || v.ProviderIdx >= len(irProviders) {
		return ""
	}
	return irProviders[v.ProviderIdx].ID
}

func (v IRView) CategoryLabel() string {
	if v.CategoryIdx <= 0 || v.CategoryIdx > len(v.Categories) {
		return "All"
	}
	return v.Categories[v.CategoryIdx-1]
}

func (v *IRView) refilter() {
	v.Filtered = ir.FilterEntries(v.Index, v.ProviderID(), v.categoryID())
	if v.BrowseCursor >= len(v.Filtered) {
		v.BrowseCursor = max(0, len(v.Filtered)-1)
	}
	if v.BrowseCursor < 0 {
		v.BrowseCursor = 0
	}
}

func (v IRView) categoryID() string {
	if v.CategoryIdx <= 0 || v.CategoryIdx > len(v.Categories) {
		return ""
	}
	return v.Categories[v.CategoryIdx-1]
}

func (v *IRView) SetIndex(entries []ir.Entry, err error) {
	v.IndexLoading = false
	v.IndexDone = true
	v.IndexErr = ""
	if err != nil {
		v.IndexErr = err.Error()
		return
	}
	v.Index = entries
	v.IndexDone = true
	v.Categories = ir.Categories(entries)
	if v.CategoryIdx > len(v.Categories) {
		v.CategoryIdx = 0
	}
	v.refilter()
	v.BrowseCursor = 0
}

func (v *IRView) CycleProvider(delta int) {
	v.ProviderIdx = (v.ProviderIdx + delta + len(irProviders)) % len(irProviders)
	v.refilter()
	v.BrowseCursor = 0
}

func (v *IRView) CycleCategory(delta int) {
	n := len(v.Categories) + 1
	v.CategoryIdx = (v.CategoryIdx + delta + n) % n
	v.refilter()
	v.BrowseCursor = 0
}

func (v IRView) SelectedEntry() (ir.Entry, bool) {
	if v.BrowseCursor < 0 || v.BrowseCursor >= len(v.Filtered) {
		return ir.Entry{}, false
	}
	return v.Filtered[v.BrowseCursor], true
}

func (v *IRView) MoveBrowse(delta int) {
	if len(v.Filtered) == 0 {
		v.BrowseCursor = 0
		return
	}
	v.BrowseCursor = (v.BrowseCursor + delta + len(v.Filtered)) % len(v.Filtered)
}

func (v *IRView) SetRemote(r ir.Remote) {
	v.RemoteLoading = false
	v.RemoteErr = ""
	v.Remote = &r
	v.GridMode = false
	v.ProbeTarget = "Power"
	v.ProbeIndex = 0
	v.ProbeLast = ir.Signal{}
	v.ProbeLastTarget = ""
	v.GridButtons = buildGridButtons(r.Signals)
	v.Note = ""
	v.NoteIsErr = false
}

func buildGridButtons(sigs []ir.Signal) []irGridButton {
	type group struct {
		name    string
		signals []ir.Signal
	}
	order := []string{}
	groups := map[string]*group{}
	for _, sig := range sigs {
		if len(sig.Timings) < 4 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(sig.Name))
		if key == "" {
			key = "signal"
		}
		g, ok := groups[key]
		if !ok {
			g = &group{name: sig.Name}
			groups[key] = g
			order = append(order, key)
		}
		g.signals = append(g.signals, sig)
	}
	used := map[string]bool{}
	var out []irGridButton
	for _, k := range order {
		g := groups[k]
		key := ir.KeyForButton(g.name)
		if key == "" || used[key] {
			key = ir.NextFreeKey(used)
		}
		if key == "" {
			break
		}
		used[key] = true
		out = append(out, irGridButton{Name: g.name, Key: key, Signals: g.signals, Last: -1})
	}
	return out
}

func matchesProbeTarget(name, target string) bool {
	n := strings.ToLower(strings.ReplaceAll(name, "_", ""))
	t := strings.ToLower(strings.ReplaceAll(target, "_", ""))
	switch t {
	case "power":
		return strings.Contains(n, "power")
	case "volup":
		return strings.Contains(n, "volup") || strings.Contains(n, "volumeup")
	case "mute":
		return strings.Contains(n, "mute")
	default:
		return n == t
	}
}

func (v IRView) probeCandidates(target string) []ir.Signal {
	if v.Remote == nil {
		return nil
	}
	var match []ir.Signal
	for _, sig := range v.Remote.Signals {
		if len(sig.Timings) < 4 {
			continue
		}
		if matchesProbeTarget(sig.Name, target) {
			match = append(match, sig)
		}
	}
	if len(match) == 0 && strings.EqualFold(target, "Power") {
		match = v.Remote.Signals
	}
	return match
}

func (v IRView) ProbeSignal() (ir.Signal, bool) {
	c := v.probeCandidates(v.ProbeTarget)
	if len(c) == 0 {
		return ir.Signal{}, false
	}
	i := v.ProbeIndex % len(c)
	return c[i], true
}

func (v *IRView) NextProbe(target string) (ir.Signal, bool) {
	if v.Remote == nil {
		return ir.Signal{}, false
	}
	if !strings.EqualFold(target, v.ProbeTarget) {
		v.ProbeTarget = target
		v.ProbeIndex = 0
	}
	c := v.probeCandidates(v.ProbeTarget)
	if len(c) == 0 {
		return ir.Signal{}, false
	}
	sig := c[v.ProbeIndex%len(c)]
	v.ProbeLast = sig
	v.ProbeLastTarget = v.ProbeTarget
	v.ProbeIndex = (v.ProbeIndex + 1) % len(c)
	return sig, true
}

func (v IRView) gridSignal(key string) (ir.Signal, string, bool) {
	for i := range v.GridButtons {
		b := &v.GridButtons[i]
		if b.Key != key {
			continue
		}
		if len(b.Signals) == 0 {
			return ir.Signal{}, "", false
		}
		sig := b.Signals[b.Index%len(b.Signals)]
		return sig, b.Name, true
	}
	return ir.Signal{}, "", false
}

func (v *IRView) advanceGrid(key string) {
	for i := range v.GridButtons {
		if v.GridButtons[i].Key == key && len(v.GridButtons[i].Signals) > 0 {
			v.GridButtons[i].Last = v.GridButtons[i].Index
			v.GridButtons[i].Index = (v.GridButtons[i].Index + 1) % len(v.GridButtons[i].Signals)
			return
		}
	}
}

func (v *IRView) AcceptRemote() bool {
	if v.Remote == nil {
		return false
	}
	var sigs []ir.Signal
	if v.GridMode {
		for _, b := range v.GridButtons {
			if len(b.Signals) == 0 {
				continue
			}
			idx := b.Index
			if b.Last >= 0 {
				idx = b.Last
			}
			sig := b.Signals[idx%len(b.Signals)]
			sig.Name = b.Name
			sigs = append(sigs, sig)
		}
	} else {
		sig, ok := v.ProbeSignal()
		if v.ProbeLastTarget != "" && strings.EqualFold(v.ProbeLastTarget, v.ProbeTarget) && len(v.ProbeLast.Timings) > 0 {
			sig, ok = v.ProbeLast, true
		}
		if !ok {
			return false
		}
		// Use every named button from the remote, but pin the accepted
		// candidate for the probed button.
		for _, s := range v.Remote.Signals {
			if len(s.Timings) < 4 {
				continue
			}
			if matchesProbeTarget(s.Name, v.ProbeTarget) {
				continue
			}
			sigs = append(sigs, s)
		}
		sig.Name = v.ProbeTarget
		sigs = append(sigs, sig)
		if len(sigs) == 1 {
			sigs = []ir.Signal{sig}
		}
	}
	if len(sigs) == 0 {
		return false
	}
	profileName := v.Remote.ProfileName()
	if strings.TrimSpace(profileName) == "" {
		profileName = "IR remote"
	}
	p := ir.BindSignals(profileName, v.Remote.Provider, v.Remote.Category, v.Remote.Brand, v.Remote.Model, sigs)
	if len(p.Bindings) == 0 {
		return false
	}
	v.Store.AddProfile(p)
	if v.saveStore() == nil {
		v.Note = "Saved profile " + p.Name
		v.NoteIsErr = false
	}
	v.Tab = IRTabRemote
	v.RemoteScroll = 0
	return true
}

func (v *IRView) saveStore() error {
	if err := v.Store.Save(v.StorePath); err != nil {
		v.Note = "Save failed: " + err.Error()
		v.NoteIsErr = true
		return err
	}
	return nil
}

func (v *IRView) StartCapture(ctx context.Context) (context.Context, int, bool) {
	if v.HW.RXPath == "" {
		v.LearnErr = "No IR receiver: " + v.HW.Err
		return nil, 0, false
	}
	if v.Capturing {
		return nil, 0, false
	}
	v.CaptureSeq++
	v.Capturing = true
	v.LearnErr = ""
	v.Learned = nil
	v.AwaitBinding = false
	v.CaptureStarted = time.Now()
	v.CaptureElapsed = 0
	v.CaptureCancel = nil
	return ctx, v.CaptureSeq, true
}

func (v *IRView) SetCaptureCancel(cancel context.CancelFunc) {
	v.CaptureCancel = cancel
}

func (v *IRView) StopCapture() {
	if v.CaptureCancel != nil {
		v.CaptureCancel()
	}
	v.Capturing = false
}

func (v *IRView) SetCaptureResult(sig ir.Signal, err error) {
	v.applyCaptureResult(sig, err)
}

func (v *IRView) ApplyCaptureResult(seq int, sig ir.Signal, err error) {
	if seq != v.CaptureSeq {
		return
	}
	v.applyCaptureResult(sig, err)
}

func (v *IRView) applyCaptureResult(sig ir.Signal, err error) {
	v.Capturing = false
	if v.CaptureCancel != nil {
		v.CaptureCancel()
		v.CaptureCancel = nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			v.LearnErr = ""
			v.Note = "Stopped"
			v.NoteIsErr = false
		} else {
			v.LearnErr = err.Error()
			v.Note = ""
		}
		v.AwaitBinding = false
		return
	}
	v.Learned = &sig
	v.AwaitBinding = true
	v.LearnErr = ""
	v.Note = ""
	v.NoteIsErr = false
}

func (v *IRView) BindLearned(key string) bool {
	if v.Learned == nil || !ir.IsBindableKey(key) {
		return false
	}
	name := "My remote"
	if p := v.Store.ActiveProfile(); p != nil && strings.TrimSpace(p.Name) != "" {
		name = p.Name
	}
	sig := v.Learned.Normalized()
	sig.Name = "Learned"
	sig.Saved = time.Now()
	v.Store.AddBinding(name, ir.Binding{Key: key, Name: "Learned", Signal: sig})
	v.AwaitBinding = false
	v.Learned = nil
	if v.saveStore() == nil {
		v.Note = "Bound to " + ir.KeyLabel(key)
		v.NoteIsErr = false
	}
	return true
}

func (v IRView) ActiveProfile() *ir.Profile {
	return v.Store.ActiveProfile()
}

func (v *IRView) CycleProfile(delta int) {
	if len(v.Store.Profiles) == 0 {
		return
	}
	n := len(v.Store.Profiles)
	v.Store.Active = (v.Store.Active + delta + n) % n
	v.RemoteScroll = 0
	if v.saveStore() == nil {
		v.Note = ""
		v.NoteIsErr = false
	}
}

func (v IRView) BindingForKey(key string) (ir.Binding, bool) {
	p := v.Store.ActiveProfile()
	if p == nil {
		return ir.Binding{}, false
	}
	for _, b := range p.Bindings {
		if b.Key == key {
			return b, true
		}
	}
	return ir.Binding{}, false
}

func (v *IRView) HandleEsc() bool {
	switch v.Tab {
	case IRTabLearn:
		if v.Capturing {
			v.StopCapture()
			return true
		}
		if v.AwaitBinding {
			v.AwaitBinding = false
			v.Learned = nil
			v.LearnErr = ""
			return true
		}
		return false
	case IRTabBrowse:
		if v.Remote != nil {
			v.Remote = nil
			v.RemoteErr = ""
			v.GridMode = false
			return true
		}
		return false
	default:
		return false
	}
}

func (v IRView) RenderLines(w, h int, pal theme.Palette, focused bool) []string {
	switch v.Tab {
	case IRTabLearn:
		return v.renderLearn(w, h, pal, focused)
	case IRTabBrowse:
		return v.renderBrowse(w, h, pal, focused)
	default:
		return v.renderRemote(w, h, pal, focused)
	}
}

func irHardwareLine(v IRView) string {
	if v.HW.RXPath == "" && v.HW.TXPath == "" {
		return "IR: no device"
	}
	rx := v.HW.RXPath
	if rx == "" {
		rx = "none"
	}
	tx := v.HW.TXPath
	if tx == "" {
		tx = "none"
	}
	return "RX " + rx + " · TX " + tx
}

func (v IRView) renderRemote(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	p := v.Store.ActiveProfile()
	if p == nil || len(p.Bindings) == 0 {
		lines := []string{
			" " + accent.Render("IR Remote"),
			" " + muted.Render("No profile yet"),
			"",
			" " + txt.Render("Learn a signal in Learn"),
			" " + txt.Render("or open Browse → device"),
			"",
			" " + muted.Render(irHardwareLine(v)),
		}
		if v.Note != "" {
			st := ok
			if v.NoteIsErr {
				st = danger
			}
			lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
		}
		return SplitLinesPadded(lines, w, h)
	}

	title := p.Name
	if p.Source != "" {
		title += " · " + p.Source
	}
	lines := []string{
		" " + accent.Render(Truncate(title, max(6, w-2))),
	}
	visible := max(1, h-2)
	maxScroll := max(0, len(p.Bindings)-visible)
	sc := min(v.RemoteScroll, maxScroll)
	end := min(len(p.Bindings), sc+visible)
	for _, b := range p.Bindings[sc:end] {
		key := ir.KeyLabel(b.Key)
		line := " " + sec.Render(FitLine(key, 6)) + txt.Render(Truncate(b.Name, max(6, w-9)))
		lines = append(lines, line)
	}
	if v.Note != "" {
		st := ok
		if v.NoteIsErr {
			st = danger
		}
		lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
	}
	if h >= 3 && v.Note == "" {
		lines = append(lines, " "+muted.Render(Truncate("[ ]:profile  Esc:tabs", max(6, w-2))))
	}
	return SplitLinesPadded(lines, w, h)
}

func (v IRView) renderLearn(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	lines := []string{
		" " + accent.Render("IR Learn · copy a signal"),
		" " + muted.Render(irHardwareLine(v)),
	}
	switch {
	case v.Capturing:
		lines = append(lines, " "+warn.Render("Listening…"))
		lines = append(lines, " "+muted.Render(Truncate(fmt.Sprintf("%ds / %ds", int(v.CaptureElapsed.Seconds()), 5), max(6, w-2))))
		lines = append(lines, " "+txt.Render("Point the remote at the receiver"))
	case v.AwaitBinding:
		sig := v.Learned
		freq := ir.DefaultFrequency
		count := 0
		if sig != nil {
			freq = sig.Frequency
			count = len(sig.Timings)
		}
		lines = append(lines, " "+ok.Render("Captured!"))
		lines = append(lines, " "+sec.Render("Timings ")+txt.Render(fmt.Sprintf("%d", count)))
		lines = append(lines, " "+sec.Render("Carrier ")+txt.Render(fmt.Sprintf("%d Hz", freq)))
		lines = append(lines, " "+warn.Render("Press a key to bind (Esc cancel)"))
	default:
		lines = append(lines, " "+txt.Render("Press l to listen"))
		lines = append(lines, " "+muted.Render("Then press a button on the remote"))
		if v.HW.RXPath == "" && v.HW.Err != "" {
			lines = append(lines, " "+danger.Render(Truncate(v.HW.Err, max(6, w-2))))
		}
	}
	if v.LearnErr != "" {
		lines = append(lines, " "+danger.Render(Truncate(v.LearnErr, max(6, w-2))))
	}
	if v.Note != "" {
		st := ok
		if v.NoteIsErr {
			st = danger
		}
		lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
	}
	if h >= 4 && v.Note == "" && v.LearnErr == "" {
		hint := "l:listen  Esc:tabs"
		if v.Capturing {
			hint = "Esc:cancel"
		} else if v.AwaitBinding {
			hint = "1-0:bind  t:test  Esc:cancel"
		}
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+muted.Render(Truncate(hint, max(6, w-2))))
	}
	return SplitLinesPadded(lines, w, h)
}

func (v IRView) renderBrowse(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	if v.Remote != nil {
		return v.renderRemoteDetail(w, h, pal, focused)
	}

	lines := []string{
		" " + accent.Render("IR Browse"),
	}
	if v.RemoteLoading {
		lines = append(lines, " "+warn.Render("Loading remote…"))
		return SplitLinesPadded(lines, w, h)
	}
	if v.RemoteErr != "" {
		lines = append(lines, " "+danger.Render(Truncate(v.RemoteErr, max(6, w-2))))
		return SplitLinesPadded(lines, w, h)
	}
	if v.IndexLoading {
		lines = append(lines, " "+warn.Render("Indexing databases…"))
		if v.Roots.Flipper == "" && v.Roots.Mi == "" {
			lines = append(lines, " "+muted.Render("No IR database directories configured"))
		}
		return SplitLinesPadded(lines, w, h)
	}
	if v.IndexErr != "" {
		lines = append(lines, " "+danger.Render(Truncate(v.IndexErr, max(6, w-2))))
		lines = append(lines, " "+muted.Render(Truncate("Set CZHAX_IR_FLIPPER / CZHAX_IR_MI", max(6, w-2))))
		return SplitLinesPadded(lines, w, h)
	}
	if len(v.Index) == 0 {
		lines = append(lines, " "+muted.Render("No remotes found"))
		lines = append(lines, " "+muted.Render("Set CZHAX_IR_FLIPPER or CZHAX_IR_MI"))
		lines = append(lines, " "+muted.Render("or use Learn to copy a remote"))
		return SplitLinesPadded(lines, w, h)
	}

	lines = append(lines, " "+sec.Render("Src ")+txt.Render(FitLine(v.ProviderLabel(), 9))+sec.Render("Type ")+txt.Render(Truncate(v.CategoryLabel(), max(4, w-19))))
	if len(v.Filtered) == 0 {
		lines = append(lines, " "+muted.Render("No remotes in this filter"))
		return SplitLinesPadded(lines, w, h)
	}
	visible := max(1, h-3)
	start, end := ScrollWindow(len(v.Filtered), v.BrowseCursor, visible)
	for i := start; i < end; i++ {
		e := v.Filtered[i]
		mark := "F"
		if e.Provider == "mi" {
			mark = "M"
		}
		row := fmt.Sprintf("▸%s %s", mark, Truncate(e.Display(), max(6, w-6)))
		if i == v.BrowseCursor && focused {
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			lines = append(lines, " "+txt.Render(Truncate(e.Display(), max(6, w-2))))
		}
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+muted.Render(Truncate("↑↓:sel ←→:type p:src ⏎:open r:index", max(6, w-2))))
	return SplitLinesPadded(lines, w, h)
}

func (v IRView) renderRemoteDetail(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	r := v.Remote
	if r == nil {
		return v.renderBrowse(w, h, pal, focused)
	}
	title := r.ProfileName()
	if title == "" {
		title = r.Name
	}
	lines := []string{
		" " + accent.Render(Truncate(title, max(6, w-2))),
	}
	if v.RemoteLoading {
		lines = append(lines, " "+warn.Render("Loading remote…"))
		return SplitLinesPadded(lines, w, h)
	}
	if v.RemoteErr != "" {
		lines = append(lines, " "+danger.Render(Truncate(v.RemoteErr, max(6, w-2))))
		return SplitLinesPadded(lines, w, h)
	}

	if v.GridMode {
		lines = append(lines, " "+sec.Render("Grid ")+muted.Render("press a key to send / next"))
		visible := max(1, h-3)
		for i, b := range v.GridButtons {
			if i >= visible {
				break
			}
			idx := b.Index
			if b.Last >= 0 {
				idx = b.Last
			}
			if len(b.Signals) > 0 {
				idx = idx % len(b.Signals)
			}
			count := fmt.Sprintf("%d/%d", idx+1, len(b.Signals))
			row := " " + sec.Render(FitLine(ir.KeyLabel(b.Key), 5)) + txt.Render(FitLine(b.Name, max(5, w-15))) + muted.Render(count)
			lines = append(lines, row)
		}
	} else {
		sig, ok := v.ProbeSignal()
		cands := v.probeCandidates(v.ProbeTarget)
		pos := 0
		if len(cands) > 0 {
			if v.ProbeLastTarget != "" && strings.EqualFold(v.ProbeLastTarget, v.ProbeTarget) && len(v.ProbeLast.Timings) > 0 {
				sig, ok = v.ProbeLast, true
				pos = (v.ProbeIndex-1+len(cands))%len(cands) + 1
			} else {
				pos = v.ProbeIndex%len(cands) + 1
			}
		}
		lines = append(lines, " "+sec.Render("Probe ")+txt.Render(v.ProbeTarget)+" "+muted.Render(fmt.Sprintf("%d/%d", pos, len(cands))))
		if ok {
			desc := sig.Name
			if sig.Protocol != "" {
				desc = sig.Protocol
			}
			if sig.Comment != "" {
				desc += " · " + sig.Comment
			}
			lines = append(lines, " "+txt.Render(Truncate(desc, max(6, w-2))))
			lines = append(lines, " "+muted.Render(fmt.Sprintf("%d timings · %d Hz", len(sig.Timings), sig.Frequency)))
		} else {
			lines = append(lines, " "+danger.Render("No candidate signals"))
		}
	}
	if v.Note != "" {
		st := ok
		if v.NoteIsErr {
			st = danger
		}
		lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
	}
	if h >= 4 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		hint := "p/v/m:try a:accept g:grid Esc:back"
		if v.GridMode {
			hint = "key:send/next a:accept g:probe Esc:back"
		}
		lines = append(lines, " "+muted.Render(Truncate(hint, max(6, w-2))))
	}
	return SplitLinesPadded(lines, w, h)
}
