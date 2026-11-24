package theme

import "github.com/charmbracelet/lipgloss"

type Palette struct {
	ID        string
	Name      string
	Accent    lipgloss.Color
	Secondary lipgloss.Color
	Warning   lipgloss.Color
	Danger    lipgloss.Color
	Success   lipgloss.Color
	Text      lipgloss.Color
	Muted     lipgloss.Color
	Border    lipgloss.Color
	SelectBg  lipgloss.Color
	SelectFg  lipgloss.Color
	HeaderBg  lipgloss.Color
	HeaderFg  lipgloss.Color
	FooterBg  lipgloss.Color
	FooterFg  lipgloss.Color

	// Multi-color semantic accents for high-contrast scanning on small LCDs
	Cyan    lipgloss.Color
	Magenta lipgloss.Color
	Yellow  lipgloss.Color
	Purple  lipgloss.Color
	Orange  lipgloss.Color
	Green   lipgloss.Color
	Red     lipgloss.Color
	Blue    lipgloss.Color
}

var Builtins = []Palette{
	{
		ID:        "cyber-green",
		Name:      "Cyber Neon",
		Accent:    lipgloss.Color("#00FF87"),
		Secondary: lipgloss.Color("#38BDF8"),
		Warning:   lipgloss.Color("#FACC15"),
		Danger:    lipgloss.Color("#FB7185"),
		Success:   lipgloss.Color("#4ADE80"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#94A3B8"),
		Border:    lipgloss.Color("#38BDF8"),
		SelectBg:  lipgloss.Color("#00FF87"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#CBD5E1"),
		Cyan:      lipgloss.Color("#22D3EE"),
		Magenta:   lipgloss.Color("#F472B6"),
		Yellow:    lipgloss.Color("#FACC15"),
		Purple:    lipgloss.Color("#C084FC"),
		Orange:    lipgloss.Color("#FB923C"),
		Green:     lipgloss.Color("#4ADE80"),
		Red:       lipgloss.Color("#FB7185"),
		Blue:      lipgloss.Color("#60A5FA"),
	},
	{
		ID:        "pineapple-blue",
		Name:      "Pineapple",
		Accent:    lipgloss.Color("#38BDF8"),
		Secondary: lipgloss.Color("#FACC15"),
		Warning:   lipgloss.Color("#FDE047"),
		Danger:    lipgloss.Color("#FB7185"),
		Success:   lipgloss.Color("#4ADE80"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#CBD5E1"),
		Border:    lipgloss.Color("#FACC15"),
		SelectBg:  lipgloss.Color("#38BDF8"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#E2E8F0"),
		Cyan:      lipgloss.Color("#22D3EE"),
		Magenta:   lipgloss.Color("#F472B6"),
		Yellow:    lipgloss.Color("#FDE047"),
		Purple:    lipgloss.Color("#C084FC"),
		Orange:    lipgloss.Color("#FB923C"),
		Green:     lipgloss.Color("#4ADE80"),
		Red:       lipgloss.Color("#FB7185"),
		Blue:      lipgloss.Color("#60A5FA"),
	},
	{
		ID:        "amber-ops",
		Name:      "Amber Ops",
		Accent:    lipgloss.Color("#FFB000"),
		Secondary: lipgloss.Color("#38BDF8"),
		Warning:   lipgloss.Color("#FFE066"),
		Danger:    lipgloss.Color("#FF6B6B"),
		Success:   lipgloss.Color("#8CE99A"),
		Text:      lipgloss.Color("#FFF8E7"),
		Muted:     lipgloss.Color("#D4B982"),
		Border:    lipgloss.Color("#FB923C"),
		SelectBg:  lipgloss.Color("#FFB000"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFF8E7"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#EAD2A0"),
		Cyan:      lipgloss.Color("#67E8F9"),
		Magenta:   lipgloss.Color("#F472B6"),
		Yellow:    lipgloss.Color("#FFE066"),
		Purple:    lipgloss.Color("#D8B4FE"),
		Orange:    lipgloss.Color("#FB923C"),
		Green:     lipgloss.Color("#86EFAC"),
		Red:       lipgloss.Color("#FCA5A5"),
		Blue:      lipgloss.Color("#93C5FD"),
	},
	{
		ID:        "purple-hax",
		Name:      "Purple Hax",
		Accent:    lipgloss.Color("#C084FC"),
		Secondary: lipgloss.Color("#22D3EE"),
		Warning:   lipgloss.Color("#FDE047"),
		Danger:    lipgloss.Color("#FB7185"),
		Success:   lipgloss.Color("#4ADE80"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#CBD5E1"),
		Border:    lipgloss.Color("#A855F7"),
		SelectBg:  lipgloss.Color("#C084FC"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#E2E8F0"),
		Cyan:      lipgloss.Color("#22D3EE"),
		Magenta:   lipgloss.Color("#F472B6"),
		Yellow:    lipgloss.Color("#FACC15"),
		Purple:    lipgloss.Color("#C084FC"),
		Orange:    lipgloss.Color("#FB923C"),
		Green:     lipgloss.Color("#4ADE80"),
		Red:       lipgloss.Color("#FB7185"),
		Blue:      lipgloss.Color("#60A5FA"),
	},
	{
		ID:        "mono-hi",
		Name:      "High Contrast",
		Accent:    lipgloss.Color("#FFFFFF"),
		Secondary: lipgloss.Color("#38BDF8"),
		Warning:   lipgloss.Color("#FEF08A"),
		Danger:    lipgloss.Color("#FCA5A5"),
		Success:   lipgloss.Color("#86EFAC"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#D1D5DB"),
		Border:    lipgloss.Color("#FFFFFF"),
		SelectBg:  lipgloss.Color("#FFFFFF"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#FFFFFF"),
		Cyan:      lipgloss.Color("#67E8F9"),
		Magenta:   lipgloss.Color("#F9A8D4"),
		Yellow:    lipgloss.Color("#FEF08A"),
		Purple:    lipgloss.Color("#DDD6FE"),
		Orange:    lipgloss.Color("#FDBA74"),
		Green:     lipgloss.Color("#86EFAC"),
		Red:       lipgloss.Color("#FCA5A5"),
		Blue:      lipgloss.Color("#93C5FD"),
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
		p.Border = lipgloss.Color("#64748B")
		p.SelectBg = lipgloss.Color("#475569")
		p.SelectFg = lipgloss.Color("#FFFFFF")
		p.Text = lipgloss.Color("#E2E8F0")
		p.Muted = lipgloss.Color("#94A3B8")
		p.Cyan = lipgloss.Color("#94A3B8")
		p.Magenta = lipgloss.Color("#94A3B8")
		p.Yellow = lipgloss.Color("#CBD5E1")
		p.Purple = lipgloss.Color("#94A3B8")
		p.Orange = lipgloss.Color("#94A3B8")
		p.Green = lipgloss.Color("#CBD5E1")
		p.Red = lipgloss.Color("#94A3B8")
		p.Blue = lipgloss.Color("#94A3B8")
	}
	return p
}

func ViewColor(p Palette, viewID int) lipgloss.Color {
	switch viewID {
	case 0: // Dash
		return p.Cyan
	case 1: // Recon
		return p.Magenta
	case 2: // Net
		return p.Yellow
	case 3: // Lab
		return p.Purple
	case 4: // Sys
		return p.Green
	case 5: // Opts
		return p.Orange
	default:
		return p.Accent
	}
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
