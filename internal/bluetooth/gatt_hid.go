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

	var (
		hidApp   *hidGattApp
		a2dpSink *a2dpSinkService
		hfpSrv   *hfpProfileService
		txCount  int
		txMu     sync.Mutex
	)

	// Register HID GATT application if enabled.
	if spec.IsHID {
		build := func(secure bool) *hidGattApp {
			return newHIDGattApp(conn, spec, secure, func(leds uint8) {
				if emit != nil {
					emit(EmuEvent{Kind: "led", LEDs: leds})
				}
			}, logf)
		}
		hidApp = build(true)
		if err := hidApp.register(adapterObj); err != nil {
			hidApp.unregister(adapterObj)
			logf("Secure HID rejected (%v), falling back to an open link", err)
			hidApp = build(false)
			if err2 := hidApp.register(adapterObj); err2 != nil {
				return fmt.Errorf("register GATT HID: %w", err2)
			}
		}
		defer hidApp.unregister(adapterObj)
		if hidApp.secure {
			logf("GATT HID service active (%s), reports need a paired link", spec.Label)
		} else {
			logf("GATT HID service active (%s), reports on an open link", spec.Label)
		}
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

	// Register the BLE Advertisement before watching peers: the watcher needs to
	// be able to re-arm advertising the moment a peer drops off.
	var uuids []string
	if spec.IsHID {
		uuids = []string{"1812"}
	} else if spec.IsAudio {
		uuids = []string{"110b"}
	}
	if u := strings.TrimSpace(cfg.CustomUUID); u != "" {
		uuids = []string{u}
	}
	adv := newLEAdvertiser(conn, adapterObj, spec.DefaultName, spec.Appearance, uuids, logf)
	advErr := adv.register()
	if advErr != nil {
		logf("LE adv warning: %v", advErr)
	}
	stopAdvKick := adv.start(ctx)
	if advErr != nil {
		adv.kickRearm() // retry once the controller has settled
	}
	defer stopAdvKick()
	defer adv.close()

	// Watch peer connections/disconnections. A disconnect re-arms advertising so
	// the host can reconnect (BlueZ does not always resume it on its own).
	stopPeerWatch := watchPeerConnections(ctx, conn, adapterPath, emit, adv.kickRearm, logf)
	defer stopPeerWatch()

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
	if _, err := exec.Command("hciconfig", hciName, "class", classHex).CombinedOutput(); err != nil && logf != nil {
		// bluetoothd owns the HCI socket, so hciconfig cannot write the class at
		// runtime. BLE hosts use the advertised Appearance instead, so this is
		// cosmetic (it only affects BR/EDR inquiry responses).
		logf("Note: BR/EDR device class left at the adapter default")
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

// leAdvertiser owns the BLE advertisement and keeps it alive. BlueZ stops
// advertising as soon as a central connects and only re-arms it if the
// application asks again, so without this the device disappears for good after
// the first connection.
type leAdvertiser struct {
	conn       *dbus.Conn
	adapter    dbus.BusObject
	path       dbus.ObjectPath
	name       string
	shortName  string
	appearance uint16
	uuids      []string
	logf       func(string, ...any)

	mu         sync.Mutex
	registered bool
	closed     bool
	kick       chan struct{}
}

func newLEAdvertiser(conn *dbus.Conn, adapter dbus.BusObject, name string, appearance uint16, uuids []string, logf func(string, ...any)) *leAdvertiser {
	shortName := name
	if len(shortName) > 16 {
		shortName = shortName[:16]
	}
	a := &leAdvertiser{
		conn:       conn,
		adapter:    adapter,
		path:       dbusAdvPath,
		name:       name,
		shortName:  shortName,
		appearance: appearance,
		uuids:      uuids,
		logf:       logf,
		kick:       make(chan struct{}, 1),
	}
	_ = conn.Export(a, dbusAdvPath, "org.bluez.LEAdvertisement1")
	return a
}

// Release is called by BlueZ when it drops the advertisement (for example after
// a peer connects). Re-arm so the host can reconnect later.
func (a *leAdvertiser) Release() *dbus.Error {
	a.mu.Lock()
	a.registered = false
	a.mu.Unlock()
	a.kickRearm()
	return nil
}

func (a *leAdvertiser) kickRearm() {
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return
	}
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

func (a *leAdvertiser) export(localName string, app uint16, disc bool, svc []string) error {
	pmap := map[string]*prop.Prop{
		"Type": {Value: "peripheral", Emit: prop.EmitConst},
	}
	if len(svc) > 0 {
		pmap["ServiceUUIDs"] = &prop.Prop{Value: svc, Emit: prop.EmitConst}
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
	_, err := prop.Export(a.conn, a.path, prop.Map{"org.bluez.LEAdvertisement1": pmap})
	return err
}

// registerLocked tries progressively simpler advertisement payloads until BlueZ
// accepts one (the controller rejects oversized or unsupported fields).
func (a *leAdvertiser) registerLocked() error {
	attempts := []struct {
		name string
		app  uint16
		disc bool
		uuid []string
	}{
		{a.name, a.appearance, true, a.uuids},
		{a.shortName, a.appearance, true, a.uuids},
		{a.shortName, a.appearance, false, a.uuids},
		{a.shortName, 0, false, a.uuids},
		{a.shortName, 0, false, nil},
	}
	var lastErr error
	for _, at := range attempts {
		if err := a.export(at.name, at.app, at.disc, at.uuid); err != nil {
			lastErr = err
			continue
		}
		err := a.adapter.Call(bluezLEAdvMgrIface+".RegisterAdvertisement", 0, a.path, map[string]dbus.Variant{}).Err
		if err == nil {
			a.registered = true
			return nil
		}
		lastErr = err
	}
	a.registered = false
	if lastErr == nil {
		lastErr = errors.New("no advertisement variant was accepted")
	}
	return lastErr
}

// register performs the first registration (called once at startup).
func (a *leAdvertiser) register() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.registerLocked()
}

// rearm unregisters and registers the advertisement again so a peer that just
// dropped off can reconnect.
func (a *leAdvertiser) rearm() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	wasRegistered := a.registered
	a.registered = false
	a.mu.Unlock()

	// Unregister outside the lock: BlueZ answers with Release(), which needs it.
	if wasRegistered {
		_ = a.adapter.Call(bluezLEAdvMgrIface+".UnregisterAdvertisement", 0, a.path).Err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	if err := a.registerLocked(); err != nil {
		if a.logf != nil {
			a.logf("LE adv rearm warning: %v", err)
		}
		return
	}
	if a.logf != nil {
		a.logf("Advertising again (waiting for a connection)")
	}
}

// start runs the re-arm worker and returns a stop function.
func (a *leAdvertiser) start(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-a.kick:
				// Give BlueZ a moment to finish the disconnect bookkeeping
				// before competing with it for the advertising instance.
				select {
				case <-ctx.Done():
					return
				case <-done:
					return
				case <-time.After(750 * time.Millisecond):
				}
				a.rearm()
			}
		}
	}()
	return func() { close(done) }
}

