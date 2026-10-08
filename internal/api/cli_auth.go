package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"autable/internal/auth"
)

// Command-line clients sign in through the browser: the CLI listens on a
// loopback port, opens /api/auth/cli/authorize, and the signed-in user
// confirms. The server then redirects to the loopback port with a one-time
// code that the CLI redeems, together with the PKCE verifier only it knows,
// for a session token it presents as "Authorization: Bearer <token>".
//
// The token is an ordinary session with a longer lifetime, so it carries
// exactly the permissions of the user who approved it and is revoked by
// logging out with it.

const (
	cliSessionTTL   = 90 * 24 * time.Hour
	cliCodeTTL      = 5 * time.Minute
	cliAuthorizeURL = "/api/auth/cli/authorize"
)

type cliAuthCode struct {
	userID    string
	challenge string
	expiresAt time.Time
}

type cliAuthCodes struct {
	mu    sync.Mutex
	codes map[string]cliAuthCode
}

func (codes *cliAuthCodes) issue(userID, challenge string) (string, error) {
	code, err := auth.NewSessionToken()
	if err != nil {
		return "", err
	}
	codes.mu.Lock()
	defer codes.mu.Unlock()
	now := time.Now()
	if codes.codes == nil {
		codes.codes = map[string]cliAuthCode{}
	}
	for key, entry := range codes.codes {
		if now.After(entry.expiresAt) {
			delete(codes.codes, key)
		}
	}
	codes.codes[code] = cliAuthCode{userID: userID, challenge: challenge, expiresAt: now.Add(cliCodeTTL)}
	return code, nil
}

// redeem consumes the code whether or not the verifier matches, so a code
// can be tried once only.
func (codes *cliAuthCodes) redeem(code, verifier string) (string, bool) {
	codes.mu.Lock()
	entry, ok := codes.codes[code]
	delete(codes.codes, code)
	codes.mu.Unlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(computed), []byte(entry.challenge)) != 1 {
		return "", false
	}
	return entry.userID, true
}

type cliAuthorizeParams struct {
	Port      int
	State     string
	Challenge string
}

func parseCLIAuthorizeParams(values url.Values) (cliAuthorizeParams, error) {
	port, err := strconv.Atoi(values.Get("port"))
	if err != nil || port < 1024 || port > 65535 {
		return cliAuthorizeParams{}, errors.New("port must be a number between 1024 and 65535")
	}
	state := values.Get("state")
	if state == "" || len(state) > 128 {
		return cliAuthorizeParams{}, errors.New("state is required")
	}
	challenge := values.Get("challenge")
	if decoded, err := base64.RawURLEncoding.DecodeString(challenge); err != nil || len(decoded) != sha256.Size {
		return cliAuthorizeParams{}, errors.New("challenge must be a base64url SHA-256 digest")
	}
	return cliAuthorizeParams{Port: port, State: state, Challenge: challenge}, nil
}

var cliAuthorizePage = template.Must(template.New("cli-authorize").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authorize command-line access</title>
<style>
body { font-family: system-ui, sans-serif; display: flex; justify-content: center; padding: 64px 16px; color: #1f2328; }
main { max-width: 420px; }
button { font-size: 16px; padding: 8px 20px; cursor: pointer; }
p { line-height: 1.5; }
</style>
</head>
<body>
<main>
<h1>Authorize command-line access</h1>
<p>A command-line client on this computer is asking to act as <strong>{{.User}}</strong>, with all of your permissions, for {{.Days}} days.</p>
<p>Only continue if you just started <code>autablectl login</code> yourself.</p>
<form method="post" action="{{.Action}}">
<input type="hidden" name="port" value="{{.Port}}">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="challenge" value="{{.Challenge}}">
<button type="submit">Authorize</button>
</form>
</main>
</body>
</html>
`))

func (server *Server) handleCLIAuthorizePage(w http.ResponseWriter, r *http.Request) {
	params, err := parseCLIAuthorizeParams(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user, ok, err := server.cookieUser(r)
	if err != nil || !ok {
		http.Redirect(w, r, "/login?redirect="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	label := user.Email
	if label == "" {
		label = user.DisplayName
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("Cache-Control", "no-store")
	_ = cliAuthorizePage.Execute(w, map[string]any{
		"User":      label,
		"Days":      int(cliSessionTTL / (24 * time.Hour)),
		"Action":    cliAuthorizeURL,
		"Port":      params.Port,
		"State":     params.State,
		"Challenge": params.Challenge,
	})
}

func (server *Server) handleCLIAuthorize(w http.ResponseWriter, r *http.Request) {
	// The session cookie is SameSite=Lax, so a cross-site form post arrives
	// without it; the fetch-metadata check rejects it early in browsers that
	// send the header.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		writeError(w, http.StatusForbidden, errors.New("cross-site authorization is not allowed"))
		return
	}
	user, ok, err := server.cookieUser(r)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication is required"))
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	params, err := parseCLIAuthorizeParams(r.PostForm)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	code, err := server.cliCodes.issue(user.ID, params.Challenge)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	callback := url.URL{
		Scheme:   "http",
		Host:     "127.0.0.1:" + strconv.Itoa(params.Port),
		Path:     "/callback",
		RawQuery: url.Values{"code": {code}, "state": {params.State}}.Encode(),
	}
	http.Redirect(w, r, callback.String(), http.StatusSeeOther)
}

type cliTokenRequest struct {
	Code     string `json:"code"`
	Verifier string `json:"verifier"`
}

type cliTokenResponse struct {
	Token     string       `json:"token"`
	ExpiresAt int64        `json:"expires_at"`
	User      userResponse `json:"user"`
}

func (server *Server) handleCLIToken(w http.ResponseWriter, r *http.Request) {
	var request cliTokenRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	userID, ok := server.cliCodes.redeem(request.Code, request.Verifier)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("invalid or expired authorization code"))
		return
	}
	user, err := server.system.User(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	session, err := server.system.CreateSession(r.Context(), user.ID, cliSessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, cliTokenResponse{
		Token:     session.Token,
		ExpiresAt: session.ExpiresAt.UnixMilli(),
		User:      toUserResponse(user),
	})
}

// bearerToken returns the token of an "Authorization: Bearer" header, or ""
// when the request carries none.
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// requestSessionToken prefers a bearer token over the session cookie.
func requestSessionToken(r *http.Request) (string, error) {
	if token := bearerToken(r); token != "" {
		return token, nil
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

// cookieUser resolves only the browser session cookie; CLI authorization
// must be confirmed in a browser, never by another bearer token.
func (server *Server) cookieUser(r *http.Request) (auth.User, bool, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return auth.User{}, false, nil
		}
		return auth.User{}, false, err
	}
	user, _, err := server.system.UserBySessionToken(r.Context(), cookie.Value)
	if err != nil {
		return auth.User{}, false, err
	}
	return user, true, nil
}
