package bluetooth

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	dbusAppPath   = dbus.ObjectPath("/com/czhax/bt/app")
	dbusAdvPath   = dbus.ObjectPath("/com/czhax/bt/adv")
	dbusAgentPath = dbus.ObjectPath("/com/czhax/bt/agent")

	uuidHIDService       = "00001812-0000-1000-8000-00805f9b34fb"
	uuidHIDInfo          = "00002a4a-0000-1000-8000-00805f9b34fb"
	uuidReportMap        = "00002a4b-0000-1000-8000-00805f9b34fb"
	uuidHIDControlPoint  = "00002a4c-0000-1000-8000-00805f9b34fb"
	uuidHIDReport        = "00002a4d-0000-1000-8000-00805f9b34fb"
	uuidProtocolMode     = "00002a4e-0000-1000-8000-00805f9b34fb"
	uuidReportRef        = "00002908-0000-1000-8000-00805f9b34fb"
	uuidBatteryService   = "0000180f-0000-1000-8000-00805f9b34fb"
	uuidBatteryLevel     = "00002a19-0000-1000-8000-00805f9b34fb"
	uuidDevInfoService   = "0000180a-0000-1000-8000-00805f9b34fb"
	uuidPnPID            = "00002a50-0000-1000-8000-00805f9b34fb"
	bluezService         = "org.bluez"
	bluezAdapterIface    = "org.bluez.Adapter1"
	bluezDeviceIface     = "org.bluez.Device1"
	bluezGattMgrIface    = "org.bluez.GattManager1"
	bluezLEAdvMgrIface   = "org.bluez.LEAdvertisingManager1"
	bluezMediaIface      = "org.bluez.Media1"
	bluezProfileMgrIface = "org.bluez.ProfileManager1"
	bluezAgentMgrIface   = "org.bluez.AgentManager1"
	dbusPropsIface       = "org.freedesktop.DBus.Properties"
	dbusObjMgrIface      = "org.freedesktop.DBus.ObjectManager"
)

