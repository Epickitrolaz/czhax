package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/czhax/czhax/internal/config"
	"github.com/czhax/czhax/internal/ir"
)

func newIRTestModel(t *testing.T, w, h int) AppModel {
	cfg := config.Default()
	m := NewAppModel(cfg, filepath.Join(t.TempDir(), "config.json"), "v0.2.0")
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(AppModel)
	m.IR.HW = ir.Hardware{RXPath: "/dev/null", TXPath: "/dev/null"}
	m.ActiveView = ViewIR
	m.ActiveTabs[ViewIR] = int(IRTabRemote)
	m.IR.Tab = IRTabRemote
	m.Focus = FocusContent
	return m
}

func irTestSignal(t *testing.T) ir.Signal {
	t.Helper()
	sig, err := ir.EncodeProtocol("NEC", 0x08, 0x05)
	if err != nil {
		t.Fatal(err)
	}
	sig.Name = "Power"
	return sig
}

func TestIRViewTabsBounds(t *testing.T) {
	for _, sz := range []struct {
		name string
		w, h int
	}{{"CZ_53x14", 53, 14}, {"CZ_45x10", 45, 10}, {"CZ_40x10", 40, 10}} {
		m := newIRTestModel(t, sz.w, sz.h)
		for _, tab := range []IRTab{IRTabRemote, IRTabLearn, IRTabBrowse} {
			m.ActiveTabs[ViewIR] = int(tab)
			m.IR.Tab = tab
			assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/"+tabName(tab))
		}
	}
}

func tabName(tab IRTab) string {
	switch tab {
	case IRTabLearn:
		return "Learn"
	case IRTabBrowse:
		return "Browse"
	default:
		return "Remote"
	}
}

func TestIRLearnBind(t *testing.T) {
	m := newIRTestModel(t, 53, 14)
	m.ActiveTabs[ViewIR] = int(IRTabLearn)
	m.IR.Tab = IRTabLearn
	sig := irTestSignal(t)
	m.IR.SetCaptureResult(sig, nil)
	if !m.IR.AwaitBinding || m.IR.Learned == nil {
		t.Fatal("expected captured signal awaiting action selection")
	}
	assertFitsBounds(t, m.View(), 53, 14, "IR/Learn/Captured")

	// Enter on "Bind to key" enters key capture mode.
	m = sendKey(m, "enter")
	if !m.IR.IsKeyCaptureActive() {
		t.Fatal("expected IsKeyCaptureActive after selecting Bind to key")
	}
	assertFitsBounds(t, m.View(), 53, 14, "IR/Learn/BindKeyPrompt")

	// Pressing reserved keys ('q', '?') must not trigger Quit or Help.
	m = sendKey(m, "q")
	if m.ConfirmExit || !m.IR.IsKeyCaptureActive() {
		t.Fatalf("expected q to be blocked cleanly in key capture, confirmExit=%v active=%v", m.ConfirmExit, m.IR.IsKeyCaptureActive())
	}
	m = sendKey(m, "?")
	if m.ShowHelp || !m.IR.IsKeyCaptureActive() {
		t.Fatalf("expected ? to be blocked cleanly in key capture, showHelp=%v active=%v", m.ShowHelp, m.IR.IsKeyCaptureActive())
	}

	// Pressing a letter key ('p') enters the optional label prompt, then 'Power' + Enter binds!
	m = sendKey(m, "p")
	if !m.IR.AwaitBindLabel || m.IR.PendingBindKey != "p" {
		t.Fatalf("expected AwaitBindLabel for 'p', got label=%v key=%q", m.IR.AwaitBindLabel, m.IR.PendingBindKey)
	}
	assertFitsBounds(t, m.View(), 53, 14, "IR/Learn/BindLabelPrompt")
	for _, ch := range []string{"P", "o", "w", "e", "r"} {
		m = sendKey(m, ch)
	}
	m = sendKey(m, "enter")
	p := m.IR.Store.ActiveProfile()
	if p == nil || len(p.Bindings) != 1 || p.Bindings[0].Key != "p" || p.Bindings[0].Name != "Power" {
		t.Fatalf("binding = %+v", p)
	}
	if m.IR.AwaitBinding || m.IR.AwaitKeyPress || m.IR.AwaitBindLabel || m.IR.Learned != nil {
		t.Fatal("expected binding state to clear")
	}
	assertFitsBounds(t, m.View(), 53, 14, "IR/Learn/Bound")

	// Also verify 'r' can be bound with blank label (default "Key R") and works in Remote tab.
	m.IR.SetCaptureResult(sig, nil)
	m = sendKey(m, "enter") // Bind to key
	m = sendKey(m, "r")     // select 'r'
	m = sendKey(m, "enter") // accept default label
	if b, ok := m.IR.BindingForKey("r"); !ok || b.Key != "r" || b.Name != "Key R" {
		t.Fatalf("expected 'r' binding, got ok=%v b=%+v", ok, b)
	}
	m.ActiveTabs[ViewIR] = int(IRTabRemote)
	m.IR.Tab = IRTabRemote
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("expected pressing bound 'r' in Remote tab to queue sendIR cmd")
	}
}

