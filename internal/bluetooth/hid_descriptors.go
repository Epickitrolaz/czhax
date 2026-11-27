package bluetooth

import "strings"

// HID Report IDs inside the composite BLE HID Report Map.
const (
	HIDReportIDKeyboard uint8 = 1
	HIDReportIDMouse    uint8 = 2
	HIDReportIDConsumer uint8 = 3
	HIDReportIDGamepad  uint8 = 4
)

// HID Modifier bitmasks.
const (
	HIDModLCtrl  uint8 = 0x01
	HIDModLShift uint8 = 0x02
	HIDModLAlt   uint8 = 0x04
	HIDModLGUI   uint8 = 0x08
	HIDModRCtrl  uint8 = 0x10
	HIDModRShift uint8 = 0x20
	HIDModRAlt   uint8 = 0x40
	HIDModRGUI   uint8 = 0x80
)

// Common Consumer Control (Page 0x0C) usages.
const (
	ConsumerPlayPause uint16 = 0x00CD
	ConsumerStop      uint16 = 0x00B7
	ConsumerNextTrack uint16 = 0x00B5
	ConsumerPrevTrack uint16 = 0x00B6
	ConsumerMute      uint16 = 0x00E2
	ConsumerVolUp     uint16 = 0x00E9
	ConsumerVolDown   uint16 = 0x00EA
	ConsumerHome      uint16 = 0x0223
)

var hidKeyboardReportDesc = []byte{
	0x05, 0x01, // Usage Page (Generic Desktop)
	0x09, 0x06, // Usage (Keyboard)
	0xA1, 0x01, // Collection (Application)
	0x85, HIDReportIDKeyboard,
	0x05, 0x07, //   Usage Page (Key Codes)
	0x19, 0xE0, //   Usage Minimum (224)
	0x29, 0xE7, //   Usage Maximum (231)
	0x15, 0x00, //   Logical Minimum (0)
	0x25, 0x01, //   Logical Maximum (1)
	0x75, 0x01, //   Report Size (1)
	0x95, 0x08, //   Report Count (8)
	0x81, 0x02, //   Input (Data, Variable, Absolute) ; Modifier byte
	0x95, 0x01, //   Report Count (1)
	0x75, 0x08, //   Report Size (8)
	0x81, 0x01, //   Input (Constant) ; Reserved byte
	0x95, 0x05, //   Report Count (5)
	0x75, 0x01, //   Report Size (1)
	0x05, 0x08, //   Usage Page (LEDs)
	0x19, 0x01, //   Usage Minimum (1)
	0x29, 0x05, //   Usage Maximum (5)
	0x91, 0x02, //   Output (Data, Variable, Absolute) ; LED report
	0x95, 0x01, //   Report Count (1)
	0x75, 0x03, //   Report Size (3)
	0x91, 0x01, //   Output (Constant) ; LED padding
	0x95, 0x06, //   Report Count (6)
	0x75, 0x08, //   Report Size (8)
	0x15, 0x00, //   Logical Minimum (0)
	0x25, 0x65, //   Logical Maximum (101)
	0x05, 0x07, //   Usage Page (Key Codes)
	0x19, 0x00, //   Usage Minimum (0)
	0x29, 0x65, //   Usage Maximum (101)
	0x81, 0x00, //   Input (Data, Array) ; 6 key codes
	0xC0, // End Collection
}