// RunEmulatorDirect runs the BlueZ D-Bus emulator in the current (root) process.
func RunEmulatorDirect(ctx context.Context, cfg EmuConfig, cmdCh <-chan EmuCommand, emit func(EmuEvent)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec := cfg.EffectiveSpec()
	hciName := strings.TrimSpace(cfg.HCI)
	if hciName == "" {
		hciName = "hci0"
	}
	if _, norm, err := hciNumber(hciName); err == nil {
		hciName = norm
	}
	_ = prepareCarwhispererAdapter(hciName)

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect system dbus: %w", err)
	}
	defer conn.Close()

	adapterPath := dbus.ObjectPath("/org/bluez/" + hciName)
	adapterObj := conn.Object(bluezService, adapterPath)

	logf := func(format string, args ...any) {
		if emit != nil {
			emit(EmuEvent{Kind: "log", Text: fmt.Sprintf(format, args...)})
		}
	}

	// Save & configure adapter properties so we restore them cleanly on stop.
	restoreAdapter := configureAdapter(conn, adapterObj, hciName, spec.DefaultName, logf)
	defer restoreAdapter()

	// Register pairing agent (JustWorks auto-accept + trust).
	agent := &emuAgent{conn: conn, logf: logf}
	unregAgent := registerEmuAgent(conn, agent, logf)
	defer unregAgent()

	// Watch peer connections/disconnections on this adapter.
	stopPeerWatch := watchPeerConnections(ctx, conn, adapterPath, emit, logf)
	defer stopPeerWatch()

	var (
		hidApp   *hidGattApp
		a2dpSink *a2dpSinkService
		hfpSrv   *hfpProfileService
		txCount  int
		txMu     sync.Mutex
	)

	// Register HID GATT application if enabled.
	if spec.IsHID {
		hidApp = newHIDGattApp(conn, spec, func(leds uint8) {
			if emit != nil {
				emit(EmuEvent{Kind: "state", LEDs: leds})
			}
		}, logf)
		if err := hidApp.register(adapterObj); err != nil {
			return fmt.Errorf("register GATT HID: %w", err)
		}
		defer hidApp.unregister(adapterObj)
		logf("GATT HID service active (%s)", spec.Label)
	}

	// Register A2DP Sink (and optional HFP) if enabled.
	if spec.IsAudio {
		var err error
		a2dpSink, err = newA2DPSinkService(ctx, conn, adapterPath, cfg, emit, logf)
		if err != nil {
			return fmt.Errorf("register A2DP sink: %w", err)
		}
		defer a2dpSink.close()
		logf("A2DP Sink active (SBC)")

		if spec.HasHFP {
			hfpSrv = newHFPProfileService(conn, logf)
			if herr := hfpSrv.register(); herr != nil {
				logf("HFP note: %v", herr)
			} else {
				defer hfpSrv.unregister()
				logf("HFP Hands-Free active")
			}
		}
	}

	// Register BLE Advertisement (for HID / Custom / discoverability).
	var uuids []string
	if spec.IsHID {
		uuids = []string{"1812"}
	} else if spec.IsAudio {
		uuids = []string{"110b"}
	}
	if u := strings.TrimSpace(cfg.CustomUUID); u != "" {
		uuids = []string{u}
	}
	unregAdv := registerLEAdvertisement(conn, adapterObj, spec.DefaultName, spec.Appearance, uuids, logf)
	defer unregAdv()

	// Set Class of Device after BlueZ profile/endpoint/advertisement registration
	// so BlueZ doesn't overwrite Major/Minor device class during service init.
	if spec.CoD != 0 {
		setHCIClass(hciName, fmt.Sprintf("0x%06x", spec.CoD), logf)
	}

	transportLabel := "BLE Adv"
	switch {
	case spec.IsHID:
		transportLabel = "BLE HID"
	case spec.IsAudio && spec.HasHFP:
		transportLabel = "A2DP+HFP"
	case spec.IsAudio:
		transportLabel = "A2DP SBC"
	}

	if emit != nil {
		emit(EmuEvent{
			Kind:      "state",
			Ready:     true,
			Transport: transportLabel,
			MonitorOn: cfg.MonitorAudio,
			Recording: cfg.RecordAudio,
		})
	}
	logf("Advertising %q on %s (CoD 0x%06X)", spec.DefaultName, hciName, spec.CoD)

	bumpTX := func() {
		txMu.Lock()
		txCount++
		n := txCount
		txMu.Unlock()
		if emit != nil {
			emit(EmuEvent{Kind: "stats", TXCount: n})
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case cmd, ok := <-cmdCh:
			if !ok {
				return nil
			}
			switch cmd.Op {
			case "stop":
				return nil
			case "key":
				if hidApp != nil && spec.HasKB {
					code, mods := cmd.Code, cmd.Mods
					if code == 0 && cmd.Key != "" {
						c, m, ok := KeyToHID(cmd.Key)
						if ok {
							code, mods = c, m
						}
					}
					if code != 0 || mods != 0 {
						if err := hidApp.sendKeyTap(code, mods); err != nil {
							logf("HID key error: %v", err)
						} else {
							bumpTX()
						}
					}
				}
			case "text":
				if hidApp != nil && spec.HasKB && cmd.Text != "" {
					for _, r := range cmd.Text {
						if ctx.Err() != nil {
							break
						}
						if c, m, ok := RuneToHID(r); ok {
							if err := hidApp.sendKeyTap(c, m); err != nil {
								logf("HID text error: %v", err)
								break
							}
							bumpTX()
							time.Sleep(25 * time.Millisecond)
						}
					}
				}
			case "mouse":
				if hidApp != nil && spec.HasMouse {
					if err := hidApp.sendMouse(cmd.Buttons, cmd.DX, cmd.DY, cmd.Wheel); err != nil {
						logf("HID mouse error: %v", err)
					} else {
						bumpTX()
					}
				}
			case "mouse_click":
				if hidApp != nil && spec.HasMouse {
					if err := hidApp.sendMouse(cmd.Buttons, 0, 0, 0); err != nil {
						logf("HID mouse error: %v", err)
					} else {
						time.Sleep(25 * time.Millisecond)
						_ = hidApp.sendMouse(0, 0, 0, 0)
						bumpTX()
					}
				}
			case "gamepad":
				if hidApp != nil && spec.HasGamepad {
					if err := hidApp.sendGamepad(cmd.GBtn, cmd.DX, cmd.DY); err != nil {
						logf("HID gamepad error: %v", err)
					} else {
						bumpTX()
					}
				}
			case "consumer":
				if hidApp != nil && spec.HasConsumer && cmd.Usage != 0 {
					if err := hidApp.sendConsumer(cmd.Usage); err != nil {
						logf("HID media error: %v", err)
					} else {
						bumpTX()
					}
				}
			case "monitor":
				if a2dpSink != nil {
					a2dpSink.setMonitor(cmd.Enabled)
				}
			case "record":
				if a2dpSink != nil {
					a2dpSink.setRecording(cmd.Enabled)
				}
			case "avrcp":
				if err := sendAVRCPCommand(conn, adapterPath, cmd.Action); err != nil {
					logf("AVRCP %s: %v", cmd.Action, err)
				} else {
					bumpTX()
					logf("AVRCP sent: %s", cmd.Action)
				}
			}
		}
	}
}

