package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"autable/internal/runnercli"
	"autable/internal/systemdb"
	"autable/internal/workflow/nodes"
)

const runnerBindingScript = `function instances(info) {
  return {
    remote_echo: "echo",
    upsert: "table.row.upsert",
    sched: "time.schedule"
  };
}
function trigger(info) {
  return { instance: "sched", params: { interval_ms: 3600000 } };
}
function run(info) {
  return info.instance("remote_echo").exec({ value: "Ada" });
}`

func runnerTestSession(t *testing.T, system *systemdb.DB) *http.Cookie {
	t.Helper()
	cookie := testSessionCookie(t, system, "u1")
	if err := system.SaveDatabaseOwner(context.Background(), "db", "u1"); err != nil {
		t.Fatal(err)
	}
	return cookie
}

func saveWorkflowWithRunners(t *testing.T, server *Server, cookie *http.Cookie, runners map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{
		"database_name": "db",
		"name":          "sync",
		"script":        runnerBindingScript,
		"runners":       runners,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/workflows", bytes.NewBuffer(payload))
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func TestRunnersEndpointRequiresAuthentication(t *testing.T) {
	server, _ := newTestServer(t)

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/databases/db/runners", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/databases/db/runners", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", recorder.Code)
	}
}

func TestRunnerTokenManagementIsOwnerOnly(t *testing.T) {
	server, system := newTestServer(t)
	cookie := testSessionCookie(t, system, "outsider")

	listRequest := httptest.NewRequest(http.MethodGet, "/api/databases/db/runners", nil)
	listRequest.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, listRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected runner listing for signed-in users, got %d %s", recorder.Code, recorder.Body.String())
	}
	var listing runnersResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.CanManage || listing.Token != nil {
		t.Fatalf("expected token metadata to be hidden from non-owners, got %#v", listing)
	}

	resetRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/runners", nil)
	resetRequest.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, resetRequest)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-owner reset to be forbidden, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRunnerTokenResetAndListing(t *testing.T) {
	server, system := newTestServer(t)
	cookie := runnerTestSession(t, system)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/databases/db/runners", nil)
	listRequest.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, listRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", recorder.Code, recorder.Body.String())
	}
	var listing runnersResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if !listing.CanManage || listing.Token == nil || listing.Token.Exists || len(listing.Runners) != 0 {
		t.Fatalf("expected empty owner-visible initial state, got %#v", listing)
	}
	remoteTypes := strings.Join(listing.RemoteNodeTypes, ",")
	if !strings.Contains(remoteTypes, "echo") || strings.Contains(remoteTypes, "table.row.upsert") || strings.Contains(remoteTypes, "time.schedule") {
		t.Fatalf("unexpected remote node types: %v", listing.RemoteNodeTypes)
	}

	resetRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/runners", nil)
	resetRequest.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, resetRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d %s", recorder.Code, recorder.Body.String())
	}
	var reset runnerTokenResetResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &reset); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(reset.Token, "atr_") || reset.CreatedAt == 0 {
		t.Fatalf("unexpected reset response: %#v", reset)
	}
	dbName, ok, err := system.LookupRunnerToken(context.Background(), reset.Token)
	if err != nil || !ok || dbName != "db" {
		t.Fatalf("expected returned token to resolve to db, got %q ok=%v err=%v", dbName, ok, err)
	}

	recorder = httptest.NewRecorder()
	listRequest = httptest.NewRequest(http.MethodGet, "/api/databases/db/runners", nil)
	listRequest.AddCookie(cookie)
	server.ServeHTTP(recorder, listRequest)
	if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Token == nil || !listing.Token.Exists || listing.Token.CreatedAt != reset.CreatedAt {
		t.Fatalf("expected token metadata after reset, got %#v", listing.Token)
	}
}

func TestSaveWorkflowValidatesRunnerBindings(t *testing.T) {
	server, system := newTestServer(t)
	cookie := runnerTestSession(t, system)

	recorder := saveWorkflowWithRunners(t, server, cookie, map[string]string{"remote_echo": "intranet"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected valid binding to save, got %d %s", recorder.Code, recorder.Body.String())
	}
	var saved workflowDefinitionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Runners["remote_echo"] != "intranet" {
		t.Fatalf("expected runners in response, got %#v", saved.Runners)
	}

	cases := []struct {
		runners map[string]string
		message string
	}{
		{map[string]string{"upsert": "intranet"}, "cannot execute on a remote runner"},
		{map[string]string{"sched": "intranet"}, "trigger instance"},
		{map[string]string{"missing": "intranet"}, "is not declared"},
		{map[string]string{"remote_echo": ""}, "must not be empty"},
	}
	for _, testCase := range cases {
		recorder := saveWorkflowWithRunners(t, server, cookie, testCase.runners)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected binding %#v to be rejected, got %d %s", testCase.runners, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), testCase.message) {
			t.Fatalf("expected %q in error, got %s", testCase.message, recorder.Body.String())
		}
	}
}

func TestRunWorkflowExecutesOnConnectedRunner(t *testing.T) {
	server, system := newTestServer(t)
	cookie := runnerTestSession(t, system)

	token, err := system.ResetRunnerToken(context.Background(), "db")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	runnerCtx, stopRunner := context.WithCancel(context.Background())
	defer stopRunner()
	go runnercli.Run(runnerCtx, runnercli.Options{
		Endpoint: httpServer.URL,
		Token:    token,
		Name:     "intranet",
	}, nodes.Remote())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := server.runnerHub.NodeTypes("db", "intranet"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, ok := server.runnerHub.NodeTypes("other", "intranet"); ok {
		t.Fatal("expected the runner to be scoped to its token's database")
	}

	recorder := saveWorkflowWithRunners(t, server, cookie, map[string]string{"remote_echo": "intranet"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("save failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var saved workflowDefinitionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}

	runRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/workflows/%d/runs", saved.ID), bytes.NewBufferString(`{"inputs":{}}`))
	runRequest.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, runRequest)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected remote run to succeed, got %d %s", recorder.Code, recorder.Body.String())
	}
	var runResult struct {
		Run struct {
			Outputs map[string]any   `json:"outputs"`
			Steps   []map[string]any `json:"steps"`
		} `json:"run"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &runResult); err != nil {
		t.Fatal(err)
	}
	if runResult.Run.Outputs["value"] != "Ada" {
		t.Fatalf("unexpected run outputs: %#v", runResult.Run.Outputs)
	}
	if len(runResult.Run.Steps) != 1 || runResult.Run.Steps[0]["runner"] != "intranet" {
		t.Fatalf("expected step to record the runner, got %#v", runResult.Run.Steps)
	}
}

func TestRunWorkflowFailsWhenBoundRunnerIsOffline(t *testing.T) {
	server, system := newTestServer(t)
	cookie := runnerTestSession(t, system)

	recorder := saveWorkflowWithRunners(t, server, cookie, map[string]string{"remote_echo": "intranet"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("save failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var saved workflowDefinitionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}

	runRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/workflows/%d/runs", saved.ID), bytes.NewBufferString(`{"inputs":{}}`))
	runRequest.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, runRequest)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected run to fail visibly, got %d %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `runner \"intranet\" is not connected`) {
		t.Fatalf("expected not-connected error, got %s", recorder.Body.String())
	}
}
