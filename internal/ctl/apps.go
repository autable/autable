package ctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// Hosted apps: ordinary frontend projects that autablectl scaffolds, builds,
// packs, and uploads. See docs/design/apps.md. What apps share with
// workflows lives in project.go.

type appManifest = projectManifest

type appSummary struct {
	ID               int64  `json:"id"`
	DatabaseName     string `json:"database_name"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	PublishedVersion int64  `json:"published_version"`
	URL              string `json:"url"`
}

type appVersionSummary struct {
	Version    int64  `json:"version"`
	SDK        string `json:"sdk"`
	Message    string `json:"message"`
	Supported  bool   `json:"supported"`
	SDKError   string `json:"sdk_error,omitempty"`
	PreviewURL string `json:"preview_url"`
}

type appDetail struct {
	App      appSummary          `json:"app"`
	Versions []appVersionSummary `json:"versions"`
}

func (detail appDetail) versionNumbers() []int64 {
	numbers := make([]int64, 0, len(detail.Versions))
	for _, version := range detail.Versions {
		numbers = append(numbers, version.Version)
	}
	return numbers
}

func readAppManifest(dir string) (appManifest, error) { return appProject.readManifest(dir) }

func writeAppManifest(dir string, manifest appManifest) error {
	return appProject.writeManifest(dir, manifest)
}

func packApp(dir string) ([]byte, packCounts, error) { return appProject.pack(dir) }

func runAppInit(a *app, args []string) error {
	return appProject.runInit(a, args, writeAppTemplate, func(dir string, _ projectManifest) []string {
		return []string{"cd " + dir, "npm install", "autablectl app dev", "autablectl app push -m \"first version\""}
	})
}

func runAppSDK(a *app, args []string) error { return appProject.runSDK(a, args) }

func runAppDev(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app dev"), args, "DIR?")
	if err != nil {
		return err
	}
	dir := optionalDir(values)
	if _, err := readAppManifest(dir); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s has no node_modules; run npm install first", dir)
	}
	fmt.Fprintf(a.stderr, "proxying /api to %s with your autablectl login\n", c.server)
	return a.npm(dir, []string{"AUTABLE_SERVER=" + c.server, "AUTABLE_TOKEN=" + c.token}, a.stdout, "run", "dev")
}

func runAppPush(a *app, args []string) error {
	result, c, err := appProject.runPush(a, args)
	if err != nil {
		return err
	}
	var uploaded struct {
		App     appSummary        `json:"app"`
		Version appVersionSummary `json:"version"`
	}
	if err := json.Unmarshal(result.response, &uploaded); err != nil {
		return err
	}
	return a.printJSON(map[string]any{
		"database":          uploaded.App.DatabaseName,
		"name":              uploaded.App.Name,
		"version":           uploaded.Version.Version,
		"sdk":               uploaded.Version.SDK,
		"published_version": uploaded.App.PublishedVersion,
		"preview_url":       c.server + uploaded.Version.PreviewURL,
		"url":               c.server + uploaded.App.URL,
		"bytes":             len(result.archive),
		"dist_files":        result.counts.dist,
		"source_files":      result.counts.source,
		"shared_files":      result.counts.shared,
		"root":              result.counts.root,
	})
}

func runAppList(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app list"), args, "DATABASE")
	if err != nil {
		return err
	}
	return a.get(apiPath("databases", values[0], "apps"))
}

func runAppVersions(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app versions"), args, "DATABASE", "NAME")
	if err != nil {
		return err
	}
	return a.get(appProject.apiPath(values[0], values[1]))
}

func runAppPublish(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app publish"), args, "DATABASE", "NAME", "VERSION")
	if err != nil {
		return err
	}
	version, err := parseVersion(values[2])
	if err != nil {
		return err
	}
	return a.send(http.MethodPost, appProject.apiPath(values[0], values[1], "publish"), map[string]int64{"version": version})
}

func runAppRollback(a *app, args []string) error {
	values, err := a.parse(a.newFlags("app rollback"), args, "DATABASE", "NAME")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	var detail appDetail
	if err := c.call(http.MethodGet, appProject.apiPath(values[0], values[1]), nil, &detail); err != nil {
		return err
	}
	target, err := rollbackTarget(detail.App.PublishedVersion, detail.versionNumbers())
	if err != nil {
		return err
	}
	var published appSummary
	if err := c.call(http.MethodPost, appProject.apiPath(values[0], values[1], "publish"), map[string]int64{"version": target}, &published); err != nil {
		return err
	}
	return a.printJSON(map[string]any{
		"database":          published.DatabaseName,
		"name":              published.Name,
		"published_version": published.PublishedVersion,
		"previous_version":  detail.App.PublishedVersion,
		"url":               c.server + published.URL,
	})
}

func runAppPull(a *app, args []string) error {
	fs := a.newFlags("app pull")
	version := fs.Int64("version", 0, "version to download (default: the published one, else the latest)")
	values, err := a.parse(fs, args, "DATABASE", "NAME", "DIR")
	if err != nil {
		return err
	}
	dir := values[2]
	if err := ensureEmptyDir(dir); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if *version <= 0 {
		var detail appDetail
		if err := c.call(http.MethodGet, appProject.apiPath(values[0], values[1]), nil, &detail); err != nil {
			return err
		}
		*version = defaultVersion(detail.App.PublishedVersion, detail.versionNumbers())
		if *version == 0 {
			return fmt.Errorf("app %s has no versions", values[1])
		}
	}
	pulled, err := appProject.pullSource(c, values[0], values[1], *version, dir)
	if err != nil {
		return err
	}
	return a.printJSON(pulledOutput(dir, *version, pulled))
}
