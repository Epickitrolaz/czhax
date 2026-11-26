package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

type irBrowseMode int

const (
	browseModeList irBrowseMode = iota
	browseModeMatchType
	browseModeMatchBrand
	browseModeMatchTest
	browseModeUnivType
	browseModeUnivRemote
)

type irRemoteMode int

const (
	remoteModeUse irRemoteMode = iota
	remoteModeList
	remoteModeNew
)

type learnMenuOption int

const (
	learnOptListen learnMenuOption = iota
	learnOptProfile
	learnOptNewRemote
	learnOptCount
)

type learnCapturedOption int

const (
	learnCapBind learnCapturedOption = iota
	learnCapTest
	learnCapAgain
	learnCapDiscard
	learnCapCount
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

	RemoteMode       irRemoteMode
	RemoteScroll     int
	RemoteListCursor int
	EmptyMenuCursor  int
	NewRemoteName    string
	NewRemoteErr     string
	NewRemoteInput   bool // when creating a remote from Learn tab

	Capturing       bool
	CaptureSeq      int
	CaptureCancel   context.CancelFunc
	CaptureStarted  time.Time
	CaptureElapsed  time.Duration
	Learned         *ir.Signal
	AwaitBinding    bool
	AwaitKeyPress   bool
	AwaitBindLabel  bool
	PendingBindKey  string
	PendingBindName string
	LearnCursor     int
	CapturedCursor  int
	LearnErr        string

	Index           []ir.Entry
	IndexLoading    bool
	IndexDone       bool
	IndexErr        string
	ProviderIdx     int
	CategoryIdx     int
	Categories      []string
	BrowseQuery     string
	BrowseSearching bool
	Filtered        []ir.Entry
	BrowseCursor    int
	BrowseMode      irBrowseMode
	ASCII           bool

	CategoryStats       []ir.CategoryCount
	TypeCursor          int
	MatchCategory       string
	MatchBrand          string
	MatchBrandQuery     string
	MatchBrandSearching bool
	MatchBrands         []ir.BrandCount
	MatchBrandCursor    int
	MatchLoading        bool
	MatchSaving         bool
	MatchErr            string
	MatchCandidates     []ir.MatchCandidate
	MatchCursor         int
	MatchActionCursor   int

	UnivCategory    string
	UnivButtons     []ir.UniversalButton
	UnivCursor      int
	UnivBlasting    bool
	UnivSeq         int
	UnivBlastTarget string
	UnivBlastCancel context.CancelFunc
	UnivProgressCh  chan ir.UniversalProgress
	UnivProgress    ir.UniversalProgress
	UnivLastBrand   string
	UnivBlastErr    string

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
	v.Filtered = ir.SearchEntries(v.Index, v.ProviderID(), v.categoryID(), v.BrowseQuery)
	if v.BrowseCursor >= len(v.Filtered) {
		v.BrowseCursor = max(0, len(v.Filtered)-1)
	}
	if v.BrowseCursor < 0 {
		v.BrowseCursor = 0
	}
}

