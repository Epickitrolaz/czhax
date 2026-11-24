package theme

import "github.com/charmbracelet/lipgloss"

type Palette struct {
	ID         string
	Name       string
	Accent     lipgloss.Color
	Secondary  lipgloss.Color
	Warning    lipgloss.Color
	Danger     lipgloss.Color
	Success    lipgloss.Color
	Text       lipgloss.Color
	Muted      lipgloss.Color
	Border     lipgloss.Color
	SelectBg   lipgloss.Color
	SelectFg   lipgloss.Color
	HeaderBg   lipgloss.Color
	HeaderFg   lipgloss.Color
	FooterBg   lipgloss.Color
	FooterFg   lipgloss.Color
}

// Builtins are tuned for high contrast on a 1.9" 320x170 ST7789 IPS LCD:
// crisp high-luminance foregrounds on pure black, avoiding low-contrast dark grays.
var Builtins = []Palette{
	{
		ID:        "cyber-green",
		Name:      "Cyber Green",
		Accent:    lipgloss.Color("#00FF87"),
		Secondary: lipgloss.Color("#72F1B8"),
		Warning:   lipgloss.Color("#FFE066"),
		Danger:    lipgloss.Color("#FF6B6B"),
		Success:   lipgloss.Color("#00FF87"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#B0BEC5"),
		Border:    lipgloss.Color("#4ADE80"),
		SelectBg:  lipgloss.Color("#00FF87"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#D0D7DE"),
	},
	{
		ID:        "pineapple-blue",
		Name:      "Pineapple Blue",
		Accent:    lipgloss.Color("#38BDF8"),
		Secondary: lipgloss.Color("#FACC15"),
		Warning:   lipgloss.Color("#FDE047"),
		Danger:    lipgloss.Color("#FB7185"),
		Success:   lipgloss.Color("#4ADE80"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#CBD5E1"),
		Border:    lipgloss.Color("#38BDF8"),
		SelectBg:  lipgloss.Color("#38BDF8"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#E2E8F0"),
	},
	{
		ID:        "amber-ops",
		Name:      "Amber Ops",
		Accent:    lipgloss.Color("#FFB000"),
		Secondary: lipgloss.Color("#FFD166"),
		Warning:   lipgloss.Color("#FFE066"),
		Danger:    lipgloss.Color("#FF6B6B"),
		Success:   lipgloss.Color("#8CE99A"),
		Text:      lipgloss.Color("#FFF8E7"),
		Muted:     lipgloss.Color("#D4B982"),
		Border:    lipgloss.Color("#FFB000"),
		SelectBg:  lipgloss.Color("#FFB000"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFF8E7"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#EAD2A0"),
	},
	{
		ID:        "purple-hax",
		Name:      "Purple Hax",
		Accent:    lipgloss.Color("#C084FC"),
		Secondary: lipgloss.Color("#7DD3FC"),
		Warning:   lipgloss.Color("#FDE047"),
		Danger:    lipgloss.Color("#FCA5A5"),
		Success:   lipgloss.Color("#86EFAC"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#CBD5E1"),
		Border:    lipgloss.Color("#A855F7"),
		SelectBg:  lipgloss.Color("#C084FC"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#E2E8F0"),
	},
	{
		ID:        "mono-hi",
		Name:      "High Contrast",
		Accent:    lipgloss.Color("#FFFFFF"),
		Secondary: lipgloss.Color("#E5E7EB"),
		Warning:   lipgloss.Color("#FEF08A"),
		Danger:    lipgloss.Color("#FCA5A5"),
		Success:   lipgloss.Color("#FFFFFF"),
		Text:      lipgloss.Color("#FFFFFF"),
		Muted:     lipgloss.Color("#D1D5DB"),
		Border:    lipgloss.Color("#FFFFFF"),
		SelectBg:  lipgloss.Color("#FFFFFF"),
		SelectFg:  lipgloss.Color("#000000"),
		HeaderBg:  lipgloss.Color("#000000"),
		HeaderFg:  lipgloss.Color("#FFFFFF"),
		FooterBg:  lipgloss.Color("#000000"),
		FooterFg:  lipgloss.Color("#FFFFFF"),
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
