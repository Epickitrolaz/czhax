package power

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"
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
	for _, nonBtn := range []string{"tca8418-keypad", "tca8418c", "bmi270-imu", "ES8389-Audio Headphones", "gpio_ir_recv", ""} {
		if isWakeButtonDevice(nonBtn) {
			t.Fatalf("did not expect %q to be flagged as dedicated wake button", nonBtn)
		}
	}
	if !isWakeKeyCode(keyEsc) || !isWakeKeyCode(keyPower) || !isWakeKeyCode(keyWakeup) || !isWakeKeyCode(keyProg1) {
		t.Fatalf("expected esc/power/wakeup/prog1 to be wake keycodes")
	}
	for _, regular := range []uint16{28, 30, 57} {
		if isWakeKeyCode(regular) {
			t.Fatalf("did not expect regular keycode %d (Enter/A/Space) to wake", regular)
		}
	}
	if !isEscWakeBytes([]byte{0x1b}) || !isEscWakeBytes([]byte{0x03}) {
		t.Fatalf("expected standalone Esc and Ctrl+C to match Esc wake")
	}
	for _, nonEsc := range [][]byte{{0x1b, '[', 'A'}, {'\r'}, {'\n'}, {' '}, {'a'}} {
		if isEscWakeBytes(nonEsc) {
			t.Fatalf("did not expect %q to wake sleep mode", nonEsc)
		}
	}
	if !isWakeGPIOLine("BOOT_BTN", "") || !isWakeGPIOLine("", "power-button") || isWakeGPIOLine("LED1", "status") {
		t.Fatalf("unexpected GPIO wake line classification")
	}
}

func TestGPIOStructSizesMatchLinuxABI(t *testing.T) {
	if got := unsafe.Sizeof(gpioChipInfo{}); got != 68 {
		t.Fatalf("gpioChipInfo size = %d, want 68", got)
	}
	if got := unsafe.Sizeof(gpioLineInfo{}); got != 72 {
		t.Fatalf("gpioLineInfo size = %d, want 72", got)
	}
	if got := unsafe.Sizeof(gpioHandleRequest{}); got != 364 {
		t.Fatalf("gpioHandleRequest size = %d, want 364", got)
	}
	if got := unsafe.Sizeof(gpioHandleData{}); got != 64 {
		t.Fatalf("gpioHandleData size = %d, want 64", got)
	}
	if got := unsafe.Sizeof(gpioV2LineInfo{}); got != 256 {
		t.Fatalf("gpioV2LineInfo size = %d, want 256", got)
	}
	if got := unsafe.Sizeof(gpioV2LineRequest{}); got != 592 {
		t.Fatalf("gpioV2LineRequest size = %d, want 592", got)
	}
	if got := unsafe.Sizeof(gpioV2LineValues{}); got != 16 {
		t.Fatalf("gpioV2LineValues size = %d, want 16", got)
	}
}