func (v *IRView) JumpBrand(delta int) {
	if len(v.Filtered) <= 1 || delta == 0 {
		return
	}
	curBrand := v.Filtered[v.BrowseCursor].Brand
	n := len(v.Filtered)
	step := 1
	if delta < 0 {
		step = -1
	}
	for off := 1; off < n; off++ {
		idx := (v.BrowseCursor + off*step + n) % n
		if v.Filtered[idx].Brand != curBrand {
			v.BrowseCursor = idx
			return
		}
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
	v.CategoryStats = ir.CategoryCounts(entries)
	v.TypeCursor = clampIndex(v.TypeCursor, len(v.CategoryStats))
	if v.CategoryIdx > len(v.Categories) {
		v.CategoryIdx = 0
	}
	v.refilter()
	v.BrowseCursor = 0
	if v.BrowseMode == browseModeMatchBrand {
		v.refreshMatchBrands()
	}
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
		if len(sig.Timings) < 3 {
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
		if len(sig.Timings) < 3 {
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
	key = ir.NormalizeBindKey(key)
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
	key = ir.NormalizeBindKey(key)
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
			if len(s.Timings) < 3 {
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

func (v IRView) IsTextInput() bool {
	switch v.Tab {
	case IRTabRemote:
		return v.RemoteMode == remoteModeNew
	case IRTabLearn:
		return v.NewRemoteInput || v.AwaitBindLabel
	case IRTabBrowse:
		if v.Remote != nil {
			return false
		}
		return (v.BrowseMode == browseModeList && v.BrowseSearching) ||
			(v.BrowseMode == browseModeMatchBrand && v.MatchBrandSearching)
	}
	return false
}

func (v IRView) IsKeyCaptureActive() bool {
	return v.Tab == IRTabLearn && v.AwaitKeyPress && !v.AwaitBindLabel && v.Learned != nil
}

func (v *IRView) MoveRemoteList(delta int) {
	total := len(v.Store.Profiles) + 1 // +1 for "+ New remote"
	v.RemoteListCursor = (v.RemoteListCursor + delta + total) % total
	v.Note = ""
	v.NoteIsErr = false
}

func (v *IRView) MoveEmptyMenu(delta int) {
	v.EmptyMenuCursor = (v.EmptyMenuCursor + delta + 5) % 5
	v.Note = ""
	v.NoteIsErr = false
}

func (v *IRView) StartMatchWizard() {
	v.Remote = nil
	v.RemoteErr = ""
	v.BrowseSearching = false
	v.MatchBrandSearching = false
	v.MatchErr = ""
	v.MatchLoading = false
	v.MatchSaving = false
	v.Note = ""
	v.NoteIsErr = false
	v.CategoryStats = ir.CategoryCounts(v.Index)
	v.TypeCursor = clampIndex(v.TypeCursor, len(v.CategoryStats))
	v.BrowseMode = browseModeMatchType
}

func (v *IRView) StartUniversalWizard() {
	v.Remote = nil
	v.RemoteErr = ""
	v.BrowseSearching = false
	v.MatchBrandSearching = false
	v.UnivBlastErr = ""
	v.Note = ""
	v.NoteIsErr = false
	v.CategoryStats = ir.CategoryCounts(v.Index)
	v.TypeCursor = clampIndex(v.TypeCursor, len(v.CategoryStats))
	v.BrowseMode = browseModeUnivType
}

func (v *IRView) MoveType(delta int) {
	if len(v.CategoryStats) == 0 {
		v.TypeCursor = 0
		return
	}
	v.TypeCursor = (v.TypeCursor + delta + len(v.CategoryStats)) % len(v.CategoryStats)
}

func (v *IRView) ConfirmMatchType() bool {
	if v.TypeCursor < 0 || v.TypeCursor >= len(v.CategoryStats) {
		return false
	}
	v.MatchCategory = v.CategoryStats[v.TypeCursor].Name
	v.MatchBrandQuery = ""
	v.MatchBrandSearching = false
	v.MatchBrandCursor = 0
	v.MatchErr = ""
	v.Note = ""
	v.NoteIsErr = false
	v.refreshMatchBrands()
	v.BrowseMode = browseModeMatchBrand
	return true
}

func (v *IRView) ConfirmUnivType() bool {
	if v.TypeCursor < 0 || v.TypeCursor >= len(v.CategoryStats) {
		return false
	}
	v.UnivCategory = v.CategoryStats[v.TypeCursor].Name
	v.UnivButtons = ir.UniversalButtonsForCategory(v.UnivCategory)
	v.UnivCursor = 0
	v.UnivBlastErr = ""
	v.UnivLastBrand = ""
	v.Note = ""
	v.NoteIsErr = false
	v.BrowseMode = browseModeUnivRemote
	return true
}

func (v *IRView) refreshMatchBrands() {
	v.MatchBrands = ir.BrandsForCategory(v.Index, v.MatchCategory, v.MatchBrandQuery)
	v.MatchBrandCursor = clampIndex(v.MatchBrandCursor, len(v.MatchBrands))
}

func (v *IRView) MoveMatchBrand(delta int) {
	if len(v.MatchBrands) == 0 {
		v.MatchBrandCursor = 0
		return
	}
	v.MatchBrandCursor = (v.MatchBrandCursor + delta + len(v.MatchBrands)) % len(v.MatchBrands)
}

func (v *IRView) JumpMatchBrandLetter(delta int) {
	if len(v.MatchBrands) <= 1 || delta == 0 {
		return
	}
	firstLetter := func(s string) byte {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return 0
		}
		return s[0]
	}
	cur := firstLetter(v.MatchBrands[v.MatchBrandCursor].Name)
	n := len(v.MatchBrands)
	step := 1
	if delta < 0 {
		step = -1
	}
	for off := 1; off < n; off++ {
		idx := (v.MatchBrandCursor + off*step + n) % n
		if firstLetter(v.MatchBrands[idx].Name) != cur {
			v.MatchBrandCursor = idx
			return
		}
	}
}

func (v IRView) SelectedMatchBrand() (ir.BrandCount, bool) {
	if v.MatchBrandCursor < 0 || v.MatchBrandCursor >= len(v.MatchBrands) {
		return ir.BrandCount{}, false
	}
	return v.MatchBrands[v.MatchBrandCursor], true
}

func (v *IRView) BeginMatchCandidates() (category, brand string, ok bool) {
	bc, ok := v.SelectedMatchBrand()
	if !ok {
		return "", "", false
	}
	v.MatchBrand = bc.Name
	v.MatchBrandSearching = false
	v.MatchLoading = true
	v.MatchSaving = false
	v.MatchErr = ""
	v.MatchCandidates = nil
	v.MatchCursor = 0
	v.MatchActionCursor = 0
	v.Note = ""
	v.NoteIsErr = false
	v.BrowseMode = browseModeMatchTest
	return v.MatchCategory, v.MatchBrand, true
}

func (v *IRView) SetMatchCandidates(cands []ir.MatchCandidate, err error) {
	v.MatchLoading = false
	v.MatchCursor = 0
	v.MatchActionCursor = 0
	if err != nil {
		v.MatchErr = err.Error()
		v.MatchCandidates = nil
		return
	}
	v.MatchErr = ""
	v.MatchCandidates = cands
}

func (v *IRView) MoveMatchAction(delta int) {
	v.MatchActionCursor = (v.MatchActionCursor + delta + 3) % 3
}

func (v IRView) CurrentMatchCandidate() (ir.MatchCandidate, bool) {
	if v.MatchCursor < 0 || v.MatchCursor >= len(v.MatchCandidates) {
		return ir.MatchCandidate{}, false
	}
	return v.MatchCandidates[v.MatchCursor], true
}

func (v *IRView) StepMatchCandidate(delta int) (ir.MatchCandidate, bool) {
	n := len(v.MatchCandidates)
	if n == 0 {
		return ir.MatchCandidate{}, false
	}
	v.MatchCursor = (v.MatchCursor + delta + n) % n
	return v.MatchCandidates[v.MatchCursor], true
}

func (v *IRView) AcceptMatchedProfile(p ir.Profile, err error) bool {
	v.MatchSaving = false
	if err != nil {
		v.MatchErr = err.Error()
		return false
	}
	v.Store.AddProfile(p)
	if v.saveStore() == nil {
		v.Note = "Saved " + p.Name
		v.NoteIsErr = false
	}
	v.RemoteScroll = 0
	v.RemoteMode = remoteModeUse
	v.BrowseMode = browseModeList
	v.Tab = IRTabRemote
	return true
}

func (v *IRView) MoveUnivButton(delta int) {
	if len(v.UnivButtons) == 0 {
		v.UnivCursor = 0
		return
	}
	v.UnivCursor = (v.UnivCursor + delta + len(v.UnivButtons)) % len(v.UnivButtons)
}

func (v IRView) SelectedUnivButton() (ir.UniversalButton, bool) {
	if v.UnivCursor < 0 || v.UnivCursor >= len(v.UnivButtons) {
		return ir.UniversalButton{}, false
	}
	return v.UnivButtons[v.UnivCursor], true
}

func (v IRView) UnivButtonForKey(key string) (ir.UniversalButton, int, bool) {
	key = ir.NormalizeBindKey(key)
	for i, b := range v.UnivButtons {
		if b.Key == key {
			return b, i, true
		}
	}
	return ir.UniversalButton{}, -1, false
}

func (v *IRView) StartUniversalBlast(target string) (context.Context, int, chan ir.UniversalProgress, bool) {
	if v.UnivBlasting {
		return nil, 0, nil, false
	}
	if v.HW.TXPath == "" {
		v.UnivBlastErr = "No IR transmitter: " + v.HW.Err
		return nil, 0, nil, false
	}
	v.UnivSeq++
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan ir.UniversalProgress, 1)
	v.UnivBlasting = true
	v.UnivBlastTarget = target
	v.UnivBlastCancel = cancel
	v.UnivProgressCh = ch
	v.UnivProgress = ir.UniversalProgress{Target: target}
	v.UnivBlastErr = ""
	v.Note = ""
	v.NoteIsErr = false
	return ctx, v.UnivSeq, ch, true
}

func (v *IRView) StopUniversalBlast() {
	if !v.UnivBlasting {
		return
	}
	if v.UnivBlastCancel != nil {
		v.UnivBlastCancel()
		v.UnivBlastCancel = nil
	}
	v.UnivBlasting = false
	if v.UnivProgress.Current.Brand != "" {
		v.UnivLastBrand = v.UnivProgress.Current.Brand
	}
	v.Note = fmt.Sprintf("Stopped (%d sent)", v.UnivProgress.UniqueSent)
	v.NoteIsErr = false
}

func (v *IRView) ApplyUniversalProgress(seq int, prog ir.UniversalProgress) {
	if !v.UnivBlasting || seq != v.UnivSeq {
		return
	}
	v.UnivProgress = prog
	if prog.Current.Brand != "" {
		v.UnivLastBrand = prog.Current.Brand
	}
}

func (v *IRView) ApplyUniversalDone(seq int, target string, uniqueSent int, lastBrand string, err error) {
	if seq != v.UnivSeq {
		return
	}
	wasBlasting := v.UnivBlasting
	v.UnivBlasting = false
	v.UnivBlastCancel = nil
	if lastBrand != "" {
		v.UnivLastBrand = lastBrand
	}
	if errors.Is(err, context.Canceled) {
		if wasBlasting {
			v.Note = fmt.Sprintf("Stopped (%d sent)", uniqueSent)
			v.NoteIsErr = false
		}
		return
	}
	if err != nil {
		v.UnivBlastErr = err.Error()
		return
	}
	v.Note = fmt.Sprintf("Done: %d %s codes", uniqueSent, v.univTargetLabel(target))
	v.NoteIsErr = false
}

func (v IRView) univTargetLabel(target string) string {
	for _, b := range v.UnivButtons {
		if b.Target == target {
			return b.Label
		}
	}
	return target
}

func (v *IRView) OpenRemoteList() {
	v.RemoteMode = remoteModeList
	v.RemoteListCursor = clampIndex(v.Store.Active, len(v.Store.Profiles))
	v.Note = ""
	v.NoteIsErr = false
}

func (v *IRView) BeginNewRemote(fromLearn bool) {
	v.NewRemoteName = ""
	v.NewRemoteErr = ""
	v.Note = ""
	v.NoteIsErr = false
	if fromLearn {
		v.NewRemoteInput = true
	} else {
		v.RemoteMode = remoteModeNew
	}
}

func (v *IRView) SelectRemoteFromList(idx int) bool {
	if !v.Store.SelectProfile(idx) {
		return false
	}
	v.RemoteScroll = 0
	v.RemoteMode = remoteModeUse
	if v.saveStore() == nil {
		if p := v.Store.ActiveProfile(); p != nil {
			v.Note = "Selected " + p.Name
			v.NoteIsErr = false
		}
	}
	return true
}

func (v *IRView) DeleteRemoteFromList(idx int) bool {
	if idx < 0 || idx >= len(v.Store.Profiles) {
		return false
	}
	name := v.Store.Profiles[idx].Name
	if !v.Store.DeleteProfile(idx) {
		return false
	}
	v.RemoteScroll = 0
	if v.RemoteListCursor > len(v.Store.Profiles) {
		v.RemoteListCursor = len(v.Store.Profiles)
	}
	if len(v.Store.Profiles) == 0 {
		v.RemoteMode = remoteModeUse
	}
	if v.saveStore() == nil {
		v.Note = "Deleted " + name
		v.NoteIsErr = false
	}
	return true
}

func (v *IRView) HandleTextKey(msg tea.KeyMsg) (handled bool) {
	if !v.IsTextInput() {
		return false
	}
	switch {
	case v.Tab == IRTabBrowse && v.BrowseMode == browseModeMatchBrand && v.MatchBrandSearching:
		switch msg.Type {
		case tea.KeyEnter:
			v.MatchBrandSearching = false
			return true
		case tea.KeyBackspace, tea.KeyDelete:
			v.MatchBrandQuery = dropLastRune(v.MatchBrandQuery)
			v.refreshMatchBrands()
			v.MatchBrandCursor = 0
			return true
		case tea.KeyCtrlU:
			v.MatchBrandQuery = ""
			v.refreshMatchBrands()
			v.MatchBrandCursor = 0
			return true
		case tea.KeySpace:
			v.MatchBrandQuery += " "
			v.refreshMatchBrands()
			v.MatchBrandCursor = 0
			return true
		case tea.KeyRunes:
			v.MatchBrandQuery += string(msg.Runes)
			v.refreshMatchBrands()
			v.MatchBrandCursor = 0
			return true
		}
		return true

	case v.Tab == IRTabBrowse && v.BrowseSearching:
		switch msg.Type {
		case tea.KeyEnter:
			v.BrowseSearching = false
			return true
		case tea.KeyBackspace, tea.KeyDelete:
			v.BrowseQuery = dropLastRune(v.BrowseQuery)
			v.refilter()
			v.BrowseCursor = 0
			return true
		case tea.KeyCtrlU:
			v.BrowseQuery = ""
			v.refilter()
			v.BrowseCursor = 0
			return true
		case tea.KeySpace:
			v.BrowseQuery += " "
			v.refilter()
			v.BrowseCursor = 0
			return true
		case tea.KeyRunes:
			v.BrowseQuery += string(msg.Runes)
			v.refilter()
			v.BrowseCursor = 0
			return true
		}
		return true

	case v.Tab == IRTabLearn && v.AwaitBindLabel:
		switch msg.Type {
		case tea.KeyEnter:
			v.ConfirmBindLabel()
			return true
		case tea.KeyBackspace, tea.KeyDelete:
			v.PendingBindName = dropLastRune(v.PendingBindName)
			v.LearnErr = ""
			return true
		case tea.KeyCtrlU:
			v.PendingBindName = ""
			v.LearnErr = ""
			return true
		case tea.KeySpace:
			v.PendingBindName += " "
			v.LearnErr = ""
			return true
		case tea.KeyRunes:
			v.PendingBindName += string(msg.Runes)
			v.LearnErr = ""
			return true
		}
		return true

	default: // remoteModeNew or NewRemoteInput
		switch msg.Type {
		case tea.KeyEnter:
			idx, err := v.Store.CreateProfile(v.NewRemoteName, "custom")
			if err != nil {
				v.NewRemoteErr = err.Error()
				return true
			}
			_ = idx
			created := strings.TrimSpace(v.NewRemoteName)
			v.NewRemoteName = ""
			v.NewRemoteErr = ""
			v.RemoteScroll = 0
			v.RemoteMode = remoteModeUse
			v.NewRemoteInput = false
			if v.saveStore() == nil {
				v.Note = "Created " + created
				v.NoteIsErr = false
			}
			return true
		case tea.KeyBackspace, tea.KeyDelete:
			v.NewRemoteName = dropLastRune(v.NewRemoteName)
			v.NewRemoteErr = ""
			return true
		case tea.KeyCtrlU:
			v.NewRemoteName = ""
			v.NewRemoteErr = ""
			return true
		case tea.KeySpace:
			v.NewRemoteName += " "
			v.NewRemoteErr = ""
			return true
		case tea.KeyRunes:
			v.NewRemoteName += string(msg.Runes)
			v.NewRemoteErr = ""
			return true
		}
		return true
	}
}

func (v *IRView) MoveLearnMenu(delta int) {
	v.LearnCursor = (v.LearnCursor + delta + int(learnOptCount)) % int(learnOptCount)
	v.LearnErr = ""
	v.Note = ""
	v.NoteIsErr = false
}

func (v *IRView) MoveCapturedMenu(delta int) {
	v.CapturedCursor = (v.CapturedCursor + delta + int(learnCapCount)) % int(learnCapCount)
	v.LearnErr = ""
}

func (v *IRView) BeginBindKey() bool {
	if v.Learned == nil {
		return false
	}
	v.AwaitBinding = true
	v.AwaitKeyPress = true
	v.AwaitBindLabel = false
	v.PendingBindKey = ""
	v.PendingBindName = ""
	v.LearnErr = ""
	v.Note = ""
	v.NoteIsErr = false
	return true
}

func (v *IRView) StartBindLabel(key string) bool {
	key = ir.NormalizeBindKey(key)
	if v.Learned == nil || !ir.IsBindableKey(key) {
		return false
	}
	v.PendingBindKey = key
	v.PendingBindName = ""
	v.AwaitKeyPress = false
	v.AwaitBindLabel = true
	v.LearnErr = ""
	return true
}

func (v *IRView) ConfirmBindLabel() bool {
	if v.Learned == nil || !ir.IsBindableKey(v.PendingBindKey) {
		return false
	}
	label := strings.TrimSpace(v.PendingBindName)
	if label == "" {
		label = "Key " + ir.KeyLabel(v.PendingBindKey)
	}
	return v.bindLearnedNamed(v.PendingBindKey, label)
}

func (v *IRView) DiscardLearned() {
	v.Learned = nil
	v.AwaitBinding = false
	v.AwaitKeyPress = false
	v.AwaitBindLabel = false
	v.PendingBindKey = ""
	v.PendingBindName = ""
	v.CapturedCursor = 0
	v.LearnErr = ""
	v.Note = ""
	v.NoteIsErr = false
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
	v.Note = ""
	v.NoteIsErr = false
	v.Learned = nil
	v.AwaitBinding = false
	v.AwaitKeyPress = false
	v.CapturedCursor = 0
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
		v.AwaitKeyPress = false
		return
	}
	v.Learned = &sig
	v.AwaitBinding = true
	v.AwaitKeyPress = false
	v.CapturedCursor = 0
	v.LearnErr = ""
	v.Note = ""
	v.NoteIsErr = false
}

func (v *IRView) BindLearned(key string) bool {
	return v.bindLearnedNamed(key, "Learned")
}

func (v *IRView) bindLearnedNamed(key, label string) bool {
	key = ir.NormalizeBindKey(key)
	if v.Learned == nil || !ir.IsBindableKey(key) {
		return false
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Learned"
	}
	sig := v.Learned.Normalized()
	sig.Name = label
	sig.Saved = time.Now()
	v.Store.AddBindingToActive(ir.Binding{Key: key, Name: label, Signal: sig})
	v.AwaitBinding = false
	v.AwaitKeyPress = false
	v.AwaitBindLabel = false
	v.PendingBindKey = ""
	v.PendingBindName = ""
	v.CapturedCursor = 0
	v.Learned = nil
	v.LearnErr = ""
	if v.saveStore() == nil {
		v.Note = "Bound " + ir.KeyLabel(key) + " (" + label + ")"
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
	key = ir.NormalizeBindKey(key)
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
	case IRTabRemote:
		if v.RemoteMode == remoteModeNew {
			v.NewRemoteName = ""
			v.NewRemoteErr = ""
			if len(v.Store.Profiles) > 0 {
				v.RemoteMode = remoteModeList
			} else {
				v.RemoteMode = remoteModeUse
			}
			return true
		}
		if v.RemoteMode == remoteModeList {
			v.RemoteMode = remoteModeUse
			return true
		}
		return false
	case IRTabLearn:
		if v.NewRemoteInput {
			v.NewRemoteInput = false
			v.NewRemoteName = ""
			v.NewRemoteErr = ""
			return true
		}
		if v.Capturing {
			v.StopCapture()
			return true
		}
		if v.AwaitBindLabel {
			v.AwaitBindLabel = false
			v.PendingBindName = ""
			v.AwaitKeyPress = true
			return true
		}
		if v.AwaitKeyPress {
			v.AwaitKeyPress = false
			v.LearnErr = ""
			return true
		}
		if v.AwaitBinding {
			v.DiscardLearned()
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
		switch v.BrowseMode {
		case browseModeMatchTest:
			v.MatchLoading = false
			v.MatchSaving = false
			v.MatchErr = ""
			v.Note = ""
			v.NoteIsErr = false
			v.BrowseMode = browseModeMatchBrand
			return true
		case browseModeMatchBrand:
			if v.MatchBrandSearching {
				v.MatchBrandSearching = false
				v.MatchBrandQuery = ""
				v.refreshMatchBrands()
				v.MatchBrandCursor = 0
				return true
			}
			if v.MatchBrandQuery != "" {
				v.MatchBrandQuery = ""
				v.refreshMatchBrands()
				v.MatchBrandCursor = 0
				return true
			}
			v.BrowseMode = browseModeMatchType
			return true
		case browseModeMatchType:
			v.BrowseMode = browseModeList
			return true
		case browseModeUnivRemote:
			if v.UnivBlasting {
				v.StopUniversalBlast()
				return true
			}
			v.UnivBlastErr = ""
			v.Note = ""
			v.NoteIsErr = false
			v.BrowseMode = browseModeUnivType
			return true
		case browseModeUnivType:
			v.BrowseMode = browseModeList
			return true
		}
		if v.BrowseSearching {
			v.BrowseSearching = false
			v.BrowseQuery = ""
			v.refilter()
			v.BrowseCursor = 0
			return true
		}
		if v.BrowseQuery != "" {
			v.BrowseQuery = ""
			v.refilter()
			v.BrowseCursor = 0
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

func (v IRView) renderNewRemote(w, h int, pal theme.Palette) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)

	lines := []string{
		" " + accent.Render("New Remote"),
		" " + sec.Render("Enter remote name:"),
	}
	fieldW := max(6, w-5)
	lines = append(lines, " > "+txt.Render(tailWindow(v.NewRemoteName, fieldW-1)+"_"))
	if v.NewRemoteErr != "" {
		lines = append(lines, " "+danger.Render(Truncate(v.NewRemoteErr, max(6, w-2))))
	}
	if h >= 5 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+muted.Render(Truncate("⏎:create  Esc:cancel", max(6, w-2))))
	}
	return SplitLinesPadded(lines, w, h)
}

func (v IRView) renderRemote(w, h int, pal theme.Palette, focused bool) []string {
	if v.RemoteMode == remoteModeNew {
		return v.renderNewRemote(w, h, pal)
	}

	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	if v.RemoteMode == remoteModeList {
		lines := []string{
			" " + accent.Render(fmt.Sprintf("IR Remotes (%d)", len(v.Store.Profiles))),
		}
		type remItem struct {
			name   string
			val    string
			active bool
			isNew  bool
		}
		items := make([]remItem, 0, len(v.Store.Profiles)+1)
		for i, prof := range v.Store.Profiles {
			items = append(items, remItem{
				name:   prof.Name,
				val:    fmt.Sprintf("%dk", len(prof.Bindings)),
				active: i == v.Store.Active,
			})
		}
		items = append(items, remItem{name: "+ New remote", val: "create", isNew: true})

		listH := max(1, h-2)
		cursor := clampIndex(v.RemoteListCursor, len(items))
		start, end := ScrollWindow(len(items), cursor, listH)
		for i := start; i < end; i++ {
			it := items[i]
			selected := i == cursor
			mark := " "
			if it.active {
				mark = "*"
			}
			nameW := max(8, w-len(it.val)-5)
			if selected && focused {
				row := fmt.Sprintf("▸%s%s %s", mark, FitLine(it.name, nameW), it.val)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := " "
				if selected {
					pfx = accent.Render("▸")
				}
				mSty := muted
				if it.active {
					mSty = ok
				}
				vSty := muted
				if it.isNew {
					vSty = ok
				}
				lines = append(lines, pfx+mSty.Render(mark)+txt.Render(FitLine(it.name, nameW))+" "+vSty.Render(it.val))
			}
		}
		if h >= 3 {
			for len(lines) < h-1 {
				lines = append(lines, "")
			}
			if v.Note != "" {
				st := ok
				if v.NoteIsErr {
					st = danger
				}
				lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
			} else {
				lines = append(lines, " "+muted.Render(Truncate("↑↓:sel ⏎:use l:learn x:del", max(6, w-2))))
			}
		}
		return SplitLinesPadded(lines, w, h)
	}

	if len(v.Store.Profiles) == 0 {
		lines := []string{
			" " + accent.Render("IR Remotes"),
			" " + muted.Render(Truncate(irHardwareLine(v), max(6, w-2))),
		}
		type emptyItem struct {
			name string
			val  string
			ok   bool
		}
		items := []emptyItem{
			{name: "+ New remote", val: "create", ok: true},
			{name: "Learn signal", val: "capture"},
			{name: "Browse library", val: "FX/Mi"},
			{name: "Match by brand", val: "guided", ok: true},
			{name: "Universal remote", val: "blast"},
		}
		cursor := clampIndex(v.EmptyMenuCursor, len(items))
		listH := max(1, h-3)
		if h < 4 {
			listH = max(1, h-2)
		}
		start, end := ScrollWindow(len(items), cursor, listH)
		for i := start; i < end; i++ {
			it := items[i]
			selected := i == cursor
			nameW := max(8, w-len(it.val)-5)
			if selected && focused {
				row := fmt.Sprintf(" ▸ %s %s", FitLine(it.name, nameW), it.val)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "   "
				if selected {
					pfx = accent.Render(" ▸ ")
				}
				vSty := muted
				if it.ok {
					vSty = ok
				}
				lines = append(lines, fmt.Sprintf("%s%s %s", pfx, txt.Render(FitLine(it.name, nameW)), vSty.Render(it.val)))
			}
		}
		if h >= 4 {
			for len(lines) < h-1 {
				lines = append(lines, "")
			}
			if v.Note != "" {
				st := ok
				if v.NoteIsErr {
					st = danger
				}
				lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
			} else {
				lines = append(lines, " "+muted.Render(Truncate("↑↓:sel  ⏎:open  Esc:tabs", max(6, w-2))))
			}
		}
		return SplitLinesPadded(lines, w, h)
	}

	p := v.Store.ActiveProfile()
	counter := fmt.Sprintf("%d/%d", v.Store.Active+1, len(v.Store.Profiles))
	titleW := max(6, w-len(counter)-3)
	title := p.Name
	if p.Source != "" && len([]rune(title))+len([]rune(p.Source))+3 <= titleW {
		title += " · " + p.Source
	}
	lines := []string{
		" " + accent.Render(FitLine(Truncate(title, titleW), titleW)) + " " + muted.Render(counter),
	}
	if len(p.Bindings) == 0 {
		lines = append(lines, " "+muted.Render("No keys bound yet"))
		lines = append(lines, " "+txt.Render(Truncate("Press ⏎ to learn keys", max(6, w-2))))
	} else {
		visible := max(1, h-2)
		maxScroll := max(0, len(p.Bindings)-visible)
		sc := min(v.RemoteScroll, maxScroll)
		end := min(len(p.Bindings), sc+visible)
		for _, b := range p.Bindings[sc:end] {
			key := ir.KeyLabel(b.Key)
			line := " " + sec.Render(FitLine(key, 6)) + txt.Render(Truncate(b.Name, max(6, w-9)))
			lines = append(lines, line)
		}
	}
	if h >= 3 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		if v.Note != "" {
			st := ok
			if v.NoteIsErr {
				st = danger
			}
			lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(Truncate("key:send ←→:rem Tab:manage", max(6, w-2))))
		}
	}
	return SplitLinesPadded(lines, w, h)
}

