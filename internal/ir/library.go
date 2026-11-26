package ir

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/czhax/czhax/internal/ir/bundled"
)

type Roots struct {
	Flipper string
	Mi      string
	Bundled bool
}

type Entry struct {
	Provider string
	Category string
	Brand    string
	Model    string
	Path     string
	Archive  string // "", "flipper", or "mi" when loaded from bundled zip
}

func (e Entry) Display() string {
	if e.Brand != "" && e.Model != "" {
		return e.Brand + " · " + e.Model
	}
	return e.Model
}

func DefaultRoots() Roots {
	return Roots{
		Bundled: true,
		Flipper: firstExisting(
			os.Getenv("CZHAX_IR_FLIPPER"),
			filepath.Join(homeDir(), "flipper-ir"),
			filepath.Join(homeDir(), "Flipper-IRDB"),
			filepath.Join(homeDir(), "flipperzero-ir"),
			"/ext/infrared",
			"/usr/share/czhax/ir/flipper",
			"/usr/local/share/czhax/ir/flipper",
		),
		Mi: firstExisting(
			os.Getenv("CZHAX_IR_MI"),
			filepath.Join(homeDir(), "mi-remote-ir"),
			filepath.Join(homeDir(), "mi_remote_database", "export"),
			filepath.Join(homeDir(), "mi_remote_database"),
			"/usr/share/czhax/ir/mi",
			"/usr/local/share/czhax/ir/mi",
		),
	}
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// LoadIndex walks bundled archives (when roots.Bundled is true) and provider
// directories and returns lightweight entries.
// Parsing every .ir file happens only when a device is opened.
func LoadIndex(roots Roots) ([]Entry, error) {
	var entries []Entry
	var errs []string
	if roots.Bundled {
		for _, provider := range []string{"flipper", "mi"} {
			zr, err := bundled.Reader(provider)
			if err != nil {
				errs = append(errs, fmt.Sprintf("bundled %s: %v", provider, err))
				continue
			}
			for _, f := range zr.File {
				if f.FileInfo().IsDir() {
					continue
				}
				if strings.ToLower(filepath.Ext(f.Name)) != ".ir" {
					continue
				}
				entry := entryFromPath(provider, provider, f.Name, f.Name)
				entry.Archive = provider
				entries = append(entries, entry)
			}
		}
	}
	for _, root := range []struct {
		provider string
		path     string
	}{
		{"flipper", roots.Flipper},
		{"mi", roots.Mi},
	} {
		if strings.TrimSpace(root.path) == "" {
			continue
		}
		rootPath := root.path
		if st, err := os.Stat(rootPath); err == nil && !st.IsDir() {
			entries = append(entries, entryFromPath(root.provider, filepath.Dir(rootPath), filepath.Base(rootPath), rootPath))
			continue
		}
		if err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") && path != rootPath {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".ir" && !(root.provider == "mi" && ext == ".json") {
				return nil
			}
			if root.provider == "mi" && ext == ".json" {
				low := strings.ToLower(path)
				if strings.Contains(low, "database_dump") || strings.Contains(low, "/models/") {
					return nil
				}
			}
			rel, err := filepath.Rel(rootPath, path)
			if err != nil {
				return nil
			}
			entry := entryFromPath(root.provider, rootPath, rel, path)
			entries = append(entries, entry)
			return nil
		}); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", rootPath, err))
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		if a.Brand != b.Brand {
			return a.Brand < b.Brand
		}
		return a.Model < b.Model
	})
	if len(errs) > 0 && len(entries) == 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return entries, nil
}

