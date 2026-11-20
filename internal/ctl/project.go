package ctl

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"autable/internal/appbundle"
)

// Apps and workflows are both projects that autablectl scaffolds, builds
// with npm, packs into the same bundle format (manifest, dist/, source/),
// and uploads as immutable versions. projectKind holds what differs.

type projectKind struct {
	// bundle names the manifest and the file a build must produce below
	// dist/, as the server checks them.
	bundle appbundle.Kind
	// noun names the kind in commands and messages: "app" or "workflow".
	noun string
	// collection is the API path segment below a database.
	collection string
	// sdkPath serves the SDK files to vendor.
	sdkPath string
	// sdkMarker starts every SDK file the server ships, so `sdk` can
	// recognize vendored files it may replace or remove.
	sdkMarker string
}

var (
	appProject = projectKind{
		bundle:     appbundle.App,
		noun:       "app",
		collection: "apps",
		sdkPath:    "/api/apps/sdk",
		sdkMarker:  "// autable app SDK.",
	}
	workflowProject = projectKind{
		bundle:     appbundle.Workflow,
		noun:       "workflow",
		collection: "workflows",
		sdkPath:    "/api/workflows/sdk",
		sdkMarker:  "// autable workflow SDK.",
	}
)

const (
	// projectSDKDir holds the vendored SDK and the generated schema.ts.
	projectSDKDir = "src/autable"
	schemaFile    = "schema.ts"
)

// projectManifest is autable.app.json or autable.workflow.json.
type projectManifest = appbundle.Manifest

// validName applies the server's rule for app and workflow names, which
// appear in URLs.
func (kind projectKind) validName(name string) error {
	if err := appbundle.ValidName(name); err != nil {
		return fmt.Errorf("%s %w", kind.noun, err)
	}
	return nil
}

func (kind projectKind) apiPath(database, name string, rest ...string) string {
	return apiPath(append([]string{"databases", database, kind.collection, name}, rest...)...)
}

func (kind projectKind) readManifest(dir string) (projectManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, kind.bundle.ManifestName))
	if errors.Is(err, os.ErrNotExist) {
		return projectManifest{}, fmt.Errorf("%s has no %s; create a project with autablectl %s init", dir, kind.bundle.ManifestName, kind.noun)
	}
	if err != nil {
		return projectManifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest projectManifest
	if err := decoder.Decode(&manifest); err != nil {
		return projectManifest{}, fmt.Errorf("%s: %w", kind.bundle.ManifestName, err)
	}
	if manifest.Database == "" {
		return projectManifest{}, fmt.Errorf("%s: database is required", kind.bundle.ManifestName)
	}
	if err := kind.validName(manifest.Name); err != nil {
		return projectManifest{}, fmt.Errorf("%s: %w", kind.bundle.ManifestName, err)
	}
	return manifest, nil
}

func (kind projectKind) writeManifest(dir string, manifest projectManifest) error {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, kind.bundle.ManifestName), out.Bytes(), 0o644)
}

// ensureEmptyDir accepts a missing or empty directory.
func ensureEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty; choose a new directory", dir)
	}
	return nil
}

func optionalDir(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return "."
}

type sdkResponse struct {
	Version  string            `json:"version"`
	MinMajor int               `json:"min_major"`
	Files    map[string]string `json:"files"`
}

// vendoredSDK is what `init` and `sdk` write into src/autable: the
// server's SDK files and the schema types of the project's database.
type vendoredSDK struct {
	kind    projectKind
	version string
	files   map[string]string
	schema  string
}