func (v IRView) renderLearn(w, h int, pal theme.Palette, focused bool) []string {
	if v.NewRemoteInput {
		return v.renderNewRemote(w, h, pal)
	}

	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	profName := "My remote"
	keyCount := 0
	if p := v.Store.ActiveProfile(); p != nil {
		if strings.TrimSpace(p.Name) != "" {
			profName = p.Name
		}
		keyCount = len(p.Bindings)
	}

	var lines []string
	var hint string

	switch {
	case v.Capturing:
		lines = []string{
			" " + accent.Render("IR Learn"),
			" " + muted.Render(Truncate(irHardwareLine(v), max(6, w-2))),
			" " + warn.Render("Listening…"),
			" " + muted.Render(Truncate(fmt.Sprintf("%ds / %ds", int(v.CaptureElapsed.Seconds()), 5), max(6, w-2))),
			" " + txt.Render(Truncate("Point remote at receiver", max(6, w-2))),
		}
		hint = "Esc:cancel"

	case v.AwaitBindLabel && v.Learned != nil:
		defLbl := "Key " + ir.KeyLabel(v.PendingBindKey)
		lines = []string{
			" " + accent.Render(Truncate("Bind ["+ir.KeyLabel(v.PendingBindKey)+"] → "+profName, max(6, w-2))),
			" " + sec.Render(Truncate("Label (Enter="+defLbl+"):", max(6, w-2))),
		}
		fieldW := max(6, w-5)
		lines = append(lines, " > "+txt.Render(tailWindow(v.PendingBindName, fieldW-1)+"_"))
		hint = "⏎:save  Esc:back"

	case v.AwaitKeyPress && v.Learned != nil:
		sig := v.Learned
		lines = []string{
			" " + accent.Render(Truncate("Bind → "+profName, max(6, w-2))),
			" " + muted.Render(Truncate(fmt.Sprintf("%d timings · %d Hz", len(sig.Timings), sig.Frequency), max(6, w-2))),
		}
		if h >= 7 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+warn.Render(Truncate("Press any key to bind:", max(6, w-2))))
		lines = append(lines, " "+txt.Render(Truncate("a-z, 0-9, F1-F8, Enter, Bksp", max(6, w-2))))
		hint = "Esc:back"

	case v.AwaitBinding && v.Learned != nil:
		sig := v.Learned
		lines = []string{
			" " + ok.Render(Truncate(fmt.Sprintf("Captured! %dt · %dHz", len(sig.Timings), sig.Frequency), max(6, w-2))),
		}
		type capItem struct {
			name string
			val  string
			ok   bool
		}
		items := []capItem{
			{name: "Bind to key", val: Truncate(profName, 8), ok: true},
			{name: "Test signal", val: "send"},
			{name: "Listen again", val: "5s"},
			{name: "Discard", val: "cancel"},
		}
		showBottom := (h >= 6 || v.Note != "" || v.LearnErr != "") && h >= 4
		listH := h - 1
		if showBottom {
			listH = h - 2
		}
		cursor := clampIndex(v.CapturedCursor, len(items))
		start, end := ScrollWindow(len(items), cursor, max(1, listH))
		for i := start; i < end; i++ {
			it := items[i]
			selected := i == cursor
			nameW := max(8, w-len([]rune(it.val))-5)
			if selected && focused {
				row := fmt.Sprintf(" ▸ %s %s", FitLine(it.name, nameW), it.val)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "   "
				if selected {
					pfx = accent.Render(" ▸ ")
				}
				valStyle := muted
				if it.ok {
					valStyle = ok
				}
				lines = append(lines, fmt.Sprintf("%s%s %s", pfx, txt.Render(FitLine(it.name, nameW)), valStyle.Render(it.val)))
			}
		}
		hint = "↑↓:sel  ⏎:run  Esc:discard"

	default:
		lines = []string{
			" " + accent.Render("IR Learn"),
			" " + muted.Render(Truncate(irHardwareLine(v), max(6, w-2))),
		}
		profVal := fmt.Sprintf("%dk", keyCount)
		if len(v.Store.Profiles) > 1 {
			profVal += "◂▸"
		}
		type learnItem struct {
			name string
			val  string
			ok   bool
		}
		items := []learnItem{
			{name: "Listen for IR", val: "5s", ok: v.HW.RXPath != ""},
			{name: "Remote: " + profName, val: profVal},
			{name: "+ New remote", val: "create", ok: true},
		}
		cursor := clampIndex(v.LearnCursor, len(items))
		for i, it := range items {
			selected := i == cursor
			nameW := max(8, w-len([]rune(it.val))-5)
			if selected && focused {
				row := fmt.Sprintf(" ▸ %s %s", FitLine(it.name, nameW), it.val)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "   "
				if selected {
					pfx = accent.Render(" ▸ ")
				}
				valStyle := muted
				if it.ok {
					valStyle = ok
				}
				lines = append(lines, fmt.Sprintf("%s%s %s", pfx, txt.Render(FitLine(it.name, nameW)), valStyle.Render(it.val)))
			}
		}
		if v.HW.RXPath == "" && v.HW.Err != "" {
			lines = append(lines, " "+danger.Render(Truncate(v.HW.Err, max(6, w-2))))
		}
		hint = "↑↓:sel  ⏎:run  Esc:tabs"
	}

	if h >= 4 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		switch {
		case v.LearnErr != "":
			lines = append(lines, " "+danger.Render(Truncate(v.LearnErr, max(6, w-2))))
		case v.Note != "":
			st := ok
			if v.NoteIsErr {
				st = danger
			}
			lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
		default:
			lines = append(lines, " "+muted.Render(Truncate(hint, max(6, w-2))))
		}
		// Keep within exact height if error/note pushed lines over h.
		if len(lines) > h {
			lines = append(lines[:h-1], lines[len(lines)-1])
		}
	}
	_ = sec
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

	if v.IndexLoading {
		title := "IR Browse"
		switch v.BrowseMode {
		case browseModeMatchType, browseModeMatchBrand, browseModeMatchTest:
			title = "Match Remote"
		case browseModeUnivType, browseModeUnivRemote:
			title = "Universal IR"
		}
		lines := []string{
			" " + accent.Render(title),
			" " + warn.Render("Indexing databases…"),
		}
		return SplitLinesPadded(lines, w, h)
	}
	if v.IndexErr != "" {
		lines := []string{
			" " + accent.Render("IR Browse"),
			" " + danger.Render(Truncate(v.IndexErr, max(6, w-2))),
			" " + muted.Render(Truncate("Set CZHAX_IR_FLIPPER / CZHAX_IR_MI", max(6, w-2))),
		}
		return SplitLinesPadded(lines, w, h)
	}
	if len(v.Index) == 0 && !v.RemoteLoading && v.RemoteErr == "" {
		lines := []string{
			" " + accent.Render("IR Browse"),
			" " + muted.Render("No remotes found"),
			" " + muted.Render("Set CZHAX_IR_FLIPPER or CZHAX_IR_MI"),
			" " + muted.Render("or use Learn to copy a remote"),
		}
		return SplitLinesPadded(lines, w, h)
	}

	switch v.BrowseMode {
	case browseModeMatchType:
		return v.renderTypePicker(w, h, pal, focused, "Match: Select Type", "↑↓:sel  ⏎:brands  Esc:back")
	case browseModeMatchBrand:
		return v.renderMatchBrand(w, h, pal, focused)
	case browseModeMatchTest:
		return v.renderMatchTest(w, h, pal, focused)
	case browseModeUnivType:
		return v.renderTypePicker(w, h, pal, focused, "Universal: Type", "↑↓:sel  ⏎:open  Esc:back")
	case browseModeUnivRemote:
		return v.renderUnivRemote(w, h, pal, focused)
	}

	countStr := fmt.Sprintf("%d", len(v.Filtered))
	if len(v.Filtered) != len(v.Index) {
		countStr = fmt.Sprintf("%d/%d", len(v.Filtered), len(v.Index))
	}
	titleW := max(6, w-len(countStr)-3)
	lines := []string{
		" " + accent.Render(FitLine("IR Browse", titleW)) + " " + muted.Render(countStr),
	}
	if v.RemoteLoading {
		lines = append(lines, " "+warn.Render("Loading remote…"))
		return SplitLinesPadded(lines, w, h)
	}
	if v.RemoteErr != "" {
		lines = append(lines, " "+danger.Render(Truncate(v.RemoteErr, max(6, w-2))))
		return SplitLinesPadded(lines, w, h)
	}

	switch {
	case v.BrowseSearching:
		fieldW := max(6, w-8)
		lines = append(lines, " "+sec.Render("Find: ")+txt.Render(tailWindow(v.BrowseQuery, fieldW-1)+"_"))
	case v.BrowseQuery != "":
		lines = append(lines, " "+sec.Render("Src ")+txt.Render(FitLine(v.ProviderLabel(), 8))+sec.Render("Find ")+warn.Render(Truncate(v.BrowseQuery, max(4, w-19))))
	default:
		lines = append(lines, " "+sec.Render("Src ")+txt.Render(FitLine(v.ProviderLabel(), 9))+sec.Render("Type ")+txt.Render(Truncate(v.CategoryLabel(), max(4, w-19))))
	}
	showModeStrip := !v.BrowseSearching && v.BrowseQuery == "" && h >= 6
	if showModeStrip {
		lines = append(lines, " "+sec.Render("m")+muted.Render(":Match ")+sec.Render("u")+muted.Render(":Univ ")+sec.Render("/")+muted.Render(":Find"))
	}
	if len(v.Filtered) == 0 {
		lines = append(lines, " "+muted.Render("No remotes match filter"))
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		if v.BrowseSearching {
			lines = append(lines, " "+muted.Render(Truncate("⏎:done  Esc:clear", max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(Truncate("←→:type p:src /:find Esc:clear", max(6, w-2))))
		}
		return SplitLinesPadded(lines, w, h)
	}
	reserved := 3
	if showModeStrip {
		reserved = 4
	}
	visible := max(1, h-reserved)
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
			lines = append(lines, " "+muted.Render(mark)+" "+txt.Render(Truncate(e.Display(), max(6, w-4))))
		}
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	if v.BrowseSearching {
		lines = append(lines, " "+muted.Render(Truncate("⏎:done  Esc:clear", max(6, w-2))))
	} else {
		lines = append(lines, " "+muted.Render(Truncate("↑↓:sel m:match u:univ ⏎:open", max(6, w-2))))
	}
	return SplitLinesPadded(lines, w, h)
}