func configureAdapter(conn *dbus.Conn, adapter dbus.BusObject, hciName, alias string, logf func(string, ...any)) func() {
	getProp := func(name string) (dbus.Variant, bool) {
		var v dbus.Variant
		err := adapter.Call(dbusPropsIface+".Get", 0, bluezAdapterIface, name).Store(&v)
		return v, err == nil
	}
	setProp := func(name string, val any) error {
		return adapter.Call(dbusPropsIface+".Set", 0, bluezAdapterIface, name, dbus.MakeVariant(val)).Err
	}

	origAlias, hasAlias := getProp("Alias")
	origDisc, hasDisc := getProp("Discoverable")
	origPair, hasPair := getProp("Pairable")
	origDiscTO, hasDiscTO := getProp("DiscoverableTimeout")
	origCoD := readHCIClass(hciName)

	_ = setProp("Powered", true)
	if strings.TrimSpace(alias) != "" {
		if err := setProp("Alias", alias); err != nil {
			logf("Set Alias warning: %v", err)
		}
	}
	_ = setProp("PairableTimeout", uint32(0))
	_ = setProp("Pairable", true)
	_ = setProp("DiscoverableTimeout", uint32(0))
	_ = setProp("Discoverable", true)

	return func() {
		if hasAlias {
			_ = adapter.Call(dbusPropsIface+".Set", 0, bluezAdapterIface, "Alias", origAlias).Err
		}
		if hasDisc {
			_ = adapter.Call(dbusPropsIface+".Set", 0, bluezAdapterIface, "Discoverable", origDisc).Err
		}
		if hasPair {
			_ = adapter.Call(dbusPropsIface+".Set", 0, bluezAdapterIface, "Pairable", origPair).Err
		}
		if hasDiscTO {
			_ = adapter.Call(dbusPropsIface+".Set", 0, bluezAdapterIface, "DiscoverableTimeout", origDiscTO).Err
		}
		if origCoD != "" {
			setHCIClass(hciName, origCoD, nil)
		}
	}
}

func readHCIClass(hciName string) string {
	if _, err := exec.LookPath("hciconfig"); err != nil {
		return ""
	}
	out, err := exec.Command("hciconfig", hciName, "class").CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if idx := strings.Index(line, "Class:"); idx >= 0 {
			fields := strings.Fields(line[idx+len("Class:"):])
			if len(fields) > 0 && strings.HasPrefix(fields[0], "0x") {
				return fields[0]
			}
		}
	}
	return ""
}

func setHCIClass(hciName, classHex string, logf func(string, ...any)) {
	if _, err := exec.LookPath("hciconfig"); err != nil {
		return
	}
	if out, err := exec.Command("hciconfig", hciName, "class", classHex).CombinedOutput(); err != nil && logf != nil {
		logf("Class %s warning: %s", classHex, strings.TrimSpace(string(out)))
	}
}

// --- Pairing Agent (org.bluez.Agent1) ---

