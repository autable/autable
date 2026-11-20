// Package wfsdk holds the TypeScript SDK that workflow projects vendor, and
// the rules for which SDK versions this server still runs. The SDK is built
// on the host API in docs/design/workflows-host-api.md. See
// docs/design/workflows-v2.md.
package wfsdk

import (
	"embed"

	"autable/internal/sdkversion"
)

// The SDK version is MAJOR.MINOR, with the same rules as the app SDK: bump
// Minor when the SDK (or the host API) gains API, bump Major (and reset
// Minor) when old workflows would break, and raise MinMajor only when this
// server stops running an old major.
const (
	Major    = 1
	Minor    = 0
	MinMajor = 1
)

//go:embed files
var embedded embed.FS

var sdkRange = sdkversion.Range{Kind: "workflow", RefreshCommand: "autablectl workflow sdk", Major: Major, Minor: Minor, MinMajor: MinMajor}

// Version returns the SDK version this server ships.
func Version() string {
	return sdkRange.Version()
}

// Files returns the SDK source files keyed by file name.
func Files() map[string]string {
	return sdkversion.Files(embedded, "files")
}

// Check returns nil when a workflow built against version can run on this
// server, and an error explaining why not otherwise.
func Check(version string) error {
	return sdkRange.Check(version)
}
