package ctl

import (
	"embed"
	"html"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The project `app init` starts from: Vite, React, react-router, and
// TypeScript, wired to the vendored SDK. Files whose names would be
// special in this repository are stored with a leading underscore
// (_gitignore) and renamed when written.
//
//go:embed all:apptemplate
var appTemplate embed.FS

const appTemplateRoot = "apptemplate"

func writeAppTemplate(dir, name, title string) error {
	replacer := strings.NewReplacer("__APP_NAME__", name, "__APP_TITLE_HTML__", html.EscapeString(title))
	return fs.WalkDir(appTemplate, appTemplateRoot, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := appTemplate.ReadFile(file)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(file, appTemplateRoot+"/")
		if base := path.Base(rel); strings.HasPrefix(base, "_") {
			rel = path.Join(path.Dir(rel), "."+strings.TrimPrefix(base, "_"))
		}
		// A checkout with CRLF line endings embeds them; projects get LF.
		content := replacer.Replace(strings.ReplaceAll(string(data), "\r\n", "\n"))
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, []byte(content), 0o644)
	})
}
