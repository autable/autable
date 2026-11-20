package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"autable/internal/appbundle"
	"autable/internal/appsdk"
	"autable/internal/metadata"
	"autable/internal/permission"
	"autable/internal/systemdb"

	"gorm.io/gorm"
)

type testAppUpload struct {
	App     appResponse        `json:"app"`
	Version appVersionResponse `json:"version"`
}

type testTarEntry struct {
	name string
	body string
}

func testTarGz(t *testing.T, entries []testTarEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func testAppManifest(t *testing.T, database, name, sdk string) string {
	t.Helper()
	manifest, err := json.Marshal(appbundle.Manifest{Database: database, Name: name, Title: "Widgets", SDK: sdk})
	if err != nil {
		t.Fatal(err)
	}
	return string(manifest)
}

// testAppBundle builds an upload whose index page and script both carry
// marker, so tests can tell versions apart.
func testAppBundle(t *testing.T, database, name, sdk, marker string) []byte {
	t.Helper()
	manifest := testAppManifest(t, database, name, sdk)
	return testTarGz(t, []testTarEntry{
		{name: appbundle.ManifestName, body: manifest},
		{name: "dist/index.html", body: `<!doctype html><html><head><meta charset="utf-8"><title>Widgets</title><script type="module" src="./assets/x.js"></script></head><body><div id="app">` + marker + `</div></body></html>`},
		{name: "dist/assets/x.js", body: `console.log("` + marker + `");`},
		{name: "source/" + appbundle.ManifestName, body: manifest},
		{name: "source/package.json", body: `{"name":"widgets","private":true}`},
		{name: "source/src/main.ts", body: `export const marker = "` + marker + `";`},
	})
}

func appTestRequest(t *testing.T, server *Server, system *systemdb.DB, userID, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	request := newTestRequest(method, target, body)
	if userID != "" {
		request.AddCookie(testSessionCookie(t, system, userID))
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func uploadTestApp(t *testing.T, server *Server, system *systemdb.DB, userID, database, name string, bundle []byte, query string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/databases/" + url.PathEscape(database) + "/apps/" + name + "/versions"
	if query != "" {
		target += "?" + query
	}
	return appTestRequest(t, server, system, userID, http.MethodPost, target, bytes.NewReader(bundle))
}

// createTestApp uploads a first, published version of an app.
func createTestApp(t *testing.T, server *Server, system *systemdb.DB, userID, database, name string) testAppUpload {
	t.Helper()
	recorder := uploadTestApp(t, server, system, userID, database, name, testAppBundle(t, database, name, appsdk.Version(), "version-1"), "publish=1")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected app create 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeTestJSON[testAppUpload](t, recorder)
}

func decodeTestJSON[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %T from %q: %v", value, recorder.Body.String(), err)
	}
	return value
}

func publishTestApp(t *testing.T, server *Server, system *systemdb.DB, userID, database, name string, version int64) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]int64{"version": version})
	if err != nil {
		t.Fatal(err)
	}
	return appTestRequest(t, server, system, userID, http.MethodPost, "/api/databases/"+url.PathEscape(database)+"/apps/"+name+"/publish", bytes.NewReader(body))
}

func appSubjectLevel(t *testing.T, system *systemdb.DB, subjectID string, scope permission.Scope, resource string) permission.Level {
	t.Helper()
	perms, err := system.EffectiveGrantsForSubject(context.Background(), subjectID)
	if err != nil {
		t.Fatal(err)
	}
	return perms.ResourceLevel(subjectID, scope, resource)
}

func TestAppUploadCreatesAppAndVersions(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "builder")

	created := createTestApp(t, server, system, "builder", "db", "widgets")
	if created.App.Name != "widgets" || created.App.DatabaseName != "db" || created.App.Title != "Widgets" {
		t.Fatalf("unexpected created app: %#v", created.App)
	}
	if created.App.LatestVersion != 1 || created.App.PublishedVersion != 1 || created.App.CreatorID != "builder" {
		t.Fatalf("expected first published version, got %#v", created.App)
	}
	if created.App.URL != "/apps/db/widgets/" {
		t.Fatalf("unexpected app url %q", created.App.URL)
	}
	if created.Version.Version != 1 || !created.Version.Supported || created.Version.SDK != appsdk.Version() || created.Version.PreviewURL != "/apps/db/widgets/@1/" {
		t.Fatalf("unexpected first version: %#v", created.Version)
	}
	if created.Version.Size == 0 || created.Version.CreatedBy != "builder" {
		t.Fatalf("expected version bookkeeping, got %#v", created.Version)
	}
	if level := appSubjectLevel(t, system, "builder", permission.ScopeApp, resourceID(created.App.ID)); level != permission.None {
		t.Fatalf("expected no grant for the creating admin, got %d", level)
	}

	second := uploadTestApp(t, server, system, "builder", "db", "widgets", testAppBundle(t, "db", "widgets", appsdk.Version(), "version-2"), "message="+url.QueryEscape("second build"))
	if second.Code != http.StatusOK {
		t.Fatalf("expected second upload 200, got %d: %s", second.Code, second.Body.String())
	}
	upload := decodeTestJSON[testAppUpload](t, second)
	if upload.Version.Version != 2 || upload.Version.Message != "second build" {
		t.Fatalf("unexpected second version: %#v", upload.Version)
	}
	if upload.App.LatestVersion != 2 || upload.App.PublishedVersion != 1 {
		t.Fatalf("expected upload without publish to keep version 1 published, got %#v", upload.App)
	}

	detail := appTestRequest(t, server, system, "builder", http.MethodGet, "/api/databases/db/apps/widgets", nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("expected app detail 200, got %d: %s", detail.Code, detail.Body.String())
	}
	response := decodeTestJSON[appDetailResponse](t, detail)
	if response.App.ID != created.App.ID || response.App.PermissionLevel != permission.Write || response.App.URL != "/apps/db/widgets/" {
		t.Fatalf("unexpected detail app: %#v", response.App)
	}
	if len(response.Versions) != 2 || response.Versions[0].Version != 2 || response.Versions[1].Version != 1 {
		t.Fatalf("expected versions newest first, got %#v", response.Versions)
	}
	for _, version := range response.Versions {
		if !version.Supported || version.SDKError != "" {
			t.Fatalf("expected supported version, got %#v", version)
		}
	}
	if response.Versions[0].PreviewURL != "/apps/db/widgets/@2/" || response.Versions[1].PreviewURL != "/apps/db/widgets/@1/" {
		t.Fatalf("unexpected preview urls: %#v", response.Versions)
	}
	if response.SDK.Version != appsdk.Version() || response.SDK.MinMajor != appsdk.MinMajor {
		t.Fatalf("unexpected sdk info: %#v", response.SDK)
	}
}