func TestIRBrowseProbeAndGrid(t *testing.T) {
	m := newIRTestModel(t, 53, 14)
	m.ActiveTabs[ViewIR] = int(IRTabBrowse)
	m.IR.Tab = IRTabBrowse
	entries := []ir.Entry{{Provider: "flipper", Category: "TV", Brand: "Acme", Model: "X1", Path: "/tmp/x.ir"}}
	m.IR.SetIndex(entries, nil)
	if len(m.IR.Filtered) != 1 {
		t.Fatalf("filtered = %d", len(m.IR.Filtered))
	}
	assertFitsBounds(t, m.View(), 53, 14, "IR/Browse/List")

	power := irTestSignal(t)
	power2 := irTestSignal(t)
	power2, _ = ir.EncodeProtocol("NEC", 0x08, 0x06)
	power2.Name = "Power"
	vol := irTestSignal(t)
	vol.Name = "Vol_up"
	remote := ir.Remote{Name: "Acme X1", Provider: "flipper", Category: "TV", Brand: "Acme", Model: "X1", Signals: []ir.Signal{power, power2, vol}}
	m.IR.SetRemote(remote)
	assertFitsBounds(t, m.View(), 53, 14, "IR/Browse/Probe")

	// Probe power: first press sends and advances the candidate.
	before := m.IR.ProbeIndex
	m = sendKey(m, "p")
	if m.IR.ProbeIndex == before {
		t.Fatal("expected probe index to advance")
	}

	// Grid mode: a mapped key sends and cycles its signal.
	m = sendKey(m, "g")
	if !m.IR.GridMode {
		t.Fatal("expected grid mode")
	}
	assertFitsBounds(t, m.View(), 53, 14, "IR/Browse/Grid")
	key := m.IR.GridButtons[0].Key
	idx := m.IR.GridButtons[0].Index
	m = sendKey(m, key)
	if len(m.IR.GridButtons[0].Signals) > 1 && m.IR.GridButtons[0].Index == idx {
		t.Fatal("expected grid index to advance")
	}

	// Accept writes a profile and switches to the Remote tab.
	m = sendKey(m, "a")
	if m.ActiveTabs[ViewIR] != int(IRTabRemote) {
		t.Fatalf("expected Remote tab after accept, got %d", m.ActiveTabs[ViewIR])
	}
	if p := m.IR.Store.ActiveProfile(); p == nil || len(p.Bindings) == 0 {
		t.Fatalf("accepted profile = %+v", p)
	}
	assertFitsBounds(t, m.View(), 53, 14, "IR/Remote/Accepted")
}

