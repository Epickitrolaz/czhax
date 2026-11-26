package ir

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultFrequency = 38000
	DefaultDutyCycle = 0.33
	MaxTimings       = 511
	maxDurationUS    = 500000
	frameGap         = 40000
)

// Signal is one IR command. Parsed Flipper entries keep their protocol
// metadata; every signal also carries the raw microsecond timings used by the
// transmitter, so learned and database signals share one send path.
type Signal struct {
	Name      string    `json:"name"`
	Comment   string    `json:"comment,omitempty"`
	Protocol  string    `json:"protocol,omitempty"`
	Address   uint32    `json:"address,omitempty"`
	Command   uint32    `json:"command,omitempty"`
	Frequency int       `json:"frequency"`
	DutyCycle float64   `json:"duty_cycle"`
	Timings   []int     `json:"timings"`
	Source    string    `json:"source,omitempty"`
	Saved     time.Time `json:"saved,omitempty"`
}

func (s Signal) Normalized() Signal {
	s.Name = strings.TrimSpace(s.Name)
	if s.Frequency <= 0 {
		s.Frequency = DefaultFrequency
	}
	if s.DutyCycle <= 0 || s.DutyCycle > 1 {
		s.DutyCycle = DefaultDutyCycle
	}
	s.Timings = normalizeTimings(s.Timings)
	return s
}

// normalizeTimings ensures a frame starts and ends with a mark (odd count),
// stays within Linux LIRC_MODE_PULSE duration limits, and fits in LIRCBUF_SIZE.
func normalizeTimings(in []int) []int {
	out := make([]int, 0, len(in))
	for _, v := range in {
		if v <= 0 {
			continue
		}
		if v > maxDurationUS {
			v = maxDurationUS
		}
		out = append(out, v)
	}
	if len(out) > MaxTimings {
		out = out[:MaxTimings]
	}
	if len(out) > 0 && len(out)%2 == 0 {
		// Linux LIRC_MODE_PULSE requires an odd count (must start and end with a pulse).
		out = out[:len(out)-1]
	}
	return out
}

func (s Signal) Validate() error {
	if len(s.Timings) < 3 {
		return fmt.Errorf("signal has too few timings (%d)", len(s.Timings))
	}
	if len(s.Timings)%2 == 0 {
		return errors.New("signal timings must have an odd count (pulse/space/.../pulse)")
	}
	for i, d := range s.Timings {
		if d <= 0 || d > maxDurationUS {
			return fmt.Errorf("timing[%d]=%d out of range (1..%d)", i, d, maxDurationUS)
		}
	}
	return nil
}

// Binding maps one keyboard key to one IR signal.
type Binding struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Signal Signal `json:"signal"`
}

// Profile is a device remote: a set of keyboard-bound IR signals.
type Profile struct {
	Name     string    `json:"name"`
	Source   string    `json:"source,omitempty"`
	Category string    `json:"category,omitempty"`
	Brand    string    `json:"brand,omitempty"`
	Model    string    `json:"model,omitempty"`
	Bindings []Binding `json:"bindings"`
}

type Store struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
	Active   int       `json:"active"`
}

func NewStore() Store {
	return Store{Version: 1}
}

func DefaultStorePath(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = "/tmp"
		}
		return filepath.Join(base, "czhax", "ir.json")
	}
	return filepath.Join(filepath.Dir(configPath), "ir.json")
}

func LoadStore(path string) (Store, error) {
	s := NewStore()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return NewStore(), err
	}
	if s.Version == 0 {
		s.Version = 1
	}
	s.normalize()
	return s, nil
}

func (s *Store) normalize() {
	if s.Active < 0 || s.Active >= len(s.Profiles) {
		s.Active = 0
	}
	for i := range s.Profiles {
		filtered := s.Profiles[i].Bindings[:0]
		seen := map[string]bool{}
		for _, b := range s.Profiles[i].Bindings {
			if strings.TrimSpace(b.Key) == "" || seen[b.Key] {
				continue
			}
			seen[b.Key] = true
			b.Signal = b.Signal.Normalized()
			filtered = append(filtered, b)
		}
		s.Profiles[i].Bindings = filtered
	}
}

