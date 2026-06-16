package api

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codetable/internal/config"
	"codetable/internal/history"
	"codetable/internal/metadata"
	"codetable/internal/permission"
	"codetable/internal/recorddb"
	"codetable/internal/systemdb"
	"codetable/internal/table"
	"codetable/internal/workflow"
)

func TestPasswordAuthSessionLifecycle(t *testing.T) {
	server, _ := newTestServer(t)

	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{
		"email":"Person@Example.com",
		"password":"correct horse"
	}`))
	registerRecorder := httptest.NewRecorder()
	server.ServeHTTP(registerRecorder, register)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("expected register 201, got %d: %s", registerRecorder.Code, registerRecorder.Body.String())
	}
	cookie := sessionCookie(t, registerRecorder)
	if !cookie.HttpOnly || cookie.Value == "" {
		t.Fatalf("expected HttpOnly session cookie, got %#v", cookie)
	}
	var registered userResponse
	if err := json.NewDecoder(registerRecorder.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	if registered.Email != "person@example.com" || registered.Provider != "password" {
		t.Fatalf("unexpected registered user: %#v", registered)
	}

	me := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	me.AddCookie(cookie)
	meRecorder := httptest.NewRecorder()
	server.ServeHTTP(meRecorder, me)
	if meRecorder.Code != http.StatusOK {
		t.Fatalf("expected me 200, got %d: %s", meRecorder.Code, meRecorder.Body.String())
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logout.AddCookie(cookie)
	logoutRecorder := httptest.NewRecorder()
	server.ServeHTTP(logoutRecorder, logout)
	if logoutRecorder.Code != http.StatusOK {
		t.Fatalf("expected logout 200, got %d: %s", logoutRecorder.Code, logoutRecorder.Body.String())
	}

	afterLogout := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	afterLogout.AddCookie(cookie)
	afterLogoutRecorder := httptest.NewRecorder()
	server.ServeHTTP(afterLogoutRecorder, afterLogout)
	if afterLogoutRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected me 401 after logout, got %d: %s", afterLogoutRecorder.Code, afterLogoutRecorder.Body.String())
	}
}

func TestLoginRejectsInvalidPassword(t *testing.T) {
	server, _ := newTestServer(t)
	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{
		"email":"person@example.com",
		"password":"correct horse"
	}`))
	server.ServeHTTP(httptest.NewRecorder(), register)

	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{
		"email":"person@example.com",
		"password":"wrong horse"
	}`))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, login)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected login 401, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestOIDCProvidersExposePublicConfig(t *testing.T) {
	server, _ := newTestServerWithOIDC(t, []config.OIDCProvider{
		{
			Name:         "main",
			IssuerURL:    "https://issuer.example",
			ClientID:     "codetable",
			ClientSecret: "secret",
			Scopes:       []string{"email"},
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/providers", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected providers 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var providers []map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&providers); err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 {
		t.Fatalf("expected one provider, got %#v", providers)
	}
	if providers[0]["name"] != "main" || providers[0]["issuer_url"] != "https://issuer.example" {
		t.Fatalf("unexpected provider response: %#v", providers[0])
	}
	if _, ok := providers[0]["client_secret"]; ok {
		t.Fatalf("provider response leaked client_secret: %#v", providers[0])
	}
	scopes, ok := providers[0]["scopes"].([]any)
	if !ok || len(scopes) != 2 || scopes[0] != "openid" || scopes[1] != "email" {
		t.Fatalf("expected openid to be prepended to custom scopes, got %#v", providers[0]["scopes"])
	}
}

func TestOIDCStartRedirectsToAuthorizeEndpoint(t *testing.T) {
	server, _ := newTestServerWithOIDC(t, []config.OIDCProvider{
		{
			Name:      "main",
			IssuerURL: "https://issuer.example/",
			ClientID:  "codetable",
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/main/start", nil)
	request.Host = "app.example"
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("expected start 302, got %d: %s", recorder.Code, recorder.Body.String())
	}

	location := recorder.Header().Get("Location")
	authorizeURL, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	if authorizeURL.Scheme != "https" || authorizeURL.Host != "issuer.example" || authorizeURL.Path != "/authorize" {
		t.Fatalf("unexpected authorize url: %s", location)
	}
	query := authorizeURL.Query()
	if query.Get("response_type") != "code" || query.Get("client_id") != "codetable" {
		t.Fatalf("unexpected authorize query: %s", authorizeURL.RawQuery)
	}
	if query.Get("scope") != "openid email profile" {
		t.Fatalf("unexpected default scopes: %q", query.Get("scope"))
	}
	if query.Get("redirect_uri") != "https://app.example/api/auth/oidc/main/callback" {
		t.Fatalf("unexpected redirect_uri: %q", query.Get("redirect_uri"))
	}
	if query.Get("state") == "" {
		t.Fatal("expected non-empty state")
	}

	cookie := oidcStateCookie(t, recorder)
	if !cookie.HttpOnly || cookie.Path != "/api/auth/oidc" {
		t.Fatalf("unexpected oidc state cookie: %#v", cookie)
	}
	if cookie.Value != "main:"+query.Get("state") {
		t.Fatalf("expected state cookie to match redirect state, got %q and %q", cookie.Value, query.Get("state"))
	}
}

func TestOIDCCallbackCreatesSessionWithVerifiedIDToken(t *testing.T) {
	issuer := newFakeOIDCIssuer(t, "codetable", "sub-123", "Person@Example.com")
	defer issuer.Close()
	server, _ := newTestServerWithOIDC(t, []config.OIDCProvider{
		{
			Name:         "main",
			IssuerURL:    issuer.URL,
			ClientID:     "codetable",
			ClientSecret: "secret",
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/main/callback?code=ok&state=state-1", nil)
	request.Host = "app.example"
	request.AddCookie(&http.Cookie{Name: oidcStateCookieName, Value: "main:state-1"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("expected callback 302, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Location") != "/" {
		t.Fatalf("expected callback to redirect to app root, got %q", recorder.Header().Get("Location"))
	}
	cookie := sessionCookie(t, recorder)
	if !cookie.HttpOnly || cookie.Value == "" {
		t.Fatalf("expected session cookie, got %#v", cookie)
	}

	me := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	me.AddCookie(cookie)
	meRecorder := httptest.NewRecorder()
	server.ServeHTTP(meRecorder, me)
	if meRecorder.Code != http.StatusOK {
		t.Fatalf("expected me 200 after oidc callback, got %d: %s", meRecorder.Code, meRecorder.Body.String())
	}
	var user userResponse
	if err := json.NewDecoder(meRecorder.Body).Decode(&user); err != nil {
		t.Fatal(err)
	}
	if user.Email != "person@example.com" || user.Provider != "oidc" {
		t.Fatalf("unexpected oidc user: %#v", user)
	}
}

func TestCreateRowAPIUsesSessionUser(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{
		"email":"person@example.com",
		"password":"correct horse"
	}`))
	registerRecorder := httptest.NewRecorder()
	server.ServeHTTP(registerRecorder, register)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("expected register 201, got %d: %s", registerRecorder.Code, registerRecorder.Body.String())
	}
	cookie := sessionCookie(t, registerRecorder)
	var user userResponse
	if err := json.NewDecoder(registerRecorder.Body).Decode(&user); err != nil {
		t.Fatal(err)
	}
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: user.ID,
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateRowAPIEnforcesPermissionsAndWritesHistory(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	grant := permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}
	if err := system.SaveGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"values":{"name":"Ada","email":"ada@example.com"}}`)
	request := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", body)
	request.Header.Set("X-Codetable-User", "u1")
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var row rowResponse
	if err := json.NewDecoder(recorder.Body).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row.RecordID != 1 {
		t.Fatalf("expected record_id 1, got %d", row.RecordID)
	}

	historyRequest := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	historyRecorder := httptest.NewRecorder()
	server.ServeHTTP(historyRecorder, historyRequest)
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", historyRecorder.Code, historyRecorder.Body.String())
	}

	rawHistory := historyRecorder.Body.Bytes()
	var changes []history.RowChange
	if err := json.Unmarshal(rawHistory, &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Values["name"] != "Ada" {
		t.Fatalf("unexpected row history: %#v", changes)
	}
	var historyEntries []rowHistoryResponse
	if err := json.Unmarshal(rawHistory, &historyEntries); err != nil {
		t.Fatal(err)
	}
	if len(historyEntries) != 1 || !strings.HasPrefix(historyEntries[0].HistoryKey, "rhistory_db_contacts_00000000000000000001_") {
		t.Fatalf("expected row history key in response, got %#v", historyEntries)
	}
}

func TestUpdateRowAPIEnforcesPermissionsAndWritesHistory(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	createRequest := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Ada","email":"ada@example.com"}
	}`))
	createRequest.Header.Set("X-Codetable-User", "u1")
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	updateRequest := httptest.NewRequest(http.MethodPatch, "/api/tables/db/contacts/rows/1", bytes.NewBufferString(`{
		"values":{"email":"ada@codetable.test"}
	}`))
	updateRequest.Header.Set("X-Codetable-User", "u1")
	updateRecorder := httptest.NewRecorder()
	server.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updated rowResponse
	if err := json.NewDecoder(updateRecorder.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Values["name"] != "Ada" || updated.Values["email"] != "ada@codetable.test" {
		t.Fatalf("unexpected updated row: %#v", updated)
	}

	historyRequest := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	historyRecorder := httptest.NewRecorder()
	server.ServeHTTP(historyRecorder, historyRequest)
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("expected history 200, got %d: %s", historyRecorder.Code, historyRecorder.Body.String())
	}
	var changes []history.RowChange
	if err := json.NewDecoder(historyRecorder.Body).Decode(&changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected create and update history entries, got %#v", changes)
	}
	if changes[1].Values["email"] != "ada@codetable.test" || changes[1].Values["name"] != "Ada" {
		t.Fatalf("unexpected update history: %#v", changes[1])
	}
}

