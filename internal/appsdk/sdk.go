// Package appsdk holds the TypeScript SDK that hosted apps vendor, and the
// rules for which SDK versions this server still runs. See
// docs/design/apps.md.
package appsdk

import (
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
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
	return fmt.Sprintf("%d.%d", Major, Minor)
}

// Files returns the SDK source files keyed by file name.
func Files() map[string]string {
	files := map[string]string{}
	entries, err := fs.ReadDir(embedded, "files")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		data, err := embedded.ReadFile("files/" + entry.Name())
		if err != nil {
			panic(err)
		}
		files[entry.Name()] = string(data)
	}
	return files
}

// Check returns nil when an app built against version can run on this
// server, and an error explaining why not otherwise.
func Check(version string) error {
	major, minor, err := parse(version)
	if err != nil {
		return err
	}
	switch {
	case major < MinMajor:
		return fmt.Errorf("app SDK %s is no longer supported: this server runs SDK %d.x to %s; update the SDK with `autablectl app sdk` and rebuild", version, MinMajor, Version())
	case major > Major || (major == Major && minor > Minor):
		return fmt.Errorf("app SDK %s is newer than this server's SDK %s; upgrade autable or rebuild with this server's SDK", version, Version())
	}
	return nil
}

func parse(version string) (int, int, error) {
	majorText, minorText, ok := strings.Cut(version, ".")
	if !ok {
		return 0, 0, fmt.Errorf("app SDK version %q must be MAJOR.MINOR", version)
	}
	major, err := strconv.Atoi(majorText)
	if err != nil || major < 0 {
		return 0, 0, fmt.Errorf("app SDK version %q must be MAJOR.MINOR", version)
	}
	minor, err := strconv.Atoi(minorText)
	if err != nil || minor < 0 {
		return 0, 0, fmt.Errorf("app SDK version %q must be MAJOR.MINOR", version)
	}
	return major, minor, nil
}
