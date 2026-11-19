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

// Hosted apps: ordinary frontend projects that autablectl scaffolds, builds,
// packs, and uploads. See docs/design/apps.md.

const (
	// appSDKDir holds the vendored SDK and the generated schema.ts.
	appSDKDir     = "src/autable"
	appSchemaFile = "schema.ts"
	// sdkFileMarker starts every SDK file the server ships, so `app sdk`
	// can recognize vendored files it may replace or remove.
	sdkFileMarker = "// autable app SDK."
)

type appManifest = appbundle.Manifest

type appSummary struct {
	ID               int64  `json:"id"`
	DatabaseName     string `json:"database_name"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	PublishedVersion int64  `json:"published_version"`
	URL              string `json:"url"`
}

type appVersionSummary struct {
	Version    int64  `json:"version"`
	SDK        string `json:"sdk"`
	Message    string `json:"message"`
	Supported  bool   `json:"supported"`
	SDKError   string `json:"sdk_error,omitempty"`
	PreviewURL string `json:"preview_url"`
}

type appDetail struct {
	App      appSummary          `json:"app"`
	Versions []appVersionSummary `json:"versions"`
}

type sdkResponse struct {
	Version  string            `json:"version"`
	MinMajor int               `json:"min_major"`
	Files    map[string]string `json:"files"`
}

func appAPIPath(database, name string, rest ...string) string {
	return apiPath(append([]string{"databases", database, "apps", name}, rest...)...)
}

func readAppManifest(dir string) (appManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, appbundle.ManifestName))
	if errors.Is(err, os.ErrNotExist) {
		return appManifest{}, fmt.Errorf("%s has no %s; create a project with autablectl app init", dir, appbundle.ManifestName)
	}
	if err != nil {
		return appManifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest appManifest
	if err := decoder.Decode(&manifest); err != nil {
		return appManifest{}, fmt.Errorf("%s: %w", appbundle.ManifestName, err)
	}
	if manifest.Database == "" {
		return appManifest{}, fmt.Errorf("%s: database is required", appbundle.ManifestName)
	}
	if err := appbundle.ValidName(manifest.Name); err != nil {
		return appManifest{}, fmt.Errorf("%s: %w", appbundle.ManifestName, err)
	}
	return manifest, nil
}

func writeAppManifest(dir string, manifest appManifest) error {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, appbundle.ManifestName), out.Bytes(), 0o644)
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

// vendoredSDK is what `app init` and `app sdk` write into src/autable: the
// server's SDK files and the schema types of the app's database.
type vendoredSDK struct {
	version string
	files   map[string]string
	schema  string
}

func fetchSDK(c *client, database string) (vendoredSDK, error) {
	var sdk sdkResponse
	if err := c.call(http.MethodGet, "/api/apps/sdk", nil, &sdk); err != nil {
		return vendoredSDK{}, fmt.Errorf("fetch SDK: %w", err)
	}
	if sdk.Version == "" || len(sdk.Files) == 0 {
		return vendoredSDK{}, errors.New("the server returned an empty SDK")
	}
	for name := range sdk.Files {
		if name == appSchemaFile || name != filepath.Base(name) || strings.ContainsAny(name, `/\:`) {
			return vendoredSDK{}, fmt.Errorf("the server's SDK has an unexpected file %q", name)
		}
	}
	var catalog schemaCatalog
	if err := c.call(http.MethodGet, "/api/metadata", nil, &catalog); err != nil {
		return vendoredSDK{}, fmt.Errorf("fetch metadata: %w", err)
	}
	schema, err := catalog.schema(database)
	if err != nil {
		return vendoredSDK{}, err
	}
	return vendoredSDK{version: sdk.Version, files: sdk.Files, schema: schema}, nil
}

// write replaces the vendored files in dir and returns their paths. Files
// of an older SDK that the server no longer ships are removed.
func (sdk vendoredSDK) write(dir string) ([]string, error) {
	target := filepath.Join(dir, filepath.FromSlash(appSDKDir))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if _, current := sdk.files[entry.Name()]; current || entry.IsDir() || entry.Name() == appSchemaFile {
			continue
		}
		existing := filepath.Join(target, entry.Name())
		data, err := os.ReadFile(existing)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(string(data), sdkFileMarker) {
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
		written = append(written, appSDKDir+"/"+name)
	}
	if err := os.WriteFile(filepath.Join(target, appSchemaFile), []byte(sdk.schema), 0o644); err != nil {
		return nil, err
	}
	written = append(written, appSDKDir+"/"+appSchemaFile)
	sort.Strings(written)
	return written, nil
}

func runAppInit(a *app, args []string) error {
	fs := a.newFlags("app init")
	database := fs.String("database", "", "database the app belongs to")
	name := fs.String("name", "", "app name: lowercase letters, digits, and hyphens; part of the app's URL")
	title := fs.String("title", "", "display name (default: the name)")
	values, err := a.parse(fs, args, "DIR")
	if err != nil {
		return err
	}
	if *database == "" || *name == "" {
		return usagef("--database and --name are required")
	}
	if err := appbundle.ValidName(*name); err != nil {
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
	sdk, err := fetchSDK(c, *database)
	if err != nil {
		return err
	}
	if err := writeAppTemplate(dir, *name, *title); err != nil {
		return err
	}
	sdkFiles, err := sdk.write(dir)
	if err != nil {
		return err
	}
	manifest := appManifest{Database: *database, Name: *name, Title: *title, SDK: sdk.version}
	if err := writeAppManifest(dir, manifest); err != nil {
		return err
	}
	return a.printJSON(map[string]any{
		"dir":      dir,
		"manifest": manifest,
		"sdk":      sdkFiles,
		"next":     []string{"cd " + dir, "npm install", "autablectl app dev", "autablectl app push -m \"first version\""},
	})
}

func runAppSDK(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app sdk"), args, "DIR?")
	if err != nil {
		return err
	}
	dir := optionalDir(values)
	manifest, err := readAppManifest(dir)
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	sdk, err := fetchSDK(c, manifest.Database)
	if err != nil {
		return err
	}
	files, err := sdk.write(dir)
	if err != nil {
		return err
	}
	previous := manifest.SDK
	manifest.SDK = sdk.version
	if err := writeAppManifest(dir, manifest); err != nil {
		return err
	}
	return a.printJSON(map[string]any{"sdk": sdk.version, "previous_sdk": previous, "files": files})
}

func optionalDir(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return "."
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

func runAppDev(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app dev"), args, "DIR?")
	if err != nil {
		return err
	}
	dir := optionalDir(values)
	if _, err := readAppManifest(dir); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s has no node_modules; run npm install first", dir)
	}
	fmt.Fprintf(a.stderr, "proxying /api to %s with your autablectl login\n", c.server)
	return a.npm(dir, []string{"AUTABLE_SERVER=" + c.server, "AUTABLE_TOKEN=" + c.token}, a.stdout, "run", "dev")
}

func runAppPush(a *app, args []string) error {
	fs := a.newFlags("app push")
	var message string
	fs.StringVar(&message, "m", "", "what changed in this version")
	fs.StringVar(&message, "message", "", "same as -m")
	publish := fs.Bool("publish", false, "publish the new version right away")
	noBuild := fs.Bool("no-build", false, "upload the existing dist/ without running npm run build")
	values, err := a.parse(fs, args, "DIR?")
	if err != nil {
		return err
	}
	dir := optionalDir(values)
	manifest, err := readAppManifest(dir)
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if !*noBuild {
		if err := a.npm(dir, nil, a.stderr, "run", "build"); err != nil {
			return err
		}
	}
	archive, packed, err := packApp(dir)
	if err != nil {
		return err
	}
	query := url.Values{}
	if message != "" {
		query.Set("message", message)
	}
	if *publish {
		query.Set("publish", "1")
	}
	target := c.server + appAPIPath(manifest.Database, manifest.Name, "versions")
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(archive))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/gzip")
	data, err := c.send(request)
	if err != nil {
		return err
	}
	var uploaded struct {
		App     appSummary        `json:"app"`
		Version appVersionSummary `json:"version"`
	}
	if err := json.Unmarshal(data, &uploaded); err != nil {
		return err
	}
	result := map[string]any{
		"database":          uploaded.App.DatabaseName,
		"name":              uploaded.App.Name,
		"version":           uploaded.Version.Version,
		"sdk":               uploaded.Version.SDK,
		"published_version": uploaded.App.PublishedVersion,
		"preview_url":       c.server + uploaded.Version.PreviewURL,
		"url":               c.server + uploaded.App.URL,
		"bytes":             len(archive),
		"dist_files":        packed.dist,
		"source_files":      packed.source,
	}
	return a.printJSON(result)
}

type packCounts struct {
	dist   int
	source int
}

// packApp builds the upload archive: the manifest, dist/ as built, and the
// project source under source/.
func packApp(dir string) ([]byte, packCounts, error) {
	var counts packCounts
	manifest, err := os.ReadFile(filepath.Join(dir, appbundle.ManifestName))
	if err != nil {
		return nil, counts, err
	}
	distDir := filepath.Join(dir, "dist")
	if _, err := os.Stat(filepath.Join(distDir, "index.html")); err != nil {
		return nil, counts, fmt.Errorf("%s has no dist/index.html; build the app first (npm run build)", dir)
	}
	distFiles, err := walkFiles(distDir, nil)
	if err != nil {
		return nil, counts, err
	}
	sourceFiles, err := appSourceFiles(dir)
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
	if err := add(appbundle.ManifestName, manifest); err != nil {
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

// appSourceFiles lists the project's source files as slash-separated paths
// relative to dir: what git would commit when dir is inside a git work
// tree, otherwise every file outside sourceSkipDirs.
func appSourceFiles(dir string) ([]string, error) {
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

func runAppList(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app list"), args, "DATABASE")
	if err != nil {
		return err
	}
	return a.get(apiPath("databases", values[0], "apps"))
}

func runAppVersions(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app versions"), args, "DATABASE", "NAME")
	if err != nil {
		return err
	}
	return a.get(appAPIPath(values[0], values[1]))
}

func parseAppVersion(value string) (int64, error) {
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version <= 0 {
		return 0, usagef("VERSION must be a positive number, got %q", value)
	}
	return version, nil
}

func runAppPublish(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app publish"), args, "DATABASE", "NAME", "VERSION")
	if err != nil {
		return err
	}
	version, err := parseAppVersion(values[2])
	if err != nil {
		return err
	}
	return a.send(http.MethodPost, appAPIPath(values[0], values[1], "publish"), map[string]int64{"version": version})
}

// rollbackTarget picks the newest version below the published one.
func rollbackTarget(published int64, versions []appVersionSummary) (int64, error) {
	if published == 0 {
		return 0, errors.New("nothing is published; publish a version instead")
	}
	target := int64(0)
	for _, version := range versions {
		if version.Version < published && version.Version > target {
			target = version.Version
		}
	}
	if target == 0 {
		return 0, fmt.Errorf("version %d is the oldest; there is nothing to roll back to", published)
	}
	return target, nil
}

func runAppRollback(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app rollback"), args, "DATABASE", "NAME")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	var detail appDetail
	if err := c.call(http.MethodGet, appAPIPath(values[0], values[1]), nil, &detail); err != nil {
		return err
	}
	target, err := rollbackTarget(detail.App.PublishedVersion, detail.Versions)
	if err != nil {
		return err
	}
	var published appSummary
	if err := c.call(http.MethodPost, appAPIPath(values[0], values[1], "publish"), map[string]int64{"version": target}, &published); err != nil {
		return err
	}
	return a.printJSON(map[string]any{
		"database":          published.DatabaseName,
		"name":              published.Name,
		"published_version": published.PublishedVersion,
		"previous_version":  detail.App.PublishedVersion,
		"url":               c.server + published.URL,
	})
}

func runAppPull(a *app, args []string) error {
	fs := a.newFlags("app pull")
	version := fs.Int64("version", 0, "version to download (default: the published one, else the latest)")
	values, err := a.parse(fs, args, "DATABASE", "NAME", "DIR")
	if err != nil {
		return err
	}
	dir := values[2]
	if err := ensureEmptyDir(dir); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if *version <= 0 {
		var detail appDetail
		if err := c.call(http.MethodGet, appAPIPath(values[0], values[1]), nil, &detail); err != nil {
			return err
		}
		*version = detail.App.PublishedVersion
		if *version == 0 {
			for _, candidate := range detail.Versions {
				*version = max(*version, candidate.Version)
			}
		}
		if *version == 0 {
			return fmt.Errorf("app %s has no versions", values[1])
		}
	}
	request, err := http.NewRequest(http.MethodGet, c.server+appAPIPath(values[0], values[1], "versions", strconv.FormatInt(*version, 10), "source"), nil)
	if err != nil {
		return err
	}
	data, err := c.send(request)
	if err != nil {
		return err
	}
	files, err := unpackSource(data, dir)
	if err != nil {
		return err
	}
	return a.printJSON(map[string]any{"dir": dir, "version": *version, "files": files})
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