type emuAgent struct {
	conn *dbus.Conn
	logf func(string, ...any)
}

func (a *emuAgent) Release() *dbus.Error { return nil }

func (a *emuAgent) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	a.logf("Agent PIN 0000 for %s", device)
	a.trustDevice(device)
	return "0000", nil
}

func (a *emuAgent) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	a.logf("Agent display PIN %s for %s", pincode, device)
	return nil
}

func (a *emuAgent) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	a.logf("Agent passkey 000000 for %s", device)
	a.trustDevice(device)
	return 0, nil
}

func (a *emuAgent) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	return nil
}

func (a *emuAgent) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	a.logf("Agent confirmed pairing (%06d)", passkey)
	a.trustDevice(device)
	return nil
}

func (a *emuAgent) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	a.logf("Agent authorized %s", device)
	a.trustDevice(device)
	return nil
}

func (a *emuAgent) AuthorizeService(device dbus.ObjectPath, uuid string) *dbus.Error {
	a.trustDevice(device)
	return nil
}

func (a *emuAgent) Cancel() *dbus.Error { return nil }

func (a *emuAgent) trustDevice(device dbus.ObjectPath) {
	go func() {
		obj := a.conn.Object(bluezService, device)
		_ = obj.Call(dbusPropsIface+".Set", 0, bluezDeviceIface, "Trusted", dbus.MakeVariant(true)).Err
	}()
}

func registerEmuAgent(conn *dbus.Conn, agent *emuAgent, logf func(string, ...any)) func() {
	if err := conn.Export(agent, dbusAgentPath, "org.bluez.Agent1"); err != nil {
		if logf != nil {
			logf("Agent export warning: %v", err)
		}
		return func() {}
	}
	mgr := conn.Object(bluezService, "/org/bluez")
	if err := mgr.Call(bluezAgentMgrIface+".RegisterAgent", 0, dbusAgentPath, "NoInputNoOutput").Err; err != nil {
		if logf != nil {
			logf("RegisterAgent warning: %v", err)
		}
		return func() {}
	}
	_ = mgr.Call(bluezAgentMgrIface+".RequestDefaultAgent", 0, dbusAgentPath).Err
	return func() {
		_ = mgr.Call(bluezAgentMgrIface+".UnregisterAgent", 0, dbusAgentPath).Err
	}
}

// --- LE Advertisement (org.bluez.LEAdvertisement1) ---

type emuAdvertisement struct{}

func (a *emuAdvertisement) Release() *dbus.Error { return nil }

func registerLEAdvertisement(conn *dbus.Conn, adapter dbus.BusObject, name string, appearance uint16, uuids []string, logf func(string, ...any)) func() {
	adv := &emuAdvertisement{}
	if err := conn.Export(adv, dbusAdvPath, "org.bluez.LEAdvertisement1"); err != nil {
		if logf != nil {
			logf("Adv export warning: %v", err)
		}
		return func() {}
	}

	tryRegister := func(localName string, app uint16, disc bool, svcUUIDs []string) error {
		pmap := map[string]*prop.Prop{
			"Type": {Value: "peripheral", Emit: prop.EmitConst},
		}
		if len(svcUUIDs) > 0 {
			pmap["ServiceUUIDs"] = &prop.Prop{Value: svcUUIDs, Emit: prop.EmitConst}
		}
		if strings.TrimSpace(localName) != "" {
			pmap["LocalName"] = &prop.Prop{Value: localName, Emit: prop.EmitConst}
		}
		if app != 0 {
			pmap["Appearance"] = &prop.Prop{Value: app, Emit: prop.EmitConst}
		}
		if disc {
			pmap["Discoverable"] = &prop.Prop{Value: true, Emit: prop.EmitConst}
		}
		if _, err := prop.Export(conn, dbusAdvPath, prop.Map{"org.bluez.LEAdvertisement1": pmap}); err != nil {
			return err
		}
		return adapter.Call(bluezLEAdvMgrIface+".RegisterAdvertisement", 0, dbusAdvPath, map[string]dbus.Variant{}).Err
	}

	shortName := name
	if len(shortName) > 16 {
		shortName = shortName[:16]
	}

	var regErr error
	attempts := []struct {
		name string
		app  uint16
		disc bool
		uuid []string
	}{
		{name, appearance, true, uuids},
		{shortName, appearance, true, uuids},
		{shortName, appearance, false, uuids},
		{shortName, 0, false, uuids},
		{shortName, 0, false, nil},
	}
	for _, a := range attempts {
		regErr = tryRegister(a.name, a.app, a.disc, a.uuid)
		if regErr == nil {
			break
		}
	}
	if regErr != nil && logf != nil {
		logf("LE adv warning: %v", regErr)
		return func() {}
	}
	return func() {
		_ = adapter.Call(bluezLEAdvMgrIface+".UnregisterAdvertisement", 0, dbusAdvPath).Err
	}
}

