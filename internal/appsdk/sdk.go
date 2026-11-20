// Package appsdk holds the TypeScript SDK that hosted apps vendor, and the
// rules for which SDK versions this server still runs. See
// docs/design/apps.md.
package appsdk

import (
	"embed"

	"autable/internal/sdkversion"
)

// The SDK version is MAJOR.MINOR. A minor release only adds API; a major
// release may change or remove it. Bump Minor when the SDK gains API,
// bump Major (and reset Minor) when old apps would break, and raise
// MinMajor only when this server stops serving an old major.
const (
	Major    = 1
	Minor    = 0
	MinMajor = 1
)

// Header carries the SDK version on every API call an app makes.
const Header = "X-Autable-SDK"

//go:embed files
var embedded embed.FS

// Version returns the SDK version this server ships.
func Version() string {
	return sdkRange.Version()
}

// Files returns the SDK source files keyed by file name.
func Files() map[string]string {
	return sdkversion.Files(embedded, "files")
}

// Check returns nil when an app built against version can run on this
// server, and an error explaining why not otherwise.
func Check(version string) error {
	return sdkRange.Check(version)
}

var sdkRange = sdkversion.Range{Kind: "app", RefreshCommand: "autablectl app sdk", Major: Major, Minor: Minor, MinMajor: MinMajor}
