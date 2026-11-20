package ctl

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"autable/internal/appbundle"
)

// gitRepo initializes a git repository in dir, or skips the test when git
// is not installed.
func gitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func buildInputsJSON(t *testing.T, inputs ...string) string {
	t.Helper()
	data, err := json.Marshal(buildInputs{Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const sharedWorkflowManifest = `{"database":"db","name":"order-sync","sdk":"1.0"}`

// sharedRepo lays out a development repository with a shared lib/ and a
// workflow project in workflows/order-sync whose build used lib/notify.ts.
func sharedRepo(t *testing.T) (repo, project string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	gitRepo(t, repo)
	project = filepath.Join(repo, "workflows", "order-sync")
	writeFiles(t, repo, map[string]string{
		".gitignore":                "node_modules\ndist\n.autable\n",
		"lib/notify.ts":             "export const notify = 1;\n",
		"lib/format/名称.ts":          "export const 名称 = 2;\n",
		"lib/unused.ts":             "export const unused = 3;\n",
		"node_modules/dep/index.js": "dependency at the root",
		"apps/other/src/main.ts":    "another project",
	})
	writeFiles(t, project, map[string]string{
		"autable.workflow.json":      sharedWorkflowManifest,
		"package.json":               "{}",
		"src/index.ts":               `import { notify } from "../../../lib/notify";`,
		"src/autable/index.ts":       "sdk",
		"dist/workflow.js":           "var __workflow = {};",
		"node_modules/esbuild/x.js":  "dependency",
		".autable/build-inputs.json": buildInputsJSON(t, "src/index.ts", "src/autable/index.ts", "../../lib/notify.ts", "../../lib/format/名称.ts", "../../node_modules/dep/index.js", "node_modules/esbuild/x.js", "../../lib/gone.ts", "<define:process>", "../../lib/notify.ts"),
	})
	return repo, project
}

func TestPackIncludesSharedFilesTheBuildUsed(t *testing.T) {
	_, project := sharedRepo(t)
	archive, counts, err := workflowProject.pack(project)
	if err != nil {
		t.Fatal(err)
	}
	entries := archiveEntries(t, archive)
	want := []string{
		"autable.workflow.json",
		"dist/workflow.js",
		"source/lib/format/名称.ts",
		"source/lib/notify.ts",
		"source/workflows/order-sync/autable.workflow.json",
		"source/workflows/order-sync/package.json",
		"source/workflows/order-sync/src/autable/index.ts",
		"source/workflows/order-sync/src/index.ts",
	}
	if got := sortedKeys(entries); !slices.Equal(got, want) {
		t.Fatalf("archive entries\n got %v\nwant %v", got, want)
	}
	if counts.root != "workflows/order-sync" || counts.shared != 2 || counts.source != 6 || !counts.inputsRecorded {
		t.Fatalf("unexpected counts %#v", counts)
	}
	if entries["source/workflows/order-sync/autable.workflow.json"] != sharedWorkflowManifest {
		t.Errorf("the project's own manifest must be uploaded unchanged")
	}
	bundle, err := appbundle.Workflow.Parse(archive)
	if err != nil {
		t.Fatalf("the server would reject the archive: %v", err)
	}
	if bundle.Manifest.Root != "workflows/order-sync" || bundle.Manifest.Name != "order-sync" {
		t.Fatalf("unexpected uploaded manifest %#v", bundle.Manifest)
	}

	// An absolute input (as Vite reports module ids) counts the same.
	writeFiles(t, project, map[string]string{
		".autable/build-inputs.json": buildInputsJSON(t, filepath.ToSlash(filepath.Join(project, "..", "..", "lib", "unused.ts"))),
	})
	archive, _, err = workflowProject.pack(project)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(archiveEntries(t, archive)); !slices.Contains(got, "source/lib/unused.ts") || slices.Contains(got, "source/lib/notify.ts") {
		t.Fatalf("expected exactly the used shared file, got %v", got)
	}
}

func TestPackWithoutSharedFilesKeepsTheProjectLayout(t *testing.T) {
	_, project := sharedRepo(t)
	writeFiles(t, project, map[string]string{".autable/build-inputs.json": buildInputsJSON(t, "src/index.ts", "node_modules/esbuild/x.js")})
	archive, counts, err := workflowProject.pack(project)
	if err != nil {
		t.Fatal(err)
	}
	entries := archiveEntries(t, archive)
	if counts.root != "" || counts.shared != 0 || entries["autable.workflow.json"] != sharedWorkflowManifest {
		t.Fatalf("unexpected counts %#v / manifest %q", counts, entries["autable.workflow.json"])
	}
	if _, ok := entries["source/src/index.ts"]; !ok {
		t.Fatalf("expected the project layout, got %v", sortedKeys(entries))
	}

	// Without the build-inputs file (an older template), only the project.
	if err := os.Remove(filepath.Join(project, ".autable", "build-inputs.json")); err != nil {
		t.Fatal(err)
	}
	if _, counts, err := workflowProject.pack(project); err != nil || counts.inputsRecorded || counts.root != "" {
		t.Fatalf("unexpected pack without inputs: %#v, %v", counts, err)
	}
}

func TestPackRefusesSharedFilesOutsideARepository(t *testing.T) {
	base := t.TempDir()
	noGitAbove(t, base)
	project := filepath.Join(base, "order-sync")
	writeFiles(t, base, map[string]string{"lib/notify.ts": "export {};"})
	writeFiles(t, project, map[string]string{
		"autable.workflow.json":      sharedWorkflowManifest,
		"dist/workflow.js":           "var __workflow = {};",
		".autable/build-inputs.json": buildInputsJSON(t, "src/index.ts", "../lib/notify.ts"),
	})
	_, _, err := workflowProject.pack(project)
	if err == nil || !strings.Contains(err.Error(), "outside the workflow directory") || !strings.Contains(err.Error(), "../lib/notify.ts") || !strings.Contains(err.Error(), "git repository") {
		t.Fatalf("expected a clear refusal, got %v", err)
	}

	// Inside a repository, files above its root are refused too.
	repo := filepath.Join(base, "repo")
	gitRepo(t, repo)
	inner := filepath.Join(repo, "order-sync")
	writeFiles(t, inner, map[string]string{
		"autable.workflow.json":      sharedWorkflowManifest,
		"dist/workflow.js":           "var __workflow = {};",
		".autable/build-inputs.json": buildInputsJSON(t, "../../lib/notify.ts"),
	})
	if _, _, err := workflowProject.pack(inner); err == nil || !strings.Contains(err.Error(), "outside the git repository") {
		t.Fatalf("expected files above the repository to be refused, got %v", err)
	}
}

func TestManifestOnDiskMustNotNameARoot(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"autable.workflow.json": `{"database":"db","name":"x","sdk":"1.0","root":"a"}`})
	if _, err := workflowProject.readManifest(dir); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("expected root on disk to be refused, got %v", err)
	}
}