// --- Peer connection watcher ---

func watchPeerConnections(ctx context.Context, conn *dbus.Conn, adapterPath dbus.ObjectPath, emit func(EmuEvent), logf func(string, ...any)) func() {
	if err := conn.AddMatchSignal(
		dbus.WithMatchSender(bluezService),
		dbus.WithMatchInterface(dbusPropsIface),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchArg(0, bluezDeviceIface),
	); err != nil {
		return func() {}
	}
	ch := make(chan *dbus.Signal, 32)
	conn.Signal(ch)

	prefix := string(adapterPath) + "/dev_"
	done := make(chan struct{})
	go func() {
		defer conn.RemoveSignal(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case sig, ok := <-ch:
				if !ok {
					return
				}
				if !strings.HasPrefix(string(sig.Path), prefix) || len(sig.Body) < 2 {
					continue
				}
				changed, ok := sig.Body[1].(map[string]dbus.Variant)
				if !ok {
					continue
				}
				connVar, hasConn := changed["Connected"]
				if !hasConn {
					continue
				}
				connected, _ := connVar.Value().(bool)
				addr, name := readDeviceInfo(conn, sig.Path)
				if emit != nil {
					emit(EmuEvent{
						Kind:      "peer",
						Connected: connected,
						PeerAddr:  addr,
						PeerName:  name,
					})
				}
				if logf != nil {
					if connected {
						logf("Connected: %s (%s)", name, addr)
					} else {
						logf("Disconnected: %s (%s)", name, addr)
					}
				}
			}
		}
	}()

	return func() {
		close(done)
	}
}

func readDeviceInfo(conn *dbus.Conn, devPath dbus.ObjectPath) (addr, name string) {
	obj := conn.Object(bluezService, devPath)
	var props map[string]dbus.Variant
	if err := obj.Call(dbusPropsIface+".GetAll", 0, bluezDeviceIface).Store(&props); err == nil {
		if v, ok := props["Address"]; ok {
			addr, _ = v.Value().(string)
		}
		if v, ok := props["Alias"]; ok {
			name, _ = v.Value().(string)
		}
		if name == "" {
			if v, ok := props["Name"]; ok {
				name, _ = v.Value().(string)
			}
		}
	}
	if addr == "" {
		base := string(devPath)
		if idx := strings.LastIndex(base, "/dev_"); idx >= 0 {
			addr = strings.ReplaceAll(base[idx+5:], "_", ":")
		}
	}
	if name == "" {
		name = addr
	}
	return addr, name
}

// --- GATT HID Application ---

type hidGattApp struct {
	conn *dbus.Conn
	spec EmuProfileSpec
	logf func(string, ...any)

	objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

	kbChar     *gattChar
	mouseChar  *gattChar
	consChar   *gattChar
	padChar    *gattChar
	batChar    *gattChar
	lastButton uint8
}

