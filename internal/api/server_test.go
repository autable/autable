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
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"autable/internal/auth"
	"autable/internal/codefiles"
	"autable/internal/config"
	"autable/internal/history"
	"autable/internal/metadata"
	"autable/internal/permission"
	"autable/internal/recorddb"
	"autable/internal/systemdb"
	"autable/internal/table"
	"autable/internal/workflow"
)

func metadataFieldNames(fields []metadata.Field) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	return names
}

func saveTestGrants(t *testing.T, system *systemdb.DB, grants ...permission.Grant) {
	t.Helper()
	for _, grant := range grants {
		if err := system.SaveGrant(context.Background(), grant); err != nil {
			t.Fatal(err)
		}
	}
}

func saveTestRecordCreateGrant(t *testing.T, system *systemdb.DB, subjectID, resource string) {
	t.Helper()
	saveTestGrants(t, system, permission.Grant{
		SubjectID: subjectID,
		Scope:     permission.ScopeRecord,
		Resource:  resource,
		Field:     "create",
		Level:     permission.Write,
	})
}

func saveTestDatabaseOwners(t *testing.T, system *systemdb.DB, dbName string, ownerIDs ...string) {
	t.Helper()
	for _, ownerID := range ownerIDs {
		if err := system.SaveDatabaseOwner(context.Background(), dbName, ownerID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPasswordAuthSessionLifecycle(t *testing.T) {
	server, _ := newTestServer(t)

	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{
		"email":"Person@Example.com",
		"display_name":"Person Example",
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
	if registered.Email != "person@example.com" || registered.DisplayName != "Person Example" || registered.Provider != "password" {
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
		"display_name":"Person Example",
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

func TestPasswordAuthDisabledRejectsPasswordEndpoints(t *testing.T) {
	server, _ := newTestServerWithAuth(t, config.AuthConfig{
		OIDC: config.OIDCConfig{
			Enabled: true,
			Providers: []config.OIDCProvider{
				{
					Name:      "main",
					IssuerURL: "https://issuer.example",
					ClientID:  "autable",
				},
			},
		},
	})

	for _, path := range []string{"/api/auth/register", "/api/auth/login"} {
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{
			"email":"person@example.com",
			"password":"correct horse"
		}`))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected %s 404, got %d: %s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestUserSearchAPIRequiresAuthenticationAndMatchesEmail(t *testing.T) {
	server, system := newTestServer(t)
	_ = testSessionCookie(t, system, "ada")
	ownerCookie := testSessionCookie(t, system, "owner")

	anonymous := httptest.NewRequest(http.MethodGet, "/api/users?query=ada", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, anonymous)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous user search 401, got %d: %s", recorder.Code, recorder.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/users?query=ADA", nil)
	request.AddCookie(ownerCookie)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected user search 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var users []userResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != "ada" || users[0].Email != "ada@example.com" || users[0].DisplayName != "ada" {
		t.Fatalf("unexpected user search response: %#v", users)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/users?query=own", nil)
	request.AddCookie(ownerCookie)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected display name search 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != "owner" || users[0].DisplayName != "owner" {
		t.Fatalf("unexpected display name search response: %#v", users)
	}
}

func TestAuthConfigExposesPublicConfig(t *testing.T) {
	server, _ := newTestServerWithOIDC(t, []config.OIDCProvider{
		{
			Name:         "main",
			IssuerURL:    "https://issuer.example",
			ClientID:     "autable",
			ClientSecret: "secret",
			Scopes:       []string{"email"},
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected auth config 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["password_enabled"] != true || response["oidc_enabled"] != true {
		t.Fatalf("unexpected auth flags: %#v", response)
	}
	if response["ai_enabled"] != false {
		t.Fatalf("unexpected ai flag: %#v", response)
	}
	providers, ok := response["oidc_providers"].([]any)
	if !ok {
		t.Fatalf("unexpected oidc providers: %#v", response["oidc_providers"])
	}
	if len(providers) != 1 {
		t.Fatalf("expected one provider, got %#v", providers)
	}
	provider, ok := providers[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected provider response: %#v", providers[0])
	}
	if provider["name"] != "main" || provider["issuer_url"] != "https://issuer.example" {
		t.Fatalf("unexpected provider response: %#v", provider)
	}
	if _, ok := provider["client_secret"]; ok {
		t.Fatalf("provider response leaked client_secret: %#v", provider)
	}
	scopes, ok := provider["scopes"].([]any)
	if !ok || len(scopes) != 2 || scopes[0] != "openid" || scopes[1] != "email" {
		t.Fatalf("expected openid to be prepended to custom scopes, got %#v", provider["scopes"])
	}
}

func TestOIDCDisabledDoesNotExposeProviders(t *testing.T) {
	server, _ := newTestServer(t)

	request := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected auth config 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response authConfigResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.PasswordEnabled || response.OIDCEnabled || len(response.OIDCProviders) != 0 {
		t.Fatalf("unexpected auth config: %#v", response)
	}

	start := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/main/start", nil)
	startRecorder := httptest.NewRecorder()
	server.ServeHTTP(startRecorder, start)
	if startRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected disabled oidc start 404, got %d: %s", startRecorder.Code, startRecorder.Body.String())
	}
}

func TestOIDCStartRedirectsToAuthorizeEndpoint(t *testing.T) {
	server, _ := newTestServerWithOIDC(t, []config.OIDCProvider{
		{
			Name:      "main",
			IssuerURL: "https://issuer.example/",
			ClientID:  "autable",
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/main/start", nil)
	request.Host = "app.example"
	request.Header.Set("X-Forwarded-Proto", "http")
	request.Header.Set("X-Forwarded-Host", "forwarded.example")
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
	if query.Get("response_type") != "code" || query.Get("client_id") != "autable" {
		t.Fatalf("unexpected authorize query: %s", authorizeURL.RawQuery)
	}
	if query.Get("scope") != "openid email profile" {
		t.Fatalf("unexpected default scopes: %q", query.Get("scope"))
	}
	if query.Get("redirect_uri") != "https://configured.example/api/auth/oidc/main/callback" {
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
	issuer := newFakeOIDCIssuer(t, "autable", "sub-123", "Person@Example.com")
	defer issuer.Close()
	server, _ := newTestServerWithOIDC(t, []config.OIDCProvider{
		{
			Name:         "main",
			IssuerURL:    issuer.URL,
			ClientID:     "autable",
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
		"display_name":"Person Example",
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
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, user.ID, "db.contacts")

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
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}
	if err := system.SaveGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")
	saveTestDatabaseOwners(t, system, "db", "u1")

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

func TestCreateFieldsAPIUsesTablePermissions(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "workspace.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/tables/workspace/contacts/fields", bytes.NewBufferString(`{
		"fields":{"email":"string","score":"float"}
	}`))
	request.AddCookie(testSessionCookie(t, system, "owner"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected field create 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response map[string][]map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response["created"]) != 2 {
		t.Fatalf("expected created fields response, got %#v", response)
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
		t.Fatalf("expected email field in metadata, got %#v", tableMeta.Fields)
	}
}

func TestUpsertRowAPIUpdatesCreatesAndNoops(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "owner", "db.contacts")

	create := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows/upsert", bytes.NewBufferString(`{
		"match_field":"email",
		"values":{"name":"Ada","email":"remote-1","status":"todo"}
	}`))
	create.AddCookie(testSessionCookie(t, system, "owner"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, create)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("expected upsert create 200, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}
	var created rowMutationResponse
	if err := json.NewDecoder(createRecorder.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Operation != "create" || created.RecordID != 1 {
		t.Fatalf("unexpected upsert create response: %#v", created)
	}

	update := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows/upsert", bytes.NewBufferString(`{
		"match_field":"email",
		"values":{"name":"Grace","email":"remote-1","status":"done"}
	}`))
	update.AddCookie(testSessionCookie(t, system, "owner"))
	updateRecorder := httptest.NewRecorder()
	server.ServeHTTP(updateRecorder, update)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("expected upsert update 200, got %d: %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updated rowMutationResponse
	if err := json.NewDecoder(updateRecorder.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Operation != "update" || updated.RecordID != 1 || updated.Values["name"] != "Grace" {
		t.Fatalf("unexpected upsert update response: %#v", updated)
	}
	historyBefore, err := server.history.GetPrefix(ctx, history.RowPrefix("db", "contacts", 1))
	if err != nil {
		t.Fatal(err)
	}

	noop := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows/upsert", bytes.NewBufferString(`{
		"match_field":"email",
		"values":{"name":"Grace","email":"remote-1","status":"done"}
	}`))
	noop.AddCookie(testSessionCookie(t, system, "owner"))
	noopRecorder := httptest.NewRecorder()
	server.ServeHTTP(noopRecorder, noop)
	if noopRecorder.Code != http.StatusOK {
		t.Fatalf("expected upsert noop 200, got %d: %s", noopRecorder.Code, noopRecorder.Body.String())
	}
	var nooped rowMutationResponse
	if err := json.NewDecoder(noopRecorder.Body).Decode(&nooped); err != nil {
		t.Fatal(err)
	}
	if nooped.Operation != "noop" || nooped.RecordID != 1 {
		t.Fatalf("unexpected upsert noop response: %#v", nooped)
	}
	historyAfter, err := server.history.GetPrefix(ctx, history.RowPrefix("db", "contacts", 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(historyAfter) != len(historyBefore) {
		t.Fatalf("noop upsert should not create row history: before=%d after=%d", len(historyBefore), len(historyAfter))
	}
}

func TestQueryRowsAPIEnforcesQueryFieldPermissions(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	writerGrants := []permission.Grant{
		{SubjectID: "writer", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
		{SubjectID: "writer", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
	}
	for _, grant := range writerGrants {
		if err := system.SaveGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	create := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Ada","email":"ada@example.com","status":"active"}
	}`))
	create.AddCookie(testSessionCookie(t, system, "writer"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, create)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}
	readerGrants := []permission.Grant{
		{SubjectID: "reader", Scope: permission.ScopeField, Resource: "db.contacts", Field: "email", Level: permission.Read},
	}
	for _, grant := range readerGrants {
		if err := system.SaveGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}

	allowed := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows/query", bytes.NewBufferString(`{
		"query":{"combinator":"and","rules":[{"field":"email","operator":"=","value":"ada@example.com"}]},
		"limit":10
	}`))
	allowed.AddCookie(testSessionCookie(t, system, "reader"))
	allowedRecorder := httptest.NewRecorder()
	server.ServeHTTP(allowedRecorder, allowed)
	if allowedRecorder.Code != http.StatusOK {
		t.Fatalf("expected query 200, got %d: %s", allowedRecorder.Code, allowedRecorder.Body.String())
	}
	var rows []rowResponse
	if err := json.NewDecoder(allowedRecorder.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["email"] != "ada@example.com" {
		t.Fatalf("unexpected readable query rows: %#v", rows)
	}
	if _, ok := rows[0].Values["status"]; ok {
		t.Fatalf("query leaked unreadable status: %#v", rows[0].Values)
	}

	denied := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows/query", bytes.NewBufferString(`{
		"query":{"combinator":"and","rules":[{"field":"status","operator":"=","value":"active"}]}
	}`))
	denied.AddCookie(testSessionCookie(t, system, "reader"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, denied)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected unreadable query field 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
}

func TestUpdateRowAPIEnforcesPermissionsAndWritesHistory(t *testing.T) {
	server, system := newTestServer(t)
	saveTestDatabaseOwners(t, system, "db", "u1")
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
		"values":{"email":"ada@autable.test"}
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
	if updated.Values["name"] != "Ada" || updated.Values["email"] != "ada@autable.test" {
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
	if changes[1].Values["email"] != "ada@autable.test" || changes[1].Values["name"] != "Ada" {
		t.Fatalf("unexpected update history: %#v", changes[1])
	}
}

func TestDeleteRowAPIEnforcesPermissionsAndWritesHistory(t *testing.T) {
	server, system := newTestServer(t)
	saveTestDatabaseOwners(t, system, "db", "u1")
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

func TestRowHistoryAPIRequiresAuthAndDatabaseOwner(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "writer",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "writer", "db.contacts")
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

	saveTestDatabaseOwners(t, system, "db", "owner")
	ownerHistory := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	ownerHistory.AddCookie(testSessionCookie(t, system, "owner"))
	ownerRecorder := httptest.NewRecorder()
	server.ServeHTTP(ownerRecorder, ownerHistory)
	if ownerRecorder.Code != http.StatusOK {
		t.Fatalf("expected owner history 200, got %d: %s", ownerRecorder.Code, ownerRecorder.Body.String())
	}
	var ownerChanges []history.RowChange
	if err := json.NewDecoder(ownerRecorder.Body).Decode(&ownerChanges); err != nil {
		t.Fatal(err)
	}
	if len(ownerChanges) != 1 || ownerChanges[0].Values["email"] != "ada@example.com" || ownerChanges[0].Values["name"] != "Ada" {
		t.Fatalf("expected owner to see full history, got %#v", ownerChanges)
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
	if readerRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected reader history 403, got %d: %s", readerRecorder.Code, readerRecorder.Body.String())
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
		"scope":"field_set",
		"resource":"db.contacts",
		"level":2
	}`))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated grant save 401, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPermissionGrantAPIRequiresDatabaseOwner(t *testing.T) {
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

	saveTestDatabaseOwners(t, system, "db", "admin")
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

	grantSet := httptest.NewRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"field_set",
		"resource":"db.contacts",
		"level":1
	}`))
	grantSet.AddCookie(testSessionCookie(t, system, "admin"))
	grantSetRecorder := httptest.NewRecorder()
	server.ServeHTTP(grantSetRecorder, grantSet)
	if grantSetRecorder.Code != http.StatusCreated {
		t.Fatalf("expected field set grant save 201, got %d: %s", grantSetRecorder.Code, grantSetRecorder.Body.String())
	}
	grants, err := system.GrantListForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].Scope != permission.ScopeFieldSet {
		t.Fatalf("expected field set grant to replace field grants, got %#v", grants)
	}

	grantField := httptest.NewRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"field",
		"resource":"db.contacts",
		"field":"name",
		"level":2
	}`))
	grantField.AddCookie(testSessionCookie(t, system, "admin"))
	grantFieldRecorder := httptest.NewRecorder()
	server.ServeHTTP(grantFieldRecorder, grantField)
	if grantFieldRecorder.Code != http.StatusCreated {
		t.Fatalf("expected field grant save 201, got %d: %s", grantFieldRecorder.Code, grantFieldRecorder.Body.String())
	}
	grants, err = system.GrantListForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].Scope != permission.ScopeField || grants[0].Field != "name" {
		t.Fatalf("expected field grant to replace field set grant, got %#v", grants)
	}
}

