// Package appbundle reads the archives autablectl uploads for hosted apps
// and workflows: a gzip-compressed tar holding the manifest
// (autable.app.json or autable.workflow.json), the built dist/ tree and the
// project source under source/. See docs/design/apps.md and
// docs/design/workflows-v2.md.
package appbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

const (
	// ManifestName is the manifest of an app bundle.
	ManifestName = "autable.app.json"
	// WorkflowManifestName is the manifest of a workflow bundle.
	WorkflowManifestName = "autable.workflow.json"
	// LibraryManifestName is the manifest of a library bundle.
	LibraryManifestName = "autable.lib.json"
	distPrefix          = "dist/"
	sourcePrefix        = "source/"

	// MaxCompressedBytes bounds an upload; MaxUncompressedBytes and
	// MaxEntries bound what it may expand to.
	MaxCompressedBytes   = 30 << 20
	MaxUncompressedBytes = 200 << 20
	MaxEntries           = 5000
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Kind describes one bundle flavour: its manifest file and the dist files
// it must contain.
type Kind struct {
	Name         string
	ManifestName string
	// RequiredDist is the file the server loads; OtherDist lists further
	// files a bundle must have.
	RequiredDist string
	OtherDist    []string
	// Instance kinds (libraries) belong to no database.
	Instance bool
}

var (
	App      = Kind{Name: "app", ManifestName: ManifestName, RequiredDist: "index.html"}
	Workflow = Kind{Name: "workflow", ManifestName: WorkflowManifestName, RequiredDist: "workflow.js"}
	// Library bundles hold the module (dist/lib.js, an IIFE assigning
	// __lib) and its type declarations for importers (dist/index.d.ts).
	Library = Kind{Name: "lib", ManifestName: LibraryManifestName, RequiredDist: "lib.js", OtherDist: []string{"index.d.ts"}, Instance: true}
)

// ValidName reports whether name can be an app's or workflow's URL slug.
func ValidName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("name %q must match %s", name, namePattern)
	}
	return nil
}

// Manifest is autable.app.json, autable.workflow.json or autable.lib.json.
// Libraries have no database.
type Manifest struct {
	Database string `json:"database,omitempty"`
	Name     string `json:"name"`
	Title    string `json:"title,omitempty"`
	SDK      string `json:"sdk"`
	// Root is set by autablectl, only in uploads, when the build used files
	// outside the project directory (a shared lib/ of the development
	// repository): source/ then mirrors the repository from its root, and
	// Root is the slash-separated path of the project within it. Empty
	// means source/ is the project directory itself.
	Root string `json:"root,omitempty"`
}

// ValidRoot reports whether root can be a manifest's Root: empty, or a
// clean relative slash-separated path inside the source tree.
func ValidRoot(root string) error {
	if root == "" {
		return nil
	}
	if strings.HasPrefix(root, "/") || strings.Contains(root, "\\") || strings.Contains(root, ":") ||
		path.Clean(root) != root || root == "." || root == ".." || strings.HasPrefix(root, "../") {
		return fmt.Errorf("root %q must be a relative path inside the source tree", root)
	}
	return nil
}

// Bundle is a parsed upload. Dist maps paths below dist/ to file contents.
type Bundle struct {
	Manifest Manifest
	Dist     map[string][]byte
}

// Parse validates an uploaded app archive; see Kind.Parse.
func Parse(archive []byte) (Bundle, error) {
	return App.Parse(archive)
}

// Source returns the source of an app archive; see Kind.Source.
func Source(archive []byte) ([]byte, error) {
	return App.Source(archive)
}

