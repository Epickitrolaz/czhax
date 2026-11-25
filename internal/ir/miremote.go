package ir

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// TVKILL JSON is the export format used by the mi_remote_database project
// (derived from the Xiaomi Mi Remote app). Pattern values are carrier cycles;
// convert them to microseconds with the pattern frequency.
type tvkillPattern struct {
	Comment   string    `json:"comment"`
	Frequency float64   `json:"frequency"`
	Pattern   []float64 `json:"pattern"`
}

type tvkillDevice struct {
	Designation string          `json:"designation"`
	Patterns    []tvkillPattern `json:"patterns"`
}

func ParseMiRemoteJSON(path string) (Remote, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Remote{}, err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	r := Remote{Name: base, Provider: "mi", Path: path}

	var devices []tvkillDevice
	if err := json.Unmarshal(data, &devices); err != nil {
		var one tvkillDevice
		if err2 := json.Unmarshal(data, &one); err2 != nil {
			return Remote{}, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}
		devices = []tvkillDevice{one}
	}
	for _, dev := range devices {
		for _, p := range dev.Patterns {
			freq := int(math.Round(p.Frequency))
			if freq <= 0 {
				freq = 37960
			}
			if len(p.Pattern) < 4 {
				continue
			}
			timings := make([]int, 0, len(p.Pattern))
			for _, cycles := range p.Pattern {
				us := int(math.Round(cycles * 1e6 / float64(freq)))
				if us > 0 {
					timings = append(timings, us)
				}
			}
			if len(timings) < 4 {
				continue
			}
			comment := strings.TrimSpace(p.Comment)
			if dev.Designation != "" {
				comment = strings.TrimSpace(dev.Designation + " " + comment)
			}
			r.Signals = append(r.Signals, Signal{
				Name:      "Power",
				Comment:   comment,
				Protocol:  "raw",
				Frequency: freq,
				DutyCycle: DefaultDutyCycle,
				Timings:   normalizeTimings(timings),
				Source:    "mi",
			})
		}
	}
	if len(r.Signals) == 0 {
		return r, fmt.Errorf("no usable patterns in %s", filepath.Base(path))
	}
	return r, nil
}
