package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerRoutesAPIRequestsToAPI(t *testing.T) {
	apiHit := false
	handler := HandlerWithFS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHit = true
		w.WriteHeader(http.StatusTeapot)
	}), testFrontendFS())

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/metadata", nil))

	if !apiHit {
		t.Fatal("expected API handler to be called")
	}
	if response.Code != http.StatusTeapot {
		t.Fatalf("expected API status, got %d", response.Code)
	}
}

func TestHandlerRoutesHostedAppsToAPI(t *testing.T) {
	var apiPaths []string
	handler := HandlerWithFS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiPaths = append(apiPaths, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}), testFrontendFS())

	for _, target := range []string{"/apps/shop/widgets/", "/apps/shop/widgets/assets/app.js", "/apps/shop/widgets/@2/orders/1"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusTeapot {
			t.Fatalf("expected %s to reach the API handler, got %d", target, response.Code)
		}
	}
	if len(apiPaths) != 3 {
		t.Fatalf("expected three API calls, got %v", apiPaths)
	}

	// Frontend routes that merely start with "apps" stay with the SPA.
	route := httptest.NewRecorder()
	handler.ServeHTTP(route, httptest.NewRequest(http.MethodGet, "/appsettings", nil))
	if route.Code != http.StatusOK || !strings.Contains(route.Body.String(), "<main>app</main>") {
		t.Fatalf("expected SPA fallback for /appsettings, got status=%d body=%q", route.Code, route.Body.String())
	}
}

func TestHandlerServesFrontendAssetsAndSPAFallback(t *testing.T) {
	handler := HandlerWithFS(http.NotFoundHandler(), testFrontendFS())

	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "console.log('ok');" {
		t.Fatalf("expected asset response, got status=%d body=%q", asset.Code, asset.Body.String())
	}

	route := httptest.NewRecorder()
	handler.ServeHTTP(route, httptest.NewRequest(http.MethodGet, "/workspace/table", nil))
	if route.Code != http.StatusOK || !strings.Contains(route.Body.String(), "<main>app</main>") {
		t.Fatalf("expected SPA fallback, got status=%d body=%q", route.Code, route.Body.String())
	}

	missingAsset := httptest.NewRecorder()
	handler.ServeHTTP(missingAsset, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if missingAsset.Code != http.StatusNotFound {
		t.Fatalf("expected missing asset 404, got %d", missingAsset.Code)
	}
}

func testFrontendFS() fs.FS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<main>app</main>")},
		"assets/app.js": {Data: []byte("console.log('ok');")},
	}
}
