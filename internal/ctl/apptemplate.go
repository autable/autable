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

// The projects `app init` and `workflow init` start from. Files whose
// names would be special in this repository are stored with a leading
// underscore (_gitignore) and renamed when written.
//
// apptemplate: Vite, React, react-router, and TypeScript, wired to the
// vendored app SDK. wftemplate: TypeScript bundled by esbuild into
// dist/workflow.js, wired to the vendored workflow SDK.
//
//go:embed all:apptemplate all:wftemplate
var projectTemplates embed.FS

func writeAppTemplate(dir, name, title string) error {
	return writeTemplate("apptemplate", dir, strings.NewReplacer("__APP_NAME__", name, "__APP_TITLE_HTML__", html.EscapeString(title)))
}

func writeWorkflowTemplate(dir, name, title string) error {
	return writeTemplate("wftemplate", dir, strings.NewReplacer("__WORKFLOW_NAME__", name, "__WORKFLOW_TITLE__", title))
}

func writeTemplate(root, dir string, replacer *strings.Replacer) error {
	return fs.WalkDir(projectTemplates, root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := projectTemplates.ReadFile(file)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(file, root+"/")
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