func TestAppUploadRequiresPermission(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")

	denied := uploadTestApp(t, server, system, "viewer", "db", "widgets", testAppBundle(t, "db", "widgets", appsdk.Version(), "x"), "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("expected upload without permission 403, got %d: %s", denied.Code, denied.Body.String())
	}
	if _, err := system.AppByName(ctx, "db", "widgets"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected denied upload to create nothing, got %v", err)
	}

	anonymous := uploadTestApp(t, server, system, "", "db", "widgets", testAppBundle(t, "db", "widgets", appsdk.Version(), "x"), "")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous upload 401, got %d: %s", anonymous.Code, anonymous.Body.String())
	}

	created := createTestApp(t, server, system, "admin", "db", "widgets")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "reader", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Open},
		permission.Grant{SubjectID: "set-reader", Scope: permission.ScopeAppSet, Resource: "db", Level: permission.Open},
		// A level the API no longer accepts, as a pre-upgrade row would be.
		permission.Grant{SubjectID: "legacy-editor", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Write},
	)
	for _, userID := range []string{"reader", "set-reader", "legacy-editor"} {
		upload := uploadTestApp(t, server, system, userID, "db", "widgets", testAppBundle(t, "db", "widgets", appsdk.Version(), "x"), "")
		if upload.Code != http.StatusForbidden {
			t.Fatalf("expected %s upload 403, got %d: %s", userID, upload.Code, upload.Body.String())
		}
		create := uploadTestApp(t, server, system, userID, "db", "gadgets", testAppBundle(t, "db", "gadgets", appsdk.Version(), "x"), "")
		if create.Code != http.StatusForbidden {
			t.Fatalf("expected %s app create 403, got %d: %s", userID, create.Code, create.Body.String())
		}
	}
	if app, err := system.AppByName(ctx, "db", "widgets"); err != nil || app.LatestVersion != 1 {
		t.Fatalf("expected denied uploads to add no version, got %#v, %v", app, err)
	}

	missingDatabase := uploadTestApp(t, server, system, "admin", "missing", "widgets", testAppBundle(t, "missing", "widgets", appsdk.Version(), "x"), "")
	if missingDatabase.Code != http.StatusNotFound {
		t.Fatalf("expected upload to unknown database 404, got %d: %s", missingDatabase.Code, missingDatabase.Body.String())
	}
}

func TestAppUploadRejectsInvalidBundles(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")

	cases := map[string]struct {
		name   string
		bundle []byte
	}{
		"manifest database mismatch": {name: "widgets", bundle: testAppBundle(t, "other", "widgets", appsdk.Version(), "x")},
		"manifest name mismatch":     {name: "widgets", bundle: testAppBundle(t, "db", "gadgets", appsdk.Version(), "x")},
		"invalid app name":           {name: "Bad_Name", bundle: testAppBundle(t, "db", "Bad_Name", appsdk.Version(), "x")},
		"not an archive":             {name: "widgets", bundle: []byte("not a bundle")},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			recorder := uploadTestApp(t, server, system, "admin", "db", tc.name, tc.bundle, "")
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	apps, err := system.Apps(context.Background(), "db")
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("expected rejected uploads to store nothing, got %#v", apps)
	}
}

func TestAppUploadRejectsUnsupportedSDK(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	for _, sdk := range []string{"0.1", "2.0", "1.99", "1"} {
		t.Run(sdk, func(t *testing.T) {
			recorder := uploadTestApp(t, server, system, "admin", "db", "widgets", testAppBundle(t, "db", "widgets", sdk, "x"), "")
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected sdk %s upload 422, got %d: %s", sdk, recorder.Code, recorder.Body.String())
			}
		})
	}
	apps, err := system.Apps(context.Background(), "db")
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("expected unsupported uploads to store nothing, got %#v", apps)
	}
}

