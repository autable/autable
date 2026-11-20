package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"autable/internal/appbundle"
	"autable/internal/config"
	"autable/internal/history"
	"autable/internal/metadata"
	"autable/internal/permission"
	"autable/internal/recorddb"
	"autable/internal/runnercli"
	"autable/internal/systemdb"
	"autable/internal/table"
	"autable/internal/wfsdk"
)

// Workflow test bundles are plain JavaScript against the host API
// (docs/design/workflows-host-api.md), the shape the SDK compiles to.

const manualOnlyTriggers = `{ start: { kind: "manual", fields: [{ name: "n", type: "number" }] } }`

func workflowScript(triggers, run string) string {
	return "var __workflow = { triggers: " + triggers + ", run: " + run + " };"
}

func workflowTestBundle(t *testing.T, database, name, sdk, script string) []byte {
	t.Helper()
	manifest, err := json.Marshal(appbundle.Manifest{Database: database, Name: name, Title: "Sync " + name, SDK: sdk})
	if err != nil {
		t.Fatal(err)
	}
	return testTarGz(t, []testTarEntry{
		{name: appbundle.WorkflowManifestName, body: string(manifest)},
		{name: "dist/workflow.js", body: script},
		{name: "source/" + appbundle.WorkflowManifestName, body: string(manifest)},
		{name: "source/src/index.ts", body: "// source of " + name},
	})
}

func pushTestWorkflow(t *testing.T, server *Server, system *systemdb.DB, userID, name, script, query string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/databases/db/workflows/" + name + "/versions"
	if query != "" {
		target += "?" + query
	}
	return appTestRequest(t, server, system, userID, http.MethodPost, target, bytes.NewReader(workflowTestBundle(t, "db", name, wfsdk.Version(), script)))
}

type testWorkflowUpload struct {
	Workflow workflowResponse        `json:"workflow"`
	Version  workflowVersionResponse `json:"version"`
}

// deployTestWorkflow pushes and publishes a workflow as the admin "admin".
func deployTestWorkflow(t *testing.T, server *Server, system *systemdb.DB, name, script string) systemdb.Workflow {
	t.Helper()
	makeTestAdmins(server, "admin")
	recorder := pushTestWorkflow(t, server, system, "admin", name, script, "publish=1")
	if recorder.Code != http.StatusCreated && recorder.Code != http.StatusOK {
		t.Fatalf("deploy %s: %d %s", name, recorder.Code, recorder.Body.String())
	}
	return decodeTestJSON[testWorkflowUpload](t, recorder).Workflow.Workflow
}

func startTestWorkflows(t *testing.T, server *Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := server.StartWorkflows(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		server.WaitWorkflows()
	})
}

func jsonBody(value string) *bytes.Reader {
	return bytes.NewReader([]byte(value))
}

func startTestRun(t *testing.T, server *Server, system *systemdb.DB, userID, name, body string) *httptest.ResponseRecorder {
	t.Helper()
	return appTestRequest(t, server, system, userID, http.MethodPost, "/api/databases/db/workflows/"+name+"/runs", jsonBody(body))
}

func finished(status string) bool {
	return status == systemdb.WorkflowRunSucceeded || status == systemdb.WorkflowRunFailed || status == systemdb.WorkflowRunCancelled
}

func waitTestRun(t *testing.T, system *systemdb.DB, workflowID, runID int64) systemdb.WorkflowRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err := system.WorkflowRun(context.Background(), workflowID, runID)
		if err != nil {
			t.Fatal(err)
		}
		if finished(run.Status) {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %d never finished", runID)
	return systemdb.WorkflowRun{}
}

// waitTestRuns waits until the workflow has count finished runs and
// returns them newest first.
func waitTestRuns(t *testing.T, system *systemdb.DB, workflowID int64, count int) []systemdb.WorkflowRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		runs, err := system.WorkflowRuns(context.Background(), workflowID, systemdb.WorkflowRunFilter{})
		if err != nil {
			t.Fatal(err)
		}
		done := 0
		for _, run := range runs {
			if finished(run.Status) {
				done++
			}
		}
		if done >= count && done == len(runs) {
			if len(runs) != count {
				t.Fatalf("expected %d runs, got %d: %#v", count, len(runs), runs)
			}
			return runs
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d finished runs, got %#v", count, runs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func outputOf(t *testing.T, run systemdb.WorkflowRun) map[string]any {
	t.Helper()
	if run.Status != systemdb.WorkflowRunSucceeded {
		t.Fatalf("expected run %d to succeed, got %s: %s", run.ID, run.Status, run.Error)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(run.Output), &output); err != nil {
		t.Fatalf("run %d output %q: %v", run.ID, run.Output, err)
	}
	return output
}

