package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"autable/internal/appbundle"
	"autable/internal/appsdk"
	"autable/internal/permission"
	"autable/internal/systemdb"
	"autable/internal/table"

	"gorm.io/gorm"
)

// Hosted apps: see docs/design/apps.md.

type appResponse struct {
	systemdb.AppDefinition
	URL string `json:"url"`
}

type appVersionResponse struct {
	systemdb.AppVersion
	Supported  bool   `json:"supported"`
	SDKError   string `json:"sdk_error,omitempty"`
	PreviewURL string `json:"preview_url"`
}

type appDetailResponse struct {
	App      appResponse          `json:"app"`
	Versions []appVersionResponse `json:"versions"`
	SDK      sdkInfo              `json:"sdk"`
}

type sdkInfo struct {
	Version  string `json:"version"`
	MinMajor int    `json:"min_major"`
}

func appURL(app systemdb.AppDefinition) string {
	return "/apps/" + url.PathEscape(app.DatabaseName) + "/" + app.Name + "/"
}

func appPreviewURL(app systemdb.AppDefinition, version int64) string {
	return appURL(app) + "@" + strconv.FormatInt(version, 10) + "/"
}

func currentSDKInfo() sdkInfo {
	return sdkInfo{Version: appsdk.Version(), MinMajor: appsdk.MinMajor}
}

func (server *Server) appRoutes() {
	server.mux.HandleFunc("GET /api/apps/sdk", server.handleAppSDK)
	server.mux.HandleFunc("GET /api/databases/{database}/apps/{name}", server.handleGetApp)
	server.mux.HandleFunc("DELETE /api/databases/{database}/apps/{name}", server.handleDeleteApp)
	server.mux.HandleFunc("POST /api/databases/{database}/apps/{name}/versions", server.handleUploadAppVersion)
	server.mux.HandleFunc("POST /api/databases/{database}/apps/{name}/publish", server.handlePublishApp)
	server.mux.HandleFunc("GET /api/databases/{database}/apps/{name}/versions/{version}/source", server.handleAppSource)
	server.mux.HandleFunc("GET /apps/{database}/{name}", server.handleServeAppRoot)
	server.mux.HandleFunc("GET /apps/{database}/{name}/{rest...}", server.handleServeApp)
}

func (server *Server) handleAppSDK(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireUserID(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   appsdk.Version(),
		"min_major": appsdk.MinMajor,
		"files":     appsdk.Files(),
	})
}