var hidMouseReportDesc = []byte{
	0x05, 0x01, // Usage Page (Generic Desktop)
	0x09, 0x02, // Usage (Mouse)
	0xA1, 0x01, // Collection (Application)
	0x85, HIDReportIDMouse,
	0x09, 0x01, //   Usage (Pointer)
	0xA1, 0x00, //   Collection (Physical)
	0x05, 0x09, //     Usage Page (Buttons)
	0x19, 0x01, //     Usage Minimum (1)
	0x29, 0x03, //     Usage Maximum (3)
	0x15, 0x00, //     Logical Minimum (0)
	0x25, 0x01, //     Logical Maximum (1)
	0x95, 0x03, //     Report Count (3)
	0x75, 0x01, //     Report Size (1)
	0x81, 0x02, //     Input (Data, Variable, Absolute)
	0x95, 0x01, //     Report Count (1)
	0x75, 0x05, //     Report Size (5)
	0x81, 0x01, //     Input (Constant) ; 5-bit padding
	0x05, 0x01, //     Usage Page (Generic Desktop)
	0x09, 0x30, //     Usage (X)
	0x09, 0x31, //     Usage (Y)
	0x09, 0x38, //     Usage (Wheel)
	0x15, 0x81, //     Logical Minimum (-127)
	0x25, 0x7F, //     Logical Maximum (127)
	0x75, 0x08, //     Report Size (8)
	0x95, 0x03, //     Report Count (3)
	0x81, 0x06, //     Input (Data, Variable, Relative)
	0xC0, //   End Collection
	0xC0, // End Collection
}

var hidConsumerReportDesc = []byte{
	0x05, 0x0C, // Usage Page (Consumer)
	0x09, 0x01, // Usage (Consumer Control)
	0xA1, 0x01, // Collection (Application)
	0x85, HIDReportIDConsumer,
	0x15, 0x00, //   Logical Minimum (0)
	0x26, 0xFF, 0x03, // Logical Maximum (1023)
	0x19, 0x00, //   Usage Minimum (0)
	0x2A, 0xFF, 0x03, // Usage Maximum (1023)
	0x75, 0x10, //   Report Size (16)
	0x95, 0x01, //   Report Count (1)
	0x81, 0x00, //   Input (Data, Array)
	0xC0, // End Collection
}

var hidGamepadReportDesc = []byte{
	0x05, 0x01, // Usage Page (Generic Desktop)
	0x09, 0x05, // Usage (Gamepad)
	0xA1, 0x01, // Collection (Application)
	0x85, HIDReportIDGamepad,
	0x05, 0x09, //   Usage Page (Buttons)
	0x19, 0x01, //   Usage Minimum (1)
	0x29, 0x10, //   Usage Maximum (16)
	0x15, 0x00, //   Logical Minimum (0)
	0x25, 0x01, //   Logical Maximum (1)
	0x75, 0x01, //   Report Size (1)
	0x95, 0x10, //   Report Count (16)
	0x81, 0x02, //   Input (Data, Variable, Absolute)
	0x05, 0x01, //   Usage Page (Generic Desktop)
	0x09, 0x30, //   Usage (X)
	0x09, 0x31, //   Usage (Y)
	0x15, 0x81, //   Logical Minimum (-127)
	0x25, 0x7F, //   Logical Maximum (127)
	0x75, 0x08, //   Report Size (8)
	0x95, 0x02, //   Report Count (2)
	0x81, 0x02, //   Input (Data, Variable, Absolute)
	0xC0, // End Collection
}

// BuildHIDReportMap builds a composite HID Report Descriptor containing the
// enabled report collections.
func BuildHIDReportMap(hasKB, hasMouse, hasConsumer, hasGamepad bool) []byte {
	var out []byte
	if hasKB {
		out = append(out, hidKeyboardReportDesc...)
	}
	if hasMouse {
		out = append(out, hidMouseReportDesc...)
	}
	if hasConsumer {
		out = append(out, hidConsumerReportDesc...)
	}
	if hasGamepad {
		out = append(out, hidGamepadReportDesc...)
	}
	return out
}

