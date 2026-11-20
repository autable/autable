package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"autable/internal/appbundle"
	"autable/internal/systemdb"
	"autable/internal/wfsdk"
)

// libraryScript is what esbuild makes of a library: an IIFE assigning __lib.
func libraryScript(body string) string {
	return "var __lib = (() => {\n" + body + "\n})();\n"
}

func libraryTestBundle(t *testing.T, name, sdk, script string) []byte {
	t.Helper()
	manifest, err := json.Marshal(appbundle.Manifest{Name: name, Title: "Lib " + name, SDK: sdk})
	if err != nil {
		t.Fatal(err)
	}
	return testTarGz(t, []testTarEntry{
		{name: appbundle.LibraryManifestName, body: string(manifest)},
		{name: "dist/lib.js", body: script},
		{name: "dist/index.d.ts", body: "export declare function label(value: string): string;\n"},
		{name: "source/" + appbundle.LibraryManifestName, body: string(manifest)},
		{name: "source/src/index.ts", body: "// source of " + name},
	})
}

func pushTestLibrary(t *testing.T, server *Server, system *systemdb.DB, userID, name, script, query string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/libs/" + name + "/versions"
	if query != "" {
		target += "?" + query
	}
	return appTestRequest(t, server, system, userID, http.MethodPost, target, bytes.NewReader(libraryTestBundle(t, name, wfsdk.Version(), script)))
}

func mustStatus(t *testing.T, recorder *httptest.ResponseRecorder, want int, what string) {
	t.Helper()
	if recorder.Code != want {
		t.Fatalf("%s: expected %d, got %d %s", what, want, recorder.Code, recorder.Body.String())
	}
}

type testRunWithLibraries struct {
	ID        int64                 `json:"id"`
	Status    string                `json:"status"`
	Error     string                `json:"error"`
	Output    map[string]any        `json:"output"`
	Libraries []map[string]any      `json:"libraries"`
	Logs      []workflowLogResponse `json:"logs"`
}

func runAndWait(t *testing.T, server *Server, system *systemdb.DB, name string) testRunWithLibraries {
	t.Helper()
	recorder := startTestRun(t, server, system, "admin", name, `{"trigger":"start","wait":true}`)
	run := decodeTestJSON[testRunWithLibraries](t, recorder)
	if run.Status == "" {
		t.Fatalf("unexpected run response %d %s", recorder.Code, recorder.Body.String())
	}
	return run
}

func librariesOf(run testRunWithLibraries) string {
	parts := []string{}
	for _, library := range run.Libraries {
		parts = append(parts, library["name"].(string)+"@"+jsonNumber(library["version"]))
	}
	return strings.Join(parts, ",")
}