func TestPermissionGrantAPIRequiresAuthentication(t *testing.T) {
	server, _ := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"table",
		"resource":"db.contacts",
		"level":2
	}`))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated grant save 401, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPermissionGrantAPISavesAuthenticatedGrant(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"field",
		"resource":"db.contacts",
		"field":"email",
		"level":2
	}`))
	request.Header.Set("X-Codetable-User", "admin")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected authenticated grant save 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	perms, err := system.GrantsForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanWriteField("u1", "db.contacts", "email") {
		t.Fatal("expected API grant to persist field write permission")
	}
}

func TestCreateDatabaseAPIWritesMetadataAndGrantsOwner(t *testing.T) {
	ctx := context.Background()
	server, system, metadataPath := newTestServerWithMetadataFile(t, metadata.Catalog{})
	request := httptest.NewRequest(http.MethodPost, "/api/databases", bytes.NewBufferString(`{
		"name":"sales",
		"sqlite_path":"./data/sales.sqlite"
	}`))
	request.Header.Set("X-Codetable-User", "owner")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected database create 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	loaded, err := metadata.Load(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	db, ok := loaded.Database("sales")
	if !ok || db.SQLitePath != "./data/sales.sqlite" {
		t.Fatalf("expected sales database in metadata, got %#v", loaded)
	}
	perms, err := system.GrantsForSubject(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanWriteResource("owner", permission.ScopeDatabase, "sales") {
		t.Fatal("expected database creator to receive database write permission")
	}
}

func TestDatabaseOwnerCanCreateTable(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: "./data/workspace.sqlite"}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeDatabase,
		Resource:  "workspace",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/databases/workspace/tables", bytes.NewBufferString(`{
		"name":"contacts",
		"display_name":"Contacts",
		"fields":[{"name":"name","type":"text","required":true},{"name":"email","type":"email"}],
		"views":[]
	}`))
	request.Header.Set("X-Codetable-User", "owner")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected table create 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	loaded, err := metadata.Load(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	tableMeta, ok := loaded.Table("workspace", "contacts")
	if !ok || tableMeta.Fields[0].Name != "name" {
		t.Fatalf("expected contacts table in metadata, got %#v", loaded)
	}
	perms, err := system.GrantsForSubject(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanWriteField("owner", "workspace.contacts", "name") {
		t.Fatal("expected table creator to receive table write permission")
	}
}

func TestDatabaseOwnerCanManageRoles(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: "./data/workspace.sqlite"}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeDatabase,
		Resource:  "workspace",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	createRole := httptest.NewRequest(http.MethodPost, "/api/databases/workspace/roles", bytes.NewBufferString(`{
		"name":"editor"
	}`))
	createRole.Header.Set("X-Codetable-User", "owner")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, createRole)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected role create 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	updateGrants := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/roles/editor/grants", bytes.NewBufferString(`{
		"grants":[
			{"scope":"table","resource":"workspace.contacts","level":2},
			{"scope":"field","resource":"workspace.contacts","field":"email","level":1},
			{"scope":"form","resource":"3","level":0}
		]
	}`))
	updateGrants.Header.Set("X-Codetable-User", "owner")
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, updateGrants)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected role grants update 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	rolesRequest := httptest.NewRequest(http.MethodGet, "/api/databases/workspace/roles", nil)
	rolesRequest.Header.Set("X-Codetable-User", "owner")
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, rolesRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected role list 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var roles []systemdb.RoleDefinition
	if err := json.Unmarshal(recorder.Body.Bytes(), &roles); err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].SubjectID != "role:workspace:editor" || len(roles[0].Grants) != 2 {
		t.Fatalf("unexpected roles response: %#v", roles)
	}
}

func TestRoleManagementRequiresDatabaseWrite(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: "./data/workspace.sqlite"}}}
	server, _, _ := newTestServerWithMetadataFile(t, catalog)

	request := httptest.NewRequest(http.MethodPost, "/api/databases/workspace/roles", bytes.NewBufferString(`{
		"name":"viewer"
	}`))
	request.Header.Set("X-Codetable-User", "viewer")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected role create forbidden, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestNonDatabaseOwnerCannotCreateTable(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: "./data/workspace.sqlite"}}}
	server, _, _ := newTestServerWithMetadataFile(t, catalog)
	request := httptest.NewRequest(http.MethodPost, "/api/databases/workspace/tables", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[{"name":"name","type":"text"}]
	}`))
	request.Header.Set("X-Codetable-User", "other")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-owner table create 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestListRowsAPIAppliesView(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"values":{"name":"Ada","email":"ada@example.com","status":"active"}}`,
		`{"values":{"name":"Grace","email":"grace@example.com","status":"active"}}`,
		`{"values":{"name":"Linus","email":"linus@example.com","status":"archived"}}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(body))
		request.Header.Set("X-Codetable-User", "u1")
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows?view=active-a", nil)
	request.Header.Set("X-Codetable-User", "u1")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var rows []rowResponse
	if err := json.NewDecoder(recorder.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two view rows, got %#v", rows)
	}
	if rows[0].Values["name"] != "Grace" || rows[1].Values["name"] != "Ada" {
		t.Fatalf("unexpected view order: %#v", rows)
	}
}

