package ctl

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"autable/internal/api"
	"autable/internal/auth"
	"autable/internal/config"
	"autable/internal/history"
	"autable/internal/metadata"
	"autable/internal/recorddb"
	"autable/internal/systemdb"
	"autable/internal/table"
)

const testWorkflowScript = "function instances(info) { return { noop: \"echo\" }; }\nfunction run(info) { return { message: \"hello \" + info.inputs.name }; }\n"

type testEnv struct {
	t      *testing.T
	server *httptest.Server
	system *systemdb.DB
	token  string
	dir    string
	// noEnv runs without AUTABLE_SERVER/AUTABLE_TOKEN, using the stored login.
	noEnv bool
	// browser stands in for the user's browser during login.
	browser func(string) error
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	system, err := systemdb.Open(ctx, filepath.Join(t.TempDir(), "system.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = system.Close() })
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "名称", Type: "string"},
				{Name: "status", Type: "string"},
			},
		}},
	}}}
	repository, err := recorddb.OpenCatalog(ctx, catalog, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	historyStore := history.NewMemoryStore()
	tables := table.NewServiceWithRepository(historyStore, repository)
	tables.SetFileBinder(system)
	server := api.NewServerWithAuthConfig(catalog, system, tables, historyStore, config.AuthConfig{
		Password: config.PasswordAuthConfig{Enabled: true},
	})
	metadataPath := filepath.Join(t.TempDir(), "metadata", "main.yml")
	if err := metadata.Save(metadataPath, catalog); err != nil {
		t.Fatal(err)
	}
	server.EnableMetadataWrites(metadataPath)

	user, err := auth.NewPasswordUser(auth.PasswordRegistration{Email: "owner@example.com", DisplayName: "Owner", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	user, err = system.UpsertUserByEmail(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if err := system.SaveDatabaseOwner(ctx, "db", user.ID); err != nil {
		t.Fatal(err)
	}
	session, err := system.CreateSession(ctx, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	return &testEnv{t: t, server: httpServer, system: system, token: session.Token, dir: t.TempDir()}
}

// run executes autablectl and returns stdout; it fails the test on a
// non-zero exit unless wantCode says otherwise.
func (env *testEnv) run(wantCode int, args ...string) string {
	env.t.Helper()
	var stdout, stderr bytes.Buffer
	a := &app{
		stdin:  strings.NewReader(""),
		stdout: &stdout,
		stderr: &stderr,
		getenv: func(name string) string {
			if env.noEnv {
				return ""
			}
			switch name {
			case "AUTABLE_SERVER":
				return env.server.URL
			case "AUTABLE_TOKEN":
				return env.token
			}
			return ""
		},
		configPath: filepath.Join(env.dir, "config.json"),
		httpClient: env.server.Client(),
		openURL:    env.browser,
	}
	if code := a.run(args); code != wantCode {
		env.t.Fatalf("autablectl %s: exit %d, want %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, wantCode, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func decode[T any](t *testing.T, output string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, output)
	}
	return value
}

func TestRowsCommandsRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	if out := env.run(0, "whoami"); !strings.Contains(out, "owner@example.com") {
		t.Fatalf("unexpected whoami: %s", out)
	}

	created := decode[map[string]any](t, env.run(0, "rows", "create", "db", "contacts", "--values", `{"名称":"Widget","status":"open"}`))
	id := int64(created["record_id"].(float64))
	recordID := jsonNumber(id)

	env.run(0, "rows", "update", "db", "contacts", recordID, "--values", `{"status":"closed"}`)
	row := decode[map[string]any](t, env.run(0, "rows", "get", "db", "contacts", recordID))
	values := row["values"].(map[string]any)
	if values["名称"] != "Widget" || values["status"] != "closed" {
		t.Fatalf("unexpected row %#v", row)
	}

	page := decode[struct {
		Rows  []map[string]any `json:"rows"`
		Total int              `json:"total"`
	}](t, env.run(0, "rows", "list", "db", "contacts", "--query", `{"field":"status","operator":"=","value":"closed"}`, "--limit", "10"))
	if page.Total != 1 || len(page.Rows) != 1 {
		t.Fatalf("unexpected page %#v", page)
	}

	env.run(0, "rows", "upsert", "db", "contacts", "--match", "名称", "--values", `{"名称":"Widget","status":"reopened"}`)
	row = decode[map[string]any](t, env.run(0, "rows", "get", "db", "contacts", recordID))
	if row["values"].(map[string]any)["status"] != "reopened" {
		t.Fatalf("upsert did not update the matching row: %#v", row)
	}

	env.run(0, "fields", "add", "db", "contacts", "--fields", `[{"name":"数量","type":"int"}]`)
	table := decode[map[string]any](t, env.run(0, "meta", "db", "contacts"))
	if !strings.Contains(mustJSON(t, table["fields"]), "数量") {
		t.Fatalf("field was not added: %#v", table["fields"])
	}

	env.run(0, "rows", "delete", "db", "contacts", recordID)
	env.run(1, "rows", "get", "db", "contacts", recordID)
}

func TestPullEditPushWorkflow(t *testing.T) {
	env := newTestEnv(t)
	work := filepath.Join(env.dir, "scripts")
	env.run(0, "pull", "--dir", work)

	// A new file under workflow/<database>/ becomes a new workflow.
	path := filepath.Join(work, "workflow", "db", "greet.js")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(testWorkflowScript), 0o644); err != nil {
		t.Fatal(err)
	}
	status := decode[[]change](t, env.run(0, "status", "--dir", work))
	if len(status) != 1 || status[0].Status != "new" || status[0].Database != "db" || status[0].Name != "greet" {
		t.Fatalf("unexpected status %#v", status)
	}
	dry := decode[[]change](t, env.run(0, "push", "--dir", work, "--dry-run"))
	if len(dry) != 1 || dry[0].Status != "would-create" {
		t.Fatalf("unexpected dry run %#v", dry)
	}
	pushed := decode[[]change](t, env.run(0, "push", "--dir", work))
	if len(pushed) != 1 || pushed[0].Status != "created" || pushed[0].ID == 0 {
		t.Fatalf("unexpected push %#v", pushed)
	}
	if status := decode[[]change](t, env.run(0, "status", "--dir", work)); len(status) != 0 {
		t.Fatalf("expected a clean status after push, got %#v", status)
	}

	// Settings changed through the CLI survive a later script push.
	env.run(0, "workflow", "set", "db", "greet", "--var", "suffix=!", "--timeout", "30")
	edited := strings.Replace(testWorkflowScript, "hello ", "hi ", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	// The settings change bumped updated_at on the server: push refuses.
	conflict := decode[[]change](t, env.run(1, "push", "--dir", work))
	if len(conflict) != 1 || conflict[0].Status != "failed" || !strings.Contains(conflict[0].Detail, "changed on the server") {
		t.Fatalf("expected a conflict, got %#v", conflict)
	}
	// Pull keeps the local edit; the conflict stands until it is merged and
	// pushed with --force.
	pulled := decode[[]pullResult](t, env.run(0, "pull", "--dir", work))
	if len(pulled) != 1 || pulled[0].Action != "kept" {
		t.Fatalf("expected pull to keep the edit, got %#v", pulled)
	}
	env.run(1, "push", "--dir", work)
	env.run(0, "push", "--dir", work, "--force")

	workflow := decode[map[string]any](t, env.run(0, "workflow", "get", "db", "greet"))
	if workflow["variables"].(map[string]any)["suffix"] != "!" || workflow["timeout_seconds"].(float64) != 30 {
		t.Fatalf("settings were lost by the script push: %#v", workflow)
	}
	if script := env.run(0, "workflow", "get", "db", "greet", "--script"); script != edited {
		t.Fatalf("unexpected script %q", script)
	}

	run := decode[map[string]any](t, env.run(0, "workflow", "run", "db", "greet", "--inputs", `{"name":"Ada"}`))
	if !strings.Contains(mustJSON(t, run["run"]), "hi Ada") {
		t.Fatalf("unexpected run %#v", run)
	}
	runs := decode[[]map[string]any](t, env.run(0, "workflow", "runs", "db", "greet"))
	if len(runs) != 1 {
		t.Fatalf("expected one run, got %#v", runs)
	}
	detail := env.run(0, "workflow", "run-log", "db", "greet", runs[0]["history_key"].(string))
	if !strings.Contains(detail, "hi Ada") {
		t.Fatalf("unexpected run log %s", detail)
	}
}

func TestPullDoesNotOverwriteLocalEdits(t *testing.T) {
	env := newTestEnv(t)
	work := filepath.Join(env.dir, "scripts")
	env.run(0, "pull", "--dir", work)
	path := filepath.Join(work, "workflow", "db", "greet.js")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(testWorkflowScript), 0o644); err != nil {
		t.Fatal(err)
	}
	env.run(0, "push", "--dir", work)

	if err := os.WriteFile(path, []byte("// local draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.run(0, "pull", "--dir", work)
	if data, _ := os.ReadFile(path); string(data) != "// local draft\n" {
		t.Fatalf("pull overwrote a local edit: %q", data)
	}
	env.run(0, "pull", "--dir", work, "--force")
	if data, _ := os.ReadFile(path); string(data) != testWorkflowScript {
		t.Fatalf("pull --force did not restore the server script: %q", data)
	}
}

func TestBrowserLoginStoresToken(t *testing.T) {
	env := newTestEnv(t)
	env.noEnv = true
	env.browser = func(target string) error {
		// Approve as the signed-in browser user, then follow the redirect
		// back to the CLI's loopback listener.
		authorize, err := url.Parse(target)
		if err != nil {
			return err
		}
		request, err := http.NewRequest(http.MethodPost, env.server.URL+authorize.Path, strings.NewReader(authorize.Query().Encode()))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(&http.Cookie{Name: "autable_session", Value: env.token})
		noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := noRedirect.Do(request)
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode != http.StatusSeeOther {
			return fmt.Errorf("authorize returned %d", response.StatusCode)
		}
		go func() {
			if callback, err := http.Get(response.Header.Get("Location")); err == nil {
				callback.Body.Close()
			}
		}()
		return nil
	}

	env.run(1, "whoami")
	login := decode[map[string]any](t, env.run(0, "login", "--server", env.server.URL))
	if login["email"] != "owner@example.com" {
		t.Fatalf("unexpected login %#v", login)
	}
	if out := env.run(0, "whoami"); !strings.Contains(out, "owner@example.com") {
		t.Fatalf("stored token did not authenticate: %s", out)
	}
	env.run(0, "logout")
	env.run(1, "whoami")
}

func TestUnauthenticatedAndUsageErrors(t *testing.T) {
	env := newTestEnv(t)
	env.token = "not-a-session"
	env.run(1, "whoami")
	env.run(2, "rows", "get", "db", "contacts")
	env.run(2, "rows", "get", "db", "contacts", "abc")
	env.run(2, "no-such-command")
}

func TestParseFlagsAcceptsInterspersedFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	limit := fs.Int("limit", 0, "")
	values, err := parseFlags(fs, []string{"db", "--limit", "5", "contacts"}, "DATABASE", "TABLE")
	if err != nil {
		t.Fatal(err)
	}
	if *limit != 5 || strings.Join(values, ",") != "db,contacts" {
		t.Fatalf("got limit %d values %v", *limit, values)
	}
	fs = flag.NewFlagSet("test", flag.ContinueOnError)
	if _, err := parseFlags(fs, []string{"db"}, "DATABASE", "TABLE?"); err != nil {
		t.Fatalf("optional argument rejected: %v", err)
	}
	if _, err := parseFlags(fs, []string{}, "DATABASE", "TABLE?"); err == nil {
		t.Fatal("expected a missing required argument to fail")
	}
}

func TestNewFileChangeResolvesDatabaseAndName(t *testing.T) {
	got := newFileChange("form/名称-db/订单.js", kindForm, "", []string{"other", "名称 db"})
	if got.Status != "new" || got.Database != "名称 db" || got.Name != "订单" {
		t.Fatalf("unexpected change %#v", got)
	}
	if got := newFileChange("workflow/missing/a.js", kindWorkflow, "", []string{"db"}); got.Status != "invalid" {
		t.Fatalf("expected unknown database to be invalid, got %#v", got)
	}
	if got := newFileChange("workflow/db/-a.js", kindWorkflow, "", []string{"db"}); got.Status != "invalid" {
		t.Fatalf("expected an unmappable name to be invalid, got %#v", got)
	}
}

func jsonNumber(value int64) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
