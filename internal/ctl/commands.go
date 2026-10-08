package ctl

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var commands map[string]command

func init() {
	commands = map[string]command{
		"login":            {"login --server URL [--token TOKEN] [--no-browser]", "sign in through the browser (or store a token)", runLogin},
		"logout":           {"logout", "revoke the stored token and forget it", runLogout},
		"whoami":           {"whoami", "show the signed-in user", runWhoami},
		"api":              {"api METHOD PATH [--data JSON]", "call any API endpoint, e.g. api GET /api/metadata", runAPI},
		"meta":             {"meta [DATABASE [TABLE]]", "show databases, tables, fields, and views", runMeta},
		"nodes":            {"nodes", "list workflow node types with their params", runNodes},
		"fields add":       {"fields add DATABASE TABLE --fields JSON", "add fields to a table", runFieldsAdd},
		"rows list":        {"rows list DATABASE TABLE [--query JSON] [--sorts JSON] [--view V] [--search S] [--limit N] [--offset N]", "query rows; prints {rows, total}", runRowsList},
		"rows get":         {"rows get DATABASE TABLE RECORD_ID", "fetch one row", runRowsGet},
		"rows create":      {"rows create DATABASE TABLE --values JSON", "create a row", runRowsCreate},
		"rows update":      {"rows update DATABASE TABLE RECORD_ID --values JSON", "update fields of a row", runRowsUpdate},
		"rows upsert":      {"rows upsert DATABASE TABLE --match FIELD --values JSON", "update the row whose FIELD matches, else create", runRowsUpsert},
		"rows delete":      {"rows delete DATABASE TABLE RECORD_ID", "delete a row", runRowsDelete},
		"rows history":     {"rows history DATABASE TABLE RECORD_ID", "show a row's change history", runRowsHistory},
		"workflow list":    {"workflow list DATABASE", "list workflows", runWorkflowList},
		"workflow get":     {"workflow get DATABASE WORKFLOW [--script]", "show a workflow (secret values are never shown)", runWorkflowGet},
		"workflow set":     {"workflow set DATABASE WORKFLOW [--enabled BOOL] [--var K=V]... [--unset-var K]... [--timeout SECONDS]", "change a workflow's settings", runWorkflowSet},
		"workflow run":     {"workflow run DATABASE WORKFLOW [--inputs JSON]", "run a workflow now and print the run", runWorkflowRun},
		"workflow runs":    {"workflow runs DATABASE WORKFLOW [--limit N]", "list recent runs (newest first)", runWorkflowRuns},
		"workflow run-log": {"workflow run-log DATABASE WORKFLOW HISTORY_KEY", "show the steps, inputs, and outputs of one run", runWorkflowRunLog},
		"form list":        {"form list DATABASE", "list forms", runFormList},
		"form get":         {"form get DATABASE FORM [--script]", "show a form", runFormGet},
		"file upload":      {"file upload PATH [--database DB --table TABLE [--record ID]]", "upload a file; prints its id for file fields", runFileUpload},
		"file download":    {"file download FILE_ID --output PATH", "download a stored file", runFileDownload},
		"pull":             {"pull [--dir DIR] [--database DB]... [--force]", "write workflow and form scripts into DIR", runPull},
		"status":           {"status [--dir DIR]", "show scripts changed locally since the last pull/push", runStatus},
		"push":             {"push [--dir DIR] [--dry-run] [--force]", "save changed and new scripts to the server", runPush},
		"skill show":       {"skill show", "print the agent skill (rules plus a map of the source)", runSkillShow},
		"skill install":    {"skill install [--dir DIR]", "write the agent skill to DIR/SKILL.md (default ~/.claude/skills/autable)", runSkillInstall},
	}
}

func (a *app) newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {}
	return fs
}

// parse wraps parseFlags and prints a command's flags for -h.
func (a *app) parse(fs *flag.FlagSet, args []string, positional ...string) ([]string, error) {
	values, err := parseFlags(fs, args, positional...)
	if errors.Is(err, flag.ErrHelp) {
		cmd := commands[fs.Name()]
		fmt.Fprintf(a.stdout, "usage: autablectl %s\n\n%s\n", cmd.usage, cmd.summary)
		fs.SetOutput(a.stdout)
		fs.PrintDefaults()
	}
	return values, err
}