func TestCreateRowAPIDeniesMissingWritePermission(t *testing.T) {
	server, _ := newTestServer(t)
	body := bytes.NewBufferString(`{"values":{"name":"Ada"}}`)
	request := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", body)
	request.Header.Set("X-Codetable-User", "u1")
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateRowAPICanUsePersistentRepository(t *testing.T) {
	ctx := context.Background()
	system, err := systemdb.Open(ctx, filepath.Join(t.TempDir(), "system.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := system.Close(); err != nil {
			t.Fatal(err)
		}
	})

	catalog := testCatalog(filepath.Join(t.TempDir(), "workspace.sqlite"))
	repository, err := recorddb.OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	historyStore := history.NewMemoryStore()
	server := NewServer(catalog, system, table.NewServiceWithRepository(historyStore, repository), historyStore)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
	request.Header.Set("X-Codetable-User", "u1")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	rows, err := repository.Rows(ctx, "db", "contacts")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["name"] != "Ada" {
		t.Fatalf("unexpected persisted API rows: %#v", rows)
	}
}

func TestWorkflowAndFormAPI(t *testing.T) {
	server, _ := newTestServer(t)

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"notify",
		"script":"export default async function run() {}",
		"secrets":{"TOKEN":"secret"},
		"variables":{"CHANNEL":"ops"}
	}`))
	workflowRequest.Header.Set("X-Codetable-User", "u1")
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}

	var workflow systemdb.WorkflowDefinition
	if err := json.NewDecoder(workflowRecorder.Body).Decode(&workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.DatabaseName != "db" {
		t.Fatalf("expected db-level workflow, got %#v", workflow)
	}
	getWorkflow := httptest.NewRequest(http.MethodGet, "/api/workflows/1", nil)
	getWorkflow.Header.Set("X-Codetable-User", "u1")
	getWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(getWorkflowRecorder, getWorkflow)
	if getWorkflowRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow 200, got %d: %s", getWorkflowRecorder.Code, getWorkflowRecorder.Body.String())
	}
	listWorkflows := httptest.NewRequest(http.MethodGet, "/api/databases/db/workflows", nil)
	listWorkflows.Header.Set("X-Codetable-User", "u1")
	listWorkflowsRecorder := httptest.NewRecorder()
	server.ServeHTTP(listWorkflowsRecorder, listWorkflows)
	if listWorkflowsRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow list 200, got %d: %s", listWorkflowsRecorder.Code, listWorkflowsRecorder.Body.String())
	}
	var workflows []systemdb.WorkflowDefinition
	if err := json.NewDecoder(listWorkflowsRecorder.Body).Decode(&workflows); err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || workflows[0].ID != workflow.ID {
		t.Fatalf("unexpected workflow list: %#v", workflows)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"contact-intake",
		"script":"root.append(api.input({ name: 'email' }))"
	}`))
	formRequest.Header.Set("X-Codetable-User", "u1")
	formRecorder := httptest.NewRecorder()
	server.ServeHTTP(formRecorder, formRequest)
	if formRecorder.Code != http.StatusCreated {
		t.Fatalf("expected form 201, got %d: %s", formRecorder.Code, formRecorder.Body.String())
	}

	var form systemdb.FormDefinition
	if err := json.NewDecoder(formRecorder.Body).Decode(&form); err != nil {
		t.Fatal(err)
	}
	if form.DatabaseName != "db" {
		t.Fatalf("expected db-level form, got %#v", form)
	}
	listForms := httptest.NewRequest(http.MethodGet, "/api/databases/db/forms", nil)
	listForms.Header.Set("X-Codetable-User", "u1")
	listFormsRecorder := httptest.NewRecorder()
	server.ServeHTTP(listFormsRecorder, listForms)
	if listFormsRecorder.Code != http.StatusOK {
		t.Fatalf("expected form list 200, got %d: %s", listFormsRecorder.Code, listFormsRecorder.Body.String())
	}
	var forms []systemdb.FormDefinition
	if err := json.NewDecoder(listFormsRecorder.Body).Decode(&forms); err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 || forms[0].ID != form.ID {
		t.Fatalf("unexpected form list: %#v", forms)
	}
	if workflow.ID != 1 || form.ID != 1 {
		t.Fatalf("expected autoincrement ids, got workflow=%d form=%d", workflow.ID, form.ID)
	}
}

