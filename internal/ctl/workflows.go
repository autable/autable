package ctl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// Workflows: TypeScript projects that autablectl scaffolds, bundles with
// esbuild into dist/workflow.js, packs, and uploads as versions, like apps.
// See docs/design/workflows-v2.md.

type workflowSummary struct {
	ID               int64  `json:"id"`
	DatabaseName     string `json:"database_name"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	PublishedVersion int64  `json:"published_version"`
	LatestVersion    int64  `json:"latest_version"`
	Enabled          bool   `json:"enabled"`
}

type workflowVersionSummary struct {
	Version int64  `json:"version"`
	SDK     string `json:"sdk"`
	Message string `json:"message"`
}

type workflowDetail struct {
	Workflow workflowSummary          `json:"workflow"`
	Versions []workflowVersionSummary `json:"versions"`
}

func (detail workflowDetail) versionNumbers() []int64 {
	numbers := make([]int64, 0, len(detail.Versions))
	for _, version := range detail.Versions {
		numbers = append(numbers, version.Version)
	}
	return numbers
}

// workflowRun is the part of a run the CLI looks at; runs are printed as
// the server returns them.
type workflowRun struct {
	ID     json.Number `json:"id"`
	Status string      `json:"status"`
	Error  string      `json:"error"`
}

func (run workflowRun) finished() bool {
	return run.Status != "" && run.Status != "queued" && run.Status != "running"
}

func workflowPath(database, name string, rest ...string) string {
	return workflowProject.apiPath(database, name, rest...)
}

func runWorkflowInit(a *app, args []string) error {
	return workflowProject.runInit(a, args, writeWorkflowTemplate, func(dir string, manifest projectManifest) []string {
		return []string{
			"cd " + dir,
			"npm install",
			"npm run typecheck",
			"autablectl workflow push --publish -m \"first version\"",
			"autablectl workflow run " + manifest.Database + " " + manifest.Name + " --wait",
		}
	})
}

func runWorkflowSDK(a *app, args []string) error { return workflowProject.runSDK(a, args) }

func runWorkflowPush(a *app, args []string) error {
	result, _, err := workflowProject.runPush(a, args)
	if err != nil {
		return err
	}
	var uploaded struct {
		Workflow workflowSummary        `json:"workflow"`
		Version  workflowVersionSummary `json:"version"`
	}
	if err := json.Unmarshal(result.response, &uploaded); err != nil {
		return fmt.Errorf("unexpected upload response: %w", err)
	}
	return a.printJSON(map[string]any{
		"database":          result.manifest.Database,
		"name":              result.manifest.Name,
		"version":           uploaded.Version.Version,
		"sdk":               uploaded.Version.SDK,
		"published_version": uploaded.Workflow.PublishedVersion,
		"enabled":           uploaded.Workflow.Enabled,
		"bytes":             len(result.archive),
		"dist_files":        result.counts.dist,
		"source_files":      result.counts.source,
		"shared_files":      result.counts.shared,
		"root":              result.counts.root,
	})
}

func runWorkflowList(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow list"), args, "DATABASE")
	if err != nil {
		return err
	}
	return a.get(apiPath("databases", values[0], "workflows"))
}

func runWorkflowVersions(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow versions"), args, "DATABASE", "NAME")
	if err != nil {
		return err
	}
	return a.get(workflowPath(values[0], values[1]))
}

func fetchWorkflowDetail(c *client, database, name string) (workflowDetail, error) {
	var detail workflowDetail
	err := c.call(http.MethodGet, workflowPath(database, name), nil, &detail)
	return detail, err
}

func runWorkflowPublish(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow publish"), args, "DATABASE", "NAME", "VERSION")
	if err != nil {
		return err
	}
	version, err := parseVersion(values[2])
	if err != nil {
		return err
	}
	return a.send(http.MethodPost, workflowPath(values[0], values[1], "publish"), map[string]int64{"version": version})
}

func runWorkflowRollback(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow rollback"), args, "DATABASE", "NAME")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	detail, err := fetchWorkflowDetail(c, values[0], values[1])
	if err != nil {
		return err
	}
	target, err := rollbackTarget(detail.Workflow.PublishedVersion, detail.versionNumbers())
	if err != nil {
		return err
	}
	if err := c.call(http.MethodPost, workflowPath(values[0], values[1], "publish"), map[string]int64{"version": target}, nil); err != nil {
		return err
	}
	return a.printJSON(map[string]any{
		"database":          values[0],
		"name":              values[1],
		"published_version": target,
		"previous_version":  detail.Workflow.PublishedVersion,
	})
}

func runWorkflowPull(a *app, args []string) error {
	fs := a.newFlags("workflow pull")
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
		detail, err := fetchWorkflowDetail(c, values[0], values[1])
		if err != nil {
			return err
		}
		*version = defaultVersion(detail.Workflow.PublishedVersion, detail.versionNumbers())
		if *version == 0 {
			return fmt.Errorf("workflow %s has no versions", values[1])
		}
	}
	pulled, err := workflowProject.pullSource(c, values[0], values[1], *version, dir)
	if err != nil {
		return err
	}
	return a.printJSON(pulledOutput(dir, *version, pulled))
}

func runWorkflowDelete(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow delete"), args, "DATABASE", "NAME")
	if err != nil {
		return err
	}
	return a.send(http.MethodDelete, workflowPath(values[0], values[1]), nil)
}

func runWorkflowRun(a *app, args []string) error {
	fs := a.newFlags("workflow run")
	trigger := fs.String("trigger", "", "manual trigger to start (default: the workflow's only manual trigger)")
	input := fs.String("input", "", "input for the run, as a JSON object (inline, @file, or -)")
	wait := fs.Bool("wait", false, "wait until the run has finished and exit 1 if it failed")
	values, err := a.parse(fs, args, "DATABASE", "NAME")
	if err != nil {
		return err
	}
	body := map[string]any{"input": map[string]any{}}
	if *trigger != "" {
		body["trigger"] = *trigger
	}
	if *input != "" {
		var parsed map[string]any
		if err := a.readJSONArg("input", *input, &parsed); err != nil {
			return err
		}
		body["input"] = parsed
	}
	if *wait {
		body["wait"] = true
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	data, err := c.do(http.MethodPost, workflowPath(values[0], values[1], "runs"), body)
	if err != nil {
		return err
	}
	if !*wait {
		return a.printRaw(data)
	}
	var run workflowRun
	if err := decodeRun(data, &run); err != nil {
		return err
	}
	// The server waits up to a minute; keep polling for longer runs, up to
	// the longest timeout a workflow is likely to have.
	deadline := time.Now().Add(maxRunWait)
	for !run.finished() {
		if run.ID == "" {
			return fmt.Errorf("the server returned a run without an id: %s", data)
		}
		if time.Now().After(deadline) {
			if err := a.printRaw(data); err != nil {
				return err
			}
			return fmt.Errorf("workflow run %s is still %s after %s; follow it with autablectl workflow run-log", run.ID, run.Status, maxRunWait)
		}
		time.Sleep(a.pollInterval())
		data, err = c.do(http.MethodGet, workflowPath(values[0], values[1], "runs", run.ID.String()), nil)
		if err != nil {
			return err
		}
		if err := decodeRun(data, &run); err != nil {
			return err
		}
	}
	if err := a.printRaw(data); err != nil {
		return err
	}
	if run.Status != "succeeded" {
		if run.Error != "" {
			return fmt.Errorf("workflow run %s %s: %s", run.ID, run.Status, run.Error)
		}
		return fmt.Errorf("workflow run %s %s", run.ID, run.Status)
	}
	return nil
}

// decodeRun reads a run, which the server returns either bare or as
// {"run": {...}}.
func decodeRun(data []byte, run *workflowRun) error {
	var wrapped struct {
		Run *workflowRun `json:"run"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&wrapped); err == nil && wrapped.Run != nil {
		*run = *wrapped.Run
		return nil
	}
	decoder = json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(run); err != nil {
		return fmt.Errorf("unexpected run response: %w", err)
	}
	return nil
}

