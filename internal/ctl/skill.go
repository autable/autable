package ctl

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"autable/docs"
	"autable/internal/appsdk"
	"autable/internal/version"
	"autable/internal/wfsdk"
)

// The agent skill is a self-contained package: SKILL.md (rules and working
// loops) plus reference/ files the agent reads on demand. Agents using it
// usually cannot see autable's source, so the references are generated from
// the canonical sources compiled into this binary (docs/, the SDKs, the
// project templates) and never drift from the release that wrote them.
//
//go:embed skill.md
var skillTemplate string

// skillManaged are the top-level entries a skill package consists of;
// writing a package replaces exactly these and leaves anything else alone.
var skillManaged = []string{"SKILL.md", "VERSION", "reference"}

func renderSkill(release string) string {
	return strings.ReplaceAll(lf(skillTemplate), "{{VERSION}}", release)
}

// lf normalizes line endings: a checkout with CRLF line endings embeds
// them, and the package is always LF.
func lf(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}

// skillPackage returns every file of the skill package keyed by its
// slash-separated path relative to the package root.
func skillPackage(release string) map[string]string {
	files := map[string]string{
		"SKILL.md": renderSkill(release),
		"VERSION":  "autablectl " + release + "\n",
	}
	for name, content := range docs.Files() {
		files["reference/"+name] = lf(content)
	}
	for name, content := range appsdk.Files() {
		files["reference/app-sdk/"+name] = lf(content)
	}
	for name, content := range wfsdk.Files() {
		files["reference/workflow-sdk/"+name] = lf(content)
	}
	for kind, root := range map[string]string{"app": "apptemplate", "workflow": "wftemplate", "lib": "libtemplate"} {
		data, err := projectTemplates.ReadFile(root + "/AGENTS.md")
		if err != nil {
			panic(err)
		}
		files["reference/templates/"+kind+"/AGENTS.md"] = lf(string(data))
	}
	return files
}

func sortedPaths(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	return paths
}

func defaultSkillDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills", "autable"), nil
}

// isEmptyDir reports whether dir is missing or has no entries.
func isEmptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// holdsAutableSkill reports whether dir already contains an autable skill
// package, which install may replace.
func holdsAutableSkill(dir string) bool {
	file, err := os.Open(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for line := 0; line < 10 && scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "name: autable" {
			return true
		}
	}
	return false
}

// writeSkillPackage replaces the package entries in dir with files, so no
// reference file of an older release lingers.
func writeSkillPackage(dir string, files map[string]string) error {
	for _, name := range skillManaged {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	for _, name := range sortedPaths(files) {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(files[name]), 0o644); err != nil {
			return fmt.Errorf("write skill: %w", err)
		}
	}
	return nil
}

// skillTarball returns the package as a .tar.gz with SKILL.md at its root.
func skillTarball(files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	archive := tar.NewWriter(gz)
	modified := time.Now().UTC().Truncate(time.Second)
	dirs := map[string]bool{}
	for _, name := range sortedPaths(files) {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = true
		}
	}
	var entries []string
	for dir := range dirs {
		entries = append(entries, dir+"/")
	}
	entries = append(entries, sortedPaths(files)...)
	sort.Strings(entries)
	for _, name := range entries {
		header := &tar.Header{Name: name, ModTime: modified, Format: tar.FormatPAX}
		if strings.HasSuffix(name, "/") {
			header.Typeflag, header.Mode = tar.TypeDir, 0o755
		} else {
			header.Typeflag, header.Mode, header.Size = tar.TypeReg, 0o644, int64(len(files[name]))
		}
		if err := archive.WriteHeader(header); err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := io.WriteString(archive, files[name]); err != nil {
				return nil, err
			}
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func packageSize(files map[string]string) int {
	total := 0
	for _, content := range files {
		total += len(content)
	}
	return total
}

func runSkillShow(a *app, args []string) error {
	if _, err := a.parse(a.newFlags("skill show"), args); err != nil {
		return err
	}
	_, err := io.WriteString(a.stdout, renderSkill(version.Version))
	return err
}

func runSkillExport(a *app, args []string) error {
	fs := a.newFlags("skill export")
	force := fs.Bool("force", false, "write into a non-empty DIR (replacing a previous package) or over an existing --tar FILE")
	tarPath := fs.String("tar", "", "also write the package as a .tar.gz with SKILL.md at its root")
	values, err := a.parse(fs, args, "DIR?")
	if err != nil {
		return err
	}
	if len(values) == 0 && *tarPath == "" {
		return usagef("expected a directory, --tar FILE, or both")
	}
	files := skillPackage(version.Version)
	result := map[string]any{"version": version.Version, "files": len(files), "bytes": packageSize(files)}
	if len(values) == 1 {
		dir := values[0]
		if empty, err := isEmptyDir(dir); err != nil {
			return err
		} else if !empty && !*force {
			return usagef("%s is not empty; pass --force to replace the skill package in it", dir)
		}
		if err := writeSkillPackage(dir, files); err != nil {
			return err
		}
		result["dir"] = dir
	}
	if *tarPath != "" {
		if _, err := os.Stat(*tarPath); err == nil && !*force {
			return usagef("%s exists; pass --force to overwrite it", *tarPath)
		}
		data, err := skillTarball(files)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*tarPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", *tarPath, err)
		}
		result["tar"] = *tarPath
	}
	return a.printJSON(result)
}

func runSkillInstall(a *app, args []string) error {
	fs := a.newFlags("skill install")
	dirFlag := fs.String("dir", "", "same as the DIR argument")
	values, err := a.parse(fs, args, "DIR?")
	if err != nil {
		return err
	}
	dir := *dirFlag
	if len(values) == 1 {
		if dir != "" {
			return usagef("give the directory either as DIR or as --dir, not both")
		}
		dir = values[0]
	}
	if dir == "" {
		if dir, err = defaultSkillDir(); err != nil {
			return err
		}
	}
	if empty, err := isEmptyDir(dir); err != nil {
		return err
	} else if !empty && !holdsAutableSkill(dir) {
		return usagef("%s is not empty and holds no autable skill; choose another directory or use skill export --force", dir)
	}
	files := skillPackage(version.Version)
	if err := writeSkillPackage(dir, files); err != nil {
		return err
	}
	return a.printJSON(map[string]any{
		"dir":     dir,
		"path":    filepath.Join(dir, "SKILL.md"),
		"version": version.Version,
		"files":   len(files),
		"bytes":   packageSize(files),
	})
}