func (server *Server) handleListApps(w http.ResponseWriter, r *http.Request, actorID, dbName string) {
	apps, err := server.system.Apps(r.Context(), dbName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	filtered, err := server.filterOpenableApps(r.Context(), actorID, apps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	responses := make([]appResponse, 0, len(filtered))
	for _, app := range filtered {
		responses = append(responses, appResponse{AppDefinition: app, URL: appURL(app)})
	}
	writeJSON(w, http.StatusOK, responses)
}

// appFromPath loads the app named in the URL for an actor who may open it
// (adminOnly false) or manage it (adminOnly true). It writes the error
// response itself.
func (server *Server) appFromPath(w http.ResponseWriter, r *http.Request, actorID string, adminOnly bool) (systemdb.AppDefinition, bool) {
	isAdmin := server.isAdmin(r.Context(), actorID)
	if adminOnly && !isAdmin {
		writeError(w, http.StatusForbidden, errAdminRequired)
		return systemdb.AppDefinition{}, false
	}
	app, err := server.system.AppByName(r.Context(), r.PathValue("database"), r.PathValue("name"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, errors.New("app not found"))
		return systemdb.AppDefinition{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return systemdb.AppDefinition{}, false
	}
	if !isAdmin {
		perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return systemdb.AppDefinition{}, false
		}
		if !canOpenApp(perms, actorID, false, app) {
			writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
			return systemdb.AppDefinition{}, false
		}
	}
	app.PermissionLevel = resourcePermissionLevel(isAdmin)
	return app, true
}

func (server *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	app, ok := server.appFromPath(w, r, actorID, false)
	if !ok {
		return
	}
	// Versions (messages, uploaders, preview links) are for admins; openers
	// only need the app and its published URL.
	versions := []systemdb.AppVersion{}
	if app.PermissionLevel >= permission.Write {
		var err error
		versions, err = server.system.AppVersions(r.Context(), app.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, appDetailResponse{
		App:      appResponse{AppDefinition: app, URL: appURL(app)},
		Versions: appVersionResponses(app, versions),
		SDK:      currentSDKInfo(),
	})
}

func appVersionResponses(app systemdb.AppDefinition, versions []systemdb.AppVersion) []appVersionResponse {
	responses := make([]appVersionResponse, 0, len(versions))
	for _, version := range versions {
		response := appVersionResponse{AppVersion: version, Supported: true, PreviewURL: appPreviewURL(app, version.Version)}
		if err := appsdk.Check(version.SDK); err != nil {
			response.Supported = false
			response.SDKError = err.Error()
		}
		responses = append(responses, response)
	}
	return responses
}

func (server *Server) handleUploadAppVersion(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireAdmin(w, r, actorID) {
		return
	}
	dbName, name := r.PathValue("database"), r.PathValue("name")
	if err := appbundle.ValidName(name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, appbundle.MaxCompressedBytes)
	archive, err := io.ReadAll(r.Body)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("bundle is larger than %d bytes", appbundle.MaxCompressedBytes))
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read bundle: %w", err))
		return
	}
	bundle, err := appbundle.Parse(archive)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if bundle.Manifest.Database != dbName || bundle.Manifest.Name != name {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%s names %s/%s, but the upload targets %s/%s", appbundle.ManifestName, bundle.Manifest.Database, bundle.Manifest.Name, dbName, name))
		return
	}
	if err := appsdk.Check(bundle.Manifest.SDK); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	app, err := server.system.AppByName(r.Context(), dbName, name)
	created := false
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if !server.databaseExists(dbName) {
			writeError(w, http.StatusNotFound, fmt.Errorf("database %q not found", dbName))
			return
		}
		app, err = server.system.CreateApp(r.Context(), systemdb.AppDefinition{DatabaseName: dbName, Name: name, Title: bundle.Manifest.Title, CreatorID: actorID})
		if err != nil {
			// A concurrent first upload may have created it; let the
			// client retry against the existing app.
			writeError(w, http.StatusConflict, fmt.Errorf("create app %s: %w", name, err))
			return
		}
		created = true
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	publish := r.URL.Query().Get("publish") == "1" || r.URL.Query().Get("publish") == "true"
	app, version, err := server.system.AddAppVersion(r.Context(), app.ID, bundle.Manifest.Title, systemdb.AppVersion{
		SDK:       bundle.Manifest.SDK,
		Message:   r.URL.Query().Get("message"),
		CreatedBy: actorID,
	}, archive, publish)
	if err != nil {
		if created {
			// Do not leave an app without versions behind.
			_ = server.system.DeleteApp(r.Context(), app.ID)
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	app.PermissionLevel = resourcePermissionLevel(true)
	server.mirrorVersion(r.Context(), actorID, appbundle.App, dbName, name, archive, mirroredVersion{
		Version: version.Version, PublishedVersion: app.PublishedVersion, SDK: version.SDK,
		Message: version.Message, CreatedBy: version.CreatedBy, CreatedAt: version.CreatedAt,
	})
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{
		"app":     appResponse{AppDefinition: app, URL: appURL(app)},
		"version": appVersionResponses(app, []systemdb.AppVersion{version})[0],
	})
}

func (server *Server) databaseExists(dbName string) bool {
	server.catalogMu.RLock()
	defer server.catalogMu.RUnlock()
	for _, database := range server.catalog.Databases {
		if database.Name == dbName {
			return true
		}
	}
	return false
}

func (server *Server) handlePublishApp(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	app, ok := server.appFromPath(w, r, actorID, true)
	if !ok {
		return
	}
	var request struct {
		Version int64 `json:"version"`
	}
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	version, err := server.system.AppVersion(r.Context(), app.ID, request.Version)
	if errors.Is(err, systemdb.ErrAppVersionNotFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("app %s has no version %d", app.Name, request.Version))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := appsdk.Check(version.SDK); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	published, err := server.system.PublishAppVersion(r.Context(), app.ID, request.Version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	published.PermissionLevel = app.PermissionLevel
	server.mirrorPublish(r.Context(), actorID, appbundle.App, app.DatabaseName, app.Name, published.PublishedVersion)
	writeJSON(w, http.StatusOK, appResponse{AppDefinition: published, URL: appURL(published)})
}

func (server *Server) handleAppSource(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	app, ok := server.appFromPath(w, r, actorID, true)
	if !ok {
		return
	}
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("version must be a positive number"))
		return
	}
	archive, err := server.system.AppVersionBundle(r.Context(), app.ID, version)
	if errors.Is(err, systemdb.ErrAppVersionNotFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("app %s has no version %d", app.Name, version))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	source, err := appbundle.Source(archive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-v%d-source.tar.gz"`, app.Name, version))
	_, _ = w.Write(source)
}

func (server *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireAdmin(w, r, actorID) {
		return
	}
	app, err := server.system.AppByName(r.Context(), r.PathValue("database"), r.PathValue("name"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, errors.New("app not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := server.system.DeleteApp(r.Context(), app.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.appBuilds.forget(app.ID)
	server.mirrorDelete(r.Context(), actorID, appbundle.App, app.DatabaseName, app.Name)
	w.WriteHeader(http.StatusNoContent)
}

// checkAppSDKHeader rejects API calls from app builds whose SDK this server
// no longer (or not yet) supports, so a tab left open across an upgrade
// stops instead of misbehaving.
func checkAppSDKHeader(w http.ResponseWriter, r *http.Request) bool {
	version := r.Header.Get(appsdk.Header)
	if version == "" {
		return true
	}
	if err := appsdk.Check(version); err != nil {
		writeError(w, http.StatusGone, err)
		return false
	}
	return true
}

// Serving.

func (server *Server) handleServeAppRoot(w http.ResponseWriter, r *http.Request) {
	redirectToTrailingSlash(w, r)
}

// redirectToTrailingSlash keeps the query, so links into an app's root
// without the slash still reach the app with their parameters.
func redirectToTrailingSlash(w http.ResponseWriter, r *http.Request) {
	target := r.URL.EscapedPath() + "/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

func (server *Server) handleServeApp(w http.ResponseWriter, r *http.Request) {
	actorID, ok, err := server.currentUserID(r)
	if err != nil || !ok {
		http.Redirect(w, r, "/login?redirect="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	app, err := server.system.AppByName(r.Context(), r.PathValue("database"), r.PathValue("name"))
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		writeAppPage(w, http.StatusInternalServerError, "Error", err.Error())
		return
	}
	if err != nil {
		writeAppPage(w, http.StatusNotFound, "App not found", "There is no app at this address.")
		return
	}
	rest := r.PathValue("rest")
	version := app.PublishedVersion
	base := appURL(app)
	// The published version is for everyone who may open the app; any
	// numbered version (preview before publishing) is for admins.
	adminOnly := false
	if strings.HasPrefix(rest, "@") {
		segment, remainder, _ := strings.Cut(rest, "/")
		parsed, err := strconv.ParseInt(strings.TrimPrefix(segment, "@"), 10, 64)
		if err != nil || parsed <= 0 {
			writeAppPage(w, http.StatusNotFound, "Version not found", "The address names an invalid app version.")
			return
		}
		if remainder == "" && !strings.HasSuffix(r.URL.Path, "/") {
			redirectToTrailingSlash(w, r)
			return
		}
		version, rest, adminOnly = parsed, remainder, true
		base = appPreviewURL(app, version)
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeAppPage(w, http.StatusInternalServerError, "Error", err.Error())
		return
	}
	isAdmin := server.isAdmin(r.Context(), actorID)
	if adminOnly && !isAdmin {
		writeAppPage(w, http.StatusForbidden, "No access", "Only admins can preview app versions.")
		return
	}
	if !canOpenApp(perms, actorID, isAdmin, app) {
		writeAppPage(w, http.StatusForbidden, "No access", "You do not have permission to open this app. Ask an admin for access.")
		return
	}
	if version == 0 {
		writeAppPage(w, http.StatusNotFound, "Not published", "This app has no published version yet.")
		return
	}
	build, err := server.appBuilds.load(r.Context(), server.system, app.ID, version)
	if errors.Is(err, systemdb.ErrAppVersionNotFound) {
		writeAppPage(w, http.StatusNotFound, "Version not found", fmt.Sprintf("This app has no version %d.", version))
		return
	}
	if err != nil {
		writeAppPage(w, http.StatusInternalServerError, "Error", err.Error())
		return
	}
	if build.sdkError != nil {
		writeAppPage(w, http.StatusGone, "This app needs an update", build.sdkError.Error())
		return
	}
	build.serve(w, r, rest, base)
}

func writeAppPage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%[1]s</title>
<style>body{font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:90vh;margin:0;color:#242424}main{max-width:32rem;padding:2rem}h1{font-size:1.25rem}p{color:#616161;line-height:1.5}</style></head>
<body><main><h1>%[1]s</h1><p>%[2]s</p><p><a href="/">autable</a></p></main></body></html>`, html.EscapeString(title), html.EscapeString(message))
}

type appBuild struct {
	key      string
	files    map[string][]byte
	sdkError error
}

func (build *appBuild) serve(w http.ResponseWriter, r *http.Request, rest, base string) {
	name := strings.TrimPrefix(path.Clean("/"+rest), "/")
	body, ok := build.files[name]
	if !ok || name == "" || name == "index.html" {
		if name != "" && name != "index.html" && strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
		body = injectBase(build.files[name], base)
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(base + name))
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%x"`, build.key, digest.Sum64()))
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
}

// injectBase makes relative asset URLs resolve against the app's own
// root, whatever client-side route the page was loaded from.
func injectBase(index []byte, base string) []byte {
	tag := []byte(`<base href="` + html.EscapeString(base) + `">`)
	lower := bytes.ToLower(index)
	// Insert right after <head>, or where the head would start when the
	// page omits it. Never before the doctype: content ahead of it would
	// switch the page into quirks mode.
	insert := 0
	for _, name := range []string{"head", "html", "!doctype"} {
		if end := startTagEnd(lower, name); end >= 0 {
			insert = end
			break
		}
	}
	out := make([]byte, 0, len(index)+len(tag))
	out = append(out, index[:insert]...)
	out = append(out, tag...)
	return append(out, index[insert:]...)
}

// startTagEnd returns the offset just past the first <name ...> start tag
// in lower, or -1. It does not match longer names such as <header>.
func startTagEnd(lower []byte, name string) int {
	open := []byte("<" + name)
	for offset := 0; ; {
		at := bytes.Index(lower[offset:], open)
		if at < 0 {
			return -1
		}
		at += offset
		next := at + len(open)
		if next < len(lower) && (lower[next] == '>' || lower[next] == '/' || isHTMLSpace(lower[next])) {
			if end := bytes.IndexByte(lower[next:], '>'); end >= 0 {
				return next + end + 1
			}
			return -1
		}
		offset = next
	}
}

func isHTMLSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r' || char == '\f'
}

// appBuildCache keeps recently served builds unpacked in memory. Versions
// are immutable, so entries never go stale; deleting an app drops them.
type appBuildCache struct {
	mu     sync.Mutex
	builds map[[2]int64]*appBuild
}

const appBuildCacheSize = 32

func (cache *appBuildCache) load(ctx context.Context, system *systemdb.DB, appID, version int64) (*appBuild, error) {
	key := [2]int64{appID, version}
	cache.mu.Lock()
	build, ok := cache.builds[key]
	cache.mu.Unlock()
	if ok {
		return build, nil
	}
	meta, err := system.AppVersion(ctx, appID, version)
	if err != nil {
		return nil, err
	}
	build = &appBuild{key: fmt.Sprintf("%d.%d", appID, version), sdkError: appsdk.Check(meta.SDK)}
	if build.sdkError == nil {
		archive, err := system.AppVersionBundle(ctx, appID, version)
		if err != nil {
			return nil, err
		}
		bundle, err := appbundle.Parse(archive)
		if err != nil {
			return nil, err
		}
		build.files = bundle.Dist
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.builds == nil {
		cache.builds = map[[2]int64]*appBuild{}
	}
	if len(cache.builds) >= appBuildCacheSize {
		for existing := range cache.builds {
			delete(cache.builds, existing)
			break
		}
	}
	cache.builds[key] = build
	return build, nil
}

func (cache *appBuildCache) forget(appID int64) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for key := range cache.builds {
		if key[0] == appID {
			delete(cache.builds, key)
		}
	}
}
