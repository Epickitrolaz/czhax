package bluetooth

import "testing"

// TestCarwhispererPINOrder pins the exact PIN list and order so a future edit
// cannot silently drop or reorder entries that users rely on.
func TestCarwhispererPINOrder(t *testing.T) {
	want := []string{
		"0000", "1234",
		"1111", "12345", "123456", "000000", "12345678", "00000000",
		"1212", "00000", "4444", "5555", "7777", "8888", "9999",
		"1122", "0001", "0011", "0111",
	}
	if len(carwhispererPINs) != len(want) {
		t.Fatalf("carwhispererPINs has %d entries, want %d", len(carwhispererPINs), len(want))
	}
	for i, pin := range want {
		if carwhispererPINs[i] != pin {
			t.Fatalf("PIN %d = %q, want %q (full list %v)", i, carwhispererPINs[i], pin, carwhispererPINs)
		}
	}

	// No duplicates: every extra attempt costs an agent spawn and an RFCOMM
	// connect on a 512 MB handheld.
	seen := map[string]bool{}
	for _, pin := range carwhispererPINs {
		if seen[pin] {
			t.Fatalf("duplicate PIN %q", pin)
		}
		seen[pin] = true
	}
}

func TestCarwhispererPairAttemptLadder(t *testing.T) {
	attempts := carwhispererPairAttempts()
	if len(attempts) != len(carwhispererPINs)+2 {
		t.Fatalf("got %d attempts, want %d (2 agents + %d PINs)",
			len(attempts), len(carwhispererPINs)+2, len(carwhispererPINs))
	}
	if attempts[0].capability != "DisplayYesNo" || attempts[0].pin != "" {
		t.Fatalf("first attempt = %+v, want a PIN-less DisplayYesNo agent", attempts[0])
	}
	if attempts[1].capability != "NoInputNoOutput" || attempts[1].pin != "" {
		t.Fatalf("second attempt = %+v, want a PIN-less NoInputNoOutput agent", attempts[1])
	}
	for i, pin := range carwhispererPINs {
		got := attempts[i+2]
		if got.capability != "KeyboardOnly" || got.pin != pin {
			t.Fatalf("attempt %d = %+v, want KeyboardOnly PIN %q", i+2, got, pin)
		}
		if got.timeout != rfcommPinTimeout {
			t.Fatalf("attempt %d timeout = %v, want %v", i+2, got.timeout, rfcommPinTimeout)
		}
	}
}

func TestPinLabel(t *testing.T) {
	if pinLabel("") != " agent" {
		t.Fatalf("pinLabel(\"\") = %q, want \" agent\"", pinLabel(""))
	}
	if pinLabel("1111") != " agent PIN 1111" {
		t.Fatalf("pinLabel(1111) = %q", pinLabel("1111"))
	}
}