func newHIDGattApp(conn *dbus.Conn, spec EmuProfileSpec, onLEDs func(uint8), logf func(string, ...any)) *hidGattApp {
	app := &hidGattApp{
		conn:    conn,
		spec:    spec,
		logf:    logf,
		objects: make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant),
	}

	svc0 := dbus.ObjectPath(string(dbusAppPath) + "/service0")
	app.addService(svc0, uuidHIDService, true)

	charIdx := 0
	nextChar := func() dbus.ObjectPath {
		p := dbus.ObjectPath(fmt.Sprintf("%s/char%d", svc0, charIdx))
		charIdx++
		return p
	}

	// Protocol Mode (0x2A4E): Report mode = 1.
	app.addChar(&gattChar{
		conn: conn, path: nextChar(), service: svc0,
		uuid: uuidProtocolMode, flags: []string{"read", "write-without-response"},
		value: []byte{0x01},
	})
	// HID Information (0x2A4A): HID v1.11, country 0, RemoteWake | NormallyConnectable.
	app.addChar(&gattChar{
		conn: conn, path: nextChar(), service: svc0,
		uuid: uuidHIDInfo, flags: []string{"read"},
		value: []byte{0x11, 0x01, 0x00, 0x03},
	})
	// Report Map (0x2A4B).
	reportMap := BuildHIDReportMap(spec.HasKB, spec.HasMouse, spec.HasConsumer, spec.HasGamepad)
	app.addChar(&gattChar{
		conn: conn, path: nextChar(), service: svc0,
		uuid: uuidReportMap, flags: []string{"read"},
		value: reportMap,
	})
	// HID Control Point (0x2A4C).
	app.addChar(&gattChar{
		conn: conn, path: nextChar(), service: svc0,
		uuid: uuidHIDControlPoint, flags: []string{"write-without-response"},
		value: []byte{0x00},
	})

	if spec.HasKB {
		p := nextChar()
		app.kbChar = &gattChar{
			conn: conn, path: p, service: svc0,
			uuid: uuidHIDReport, flags: []string{"read", "notify"},
			value: make([]byte, 8),
		}
		app.addChar(app.kbChar)
		app.addDesc(p, "desc0", uuidReportRef, []string{"read"}, []byte{HIDReportIDKeyboard, 0x01})

		// Keyboard Output Report (LEDs).
		outPath := nextChar()
		app.addChar(&gattChar{
			conn: conn, path: outPath, service: svc0,
			uuid: uuidHIDReport, flags: []string{"read", "write", "write-without-response"},
			value: []byte{0x00},
			onWrite: func(v []byte) {
				if len(v) > 0 && onLEDs != nil {
					onLEDs(v[0])
				}
			},
		})
		app.addDesc(outPath, "desc0", uuidReportRef, []string{"read"}, []byte{HIDReportIDKeyboard, 0x02})
	}
	if spec.HasMouse {
		p := nextChar()
		app.mouseChar = &gattChar{
			conn: conn, path: p, service: svc0,
			uuid: uuidHIDReport, flags: []string{"read", "notify"},
			value: make([]byte, 4),
		}
		app.addChar(app.mouseChar)
		app.addDesc(p, "desc0", uuidReportRef, []string{"read"}, []byte{HIDReportIDMouse, 0x01})
	}
	if spec.HasConsumer {
		p := nextChar()
		app.consChar = &gattChar{
			conn: conn, path: p, service: svc0,
			uuid: uuidHIDReport, flags: []string{"read", "notify"},
			value: make([]byte, 2),
		}
		app.addChar(app.consChar)
		app.addDesc(p, "desc0", uuidReportRef, []string{"read"}, []byte{HIDReportIDConsumer, 0x01})
	}
	if spec.HasGamepad {
		p := nextChar()
		app.padChar = &gattChar{
			conn: conn, path: p, service: svc0,
			uuid: uuidHIDReport, flags: []string{"read", "notify"},
			value: make([]byte, 4),
		}
		app.addChar(app.padChar)
		app.addDesc(p, "desc0", uuidReportRef, []string{"read"}, []byte{HIDReportIDGamepad, 0x01})
	}

	// Battery Service (0x180F)
	svc1 := dbus.ObjectPath(string(dbusAppPath) + "/service1")
	app.addService(svc1, uuidBatteryService, true)
	app.batChar = &gattChar{
		conn: conn, path: dbus.ObjectPath(string(svc1) + "/char0"), service: svc1,
		uuid: uuidBatteryLevel, flags: []string{"read", "notify"},
		value: []byte{100},
	}
	app.addChar(app.batChar)

	// Device Information Service (0x180A)
	svc2 := dbus.ObjectPath(string(dbusAppPath) + "/service2")
	app.addService(svc2, uuidDevInfoService, true)
	app.addChar(&gattChar{
		conn: conn, path: dbus.ObjectPath(string(svc2) + "/char0"), service: svc2,
		uuid: uuidPnPID, flags: []string{"read"},
		value: []byte{0x02, 0x6B, 0x1D, 0x01, 0x00, 0x00, 0x01},
	})

	return app
}