func runLogin(a *app, args []string) error {
	fs := a.newFlags("login")
	server := fs.String("server", a.getenv("AUTABLE_SERVER"), "autable server URL, e.g. https://autable.example.com")
	token := fs.String("token", "", "store this session token instead of signing in through the browser")
	noBrowser := fs.Bool("no-browser", false, "print the authorization URL without opening a browser")
	if _, err := a.parse(fs, args); err != nil {
		return err
	}
	if *server == "" {
		if creds, ok, err := loadCredentials(a.configPath); err == nil && ok {
			*server = creds.Server
		}
	}
	base, err := normalizeServer(*server)
	if err != nil {
		return usagef("%v", err)
	}
	creds := credentials{Server: base}
	if *token != "" {
		authed := &client{server: base, token: *token, http: a.httpClient}
		var user userInfo
		if err := authed.call(http.MethodGet, "/api/auth/me", nil, &user); err != nil {
			return fmt.Errorf("token rejected: %w", err)
		}
		creds.Token, creds.Email = *token, user.Email
	} else {
		issued, err := a.browserLogin(a.context(), base, !*noBrowser)
		if err != nil {
			return err
		}
		creds.Token, creds.Email, creds.ExpiresAt = issued.Token, issued.User.Email, issued.ExpiresAt
	}
	if err := saveCredentials(a.configPath, creds); err != nil {
		return err
	}
	return a.printJSON(map[string]any{"server": creds.Server, "email": creds.Email, "expires_at": creds.ExpiresAt, "config": a.configPath})
}

func runLogout(a *app, args []string) error {
	if _, err := a.parse(a.newFlags("logout"), args); err != nil {
		return err
	}
	c, err := a.client()
	if err == nil {
		if err := c.call(http.MethodPost, "/api/auth/logout", nil, nil); err != nil {
			fmt.Fprintln(a.stderr, "warning: server did not revoke the token:", err)
		}
	}
	if err := removeCredentials(a.configPath); err != nil {
		return err
	}
	return a.printJSON(map[string]bool{"ok": true})
}

func runWhoami(a *app, args []string) error {
	if _, err := a.parse(a.newFlags("whoami"), args); err != nil {
		return err
	}
	return a.get("/api/auth/me")
}

// get prints the response of a GET request.
func (a *app) get(path string) error {
	c, err := a.client()
	if err != nil {
		return err
	}
	data, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return a.printRaw(data)
}

// send prints the response of a request with a JSON body.
func (a *app) send(method, path string, body any) error {
	c, err := a.client()
	if err != nil {
		return err
	}
	data, err := c.do(method, path, body)
	if err != nil {
		return err
	}
	return a.printRaw(data)
}