func TestWorkflowRunAPI(t *testing.T) {
	server, _ := newTestServer(t)

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"welcome",
		"script":"function run(info) { const echoed = info.node(\"echo\", { value: info.inputs.name }); return { message: echoed.value + \"-\" + info.variables.suffix }; }",
		"variables":{"suffix":"done"}
	}`))
	workflowRequest.Header.Set("X-Codetable-User", "u1")
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	var saved systemdb.WorkflowDefinition
	if err := json.NewDecoder(workflowRecorder.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}

	runRequest := httptest.NewRequest(http.MethodPost, "/api/workflows/1/runs", bytes.NewBufferString(`{"inputs":{"name":"Ada"}}`))
	runRequest.Header.Set("X-Codetable-User", "u1")
	runRecorder := httptest.NewRecorder()
	server.ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow run 201, got %d: %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runResponse workflowRunResponse
	if err := json.NewDecoder(runRecorder.Body).Decode(&runResponse); err != nil {
		t.Fatal(err)
	}
	if runResponse.HistoryKey == "" || runResponse.Run.Outputs["message"] != "Ada-done" {
		t.Fatalf("unexpected workflow run response: %#v", runResponse)
	}
	if len(runResponse.Run.Steps) != 1 || runResponse.Run.Steps[0].NodeID != "echo" {
		t.Fatalf("unexpected workflow run steps: %#v", runResponse.Run.Steps)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/workflows/1/runs", nil)
	listRequest.Header.Set("X-Codetable-User", "u1")
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow run list 200, got %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var runs []workflowRunResponse
	if err := json.NewDecoder(listRecorder.Body).Decode(&runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].HistoryKey != runResponse.HistoryKey {
		t.Fatalf("unexpected workflow run list: %#v", runs)
	}
}

func TestWorkflowNodesAPI(t *testing.T) {
	server, _ := newTestServer(t)

	request := httptest.NewRequest(http.MethodGet, "/api/workflow/nodes", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected workflow nodes 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var nodes []workflow.NodeInfo
	if err := json.NewDecoder(recorder.Body).Decode(&nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Type != "echo" || nodes[1].Type != "table.record.changed" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
	if !nodes[1].Trigger || len(nodes[1].Inputs) == 0 || len(nodes[1].Outputs) == 0 {
		t.Fatalf("expected trigger node ports: %#v", nodes[1])
	}
}

func TestWorkflowRunAPIWithRecordChangedTrigger(t *testing.T) {
	ctx := context.Background()
	server, _ := newTestServer(t)
	historyKey, err := history.SaveRowChange(ctx, server.history, history.RowChange{
		Database: "db",
		Table:    "contacts",
		RecordID: 9,
		Values:   map[string]any{"name": "Ada"},
		ActorID:  "u1",
	})
	if err != nil {
		t.Fatal(err)
	}

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"triggered",
		"script":"function run(info) { const changed = info.node(\"table.record.changed\", { history_key: info.inputs.history_key }); return { record_id: changed.record.record_id, name: changed.values.name }; }"
	}`))
	workflowRequest.Header.Set("X-Codetable-User", "u1")
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}

	runRequest := httptest.NewRequest(http.MethodPost, "/api/workflows/1/runs", bytes.NewBufferString(`{"inputs":{"history_key":"`+historyKey+`"}}`))
	runRequest.Header.Set("X-Codetable-User", "u1")
	runRecorder := httptest.NewRecorder()
	server.ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow run 201, got %d: %s", runRecorder.Code, runRecorder.Body.String())
	}
	var response workflowRunResponse
	if err := json.NewDecoder(runRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Run.Outputs["record_id"] != float64(9) || response.Run.Outputs["name"] != "Ada" {
		t.Fatalf("unexpected trigger outputs: %#v", response.Run.Outputs)
	}
	if len(response.Run.Steps) != 1 || response.Run.Steps[0].NodeID != "table.record.changed" {
		t.Fatalf("unexpected trigger steps: %#v", response.Run.Steps)
	}
}