func TestTemplatesRecordBuildInputs(t *testing.T) {
	wf := t.TempDir()
	if err := writeWorkflowTemplate(wf, "x", "X"); err != nil {
		t.Fatal(err)
	}
	app := t.TempDir()
	if err := writeAppTemplate(app, "x", "X"); err != nil {
		t.Fatal(err)
	}
	for file, wants := range map[string][]string{
		filepath.Join(wf, "build.mjs"):       {"metafile: true", buildInputsFile, `"@autable/workflow": "./src/autable/index.ts"`},
		filepath.Join(wf, "tsconfig.json"):   {`"@autable/workflow": ["./src/autable/index.ts"]`},
		filepath.Join(wf, "AGENTS.md"):       {"lib/", "@autable/workflow"},
		filepath.Join(app, "vite.config.ts"): {"this.getModuleIds()", buildInputsFile, `"@autable/app"`},
		filepath.Join(app, "tsconfig.json"):  {`"@autable/app": ["./src/autable/autable.ts"]`},
		filepath.Join(app, "AGENTS.md"):      {"lib/", "@autable/app"},
		filepath.Join(wf, "README.md"):       {"lib/"},
		filepath.Join(app, "README.md"):      {"lib/"},
	} {
		content := readFile(t, file)
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Errorf("%s lacks %q", file, want)
			}
		}
	}
}