// KeyToHID converts a Bubble Tea key string (e.g. "a", "A", "enter", "ctrl+c")
// to a USB HID Usage ID (Page 0x07) and modifier bitmask.
func KeyToHID(key string) (code, mods uint8, ok bool) {
	if strings.HasPrefix(key, "ctrl+") && len(key) == 6 {
		if c, m, ok2 := KeyToHID(key[5:]); ok2 {
			return c, m | HIDModLCtrl, true
		}
	}
	if strings.HasPrefix(key, "alt+") && len(key) > 4 {
		if c, m, ok2 := KeyToHID(key[4:]); ok2 {
			return c, m | HIDModLAlt, true
		}
	}
	switch key {
	case "enter":
		return 0x28, 0, true
	case "esc":
		return 0x29, 0, true
	case "backspace":
		return 0x2A, 0, true
	case "tab":
		return 0x2B, 0, true
	case " ", "space":
		return 0x2C, 0, true
	case "delete":
		return 0x4C, 0, true
	case "home":
		return 0x4A, 0, true
	case "end":
		return 0x4D, 0, true
	case "pgup":
		return 0x4B, 0, true
	case "pgdown":
		return 0x4E, 0, true
	case "right":
		return 0x4F, 0, true
	case "left":
		return 0x50, 0, true
	case "down":
		return 0x51, 0, true
	case "up":
		return 0x52, 0, true
	case "f1":
		return 0x3A, 0, true
	case "f2":
		return 0x3B, 0, true
	case "f3":
		return 0x3C, 0, true
	case "f4":
		return 0x3D, 0, true
	case "f5":
		return 0x3E, 0, true
	case "f6":
		return 0x3F, 0, true
	case "f7":
		return 0x40, 0, true
	case "f8":
		return 0x41, 0, true
	case "f9":
		return 0x42, 0, true
	case "f10":
		return 0x43, 0, true
	case "f11":
		return 0x44, 0, true
	case "f12":
		return 0x45, 0, true
	}
	rs := []rune(key)
	if len(rs) == 1 {
		return RuneToHID(rs[0])
	}
	return 0, 0, false
}

// RuneToHID converts a printable US-ASCII rune or newline/tab to a USB HID keycode + modifiers.
func RuneToHID(r rune) (code, mods uint8, ok bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return uint8(0x04 + (r - 'a')), 0, true
	case r >= 'A' && r <= 'Z':
		return uint8(0x04 + (r - 'A')), HIDModLShift, true
	case r >= '1' && r <= '9':
		return uint8(0x1E + (r - '1')), 0, true
	case r == '0':
		return 0x27, 0, true
	}
	switch r {
	case '\n', '\r':
		return 0x28, 0, true
	case '\t':
		return 0x2B, 0, true
	case ' ':
		return 0x2C, 0, true
	case '!':
		return 0x1E, HIDModLShift, true
	case '@':
		return 0x1F, HIDModLShift, true
	case '#':
		return 0x20, HIDModLShift, true
	case '$':
		return 0x21, HIDModLShift, true
	case '%':
		return 0x22, HIDModLShift, true
	case '^':
		return 0x23, HIDModLShift, true
	case '&':
		return 0x24, HIDModLShift, true
	case '*':
		return 0x25, HIDModLShift, true
	case '(':
		return 0x26, HIDModLShift, true
	case ')':
		return 0x27, HIDModLShift, true
	case '-':
		return 0x2D, 0, true
	case '_':
		return 0x2D, HIDModLShift, true
	case '=':
		return 0x2E, 0, true
	case '+':
		return 0x2E, HIDModLShift, true
	case '[':
		return 0x2F, 0, true
	case '{':
		return 0x2F, HIDModLShift, true
	case ']':
		return 0x30, 0, true
	case '}':
		return 0x30, HIDModLShift, true
	case '\\':
		return 0x31, 0, true
	case '|':
		return 0x31, HIDModLShift, true
	case ';':
		return 0x33, 0, true
	case ':':
		return 0x33, HIDModLShift, true
	case '\'':
		return 0x34, 0, true
	case '"':
		return 0x34, HIDModLShift, true
	case '`':
		return 0x35, 0, true
	case '~':
		return 0x35, HIDModLShift, true
	case ',':
		return 0x36, 0, true
	case '<':
		return 0x36, HIDModLShift, true
	case '.':
		return 0x37, 0, true
	case '>':
		return 0x37, HIDModLShift, true
	case '/':
		return 0x38, 0, true
	case '?':
		return 0x38, HIDModLShift, true
	}
	return 0, 0, false
}