func TestWorkflowAndFormPermissions(t *testing.T) {
	server, system := newTestServer(t)

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"restricted",
		"script":"function run(info) { return info.inputs; }"
	}`))
	workflowRequest.Header.Set("X-Codetable-User", "owner")
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"restricted-form",
		"script":"root.append(api.input({ name: 'email' }))"
	}`))
	formRequest.Header.Set("X-Codetable-User", "owner")
	formRecorder := httptest.NewRecorder()
	server.ServeHTTP(formRecorder, formRequest)
	if formRecorder.Code != http.StatusCreated {
		t.Fatalf("expected form 201, got %d: %s", formRecorder.Code, formRecorder.Body.String())
	}

	otherWorkflow := httptest.NewRequest(http.MethodGet, "/api/workflows/1", nil)
	otherWorkflow.Header.Set("X-Codetable-User", "other")
	otherWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(otherWorkflowRecorder, otherWorkflow)
	if otherWorkflowRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected workflow 403, got %d: %s", otherWorkflowRecorder.Code, otherWorkflowRecorder.Body.String())
	}

	otherForms := httptest.NewRequest(http.MethodGet, "/api/databases/db/forms", nil)
	otherForms.Header.Set("X-Codetable-User", "other")
	otherFormsRecorder := httptest.NewRecorder()
	server.ServeHTTP(otherFormsRecorder, otherForms)
	if otherFormsRecorder.Code != http.StatusOK {
		t.Fatalf("expected form list 200, got %d: %s", otherFormsRecorder.Code, otherFormsRecorder.Body.String())
	}
	var forms []systemdb.FormDefinition
	if err := json.NewDecoder(otherFormsRecorder.Body).Decode(&forms); err != nil {
		t.Fatal(err)
	}
	if len(forms) != 0 {
		t.Fatalf("expected unreadable forms to be filtered, got %#v", forms)
	}

	if err := system.SaveGrant(context.Background(), permission.Grant{
		SubjectID: "other",
		Scope:     permission.ScopeWorkflow,
		Resource:  "1",
		Level:     permission.Read,
	}); err != nil {
		t.Fatal(err)
	}
	readWorkflow := httptest.NewRequest(http.MethodGet, "/api/workflows/1", nil)
	readWorkflow.Header.Set("X-Codetable-User", "other")
	readWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(readWorkflowRecorder, readWorkflow)
	if readWorkflowRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow 200, got %d: %s", readWorkflowRecorder.Code, readWorkflowRecorder.Body.String())
	}

	runWorkflow := httptest.NewRequest(http.MethodPost, "/api/workflows/1/runs", bytes.NewBufferString(`{"inputs":{"name":"Ada"}}`))
	runWorkflow.Header.Set("X-Codetable-User", "other")
	runWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(runWorkflowRecorder, runWorkflow)
	if runWorkflowRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected read-only workflow run 403, got %d: %s", runWorkflowRecorder.Code, runWorkflowRecorder.Body.String())
	}
}