func TestLibrariesLinkAtRunTimeInTheirPublishedVersion(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	mirror := t.TempDir()
	server.SetRepositoryRoot(mirror)
	server.SetRepositorySync(&recordingSync{})
	startTestWorkflows(t, server)

	usesText := workflowScript(manualOnlyTriggers, `function () {
		var text = __autable_lib("@lib/text");
		return { label: text.label("a"), token: text.token() };
	}`)

	// Unknown and unpublished libraries fail publishing with 422.
	missing := pushTestWorkflow(t, server, system, "admin", "labels", usesText, "publish=1")
	mustStatus(t, missing, http.StatusUnprocessableEntity, "workflow importing a missing library")
	if !strings.Contains(missing.Body.String(), "@lib/text") || !strings.Contains(missing.Body.String(), "does not exist") {
		t.Fatalf("expected the missing library named, got %s", missing.Body.String())
	}
	first := pushTestLibrary(t, server, system, "admin", "text", libraryScript(`return { label: (value) => "v1:" + value, token: () => __autable_host.secret("token") || null };`), "message=first")
	mustStatus(t, first, http.StatusCreated, "first library push")
	unpublished := pushTestWorkflow(t, server, system, "admin", "labels", usesText, "publish=1")
	mustStatus(t, unpublished, http.StatusUnprocessableEntity, "workflow importing an unpublished library")
	if !strings.Contains(unpublished.Body.String(), "no published version") {
		t.Fatalf("expected the unpublished library explained, got %s", unpublished.Body.String())
	}
	if _, err := system.WorkflowByName(context.Background(), "db", "labels"); err == nil {
		t.Fatal("a rejected first push must not leave a workflow behind")
	}
	mustStatus(t, appTestRequest(t, server, system, "admin", http.MethodPost, "/api/libs/text/publish", jsonBody(`{"version":1}`)), http.StatusOK, "publish library v1")

	deployed := pushTestWorkflow(t, server, system, "admin", "labels", usesText, "publish=1")
	mustStatus(t, deployed, http.StatusCreated, "workflow importing a published library")
	if upload := decodeTestJSON[testWorkflowUpload](t, deployed); !slices.Equal(upload.Version.Libraries, []string{"text"}) {
		t.Fatalf("expected the version to record its imports, got %#v", upload.Version)
	}
	mustStatus(t, appTestRequest(t, server, system, "admin", http.MethodPut, "/api/databases/db/workflows/labels/secrets/token", jsonBody(`{"value":"caller-secret"}`)), http.StatusNoContent, "set secret")

	run := runAndWait(t, server, system, "labels")
	if run.Status != systemdb.WorkflowRunSucceeded || run.Output["label"] != "v1:a" || run.Output["token"] != "caller-secret" || librariesOf(run) != "text@1" {
		t.Fatalf("unexpected first run %#v", run)
	}

	// Publishing v2 changes the workflow's next run without pushing it.
	mustStatus(t, pushTestLibrary(t, server, system, "admin", "text", libraryScript(`return { label: (value) => "v2:" + value, token: () => "lib" };`), "publish=1&message=second"), http.StatusOK, "library v2")
	run = runAndWait(t, server, system, "labels")
	if run.Output["label"] != "v2:a" || librariesOf(run) != "text@2" {
		t.Fatalf("expected v2 behaviour, got %#v", run)
	}
	// Rolling back is publishing v1 again.
	mustStatus(t, appTestRequest(t, server, system, "admin", http.MethodPost, "/api/libs/text/publish", jsonBody(`{"version":1}`)), http.StatusOK, "roll back library")
	run = runAndWait(t, server, system, "labels")
	if run.Output["label"] != "v1:a" || librariesOf(run) != "text@1" {
		t.Fatalf("expected v1 behaviour after the rollback, got %#v", run)
	}
	detail := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/workflows/labels/runs/"+jsonNumber(run.ID), nil)
	if detailRun := decodeTestJSON[testRunWithLibraries](t, detail); librariesOf(detailRun) != "text@1" {
		t.Fatalf("expected the run detail to show the library versions, got %s", detail.Body.String())
	}

	// A library importing a library, imported at the top of a workflow
	// bundle (evaluated when triggers are read, too).
	greeting := libraryScript(`var text = __autable_lib("@lib/text"); return { greet: (name) => text.label("hello " + name) };`)
	mustStatus(t, pushTestLibrary(t, server, system, "admin", "greeting", greeting, "publish=1"), http.StatusCreated, "library importing a library")
	mustStatus(t, pushTestWorkflow(t, server, system, "admin", "greeter", `var __workflow = (() => {
		var greeting = __autable_lib("@lib/greeting");
		return { triggers: { start: { kind: "manual" } }, run: () => ({ text: greeting.greet("ada") }) };
	})();`, "publish=1"), http.StatusCreated, "workflow importing greeting")
	run = runAndWait(t, server, system, "greeter")
	if run.Output["text"] != "v1:hello ada" || librariesOf(run) != "text@1,greeting@1" {
		t.Fatalf("unexpected greeter run %#v", run)
	}

	// A version that would close an import cycle cannot be published.
	cyclic := pushTestLibrary(t, server, system, "admin", "text", libraryScript(`var greeting = __autable_lib("@lib/greeting"); return { label: (value) => value };`), "publish=1")
	mustStatus(t, cyclic, http.StatusUnprocessableEntity, "cyclic library")
	if message := decodeTestJSON[map[string]string](t, cyclic)["error"]; !strings.Contains(message, "text -> greeting -> text") {
		t.Fatalf("expected the cycle named, got %s", cyclic.Body.String())
	}
	selfImport := pushTestLibrary(t, server, system, "admin", "text", libraryScript(`__autable_lib("@lib/text"); return {};`), "publish=1")
	mustStatus(t, selfImport, http.StatusUnprocessableEntity, "self-importing library")

	// The detail lists versions and importers; types are served.
	var libraryDetail libraryDetailResponse
	libraryDetail = decodeTestJSON[libraryDetailResponse](t, appTestRequest(t, server, system, "admin", http.MethodGet, "/api/libs/text", nil))
	if libraryDetail.Library.PublishedVersion != 1 || len(libraryDetail.Versions) != 2 || libraryDetail.Versions[0].Message != "second" || !libraryDetail.Versions[1].Published {
		t.Fatalf("unexpected library detail %#v", libraryDetail)
	}
	importers := []string{}
	for _, importer := range libraryDetail.Library.ImportedBy {
		importers = append(importers, importer.Kind+":"+importer.DatabaseName+"/"+importer.Name)
	}
	if strings.Join(importers, ",") != "workflow:db/labels,library:/greeting" {
		t.Fatalf("unexpected importers %v", importers)
	}
	types := decodeTestJSON[map[string]any](t, appTestRequest(t, server, system, "admin", http.MethodGet, "/api/libs/text/types", nil))
	if types["version"] != float64(1) || !strings.Contains(types["types"].(string), "declare function label") {
		t.Fatalf("unexpected types %#v", types)
	}
	listed := decodeTestJSON[[]libraryResponse](t, appTestRequest(t, server, system, "admin", http.MethodGet, "/api/libs", nil))
	if len(listed) != 2 || listed[0].Name != "greeting" || listed[1].Name != "text" || len(listed[1].ImportedBy) != 2 {
		t.Fatalf("unexpected library list %#v", listed)
	}
	source := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/libs/text/versions/2/source", nil)
	mustStatus(t, source, http.StatusOK, "library source")
	if entries := readTestTarGz(t, source.Body.Bytes()); entries["src/index.ts"] != "// source of text" || entries[appbundle.LibraryManifestName] == "" {
		t.Fatalf("unexpected source entries %#v", entries)
	}
	mustStatus(t, appTestRequest(t, server, system, "admin", http.MethodGet, "/api/libs/text/versions/9/source", nil), http.StatusNotFound, "missing version source")

	// Libraries are mirrored into the repository under lib/<name>/.
	if data, err := os.ReadFile(filepath.Join(mirror, "lib", "text", "src", "index.ts")); err != nil || string(data) != "// source of text" {
		t.Fatalf("expected the library source in the mirror, got %q, %v", data, err)
	}

	// Imported libraries cannot be deleted.
	refused := appTestRequest(t, server, system, "admin", http.MethodDelete, "/api/libs/text", nil)
	mustStatus(t, refused, http.StatusConflict, "delete imported library")
	if !strings.Contains(refused.Body.String(), "workflow db/labels v1") || !strings.Contains(refused.Body.String(), "library greeting v1") {
		t.Fatalf("expected the importers named, got %s", refused.Body.String())
	}

	// A library that disappears (here behind the API's back) fails the run
	// with a clear error.
	library, _ := system.LibraryByName(context.Background(), "greeting")
	if err := system.DeleteLibrary(context.Background(), library.ID); err != nil {
		t.Fatal(err)
	}
	run = runAndWait(t, server, system, "greeter")
	if run.Status != systemdb.WorkflowRunFailed || !strings.Contains(run.Error, "library greeting does not exist") {
		t.Fatalf("expected the run to fail naming the library, got %#v", run)
	}

	// Once nothing imports it, a library can be deleted.
	mustStatus(t, pushTestWorkflow(t, server, system, "admin", "labels", workflowScript(manualOnlyTriggers, `function () { return 1; }`), "publish=1"), http.StatusOK, "workflow without the import")
	mustStatus(t, appTestRequest(t, server, system, "admin", http.MethodDelete, "/api/libs/text", nil), http.StatusNoContent, "delete library")
	if _, err := os.Stat(filepath.Join(mirror, "lib", "text")); !os.IsNotExist(err) {
		t.Fatalf("expected the mirrored library removed, got %v", err)
	}
	mustStatus(t, appTestRequest(t, server, system, "admin", http.MethodGet, "/api/libs/text", nil), http.StatusNotFound, "deleted library")
}