func (a *leAdvertiser) close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	reg := a.registered
	a.registered = false
	a.mu.Unlock()
	if reg {
		_ = a.adapter.Call(bluezLEAdvMgrIface+".UnregisterAdvertisement", 0, a.path).Err
	}
}

// --- Peer connection watcher ---

func watchPeerConnections(ctx context.Context, conn *dbus.Conn, adapterPath dbus.ObjectPath, emit func(EmuEvent), onDisconnect func(), logf func(string, ...any)) func() {
	rules := [][]dbus.MatchOption{
		{
			dbus.WithMatchSender(bluezService),
			dbus.WithMatchInterface(dbusPropsIface),
			dbus.WithMatchMember("PropertiesChanged"),
			dbus.WithMatchArg(0, bluezDeviceIface),
		},
		// ObjectManager signals come from "/" and carry the object path as the
		// first argument, so they must not be filtered by path namespace.
		{
			dbus.WithMatchSender(bluezService),
			dbus.WithMatchInterface(dbusObjMgrIface),
			dbus.WithMatchMember("InterfacesAdded"),
		},
		{
			dbus.WithMatchSender(bluezService),
			dbus.WithMatchInterface(dbusObjMgrIface),
			dbus.WithMatchMember("InterfacesRemoved"),
		},
	}
	for _, r := range rules {
		_ = conn.AddMatchSignal(r...)
	}

	ch := make(chan *dbus.Signal, 128)
	conn.Signal(ch)

	prefix := string(adapterPath) + "/dev_"
	state := make(map[dbus.ObjectPath]bool)
	var active dbus.ObjectPath
	done := make(chan struct{})

	var resolvedMu sync.Mutex
	resolved := make(map[dbus.ObjectPath]bool)
	markResolved := func(p dbus.ObjectPath) {
		resolvedMu.Lock()
		resolved[p] = true
		resolvedMu.Unlock()
	}
	isResolved := func(p dbus.ObjectPath) bool {
		resolvedMu.Lock()
		defer resolvedMu.Unlock()
		return resolved[p]
	}

	report := func(path dbus.ObjectPath, connected bool) {
		prev, seen := state[path]
		if seen && prev == connected {
			return
		}
		if len(state) > 96 {
			// Drop stale entries from long discovery runs.
			state = make(map[dbus.ObjectPath]bool)
		}
		state[path] = connected

		if connected {
			active = path
			// A BR/EDR connection never triggers GATT discovery, and that is the
			// most common reason a PC appears to connect and then drops us.
			go func(p dbus.ObjectPath) {
				select {
				case <-ctx.Done():
				case <-done:
				case <-time.After(6 * time.Second):
					if !isResolved(p) && logf != nil {
						logf("No GATT discovery yet: the host may have connected over Classic BR/EDR")
					}
				}
			}(path)
		} else if active != path {
			// Discovery churn for unrelated devices is not interesting.
			return
		} else {
			active = ""
		}

		addr, name := addrFromPath(path), ""
		if connected {
			if a, n := readDeviceInfo(conn, path); a != "" {
				addr, name = a, n
			}
		}
		if name == "" {
			name = addr
		}
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
		if !connected && onDisconnect != nil {
			onDisconnect()
		}
	}

	// Catch a peer that connected before we finished initialising.
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := conn.Object(bluezService, "/").Call(dbusObjMgrIface+".GetManagedObjects", 0).Store(&objs); err == nil {
		for p, ifaces := range objs {
			if !strings.HasPrefix(string(p), prefix) {
				continue
			}
			devProps, ok := ifaces[bluezDeviceIface]
			if !ok {
				continue
			}
			if v, ok2 := devProps["Connected"]; ok2 {
				if c, _ := v.Value().(bool); c {
					report(p, true)
				}
			}
			if v, ok2 := devProps["ServicesResolved"]; ok2 {
				if r, _ := v.Value().(bool); r {
					markResolved(p)
				}
			}
		}
	}

	go func() {
		defer conn.RemoveSignal(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case sig, ok := <-ch:
				if !ok || sig == nil {
					return
				}
				switch sig.Name {
				case "PropertiesChanged":
					if len(sig.Body) < 2 || !strings.HasPrefix(string(sig.Path), prefix) {
						continue
					}
					if iface, _ := sig.Body[0].(string); iface != bluezDeviceIface {
						continue
					}
					changed, ok := sig.Body[1].(map[string]dbus.Variant)
					if !ok {
						continue
					}
					if v, has := changed["Connected"]; has {
						c, _ := v.Value().(bool)
						report(sig.Path, c)
						continue
					}
					if v, has := changed["ServicesResolved"]; has {
						if r, _ := v.Value().(bool); r {
							markResolved(sig.Path)
							if logf != nil {
								logf("Host finished service discovery")
							}
						}
					}
				case "InterfacesAdded":
					if len(sig.Body) < 2 {
						continue
					}
					p, _ := sig.Body[0].(dbus.ObjectPath)
					if !strings.HasPrefix(string(p), prefix) {
						continue
					}
					objs, ok := sig.Body[1].(map[string]map[string]dbus.Variant)
					if !ok {
						continue
					}
					devProps, has := objs[bluezDeviceIface]
					if !has {
						continue
					}
					c := false
					if v, ok2 := devProps["Connected"]; ok2 {
						c, _ = v.Value().(bool)
					}
					report(p, c)
				case "InterfacesRemoved":
					if len(sig.Body) < 2 {
						continue
					}
					p, _ := sig.Body[0].(dbus.ObjectPath)
					if !strings.HasPrefix(string(p), prefix) {
						continue
					}
					ifaces, ok := sig.Body[1].([]string)
					if !ok {
						continue
					}
					for _, i := range ifaces {
						if i == bluezDeviceIface {
							report(p, false)
							break
						}
					}
				}
			}
		}
	}()

	return func() {
		close(done)
	}
}

