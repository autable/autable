package ctl

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Libraries: instance-wide TypeScript modules that workflows import as
// "@lib/<name>" and that every run loads in their published version, so
// publishing a library changes every importing workflow at its next run.
// They are built, versioned, and pulled like workflows (project.go) but
// belong to no database. See docs/design/workflows-v2.md.

type libSummary struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	PublishedVersion int64  `json:"published_version"`
	LatestVersion    int64  `json:"latest_version"`
}

type libVersionSummary struct {
	Version int64    `json:"version"`
	SDK     string   `json:"sdk"`
	Message string   `json:"message"`
	Imports []string `json:"imports"`
}

type libDetail struct {
	Library  libSummary          `json:"library"`
	Versions []libVersionSummary `json:"versions"`
}

func (detail libDetail) versionNumbers() []int64 {
	numbers := make([]int64, 0, len(detail.Versions))
	for _, version := range detail.Versions {
		numbers = append(numbers, version.Version)
	}
	return numbers
}

func libPath(name string, rest ...string) string { return libProject.apiPath("", name, rest...) }

func runLibInit(a *app, args []string) error {
	return libProject.runInit(a, args, writeLibTemplate, func(dir string, manifest projectManifest) []string {
		return []string{
			"cd " + dir,
			"npm install",
			"npm run build",
			"autablectl lib push -m \"first version\"",
			"autablectl lib publish " + manifest.Name + " 1   # every importing workflow uses it from its next run",
		}
	})
}

func runLibSDK(a *app, args []string) error { return libProject.runSDK(a, args) }

func runLibPush(a *app, args []string) error {
	result, _, err := libProject.runPush(a, args)
	if err != nil {
		return err
	}
	var uploaded struct {
		Library libSummary        `json:"library"`
		Version libVersionSummary `json:"version"`
	}
	if err := json.Unmarshal(result.response, &uploaded); err != nil {
		return fmt.Errorf("unexpected upload response: %w", err)
	}
	return a.printJSON(map[string]any{
		"name":              result.manifest.Name,
		"version":           uploaded.Version.Version,
		"sdk":               uploaded.Version.SDK,
		"imports":           uploaded.Version.Imports,
		"published_version": uploaded.Library.PublishedVersion,
		"bytes":             len(result.archive),
		"dist_files":        result.counts.dist,
		"source_files":      result.counts.source,
		"shared_files":      result.counts.shared,
		"root":              result.counts.root,
	})
}

func runLibList(a *app, args []string) error {
	if _, err := a.parse(a.newFlags("lib list"), args); err != nil {
		return err
	}
	return a.get(apiPath("libs"))
}

func runLibVersions(a *app, args []string) error {
	values, err := a.parse(a.newFlags("lib versions"), args, "NAME")
	if err != nil {
		return err
	}
	return a.get(libPath(values[0]))
}

func fetchLibDetail(c *client, name string) (libDetail, error) {
	var detail libDetail
	err := c.call(http.MethodGet, libPath(name), nil, &detail)
	return detail, err
}

func runLibPublish(a *app, args []string) error {
	values, err := a.parse(a.newFlags("lib publish"), args, "NAME", "VERSION")
	if err != nil {
		return err
	}
	version, err := parseVersion(values[1])
	if err != nil {
		return err
	}
	return a.send(http.MethodPost, libPath(values[0], "publish"), map[string]int64{"version": version})
}

func runLibRollback(a *app, args []string) error {
	values, err := a.parse(a.newFlags("lib rollback"), args, "NAME")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	detail, err := fetchLibDetail(c, values[0])
	if err != nil {
		return err
	}
	target, err := rollbackTarget(detail.Library.PublishedVersion, detail.versionNumbers())
	if err != nil {
		return err
	}
	if err := c.call(http.MethodPost, libPath(values[0], "publish"), map[string]int64{"version": target}, nil); err != nil {
		return err
	}
	return a.printJSON(map[string]any{
		"name":              values[0],
		"published_version": target,
		"previous_version":  detail.Library.PublishedVersion,
	})
}

func runLibPull(a *app, args []string) error {
	fs := a.newFlags("lib pull")
	version := fs.Int64("version", 0, "version to download (default: the published one, else the latest)")
	values, err := a.parse(fs, args, "NAME", "DIR")
	if err != nil {
		return err
	}
	dir := values[1]
	if err := ensureEmptyDir(dir); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if *version <= 0 {
		detail, err := fetchLibDetail(c, values[0])
		if err != nil {
			return err
		}
		*version = defaultVersion(detail.Library.PublishedVersion, detail.versionNumbers())
		if *version == 0 {
			return fmt.Errorf("library %s has no versions", values[0])
		}
	}
	pulled, err := libProject.pullSource(c, "", values[0], *version, dir)
	if err != nil {
		return err
	}
	return a.printJSON(pulledOutput(dir, *version, pulled))
}

func runLibDelete(a *app, args []string) error {
	values, err := a.parse(a.newFlags("lib delete"), args, "NAME")
	if err != nil {
		return err
	}
	return a.send(http.MethodDelete, libPath(values[0]), nil)
}
