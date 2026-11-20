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
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"autable/internal/auth"
	"autable/internal/config"
	"autable/internal/history"
	"autable/internal/metadata"
	"autable/internal/permission"
	"autable/internal/recorddb"
	"autable/internal/systemdb"
	"autable/internal/table"
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

func saveTestAllViewGrant(t *testing.T, system *systemdb.DB, subjectID, resource string) {
	t.Helper()
	saveTestGrants(t, system, permission.Grant{
		SubjectID: subjectID,
		Scope:     permission.ScopeView,
		Resource:  resource,
		Field:     "all",
		Level:     permission.Read,
	})
}

// makeTestAdmins lists the users created by testSessionCookie (email
// <id>@example.com) as admins of the test server.
func makeTestAdmins(server *Server, userIDs ...string) {
	for _, userID := range userIDs {
		server.auth.Admins = append(server.auth.Admins, userID+"@example.com")
	}
}

// newTestRequest is httptest.NewRequest with the JSON content type browsers
// and the CLI send with a request body.
func newTestRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func TestPasswordAuthSessionLifecycle(t *testing.T) {
	server, _ := newTestServer(t)

	register := newTestRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{
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

	me := newTestRequest(http.MethodGet, "/api/auth/me", nil)
	me.AddCookie(cookie)
	meRecorder := httptest.NewRecorder()
	server.ServeHTTP(meRecorder, me)
	if meRecorder.Code != http.StatusOK {
		t.Fatalf("expected me 200, got %d: %s", meRecorder.Code, meRecorder.Body.String())
	}

	logout := newTestRequest(http.MethodPost, "/api/auth/logout", nil)
	logout.AddCookie(cookie)
	logoutRecorder := httptest.NewRecorder()
	server.ServeHTTP(logoutRecorder, logout)
	if logoutRecorder.Code != http.StatusOK {
		t.Fatalf("expected logout 200, got %d: %s", logoutRecorder.Code, logoutRecorder.Body.String())
	}

	afterLogout := newTestRequest(http.MethodGet, "/api/auth/me", nil)
	afterLogout.AddCookie(cookie)
	afterLogoutRecorder := httptest.NewRecorder()
	server.ServeHTTP(afterLogoutRecorder, afterLogout)
	if afterLogoutRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected me 401 after logout, got %d: %s", afterLogoutRecorder.Code, afterLogoutRecorder.Body.String())
	}
}

func TestLoginRejectsInvalidPassword(t *testing.T) {
	server, _ := newTestServer(t)
	register := newTestRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{
		"email":"person@example.com",
		"display_name":"Person Example",
		"password":"correct horse"
	}`))
	server.ServeHTTP(httptest.NewRecorder(), register)

	login := newTestRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{
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
		request := newTestRequest(http.MethodPost, path, bytes.NewBufferString(`{
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

	anonymous := newTestRequest(http.MethodGet, "/api/users?query=ada", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, anonymous)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous user search 401, got %d: %s", recorder.Code, recorder.Body.String())
	}

	request := newTestRequest(http.MethodGet, "/api/users?query=ADA", nil)
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
	request = newTestRequest(http.MethodGet, "/api/users?query=own", nil)
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

	request := newTestRequest(http.MethodGet, "/api/auth/config", nil)
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

	request := newTestRequest(http.MethodGet, "/api/auth/config", nil)
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

	start := newTestRequest(http.MethodGet, "/api/auth/oidc/main/start", nil)
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

	request := newTestRequest(http.MethodGet, "/api/auth/oidc/main/start", nil)
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

	request := newTestRequest(http.MethodGet, "/api/auth/oidc/main/callback?code=ok&state=state-1", nil)
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
	// The server's public URL is https, so the cookie is host-locked.
	if cookie.Name != secureSessionCookieName || !cookie.Secure || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("expected a Secure __Host- session cookie, got %#v", cookie)
	}

	me := newTestRequest(http.MethodGet, "/api/auth/me", nil)
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
	register := newTestRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{
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
		Level:     permission.FieldAll,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, user.ID, "db.contacts")

	request := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
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
		Level:     permission.FieldAll,
	}
	if err := system.SaveGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")
	makeTestAdmins(server, "u1")

	body := bytes.NewBufferString(`{"values":{"name":"Ada","email":"ada@example.com"}}`)
	request := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", body)
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

	historyRequest := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
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
		Level:     permission.FieldAll,
	}); err != nil {
		t.Fatal(err)
	}
	makeTestAdmins(server, "owner")

	request := newTestRequest(http.MethodPost, "/api/tables/workspace/contacts/fields", bytes.NewBufferString(`{
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
		Level:     permission.FieldAll,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "owner", "db.contacts")
	saveTestAllViewGrant(t, system, "owner", "db.contacts")

	create := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows/upsert", bytes.NewBufferString(`{
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

	update := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows/upsert", bytes.NewBufferString(`{
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

	noop := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows/upsert", bytes.NewBufferString(`{
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
		{SubjectID: "writer", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll},
		{SubjectID: "writer", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
	}
	for _, grant := range writerGrants {
		if err := system.SaveGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	create := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
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
		{SubjectID: "reader", Scope: permission.ScopeView, Resource: "db.contacts", Field: "all", Level: permission.Read},
	}
	for _, grant := range readerGrants {
		if err := system.SaveGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}

	allowed := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows/query", bytes.NewBufferString(`{
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

	denied := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows/query", bytes.NewBufferString(`{
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
	makeTestAdmins(server, "u1")
	createRequest := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Ada","email":"ada@example.com"}
	}`))
	createRequest.AddCookie(testSessionCookie(t, system, "u1"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	updateRequest := newTestRequest(http.MethodPatch, "/api/tables/db/contacts/rows/1", bytes.NewBufferString(`{
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

	historyRequest := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
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
	makeTestAdmins(server, "u1")
	createRequest := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Ada","email":"ada@example.com"}
	}`))
	createRequest.AddCookie(testSessionCookie(t, system, "u1"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	deleteRequest := newTestRequest(http.MethodDelete, "/api/tables/db/contacts/rows/1", nil)
	deleteRequest.AddCookie(testSessionCookie(t, system, "u1"))
	deleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	rowsRequest := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows", nil)
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

	historyRequest := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
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

func TestRowHistoryAPIRequiresAuthAndRowSet(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "writer",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.FieldAll,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "writer", "db.contacts")
	createRequest := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Ada","email":"ada@example.com","status":"active"}
	}`))
	createRequest.AddCookie(testSessionCookie(t, system, "writer"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	anonymousHistory := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	anonymousRecorder := httptest.NewRecorder()
	server.ServeHTTP(anonymousRecorder, anonymousHistory)
	if anonymousRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous history 401, got %d: %s", anonymousRecorder.Code, anonymousRecorder.Body.String())
	}

	makeTestAdmins(server, "owner")
	ownerHistory := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
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
	readerHistory := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	readerHistory.AddCookie(testSessionCookie(t, system, "reader"))
	readerRecorder := httptest.NewRecorder()
	server.ServeHTTP(readerRecorder, readerHistory)
	if readerRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected reader history 404, got %d: %s", readerRecorder.Code, readerRecorder.Body.String())
	}

	deniedHistory := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows/1/history", nil)
	deniedHistory.AddCookie(testSessionCookie(t, system, "denied"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, deniedHistory)
	if deniedRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected denied history 404, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
}

