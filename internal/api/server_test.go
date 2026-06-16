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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codetable/internal/auth"
	"codetable/internal/codefiles"
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
	request.AddCookie(testSessionCookie(t, system, "u1"))
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
	historyRequest.AddCookie(testSessionCookie(t, system, "u1"))
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
	createRequest.AddCookie(testSessionCookie(t, system, "u1"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	updateRequest := httptest.NewRequest(http.MethodPatch, "/api/tables/db/contacts/rows/1", bytes.NewBufferString(`{
		"values":{"email":"ada@codetable.test"}
	}`))
	updateRequest.AddCookie(testSessionCookie(t, system, "u1"))
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
	historyRequest.AddCookie(testSessionCookie(t, system, "u1"))
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

func TestDeleteRowAPIEnforcesPermissionsAndWritesHistory(t *testing.T) {
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
	createRequest.AddCookie(testSessionCookie(t, system, "u1"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/tables/db/contacts/rows/1", nil)
	deleteRequest.AddCookie(testSessionCookie(t, system, "u1"))
	deleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	rowsRequest := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows", nil)
	rowsRequest.AddCookie(testSessionCookie(t, system, "u1"))
	rowsRecorder := httptest.NewRecorder()
	server.ServeHTTP(rowsRecorder, rowsRequest)
	if rowsRecorder.Code != http.StatusOK {
		t.Fatalf("expected row list 200, got %d: %s", rowsRecorder.Code, rowsRecorder.Body.String())
	}
	var rows []rowResponse
	if err := json.NewDecoder(rowsRecorder.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected deleted row to disappear, got %#v", rows)
	}

	historyRequest := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	historyRequest.AddCookie(testSessionCookie(t, system, "u1"))
	historyRecorder := httptest.NewRecorder()
	server.ServeHTTP(historyRecorder, historyRequest)
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("expected history 200, got %d: %s", historyRecorder.Code, historyRecorder.Body.String())
	}
	var changes []history.RowChange
	if err := json.NewDecoder(historyRecorder.Body).Decode(&changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[1].Operation != "delete" || changes[1].Values["name"] != "Ada" {
		t.Fatalf("unexpected delete history: %#v", changes)
	}
}

func TestRowHistoryAPIRequiresAuthAndFiltersReadableFields(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "writer",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	createRequest := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Ada","email":"ada@example.com","status":"active"}
	}`))
	createRequest.AddCookie(testSessionCookie(t, system, "writer"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	anonymousHistory := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	anonymousRecorder := httptest.NewRecorder()
	server.ServeHTTP(anonymousRecorder, anonymousHistory)
	if anonymousRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous history 401, got %d: %s", anonymousRecorder.Code, anonymousRecorder.Body.String())
	}

	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "reader",
		Scope:     permission.ScopeField,
		Resource:  "db.contacts",
		Field:     "email",
		Level:     permission.Read,
	}); err != nil {
		t.Fatal(err)
	}
	readerHistory := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	readerHistory.AddCookie(testSessionCookie(t, system, "reader"))
	readerRecorder := httptest.NewRecorder()
	server.ServeHTTP(readerRecorder, readerHistory)
	if readerRecorder.Code != http.StatusOK {
		t.Fatalf("expected reader history 200, got %d: %s", readerRecorder.Code, readerRecorder.Body.String())
	}
	var changes []history.RowChange
	if err := json.NewDecoder(readerRecorder.Body).Decode(&changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected one readable history entry, got %#v", changes)
	}
	if changes[0].Values["email"] != "ada@example.com" {
		t.Fatalf("expected readable email in history, got %#v", changes[0].Values)
	}
	if _, ok := changes[0].Values["name"]; ok {
		t.Fatalf("history leaked unreadable name field: %#v", changes[0].Values)
	}

	deniedHistory := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	deniedHistory.AddCookie(testSessionCookie(t, system, "denied"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, deniedHistory)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected denied history 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
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

func TestPermissionGrantAPIRequiresDatabaseWrite(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	denied := httptest.NewRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"field",
		"resource":"db.contacts",
		"field":"email",
		"level":2
	}`))
	denied.AddCookie(testSessionCookie(t, system, "viewer"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, denied)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-owner grant save 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}

	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "admin",
		Scope:     permission.ScopeDatabase,
		Resource:  "db",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	allowed := httptest.NewRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"field",
		"resource":"db.contacts",
		"field":"email",
		"level":2
	}`))
	allowed.AddCookie(testSessionCookie(t, system, "admin"))
	allowedRecorder := httptest.NewRecorder()
	server.ServeHTTP(allowedRecorder, allowed)
	if allowedRecorder.Code != http.StatusCreated {
		t.Fatalf("expected db owner grant save 201, got %d: %s", allowedRecorder.Code, allowedRecorder.Body.String())
	}

	perms, err := system.GrantsForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanWriteField("u1", "db.contacts", "email") {
		t.Fatal("expected API grant to persist field write permission")
	}
}

func TestMetadataAPIOnlyReturnsVisibleDatabasesAndTables(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{
			Name:       "workspace",
			SQLitePath: "./data/workspace.sqlite",
			Tables: []metadata.Table{
				{
					Name: "contacts",
					Fields: []metadata.Field{
						{Name: "name", Type: "text"},
						{Name: "email", Type: "email"},
						{Name: "status", Type: "text"},
					},
					Views: []metadata.View{
						{Name: "by-email", Filters: []metadata.ViewFilter{{Field: "email", Op: "not_empty"}}},
						{Name: "active", Filters: []metadata.ViewFilter{{Field: "status", Op: "eq", Value: "active"}}},
					},
				},
				{Name: "private_notes", Fields: []metadata.Field{{Name: "body", Type: "text"}}},
			},
		},
		{
			Name:       "hidden",
			SQLitePath: "./data/hidden.sqlite",
			Tables:     []metadata.Table{{Name: "secrets", Fields: []metadata.Field{{Name: "value", Type: "text"}}}},
		},
	}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "reader",
		Scope:     permission.ScopeField,
		Resource:  "workspace.contacts",
		Field:     "email",
		Level:     permission.Read,
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/metadata", nil)
	request.AddCookie(testSessionCookie(t, system, "reader"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected metadata 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var visible metadata.Catalog
	if err := json.NewDecoder(recorder.Body).Decode(&visible); err != nil {
		t.Fatal(err)
	}
	if len(visible.Databases) != 1 || visible.Databases[0].Name != "workspace" {
		t.Fatalf("expected only visible workspace database, got %#v", visible)
	}
	if len(visible.Databases[0].Tables) != 1 || visible.Databases[0].Tables[0].Name != "contacts" {
		t.Fatalf("expected only visible contacts table, got %#v", visible.Databases[0].Tables)
	}
	if len(visible.Databases[0].Tables[0].Fields) != 1 || visible.Databases[0].Tables[0].Fields[0].Name != "email" {
		t.Fatalf("expected only readable email field, got %#v", visible.Databases[0].Tables[0].Fields)
	}
	if len(visible.Databases[0].Tables[0].Views) != 1 || visible.Databases[0].Tables[0].Views[0].Name != "by-email" {
		t.Fatalf("expected only views based on readable fields, got %#v", visible.Databases[0].Tables[0].Views)
	}

	anonymous := httptest.NewRequest(http.MethodGet, "/api/metadata", nil)
	anonymousRecorder := httptest.NewRecorder()
	server.ServeHTTP(anonymousRecorder, anonymous)
	if anonymousRecorder.Code != http.StatusOK {
		t.Fatalf("expected anonymous metadata 200, got %d: %s", anonymousRecorder.Code, anonymousRecorder.Body.String())
	}
	var anonymousCatalog metadata.Catalog
	if err := json.NewDecoder(anonymousRecorder.Body).Decode(&anonymousCatalog); err != nil {
		t.Fatal(err)
	}
	if len(anonymousCatalog.Databases) != 0 {
		t.Fatalf("expected anonymous metadata to be empty, got %#v", anonymousCatalog)
	}
}

func TestCreateDatabaseAPIWritesMetadataAndGrantsOwner(t *testing.T) {
	ctx := context.Background()
	server, system, metadataPath := newTestServerWithMetadataFile(t, metadata.Catalog{})
	request := httptest.NewRequest(http.MethodPost, "/api/databases", bytes.NewBufferString(`{
		"name":"sales",
		"sqlite_path":"./data/sales.sqlite"
	}`))
	request.AddCookie(testSessionCookie(t, system, "owner"))
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
	request.AddCookie(testSessionCookie(t, system, "owner"))
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

func TestTableOwnerCanUpdateFieldsAndViews(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "workspace",
		SQLitePath: "./data/workspace.sqlite",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "text"},
				{Name: "status", Type: "text"},
			},
			Views: []metadata.View{{
				Name:    "active",
				Filters: []metadata.ViewFilter{{Field: "status", Op: "eq", Value: "active"}},
			}},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeTable,
		Resource:  "workspace.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[
			{"name":"name","type":"text"},
			{"name":"status","type":"text","deleted":true},
			{"name":"email","type":"email"}
		],
		"views":[
			{"name":"active","filters":[{"field":"status","op":"eq","value":"active"}]},
			{"name":"active-by-name","base_view":"active","sorts":[{"field":"name","direction":"asc"}]}
		]
	}`))
	request.AddCookie(testSessionCookie(t, system, "owner"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected table metadata update 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	loaded, err := metadata.Load(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	tableMeta, ok := loaded.Table("workspace", "contacts")
	if !ok {
		t.Fatal("expected contacts table")
	}
	if _, ok := tableMeta.Field("email"); !ok {
		t.Fatalf("expected added email field, got %#v", tableMeta.Fields)
	}
	statusField, _ := tableMeta.Field("status")
	if !statusField.Deleted {
		t.Fatalf("expected status to be soft-deleted, got %#v", tableMeta.Fields)
	}
	resolved, err := tableMeta.ResolveView("active-by-name")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Filters) != 1 || len(resolved.Sorts) != 1 {
		t.Fatalf("expected composed based view, got %#v", resolved)
	}
}

func TestDatabaseOwnerCanManageRoles(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "workspace",
		SQLitePath: "./data/workspace.sqlite",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "text"},
				{Name: "email", Type: "email"},
			},
		}},
	}}}
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
	createRole.AddCookie(testSessionCookie(t, system, "owner"))
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
	updateGrants.AddCookie(testSessionCookie(t, system, "owner"))
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, updateGrants)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected role grants update 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	updateMembers := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/roles/editor/members", bytes.NewBufferString(`{
		"members":["u1","u2","u1"]
	}`))
	updateMembers.AddCookie(testSessionCookie(t, system, "owner"))
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, updateMembers)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected role members update 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	rolesRequest := httptest.NewRequest(http.MethodGet, "/api/databases/workspace/roles", nil)
	rolesRequest.AddCookie(testSessionCookie(t, system, "owner"))
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, rolesRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected role list 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var roles []systemdb.RoleDefinition
	if err := json.Unmarshal(recorder.Body.Bytes(), &roles); err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].SubjectID != "role:workspace:editor" || len(roles[0].Grants) != 2 || len(roles[0].Members) != 2 {
		t.Fatalf("unexpected roles response: %#v", roles)
	}
}

func TestRoleGrantValidationKeepsResourcesInsideDatabase(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{
			Name:       "workspace",
			SQLitePath: "./data/workspace.sqlite",
			Tables: []metadata.Table{{
				Name: "contacts",
				Fields: []metadata.Field{
					{Name: "name", Type: "text"},
					{Name: "legacy", Type: "text", Deleted: true},
				},
			}},
		},
		{
			Name:       "other",
			SQLitePath: "./data/other.sqlite",
			Tables: []metadata.Table{{
				Name:   "contacts",
				Fields: []metadata.Field{{Name: "name", Type: "text"}},
			}},
		},
	}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	workspaceWorkflow, err := system.SaveWorkflow(ctx, systemdb.WorkflowDefinition{
		DatabaseName: "workspace",
		Name:         "workspace-flow",
		Script:       "function run() {}",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherWorkflow, err := system.SaveWorkflow(ctx, systemdb.WorkflowDefinition{
		DatabaseName: "other",
		Name:         "other-flow",
		Script:       "function run() {}",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceForm, err := system.SaveForm(ctx, systemdb.FormDefinition{
		DatabaseName: "workspace",
		Name:         "workspace-form",
		Script:       "root.append()",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherForm, err := system.SaveForm(ctx, systemdb.FormDefinition{
		DatabaseName: "other",
		Name:         "other-form",
		Script:       "root.append()",
	})
	if err != nil {
		t.Fatal(err)
	}

	valid := []permission.Grant{
		{Scope: permission.ScopeDatabase, Resource: "workspace", Level: permission.Write},
		{Scope: permission.ScopeTable, Resource: "workspace.contacts", Level: permission.Read},
		{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "name", Level: permission.Read},
		{Scope: permission.ScopeWorkflow, Resource: resourceID(workspaceWorkflow.ID), Level: permission.Read},
		{Scope: permission.ScopeForm, Resource: resourceID(workspaceForm.ID), Level: permission.Read},
		{Scope: permission.ScopeTable, Resource: "other.contacts", Level: permission.None},
	}
	if err := server.validateRoleGrants(ctx, "workspace", valid); err != nil {
		t.Fatalf("expected valid workspace grants, got %v", err)
	}

	invalidCases := []struct {
		name  string
		grant permission.Grant
	}{
		{name: "other table", grant: permission.Grant{Scope: permission.ScopeTable, Resource: "other.contacts", Level: permission.Read}},
		{name: "other field", grant: permission.Grant{Scope: permission.ScopeField, Resource: "other.contacts", Field: "name", Level: permission.Read}},
		{name: "deleted field", grant: permission.Grant{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "legacy", Level: permission.Read}},
		{name: "record id field", grant: permission.Grant{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "record_id", Level: permission.Read}},
		{name: "other workflow", grant: permission.Grant{Scope: permission.ScopeWorkflow, Resource: resourceID(otherWorkflow.ID), Level: permission.Read}},
		{name: "other form", grant: permission.Grant{Scope: permission.ScopeForm, Resource: resourceID(otherForm.ID), Level: permission.Read}},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := server.validateRoleGrants(ctx, "workspace", []permission.Grant{tc.grant}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRoleGrantAPIRejectsCrossDatabaseResources(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{Name: "workspace", SQLitePath: "./data/workspace.sqlite", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "text"}}}}},
		{Name: "other", SQLitePath: "./data/other.sqlite", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "text"}}}}},
	}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeDatabase,
		Resource:  "workspace",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.SaveRole(ctx, systemdb.RoleDefinition{DatabaseName: "workspace", Name: "editor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleGrants(ctx, "workspace", "editor", []permission.Grant{
		{Scope: permission.ScopeTable, Resource: "workspace.contacts", Level: permission.Read},
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/roles/editor/grants", bytes.NewBufferString(`{
		"grants":[{"scope":"table","resource":"other.contacts","level":2}]
	}`))
	request.AddCookie(testSessionCookie(t, system, "owner"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected cross-database grant 400, got %d: %s", recorder.Code, recorder.Body.String())
	}

	role, err := system.Role(ctx, "workspace", "editor")
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Grants) != 1 || role.Grants[0].Resource != "workspace.contacts" {
		t.Fatalf("expected rejected update to leave existing grants intact, got %#v", role.Grants)
	}
}

func TestRoleMembershipGrantsEffectiveTableAccess(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if _, err := system.SaveRole(ctx, systemdb.RoleDefinition{DatabaseName: "db", Name: "editor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleGrants(ctx, "db", "editor", []permission.Grant{
		{Scope: permission.ScopeTable, Resource: "db.contacts", Level: permission.Write},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleMembers(ctx, "db", "editor", []string{"u1"}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Ada","email":"ada@example.com"}
	}`))
	request.AddCookie(testSessionCookie(t, system, "u1"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected role member create 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestRoleFieldGrantOverridesTableWrite(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	createRequest := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Ada","email":"ada@example.com"}
	}`))
	createRequest.AddCookie(testSessionCookie(t, system, "owner"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected owner create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	if _, err := system.SaveRole(ctx, systemdb.RoleDefinition{DatabaseName: "db", Name: "editor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleGrants(ctx, "db", "editor", []permission.Grant{
		{Scope: permission.ScopeTable, Resource: "db.contacts", Level: permission.Write},
		{Scope: permission.ScopeField, Resource: "db.contacts", Field: "email", Level: permission.None},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleMembers(ctx, "db", "editor", []string{"u1"}); err != nil {
		t.Fatal(err)
	}

	allowedRequest := httptest.NewRequest(http.MethodPatch, "/api/tables/db/contacts/rows/1", bytes.NewBufferString(`{
		"values":{"name":"Grace"}
	}`))
	allowedRequest.AddCookie(testSessionCookie(t, system, "u1"))
	allowedRecorder := httptest.NewRecorder()
	server.ServeHTTP(allowedRecorder, allowedRequest)
	if allowedRecorder.Code != http.StatusOK {
		t.Fatalf("expected role member name update 200, got %d: %s", allowedRecorder.Code, allowedRecorder.Body.String())
	}

	deniedRequest := httptest.NewRequest(http.MethodPatch, "/api/tables/db/contacts/rows/1", bytes.NewBufferString(`{
		"values":{"email":"blocked@example.com"}
	}`))
	deniedRequest.AddCookie(testSessionCookie(t, system, "u1"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, deniedRequest)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected role member field override 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
}

func TestRoleManagementRequiresDatabaseWrite(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: "./data/workspace.sqlite"}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)

	request := httptest.NewRequest(http.MethodPost, "/api/databases/workspace/roles", bytes.NewBufferString(`{
		"name":"viewer"
	}`))
	request.AddCookie(testSessionCookie(t, system, "viewer"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected role create forbidden, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestNonDatabaseOwnerCannotCreateTable(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: "./data/workspace.sqlite"}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	request := httptest.NewRequest(http.MethodPost, "/api/databases/workspace/tables", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[{"name":"name","type":"text"}]
	}`))
	request.AddCookie(testSessionCookie(t, system, "other"))
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
		request.AddCookie(testSessionCookie(t, system, "u1"))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows?view=active-a", nil)
	request.AddCookie(testSessionCookie(t, system, "u1"))
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

	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "reader",
		Scope:     permission.ScopeField,
		Resource:  "db.contacts",
		Field:     "email",
		Level:     permission.Read,
	}); err != nil {
		t.Fatal(err)
	}
	deniedView := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows?view=active-a", nil)
	deniedView.AddCookie(testSessionCookie(t, system, "reader"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, deniedView)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected unreadable view 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
}

