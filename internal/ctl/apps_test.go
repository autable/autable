package ctl

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"autable/internal/appbundle"
	"autable/internal/appsdk"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// archiveEntries lists the regular files of a tar.gz with their contents.
func archiveEntries(t *testing.T, archive []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	entries := map[string]string{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = string(body)
	}
}

func sortedKeys(entries map[string]string) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// noGitAbove keeps git from treating dir as part of a repository that
// happens to contain the test's temp directory.
func noGitAbove(t *testing.T, dir string) {
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
}

func TestGenerateSchemaMapsFieldTypes(t *testing.T) {
	schema := generateSchema("app", "名称 db", []schemaTable{
		{Name: "联系人", DisplayName: "Contacts */ list", Fields: []schemaField{
			{Name: "名称", Type: "string"},
			{Name: "数量", Type: "int"},
			{Name: "price", Type: "float"},
			{Name: "状态", Type: "string", Options: []string{"开放", "关闭"}},
			{Name: "tags", Type: "string", Options: []string{"a", "b"}, Multiple: true},
			{Name: "owner", Type: "relation", RelationTable: "people"},
			{Name: "附件", Type: "file"},
			{Name: "合计", Type: "formula", ValueType: "float"},
			{Name: "old", Type: "string", Deleted: true},
			{Name: `say "hi"\`, Type: "string"},
		}},
		{Name: "orders", Fields: []schemaField{{Name: "备注", Type: "string"}}},
	})
	for _, want := range []string{
		`export const DATABASE = "名称 db";`,
		"run `autablectl app sdk` again",
		"  /** Contacts *\\/ list */\n  \"联系人\": {\n",
		`    "名称": string | null;`,
		`    "数量": number | null;`,
		`    "price": number | null;`,
		`    "状态": "开放" | "关闭" | "" | null;`,
		"    /** options, several joined by \",\" (see splitOptions): a, b */\n    \"tags\": string | null;",
		"    /** record id in table \"people\" */\n    \"owner\": number | null;",
		`    "附件": number | null;`,
		`    "合计": number | null;`,
		`    "say \"hi\"\\": string | null;`,
		"export interface ReadOnlyFields {\n  \"联系人\": \"合计\";\n  \"orders\": never;\n}",
		`export const TABLE_NAMES = ["联系人", "orders"] as const;`,
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema lacks %q:\n%s", want, schema)
		}
	}
	if strings.Contains(schema, `"old"`) {
		t.Errorf("deleted fields must not be typed:\n%s", schema)
	}

	empty := generateSchema("workflow", "db", nil)
	for _, want := range []string{"run `autablectl workflow sdk` again", "export interface Tables {}", "export interface ReadOnlyFields {}", "export const TABLE_NAMES = [] as const;"} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty schema lacks %q:\n%s", want, empty)
		}
	}
}

func TestAppInitScaffoldsProject(t *testing.T) {
	env := newTestEnv(t)
	dir := filepath.Join(env.dir, "contacts-app")
	out := decode[map[string]any](t, env.run(0, "app", "init", dir, "--database", "db", "--name", "contacts", "--title", "Contacts & more"))
	if out["dir"] != dir {
		t.Fatalf("unexpected output %#v", out)
	}

	manifest, err := readAppManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest != (appManifest{Database: "db", Name: "contacts", Title: "Contacts & more", SDK: appsdk.Version()}) {
		t.Fatalf("unexpected manifest %#v", manifest)
	}
	for rel, want := range map[string]string{
		"package.json":           `"name": "contacts"`,
		"index.html":             "<title>Contacts &amp; more</title>",
		"vite.config.ts":         `base: "./"`,
		".gitignore":             "node_modules",
		"src/main.tsx":           "basename={basePath()}",
		"src/autable/autable.ts": appsdk.Files()["autable.ts"],
		"src/autable/schema.ts":  `"名称": string | null;`,
		"AGENTS.md":              "autablectl app push",
	} {
		content := readFile(t, filepath.Join(dir, filepath.FromSlash(rel)))
		if !strings.Contains(content, want) {
			t.Errorf("%s lacks %q", rel, want)
		}
		if rel != "src/autable/autable.ts" && strings.Contains(content, "\r\n") {
			t.Errorf("%s has CRLF line endings", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "_gitignore")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the template's _gitignore must be written as .gitignore")
	}
	if strings.Contains(readFile(t, filepath.Join(dir, "package.json")), "__APP_") {
		t.Errorf("placeholders left in package.json")
	}

	// Never into a non-empty directory, never with a bad name or database.
	env.run(1, "app", "init", dir, "--database", "db", "--name", "contacts")
	env.run(2, "app", "init", filepath.Join(env.dir, "x"), "--database", "db", "--name", "Not_Valid")
	env.run(2, "app", "init", filepath.Join(env.dir, "x"), "--database", "db")
	env.run(1, "app", "init", filepath.Join(env.dir, "missing-db"), "--database", "nope", "--name", "x")
	if _, err := os.Stat(filepath.Join(env.dir, "missing-db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a failed init must not leave a directory behind")
	}
}

func TestAppSDKRefreshesSchemaAndManifest(t *testing.T) {
	env := newTestEnv(t)
	dir := filepath.Join(env.dir, "app")
	env.run(0, "app", "init", dir, "--database", "db", "--name", "contacts")
	manifest, err := readAppManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	manifest.SDK = "0.9"
	if err := writeAppManifest(dir, manifest); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{
		"src/autable/retired.ts": appProject.sdkMarker + " A file a newer SDK dropped.\n",
		"src/autable/mine.ts":    "export const mine = 1;\n",
	})
	env.run(0, "fields", "add", "db", "contacts", "--fields", `[{"name":"数量","type":"int"}]`)

	out := decode[map[string]any](t, env.run(0, "app", "sdk", dir))
	if out["sdk"] != appsdk.Version() || out["previous_sdk"] != "0.9" {
		t.Fatalf("unexpected output %#v", out)
	}
	if manifest, _ := readAppManifest(dir); manifest.SDK != appsdk.Version() {
		t.Fatalf("manifest sdk not updated: %#v", manifest)
	}
	if schema := readFile(t, filepath.Join(dir, "src", "autable", "schema.ts")); !strings.Contains(schema, `"数量": number | null;`) {
		t.Fatalf("schema not refreshed:\n%s", schema)
	}
	if _, err := os.Stat(filepath.Join(dir, "src", "autable", "retired.ts")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a vendored file the server no longer ships must be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "src", "autable", "mine.ts")); err != nil {
		t.Errorf("files autablectl did not vendor must stay: %v", err)
	}
}

func TestPackAppOutsideGitSkipsDependenciesAndOutput(t *testing.T) {
	dir := t.TempDir()
	noGitAbove(t, dir)
	writeFiles(t, dir, map[string]string{
		"autable.app.json":           `{"database":"db","name":"contacts","sdk":"1.0"}`,
		"package.json":               "{}",
		"src/main.ts":                "main",
		"src/autable/autable.ts":     "sdk",
		"src/名称.ts":                  "unicode",
		"node_modules/dep/index.js":  "dep",
		"src/node_modules/nested.js": "nested dep",
		"dist/index.html":            "<html></html>",
		"dist/assets/app.js":         "built",
		".autable/state.json":        "{}",
		".git/config":                "not a repository",
	})
	archive, counts, err := packApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries := archiveEntries(t, archive)
	want := []string{
		"autable.app.json",
		"dist/assets/app.js",
		"dist/index.html",
		"source/autable.app.json",
		"source/package.json",
		"source/src/autable/autable.ts",
		"source/src/main.ts",
		"source/src/名称.ts",
	}
	if got := sortedKeys(entries); !slices.Equal(got, want) {
		t.Fatalf("archive entries\n got %v\nwant %v", got, want)
	}
	if counts.dist != 2 || counts.source != 5 {
		t.Fatalf("unexpected counts %#v", counts)
	}
	bundle, err := appbundle.Parse(archive)
	if err != nil {
		t.Fatalf("the server would reject the archive: %v", err)
	}
	if string(bundle.Dist["assets/app.js"]) != "built" {
		t.Fatalf("unexpected dist %#v", bundle.Dist)
	}

	if err := os.Remove(filepath.Join(dir, "dist", "index.html")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := packApp(dir); err == nil || !strings.Contains(err.Error(), "dist/index.html") {
		t.Fatalf("expected a missing build to fail, got %v", err)
	}
}

func TestPackAppInGitUsesGitFileList(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	writeFiles(t, dir, map[string]string{
		".gitignore":          "node_modules\ndist\nsecret.txt\n",
		"autable.app.json":    `{"database":"db","name":"contacts","sdk":"1.0"}`,
		"src/tracked.ts":      "tracked",
		"src/removed.ts":      "staged, then deleted",
		"src/untracked.ts":    "new",
		"secret.txt":          "ignored",
		"node_modules/x.js":   "dep",
		"dist/index.html":     "<html></html>",
		".autable/state.json": "{}",
	})
	git("add", "src/tracked.ts", "src/removed.ts")
	if err := os.Remove(filepath.Join(dir, "src", "removed.ts")); err != nil {
		t.Fatal(err)
	}
	archive, _, err := packApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"autable.app.json",
		"dist/index.html",
		"source/.autable/state.json",
		"source/.gitignore",
		"source/autable.app.json",
		"source/src/tracked.ts",
		"source/src/untracked.ts",
	}
	if got := sortedKeys(archiveEntries(t, archive)); !slices.Equal(got, want) {
		t.Fatalf("archive entries\n got %v\nwant %v", got, want)
	}
}

func TestRollbackTargetIsNewestBelowPublished(t *testing.T) {
	versions := func(numbers ...int64) []int64 { return numbers }
	cases := []struct {
		published int64
		versions  []int64
		want      int64
	}{
		{3, versions(4, 3, 2, 1), 2},
		{5, versions(1, 4, 5), 4},
		{2, versions(6, 5, 2, 1), 1},
		{1, versions(3, 2, 1), 0},
		{0, versions(1), 0},
	}
	for _, tc := range cases {
		got, err := rollbackTarget(tc.published, tc.versions)
		if tc.want == 0 {
			if err == nil {
				t.Errorf("published %d: expected an error, got %d", tc.published, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("published %d: got %d, %v; want %d", tc.published, got, err, tc.want)
		}
	}
}

func TestAppPushPublishRollbackPull(t *testing.T) {
	env := newTestEnv(t)
	dir := filepath.Join(env.dir, "app")
	noGitAbove(t, dir)
	env.run(0, "app", "init", dir, "--database", "db", "--name", "contacts")
	writeFiles(t, dir, map[string]string{"node_modules/dep/index.js": "dependency"})

	builds := 0
	env.npm = func(buildDir string, extra []string, out io.Writer, args ...string) error {
		if strings.Join(args, " ") != "run build" {
			return fmt.Errorf("unexpected npm %v", args)
		}
		builds++
		writeFiles(t, buildDir, map[string]string{
			"dist/index.html":    fmt.Sprintf("<!doctype html><html><head></head><body>build %d</body></html>", builds),
			"dist/assets/app.js": "console.log(1)",
		})
		return nil
	}

	type pushed struct {
		Version          int64  `json:"version"`
		PublishedVersion int64  `json:"published_version"`
		PreviewURL       string `json:"preview_url"`
		URL              string `json:"url"`
		SourceFiles      int    `json:"source_files"`
	}
	first := decode[pushed](t, env.run(0, "app", "push", dir, "-m", "first"))
	if first.Version != 1 || first.PublishedVersion != 0 || first.PreviewURL != env.server.URL+"/apps/db/contacts/@1/" || first.URL != env.server.URL+"/apps/db/contacts/" {
		t.Fatalf("unexpected first push %#v", first)
	}
	second := decode[pushed](t, env.run(0, "app", "push", dir, "--publish"))
	if second.Version != 2 || second.PublishedVersion != 2 {
		t.Fatalf("unexpected second push %#v", second)
	}
	third := decode[pushed](t, env.run(0, "app", "push", dir, "--no-build", "--message", "third"))
	if third.Version != 3 || third.PublishedVersion != 2 || builds != 2 {
		t.Fatalf("unexpected third push %#v (builds %d)", third, builds)
	}

	env.run(0, "app", "publish", "db", "contacts", "3")
	rolled := decode[map[string]any](t, env.run(0, "app", "rollback", "db", "contacts"))
	if rolled["published_version"] != float64(2) || rolled["previous_version"] != float64(3) {
		t.Fatalf("unexpected rollback %#v", rolled)
	}
	env.run(0, "app", "rollback", "db", "contacts")
	env.run(1, "app", "rollback", "db", "contacts")

	detail := decode[appDetail](t, env.run(0, "app", "versions", "db", "contacts"))
	if detail.App.PublishedVersion != 1 || len(detail.Versions) != 3 {
		t.Fatalf("unexpected versions %#v", detail)
	}
	messages := map[int64]string{}
	for _, version := range detail.Versions {
		messages[version.Version] = version.Message
	}
	if messages[1] != "first" || messages[3] != "third" {
		t.Fatalf("unexpected messages %#v", messages)
	}
	if apps := decode[[]appSummary](t, env.run(0, "app", "list", "db")); len(apps) != 1 || apps[0].Name != "contacts" {
		t.Fatalf("unexpected list %#v", apps)
	}

	// The published build is what users get.
	request, err := http.NewRequest(http.MethodGet, env.server.URL+"/apps/db/contacts/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+env.token)
	response, err := env.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(page), "build 1") || !strings.Contains(string(page), `<base href="/apps/db/contacts/">`) {
		t.Fatalf("unexpected app page %d: %s", response.StatusCode, page)
	}

	// pull defaults to the published version and restores the source.
	pulled := filepath.Join(env.dir, "pulled")
	out := decode[map[string]any](t, env.run(0, "app", "pull", "db", "contacts", pulled))
	if out["version"] != float64(1) {
		t.Fatalf("pull should default to the published version: %#v", out)
	}
	for _, rel := range []string{"autable.app.json", "package.json", "src/App.tsx", "src/autable/schema.ts", ".gitignore"} {
		if readFile(t, filepath.Join(pulled, filepath.FromSlash(rel))) != readFile(t, filepath.Join(dir, filepath.FromSlash(rel))) {
			t.Errorf("pulled %s differs from the pushed project", rel)
		}
	}
	for _, rel := range []string{"node_modules", "dist"} {
		if _, err := os.Stat(filepath.Join(pulled, rel)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("pulled source must not contain %s", rel)
		}
	}
	env.run(1, "app", "pull", "db", "contacts", pulled)
	if out := decode[map[string]any](t, env.run(0, "app", "pull", "db", "contacts", filepath.Join(env.dir, "v3"), "--version", "3")); out["version"] != float64(3) {
		t.Fatalf("unexpected pull %#v", out)
	}
	env.run(1, "app", "pull", "db", "contacts", filepath.Join(env.dir, "v9"), "--version", "9")
}

func TestAppPullDefaultsToLatestWhenUnpublished(t *testing.T) {
	env := newTestEnv(t)
	dir := filepath.Join(env.dir, "app")
	noGitAbove(t, dir)
	env.run(0, "app", "init", dir, "--database", "db", "--name", "contacts")
	writeFiles(t, dir, map[string]string{"dist/index.html": "<html></html>"})
	env.run(0, "app", "push", dir, "--no-build")
	writeFiles(t, dir, map[string]string{"src/later.ts": "export {};\n"})
	env.run(0, "app", "push", dir, "--no-build")
	pulled := filepath.Join(env.dir, "pulled")
	if out := decode[map[string]any](t, env.run(0, "app", "pull", "db", "contacts", pulled)); out["version"] != float64(2) {
		t.Fatalf("pull should default to the latest version: %#v", out)
	}
	if readFile(t, filepath.Join(pulled, "src", "later.ts")) != "export {};\n" {
		t.Fatal("pulled the wrong version")
	}
}

func TestAppDevRunsNPMWithLogin(t *testing.T) {
	env := newTestEnv(t)
	dir := filepath.Join(env.dir, "app")
	env.run(0, "app", "init", dir, "--database", "db", "--name", "contacts")
	env.run(1, "app", "dev", dir) // no node_modules yet
	writeFiles(t, dir, map[string]string{"node_modules/.package-lock.json": "{}"})
	var gotEnv, gotArgs []string
	env.npm = func(_ string, extra []string, _ io.Writer, args ...string) error {
		gotEnv, gotArgs = extra, args
		return nil
	}
	env.run(0, "app", "dev", dir)
	if strings.Join(gotArgs, " ") != "run dev" {
		t.Fatalf("unexpected npm args %v", gotArgs)
	}
	if !slices.Contains(gotEnv, "AUTABLE_SERVER="+env.server.URL) || !slices.Contains(gotEnv, "AUTABLE_TOKEN="+env.token) {
		t.Fatalf("dev server did not get the login: %v", gotEnv)
	}
}

func TestUnpackSourceRejectsUnsafePaths(t *testing.T) {
	pack := func(name string) []byte {
		var out bytes.Buffer
		gz := gzip.NewWriter(&out)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte("x"))
		_ = tw.Close()
		_ = gz.Close()
		return out.Bytes()
	}
	for _, name := range []string{"../escape.txt", "a/../../escape.txt", "/abs.txt", `a\b.txt`, "./a.txt"} {
		dir := t.TempDir()
		if _, err := unpackSource(pack(name), dir); err == nil {
			t.Errorf("%q: expected the entry to be rejected", name)
		}
	}
	dir := t.TempDir()
	files, err := unpackSource(pack("src/名称.ts"), dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("unexpected unpack %v %v", files, err)
	}
	if readFile(t, filepath.Join(dir, "src", "名称.ts")) != "x" {
		t.Fatal("file not written")
	}
}

func TestAppManifestRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"autable.app.json": `{"database":"db","name":"x","sdk":"1.0","extra":1}`})
	if _, err := readAppManifest(dir); err == nil {
		t.Fatal("expected unknown manifest fields to be rejected, as the server does")
	}
	data, _ := json.Marshal(appManifest{Database: "db", Name: "x", SDK: "1.0"})
	writeFiles(t, dir, map[string]string{"autable.app.json": string(data)})
	if _, err := readAppManifest(dir); err != nil {
		t.Fatal(err)
	}
}