func (v IRView) renderTypePicker(w, h int, pal theme.Palette, focused bool, title, hint string) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	countStr := fmt.Sprintf("%d", len(v.CategoryStats))
	titleW := max(6, w-len(countStr)-3)
	lines := []string{
		" " + accent.Render(FitLine(Truncate(title, titleW), titleW)) + " " + muted.Render(countStr),
	}
	if len(v.CategoryStats) == 0 {
		lines = append(lines, " "+muted.Render("No device types available"))
	} else {
		listH := max(1, h-2)
		cursor := clampIndex(v.TypeCursor, len(v.CategoryStats))
		start, end := ScrollWindow(len(v.CategoryStats), cursor, listH)
		for i := start; i < end; i++ {
			cat := v.CategoryStats[i]
			val := fmt.Sprintf("%d", cat.Count)
			nameW := max(8, w-len(val)-5)
			if i == cursor && focused {
				row := fmt.Sprintf(" ▸ %s %s", FitLine(cat.Name, nameW), val)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "   "
				if i == cursor {
					pfx = accent.Render(" ▸ ")
				}
				lines = append(lines, pfx+txt.Render(FitLine(cat.Name, nameW))+" "+muted.Render(val))
			}
		}
	}
	if h >= 3 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+muted.Render(Truncate(hint, max(6, w-2))))
	}
	return SplitLinesPadded(lines, w, h)
}

