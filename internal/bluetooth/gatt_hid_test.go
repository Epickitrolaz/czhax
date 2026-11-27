package bluetooth

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

// parseInputReportSizes walks a HID report map and returns the byte length of
// every Input report, keyed by report ID and excluding the leading Report ID
// byte that GATT notifications carry.
func parseInputReportSizes(t *testing.T, desc []byte) map[uint8]int {
	t.Helper()
	bits := map[uint8]int{}
	var reportID uint8
	var rSize, rCount int

	for i := 0; i < len(desc); {
		prefix := desc[i]
		if prefix == 0xFE { // long item
			if i+2 >= len(desc) {
				t.Fatalf("truncated long item at %d", i)
			}
			i += 3 + int(desc[i+1])
			continue
		}
		size := int(prefix & 0x03)
		if size == 3 {
			size = 4
		}
		if i+1+size > len(desc) {
			t.Fatalf("truncated item at %d", i)
		}
		typ := (prefix >> 2) & 0x03
		tag := prefix >> 4
		data := desc[i+1 : i+1+size]
		i += 1 + size
		val := 0
		for k := len(data) - 1; k >= 0; k-- {
			val = val<<8 | int(data[k])
		}
		switch typ {
		case 0: // main item
			if tag == 0x8 { // Input
				bits[reportID] += rSize * rCount
			}
		case 1: // global item
			switch tag {
			case 0x7: // Report Size
				rSize = val
			case 0x8: // Report ID
				reportID = uint8(val)
			case 0x9: // Report Count
				rCount = val
			}
		}
	}

	sizes := make(map[uint8]int, len(bits))
	for id, b := range bits {
		if b%8 != 0 {
			t.Fatalf("report %d is %d bits, not byte aligned", id, b)
		}
		sizes[id] = b / 8
	}
	return sizes
}

// TestReportPayloadsMatchReportMap guards the notification framing: every
// report must start with the Report ID declared in the descriptor, otherwise
// hosts discard the input while the link itself still looks healthy.
func TestReportPayloadsMatchReportMap(t *testing.T) {
	sizes := parseInputReportSizes(t, BuildHIDReportMap(true, true, true, true))

	cases := []struct {
		name    string
		id      uint8
		payload []byte
	}{
		{"keyboard", HIDReportIDKeyboard, keyboardReport(HIDModLShift, 0x04)},
		{"mouse", HIDReportIDMouse, mouseReport(1, 4, -4, 1)},
		{"consumer", HIDReportIDConsumer, consumerReport(ConsumerPlayPause)},
		{"gamepad", HIDReportIDGamepad, gamepadReport(0x0003, 1, -1)},
	}
	for _, c := range cases {
		want, ok := sizes[c.id]
		if !ok {
			t.Fatalf("%s: report ID %d is not in the report map", c.name, c.id)
		}
		if len(c.payload) != want+1 {
			t.Errorf("%s: payload is %d bytes, report map declares %d + 1 ID byte", c.name, len(c.payload), want)
		}
		if c.payload[0] != c.id {
			t.Errorf("%s: payload starts with %#x, want report ID %#x", c.name, c.payload[0], c.id)
		}
	}

	// A keyboard-only profile keeps the same framing.
	kbSizes := parseInputReportSizes(t, BuildHIDReportMap(true, false, false, false))
	if len(kbSizes) != 1 {
		t.Fatalf("keyboard-only map declares %d reports, want 1", len(kbSizes))
	}
	if got := len(keyboardReport(0, 0)); got != kbSizes[HIDReportIDKeyboard]+1 {
		t.Fatalf("keyboard payload %d bytes, want %d", got, kbSizes[HIDReportIDKeyboard]+1)
	}
}

func TestGATTReadValueHonorsOffset(t *testing.T) {
	val := []byte("0123456789")
	c := &gattChar{value: val}

	got, derr := c.ReadValue(nil)
	if derr != nil || string(got) != string(val) {
		t.Fatalf("offset 0 read = %q (%v), want full value", got, derr)
	}
	got, _ = c.ReadValue(map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(4))})
	if string(got) != "456789" {
		t.Fatalf("offset 4 read = %q, want %q", got, "456789")
	}
	got, _ = c.ReadValue(map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(len(val)))})
	if len(got) != 0 {
		t.Fatalf("offset at end read = %q, want empty", got)
	}

	d := &gattDesc{value: val}
	got, _ = d.ReadValue(map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(2))})
	if string(got) != "23456789" {
		t.Fatalf("descriptor offset read = %q", got)
	}
}

func TestGATTWriteValueOffsetSplices(t *testing.T) {
	c := &gattChar{value: []byte{0x01, 0x02, 0x03, 0x04}}
	if err := c.WriteValue([]byte{0xAA}, map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(2))}); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}
	if want := []byte{0x01, 0x02, 0xAA, 0x04}; string(c.value) != string(want) {
		t.Fatalf("long write produced % x, want % x", c.value, want)
	}
	if err := c.WriteValue([]byte{0x09}, nil); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}
	if string(c.value) != "\x09" {
		t.Fatalf("plain write produced % x", c.value)
	}
}

func TestAdvertiserRearmIsNonBlocking(t *testing.T) {
	a := &leAdvertiser{kick: make(chan struct{}, 1)}
	for i := 0; i < 5; i++ {
		a.kickRearm() // must never block, even with a pending kick
	}
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	a.kickRearm()
	select {
	case <-a.kick:
	default:
		t.Fatal("expected exactly one queued kick")
	}
}

// TestReportMapReadChunksReassemble mimics the peer side of a long attribute
// read: a Read Request followed by Read Blob requests with increasing offsets
// and an MTU of 23. Ignoring the offset makes the peer rebuild a report map
// full of duplicated head bytes, which hosts reject by dropping the link.
func TestReportMapReadChunksReassemble(t *testing.T) {
	full := BuildHIDReportMap(true, true, true, true)
	if len(full) <= 20 {
		t.Fatalf("report map is only %d bytes; the test needs a multi-chunk map", len(full))
	}
	c := &gattChar{value: full, uuid: uuidReportMap}

	var got []byte
	for off := 0; off < len(full); {
		chunk, derr := c.ReadValue(map[string]dbus.Variant{"offset": dbus.MakeVariant(uint16(off))})
		if derr != nil {
			t.Fatalf("read at offset %d: %v", off, derr)
		}
		if len(chunk) == 0 {
			t.Fatalf("read at offset %d returned no data", off)
		}
		if len(chunk) > 20 { // ATT payload for MTU 23
			chunk = chunk[:20]
		}
		got = append(got, chunk...)
		off += len(chunk)
	}
	if string(got) != string(full) {
		t.Fatalf("reassembled map (%d bytes) differs from the %d byte descriptor", len(got), len(full))
	}
}

func TestKeyboardLEDWriteUsesReportID(t *testing.T) {
	var leds uint8
	c := &gattChar{}
	c.onWrite = func(v []byte) {
		if len(v) >= 2 && v[0] == HIDReportIDKeyboard {
			leds = v[1]
		}
	}
	if err := c.WriteValue([]byte{HIDReportIDKeyboard, 0x02}, nil); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}
	if leds != 0x02 {
		t.Fatalf("LED byte = %#x, want 0x02 (caps lock)", leds)
	}
}