func (kind projectKind) fetchSDK(c *client, database string) (vendoredSDK, error) {
	var sdk sdkResponse
	if err := c.call(http.MethodGet, kind.sdkPath, nil, &sdk); err != nil {
		return vendoredSDK{}, fmt.Errorf("fetch SDK: %w", err)
	}
	if sdk.Version == "" || len(sdk.Files) == 0 {
		return vendoredSDK{}, errors.New("the server returned an empty SDK")
	}
	for name := range sdk.Files {
		if name == schemaFile || name != filepath.Base(name) || strings.ContainsAny(name, `/\:`) {
			return vendoredSDK{}, fmt.Errorf("the server's SDK has an unexpected file %q", name)
		}
	}
	var catalog schemaCatalog
	if err := c.call(http.MethodGet, "/api/metadata", nil, &catalog); err != nil {
		return vendoredSDK{}, fmt.Errorf("fetch metadata: %w", err)
	}
	schema, err := catalog.schema(database, kind.noun)
	if err != nil {
		return vendoredSDK{}, err
	}
	return vendoredSDK{kind: kind, version: sdk.Version, files: sdk.Files, schema: schema}, nil
}

// write replaces the vendored files in dir and returns their paths. Files
// of an older SDK that the server no longer ships are removed.
func (sdk vendoredSDK) write(dir string) ([]string, error) {
	target := filepath.Join(dir, filepath.FromSlash(projectSDKDir))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if _, current := sdk.files[entry.Name()]; current || entry.IsDir() || entry.Name() == schemaFile {
			continue
		}
		existing := filepath.Join(target, entry.Name())
		data, err := os.ReadFile(existing)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(string(data), sdk.kind.sdkMarker) {
			if err := os.Remove(existing); err != nil {
				return nil, err
			}
		}
	}
	written := []string{}
	for name, content := range sdk.files {
		if err := os.WriteFile(filepath.Join(target, name), []byte(content), 0o644); err != nil {
			return nil, err
		}
		written = append(written, projectSDKDir+"/"+name)
	}
	if err := os.WriteFile(filepath.Join(target, schemaFile), []byte(sdk.schema), 0o644); err != nil {
		return nil, err
	}
	written = append(written, projectSDKDir+"/"+schemaFile)
	sort.Strings(written)
	return written, nil
}

// runInit scaffolds a project: the template, the vendored SDK, the schema
// types, and the manifest. It prints the manifest and the next steps.
func (kind projectKind) runInit(a *app, args []string, template func(dir, name, title string) error, next func(dir string, manifest projectManifest) []string) error {
	fs := a.newFlags(kind.noun + " init")
	database := fs.String("database", "", "database the "+kind.noun+" belongs to")
	name := fs.String("name", "", kind.noun+" name: lowercase letters, digits, and hyphens; part of its URLs")
	title := fs.String("title", "", "display name (default: the name)")
	values, err := a.parse(fs, args, "DIR")
	if err != nil {
		return err
	}
	if *database == "" || *name == "" {
		return usagef("--database and --name are required")
	}
	if err := kind.validName(*name); err != nil {
		return usagef("%v", err)
	}
	if *title == "" {
		*title = *name
	}
	dir := values[0]
	if err := ensureEmptyDir(dir); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	sdk, err := kind.fetchSDK(c, *database)
	if err != nil {
		return err
	}
	if err := template(dir, *name, *title); err != nil {
		return err
	}
	sdkFiles, err := sdk.write(dir)
	if err != nil {
		return err
	}
	manifest := projectManifest{Database: *database, Name: *name, Title: *title, SDK: sdk.version}
	if err := kind.writeManifest(dir, manifest); err != nil {
		return err
	}
	return a.printJSON(map[string]any{
		"dir":      dir,
		"manifest": manifest,
		"sdk":      sdkFiles,
		"next":     next(dir, manifest),
	})
}

// runSDK refreshes the vendored SDK and schema types of the project in DIR.
func (kind projectKind) runSDK(a *app, args []string) error {
	values, err := a.parse(a.newFlags(kind.noun+" sdk"), args, "DIR?")
	if err != nil {
		return err
	}
	dir := optionalDir(values)
	manifest, err := kind.readManifest(dir)
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	sdk, err := kind.fetchSDK(c, manifest.Database)
	if err != nil {
		return err
	}
	files, err := sdk.write(dir)
	if err != nil {
		return err
	}
	previous := manifest.SDK
	manifest.SDK = sdk.version
	if err := kind.writeManifest(dir, manifest); err != nil {
		return err
	}
	return a.printJSON(map[string]any{"sdk": sdk.version, "previous_sdk": previous, "files": files})
}