func TestAppPublishAndRollback(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	created := createTestApp(t, server, system, "admin", "db", "widgets")
	saveTestGrants(t, system, permission.Grant{SubjectID: "reader", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Open})

	second := uploadTestApp(t, server, system, "admin", "db", "widgets", testAppBundle(t, "db", "widgets", appsdk.Version(), "version-2"), "")
	if second.Code != http.StatusOK {
		t.Fatalf("expected second upload 200, got %d: %s", second.Code, second.Body.String())
	}
	assertServedMarker(t, server, system, "reader", "/apps/db/widgets/", "version-1")

	published := publishTestApp(t, server, system, "admin", "db", "widgets", 2)
	if published.Code != http.StatusOK {
		t.Fatalf("expected publish 200, got %d: %s", published.Code, published.Body.String())
	}
	if app := decodeTestJSON[appResponse](t, published); app.PublishedVersion != 2 || app.LatestVersion != 2 || app.PermissionLevel != permission.Write {
		t.Fatalf("unexpected published app: %#v", app)
	}
	assertServedMarker(t, server, system, "reader", "/apps/db/widgets/", "version-2")
	assertServedMarker(t, server, system, "reader", "/apps/db/widgets/assets/x.js", "version-2")

	rollback := publishTestApp(t, server, system, "admin", "db", "widgets", 1)
	if rollback.Code != http.StatusOK {
		t.Fatalf("expected rollback 200, got %d: %s", rollback.Code, rollback.Body.String())
	}
	assertServedMarker(t, server, system, "reader", "/apps/db/widgets/", "version-1")

	missing := publishTestApp(t, server, system, "admin", "db", "widgets", 3)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected publish of missing version 404, got %d: %s", missing.Code, missing.Body.String())
	}
	readerPublish := publishTestApp(t, server, system, "reader", "db", "widgets", 2)
	if readerPublish.Code != http.StatusForbidden {
		t.Fatalf("expected reader publish 403, got %d: %s", readerPublish.Code, readerPublish.Body.String())
	}
	unknownApp := publishTestApp(t, server, system, "admin", "db", "gadgets", 1)
	if unknownApp.Code != http.StatusNotFound {
		t.Fatalf("expected publish of unknown app 404, got %d: %s", unknownApp.Code, unknownApp.Body.String())
	}
	app, err := system.AppByName(context.Background(), "db", "widgets")
	if err != nil {
		t.Fatal(err)
	}
	if app.PublishedVersion != 1 {
		t.Fatalf("expected failed publishes to keep version 1, got %#v", app)
	}

	publishOnUpload := uploadTestApp(t, server, system, "admin", "db", "widgets", testAppBundle(t, "db", "widgets", appsdk.Version(), "version-3"), "publish=1")
	if publishOnUpload.Code != http.StatusOK {
		t.Fatalf("expected publishing upload 200, got %d: %s", publishOnUpload.Code, publishOnUpload.Body.String())
	}
	if upload := decodeTestJSON[testAppUpload](t, publishOnUpload); upload.App.PublishedVersion != 3 {
		t.Fatalf("expected upload with publish=1 to publish version 3, got %#v", upload.App)
	}
	assertServedMarker(t, server, system, "reader", "/apps/db/widgets/", "version-3")
}

func TestAppUnsupportedStoredVersion(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	created := createTestApp(t, server, system, "admin", "db", "widgets")
	// A version built against an SDK this server no longer runs, as left
	// behind by an upgrade.
	if _, _, err := system.AddAppVersion(ctx, created.App.ID, "", systemdb.AppVersion{SDK: "0.9", CreatedBy: "admin"}, testAppBundle(t, "db", "widgets", "0.9", "old"), false); err != nil {
		t.Fatal(err)
	}

	detail := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/apps/widgets", nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("expected detail 200, got %d: %s", detail.Code, detail.Body.String())
	}
	response := decodeTestJSON[appDetailResponse](t, detail)
	if len(response.Versions) != 2 || response.Versions[0].Version != 2 || response.Versions[0].Supported || response.Versions[0].SDKError == "" {
		t.Fatalf("expected unsupported newest version, got %#v", response.Versions)
	}
	if !response.Versions[1].Supported {
		t.Fatalf("expected version 1 supported, got %#v", response.Versions[1])
	}

	publish := publishTestApp(t, server, system, "admin", "db", "widgets", 2)
	if publish.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected publish of unsupported version 422, got %d: %s", publish.Code, publish.Body.String())
	}
	preview := appTestRequest(t, server, system, "admin", http.MethodGet, "/apps/db/widgets/@2/", nil)
	if preview.Code != http.StatusGone || !strings.Contains(preview.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected unsupported version page 410, got %d: %s", preview.Code, preview.Body.String())
	}
	assertServedMarker(t, server, system, "admin", "/apps/db/widgets/", "version-1")
}

