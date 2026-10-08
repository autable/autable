package ctl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"autable/internal/scriptpath"
)

// A pulled directory mirrors the repository layout the server writes
// (workflow/<database>/<name>.js, form/<database>/<name>.js) and records in
// .autable/state.json which server object each file came from, the
// object's updated_at at that moment, and the file's hash. The hash tells
// which files were edited locally; updated_at tells whether someone changed
// the object on the server since, in which case push refuses to overwrite.

const (
	kindWorkflow = "workflow"
	kindForm     = "form"
	stateDir     = ".autable"
	stateFile    = "state.json"
)

type syncState struct {
	Server string                `json:"server"`
	Files  map[string]stateEntry `json:"files"`
}

type stateEntry struct {
	Kind      string `json:"kind"`
	ID        int64  `json:"id"`
	Database  string `json:"database"`
	Name      string `json:"name"`
	UpdatedAt int64  `json:"updated_at"`
	SHA256    string `json:"sha256"`
}

func hashScript(script string) string {
	sum := sha256.Sum256([]byte(script))
	return hex.EncodeToString(sum[:])
}

func statePath(dir string) string {
	return filepath.Join(dir, stateDir, stateFile)
}

func loadState(dir string) (syncState, bool, error) {
	data, err := os.ReadFile(statePath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return syncState{Files: map[string]stateEntry{}}, false, nil
	}
	if err != nil {
		return syncState{}, false, err
	}
	var state syncState
	if err := json.Unmarshal(data, &state); err != nil {
		return syncState{}, false, fmt.Errorf("read %s: %w", statePath(dir), err)
	}
	if state.Files == nil {
		state.Files = map[string]stateEntry{}
	}
	return state, true, nil
}

func saveState(dir string, state syncState) error {
	if err := os.MkdirAll(filepath.Join(dir, stateDir), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(dir), append(data, '\n'), 0o644)
}

func readLocal(dir, rel string) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

func writeLocal(dir, rel, script string) error {
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(script), 0o644)
}

func databaseNames(c *client) ([]string, error) {
	var catalog catalogResponse
	if err := c.call(http.MethodGet, "/api/metadata", nil, &catalog); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(catalog.Databases))
	for _, database := range catalog.Databases {
		names = append(names, database.Name)
	}
	return names, nil
}

// checkServer refuses to mix servers in one directory.
func checkServer(state syncState, existed bool, server string) error {
	if existed && state.Server != "" && state.Server != server {
		return fmt.Errorf("this directory was pulled from %s, not %s", state.Server, server)
	}
	return nil
}

type pullResult struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

type remoteEntry struct {
	entry  stateEntry
	script string
}

func runPull(a *app, args []string) error {
	flags := a.newFlags("pull")
	dir := flags.String("dir", ".", "directory to write scripts into")
	force := flags.Bool("force", false, "overwrite local edits")
	var databases multiFlag
	flags.Var(&databases, "database", "only pull this database (repeatable; default all readable)")
	if _, err := a.parse(flags, args); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	state, existed, err := loadState(*dir)
	if err != nil {
		return err
	}
	if err := checkServer(state, existed, c.server); err != nil {
		return err
	}
	state.Server = c.server
	if len(databases) == 0 {
		names, err := databaseNames(c)
		if err != nil {
			return err
		}
		databases = names
	}
	pulled := map[string]bool{}
	for _, database := range databases {
		pulled[database] = true
	}

	remote := map[string]remoteEntry{}
	results := []pullResult{}
	add := func(kind string, script remoteScript) {
		rel := scriptpath.Relative(kind, script.DatabaseName, script.Name)
		if previous, taken := remote[rel]; taken {
			results = append(results, pullResult{Path: rel, Action: "skipped", Detail: fmt.Sprintf("%s %q maps to the same file as %q; rename one on the server", kind, script.Name, previous.entry.Name)})
			return
		}
		remote[rel] = remoteEntry{
			entry:  stateEntry{Kind: kind, ID: script.ID, Database: script.DatabaseName, Name: script.Name, UpdatedAt: script.UpdatedAt, SHA256: hashScript(script.Script)},
			script: script.Script,
		}
	}
	for _, database := range databases {
		workflows, err := listWorkflows(c, database)
		if err != nil {
			return fmt.Errorf("list workflows of %s: %w", database, err)
		}
		for _, workflow := range workflows {
			add(kindWorkflow, workflow.remoteScript)
		}
		forms, err := listForms(c, database)
		if err != nil {
			return fmt.Errorf("list forms of %s: %w", database, err)
		}
		for _, form := range forms {
			add(kindForm, form.remoteScript)
		}
	}

	paths := make([]string, 0, len(remote))
	for rel := range remote {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		item := remote[rel]
		local, exists, err := readLocal(*dir, rel)
		if err != nil {
			return err
		}
		previous, tracked := state.Files[rel]
		localHash := hashScript(local)
		switch {
		case exists && localHash == item.entry.SHA256:
			results = append(results, pullResult{Path: rel, Action: "unchanged"})
		case exists && !*force && (!tracked || localHash != previous.SHA256):
			results = append(results, pullResult{Path: rel, Action: "kept", Detail: "local file has unpushed edits; push them or pull --force"})
			continue
		default:
			if err := writeLocal(*dir, rel, item.script); err != nil {
				return err
			}
			results = append(results, pullResult{Path: rel, Action: "written"})
		}
		state.Files[rel] = item.entry
	}
	// Objects that disappeared from the server: drop their unedited files.
	for rel, entry := range state.Files {
		if _, ok := remote[rel]; ok || !pulled[entry.Database] {
			continue
		}
		local, exists, err := readLocal(*dir, rel)
		if err != nil {
			return err
		}
		if exists && hashScript(local) != entry.SHA256 && !*force {
			// Stays tracked so push reports the missing object instead of
			// silently recreating it.
			results = append(results, pullResult{Path: rel, Action: "kept", Detail: "deleted on the server but edited locally"})
			continue
		}
		if exists {
			if err := os.Remove(filepath.Join(*dir, filepath.FromSlash(rel))); err != nil {
				return err
			}
		}
		results = append(results, pullResult{Path: rel, Action: "removed", Detail: "deleted on the server"})
		delete(state.Files, rel)
	}
	if err := saveState(*dir, state); err != nil {
		return err
	}
	return a.printJSON(results)
}