// TestSharedLibraryPushAndPull pushes a workflow and an app of one
// development repository that both use lib/, then pulls them: each pull
// is a buildable slice of the repository, the project at its old path.
func TestSharedLibraryPushAndPull(t *testing.T) {
	env := newTestEnv(t)
	repo := filepath.Join(env.dir, "repo")
	gitRepo(t, repo)
	writeFiles(t, repo, map[string]string{
		".gitignore":     "node_modules\ndist\n.autable\n",
		"lib/notify.ts":  "export const notify = (text: string) => text;\n",
		"lib/theme.ts":   "export const accent = \"teal\";\n",
		"lib/unused.ts":  "export {};\n",
		"package.json":   `{"private":true}`,
		"docs/notes.txt": "not part of any build",
	})
	wf := filepath.Join(repo, "workflows", "order-sync")
	app := filepath.Join(repo, "apps", "board")
	env.run(0, "workflow", "init", wf, "--database", "db", "--name", "order-sync")
	env.run(0, "app", "init", app, "--database", "db", "--name", "board")

	env.npm = func(buildDir string, _ []string, _ io.Writer, args ...string) error {
		if strings.Join(args, " ") != "run build" {
			return fmt.Errorf("unexpected npm %v", args)
		}
		switch buildDir {
		case wf:
			writeFiles(t, buildDir, map[string]string{
				"dist/workflow.js":           testWorkflowBundle,
				".autable/build-inputs.json": buildInputsJSON(t, "src/index.ts", "src/autable/index.ts", "../../lib/notify.ts"),
			})
		case app:
			writeFiles(t, buildDir, map[string]string{
				"dist/index.html":            "<!doctype html><html><head></head><body></body></html>",
				".autable/build-inputs.json": buildInputsJSON(t, filepath.ToSlash(filepath.Join(app, "src", "main.tsx")), filepath.ToSlash(filepath.Join(repo, "lib", "theme.ts")), filepath.ToSlash(filepath.Join(repo, "lib", "notify.ts")), "\x00vite/modulepreload-polyfill.js"),
			})
		default:
			return fmt.Errorf("unexpected build in %s", buildDir)
		}
		return nil
	}

	type pushResult struct {
		Version     int64  `json:"version"`
		Root        string `json:"root"`
		SharedFiles int    `json:"shared_files"`
	}
	if out := decode[pushResult](t, env.run(0, "workflow", "push", wf, "--publish")); out.Version != 1 || out.Root != "workflows/order-sync" || out.SharedFiles != 1 {
		t.Fatalf("unexpected workflow push %#v", out)
	}
	if out := decode[pushResult](t, env.run(0, "app", "push", app, "--publish")); out.Version != 1 || out.Root != "apps/board" || out.SharedFiles != 2 {
		t.Fatalf("unexpected app push %#v", out)
	}

	type pullResult struct {
		Root       string   `json:"root"`
		ProjectDir string   `json:"project_dir"`
		Files      []string `json:"files"`
	}
	check := func(kind, name, root string, shared, absent []string, projectFiles ...string) {
		t.Helper()
		pulled := filepath.Join(env.dir, "pulled-"+name)
		out := decode[pullResult](t, env.run(0, kind, "pull", "db", name, pulled))
		if out.Root != root || out.ProjectDir != filepath.Join(pulled, filepath.FromSlash(root)) {
			t.Fatalf("unexpected %s pull %#v", kind, out)
		}
		for _, rel := range shared {
			if readFile(t, filepath.Join(pulled, filepath.FromSlash(rel))) != readFile(t, filepath.Join(repo, filepath.FromSlash(rel))) {
				t.Errorf("%s: pulled %s differs", kind, rel)
			}
		}
		for _, rel := range projectFiles {
			if readFile(t, filepath.Join(out.ProjectDir, filepath.FromSlash(rel))) != readFile(t, filepath.Join(repo, filepath.FromSlash(root), filepath.FromSlash(rel))) {
				t.Errorf("%s: pulled project file %s differs", kind, rel)
			}
			if !slices.Contains(out.Files, root+"/"+rel) {
				t.Errorf("%s: pull output lacks %s: %v", kind, rel, out.Files)
			}
		}
		for _, rel := range absent {
			if _, err := os.Stat(filepath.Join(pulled, filepath.FromSlash(rel))); err == nil {
				t.Errorf("%s: pulled slice must not contain %s", kind, rel)
			}
		}
		// The pulled project reads as a project again.
		manifest, err := projectKindFor(kind).readManifest(out.ProjectDir)
		if err != nil || manifest.Name != name || manifest.Root != "" {
			t.Fatalf("%s: pulled manifest %#v, %v", kind, manifest, err)
		}
	}
	check("workflow", "order-sync", "workflows/order-sync", []string{"lib/notify.ts"},
		[]string{"lib/theme.ts", "lib/unused.ts", "docs", "package.json", "autable.workflow.json", "apps"},
		"autable.workflow.json", "package.json", "build.mjs", "src/index.ts", "src/autable/schema.ts")
	check("app", "board", "apps/board", []string{"lib/notify.ts", "lib/theme.ts"},
		[]string{"lib/unused.ts", "docs", "autable.app.json", "workflows"},
		"autable.app.json", "vite.config.ts", "src/App.tsx")
}

func projectKindFor(noun string) projectKind {
	if noun == "app" {
		return appProject
	}
	return workflowProject
}
