package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"autable/internal/appbundle"
)

// The git repository is the deployment's readable backup: besides the
// metadata, every pushed app and workflow version is written there as
// source, so the repository history holds every version as plain code.
//
//	app/<database>/<name>/        source of the latest pushed version
//	workflow/<database>/<name>/   same for workflows
//	lib/<name>/                   same for libraries, which are instance-wide
//	.../.autable-version.json     which version that is, and which is published
//
// A version uploaded with files from outside its project (a shared lib/ of
// the development repository) carries a slice of that repository; the
// mirror holds the slice as is, with the project at <root>/ and the
// shared files at their repository paths, and .autable-version.json
// records the root.

const versionFileName = ".autable-version.json"

type mirroredVersion struct {
	Version          int64  `json:"version"`
	PublishedVersion int64  `json:"published_version"`
	SDK              string `json:"sdk"`
	Message          string `json:"message,omitempty"`
	CreatedBy        string `json:"created_by,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	// Root is the project's path within the mirrored directory when the
	// version was uploaded as a repository slice.
	Root string `json:"root,omitempty"`
}

func (server *Server) SetRepositoryRoot(root string) {
	server.repositoryRoot = root
}

var unsafeSegment = regexp.MustCompile(`[^\pL\pN._-]+`)

// repositorySegment turns a database or app name into a safe directory name.
func repositorySegment(name string) string {
	segment := strings.Trim(unsafeSegment.ReplaceAllString(name, "-"), "-.")
	if segment == "" {
		return "unnamed"
	}
	return segment
}

func mirrorRelativePath(kind appbundle.Kind, databaseName, name string) string {
	if kind.Instance {
		return path.Join(kind.Name, repositorySegment(name))
	}
	return path.Join(kind.Name, repositorySegment(databaseName), repositorySegment(name))
}

// mirrorVersion replaces the mirrored source with the given upload and
// records it in the repository. Failures are logged, not returned: the
// version is already stored, and the mirror is a backup.
func (server *Server) mirrorVersion(ctx context.Context, actorID string, kind appbundle.Kind, databaseName, name string, archive []byte, version mirroredVersion) {
	if server.repositoryRoot == "" {
		return
	}
	relative := mirrorRelativePath(kind, databaseName, name)
	err := func() error {
		bundle, err := kind.Parse(archive)
		if err != nil {
			return err
		}
		source, err := kind.Source(archive)
		if err != nil {
			return err
		}
		dir := filepath.Join(server.repositoryRoot, filepath.FromSlash(relative))
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := extractTarGz(source, dir); err != nil {
			return err
		}
		if version.Root = bundle.Manifest.Root; version.Root != "" {
			// The top manifest only locates the project, whose own copy
			// is at <root>/; .autable-version.json records the root.
			if err := os.Remove(filepath.Join(dir, kind.ManifestName)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return writeVersionFile(dir, version)
	}()
	if err != nil {
		slog.Warn("mirror version into repository", "path", relative, "error", err)
		return
	}
	summary := fmt.Sprintf("pushed %s %s v%d", kind.Name, mirrorLabel(kind, databaseName, name), version.Version)
	if version.Message != "" {
		summary += ": " + version.Message
	}
	if version.PublishedVersion == version.Version {
		summary += " (published)"
	}
	server.notifyRepositoryChange(ctx, actorID, kind.Name+".push", summary, relative)
}

// mirrorPublish records a publish or rollback in the mirrored version file.
func (server *Server) mirrorPublish(ctx context.Context, actorID string, kind appbundle.Kind, databaseName, name string, published int64) {
	if server.repositoryRoot == "" {
		return
	}
	relative := mirrorRelativePath(kind, databaseName, name)
	dir := filepath.Join(server.repositoryRoot, filepath.FromSlash(relative))
	data, err := os.ReadFile(filepath.Join(dir, versionFileName))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var version mirroredVersion
	if err == nil {
		err = json.Unmarshal(data, &version)
	}
	if err == nil {
		version.PublishedVersion = published
		err = writeVersionFile(dir, version)
	}
	if err != nil {
		slog.Warn("mirror publish into repository", "path", relative, "error", err)
		return
	}
	server.notifyRepositoryChange(ctx, actorID, kind.Name+".publish", fmt.Sprintf("published %s %s v%d", kind.Name, mirrorLabel(kind, databaseName, name), published), relative)
}

// mirrorDelete removes the mirrored source of a deleted app or workflow.
func (server *Server) mirrorDelete(ctx context.Context, actorID string, kind appbundle.Kind, databaseName, name string) {
	if server.repositoryRoot == "" {
		return
	}
	relative := mirrorRelativePath(kind, databaseName, name)
	if err := os.RemoveAll(filepath.Join(server.repositoryRoot, filepath.FromSlash(relative))); err != nil {
		slog.Warn("remove mirrored source from repository", "path", relative, "error", err)
		return
	}
	server.notifyRepositoryChange(ctx, actorID, kind.Name+".delete", fmt.Sprintf("deleted %s %s", kind.Name, mirrorLabel(kind, databaseName, name)), relative)
}

// mirrorLabel names an app, workflow or library in commit summaries.
func mirrorLabel(kind appbundle.Kind, databaseName, name string) string {
	if kind.Instance {
		return name
	}
	return databaseName + "/" + name
}

func writeVersionFile(dir string, version mirroredVersion) error {
	data, err := json.MarshalIndent(version, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, versionFileName), append(data, '\n'), 0o644)
}

// extractTarGz unpacks an archive produced by Kind.Source, whose entry
// names were already validated as clean relative paths.
func extractTarGz(archive []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(header.Name))
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(filepath.Separator)) {
			return fmt.Errorf("source entry %q escapes the directory", header.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			return err
		}
	}
}
