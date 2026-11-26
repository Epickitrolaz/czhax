package ir

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUniversalCategoryBrandAndDeduplication(t *testing.T) {
	root := t.TempDir()
	acEpson1 := filepath.Join(root, "flipper", "ACs", "Epson", "E1.ir")
	acEpson2 := filepath.Join(root, "mi", "ACs", "EPSON", "E2.ir")
	acDaikin := filepath.Join(root, "mi", "ACs", "Daikin", "D1.ir")
	tvSamsung := filepath.Join(root, "flipper", "TVs", "Samsung", "S1.ir")
	for _, p := range []string{acEpson1, acEpson2, acDaikin, tvSamsung} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// E1 has only Power (same code as E2's POWER), while E2 has Power + Temp+ + Temp- + Mode + Fan.
	writeIR(t, acEpson1, `Filetype: IR signals file
Version: 1
#
name: Power
type: parsed
protocol: NEC
address: 10 00 00 00
command: 20 00 00 00
`)
	writeIR(t, acEpson2, `Filetype: IR signals file
Version: 1
#
name: POWER
type: parsed
protocol: NEC
address: 10 00 00 00
command: 20 00 00 00
#
name: POWER_R
type: parsed
protocol: NEC
address: 10 00 00 00
command: 21 00 00 00
#
name: TEMP+
type: parsed
protocol: NEC
address: 10 00 00 00
command: 22 00 00 00
#
name: TEMP-
type: parsed
protocol: NEC
address: 10 00 00 00
command: 23 00 00 00
#
name: Mode
type: parsed
protocol: NEC
address: 10 00 00 00
command: 24 00 00 00
#
name: Fan
type: parsed
protocol: NEC
address: 10 00 00 00
command: 25 00 00 00
`)
	writeIR(t, acDaikin, `Filetype: IR signals file
Version: 1
#
name: POWER
type: parsed
protocol: NEC
address: 30 00 00 00
command: 01 00 00 00
#
name: TEMP+
type: parsed
protocol: NEC
address: 30 00 00 00
command: 02 00 00 00
`)
	writeIR(t, tvSamsung, `Filetype: IR signals file
Version: 1
#
name: Power
type: parsed
protocol: Samsung32
address: 07 00 00 00
command: 02 00 00 00
`)

	entries, err := LoadIndex(Roots{
		Flipper: filepath.Join(root, "flipper"),
		Mi:      filepath.Join(root, "mi"),
	})
	if err != nil {
		t.Fatal(err)
	}

	cats := CategoryCounts(entries)
	if len(cats) != 2 || cats[0].Name != "TV" || cats[1].Name != "AC" {
		t.Fatalf("CategoryCounts = %+v", cats)
	}

	brands := BrandsForCategory(entries, "AC", "")
	if len(brands) != 2 {
		t.Fatalf("expected 2 merged AC brands, got %+v", brands)
	}
	if brands[0].Name != "Daikin" || brands[1].Name != "Epson" || brands[1].Count != 2 {
		t.Fatalf("merged brands = %+v", brands)
	}

	filtered := BrandsForCategory(entries, "AC", "eps")
	if len(filtered) != 1 || filtered[0].Name != "Epson" {
		t.Fatalf("filtered brands = %+v", filtered)
	}

	// CollectBrandPowerCandidates should deduplicate E1 & E2's identical 0x10/0x20 Power
	// and keep E2 (6 signals > 1 signal) plus E2's POWER_R (0x10/0x21).
	cands, err := CollectBrandPowerCandidates(entries, "AC", "Epson")
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("expected 2 unique Power candidates, got %d: %+v", len(cands), cands)
	}
	if cands[0].Entry.Model != "E2" || cands[0].SignalCount != 6 {
		t.Fatalf("expected richer entry E2 to win dedup, got %+v", cands[0])
	}

	// BindMatchedRemote pins the confirmed Power signal (candidate 1: POWER_R) to key "1"
	// and keeps all remaining AC buttons in the saved profile.
	prof, err := BindMatchedRemote(cands[1])
	if err != nil {
		t.Fatal(err)
	}
	if prof.Category != "AC" || !strings.EqualFold(prof.Brand, "Epson") || len(prof.Bindings) < 5 {
		t.Fatalf("unexpected bound profile: %+v", prof)
	}
	if prof.Bindings[0].Key != "1" || prof.Bindings[0].Signal.Command != 0x21 {
		t.Fatalf("expected confirmed Power_R on key 1, got %+v", prof.Bindings[0])
	}
	hasTempUp := false
	for _, b := range prof.Bindings {
		if b.Key == "2" && b.Name == "TEMP+" {
			hasTempUp = true
		}
	}
	if !hasTempUp {
		t.Fatalf("expected TEMP+ on key 2 in AC profile: %+v", prof.Bindings)
	}

	// BlastUniversalWithSender: blasts unique signals for target in category, validates odd LIRC timings,
	// prioritizes preferBrand, and stops cleanly on context cancel.
	acEntries := EntriesForCategory(entries, "AC", "Epson")
	if len(acEntries) != 3 || acEntries[0].Brand != "Epson" {
		t.Fatalf("EntriesForCategory preferBrand order = %+v", acEntries)
	}

	var sent []Signal
	err = BlastUniversalWithSender(context.Background(), func(sig Signal) error {
		if err := sig.Validate(); err != nil {
			return err
		}
		sent = append(sent, sig)
		return nil
	}, 0, acEntries, "power", nil)
	if err != nil {
		t.Fatalf("BlastUniversalWithSender error: %v", err)
	}
	// E1 & E2 share 0x10/0x20, E2 has 0x10/0x21, Daikin has 0x30/0x01 => 3 unique power codes.
	if len(sent) != 3 {
		t.Fatalf("expected 3 deduplicated signals sent, got %d", len(sent))
	}

	// Verify cancellation stops mid-blast.
	ctx, cancel := context.WithCancel(context.Background())
	sent = nil
	err = BlastUniversalWithSender(ctx, func(sig Signal) error {
		sent = append(sent, sig)
		cancel()
		return nil
	}, 0, acEntries, "power", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("expected blast to stop after 1 signal on cancel, got %d", len(sent))
	}
}

func TestBundledCollectBrandPowerCandidatesEpson(t *testing.T) {
	entries, err := LoadIndex(DefaultRoots())
	if err != nil {
		t.Fatal(err)
	}
	cands, err := CollectBrandPowerCandidates(entries, "Projector", "Epson")
	if err != nil {
		t.Fatalf("CollectBrandPowerCandidates(Projector, Epson): %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("expected at least 1 Epson Projector power candidate")
	}
	for i, c := range cands {
		if err := c.Signal.Validate(); err != nil {
			t.Fatalf("candidate %d invalid LIRC timings: %v", i, err)
		}
	}
}

func TestUniversalButtonsForCategory(t *testing.T) {
	for _, cat := range []string{"AC", "TV", "Projector", "Fan", "Lights", "Audio", "DVD"} {
		btns := UniversalButtonsForCategory(cat)
		if len(btns) < 5 {
			t.Fatalf("category %s returned too few buttons: %+v", cat, btns)
		}
	}
}

func writeIR(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
