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

	// Pressing a letter key ('p') binds cleanly!
	m = sendKey(m, "p")
	p := m.IR.Store.ActiveProfile()
	if p == nil || len(p.Bindings) != 1 || p.Bindings[0].Key != "p" {
		t.Fatalf("binding = %+v", p)
	}
	if m.IR.AwaitBinding || m.IR.AwaitKeyPress || m.IR.Learned != nil {
		t.Fatal("expected binding state to clear")
	}
	assertFitsBounds(t, m.View(), 53, 14, "IR/Learn/Bound")

	// Also verify 'r' can be bound without triggering global refresh, and works in Remote tab.
	m.IR.SetCaptureResult(sig, nil)
	m = sendKey(m, "enter") // Bind to key
	m = sendKey(m, "r")     // bind 'r'
	if b, ok := m.IR.BindingForKey("r"); !ok || b.Key != "r" {
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