func TestWorkflowUploadPublishSettingsAndDelete(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	script := workflowScript(`{
		start: { kind: "manual" },
		hook: { kind: "webhook", path: "inbound" },
		daily: { kind: "schedule", cron: "0 9 * * 1-5", timezone: "Asia/Shanghai" },
		changed: { kind: "recordChanged", table: "contacts", operations: ["update"], fields: ["status"] },
	}`, `function () { return 1; }`)

	first := pushTestWorkflow(t, server, system, "admin", "sync", script, "message=first")
	if first.Code != http.StatusCreated {
		t.Fatalf("expected 201 creating the workflow, got %d %s", first.Code, first.Body.String())
	}
	created := decodeTestJSON[testWorkflowUpload](t, first)
	if created.Workflow.PublishedVersion != 0 || created.Version.Version != 1 || created.Version.Message != "first" || !created.Version.Supported || len(created.Workflow.Triggers) != 0 {
		t.Fatalf("unexpected first upload %#v", created)
	}
	if !created.Workflow.Enabled || created.Workflow.TimeoutSeconds != 600 || created.Workflow.HistoryRetentionDays != 30 || created.Workflow.Title != "Sync sync" {
		t.Fatalf("unexpected defaults %#v", created.Workflow)
	}

	publish := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/databases/db/workflows/sync/publish", jsonBody(`{"version":1}`))
	if publish.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", publish.Code, publish.Body.String())
	}
	published := decodeTestJSON[workflowResponse](t, publish)
	if published.PublishedVersion != 1 || len(published.Triggers) != 4 {
		t.Fatalf("unexpected published workflow %#v", published)
	}
	byName := map[string]map[string]any{}
	for _, trigger := range published.Triggers {
		byName[trigger["name"].(string)] = trigger
	}
	if byName["hook"]["url"] != "/api/hooks/db/sync/inbound" || byName["daily"]["timezone"] != "Asia/Shanghai" || byName["changed"]["fields"].([]any)[0] != "status" || byName["start"]["kind"] != "manual" {
		t.Fatalf("unexpected triggers %#v", byName)
	}

	second := pushTestWorkflow(t, server, system, "admin", "sync", workflowScript(manualOnlyTriggers, `function () { return 2; }`), "publish=1")
	if second.Code != http.StatusOK {
		t.Fatalf("second push: %d %s", second.Code, second.Body.String())
	}
	if upload := decodeTestJSON[testWorkflowUpload](t, second); upload.Workflow.PublishedVersion != 2 || len(upload.Workflow.Triggers) != 1 {
		t.Fatalf("expected the second version published with its triggers, got %#v", upload.Workflow)
	}

	// Invalid triggers fail publishing with 422 and change nothing.
	broken := pushTestWorkflow(t, server, system, "admin", "sync", workflowScript(`{ t: { kind: "schedule", cron: "61 * * * *" } }`, `function () {}`), "publish=1")
	if broken.Code != http.StatusUnprocessableEntity || !strings.Contains(broken.Body.String(), "outside 0-59") {
		t.Fatalf("expected invalid triggers to be rejected with 422, got %d %s", broken.Code, broken.Body.String())
	}
	unknownTable := pushTestWorkflow(t, server, system, "admin", "sync", workflowScript(`{ t: { kind: "recordChanged", table: "missing" } }`, `function () {}`), "")
	if unknownTable.Code != http.StatusOK {
		t.Fatalf("expected an unpublished upload to be stored, got %d %s", unknownTable.Code, unknownTable.Body.String())
	}
	if publish := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/databases/db/workflows/sync/publish", jsonBody(`{"version":3}`)); publish.Code != http.StatusUnprocessableEntity || !strings.Contains(publish.Body.String(), "does not exist") {
		t.Fatalf("expected publishing invalid triggers to fail with 422, got %d %s", publish.Code, publish.Body.String())
	}
	noRun := pushTestWorkflow(t, server, system, "admin", "sync", `var __workflow = { triggers: {} };`, "publish=1")
	if noRun.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected a bundle without run to be rejected, got %d %s", noRun.Code, noRun.Body.String())
	}
	syntax := pushTestWorkflow(t, server, system, "admin", "sync", `var __workflow = {`, "")
	if syntax.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected a bundle that does not compile to be rejected, got %d %s", syntax.Code, syntax.Body.String())
	}
	unsupported := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/databases/db/workflows/sync/versions", bytes.NewReader(workflowTestBundle(t, "db", "sync", "99.0", script)))
	if unsupported.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected an unsupported SDK to be rejected, got %d %s", unsupported.Code, unsupported.Body.String())
	}
	mismatch := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/databases/db/workflows/sync/versions", bytes.NewReader(workflowTestBundle(t, "db", "other", wfsdk.Version(), script)))
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("expected a manifest mismatch to be rejected, got %d %s", mismatch.Code, mismatch.Body.String())
	}
	appBundle := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/databases/db/workflows/sync/versions", bytes.NewReader(testAppBundle(t, "db", "sync", wfsdk.Version(), "x")))
	if appBundle.Code != http.StatusBadRequest {
		t.Fatalf("expected an app bundle to be rejected, got %d %s", appBundle.Code, appBundle.Body.String())
	}
	missingDB := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/databases/missing/workflows/sync/versions", bytes.NewReader(workflowTestBundle(t, "missing", "sync", wfsdk.Version(), script)))
	if missingDB.Code != http.StatusNotFound {
		t.Fatalf("expected an unknown database to 404, got %d %s", missingDB.Code, missingDB.Body.String())
	}

	// Roll back to version 1.
	if rollback := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/databases/db/workflows/sync/publish", jsonBody(`{"version":1}`)); rollback.Code != http.StatusOK {
		t.Fatalf("rollback: %d %s", rollback.Code, rollback.Body.String())
	}

	// Settings, secrets and vars.
	patch := appTestRequest(t, server, system, "admin", http.MethodPatch, "/api/databases/db/workflows/sync", jsonBody(`{"enabled":false,"timeout_seconds":30,"history_retention_days":7}`))
	if patch.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", patch.Code, patch.Body.String())
	}
	if updated := decodeTestJSON[workflowResponse](t, patch); updated.Enabled || updated.TimeoutSeconds != 30 || updated.HistoryRetentionDays != 7 {
		t.Fatalf("unexpected settings %#v", updated)
	}
	for _, body := range []string{`{"timeout_seconds":0}`, `{"history_retention_days":0}`, `{"unknown":1}`} {
		if bad := appTestRequest(t, server, system, "admin", http.MethodPatch, "/api/databases/db/workflows/sync", jsonBody(body)); bad.Code != http.StatusBadRequest {
			t.Fatalf("expected %s to be rejected, got %d %s", body, bad.Code, bad.Body.String())
		}
	}
	for _, request := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/databases/db/workflows/sync/secrets/api_key", `{"value":"s3cret-value"}`},
		{http.MethodPut, "/api/databases/db/workflows/sync/secrets/other", `{"value":"x"}`},
		{http.MethodDelete, "/api/databases/db/workflows/sync/secrets/other", ""},
		{http.MethodPut, "/api/databases/db/workflows/sync/vars/channel", `{"value":"ops"}`},
	} {
		if recorder := appTestRequest(t, server, system, "admin", request.method, request.path, jsonBody(request.body)); recorder.Code != http.StatusNoContent {
			t.Fatalf("%s %s: %d %s", request.method, request.path, recorder.Code, recorder.Body.String())
		}
	}
	if missing := appTestRequest(t, server, system, "admin", http.MethodDelete, "/api/databases/db/workflows/sync/vars/missing", nil); missing.Code != http.StatusNotFound {
		t.Fatalf("expected removing a missing var to 404, got %d", missing.Code)
	}
	if bad := appTestRequest(t, server, system, "admin", http.MethodPut, "/api/databases/db/workflows/sync/vars/bad%20name", jsonBody(`{"value":"x"}`)); bad.Code != http.StatusBadRequest {
		t.Fatalf("expected an invalid var name to be rejected, got %d", bad.Code)
	}

	detail := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/workflows/sync", nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body.String())
	}
	if strings.Contains(detail.Body.String(), "s3cret-value") {
		t.Fatal("secret values must never be returned")
	}
	response := decodeTestJSON[workflowDetailResponse](t, detail)
	if len(response.Versions) != 3 || response.Versions[2].Version != 1 || !response.Versions[2].Published || response.Versions[0].Published {
		t.Fatalf("unexpected versions %#v", response.Versions)
	}
	if strings.Join(response.Secrets, ",") != "api_key" || response.Vars["channel"] != "ops" || response.SDK.Version != wfsdk.Version() {
		t.Fatalf("unexpected secrets/vars/sdk %#v %#v %#v", response.Secrets, response.Vars, response.SDK)
	}

	list := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/workflows", nil)
	if workflows := decodeTestJSON[[]workflowResponse](t, list); len(workflows) != 1 || workflows[0].Name != "sync" || workflows[0].PermissionLevel != permission.Write {
		t.Fatalf("unexpected admin list %#v", workflows)
	}

	source := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/workflows/sync/versions/1/source", nil)
	if source.Code != http.StatusOK {
		t.Fatalf("source: %d", source.Code)
	}
	if files := readTestTarGz(t, source.Body.Bytes()); files["src/index.ts"] != "// source of sync" || files[appbundle.WorkflowManifestName] == "" {
		t.Fatalf("unexpected source %#v", files)
	}

	sdk := appTestRequest(t, server, system, "viewer", http.MethodGet, "/api/workflows/sdk", nil)
	if sdkResponse := decodeTestJSON[map[string]any](t, sdk); sdkResponse["version"] != wfsdk.Version() || sdkResponse["files"].(map[string]any)["index.ts"] == nil {
		t.Fatalf("unexpected sdk response %#v", sdkResponse)
	}

	workflow, err := system.WorkflowByName(context.Background(), "db", "sync")
	if err != nil {
		t.Fatal(err)
	}
	saveTestGrants(t, system, permission.Grant{SubjectID: systemdb.WorkflowSubjectID(workflow.ID), Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll})
	if deleted := appTestRequest(t, server, system, "admin", http.MethodDelete, "/api/databases/db/workflows/sync", nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body.String())
	}
	if gone := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/workflows/sync", nil); gone.Code != http.StatusNotFound {
		t.Fatalf("expected the deleted workflow to 404, got %d", gone.Code)
	}
	if grants, _ := system.GrantListForSubject(context.Background(), systemdb.WorkflowSubjectID(workflow.ID)); len(grants) != 0 {
		t.Fatalf("expected the subject's grants to go with the workflow, got %#v", grants)
	}
}