func TestLibraryUploadValidation(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	upload := func(name string, bundle []byte) *httptest.ResponseRecorder {
		return appTestRequest(t, server, system, "admin", http.MethodPost, "/api/libs/"+name+"/versions", bytes.NewReader(bundle))
	}
	script := libraryScript(`return {};`)
	mustStatus(t, upload("text", libraryTestBundle(t, "other", wfsdk.Version(), script)), http.StatusBadRequest, "name mismatch")
	mustStatus(t, upload("text", libraryTestBundle(t, "text", "99.0", script)), http.StatusUnprocessableEntity, "unsupported SDK")
	mustStatus(t, upload("text", libraryTestBundle(t, "text", wfsdk.Version(), "var __lib = {")), http.StatusUnprocessableEntity, "syntax error")
	mustStatus(t, upload("text", workflowTestBundle(t, "db", "text", wfsdk.Version(), script)), http.StatusBadRequest, "workflow bundle")
	mustStatus(t, upload("Bad_Name", libraryTestBundle(t, "Bad_Name", wfsdk.Version(), script)), http.StatusBadRequest, "bad name")
	missing := upload("text", libraryTestBundle(t, "text", wfsdk.Version(), libraryScript(`__autable_lib("@lib/nothing"); return {};`)))
	mustStatus(t, missing, http.StatusCreated, "unpublished upload with a missing import")
	mustStatus(t, appTestRequest(t, server, system, "admin", http.MethodPost, "/api/libs/text/publish", jsonBody(`{"version":1}`)), http.StatusUnprocessableEntity, "publish with a missing import")
	mustStatus(t, appTestRequest(t, server, system, "admin", http.MethodPost, "/api/libs/text/publish", jsonBody(`{"version":7}`)), http.StatusNotFound, "publish a missing version")
	mustStatus(t, appTestRequest(t, server, system, "admin", http.MethodGet, "/api/libs/text/types", nil), http.StatusNotFound, "types while unpublished")
}

