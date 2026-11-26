package ir

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type UniversalButton struct {
	Key    string
	Label  string
	Target string
}

type UniversalProgress struct {
	Target       string
	EntryIndex   int
	TotalEntries int
	UniqueSent   int
	Current      Entry
	Signal       Signal
}

// MatchCandidate is one unique Power signal discovered for a (Category, Brand)
// paired with the best Entry that contains it.
type MatchCandidate struct {
	Entry       Entry
	Signal      Signal
	SignalCount int
}

// UniversalButtonsForCategory returns a temporary universal remote button layout
// tailored to the selected device category.
func UniversalButtonsForCategory(category string) []UniversalButton {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "ac":
		return []UniversalButton{
			{Key: "1", Label: "Power", Target: "power"},
			{Key: "2", Label: "Temp +", Target: "temp_up"},
			{Key: "3", Label: "Temp -", Target: "temp_dn"},
			{Key: "4", Label: "Mode", Target: "mode"},
			{Key: "5", Label: "Fan", Target: "fan"},
			{Key: "6", Label: "Swing", Target: "swing"},
			{Key: "7", Label: "Timer", Target: "timer"},
		}
	case "fan":
		return []UniversalButton{
			{Key: "1", Label: "Power", Target: "power"},
			{Key: "2", Label: "Speed", Target: "fan"},
			{Key: "3", Label: "Mode", Target: "mode"},
			{Key: "4", Label: "Swing", Target: "swing"},
			{Key: "5", Label: "Timer", Target: "timer"},
			{Key: "6", Label: "Light", Target: "light"},
		}
	case "lights":
		return []UniversalButton{
			{Key: "1", Label: "On", Target: "on"},
			{Key: "2", Label: "Off", Target: "off"},
			{Key: "3", Label: "Power", Target: "power"},
			{Key: "4", Label: "White", Target: "white"},
			{Key: "5", Label: "Red", Target: "red"},
			{Key: "6", Label: "Green", Target: "green"},
			{Key: "7", Label: "Blue", Target: "blue"},
			{Key: "8", Label: "Fade", Target: "fade"},
		}
	case "projector":
		return []UniversalButton{
			{Key: "1", Label: "Power", Target: "power"},
			{Key: "2", Label: "Vol +", Target: "vol_up"},
			{Key: "3", Label: "Vol -", Target: "vol_dn"},
			{Key: "4", Label: "Mute", Target: "mute"},
			{Key: "5", Label: "Menu", Target: "menu"},
			{Key: "6", Label: "Source", Target: "source"},
			{Key: "7", Label: "Up", Target: "up"},
			{Key: "8", Label: "Down", Target: "down"},
			{Key: "enter", Label: "OK", Target: "ok"},
			{Key: "backspace", Label: "Back", Target: "back"},
		}
	case "audio", "dvd":
		return []UniversalButton{
			{Key: "1", Label: "Power", Target: "power"},
			{Key: "2", Label: "Vol +", Target: "vol_up"},
			{Key: "3", Label: "Vol -", Target: "vol_dn"},
			{Key: "4", Label: "Mute", Target: "mute"},
			{Key: "5", Label: "Play", Target: "play"},
			{Key: "6", Label: "Pause", Target: "pause"},
			{Key: "7", Label: "Up", Target: "up"},
			{Key: "8", Label: "Down", Target: "down"},
			{Key: "enter", Label: "OK", Target: "ok"},
			{Key: "backspace", Label: "Back", Target: "back"},
		}
	default: // TV, Set-top, Stream, etc.
		return []UniversalButton{
			{Key: "1", Label: "Power", Target: "power"},
			{Key: "2", Label: "Vol +", Target: "vol_up"},
			{Key: "3", Label: "Vol -", Target: "vol_dn"},
			{Key: "4", Label: "Mute", Target: "mute"},
			{Key: "5", Label: "Ch +", Target: "ch_up"},
			{Key: "6", Label: "Ch -", Target: "ch_dn"},
			{Key: "7", Label: "Up", Target: "up"},
			{Key: "8", Label: "Down", Target: "down"},
			{Key: "9", Label: "Left", Target: "left"},
			{Key: "0", Label: "Right", Target: "right"},
			{Key: "enter", Label: "OK", Target: "ok"},
			{Key: "backspace", Label: "Back", Target: "back"},
		}
	}
}

type CategoryCount struct {
	Name  string
	Count int
}

