package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadBatteryTimeToEmptyDirect(t *testing.T) {
	root := t.TempDir()
	bat := filepath.Join(root, "bq27220-0")
	if err := os.MkdirAll(bat, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, val string) {
		if err := os.WriteFile(filepath.Join(bat, name), []byte(val+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("type", "Battery")
	write("capacity", "74")
	write("status", "Discharging")
	write("time_to_empty_now", "9900") // 2h45m

	got := readBatteryFromDir(root)
	if !got.Present || got.Pct != 74 || got.Charging {
		t.Fatalf("unexpected battery status: %+v", got)
	}
	if !got.HasTimeLeft || got.TimeLeft != 9900*time.Second {
		t.Fatalf("expected 2h45m time left, got %+v", got)
	}
	if FormatDuration(got.TimeLeft) != "2h45m" {
		t.Fatalf("expected 2h45m, got %q", FormatDuration(got.TimeLeft))
	}
}

func TestReadBatteryTimeToEmptyFromChargeAndCurrent(t *testing.T) {
	root := t.TempDir()
	bat := filepath.Join(root, "bat0")
	if err := os.MkdirAll(bat, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, val string) {
		if err := os.WriteFile(filepath.Join(bat, name), []byte(val+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("type", "Battery")
	write("capacity", "50")
	write("status", "Discharging")
	write("charge_now", "1000000")   // 1 Ah
	write("current_now", "-500000")  // 0.5 A discharge -> 2h

	got := readBatteryFromDir(root)
	if !got.HasTimeLeft || got.TimeLeft != 2*time.Hour {
		t.Fatalf("expected 2h estimated from charge/current, got %+v", got)
	}
}
