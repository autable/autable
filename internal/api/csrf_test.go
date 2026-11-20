package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"autable/internal/permission"
	"autable/internal/systemdb"
)

// rawRequest builds a request without the JSON content type newTestRequest
// adds, so tests control every header.
func rawRequest(method, target, body string) *http.Request {
	if body == "" {
		return httptest.NewRequest(method, target, nil)
	}
	return httptest.NewRequest(method, target, strings.NewReader(body))
}

func serve(server *Server, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func TestCrossSiteRequestsAreRejected(t *testing.T) {
	server, system := newTestServer(t)
	server.SetPublicURL("https://autable.example")
	saveTestRecordCreateGrant(t, system, "writer", "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: "writer", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll})
	cookie := testSessionCookie(t, system, "writer")
	cookie.Name = secureSessionCookieName
	const body = `{"values":{"name":"Ada"}}`

	cases := []struct {
		name    string
		headers map[string]string
		status  int
	}{
		{"same-origin fetch", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusCreated},
		{"user-initiated", map[string]string{"Sec-Fetch-Site": "none"}, http.StatusCreated},
		{"cross-site fetch", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"same-site fetch", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"cross-site despite matching origin", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://autable.example"}, http.StatusForbidden},
		{"public url origin", map[string]string{"Origin": "https://autable.example"}, http.StatusCreated},
		{"request host origin", map[string]string{"Origin": "http://internal.example:8080"}, http.StatusCreated},
		{"foreign origin", map[string]string{"Origin": "https://attacker.example"}, http.StatusForbidden},
		{"public url host on another scheme", map[string]string{"Origin": "http://autable.example"}, http.StatusForbidden},
		{"opaque origin", map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"no browser headers", map[string]string{}, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := rawRequest(http.MethodPost, "/api/tables/db/contacts/rows", body)
			request.Host = "internal.example:8080"
			request.Header.Set("Content-Type", "application/json")
			for key, value := range tc.headers {
				request.Header.Set(key, value)
			}
			request.AddCookie(cookie)
			if recorder := serve(server, request); recorder.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, recorder.Code, recorder.Body.String())
			}
		})
	}

	// Safe methods are never blocked: navigations and reads carry no
	// state change.
	read := rawRequest(http.MethodGet, "/api/auth/me", "")
	read.Header.Set("Sec-Fetch-Site", "cross-site")
	read.AddCookie(cookie)
	if recorder := serve(server, read); recorder.Code != http.StatusOK {
		t.Fatalf("expected cross-site GET to pass the CSRF check, got %d: %s", recorder.Code, recorder.Body.String())
	}

	// Cross-site login would sign the victim in to the attacker's account.
	login := rawRequest(http.MethodPost, "/api/auth/login", `{"email":"writer@example.com","password":"correct horse"}`)
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("Origin", "https://attacker.example")
	if recorder := serve(server, login); recorder.Code != http.StatusForbidden {
		t.Fatalf("expected cross-site login 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestJSONEndpointsRequireJSONContentType(t *testing.T) {
	server, system := newTestServer(t)
	saveTestRecordCreateGrant(t, system, "writer", "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: "writer", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll})
	cookie := testSessionCookie(t, system, "writer")
	const body = `{"values":{"name":"Ada"}}`

	for _, contentType := range []string{"", "text/plain", "text/plain;charset=UTF-8", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
		request := rawRequest(http.MethodPost, "/api/tables/db/contacts/rows", body)
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.AddCookie(cookie)
		if recorder := serve(server, request); recorder.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("expected content type %q rejected with 415, got %d: %s", contentType, recorder.Code, recorder.Body.String())
		}
	}
	for _, contentType := range []string{"application/json", "application/json; charset=utf-8"} {
		request := rawRequest(http.MethodPost, "/api/tables/db/contacts/rows", body)
		request.Header.Set("Content-Type", contentType)
		request.AddCookie(cookie)
		if recorder := serve(server, request); recorder.Code != http.StatusCreated {
			t.Fatalf("expected content type %q accepted, got %d: %s", contentType, recorder.Code, recorder.Body.String())
		}
	}
}

func TestBearerRequestsAreExemptFromBrowserChecks(t *testing.T) {
	server, system := newTestServer(t)
	server.SetPublicURL("https://autable.example")
	saveTestRecordCreateGrant(t, system, "cli-user", "db.contacts")
	saveTestGrants(t, system, permission.Grant{SubjectID: "cli-user", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll})
	testSessionCookie(t, system, "cli-user")
	session, err := system.CreateSession(context.Background(), "cli-user", cliSessionTTL)
	if err != nil {
		t.Fatal(err)
	}

	// A bearer token is never attached by a browser on its own, so neither
	// the origin checks nor the content-type rule apply.
	request := rawRequest(http.MethodPost, "/api/tables/db/contacts/rows", `{"values":{"name":"Ada"}}`)
	request.Header.Set("Authorization", "Bearer "+session.Token)
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	if recorder := serve(server, request); recorder.Code != http.StatusCreated {
		t.Fatalf("expected bearer request 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSessionCookieNameAndFlagsFollowPublicURL(t *testing.T) {
	register := func(server *Server, email string) *http.Cookie {
		t.Helper()
		request := newTestRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{"email":"`+email+`","display_name":"Person","password":"correct horse"}`))
		recorder := serve(server, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected register 201, got %d: %s", recorder.Code, recorder.Body.String())
		}
		return sessionCookie(t, recorder)
	}

	plain, _ := newTestServer(t)
	cookie := register(plain, "plain@example.com")
	if cookie.Name != sessionCookieName || cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("expected a plain HttpOnly Lax cookie over http, got %#v", cookie)
	}

	secure, _ := newTestServer(t)
	secure.SetPublicURL("https://autable.example/")
	cookie = register(secure, "secure@example.com")
	if cookie.Name != secureSessionCookieName || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("expected a Secure __Host- cookie over https, got %#v", cookie)
	}
	me := newTestRequest(http.MethodGet, "/api/auth/me", nil)
	me.AddCookie(cookie)
	if recorder := serve(secure, me); recorder.Code != http.StatusOK {
		t.Fatalf("expected the __Host- cookie to authenticate, got %d: %s", recorder.Code, recorder.Body.String())
	}
	// The plain name is not read over https, so a cookie planted by a
	// plain-http page or a sibling host cannot stand in for the session.
	planted := newTestRequest(http.MethodGet, "/api/auth/me", nil)
	planted.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie.Value})
	if recorder := serve(secure, planted); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected the plain cookie name to be ignored over https, got %d: %s", recorder.Code, recorder.Body.String())
	}

	logout := newTestRequest(http.MethodPost, "/api/auth/logout", nil)
	logout.AddCookie(cookie)
	recorder := serve(secure, logout)
	cleared := sessionCookie(t, recorder)
	if cleared.Name != secureSessionCookieName || !cleared.Secure || cleared.MaxAge >= 0 {
		t.Fatalf("expected logout to clear the __Host- cookie, got %#v", cleared)
	}
}

func TestCurrentUserReportsAdmin(t *testing.T) {
	server, system := newTestServer(t)
	server.auth.Admins = []string{"Boss@Example.com"}

	me := func(userID string) currentUserResponse {
		t.Helper()
		request := newTestRequest(http.MethodGet, "/api/auth/me", nil)
		request.AddCookie(testSessionCookie(t, system, userID))
		recorder := serve(server, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected me 200, got %d: %s", recorder.Code, recorder.Body.String())
		}
		return decodeTestJSON[currentUserResponse](t, recorder)
	}
	if user := me("boss"); !user.IsAdmin || user.Email != "boss@example.com" {
		t.Fatalf("expected the configured email to be an admin, case-insensitively, got %#v", user)
	}
	if user := me("member"); user.IsAdmin {
		t.Fatalf("expected other users not to be admins, got %#v", user)
	}

	login := newTestRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"boss@example.com","password":"correct horse"}`))
	recorder := serve(server, login)
	if recorder.Code != http.StatusOK || !decodeTestJSON[currentUserResponse](t, recorder).IsAdmin {
		t.Fatalf("expected login to report the admin flag, got %d: %s", recorder.Code, recorder.Body.String())
	}
	// Workflow subjects are never admins, whatever their id looks like.
	if server.isAdmin(context.Background(), systemdb.WorkflowSubjectID(1)) {
		t.Fatal("expected workflow subjects never to be admins")
	}
}

func TestRegisterRefusesExistingEmail(t *testing.T) {
	server, _ := newTestServer(t)
	server.auth.Admins = []string{"boss@example.com"}
	first := newTestRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"boss@example.com","display_name":"Boss","password":"correct horse"}`))
	if recorder := serve(server, first); recorder.Code != http.StatusCreated {
		t.Fatalf("expected first register 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	// Registering the same email again must not take the account over.
	again := newTestRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"Boss@Example.com","display_name":"Mallory","password":"another horse"}`))
	recorder := serve(server, again)
	if recorder.Code != http.StatusConflict || len(recorder.Result().Cookies()) != 0 {
		t.Fatalf("expected duplicate register 409 without a session, got %d: %s", recorder.Code, recorder.Body.String())
	}
	stolen := newTestRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"boss@example.com","password":"another horse"}`))
	if recorder := serve(server, stolen); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected the new password to be rejected, got %d: %s", recorder.Code, recorder.Body.String())
	}
	original := newTestRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"boss@example.com","password":"correct horse"}`))
	recorder = serve(server, original)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected the original password to keep working, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var user currentUserResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &user); err != nil || user.DisplayName != "Boss" || !user.IsAdmin {
		t.Fatalf("expected the original account, got %#v, %v", user, err)
	}
}