// CategoryCounts returns distinct categories ordered with common consumer device
// types first, then by entry count descending.
func CategoryCounts(entries []Entry) []CategoryCount {
	counts := map[string]int{}
	for _, e := range entries {
		cat := strings.TrimSpace(e.Category)
		if cat == "" {
			cat = "Other"
		}
		counts[cat]++
	}
	out := make([]CategoryCount, 0, len(counts))
	for k, n := range counts {
		out = append(out, CategoryCount{Name: k, Count: n})
	}
	rank := map[string]int{
		"TV": 1, "AC": 2, "Projector": 3, "Audio": 4, "Set-top": 5, "Fan": 6, "DVD": 7, "Stream": 8,
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := rank[out[i].Name], rank[out[j].Name]
		switch {
		case ri > 0 && rj > 0:
			return ri < rj
		case ri > 0 || rj > 0:
			return ri > 0
		case out[i].Count != out[j].Count:
			return out[i].Count > out[j].Count
		default:
			return out[i].Name < out[j].Name
		}
	})
	return out
}

type BrandCount struct {
	Name  string
	Count int
}

// BrandsForCategory returns distinct brands in category (case-insensitively
// merged) matching query, sorted alphabetically.
func BrandsForCategory(entries []Entry, category, query string) []BrandCount {
	type brandAgg struct {
		name  string
		count int
	}
	aggs := map[string]*brandAgg{}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, e := range entries {
		if category != "" && !strings.EqualFold(e.Category, category) {
			continue
		}
		brand := strings.TrimSpace(e.Brand)
		if brand == "" {
			brand = "Unknown"
		}
		low := strings.ToLower(brand)
		if q != "" && !strings.Contains(low, q) {
			continue
		}
		agg, ok := aggs[low]
		if !ok {
			aggs[low] = &brandAgg{name: brand, count: 1}
		} else {
			agg.count++
			// Prefer mixed/title-cased brand display over ALL-CAPS when merging.
			if isAllUpper(agg.name) && !isAllUpper(brand) {
				agg.name = brand
			}
		}
	}
	out := make([]BrandCount, 0, len(aggs))
	for _, a := range aggs {
		out = append(out, BrandCount{Name: a.name, Count: a.count})
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if li != lj {
			return li < lj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func isAllUpper(s string) bool {
	hasLetter := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= 'A' && r <= 'Z' {
			hasLetter = true
		}
	}
	return hasLetter
}

func EntriesForBrand(entries []Entry, category, brand string) []Entry {
	out := make([]Entry, 0)
	for _, e := range entries {
		if category != "" && !strings.EqualFold(e.Category, category) {
			continue
		}
		if strings.EqualFold(e.Brand, brand) {
			out = append(out, e)
		}
	}
	return out
}

// EntriesForCategory returns all entries in category, optionally ordering
// preferBrand entries first so follow-up universal button blasts hit faster.
func EntriesForCategory(entries []Entry, category, preferBrand string) []Entry {
	out := make([]Entry, 0)
	var rest []Entry
	for _, e := range entries {
		if category != "" && !strings.EqualFold(e.Category, category) {
			continue
		}
		if preferBrand != "" && strings.EqualFold(e.Brand, preferBrand) {
			out = append(out, e)
		} else {
			rest = append(rest, e)
		}
	}
	return append(out, rest...)
}

// FindPowerSignal selects the best power-toggle/on/off signal from a remote.
func FindPowerSignal(sigs []Signal) (Signal, bool) {
	if matches := SignalsForTarget(sigs, "power"); len(matches) > 0 {
		return matches[0], true
	}
	for _, s := range sigs {
		if len(s.Timings) >= 3 {
			return s, true
		}
	}
	return Signal{}, false
}

// CollectBrandPowerCandidates scans all entries for (category, brand) and
// returns deduplicated Power signals paired with the richest remote Entry.
func CollectBrandPowerCandidates(entries []Entry, category, brand string) ([]MatchCandidate, error) {
	brandEntries := EntriesForBrand(entries, category, brand)
	if len(brandEntries) == 0 {
		return nil, fmt.Errorf("no %s remotes found for %s", category, brand)
	}

	type scored struct {
		cand MatchCandidate
		tier int
		seq  int
	}
	var collected []scored
	idxByFP := make(map[string]int, 64)
	seq := 0

	for _, e := range brandEntries {
		r, err := LoadEntry(e)
		if err != nil {
			continue
		}
		validCount := 0
		for _, s := range r.Signals {
			if len(s.Timings) >= 3 {
				validCount++
			}
		}
		sigs := SignalsForTarget(r.Signals, "power")
		fallback := false
		if len(sigs) == 0 {
			if first, ok := FindPowerSignal(r.Signals); ok {
				sigs = []Signal{first}
				fallback = true
			}
		}
		for _, sig := range sigs {
			sig = sig.Normalized()
			if sig.Validate() != nil {
				continue
			}
			tier := powerTier(sig.Name, fallback)
			fp := SignalFingerprint(sig)
			if existingIdx, dup := idxByFP[fp]; dup {
				cur := &collected[existingIdx]
				if tier < cur.tier || (tier == cur.tier && validCount > cur.cand.SignalCount) {
					cur.cand = MatchCandidate{Entry: e, Signal: sig, SignalCount: validCount}
					cur.tier = tier
				}
				continue
			}
			idxByFP[fp] = len(collected)
			collected = append(collected, scored{
				cand: MatchCandidate{Entry: e, Signal: sig, SignalCount: validCount},
				tier: tier,
				seq:  seq,
			})
			seq++
		}
	}

	if len(collected) == 0 {
		return nil, fmt.Errorf("no usable Power signals for %s %s", brand, category)
	}

	sort.SliceStable(collected, func(i, j int) bool {
		if collected[i].tier != collected[j].tier {
			return collected[i].tier < collected[j].tier
		}
		return collected[i].seq < collected[j].seq
	})

	out := make([]MatchCandidate, len(collected))
	for i, c := range collected {
		out[i] = c.cand
	}
	return out, nil
}

func powerTier(name string, fallback bool) int {
	if fallback {
		return 3
	}
	n := normalizeName(name)
	switch {
	case n == "power" || n == "pwr" || n == "power_toggle":
		return 0
	case n == "power_r" || n == "power_on" || n == "on" || strings.HasPrefix(n, "power"):
		return 1
	default:
		return 2
	}
}

// BindMatchedRemote loads the full remote for cand and produces a saved Profile
// with the confirmed Power signal pinned to key "1".
func BindMatchedRemote(cand MatchCandidate) (Profile, error) {
	r, err := LoadEntry(cand.Entry)
	if err != nil {
		return Profile{}, err
	}
	winSig := cand.Signal.Normalized()
	if strings.TrimSpace(winSig.Name) == "" {
		winSig.Name = "Power"
	}
	winFP := SignalFingerprint(winSig)

	sigs := make([]Signal, 0, len(r.Signals)+1)
	sigs = append(sigs, winSig)
	for _, s := range r.Signals {
		if len(s.Timings) < 3 {
			continue
		}
		sn := s.Normalized()
		if SignalFingerprint(sn) == winFP {
			continue
		}
		sigs = append(sigs, sn)
	}
	name := r.ProfileName()
	if strings.TrimSpace(name) == "" {
		name = cand.Entry.Display()
	}
	p := BindSignals(name, r.Provider, r.Category, r.Brand, r.Model, sigs)
	if len(p.Bindings) == 0 {
		return Profile{}, fmt.Errorf("no usable signals in %s", cand.Entry.Display())
	}
	return p, nil
}

// SignalsForTarget returns signals in sigs matching a logical universal button target.
func SignalsForTarget(sigs []Signal, target string) []Signal {
	var out []Signal
	for _, s := range sigs {
		if len(s.Timings) < 3 {
			continue
		}
		if matchesUniversalTarget(s.Name, target) {
			out = append(out, s)
		}
	}
	return out
}

func matchesUniversalTarget(name, target string) bool {
	n := normalizeName(name)
	switch target {
	case "power":
		return n == "power" || n == "pwr" || n == "power_r" || n == "power_on" || n == "power_off" ||
			n == "on" || n == "off" || strings.HasPrefix(n, "power") || strings.HasPrefix(n, "pwr")
	case "on":
		return n == "on" || n == "power_on" || n == "power"
	case "off":
		return n == "off" || n == "power_off"
	case "vol_up":
		return n == "vol+" || n == "vol_up" || n == "volume_up" || n == "vol_plus" || n == "volume+"
	case "vol_dn":
		return n == "vol-" || n == "vol_dn" || n == "vol_down" || n == "volume_down" || n == "vol_minus" || n == "volume-"
	case "mute":
		return strings.Contains(n, "mute")
	case "ch_up":
		return n == "ch+" || n == "ch_up" || n == "ch_next" || n == "channel_up" || n == "channel+"
	case "ch_dn":
		return n == "ch-" || n == "ch_dn" || n == "ch_prev" || n == "ch_down" || n == "channel_down" || n == "channel-"
	case "temp_up":
		return n == "temp+" || n == "temp_up" || n == "temperature+" || n == "temperature_up" || strings.HasPrefix(n, "temp_up")
	case "temp_dn":
		return n == "temp-" || n == "temp_dn" || n == "temp_down" || n == "temperature-" || n == "temperature_down" || strings.HasPrefix(n, "temp_d")
	case "mode":
		return n == "mode" || n == "cool" || n == "heat" || n == "auto" || n == "dry" || n == "wind_type"
	case "fan":
		return n == "fan" || n == "speed" || n == "wind_speed" || n == "fan_speed"
	case "swing":
		return n == "swing" || n == "swing_r" || n == "osc" || n == "oscillate" || n == "oscillation" || n == "shake_wind" || n == "rotate" || n == "lr_wind_mode"
	case "timer":
		return n == "timer" || n == "sleep"
	case "light":
		return n == "light" || n == "led"
	case "white", "red", "green", "blue", "fade":
		return n == target
	case "up":
		return n == "up" || n == "arrow_up"
	case "down":
		return n == "down" || n == "arrow_down"
	case "left":
		return n == "left" || n == "arrow_left"
	case "right":
		return n == "right" || n == "arrow_right"
	case "ok":
		return n == "ok" || n == "enter" || n == "select" || n == "confirm"
	case "back":
		return n == "back" || n == "return" || n == "exit"
	case "menu":
		return n == "menu" || n == "home" || n == "setup"
	case "source":
		return n == "source" || n == "input" || n == "hdmi" || n == "av"
	case "play":
		return n == "play" || n == "play_pause"
	case "pause":
		return n == "pause" || n == "stop"
	default:
		return n == normalizeName(target)
	}
}

// SignalFingerprint creates a jitter-tolerant key for deduplicating IR signals.
func SignalFingerprint(sig Signal) string {
	if p := strings.ToUpper(strings.TrimSpace(sig.Protocol)); p != "" && p != "RAW" {
		return fmt.Sprintf("%s:%x:%x", p, sig.Address, sig.Command)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "RAW:%d:", sig.Frequency/1000)
	limit := len(sig.Timings)
	if limit > 80 {
		limit = 80
	}
	for i := 0; i < limit; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		bucket := (sig.Timings[i] + 50) / 100
		fmt.Fprintf(&b, "%d", bucket)
	}
	return b.String()
}

// BlastUniversal cycles through every unique matching signal in entries for
// target until complete or ctx is cancelled.
func BlastUniversal(ctx context.Context, txPath string, entries []Entry, target string, onProgress func(UniversalProgress)) error {
	if strings.TrimSpace(txPath) == "" {
		return fmt.Errorf("no IR transmitter")
	}
	tx, err := OpenTransmitter(txPath)
	if err != nil {
		return err
	}
	defer tx.Close()
	return BlastUniversalWithSender(ctx, tx.Send, 110*time.Millisecond, entries, target, onProgress)
}

// BlastUniversalWithSender executes the universal blast loop using sendFn,
// allowing unit tests to exercise deduplication, validation, and cancellation.
func BlastUniversalWithSender(ctx context.Context, sendFn func(Signal) error, gap time.Duration, entries []Entry, target string, onProgress func(UniversalProgress)) error {
	if sendFn == nil {
		return fmt.Errorf("no IR sender")
	}
	seen := make(map[string]struct{}, 256)
	uniqueSent := 0
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		r, err := LoadEntry(e)
		if err != nil {
			continue
		}
		sigs := SignalsForTarget(r.Signals, target)
		for _, sig := range sigs {
			if err := ctx.Err(); err != nil {
				return err
			}
			sig = sig.Normalized()
			if sig.Validate() != nil {
				continue
			}
			fp := SignalFingerprint(sig)
			if _, dup := seen[fp]; dup {
				continue
			}
			seen[fp] = struct{}{}
			uniqueSent++
			if onProgress != nil {
				onProgress(UniversalProgress{
					Target:       target,
					EntryIndex:   i + 1,
					TotalEntries: len(entries),
					UniqueSent:   uniqueSent,
					Current:      e,
					Signal:       sig,
				})
			}
			if err := sendFn(sig); err != nil {
				return err
			}
			if gap > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(gap):
				}
			}
		}
	}
	if uniqueSent == 0 {
		return fmt.Errorf("no %s signals found in %d remotes", target, len(entries))
	}
	return nil
}