func TestCreateRowAPIDeniesMissingWritePermission(t *testing.T) {
	server, system := newTestServer(t)
	body := bytes.NewBufferString(`{"values":{"name":"Ada"}}`)
	request := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", body)
	request.AddCookie(testSessionCookie(t, system, "u1"))
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
	request.AddCookie(testSessionCookie(t, system, "u1"))
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
	server, system := newTestServer(t)
	codeRoot := t.TempDir()
	server.SetCodeFileStore(codefiles.NewStore(codeRoot))
	if err := system.SaveGrant(context.Background(), permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"notify",
		"script":"export default async function run() {}",
		"secrets":{"TOKEN":"secret"},
		"variables":{"CHANNEL":"ops"}
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
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
	getWorkflow.AddCookie(testSessionCookie(t, system, "u1"))
	getWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(getWorkflowRecorder, getWorkflow)
	if getWorkflowRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow 200, got %d: %s", getWorkflowRecorder.Code, getWorkflowRecorder.Body.String())
	}
	listWorkflows := httptest.NewRequest(http.MethodGet, "/api/databases/db/workflows", nil)
	listWorkflows.AddCookie(testSessionCookie(t, system, "u1"))
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
	formRequest.AddCookie(testSessionCookie(t, system, "u1"))
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
	listForms.AddCookie(testSessionCookie(t, system, "u1"))
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
	workflowScript, err := os.ReadFile(filepath.Join(codeRoot, "workflows", "db", "00000000000000000001-notify.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(workflowScript) != workflow.Script {
		t.Fatalf("unexpected workflow code file: %s", workflowScript)
	}
	formScript, err := os.ReadFile(filepath.Join(codeRoot, "forms", "db", "00000000000000000001-contact-intake.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(formScript) != form.Script {
		t.Fatalf("unexpected form code file: %s", formScript)
	}
	fileWorkflowScript := "function run(info) { return { message: info.inputs.name + '-from-file' }; }"
	if err := os.WriteFile(filepath.Join(codeRoot, "workflows", "db", "00000000000000000001-notify.js"), []byte(fileWorkflowScript), 0o644); err != nil {
		t.Fatal(err)
	}
	getWorkflow = httptest.NewRequest(http.MethodGet, "/api/workflows/1", nil)
	getWorkflow.AddCookie(testSessionCookie(t, system, "u1"))
	getWorkflowRecorder = httptest.NewRecorder()
	server.ServeHTTP(getWorkflowRecorder, getWorkflow)
	if getWorkflowRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow reload 200, got %d: %s", getWorkflowRecorder.Code, getWorkflowRecorder.Body.String())
	}
	var reloadedWorkflow systemdb.WorkflowDefinition
	if err := json.NewDecoder(getWorkflowRecorder.Body).Decode(&reloadedWorkflow); err != nil {
		t.Fatal(err)
	}
	if reloadedWorkflow.Script != fileWorkflowScript {
		t.Fatalf("expected workflow script from repository file, got %q", reloadedWorkflow.Script)
	}
	runWorkflow := httptest.NewRequest(http.MethodPost, "/api/workflows/1/runs", bytes.NewBufferString(`{"inputs":{"name":"Ada"}}`))
	runWorkflow.AddCookie(testSessionCookie(t, system, "u1"))
	runWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(runWorkflowRecorder, runWorkflow)
	if runWorkflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected file-backed workflow run 201, got %d: %s", runWorkflowRecorder.Code, runWorkflowRecorder.Body.String())
	}
	var run workflowRunResponse
	if err := json.NewDecoder(runWorkflowRecorder.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if run.Run.Outputs["message"] != "Ada-from-file" {
		t.Fatalf("expected workflow run to use repository script, got %#v", run.Run.Outputs)
	}

	fileFormScript := "root.append(api.input({ name: 'from_file' }))"
	if err := os.WriteFile(filepath.Join(codeRoot, "forms", "db", "00000000000000000001-contact-intake.js"), []byte(fileFormScript), 0o644); err != nil {
		t.Fatal(err)
	}
	getForm := httptest.NewRequest(http.MethodGet, "/api/forms/1", nil)
	getForm.AddCookie(testSessionCookie(t, system, "u1"))
	getFormRecorder := httptest.NewRecorder()
	server.ServeHTTP(getFormRecorder, getForm)
	if getFormRecorder.Code != http.StatusOK {
		t.Fatalf("expected form reload 200, got %d: %s", getFormRecorder.Code, getFormRecorder.Body.String())
	}
	var reloadedForm systemdb.FormDefinition
	if err := json.NewDecoder(getFormRecorder.Body).Decode(&reloadedForm); err != nil {
		t.Fatal(err)
	}
	if reloadedForm.Script != fileFormScript {
		t.Fatalf("expected form script from repository file, got %q", reloadedForm.Script)
	}
}

func TestWorkflowAndFormCreationRequiresDatabaseOrTableWrite(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)

	deniedWorkflow := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"denied",
		"script":"function run() { return {}; }"
	}`))
	deniedWorkflow.AddCookie(testSessionCookie(t, system, "viewer"))
	deniedWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedWorkflowRecorder, deniedWorkflow)
	if deniedWorkflowRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected workflow create 403, got %d: %s", deniedWorkflowRecorder.Code, deniedWorkflowRecorder.Body.String())
	}

	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "table-owner",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	tableOwnerWorkflow := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"allowed-by-table",
		"script":"function run() { return {}; }"
	}`))
	tableOwnerWorkflow.AddCookie(testSessionCookie(t, system, "table-owner"))
	tableOwnerWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(tableOwnerWorkflowRecorder, tableOwnerWorkflow)
	if tableOwnerWorkflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected table owner workflow create 201, got %d: %s", tableOwnerWorkflowRecorder.Code, tableOwnerWorkflowRecorder.Body.String())
	}

	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "db-owner",
		Scope:     permission.ScopeDatabase,
		Resource:  "db",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	dbOwnerForm := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"allowed-by-db",
		"script":"root.append(api.input({ name: 'email' }))"
	}`))
	dbOwnerForm.AddCookie(testSessionCookie(t, system, "db-owner"))
	dbOwnerFormRecorder := httptest.NewRecorder()
	server.ServeHTTP(dbOwnerFormRecorder, dbOwnerForm)
	if dbOwnerFormRecorder.Code != http.StatusCreated {
		t.Fatalf("expected db owner form create 201, got %d: %s", dbOwnerFormRecorder.Code, dbOwnerFormRecorder.Body.String())
	}
}

func TestDatabaseWriteCanManageDatabaseWorkflowsAndForms(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "table-owner",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "db-owner",
		Scope:     permission.ScopeDatabase,
		Resource:  "db",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"owned-by-table",
		"script":"function run(info) { return { message: info.inputs.name }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "table-owner"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected table owner workflow create 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	var workflow systemdb.WorkflowDefinition
	if err := json.NewDecoder(workflowRecorder.Body).Decode(&workflow); err != nil {
		t.Fatal(err)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"owned-by-table",
		"script":"root.append()"
	}`))
	formRequest.AddCookie(testSessionCookie(t, system, "table-owner"))
	formRecorder := httptest.NewRecorder()
	server.ServeHTTP(formRecorder, formRequest)
	if formRecorder.Code != http.StatusCreated {
		t.Fatalf("expected table owner form create 201, got %d: %s", formRecorder.Code, formRecorder.Body.String())
	}
	var form systemdb.FormDefinition
	if err := json.NewDecoder(formRecorder.Body).Decode(&form); err != nil {
		t.Fatal(err)
	}

	listWorkflows := httptest.NewRequest(http.MethodGet, "/api/databases/db/workflows", nil)
	listWorkflows.AddCookie(testSessionCookie(t, system, "db-owner"))
	listWorkflowsRecorder := httptest.NewRecorder()
	server.ServeHTTP(listWorkflowsRecorder, listWorkflows)
	if listWorkflowsRecorder.Code != http.StatusOK {
		t.Fatalf("expected db owner workflow list 200, got %d: %s", listWorkflowsRecorder.Code, listWorkflowsRecorder.Body.String())
	}
	var workflows []systemdb.WorkflowDefinition
	if err := json.NewDecoder(listWorkflowsRecorder.Body).Decode(&workflows); err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || workflows[0].ID != workflow.ID {
		t.Fatalf("expected db owner to see workflow, got %#v", workflows)
	}

	getWorkflow := httptest.NewRequest(http.MethodGet, "/api/workflows/1", nil)
	getWorkflow.AddCookie(testSessionCookie(t, system, "db-owner"))
	getWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(getWorkflowRecorder, getWorkflow)
	if getWorkflowRecorder.Code != http.StatusOK {
		t.Fatalf("expected db owner workflow get 200, got %d: %s", getWorkflowRecorder.Code, getWorkflowRecorder.Body.String())
	}

	updateWorkflow := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"id":1,
		"name":"owned-by-table",
		"script":"function run() { return { updated: true }; }"
	}`))
	updateWorkflow.AddCookie(testSessionCookie(t, system, "db-owner"))
	updateWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(updateWorkflowRecorder, updateWorkflow)
	if updateWorkflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected db owner workflow update 201, got %d: %s", updateWorkflowRecorder.Code, updateWorkflowRecorder.Body.String())
	}

	listForms := httptest.NewRequest(http.MethodGet, "/api/databases/db/forms", nil)
	listForms.AddCookie(testSessionCookie(t, system, "db-owner"))
	listFormsRecorder := httptest.NewRecorder()
	server.ServeHTTP(listFormsRecorder, listForms)
	if listFormsRecorder.Code != http.StatusOK {
		t.Fatalf("expected db owner form list 200, got %d: %s", listFormsRecorder.Code, listFormsRecorder.Body.String())
	}
	var forms []systemdb.FormDefinition
	if err := json.NewDecoder(listFormsRecorder.Body).Decode(&forms); err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 || forms[0].ID != form.ID {
		t.Fatalf("expected db owner to see form, got %#v", forms)
	}

	getForm := httptest.NewRequest(http.MethodGet, "/api/forms/1", nil)
	getForm.AddCookie(testSessionCookie(t, system, "db-owner"))
	getFormRecorder := httptest.NewRecorder()
	server.ServeHTTP(getFormRecorder, getForm)
	if getFormRecorder.Code != http.StatusOK {
		t.Fatalf("expected db owner form get 200, got %d: %s", getFormRecorder.Code, getFormRecorder.Body.String())
	}

	updateForm := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"id":1,
		"name":"owned-by-table",
		"script":"root.append(api.submit('Save'))"
	}`))
	updateForm.AddCookie(testSessionCookie(t, system, "db-owner"))
	updateFormRecorder := httptest.NewRecorder()
	server.ServeHTTP(updateFormRecorder, updateForm)
	if updateFormRecorder.Code != http.StatusCreated {
		t.Fatalf("expected db owner form update 201, got %d: %s", updateFormRecorder.Code, updateFormRecorder.Body.String())
	}
}