// Parse validates an uploaded archive and returns its manifest and dist
// files. Source files are checked for safe paths but not kept.
func (kind Kind) Parse(archive []byte) (Bundle, error) {
	if len(archive) > MaxCompressedBytes {
		return Bundle{}, fmt.Errorf("bundle is %d bytes; the limit is %d", len(archive), MaxCompressedBytes)
	}
	bundle := Bundle{Dist: map[string][]byte{}}
	manifestSeen := false
	err := walk(archive, func(name string, body []byte) error {
		switch {
		case name == kind.ManifestName:
			manifestSeen = true
			// Unknown fields are ignored so newer autablectl releases can
			// add manifest fields without breaking uploads to older servers.
			if err := json.Unmarshal(body, &bundle.Manifest); err != nil {
				return fmt.Errorf("%s: %w", kind.ManifestName, err)
			}
		case strings.HasPrefix(name, distPrefix):
			bundle.Dist[strings.TrimPrefix(name, distPrefix)] = body
		case strings.HasPrefix(name, sourcePrefix):
		default:
			return fmt.Errorf("unexpected bundle entry %q: entries belong under dist/ or source/", name)
		}
		return nil
	})
	if err != nil {
		return Bundle{}, err
	}
	if !manifestSeen {
		return Bundle{}, fmt.Errorf("bundle has no %s", kind.ManifestName)
	}
	for _, required := range append([]string{kind.RequiredDist}, kind.OtherDist...) {
		if _, ok := bundle.Dist[required]; !ok {
			return Bundle{}, fmt.Errorf("bundle has no dist/%s", required)
		}
	}
	if err := ValidName(bundle.Manifest.Name); err != nil {
		return Bundle{}, err
	}
	switch {
	case kind.Instance && bundle.Manifest.Database != "":
		return Bundle{}, fmt.Errorf("%s: a %s belongs to no database; remove database", kind.ManifestName, kind.Name)
	case !kind.Instance && bundle.Manifest.Database == "":
		return Bundle{}, fmt.Errorf("%s: database is required", kind.ManifestName)
	}
	if err := ValidRoot(bundle.Manifest.Root); err != nil {
		return Bundle{}, fmt.Errorf("%s: %w", kind.ManifestName, err)
	}
	return bundle, nil
}

// Source returns a gzip-compressed tar of the manifest and the source tree,
// with the source/ prefix removed, ready to unpack as a project directory.
// When the manifest has a Root, the tree is a slice of the development
// repository: the manifest (with root) stays at the top, telling
// autablectl where the project is, and the project's own copy stays at
// <root>/<manifest>.
func (kind Kind) Source(archive []byte) ([]byte, error) {
	manifest, err := kind.readManifest(archive)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	err = walk(archive, func(name string, body []byte) error {
		target := ""
		switch {
		case name == kind.ManifestName:
			target = kind.ManifestName
		case name == sourcePrefix+kind.ManifestName && manifest.Root == "":
			// The root manifest is the authoritative copy of the same file.
			return nil
		case strings.HasPrefix(name, sourcePrefix):
			target = strings.TrimPrefix(name, sourcePrefix)
		default:
			return nil
		}
		if err := tw.WriteHeader(&tar.Header{Name: target, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// readManifest returns the manifest of an archive without validating the
// rest; a missing manifest reads as the zero Manifest.
func (kind Kind) readManifest(archive []byte) (Manifest, error) {
	var manifest Manifest
	err := walk(archive, func(name string, body []byte) error {
		if name != kind.ManifestName {
			return nil
		}
		if err := json.Unmarshal(body, &manifest); err != nil {
			return fmt.Errorf("%s: %w", kind.ManifestName, err)
		}
		return ValidRoot(manifest.Root)
	})
	return manifest, err
}

// walk calls visit for every regular file in the archive, enforcing the
// size, count and path rules. Directory entries are skipped.
func walk(archive []byte, visit func(name string, body []byte) error) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("bundle is not gzip: %w", err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var total int64
	entries := 0
	seen := map[string]bool{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("bundle is not a valid tar archive: %w", err)
		}
		entries++
		if entries > MaxEntries {
			return fmt.Errorf("bundle has more than %d entries", MaxEntries)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("bundle entry %q is not a regular file", header.Name)
		}
		name, err := cleanName(header.Name)
		if err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("bundle entry %q appears twice", name)
		}
		seen[name] = true
		total += header.Size
		if header.Size < 0 || total > MaxUncompressedBytes {
			return fmt.Errorf("bundle expands beyond %d bytes", MaxUncompressedBytes)
		}
		body, err := io.ReadAll(io.LimitReader(reader, header.Size))
		if err != nil {
			return fmt.Errorf("read bundle entry %q: %w", name, err)
		}
		if err := visit(name, body); err != nil {
			return err
		}
	}
}

func cleanName(name string) (string, error) {
	trimmed := strings.TrimPrefix(name, "./")
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return "", fmt.Errorf("bundle entry %q has an unsafe path", name)
	}
	cleaned := path.Clean(trimmed)
	if cleaned != trimmed || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("bundle entry %q has an unsafe path", name)
	}
	return cleaned, nil
}