func TestWorkflowManagementIsAdminOnly(t *testing.T) {
	server, system := newTestServer(t)
	workflow := deployTestWorkflow(t, server, system, "sync", workflowScript(manualOnlyTriggers, `function () {}`))
	// A run grant and data grants give no access to code or configuration.
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "runner", Scope: permission.ScopeWorkflow, Resource: resourceID(workflow.ID), Level: permission.Run},
		permission.Grant{SubjectID: "runner", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll},
	)
	for _, userID := range []string{"viewer", "runner"} {
		requests := []struct {
			method, path string
			body         []byte
		}{
			{http.MethodPost, "/api/databases/db/workflows/sync/versions", workflowTestBundle(t, "db", "sync", wfsdk.Version(), workflowScript(manualOnlyTriggers, `function () {}`))},
			{http.MethodPost, "/api/databases/db/workflows/created/versions", workflowTestBundle(t, "db", "created", wfsdk.Version(), workflowScript(manualOnlyTriggers, `function () {}`))},
			{http.MethodPost, "/api/databases/db/workflows/sync/publish", []byte(`{"version":1}`)},
			{http.MethodGet, "/api/databases/db/workflows/sync", nil},
			{http.MethodPatch, "/api/databases/db/workflows/sync", []byte(`{"enabled":false}`)},
			{http.MethodPut, "/api/databases/db/workflows/sync/secrets/k", []byte(`{"value":"v"}`)},
			{http.MethodDelete, "/api/databases/db/workflows/sync/secrets/k", nil},
			{http.MethodPut, "/api/databases/db/workflows/sync/vars/k", []byte(`{"value":"v"}`)},
			{http.MethodGet, "/api/databases/db/workflows/sync/versions/1/source", nil},
			{http.MethodGet, "/api/databases/db/workflows/sync/runs", nil},
			{http.MethodPost, "/api/databases/db/workflows/sync/runs/1/cancel", nil},
			{http.MethodDelete, "/api/databases/db/workflows/sync", nil},
		}
		for _, request := range requests {
			recorder := appTestRequest(t, server, system, userID, request.method, request.path, bytes.NewReader(request.body))
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("%s: expected %s %s to be 403, got %d %s", userID, request.method, request.path, recorder.Code, recorder.Body.String())
			}
		}
	}
	if anonymous := appTestRequest(t, server, system, "", http.MethodGet, "/api/databases/db/workflows/sync", nil); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous access to be 401, got %d", anonymous.Code)
	}
	if _, err := system.WorkflowByName(context.Background(), "db", "created"); err == nil {
		t.Fatal("a non-admin push must not create a workflow")
	}
}

