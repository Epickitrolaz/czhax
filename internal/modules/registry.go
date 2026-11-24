package modules

import "strings"

type Category string

const (
	CatSec   Category = "sec"
	CatBasic Category = "basic"
)

type Status string

const (
	StatusReady  Status = "READY"
	StatusPhase2 Status = "PH2"
)

// ViewTarget identifies the left-sidebar view and horizontal sub-tab index for a module.
type ViewTarget struct {
	View int
	Tab  int
}

const (
	ViewDash  = 0
	ViewRecon = 1
	ViewNet   = 2
	ViewLab   = 3
	ViewSys   = 4
	ViewOpts  = 5
)

type Item struct {
	ID          string
	Name        string
	ShortName   string
	Tag         string
	Category    Category
	Status      Status
	Target      ViewTarget
	Summary     string
	Hardware    string
	Privileges  string
	PlannedOpts []string
}

func All() []Item {
	return []Item{
		// Recon (Left View: Recon, Top Tabs: WiFi | BLE | IR)
		{
			ID:          "wifi-recon",
			Name:        "Wi-Fi Recon",
			ShortName:   "WiFi",
			Tag:         "WIFI",
			Category:    CatSec,
			Status:      StatusPhase2,
			Target:      ViewTarget{View: ViewRecon, Tab: 0},
			Summary:     "AP/client survey, RSSI & channel map on wlan0.",
			Hardware:    "CM0 2.4GHz Wi-Fi / USB OTG",
			Privileges:  "root / CAP_NET_ADMIN",
			PlannedOpts: []string{"Passive AP Scan", "Channel Hop 1-14", "Client Probe Log"},
		},
		{
			ID:          "ble-scan",
			Name:        "BLE Scanner",
			ShortName:   "BLE",
			Tag:         "BLE",
			Category:    CatSec,
			Status:      StatusPhase2,
			Target:      ViewTarget{View: ViewRecon, Tab: 1},
			Summary:     "BLE advertisement & beacon monitor.",
			Hardware:    "CM0 Bluetooth",
			Privileges:  "bluetooth group",
			PlannedOpts: []string{"Beacon Monitor", "Device RSSI List", "Adv Inspector"},
		},
		{
			ID:          "ir-lab",
			Name:        "IR Tx/Rx Lab",
			ShortName:   "IR",
			Tag:         "IR",
			Category:    CatSec,
			Status:      StatusPhase2,
			Target:      ViewTarget{View: ViewRecon, Tab: 2},
			Summary:     "Cardputer Zero IR signal capture & replay.",
			Hardware:    "Built-in IR Tx + Rx",
			Privileges:  "lirc / gpio group",
			PlannedOpts: []string{"IR Raw Capture", "Replay Vault", "NEC/RC5 Decode"},
		},

		// Net (Left View: Net, Top Tabs: IFs | Scan | Pkt)
		{
			ID:         "net-if",
			Name:       "Net Interfaces",
			ShortName:  "IFs",
			Tag:        "NET",
			Category:   CatBasic,
			Status:     StatusReady,
			Target:     ViewTarget{View: ViewNet, Tab: 0},
			Summary:    "Live local NICs, IPs, MACs, MTU and link states.",
			Hardware:   "Linux netlink",
			Privileges: "standard user",
		},
		{
			ID:          "net-scan",
			Name:        "LAN & Port Scan",
			ShortName:   "Scan",
			Tag:         "SCAN",
			Category:    CatSec,
			Status:      StatusPhase2,
			Target:      ViewTarget{View: ViewNet, Tab: 1},
			Summary:     "Subnet ARP sweep & TCP port probe.",
			Hardware:    "Wi-Fi / SR9900A Ethernet",
			Privileges:  "standard (SYN needs root)",
			PlannedOpts: []string{"ARP Host Sweep", "Top-100 TCP Probe", "Banner Grab"},
		},
		{
			ID:          "pkt-dns",
			Name:        "Packet & DNS",
			ShortName:   "Pkt",
			Tag:         "PKT",
			Category:    CatSec,
			Status:      StatusPhase2,
			Target:      ViewTarget{View: ViewNet, Tab: 2},
			Summary:     "Live packet stats, DNS lookup & ARP watch.",
			Hardware:    "Any up net interface",
			Privileges:  "CAP_NET_RAW for capture",
			PlannedOpts: []string{"DNS Lookup", "ARP Spoof Watch", "Packet Summary"},
		},

		// Lab (Left View: Lab, Top Tabs: Crypto | HW Bus)
		{
			ID:          "creds-crypto",
			Name:        "Crypto & Hash",
			ShortName:   "Crypto",
			Tag:         "CRY",
			Category:    CatSec,
			Status:      StatusPhase2,
			Target:      ViewTarget{View: ViewLab, Tab: 0},
			Summary:     "Hash ID, Base64/Hex codec & passphrase gen.",
			Hardware:    "CPU only (offline)",
			Privileges:  "standard user",
			PlannedOpts: []string{"Hash Identifier", "B64 / Hex Codec", "Passphrase Gen"},
		},
		{
			ID:          "hw-gpio",
			Name:        "I2C / UART / Cap",
			ShortName:   "HW Bus",
			Tag:         "HW",
			Category:    CatSec,
			Status:      StatusPhase2,
			Target:      ViewTarget{View: ViewLab, Tab: 1},
			Summary:     "Probe CZ I2C1 (0x34/0x4F/0x68), Grove & EXT UART.",
			Hardware:    "I2C1 / UART0 / EXT 14P",
			Privileges:  "i2c / dialout group",
			PlannedOpts: []string{"I2C Bus Map", "Grove UART Term", "USB Host/Slave"},
		},

		// Sys (Left View: Sys, Top Tabs: Mon | Notes | Files)
		{
			ID:         "sys-mon",
			Name:       "System Monitor",
			ShortName:  "Mon",
			Tag:        "SYS",
			Category:   CatBasic,
			Status:     StatusReady,
			Target:     ViewTarget{View: ViewSys, Tab: 0},
			Summary:    "Live CPU load, RAM, disk, temp & battery.",
			Hardware:   "Linux /proc & /sys",
			Privileges: "standard user",
		},
		{
			ID:         "notes",
			Name:       "Quick Notes",
			ShortName:  "Notes",
			Tag:        "NOTE",
			Category:   CatBasic,
			Status:     StatusReady,
			Target:     ViewTarget{View: ViewSys, Tab: 1},
			Summary:    "Field notes saved in ~/.config/czhax/notes.txt.",
			Hardware:   "microSD storage",
			Privileges: "standard user",
		},
		{
			ID:         "files",
			Name:       "File Browser",
			ShortName:  "Files",
			Tag:        "FILE",
			Category:   CatBasic,
			Status:     StatusReady,
			Target:     ViewTarget{View: ViewSys, Tab: 2},
			Summary:    "Directory navigator and text file pager.",
			Hardware:   "local filesystem",
			Privileges: "standard user",
		},
	}
}

func FindByID(id string) (Item, bool) {
	for _, it := range All() {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

func FindByTarget(view, tab int) (Item, bool) {
	for _, it := range All() {
		if it.Target.View == view && it.Target.Tab == tab {
			return it, true
		}
	}
	return Item{}, false
}

func Search(query string) []Item {
	q := strings.ToLower(strings.TrimSpace(query))
	all := All()
	if q == "" {
		return all
	}
	var out []Item
	for _, it := range all {
		if strings.Contains(strings.ToLower(it.Name), q) ||
			strings.Contains(strings.ToLower(it.Tag), q) ||
			strings.Contains(strings.ToLower(it.Summary), q) ||
			strings.Contains(strings.ToLower(it.ID), q) {
			out = append(out, it)
		}
	}
	return out
}
