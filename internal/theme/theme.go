package theme

import "github.com/charmbracelet/lipgloss"

// Palette uses a clean, predictable 4-role color system for small LCDs:
//  1. Accent    -> active view, active tab, active selection
//  2. Secondary -> field labels, keycaps, section headers
//  3. Text/Muted/Border -> crisp white values + calm slate structure
//  4. Success/Warning/Danger -> semantic state only (UP/ON, warn, DN/OFF)
type Palette struct {
	ID        string
	Name      string
	Accent    lipgloss.Color
	Secondary lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Danger    lipgloss.Color
	Text      lipgloss.Color
	Muted     lipgloss.Color
	Border    lipgloss.Color
	SelectBg  lipgloss.Color
	SelectFg  lipgloss.Color
	HeaderBg  lipgloss.Color
	HeaderFg  lipgloss.Color
	FooterBg  lipgloss.Color
	FooterFg  lipgloss.Color
}

var Builtins = []Palette{
	{
		ID:        "pineapple-blue",
		Name:      "Pineapple",
		Accent:    lipgloss.Color("#38BDF8"), // Sky Cyan: active focus & selection
		Secondary: lipgloss.Color("#FACC15"), // Amber Gold: labels & keycaps
		Success:   lipgloss.Color("#4ADE80"), // Green: UP / ON / healthy
		Warning:   lipgloss.Color("#FDE047"), // Yellow: caution / PH2
		Danger:    lipgloss.Color("#FB7185"), // Coral Red: DN / OFF / alert
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#94A3B8"),
		Border:    lipgloss.Color("#475569"),
		SelectBg:  lipgloss.Color("#38BDF8"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#CBD5E1"),
	},
	{
		ID:        "cyber-green",
		Name:      "Cyber Ops",
		Accent:    lipgloss.Color("#00FF87"), // Neon Mint: active focus & selection
		Secondary: lipgloss.Color("#38BDF8"), // Sky Blue: labels & keycaps
		Success:   lipgloss.Color("#4ADE80"),
		Warning:   lipgloss.Color("#FACC15"),
		Danger:    lipgloss.Color("#FB7185"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#94A3B8"),
		Border:    lipgloss.Color("#475569"),
		SelectBg:  lipgloss.Color("#00FF87"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#CBD5E1"),
	},
	{
		ID:        "amber-ops",
		Name:      "Amber Ops",
		Accent:    lipgloss.Color("#FFB000"), // Warm Amber: active focus & selection
		Secondary: lipgloss.Color("#67E8F9"), // Ice Cyan: labels & keycaps
		Success:   lipgloss.Color("#86EFAC"),
		Warning:   lipgloss.Color("#FFE066"),
		Danger:    lipgloss.Color("#FCA5A5"),
		Text:      lipgloss.Color("#FFF8E7"),
		Muted:     lipgloss.Color("#A89878"),
		Border:    lipgloss.Color("#5C4D32"),
		SelectBg:  lipgloss.Color("#FFB000"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFF8E7"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#D4C29E"),
	},
	{
		ID:        "purple-hax",
		Name:      "Purple Hax",
		Accent:    lipgloss.Color("#C084FC"), // Violet: active focus & selection
		Secondary: lipgloss.Color("#38BDF8"), // Cyan: labels & keycaps
		Success:   lipgloss.Color("#4ADE80"),
		Warning:   lipgloss.Color("#FACC15"),
		Danger:    lipgloss.Color("#FB7185"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#94A3B8"),
		Border:    lipgloss.Color("#52476B"),
		SelectBg:  lipgloss.Color("#C084FC"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#CBD5E1"),
	},
	{
		ID:        "mono-hi",
		Name:      "High Contrast",
		Accent:    lipgloss.Color("#FFFFFF"),
		Secondary: lipgloss.Color("#CBD5E1"),
		Success:   lipgloss.Color("#86EFAC"),
		Warning:   lipgloss.Color("#FEF08A"),
		Danger:    lipgloss.Color("#FCA5A5"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#9CA3AF"),
		Border:    lipgloss.Color("#6B7280"),
		SelectBg:  lipgloss.Color("#FFFFFF"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#E5E7EB"),
	},
}

func Resolve(id string, stealth bool) Palette {
	p := Builtins[0]
	for _, b := range Builtins {
		if b.ID == id {
			p = b
			break
		}
	}
	if stealth {
		p.Accent = lipgloss.Color("#94A3B8")
		p.Secondary = lipgloss.Color("#CBD5E1")
		p.Success = lipgloss.Color("#CBD5E1")
		p.Warning = lipgloss.Color("#94A3B8")
		p.Danger = lipgloss.Color("#94A3B8")
		p.Border = lipgloss.Color("#475569")
		p.SelectBg = lipgloss.Color("#475569")
		p.SelectFg = lipgloss.Color("#FFFFFF")
		p.Text = lipgloss.Color("#E2E8F0")
		p.Muted = lipgloss.Color("#64748B")
	}
	return p
}

func NextID(current string) string {
	for i, b := range Builtins {
		if b.ID == current {
			return Builtins[(i+1)%len(Builtins)].ID
		}
	}
	return Builtins[0].ID
}

func PrevID(current string) string {
	for i, b := range Builtins {
		if b.ID == current {
			return Builtins[(i-1+len(Builtins))%len(Builtins)].ID
		}
	}
	return Builtins[0].ID
}