func (v IRView) renderMatchBrand(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	countStr := fmt.Sprintf("%d", len(v.MatchBrands))
	titleW := max(6, w-len(countStr)-3)
	lines := []string{
		" " + accent.Render(FitLine(Truncate("Match: "+v.MatchCategory, titleW), titleW)) + " " + muted.Render(countStr),
	}
	switch {
	case v.MatchBrandSearching:
		fieldW := max(6, w-8)
		lines = append(lines, " "+sec.Render("Find: ")+txt.Render(tailWindow(v.MatchBrandQuery, fieldW-1)+"_"))
	case v.MatchBrandQuery != "":
		lines = append(lines, " "+sec.Render("Find: ")+warn.Render(Truncate(v.MatchBrandQuery, max(6, w-8))))
	default:
		lines = append(lines, " "+muted.Render(Truncate("/:find  [/]:A-Z  ⏎:test", max(6, w-2))))
	}

	if len(v.MatchBrands) == 0 {
		lines = append(lines, " "+muted.Render("No matching brands"))
	} else {
		listH := max(1, h-3)
		cursor := clampIndex(v.MatchBrandCursor, len(v.MatchBrands))
		start, end := ScrollWindow(len(v.MatchBrands), cursor, listH)
		for i := start; i < end; i++ {
			b := v.MatchBrands[i]
			val := fmt.Sprintf("%d", b.Count)
			nameW := max(8, w-len(val)-5)
			if i == cursor && focused {
				row := fmt.Sprintf(" ▸ %s %s", FitLine(b.Name, nameW), val)
				lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
			} else {
				pfx := "   "
				if i == cursor {
					pfx = accent.Render(" ▸ ")
				}
				lines = append(lines, pfx+txt.Render(FitLine(b.Name, nameW))+" "+muted.Render(val))
			}
		}
	}
	if h >= 4 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		hint := "↑↓:sel /:find [/]:A-Z ⏎:test"
		if v.MatchBrandSearching {
			hint = "⏎:done  Esc:clear"
		}
		if v.MatchErr != "" {
			lines = append(lines, " "+danger.Render(Truncate(v.MatchErr, max(6, w-2))))
		} else {
			lines = append(lines, " "+muted.Render(Truncate(hint, max(6, w-2))))
		}
	}
	return SplitLinesPadded(lines, w, h)
}

