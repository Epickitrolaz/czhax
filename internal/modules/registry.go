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

type Item struct {
	ID          string
	Name        string
	ShortName   string
	Tag         string
	Category    Category
	Status      Status
	Summary     string
	Hardware    string
	Privileges  string
	PlannedOpts []string
}

func All() []Item {
	return []Item{
		// Cybersecurity Modules (Phase 2 roadmap + Phase 1 pre-flight views)
		{
			ID:          "wifi-recon",
			Name:        "Wi-Fi Recon",
			ShortName:   "WiFi",
			Tag:         "WIFI",
			Category:    CatSec,
			Status:      StatusPhase2,
			Summary:     "Passive AP/client survey, RSSI & channel map on wlan0/USB adapter.",
			Hardware:    "CM0 2.4GHz Wi-Fi / USB OTG adapter",
			Privileges:  "root / CAP_NET_ADMIN",
			PlannedOpts: []string{"Passive AP Scan", "Channel Hop 1-14", "Client Probe Log"},
		},
		{
			ID:          "net-scan",
			Name:        "LAN & Port Scan",
			ShortName:   "Scan",
			Tag:         "NET",
			Category:    CatSec,
			Status:      StatusPhase2,
			Summary:     "Fast subnet ARP sweep and TCP service probe for pocket recon.",
			Hardware:    "Wi-Fi / SR9900A 100M Ethernet",
			Privileges:  "standard (ARP/SYN needs root)",
			PlannedOpts: []string{"ARP Host Discovery", "Top-100 TCP Probe", "Banner Grab"},
		},
		{
			ID:          "ble-rf",
			Name:        "BLE & IR Lab",
			ShortName:   "BLE/IR",
			Tag:         "RF",
			Category:    CatSec,
			Status:      StatusPhase2,
			Summary:     "BLE advertisement scanner + Cardputer Zero IR Tx/Rx capture.",
			Hardware:    "CM0 BT + built-in IR Tx/Rx",
			Privileges:  "bluetooth / lirc group",
			PlannedOpts: []string{"BLE Beacon Monitor", "IR Raw Capture", "IR Replay Vault"},
		},
		{
			ID:          "pkt-dns",
			Name:        "Packet & DNS",
			ShortName:   "Pkt/DNS",
			Tag:         "PKT",
			Category:    CatSec,
			Status:      StatusPhase2,
			Summary:     "Compact live packet summary, DNS resolver & ARP watch.",
			Hardware:    "Any up net interface",
			Privileges:  "CAP_NET_RAW for capture",
			PlannedOpts: []string{"DNS Lookup", "ARP Spoof Watch", "Mini TShark Stats"},
		},
		{
			ID:          "creds-crypto",
			Name:        "Crypto & Hash",
			ShortName:   "Crypto",
			Tag:         "CRY",
			Category:    CatSec,
			Status:      StatusPhase2,
			Summary:     "Offline hash identifier, Base64/Hex codec & entropy meter.",
			Hardware:    "CPU only (offline)",
			Privileges:  "standard user",
			PlannedOpts: []string{"Hash ID", "B64 / Hex / URL Codec", "Passphrase Gen"},
		},
		{
			ID:          "hw-gpio",
			Name:        "I2C / UART / Cap",
			ShortName:   "HW Bus",
			Tag:         "HW",
			Category:    CatSec,
			Status:      StatusPhase2,
			Summary:     "Probe CZ I2C1 (0x34 KB, 0x4F IOE, 0x68 IMU), Grove & EXT UART.",
			Hardware:    "I2C1 / UART0 / EXT 14P",
			Privileges:  "i2c / dialout group",
			PlannedOpts: []string{"I2C Bus Map", "Grove UART Term", "USB Host/Slave Check"},
		},

		// Basic Features (Active in Phase 1!)
		{
			ID:         "sys-mon",
			Name:       "System Monitor",
			ShortName:  "SysMon",
			Tag:        "SYS",
			Category:   CatBasic,
			Status:     StatusReady,
			Summary:    "Live CPU load, RAM, temp, battery & CM0 hardware telemetry.",
			Hardware:   "Linux /proc & /sys",
			Privileges: "standard user",
		},
		{
			ID:         "net-if",
			Name:       "Net Interfaces",
			ShortName:  "NetIF",
			Tag:        "NET",
			Category:   CatBasic,
			Status:     StatusReady,
			Summary:    "Inspect local NICs, IPs, MACs, MTU and link states.",
			Hardware:   "Linux netlink",
			Privileges: "standard user",
		},
		{
			ID:         "notes",
			Name:       "Quick Notes",
			ShortName:  "Notes",
			Tag:        "NOTE",
			Category:   CatBasic,
			Status:     StatusReady,
			Summary:    "Pocket field notes saved locally in ~/.config/czhax/notes.txt.",
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
			Summary:    "Compact directory navigator and text file pager.",
			Hardware:   "local filesystem",
			Privileges: "standard user",
		},
	}
}

func ByCategory(cat Category) []Item {
	var out []Item
	for _, it := range All() {
		if it.Category == cat {
			out = append(out, it)
		}
	}
	return out
}

func FindByID(id string) (Item, bool) {
	for _, it := range All() {
		if it.ID == id {
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