type change struct {
	Path     string `json:"path"`
	Status   string `json:"status"`
	Kind     string `json:"kind"`
	Database string `json:"database"`
	Name     string `json:"name"`
	ID       int64  `json:"id,omitempty"`
	Detail   string `json:"detail,omitempty"`
	script   string
	entry    stateEntry
}

// localChanges compares the directory with the recorded state. New files
// resolve their database from the directory name against the server's
// databases, and their name from the file name.
func localChanges(dir string, state syncState, databases []string) ([]change, error) {
	changes := []change{}
	seen := map[string]bool{}
	for _, kind := range []string{kindWorkflow, kindForm} {
		root := filepath.Join(dir, kind)
		err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) && file == root {
				return filepath.SkipDir
			}
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(file) != ".js" {
				return nil
			}
			relative, err := filepath.Rel(dir, file)
			if err != nil {
				return err
			}
			rel := filepath.ToSlash(relative)
			seen[rel] = true
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			script := string(data)
			if tracked, ok := state.Files[rel]; ok {
				if hashScript(script) != tracked.SHA256 {
					changes = append(changes, change{Path: rel, Status: "modified", Kind: tracked.Kind, Database: tracked.Database, Name: tracked.Name, ID: tracked.ID, script: script, entry: tracked})
				}
				return nil
			}
			changes = append(changes, newFileChange(rel, kind, script, databases))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for rel, tracked := range state.Files {
		if !seen[rel] {
			changes = append(changes, change{Path: rel, Status: "deleted", Kind: tracked.Kind, Database: tracked.Database, Name: tracked.Name, ID: tracked.ID, Detail: "push does not delete; delete it on the server if intended"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

func newFileChange(rel, kind, script string, databases []string) change {
	result := change{Path: rel, Status: "new", Kind: kind, script: script}
	parts := strings.Split(rel, "/")
	if len(parts) != 3 {
		result.Status, result.Detail = "invalid", "expected "+kind+"/<database>/<name>.js"
		return result
	}
	for _, database := range databases {
		if scriptpath.Segment(database) == parts[1] {
			result.Database = database
		}
	}
	if result.Database == "" {
		result.Status, result.Detail = "invalid", fmt.Sprintf("no readable database matches directory %q", parts[1])
		return result
	}
	result.Name = strings.TrimSuffix(parts[2], path.Ext(parts[2]))
	if scriptpath.Relative(kind, result.Database, result.Name) != rel {
		result.Status, result.Detail = "invalid", "file name contains characters a "+kind+" name maps differently; rename the file"
	}
	return result
}

func (a *app) changes(dir string) (*client, syncState, []change, error) {
	c, err := a.client()
	if err != nil {
		return nil, syncState{}, nil, err
	}
	state, existed, err := loadState(dir)
	if err != nil {
		return nil, syncState{}, nil, err
	}
	if !existed {
		return nil, syncState{}, nil, fmt.Errorf("%s has no %s; run autablectl pull first", dir, filepath.ToSlash(filepath.Join(stateDir, stateFile)))
	}
	if err := checkServer(state, existed, c.server); err != nil {
		return nil, syncState{}, nil, err
	}
	databases, err := databaseNames(c)
	if err != nil {
		return nil, syncState{}, nil, err
	}
	changes, err := localChanges(dir, state, databases)
	return c, state, changes, err
}

func runStatus(a *app, args []string) error {
	flags := a.newFlags("status")
	dir := flags.String("dir", ".", "pulled directory")
	if _, err := a.parse(flags, args); err != nil {
		return err
	}
	_, _, changes, err := a.changes(*dir)
	if err != nil {
		return err
	}
	return a.printJSON(changes)
}

func runPush(a *app, args []string) error {
	flags := a.newFlags("push")
	dir := flags.String("dir", ".", "pulled directory")
	dryRun := flags.Bool("dry-run", false, "only show what would be saved")
	force := flags.Bool("force", false, "overwrite objects changed on the server since the last pull")
	if _, err := a.parse(flags, args); err != nil {
		return err
	}
	c, state, changes, err := a.changes(*dir)
	if err != nil {
		return err
	}
	results := make([]change, 0, len(changes))
	failed := false
	for _, item := range changes {
		if item.Status != "modified" && item.Status != "new" {
			results = append(results, item)
			if item.Status == "invalid" {
				failed = true
			}
			continue
		}
		saved, err := pushChange(c, item, *dryRun, *force)
		if err != nil {
			item.Status, item.Detail = "failed", err.Error()
			results = append(results, item)
			failed = true
			continue
		}
		if *dryRun {
			item.Status = "would-" + map[string]string{"modified": "update", "new": "create"}[item.Status]
		} else {
			item.Status = map[string]string{"modified": "updated", "new": "created"}[item.Status]
			item.ID = saved.ID
			state.Files[item.Path] = stateEntry{Kind: item.Kind, ID: saved.ID, Database: saved.DatabaseName, Name: saved.Name, UpdatedAt: saved.UpdatedAt, SHA256: hashScript(item.script)}
		}
		results = append(results, item)
	}
	if !*dryRun {
		if err := saveState(*dir, state); err != nil {
			return err
		}
	}
	if err := a.printJSON(results); err != nil {
		return err
	}
	if failed {
		return errors.New("some scripts were not pushed; see detail")
	}
	return nil
}

var errConflict = errors.New("changed on the server since the last pull; pull (your edits are kept) and merge, or push --force")

// pushChange saves one script. Updates send the server's current settings
// back unchanged with only the script replaced.
func pushChange(c *client, item change, dryRun, force bool) (remoteScript, error) {
	var saved remoteScript
	switch item.Kind {
	case kindWorkflow:
		var current remoteWorkflow
		if item.Status == "modified" {
			if err := c.call(http.MethodGet, apiPath("workflows", fmt.Sprint(item.ID)), nil, &current); err != nil {
				return saved, err
			}
			if current.UpdatedAt != item.entry.UpdatedAt && !force {
				return saved, errConflict
			}
		} else {
			if _, err := findWorkflow(c, item.Database, item.Name); err == nil {
				return saved, errors.New("a workflow with this name already exists on the server; run pull")
			}
			current = remoteWorkflow{remoteScript: remoteScript{DatabaseName: item.Database, Name: item.Name}}
		}
		current.Script = item.script
		if dryRun {
			return current.remoteScript, nil
		}
		err := c.call(http.MethodPost, "/api/workflows", current.saveRequest(), &saved)
		return saved, err
	case kindForm:
		var current remoteForm
		if item.Status == "modified" {
			if err := c.call(http.MethodGet, apiPath("forms", fmt.Sprint(item.ID)), nil, &current); err != nil {
				return saved, err
			}
			if current.UpdatedAt != item.entry.UpdatedAt && !force {
				return saved, errConflict
			}
		} else {
			if _, err := findForm(c, item.Database, item.Name); err == nil {
				return saved, errors.New("a form with this name already exists on the server; run pull")
			}
			current = remoteForm{remoteScript{DatabaseName: item.Database, Name: item.Name}}
		}
		current.Script = item.script
		if dryRun {
			return current.remoteScript, nil
		}
		err := c.call(http.MethodPost, "/api/forms", current.saveRequest(), &saved)
		return saved, err
	}
	return saved, fmt.Errorf("unknown kind %q", item.Kind)
}