func TestPermissionGrantAPIRequiresAuthentication(t *testing.T) {
	server, _ := newTestServer(t)
	request := newTestRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"field_set",
		"resource":"db.contacts",
		"level":7
	}`))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated grant save 401, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestPermissionGrantAPIIsAdminOnly(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	denied := newTestRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"field",
		"resource":"db.contacts",
		"field":"email",
		"level":7
	}`))
	denied.AddCookie(testSessionCookie(t, system, "viewer"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, denied)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin grant save 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}

	makeTestAdmins(server, "admin")
	allowed := newTestRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"field",
		"resource":"db.contacts",
		"field":"email",
		"level":7
	}`))
	allowed.AddCookie(testSessionCookie(t, system, "admin"))
	allowedRecorder := httptest.NewRecorder()
	server.ServeHTTP(allowedRecorder, allowed)
	if allowedRecorder.Code != http.StatusCreated {
		t.Fatalf("expected admin grant save 201, got %d: %s", allowedRecorder.Code, allowedRecorder.Body.String())
	}

	perms, err := system.GrantsForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanUpdateField("u1", "db.contacts", "email") {
		t.Fatal("expected API grant to persist field write permission")
	}

	grantSet := newTestRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
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

	grantField := newTestRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"u1",
		"scope":"field",
		"resource":"db.contacts",
		"field":"name",
		"level":7
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

	request := newTestRequest(http.MethodGet, "/api/metadata", nil)
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
	if visible.Databases[0].Tables[0].PermissionLevel != int(permission.Read) {
		t.Fatalf("expected field-only reader to see the table read-only, got %#v", visible.Databases[0].Tables[0])
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

	anonymous := newTestRequest(http.MethodGet, "/api/metadata", nil)
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

func TestCreateDatabaseAPIIsAdminOnly(t *testing.T) {
	server, system, metadataPath := newTestServerWithMetadataFile(t, metadata.Catalog{})
	makeTestAdmins(server, "admin")

	denied := newTestRequest(http.MethodPost, "/api/databases", bytes.NewBufferString(`{"name":"sales"}`))
	denied.AddCookie(testSessionCookie(t, system, "member"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, denied)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin database create 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
	if _, ok := server.catalogSnapshot().Database("sales"); ok {
		t.Fatal("expected denied database create to change nothing")
	}

	request := newTestRequest(http.MethodPost, "/api/databases", bytes.NewBufferString(`{
		"name":"sales"
	}`))
	request.AddCookie(testSessionCookie(t, system, "admin"))
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
	// Creating a database confers nothing: the creator is an admin anyway,
	// and anyone else sees it only through grants.
	memberCatalog := newTestRequest(http.MethodGet, "/api/metadata", nil)
	memberCatalog.AddCookie(testSessionCookie(t, system, "member"))
	memberRecorder := httptest.NewRecorder()
	server.ServeHTTP(memberRecorder, memberCatalog)
	if strings.Contains(memberRecorder.Body.String(), "sales") {
		t.Fatalf("expected the new database hidden from non-admins, got %s", memberRecorder.Body.String())
	}
}

func TestAdminCanCreateTable(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "owner")

	request := newTestRequest(http.MethodPost, "/api/databases/workspace/tables", bytes.NewBufferString(`{
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
}

