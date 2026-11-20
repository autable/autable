package ctl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autable/internal/wfsdk"
)

// testLibraryBundle stands in for what the library template's build
// produces: an IIFE assigning __lib.
func testLibraryBundle(prefix string) string {
	return `var __lib = (() => ({ label: (value) => "` + prefix + `" + value, secret: () => __autable_host.secret("token") || null }))();`
}

// testLibraryWorkflowBundle imports @lib/text as the workflow template's
// build emits it.
const testLibraryWorkflowBundle = `var __workflow = (() => {
  var text = __autable_lib("@lib/text");
  return {
    triggers: { start: { kind: "manual" } },
    run: function () { return { label: text.label("a"), secret: text.secret() }; }
  };
})();
`

func TestLibraryLifecycle(t *testing.T) {
	env := newTestEnv(t)
	libDir := filepath.Join(env.dir, "text")
	noGitAbove(t, libDir)

	out := decode[map[string]any](t, env.run(0, "lib", "init", libDir, "--name", "text", "--title", "Text helpers"))
	if out["dir"] != libDir || mustJSON(t, out["libraries"]) != "[]" {
		t.Fatalf("unexpected lib init output %#v", out)
	}
	manifest, err := libProject.readManifest(libDir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest != (projectManifest{Name: "text", Title: "Text helpers", SDK: wfsdk.Version()}) {
		t.Fatalf("unexpected manifest %#v", manifest)
	}
	if raw := readFile(t, filepath.Join(libDir, "autable.lib.json")); strings.Contains(raw, "database") {
		t.Fatalf("a library manifest has no database: %s", raw)
	}
	for rel, want := range map[string]string{
		"package.json":          `"name": "text"`,
		"build.mjs":             `globalName: "__lib"`,
		"tsconfig.json":         `"@autable/workflow"`,
		"src/index.ts":          `from "@autable/workflow"`,
		"src/autable/index.ts":  wfsdk.Files()["index.ts"],
		"src/autable/schema.ts": "export interface Tables {}",
		"AGENTS.md":             "every workflow that\nimports it",
		"README.md":             "# Text helpers",
		".gitignore":            "node_modules",
	} {
		content := readFile(t, filepath.Join(libDir, filepath.FromSlash(rel)))
		if !strings.Contains(content, want) {
			t.Errorf("%s lacks %q", rel, want)
		}
		if strings.Contains(content, "__LIB_") {
			t.Errorf("%s has placeholders", rel)
		}
	}
	env.run(2, "lib", "init", filepath.Join(env.dir, "x"))
	env.run(1, "lib", "init", libDir, "--name", "text")

	version := 0
	env.npm = func(buildDir string, _ []string, _ io.Writer, args ...string) error {
		if strings.Join(args, " ") != "run build" {
			return fmt.Errorf("unexpected npm %v", args)
		}
		if buildDir == libDir {
			version++
			writeFiles(t, buildDir, map[string]string{
				"dist/lib.js": testLibraryBundle(fmt.Sprintf("v%d:", version)),
				// A plain module declaration, which vendoring wraps.
				"dist/index.d.ts": "/** Labels a value. */\nexport declare function label(value: string): string;\nexport declare function secret(): string | null;\n",
			})
			return nil
		}
		writeFiles(t, buildDir, map[string]string{"dist/workflow.js": testLibraryWorkflowBundle})
		return nil
	}
	type libPushResult struct {
		Version          int64 `json:"version"`
		PublishedVersion int64 `json:"published_version"`
	}
	if pushed := decode[libPushResult](t, env.run(0, "lib", "push", libDir, "-m", "first")); pushed.Version != 1 || pushed.PublishedVersion != 0 {
		t.Fatalf("unexpected first lib push %#v", pushed)
	}

	// A workflow importing an unpublished library cannot be published.
	wfDir := filepath.Join(env.dir, "labels")
	noGitAbove(t, wfDir)
	env.run(0, "workflow", "init", wfDir, "--database", "db", "--name", "labels")
	if _, stderr := env.runOutput(1, "workflow", "push", wfDir, "--publish"); !strings.Contains(stderr, "no published version") {
		t.Fatalf("expected the unpublished library to block publishing, got %q", stderr)
	}
	env.run(0, "lib", "publish", "text", "1")

	// workflow sdk vendors the published library's declarations.
	writeFiles(t, wfDir, map[string]string{
		"src/autable/libs/gone.d.ts": libraryTypesMarker + " stale\n",
		"src/autable/libs/mine.d.ts": "declare module \"x\" {}\n",
	})
	sdkOut := decode[map[string]any](t, env.run(0, "workflow", "sdk", wfDir))
	if mustJSON(t, sdkOut["libraries"]) != `[{"name":"text","version":1}]` {
		t.Fatalf("unexpected sdk output %#v", sdkOut)
	}
	declaration := readFile(t, filepath.Join(wfDir, "src", "autable", "libs", "text.d.ts"))
	for _, want := range []string{libraryTypesMarker, "@lib/text version 1", "declare module \"@lib/text\" {\n/** Labels a value. */\nexport function label(value: string): string;", "\n}\n"} {
		if !strings.Contains(declaration, want) {
			t.Errorf("vendored declaration lacks %q:\n%s", want, declaration)
		}
	}
	if _, err := os.Stat(filepath.Join(wfDir, "src", "autable", "libs", "gone.d.ts")); !errors.Is(err, os.ErrNotExist) {
		t.Error("declarations of libraries no longer published must be removed")
	}
	if _, err := os.Stat(filepath.Join(wfDir, "src", "autable", "libs", "mine.d.ts")); err != nil {
		t.Error("files autablectl did not vendor must stay")
	}
	// A library does not vendor its own declarations.
	if libSDK := decode[map[string]any](t, env.run(0, "lib", "sdk", libDir)); mustJSON(t, libSDK["libraries"]) != "[]" {
		t.Fatalf("a library must not vendor itself: %#v", libSDK)
	}

	env.run(0, "workflow", "push", wfDir, "--publish")
	env.stdin = "caller-secret"
	env.run(0, "workflow", "secret", "set", "db", "labels", "token")
	env.stdin = ""
	runOnce := func() map[string]any {
		t.Helper()
		run := decode[map[string]any](t, env.run(0, "workflow", "run", "db", "labels", "--wait"))
		return run
	}
	run := runOnce()
	if mustJSON(t, run["output"]) != `{"label":"v1:a","secret":"caller-secret"}` || mustJSON(t, run["libraries"]) != `[{"name":"text","version":1}]` {
		t.Fatalf("unexpected first run %#v", run)
	}

	// Publishing v2 changes the next run without pushing the workflow.
	env.run(0, "lib", "push", libDir, "--publish", "-m", "second")
	if run := runOnce(); mustJSON(t, run["output"]) != `{"label":"v2:a","secret":"caller-secret"}` {
		t.Fatalf("expected v2 behaviour, got %#v", run)
	}
	rolled := decode[map[string]any](t, env.run(0, "lib", "rollback", "text"))
	if rolled["published_version"] != float64(1) || rolled["previous_version"] != float64(2) {
		t.Fatalf("unexpected rollback %#v", rolled)
	}
	if run := runOnce(); mustJSON(t, run["output"]) != `{"label":"v1:a","secret":"caller-secret"}` {
		t.Fatalf("expected v1 behaviour after the rollback, got %#v", run)
	}

	detail := decode[map[string]any](t, env.run(0, "lib", "versions", "text"))
	if importers := mustJSON(t, detail["library"].(map[string]any)["imported_by"]); importers != `[{"database_name":"db","kind":"workflow","name":"labels","title":"labels","version":1}]` {
		t.Fatalf("expected the importing workflow listed, got %#v", detail)
	}
	if listed := decode[[]libSummary](t, env.run(0, "lib", "list")); len(listed) != 1 || listed[0].Name != "text" || listed[0].PublishedVersion != 1 {
		t.Fatalf("unexpected list %#v", listed)
	}
	if _, stderr := env.runOutput(1, "lib", "delete", "text"); !strings.Contains(stderr, "workflow db/labels") {
		t.Fatalf("expected deleting an imported library to be refused, got %q", stderr)
	}

	pulled := filepath.Join(env.dir, "pulled-text")
	if out := decode[map[string]any](t, env.run(0, "lib", "pull", "text", pulled, "--version", "2")); out["version"] != float64(2) || out["root"] != "" {
		t.Fatalf("unexpected pull %#v", out)
	}
	for _, rel := range []string{"autable.lib.json", "package.json", "build.mjs", "src/index.ts"} {
		if readFile(t, filepath.Join(pulled, filepath.FromSlash(rel))) != readFile(t, filepath.Join(libDir, filepath.FromSlash(rel))) {
			t.Errorf("pulled %s differs", rel)
		}
	}

	// Non-admins cannot manage libraries.
	admin := env.token
	env.token = env.memberToken
	if _, stderr := env.runOutput(1, "lib", "list"); !strings.Contains(stderr, "403") {
		t.Fatalf("expected the library list to be refused, got %q", stderr)
	}
	env.token = admin

	// Once nothing imports it, it can be deleted.
	writeFiles(t, wfDir, map[string]string{"dist/workflow.js": testWorkflowBundle})
	env.run(0, "workflow", "push", wfDir, "--no-build", "--publish")
	env.run(0, "lib", "delete", "text")
	env.run(1, "lib", "versions", "text")
}

func TestLibraryManifestValidation(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"autable.lib.json": `{"database":"db","name":"text","sdk":"1.0"}`})
	if _, err := libProject.readManifest(dir); err == nil || !strings.Contains(err.Error(), "no database") {
		t.Fatalf("expected a database to be refused, got %v", err)
	}
	writeFiles(t, dir, map[string]string{"autable.lib.json": `{"name":"text","sdk":"1.0"}`})
	if _, err := libProject.readManifest(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := libProject.readManifest(t.TempDir()); err == nil || !strings.Contains(err.Error(), "autablectl lib init") {
		t.Fatalf("expected a hint to run lib init, got %v", err)
	}
	if got := libProject.apiPath("", "text", "versions"); got != "/api/libs/text/versions" {
		t.Fatalf("unexpected library API path %s", got)
	}
}

func TestLibraryDeclarationKeepsAmbientModules(t *testing.T) {
	ambient := "// header\n\ndeclare module \"@lib/http\" {\nexport function post(): void;\n}\n"
	got := libraryDeclaration(libraryTypes{Name: "http", Version: 4, Types: ambient})
	if !strings.HasPrefix(got, libraryTypesMarker) || !strings.Contains(got, ambient) || strings.Count(got, "declare module") != 1 {
		t.Fatalf("expected ambient declarations kept as they are:\n%s", got)
	}
}
