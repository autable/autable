package ctl

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"autable/internal/version"
)

// The skill is a short guide for coding agents: rules for changing a live
// server and a map of where each behavior lives in the source. It links the
// source at the release matching this binary instead of restating behavior,
// so it does not go stale as the code changes.
//
//go:embed skill.md
var skillTemplate string

const sourceRepository = "https://github.com/autable/autable"

func skillSource(release string) string {
	if release == "" || release == "dev" {
		return sourceRepository + "/tree/main"
	}
	return sourceRepository + "/tree/v" + strings.TrimPrefix(release, "v")
}

func renderSkill(release string) string {
	return strings.NewReplacer("{{SOURCE}}", skillSource(release), "{{VERSION}}", release).Replace(skillTemplate)
}

func defaultSkillDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills", "autable"), nil
}

func runSkillShow(a *app, args []string) error {
	if _, err := a.parse(a.newFlags("skill show"), args); err != nil {
		return err
	}
	_, err := io.WriteString(a.stdout, renderSkill(version.Version))
	return err
}

func runSkillInstall(a *app, args []string) error {
	fs := a.newFlags("skill install")
	dir := fs.String("dir", "", "directory to write SKILL.md into (default ~/.claude/skills/autable)")
	if _, err := a.parse(fs, args); err != nil {
		return err
	}
	if *dir == "" {
		defaultDir, err := defaultSkillDir()
		if err != nil {
			return err
		}
		*dir = defaultDir
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(*dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(renderSkill(version.Version)), 0o644); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}
	return a.printJSON(map[string]string{"path": path, "source": skillSource(version.Version)})
}