func TestAppListAndDetailFollowPermissions(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	alpha := createTestApp(t, server, system, "admin", "db", "alpha")
	createTestApp(t, server, system, "admin", "db", "beta")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "reader", Scope: permission.ScopeApp, Resource: resourceID(alpha.App.ID), Level: permission.Open},
		permission.Grant{SubjectID: "set-reader", Scope: permission.ScopeAppSet, Resource: "db", Level: permission.Open},
	)

	list := func(userID string) []appResponse {
		t.Helper()
		recorder := appTestRequest(t, server, system, userID, http.MethodGet, "/api/databases/db/apps", nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected app list 200 for %s, got %d: %s", userID, recorder.Code, recorder.Body.String())
		}
		return decodeTestJSON[[]appResponse](t, recorder)
	}
	if apps := list("stranger"); len(apps) != 0 {
		t.Fatalf("expected no apps for stranger, got %#v", apps)
	}
	if apps := list("reader"); len(apps) != 1 || apps[0].Name != "alpha" || apps[0].PermissionLevel != permission.Read || apps[0].URL != "/apps/db/alpha/" {
		t.Fatalf("expected only alpha for reader, got %#v", apps)
	}
	if apps := list("set-reader"); len(apps) != 2 || apps[0].PermissionLevel != permission.Read || apps[1].PermissionLevel != permission.Read {
		t.Fatalf("expected both apps readable through app_set, got %#v", apps)
	}
	if apps := list("admin"); len(apps) != 2 || apps[0].Name != "alpha" || apps[1].Name != "beta" || apps[0].PermissionLevel != permission.Write {
		t.Fatalf("expected admin to see both apps with write, got %#v", apps)
	}

	for _, tc := range []struct {
		user, app string
		status    int
	}{
		{"stranger", "alpha", http.StatusForbidden},
		{"reader", "alpha", http.StatusOK},
		{"reader", "beta", http.StatusForbidden},
		{"set-reader", "beta", http.StatusOK},
		{"admin", "missing", http.StatusNotFound},
	} {
		recorder := appTestRequest(t, server, system, tc.user, http.MethodGet, "/api/databases/db/apps/"+tc.app, nil)
		if recorder.Code != tc.status {
			t.Fatalf("expected %s detail of %s %d, got %d: %s", tc.user, tc.app, tc.status, recorder.Code, recorder.Body.String())
		}
	}
	readerDetail := decodeTestJSON[appDetailResponse](t, appTestRequest(t, server, system, "reader", http.MethodGet, "/api/databases/db/apps/alpha", nil))
	if readerDetail.App.PermissionLevel != permission.Open || readerDetail.App.URL != "/apps/db/alpha/" {
		t.Fatalf("expected reader detail permission open, got %#v", readerDetail.App)
	}
	if len(readerDetail.Versions) != 0 {
		t.Fatalf("expected versions to be admin-only, got %#v", readerDetail.Versions)
	}
	adminDetail := decodeTestJSON[appDetailResponse](t, appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/apps/alpha", nil))
	if adminDetail.App.PermissionLevel != permission.Write || len(adminDetail.Versions) != 1 {
		t.Fatalf("expected admin detail with versions, got %#v", adminDetail)
	}
}

func TestAppCatalogPermissionLevelAndVisibility(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	created := createTestApp(t, server, system, "admin", "db", "widgets")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "app-reader", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Open},
		permission.Grant{SubjectID: "set-reader", Scope: permission.ScopeAppSet, Resource: "db", Level: permission.Open},
	)

	catalogFor := func(userID string) []metadata.Database {
		t.Helper()
		recorder := appTestRequest(t, server, system, userID, http.MethodGet, "/api/metadata", nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected metadata 200 for %s, got %d: %s", userID, recorder.Code, recorder.Body.String())
		}
		return decodeTestJSON[metadata.Catalog](t, recorder).Databases
	}
	if databases := catalogFor("stranger"); len(databases) != 0 {
		t.Fatalf("expected no databases for stranger, got %#v", databases)
	}
	appReader := catalogFor("app-reader")
	if len(appReader) != 1 || appReader[0].Name != "db" || len(appReader[0].Tables) != 0 {
		t.Fatalf("expected db visible through an openable app only, got %#v", appReader)
	}
	setReader := catalogFor("set-reader")
	if len(setReader) != 1 || setReader[0].Name != "db" || len(setReader[0].Tables) != 0 {
		t.Fatalf("expected db visible through app_set, got %#v", setReader)
	}
	admin := catalogFor("admin")
	if len(admin) != 1 || len(admin[0].Tables) != 1 || admin[0].Tables[0].PermissionLevel != int(permission.Write) {
		t.Fatalf("expected admin to see every table with full level, got %#v", admin)
	}
}