func (a *hidGattApp) addService(path dbus.ObjectPath, uuid string, primary bool) {
	svc := &gattService{}
	_ = a.conn.Export(svc, path, "org.bluez.GattService1")
	_, _ = prop.Export(a.conn, path, prop.Map{
		"org.bluez.GattService1": {
			"UUID":    {Value: uuid, Emit: prop.EmitConst},
			"Primary": {Value: primary, Emit: prop.EmitConst},
		},
	})
	a.objects[path] = map[string]map[string]dbus.Variant{
		"org.bluez.GattService1": {
			"UUID":    dbus.MakeVariant(uuid),
			"Primary": dbus.MakeVariant(primary),
		},
	}
}

func (a *hidGattApp) addChar(c *gattChar) {
	_ = a.conn.Export(c, c.path, "org.bluez.GattCharacteristic1")
	props, _ := prop.Export(a.conn, c.path, prop.Map{
		"org.bluez.GattCharacteristic1": {
			"UUID":      {Value: c.uuid, Emit: prop.EmitConst},
			"Service":   {Value: c.service, Emit: prop.EmitConst},
			"Flags":     {Value: c.flags, Emit: prop.EmitConst},
			"Value":     {Value: c.value, Emit: prop.EmitTrue},
			"Notifying": {Value: false, Emit: prop.EmitTrue},
		},
	})
	c.props = props
	a.objects[c.path] = map[string]map[string]dbus.Variant{
		"org.bluez.GattCharacteristic1": {
			"UUID":      dbus.MakeVariant(c.uuid),
			"Service":   dbus.MakeVariant(c.service),
			"Flags":     dbus.MakeVariant(c.flags),
			"Value":     dbus.MakeVariant(c.value),
			"Notifying": dbus.MakeVariant(false),
		},
	}
}

func (a *hidGattApp) addDesc(charPath dbus.ObjectPath, suffix, uuid string, flags []string, val []byte) {
	p := dbus.ObjectPath(string(charPath) + "/" + suffix)
	d := &gattDesc{value: val}
	_ = a.conn.Export(d, p, "org.bluez.GattDescriptor1")
	_, _ = prop.Export(a.conn, p, prop.Map{
		"org.bluez.GattDescriptor1": {
			"UUID":           {Value: uuid, Emit: prop.EmitConst},
			"Characteristic": {Value: charPath, Emit: prop.EmitConst},
			"Flags":          {Value: flags, Emit: prop.EmitConst},
			"Value":          {Value: val, Emit: prop.EmitFalse},
		},
	})
	a.objects[p] = map[string]map[string]dbus.Variant{
		"org.bluez.GattDescriptor1": {
			"UUID":           dbus.MakeVariant(uuid),
			"Characteristic": dbus.MakeVariant(charPath),
			"Flags":          dbus.MakeVariant(flags),
			"Value":          dbus.MakeVariant(val),
		},
	}
}

func (a *hidGattApp) GetManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
	return a.objects, nil
}

func (a *hidGattApp) register(adapter dbus.BusObject) error {
	if err := a.conn.Export(a, dbusAppPath, dbusObjMgrIface); err != nil {
		return err
	}
	_ = a.conn.Export(introspect.NewIntrospectable(&introspect.Node{
		Name: string(dbusAppPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{Name: dbusObjMgrIface},
		},
	}), dbusAppPath, "org.freedesktop.DBus.Introspectable")

	return adapter.Call(bluezGattMgrIface+".RegisterApplication", 0, dbusAppPath, map[string]dbus.Variant{}).Err
}

