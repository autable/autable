package ctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"autable/internal/appbundle"
	"autable/internal/permission"
	"autable/internal/wfsdk"
)

func TestLookupCommandMatchesLongestName(t *testing.T) {
	cases := map[string]string{
		"workflow secret set db wf KEY": "workflow secret set",
		"workflow var unset db wf KEY":  "workflow var unset",
		"workflow run db wf --wait":     "workflow run",
		"workflow run-log db wf 3":      "workflow run-log",
		"whoami":                        "whoami",
	}
	for line, want := range cases {
		cmd, _, ok := lookupCommand(strings.Fields(line))
		if !ok || cmd.usage != commands[want].usage {
			t.Errorf("%q: got %q, want %q", line, cmd.usage, want)
		}
	}
	if _, _, ok := lookupCommand([]string{"workflow", "secret"}); ok {
		t.Error("an incomplete command must not match")
	}
	for _, retired := range []string{"pull", "push", "status", "nodes", "workflow get", "workflow set"} {
		if _, ok := commands[retired]; ok {
			t.Errorf("command %q should be gone", retired)
		}
	}
}

func TestReadSecretValueFromPipe(t *testing.T) {
	for input, want := range map[string]string{
		"s3cret\n":     "s3cret",
		"s3cret\r\n":   "s3cret",
		"two\nlines\n": "two\nlines",
		"no newline":   "no newline",
		" spaced \n\n": " spaced \n",
		"":             "",
	} {
		a := &app{stdin: strings.NewReader(input)}
		got, err := a.readSecretValue("key")
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestDecodeRunAcceptsBareAndWrappedRuns(t *testing.T) {
	for _, body := range []string{
		`{"id": 12, "status": "failed", "error": "boom"}`,
		`{"run": {"id": 12, "status": "failed", "error": "boom"}}`,
	} {
		var run workflowRun
		if err := decodeRun([]byte(body), &run); err != nil {
			t.Fatal(err)
		}
		if run.ID.String() != "12" || run.Status != "failed" || run.Error != "boom" || !run.finished() {
			t.Fatalf("%s: unexpected run %#v", body, run)
		}
	}
	for status, finished := range map[string]bool{"queued": false, "running": false, "succeeded": true, "failed": true, "cancelled": true} {
		if (workflowRun{Status: status}).finished() != finished {
			t.Errorf("status %s: finished should be %v", status, finished)
		}
	}
}

func TestWorkflowTemplateAndPack(t *testing.T) {
	dir := t.TempDir()
	noGitAbove(t, dir)
	if err := writeWorkflowTemplate(dir, "nightly-sync", "Nightly sync"); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{
		"package.json":  `"name": "nightly-sync"`,
		"build.mjs":     `globalName: "__workflow"`,
		"tsconfig.json": `"lib": ["ES2022"]`,
		".gitignore":    "node_modules",
		"src/index.ts":  "TriggerEvent<typeof triggers>",
		"README.md":     "# Nightly sync",
		"AGENTS.md":     "autablectl workflow push",
		"CLAUDE.md":     "AGENTS.md",
	} {
		content := readFile(t, filepath.Join(dir, filepath.FromSlash(rel)))
		if !strings.Contains(content, want) {
			t.Errorf("%s lacks %q", rel, want)
		}
		if strings.Contains(content, "\r\n") || strings.Contains(content, "__WORKFLOW_") {
			t.Errorf("%s has CRLF line endings or placeholders", rel)
		}
	}
	for _, rel := range []string{"src/index.ts", "AGENTS.md"} {
		if content := readFile(t, filepath.Join(dir, filepath.FromSlash(rel))); strings.Contains(content, "step(") || strings.Contains(content, "waitFor") {
			t.Errorf("%s still describes durable steps", rel)
		}
	}

	writeFiles(t, dir, map[string]string{
		"autable.workflow.json":  `{"database":"db","name":"nightly-sync","sdk":"1.0"}`,
		"src/autable/index.ts":   "sdk",
		"node_modules/x/x.js":    "dep",
		"dist/workflow.js":       "var __workflow = {};",
		"dist/workflow.js.map":   "{}",
		"autable.app.json":       `{"not":"used"}`,
		"src/autable/schema.ts":  "schema",
		".autable/anything.json": "{}",
	})
	archive, counts, err := workflowProject.pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries := sortedKeys(archiveEntries(t, archive))
	for _, want := range []string{"autable.workflow.json", "dist/workflow.js", "source/src/index.ts", "source/src/autable/schema.ts", "source/package.json"} {
		if !slices.Contains(entries, want) {
			t.Errorf("archive lacks %s: %v", want, entries)
		}
	}
	for _, entry := range entries {
		if strings.Contains(entry, "node_modules") || strings.HasPrefix(entry, "source/dist/") {
			t.Errorf("archive must not contain %s", entry)
		}
	}
	if counts.dist != 2 {
		t.Errorf("unexpected dist count %d", counts.dist)
	}
	bundle, err := appbundle.Workflow.Parse(archive)
	if err != nil {
		t.Fatalf("the server would reject the archive: %v", err)
	}
	if bundle.Manifest.Name != "nightly-sync" || string(bundle.Dist["workflow.js"]) != "var __workflow = {};" {
		t.Fatalf("unexpected bundle %#v", bundle.Manifest)
	}

	if err := os.Remove(filepath.Join(dir, "dist", "workflow.js")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := workflowProject.pack(dir); err == nil || !strings.Contains(err.Error(), "dist/workflow.js") {
		t.Fatalf("expected a missing build to fail, got %v", err)
	}
}

func TestWorkflowManifestValidation(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"autable.workflow.json": `{"database":"db","name":"Bad_Name","sdk":"1.0"}`})
	if _, err := workflowProject.readManifest(dir); err == nil || !strings.Contains(err.Error(), "workflow name") {
		t.Fatalf("expected the name rule, got %v", err)
	}
	writeFiles(t, dir, map[string]string{"autable.workflow.json": `{"database":"db","name":"ok","sdk":"1.0","extra":true}`})
	if _, err := workflowProject.readManifest(dir); err == nil {
		t.Fatal("expected unknown manifest fields to be rejected")
	}
	if _, err := workflowProject.readManifest(t.TempDir()); err == nil || !strings.Contains(err.Error(), "autablectl workflow init") {
		t.Fatalf("expected a hint to run workflow init, got %v", err)
	}
}

// testWorkflowBundle stands in for what `npm run build` produces: an IIFE
// defining globalThis.__workflow, calling the host API directly.
const testWorkflowBundle = `var __workflow = (function () {
  return {
    triggers: { start: { kind: "manual", fields: [{ name: "name", type: "string" }] } },
    run: function (event) {
      var host = globalThis.__autable_host;
      host.log("info", "greeting", { name: event.input.name });
      if (event.input.name === "fail") throw new Error("asked to fail");
      return { message: "hello " + event.input.name, secret: host.secret("token") !== undefined, region: host.variable("region") || null };
    }
  };
})();
`

func TestWorkflowLifecycle(t *testing.T) {
	env := newTestEnv(t)
	dir := filepath.Join(env.dir, "greet")
	noGitAbove(t, dir)
	out := decode[map[string]any](t, env.run(0, "workflow", "init", dir, "--database", "db", "--name", "greet", "--title", "Greet"))
	if out["dir"] != dir {
		t.Fatalf("unexpected init output %#v", out)
	}
	manifest, err := workflowProject.readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest != (projectManifest{Database: "db", Name: "greet", Title: "Greet", SDK: wfsdk.Version()}) {
		t.Fatalf("unexpected manifest %#v", manifest)
	}
	for rel, want := range map[string]string{
		"src/autable/index.ts":  wfsdk.Files()["index.ts"],
		"src/autable/host.ts":   "__autable_host",
		"src/autable/schema.ts": `"名称": string | null;`,
		"AGENTS.md":             "autablectl workflow push",
	} {
		if content := readFile(t, filepath.Join(dir, filepath.FromSlash(rel))); !strings.Contains(content, want) {
			t.Errorf("%s lacks %q", rel, want)
		}
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "src", "autable", "schema.ts")), "autablectl workflow sdk") {
		t.Error("schema.ts should name the workflow refresh command")
	}
	env.run(1, "workflow", "init", dir, "--database", "db", "--name", "greet")
	env.run(2, "workflow", "init", filepath.Join(env.dir, "x"), "--database", "db", "--name", "Bad_Name")

	// workflow sdk refreshes the vendored files and drops retired ones.
	writeFiles(t, dir, map[string]string{"src/autable/retired.ts": workflowProject.sdkMarker + " gone\n"})
	env.run(0, "workflow", "sdk", dir)
	if _, err := os.Stat(filepath.Join(dir, "src", "autable", "retired.ts")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a vendored file the server no longer ships must be removed")
	}

	builds := 0
	env.npm = func(buildDir string, _ []string, _ io.Writer, args ...string) error {
		if strings.Join(args, " ") != "run build" {
			return fmt.Errorf("unexpected npm %v", args)
		}
		builds++
		writeFiles(t, buildDir, map[string]string{"dist/workflow.js": testWorkflowBundle})
		return nil
	}
	type pushResult struct {
		Version          int64 `json:"version"`
		PublishedVersion int64 `json:"published_version"`
		SourceFiles      int   `json:"source_files"`
	}
	first := decode[pushResult](t, env.run(0, "workflow", "push", dir, "-m", "first"))
	if first.Version != 1 || first.PublishedVersion != 0 || first.SourceFiles == 0 {
		t.Fatalf("unexpected first push %#v", first)
	}
	second := decode[pushResult](t, env.run(0, "workflow", "push", dir, "--publish"))
	if second.Version != 2 || second.PublishedVersion != 2 || builds != 2 {
		t.Fatalf("unexpected second push %#v", second)
	}

	env.stdin = "tok\n"
	if set := decode[map[string]any](t, env.run(0, "workflow", "secret", "set", "db", "greet", "token")); set["length"] != float64(3) {
		t.Fatalf("unexpected secret output %#v", set)
	}
	env.stdin = ""
	env.run(1, "workflow", "secret", "set", "db", "greet", "empty")
	env.run(2, "workflow", "secret", "set", "db", "greet", "token", "value-on-argv")
	env.run(0, "workflow", "var", "set", "db", "greet", "region", "eu")

	run := decode[map[string]any](t, env.run(0, "workflow", "run", "db", "greet", "--input", `{"name":"Ada"}`, "--wait"))
	if run["status"] != "succeeded" || !strings.Contains(mustJSON(t, run["output"]), `"message":"hello Ada"`) ||
		!strings.Contains(mustJSON(t, run["output"]), `"secret":true`) || !strings.Contains(mustJSON(t, run["output"]), `"region":"eu"`) {
		t.Fatalf("unexpected run %#v", run)
	}
	failedOut, stderr := env.runOutput(1, "workflow", "run", "db", "greet", "--trigger", "start", "--input", `{"name":"fail"}`, "--wait")
	if failed := decode[map[string]any](t, failedOut); failed["status"] != "failed" || !strings.Contains(stderr, "asked to fail") {
		t.Fatalf("unexpected failed run %s / %s", failedOut, stderr)
	}
	runs := decode[[]map[string]any](t, env.run(0, "workflow", "runs", "db", "greet"))
	if len(runs) != 2 {
		t.Fatalf("expected two runs, got %#v", runs)
	}
	if failedRuns := decode[[]map[string]any](t, env.run(0, "workflow", "runs", "db", "greet", "--status", "failed")); len(failedRuns) != 1 {
		t.Fatalf("expected one failed run, got %#v", failedRuns)
	}
	runLog := env.run(0, "workflow", "run-log", "db", "greet", jsonNumber(int64(run["id"].(float64))))
	if !strings.Contains(runLog, "greeting") || !strings.Contains(runLog, "Ada") {
		t.Fatalf("unexpected run log %s", runLog)
	}

	detail := decode[map[string]any](t, env.run(0, "workflow", "versions", "db", "greet"))
	if mustJSON(t, detail["secrets"]) != `["token"]` || mustJSON(t, detail["vars"]) != `{"region":"eu"}` || strings.Contains(mustJSON(t, detail), "tok\"") {
		t.Fatalf("unexpected detail %#v", detail)
	}
	env.run(0, "workflow", "secret", "unset", "db", "greet", "token")
	env.run(0, "workflow", "var", "unset", "db", "greet", "region")
	env.run(0, "workflow", "disable", "db", "greet")
	parsed := decode[workflowDetail](t, env.run(0, "workflow", "versions", "db", "greet"))
	if parsed.Workflow.Enabled || len(parsed.Versions) != 2 {
		t.Fatalf("unexpected detail after disable %#v", parsed)
	}
	detail = decode[map[string]any](t, env.run(0, "workflow", "versions", "db", "greet"))
	if mustJSON(t, detail["secrets"]) != "[]" || mustJSON(t, detail["vars"]) != "{}" {
		t.Fatalf("secrets and vars were not removed: %#v", detail)
	}
	env.run(0, "workflow", "enable", "db", "greet")
	if listed := decode[[]workflowSummary](t, env.run(0, "workflow", "list", "db")); len(listed) != 1 || listed[0].Name != "greet" || !listed[0].Enabled {
		t.Fatalf("unexpected list %#v", listed)
	}

	rolled := decode[map[string]any](t, env.run(0, "workflow", "rollback", "db", "greet"))
	if rolled["published_version"] != float64(1) || rolled["previous_version"] != float64(2) {
		t.Fatalf("unexpected rollback %#v", rolled)
	}
	env.run(1, "workflow", "rollback", "db", "greet")
	env.run(0, "workflow", "publish", "db", "greet", "2")

	pulled := filepath.Join(env.dir, "pulled")
	if out := decode[map[string]any](t, env.run(0, "workflow", "pull", "db", "greet", pulled)); out["version"] != float64(2) {
		t.Fatalf("pull should default to the published version: %#v", out)
	}
	for _, rel := range []string{"autable.workflow.json", "package.json", "src/index.ts", "src/autable/schema.ts", ".gitignore"} {
		if readFile(t, filepath.Join(pulled, filepath.FromSlash(rel))) != readFile(t, filepath.Join(dir, filepath.FromSlash(rel))) {
			t.Errorf("pulled %s differs from the pushed project", rel)
		}
	}

	// A user without a run grant cannot start it; admin-only commands are refused.
	admin := env.token
	env.token = env.memberToken
	env.run(1, "workflow", "run", "db", "greet")
	if _, stderr := env.runOutput(1, "workflow", "versions", "db", "greet"); !strings.Contains(stderr, "403") && !strings.Contains(stderr, "404") {
		t.Fatalf("expected the detail to be refused, got %q", stderr)
	}
	// With a run grant the member sees the workflow and starts its manual
	// trigger; the run acts with the workflow's grants, not the member's.
	env.token = admin
	if err := env.system.SaveGrant(context.Background(), permission.Grant{SubjectID: memberID(t, env), Scope: permission.ScopeWorkflow, Resource: jsonNumber(parsed.Workflow.ID), Level: permission.Run}); err != nil {
		t.Fatal(err)
	}
	env.token = env.memberToken
	if listed := decode[[]map[string]any](t, env.run(0, "workflow", "list", "db")); len(listed) != 1 || listed[0]["name"] != "greet" {
		t.Fatalf("expected the runnable workflow, got %#v", listed)
	}
	memberRun := decode[map[string]any](t, env.run(0, "workflow", "run", "db", "greet", "--input", `{"name":"Bo"}`, "--wait"))
	if memberRun["status"] != "succeeded" || !strings.Contains(mustJSON(t, memberRun["output"]), "hello Bo") {
		t.Fatalf("unexpected member run %#v", memberRun)
	}
	if _, stderr := env.runOutput(1, "workflow", "runs", "db", "greet"); !strings.Contains(stderr, "403") {
		t.Fatalf("run history should stay admin-only, got %q", stderr)
	}
	env.token = admin

	env.run(0, "workflow", "delete", "db", "greet")
	env.run(1, "workflow", "versions", "db", "greet")
}
