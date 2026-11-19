// Package appbundle reads the archives autablectl uploads for hosted apps:
// a gzip-compressed tar holding autable.app.json, the built dist/ tree and
// the project source under source/. See docs/design/apps.md.
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
	ManifestName = "autable.app.json"
	distPrefix   = "dist/"
	sourcePrefix = "source/"

	// MaxCompressedBytes bounds an upload; MaxUncompressedBytes and
	// MaxEntries bound what it may expand to.
	MaxCompressedBytes   = 30 << 20
	MaxUncompressedBytes = 200 << 20
	MaxEntries           = 5000
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidName reports whether name can be an app's URL slug.
func ValidName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("app name %q must match %s", name, namePattern)
	}
	return nil
}

type Manifest struct {
	Database string `json:"database"`
	Name     string `json:"name"`
	Title    string `json:"title,omitempty"`
	SDK      string `json:"sdk"`
}

// Bundle is a parsed upload. Dist maps paths below dist/ to file contents.
type Bundle struct {
	Manifest Manifest
	Dist     map[string][]byte
}

// Parse validates an uploaded archive and returns its manifest and dist
// files. Source files are checked for safe paths but not kept.
func Parse(archive []byte) (Bundle, error) {
	if len(archive) > MaxCompressedBytes {
		return Bundle{}, fmt.Errorf("bundle is %d bytes; the limit is %d", len(archive), MaxCompressedBytes)
	}
	bundle := Bundle{Dist: map[string][]byte{}}
	manifestSeen := false
	err := walk(archive, func(name string, body []byte) error {
		switch {
		case name == ManifestName:
			manifestSeen = true
			// Unknown fields are ignored so newer autablectl releases can
			// add manifest fields without breaking uploads to older servers.
			if err := json.Unmarshal(body, &bundle.Manifest); err != nil {
				return fmt.Errorf("%s: %w", ManifestName, err)
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
		return Bundle{}, fmt.Errorf("bundle has no %s", ManifestName)
	}
	if _, ok := bundle.Dist["index.html"]; !ok {
		return Bundle{}, errors.New("bundle has no dist/index.html")
	}
	if err := ValidName(bundle.Manifest.Name); err != nil {
		return Bundle{}, err
	}
	if bundle.Manifest.Database == "" {
		return Bundle{}, fmt.Errorf("%s: database is required", ManifestName)
	}
	return bundle, nil
}

// Source returns a gzip-compressed tar of the manifest and the source tree,
// with the source/ prefix removed, ready to unpack as a project directory.
func Source(archive []byte) ([]byte, error) {
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	err := walk(archive, func(name string, body []byte) error {
		target := ""
		switch {
		case name == ManifestName:
			target = ManifestName
		case name == sourcePrefix+ManifestName:
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