func TestIRRemoteTabSendsBoundKey(t *testing.T) {
	m := newIRTestModel(t, 45, 10)
	sig := irTestSignal(t)
	m.IR.Store.AddBinding("TV", ir.Binding{Key: "1", Name: "Power", Signal: sig})
	m.IR.Tab = IRTabRemote
	m.ActiveTabs[ViewIR] = int(IRTabRemote)
	assertFitsBounds(t, m.View(), 45, 10, "IR/Remote/Profile")
	m = sendKey(m, "1") // command is queued; state should remain stable
	assertFitsBounds(t, m.View(), 45, 10, "IR/Remote/AfterSend")
}

func TestIREscAndProfileCycle(t *testing.T) {
	m := newIRTestModel(t, 53, 14)
	sig := irTestSignal(t)
	for _, name := range []string{"TV", "Soundbar"} {
		m.IR.Store.AddProfile(ir.BindSignals(name, "flipper", "TV", name, "1", []ir.Signal{sig}))
	}
	m.IR.Tab = IRTabRemote
	m.ActiveTabs[ViewIR] = int(IRTabRemote)
	first := m.IR.Store.Active
	m = sendKey(m, "]")
	if m.IR.Store.Active == first {
		t.Fatal("expected profile cycle")
	}

	m.IR.Tab = IRTabBrowse
	m.ActiveTabs[ViewIR] = int(IRTabBrowse)
	m.IR.SetRemote(ir.Remote{Name: "R", Signals: []ir.Signal{sig}})
	m = sendKey(m, "esc")
	if m.IR.Remote != nil {
		t.Fatal("expected esc to leave remote detail")
	}
	if m.Focus != FocusContent {
		t.Fatal("expected to stay in content after leaving remote detail")
	}
	m = sendKey(m, "esc")
	if m.Focus != FocusTabs {
		t.Fatal("expected second esc to return to tabs")
	}
}

func TestIRMissingHardwareMessage(t *testing.T) {
	m := newIRTestModel(t, 53, 14)
	m.IR.HW = ir.DiscoverHardware()
	m.IR.Tab = IRTabLearn
	m.ActiveTabs[ViewIR] = int(IRTabLearn)
	view := m.View()
	if !strings.Contains(view, "IR") {
		t.Fatalf("expected IR hardware line in view:\n%s", view)
	}
}

func TestIRProbeAcceptUsesCurrentCandidate(t *testing.T) {
	m := newIRTestModel(t, 53, 14)
	m.ActiveTabs[ViewIR] = int(IRTabBrowse)
	m.IR.Tab = IRTabBrowse
	p1, _ := ir.EncodeProtocol("NEC", 0x08, 0x05)
	p1.Name = "Power"
	p2, _ := ir.EncodeProtocol("NEC", 0x08, 0x06)
	p2.Name = "Power"
	m.IR.SetRemote(ir.Remote{Name: "Acme", Provider: "flipper", Category: "TV", Brand: "Acme", Model: "X1", Signals: []ir.Signal{p1, p2}})

	m = sendKey(m, "p") // send candidate 1, advance to candidate 2
	if m.IR.ProbeIndex != 1 {
		t.Fatalf("probe index = %d, want 1", m.IR.ProbeIndex)
	}
	m = sendKey(m, "a") // accept the candidate that was actually sent (1)
	p := m.IR.Store.ActiveProfile()
	if p == nil || len(p.Bindings) != 1 {
		t.Fatalf("profile = %+v", p)
	}
	if got := p.Bindings[0].Signal.Command; got != 0x05 {
		t.Fatalf("accepted command = %#x, want 0x05", got)
	}
	if m.ActiveTabs[ViewIR] != int(IRTabRemote) {
		t.Fatalf("expected Remote tab after accept, got %d", m.ActiveTabs[ViewIR])
	}
}