func TestLibraryManagementIsAdminOnly(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	mustStatus(t, pushTestLibrary(t, server, system, "admin", "text", libraryScript(`return {};`), "publish=1"), http.StatusCreated, "admin push")
	requests := []struct {
		method, path string
		body         []byte
	}{
		{http.MethodGet, "/api/libs", nil},
		{http.MethodGet, "/api/libs/text", nil},
		{http.MethodPost, "/api/libs/text/versions", libraryTestBundle(t, "text", wfsdk.Version(), libraryScript(`return {};`))},
		{http.MethodPost, "/api/libs/created/versions", libraryTestBundle(t, "created", wfsdk.Version(), libraryScript(`return {};`))},
		{http.MethodPost, "/api/libs/text/publish", []byte(`{"version":1}`)},
		{http.MethodGet, "/api/libs/text/versions/1/source", nil},
		{http.MethodGet, "/api/libs/text/types", nil},
		{http.MethodDelete, "/api/libs/text", nil},
	}
	for _, request := range requests {
		recorder := appTestRequest(t, server, system, "viewer", request.method, request.path, bytes.NewReader(request.body))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("expected %s %s to be 403 for a non-admin, got %d %s", request.method, request.path, recorder.Code, recorder.Body.String())
		}
	}
	if anonymous := appTestRequest(t, server, system, "", http.MethodGet, "/api/libs", nil); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous access to be 401, got %d", anonymous.Code)
	}
	if _, err := system.LibraryByName(context.Background(), "created"); err == nil {
		t.Fatal("a non-admin push must not create a library")
	}
}