func (a *hidGattApp) unregister(adapter dbus.BusObject) {
	_ = adapter.Call(bluezGattMgrIface+".UnregisterApplication", 0, dbusAppPath).Err
}

func (a *hidGattApp) sendKeyTap(code, mods uint8) error {
	if a.kbChar == nil {
		return errors.New("keyboard report not active")
	}
	down := []byte{mods, 0x00, code, 0, 0, 0, 0, 0}
	if err := a.kbChar.notify(down); err != nil {
		return err
	}
	time.Sleep(12 * time.Millisecond)
	up := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	return a.kbChar.notify(up)
}

func (a *hidGattApp) sendMouse(buttons uint8, dx, dy, wheel int8) error {
	if a.mouseChar == nil {
		return errors.New("mouse report not active")
	}
	a.lastButton = buttons
	rep := []byte{buttons, byte(dx), byte(dy), byte(wheel)}
	return a.mouseChar.notify(rep)
}

func (a *hidGattApp) sendGamepad(buttons uint16, dx, dy int8) error {
	if a.padChar == nil {
		return errors.New("gamepad report not active")
	}
	rep := []byte{byte(buttons), byte(buttons >> 8), byte(dx), byte(dy)}
	if err := a.padChar.notify(rep); err != nil {
		return err
	}
	if buttons != 0 {
		time.Sleep(45 * time.Millisecond)
		return a.padChar.notify([]byte{0, 0, byte(dx), byte(dy)})
	}
	return nil
}

func (a *hidGattApp) sendConsumer(usage uint16) error {
	if a.consChar == nil {
		return errors.New("consumer report not active")
	}
	down := []byte{byte(usage), byte(usage >> 8)}
	if err := a.consChar.notify(down); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	return a.consChar.notify([]byte{0, 0})
}

type gattService struct{}

type gattChar struct {
	conn    *dbus.Conn
	path    dbus.ObjectPath
	service dbus.ObjectPath
	uuid    string
	flags   []string
	props   *prop.Properties
	onWrite func([]byte)

	mu        sync.Mutex
	value     []byte
	notifying bool
}

func (c *gattChar) ReadValue(options map[string]dbus.Variant) ([]byte, *dbus.Error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(c.value))
	copy(cp, c.value)
	return cp, nil
}

func (c *gattChar) WriteValue(value []byte, options map[string]dbus.Variant) *dbus.Error {
	c.mu.Lock()
	c.value = append([]byte(nil), value...)
	cb := c.onWrite
	c.mu.Unlock()
	if cb != nil {
		cb(value)
	}
	return nil
}

func (c *gattChar) StartNotify() *dbus.Error {
	c.mu.Lock()
	c.notifying = true
	c.mu.Unlock()
	if c.props != nil {
		c.props.SetMust("org.bluez.GattCharacteristic1", "Notifying", true)
	}
	return nil
}

func (c *gattChar) StopNotify() *dbus.Error {
	c.mu.Lock()
	c.notifying = false
	c.mu.Unlock()
	if c.props != nil {
		c.props.SetMust("org.bluez.GattCharacteristic1", "Notifying", false)
	}
	return nil
}

func (c *gattChar) notify(val []byte) error {
	c.mu.Lock()
	c.value = append([]byte(nil), val...)
	c.mu.Unlock()
	if c.props != nil {
		c.props.SetMust("org.bluez.GattCharacteristic1", "Value", append([]byte(nil), val...))
		return nil
	}
	return c.conn.Emit(c.path, dbusPropsIface+".PropertiesChanged",
		"org.bluez.GattCharacteristic1",
		map[string]dbus.Variant{"Value": dbus.MakeVariant(val)},
		[]string{})
}

type gattDesc struct {
	mu    sync.Mutex
	value []byte
}

func (d *gattDesc) ReadValue(options map[string]dbus.Variant) ([]byte, *dbus.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := make([]byte, len(d.value))
	copy(cp, d.value)
	return cp, nil
}

func (d *gattDesc) WriteValue(value []byte, options map[string]dbus.Variant) *dbus.Error {
	d.mu.Lock()
	d.value = append([]byte(nil), value...)
	d.mu.Unlock()
	return nil
}