func TestIRModesFitSmallScreens(t *testing.T) {
	sig := irTestSignal(t)
	for _, sz := range []struct {
		name string
		w, h int
	}{{"CZ_40x10", 40, 10}, {"CZ_45x10", 45, 10}} {
		m := newIRTestModel(t, sz.w, sz.h)
		m.ActiveTabs[ViewIR] = int(IRTabLearn)
		m.IR.Tab = IRTabLearn
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/LearnMenu")
		m.IR.SetCaptureResult(sig, nil)
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/LearnCaptured")
		m = sendKey(m, "enter")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/LearnBindKey")

		m.ActiveTabs[ViewIR] = int(IRTabBrowse)
		m.IR.Tab = IRTabBrowse
		m.IR.SetIndex([]ir.Entry{{Provider: "mi", Category: "TV", Brand: "Mi Remote", Model: "Xiaomi_TV", Path: "/tmp/x.json"}}, nil)
		m.IR.SetRemote(ir.Remote{Name: "Mi TV", Provider: "mi", Category: "TV", Brand: "Mi Remote", Model: "TV", Signals: []ir.Signal{sig, sig, sig}})
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Probe")
		m.IR.GridMode = true
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Grid")
	}
}

func TestIRMultipleRemotesFlow(t *testing.T) {
	sig := irTestSignal(t)
	for _, sz := range []struct {
		name string
		w, h int
	}{{"CZ_40x10", 40, 10}, {"CZ_45x10", 45, 10}, {"CZ_53x14", 53, 14}} {
		m := newIRTestModel(t, sz.w, sz.h)

		// Empty Remote tab -> Enter on "+ New remote" -> type "Living" + Enter
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/RemoteEmptyMenu")
		m = sendKey(m, "enter")
		if m.IR.RemoteMode != remoteModeNew {
			t.Fatalf("[%s] expected remoteModeNew, got %v", sz.name, m.IR.RemoteMode)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/NewRemote")
		for _, ch := range []string{"L", "i", "v", "i", "n", "g"} {
			m = sendKey(m, ch)
		}
		m = sendKey(m, "enter")
		if len(m.IR.Store.Profiles) != 1 || m.IR.Store.ActiveProfile().Name != "Living" {
			t.Fatalf("[%s] expected created 'Living' profile, got %+v", sz.name, m.IR.Store.Profiles)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/RemoteEmptyBindings")

		// Switch to Learn tab and create a second remote "RoomAC" from Learn menu
		m.ActiveTabs[ViewIR] = int(IRTabLearn)
		m.IR.Tab = IRTabLearn
		m = sendKey(m, "down") // Remote: Living
		m = sendKey(m, "down") // + New remote
		m = sendKey(m, "enter")
		if !m.IR.NewRemoteInput {
			t.Fatalf("[%s] expected NewRemoteInput in Learn", sz.name)
		}
		for _, ch := range []string{"R", "o", "o", "m", "A", "C"} {
			m = sendKey(m, ch)
		}
		m = sendKey(m, "enter")
		if len(m.IR.Store.Profiles) != 2 || m.IR.Store.ActiveProfile().Name != "RoomAC" {
			t.Fatalf("[%s] expected 'RoomAC' active, got %+v", sz.name, m.IR.Store.Profiles)
		}

		// Capture and bind key '1' ("Cool") onto RoomAC
		m.IR.SetCaptureResult(sig, nil)
		m = sendKey(m, "enter") // Bind to key
		m = sendKey(m, "1")     // key '1'
		for _, ch := range []string{"C", "o", "o", "l"} {
			m = sendKey(m, ch)
		}
		m = sendKey(m, "enter")
		if got := len(m.IR.Store.Profiles[1].Bindings); got != 1 {
			t.Fatalf("[%s] expected 1 binding on RoomAC, got %d", sz.name, got)
		}

		// Switch back to Remote tab, open Remotes Manager via Tab, select Living, learn a key on it
		m.ActiveTabs[ViewIR] = int(IRTabRemote)
		m.IR.Tab = IRTabRemote
		m = sendKey(m, "tab")
		if m.IR.RemoteMode != remoteModeList {
			t.Fatalf("[%s] expected remoteModeList on Tab, got %v", sz.name, m.IR.RemoteMode)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/RemoteList")
		m = sendKey(m, "up") // Living (index 0)
		m = sendKey(m, "enter")
		if m.IR.Store.ActiveProfile().Name != "Living" || m.IR.RemoteMode != remoteModeUse {
			t.Fatalf("[%s] expected Living selected in use mode", sz.name)
		}

		// Cycle remotes with right arrow, then delete RoomAC in manager
		m = sendKey(m, "right")
		if m.IR.Store.ActiveProfile().Name != "RoomAC" {
			t.Fatalf("[%s] expected RoomAC on right arrow", sz.name)
		}
		m = sendKey(m, "tab")
		m = sendKey(m, "x")
		if len(m.IR.Store.Profiles) != 1 || m.IR.Store.ActiveProfile().Name != "Living" {
			t.Fatalf("[%s] expected RoomAC deleted, remaining=%+v", sz.name, m.IR.Store.Profiles)
		}
	}
}

func TestIRBrowseSearchAndBrandJump(t *testing.T) {
	m := newIRTestModel(t, 40, 10)
	m.ActiveTabs[ViewIR] = int(IRTabBrowse)
	m.IR.Tab = IRTabBrowse
	m.IR.SetIndex([]ir.Entry{
		{Provider: "flipper", Category: "TV", Brand: "LG", Model: "OLED_C1", Path: "a.ir"},
		{Provider: "mi", Category: "TV", Brand: "Samsung", Model: "QN90", Path: "b.ir"},
		{Provider: "flipper", Category: "TV", Brand: "Sony", Model: "Bravia", Path: "c.ir"},
	}, nil)

	// Brand jump ']' moves LG -> Samsung -> Sony
	m = sendKey(m, "]")
	if e, ok := m.IR.SelectedEntry(); !ok || e.Brand != "Samsung" {
		t.Fatalf("expected Samsung on brand jump, got %+v", e)
	}

	// Search '/' -> "sony" narrows to 1 entry; Enter locks, Esc clears
	m = sendKey(m, "/")
	if !m.IR.BrowseSearching {
		t.Fatal("expected BrowseSearching on '/'")
	}
	assertFitsBounds(t, m.View(), 40, 10, "IR/Browse/SearchInput")
	for _, ch := range []string{"s", "o", "n", "y"} {
		m = sendKey(m, ch)
	}
	if len(m.IR.Filtered) != 1 || m.IR.Filtered[0].Brand != "Sony" {
		t.Fatalf("expected filtered Sony entry, got %+v", m.IR.Filtered)
	}
	m = sendKey(m, "enter")
	if m.IR.BrowseSearching {
		t.Fatal("expected Enter to exit search input while keeping filter")
	}
	assertFitsBounds(t, m.View(), 40, 10, "IR/Browse/SearchLocked")
	m = sendKey(m, "esc")
	if m.IR.BrowseQuery != "" || len(m.IR.Filtered) != 3 {
		t.Fatalf("expected Esc to clear query, got query=%q len=%d", m.IR.BrowseQuery, len(m.IR.Filtered))
	}
}

func TestIRGuidedMatchFlow(t *testing.T) {
	p1, _ := ir.EncodeProtocol("NEC", 0x10, 0x01)
	p1.Name = "Power"
	p2, _ := ir.EncodeProtocol("NEC", 0x10, 0x02)
	p2.Name = "Power"
	tUp, _ := ir.EncodeProtocol("NEC", 0x10, 0x03)
	tUp.Name = "Temp+"

	for _, sz := range []struct {
		name string
		w, h int
	}{{"CZ_40x10", 40, 10}, {"CZ_45x10", 45, 10}, {"CZ_53x14", 53, 14}, {"Wide_100x30", 100, 30}} {
		m := newIRTestModel(t, sz.w, sz.h)
		m.IR.SetIndex([]ir.Entry{
			{Provider: "mi", Category: "AC", Brand: "Daikin", Model: "D1", Path: "d1.ir"},
			{Provider: "mi", Category: "AC", Brand: "Epson", Model: "E1", Path: "e1.ir"},
			{Provider: "flipper", Category: "TV", Brand: "LG", Model: "C1", Path: "c1.ir"},
		}, nil)

		// Launch Guided Match from Browse via 'm'.
		m.ActiveTabs[ViewIR] = int(IRTabBrowse)
		m.IR.Tab = IRTabBrowse
		m = sendKey(m, "m")
		if m.IR.BrowseMode != browseModeMatchType {
			t.Fatalf("[%s] expected browseModeMatchType, got %v", sz.name, m.IR.BrowseMode)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Match/Type")

		// Pick AC (second after TV).
		m = sendKey(m, "down")
		m = sendKey(m, "enter")
		if m.IR.BrowseMode != browseModeMatchBrand || m.IR.MatchCategory != "AC" {
			t.Fatalf("[%s] expected match brand for AC, got mode=%v cat=%q", sz.name, m.IR.BrowseMode, m.IR.MatchCategory)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Match/Brand")

		// Search '/' -> "eps" narrows to Epson; Enter locks filter.
		m = sendKey(m, "/")
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Match/BrandSearch")
		for _, ch := range []string{"e", "p", "s"} {
			m = sendKey(m, ch)
		}
		m = sendKey(m, "enter")
		if len(m.IR.MatchBrands) != 1 || m.IR.MatchBrands[0].Name != "Epson" {
			t.Fatalf("[%s] expected Epson brand after filter, got %+v", sz.name, m.IR.MatchBrands)
		}

		// Open Epson -> loading -> deliver candidates -> step & confirm.
		m = sendKey(m, "enter")
		if m.IR.BrowseMode != browseModeMatchTest || !m.IR.MatchLoading {
			t.Fatalf("[%s] expected match test loading, got mode=%v loading=%v", sz.name, m.IR.BrowseMode, m.IR.MatchLoading)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Match/Loading")

		next, _ := m.Update(irMatchCandidatesMsg{
			cands: []ir.MatchCandidate{
				{Entry: m.IR.Index[1], Signal: p1, SignalCount: 2},
				{Entry: m.IR.Index[1], Signal: p2, SignalCount: 2},
			},
		})
		m = next.(AppModel)
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Match/Test")

		// Test Power, step to #2, step back to #1.
		m = sendKey(m, "enter")
		m = sendKey(m, "right")
		if m.IR.MatchCursor != 1 {
			t.Fatalf("[%s] expected MatchCursor 1 after right, got %d", sz.name, m.IR.MatchCursor)
		}
		m = sendKey(m, "left")
		if m.IR.MatchCursor != 0 {
			t.Fatalf("[%s] expected MatchCursor 0 after left, got %d", sz.name, m.IR.MatchCursor)
		}

		// Confirm match via 'y' -> deliver saved profile -> switches to Remote tab.
		m = sendKey(m, "y")
		if !m.IR.MatchSaving {
			t.Fatalf("[%s] expected MatchSaving after 'y'", sz.name)
		}
		prof := ir.BindSignals("Epson E1", "mi", "AC", "Epson", "E1", []ir.Signal{p1, tUp})
		next, _ = m.Update(irMatchSavedMsg{profile: prof})
		m = next.(AppModel)
		if m.ActiveTabs[ViewIR] != int(IRTabRemote) || len(m.IR.Store.Profiles) != 1 {
			t.Fatalf("[%s] expected saved remote on Remote tab, tab=%d profiles=%d", sz.name, m.ActiveTabs[ViewIR], len(m.IR.Store.Profiles))
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Match/SavedRemote")
	}
}

func TestIRUniversalRemoteFlow(t *testing.T) {
	sig := irTestSignal(t)
	for _, sz := range []struct {
		name string
		w, h int
	}{{"CZ_40x10", 40, 10}, {"CZ_45x10", 45, 10}, {"CZ_53x14", 53, 14}, {"Wide_100x30", 100, 30}} {
		m := newIRTestModel(t, sz.w, sz.h)
		m.IR.SetIndex([]ir.Entry{
			{Provider: "mi", Category: "AC", Brand: "Daikin", Model: "D1", Path: "d1.ir"},
			{Provider: "flipper", Category: "TV", Brand: "LG", Model: "C1", Path: "c1.ir"},
		}, nil)

		// Launch Universal Remote from empty Remote menu (index 4: "Universal remote").
		for i := 0; i < 4; i++ {
			m = sendKey(m, "down")
		}
		m = sendKey(m, "enter")
		if m.ActiveTabs[ViewIR] != int(IRTabBrowse) || m.IR.BrowseMode != browseModeUnivType {
			t.Fatalf("[%s] expected browseModeUnivType from empty menu, tab=%d mode=%v", sz.name, m.ActiveTabs[ViewIR], m.IR.BrowseMode)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Univ/Type")

		// Pick AC -> opens temporary Universal remote (does not save a profile).
		m = sendKey(m, "down")
		m = sendKey(m, "enter")
		if m.IR.BrowseMode != browseModeUnivRemote || m.IR.UnivCategory != "AC" {
			t.Fatalf("[%s] expected browseModeUnivRemote for AC, got mode=%v cat=%q", sz.name, m.IR.BrowseMode, m.IR.UnivCategory)
		}
		if len(m.IR.Store.Profiles) != 0 {
			t.Fatalf("[%s] universal remote must remain temporary (profiles=%d)", sz.name, len(m.IR.Store.Profiles))
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Univ/Remote")

		// Press '1' (Power) to start blasting; feed live progress; stop with Space.
		m = sendKey(m, "1")
		if !m.IR.UnivBlasting {
			t.Fatalf("[%s] expected UnivBlasting after pressing 1", sz.name)
		}
		next, _ := m.Update(irUniversalProgressMsg{
			seq: m.IR.UnivSeq,
			prog: ir.UniversalProgress{
				Target:       "power",
				EntryIndex:   1,
				TotalEntries: 1,
				UniqueSent:   1,
				Current:      m.IR.Index[0],
				Signal:       sig,
			},
		})
		m = next.(AppModel)
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Univ/Blasting")

		m = sendKey(m, " ")
		if m.IR.UnivBlasting || m.IR.UnivLastBrand != "Daikin" {
			t.Fatalf("[%s] expected blast stopped with UnivLastBrand=Daikin, blasting=%v brand=%q", sz.name, m.IR.UnivBlasting, m.IR.UnivLastBrand)
		}
		assertFitsBounds(t, m.View(), sz.w, sz.h, sz.name+"/IR/Univ/Stopped")

		// Esc pops UnivRemote -> UnivType -> BrowseList -> FocusTabs.
		m = sendKey(m, "esc")
		if m.IR.BrowseMode != browseModeUnivType {
			t.Fatalf("[%s] expected Esc to pop to UnivType, got %v", sz.name, m.IR.BrowseMode)
		}
		m = sendKey(m, "esc")
		if m.IR.BrowseMode != browseModeList {
			t.Fatalf("[%s] expected Esc to pop to BrowseList, got %v", sz.name, m.IR.BrowseMode)
		}
		m = sendKey(m, "esc")
		if m.Focus != FocusTabs {
			t.Fatalf("[%s] expected Esc to return to FocusTabs", sz.name)
		}
	}
}