func TestAppGrantsAreExclusive(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	created := createTestApp(t, server, system, "admin", "db", "widgets")
	appResource := resourceID(created.App.ID)

	saveGrant := func(grant string) {
		t.Helper()
		recorder := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/permissions/grants", strings.NewReader(grant))
		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected grant 201, got %d: %s", recorder.Code, recorder.Body.String())
		}
	}
	saveGrant(`{"subject_id":"member","scope":"app","resource":"` + appResource + `","level":1}`)
	saveGrant(`{"subject_id":"member","scope":"app_set","resource":"db","level":1}`)
	if level := appSubjectLevel(t, system, "member", permission.ScopeApp, appResource); level != permission.None {
		t.Fatalf("expected app_set grant to replace app grant, got app level %d", level)
	}
	if level := appSubjectLevel(t, system, "member", permission.ScopeAppSet, "db"); level != permission.Open {
		t.Fatalf("expected app_set open, got %d", level)
	}
	saveGrant(`{"subject_id":"member","scope":"app","resource":"` + appResource + `","level":1}`)
	if level := appSubjectLevel(t, system, "member", permission.ScopeAppSet, "db"); level != permission.None {
		t.Fatalf("expected app grant to replace app_set grant, got app_set level %d", level)
	}

	// Open is the only app level: anything above it is rejected.
	for _, grant := range []string{
		`{"subject_id":"member","scope":"app","resource":"` + appResource + `","level":2}`,
		`{"subject_id":"member","scope":"app_set","resource":"db","level":2}`,
	} {
		rejected := appTestRequest(t, server, system, "admin", http.MethodPost, "/api/permissions/grants", strings.NewReader(grant))
		if rejected.Code != http.StatusBadRequest {
			t.Fatalf("expected app grant above open 400, got %d: %s", rejected.Code, rejected.Body.String())
		}
	}

	other := appTestRequest(t, server, system, "stranger", http.MethodPost, "/api/permissions/grants", strings.NewReader(`{"subject_id":"member","scope":"app","resource":"`+appResource+`","level":1}`))
	if other.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin grant 403, got %d: %s", other.Code, other.Body.String())
	}
}

func assertServedMarker(t *testing.T, server *Server, system *systemdb.DB, userID, target, marker string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := appTestRequest(t, server, system, userID, http.MethodGet, target, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected %s 200, got %d: %s", target, recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), marker) {
		t.Fatalf("expected %s to serve %q, got %s", target, marker, recorder.Body.String())
	}
	return recorder
}

func TestServeApp(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	created := createTestApp(t, server, system, "admin", "db", "widgets")
	saveTestGrants(t, system, permission.Grant{SubjectID: "reader", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Open})

	anonymous := appTestRequest(t, server, system, "", http.MethodGet, "/apps/db/widgets/orders/42?tab=open", nil)
	if anonymous.Code != http.StatusFound {
		t.Fatalf("expected anonymous 302, got %d: %s", anonymous.Code, anonymous.Body.String())
	}
	if location := anonymous.Header().Get("Location"); location != "/login?redirect="+url.QueryEscape("/apps/db/widgets/orders/42?tab=open") {
		t.Fatalf("unexpected login redirect %q", location)
	}

	index := assertServedMarker(t, server, system, "reader", "/apps/db/widgets/", "version-1")
	if !strings.Contains(index.Body.String(), `<head><base href="/apps/db/widgets/"><meta charset="utf-8">`) {
		t.Fatalf("expected base injected at the start of head, got %s", index.Body.String())
	}
	if strings.Count(index.Body.String(), "<base ") != 1 {
		t.Fatalf("expected exactly one base tag, got %s", index.Body.String())
	}
	if contentType := index.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("expected html content type, got %q", contentType)
	}
	if cache := index.Header().Get("Cache-Control"); cache != "no-cache" {
		t.Fatalf("expected index no-cache, got %q", cache)
	}

	for _, target := range []string{"/apps/db/widgets/orders/42", "/apps/db/widgets/orders/", "/apps/db/widgets/index.html"} {
		deep := assertServedMarker(t, server, system, "reader", target, `<base href="/apps/db/widgets/">`)
		if !strings.HasPrefix(deep.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("expected %s to fall back to index.html, got %q", target, deep.Header().Get("Content-Type"))
		}
	}

	missing := appTestRequest(t, server, system, "reader", http.MethodGet, "/apps/db/widgets/assets/missing.js", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected missing asset 404, got %d: %s", missing.Code, missing.Body.String())
	}

	asset := assertServedMarker(t, server, system, "reader", "/apps/db/widgets/assets/x.js", `console.log("version-1");`)
	if contentType := asset.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
		t.Fatalf("expected javascript content type, got %q", contentType)
	}
	if strings.Contains(asset.Body.String(), "<base") {
		t.Fatalf("expected asset served verbatim, got %s", asset.Body.String())
	}
	etag := asset.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected asset etag")
	}
	conditional := newTestRequest(http.MethodGet, "/apps/db/widgets/assets/x.js", nil)
	conditional.AddCookie(testSessionCookie(t, system, "reader"))
	conditional.Header.Set("If-None-Match", etag)
	notModified := httptest.NewRecorder()
	server.ServeHTTP(notModified, conditional)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("expected conditional asset 304, got %d", notModified.Code)
	}

	root := appTestRequest(t, server, system, "reader", http.MethodGet, "/apps/db/widgets", nil)
	if root.Code != http.StatusMovedPermanently || root.Header().Get("Location") != "/apps/db/widgets/" {
		t.Fatalf("expected root 301 to trailing slash, got %d %q", root.Code, root.Header().Get("Location"))
	}
	rootQuery := appTestRequest(t, server, system, "reader", http.MethodGet, "/apps/db/widgets?tab=open", nil)
	if rootQuery.Code != http.StatusMovedPermanently || rootQuery.Header().Get("Location") != "/apps/db/widgets/?tab=open" {
		t.Fatalf("expected root redirect to keep the query, got %d %q", rootQuery.Code, rootQuery.Header().Get("Location"))
	}

	stranger := appTestRequest(t, server, system, "stranger", http.MethodGet, "/apps/db/widgets/", nil)
	if stranger.Code != http.StatusForbidden || !strings.HasPrefix(stranger.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected stranger 403 page, got %d: %s", stranger.Code, stranger.Body.String())
	}
	if strings.Contains(stranger.Body.String(), "version-1") {
		t.Fatalf("expected no app content for stranger, got %s", stranger.Body.String())
	}
	strangerAsset := appTestRequest(t, server, system, "stranger", http.MethodGet, "/apps/db/widgets/assets/x.js", nil)
	if strangerAsset.Code != http.StatusForbidden {
		t.Fatalf("expected stranger asset 403, got %d", strangerAsset.Code)
	}

	unknown := appTestRequest(t, server, system, "reader", http.MethodGet, "/apps/db/gadgets/", nil)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("expected unknown app 404, got %d: %s", unknown.Code, unknown.Body.String())
	}
}

