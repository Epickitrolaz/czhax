package bluetooth

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeHCI(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "hci0"},
		{"0", "hci0"},
		{"hci1", "hci1"},
		{"2", "hci2"},
	} {
		if got := NormalizeHCI(tc.in); got != tc.want {
			t.Fatalf("NormalizeHCI(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRFCOMMResponse(t *testing.T) {
	if got := rfcommResponse("AT+BRSF=5"); !strings.Contains(got, "+BRSF: 63") {
		t.Fatalf("unexpected BRSF response %q", got)
	}
	if got := rfcommResponse("AT+CIND=?"); !strings.Contains(got, "call_setup") {
		t.Fatalf("unexpected CIND response %q", got)
	}
	if got := rfcommResponse("AT+VGS=15"); got != "OK\r\n" {
		t.Fatalf("unexpected default response %q", got)
	}
}

func TestCarwhispererRecordPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	p := CarwhispererRecordPath("AA:BB:CC:DD:EE:FF")
	if filepath.Ext(p) != ".wav" || !strings.Contains(filepath.Base(p), "carwhisperer-AABBCCDDEEFF-") {
		t.Fatalf("unexpected record path %q", p)
	}
}

func TestAttackKindString(t *testing.T) {
	for _, tc := range []struct {
		kind AttackKind
		want string
	}{
		{AttackCarwhisperer, "Carwhisperer"},
		{AttackWhisperPair, "WhisperPair"},
		{AttackL2Flood, "l2ping flood"},
	} {
		if got := tc.kind.String(); got != tc.want {
			t.Fatalf("AttackKind(%d) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
