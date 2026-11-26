package oui

import "testing"

func TestLookup(t *testing.T) {
	if got := Lookup("84:D3:52:E8:E8:FB"); got != "Tonly Technology Co. Ltd" {
		t.Fatalf("Lookup(84:D3:52) = %q", got)
	}
	if got := Lookup("00:00:00:00:00:00"); got != "XEROX CORPORATION" {
		t.Fatalf("Lookup(00:00:00) = %q", got)
	}
	if got := Lookup("not-a-mac"); got != "" {
		t.Fatalf("Lookup(invalid) = %q", got)
	}
}
