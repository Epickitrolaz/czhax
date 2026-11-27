package ui

import (
	"strings"
	"testing"

	"github.com/czhax/czhax/internal/bluetooth"
	"github.com/czhax/czhax/internal/theme"
)

// TestMouseStepScaling covers the fine/3x/9x scheme requested for the BT Emu
// mouse pad: Ctrl keeps the base step, a bare arrow is 3x, Shift is 9x.
func TestMouseStepScaling(t *testing.T) {
	cases := []struct {
		key  string
		want int
	}{
		{"right", 3},
		{"d", 3},
		{"up", 3},
		{"ctrl+right", 1},
		{"ctrl+d", 1},
		{"shift+right", 9},
		{"shift+d", 9},
		{"D", 9}, // shift+letter arrives as an uppercase rune
		{"alt+d", 3},
	}
	for _, c := range cases {
		if got := mouseStepScale(c.key); got != c.want {
			t.Errorf("mouseStepScale(%q) = %d, want %d", c.key, got, c.want)
		}
	}
}

func TestUnmodifiedKeyStripsModifiers(t *testing.T) {
	cases := map[string]string{
		"right":            "right",
		"ctrl+right":       "right",
		"shift+right":      "right",
		"ctrl+shift+Right": "right",
		"ctrl+d":           "d",
		"D":                "d",
		"+":                "+",
		"enter":            "enter",
	}
	for in, want := range cases {
		if got := unmodifiedKey(in); got != want {
			t.Errorf("unmodifiedKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClampMouseDelta(t *testing.T) {
	if got := clampMouseDelta(0); got != 0 {
		t.Fatalf("clampMouseDelta(0) = %d", got)
	}
	if got := clampMouseDelta(40 * 9); got != 127 {
		t.Fatalf("clampMouseDelta(360) = %d, want 127", got)
	}
	if got := clampMouseDelta(-40 * 9); got != -127 {
		t.Fatalf("clampMouseDelta(-360) = %d, want -127", got)
	}
}

// TestEmuSetupShowsOnlyHCI is the "just show hci0, hci1" request: the radio row
// must not append the bus (USB/UART) or a status word.
func TestEmuSetupShowsOnlyHCI(t *testing.T) {
	v := NewBluetoothEmuView()
	v.Adapters = []bluetooth.Adapter{
		{Name: "hci1", Bus: "USB", Up: true},
		{Name: "hci0", Bus: "UART", Up: true},
	}
	v.AdapterIdx = 0
	v.AdapterManual = true

	joined := strings.Join(v.RenderLines(40, 10, theme.HighContrast, true), "\n")
	if !strings.Contains(joined, "hci1") {
		t.Fatalf("expected the selected adapter name:\n%s", joined)
	}
	for _, unwanted := range []string{"USB", "UART", "UP", "DN"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("setup screen should show only the HCI name, found %q:\n%s", unwanted, joined)
		}
	}
}
