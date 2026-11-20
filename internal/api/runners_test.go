package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"autable/internal/systemdb"
)

func runnerTestSession(t *testing.T, server *Server, system *systemdb.DB) *http.Cookie {
	t.Helper()
	makeTestAdmins(server, "u1")
	return testSessionCookie(t, system, "u1")
}

func TestRunnersEndpointRequiresAuthentication(t *testing.T) {
	server, _ := newTestServer(t)

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, newTestRequest(http.MethodGet, "/api/databases/db/runners", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, newTestRequest(http.MethodPost, "/api/databases/db/runners", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", recorder.Code)
	}
}

func TestRunnersAreAdminOnly(t *testing.T) {
	server, system := newTestServer(t)
	cookie := testSessionCookie(t, system, "outsider")

	listRequest := newTestRequest(http.MethodGet, "/api/databases/db/runners", nil)
	listRequest.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, listRequest)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected runner listing to be admin-only, got %d %s", recorder.Code, recorder.Body.String())
	}

	resetRequest := newTestRequest(http.MethodPost, "/api/databases/db/runners", nil)
	resetRequest.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, resetRequest)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin reset to be forbidden, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRunnerTokenResetAndListing(t *testing.T) {
	server, system := newTestServer(t)
	cookie := runnerTestSession(t, server, system)

	listRequest := newTestRequest(http.MethodGet, "/api/databases/db/runners", nil)
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
	if listing.Token.Exists || len(listing.Runners) != 0 {
		t.Fatalf("expected empty initial state, got %#v", listing)
	}

	resetRequest := newTestRequest(http.MethodPost, "/api/databases/db/runners", nil)
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
	listRequest = newTestRequest(http.MethodGet, "/api/databases/db/runners", nil)
	listRequest.AddCookie(cookie)
	server.ServeHTTP(recorder, listRequest)
	if err := json.Unmarshal(recorder.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if !listing.Token.Exists || listing.Token.CreatedAt != reset.CreatedAt {
		t.Fatalf("expected token metadata after reset, got %#v", listing.Token)
	}
}