func TestMetadataAPIOnlyReturnsVisibleDatabasesAndTables(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{
			Name: "workspace",
			Tables: []metadata.Table{
				{
					Name: "contacts",
					Fields: []metadata.Field{
						{Name: "name", Type: "string"},
						{Name: "email", Type: "string"},
						{Name: "status", Type: "string"},
					},
					Views: []metadata.View{
						{
							Name: "by-email",
							Query: &metadata.ViewQuery{
								Combinator: "and",
								Rules:      []metadata.ViewQueryRule{{Field: "email", Operator: "notNull"}},
							},
						},
						{
							Name: "active",
							Query: &metadata.ViewQuery{
								Combinator: "and",
								Rules:      []metadata.ViewQueryRule{{Field: "status", Operator: "=", Value: "active"}},
							},
						},
					},
				},
				{Name: "private_notes", Fields: []metadata.Field{{Name: "body", Type: "string"}}},
			},
		},
		{
			Name:   "hidden",
			Tables: []metadata.Table{{Name: "secrets", Fields: []metadata.Field{{Name: "value", Type: "string"}}}},
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
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "reader",
		Scope:     permission.ScopeView,
		Resource:  "workspace.contacts",
		Field:     "by-email",
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
	if visible.Databases[0].Tables[0].PermissionLevel != int(permission.None) {
		t.Fatalf("expected field-only reader to have no table write permission, got %#v", visible.Databases[0].Tables[0])
	}
	if len(visible.Databases[0].Tables[0].Fields) != 1 || visible.Databases[0].Tables[0].Fields[0].Name != "email" {
		t.Fatalf("expected only readable email field, got %#v", visible.Databases[0].Tables[0].Fields)
	}
	if visible.Databases[0].Tables[0].Fields[0].PermissionLevel != int(permission.Read) {
		t.Fatalf("expected readable email field permission level, got %#v", visible.Databases[0].Tables[0].Fields[0])
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

func TestCreateDatabaseAPIWritesMetadataAndStoresOwner(t *testing.T) {
	server, system, metadataPath := newTestServerWithMetadataFile(t, metadata.Catalog{})
	request := httptest.NewRequest(http.MethodPost, "/api/databases", bytes.NewBufferString(`{
		"name":"sales"
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
	if !ok || db.Name != "sales" {
		t.Fatalf("expected sales database in metadata, got %#v", loaded)
	}
	isOwner, err := system.IsDatabaseOwner(context.Background(), "owner", "sales")
	if err != nil {
		t.Fatal(err)
	}
	if !isOwner {
		t.Fatal("expected database creator to be stored as owner")
	}
}

func TestDatabaseOwnerCanCreateTable(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	saveTestDatabaseOwners(t, system, "workspace", "owner")

	request := httptest.NewRequest(http.MethodPost, "/api/databases/workspace/tables", bytes.NewBufferString(`{
		"name":"contacts",
		"display_name":"Contacts",
		"fields":[{"name":"name","type":"string"},{"name":"email","type":"string"}],
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
	isOwner, err := system.IsDatabaseOwner(context.Background(), "owner", "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if !isOwner {
		t.Fatal("expected database owner to be stored")
	}
}

func TestDatabaseOwnerCanCreateEmptyTable(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	saveTestDatabaseOwners(t, system, "workspace", "owner")

	request := httptest.NewRequest(http.MethodPost, "/api/databases/workspace/tables", bytes.NewBufferString(`{
		"name":"empty",
		"display_name":"Empty",
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
	tableMeta, ok := loaded.Table("workspace", "empty")
	if !ok {
		t.Fatalf("expected empty table in metadata, got %#v", loaded)
	}
	if len(tableMeta.Fields) != 0 {
		t.Fatalf("new tables should not get default fields, got %#v", tableMeta.Fields)
	}
}

func TestTableOwnerCanUpdateFieldsAndViews(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "status", Type: "string"},
			},
			Views: []metadata.View{{
				Name: "active",
				Query: &metadata.ViewQuery{
					Combinator: "and",
					Rules:      []metadata.ViewQueryRule{{Field: "status", Operator: "=", Value: "active"}},
				},
			}},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	saveTestDatabaseOwners(t, system, "workspace", "owner")
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "workspace.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeViewSet,
		Resource:  "workspace.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[
			{"name":"name","type":"string"},
			{"name":"status","type":"string","deleted":true},
			{"name":"email","type":"string"}
			],
			"views":[
				{"name":"active","query":{"combinator":"and","rules":[{"field":"name","operator":"contains","value":"a"}]}},
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
	if resolved.Query == nil || len(resolved.Query.Rules) != 1 || len(resolved.Sorts) != 1 {
		t.Fatalf("expected composed based view, got %#v", resolved)
	}
}

func TestTableMetadataUpdatePreservesOmittedFieldsAndViews(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "email", Type: "string"},
				{Name: "hidden_notes", Type: "string"},
			},
			Views: []metadata.View{
				{Name: "all"},
				{Name: "hidden-view", Query: &metadata.ViewQuery{Combinator: "and", Rules: []metadata.ViewQueryRule{{Field: "hidden_notes", Operator: "notNull"}}}},
			},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "editor", Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.Write},
		permission.Grant{SubjectID: "editor", Scope: permission.ScopeViewSet, Resource: "workspace.contacts", Level: permission.Write},
	)

	request := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[
			{"name":"name","type":"string"},
			{"name":"phone","type":"string"}
		],
		"views":[
			{"name":"all","sorts":[{"field":"name","direction":"asc"}]}
		]
	}`))
	request.AddCookie(testSessionCookie(t, system, "editor"))
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
		t.Fatalf("omitted email field was removed: %#v", tableMeta.Fields)
	}
	if _, ok := tableMeta.Field("hidden_notes"); !ok {
		t.Fatalf("omitted hidden field was removed: %#v", tableMeta.Fields)
	}
	if _, ok := tableMeta.Field("phone"); !ok {
		t.Fatalf("expected new phone field, got %#v", tableMeta.Fields)
	}
	if _, ok := tableMeta.View("hidden-view"); !ok {
		t.Fatalf("omitted view was removed: %#v", tableMeta.Views)
	}
}

func TestFieldSetWriterCannotDeleteField(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}, {Name: "status", Type: "string"}},
		}},
	}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	saveTestGrants(t, system, permission.Grant{SubjectID: "editor", Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.Write})

	request := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[{"name":"status","type":"string","deleted":true}]
	}`))
	request.AddCookie(testSessionCookie(t, system, "editor"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected field delete 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestViewWriterCanUpdateOnlyGrantedExistingView(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}, {Name: "status", Type: "string"}},
			Views:  []metadata.View{{Name: "active"}},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	saveTestGrants(t, system, permission.Grant{SubjectID: "editor", Scope: permission.ScopeView, Resource: "workspace.contacts", Field: "active", Level: permission.Write})

	updateExisting := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"views":[{"name":"active","query":{"combinator":"and","rules":[{"field":"status","operator":"=","value":"active"}]}}]
	}`))
	updateExisting.AddCookie(testSessionCookie(t, system, "editor"))
	updateRecorder := httptest.NewRecorder()
	server.ServeHTTP(updateRecorder, updateExisting)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("expected view update 200, got %d: %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	loaded, err := metadata.Load(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	tableMeta, _ := loaded.Table("workspace", "contacts")
	active, _ := tableMeta.View("active")
	if active.Query == nil || len(active.Query.Rules) != 1 {
		t.Fatalf("expected active view query update, got %#v", active)
	}

	createView := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"views":[{"name":"new-view"}]
	}`))
	createView.AddCookie(testSessionCookie(t, system, "editor"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createView)
	if createRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected view create 403, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}
}

func TestTableOwnerCanMoveFieldPosition(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "hidden", Type: "string"},
				{Name: "email", Type: "string"},
				{Name: "status", Type: "string"},
			},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "workspace.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/databases/workspace/tables/contacts/fields/status/position", bytes.NewBufferString(`{"before":"name"}`))
	request.AddCookie(testSessionCookie(t, system, "owner"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected field move 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	loaded, err := metadata.Load(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	tableMeta, ok := loaded.Table("workspace", "contacts")
	if !ok {
		t.Fatal("expected contacts table")
	}
	if got := metadataFieldNames(tableMeta.Fields); !slices.Equal(got, []string{"status", "name", "hidden", "email"}) {
		t.Fatalf("unexpected field order: %#v", got)
	}
}

func TestWorkflowFieldCreateNodeAddsMissingFields(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "legacy", Type: "string", Deleted: true},
			},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "workspace.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	autable := server.workflowAutableService()
	output, err := autable.CreateFields(ctx, map[string]any{
		"table": "contacts",
		"fields": []any{
			"name",
			map[string]any{"name": "email", "type": "string"},
			map[string]any{"name": "score", "type": "float"},
			map[string]any{"name": "legacy", "type": "string"},
		},
	}, workflow.RuntimeInfo{DatabaseName: "workspace", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	created := output["created"].([]map[string]any)
	restored := output["restored"].([]map[string]any)
	existing := output["existing"].([]map[string]any)
	if len(created) != 2 || created[0]["name"] != "email" || created[1]["name"] != "score" {
		t.Fatalf("unexpected created fields: %#v", output)
	}
	if len(restored) != 1 || restored[0]["name"] != "legacy" {
		t.Fatalf("unexpected restored fields: %#v", output)
	}
	if len(existing) != 1 || existing[0]["name"] != "name" {
		t.Fatalf("unexpected existing fields: %#v", output)
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
		t.Fatalf("expected email field in metadata, got %#v", tableMeta.Fields)
	}
	legacy, _ := tableMeta.Field("legacy")
	if legacy.Deleted {
		t.Fatalf("expected legacy field to be restored, got %#v", legacy)
	}

	output, err = autable.CreateFields(ctx, map[string]any{
		"table": "contacts",
		"fields": []any{
			"email",
			map[string]any{"name": "score", "type": "float"},
		},
	}, workflow.RuntimeInfo{DatabaseName: "workspace", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(output["created"].([]map[string]any)) != 0 || len(output["existing"].([]map[string]any)) != 2 {
		t.Fatalf("expected idempotent second call, got %#v", output)
	}
	beforeNoopWrite, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err = autable.CreateFields(ctx, map[string]any{
		"table":  "contacts",
		"fields": []any{"email"},
	}, workflow.RuntimeInfo{DatabaseName: "workspace", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	afterNoopWrite, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeNoopWrite, afterNoopWrite) {
		t.Fatal("idempotent field create should not rewrite metadata")
	}
}

func TestWorkflowFieldCreateNodeRejectsUnsafeFieldNames(t *testing.T) {
	ctx := context.Background()
	for _, fieldName := range []string{"单位.采购明细", "bad;name", "bad`name", "bad\nname"} {
		catalog := metadata.Catalog{Databases: []metadata.Database{{
			Name: "workspace",
			Tables: []metadata.Table{{
				Name:   "contacts",
				Fields: []metadata.Field{{Name: "name", Type: "string"}},
			}},
		}}}
		server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
		if err := system.SaveGrant(ctx, permission.Grant{
			SubjectID: "owner",
			Scope:     permission.ScopeFieldSet,
			Resource:  "workspace.contacts",
			Level:     permission.Write,
		}); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(metadataPath)
		if err != nil {
			t.Fatal(err)
		}

		_, err = server.workflowAutableService().CreateFields(ctx, map[string]any{
			"table": "contacts",
			"fields": map[string]any{
				fieldName: "string",
			},
		}, workflow.RuntimeInfo{DatabaseName: "workspace", CreatorID: "owner"})
		if err == nil || !strings.Contains(err.Error(), "is unsafe") {
			t.Fatalf("expected unsafe field name error for %q, got %v", fieldName, err)
		}
		after, err := os.ReadFile(metadataPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("unsafe field create for %q must not rewrite metadata", fieldName)
		}
	}
}