func TestRunGrantStartsManualRunsAndSeesOnlyOwnRuns(t *testing.T) {
	server, system := newTestServer(t)
	startTestWorkflows(t, server)
	workflow := deployTestWorkflow(t, server, system, "sync", workflowScript(`{
		start: { kind: "manual", fields: [{ name: "n", label: "Number", type: "number" }] },
		other: { kind: "manual" },
		hook: { kind: "webhook", path: "in" },
	}`, `async function (event) {
		console.log("secret detail");
		return { trigger: event.trigger, doubled: (event.input.n || 0) * 2, by: event.startedBy ? event.startedBy.email : null };
	}`))
	hidden := deployTestWorkflow(t, server, system, "hidden", workflowScript(`{ hook: { kind: "webhook", path: "x" } }`, `function () {}`))
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "runner", Scope: permission.ScopeWorkflow, Resource: resourceID(workflow.ID), Level: permission.Run},
		permission.Grant{SubjectID: "runner", Scope: permission.ScopeWorkflow, Resource: resourceID(hidden.ID), Level: permission.Run},
		permission.Grant{SubjectID: "peer", Scope: permission.ScopeWorkflow, Resource: resourceID(workflow.ID), Level: permission.Run},
	)

	// The list shows runnable workflows with manual triggers only.
	list := appTestRequest(t, server, system, "runner", http.MethodGet, "/api/databases/db/workflows", nil)
	runnable := decodeTestJSON[[]runnableWorkflowResponse](t, list)
	if len(runnable) != 1 || runnable[0].Name != "sync" || len(runnable[0].Triggers) != 2 || runnable[0].PermissionLevel != permission.Read {
		t.Fatalf("unexpected runnable list %#v", runnable)
	}
	if strings.Contains(list.Body.String(), `"webhook"`) {
		t.Fatalf("expected only manual triggers in the list, got %s", list.Body.String())
	}
	if viewer := decodeTestJSON[[]runnableWorkflowResponse](t, appTestRequest(t, server, system, "viewer", http.MethodGet, "/api/databases/db/workflows", nil)); len(viewer) != 0 {
		t.Fatalf("expected nothing for users without a grant, got %#v", viewer)
	}

	if denied := startTestRun(t, server, system, "viewer", "sync", `{"trigger":"start"}`); denied.Code != http.StatusForbidden {
		t.Fatalf("expected a run without grant to be 403, got %d", denied.Code)
	}
	if ambiguous := startTestRun(t, server, system, "runner", "sync", `{}`); ambiguous.Code != http.StatusBadRequest {
		t.Fatalf("expected an ambiguous manual trigger to be 400, got %d %s", ambiguous.Code, ambiguous.Body.String())
	}
	if webhook := startTestRun(t, server, system, "runner", "sync", `{"trigger":"hook"}`); webhook.Code != http.StatusBadRequest {
		t.Fatalf("expected starting a webhook trigger to be 400, got %d", webhook.Code)
	}
	if typed := startTestRun(t, server, system, "runner", "sync", `{"trigger":"start","input":{"n":"two"}}`); typed.Code != http.StatusBadRequest || !strings.Contains(typed.Body.String(), "must be a number") {
		t.Fatalf("expected a wrongly typed input to be 400, got %d %s", typed.Code, typed.Body.String())
	}

	waited := startTestRun(t, server, system, "runner", "sync", `{"trigger":"start","input":{"n":21},"wait":true}`)
	if waited.Code != http.StatusOK {
		t.Fatalf("expected a waited run to be 200, got %d %s", waited.Code, waited.Body.String())
	}
	if strings.Contains(waited.Body.String(), "secret detail") || strings.Contains(waited.Body.String(), `"event"`) || strings.Contains(waited.Body.String(), `"logs"`) {
		t.Fatalf("expected the user view to hide event and logs, got %s", waited.Body.String())
	}
	userRun := decodeTestJSON[map[string]any](t, waited)
	output := userRun["output"].(map[string]any)
	if userRun["status"] != systemdb.WorkflowRunSucceeded || output["doubled"] != 42.0 || output["trigger"] != "start" || output["by"] != "runner@example.com" {
		t.Fatalf("unexpected user run %#v", userRun)
	}
	runPath := "/api/databases/db/workflows/sync/runs/" + jsonNumber(userRun["id"])
	if own := appTestRequest(t, server, system, "runner", http.MethodGet, runPath, nil); own.Code != http.StatusOK || strings.Contains(own.Body.String(), `"logs"`) {
		t.Fatalf("expected the starter to read their own run without logs, got %d %s", own.Code, own.Body.String())
	}
	if peer := appTestRequest(t, server, system, "peer", http.MethodGet, runPath, nil); peer.Code != http.StatusNotFound {
		t.Fatalf("expected another user's run to be 404, got %d", peer.Code)
	}
	if list := appTestRequest(t, server, system, "runner", http.MethodGet, "/api/databases/db/workflows/sync/runs", nil); list.Code != http.StatusForbidden {
		t.Fatalf("expected the run list to be admin-only, got %d", list.Code)
	}

	admin := appTestRequest(t, server, system, "admin", http.MethodGet, runPath, nil)
	adminRun := decodeTestJSON[workflowRunView](t, admin)
	if len(adminRun.Logs) != 1 || adminRun.Logs[0].Message != "secret detail" || !strings.Contains(string(adminRun.Event), `"n":21`) || adminRun.StartedBy != "runner" || adminRun.Version != 1 {
		t.Fatalf("unexpected admin run view %#v", adminRun)
	}
	runs := decodeTestJSON[[]workflowRunListedView](t, appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/workflows/sync/runs?status=succeeded&limit=10", nil))
	if len(runs) != 1 || runs[0].ID != adminRun.ID {
		t.Fatalf("unexpected run list %#v", runs)
	}

	// Without waiting the run is queued and answered with 201.
	queued := startTestRun(t, server, system, "runner", "sync", `{"trigger":"other"}`)
	if queued.Code != http.StatusCreated {
		t.Fatalf("expected 201 for a queued run, got %d %s", queued.Code, queued.Body.String())
	}
	waitTestRun(t, system, workflow.ID, int64(decodeTestJSON[map[string]any](t, queued)["id"].(float64)))

	// Disabling stops granted users, not admins.
	if patch := appTestRequest(t, server, system, "admin", http.MethodPatch, "/api/databases/db/workflows/sync", jsonBody(`{"enabled":false}`)); patch.Code != http.StatusOK {
		t.Fatal(patch.Body.String())
	}
	if disabled := startTestRun(t, server, system, "runner", "sync", `{"trigger":"start"}`); disabled.Code != http.StatusConflict {
		t.Fatalf("expected a disabled workflow to be 409 for granted users, got %d", disabled.Code)
	}
	if adminRun := startTestRun(t, server, system, "admin", "sync", `{"trigger":"start","wait":true}`); adminRun.Code != http.StatusOK {
		t.Fatalf("expected admins to run disabled workflows, got %d %s", adminRun.Code, adminRun.Body.String())
	}
	if list := decodeTestJSON[[]runnableWorkflowResponse](t, appTestRequest(t, server, system, "runner", http.MethodGet, "/api/databases/db/workflows", nil)); len(list) != 0 {
		t.Fatalf("expected disabled workflows to leave the runnable list, got %#v", list)
	}
}

