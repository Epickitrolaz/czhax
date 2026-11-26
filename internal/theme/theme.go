package theme

import "github.com/charmbracelet/lipgloss"

// Palette defines the High Contrast color scheme for Cardputer Zero's 1.9" LCD.
type Palette struct {
	ID        string
	Name      string
	Accent    lipgloss.Color
	Secondary lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Orange    lipgloss.Color
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

var HighContrast = Palette{
	ID:        "mono-hi",
	Name:      "High Contrast",
	Accent:    lipgloss.Color("#FFFFFF"),
	Secondary: lipgloss.Color("#E5E7EB"),
	Success:   lipgloss.Color("#86EFAC"),
	Warning:   lipgloss.Color("#FEF08A"),
	Orange:    lipgloss.Color("#FB923C"),
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
}

func Resolve(_ string, stealth bool) Palette {
	p := HighContrast
	if stealth {
		p.Accent = lipgloss.Color("#9CA3AF")
		p.Secondary = lipgloss.Color("#D1D5DB")
		p.Success = lipgloss.Color("#D1D5DB")
		p.Warning = lipgloss.Color("#9CA3AF")
		p.Orange = lipgloss.Color("#9CA3AF")
		p.Danger = lipgloss.Color("#9CA3AF")
		p.Border = lipgloss.Color("#4B5563")
		p.SelectBg = lipgloss.Color("#4B5563")
		p.SelectFg = lipgloss.Color("#FFFFFF")
		p.Text = lipgloss.Color("#E5E7EB")
		p.Muted = lipgloss.Color("#6B7280")
	}
	return p
}
