package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func cliChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func cliAuthorizeForm(port, state, challenge string) url.Values {
	return url.Values{"port": {port}, "state": {state}, "challenge": {challenge}}
}

// authorizeCLI posts the browser confirmation and returns the redirect.
func authorizeCLI(t *testing.T, server *Server, cookie *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, cliAuthorizeURL, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func redeemCLICode(t *testing.T, server *Server, code, verifier string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"code": code, "verifier": verifier})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/cli/token", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func bearerRequest(method, target, token string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func TestCLILoginIssuesBearerSession(t *testing.T) {
	server, system := newTestServer(t)
	cookie := testSessionCookie(t, system, "cli-user")
	verifier := "a-verifier-only-the-cli-knows-0123456789abcdef"
	challenge := cliChallenge(verifier)
	query := cliAuthorizeForm("43210", "state-1", challenge).Encode()

	// Signed out: the browser is sent through the login page and back.
	page := httptest.NewRecorder()
	server.ServeHTTP(page, httptest.NewRequest(http.MethodGet, cliAuthorizeURL+"?"+query, nil))
	if page.Code != http.StatusFound {
		t.Fatalf("expected redirect to login, got %d: %s", page.Code, page.Body.String())
	}
	location, err := url.Parse(page.Header().Get("Location"))
	if err != nil || location.Path != "/login" || location.Query().Get("redirect") != cliAuthorizeURL+"?"+query {
		t.Fatalf("unexpected login redirect %q", page.Header().Get("Location"))
	}

	// Signed in: a confirmation page that cannot be framed.
	pageRequest := httptest.NewRequest(http.MethodGet, cliAuthorizeURL+"?"+query, nil)
	pageRequest.AddCookie(cookie)
	page = httptest.NewRecorder()
	server.ServeHTTP(page, pageRequest)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "cli-user@example.com") || !strings.Contains(page.Body.String(), `value="43210"`) {
		t.Fatalf("unexpected confirmation page %d: %s", page.Code, page.Body.String())
	}
	if page.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("expected X-Frame-Options DENY, got %q", page.Header().Get("X-Frame-Options"))
	}

	confirm := authorizeCLI(t, server, cookie, cliAuthorizeForm("43210", "state-1", challenge))
	if confirm.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", confirm.Code, confirm.Body.String())
	}
	callback, err := url.Parse(confirm.Header().Get("Location"))
	if err != nil || callback.Scheme != "http" || callback.Host != "127.0.0.1:43210" || callback.Path != "/callback" || callback.Query().Get("state") != "state-1" {
		t.Fatalf("unexpected callback %q", confirm.Header().Get("Location"))
	}
	code := callback.Query().Get("code")

	redeemed := redeemCLICode(t, server, code, verifier)
	if redeemed.Code != http.StatusOK {
		t.Fatalf("expected token 200, got %d: %s", redeemed.Code, redeemed.Body.String())
	}
	var issued cliTokenResponse
	if err := json.NewDecoder(redeemed.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	if issued.Token == "" || issued.User.ID != "cli-user" {
		t.Fatalf("unexpected token response %#v", issued)
	}
	if remaining := time.Until(time.UnixMilli(issued.ExpiresAt)); remaining < cliSessionTTL-time.Minute {
		t.Fatalf("expected a %s session, expires in %s", cliSessionTTL, remaining)
	}

	// The code is single use.
	if again := redeemCLICode(t, server, code, verifier); again.Code != http.StatusUnauthorized {
		t.Fatalf("expected reused code 401, got %d", again.Code)
	}

	me := httptest.NewRecorder()
	server.ServeHTTP(me, bearerRequest(http.MethodGet, "/api/auth/me", issued.Token))
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), "cli-user@example.com") {
		t.Fatalf("expected bearer token to authenticate, got %d: %s", me.Code, me.Body.String())
	}

	logout := httptest.NewRecorder()
	server.ServeHTTP(logout, bearerRequest(http.MethodPost, "/api/auth/logout", issued.Token))
	if logout.Code != http.StatusOK {
		t.Fatalf("expected logout 200, got %d", logout.Code)
	}
	me = httptest.NewRecorder()
	server.ServeHTTP(me, bearerRequest(http.MethodGet, "/api/auth/me", issued.Token))
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked token 401, got %d: %s", me.Code, me.Body.String())
	}
}

func TestCLILoginRejectsWrongVerifierAndConsumesCode(t *testing.T) {
	server, system := newTestServer(t)
	cookie := testSessionCookie(t, system, "cli-user")
	verifier := "the-real-verifier-0123456789abcdefghijklmnop"
	confirm := authorizeCLI(t, server, cookie, cliAuthorizeForm("43210", "s", cliChallenge(verifier)))
	callback, err := url.Parse(confirm.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := callback.Query().Get("code")
	if wrong := redeemCLICode(t, server, code, "someone-elses-verifier"); wrong.Code != http.StatusUnauthorized {
		t.Fatalf("expected wrong verifier 401, got %d", wrong.Code)
	}
	if right := redeemCLICode(t, server, code, verifier); right.Code != http.StatusUnauthorized {
		t.Fatalf("expected the code to be spent after a failed attempt, got %d", right.Code)
	}
}

func TestCLIAuthorizeRequiresBrowserSession(t *testing.T) {
	server, system := newTestServer(t)
	cookie := testSessionCookie(t, system, "cli-user")
	challenge := cliChallenge("verifier")
	form := cliAuthorizeForm("43210", "s", challenge)

	if anonymous := authorizeCLI(t, server, nil, form); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("expected anonymous authorize 401, got %d", anonymous.Code)
	}

	// A bearer token cannot mint further tokens; only a browser can approve.
	bearer := httptest.NewRequest(http.MethodPost, cliAuthorizeURL, strings.NewReader(form.Encode()))
	bearer.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	bearer.Header.Set("Authorization", "Bearer "+cookie.Value)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, bearer)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected bearer-only authorize 401, got %d", recorder.Code)
	}

	crossSite := httptest.NewRequest(http.MethodPost, cliAuthorizeURL, strings.NewReader(form.Encode()))
	crossSite.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSite.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, crossSite)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected cross-site authorize 403, got %d", recorder.Code)
	}

	for name, bad := range map[string]url.Values{
		"privileged port": cliAuthorizeForm("80", "s", challenge),
		"missing state":   cliAuthorizeForm("43210", "", challenge),
		"bad challenge":   cliAuthorizeForm("43210", "s", "not-a-digest"),
	} {
		if response := authorizeCLI(t, server, cookie, bad); response.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", name, response.Code)
		}
	}
}