func TestServeAppPreviewIsAdminOnly(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	created := createTestApp(t, server, system, "admin", "db", "widgets")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "reader", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Open},
		permission.Grant{SubjectID: "legacy-editor", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Write},
	)
	if recorder := uploadTestApp(t, server, system, "admin", "db", "widgets", testAppBundle(t, "db", "widgets", appsdk.Version(), "version-2"), ""); recorder.Code != http.StatusOK {
		t.Fatalf("expected admin upload 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	for _, userID := range []string{"reader", "legacy-editor"} {
		preview := appTestRequest(t, server, system, userID, http.MethodGet, "/apps/db/widgets/@1/", nil)
		if preview.Code != http.StatusForbidden || strings.Contains(preview.Body.String(), "version-1") {
			t.Fatalf("expected %s preview 403, got %d: %s", userID, preview.Code, preview.Body.String())
		}
	}

	preview := assertServedMarker(t, server, system, "admin", "/apps/db/widgets/@2/", "version-2")
	if !strings.Contains(preview.Body.String(), `<base href="/apps/db/widgets/@2/">`) {
		t.Fatalf("expected preview base, got %s", preview.Body.String())
	}
	adminPreview := assertServedMarker(t, server, system, "admin", "/apps/db/widgets/@1/orders/7", "version-1")
	if !strings.Contains(adminPreview.Body.String(), `<base href="/apps/db/widgets/@1/">`) {
		t.Fatalf("expected preview deep path base, got %s", adminPreview.Body.String())
	}
	assertServedMarker(t, server, system, "admin", "/apps/db/widgets/@2/assets/x.js", `console.log("version-2");`)
	// The published version is still version 1.
	assertServedMarker(t, server, system, "reader", "/apps/db/widgets/", "version-1")

	if index, preview := appTestRequest(t, server, system, "admin", http.MethodGet, "/apps/db/widgets/", nil), appTestRequest(t, server, system, "admin", http.MethodGet, "/apps/db/widgets/@1/", nil); index.Header().Get("ETag") == preview.Header().Get("ETag") {
		t.Fatalf("expected published and preview index pages to have distinct etags, got %q", index.Header().Get("ETag"))
	}

	noSlash := appTestRequest(t, server, system, "admin", http.MethodGet, "/apps/db/widgets/@2", nil)
	if noSlash.Code != http.StatusMovedPermanently || noSlash.Header().Get("Location") != "/apps/db/widgets/@2/" {
		t.Fatalf("expected preview 301 to trailing slash, got %d %q", noSlash.Code, noSlash.Header().Get("Location"))
	}
	for _, target := range []string{"/apps/db/widgets/@9/", "/apps/db/widgets/@x/", "/apps/db/widgets/@0/"} {
		recorder := appTestRequest(t, server, system, "admin", http.MethodGet, target, nil)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected %s 404, got %d: %s", target, recorder.Code, recorder.Body.String())
		}
	}
}

func TestServeUnpublishedApp(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	recorder := uploadTestApp(t, server, system, "admin", "db", "widgets", testAppBundle(t, "db", "widgets", appsdk.Version(), "draft"), "")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected app create 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if upload := decodeTestJSON[testAppUpload](t, recorder); upload.App.PublishedVersion != 0 {
		t.Fatalf("expected unpublished app, got %#v", upload.App)
	}
	page := appTestRequest(t, server, system, "admin", http.MethodGet, "/apps/db/widgets/", nil)
	if page.Code != http.StatusNotFound || !strings.Contains(page.Body.String(), "Not published") {
		t.Fatalf("expected not-published page 404, got %d: %s", page.Code, page.Body.String())
	}
	if strings.Contains(page.Body.String(), "draft") {
		t.Fatalf("expected unpublished content to stay hidden, got %s", page.Body.String())
	}
	assertServedMarker(t, server, system, "admin", "/apps/db/widgets/@1/", "draft")
}

