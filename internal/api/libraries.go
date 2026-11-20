package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/dop251/goja"
	"gorm.io/gorm"

	"autable/internal/appbundle"
	"autable/internal/systemdb"
	"autable/internal/wfruntime"
	"autable/internal/wfsdk"
)

// Libraries: instance-wide TypeScript modules that workflows import as
// "@lib/<name>". A run always loads a library's published version, so
// publishing (or rolling back) a library changes every workflow that
// imports it at its next run, without pushing the workflows. Admin-only.
// See docs/design/workflows-v2.md.

type libraryVersionResponse struct {
	systemdb.LibraryVersion
	Published bool   `json:"published"`
	Supported bool   `json:"supported"`
	SDKError  string `json:"sdk_error,omitempty"`
}

type libraryResponse struct {
	systemdb.Library
	ImportedBy []systemdb.LibraryImporter `json:"imported_by"`
}

type libraryDetailResponse struct {
	Library  libraryResponse          `json:"library"`
	Versions []libraryVersionResponse `json:"versions"`
	SDK      sdkInfo                  `json:"sdk"`
}

func (server *Server) libraryRoutes() {
	server.mux.HandleFunc("GET /api/libs", server.handleListLibraries)
	server.mux.HandleFunc("GET /api/libs/{name}", server.handleGetLibrary)
	server.mux.HandleFunc("DELETE /api/libs/{name}", server.handleDeleteLibrary)
	server.mux.HandleFunc("POST /api/libs/{name}/versions", server.handleUploadLibraryVersion)
	server.mux.HandleFunc("POST /api/libs/{name}/publish", server.handlePublishLibrary)
	server.mux.HandleFunc("GET /api/libs/{name}/versions/{version}/source", server.handleLibrarySource)
	server.mux.HandleFunc("GET /api/libs/{name}/types", server.handleLibraryTypes)
}

// requireAdminUser authenticates an admin, writing the refusal itself.
func (server *Server) requireAdminUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	actorID, ok := server.requireUserID(w, r)
	if !ok || !server.requireAdmin(w, r, actorID) {
		return "", false
	}
	return actorID, true
}