func TestAdminCanCreateEmptyTable(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "owner")

	request := newTestRequest(http.MethodPost, "/api/databases/workspace/tables", bytes.NewBufferString(`{
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

func TestAdminCanUpdateFieldsAndViews(t *testing.T) {
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
	makeTestAdmins(server, "owner")
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeFieldSet,
		Resource:  "workspace.contacts",
		Level:     permission.FieldAll,
	}); err != nil {
		t.Fatal(err)
	}
	makeTestAdmins(server, "owner")
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "owner",
		Scope:     permission.ScopeViewSet,
		Resource:  "workspace.contacts",
		Level:     permission.Write,
	}); err != nil {
		t.Fatal(err)
	}

	request := newTestRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
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
				{Name: "everyone"},
				{Name: "hidden-view", Query: &metadata.ViewQuery{Combinator: "and", Rules: []metadata.ViewQueryRule{{Field: "hidden_notes", Operator: "notNull"}}}},
			},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "editor")

	request := newTestRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[
			{"name":"name","type":"string"},
			{"name":"phone","type":"string"}
		],
		"views":[
			{"name":"everyone","sorts":[{"field":"name","direction":"asc"}]}
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
	saveTestGrants(t, system, permission.Grant{SubjectID: "editor", Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.FieldAll})

	request := newTestRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
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

func TestViewMetadataChangesAreAdminOnly(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}, {Name: "status", Type: "string"}},
			Views:  []metadata.View{{Name: "active"}},
		}},
	}}}
	server, system, metadataPath := newTestServerWithMetadataFile(t, catalog)
	// View definitions are the row-level permission boundary: even a view
	// write grant no longer allows changing them.
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "editor", Scope: permission.ScopeView, Resource: "workspace.contacts", Field: "active", Level: permission.Write},
		permission.Grant{SubjectID: "editor", Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.FieldAll},
	)

	updateExisting := newTestRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"views":[{"name":"active","query":{"combinator":"and","rules":[{"field":"status","operator":"=","value":"active"}]}}]
	}`))
	updateExisting.AddCookie(testSessionCookie(t, system, "editor"))
	updateRecorder := httptest.NewRecorder()
	server.ServeHTTP(updateRecorder, updateExisting)
	if updateRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin view update 403, got %d: %s", updateRecorder.Code, updateRecorder.Body.String())
	}

	// Table metadata is structure: even echoing the views back unchanged
	// is refused to non-admins.
	unchanged := newTestRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[{"name":"phone","type":"string"}],
		"views":[{"name":"active","display_name":"","sorts":[]}]
	}`))
	unchanged.AddCookie(testSessionCookie(t, system, "editor"))
	unchangedRecorder := httptest.NewRecorder()
	server.ServeHTTP(unchangedRecorder, unchanged)
	if unchangedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin metadata put 403, got %d: %s", unchangedRecorder.Code, unchangedRecorder.Body.String())
	}

	makeTestAdmins(server, "boss")
	ownerUpdate := newTestRequest(http.MethodPut, "/api/databases/workspace/tables/contacts", bytes.NewBufferString(`{
		"name":"contacts",
		"views":[{"name":"active","query":{"combinator":"and","rules":[{"field":"status","operator":"=","value":"active"}]}}]
	}`))
	ownerUpdate.AddCookie(testSessionCookie(t, system, "boss"))
	ownerRecorder := httptest.NewRecorder()
	server.ServeHTTP(ownerRecorder, ownerUpdate)
	if ownerRecorder.Code != http.StatusOK {
		t.Fatalf("expected admin view update 200, got %d: %s", ownerRecorder.Code, ownerRecorder.Body.String())
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
}

func TestAdminCanMoveFieldPosition(t *testing.T) {
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
		Level:     permission.FieldAll,
	}); err != nil {
		t.Fatal(err)
	}
	makeTestAdmins(server, "owner")

	request := newTestRequest(http.MethodPatch, "/api/databases/workspace/tables/contacts/fields/status/position", bytes.NewBufferString(`{"before":"name"}`))
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

func TestAdminCanManageRoles(t *testing.T) {
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
	makeTestAdmins(server, "owner")

	createRole := newTestRequest(http.MethodPost, "/api/databases/workspace/roles", bytes.NewBufferString(`{
		"name":"editor"
	}`))
	createRole.AddCookie(testSessionCookie(t, system, "owner"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, createRole)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected role create 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	updateGrants := newTestRequest(http.MethodPut, "/api/databases/workspace/roles/editor/grants", bytes.NewBufferString(`{
		"grants":[
			{"scope":"field_set","resource":"workspace.contacts","level":7},
			{"scope":"app_set","resource":"workspace","level":1},
			{"scope":"app","resource":"3","level":0}
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
	workflow, err := system.CreateWorkflow(ctx, systemdb.Workflow{DatabaseName: "workspace", Name: "sync-contacts"})
	if err != nil {
		t.Fatal(err)
	}
	updateMembers := newTestRequest(http.MethodPut, "/api/databases/workspace/roles/editor/members", bytes.NewBufferString(`{
		"members":[{"type":"user","id":"u1"},{"type":"user","id":"u2"},{"type":"user","id":"u1"},{"type":"workflow","id":"`+strconv.FormatInt(workflow.ID, 10)+`"}]
	}`))
	updateMembers.AddCookie(testSessionCookie(t, system, "owner"))
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, updateMembers)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected role members update 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	rolesRequest := newTestRequest(http.MethodGet, "/api/databases/workspace/roles", nil)
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
	workspaceWorkflow, err := system.CreateWorkflow(ctx, systemdb.Workflow{DatabaseName: "workspace", Name: "workspace-flow"})
	if err != nil {
		t.Fatal(err)
	}
	otherWorkflow, err := system.CreateWorkflow(ctx, systemdb.Workflow{DatabaseName: "other", Name: "other-flow"})
	if err != nil {
		t.Fatal(err)
	}
	workspaceApp, err := system.CreateApp(ctx, systemdb.AppDefinition{DatabaseName: "workspace", Name: "workspace-app"})
	if err != nil {
		t.Fatal(err)
	}
	otherApp, err := system.CreateApp(ctx, systemdb.AppDefinition{DatabaseName: "other", Name: "other-app"})
	if err != nil {
		t.Fatal(err)
	}

	valid := []permission.Grant{
		{Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.Read},
		{Scope: permission.ScopeRecord, Resource: "workspace.contacts", Field: "create", Level: permission.Write},
		{Scope: permission.ScopeWorkflow, Resource: resourceID(workspaceWorkflow.ID), Level: permission.Read},
		{Scope: permission.ScopeApp, Resource: resourceID(workspaceApp.ID), Level: permission.Read},
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
		{name: "other app", grant: permission.Grant{Scope: permission.ScopeApp, Resource: resourceID(otherApp.ID), Level: permission.Read}},
		{name: "other app set", grant: permission.Grant{Scope: permission.ScopeAppSet, Resource: "other", Level: permission.Read}},
		{name: "missing app", grant: permission.Grant{Scope: permission.ScopeApp, Resource: "999", Level: permission.Read}},
		{name: "workflow above run", grant: permission.Grant{Scope: permission.ScopeWorkflow, Resource: resourceID(workspaceWorkflow.ID), Level: permission.Write}},
		{name: "app above open", grant: permission.Grant{Scope: permission.ScopeApp, Resource: resourceID(workspaceApp.ID), Level: permission.Write}},
		{name: "app set above open", grant: permission.Grant{Scope: permission.ScopeAppSet, Resource: "workspace", Level: permission.Write}},
		{name: "removed workflow set scope", grant: permission.Grant{Scope: "workflow_set", Resource: "workspace", Level: permission.Read}},
		{name: "removed field add scope", grant: permission.Grant{Scope: "field_add", Resource: "workspace.contacts", Level: permission.Write}},
		{name: "removed field modify scope", grant: permission.Grant{Scope: "field_modify", Resource: "workspace.contacts", Field: "name", Level: permission.Write}},
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

	mixedAppGrants := []permission.Grant{
		{Scope: permission.ScopeAppSet, Resource: "workspace", Level: permission.Read},
		{Scope: permission.ScopeApp, Resource: resourceID(workspaceApp.ID), Level: permission.Read},
	}
	if err := server.validateRoleGrants(ctx, "workspace", mixedAppGrants); err == nil {
		t.Fatal("expected app set and app grants to be mutually exclusive")
	}
}

func TestRoleGrantAPIRejectsCrossDatabaseResources(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{Name: "workspace", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "string"}}}}},
		{Name: "other", Tables: []metadata.Table{{Name: "contacts", Fields: []metadata.Field{{Name: "name", Type: "string"}}}}},
	}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "owner")
	if _, err := system.SaveRole(ctx, systemdb.RoleDefinition{DatabaseName: "workspace", Name: "editor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleGrants(ctx, "workspace", "editor", []permission.Grant{
		{Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.Read},
	}); err != nil {
		t.Fatal(err)
	}

	request := newTestRequest(http.MethodPut, "/api/databases/workspace/roles/editor/grants", bytes.NewBufferString(`{
		"grants":[{"scope":"field_set","resource":"other.contacts","level":7}]
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
		{Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll},
		{Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleMembers(ctx, "db", "editor", []systemdb.RoleMember{{Type: "user", ID: "u1"}}); err != nil {
		t.Fatal(err)
	}

	request := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
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
		Level:     permission.FieldAll,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "owner", "db.contacts")
	saveTestAllViewGrant(t, system, "owner", "db.contacts")
	createRequest := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{
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
		{Scope: permission.ScopeField, Resource: "db.contacts", Field: "name", Level: permission.FieldAll},
		{Scope: permission.ScopeField, Resource: "db.contacts", Field: "email", Level: permission.Read},
		{Scope: permission.ScopeView, Resource: "db.contacts", Field: "all", Level: permission.Read},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := system.ReplaceRoleMembers(ctx, "db", "editor", []systemdb.RoleMember{{Type: "user", ID: "u1"}}); err != nil {
		t.Fatal(err)
	}

	allowedRequest := newTestRequest(http.MethodPatch, "/api/tables/db/contacts/rows/1", bytes.NewBufferString(`{
		"values":{"name":"Grace"}
	}`))
	allowedRequest.AddCookie(testSessionCookie(t, system, "u1"))
	allowedRecorder := httptest.NewRecorder()
	server.ServeHTTP(allowedRecorder, allowedRequest)
	if allowedRecorder.Code != http.StatusOK {
		t.Fatalf("expected role member name update 200, got %d: %s", allowedRecorder.Code, allowedRecorder.Body.String())
	}

	deniedRequest := newTestRequest(http.MethodPatch, "/api/tables/db/contacts/rows/1", bytes.NewBufferString(`{
		"values":{"email":"blocked@example.com"}
	}`))
	deniedRequest.AddCookie(testSessionCookie(t, system, "u1"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, deniedRequest)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected role member ungranted field write 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
}

func TestRoleManagementIsAdminOnly(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	if _, err := system.SaveRole(context.Background(), systemdb.RoleDefinition{DatabaseName: "workspace", Name: "readers"}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ method, target, body string }{
		{http.MethodPost, "/api/databases/workspace/roles", `{"name":"viewer"}`},
		{http.MethodGet, "/api/databases/workspace/roles", ""},
		{http.MethodPut, "/api/databases/workspace/roles/readers/grants", `{"grants":[]}`},
		{http.MethodPut, "/api/databases/workspace/roles/readers/members", `{"members":[{"type":"user","id":"viewer"}]}`},
	} {
		var body io.Reader
		if tc.body != "" {
			body = strings.NewReader(tc.body)
		}
		recorder := appTestRequest(t, server, system, "viewer", tc.method, tc.target, body)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("expected non-admin %s %s 403, got %d: %s", tc.method, tc.target, recorder.Code, recorder.Body.String())
		}
	}
	perms, err := system.EffectiveGrantsForSubject(context.Background(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if perms.CanReadResource("viewer", permission.ScopeAppSet, "workspace") {
		t.Fatal("expected no grants to come out of denied role changes")
	}
}

func TestNonAdminCannotCreateTable(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	request := newTestRequest(http.MethodPost, "/api/databases/workspace/tables", bytes.NewBufferString(`{
		"name":"contacts",
		"fields":[{"name":"name","type":"string"}]
	}`))
	request.AddCookie(testSessionCookie(t, system, "other"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin table create 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestListRowsAPIAppliesView(t *testing.T) {
	ctx := context.Background()
	server, system := newTestServer(t)
	if err := system.SaveGrant(ctx, permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.FieldAll,
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
		request := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(body))
		request.AddCookie(testSessionCookie(t, system, "u1"))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
		}
	}

	request := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows?view=active-a", nil)
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

	viewSortOverride := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows?view=active-a&sort_field=name&sort_direction=asc", nil)
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

	sortRequest := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows?sort_field=name&sort_direction=asc", nil)
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

	invalidSort := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows?sort_field=name&sort_direction=sideways", nil)
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
	deniedView := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows?view=active-a", nil)
	deniedView.AddCookie(testSessionCookie(t, system, "reader"))
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, deniedView)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected unreadable view 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}

	deniedSort := newTestRequest(http.MethodGet, "/api/tables/db/contacts/rows?sort_field=status&sort_direction=desc", nil)
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
	request := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", body)
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
		Level:     permission.FieldAll,
	}); err != nil {
		t.Fatal(err)
	}
	saveTestRecordCreateGrant(t, system, "u1", "db.contacts")

	request := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
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
		if cookie.Name == sessionCookieName || cookie.Name == secureSessionCookieName {
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
	request := newTestRequest(http.MethodPost, "/api/files", body)
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
	saveTestAllViewGrant(t, system, "uploader", "db.contacts")
	makeTestAdmins(server, "seeder")
	seedRow := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
	seedRow.AddCookie(testSessionCookie(t, system, "seeder"))
	seedRecorder := httptest.NewRecorder()
	server.ServeHTTP(seedRecorder, seedRow)
	if seedRecorder.Code != http.StatusCreated {
		t.Fatalf("expected seed row 201, got %d: %s", seedRecorder.Code, seedRecorder.Body.String())
	}

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

	download := newTestRequest(http.MethodGet, fmt.Sprintf("/api/files/%d", record.ID), nil)
	download.AddCookie(cookie)
	downloadRecorder := httptest.NewRecorder()
	server.ServeHTTP(downloadRecorder, download)
	if downloadRecorder.Code != http.StatusOK || downloadRecorder.Body.String() != "pdf-bytes" {
		t.Fatalf("unexpected download response %d: %s", downloadRecorder.Code, downloadRecorder.Body.String())
	}
	if disposition := downloadRecorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "attachment") {
		t.Fatalf("unexpected content disposition %q", disposition)
	}

	metadata := newTestRequest(http.MethodPost, "/api/files/metadata", strings.NewReader(`{"ids":[1,2,999]}`))
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

	request := newTestRequest(http.MethodPost, "/api/files", strings.NewReader("ignored"))
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without a file store, got %d", recorder.Code)
	}

	anonymous := newTestRequest(http.MethodGet, "/api/files/1", nil)
	anonymousRecorder := httptest.NewRecorder()
	server.ServeHTTP(anonymousRecorder, anonymous)
	if anonymousRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", anonymousRecorder.Code)
	}
}

func TestFilePermissionsGateUploadAndDownload(t *testing.T) {
	server, system := newTestServer(t)
	server.SetFileStore(newMemoryFileStore())

	ownerCookie := testSessionCookie(t, system, "owner")
	makeTestAdmins(server, "owner")
	grantedCookie := testSessionCookie(t, system, "granted")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "granted", Scope: permission.ScopeFile, Resource: "db.contacts", Level: permission.Read},
	)
	saveTestAllViewGrant(t, system, "granted", "db.contacts")
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

	seedRow := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(`{"values":{"name":"Ada"}}`))
	seedRow.AddCookie(ownerCookie)
	seedRecorder := httptest.NewRecorder()
	server.ServeHTTP(seedRecorder, seedRow)
	if seedRecorder.Code != http.StatusCreated {
		t.Fatalf("expected seed row 201, got %d: %s", seedRecorder.Code, seedRecorder.Body.String())
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
		request := newTestRequest(http.MethodGet, fmt.Sprintf("/api/files/%d", record.ID), nil)
		request.AddCookie(testCase.cookie)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != testCase.expected {
			t.Fatalf("%s: expected download %d, got %d: %s", testCase.name, testCase.expected, recorder.Code, recorder.Body.String())
		}
	}

	metadata := newTestRequest(http.MethodPost, "/api/files/metadata", strings.NewReader(fmt.Sprintf(`{"ids":[%d]}`, record.ID)))
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
	makeTestAdmins(server, "owner2")

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

	download := newTestRequest(http.MethodGet, fmt.Sprintf("/api/files/%d", record.ID), nil)
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
	makeTestAdmins(server, "owner3")

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
	makeTestAdmins(server, "grant-owner")

	save := func(body string) *httptest.ResponseRecorder {
		request := newTestRequest(http.MethodPost, "/api/permissions/grants", strings.NewReader(body))
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

func TestDownloadRedirectsBrowsersToLogin(t *testing.T) {
	server, _ := newTestServer(t)

	browser := newTestRequest(http.MethodGet, "/api/files/6", nil)
	browser.Header.Set("Accept", "text/html,application/xhtml+xml")
	browserRecorder := httptest.NewRecorder()
	server.ServeHTTP(browserRecorder, browser)
	if browserRecorder.Code != http.StatusFound {
		t.Fatalf("expected 302 for browser navigation, got %d", browserRecorder.Code)
	}
	if location := browserRecorder.Header().Get("Location"); location != "/login?redirect=%2Fapi%2Ffiles%2F6" {
		t.Fatalf("unexpected redirect target %q", location)
	}

	apiClient := newTestRequest(http.MethodGet, "/api/files/6", nil)
	apiRecorder := httptest.NewRecorder()
	server.ServeHTTP(apiRecorder, apiClient)
	if apiRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for api clients, got %d", apiRecorder.Code)
	}
}

func TestDownloadServesPreviewableTypesInline(t *testing.T) {
	server, system := newTestServer(t)
	server.SetFileStore(newMemoryFileStore())
	cookie := testSessionCookie(t, system, "preview-owner")
	makeTestAdmins(server, "preview-owner")

	upload := func(fileName string, contentType string) systemdb.FileRecord {
		t.Helper()
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, fileName))
		header.Set("Content-Type", contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("content")); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"database_name": "db", "table_name": "contacts", "record_id": "1"} {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		request := newTestRequest(http.MethodPost, "/api/files", body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected upload 201, got %d: %s", recorder.Code, recorder.Body.String())
		}
		var record systemdb.FileRecord
		if err := json.NewDecoder(recorder.Body).Decode(&record); err != nil {
			t.Fatal(err)
		}
		return record
	}

	cases := []struct {
		fileName    string
		contentType string
		disposition string
	}{
		{"contract.pdf", "application/pdf", "inline"},
		{"photo.png", "image/png", "inline"},
		{"vector.svg", "image/svg+xml", "attachment"},
		{"data.bin", "application/octet-stream", "attachment"},
	}
	for _, testCase := range cases {
		record := upload(testCase.fileName, testCase.contentType)
		request := newTestRequest(http.MethodGet, fmt.Sprintf("/api/files/%d", record.ID), nil)
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", testCase.fileName, recorder.Code, recorder.Body.String())
		}
		if disposition := recorder.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, testCase.disposition) {
			t.Fatalf("%s: expected %s disposition, got %q", testCase.fileName, testCase.disposition, disposition)
		}
		if sniff := recorder.Header().Get("X-Content-Type-Options"); sniff != "nosniff" {
			t.Fatalf("%s: expected nosniff, got %q", testCase.fileName, sniff)
		}
	}
}

func TestOIDCStartStoresSafeRedirect(t *testing.T) {
	server, _ := newTestServerWithOIDC(t, []config.OIDCProvider{{
		Name:      "main",
		IssuerURL: "https://issuer.example",
		ClientID:  "autable",
	}})

	request := newTestRequest(http.MethodGet, "/api/auth/oidc/main/start?redirect=%2Fapi%2Ffiles%2F6", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", recorder.Code, recorder.Body.String())
	}
	redirectCookie := ""
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "autable_oidc_redirect" {
			redirectCookie = cookie.Value
		}
	}
	if redirectCookie != "%2Fapi%2Ffiles%2F6" {
		t.Fatalf("expected redirect cookie, got %q", redirectCookie)
	}

	evil := newTestRequest(http.MethodGet, "/api/auth/oidc/main/start?redirect=https%3A%2F%2Fevil.example", nil)
	evilRecorder := httptest.NewRecorder()
	server.ServeHTTP(evilRecorder, evil)
	for _, cookie := range evilRecorder.Result().Cookies() {
		if cookie.Name == "autable_oidc_redirect" && cookie.Value != "" {
			t.Fatalf("expected absolute redirect to be dropped, got %q", cookie.Value)
		}
	}
}

func TestQueryRowsPageAPIReturnsRowsAndTotal(t *testing.T) {
	server, system := newTestServer(t)
	makeTestAdmins(server, "u1")
	for _, body := range []string{
		`{"values":{"name":"Ada","email":"ada@example.com","status":"active"}}`,
		`{"values":{"name":"Grace","email":"grace@example.com","status":"active"}}`,
		`{"values":{"name":"Linus","email":"linus@example.com","status":"archived"}}`,
	} {
		create := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows", bytes.NewBufferString(body))
		create.AddCookie(testSessionCookie(t, system, "u1"))
		createRecorder := httptest.NewRecorder()
		server.ServeHTTP(createRecorder, create)
		if createRecorder.Code != http.StatusCreated {
			t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
		}
	}

	page := func(body string) rowPageResponse {
		t.Helper()
		request := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows/page", bytes.NewBufferString(body))
		request.AddCookie(testSessionCookie(t, system, "u1"))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected page 200, got %d: %s", recorder.Code, recorder.Body.String())
		}
		var response rowPageResponse
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		return response
	}

	first := page(`{"limit":2}`)
	if first.Total != 3 || len(first.Rows) != 2 || first.Rows[0].Values["name"] != "Ada" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second := page(`{"limit":2,"offset":2}`)
	if second.Total != 3 || len(second.Rows) != 1 || second.Rows[0].Values["name"] != "Linus" {
		t.Fatalf("unexpected second page: %#v", second)
	}
	searched := page(`{"limit":10,"search":"grace"}`)
	if searched.Total != 1 || len(searched.Rows) != 1 || searched.Rows[0].Values["name"] != "Grace" {
		t.Fatalf("unexpected search page: %#v", searched)
	}

	invalid := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows/page", bytes.NewBufferString(`{"offset":2}`))
	invalid.AddCookie(testSessionCookie(t, system, "u1"))
	invalidRecorder := httptest.NewRecorder()
	server.ServeHTTP(invalidRecorder, invalid)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected offset-without-limit 400, got %d: %s", invalidRecorder.Code, invalidRecorder.Body.String())
	}

	// The legacy query endpoint keeps its bare-array response even with the
	// new paging fields present.
	query := newTestRequest(http.MethodPost, "/api/tables/db/contacts/rows/query", bytes.NewBufferString(`{"limit":2,"offset":1,"search":"a"}`))
	query.AddCookie(testSessionCookie(t, system, "u1"))
	queryRecorder := httptest.NewRecorder()
	server.ServeHTTP(queryRecorder, query)
	if queryRecorder.Code != http.StatusOK {
		t.Fatalf("expected query 200, got %d: %s", queryRecorder.Code, queryRecorder.Body.String())
	}
	var rows []rowResponse
	if err := json.NewDecoder(queryRecorder.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("unexpected query rows: %#v", rows)
	}
}

func TestRowReadsRequireViewGrantAndCurrentUserViewIsolatesRows(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "owner_email", Type: "string"},
			},
			Views: []metadata.View{{
				Name: "mine",
				Query: &metadata.ViewQuery{
					Combinator: "and",
					Rules:      []metadata.ViewQueryRule{{Field: "owner_email", Operator: "=", Value: "$current_user"}},
				},
			}},
		}},
	}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "boss")
	bossCookie := testSessionCookie(t, system, "boss")
	for _, body := range []string{
		`{"values":{"name":"Alice row","owner_email":"alice@example.com"}}`,
		`{"values":{"name":"Bob row","owner_email":"bob@example.com"}}`,
	} {
		create := newTestRequest(http.MethodPost, "/api/tables/workspace/contacts/rows", bytes.NewBufferString(body))
		create.AddCookie(bossCookie)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, create)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected create 201, got %d: %s", recorder.Code, recorder.Body.String())
		}
	}
	aliceCookie := testSessionCookie(t, system, "alice")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "alice", Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.FieldRead},
		permission.Grant{SubjectID: "alice", Scope: permission.ScopeView, Resource: "workspace.contacts", Field: "mine", Level: permission.Read},
	)

	// Without a grant on the built-in all view, the unfiltered list is denied.
	denied := newTestRequest(http.MethodGet, "/api/tables/workspace/contacts/rows", nil)
	denied.AddCookie(aliceCookie)
	deniedRecorder := httptest.NewRecorder()
	server.ServeHTTP(deniedRecorder, denied)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected unfiltered list 403, got %d: %s", deniedRecorder.Code, deniedRecorder.Body.String())
	}

	// The $current_user view returns only the caller's rows.
	mine := newTestRequest(http.MethodGet, "/api/tables/workspace/contacts/rows?view=mine", nil)
	mine.AddCookie(aliceCookie)
	mineRecorder := httptest.NewRecorder()
	server.ServeHTTP(mineRecorder, mine)
	if mineRecorder.Code != http.StatusOK {
		t.Fatalf("expected mine view 200, got %d: %s", mineRecorder.Code, mineRecorder.Body.String())
	}
	var rows []rowResponse
	if err := json.NewDecoder(mineRecorder.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["name"] != "Alice row" {
		t.Fatalf("expected only alice's row, got %#v", rows)
	}

	// Granting the built-in all view through the grants API opens the full list.
	grantAll := newTestRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"alice",
		"scope":"view",
		"resource":"workspace.contacts",
		"field":"all",
		"level":1
	}`))
	grantAll.AddCookie(bossCookie)
	grantRecorder := httptest.NewRecorder()
	server.ServeHTTP(grantRecorder, grantAll)
	if grantRecorder.Code != http.StatusCreated {
		t.Fatalf("expected all-view grant 201, got %d: %s", grantRecorder.Code, grantRecorder.Body.String())
	}
	full := newTestRequest(http.MethodGet, "/api/tables/workspace/contacts/rows", nil)
	full.AddCookie(aliceCookie)
	fullRecorder := httptest.NewRecorder()
	server.ServeHTTP(fullRecorder, full)
	if fullRecorder.Code != http.StatusOK {
		t.Fatalf("expected granted list 200, got %d: %s", fullRecorder.Code, fullRecorder.Body.String())
	}
	rows = nil
	if err := json.NewDecoder(fullRecorder.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected both rows after all grant, got %#v", rows)
	}
}

