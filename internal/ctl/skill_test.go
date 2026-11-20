package ctl

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"autable/internal/version"
)

// readTree returns every file under dir keyed by slash path.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, file)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSkillExportWritesSelfContainedPackage(t *testing.T) {
	env := newTestEnv(t)
	dir := filepath.Join(env.dir, "export")
	exported := decode[map[string]any](t, env.run(0, "skill", "export", dir))
	if exported["dir"] != dir || exported["version"] != version.Version {
		t.Fatalf("unexpected export output: %#v", exported)
	}
	files := readTree(t, dir)
	if len(files) != int(exported["files"].(float64)) {
		t.Fatalf("export reported %v files, wrote %d", exported["files"], len(files))
	}

	skill, ok := files["SKILL.md"]
	if !ok {
		t.Fatal("the package has no SKILL.md at its root")
	}
	frontmatter := regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n`).FindStringSubmatch(skill)
	if frontmatter == nil {
		t.Fatalf("SKILL.md has no frontmatter: %s", skill[:min(200, len(skill))])
	}
	if !regexp.MustCompile(`(?m)^name: autable$`).MatchString(frontmatter[1]) {
		t.Fatalf("frontmatter lacks name: autable: %s", frontmatter[1])
	}
	description := regexp.MustCompile(`(?m)^description: (.+)$`).FindStringSubmatch(frontmatter[1])
	if description == nil || len(description[1]) < 100 || len(description[1]) > 1024 || !strings.Contains(description[1], "autablectl") {
		t.Fatalf("frontmatter needs a precise description of at most 1024 characters: %v", description)
	}
	if lines := strings.Count(skill, "\n"); lines > 200 {
		t.Errorf("SKILL.md has %d lines; keep it focused", lines)
	}
	if strings.Contains(skill, "{{") || !strings.Contains(skill, "autablectl "+version.Version) {
		t.Fatal("SKILL.md is not rendered with the version")
	}
	if files["VERSION"] != "autablectl "+version.Version+"\n" {
		t.Fatalf("VERSION: %q", files["VERSION"])
	}

	// Every reference SKILL.md names exists in the package.
	refs := regexp.MustCompile("`(reference/[^`]+)`").FindAllStringSubmatch(skill, -1)
	if len(refs) < 12 {
		t.Fatalf("expected SKILL.md to index the reference files, found %d", len(refs))
	}
	for _, ref := range refs {
		name := ref[1]
		if strings.HasSuffix(name, "/") {
			found := false
			for file := range files {
				found = found || strings.HasPrefix(file, name)
			}
			if !found {
				t.Errorf("SKILL.md references %s, which is empty in the package", name)
			}
		} else if _, ok := files[name]; !ok {
			t.Errorf("SKILL.md references %s, which is not in the package", name)
		}
	}
	// The generated references track their canonical sources.
	for _, name := range []string{
		"reference/cli.md", "reference/query.md", "reference/http-api.md", "reference/permissions.md",
		"reference/design/apps.md", "reference/design/workflows-v2.md", "reference/design/workflows-host-api.md",
		"reference/design/remote-runner.md", "reference/app-sdk/autable.ts", "reference/workflow-sdk/index.ts",
		"reference/templates/app/AGENTS.md", "reference/templates/workflow/AGENTS.md", "reference/templates/lib/AGENTS.md",
	} {
		if strings.TrimSpace(files[name]) == "" {
			t.Errorf("package lacks %s", name)
		}
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "docs", "query.md"))
	if err != nil {
		t.Fatal(err)
	}
	if files["reference/query.md"] != strings.ReplaceAll(string(source), "\r\n", "\n") {
		t.Error("reference/query.md differs from docs/query.md")
	}

	// Self-contained: no pointers into autable's source, no inline shell.
	sourcePointers := regexp.MustCompile(`(?i)github\.com/autable|\{\{SOURCE\}\}|read the source|source code is the reference|where to look in the source`)
	inlineShell := regexp.MustCompile("!`")
	total := 0
	for name, content := range files {
		total += len(content)
		if sourcePointers.MatchString(content) {
			t.Errorf("%s points the agent at autable's source: %q", name, sourcePointers.FindString(content))
		}
		if inlineShell.MatchString(content) {
			t.Errorf("%s contains an inline shell command (!`)", name)
		}
		if strings.Contains(content, "\r\n") {
			t.Errorf("%s has CRLF line endings", name)
		}
	}
	if skillPathPattern := regexp.MustCompile("`(?:internal|cmd|web)/"); skillPathPattern.MatchString(skill) {
		t.Errorf("SKILL.md names a source path: %s", skillPathPattern.FindString(skill))
	}
	if total >= 10<<20 {
		t.Fatalf("package is %d bytes; the limit is 10 MB", total)
	}

	// A non-empty directory is refused unless --force, which replaces the
	// package and drops files of an older one.
	env.run(2, "skill", "export", dir)
	stale := filepath.Join(dir, "reference", "stale.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.run(0, "skill", "export", dir, "--force")
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("export --force kept a stale reference file")
	}
	if again := readTree(t, dir); len(again) != len(files) {
		t.Fatalf("export --force wrote %d files, want %d", len(again), len(files))
	}

	// skill show prints SKILL.md.
	if shown := env.run(0, "skill", "show"); shown != skill {
		t.Fatal("skill show and the exported SKILL.md disagree")
	}
	env.run(2, "skill", "export")
}

func TestSkillExportTarball(t *testing.T) {
	env := newTestEnv(t)
	archive := filepath.Join(env.dir, "autable-skill.tar.gz")
	env.run(0, "skill", "export", "--tar", archive)
	env.run(2, "skill", "export", "--tar", archive)
	env.run(0, "skill", "export", "--tar", archive, "--force")

	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	got := map[string]string{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, reader); err != nil {
			t.Fatal(err)
		}
		got[header.Name] = buf.String()
	}
	want := skillPackage(version.Version)
	if len(got) != len(want) {
		t.Fatalf("tarball has %d files, want %d", len(got), len(want))
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("tarball entry %s differs from the package", name)
		}
	}
}

func TestSkillInstallWritesSamePackage(t *testing.T) {
	env := newTestEnv(t)
	exported := filepath.Join(env.dir, "export")
	env.run(0, "skill", "export", exported)
	want := readTree(t, exported)

	dir := filepath.Join(env.dir, "skills", "autable")
	installed := decode[map[string]any](t, env.run(0, "skill", "install", dir))
	if installed["path"] != filepath.Join(dir, "SKILL.md") {
		t.Fatalf("unexpected install output: %#v", installed)
	}
	got := readTree(t, dir)
	if len(got) != len(want) {
		t.Fatalf("install wrote %d files, export %d", len(got), len(want))
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("install and export disagree on %s", name)
		}
	}

	// Installing again replaces the previous package; --dir still works.
	stale := filepath.Join(dir, "reference", "stale.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.run(0, "skill", "install", "--dir", dir)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("install kept a stale reference file")
	}
	env.run(2, "skill", "install", dir, "--dir", dir)

	// A directory holding something else is left alone.
	other := filepath.Join(env.dir, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.run(2, "skill", "install", other)
	if _, err := os.Stat(filepath.Join(other, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("install wrote into a directory that holds no autable skill")
	}
}