func runAPI(a *app, args []string) error {
	fs := a.newFlags("api")
	data := fs.String("data", "", "JSON request body (inline, @file, or -)")
	values, err := a.parse(fs, args, "METHOD", "PATH")
	if err != nil {
		return err
	}
	method, path := strings.ToUpper(values[0]), values[1]
	if !strings.HasPrefix(path, "/") {
		return usagef("PATH must start with /, e.g. /api/metadata")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	var body io.Reader
	if *data != "" {
		raw, err := a.readArg(*data)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, c.server+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.send(request)
	if err != nil {
		return err
	}
	return a.printRaw(response)
}

type catalogResponse struct {
	Databases []struct {
		Name   string `json:"name"`
		Tables []struct {
			Name string `json:"name"`
		} `json:"tables"`
	} `json:"databases"`
}

func runMeta(a *app, args []string) error {
	values, err := a.parse(a.newFlags("meta"), args, "DATABASE?", "TABLE?")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	var catalog struct {
		Databases []map[string]any `json:"databases"`
	}
	if err := c.call(http.MethodGet, "/api/metadata", nil, &catalog); err != nil {
		return err
	}
	if len(values) == 0 {
		return a.printJSON(catalog)
	}
	for _, database := range catalog.Databases {
		if database["name"] != values[0] {
			continue
		}
		if len(values) == 1 {
			return a.printJSON(database)
		}
		tables, _ := database["tables"].([]any)
		for _, rawTable := range tables {
			if table, ok := rawTable.(map[string]any); ok && table["name"] == values[1] {
				return a.printJSON(table)
			}
		}
		return fmt.Errorf("table %q not found in database %q (or not readable)", values[1], values[0])
	}
	return fmt.Errorf("database %q not found (or not readable)", values[0])
}

func runNodes(a *app, args []string) error {
	if _, err := a.parse(a.newFlags("nodes"), args); err != nil {
		return err
	}
	return a.get("/api/workflow/nodes")
}

func runFieldsAdd(a *app, args []string) error {
	fs := a.newFlags("fields add")
	fieldsArg := fs.String("fields", "", `fields to add: [{"name":"数量","type":"int"}], or {"name":"type"}`)
	values, err := a.parse(fs, args, "DATABASE", "TABLE")
	if err != nil {
		return err
	}
	if *fieldsArg == "" {
		return usagef("--fields is required")
	}
	var fields any
	if err := a.readJSONArg("fields", *fieldsArg, &fields); err != nil {
		return err
	}
	return a.send(http.MethodPost, apiPath("tables", values[0], values[1], "fields"), map[string]any{"fields": fields})
}

func runRowsList(a *app, args []string) error {
	fs := a.newFlags("rows list")
	query := fs.String("query", "", `filter, e.g. {"field":"状态","operator":"=","value":"open"} or {"combinator":"and","rules":[...]}`)
	sorts := fs.String("sorts", "", `sort order, e.g. [{"field":"ct_record_id","direction":"desc"}]`)
	view := fs.String("view", "", "apply a saved view")
	search := fs.String("search", "", "full-text search across fields")
	limit := fs.Int("limit", 100, "maximum rows to return")
	offset := fs.Int("offset", 0, "rows to skip")
	values, err := a.parse(fs, args, "DATABASE", "TABLE")
	if err != nil {
		return err
	}
	body := map[string]any{"limit": *limit, "offset": *offset}
	if *view != "" {
		body["view"] = *view
	}
	if *search != "" {
		body["search"] = *search
	}
	if *query != "" {
		var parsed map[string]any
		if err := a.readJSONArg("query", *query, &parsed); err != nil {
			return err
		}
		if _, single := parsed["field"]; single {
			parsed = map[string]any{"combinator": "and", "rules": []any{parsed}}
		}
		body["query"] = parsed
	}
	if *sorts != "" {
		var parsed any
		if err := a.readJSONArg("sorts", *sorts, &parsed); err != nil {
			return err
		}
		body["sorts"] = parsed
	}
	return a.send(http.MethodPost, apiPath("tables", values[0], values[1], "rows", "page"), body)
}

func parseRecordID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, usagef("RECORD_ID must be a positive number, got %q", value)
	}
	return id, nil
}

func runRowsGet(a *app, args []string) error {
	values, err := a.parse(a.newFlags("rows get"), args, "DATABASE", "TABLE", "RECORD_ID")
	if err != nil {
		return err
	}
	id, err := parseRecordID(values[2])
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	var rows []map[string]any
	err = c.call(http.MethodPost, apiPath("tables", values[0], values[1], "rows", "query"), map[string]any{
		"query": map[string]any{"combinator": "and", "rules": []any{
			map[string]any{"field": "ct_record_id", "operator": "=", "value": id},
		}},
		"limit": 1,
	}, &rows)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("record %d not found in %s/%s", id, values[0], values[1])
	}
	return a.printJSON(rows[0])
}

func (a *app) valuesFlag(fs *flag.FlagSet) *string {
	return fs.String("values", "", `field values, e.g. {"名称":"Widget","数量":3}`)
}