func newTestServer(t *testing.T) (*Server, *systemdb.DB) {
	t.Helper()
	return newTestServerWithOIDC(t, nil)
}

func newTestServerWithOIDC(t *testing.T, providers []config.OIDCProvider) (*Server, *systemdb.DB) {
	t.Helper()
	system, err := systemdb.Open(context.Background(), filepath.Join(t.TempDir(), "system.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := system.Close(); err != nil {
			t.Fatal(err)
		}
	})
	historyStore := history.NewMemoryStore()
	catalog := testCatalog("./db.sqlite")
	return NewServerWithOIDCProviders(catalog, system, table.NewService(historyStore), historyStore, providers), system
}

func newTestServerWithMetadataFile(t *testing.T, catalog metadata.Catalog) (*Server, *systemdb.DB, string) {
	t.Helper()
	system, err := systemdb.Open(context.Background(), filepath.Join(t.TempDir(), "system.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := system.Close(); err != nil {
			t.Fatal(err)
		}
	})
	metadataPath := filepath.Join(t.TempDir(), "metadata", "main.yml")
	if err := metadata.Save(metadataPath, catalog); err != nil {
		t.Fatal(err)
	}
	historyStore := history.NewMemoryStore()
	server := NewServer(catalog, system, table.NewService(historyStore), historyStore)
	server.EnableMetadataWrites(metadataPath)
	return server, system, metadataPath
}