func TestWorkflowAndFormUpdatesCannotMoveAcrossDatabases(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{Name: "db", SQLitePath: "./data/db.sqlite", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "text"}}}}},
		{Name: "other", SQLitePath: "./data/other.sqlite", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "text"}}}}},
	}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"notify",
		"script":"function run() { return {}; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow create 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	var savedWorkflow systemdb.WorkflowDefinition
	if err := json.NewDecoder(workflowRecorder.Body).Decode(&savedWorkflow); err != nil {
		t.Fatal(err)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"intake",
		"script":"root.append(api.input({ name: 'email' }))"
	}`))
	formRequest.AddCookie(testSessionCookie(t, system, "u1"))
	formRecorder := httptest.NewRecorder()
	server.ServeHTTP(formRecorder, formRequest)
	if formRecorder.Code != http.StatusCreated {
		t.Fatalf("expected form create 201, got %d: %s", formRecorder.Code, formRecorder.Body.String())
	}
	var savedForm systemdb.FormDefinition
	if err := json.NewDecoder(formRecorder.Body).Decode(&savedForm); err != nil {
		t.Fatal(err)
	}

	moveWorkflowByPath := httptest.NewRequest(http.MethodPost, "/api/databases/other/workflows", bytes.NewBufferString(`{
		"id":1,
		"name":"notify",
		"script":"function run() { return { moved: true }; }"
	}`))
	moveWorkflowByPath.AddCookie(testSessionCookie(t, system, "u1"))
	moveWorkflowByPathRecorder := httptest.NewRecorder()
	server.ServeHTTP(moveWorkflowByPathRecorder, moveWorkflowByPath)
	if moveWorkflowByPathRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected cross-db workflow path update 400, got %d: %s", moveWorkflowByPathRecorder.Code, moveWorkflowByPathRecorder.Body.String())
	}

	moveWorkflowByBody := httptest.NewRequest(http.MethodPost, "/api/workflows", bytes.NewBufferString(`{
		"id":1,
		"database_name":"other",
		"name":"notify",
		"script":"function run() { return { moved: true }; }"
	}`))
	moveWorkflowByBody.AddCookie(testSessionCookie(t, system, "u1"))
	moveWorkflowByBodyRecorder := httptest.NewRecorder()
	server.ServeHTTP(moveWorkflowByBodyRecorder, moveWorkflowByBody)
	if moveWorkflowByBodyRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected cross-db workflow body update 400, got %d: %s", moveWorkflowByBodyRecorder.Code, moveWorkflowByBodyRecorder.Body.String())
	}

	moveFormByPath := httptest.NewRequest(http.MethodPost, "/api/databases/other/forms", bytes.NewBufferString(`{
		"id":1,
		"name":"intake",
		"script":"root.append(api.input({ name: 'moved' }))"
	}`))
	moveFormByPath.AddCookie(testSessionCookie(t, system, "u1"))
	moveFormByPathRecorder := httptest.NewRecorder()
	server.ServeHTTP(moveFormByPathRecorder, moveFormByPath)
	if moveFormByPathRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected cross-db form path update 400, got %d: %s", moveFormByPathRecorder.Code, moveFormByPathRecorder.Body.String())
	}

	moveFormByBody := httptest.NewRequest(http.MethodPost, "/api/forms", bytes.NewBufferString(`{
		"id":1,
		"database_name":"other",
		"name":"intake",
		"script":"root.append(api.input({ name: 'moved' }))"
	}`))
	moveFormByBody.AddCookie(testSessionCookie(t, system, "u1"))
	moveFormByBodyRecorder := httptest.NewRecorder()
	server.ServeHTTP(moveFormByBodyRecorder, moveFormByBody)
	if moveFormByBodyRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected cross-db form body update 400, got %d: %s", moveFormByBodyRecorder.Code, moveFormByBodyRecorder.Body.String())
	}

	workflowAfter, err := system.Workflow(ctx, savedWorkflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if workflowAfter.DatabaseName != "db" || workflowAfter.Script != savedWorkflow.Script {
		t.Fatalf("expected workflow to remain in db unchanged, got %#v", workflowAfter)
	}
	formAfter, err := system.Form(ctx, savedForm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if formAfter.DatabaseName != "db" || formAfter.Script != savedForm.Script {
		t.Fatalf("expected form to remain in db unchanged, got %#v", formAfter)
	}
}

func TestWorkflowRunAPI(t *testing.T) {
	server, system := newTestServer(t)
	if err := system.SaveGrant(context.Background(), permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"welcome",
		"script":"function run(info) { const echoed = info.node(\"echo\", { value: info.inputs.name }); return { message: echoed.value + \"-\" + info.variables.suffix }; }",
		"variables":{"suffix":"done"}
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
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
	runRequest.AddCookie(testSessionCookie(t, system, "u1"))
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
	listRequest.AddCookie(testSessionCookie(t, system, "u1"))
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
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
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
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}

	runRequest := httptest.NewRequest(http.MethodPost, "/api/workflows/1/runs", bytes.NewBufferString(`{"inputs":{"history_key":"`+historyKey+`"}}`))
	runRequest.AddCookie(testSessionCookie(t, system, "u1"))
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
	if err := system.SaveGrant(context.Background(), permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeDatabase,
		Resource:  "db",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"restricted",
		"script":"function run(info) { return info.inputs; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "owner"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	var createdWorkflow systemdb.WorkflowDefinition
	if err := json.NewDecoder(workflowRecorder.Body).Decode(&createdWorkflow); err != nil {
		t.Fatal(err)
	}
	if createdWorkflow.PermissionLevel != permission.Write {
		t.Fatalf("expected created workflow write permission, got %d", createdWorkflow.PermissionLevel)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"restricted-form",
		"script":"root.append(api.input({ name: 'email' }))"
	}`))
	formRequest.AddCookie(testSessionCookie(t, system, "owner"))
	formRecorder := httptest.NewRecorder()
	server.ServeHTTP(formRecorder, formRequest)
	if formRecorder.Code != http.StatusCreated {
		t.Fatalf("expected form 201, got %d: %s", formRecorder.Code, formRecorder.Body.String())
	}
	var createdForm systemdb.FormDefinition
	if err := json.NewDecoder(formRecorder.Body).Decode(&createdForm); err != nil {
		t.Fatal(err)
	}
	if createdForm.PermissionLevel != permission.Write {
		t.Fatalf("expected created form write permission, got %d", createdForm.PermissionLevel)
	}

	otherWorkflow := httptest.NewRequest(http.MethodGet, "/api/workflows/1", nil)
	otherWorkflow.AddCookie(testSessionCookie(t, system, "other"))
	otherWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(otherWorkflowRecorder, otherWorkflow)
	if otherWorkflowRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected workflow 403, got %d: %s", otherWorkflowRecorder.Code, otherWorkflowRecorder.Body.String())
	}

	otherForms := httptest.NewRequest(http.MethodGet, "/api/databases/db/forms", nil)
	otherForms.AddCookie(testSessionCookie(t, system, "other"))
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
	readWorkflow.AddCookie(testSessionCookie(t, system, "other"))
	readWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(readWorkflowRecorder, readWorkflow)
	if readWorkflowRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow 200, got %d: %s", readWorkflowRecorder.Code, readWorkflowRecorder.Body.String())
	}
	var readableWorkflow systemdb.WorkflowDefinition
	if err := json.NewDecoder(readWorkflowRecorder.Body).Decode(&readableWorkflow); err != nil {
		t.Fatal(err)
	}
	if readableWorkflow.PermissionLevel != permission.Read {
		t.Fatalf("expected read-only workflow permission, got %d", readableWorkflow.PermissionLevel)
	}

	runWorkflow := httptest.NewRequest(http.MethodPost, "/api/workflows/1/runs", bytes.NewBufferString(`{"inputs":{"name":"Ada"}}`))
	runWorkflow.AddCookie(testSessionCookie(t, system, "other"))
	runWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(runWorkflowRecorder, runWorkflow)
	if runWorkflowRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected read-only workflow run 403, got %d: %s", runWorkflowRecorder.Code, runWorkflowRecorder.Body.String())
	}

	if err := system.SaveGrant(context.Background(), permission.Grant{
		SubjectID: "other",
		Scope:     permission.ScopeForm,
		Resource:  "1",
		Level:     permission.Read,
	}); err != nil {
		t.Fatal(err)
	}
	readForms := httptest.NewRequest(http.MethodGet, "/api/databases/db/forms", nil)
	readForms.AddCookie(testSessionCookie(t, system, "other"))
	readFormsRecorder := httptest.NewRecorder()
	server.ServeHTTP(readFormsRecorder, readForms)
	if readFormsRecorder.Code != http.StatusOK {
		t.Fatalf("expected form list 200, got %d: %s", readFormsRecorder.Code, readFormsRecorder.Body.String())
	}
	if err := json.NewDecoder(readFormsRecorder.Body).Decode(&forms); err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 || forms[0].PermissionLevel != permission.Read {
		t.Fatalf("expected read-only form permission in list, got %#v", forms)
	}

	readForm := httptest.NewRequest(http.MethodGet, "/api/forms/1", nil)
	readForm.AddCookie(testSessionCookie(t, system, "other"))
	readFormRecorder := httptest.NewRecorder()
	server.ServeHTTP(readFormRecorder, readForm)
	if readFormRecorder.Code != http.StatusOK {
		t.Fatalf("expected form 200, got %d: %s", readFormRecorder.Code, readFormRecorder.Body.String())
	}
	var readableForm systemdb.FormDefinition
	if err := json.NewDecoder(readFormRecorder.Body).Decode(&readableForm); err != nil {
		t.Fatal(err)
	}
	if readableForm.PermissionLevel != permission.Read {
		t.Fatalf("expected read-only form permission, got %d", readableForm.PermissionLevel)
	}

	updateForm := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"id":1,
		"name":"restricted-form",
		"script":"root.append(api.input({ name: 'other' }))"
	}`))
	updateForm.AddCookie(testSessionCookie(t, system, "other"))
	updateFormRecorder := httptest.NewRecorder()
	server.ServeHTTP(updateFormRecorder, updateForm)
	if updateFormRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected read-only form update 403, got %d: %s", updateFormRecorder.Code, updateFormRecorder.Body.String())
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

func testSessionCookie(t *testing.T, system *systemdb.DB, userID string) *http.Cookie {
	t.Helper()
	user, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:    userID + "@example.com",
		Password: "correct horse",
	})
	if err != nil {
		t.Fatal(err)
	}
	user.ID = userID
	if _, err := system.UpsertUserByEmail(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	session, err := system.CreateSession(context.Background(), userID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: session.Token}
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