// addrFromPath decodes the BD_ADDR BlueZ encodes in device object paths.
func addrFromPath(p dbus.ObjectPath) string {
	base := string(p)
	idx := strings.LastIndex(base, "/dev_")
	if idx < 0 {
		return ""
	}
	return strings.ReplaceAll(base[idx+5:], "_", ":")
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
		addr = addrFromPath(devPath)
	}
	if name == "" {
		name = addr
	}
	return addr, name
}

// --- GATT HID Application ---

type hidGattApp struct {
	conn   *dbus.Conn
	spec   EmuProfileSpec
	secure bool
	logf   func(string, ...any)

	objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

	kbChar     *gattChar
	mouseChar  *gattChar
	consChar   *gattChar
	padChar    *gattChar
	batChar    *gattChar
	lastButton uint8
}

// reportFlags builds the characteristic flags for a HID report. HOGP requires
// reports to live on an encrypted link, so secure mode marks them accordingly;
// open mode keeps the flags plain so a host without a working pairer can still
// read them.
func (a *hidGattApp) reportFlags(extras ...string) []string {
	flags := make([]string, 0, len(extras)+2)
	if a.secure {
		flags = append(flags, "encrypt-read")
	} else {
		flags = append(flags, "read")
	}
	for _, e := range extras {
		if a.secure && (e == "notify" || e == "write") {
			flags = append(flags, "encrypt-"+e)
			continue
		}
		flags = append(flags, e)
	}
	return flags
}