// npm runs npm in dir with extra environment variables. Its output goes to
// out so stdout stays JSON for commands that print a result.
func (a *app) npm(dir string, env []string, out io.Writer, args ...string) error {
	if a.runNPM != nil {
		return a.runNPM(dir, env, out, args...)
	}
	cmd := exec.Command("npm", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = a.stdin
	cmd.Stdout = out
	cmd.Stderr = a.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// pushed is the outcome of uploading a version.
type pushed struct {
	manifest projectManifest
	archive  []byte
	counts   packCounts
	response []byte
}

// runPush builds (unless --no-build), packs, and uploads the project in DIR
// as a new version.
func (kind projectKind) runPush(a *app, args []string) (pushed, *client, error) {
	fs := a.newFlags(kind.noun + " push")
	var message string
	fs.StringVar(&message, "m", "", "what changed in this version")
	fs.StringVar(&message, "message", "", "same as -m")
	publish := fs.Bool("publish", false, "publish the new version right away")
	noBuild := fs.Bool("no-build", false, "upload the existing dist/ without running npm run build")
	values, err := a.parse(fs, args, "DIR?")
	if err != nil {
		return pushed{}, nil, err
	}
	dir := optionalDir(values)
	manifest, err := kind.readManifest(dir)
	if err != nil {
		return pushed{}, nil, err
	}
	c, err := a.client()
	if err != nil {
		return pushed{}, nil, err
	}
	if !*noBuild {
		if err := a.npm(dir, nil, a.stderr, "run", "build"); err != nil {
			return pushed{}, nil, err
		}
	}
	archive, counts, err := kind.pack(dir)
	if err != nil {
		return pushed{}, nil, err
	}
	query := url.Values{}
	if message != "" {
		query.Set("message", message)
	}
	if *publish {
		query.Set("publish", "1")
	}
	target := c.server + kind.apiPath(manifest.Database, manifest.Name, "versions")
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(archive))
	if err != nil {
		return pushed{}, nil, err
	}
	request.Header.Set("Content-Type", "application/gzip")
	data, err := c.send(request)
	if err != nil {
		return pushed{}, nil, err
	}
	return pushed{manifest: manifest, archive: archive, counts: counts, response: data}, c, nil
}

type packCounts struct {
	dist   int
	source int
}

// pack builds the upload archive: the manifest, dist/ as built, and the
// project source under source/.
func (kind projectKind) pack(dir string) ([]byte, packCounts, error) {
	var counts packCounts
	manifest, err := os.ReadFile(filepath.Join(dir, kind.bundle.ManifestName))
	if err != nil {
		return nil, counts, err
	}
	distDir := filepath.Join(dir, "dist")
	if _, err := os.Stat(filepath.Join(distDir, filepath.FromSlash(kind.bundle.RequiredDist))); err != nil {
		return nil, counts, fmt.Errorf("%s has no dist/%s; build the %s first (npm run build)", dir, kind.bundle.RequiredDist, kind.noun)
	}
	distFiles, err := walkFiles(distDir, nil)
	if err != nil {
		return nil, counts, err
	}
	sourceFiles, err := projectSourceFiles(dir)
	if err != nil {
		return nil, counts, err
	}
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	add := func(name string, body []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}
	if err := add(kind.bundle.ManifestName, manifest); err != nil {
		return nil, counts, err
	}
	for _, rel := range distFiles {
		body, err := os.ReadFile(filepath.Join(distDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, counts, err
		}
		if err := add("dist/"+rel, body); err != nil {
			return nil, counts, err
		}
		counts.dist++
	}
	for _, rel := range sourceFiles {
		body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, counts, err
		}
		if err := add("source/"+rel, body); err != nil {
			return nil, counts, err
		}
		counts.source++
	}
	if err := tw.Close(); err != nil {
		return nil, counts, err
	}
	if err := gz.Close(); err != nil {
		return nil, counts, err
	}
	if out.Len() > appbundle.MaxCompressedBytes {
		return nil, counts, fmt.Errorf("the bundle is %d bytes; the server accepts at most %d", out.Len(), appbundle.MaxCompressedBytes)
	}
	return out.Bytes(), counts, nil
}

// sourceSkipDirs are left out of the source when the project is not a git
// repository: dependencies, build output, and local state.
var sourceSkipDirs = map[string]bool{"node_modules": true, "dist": true, ".git": true, ".autable": true}

// projectSourceFiles lists the project's source files as slash-separated
// paths relative to dir: what git would commit when dir is inside a git
// work tree, otherwise every file outside sourceSkipDirs.
func projectSourceFiles(dir string) ([]string, error) {
	if listed, ok, err := gitSourceFiles(dir); err != nil || ok {
		return listed, err
	}
	return walkFiles(dir, sourceSkipDirs)
}

func gitSourceFiles(dir string) ([]string, bool, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, false, nil
	}
	inside, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(inside)) != "true" {
		return nil, false, nil
	}
	output, err := exec.Command("git", "-C", dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, false, fmt.Errorf("git ls-files: %w", err)
	}
	seen := map[string]bool{}
	files := []string{}
	for _, rel := range strings.Split(string(output), "\x00") {
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		// Deleted but still staged files, and submodules, are not regular
		// files on disk.
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		files = append(files, rel)
	}
	sort.Strings(files)
	return files, true, nil
}

