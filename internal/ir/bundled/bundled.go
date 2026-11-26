package bundled

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"sync"
)

//go:embed flipper.zip
var flipperZip []byte

//go:embed mi.zip
var miZip []byte

var (
	flipperOnce sync.Once
	flipperRdr  *zip.Reader
	flipperErr  error

	miOnce sync.Once
	miRdr  *zip.Reader
	miErr  error
)

// Reader returns the lazily initialized zip.Reader for a bundled IR database
// ("flipper" or "mi").
func Reader(provider string) (*zip.Reader, error) {
	switch provider {
	case "flipper":
		flipperOnce.Do(func() {
			flipperRdr, flipperErr = zip.NewReader(bytes.NewReader(flipperZip), int64(len(flipperZip)))
		})
		return flipperRdr, flipperErr
	case "mi":
		miOnce.Do(func() {
			miRdr, miErr = zip.NewReader(bytes.NewReader(miZip), int64(len(miZip)))
		})
		return miRdr, miErr
	default:
		return nil, fmt.Errorf("unknown bundled IR provider %q", provider)
	}
}
