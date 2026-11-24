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
	Surface    lipgloss.Color
	SurfaceAlt lipgloss.Color
	SelectBg   lipgloss.Color
	SelectFg   lipgloss.Color
	HeaderBg   lipgloss.Color
	HeaderFg   lipgloss.Color
	FooterBg   lipgloss.Color
	FooterFg   lipgloss.Color
}

var Builtins = []Palette{
	{
		ID:         "cyber-green",
		Name:       "Cyber Green",
		Accent:     lipgloss.Color("#00FF87"),
		Secondary:  lipgloss.Color("#00D7AF"),
		Warning:    lipgloss.Color("#FFD75F"),
		Danger:     lipgloss.Color("#FF5F5F"),
		Success:    lipgloss.Color("#00FF87"),
		Text:       lipgloss.Color("#E4E4E4"),
		Muted:      lipgloss.Color("#767676"),
		Surface:    lipgloss.Color("#141B1F"),
		SurfaceAlt: lipgloss.Color("#1E282E"),
		SelectBg:   lipgloss.Color("#00FF87"),
		SelectFg:   lipgloss.Color("#0B0F14"),
		HeaderBg:   lipgloss.Color("#101A16"),
		HeaderFg:   lipgloss.Color("#E4E4E4"),
		FooterBg:   lipgloss.Color("#141B1F"),
		FooterFg:   lipgloss.Color("#9E9E9E"),
	},
	{
		ID:         "purple-hax",
		Name:       "Purple Hax",
		Accent:     lipgloss.Color("#A855F7"),
		Secondary:  lipgloss.Color("#38BDF8"),
		Warning:    lipgloss.Color("#FBBF24"),
		Danger:     lipgloss.Color("#F87171"),
		Success:    lipgloss.Color("#4ADE80"),
		Text:       lipgloss.Color("#F1F5F9"),
		Muted:      lipgloss.Color("#94A3B8"),
		Surface:    lipgloss.Color("#191224"),
		SurfaceAlt: lipgloss.Color("#271B38"),
		SelectBg:   lipgloss.Color("#9333EA"),
		SelectFg:   lipgloss.Color("#FFFFFF"),
		HeaderBg:   lipgloss.Color("#1E1230"),
		HeaderFg:   lipgloss.Color("#F1F5F9"),
		FooterBg:   lipgloss.Color("#191224"),
		FooterFg:   lipgloss.Color("#CBD5E1"),
	},
	{
		ID:         "amber-ops",
		Name:       "Amber Ops",
		Accent:     lipgloss.Color("#FFB000"),
		Secondary:  lipgloss.Color("#FF8700"),
		Warning:    lipgloss.Color("#FFD700"),
		Danger:     lipgloss.Color("#FF5555"),
		Success:    lipgloss.Color("#87FF5F"),
		Text:       lipgloss.Color("#F5E6C8"),
		Muted:      lipgloss.Color("#8A7755"),
		Surface:    lipgloss.Color("#1A140B"),
		SurfaceAlt: lipgloss.Color("#291F11"),
		SelectBg:   lipgloss.Color("#FFB000"),
		SelectFg:   lipgloss.Color("#140E05"),
		HeaderBg:   lipgloss.Color("#21180A"),
		HeaderFg:   lipgloss.Color("#F5E6C8"),
		FooterBg:   lipgloss.Color("#1A140B"),
		FooterFg:   lipgloss.Color("#B89F72"),
	},
	{
		ID:         "ice-cyan",
		Name:       "Ice Cyan",
		Accent:     lipgloss.Color("#00E5FF"),
		Secondary:  lipgloss.Color("#7C3AED"),
		Warning:    lipgloss.Color("#FACC15"),
		Danger:     lipgloss.Color("#FB7185"),
		Success:    lipgloss.Color("#34D399"),
		Text:       lipgloss.Color("#E2F1F8"),
		Muted:      lipgloss.Color("#6B8A9E"),
		Surface:    lipgloss.Color("#0D1821"),
		SurfaceAlt: lipgloss.Color("#162736"),
		SelectBg:   lipgloss.Color("#00E5FF"),
		SelectFg:   lipgloss.Color("#071118"),
		HeaderBg:   lipgloss.Color("#0E202E"),
		HeaderFg:   lipgloss.Color("#E2F1F8"),
		FooterBg:   lipgloss.Color("#0D1821"),
		FooterFg:   lipgloss.Color("#8CB0C4"),
	},
	{
		ID:         "mono-hi",
		Name:       "High Contrast",
		Accent:     lipgloss.Color("#FFFFFF"),
		Secondary:  lipgloss.Color("#D0D0D0"),
		Warning:    lipgloss.Color("#FFFF87"),
		Danger:     lipgloss.Color("#FF8787"),
		Success:    lipgloss.Color("#FFFFFF"),
		Text:       lipgloss.Color("#FFFFFF"),
		Muted:      lipgloss.Color("#9E9E9E"),
		Surface:    lipgloss.Color("#1C1C1C"),
		SurfaceAlt: lipgloss.Color("#303030"),
		SelectBg:   lipgloss.Color("#FFFFFF"),
		SelectFg:   lipgloss.Color("#000000"),
		HeaderBg:   lipgloss.Color("#262626"),
		HeaderFg:   lipgloss.Color("#FFFFFF"),
		FooterBg:   lipgloss.Color("#1C1C1C"),
		FooterFg:   lipgloss.Color("#D0D0D0"),
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
		p.Accent = lipgloss.Color("#6C7A89")
		p.Secondary = lipgloss.Color("#566270")
		p.SelectBg = lipgloss.Color("#3E4A59")
		p.SelectFg = lipgloss.Color("#E4E4E4")
		p.HeaderBg = lipgloss.Color("#121518")
		p.FooterBg = lipgloss.Color("#121518")
		p.Text = lipgloss.Color("#B8C0C8")
		p.Muted = lipgloss.Color("#586069")
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