func TestWorkflowFieldCreateNodeAllowsReadableBusinessFieldNames(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "workspace.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := server.workflowAutableService().CreateFields(ctx, map[string]any{
		"table": "contacts",
		"fields": map[string]any{
			"当前负责人（人员）": "string",
			"采购 明细":     "string",
		},
	}, workflow.RuntimeInfo{DatabaseName: "workspace", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := metadata.Load(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	tableMeta, ok := loaded.Table("workspace", "contacts")
	if !ok {
		t.Fatal("expected contacts table")
	}
	if _, ok := tableMeta.Field("当前负责人（人员）"); !ok {
		t.Fatalf("expected Chinese field in metadata, got %#v", tableMeta.Fields)
	}
	if _, ok := tableMeta.Field("采购 明细"); !ok {
		t.Fatalf("expected spaced field in metadata, got %#v", tableMeta.Fields)
	}
}

func TestWorkflowFieldCreateNodeAddsExternalCreatedFieldsOnFirstRun(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name: "b表",
			Fields: []metadata.Field{
				{Name: "name", Type: "string", Deleted: true},
			},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "workspace.b表",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	output, err := server.workflowAutableService().CreateFields(ctx, map[string]any{
		"table": "b表",
		"fields": map[string]any{
			"Assignees":                   "string",
			"Created":                     "string",
			"Created By":                  "string",
			"Description":                 "string",
			"Done":                        "string",
			"Done At":                     "string",
			"Identifier":                  "string",
			"Percent Done":                "string",
			"Priority":                    "string",
			"Project ID":                  "string",
			"Task ID":                     "string",
			"Title":                       "string",
			"Updated":                     "string",
			"dingtalk_created_time":       "string",
			"dingtalk_last_modified_time": "string",
			"dingtalk_record_id":          "string",
			"工单总结":                        "string",
		},
	}, workflow.RuntimeInfo{DatabaseName: "workspace", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(output["created"].([]map[string]any)) != 17 {
		t.Fatalf("unexpected created fields: %#v", output)
	}
	loaded, err := metadata.Load(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	tableMeta, ok := loaded.Table("workspace", "b表")
	if !ok {
		t.Fatal("expected b表 metadata")
	}
	if _, ok := tableMeta.Field("Created"); !ok {
		t.Fatalf("expected Created in metadata, got %#v", tableMeta.Fields)
	}
	repository, err := recorddb.OpenCatalog(ctx, loaded, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := repository.CreateRow(ctx, "workspace", tableMeta, map[string]any{
		"Created": "1780493834000",
		"Updated": "1780620092000",
		"Title":   "问卷提交 [indask]",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowFieldCreateNodeRequiresTableWrite(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
	server, _, _ := newTestServerWithMetadataFile(t, catalog)

	if _, err := server.workflowAutableService().CreateFields(ctx, map[string]any{
		"table":  "contacts",
		"fields": []any{"email"},
	}, workflow.RuntimeInfo{DatabaseName: "workspace", CreatorID: "viewer"}); !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestWorkflowRowUpsertUpdatesFirstMatchOrCreates(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "owner", "db.contacts")

	autable := server.workflowAutableService()
	created, err := autable.CreateRow(ctx, map[string]any{
		"table":  "contacts",
		"values": map[string]any{"name": "Old", "email": "remote-1", "status": "todo"},
	}, workflow.RuntimeInfo{DatabaseName: "db", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	original := created["record"].(map[string]any)

	updated, err := autable.UpsertRow(ctx, map[string]any{
		"table":       "contacts",
		"match_field": "email",
		"values":      map[string]any{"name": "Updated", "email": "remote-1", "status": "done"},
	}, workflow.RuntimeInfo{DatabaseName: "db", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if updated["operation"] != "update" {
		t.Fatalf("expected update operation, got %#v", updated)
	}
	updatedRecord := updated["record"].(map[string]any)
	if updatedRecord["record_id"] != original["record_id"] {
		t.Fatalf("expected existing record to be updated, got original=%#v updated=%#v", original, updatedRecord)
	}
	updatedValues := updatedRecord["values"].(map[string]any)
	if updatedValues["name"] != "Updated" || updatedValues["status"] != "done" {
		t.Fatalf("unexpected updated values: %#v", updatedValues)
	}
	recordID := updatedRecord["record_id"].(int64)
	historyBefore, err := server.history.GetPrefix(ctx, history.RowPrefix("db", "contacts", recordID))
	if err != nil {
		t.Fatal(err)
	}

	nooped, err := autable.UpsertRow(ctx, map[string]any{
		"table":       "contacts",
		"match_field": "email",
		"values":      map[string]any{"name": "Updated", "email": "remote-1", "status": "done"},
	}, workflow.RuntimeInfo{DatabaseName: "db", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if nooped["operation"] != "noop" {
		t.Fatalf("expected noop operation, got %#v", nooped)
	}
	historyAfter, err := server.history.GetPrefix(ctx, history.RowPrefix("db", "contacts", recordID))
	if err != nil {
		t.Fatal(err)
	}
	if len(historyAfter) != len(historyBefore) {
		t.Fatalf("noop upsert should not create row history: before=%d after=%d", len(historyBefore), len(historyAfter))
	}

	upserted, err := autable.UpsertRow(ctx, map[string]any{
		"table":       "contacts",
		"match_field": "email",
		"values":      map[string]any{"name": "Created", "email": "remote-2", "status": "todo"},
	}, workflow.RuntimeInfo{DatabaseName: "db", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if upserted["operation"] != "create" {
		t.Fatalf("expected create operation, got %#v", upserted)
	}
}

func TestWorkflowRowUpsertRequiresMatchValue(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := server.workflowAutableService().UpsertRow(ctx, map[string]any{
		"table":       "contacts",
		"match_field": "email",
		"values":      map[string]any{"name": "Ada"},
	}, workflow.RuntimeInfo{DatabaseName: "db", CreatorID: "owner"})
	if err == nil || !strings.Contains(err.Error(), "values.email is required") {
		t.Fatalf("expected missing match value error, got %v", err)
	}
}

func TestWorkflowRowQueryUsesPublicRowQueryOptions(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "owner", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
	)
	saveTestRecordCreateGrant(t, system, "owner", "db.contacts")

	autable := server.workflowAutableService()
	for _, values := range []map[string]any{
		{"name": "Ada", "email": "ada@example.com", "status": "active"},
		{"name": "Grace", "email": "grace@example.com", "status": "inactive"},
		{"name": "Alan", "email": "alan@example.com", "status": "active"},
	} {
		if _, err := autable.CreateRow(ctx, map[string]any{
			"table":  "contacts",
			"values": values,
		}, workflow.RuntimeInfo{DatabaseName: "db", CreatorID: "owner"}); err != nil {
			t.Fatal(err)
		}
	}

	output, err := autable.ListRows(ctx, map[string]any{
		"table": "contacts",
		"query": map[string]any{
			"field": "status",
			"op":    "=",
			"value": "active",
		},
		"sorts": []any{
			map[string]any{"field": "name", "direction": "desc"},
		},
		"limit": 1,
	}, workflow.RuntimeInfo{DatabaseName: "db", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	rows := output["rows"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("expected one queried row, got %#v", output)
	}
	values := rows[0]["values"].(map[string]any)
	if values["name"] != "Alan" || values["status"] != "active" {
		t.Fatalf("unexpected queried row: %#v", rows[0])
	}
}

func TestDatabaseOwnerCanManageRoles(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "email", Type: "string"},
			},
		}},
	}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	saveTestDatabaseOwners(t, system, "workspace", "owner")

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
			{"scope":"field_set","resource":"workspace.contacts","level":2},
			{"scope":"workflow_set","resource":"workspace","level":1},
			{"scope":"form","resource":"3","level":0}
		]
	}`))
	updateGrants.AddCookie(testSessionCookie(t, system, "owner"))
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, updateGrants)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected role grants update 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	_ = testSessionCookie(t, system, "u1")
	_ = testSessionCookie(t, system, "u2")
	workflow, err := system.SaveWorkflow(ctx, systemdb.WorkflowDefinition{
		DatabaseName: "workspace",
		Name:         "sync-contacts",
		CreatorID:    "owner",
		Script:       "function run() { return {}; }",
	})
	if err != nil {
		t.Fatal(err)
	}
	updateMembers := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/roles/editor/members", bytes.NewBufferString(`{
		"members":[{"type":"user","id":"u1"},{"type":"user","id":"u2"},{"type":"user","id":"u1"},{"type":"workflow","id":"`+strconv.FormatInt(workflow.ID, 10)+`"}]
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
	var roles []roleDefinitionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &roles); err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].SubjectID != "role:workspace:editor" || len(roles[0].Grants) != 2 || len(roles[0].Members) != 3 {
		t.Fatalf("unexpected roles response: %#v", roles)
	}
	if roles[0].Members[0] != (systemdb.RoleMember{Type: "user", ID: "u1"}) || roles[0].Members[1] != (systemdb.RoleMember{Type: "user", ID: "u2"}) {
		t.Fatalf("expected typed role members, got %#v", roles[0].Members)
	}
	if len(roles[0].MemberUsers) != 2 || roles[0].MemberUsers[0].Email != "u1@example.com" || roles[0].MemberUsers[1].Email != "u2@example.com" {
		t.Fatalf("expected member users in role response, got %#v", roles[0].MemberUsers)
	}
	if len(roles[0].MemberWorkflows) != 1 || roles[0].MemberWorkflows[0].Name != "sync-contacts" {
		t.Fatalf("expected member workflows in role response, got %#v", roles[0].MemberWorkflows)
	}
}

func TestRoleGrantValidationKeepsResourcesInsideDatabase(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{
			Name: "workspace",
			Tables: []metadata.Table{{
				Name: "contacts",
				Fields: []metadata.Field{
					{Name: "name", Type: "string"},
					{Name: "legacy", Type: "string", Deleted: true},
				},
			}},
		},
		{
			Name: "other",
			Tables: []metadata.Table{{
				Name:   "contacts",
				Fields: []metadata.Field{{Name: "name", Type: "string"}},
			}},
		},
	}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	workspaceWorkflow, err := system.SaveWorkflow(ctx, systemdb.WorkflowDefinition{
		DatabaseName: "workspace",
		Name:         "workspace-flow",
		Script:       "function instances(info) { return { noop: \"echo\" }; } function run() {}",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherWorkflow, err := system.SaveWorkflow(ctx, systemdb.WorkflowDefinition{
		DatabaseName: "other",
		Name:         "other-flow",
		Script:       "function instances(info) { return { noop: \"echo\" }; } function run() {}",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceForm, err := system.SaveForm(ctx, systemdb.FormDefinition{
		DatabaseName: "workspace",
		Name:         "workspace-form",
		Script:       "function render(api, root) { root.append(api.input({ field: 'name' }), api.submit('Save')); return { table: 'contacts' }; }",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherForm, err := system.SaveForm(ctx, systemdb.FormDefinition{
		DatabaseName: "other",
		Name:         "other-form",
		Script:       "function render(api, root) { root.append(api.input({ field: 'name' }), api.submit('Save')); return { table: 'contacts' }; }",
	})
	if err != nil {
		t.Fatal(err)
	}

	valid := []permission.Grant{
		{Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.Read},
		{Scope: permission.ScopeRecord, Resource: "workspace.contacts", Field: "create", Level: permission.Write},
		{Scope: permission.ScopeWorkflow, Resource: resourceID(workspaceWorkflow.ID), Level: permission.Read},
		{Scope: permission.ScopeForm, Resource: resourceID(workspaceForm.ID), Level: permission.Read},
		{Scope: permission.ScopeFieldSet, Resource: "other.contacts", Level: permission.None},
	}
	if err := server.validateRoleGrants(ctx, "workspace", valid); err != nil {
		t.Fatalf("expected valid workspace grants, got %v", err)
	}

	invalidCases := []struct {
		name  string
		grant permission.Grant
	}{
		{name: "other table", grant: permission.Grant{Scope: permission.ScopeFieldSet, Resource: "other.contacts", Level: permission.Read}},
		{name: "other field", grant: permission.Grant{Scope: permission.ScopeField, Resource: "other.contacts", Field: "name", Level: permission.Read}},
		{name: "other record", grant: permission.Grant{Scope: permission.ScopeRecord, Resource: "other.contacts", Field: "create", Level: permission.Write}},
		{name: "record read level", grant: permission.Grant{Scope: permission.ScopeRecord, Resource: "workspace.contacts", Field: "create", Level: permission.Read}},
		{name: "record unknown action", grant: permission.Grant{Scope: permission.ScopeRecord, Resource: "workspace.contacts", Field: "archive", Level: permission.Write}},
		{name: "deleted field", grant: permission.Grant{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "legacy", Level: permission.Read}},
		{name: "record id field", grant: permission.Grant{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "ct_record_id", Level: permission.Read}},
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

	mixedFieldGrants := []permission.Grant{
		{Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.Read},
		{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "name", Level: permission.Read},
	}
	if err := server.validateRoleGrants(ctx, "workspace", mixedFieldGrants); err == nil {
		t.Fatal("expected field set and field grants to be mutually exclusive")
	}

	mixedWorkflowGrants := []permission.Grant{
		{Scope: permission.ScopeWorkflowSet, Resource: "workspace", Level: permission.Read},
		{Scope: permission.ScopeWorkflow, Resource: resourceID(workspaceWorkflow.ID), Level: permission.Read},
	}
	if err := server.validateRoleGrants(ctx, "workspace", mixedWorkflowGrants); err == nil {
		t.Fatal("expected workflow set and workflow grants to be mutually exclusive")
	}
}

func TestRoleGrantAPIRejectsCrossDatabaseResources(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{Name: "workspace", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "string"}}}}},
		{Name: "other", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "string"}}}}},
	}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	saveTestDatabaseOwners(t, system, "workspace", "owner")
	if _, err := system.SaveRole(ctx, systemdb.RoleDefinition{DatabaseName: "workspace", Name: "editor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleGrants(ctx, "workspace", "editor", []permission.Grant{
		{Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.Read},
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/databases/workspace/roles/editor/grants", bytes.NewBufferString(`{
		"grants":[{"scope":"field_set","resource":"other.contacts","level":2}]
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
		{Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
		{Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleMembers(ctx, "db", "editor", []systemdb.RoleMember{{Type: "user", ID: "u1"}}); err != nil {
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

func TestRoleFieldGrantAllowsOnlyGrantedFields(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "owner", "db.contacts")
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
		{Scope: permission.ScopeField, Resource: "db.contacts", Field: "name", Level: permission.Write},
		{Scope: permission.ScopeField, Resource: "db.contacts", Field: "email", Level: permission.Read},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleMembers(ctx, "db", "editor", []systemdb.RoleMember{{Type: "user", ID: "u1"}}); err != nil {
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
		t.Fatalf("expected role member ungranted field write 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
}

func TestRoleManagementRequiresDatabaseWrite(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
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
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	request := httptest.NewRequest(http.MethodPost, "/api/databases/workspace/tables", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[{"name":"name","type":"string"}]
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
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeViewSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")
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

	viewSortOverride := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows?view=active-a&sort_field=name&sort_direction=asc", nil)
	viewSortOverride.AddCookie(testSessionCookie(t, system, "u1"))
	viewSortOverrideRecorder := httptest.NewRecorder()
	server.ServeHTTP(viewSortOverrideRecorder, viewSortOverride)
	if viewSortOverrideRecorder.Code != http.StatusOK {
		t.Fatalf("expected view temporary sort 200, got %d: %s", viewSortOverrideRecorder.Code, viewSortOverrideRecorder.Body.String())
	}
	var overrideRows []rowResponse
	if err := json.NewDecoder(viewSortOverrideRecorder.Body).Decode(&overrideRows); err != nil {
		t.Fatal(err)
	}
	if len(overrideRows) != 2 || overrideRows[0].Values["name"] != "Ada" || overrideRows[1].Values["name"] != "Grace" {
		t.Fatalf("temporary sort did not override view sort: %#v", overrideRows)
	}

	sortRequest := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows?sort_field=name&sort_direction=asc", nil)
	sortRequest.AddCookie(testSessionCookie(t, system, "u1"))
	sortRecorder := httptest.NewRecorder()
	server.ServeHTTP(sortRecorder, sortRequest)
	if sortRecorder.Code != http.StatusOK {
		t.Fatalf("expected temporary sort 200, got %d: %s", sortRecorder.Code, sortRecorder.Body.String())
	}
	var sortedRows []rowResponse
	if err := json.NewDecoder(sortRecorder.Body).Decode(&sortedRows); err != nil {
		t.Fatal(err)
	}
	if len(sortedRows) != 3 || sortedRows[0].Values["name"] != "Ada" || sortedRows[1].Values["name"] != "Grace" || sortedRows[2].Values["name"] != "Linus" {
		t.Fatalf("unexpected temporary sort order: %#v", sortedRows)
	}

	invalidSort := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows?sort_field=name&sort_direction=sideways", nil)
	invalidSort.AddCookie(testSessionCookie(t, system, "u1"))
	invalidRecorder := httptest.NewRecorder()
	server.ServeHTTP(invalidRecorder, invalidSort)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid temporary sort 400, got %d: %s", invalidRecorder.Code, invalidRecorder.Body.String())
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

	deniedSort := httptest.NewRequest(http.MethodGet, "/api/tables/db/contacts/rows?sort_field=status&sort_direction=desc", nil)
	deniedSort.AddCookie(testSessionCookie(t, system, "reader"))
	deniedSortRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedSortRecorder, deniedSort)
	if deniedSortRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected unreadable temporary sort 403, got %d: %s", deniedSortRecorder.Code, deniedSortRecorder.Body.String())
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
	repository, err := recorddb.OpenCatalog(ctx, catalog, t.TempDir())
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
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")

	request := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
	request.AddCookie(testSessionCookie(t, system, "u1"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	tableMeta, _ := catalog.Table("db", "contacts")
	rows, err := repository.Rows(ctx, "db", tableMeta)
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
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeFormSet, Resource: "db", Level: permission.Write},
	)

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"notify",
		"script":"function instances(info) { return { noop: \"echo\" }; } function run() {}",
		"secrets":{"TOKEN":"secret"},
		"variables":{"CHANNEL":"ops"}
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}

	workflowResponseBody := workflowRecorder.Body.String()
	var workflow workflowDefinitionResponse
	if err := json.Unmarshal([]byte(workflowResponseBody), &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.DatabaseName != "db" {
		t.Fatalf("expected db-level workflow, got %#v", workflow)
	}
	if workflow.Secrets["TOKEN"] != len("secret") {
		t.Fatalf("expected workflow response to expose secret length only, got %#v", workflow.Secrets)
	}
	if strings.Contains(workflowResponseBody, `"TOKEN":"secret"`) || strings.Contains(workflowResponseBody, "hidden-token") {
		t.Fatalf("workflow response leaked secret value: %s", workflowResponseBody)
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
	listWorkflowsResponseBody := listWorkflowsRecorder.Body.String()
	var workflows []workflowDefinitionResponse
	if err := json.Unmarshal([]byte(listWorkflowsResponseBody), &workflows); err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || workflows[0].ID != workflow.ID {
		t.Fatalf("unexpected workflow list: %#v", workflows)
	}
	if workflows[0].Secrets["TOKEN"] != len("secret") || strings.Contains(listWorkflowsResponseBody, `"TOKEN":"secret"`) || strings.Contains(listWorkflowsResponseBody, "hidden-token") {
		t.Fatalf("workflow list leaked secret value: %s", listWorkflowsResponseBody)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"contact-intake",
		"script":"function render(api, root) { root.append(api.input({ field: 'email' }), api.submit('Save')); return { table: 'contacts' }; }"
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
	workflowPath := filepath.Join(codeRoot, "workflow", "db", "notify.js")
	workflowScript, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(workflowScript) != workflow.Script {
		t.Fatalf("unexpected workflow code file: %s", workflowScript)
	}
	formPath := filepath.Join(codeRoot, "form", "db", "contact-intake.js")
	formScript, err := os.ReadFile(formPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(formScript) != form.Script {
		t.Fatalf("unexpected form code file: %s", formScript)
	}
	fileWorkflowScript := "function instances(info) { return { noop: \"echo\" }; } function run(info) { return { message: info.inputs.name + '-from-file' }; }"
	if err := os.WriteFile(workflowPath, []byte(fileWorkflowScript), 0o644); err != nil {
		t.Fatal(err)
	}
	getWorkflow = httptest.NewRequest(http.MethodGet, "/api/workflows/1", nil)
	getWorkflow.AddCookie(testSessionCookie(t, system, "u1"))
	getWorkflowRecorder = httptest.NewRecorder()
	server.ServeHTTP(getWorkflowRecorder, getWorkflow)
	if getWorkflowRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow reload 200, got %d: %s", getWorkflowRecorder.Code, getWorkflowRecorder.Body.String())
	}
	var reloadedWorkflow workflowDefinitionResponse
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

	fileFormScript := "function render(api, root) { root.append(api.input({ field: 'name', label: 'From file' }), api.submit('Save')); return { table: 'contacts' }; }"
	if err := os.WriteFile(formPath, []byte(fileFormScript), 0o644); err != nil {
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

	renameWorkflow := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"id":1,
		"name":"renamed-notify",
		"script":"function instances(info) { return { noop: \"echo\" }; } function run() { return { renamed: true }; }",
		"secrets":{},
		"variables":{}
	}`))
	renameWorkflow.AddCookie(testSessionCookie(t, system, "u1"))
	renameWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(renameWorkflowRecorder, renameWorkflow)
	if renameWorkflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow rename 201, got %d: %s", renameWorkflowRecorder.Code, renameWorkflowRecorder.Body.String())
	}
	if _, err := os.Stat(workflowPath); !os.IsNotExist(err) {
		t.Fatalf("expected old workflow file to be removed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(codeRoot, "workflow", "db", "renamed-notify.js")); err != nil {
		t.Fatalf("expected renamed workflow file, got %v", err)
	}
}

func TestWorkflowAndFormCreationRequiresSetOrDatabaseWrite(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)

	deniedWorkflow := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"denied",
		"script":"function instances(info) { return { noop: \"echo\" }; } function run() { return {}; }"
	}`))
	deniedWorkflow.AddCookie(testSessionCookie(t, system, "viewer"))
	deniedWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedWorkflowRecorder, deniedWorkflow)
	if deniedWorkflowRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected workflow create 403, got %d: %s", deniedWorkflowRecorder.Code, deniedWorkflowRecorder.Body.String())
	}

	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "table-owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	tableOwnerWorkflow := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"allowed-by-table",
		"script":"function instances(info) { return { noop: \"echo\" }; } function run() { return {}; }"
	}`))
	tableOwnerWorkflow.AddCookie(testSessionCookie(t, system, "table-owner"))
	tableOwnerWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(tableOwnerWorkflowRecorder, tableOwnerWorkflow)
	if tableOwnerWorkflowRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected field set writer workflow create 403, got %d: %s", tableOwnerWorkflowRecorder.Code, tableOwnerWorkflowRecorder.Body.String())
	}

	saveTestGrants(t, system,
		permission.Grant{SubjectID: "workflow-owner", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write},
		permission.Grant{SubjectID: "form-owner", Scope: permission.ScopeFormSet, Resource: "db", Level: permission.Write},
	)
	saveTestDatabaseOwners(t, system, "db", "db-owner")
	workflowOwnerWorkflow := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"allowed-by-workflow-set",
		"script":"function instances(info) { return { noop: \"echo\" }; } function run() { return {}; }"
	}`))
	workflowOwnerWorkflow.AddCookie(testSessionCookie(t, system, "workflow-owner"))
	workflowOwnerWorkflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowOwnerWorkflowRecorder, workflowOwnerWorkflow)
	if workflowOwnerWorkflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow set writer workflow create 201, got %d: %s", workflowOwnerWorkflowRecorder.Code, workflowOwnerWorkflowRecorder.Body.String())
	}
	formOwnerForm := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"allowed-by-form-set",
		"script":"function render(api, root) { root.append(api.input({ field: 'email' }), api.submit('Save')); return { table: 'contacts' }; }"
	}`))
	formOwnerForm.AddCookie(testSessionCookie(t, system, "form-owner"))
	formOwnerFormRecorder := httptest.NewRecorder()
	server.ServeHTTP(formOwnerFormRecorder, formOwnerForm)
	if formOwnerFormRecorder.Code != http.StatusCreated {
		t.Fatalf("expected form set writer form create 201, got %d: %s", formOwnerFormRecorder.Code, formOwnerFormRecorder.Body.String())
	}
	dbOwnerForm := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"allowed-by-db",
		"script":"function render(api, root) { root.append(api.input({ field: 'email' }), api.submit('Save')); return { table: 'contacts' }; }"
	}`))
	dbOwnerForm.AddCookie(testSessionCookie(t, system, "db-owner"))
	dbOwnerFormRecorder := httptest.NewRecorder()
	server.ServeHTTP(dbOwnerFormRecorder, dbOwnerForm)
	if dbOwnerFormRecorder.Code != http.StatusCreated {
		t.Fatalf("expected db owner form create 201, got %d: %s", dbOwnerFormRecorder.Code, dbOwnerFormRecorder.Body.String())
	}
}

func TestDatabaseOwnerCanManageDatabaseWorkflowsAndForms(t *testing.T) {
	server, system := newTestServer(t)
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "set-owner", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write},
		permission.Grant{SubjectID: "set-owner", Scope: permission.ScopeFormSet, Resource: "db", Level: permission.Write},
	)
	saveTestDatabaseOwners(t, system, "db", "db-owner")

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"owned-by-set",
		"script":"function instances(info) { return { noop: \"echo\" }; } function run(info) { return { message: info.inputs.name }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "set-owner"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow set owner workflow create 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	var workflow workflowDefinitionResponse
	if err := json.NewDecoder(workflowRecorder.Body).Decode(&workflow); err != nil {
		t.Fatal(err)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"owned-by-set",
		"script":"function render(api, root) { root.append(api.input({ field: 'name' }), api.submit('Save')); return { table: 'contacts' }; }"
	}`))
	formRequest.AddCookie(testSessionCookie(t, system, "set-owner"))
	formRecorder := httptest.NewRecorder()
	server.ServeHTTP(formRecorder, formRequest)
	if formRecorder.Code != http.StatusCreated {
		t.Fatalf("expected form set owner form create 201, got %d: %s", formRecorder.Code, formRecorder.Body.String())
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
	var workflows []workflowDefinitionResponse
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
		"name":"owned-by-set",
		"script":"function instances(info) { return { noop: \"echo\" }; } function run() { return { updated: true }; }"
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
		"name":"owned-by-set",
		"script":"function render(api, root) { root.append(api.input({ field: 'name' }), api.submit('Save')); return { table: 'contacts' }; }"
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
		{Name: "db", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "string"}}}}},
		{Name: "other", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "string"}}}}},
	}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeFormSet, Resource: "db", Level: permission.Write},
	)

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"notify",
		"script":"function instances(info) { return { noop: \"echo\" }; } function run() { return {}; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow create 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	var savedWorkflow workflowDefinitionResponse
	if err := json.NewDecoder(workflowRecorder.Body).Decode(&savedWorkflow); err != nil {
		t.Fatal(err)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"intake",
		"script":"function render(api, root) { root.append(api.input({ field: 'email' }), api.submit('Save')); return { table: 'contacts' }; }"
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
		"script":"function instances(info) { return { noop: \"echo\" }; } function run() { return { moved: true }; }"
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
		"script":"function instances(info) { return { noop: \"echo\" }; } function run() { return { moved: true }; }"
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
		"script":"function render(api, root) { root.append(api.input({ field: 'name' }), api.submit('Save')); return { table: 'contacts' }; }"
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
		"script":"function render(api, root) { root.append(api.input({ field: 'name' }), api.submit('Save')); return { table: 'contacts' }; }"
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
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"welcome",
		"script":"function instances(info) { return { welcome_echo: { node: \"echo\", variables: [{ name: \"suffix\", type: \"string\" }] } }; }\nfunction run(info) { const echoed = info.instance(\"welcome_echo\").exec({ value: info.inputs.name }); return { message: echoed.value + \"-done\" }; }",
		"variables":{"welcome_echo.suffix":"done"}
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
	if len(runResponse.Run.Steps) != 1 || runResponse.Run.Steps[0].NodeID != "welcome_echo" || runResponse.Run.Steps[0].NodeType != "echo" {
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
	if !runs[0].Summary || runs[0].Run.Inputs != nil || runs[0].Run.Outputs != nil || len(runs[0].Run.Steps) != 0 {
		t.Fatalf("expected workflow run list to return summary only, got %#v", runs[0])
	}
	detail := fetchWorkflowRun(t, server, system, saved.ID, runResponse.HistoryKey, "u1")
	if detail.Summary || detail.Run.Outputs["message"] != "Ada-done" {
		t.Fatalf("unexpected workflow run detail: %#v", detail)
	}
}

func TestWorkflowTableCreateNodeUsesWorkflowPermissions(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	saveTestGrants(t, system, permission.Grant{SubjectID: "creator", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"create-contact",
		"script":"function instances(info) { return { create_contact: \"table.row.create\" }; }\nfunction run(info) { const created = info.instance(\"create_contact\").exec({ table: \"contacts\", values: { name: info.inputs.name } }); return { record_id: created.record.record_id, name: created.record.values.name, database: info.database_name }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "creator"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	workflowSubject := systemdb.WorkflowSubjectID(1)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: workflowSubject,
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, workflowSubject, "db.contacts")

	runRequest := httptest.NewRequest(http.MethodPost, "/api/workflows/1/runs", bytes.NewBufferString(`{"inputs":{"name":"Ada"}}`))
	runRequest.AddCookie(testSessionCookie(t, system, "creator"))
	runRecorder := httptest.NewRecorder()
	server.ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow run 201, got %d: %s", runRecorder.Code, runRecorder.Body.String())
	}
	var response workflowRunResponse
	if err := json.NewDecoder(runRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Run.Outputs["record_id"] != float64(1) || response.Run.Outputs["name"] != "Ada" || response.Run.Outputs["database"] != "db" {
		t.Fatalf("unexpected table node outputs: %#v", response.Run.Outputs)
	}
	if len(response.Run.Steps) != 1 || response.Run.Steps[0].NodeID != "create_contact" || response.Run.Steps[0].NodeType != "table.row.create" {
		t.Fatalf("unexpected table node steps: %#v", response.Run.Steps)
	}
}

func TestWorkflowTableCreateNodeIgnoresRunnerPermissions(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	saveTestGrants(t, system, permission.Grant{SubjectID: "creator", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"create-contact",
		"script":"function instances(info) { return { create_contact: \"table.row.create\" }; }\nfunction run(info) { return info.instance(\"create_contact\").exec({ table: \"contacts\", values: { name: \"Ada\" } }); }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "creator"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "runner",
		Scope:     permission.ScopeWorkflow,
		Resource:  "1",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	runRequest := httptest.NewRequest(http.MethodPost, "/api/workflows/1/runs", bytes.NewBufferString(`{"inputs":{}}`))
	runRequest.AddCookie(testSessionCookie(t, system, "runner"))
	runRecorder := httptest.NewRecorder()
	server.ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected workflow run 400, got %d: %s", runRecorder.Code, runRecorder.Body.String())
	}
	var response workflowRunResponse
	if err := json.NewDecoder(runRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Run.Error == "" || len(response.Run.Steps) != 1 || response.Run.Steps[0].Error == "" {
		t.Fatalf("expected workflow subject permission failure in run history, got %#v", response.Run)
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
	expectedTypes := []string{
		"dingtalk.approval.create",
		"dingtalk.notable.records.list",
		"dingtalk.robot.oto.batch_send",
		"dingtalk.robot.send",
		"echo",
		"github.file.content.get",
		"kingdee.purchaseorder.list",
		"table.field.create",
		"table.record.changed",
		"table.row.create",
		"table.row.delete",
		"table.row.list",
		"table.row.query",
		"table.row.upsert",
		"table.row.update",
		"time.schedule",
	}
	if len(nodes) != len(expectedTypes) {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
	byType := map[string]workflow.NodeInfo{}
	for _, node := range nodes {
		byType[node.Type] = node
	}
	for _, expectedType := range expectedTypes {
		node, ok := byType[expectedType]
		if !ok {
			t.Fatalf("expected node %q in %#v", expectedType, nodes)
		}
		if node.Documentation["en-US"] == "" || node.Documentation["zh-CN"] == "" {
			t.Fatalf("expected bilingual docs for node %q: %#v", expectedType, node.Documentation)
		}
	}
	dingtalk := byType["dingtalk.robot.send"]
	if len(dingtalk.Inputs) != 3 || dingtalk.Inputs[0].Name != "content" {
		t.Fatalf("expected dingtalk node inputs: %#v", dingtalk)
	}
	if len(dingtalk.Secrets) != 1 || dingtalk.Secrets[0].Name != "access_token" {
		t.Fatalf("expected dingtalk node secrets: %#v", dingtalk)
	}
	dingtalkRobotOTO := byType["dingtalk.robot.oto.batch_send"]
	if len(dingtalkRobotOTO.Inputs) != 3 || dingtalkRobotOTO.Inputs[0].Name != "userIds" || dingtalkRobotOTO.Inputs[1].Name != "msgKey" || dingtalkRobotOTO.Inputs[2].Name != "msgParam" {
		t.Fatalf("expected dingtalk robot OTO node inputs: %#v", dingtalkRobotOTO)
	}
	if len(dingtalkRobotOTO.Variables) != 1 || dingtalkRobotOTO.Variables[0].Name != "robot_code" {
		t.Fatalf("expected dingtalk robot OTO node variables: %#v", dingtalkRobotOTO)
	}
	if len(dingtalkRobotOTO.Secrets) != 2 || dingtalkRobotOTO.Secrets[0].Name != "app_key" || dingtalkRobotOTO.Secrets[1].Name != "app_secret" {
		t.Fatalf("expected dingtalk robot OTO node secrets: %#v", dingtalkRobotOTO)
	}
	fieldCreate := byType["table.field.create"]
	if len(fieldCreate.Inputs) != 3 || fieldCreate.Inputs[0].Name != "database" || fieldCreate.Inputs[2].Name != "fields" {
		t.Fatalf("expected table field create inputs: %#v", fieldCreate)
	}
	upsert := byType["table.row.upsert"]
	if len(upsert.Inputs) != 4 || upsert.Inputs[2].Name != "match_field" || upsert.Outputs[1].Name != "operation" {
		t.Fatalf("expected table row upsert ports: %#v", upsert)
	}
	notable := byType["dingtalk.notable.records.list"]
	if len(notable.Inputs) != 4 || notable.Inputs[0].Name != "field_id_or_names" {
		t.Fatalf("expected dingtalk notable node inputs: %#v", notable)
	}
	if len(notable.Variables) != 3 || notable.Variables[0].Name != "base_id" || notable.Variables[1].Name != "sheet_id_or_name" || notable.Variables[2].Name != "operator_id" {
		t.Fatalf("expected dingtalk notable node variables: %#v", notable)
	}
	if len(notable.Secrets) != 2 || notable.Secrets[0].Name != "app_key" || notable.Secrets[1].Name != "app_secret" {
		t.Fatalf("expected dingtalk notable node secrets: %#v", notable)
	}
	githubContent := byType["github.file.content.get"]
	if len(githubContent.Inputs) != 4 || githubContent.Inputs[0].Name != "owner" || githubContent.Inputs[1].Name != "repo" || githubContent.Inputs[2].Name != "path" || githubContent.Inputs[3].Name != "ref" {
		t.Fatalf("expected github content node inputs: %#v", githubContent)
	}
	if len(githubContent.Secrets) != 1 || githubContent.Secrets[0].Name != "token" {
		t.Fatalf("expected github content node secret: %#v", githubContent)
	}
	if !byType["table.record.changed"].Trigger || len(byType["table.record.changed"].Inputs) == 0 || len(byType["table.record.changed"].Outputs) == 0 || !byType["time.schedule"].Trigger {
		t.Fatalf("expected trigger node ports: %#v", nodes)
	}
}

func TestWorkflowRunAPIWithRecordChangedTrigger(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})
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
		"script":"function instances(info) { return { row_change: \"table.record.changed\" }; }\nfunction run(info) { return { record_id: info.inputs.record.record_id, name: info.inputs.values.name }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}

	runRequest := httptest.NewRequest(http.MethodPost, "/api/workflows/1/runs", bytes.NewBufferString(`{"inputs":{"history_key":"`+historyKey+`","record":{"record_id":9,"table":"contacts"},"values":{"name":"Ada"}}}`))
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
	if len(response.Run.Steps) != 0 {
		t.Fatalf("trigger node should feed run inputs without a run step: %#v", response.Run.Steps)
	}
}

func TestRowCreateAutomaticallyRunsMatchingWorkflowTrigger(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"auto-contact",
		"script":"function instances(info) { return { row_change: \"table.record.changed\" }; }\nfunction trigger(info) { return { instance: \"row_change\", params: { table: \"contacts\", operations: [\"create\"], fields: [\"name\"] } }; }\nfunction run(info) { return { operation: info.inputs.operation, record_id: info.inputs.record.record_id, name: info.inputs.diff.name.new }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	saveTestGrants(t, system, permission.Grant{SubjectID: systemdb.WorkflowSubjectID(1), Scope: permission.ScopeField, Resource: "db.contacts", Field: "name", Level: permission.Read})

	createRow := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
	createRow.AddCookie(testSessionCookie(t, system, "u1"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRow)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected row create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/workflows/1/runs", nil)
	listRequest.AddCookie(testSessionCookie(t, system, "u1"))
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow runs 200, got %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var runs []workflowRunResponse
	if err := json.NewDecoder(listRecorder.Body).Decode(&runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one automatic workflow run, got %#v", runs)
	}
	run := fetchWorkflowRun(t, server, system, 1, runs[0].HistoryKey, "u1")
	if run.Run.Inputs["operation"] != "create" || run.Run.Outputs["name"] != "Ada" || run.Run.Outputs["record_id"] != float64(1) {
		t.Fatalf("unexpected automatic workflow run: %#v", run.Run)
	}
	if len(run.Run.Steps) != 0 {
		t.Fatalf("trigger node should feed run inputs without being executed in run steps: %#v", run.Run.Steps)
	}
}

func TestWorkflowWorkersConsumeRowChangeEvents(t *testing.T) {
	ctx := context.Background()
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server, system := newTestServer(t)
	server.StartWorkflowWorkers(workerCtx)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"worker-contact",
		"script":"function instances(info) { return { row_change: \"table.record.changed\" }; }\nfunction trigger(info) { return { instance: \"row_change\", params: { table: \"contacts\", operations: [\"create\"], fields: [\"name\"] } }; }\nfunction run(info) { return { name: info.inputs.diff.name.new }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	saveTestGrants(t, system, permission.Grant{SubjectID: systemdb.WorkflowSubjectID(1), Scope: permission.ScopeField, Resource: "db.contacts", Field: "name", Level: permission.Read})

	createRow := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
	createRow.AddCookie(testSessionCookie(t, system, "u1"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRow)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected row create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	runs := waitWorkflowRunCount(t, server, system, 1, "u1", 1)
	run := fetchWorkflowRun(t, server, system, 1, runs[0].HistoryKey, "u1")
	if run.Run.Outputs["name"] != "Ada" {
		t.Fatalf("unexpected worker workflow run: %#v", run.Run)
	}
	if len(run.Run.Steps) != 0 {
		t.Fatalf("trigger node should not be called from run: %#v", run.Run.Steps)
	}
}

func TestRowCreateDoesNotRunWorkflowWhenTriggerFieldsDoNotMatch(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"status-only",
		"script":"function instances(info) { return { row_change: \"table.record.changed\" }; }\nfunction trigger(info) { return { instance: \"row_change\", params: { table: \"contacts\", operations: [\"create\"], fields: [\"status\"] } }; }\nfunction run(info) { return { unexpected: true }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	saveTestGrants(t, system, permission.Grant{SubjectID: systemdb.WorkflowSubjectID(1), Scope: permission.ScopeField, Resource: "db.contacts", Field: "name", Level: permission.Read})

	createRow := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
	createRow.AddCookie(testSessionCookie(t, system, "u1"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRow)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected row create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/workflows/1/runs", nil)
	listRequest.AddCookie(testSessionCookie(t, system, "u1"))
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow runs 200, got %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var runs []workflowRunResponse
	if err := json.NewDecoder(listRecorder.Body).Decode(&runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no automatic workflow runs, got %#v", runs)
	}
}

func TestRowCreateDoesNotExposeUnreadableFieldsToWorkflowTrigger(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"no-field-access",
		"script":"function instances(info) { return { row_change: \"table.record.changed\" }; }\nfunction trigger(info) { return { instance: \"row_change\", params: { table: \"contacts\", operations: [\"create\"], fields: [\"name\"] } }; }\nfunction run(info) { return { unexpected: info.inputs.diff.name.new }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}

	createRow := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
	createRow.AddCookie(testSessionCookie(t, system, "u1"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRow)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected row create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/workflows/1/runs", nil)
	listRequest.AddCookie(testSessionCookie(t, system, "u1"))
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected workflow runs 200, got %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var runs []workflowRunResponse
	if err := json.NewDecoder(listRecorder.Body).Decode(&runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no workflow runs without field read permission, got %#v", runs)
	}
}

func TestScheduleTickRunsIntervalWorkflowUsingRunHistory(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestGrants(t, system, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"interval-workflow",
		"script":"function instances(info) { return { every_interval: \"time.schedule\" }; }\nfunction trigger(info) { return { instance: \"every_interval\", params: { interval_ms: 15000 } }; }\nfunction run(info) { return { scheduled_at: info.inputs.scheduled_at, event: info.inputs.event }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}

	first := time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)
	server.dispatchScheduleTick(ctx, "db", first)
	assertWorkflowRunCount(t, server, system, 1, "u1", 1)
	server.dispatchScheduleTick(ctx, "db", first.Add(14*time.Second))
	assertWorkflowRunCount(t, server, system, 1, "u1", 1)
	server.dispatchScheduleTick(ctx, "db", first.Add(15*time.Second))
	runs := assertWorkflowRunCount(t, server, system, 1, "u1", 2)
	run := fetchWorkflowRun(t, server, system, 1, runs[1].HistoryKey, "u1")
	if run.Run.Outputs["event"] != "schedule" || run.Run.Outputs["scheduled_at"] != float64(first.Add(15*time.Second).UnixMilli()) {
		t.Fatalf("unexpected scheduled workflow output: %#v", run.Run.Outputs)
	}
}

func TestScheduleTickRunsDailyWorkflowOncePerDay(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestGrants(t, system, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write})

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"daily-workflow",
		"script":"function instances(info) { return { daily: \"time.schedule\" }; }\nfunction trigger(info) { return { instance: \"daily\", params: { daily_at: \"09:30\" } }; }\nfunction run(info) { return { scheduled_at: info.inputs.scheduled_at }; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "u1"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}

	day := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	server.dispatchScheduleTick(ctx, "db", day.Add(9*time.Hour+29*time.Minute))
	assertWorkflowRunCount(t, server, system, 1, "u1", 0)
	server.dispatchScheduleTick(ctx, "db", day.Add(9*time.Hour+30*time.Minute))
	assertWorkflowRunCount(t, server, system, 1, "u1", 1)
	server.dispatchScheduleTick(ctx, "db", day.Add(10*time.Hour))
	assertWorkflowRunCount(t, server, system, 1, "u1", 1)
	server.dispatchScheduleTick(ctx, "db", day.Add(24*time.Hour+9*time.Hour+30*time.Minute))
	assertWorkflowRunCount(t, server, system, 1, "u1", 2)
}

func TestWorkflowRunsListDefaultsToLatestRuns(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	saveTestGrants(t, system, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Read})
	saved, err := system.SaveWorkflow(ctx, systemdb.WorkflowDefinition{
		DatabaseName: "db",
		Name:         "many-runs",
		Script:       `function instances(info) { return { echo: "echo" }; } function run(info) { return info.inputs; }`,
		CreatorID:    "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= defaultWorkflowRunListLimit+5; index++ {
		if _, err := history.SaveWorkflowRun(ctx, server.history, history.WorkflowRun{
			WorkflowID: saved.ID,
			Timestamp:  int64(index),
			Outputs:    map[string]any{"index": index},
			Steps:      []history.StepRecord{},
		}); err != nil {
			t.Fatal(err)
		}
	}

	runs := fetchWorkflowRuns(t, server, system, saved.ID, "u1")
	if len(runs) != defaultWorkflowRunListLimit {
		t.Fatalf("expected default workflow run limit, got %d", len(runs))
	}
	if runs[0].Run.Timestamp != 6 || runs[len(runs)-1].Run.Timestamp != int64(defaultWorkflowRunListLimit+5) {
		t.Fatalf("expected latest runs in ascending order, first=%d last=%d", runs[0].Run.Timestamp, runs[len(runs)-1].Run.Timestamp)
	}
	if !runs[0].Summary || runs[0].Run.Outputs != nil || len(runs[0].Run.Steps) != 0 {
		t.Fatalf("expected run list to return summaries only, got %#v", runs[0])
	}

	request := httptest.NewRequest(http.MethodGet, "/api/workflows/"+strconv.FormatInt(saved.ID, 10)+"/runs?limit=2", nil)
	request.AddCookie(testSessionCookie(t, system, "u1"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected workflow runs 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var limited []workflowRunResponse
	if err := json.NewDecoder(recorder.Body).Decode(&limited); err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || limited[0].Run.Timestamp != int64(defaultWorkflowRunListLimit+4) || limited[1].Run.Timestamp != int64(defaultWorkflowRunListLimit+5) {
		t.Fatalf("unexpected limited runs: %#v", limited)
	}
}

func TestWorkflowAndFormPermissions(t *testing.T) {
	server, system := newTestServer(t)
	saveTestDatabaseOwners(t, system, "db", "owner")

	workflowRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/workflows", bytes.NewBufferString(`{
		"name":"restricted",
		"script":"function instances(info) { return { noop: \"echo\" }; } function run(info) { return info.inputs; }"
	}`))
	workflowRequest.AddCookie(testSessionCookie(t, system, "owner"))
	workflowRecorder := httptest.NewRecorder()
	server.ServeHTTP(workflowRecorder, workflowRequest)
	if workflowRecorder.Code != http.StatusCreated {
		t.Fatalf("expected workflow 201, got %d: %s", workflowRecorder.Code, workflowRecorder.Body.String())
	}
	var createdWorkflow workflowDefinitionResponse
	if err := json.NewDecoder(workflowRecorder.Body).Decode(&createdWorkflow); err != nil {
		t.Fatal(err)
	}
	if createdWorkflow.PermissionLevel != permission.Write {
		t.Fatalf("expected created workflow write permission, got %d", createdWorkflow.PermissionLevel)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"restricted-form",
		"script":"function render(api, root) { root.append(api.input({ field: 'email' }), api.submit('Save')); return { table: 'contacts' }; }"
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
	var readableWorkflow workflowDefinitionResponse
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
		"script":"function render(api, root) { root.append(api.input({ field: 'name' }), api.submit('Save')); return { table: 'contacts' }; }"
	}`))
	updateForm.AddCookie(testSessionCookie(t, system, "other"))
	updateFormRecorder := httptest.NewRecorder()
	server.ServeHTTP(updateFormRecorder, updateForm)
	if updateFormRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected read-only form update 403, got %d: %s", updateFormRecorder.Code, updateFormRecorder.Body.String())
	}
}

