package power

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnterAndExitSleepRestoresSysfs(t *testing.T) {
	root := t.TempDir()
	blDir := filepath.Join(root, "class/backlight/backlight")
	fbDir := filepath.Join(root, "class/graphics/fb0")
	cpuDir := filepath.Join(root, "devices/system/cpu/cpu0/cpufreq")
	ledDir := filepath.Join(root, "class/leds/fn-led")
	for _, d := range []string{blDir, fbDir, cpuDir, ledDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	blFile := filepath.Join(blDir, "brightness")
	fbFile := filepath.Join(fbDir, "blank")
	govFile := filepath.Join(cpuDir, "scaling_governor")
	ledFile := filepath.Join(ledDir, "brightness")

	_ = os.WriteFile(blFile, []byte("75\n"), 0o644)
	_ = os.WriteFile(fbFile, []byte("0\n"), 0o644)
	_ = os.WriteFile(govFile, []byte("ondemand\n"), 0o644)
	_ = os.WriteFile(ledFile, []byte("1\n"), 0o644)

	saved := EnterSleep(root)

	checkVal := func(path, want string) {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got := string(b)
		if got != want+"\n" {
			t.Fatalf("%s = %q, want %q", path, got, want+"\n")
		}
	}

	checkVal(blFile, "0")
	checkVal(fbFile, "1")
	checkVal(govFile, "powersave")
	checkVal(ledFile, "0")

	ExitSleep(saved)

	checkVal(blFile, "75")
	checkVal(fbFile, "0")
	checkVal(govFile, "ondemand")
	checkVal(ledFile, "1")
}

func TestWakeDetectionHelpers(t *testing.T) {
	if !isWakeButtonDevice("gpio-keys") || !isWakeButtonDevice("pwr_button") || !isWakeButtonDevice("cardputer-btn") {
		t.Fatalf("expected button/gpio devices to be detected as wake buttons")
	}
	if isWakeButtonDevice("tca8418-keypad") || isWakeButtonDevice("bmi270-imu") || isWakeButtonDevice("") {
		t.Fatalf("did not expect keypad, imu, or empty name to be flagged as dedicated button")
	}
	if !isWakeKeyCode(keyPower) || !isWakeKeyCode(keyWakeup) || isWakeKeyCode(30) {
		t.Fatalf("unexpected wake keycode classification")
	}
	if !isWakeGPIOLine("BOOT_BTN", "") || !isWakeGPIOLine("", "power-button") || isWakeGPIOLine("LED1", "status") {
		t.Fatalf("unexpected GPIO wake line classification")
	}
}