func TestServeAppWithNonASCIIDatabaseName(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:   "名称",
		Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "备注", Type: "string"}}}},
	}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "admin")
	escaped := url.PathEscape("名称")

	created := createTestApp(t, server, system, "admin", "名称", "widgets")
	if created.App.DatabaseName != "名称" || created.App.URL != "/apps/"+escaped+"/widgets/" || created.Version.PreviewURL != "/apps/"+escaped+"/widgets/@1/" {
		t.Fatalf("expected escaped app urls, got %#v %#v", created.App, created.Version)
	}

	anonymous := appTestRequest(t, server, system, "", http.MethodGet, "/apps/"+escaped+"/widgets/", nil)
	location, err := url.Parse(anonymous.Header().Get("Location"))
	if anonymous.Code != http.StatusFound || err != nil || location.Path != "/login" || location.Query().Get("redirect") != "/apps/"+escaped+"/widgets/" {
		t.Fatalf("unexpected login redirect %d %q", anonymous.Code, anonymous.Header().Get("Location"))
	}

	root := appTestRequest(t, server, system, "admin", http.MethodGet, "/apps/"+escaped+"/widgets", nil)
	if root.Code != http.StatusMovedPermanently || root.Header().Get("Location") != "/apps/"+escaped+"/widgets/" {
		t.Fatalf("expected escaped root redirect, got %d %q", root.Code, root.Header().Get("Location"))
	}
	previewRoot := appTestRequest(t, server, system, "admin", http.MethodGet, "/apps/"+escaped+"/widgets/@1", nil)
	if previewRoot.Code != http.StatusMovedPermanently || previewRoot.Header().Get("Location") != "/apps/"+escaped+"/widgets/@1/" {
		t.Fatalf("expected escaped preview redirect, got %d %q", previewRoot.Code, previewRoot.Header().Get("Location"))
	}

	index := assertServedMarker(t, server, system, "admin", "/apps/"+escaped+"/widgets/orders/1", "version-1")
	if !strings.Contains(index.Body.String(), `<base href="/apps/`+escaped+`/widgets/">`) {
		t.Fatalf("expected escaped base href, got %s", index.Body.String())
	}
	detail := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/"+escaped+"/apps/widgets", nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("expected detail 200, got %d: %s", detail.Code, detail.Body.String())
	}
}

func TestAppSourceDownload(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	created := createTestApp(t, server, system, "admin", "db", "widgets")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "reader", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Open},
		permission.Grant{SubjectID: "legacy-editor", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Write},
	)

	for _, userID := range []string{"reader", "legacy-editor"} {
		denied := appTestRequest(t, server, system, userID, http.MethodGet, "/api/databases/db/apps/widgets/versions/1/source", nil)
		if denied.Code != http.StatusForbidden {
			t.Fatalf("expected %s source 403, got %d: %s", userID, denied.Code, denied.Body.String())
		}
	}
	source := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/apps/widgets/versions/1/source", nil)
	if source.Code != http.StatusOK {
		t.Fatalf("expected source 200, got %d: %s", source.Code, source.Body.String())
	}
	if contentType := source.Header().Get("Content-Type"); contentType != "application/gzip" {
		t.Fatalf("expected gzip content type, got %q", contentType)
	}
	if disposition := source.Header().Get("Content-Disposition"); !strings.Contains(disposition, "widgets-v1-source.tar.gz") {
		t.Fatalf("unexpected content disposition %q", disposition)
	}
	files := readTestTarGz(t, source.Body.Bytes())
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{appbundle.ManifestName, "package.json", "src/main.ts"}) {
		t.Fatalf("unexpected source entries %v", names)
	}
	if files["src/main.ts"] != `export const marker = "version-1";` {
		t.Fatalf("unexpected source file %q", files["src/main.ts"])
	}
	if files[appbundle.ManifestName] != testAppManifest(t, "db", "widgets", appsdk.Version()) {
		t.Fatalf("unexpected manifest %q", files[appbundle.ManifestName])
	}

	missing := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/apps/widgets/versions/9/source", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected missing version source 404, got %d: %s", missing.Code, missing.Body.String())
	}
	invalid := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/apps/widgets/versions/x/source", nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid version source 400, got %d: %s", invalid.Code, invalid.Body.String())
	}
}

func readTestTarGz(t *testing.T, archive []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	files := map[string]string{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = string(body)
	}
}

