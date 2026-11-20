// Package docs embeds the product documentation so that binaries can ship
// it, e.g. as the reference files of the agent skill package that
// `autablectl skill export` writes.
package docs

import (
	"embed"
	"io/fs"
)

//go:embed *.md design/*.md
var embedded embed.FS

// Files returns every embedded document keyed by its slash-separated path
// relative to docs/, e.g. "cli.md" or "design/apps.md".
func Files() map[string]string {
	files := map[string]string{}
	err := fs.WalkDir(embedded, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := embedded.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		panic(err)
	}
	return files
}
