package oui

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

//go:embed vendors.txt.gz
var compressed []byte

var (
	once   sync.Once
	vendor map[uint32]string
)

func load() {
	vendor = make(map[uint32]string, 40565)
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return
	}
	defer zr.Close()
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 0, 4096), 256*1024)
	for sc.Scan() {
		line := sc.Text()
		key, name, ok := strings.Cut(line, "\t")
		if !ok || len(key) != 6 || name == "" {
			continue
		}
		n, err := strconv.ParseUint(key, 16, 32)
		if err != nil {
			continue
		}
		vendor[uint32(n)] = name
	}
}

// Lookup returns the IEEE OUI organization for a MAC address, or "".
func Lookup(mac string) string {
	once.Do(load)
	hex := strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(strings.ToUpper(strings.TrimSpace(mac)))
	if len(hex) < 6 {
		return ""
	}
	n, err := strconv.ParseUint(hex[:6], 16, 32)
	if err != nil {
		return ""
	}
	return vendor[uint32(n)]
}