func (s Store) Save(path string) error {
	if path == "" {
		return errors.New("empty IR store path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *Store) ActiveProfile() *Profile {
	if len(s.Profiles) == 0 {
		return nil
	}
	if s.Active < 0 || s.Active >= len(s.Profiles) {
		s.Active = 0
	}
	return &s.Profiles[s.Active]
}

func (s *Store) AddProfile(p Profile) int {
	for i := range s.Profiles {
		if s.Profiles[i].Name == p.Name {
			s.Profiles[i] = p
			s.Active = i
			return i
		}
	}
	s.Profiles = append(s.Profiles, p)
	s.Active = len(s.Profiles) - 1
	return s.Active
}

func (s *Store) CreateProfile(name, source string) (int, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return s.Active, errors.New("remote name cannot be empty")
	}
	for _, p := range s.Profiles {
		if strings.EqualFold(strings.TrimSpace(p.Name), trimmed) {
			return s.Active, errors.New("remote already exists")
		}
	}
	if strings.TrimSpace(source) == "" {
		source = "custom"
	}
	s.Profiles = append(s.Profiles, Profile{Name: trimmed, Source: source})
	s.Active = len(s.Profiles) - 1
	return s.Active, nil
}

func (s *Store) SelectProfile(idx int) bool {
	if idx < 0 || idx >= len(s.Profiles) {
		return false
	}
	s.Active = idx
	return true
}

func (s *Store) DeleteProfile(idx int) bool {
	if idx < 0 || idx >= len(s.Profiles) {
		return false
	}
	s.Profiles = append(s.Profiles[:idx], s.Profiles[idx+1:]...)
	if len(s.Profiles) == 0 {
		s.Active = 0
	} else if s.Active >= len(s.Profiles) {
		s.Active = len(s.Profiles) - 1
	} else if s.Active > idx {
		s.Active--
	}
	return true
}

func (s *Store) AddBindingToActive(b Binding) int {
	if p := s.ActiveProfile(); p != nil && strings.TrimSpace(p.Name) != "" {
		return s.AddBinding(p.Name, b)
	}
	return s.AddBinding("My remote", b)
}

func (s *Store) DeleteBinding(profileIdx int, key string) bool {
	if profileIdx < 0 || profileIdx >= len(s.Profiles) {
		return false
	}
	key = NormalizeBindKey(key)
	bs := s.Profiles[profileIdx].Bindings
	for i := range bs {
		if bs[i].Key == key {
			s.Profiles[profileIdx].Bindings = append(bs[:i], bs[i+1:]...)
			return true
		}
	}
	return false
}

func (s *Store) AddBinding(profileName string, b Binding) int {
	name := strings.TrimSpace(profileName)
	if name == "" {
		name = "My remote"
	}
	var idx int
	found := false
	for i := range s.Profiles {
		if s.Profiles[i].Name == name {
			idx, found = i, true
			break
		}
	}
	if !found {
		s.Profiles = append(s.Profiles, Profile{Name: name, Source: "learned"})
		idx = len(s.Profiles) - 1
	}
	p := &s.Profiles[idx]
	for i := range p.Bindings {
		if p.Bindings[i].Key == b.Key {
			p.Bindings[i] = b
			s.Active = idx
			return idx
		}
	}
	p.Bindings = append(p.Bindings, b)
	sortBindings(p.Bindings)
	s.Active = idx
	return idx
}

func sortBindings(bs []Binding) {
	sort.SliceStable(bs, func(i, j int) bool {
		return KeyOrder(bs[i].Key) < KeyOrder(bs[j].Key)
	})
}

// KeyOrder gives the display/assignment order for supported keys.
func KeyOrder(key string) int {
	for i, k := range KeyPool {
		if k == key {
			return i
		}
	}
	return 1000
}

var KeyPool = []string{
	"1", "2", "3", "4", "5", "6", "7", "8", "9", "0",
	"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m",
	"n", "o", "p", "r", "s", "t", "u", "v", "w", "x", "y", "z",
	"enter", "backspace", "space",
	"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8",
}

func NormalizeBindKey(key string) string {
	switch key {
	case " ":
		return "space"
	}
	if len(key) == 1 && key[0] >= 'A' && key[0] <= 'Z' {
		return strings.ToLower(key)
	}
	return key
}

func KeyLabel(key string) string {
	switch key {
	case "enter":
		return "Enter"
	case "backspace":
		return "Bksp"
	case "space":
		return "Space"
	case "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8":
		return strings.ToUpper(key)
	default:
		if len(key) == 1 && key[0] >= 'a' && key[0] <= 'z' {
			return strings.ToUpper(key)
		}
		return key
	}
}

func IsBindableKey(key string) bool {
	key = NormalizeBindKey(key)
	for _, k := range KeyPool {
		if k == key {
			return true
		}
	}
	return false
}

func NextFreeKey(used map[string]bool) string {
	for _, k := range KeyPool {
		if !used[k] {
			return k
		}
	}
	return ""
}

// KeyForButton maps common Flipper/Mi button names to the default key layout.
func KeyForButton(name string) string {
	n := normalizeName(name)
	switch {
	case strings.HasPrefix(n, "power") || n == "pwr" || n == "on" || n == "off":
		return "1"
	case strings.HasPrefix(n, "vol_up") || strings.HasPrefix(n, "volume_up") || n == "vol_plus" || n == "vol+":
		return "2"
	case strings.HasPrefix(n, "vol_d") || strings.HasPrefix(n, "volume_d") || n == "vol_" || n == "vol-" || n == "vol_minus":
		return "3"
	case strings.HasPrefix(n, "mute"):
		return "4"
	case strings.HasPrefix(n, "ch_next") || strings.HasPrefix(n, "ch_up") || strings.HasPrefix(n, "channel_up") || n == "ch+":
		return "5"
	case strings.HasPrefix(n, "ch_prev") || strings.HasPrefix(n, "ch_dn") || strings.HasPrefix(n, "ch_down") || strings.HasPrefix(n, "channel_d") || n == "ch_" || n == "ch-":
		return "6"
	case n == "up" || n == "arrow_up":
		return "7"
	case n == "down" || n == "arrow_down":
		return "8"
	case n == "left" || n == "arrow_left":
		return "9"
	case n == "right" || n == "arrow_right":
		return "0"
	case n == "ok" || n == "enter" || n == "select" || n == "confirm":
		return "enter"
	case n == "back" || n == "exit" || n == "return":
		return "backspace"
	default:
		return ""
	}
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

// BindSignals converts a remote's signals into a profile. Named buttons get
// their conventional key; unmatched buttons take the next free key.
func BindSignals(profileName, source, category, brand, model string, sigs []Signal) Profile {
	p := Profile{Name: profileName, Source: source, Category: category, Brand: brand, Model: model}
	used := map[string]bool{}
	for _, sig := range sigs {
		if len(sig.Timings) == 0 {
			continue
		}
		key := KeyForButton(sig.Name)
		if key != "" && used[key] {
			key = ""
		}
		if key == "" {
			key = NextFreeKey(used)
		}
		if key == "" {
			break
		}
		used[key] = true
		sig = sig.Normalized()
		p.Bindings = append(p.Bindings, Binding{Key: key, Name: sig.Name, Signal: sig})
	}
	sortBindings(p.Bindings)
	return p
}