func TestDeleteAppIsAdminOnly(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	makeTestAdmins(server, "admin")
	created := createTestApp(t, server, system, "admin", "db", "widgets")
	saveTestGrants(t, system, permission.Grant{SubjectID: "reader", Scope: permission.ScopeApp, Resource: resourceID(created.App.ID), Level: permission.Open})
	assertServedMarker(t, server, system, "reader", "/apps/db/widgets/", "version-1")

	readerDelete := appTestRequest(t, server, system, "reader", http.MethodDelete, "/api/databases/db/apps/widgets", nil)
	if readerDelete.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin delete 403, got %d: %s", readerDelete.Code, readerDelete.Body.String())
	}
	adminDelete := appTestRequest(t, server, system, "admin", http.MethodDelete, "/api/databases/db/apps/widgets", nil)
	if adminDelete.Code != http.StatusNoContent {
		t.Fatalf("expected admin delete 204, got %d: %s", adminDelete.Code, adminDelete.Body.String())
	}

	if _, err := system.App(ctx, created.App.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected app removed, got %v", err)
	}
	if _, err := system.AppVersion(ctx, created.App.ID, 1); !errors.Is(err, systemdb.ErrAppVersionNotFound) {
		t.Fatalf("expected versions removed, got %v", err)
	}
	if level := appSubjectLevel(t, system, "reader", permission.ScopeApp, resourceID(created.App.ID)); level != permission.None {
		t.Fatalf("expected app grants removed, got %d", level)
	}
	detail := appTestRequest(t, server, system, "admin", http.MethodGet, "/api/databases/db/apps/widgets", nil)
	if detail.Code != http.StatusNotFound {
		t.Fatalf("expected deleted app detail 404, got %d: %s", detail.Code, detail.Body.String())
	}
	served := appTestRequest(t, server, system, "admin", http.MethodGet, "/apps/db/widgets/", nil)
	if served.Code != http.StatusNotFound {
		t.Fatalf("expected deleted app page 404, got %d: %s", served.Code, served.Body.String())
	}
	again := appTestRequest(t, server, system, "admin", http.MethodDelete, "/api/databases/db/apps/widgets", nil)
	if again.Code != http.StatusNotFound {
		t.Fatalf("expected second delete 404, got %d: %s", again.Code, again.Body.String())
	}

	recreated := uploadTestApp(t, server, system, "admin", "db", "widgets", testAppBundle(t, "db", "widgets", appsdk.Version(), "fresh"), "publish=1")
	if recreated.Code != http.StatusCreated {
		t.Fatalf("expected re-create 201, got %d: %s", recreated.Code, recreated.Body.String())
	}
	if upload := decodeTestJSON[testAppUpload](t, recreated); upload.Version.Version != 1 {
		t.Fatalf("expected re-created app to restart at version 1, got %#v", upload.Version)
	}
	assertServedMarker(t, server, system, "admin", "/apps/db/widgets/", "fresh")
}

func TestAppSDKEndpointAndHeaderGate(t *testing.T) {
	server, system := newTestServer(t)

	anonymous := appTestRequest(t, server, system, "", http.MethodGet, "/api/apps/sdk", nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous sdk 401, got %d", anonymous.Code)
	}
	recorder := appTestRequest(t, server, system, "u1", http.MethodGet, "/api/apps/sdk", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected sdk 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	sdk := decodeTestJSON[struct {
		Version  string            `json:"version"`
		MinMajor int               `json:"min_major"`
		Files    map[string]string `json:"files"`
	}](t, recorder)
	if sdk.Version != appsdk.Version() || sdk.MinMajor != appsdk.MinMajor {
		t.Fatalf("unexpected sdk response %#v", sdk)
	}
	if _, ok := sdk.Files["autable.ts"]; !ok {
		t.Fatalf("expected autable.ts in sdk files, got %v", sdk.Files)
	}

	withHeader := func(userID, target, version string) *httptest.ResponseRecorder {
		t.Helper()
		request := newTestRequest(http.MethodGet, target, nil)
		if userID != "" {
			request.AddCookie(testSessionCookie(t, system, userID))
		}
		request.Header.Set(appsdk.Header, version)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		return recorder
	}
	for _, version := range []string{"0.9", "2.0", "garbage"} {
		if recorder := withHeader("u1", "/api/auth/me", version); recorder.Code != http.StatusGone {
			t.Fatalf("expected sdk %s 410, got %d: %s", version, recorder.Code, recorder.Body.String())
		}
	}
	if recorder := withHeader("", "/api/metadata", "0.9"); recorder.Code != http.StatusGone {
		t.Fatalf("expected anonymous old sdk 410, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := withHeader("u1", "/api/auth/me", "1.0"); recorder.Code != http.StatusOK {
		t.Fatalf("expected sdk 1.0 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := withHeader("u1", "/api/auth/me", appsdk.Version()); recorder.Code != http.StatusOK {
		t.Fatalf("expected current sdk 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestInjectBase(t *testing.T) {
	cases := []struct {
		name, index, want string
	}{
		{"after head", `<!doctype html><html><head><title>x</title></head><body></body></html>`, `<!doctype html><html><head><base href="/b/"><title>x</title></head><body></body></html>`},
		{"head with attributes", `<HTML><HEAD lang="en"><title>x</title>`, `<HTML><HEAD lang="en"><base href="/b/"><title>x</title>`},
		{"header is not head", `<!doctype html><html lang="en"><body><header>x</header>`, `<!doctype html><html lang="en"><base href="/b/"><body><header>x</header>`},
		{"after doctype", `<!DOCTYPE html><p>x</p>`, `<!DOCTYPE html><base href="/b/"><p>x</p>`},
		{"bare fragment", `<p>x</p>`, `<base href="/b/"><p>x</p>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(injectBase([]byte(tc.index), "/b/")); got != tc.want {
				t.Fatalf("injectBase:\n got %s\nwant %s", got, tc.want)
			}
		})
	}
	if got := string(injectBase([]byte(`<head>`), `/apps/a"b/`)); got != `<head><base href="/apps/a&#34;b/">` {
		t.Fatalf("expected escaped base, got %s", got)
	}
}