// adminLibrary authenticates an admin and loads the library in the URL.
func (server *Server) adminLibrary(w http.ResponseWriter, r *http.Request) (string, systemdb.Library, bool) {
	actorID, ok := server.requireAdminUser(w, r)
	if !ok {
		return "", systemdb.Library{}, false
	}
	library, err := server.system.LibraryByName(r.Context(), r.PathValue("name"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, errors.New("library not found"))
		return "", systemdb.Library{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return "", systemdb.Library{}, false
	}
	return actorID, library, true
}

func (server *Server) libraryResponse(ctx context.Context, library systemdb.Library) (libraryResponse, error) {
	importers, err := server.system.LibraryImporters(ctx, library.Name)
	if err != nil {
		return libraryResponse{}, err
	}
	return libraryResponse{Library: library, ImportedBy: importers}, nil
}

func libraryVersionResponses(library systemdb.Library, versions []systemdb.LibraryVersion) []libraryVersionResponse {
	responses := make([]libraryVersionResponse, 0, len(versions))
	for _, version := range versions {
		response := libraryVersionResponse{LibraryVersion: version, Published: version.Version == library.PublishedVersion, Supported: true}
		if err := wfsdk.Check(version.SDK); err != nil {
			response.Supported = false
			response.SDKError = err.Error()
		}
		responses = append(responses, response)
	}
	return responses
}

func (server *Server) handleListLibraries(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdminUser(w, r); !ok {
		return
	}
	libraries, err := server.system.Libraries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	responses := make([]libraryResponse, 0, len(libraries))
	for _, library := range libraries {
		response, err := server.libraryResponse(r.Context(), library)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		responses = append(responses, response)
	}
	writeJSON(w, http.StatusOK, responses)
}

func (server *Server) handleGetLibrary(w http.ResponseWriter, r *http.Request) {
	_, library, ok := server.adminLibrary(w, r)
	if !ok {
		return
	}
	server.writeLibraryDetail(w, r.Context(), http.StatusOK, library)
}

func (server *Server) writeLibraryDetail(w http.ResponseWriter, ctx context.Context, status int, library systemdb.Library) {
	response, err := server.libraryResponse(ctx, library)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	versions, err := server.system.LibraryVersions(ctx, library.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, status, libraryDetailResponse{Library: response, Versions: libraryVersionResponses(library, versions), SDK: currentWorkflowSDKInfo()})
}

// checkLibraryImports verifies that every library a bundle imports can be
// loaded now: it exists, has a published version, and that version runs
// on this server. self names the library being published, which may not
// import itself.
func (server *Server) checkLibraryImports(ctx context.Context, imports []string, self string) error {
	for _, name := range imports {
		if err := wfruntime.ValidLibraryName(name); err != nil {
			return fmt.Errorf("the bundle imports %q: %w", wfruntime.LibraryPrefix+name, err)
		}
		if name == self {
			return fmt.Errorf("library %s imports itself", self)
		}
		if _, err := server.workflows.libraryResolver().Library(ctx, name); err != nil {
			return fmt.Errorf("the bundle imports %s%s, which cannot be loaded: %w", wfruntime.LibraryPrefix, name, err)
		}
	}
	return nil
}

// checkLibraryCycle refuses to publish imports for library name that would
// make the published libraries import each other in a circle.
func (server *Server) checkLibraryCycle(ctx context.Context, name string, imports []string) error {
	graph, err := server.system.PublishedLibraryImports(ctx)
	if err != nil {
		return err
	}
	graph[name] = imports
	var path []string
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(current string) error
	visit = func(current string) error {
		if visiting[current] {
			start := 0
			for index, entry := range path {
				if entry == current {
					start = index
				}
			}
			return fmt.Errorf("publishing would make libraries import each other: %s", strings.Join(append(append([]string{}, path[start:]...), current), " -> "))
		}
		if done[current] {
			return nil
		}
		visiting[current] = true
		path = append(path, current)
		for _, next := range graph[current] {
			if err := visit(next); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		visiting[current] = false
		done[current] = true
		return nil
	}
	return visit(name)
}

// libraryPublishCheck validates a library bundle for publishing and returns
// the libraries it imports.
func (server *Server) libraryPublishCheck(ctx context.Context, name string, bundle appbundle.Bundle) ([]string, error) {
	imports := wfruntime.Imports(string(bundle.Dist[appbundle.Library.RequiredDist]))
	if err := server.checkLibraryImports(ctx, imports, name); err != nil {
		return nil, err
	}
	if err := server.checkLibraryCycle(ctx, name, imports); err != nil {
		return nil, err
	}
	return imports, nil
}

func (server *Server) handleUploadLibraryVersion(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireAdminUser(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
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
	bundle, err := appbundle.Library.Parse(archive)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if bundle.Manifest.Name != name {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%s names %s, but the upload targets %s", appbundle.LibraryManifestName, bundle.Manifest.Name, name))
		return
	}
	if err := wfsdk.Check(bundle.Manifest.SDK); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if _, err := wfruntime.CompileLibrary(name, 0, string(bundle.Dist[appbundle.Library.RequiredDist])); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	publish := r.URL.Query().Get("publish") == "1" || r.URL.Query().Get("publish") == "true"
	var imports []string
	if publish {
		// Check before storing anything: a bundle that cannot be published
		// is rejected as a whole.
		if imports, err = server.libraryPublishCheck(r.Context(), name, bundle); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
	}
	library, err := server.system.LibraryByName(r.Context(), name)
	created := false
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		library, err = server.system.CreateLibrary(r.Context(), systemdb.Library{Name: name, Title: bundle.Manifest.Title, CreatorID: actorID})
		if err != nil {
			writeError(w, http.StatusConflict, fmt.Errorf("create library %s: %w", name, err))
			return
		}
		created = true
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	library, version, err := server.system.AddLibraryVersion(r.Context(), library.ID, bundle.Manifest.Title, systemdb.LibraryVersion{
		SDK:       bundle.Manifest.SDK,
		Message:   r.URL.Query().Get("message"),
		CreatedBy: actorID,
	}, archive)
	if err == nil && publish {
		library, err = server.system.PublishLibraryVersion(r.Context(), library.ID, version.Version, imports)
		version.Imports = imports
	}
	if err != nil {
		if created {
			_ = server.system.DeleteLibrary(r.Context(), library.ID)
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.mirrorVersion(r.Context(), actorID, appbundle.Library, "", library.Name, archive, mirroredVersion{
		Version: version.Version, PublishedVersion: library.PublishedVersion, SDK: version.SDK,
		Message: version.Message, CreatedBy: version.CreatedBy, CreatedAt: version.CreatedAt,
	})
	response, err := server.libraryResponse(r.Context(), library)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{
		"library": response,
		"version": libraryVersionResponses(library, []systemdb.LibraryVersion{version})[0],
	})
}

func (server *Server) handlePublishLibrary(w http.ResponseWriter, r *http.Request) {
	actorID, library, ok := server.adminLibrary(w, r)
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
	version, err := server.system.LibraryVersion(r.Context(), library.ID, request.Version)
	if errors.Is(err, systemdb.ErrLibraryVersionNotFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("library %s has no version %d", library.Name, request.Version))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := wfsdk.Check(version.SDK); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	archive, err := server.system.LibraryVersionBundle(r.Context(), library.ID, version.Version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	bundle, err := appbundle.Library.Parse(archive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	imports, err := server.libraryPublishCheck(r.Context(), library.Name, bundle)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	published, err := server.system.PublishLibraryVersion(r.Context(), library.ID, version.Version, imports)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.mirrorPublish(r.Context(), actorID, appbundle.Library, "", library.Name, published.PublishedVersion)
	server.writeLibraryDetail(w, r.Context(), http.StatusOK, published)
}

func (server *Server) handleLibrarySource(w http.ResponseWriter, r *http.Request) {
	_, library, ok := server.adminLibrary(w, r)
	if !ok {
		return
	}
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("version must be a positive number"))
		return
	}
	archive, err := server.system.LibraryVersionBundle(r.Context(), library.ID, version)
	if errors.Is(err, systemdb.ErrLibraryVersionNotFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("library %s has no version %d", library.Name, version))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	source, err := appbundle.Library.Source(archive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-v%d-source.tar.gz"`, library.Name, version))
	_, _ = w.Write(source)
}

// handleLibraryTypes serves the type declarations of the published
// version, which `autablectl workflow sdk` vendors for importers.
func (server *Server) handleLibraryTypes(w http.ResponseWriter, r *http.Request) {
	_, library, ok := server.adminLibrary(w, r)
	if !ok {
		return
	}
	if library.PublishedVersion == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("library %s has no published version", library.Name))
		return
	}
	archive, err := server.system.LibraryVersionBundle(r.Context(), library.ID, library.PublishedVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	bundle, err := appbundle.Library.Parse(archive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    library.Name,
		"version": library.PublishedVersion,
		"types":   string(bundle.Dist["index.d.ts"]),
	})
}

func (server *Server) handleDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	actorID, library, ok := server.adminLibrary(w, r)
	if !ok {
		return
	}
	importers, err := server.system.LibraryImporters(r.Context(), library.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(importers) > 0 {
		names := make([]string, 0, len(importers))
		for _, importer := range importers {
			if importer.Kind == "workflow" {
				names = append(names, fmt.Sprintf("workflow %s/%s v%d", importer.DatabaseName, importer.Name, importer.Version))
			} else {
				names = append(names, fmt.Sprintf("library %s v%d", importer.Name, importer.Version))
			}
		}
		writeError(w, http.StatusConflict, fmt.Errorf("library %s is imported by the published %s; publish versions that do not import it first", library.Name, strings.Join(names, ", ")))
		return
	}
	if err := server.system.DeleteLibrary(r.Context(), library.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.workflows.forgetLibrary(library.ID)
	server.mirrorDelete(r.Context(), actorID, appbundle.Library, "", library.Name)
	w.WriteHeader(http.StatusNoContent)
}

// libraryResolver loads the published version of a library for a run.
type libraryResolver struct {
	engine *workflowEngine
}

func (engine *workflowEngine) libraryResolver() libraryResolver {
	return libraryResolver{engine: engine}
}

func (resolver libraryResolver) Library(ctx context.Context, name string) (wfruntime.Library, error) {
	engine := resolver.engine
	system := engine.server.system
	library, err := system.LibraryByName(ctx, name)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return wfruntime.Library{}, fmt.Errorf("library %s does not exist; push it with autablectl lib push", name)
	}
	if err != nil {
		return wfruntime.Library{}, err
	}
	if library.PublishedVersion == 0 {
		return wfruntime.Library{}, fmt.Errorf("library %s has no published version; publish one with autablectl lib publish", name)
	}
	key := [2]int64{library.ID, library.PublishedVersion}
	engine.mu.Lock()
	program, ok := engine.libPrograms[key]
	engine.mu.Unlock()
	if ok {
		return wfruntime.Library{Name: name, Version: library.PublishedVersion, Program: program}, nil
	}
	version, err := system.LibraryVersion(ctx, library.ID, library.PublishedVersion)
	if err != nil {
		return wfruntime.Library{}, err
	}
	if err := wfsdk.Check(version.SDK); err != nil {
		return wfruntime.Library{}, fmt.Errorf("library %s version %d: %w", name, version.Version, err)
	}
	archive, err := system.LibraryVersionBundle(ctx, library.ID, version.Version)
	if err != nil {
		return wfruntime.Library{}, err
	}
	bundle, err := appbundle.Library.Parse(archive)
	if err != nil {
		return wfruntime.Library{}, err
	}
	program, err = wfruntime.CompileLibrary(name, version.Version, string(bundle.Dist[appbundle.Library.RequiredDist]))
	if err != nil {
		return wfruntime.Library{}, err
	}
	engine.cacheLibraryProgram(key, program)
	return wfruntime.Library{Name: name, Version: version.Version, Program: program}, nil
}

func (engine *workflowEngine) cacheLibraryProgram(key [2]int64, program *goja.Program) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.libPrograms) >= programCacheSize {
		for existing := range engine.libPrograms {
			delete(engine.libPrograms, existing)
			break
		}
	}
	engine.libPrograms[key] = program
}

func (engine *workflowEngine) forgetLibrary(libraryID int64) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	for key := range engine.libPrograms {
		if key[0] == libraryID {
			delete(engine.libPrograms, key)
		}
	}
}