// maxRunWait bounds `workflow run --wait`.
const maxRunWait = 30 * time.Minute

func (a *app) pollInterval() time.Duration {
	if a.pollEvery > 0 {
		return a.pollEvery
	}
	return 2 * time.Second
}

func runWorkflowRuns(a *app, args []string) error {
	fs := a.newFlags("workflow runs")
	status := fs.String("status", "", "only runs with this status: queued, running, succeeded, failed, or cancelled")
	limit := fs.Int("limit", 20, "number of runs (newest first)")
	before := fs.String("before", "", "only runs older than this run id, for paging")
	values, err := a.parse(fs, args, "DATABASE", "NAME")
	if err != nil {
		return err
	}
	query := url.Values{}
	if *status != "" {
		query.Set("status", *status)
	}
	if *limit > 0 {
		query.Set("limit", fmt.Sprint(*limit))
	}
	if *before != "" {
		query.Set("before", *before)
	}
	target := workflowPath(values[0], values[1], "runs")
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return a.get(target)
}

func runWorkflowRunLog(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow run-log"), args, "DATABASE", "NAME", "RUN_ID")
	if err != nil {
		return err
	}
	return a.get(workflowPath(values[0], values[1], "runs", values[2]))
}

// readSecretValue reads a secret from stdin: without echo after a prompt
// when stdin is a terminal, otherwise everything piped in, minus one
// trailing line break. Secrets never come from the command line, where
// shell history and process lists would keep them.
func (a *app) readSecretValue(key string) (string, error) {
	if file, ok := a.stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprintf(a.stderr, "value for secret %s: ", key)
		value, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(a.stderr)
		if err != nil {
			return "", err
		}
		return string(value), nil
	}
	data, err := io.ReadAll(bufio.NewReader(a.stdin))
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(string(data), "\n")
	return strings.TrimSuffix(value, "\r"), nil
}