func TestExistingRowMutationsAreScopedToActorRowSet(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "owner_email", Type: "string"},
			},
			Views: []metadata.View{{
				Name: "mine",
				Query: &metadata.ViewQuery{
					Combinator: "and",
					Rules:      []metadata.ViewQueryRule{{Field: "owner_email", Operator: "=", Value: "$current_user"}},
				},
			}},
		}},
	}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "boss")
	bossCookie := testSessionCookie(t, system, "boss")
	for _, body := range []string{
		`{"values":{"name":"Alice row","owner_email":"alice@example.com"}}`,
		`{"values":{"name":"Bob row","owner_email":"bob@example.com"}}`,
	} {
		create := newTestRequest(http.MethodPost, "/api/tables/workspace/contacts/rows", bytes.NewBufferString(body))
		create.AddCookie(bossCookie)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, create)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected create 201, got %d: %s", recorder.Code, recorder.Body.String())
		}
	}
	aliceCookie := testSessionCookie(t, system, "alice")
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "alice", Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.FieldAll},
		permission.Grant{SubjectID: "alice", Scope: permission.ScopeView, Resource: "workspace.contacts", Field: "mine", Level: permission.Read},
		permission.Grant{SubjectID: "alice", Scope: permission.ScopeRecord, Resource: "workspace.contacts", Field: "delete", Level: permission.Write},
	)

	// Alice's own row (record 1) is editable; Bob's row (record 2) behaves
	// as nonexistent for both update and delete.
	allowed := newTestRequest(http.MethodPatch, "/api/tables/workspace/contacts/rows/1", bytes.NewBufferString(`{"values":{"name":"Alice edited"}}`))
	allowed.AddCookie(aliceCookie)
	allowedRecorder := httptest.NewRecorder()
	server.ServeHTTP(allowedRecorder, allowed)
	if allowedRecorder.Code != http.StatusOK {
		t.Fatalf("expected own-row update 200, got %d: %s", allowedRecorder.Code, allowedRecorder.Body.String())
	}
	blocked := newTestRequest(http.MethodPatch, "/api/tables/workspace/contacts/rows/2", bytes.NewBufferString(`{"values":{"name":"hijack"}}`))
	blocked.AddCookie(aliceCookie)
	blockedRecorder := httptest.NewRecorder()
	server.ServeHTTP(blockedRecorder, blocked)
	if blockedRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected out-of-set update 404, got %d: %s", blockedRecorder.Code, blockedRecorder.Body.String())
	}
	blockedDelete := newTestRequest(http.MethodDelete, "/api/tables/workspace/contacts/rows/2", nil)
	blockedDelete.AddCookie(aliceCookie)
	blockedDeleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(blockedDeleteRecorder, blockedDelete)
	if blockedDeleteRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected out-of-set delete 404, got %d: %s", blockedDeleteRecorder.Code, blockedDeleteRecorder.Body.String())
	}

	// Creating a row that lands outside the actor's row set still succeeds.
	create := newTestRequest(http.MethodPost, "/api/tables/workspace/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"submitted","owner_email":"someone-else@example.com"}
	}`))
	createGrant := newTestRequest(http.MethodPost, "/api/permissions/grants", bytes.NewBufferString(`{
		"subject_id":"alice",
		"scope":"record",
		"resource":"workspace.contacts",
		"field":"create",
		"level":2
	}`))
	createGrant.AddCookie(bossCookie)
	grantRecorder := httptest.NewRecorder()
	server.ServeHTTP(grantRecorder, createGrant)
	if grantRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create grant 201, got %d: %s", grantRecorder.Code, grantRecorder.Body.String())
	}
	create.AddCookie(aliceCookie)
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, create)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected out-of-set create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}
}

func TestSchemaChangesAreAdminOnly(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{
			{Name: "companies", Fields: []metadata.Field{{Name: "title", Type: "string"}}},
			{Name: "vendors", Fields: []metadata.Field{{Name: "title", Type: "string"}}},
			{
				Name: "contacts",
				Fields: []metadata.Field{
					{Name: "name", Type: "string"},
					{Name: "score", Type: "int"},
					{Name: "company", Type: "relation", RelationTable: "companies"},
				},
			},
		},
	}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "admin")
	// Full data access plus rows of the removed metadata scopes, as an
	// upgraded deployment might still hold: none of it confers schema.
	saveTestGrants(t, system,
		permission.Grant{SubjectID: "editor", Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.FieldAll},
		permission.Grant{SubjectID: "editor", Scope: permission.ScopeViewSet, Resource: "workspace.contacts", Level: permission.Read},
		permission.Grant{SubjectID: "editor", Scope: permission.ScopeRecord, Resource: "workspace.contacts", Field: "create", Level: permission.Write},
		permission.Grant{SubjectID: "editor", Scope: "field_add", Resource: "workspace.contacts", Level: permission.Write},
		permission.Grant{SubjectID: "editor", Scope: "field_modify", Resource: "workspace.contacts", Field: "company", Level: permission.Write},
	)
	saveTestAllViewGrant(t, system, "editor", "workspace.contacts")

	request := func(userID, method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		return appTestRequest(t, server, system, userID, method, target, strings.NewReader(body))
	}
	const tablePath = "/api/databases/workspace/tables/contacts"
	changes := []struct{ name, method, target, body string }{
		{"new field", http.MethodPut, tablePath, `{"name":"contacts","fields":[{"name":"phone","type":"string"}]}`},
		{"new formula", http.MethodPut, tablePath, `{"name":"contacts","fields":[{"name":"derived","type":"formula","value_type":"int","formula":"1"}]}`},
		{"relation retarget", http.MethodPut, tablePath, `{"name":"contacts","fields":[{"name":"company","type":"relation","relation_table":"vendors"}]}`},
		{"field delete", http.MethodPut, tablePath, `{"name":"contacts","fields":[{"name":"score","type":"int","deleted":true}]}`},
		{"new view", http.MethodPut, tablePath, `{"name":"contacts","views":[{"name":"named","sorts":[{"field":"name","direction":"asc"}]}]}`},
		{"field move", http.MethodPatch, "/api/databases/workspace/tables/contacts/fields/score/position", `{"position":"start"}`},
		{"new table", http.MethodPost, "/api/databases/workspace/tables", `{"name":"notes","fields":[{"name":"body","type":"string"}],"views":[]}`},
		{"fields endpoint", http.MethodPost, "/api/tables/workspace/contacts/fields", `{"fields":[{"name":"phone","type":"string"}]}`},
	}
	for _, change := range changes {
		if recorder := request("editor", change.method, change.target, change.body); recorder.Code != http.StatusForbidden {
			t.Fatalf("expected non-admin %s 403, got %d: %s", change.name, recorder.Code, recorder.Body.String())
		}
	}
	if fields := metadataFieldNames(mustTable(t, server, "workspace", "contacts").Fields); strings.Join(fields, ",") != "name,score,company" {
		t.Fatalf("expected denied changes to leave the schema alone, got %v", fields)
	}
	// Confirming fields that already exist changes nothing and stays open
	// to anyone who can see the table, so idempotent scripts keep working.
	if recorder := request("editor", http.MethodPost, "/api/tables/workspace/contacts/fields", `{"fields":[{"name":"name","type":"string"}]}`); recorder.Code != http.StatusOK {
		t.Fatalf("expected confirming an existing field 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := request("stranger", http.MethodPost, "/api/tables/workspace/contacts/fields", `{"fields":[{"name":"name","type":"string"}]}`); recorder.Code != http.StatusForbidden {
		t.Fatalf("expected confirming fields without any table grant 403, got %d: %s", recorder.Code, recorder.Body.String())
	}

	for _, change := range changes {
		recorder := request("admin", change.method, change.target, change.body)
		if recorder.Code < 200 || recorder.Code > 299 {
			t.Fatalf("expected admin %s to succeed, got %d: %s", change.name, recorder.Code, recorder.Body.String())
		}
	}
	contacts := mustTable(t, server, "workspace", "contacts")
	if phone, ok := contacts.Field("phone"); !ok || phone.Deleted {
		t.Fatalf("expected admin field add, got %#v", contacts.Fields)
	}
	if company, _ := contacts.Field("company"); company.RelationTable != "vendors" {
		t.Fatalf("expected admin relation retarget, got %#v", company)
	}
	if score, _ := contacts.Field("score"); !score.Deleted {
		t.Fatalf("expected admin field delete, got %#v", score)
	}
	if _, ok := server.catalogSnapshot().Table("workspace", "notes"); !ok {
		t.Fatal("expected admin table create")
	}
}

func mustTable(t *testing.T, server *Server, dbName, tableName string) metadata.Table {
	t.Helper()
	tableMeta, ok := server.catalogSnapshot().Table(dbName, tableName)
	if !ok {
		t.Fatalf("table %s.%s not found", dbName, tableName)
	}
	return tableMeta
}

func TestRowHistoryOpensToRowSetMembersWithFieldRedaction(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "workspace",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "secret", Type: "string"},
			},
		}},
	}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "boss")
	create := newTestRequest(http.MethodPost, "/api/tables/workspace/contacts/rows", bytes.NewBufferString(`{
		"values":{"name":"Ada","secret":"classified"}
	}`))
	create.AddCookie(testSessionCookie(t, system, "boss"))
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, create)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRecorder.Code, createRecorder.Body.String())
	}

	saveTestGrants(t, system,
		permission.Grant{SubjectID: "reader", Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "name", Level: permission.FieldRead},
	)
	saveTestAllViewGrant(t, system, "reader", "workspace.contacts")
	request := newTestRequest(http.MethodGet, "/api/tables/workspace/contacts/rows/1/history", nil)
	request.AddCookie(testSessionCookie(t, system, "reader"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected row-set member history 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var entries []rowHistoryResponse
	if err := json.NewDecoder(recorder.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Values["name"] != "Ada" {
		t.Fatalf("expected readable history values, got %#v", entries)
	}
	if _, ok := entries[0].Values["secret"]; ok {
		t.Fatalf("history leaked unreadable field: %#v", entries[0].Values)
	}
	if _, ok := entries[0].Diff["secret"]; ok {
		t.Fatalf("history diff leaked unreadable field: %#v", entries[0].Diff)
	}
}