func testCatalog(sqlitePath string) metadata.Catalog {
	return metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: sqlitePath,
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "text", Required: true},
				{Name: "email", Type: "email"},
				{Name: "status", Type: "text"},
			},
			Views: []metadata.View{
				{
					Name:    "active",
					Filters: []metadata.ViewFilter{{Field: "status", Op: "eq", Value: "active"}},
				},
				{
					Name:     "active-a",
					BaseView: "active",
					Filters:  []metadata.ViewFilter{{Field: "name", Op: "contains", Value: "a"}},
					Sorts:    []metadata.ViewSort{{Field: "name", Direction: "desc"}},
				},
			},
		}},
	}}}
}

func sessionCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	t.Fatalf("missing session cookie in Set-Cookie headers: %s", strings.Join(recorder.Result().Header.Values("Set-Cookie"), ", "))
	return nil
}

func oidcStateCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == oidcStateCookieName {
			return cookie
		}
	}
	t.Fatalf("missing oidc state cookie in Set-Cookie headers: %s", strings.Join(recorder.Result().Header.Values("Set-Cookie"), ", "))
	return nil
}

func newFakeOIDCIssuer(t *testing.T, clientID, subject, email string) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(t, w, map[string]any{
				"issuer":                                issuer.URL,
				"authorization_endpoint":                issuer.URL + "/authorize",
				"token_endpoint":                        issuer.URL + "/token",
				"jwks_uri":                              issuer.URL + "/jwks",
				"userinfo_endpoint":                     issuer.URL + "/userinfo",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/token":
			if r.Method != http.MethodPost {
				t.Fatalf("expected token exchange to use POST, got %s", r.Method)
			}
			writeTestJSON(t, w, map[string]any{
				"access_token": "access-token",
				"token_type":   "Bearer",
				"id_token":     signTestIDToken(t, key, issuer.URL, clientID, subject, email),
			})
		case "/jwks":
			writeTestJSON(t, w, map[string]any{
				"keys": []map[string]any{
					{
						"kty": "RSA",
						"use": "sig",
						"kid": "test-key",
						"alg": "RS256",
						"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
						"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
					},
				},
			})
		case "/userinfo":
			writeTestJSON(t, w, map[string]any{
				"email":          email,
				"email_verified": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return issuer
}

func signTestIDToken(t *testing.T, key *rsa.PrivateKey, issuer, audience, subject, email string) string {
	t.Helper()
	header := map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"}
	now := time.Now().UTC()
	payload := map[string]any{
		"iss":            issuer,
		"sub":            subject,
		"aud":            audience,
		"exp":            now.Add(time.Hour).Unix(),
		"iat":            now.Add(-time.Minute).Unix(),
		"email":          email,
		"email_verified": true,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	sum := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