func TestPublishedFormRequiresExplicitFormPermission(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestGrants(t, system, permission.Grant{SubjectID: "owner", Scope: permission.ScopeFormSet, Resource: "db", Level: permission.Write})

	formRequest := httptest.NewRequest(http.MethodPost, "/api/databases/db/forms", bytes.NewBufferString(`{
		"name":"published-intake",
		"script":"function render(api, root) { root.append(api.input({ field: 'name' }), api.input({ field: 'email' }), api.submit('Submit')); return { table: 'contacts' }; }"
	}`))
	formRequest.AddCookie(testSessionCookie(t, system, "owner"))
	formRecorder := httptest.NewRecorder()
	server.ServeHTTP(formRecorder, formRequest)
	if formRecorder.Code != http.StatusCreated {
		t.Fatalf("expected form 201, got %d: %s", formRecorder.Code, formRecorder.Body.String())
	}

	publishRequest := httptest.NewRequest(http.MethodPost, "/api/forms/1/publish", nil)
	publishRequest.AddCookie(testSessionCookie(t, system, "owner"))
	publishRecorder := httptest.NewRecorder()
	server.ServeHTTP(publishRecorder, publishRequest)
	if publishRecorder.Code != http.StatusOK {
		t.Fatalf("expected publish 200, got %d: %s", publishRecorder.Code, publishRecorder.Body.String())
	}
	var published systemdb.FormDefinition
	if err := json.NewDecoder(publishRecorder.Body).Decode(&published); err != nil {
		t.Fatal(err)
	}
	if published.PublishedToken == "" {
		t.Fatalf("expected published token, got %#v", published)
	}

	anonymousRead := httptest.NewRequest(http.MethodGet, "/api/published/forms/"+published.PublishedToken, nil)
	anonymousRecorder := httptest.NewRecorder()
	server.ServeHTTP(anonymousRecorder, anonymousRead)
	if anonymousRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous published form 401, got %d: %s", anonymousRecorder.Code, anonymousRecorder.Body.String())
	}

	saveTestDatabaseOwners(t, system, "db", "db-owner")
	dbOwnerRead := httptest.NewRequest(http.MethodGet, "/api/published/forms/"+published.PublishedToken, nil)
	dbOwnerRead.AddCookie(testSessionCookie(t, system, "db-owner"))
	dbOwnerRecorder := httptest.NewRecorder()
	server.ServeHTTP(dbOwnerRecorder, dbOwnerRead)
	if dbOwnerRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected db owner without form grant 403, got %d: %s", dbOwnerRecorder.Code, dbOwnerRecorder.Body.String())
	}

	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "reader",
		Scope:     permission.ScopeForm,
		Resource:  "1",
		Level:     permission.Read,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestGrants(t, system, permission.Grant{SubjectID: "reader", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write})
	saveTestRecordCreateGrant(t, system, "reader", "db.contacts")
	readerRead := httptest.NewRequest(http.MethodGet, "/api/published/forms/"+published.PublishedToken, nil)
	readerRead.AddCookie(testSessionCookie(t, system, "reader"))
	readerRecorder := httptest.NewRecorder()
	server.ServeHTTP(readerRecorder, readerRead)
	if readerRecorder.Code != http.StatusOK {
		t.Fatalf("expected reader published form 200, got %d: %s", readerRecorder.Code, readerRecorder.Body.String())
	}
	var readableForm systemdb.FormDefinition
	if err := json.NewDecoder(readerRecorder.Body).Decode(&readableForm); err != nil {
		t.Fatal(err)
	}
	if readableForm.PermissionLevel != permission.Read {
		t.Fatalf("expected read permission on published form, got %#v", readableForm)
	}

	submitRequest := httptest.NewRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Published Reader","email":"reader@example.com"}
	}`))
	submitRequest.AddCookie(testSessionCookie(t, system, "reader"))
	submitRecorder := httptest.NewRecorder()
	server.ServeHTTP(submitRecorder, submitRequest)
	if submitRecorder.Code != http.StatusCreated {
		t.Fatalf("expected reader published form submit 201, got %d: %s", submitRecorder.Code, submitRecorder.Body.String())
	}
	var row rowResponse
	if err := json.NewDecoder(submitRecorder.Body).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row.RecordID == 0 || row.Values["name"] != "Published Reader" {
		t.Fatalf("unexpected published submit row: %#v", row)
	}
}

func newTestServer(t *testing.T) (*Server, *systemdb.DB) {
	t.Helper()
	return newTestServerWithAuth(t, config.AuthConfig{
		Password: config.PasswordAuthConfig{Enabled: true},
	})
}

func newTestServerWithOIDC(t *testing.T, providers []config.OIDCProvider) (*Server, *systemdb.DB) {
	t.Helper()
	return newTestServerWithAuth(t, config.AuthConfig{
		Password: config.PasswordAuthConfig{Enabled: true},
		OIDC: config.OIDCConfig{
			Enabled:   true,
			Providers: providers,
		},
	})
}

func newTestServerWithAuth(t *testing.T, authConfig config.AuthConfig) (*Server, *systemdb.DB) {
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
	catalog := testCatalog(filepath.Join(t.TempDir(), "db.sqlite"))
	repository, err := recorddb.OpenCatalog(context.Background(), catalog, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	tableService := table.NewServiceWithRepository(historyStore, repository)
	tableService.SetFileBinder(system)
	server := NewServerWithAuthConfig(catalog, system, tableService, historyStore, authConfig)
	if authConfig.OIDC.Enabled {
		server.SetPublicURL("https://configured.example")
	}
	return server, system
}

func assertWorkflowRunCount(t *testing.T, server *Server, system *systemdb.DB, workflowID int64, actorID string, expected int) []workflowRunResponse {
	t.Helper()
	runs := fetchWorkflowRuns(t, server, system, workflowID, actorID)
	if len(runs) != expected {
		t.Fatalf("expected %d workflow runs, got %#v", expected, runs)
	}
	return runs
}

func waitWorkflowRunCount(t *testing.T, server *Server, system *systemdb.DB, workflowID int64, actorID string, expected int) []workflowRunResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var runs []workflowRunResponse
	for time.Now().Before(deadline) {
		runs = fetchWorkflowRuns(t, server, system, workflowID, actorID)
		if len(runs) == expected {
			return runs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d workflow runs, got %#v", expected, runs)
	return nil
}

func fetchWorkflowRuns(t *testing.T, server *Server, system *systemdb.DB, workflowID int64, actorID string) []workflowRunResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/workflows/"+strconv.FormatInt(workflowID, 10)+"/runs", nil)
	request.AddCookie(testSessionCookie(t, system, actorID))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected workflow runs 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var runs []workflowRunResponse
	if err := json.NewDecoder(recorder.Body).Decode(&runs); err != nil {
		t.Fatal(err)
	}
	return runs
}

func fetchWorkflowRun(t *testing.T, server *Server, system *systemdb.DB, workflowID int64, historyKey string, actorID string) workflowRunResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/workflows/"+strconv.FormatInt(workflowID, 10)+"/runs/"+url.PathEscape(historyKey), nil)
	request.AddCookie(testSessionCookie(t, system, actorID))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected workflow run detail 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var run workflowRunResponse
	if err := json.NewDecoder(recorder.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	return run
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
	repository, err := recorddb.OpenCatalog(context.Background(), catalog, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	server := NewServer(catalog, system, table.NewServiceWithRepository(historyStore, repository), historyStore)
	server.EnableMetadataWrites(metadataPath)
	return server, system, metadataPath
}

func testCatalog(sqlitePath string) metadata.Catalog {
	return metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "email", Type: "string"},
				{Name: "status", Type: "string"},
			},
			Views: []metadata.View{
				{
					Name: "active",
					Query: &metadata.ViewQuery{
						Combinator: "and",
						Rules:      []metadata.ViewQueryRule{{Field: "status", Operator: "=", Value: "active"}},
					},
				},
				{
					Name:     "active-a",
					BaseView: "active",
					Query: &metadata.ViewQuery{
						Combinator: "and",
						Rules:      []metadata.ViewQueryRule{{Field: "name", Operator: "contains", Value: "a"}},
					},
					Sorts: []metadata.ViewSort{{Field: "name", Direction: "desc"}},
				},
			},
		}},
	}}}
}

func testSessionCookie(t *testing.T, system *systemdb.DB, userID string) *http.Cookie {
	t.Helper()
	user, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:       userID + "@example.com",
		DisplayName: userID,
		Password:    "correct horse",
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
				"name":           "Person Example",
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
		"name":           "Person Example",
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

func TestCleanupWorkflowHistoryHonorsRetention(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)

	script := `function instances(info) { return { main: "echo" }; } function run(info) { return {}; }`
	forever, err := system.SaveWorkflow(ctx, systemdb.WorkflowDefinition{DatabaseName: "db", Name: "forever", Script: script})
	if err != nil {
		t.Fatal(err)
	}
	sevenDays := int64(7)
	limited, err := system.SaveWorkflow(ctx, systemdb.WorkflowDefinition{DatabaseName: "db", Name: "limited", Script: script, HistoryRetentionDays: &sevenDays})
	if err != nil {
		t.Fatal(err)
	}
	noHistory := int64(0)
	none, err := system.SaveWorkflow(ctx, systemdb.WorkflowDefinition{DatabaseName: "db", Name: "none", Script: script, HistoryRetentionDays: &noHistory})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().UnixMilli()
	oldTimestamp := now - 30*24*time.Hour.Milliseconds()
	for _, workflowDefinition := range []systemdb.WorkflowDefinition{forever, limited, none} {
		for _, timestamp := range []int64{oldTimestamp, now} {
			if _, err := history.SaveWorkflowRun(ctx, server.history, history.WorkflowRun{WorkflowID: workflowDefinition.ID, Timestamp: timestamp}); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := server.CleanupWorkflowHistory(ctx); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		workflowID int64
		expected   int
	}{
		{forever.ID, 2},
		{limited.ID, 1},
		{none.ID, 0},
	}
	for _, testCase := range cases {
		entries, err := server.history.GetPrefix(ctx, history.WorkflowPrefix(testCase.workflowID))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != testCase.expected {
			t.Fatalf("workflow %d: expected %d runs after cleanup, got %d", testCase.workflowID, testCase.expected, len(entries))
		}
	}
}

func TestSaveWorkflowRejectsNegativeRetention(t *testing.T) {
	server, system := newTestServer(t)
	cookie := testSessionCookie(t, system, "retention-user")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "retention-user", Scope: permission.ScopeWorkflowSet, Resource: "db", Level: permission.Write},
	)

	negative := int64(-1)
	body, err := json.Marshal(systemdb.WorkflowDefinition{
		DatabaseName:         "db",
		Name:                 "bad-retention",
		Script:               "function instances(info) { return { main: \"echo\" }; } function run(info) { return {}; }",
		HistoryRetentionDays: &negative,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/workflows", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative retention, got %d: %s", response.Code, response.Body.String())
	}
}

type memoryFileStore struct {
	objects map[string][]byte
}

func newMemoryFileStore() *memoryFileStore {
	return &memoryFileStore{objects: map[string][]byte{}}
}

func (store *memoryFileStore) Put(_ context.Context, id int64, name string, _ string, _ int64, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	store.objects[fmt.Sprintf("%d/%s", id, name)] = data
	return nil
}

func (store *memoryFileStore) Get(_ context.Context, id int64, name string) (io.ReadCloser, error) {
	data, ok := store.objects[fmt.Sprintf("%d/%s", id, name)]
	if !ok {
		return nil, fmt.Errorf("object %d/%s not found", id, name)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func uploadTestFileRequest(t *testing.T, server *Server, cookie *http.Cookie, fileName string, content string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/files", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func uploadTestFile(t *testing.T, server *Server, cookie *http.Cookie, fileName string, content string) systemdb.FileRecord {
	t.Helper()
	recorder := uploadTestFileRequest(t, server, cookie, fileName, content, map[string]string{
		"database_name": "db",
		"table_name":    "contacts",
		"record_id":     "1",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected upload 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var record systemdb.FileRecord
	if err := json.NewDecoder(recorder.Body).Decode(&record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestFileUploadDownloadAndMetadata(t *testing.T) {
	server, system := newTestServer(t)
	files := newMemoryFileStore()
	server.SetFileStore(files)
	cookie := testSessionCookie(t, system, "uploader")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "uploader", Scope: permission.ScopeFile, Resource: "db.contacts", Level: permission.Read},
	)

	record := uploadTestFile(t, server, cookie, "../weird/../报价单.pdf", "pdf-bytes")
	if record.ID != 1 || record.Name != "报价单.pdf" || record.Size != int64(len("pdf-bytes")) {
		t.Fatalf("unexpected file record: %#v", record)
	}
	if record.DatabaseName != "db" || record.TableName != "contacts" || record.RecordID != 1 {
		t.Fatalf("unexpected file binding: %#v", record)
	}
	second := uploadTestFile(t, server, cookie, "notes.txt", "hello")
	if second.ID != 2 {
		t.Fatalf("expected increasing file ids, got %#v", second)
	}

	download := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%d", record.ID), nil)
	download.AddCookie(cookie)
	downloadRecorder := httptest.NewRecorder()
	server.ServeHTTP(downloadRecorder, download)
	if downloadRecorder.Code != http.StatusOK || downloadRecorder.Body.String() != "pdf-bytes" {
		t.Fatalf("unexpected download response %d: %s", downloadRecorder.Code, downloadRecorder.Body.String())
	}
	if disposition := downloadRecorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "attachment") {
		t.Fatalf("unexpected content disposition %q", disposition)
	}

	metadata := httptest.NewRequest(http.MethodPost, "/api/files/metadata", strings.NewReader(`{"ids":[1,2,999]}`))
	metadata.Header.Set("Content-Type", "application/json")
	metadata.AddCookie(cookie)
	metadataRecorder := httptest.NewRecorder()
	server.ServeHTTP(metadataRecorder, metadata)
	if metadataRecorder.Code != http.StatusOK {
		t.Fatalf("expected metadata 200, got %d: %s", metadataRecorder.Code, metadataRecorder.Body.String())
	}
	var records []systemdb.FileRecord
	if err := json.NewDecoder(metadataRecorder.Body).Decode(&records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Name != "报价单.pdf" || records[1].Name != "notes.txt" {
		t.Fatalf("unexpected metadata records: %#v", records)
	}
}

func TestFileEndpointsWithoutStoreOrSession(t *testing.T) {
	server, system := newTestServer(t)
	cookie := testSessionCookie(t, system, "uploader")

	request := httptest.NewRequest(http.MethodPost, "/api/files", strings.NewReader("ignored"))
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without a file store, got %d", recorder.Code)
	}

	anonymous := httptest.NewRequest(http.MethodGet, "/api/files/1", nil)
	anonymousRecorder := httptest.NewRecorder()
	server.ServeHTTP(anonymousRecorder, anonymous)
	if anonymousRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", anonymousRecorder.Code)
	}
}

func TestFilePermissionsGateUploadAndDownload(t *testing.T) {
	server, system := newTestServer(t)
	server.SetFileStore(newMemoryFileStore())
	ctx := context.Background()

	ownerCookie := testSessionCookie(t, system, "owner")
	if err := system.SaveDatabaseOwner(ctx, "db", "owner"); err != nil {
		t.Fatal(err)
	}
	grantedCookie := testSessionCookie(t, system, "granted")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "granted", Scope: permission.ScopeFile, Resource: "db.contacts", Level: permission.Read},
	)
	strangerCookie := testSessionCookie(t, system, "stranger")

	missingBinding := uploadTestFileRequest(t, server, ownerCookie, "a.txt", "x", map[string]string{})
	if missingBinding.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without table binding, got %d", missingBinding.Code)
	}
	unknownTable := uploadTestFileRequest(t, server, ownerCookie, "a.txt", "x", map[string]string{
		"database_name": "db", "table_name": "missing",
	})
	if unknownTable.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown table, got %d", unknownTable.Code)
	}
	denied := uploadTestFileRequest(t, server, strangerCookie, "a.txt", "x", map[string]string{
		"database_name": "db", "table_name": "contacts",
	})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for uploader without a file grant, got %d", denied.Code)
	}

	record := uploadTestFile(t, server, ownerCookie, "report.pdf", "pdf")

	cases := []struct {
		name     string
		cookie   *http.Cookie
		expected int
	}{
		{"owner", ownerCookie, http.StatusOK},
		{"granted", grantedCookie, http.StatusOK},
		{"stranger", strangerCookie, http.StatusForbidden},
	}
	for _, testCase := range cases {
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%d", record.ID), nil)
		request.AddCookie(testCase.cookie)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != testCase.expected {
			t.Fatalf("%s: expected download %d, got %d: %s", testCase.name, testCase.expected, recorder.Code, recorder.Body.String())
		}
	}

	metadata := httptest.NewRequest(http.MethodPost, "/api/files/metadata", strings.NewReader(fmt.Sprintf(`{"ids":[%d]}`, record.ID)))
	metadata.Header.Set("Content-Type", "application/json")
	metadata.AddCookie(strangerCookie)
	metadataRecorder := httptest.NewRecorder()
	server.ServeHTTP(metadataRecorder, metadata)
	if metadataRecorder.Code != http.StatusOK {
		t.Fatalf("expected metadata 200, got %d", metadataRecorder.Code)
	}
	var records []systemdb.FileRecord
	if err := json.NewDecoder(metadataRecorder.Body).Decode(&records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected metadata to hide unauthorized files, got %#v", records)
	}
}

func TestUnboundFilesAreNotDownloadable(t *testing.T) {
	server, system := newTestServer(t)
	server.SetFileStore(newMemoryFileStore())
	ownerCookie := testSessionCookie(t, system, "owner2")
	if err := system.SaveDatabaseOwner(context.Background(), "db", "owner2"); err != nil {
		t.Fatal(err)
	}

	recorder := uploadTestFileRequest(t, server, ownerCookie, "pending.txt", "x", map[string]string{
		"database_name": "db", "table_name": "contacts", "record_id": "0",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected upload 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var record systemdb.FileRecord
	if err := json.NewDecoder(recorder.Body).Decode(&record); err != nil {
		t.Fatal(err)
	}

	download := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%d", record.ID), nil)
	download.AddCookie(ownerCookie)
	downloadRecorder := httptest.NewRecorder()
	server.ServeHTTP(downloadRecorder, download)
	if downloadRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an unbound file even for the owner, got %d", downloadRecorder.Code)
	}
}

func TestUploadRejectsFilesOverTheLimit(t *testing.T) {
	server, system := newTestServer(t)
	server.SetFileStore(newMemoryFileStore())
	server.SetFileUploadLimit(8)
	cookie := testSessionCookie(t, system, "owner3")
	if err := system.SaveDatabaseOwner(context.Background(), "db", "owner3"); err != nil {
		t.Fatal(err)
	}

	recorder := uploadTestFileRequest(t, server, cookie, "big.bin", "123456789", map[string]string{
		"database_name": "db", "table_name": "contacts",
	})
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 over the upload limit, got %d: %s", recorder.Code, recorder.Body.String())
	}
	small := uploadTestFileRequest(t, server, cookie, "small.bin", "1234", map[string]string{
		"database_name": "db", "table_name": "contacts",
	})
	if small.Code != http.StatusCreated {
		t.Fatalf("expected 201 under the limit, got %d: %s", small.Code, small.Body.String())
	}
}

func TestSaveFileGrant(t *testing.T) {
	server, system := newTestServer(t)
	cookie := testSessionCookie(t, system, "grant-owner")
	if err := system.SaveDatabaseOwner(context.Background(), "db", "grant-owner"); err != nil {
		t.Fatal(err)
	}

	save := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/permissions/grants", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		return recorder
	}

	granted := save(`{"subject_id":"viewer","scope":"file","resource":"db.contacts","field":"","level":1}`)
	if granted.Code != http.StatusCreated {
		t.Fatalf("expected file grant 201, got %d: %s", granted.Code, granted.Body.String())
	}
	badTable := save(`{"subject_id":"viewer","scope":"file","resource":"db.missing","field":"","level":1}`)
	if badTable.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown table, got %d: %s", badTable.Code, badTable.Body.String())
	}
	withField := save(`{"subject_id":"viewer","scope":"file","resource":"db.contacts","field":"x","level":1}`)
	if withField.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for file grant with field, got %d: %s", withField.Code, withField.Body.String())
	}
}
