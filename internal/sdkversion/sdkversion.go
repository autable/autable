// Package sdkversion implements the MAJOR.MINOR compatibility rule shared
// by the SDKs autable embeds (apps and workflows). See docs/design/apps.md.
package sdkversion

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// Range describes one SDK a server ships: its current version and the
// oldest major it still runs. A minor release only adds API; a major
// release may change or remove it.
type Range struct {
	// Kind names the SDK in messages, e.g. "app" or "workflow".
	Kind string
	// RefreshCommand is the command that updates a project's SDK.
	RefreshCommand string
	Major          int
	Minor          int
	MinMajor       int
}

// Version returns the version this server ships, MAJOR.MINOR.
func (r Range) Version() string {
	return fmt.Sprintf("%d.%d", r.Major, r.Minor)
}

// Check returns nil when code built against version can run on this
// server, and an error explaining why not otherwise.
func (r Range) Check(version string) error {
	major, minor, err := r.parse(version)
	if err != nil {
		return err
	}
	switch {
	case major < r.MinMajor:
		return fmt.Errorf("%s SDK %s is no longer supported: this server runs SDK %d.x to %s; update the SDK with `%s` and rebuild", r.Kind, version, r.MinMajor, r.Version(), r.RefreshCommand)
	case major > r.Major || (major == r.Major && minor > r.Minor):
		return fmt.Errorf("%s SDK %s is newer than this server's SDK %s; upgrade autable or rebuild with this server's SDK", r.Kind, version, r.Version())
	}
	return nil
}

func (r Range) parse(version string) (int, int, error) {
	invalid := fmt.Errorf("%s SDK version %q must be MAJOR.MINOR", r.Kind, version)
	majorText, minorText, ok := strings.Cut(version, ".")
	if !ok {
		return 0, 0, invalid
	}
	major, err := strconv.Atoi(majorText)
	if err != nil || major < 0 {
		return 0, 0, invalid
	}
	minor, err := strconv.Atoi(minorText)
	if err != nil || minor < 0 {
		return 0, 0, invalid
	}
	return major, minor, nil
}

// Files returns the regular files directly inside dir of fsys, keyed by
// file name. It panics when dir cannot be read: SDK files are embedded at
// build time, so a failure is a build defect.
func Files(fsys fs.FS, dir string) map[string]string {
	files := map[string]string{}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(fsys, dir+"/"+entry.Name())
		if err != nil {
			panic(err)
		}
		files[entry.Name()] = string(data)
	}
	return files
}