func jsonNumber(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestWorkflowTablesRunAsTheWorkflowSubject(t *testing.T) {
	server, system := newTestServer(t)
	startTestWorkflows(t, server)
	workflow := deployTestWorkflow(t, server, system, "writer", workflowScript(manualOnlyTriggers, `async function () {
		const h = __autable_host;
		const created = await h.table("create", "contacts", { values: { name: "Ada", status: "new" } });
		const updated = await h.table("update", "contacts", { record_id: created.record_id, values: { status: "active" } });
		const upserted = await h.table("upsert", "contacts", { match_field: "name", values: { name: "Ada", status: "active" } });
		const rows = await h.table("list", "contacts", { view: "active", query: { combinator: "and", rules: [{ field: "name", operator: "=", value: "Ada" }] } });
		const page = await h.table("page", "contacts", { limit: 1 });
		const changes = await h.table("history", "contacts", { record_id: created.record_id });
		let denied = "";
		try { await h.table("delete", "contacts", { record_id: created.record_id }); } catch (error) { denied = error.name; }
		let missing = "";
		try { await h.table("update", "contacts", { record_id: 999, values: { status: "x" } }); } catch (error) { missing = error.name; }
		const user = await h.users("get", { id: "admin" });
		const nobody = await h.users("byEmail", { email: "nobody@example.com" });
		return { id: created.record_id, status: updated.values.status, upsert: upserted.operation, listed: rows.length, total: page.total, changes: changes.length, denied, missing, user: user.email, nobody };
	}`))
	subject := systemdb.WorkflowSubjectID(workflow.ID)

	// Without grants the subject can do nothing.
	run := waitTestRun(t, system, workflow.ID, int64(decodeTestJSON[map[string]any](t, startTestRun(t, server, system, "admin", "writer", `{}`))["id"].(float64)))
	if run.Status != systemdb.WorkflowRunFailed || !strings.Contains(run.Error, "PermissionError") && !strings.Contains(run.Error, "permission") {
		t.Fatalf("expected the ungranted run to fail on permissions, got %s %q", run.Status, run.Error)
	}

	saveTestRecordCreateGrant(t, system, subject, "db.contacts")
	saveTestAllViewGrant(t, system, subject, "db.contacts")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: subject, Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll},
		permission.Grant{SubjectID: subject, Scope: permission.ScopeView, Resource: "db.contacts", Field: "active", Level: permission.Read},
	)
	waited := startTestRun(t, server, system, "admin", "writer", `{"wait":true}`)
	view := decodeTestJSON[workflowRunView](t, waited)
	if view.Status != systemdb.WorkflowRunSucceeded {
		t.Fatalf("expected the granted run to succeed, got %s: %s", view.Status, view.Error)
	}
	var output map[string]any
	if err := json.Unmarshal(view.Output, &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != "active" || output["upsert"] != "noop" || output["listed"] != 1.0 || output["total"] != 1.0 || output["changes"] != 2.0 || output["denied"] != "PermissionError" || output["missing"] != "NotFoundError" || output["user"] != "admin@example.com" || output["nobody"] != nil {
		t.Fatalf("unexpected output %#v", output)
	}
	entries, err := server.history.GetPrefix(context.Background(), history.RowPrefix("db", "contacts", int64(output["id"].(float64))))
	if err != nil || len(entries) == 0 {
		t.Fatal(err)
	}
	change, _ := history.DecodeRowChange(entries[0])
	if change.ActorID != subject {
		t.Fatalf("expected the row to be written by %s, got %q", subject, change.ActorID)
	}
}

func TestRecordChangedTriggersFilterAndDoNotLoop(t *testing.T) {
	server, system := newTestServer(t)
	startTestWorkflows(t, server)
	// "follow" reacts to status changes and writes the row it reacts to.
	follow := deployTestWorkflow(t, server, system, "follow", workflowScript(`{
		changed: { kind: "recordChanged", table: "contacts", operations: ["update"], fields: ["status"] },
	}`, `async function (event) {
		const change = event.change;
		await __autable_host.table("update", "contacts", { record_id: change.record_id, values: { status: change.values.status + "+" } });
		return { actor: change.actor, fields: change.changedFields, values: change.values, diff: change.diff, key: change.history_key };
	}`))
	// "other" writes status when asked; its writes must fire "follow".
	other := deployTestWorkflow(t, server, system, "other", workflowScript(manualOnlyTriggers, `async function () {
		await __autable_host.table("update", "contacts", { record_id: 1, values: { status: "from-other" } });
	}`))
	for _, workflow := range []systemdb.Workflow{follow, other} {
		subject := systemdb.WorkflowSubjectID(workflow.ID)
		saveTestAllViewGrant(t, system, subject, "db.contacts")
		saveTestGrants(t, system,
			permission.Grant{SubjectID: subject, Scope: permission.ScopeField, Resource: "db.contacts", Field: "name", Level: permission.FieldRead},
			permission.Grant{SubjectID: subject, Scope: permission.ScopeField, Resource: "db.contacts", Field: "status", Level: permission.FieldAll},
		)
	}
	makeTestAdmins(server, "editor")
	create := appTestRequest(t, server, system, "editor", http.MethodPost, "/api/tables/db/contacts/rows", jsonBody(`{"values":{"name":"Ada","email":"ada@example.com","status":"new"}}`))
	if create.Code != http.StatusCreated {
		t.Fatal(create.Body.String())
	}
	// Creates do not match operations ["update"]; a name-only update does
	// not match fields ["status"].
	if update := appTestRequest(t, server, system, "editor", http.MethodPatch, "/api/tables/db/contacts/rows/1", jsonBody(`{"values":{"name":"Ada L."}}`)); update.Code != http.StatusOK {
		t.Fatal(update.Body.String())
	}
	if update := appTestRequest(t, server, system, "editor", http.MethodPatch, "/api/tables/db/contacts/rows/1", jsonBody(`{"values":{"status":"active","email":"new@example.com"}}`)); update.Code != http.StatusOK {
		t.Fatal(update.Body.String())
	}
	runs := waitTestRuns(t, system, follow.ID, 1)
	output := outputOf(t, runs[0])
	actor := output["actor"].(map[string]any)
	if actor["id"] != "editor" || actor["type"] != "user" || actor["email"] != "editor@example.com" {
		t.Fatalf("unexpected actor %#v", actor)
	}
	if fields := output["fields"].([]any); len(fields) != 1 || fields[0] != "status" {
		t.Fatalf("expected only the readable changed field, got %#v", fields)
	}
	if values := output["values"].(map[string]any); values["email"] != nil || values["status"] != "active" || values["name"] != "Ada L." {
		t.Fatalf("expected unreadable fields filtered out, got %#v", values)
	}
	if _, ok := output["diff"].(map[string]any)["email"]; ok || output["key"] == "" {
		t.Fatalf("expected the diff filtered and a history key, got %#v", output)
	}

	// The run's own write (status "active+") did not fire it again.
	time.Sleep(200 * time.Millisecond)
	if runs := waitTestRuns(t, system, follow.ID, 1); len(runs) != 1 {
		t.Fatalf("expected no self-trigger, got %d runs", len(runs))
	}

	// Another workflow's write does fire it.
	if run := startTestRun(t, server, system, "admin", "other", `{"wait":true}`); run.Code != http.StatusOK {
		t.Fatal(run.Body.String())
	}
	runs = waitTestRuns(t, system, follow.ID, 2)
	second := outputOf(t, runs[0])
	if secondActor := second["actor"].(map[string]any); secondActor["id"] != systemdb.WorkflowSubjectID(other.ID) || secondActor["type"] != "workflow" {
		t.Fatalf("unexpected actor of the second run %#v", secondActor)
	}

	// A workflow whose subject cannot read the table is not fired.
	blind := deployTestWorkflow(t, server, system, "blind", workflowScript(`{ any: { kind: "recordChanged", table: "contacts" } }`, `function () {}`))
	if update := appTestRequest(t, server, system, "editor", http.MethodPatch, "/api/tables/db/contacts/rows/1", jsonBody(`{"values":{"status":"again"}}`)); update.Code != http.StatusOK {
		t.Fatal(update.Body.String())
	}
	waitTestRuns(t, system, follow.ID, 3)
	if runs, _ := system.WorkflowRuns(context.Background(), blind.ID, systemdb.WorkflowRunFilter{}); len(runs) != 0 {
		t.Fatalf("expected no runs without read grants, got %#v", runs)
	}
}