func (v IRView) renderMatchTest(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	header := v.MatchCategory + " · " + v.MatchBrand
	if v.MatchLoading {
		lines := []string{
			" " + accent.Render(Truncate("Match: "+header, max(6, w-2))),
			" " + warn.Render("Loading Power codes…"),
		}
		return SplitLinesPadded(lines, w, h)
	}
	if v.MatchSaving {
		lines := []string{
			" " + accent.Render(Truncate("Match: "+header, max(6, w-2))),
			" " + warn.Render("Saving full remote…"),
		}
		return SplitLinesPadded(lines, w, h)
	}
	if len(v.MatchCandidates) == 0 {
		errTxt := v.MatchErr
		if errTxt == "" {
			errTxt = "No Power candidates found"
		}
		lines := []string{
			" " + accent.Render(Truncate("Match: "+header, max(6, w-2))),
			" " + danger.Render(Truncate(errTxt, max(6, w-2))),
		}
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+muted.Render(Truncate("Esc:back", max(6, w-2))))
		return SplitLinesPadded(lines, w, h)
	}

	cand := v.MatchCandidates[v.MatchCursor]
	counter := fmt.Sprintf("%d/%d", v.MatchCursor+1, len(v.MatchCandidates))
	titleW := max(6, w-len(counter)-3)
	proto := cand.Signal.Protocol
	if proto == "" {
		proto = "raw"
	}
	lines := []string{
		" " + accent.Render(FitLine(Truncate(header, titleW), titleW)) + " " + muted.Render(counter),
		" " + sec.Render("Model ") + txt.Render(Truncate(cand.Entry.Model+" ("+cand.Entry.Provider+")", max(6, w-8))),
	}
	if v.Note != "" {
		st := ok
		if v.NoteIsErr {
			st = danger
		}
		lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
	} else if v.MatchErr != "" {
		lines = append(lines, " "+danger.Render(Truncate(v.MatchErr, max(6, w-2))))
	} else {
		lines = append(lines, " "+muted.Render(Truncate(fmt.Sprintf("%s · %dt · %dHz (%dk)", proto, len(cand.Signal.Timings), cand.Signal.Frequency, cand.SignalCount), max(6, w-2))))
	}

	type actItem struct {
		name string
		val  string
		ok   bool
	}
	nextNum := (v.MatchCursor+1)%len(v.MatchCandidates) + 1
	items := []actItem{
		{name: "Test Power", val: fmt.Sprintf("#%d", v.MatchCursor+1), ok: true},
		{name: "Next code", val: fmt.Sprintf("#%d", nextNum)},
		{name: "Works! Save remote", val: "save", ok: true},
	}
	listH := max(1, h-4)
	cursor := clampIndex(v.MatchActionCursor, len(items))
	start, end := ScrollWindow(len(items), cursor, listH)
	for i := start; i < end; i++ {
		it := items[i]
		nameW := max(8, w-len([]rune(it.val))-5)
		if i == cursor && focused {
			row := fmt.Sprintf(" ▸ %s %s", FitLine(it.name, nameW), it.val)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if i == cursor {
				pfx = accent.Render(" ▸ ")
			}
			vSty := muted
			if it.ok {
				vSty = ok
			}
			lines = append(lines, fmt.Sprintf("%s%s %s", pfx, txt.Render(FitLine(it.name, nameW)), vSty.Render(it.val)))
		}
	}
	if h >= 4 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+muted.Render(Truncate("⏎:run ←→:step p:try y:save", max(6, w-2))))
		if len(lines) > h {
			lines = append(lines[:h-1], lines[len(lines)-1])
		}
	}
	return SplitLinesPadded(lines, w, h)
}

