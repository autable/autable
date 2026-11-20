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
	"slices"
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
	// instance kinds (libraries) belong to no database: their manifest has
	// none and their API lives at /api/<collection>/<name>.
	instance bool
	// importsLibraries makes `init` and `sdk` vendor the type declarations
	// of the published libraries.
	importsLibraries bool
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
		bundle:           appbundle.Workflow,
		noun:             "workflow",
		collection:       "workflows",
		sdkPath:          "/api/workflows/sdk",
		sdkMarker:        "// autable workflow SDK.",
		importsLibraries: true,
	}
	// Libraries vendor the workflow SDK: they run inside workflow runs.
	libProject = projectKind{
		bundle:           appbundle.Library,
		noun:             "lib",
		collection:       "libs",
		sdkPath:          "/api/workflows/sdk",
		sdkMarker:        "// autable workflow SDK.",
		instance:         true,
		importsLibraries: true,
	}
)

const (
	// projectSDKDir holds the vendored SDK and the generated schema.ts.
	projectSDKDir = "src/autable"
	schemaFile    = "schema.ts"
	// libraryTypesDir, below projectSDKDir, holds the vendored type
	// declarations of the published libraries, one <name>.d.ts each.
	libraryTypesDir    = "libs"
	libraryTypesMarker = "// autable library types."
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
	if kind.instance {
		return apiPath(append([]string{kind.collection, name}, rest...)...)
	}
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
	switch {
	case kind.instance && manifest.Database != "":
		return projectManifest{}, fmt.Errorf("%s: a %s belongs to no database; remove database", kind.bundle.ManifestName, kind.noun)
	case !kind.instance && manifest.Database == "":
		return projectManifest{}, fmt.Errorf("%s: database is required", kind.bundle.ManifestName)
	}
	if err := kind.validName(manifest.Name); err != nil {
		return projectManifest{}, fmt.Errorf("%s: %w", kind.bundle.ManifestName, err)
	}
	if manifest.Root != "" {
		return projectManifest{}, fmt.Errorf("%s: root is recorded by autablectl push in the uploaded copy only; remove it", kind.bundle.ManifestName)
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
// server's SDK files, the schema types of the project's database, and for
// workflows and libraries the types of the published libraries.
type vendoredSDK struct {
	kind    projectKind
	version string
	files   map[string]string
	schema  string
	// libraries are the published libraries' type declarations; nil when
	// they could not be listed (see libraryNote).
	libraries   []libraryTypes
	libraryNote string
}

// libraryTypes is a published library's dist/index.d.ts.
type libraryTypes struct {
	Name    string `json:"name"`
	Version int64  `json:"version"`
	Types   string `json:"types"`
}

// fetchSDK downloads what `init` and `sdk` vendor. self is the project's
// own name, which a library does not vendor types for.
func (kind projectKind) fetchSDK(c *client, database, self string) (vendoredSDK, error) {
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
	var schema string
	if kind.instance {
		schema = instanceSchema(kind.noun)
	} else {
		var catalog schemaCatalog
		if err := c.call(http.MethodGet, "/api/metadata", nil, &catalog); err != nil {
			return vendoredSDK{}, fmt.Errorf("fetch metadata: %w", err)
		}
		var err error
		if schema, err = catalog.schema(database, kind.noun); err != nil {
			return vendoredSDK{}, err
		}
	}
	vendored := vendoredSDK{kind: kind, version: sdk.Version, files: sdk.Files, schema: schema}
	if kind.importsLibraries {
		vendored.libraries, vendored.libraryNote = fetchLibraryTypes(c, self)
	}
	return vendored, nil
}

// instanceSchema is schema.ts of a library: libraries belong to no
// database, so they know no tables; they take table data from the
// workflow that imports them.
func instanceSchema(noun string) string {
	schema := generateSchema(noun, "", nil)
	if _, rest, ok := strings.Cut(schema, "\n\n"); ok {
		return "// Generated by autablectl. A library belongs to no database, so it knows no\n// tables: take rows and table clients from the importing workflow.\n\n" + rest
	}
	return schema
}

// fetchLibraryTypes lists the published libraries and downloads their
// type declarations. A server that cannot list them (older, or the user
// is no admin) yields no libraries and a note instead of an error.
func fetchLibraryTypes(c *client, self string) ([]libraryTypes, string) {
	var listed []struct {
		Name             string `json:"name"`
		PublishedVersion int64  `json:"published_version"`
	}
	if err := c.call(http.MethodGet, "/api/libs", nil, &listed); err != nil {
		return nil, fmt.Sprintf("library types were not updated: %v", err)
	}
	types := []libraryTypes{}
	for _, library := range listed {
		if library.PublishedVersion == 0 || library.Name == self || appbundle.ValidName(library.Name) != nil {
			continue
		}
		var fetched libraryTypes
		if err := c.call(http.MethodGet, apiPath("libs", library.Name, "types"), nil, &fetched); err != nil {
			return nil, fmt.Sprintf("library types were not updated: %s: %v", library.Name, err)
		}
		fetched.Name = library.Name
		types = append(types, fetched)
	}
	return types, ""
}

// libraryDeclaration is the vendored <name>.d.ts of a library: its
// declarations as `declare module "@lib/<name>"`. Library templates emit
// that form already; plain module declarations are wrapped.
func libraryDeclaration(library libraryTypes) string {
	module := "@lib/" + library.Name
	body := strings.ReplaceAll(library.Types, "\r\n", "\n")
	if !strings.Contains(body, "declare module \""+module+"\"") {
		lines := strings.Split(strings.TrimSpace(body), "\n")
		for index, line := range lines {
			if rest, ok := strings.CutPrefix(line, "export declare "); ok {
				lines[index] = "export " + rest
			} else if rest, ok := strings.CutPrefix(line, "declare "); ok {
				lines[index] = rest
			}
		}
		body = "declare module \"" + module + "\" {\n" + strings.Join(lines, "\n") + "\n}\n"
	}
	return fmt.Sprintf("%s Vendored by autablectl from %s version %d; do not edit:\n// run `autablectl workflow sdk` (or `lib sdk`) again after it is published anew.\n\n%s", libraryTypesMarker, module, library.Version, strings.TrimLeft(body, "\n"))
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
	if sdk.libraries != nil {
		libraries, err := sdk.writeLibraries(filepath.Join(target, libraryTypesDir))
		if err != nil {
			return nil, err
		}
		written = append(written, libraries...)
	}
	sort.Strings(written)
	return written, nil
}

// writeLibraries replaces the vendored library declarations; files of
// libraries no longer published are removed.
func (sdk vendoredSDK) writeLibraries(dir string) ([]string, error) {
	current := map[string]bool{}
	for _, library := range sdk.libraries {
		current[library.Name+".d.ts"] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if current[entry.Name()] || entry.IsDir() {
			continue
		}
		existing := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(existing)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(string(data), libraryTypesMarker) {
			if err := os.Remove(existing); err != nil {
				return nil, err
			}
		}
	}
	if len(sdk.libraries) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	written := []string{}
	for _, library := range sdk.libraries {
		if err := os.WriteFile(filepath.Join(dir, library.Name+".d.ts"), []byte(libraryDeclaration(library)), 0o644); err != nil {
			return nil, err
		}
		written = append(written, projectSDKDir+"/"+libraryTypesDir+"/"+library.Name+".d.ts")
	}
	return written, nil
}

// librarySummary is how init and sdk report the vendored libraries.
func (sdk vendoredSDK) librarySummary() []map[string]any {
	summary := []map[string]any{}
	for _, library := range sdk.libraries {
		summary = append(summary, map[string]any{"name": library.Name, "version": library.Version})
	}
	return summary
}

// runInit scaffolds a project: the template, the vendored SDK, the schema
// types, and the manifest. It prints the manifest and the next steps.
func (kind projectKind) runInit(a *app, args []string, template func(dir, name, title string) error, next func(dir string, manifest projectManifest) []string) error {
	fs := a.newFlags(kind.noun + " init")
	database := new(string)
	if !kind.instance {
		database = fs.String("database", "", "database the "+kind.noun+" belongs to")
	}
	name := fs.String("name", "", kind.noun+" name: lowercase letters, digits, and hyphens; part of its URLs")
	title := fs.String("title", "", "display name (default: the name)")
	values, err := a.parse(fs, args, "DIR")
	if err != nil {
		return err
	}
	switch {
	case kind.instance && *name == "":
		return usagef("--name is required")
	case !kind.instance && (*database == "" || *name == ""):
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
	sdk, err := kind.fetchSDK(c, *database, *name)
	if err != nil {
		return err
	}
	a.note(sdk.libraryNote)
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
	output := map[string]any{
		"dir":      dir,
		"manifest": manifest,
		"sdk":      sdkFiles,
		"next":     next(dir, manifest),
	}
	if kind.importsLibraries {
		output["libraries"] = sdk.librarySummary()
	}
	return a.printJSON(output)
}

// note prints a non-fatal remark on stderr.
func (a *app) note(text string) {
	if text != "" {
		fmt.Fprintln(a.stderr, "note:", text)
	}
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
	sdk, err := kind.fetchSDK(c, manifest.Database, manifest.Name)
	if err != nil {
		return err
	}
	a.note(sdk.libraryNote)
	files, err := sdk.write(dir)
	if err != nil {
		return err
	}
	previous := manifest.SDK
	manifest.SDK = sdk.version
	if err := kind.writeManifest(dir, manifest); err != nil {
		return err
	}
	output := map[string]any{"sdk": sdk.version, "previous_sdk": previous, "files": files}
	if kind.importsLibraries {
		output["libraries"] = sdk.librarySummary()
	}
	return a.printJSON(output)
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
	if !counts.inputsRecorded {
		fmt.Fprintf(a.stderr, "note: the build wrote no %s, so only the project directory is uploaded as source; files it imports from outside (a shared lib/) are left out. Current %s templates record it; see docs/cli.md.\n", buildInputsFile, kind.noun)
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
	// shared counts the source files from outside the project directory.
	shared int
	// root is the project's path within the uploaded source tree; empty
	// when source/ is the project directory.
	root string
	// inputsRecorded is false when the build left no build-inputs file, so
	// files it used outside the project could not be found.
	inputsRecorded bool
}

// pack builds the upload archive: the manifest, dist/ as built, and the
// source under source/ (see sourceTree).
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
	tree, err := kind.sourceTree(dir)
	if err != nil {
		return nil, counts, err
	}
	counts.root = tree.root
	counts.inputsRecorded = tree.inputsRecorded
	if tree.root != "" {
		// The uploaded manifest says where the project is in the tree.
		var parsed projectManifest
		if err := json.Unmarshal(manifest, &parsed); err != nil {
			return nil, counts, fmt.Errorf("%s: %w", kind.bundle.ManifestName, err)
		}
		parsed.Root = tree.root
		if manifest, err = json.MarshalIndent(parsed, "", "  "); err != nil {
			return nil, counts, err
		}
		manifest = append(manifest, '\n')
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
	for _, file := range tree.files {
		body, err := os.ReadFile(file.path)
		if err != nil {
			return nil, counts, err
		}
		if err := add("source/"+file.name, body); err != nil {
			return nil, counts, err
		}
		counts.source++
		if file.shared {
			counts.shared++
		}
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

// buildInputsFile is written by the templates' builds (build.mjs for
// workflows, a Vite plugin in vite.config.ts for apps): every file the
// bundle was built from, as paths relative to the project directory.
const buildInputsFile = ".autable/build-inputs.json"

type buildInputs struct {
	Inputs []string `json:"inputs"`
}

// sourceFile is one file of the uploaded source.
type sourceFile struct {
	// name is the slash-separated path below source/.
	name string
	// path is the file on disk.
	path string
	// shared marks a file from outside the project directory.
	shared bool
}

type sourceTreeResult struct {
	root           string
	files          []sourceFile
	inputsRecorded bool
}

// sourceTree decides what goes under source/. Normally that is the
// project directory (projectSourceFiles). When the build used files
// outside it, such as a shared lib/ of a development repository holding
// several projects, source/ is instead a slice of that repository: the
// project's files below its path in the repository (the root), plus
// exactly the outside files the build used, at their paths from the
// repository root, so the pulled slice builds as it did here. The
// repository is the git work tree containing the project; outside git,
// nothing outside the project directory can be uploaded.
func (kind projectKind) sourceTree(dir string) (sourceTreeResult, error) {
	var result sourceTreeResult
	projectFiles, err := projectSourceFiles(dir)
	if err != nil {
		return result, err
	}
	outside, recorded, err := outsideBuildInputs(dir)
	if err != nil {
		return result, err
	}
	result.inputsRecorded = recorded
	if len(outside) == 0 {
		for _, rel := range projectFiles {
			result.files = append(result.files, sourceFile{name: rel, path: filepath.Join(dir, filepath.FromSlash(rel))})
		}
		return result, nil
	}
	prefix, inGit, err := gitPrefix(dir)
	if err != nil {
		return result, err
	}
	if !inGit {
		where := dir
		if abs, err := filepath.Abs(dir); err == nil {
			where = abs
		}
		return result, fmt.Errorf("the build used files outside the %s directory %s (%s); autablectl uploads such shared files only from a git repository holding both, whose root becomes the root of the uploaded source: put the project and the shared files in one git repository (for a pulled slice, run git init at its top), or move the files into the project", kind.noun, where, strings.Join(outside, ", "))
	}
	root := strings.TrimSuffix(prefix, "/")
	if root == "" {
		// The project is the repository root, so nothing used can be outside it.
		return result, fmt.Errorf("the build used files outside the git repository at %s: %s", dir, strings.Join(outside, ", "))
	}
	result.root = root
	seen := map[string]bool{}
	for _, rel := range projectFiles {
		name := root + "/" + rel
		seen[name] = true
		result.files = append(result.files, sourceFile{name: name, path: filepath.Join(dir, filepath.FromSlash(rel))})
	}
	if manifestName := root + "/" + kind.bundle.ManifestName; !seen[manifestName] {
		// pull finds the project by its manifest, ignored by git or not.
		seen[manifestName] = true
		result.files = append(result.files, sourceFile{name: manifestName, path: filepath.Join(dir, kind.bundle.ManifestName)})
	}
	for _, rel := range outside {
		name := path.Clean(root + "/" + rel)
		if name == ".." || strings.HasPrefix(name, "../") {
			return result, fmt.Errorf("the build used %s, which is outside the git repository containing %s; move it into the repository", rel, dir)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		result.files = append(result.files, sourceFile{name: name, path: filepath.Join(dir, filepath.FromSlash(rel)), shared: true})
	}
	sort.Slice(result.files, func(i, j int) bool { return result.files[i].name < result.files[j].name })
	return result, nil
}

// outsideBuildInputs reads the build-inputs file and returns the inputs
// outside dir, as sorted slash-separated paths relative to dir ("../..."),
// leaving out dependencies (any path through node_modules) and generated
// or virtual modules that are not files. recorded is false when the build
// wrote no build-inputs file.
func outsideBuildInputs(dir string) ([]string, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(buildInputsFile)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var inputs buildInputs
	if err := json.Unmarshal(data, &inputs); err != nil {
		return nil, false, fmt.Errorf("%s: %w; rebuild the project", buildInputsFile, err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	outside := []string{}
	for _, input := range inputs.Inputs {
		rel := input
		if filepath.IsAbs(input) {
			if rel, err = filepath.Rel(absDir, input); err != nil {
				return nil, true, fmt.Errorf("the build used %s, which is outside the project directory and its repository", input)
			}
		}
		rel = path.Clean(filepath.ToSlash(rel))
		if rel != ".." && !strings.HasPrefix(rel, "../") {
			// Inside the project, which is uploaded as a whole anyway.
			continue
		}
		if slices.Contains(strings.Split(rel, "/"), "node_modules") || seen[rel] {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || !info.Mode().IsRegular() {
			// Not a file on disk: a virtual module, or the build is stale.
			continue
		}
		seen[rel] = true
		outside = append(outside, rel)
	}
	sort.Strings(outside)
	return outside, true, nil
}

// gitPrefix returns the path of dir within its git work tree, with a
// trailing slash ("" at the top), and whether dir is inside one.
func gitPrefix(dir string) (string, bool, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", false, nil
	}
	inside, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(inside)) != "true" {
		return "", false, nil
	}
	prefix, err := exec.Command("git", "-C", dir, "rev-parse", "--show-prefix").Output()
	if err != nil {
		return "", false, fmt.Errorf("git rev-parse --show-prefix: %w", err)
	}
	return strings.TrimRight(string(prefix), "\r\n"), true, nil
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

// pulledSource is what `pull` wrote.
type pulledSource struct {
	files []string
	// root is the project's path below the pull directory: empty when the
	// version was uploaded from its project directory alone, else the
	// project's path in the repository slice it was uploaded with.
	root string
}

// pullSource downloads the source of one version into dir.
func (kind projectKind) pullSource(c *client, database, name string, version int64, dir string) (pulledSource, error) {
	request, err := http.NewRequest(http.MethodGet, c.server+kind.apiPath(database, name, "versions", strconv.FormatInt(version, 10), "source"), nil)
	if err != nil {
		return pulledSource{}, err
	}
	data, err := c.send(request)
	if err != nil {
		return pulledSource{}, err
	}
	files, err := unpackSource(data, dir)
	if err != nil {
		return pulledSource{}, err
	}
	return kind.placeManifest(dir, files)
}

// placeManifest finishes a pull. The source archive carries the uploaded
// manifest at its top; when that names a root, the archive is a slice of
// a development repository, and the top copy (which only says where the
// project is) gives way to the project's own manifest at <root>/.
func (kind projectKind) placeManifest(dir string, files []string) (pulledSource, error) {
	top := filepath.Join(dir, kind.bundle.ManifestName)
	data, err := os.ReadFile(top)
	if errors.Is(err, os.ErrNotExist) {
		return pulledSource{files: files}, nil
	}
	if err != nil {
		return pulledSource{}, err
	}
	var manifest projectManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return pulledSource{}, fmt.Errorf("pulled %s: %w", kind.bundle.ManifestName, err)
	}
	root := manifest.Root
	if root == "" {
		return pulledSource{files: files}, nil
	}
	if err := appbundle.ValidRoot(root); err != nil {
		return pulledSource{}, fmt.Errorf("pulled %s: %w", kind.bundle.ManifestName, err)
	}
	if err := os.Remove(top); err != nil {
		return pulledSource{}, err
	}
	kept := make([]string, 0, len(files))
	for _, file := range files {
		if file != kind.bundle.ManifestName {
			kept = append(kept, file)
		}
	}
	own := filepath.Join(dir, filepath.FromSlash(root), kind.bundle.ManifestName)
	if _, err := os.Stat(own); errors.Is(err, os.ErrNotExist) {
		manifest.Root = ""
		if err := os.MkdirAll(filepath.Dir(own), 0o755); err != nil {
			return pulledSource{}, err
		}
		if err := kind.writeManifest(filepath.Dir(own), manifest); err != nil {
			return pulledSource{}, err
		}
		kept = append(kept, path.Join(root, kind.bundle.ManifestName))
	} else if err != nil {
		return pulledSource{}, err
	}
	return pulledSource{files: kept, root: root}, nil
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

// pulledOutput is what `pull` prints: project_dir is where to build.
func pulledOutput(dir string, version int64, pulled pulledSource) map[string]any {
	return map[string]any{
		"dir":         dir,
		"version":     version,
		"root":        pulled.root,
		"project_dir": filepath.Join(dir, filepath.FromSlash(pulled.root)),
		"files":       pulled.files,
	}
}