func runWorkflowSecretSet(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow secret set"), args, "DATABASE", "NAME", "KEY")
	if err != nil {
		return err
	}
	value, err := a.readSecretValue(values[2])
	if err != nil {
		return err
	}
	if value == "" {
		return errors.New("the secret value is empty; pipe it on stdin or type it at the prompt")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if err := c.call(http.MethodPut, workflowPath(values[0], values[1], "secrets", values[2]), map[string]string{"value": value}, nil); err != nil {
		return err
	}
	// The value is never echoed back.
	return a.printJSON(map[string]any{"secret": values[2], "set": true, "length": len(value)})
}

func runWorkflowSecretUnset(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow secret unset"), args, "DATABASE", "NAME", "KEY")
	if err != nil {
		return err
	}
	return a.send(http.MethodDelete, workflowPath(values[0], values[1], "secrets", values[2]), nil)
}

func runWorkflowVarSet(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow var set"), args, "DATABASE", "NAME", "KEY", "VALUE")
	if err != nil {
		return err
	}
	return a.send(http.MethodPut, workflowPath(values[0], values[1], "vars", values[2]), map[string]string{"value": values[3]})
}

func runWorkflowVarUnset(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow var unset"), args, "DATABASE", "NAME", "KEY")
	if err != nil {
		return err
	}
	return a.send(http.MethodDelete, workflowPath(values[0], values[1], "vars", values[2]), nil)
}

func setWorkflowEnabled(enabled bool) func(a *app, args []string) error {
	name := "workflow disable"
	if enabled {
		name = "workflow enable"
	}
	return func(a *app, args []string) error {
		values, err := a.parse(a.newFlags(name), args, "DATABASE", "NAME")
		if err != nil {
			return err
		}
		return a.send(http.MethodPatch, workflowPath(values[0], values[1]), map[string]bool{"enabled": enabled})
	}
}
