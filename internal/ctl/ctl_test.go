package ctl

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

type testEnv struct {
	t      *testing.T
	server *httptest.Server
	system *systemdb.DB
	token  string
	// memberToken signs in a user who is not an admin.
	memberToken string
	dir         string
	// noEnv runs without AUTABLE_SERVER/AUTABLE_TOKEN, using the stored login.
	noEnv bool
	// browser stands in for the user's browser during login.
	browser func(string) error
	// stdin is what the command reads on stdin.
	stdin string
	// npm stands in for running npm.
	npm func(dir string, env []string, out io.Writer, args ...string) error
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
		Admins:   []string{"admin@example.com"},
		Password: config.PasswordAuthConfig{Enabled: true},
	})
	metadataPath := filepath.Join(t.TempDir(), "metadata", "main.yml")
	if err := metadata.Save(metadataPath, catalog); err != nil {
		t.Fatal(err)
	}
	server.EnableMetadataWrites(metadataPath)
	workflowCtx, stopWorkflows := context.WithCancel(ctx)
	if err := server.StartWorkflows(workflowCtx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopWorkflows()
		server.WaitWorkflows()
	})

	user, err := auth.NewPasswordUser(auth.PasswordRegistration{Email: "admin@example.com", DisplayName: "Admin", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	user, err = system.UpsertUserByEmail(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	session, err := system.CreateSession(ctx, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	member, err := auth.NewPasswordUser(auth.PasswordRegistration{Email: "member@example.com", DisplayName: "Member", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	member, err = system.UpsertUserByEmail(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	memberSession, err := system.CreateSession(ctx, member.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	return &testEnv{t: t, server: httpServer, system: system, token: session.Token, memberToken: memberSession.Token, dir: t.TempDir()}
}

// run executes autablectl and returns stdout; it fails the test on a
// non-zero exit unless wantCode says otherwise.
func (env *testEnv) run(wantCode int, args ...string) string {
	env.t.Helper()
	stdout, _ := env.runOutput(wantCode, args...)
	return stdout
}

// runOutput is run that also returns stderr.
func (env *testEnv) runOutput(wantCode int, args ...string) (string, string) {
	env.t.Helper()
	var stdout, stderr bytes.Buffer
	a := &app{
		stdin:  strings.NewReader(env.stdin),
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
		runNPM:     env.npm,
		pollEvery:  10 * time.Millisecond,
	}
	if code := a.run(args); code != wantCode {
		env.t.Fatalf("autablectl %s: exit %d, want %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, wantCode, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
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
	if me := decode[map[string]any](t, env.run(0, "whoami")); me["email"] != "admin@example.com" || me["is_admin"] != true {
		t.Fatalf("unexpected whoami: %#v", me)
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
	if login["email"] != "admin@example.com" {
		t.Fatalf("unexpected login %#v", login)
	}
	if out := env.run(0, "whoami"); !strings.Contains(out, "admin@example.com") {
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

func TestNonAdminSeesServerRefusals(t *testing.T) {
	env := newTestEnv(t)
	env.token = env.memberToken
	if me := decode[map[string]any](t, env.run(0, "whoami")); me["email"] != "member@example.com" || me["is_admin"] != false {
		t.Fatalf("unexpected whoami: %#v", me)
	}
	// Admin-only commands report the server's refusal.
	_, stderr := env.runOutput(1, "rows", "create", "db", "contacts", "--values", `{"status":"open"}`)
	if !strings.Contains(stderr, "403") {
		t.Fatalf("expected a 403 for a write without grants, got %q", stderr)
	}
	if _, stderr := env.runOutput(1, "fields", "add", "db", "contacts", "--fields", `[{"name":"数量","type":"int"}]`); !strings.Contains(stderr, "403") {
		t.Fatalf("expected adding a field to need an admin, got %q", stderr)
	}
}

func memberID(t *testing.T, env *testEnv) string {
	t.Helper()
	user, err := env.system.UserByEmail(context.Background(), "member@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}
