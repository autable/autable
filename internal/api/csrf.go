package api

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// Cross-site request forgery defenses for the API. A browser attaches the
// session cookie to any request it sends to this origin, including ones a
// foreign page triggers, so state-changing API requests must prove they
// come from this origin:
//
//   - Sec-Fetch-Site, sent by all current browsers, must be same-origin or
//     none (typed by the user / not initiated by a page).
//   - Without it, an Origin header must name this origin: the origin of
//     server.public_url, or the host the request was sent to.
//   - JSON endpoints only accept Content-Type: application/json, which a
//     foreign page cannot send without a CORS preflight this server never
//     grants (see readJSON).
//
// Requests authenticated with "Authorization: Bearer" (autablectl, scripts)
// carry no ambient credentials and are exempt, as is the workflow webhook,
// which authenticates with a token in its body.

var errCrossSiteRequest = errors.New("cross-site request rejected")

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// isWebhookPath matches POST /api/databases/{database}/workflows/{id}/webhook.
func isWebhookPath(path string) bool {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	return len(segments) == 6 && segments[0] == "api" && segments[1] == "databases" && segments[3] == "workflows" && segments[5] == "webhook"
}

// checkSameOrigin writes 403 and returns false for a state-changing API
// request a foreign page could have triggered.
func (server *Server) checkSameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if !isUnsafeMethod(r.Method) || bearerToken(r) != "" || isWebhookPath(r.URL.Path) {
		return true
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		if site == "same-origin" || site == "none" {
			return true
		}
		writeError(w, http.StatusForbidden, errCrossSiteRequest)
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && !server.isOwnOrigin(r, origin) {
		writeError(w, http.StatusForbidden, errCrossSiteRequest)
		return false
	}
	return true
}

// isOwnOrigin accepts the origin of server.public_url, or an origin whose
// host matches the Host the request was sent to. The scheme is only
// compared against public_url: TLS may terminate at a proxy, and the
// server does not trust X-Forwarded-* headers.
func (server *Server) isOwnOrigin(r *http.Request, origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if server.publicURL != "" {
		public, err := url.Parse(server.publicURL)
		if err == nil && strings.EqualFold(public.Scheme, parsed.Scheme) && strings.EqualFold(public.Host, parsed.Host) {
			return true
		}
	}
	return r.Host != "" && strings.EqualFold(parsed.Host, r.Host)
}

var errJSONContentType = errors.New("request body must be sent with Content-Type: application/json")

// requireJSONContentType is the readJSON half of the CSRF defense; bearer
// requests are exempt like they are from the origin checks.
func requireJSONContentType(r *http.Request) error {
	if bearerToken(r) != "" {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errJSONContentType
	}
	return nil
}