// walkFiles lists regular files below root as sorted slash-separated
// relative paths, skipping directories named in skip.
func walkFiles(root string, skip map[string]bool) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if file != root && skip[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

func parseVersion(value string) (int64, error) {
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version <= 0 {
		return 0, usagef("VERSION must be a positive number, got %q", value)
	}
	return version, nil
}

// rollbackTarget picks the newest version below the published one.
func rollbackTarget(published int64, versions []int64) (int64, error) {
	if published == 0 {
		return 0, errors.New("nothing is published; publish a version instead")
	}
	target := int64(0)
	for _, version := range versions {
		if version < published && version > target {
			target = version
		}
	}
	if target == 0 {
		return 0, fmt.Errorf("version %d is the oldest; there is nothing to roll back to", published)
	}
	return target, nil
}

// defaultVersion is the version `pull` downloads without --version: the
// published one, else the newest.
func defaultVersion(published int64, versions []int64) int64 {
	if published > 0 {
		return published
	}
	newest := int64(0)
	for _, version := range versions {
		newest = max(newest, version)
	}
	return newest
}

// pullSource downloads the source of one version into dir.
func (kind projectKind) pullSource(c *client, database, name string, version int64, dir string) ([]string, error) {
	request, err := http.NewRequest(http.MethodGet, c.server+kind.apiPath(database, name, "versions", strconv.FormatInt(version, 10), "source"), nil)
	if err != nil {
		return nil, err
	}
	data, err := c.send(request)
	if err != nil {
		return nil, err
	}
	return unpackSource(data, dir)
}

// unpackSource extracts a source archive into dir, refusing entries that
// would land outside it.
func unpackSource(archive []byte, dir string) ([]string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("source is not gzip: %w", err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	files := []string{}
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("source is not a valid tar archive: %w", err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("source entry %q is not a regular file", header.Name)
		}
		name := header.Name
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || path.Clean(name) != name || !filepath.IsLocal(filepath.FromSlash(name)) {
			return nil, fmt.Errorf("source entry %q has an unsafe path", name)
		}
		total += header.Size
		if total > appbundle.MaxUncompressedBytes {
			return nil, fmt.Errorf("source expands beyond %d bytes", appbundle.MaxUncompressedBytes)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(reader, header.Size))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			return nil, err
		}
		files = append(files, name)
	}
	if len(files) == 0 {
		return nil, errors.New("the source archive is empty")
	}
	return files, nil
}
