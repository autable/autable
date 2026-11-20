package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func Handler(api http.Handler) http.Handler {
	dist, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		panic(err)
	}
	return HandlerWithFS(api, dist)
}

func HandlerWithFS(api http.Handler, frontend fs.FS) http.Handler {
	files := http.FileServer(http.FS(frontend))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/apps/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "." || name == "" {
			serveIndex(w, r, frontend)
			return
		}
		stat, err := fs.Stat(frontend, name)
		if err == nil && !stat.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") || strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		serveIndex(w, r, frontend)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, frontend fs.FS) {
	data, err := fs.ReadFile(frontend, "index.html")
	if err != nil {
		http.Error(w, "frontend index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