func (a *app) readValues(raw string) (map[string]any, error) {
	if raw == "" {
		return nil, usagef("--values is required")
	}
	var values map[string]any
	if err := a.readJSONArg("values", raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func runRowsCreate(a *app, args []string) error {
	fs := a.newFlags("rows create")
	rawValues := a.valuesFlag(fs)
	values, err := a.parse(fs, args, "DATABASE", "TABLE")
	if err != nil {
		return err
	}
	fields, err := a.readValues(*rawValues)
	if err != nil {
		return err
	}
	return a.send(http.MethodPost, apiPath("tables", values[0], values[1], "rows"), map[string]any{"values": fields})
}

func runRowsUpdate(a *app, args []string) error {
	fs := a.newFlags("rows update")
	rawValues := a.valuesFlag(fs)
	values, err := a.parse(fs, args, "DATABASE", "TABLE", "RECORD_ID")
	if err != nil {
		return err
	}
	if _, err := parseRecordID(values[2]); err != nil {
		return err
	}
	fields, err := a.readValues(*rawValues)
	if err != nil {
		return err
	}
	return a.send(http.MethodPatch, apiPath("tables", values[0], values[1], "rows", values[2]), map[string]any{"values": fields})
}

func runRowsUpsert(a *app, args []string) error {
	fs := a.newFlags("rows upsert")
	rawValues := a.valuesFlag(fs)
	match := fs.String("match", "", "field whose value identifies the row")
	values, err := a.parse(fs, args, "DATABASE", "TABLE")
	if err != nil {
		return err
	}
	if *match == "" {
		return usagef("--match is required")
	}
	fields, err := a.readValues(*rawValues)
	if err != nil {
		return err
	}
	return a.send(http.MethodPost, apiPath("tables", values[0], values[1], "rows", "upsert"), map[string]any{"match_field": *match, "values": fields})
}

func runRowsDelete(a *app, args []string) error {
	values, err := a.parse(a.newFlags("rows delete"), args, "DATABASE", "TABLE", "RECORD_ID")
	if err != nil {
		return err
	}
	if _, err := parseRecordID(values[2]); err != nil {
		return err
	}
	return a.send(http.MethodDelete, apiPath("tables", values[0], values[1], "rows", values[2]), nil)
}

func runRowsHistory(a *app, args []string) error {
	values, err := a.parse(a.newFlags("rows history"), args, "DATABASE", "TABLE", "RECORD_ID")
	if err != nil {
		return err
	}
	if _, err := parseRecordID(values[2]); err != nil {
		return err
	}
	return a.get(apiPath("tables", values[0], values[1], "rows", values[2], "history"))
}

// remoteScript is the part of a workflow or form response the CLI needs.
// Workflow settings stay raw so saving sends back exactly what was read.
type remoteScript struct {
	ID           int64  `json:"id"`
	DatabaseName string `json:"database_name"`
	Name         string `json:"name"`
	Script       string `json:"script"`
	UpdatedAt    int64  `json:"updated_at"`
}

type remoteWorkflow struct {
	remoteScript
	Enabled              bool              `json:"enabled"`
	Variables            map[string]string `json:"variables"`
	Runners              map[string]string `json:"runners"`
	HistoryRetentionDays *int64            `json:"history_retention_days"`
	TimeoutSeconds       *int64            `json:"timeout_seconds"`
}

type remoteForm struct {
	remoteScript
}

// saveRequest builds a full workflow definition for POST /api/workflows.
// Secrets are sent empty: the server merges them into the stored ones, so
// saving never touches secret values.
func (workflow remoteWorkflow) saveRequest() map[string]any {
	variables := workflow.Variables
	if variables == nil {
		variables = map[string]string{}
	}
	runners := workflow.Runners
	if runners == nil {
		runners = map[string]string{}
	}
	return map[string]any{
		"id":                     workflow.ID,
		"database_name":          workflow.DatabaseName,
		"name":                   workflow.Name,
		"script":                 workflow.Script,
		"enabled":                workflow.Enabled,
		"secrets":                map[string]string{},
		"variables":              variables,
		"runners":                runners,
		"history_retention_days": workflow.HistoryRetentionDays,
		"timeout_seconds":        workflow.TimeoutSeconds,
	}
}

func (form remoteForm) saveRequest() map[string]any {
	return map[string]any{
		"id":            form.ID,
		"database_name": form.DatabaseName,
		"name":          form.Name,
		"script":        form.Script,
	}
}

func listWorkflows(c *client, database string) ([]remoteWorkflow, error) {
	var workflows []remoteWorkflow
	err := c.call(http.MethodGet, apiPath("databases", database, "workflows"), nil, &workflows)
	return workflows, err
}

func listForms(c *client, database string) ([]remoteForm, error) {
	var forms []remoteForm
	err := c.call(http.MethodGet, apiPath("databases", database, "forms"), nil, &forms)
	return forms, err
}

// findWorkflow resolves a workflow by name, or by id when the reference is
// a number no workflow is named.
func findWorkflow(c *client, database, ref string) (remoteWorkflow, error) {
	workflows, err := listWorkflows(c, database)
	if err != nil {
		return remoteWorkflow{}, err
	}
	for _, workflow := range workflows {
		if workflow.Name == ref {
			return workflow, nil
		}
	}
	for _, workflow := range workflows {
		if strconv.FormatInt(workflow.ID, 10) == ref {
			return workflow, nil
		}
	}
	return remoteWorkflow{}, fmt.Errorf("workflow %q not found in database %q (or not readable)", ref, database)
}

func findForm(c *client, database, ref string) (remoteForm, error) {
	forms, err := listForms(c, database)
	if err != nil {
		return remoteForm{}, err
	}
	for _, form := range forms {
		if form.Name == ref {
			return form, nil
		}
	}
	for _, form := range forms {
		if strconv.FormatInt(form.ID, 10) == ref {
			return form, nil
		}
	}
	return remoteForm{}, fmt.Errorf("form %q not found in database %q (or not readable)", ref, database)
}

func runWorkflowList(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow list"), args, "DATABASE")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	workflows, err := listWorkflows(c, values[0])
	if err != nil {
		return err
	}
	summaries := make([]map[string]any, 0, len(workflows))
	for _, workflow := range workflows {
		summaries = append(summaries, map[string]any{
			"id":         workflow.ID,
			"name":       workflow.Name,
			"enabled":    workflow.Enabled,
			"updated_at": workflow.UpdatedAt,
		})
	}
	return a.printJSON(summaries)
}

func runWorkflowGet(a *app, args []string) error {
	fs := a.newFlags("workflow get")
	scriptOnly := fs.Bool("script", false, "print only the script source")
	values, err := a.parse(fs, args, "DATABASE", "WORKFLOW")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	workflow, err := findWorkflow(c, values[0], values[1])
	if err != nil {
		return err
	}
	if *scriptOnly {
		_, err := io.WriteString(a.stdout, workflow.Script)
		return err
	}
	return a.get(apiPath("workflows", strconv.FormatInt(workflow.ID, 10)))
}

type multiFlag []string

func (values *multiFlag) String() string     { return strings.Join(*values, ",") }
func (values *multiFlag) Set(v string) error { *values = append(*values, v); return nil }

func runWorkflowSet(a *app, args []string) error {
	fs := a.newFlags("workflow set")
	enabled := fs.String("enabled", "", "true or false")
	timeout := fs.Int64("timeout", 0, "run timeout in seconds (0 keeps the current value)")
	var setVars, unsetVars multiFlag
	fs.Var(&setVars, "var", "set a variable, KEY=VALUE (repeatable)")
	fs.Var(&unsetVars, "unset-var", "remove a variable (repeatable)")
	values, err := a.parse(fs, args, "DATABASE", "WORKFLOW")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	workflow, err := findWorkflow(c, values[0], values[1])
	if err != nil {
		return err
	}
	if *enabled != "" {
		parsed, err := strconv.ParseBool(*enabled)
		if err != nil {
			return usagef("--enabled must be true or false")
		}
		workflow.Enabled = parsed
	}
	if *timeout > 0 {
		workflow.TimeoutSeconds = timeout
	}
	if workflow.Variables == nil {
		workflow.Variables = map[string]string{}
	}
	for _, pair := range setVars {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return usagef("--var must be KEY=VALUE, got %q", pair)
		}
		workflow.Variables[key] = value
	}
	for _, key := range unsetVars {
		delete(workflow.Variables, key)
	}
	return a.send(http.MethodPost, "/api/workflows", workflow.saveRequest())
}

func runWorkflowRun(a *app, args []string) error {
	fs := a.newFlags("workflow run")
	inputs := fs.String("inputs", "", "inputs passed to the run, as a JSON object")
	values, err := a.parse(fs, args, "DATABASE", "WORKFLOW")
	if err != nil {
		return err
	}
	body := map[string]any{"inputs": map[string]any{}}
	if *inputs != "" {
		var parsed map[string]any
		if err := a.readJSONArg("inputs", *inputs, &parsed); err != nil {
			return err
		}
		body["inputs"] = parsed
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	workflow, err := findWorkflow(c, values[0], values[1])
	if err != nil {
		return err
	}
	data, err := c.do(http.MethodPost, apiPath("workflows", strconv.FormatInt(workflow.ID, 10), "runs"), body)
	var failed *apiError
	if errors.As(err, &failed) && failed.Status == http.StatusBadRequest && len(failed.Body) > 0 {
		// A failed run still returns the run record with its error.
		if printErr := a.printRaw(failed.Body); printErr != nil {
			return printErr
		}
		return errors.New("workflow run failed")
	}
	if err != nil {
		return err
	}
	return a.printRaw(data)
}

func runWorkflowRuns(a *app, args []string) error {
	fs := a.newFlags("workflow runs")
	limit := fs.Int("limit", 20, "number of runs")
	values, err := a.parse(fs, args, "DATABASE", "WORKFLOW")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	workflow, err := findWorkflow(c, values[0], values[1])
	if err != nil {
		return err
	}
	return a.get(apiPath("workflows", strconv.FormatInt(workflow.ID, 10), "runs") + "?limit=" + strconv.Itoa(*limit))
}

func runWorkflowRunLog(a *app, args []string) error {
	values, err := a.parse(a.newFlags("workflow run-log"), args, "DATABASE", "WORKFLOW", "HISTORY_KEY")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	workflow, err := findWorkflow(c, values[0], values[1])
	if err != nil {
		return err
	}
	return a.get(apiPath("workflows", strconv.FormatInt(workflow.ID, 10), "runs", values[2]))
}

func runFormList(a *app, args []string) error {
	values, err := a.parse(a.newFlags("form list"), args, "DATABASE")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	forms, err := listForms(c, values[0])
	if err != nil {
		return err
	}
	summaries := make([]map[string]any, 0, len(forms))
	for _, form := range forms {
		summaries = append(summaries, map[string]any{"id": form.ID, "name": form.Name, "updated_at": form.UpdatedAt})
	}
	return a.printJSON(summaries)
}

func runFormGet(a *app, args []string) error {
	fs := a.newFlags("form get")
	scriptOnly := fs.Bool("script", false, "print only the script source")
	values, err := a.parse(fs, args, "DATABASE", "FORM")
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	form, err := findForm(c, values[0], values[1])
	if err != nil {
		return err
	}
	if *scriptOnly {
		_, err := io.WriteString(a.stdout, form.Script)
		return err
	}
	return a.get(apiPath("forms", strconv.FormatInt(form.ID, 10)))
}

func runFileUpload(a *app, args []string) error {
	fs := a.newFlags("file upload")
	database := fs.String("database", "", "database the file belongs to")
	tableName := fs.String("table", "", "table the file belongs to")
	record := fs.Int64("record", 0, "record the file belongs to")
	values, err := a.parse(fs, args, "PATH")
	if err != nil {
		return err
	}
	file, err := os.Open(values[0])
	if err != nil {
		return err
	}
	defer file.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(values[0]))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	fields := map[string]string{"database_name": *database, "table_name": *tableName}
	if *record > 0 {
		fields["record_id"] = strconv.FormatInt(*record, 10)
	}
	for key, value := range fields {
		if value == "" {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, c.server+"/api/files", &body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	data, err := c.send(request)
	if err != nil {
		return err
	}
	return a.printRaw(data)
}

func runFileDownload(a *app, args []string) error {
	fs := a.newFlags("file download")
	output := fs.String("output", "", "path to write the file to")
	values, err := a.parse(fs, args, "FILE_ID")
	if err != nil {
		return err
	}
	if *output == "" {
		return usagef("--output is required")
	}
	if _, err := strconv.ParseInt(values[0], 10, 64); err != nil {
		return usagef("FILE_ID must be a number")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodGet, c.server+"/api/files/"+url.PathEscape(values[0]), nil)
	if err != nil {
		return err
	}
	data, err := c.send(request)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		return err
	}
	return a.printJSON(map[string]any{"path": *output, "size": len(data)})
}