func entryFromPath(provider, root, rel, full string) Entry {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	base := parts[len(parts)-1]
	model := strings.TrimSuffix(base, filepath.Ext(base))
	e := Entry{Provider: provider, Model: model, Path: full}
	switch {
	case len(parts) >= 3:
		catPart := parts[0]
		if len(parts) >= 4 && isWrapperDir(catPart) {
			catPart = parts[1]
		}
		e.Category = normalizeCategory(catPart)
		e.Brand = parts[len(parts)-2]
	case len(parts) == 2:
		e.Category = normalizeCategory(filepath.Base(root))
		e.Brand = parts[0]
	default:
		e.Category = normalizeCategory(filepath.Base(root))
		e.Brand = "Unknown"
	}
	if provider == "mi" && strings.EqualFold(filepath.Ext(base), ".json") {
		e.Brand = "Mi Remote"
		if strings.HasPrefix(strings.ToLower(model), "xiaomi_") {
			e.Category = normalizeCategory(model[len("xiaomi_"):])
		} else if len(parts) >= 3 && isWrapperDir(parts[0]) {
			e.Category = normalizeCategory(parts[1])
		} else if len(parts) >= 2 && !isWrapperDir(parts[0]) {
			e.Category = normalizeCategory(parts[0])
		}
	}
	return e
}

func isWrapperDir(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "codes", "export", "exports", "ir", "irdb", "remotes", "database", "database_dump":
		return true
	}
	return false
}

func normalizeCategory(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "tvs", "tv", "xiaomi_tv", "universal_tv_remotes", "tv_tuner", "monitors", "touchscreen_displays", "digital_signs":
		return "TV"
	case "acs", "ac", "air conditioners", "air_conditioner", "xiaomi_ac":
		return "AC"
	case "audio", "audio/video", "a_v receiver", "av", "xiaomi_audio",
		"audio_and_video_receivers", "soundbars", "speakers", "head_units", "cd_players", "minidisc", "car_multimedia", "multimedia":
		return "Audio"
	case "dvds", "dvd", "xiaomi_dvd", "dvd_players", "blu-ray", "vcr", "laserdisc":
		return "DVD"
	case "projectors", "projector", "xiaomi_projector":
		return "Projector"
	case "set top boxes", "set-top box", "set top box", "set_top_boxes", "cable_boxes", "dvb-t", "stb", "cable _ satellite box", "xiaomi_stb":
		return "Set-top"
	case "streaming_devices":
		return "Stream"
	case "cameras", "camera", "xiaomi_camera", "cctv":
		return "Camera"
	case "fans", "fan", "xiaomi_fan":
		return "Fan"
	case "led_lighting":
		return "Lights"
	case "air_purifiers", "humidifiers":
		return "Air"
	case "vacuum_cleaners", "window_cleaners", "dust_collectors":
		return "Vacuum"
	case "networking", "internet box", "xiaomi_box", "converters", "kvm":
		return "Box"
	case "_converted_", "miscellaneous", "flipper", "mi":
		return "Other"
	}
	if s == "" {
		return "Other"
	}
	return strings.ReplaceAll(s, "_", " ")
}

func LoadEntry(e Entry) (Remote, error) {
	var r Remote
	var err error
	if e.Archive != "" {
		zr, zerr := bundled.Reader(e.Archive)
		if zerr != nil {
			return Remote{}, zerr
		}
		zf, zerr := zr.Open(e.Path)
		if zerr != nil {
			return Remote{}, zerr
		}
		defer zf.Close()
		r, err = ParseFlipperReader(e.Model, e.Path, zf)
	} else {
		switch strings.ToLower(filepath.Ext(e.Path)) {
		case ".ir":
			r, err = ParseFlipperFile(e.Path)
		case ".json":
			r, err = ParseMiRemoteJSON(e.Path)
		default:
			return Remote{}, fmt.Errorf("unsupported IR database file %s", e.Path)
		}
	}
	if err != nil {
		return r, err
	}
	r = r.WithMeta(e.Provider, e.Category, e.Brand, e.Model)
	r.Name = e.Display()
	return r, nil
}

func FilterEntries(entries []Entry, provider, category string) []Entry {
	return SearchEntries(entries, provider, category, "")
}

func SearchEntries(entries []Entry, provider, category, query string) []Entry {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if provider != "" && e.Provider != provider {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		if q != "" {
			if !strings.Contains(strings.ToLower(e.Brand), q) &&
				!strings.Contains(strings.ToLower(e.Model), q) &&
				!strings.Contains(strings.ToLower(e.Category), q) {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

func Categories(entries []Entry) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if e.Category == "" || seen[e.Category] {
			continue
		}
		seen[e.Category] = true
		out = append(out, e.Category)
	}
	sort.Strings(out)
	return out
}