func TestIncludeOwnWritesLetsAWorkflowReactToItself(t *testing.T) {
	server, system := newTestServer(t)
	startTestWorkflows(t, server)
	workflow := deployTestWorkflow(t, server, system, "stages", workflowScript(`{
		changed: { kind: "recordChanged", table: "contacts", operations: ["create", "update"], fields: ["status"], includeOwnWrites: true },
	}`, `async function (event) {
		const next = { new: "review", review: "done" }[event.change.values.status];
		if (next) await __autable_host.table("update", "contacts", { record_id: event.change.record_id, values: { status: next } });
		return event.change.values.status;
	}`))
	subject := systemdb.WorkflowSubjectID(workflow.ID)
	saveTestAllViewGrant(t, system, subject, "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: subject, Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll})
	makeTestAdmins(server, "editor")
	if create := appTestRequest(t, server, system, "editor", http.MethodPost, "/api/tables/db/contacts/rows", jsonBody(`{"values":{"name":"Ada","status":"new"}}`)); create.Code != http.StatusCreated {
		t.Fatal(create.Body.String())
	}
	runs := waitTestRuns(t, system, workflow.ID, 3)
	stages := []string{runs[2].Output, runs[1].Output, runs[0].Output}
	if strings.Join(stages, ",") != `"new","review","done"` {
		t.Fatalf("expected the stages to advance through own writes, got %v", stages)
	}
}

func TestScheduleTriggerMatchesTimezoneOncePerMinute(t *testing.T) {
	server, system := newTestServer(t)
	startTestWorkflows(t, server)
	workflow := deployTestWorkflow(t, server, system, "daily", workflowScript(`{
		morning: { kind: "schedule", cron: "0 9 * * 1-5", timezone: "Asia/Shanghai" },
	}`, `function (event) { return { at: event.scheduledAt, trigger: event.trigger }; }`))
	engine := server.workflows
	ctx := context.Background()
	friday0900Shanghai := time.Date(2026, 11, 20, 1, 0, 30, 0, time.UTC)
	engine.scheduleTick(ctx, friday0900Shanghai)
	engine.scheduleTick(ctx, friday0900Shanghai.Add(10*time.Second)) // same minute: no second run
	engine.scheduleTick(ctx, time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	engine.scheduleTick(ctx, time.Date(2026, 11, 21, 1, 0, 0, 0, time.UTC)) // Saturday
	runs := waitTestRuns(t, system, workflow.ID, 1)
	output := outputOf(t, runs[0])
	if output["at"] != float64(friday0900Shanghai.Truncate(time.Minute).UnixMilli()) || output["trigger"] != "morning" {
		t.Fatalf("unexpected scheduled run %#v", output)
	}
	// Disabled workflows do not fire.
	disabled := false
	if _, err := system.UpdateWorkflowSettings(ctx, workflow.ID, systemdb.WorkflowSettings{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	engine.scheduleTick(ctx, time.Date(2026, 11, 23, 1, 0, 0, 0, time.UTC))
	time.Sleep(100 * time.Millisecond)
	waitTestRuns(t, system, workflow.ID, 1)
}

func webhookRequest(method, target, body string, headers map[string]string) *http.Request {
	request := rawRequest(method, target, body)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	return request
}

func TestWebhookAnswersWithTheRunAndVerifiesSignatures(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	workflow := deployTestWorkflow(t, server, system, "inbound", workflowScript(`{
		hook: { kind: "webhook", path: "events/v1" },
		slow: { kind: "webhook", path: "slow" },
	}`, `function (event) {
		const h = __autable_host;
		const request = event.request;
		if (event.trigger === "slow") { for (;;) {} }
		const expected = h.encode("hex", h.hmac("sha256", h.secret("signing_key"), request.body));
		if (!h.timingSafeEqual(expected, request.headers["x-signature"] || "")) {
			return { status: 401, body: { error: "bad signature" } };
		}
		const payload = JSON.parse(new TextDecoder().decode(request.body));
		return { status: 200, headers: { "X-Handled-By": "workflow" }, body: "ok " + payload.event + " " + request.method + " " + request.query.mode[0] + " " + request.path };
	}`))
	if recorder := appTestRequest(t, server, system, "admin", http.MethodPut, "/api/databases/db/workflows/inbound/secrets/signing_key", jsonBody(`{"value":"k3y"}`)); recorder.Code != http.StatusNoContent {
		t.Fatal(recorder.Body.String())
	}
	body := `{"event":"paid"}`
	mac := hmac.New(sha256.New, []byte("k3y"))
	mac.Write([]byte(body))
	signature := hex.EncodeToString(mac.Sum(nil))

	// No session, a foreign origin and no JSON content type: the code is
	// the only gate.
	good := serve(server, webhookRequest(http.MethodPost, "/api/hooks/db/inbound/events/v1?mode=live", body, map[string]string{"X-Signature": signature, "Origin": "https://sender.example", "Sec-Fetch-Site": "cross-site", "Content-Type": "text/plain"}))
	if good.Code != http.StatusOK || good.Body.String() != "ok paid POST live events/v1" || good.Header().Get("X-Handled-By") != "workflow" || !strings.HasPrefix(good.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("unexpected webhook answer %d %q %v", good.Code, good.Body.String(), good.Header())
	}
	bad := serve(server, webhookRequest(http.MethodPost, "/api/hooks/db/inbound/events/v1?mode=live", body, map[string]string{"X-Signature": "00"}))
	if bad.Code != http.StatusUnauthorized || !strings.Contains(bad.Body.String(), "bad signature") || bad.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected the code to reject a bad signature, got %d %s", bad.Code, bad.Body.String())
	}
	if unknown := serve(server, webhookRequest(http.MethodPost, "/api/hooks/db/inbound/other", body, nil)); unknown.Code != http.StatusNotFound {
		t.Fatalf("expected an unknown path to 404, got %d", unknown.Code)
	}
	runs, err := system.WorkflowRuns(context.Background(), workflow.ID, systemdb.WorkflowRunFilter{})
	if err != nil || len(runs) != 2 || runs[1].Status != systemdb.WorkflowRunSucceeded || runs[1].Output != `{"headers":{"Content-Type":"text/plain; charset=utf-8","X-Handled-By":"workflow"},"status":200}` {
		t.Fatalf("expected webhook runs recorded with their response, got %#v, %v", runs, err)
	}

	// The time limit answers 504 (the workflow timeout is below 15 s here).
	timeout := int64(1)
	if _, err := system.UpdateWorkflowSettings(context.Background(), workflow.ID, systemdb.WorkflowSettings{TimeoutSeconds: &timeout}); err != nil {
		t.Fatal(err)
	}
	slow := serve(server, webhookRequest(http.MethodGet, "/api/hooks/db/inbound/slow", "", nil))
	if slow.Code != http.StatusGatewayTimeout || !strings.Contains(slow.Body.String(), "run_id") {
		t.Fatalf("expected the slow webhook to 504, got %d %s", slow.Code, slow.Body.String())
	}

	disabled := false
	if _, err := system.UpdateWorkflowSettings(context.Background(), workflow.ID, systemdb.WorkflowSettings{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if off := serve(server, webhookRequest(http.MethodPost, "/api/hooks/db/inbound/events/v1", body, nil)); off.Code != http.StatusNotFound {
		t.Fatalf("expected a disabled workflow's webhook to 404, got %d", off.Code)
	}
}

func TestFetchDirectlyAndThroughARunner(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			_, _ = w.Write(bytes.Repeat([]byte("x"), 20<<20+1))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"seen":"` + r.Method + " " + r.Header.Get("Authorization") + `"}`))
		}
	}))
	defer target.Close()
	server, system := newTestServer(t)
	startTestWorkflows(t, server)
	workflow := deployTestWorkflow(t, server, system, "calls", workflowScript(manualOnlyTriggers, `async function (event) {
		const h = __autable_host;
		const call = async (via) => {
			const response = await h.fetch({ url: h.variable("target") + "/api", method: "POST", headers: { Authorization: "Bearer " + h.secret("token") }, body: "{}", via });
			return JSON.parse(new TextDecoder().decode(response.body)).seen + " " + response.status;
		};
		if (event.input.large) {
			await h.fetch({ url: h.variable("target") + "/large" });
		}
		return { direct: await call(undefined), viaRunner: await call("office") };
	}`))
	ctx := context.Background()
	if err := system.SetWorkflowVar(ctx, workflow.ID, "target", target.URL); err != nil {
		t.Fatal(err)
	}
	if err := system.SetWorkflowSecret(ctx, workflow.ID, "token", "t0k"); err != nil {
		t.Fatal(err)
	}

	// Without the runner online the via call fails the run.
	offline := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "calls", `{"wait":true}`))
	if offline.Status != systemdb.WorkflowRunFailed || !strings.Contains(offline.Error, `runner "office" is not connected`) {
		t.Fatalf("expected the offline runner to fail the run, got %s %q", offline.Status, offline.Error)
	}

	api := httptest.NewServer(server)
	defer api.Close()
	token, err := system.ResetRunnerToken(ctx, "db")
	if err != nil {
		t.Fatal(err)
	}
	runnerCtx, stopRunner := context.WithCancel(ctx)
	defer stopRunner()
	go func() {
		_ = runnercli.Run(runnerCtx, runnercli.Options{Endpoint: api.URL, Token: token, Name: "office"})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(server.runnerHub.Runners("db")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("runner never connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	online := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "calls", `{"wait":true}`))
	if online.Status != systemdb.WorkflowRunSucceeded || string(online.Output) != `{"direct":"POST Bearer t0k 200","viaRunner":"POST Bearer t0k 200"}` {
		t.Fatalf("unexpected run %s %q %s", online.Status, online.Error, online.Output)
	}

	large := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "calls", `{"wait":true,"input":{"large":true}}`))
	if large.Status != systemdb.WorkflowRunFailed || !strings.Contains(large.Error, "exceeds 20 MiB") {
		t.Fatalf("expected the response cap to fail the run, got %s %q", large.Status, large.Error)
	}
}

func TestRunTimeoutAndCancel(t *testing.T) {
	server, system := newTestServer(t)
	startTestWorkflows(t, server)
	workflow := deployTestWorkflow(t, server, system, "busy", workflowScript(`{ spin: { kind: "manual" }, idle: { kind: "manual" } }`, `function (event) {
		if (event.trigger === "spin") { for (;;) {} }
		return new Promise((resolve) => setTimeout(resolve, 60000));
	}`))
	timeout := int64(1)
	if _, err := system.UpdateWorkflowSettings(context.Background(), workflow.ID, systemdb.WorkflowSettings{TimeoutSeconds: &timeout}); err != nil {
		t.Fatal(err)
	}
	spun := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "busy", `{"trigger":"spin","wait":true}`))
	if spun.Status != systemdb.WorkflowRunFailed || !strings.Contains(spun.Error, "time limit") {
		t.Fatalf("expected the CPU-bound run to time out, got %s %q", spun.Status, spun.Error)
	}

	timeout = 600
	if _, err := system.UpdateWorkflowSettings(context.Background(), workflow.ID, systemdb.WorkflowSettings{TimeoutSeconds: &timeout}); err != nil {
		t.Fatal(err)
	}
	started := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "busy", `{"trigger":"idle"}`))
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, _ := system.WorkflowRun(context.Background(), workflow.ID, started.ID)
		if run.Status == systemdb.WorkflowRunRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never started: %#v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancelPath := "/api/databases/db/workflows/busy/runs/" + jsonNumber(started.ID) + "/cancel"
	cancelled := appTestRequest(t, server, system, "admin", http.MethodPost, cancelPath, nil)
	if run := decodeTestJSON[workflowRunView](t, cancelled); cancelled.Code != http.StatusOK || run.Status != systemdb.WorkflowRunCancelled {
		t.Fatalf("expected the run cancelled, got %d %s", cancelled.Code, cancelled.Body.String())
	}
	if again := appTestRequest(t, server, system, "admin", http.MethodPost, cancelPath, nil); again.Code != http.StatusConflict {
		t.Fatalf("expected cancelling a finished run to be 409, got %d", again.Code)
	}
}

func TestQueuedRunsKeepTheirVersionAndCanBeCancelled(t *testing.T) {
	// The engine is not started yet, so runs stay queued.
	server, system := newTestServer(t)
	deployTestWorkflow(t, server, system, "pinned", workflowScript(manualOnlyTriggers, `function () { return "v1:" + __autable_host.info.version; }`))
	first := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "pinned", `{}`))
	second := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "pinned", `{}`))
	if first.Status != systemdb.WorkflowRunQueued || first.Version != 1 {
		t.Fatalf("unexpected queued run %#v", first)
	}
	if push := pushTestWorkflow(t, server, system, "admin", "pinned", workflowScript(manualOnlyTriggers, `function () { return "v2"; }`), "publish=1"); push.Code != http.StatusOK {
		t.Fatal(push.Body.String())
	}
	cancel := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/databases/db/workflows/pinned/runs/"+jsonNumber(second.ID)+"/cancel", nil)
	if run := decodeTestJSON[workflowRunView](t, cancel); run.Status != systemdb.WorkflowRunCancelled {
		t.Fatalf("expected the queued run cancelled, got %#v", run)
	}
	startTestWorkflows(t, server)
	run := waitTestRun(t, system, first.WorkflowID, first.ID)
	if run.Status != systemdb.WorkflowRunSucceeded || run.Output != `"v1:1"` {
		t.Fatalf("expected the queued run to use its pinned version, got %s %q %s", run.Status, run.Output, run.Error)
	}
	if run, _ := system.WorkflowRun(context.Background(), first.WorkflowID, second.ID); run.Status != systemdb.WorkflowRunCancelled || run.StartedAt != 0 {
		t.Fatalf("expected the cancelled run never to start, got %#v", run)
	}
	latest := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "pinned", `{"wait":true}`))
	if string(latest.Output) != `"v2"` {
		t.Fatalf("expected new runs to use the new version, got %s", latest.Output)
	}
}

// openPersistentTestServer opens a server on files under dir, so a test can
// close it and open it again like a restart.
func openPersistentTestServer(t *testing.T, dir string) (*Server, *systemdb.DB, func()) {
	t.Helper()
	ctx := context.Background()
	system, err := systemdb.Open(ctx, filepath.Join(dir, "system.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := testCatalog(filepath.Join(dir, "db.sqlite"))
	repository, err := recorddb.OpenCatalog(ctx, catalog, dir)
	if err != nil {
		t.Fatal(err)
	}
	historyStore := history.NewMemoryStore()
	tableService := table.NewServiceWithRepository(historyStore, repository)
	server := NewServerWithAuthConfig(catalog, system, tableService, historyStore, config.AuthConfig{Admins: []string{"admin@example.com"}})
	runCtx, cancel := context.WithCancel(ctx)
	if err := server.StartWorkflows(runCtx); err != nil {
		t.Fatal(err)
	}
	return server, system, func() {
		cancel()
		server.WaitWorkflows()
		_ = repository.Close()
		_ = system.Close()
	}
}

func TestRestartMarksUnfinishedRunsInterrupted(t *testing.T) {
	dir := t.TempDir()
	server, system, stop := openPersistentTestServer(t, dir)
	deployTestWorkflow(t, server, system, "long", workflowScript(manualOnlyTriggers, `function () { return new Promise((resolve) => setTimeout(resolve, 60000)); }`))
	running := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "long", `{}`))
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, _ := system.WorkflowRun(context.Background(), running.WorkflowID, running.ID)
		if run.Status == systemdb.WorkflowRunRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never started: %#v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()

	server, system, stop = openPersistentTestServer(t, dir)
	defer stop()
	run, err := system.WorkflowRun(context.Background(), running.WorkflowID, running.ID)
	if err != nil || run.Status != systemdb.WorkflowRunFailed || run.Error != "interrupted by server restart" || run.FinishedAt == 0 {
		t.Fatalf("expected the run marked interrupted, got %#v, %v", run, err)
	}
	// The restarted server runs new work normally.
	if after := startTestRun(t, server, system, "admin", "long", `{}`); after.Code != http.StatusCreated {
		t.Fatalf("expected runs after the restart, got %d %s", after.Code, after.Body.String())
	}
}

func TestRetentionKeepsUnfinishedRuns(t *testing.T) {
	server, system := newTestServer(t)
	workflow := deployTestWorkflow(t, server, system, "kept", workflowScript(manualOnlyTriggers, `function () {}`))
	ctx := context.Background()
	old, _ := system.CreateWorkflowRun(ctx, systemdb.WorkflowRun{WorkflowID: workflow.ID, Trigger: "start"})
	if _, err := system.FinishWorkflowRun(ctx, old.ID, systemdb.WorkflowRunSucceeded, "", "", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	waiting, _ := system.CreateWorkflowRun(ctx, systemdb.WorkflowRun{WorkflowID: workflow.ID, Trigger: "start"})
	server.SetWorkflowClock(func() time.Time { return time.Now().Add(31 * 24 * time.Hour) })
	if err := server.CleanupWorkflowRuns(ctx); err != nil {
		t.Fatal(err)
	}
	runs, err := system.WorkflowRuns(ctx, workflow.ID, systemdb.WorkflowRunFilter{})
	if err != nil || len(runs) != 1 || runs[0].ID != waiting.ID {
		t.Fatalf("expected only the unfinished run to remain, got %#v, %v", runs, err)
	}
}

func TestWorkflowWritesNumericFields(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "db", Tables: []metadata.Table{{
		Name: "orders",
		Fields: []metadata.Field{
			{Name: "名称", Type: "string"},
			{Name: "数量", Type: "int"},
			{Name: "价格", Type: "float"},
		},
	}}}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	startTestWorkflows(t, server)
	workflow := deployTestWorkflow(t, server, system, "numbers", workflowScript(manualOnlyTriggers, `async function () {
		const h = __autable_host;
		const created = await h.table("create", "orders", { values: { 名称: "a", 数量: 2, 价格: 1.5 } });
		const updated = await h.table("update", "orders", { record_id: created.record_id, values: { 数量: 3, 价格: 2 } });
		const upserted = await h.table("upsert", "orders", { match_field: "名称", values: { 名称: "a", 数量: 4, 价格: 2.25 } });
		const fresh = await h.table("upsert", "orders", { match_field: "数量", values: { 名称: "b", 数量: 7, 价格: 0.5 } });
		return { created: created.values, updated: updated.values, upserted: [upserted.operation, upserted.values], fresh: [fresh.operation, fresh.values] };
	}`))
	subject := systemdb.WorkflowSubjectID(workflow.ID)
	saveTestRecordCreateGrant(t, system, subject, "db.orders")
	saveTestAllViewGrant(t, system, subject, "db.orders")
	saveTestGrants(t, system, permission.Grant{SubjectID: subject, Scope: permission.ScopeFieldSet, Resource: "db.orders", Level: permission.FieldAll})
	run := decodeTestJSON[workflowRunView](t, startTestRun(t, server, system, "admin", "numbers", `{"wait":true}`))
	if run.Status != systemdb.WorkflowRunSucceeded {
		t.Fatalf("expected numeric writes to succeed, got %s: %s", run.Status, run.Error)
	}
	want := `{"created":{"价格":1.5,"名称":"a","数量":2},"updated":{"价格":2,"名称":"a","数量":3},"upserted":["update",{"价格":2.25,"名称":"a","数量":4}],"fresh":["create",{"价格":0.5,"名称":"b","数量":7}]}`
	if string(run.Output) != want {
		t.Fatalf("unexpected output\n got %s\nwant %s", run.Output, want)
	}
}