func newHIDGattApp(conn *dbus.Conn, spec EmuProfileSpec, secure bool, onLEDs func(uint8), logf func(string, ...any)) *hidGattApp {
	app := &hidGattApp{
		conn:    conn,
		spec:    spec,
		secure:  secure,
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
			uuid: uuidHIDReport, flags: app.reportFlags("notify"),
			value: make([]byte, 9), label: "Keyboard",
		}
		app.addChar(app.kbChar)
		app.addDesc(p, "desc0", uuidReportRef, []string{"read"}, []byte{HIDReportIDKeyboard, 0x01})

		// Keyboard Output Report (LEDs).
		outPath := nextChar()
		app.addChar(&gattChar{
			conn: conn, path: outPath, service: svc0,
			uuid: uuidHIDReport, flags: app.reportFlags("write", "write-without-response"),
			value: []byte{0x00}, label: "Keyboard LEDs",
			onWrite: func(v []byte) {
				if onLEDs == nil {
					return
				}
				// Output report layout: [report ID, LED bits].
				if len(v) >= 2 && v[0] == HIDReportIDKeyboard {
					onLEDs(v[1])
				} else if len(v) == 1 {
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
			uuid: uuidHIDReport, flags: app.reportFlags("notify"),
			value: make([]byte, 5), label: "Mouse",
		}
		app.addChar(app.mouseChar)
		app.addDesc(p, "desc0", uuidReportRef, []string{"read"}, []byte{HIDReportIDMouse, 0x01})
	}
	if spec.HasConsumer {
		p := nextChar()
		app.consChar = &gattChar{
			conn: conn, path: p, service: svc0,
			uuid: uuidHIDReport, flags: app.reportFlags("notify"),
			value: make([]byte, 3), label: "Media",
		}
		app.addChar(app.consChar)
		app.addDesc(p, "desc0", uuidReportRef, []string{"read"}, []byte{HIDReportIDConsumer, 0x01})
	}
	if spec.HasGamepad {
		p := nextChar()
		app.padChar = &gattChar{
			conn: conn, path: p, service: svc0,
			uuid: uuidHIDReport, flags: app.reportFlags("notify"),
			value: make([]byte, 5), label: "Gamepad",
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
	if c.logf == nil {
		c.logf = a.logf
	}
	if c.label == "" {
		c.label = charLabel(c.uuid)
	}
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

// Every report in the composite map carries a Report ID, so each notification
// payload is "<report ID><report bytes>". Hosts silently drop reports whose
// leading byte is not one of the advertised IDs, so build them in one place.

// keyboardReport builds Report ID 1: modifiers, reserved, 6 key slots.
func keyboardReport(mods, code uint8) []byte {
	return []byte{HIDReportIDKeyboard, mods, 0x00, code, 0, 0, 0, 0, 0}
}

// mouseReport builds Report ID 2: buttons, X, Y, wheel.
func mouseReport(buttons uint8, dx, dy, wheel int8) []byte {
	return []byte{HIDReportIDMouse, buttons, byte(dx), byte(dy), byte(wheel)}
}

// consumerReport builds Report ID 3: one 16-bit consumer usage.
func consumerReport(usage uint16) []byte {
	return []byte{HIDReportIDConsumer, byte(usage), byte(usage >> 8)}
}

// gamepadReport builds Report ID 4: 16 buttons, X, Y.
func gamepadReport(buttons uint16, dx, dy int8) []byte {
	return []byte{HIDReportIDGamepad, byte(buttons), byte(buttons >> 8), byte(dx), byte(dy)}
}

func (a *hidGattApp) sendKeyTap(code, mods uint8) error {
	if a.kbChar == nil {
		return errors.New("keyboard report not active")
	}
	down := keyboardReport(mods, code)
	if err := a.kbChar.notify(down); err != nil {
		return err
	}
	time.Sleep(12 * time.Millisecond)
	up := keyboardReport(0, 0)
	return a.kbChar.notify(up)
}

func (a *hidGattApp) sendMouse(buttons uint8, dx, dy, wheel int8) error {
	if a.mouseChar == nil {
		return errors.New("mouse report not active")
	}
	a.lastButton = buttons
	rep := mouseReport(buttons, dx, dy, wheel)
	return a.mouseChar.notify(rep)
}

func (a *hidGattApp) sendGamepad(buttons uint16, dx, dy int8) error {
	if a.padChar == nil {
		return errors.New("gamepad report not active")
	}
	rep := gamepadReport(buttons, dx, dy)
	if err := a.padChar.notify(rep); err != nil {
		return err
	}
	if buttons != 0 {
		time.Sleep(45 * time.Millisecond)
		return a.padChar.notify(gamepadReport(0, dx, dy))
	}
	return nil
}

func (a *hidGattApp) sendConsumer(usage uint16) error {
	if a.consChar == nil {
		return errors.New("consumer report not active")
	}
	down := consumerReport(usage)
	if err := a.consChar.notify(down); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	return a.consChar.notify(consumerReport(0))
}

type gattService struct{}

func charLabel(uuid string) string {
	switch uuid {
	case uuidProtocolMode:
		return "Protocol Mode"
	case uuidHIDInfo:
		return "HID Info"
	case uuidReportMap:
		return "Report Map"
	case uuidHIDControlPoint:
		return "Control Point"
	case uuidBatteryLevel:
		return "Battery"
	case uuidPnPID:
		return "PnP ID"
	}
	return ""
}

type gattChar struct {
	conn    *dbus.Conn
	path    dbus.ObjectPath
	service dbus.ObjectPath
	uuid    string
	flags   []string
	props   *prop.Properties
	onWrite func([]byte)
	label   string
	logf    func(string, ...any)

	mu        sync.Mutex
	value     []byte
	notifying bool
}

// attOffset extracts the ATT read/write offset from the options BlueZ passes to
// ReadValue/WriteValue.
func attOffset(options map[string]dbus.Variant) int {
	v, ok := options["offset"]
	if !ok {
		return 0
	}
	switch n := v.Value().(type) {
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case int32:
		return int(n)
	case int:
		return n
	}
	return 0
}

// readChunk applies the ATT Read Blob offset. Attributes larger than the
// negotiated MTU are read as a Read Request followed by Read Blob Requests with
// increasing offsets. A handler that ignores the offset replays the head of the
// value on every chunk, so the peer reassembles corrupt data and (for the HID
// Report Map) drops the link as soon as it parses the descriptor.
func readChunk(val []byte, options map[string]dbus.Variant) []byte {
	off := attOffset(options)
	if off <= 0 {
		return val
	}
	if off >= len(val) {
		return []byte{}
	}
	return val[off:]
}

func (c *gattChar) ReadValue(options map[string]dbus.Variant) ([]byte, *dbus.Error) {
	c.mu.Lock()
	cp := append([]byte(nil), c.value...)
	c.mu.Unlock()
	return readChunk(cp, options), nil
}

func (c *gattChar) WriteValue(value []byte, options map[string]dbus.Variant) *dbus.Error {
	c.mu.Lock()
	if off := attOffset(options); off > 0 {
		// Long/prepared write: overwrite in place at the requested offset.
		if off > len(c.value) {
			off = len(c.value)
		}
		buf := append([]byte(nil), c.value...)
		if need := off + len(value); need > len(buf) {
			buf = append(buf, make([]byte, need-len(buf))...)
		}
		copy(buf[off:], value)
		c.value = buf
	} else {
		c.value = append([]byte(nil), value...)
	}
	snapshot := append([]byte(nil), c.value...)
	cb := c.onWrite
	c.mu.Unlock()
	if cb != nil {
		cb(snapshot)
	}
	return nil
}

func (c *gattChar) StartNotify() *dbus.Error {
	c.mu.Lock()
	c.notifying = true
	logf := c.logf
	label := c.label
	c.mu.Unlock()
	if logf != nil && label != "" {
		logf("%s notifications enabled", label)
	}
	if c.props != nil {
		// Set (not SetMust) so a peer that vanished mid-request cannot panic.
		safePropSet(c.props, "org.bluez.GattCharacteristic1", "Notifying", true)
	}
	return nil
}

func (c *gattChar) StopNotify() *dbus.Error {
	c.mu.Lock()
	c.notifying = false
	logf := c.logf
	label := c.label
	c.mu.Unlock()
	if logf != nil && label != "" {
		logf("%s notifications disabled", label)
	}
	if c.props != nil {
		safePropSet(c.props, "org.bluez.GattCharacteristic1", "Notifying", false)
	}
	return nil
}

func (c *gattChar) notify(val []byte) error {
	c.mu.Lock()
	c.value = append([]byte(nil), val...)
	c.mu.Unlock()
	if c.props != nil {
		// Set (not SetMust): a disconnect races the emit and must not panic.
		safePropSet(c.props, "org.bluez.GattCharacteristic1", "Value", append([]byte(nil), val...))
		return nil
	}
	return c.conn.Emit(c.path, dbusPropsIface+".PropertiesChanged",
		"org.bluez.GattCharacteristic1",
		map[string]dbus.Variant{"Value": dbus.MakeVariant(val)},
		[]string{})
}

// safePropSet updates an exported property and emits PropertiesChanged (BlueZ
// forwards characteristic Value changes to subscribed peers this way). A peer
// that disconnects mid-emit surfaces as a panic from prop.SetMust, so it is
// recovered here instead of taking the whole emulator down.
func safePropSet(p *prop.Properties, iface, name string, v any) {
	if p == nil {
		return
	}
	defer func() { _ = recover() }()
	p.SetMust(iface, name, v)
}

type gattDesc struct {
	mu    sync.Mutex
	value []byte
}

func (d *gattDesc) ReadValue(options map[string]dbus.Variant) ([]byte, *dbus.Error) {
	d.mu.Lock()
	cp := append([]byte(nil), d.value...)
	d.mu.Unlock()
	return readChunk(cp, options), nil
}

func (d *gattDesc) WriteValue(value []byte, options map[string]dbus.Variant) *dbus.Error {
	d.mu.Lock()
	d.value = append([]byte(nil), value...)
	d.mu.Unlock()
	return nil
}