func (v IRView) renderUnivRemote(w, h int, pal theme.Palette, focused bool) []string {
	accent := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	sec := lipgloss.NewStyle().Bold(true).Foreground(pal.Secondary)
	ok := lipgloss.NewStyle().Bold(true).Foreground(pal.Success)
	warn := lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	danger := lipgloss.NewStyle().Bold(true).Foreground(pal.Danger)
	muted := lipgloss.NewStyle().Foreground(pal.Muted)
	txt := lipgloss.NewStyle().Foreground(pal.Text)
	sel := lipgloss.NewStyle().Bold(true).Foreground(pal.SelectFg).Background(pal.SelectBg)

	if v.UnivBlasting {
		badge := "BLAST"
		titleW := max(6, w-len(badge)-3)
		p := v.UnivProgress
		ratio := 0.0
		if p.TotalEntries > 0 {
			ratio = float64(p.EntryIndex) / float64(p.TotalEntries)
		}
		count := fmt.Sprintf("%d/%d #%d", p.EntryIndex, p.TotalEntries, p.UniqueSent)
		barW := max(4, w-len(count)-3)
		curName := p.Current.Display()
		if strings.TrimSpace(curName) == "" {
			curName = "scanning remotes…"
		}
		proto := p.Signal.Protocol
		if proto == "" {
			proto = "raw"
		}
		sigLine := fmt.Sprintf("%s · %dt · %dHz", proto, len(p.Signal.Timings), p.Signal.Frequency)
		if len(p.Signal.Timings) == 0 {
			sigLine = "Preparing IR transmitter…"
		}
		lines := []string{
			" " + accent.Render(FitLine(Truncate("Univ: "+v.UnivCategory, titleW), titleW)) + " " + warn.Render(badge),
			" " + sec.Render("Btn ") + txt.Render(Truncate(v.univTargetLabel(v.UnivBlastTarget), max(6, w-6))),
			" " + accent.Render(MiniBar(ratio, barW, v.ASCII)) + " " + muted.Render(count),
			" " + sec.Render("Try ") + txt.Render(Truncate(curName, max(6, w-6))),
			" " + muted.Render(Truncate(sigLine, max(6, w-2))),
		}
		if h >= 7 {
			lines = append(lines, " "+txt.Render(Truncate("Point IR at device", max(6, w-2))))
		}
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+warn.Render(Truncate("Esc / Space: STOP", max(6, w-2))))
		return SplitLinesPadded(lines, w, h)
	}

	badge := "TEMP"
	titleW := max(6, w-len(badge)-3)
	lines := []string{
		" " + accent.Render(FitLine(Truncate("Univ: "+v.UnivCategory, titleW), titleW)) + " " + muted.Render(badge),
	}
	hasStatus := v.UnivBlastErr != "" || v.Note != ""
	reserved := 2
	if hasStatus {
		reserved = 3
	}
	listH := max(1, h-reserved)
	cursor := clampIndex(v.UnivCursor, len(v.UnivButtons))
	start, end := ScrollWindow(len(v.UnivButtons), cursor, listH)
	for i := start; i < end; i++ {
		b := v.UnivButtons[i]
		keyLbl := ir.KeyLabel(b.Key)
		if i == cursor && focused {
			row := fmt.Sprintf(" ▸ %s %s", FitLine(keyLbl, 5), b.Label)
			lines = append(lines, sel.Width(w).Render(Truncate(row, w)))
		} else {
			pfx := "   "
			if i == cursor {
				pfx = accent.Render(" ▸ ")
			}
			lines = append(lines, pfx+sec.Render(FitLine(keyLbl, 5))+" "+txt.Render(Truncate(b.Label, max(6, w-10))))
		}
	}
	if h >= 3 {
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		switch {
		case v.UnivBlastErr != "":
			lines = append(lines, " "+danger.Render(Truncate(v.UnivBlastErr, max(6, w-2))))
		case v.Note != "":
			st := ok
			if v.NoteIsErr {
				st = danger
			}
			lines = append(lines, " "+st.Render(Truncate(v.Note, max(6, w-2))))
		default:
			lines = append(lines, " "+muted.Render(Truncate("key/⏎:blast ↑↓:sel Esc:back", max(6, w-2))))
		}
		if len(lines) > h {
			lines = append(lines[:h-1], lines[len(lines)-1])
		}
	}
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
