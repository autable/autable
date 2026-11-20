package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"autable/internal/appbundle"
	"autable/internal/permission"
	"autable/internal/systemdb"
	"autable/internal/table"
	"autable/internal/wfruntime"
	"autable/internal/wfsdk"

	"gorm.io/gorm"
)

// Workflows: see docs/design/workflows-v2.md (model, API) and
// docs/design/workflows-host-api.md (what the code sees).

const (
	webhookPathPrefix     = "/api/hooks/"
	maxWebhookBodyBytes   = 5 << 20
	webhookTimeout        = 15 * time.Second
	manualRunWaitLimit    = 60 * time.Second
	maxWorkflowInputBytes = 1 << 20
)

var workflowKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

// workflowSummary names a workflow, e.g. as a role member.
type workflowSummary struct {
	ID           int64  `json:"id"`
	DatabaseName string `json:"database_name"`
	Name         string `json:"name"`
	Title        string `json:"title"`
}

func workflowSummaryOf(workflow systemdb.Workflow) workflowSummary {
	return workflowSummary{ID: workflow.ID, DatabaseName: workflow.DatabaseName, Name: workflow.Name, Title: workflow.Title}
}

type workflowResponse struct {
	systemdb.Workflow
	Triggers []map[string]any       `json:"triggers"`
	LastRun  *workflowRunListedView `json:"last_run,omitempty"`
}

// runnableWorkflowResponse is what users with a run grant see.
type runnableWorkflowResponse struct {
	ID               int64            `json:"id"`
	DatabaseName     string           `json:"database_name"`
	Name             string           `json:"name"`
	Title            string           `json:"title"`
	Enabled          bool             `json:"enabled"`
	PublishedVersion int64            `json:"published_version"`
	PermissionLevel  permission.Level `json:"permission_level"`
	Triggers         []map[string]any `json:"triggers"`
}

type workflowVersionResponse struct {
	systemdb.WorkflowVersion
	Published bool   `json:"published"`
	Supported bool   `json:"supported"`
	SDKError  string `json:"sdk_error,omitempty"`
}

type workflowDetailResponse struct {
	Workflow workflowResponse          `json:"workflow"`
	Versions []workflowVersionResponse `json:"versions"`
	Secrets  []string                  `json:"secrets"`
	Vars     map[string]string         `json:"vars"`
	SDK      sdkInfo                   `json:"sdk"`
}

// workflowRunView is a run as admins see it.
type workflowRunView struct {
	ID         int64                 `json:"id"`
	WorkflowID int64                 `json:"workflow_id"`
	Version    int64                 `json:"version"`
	Trigger    string                `json:"trigger"`
	Status     string                `json:"status"`
	Event      json.RawMessage       `json:"event,omitempty"`
	Output     json.RawMessage       `json:"output,omitempty"`
	Error      string                `json:"error,omitempty"`
	StartedBy  string                `json:"started_by,omitempty"`
	CreatedAt  int64                 `json:"created_at"`
	StartedAt  int64                 `json:"started_at,omitempty"`
	FinishedAt int64                 `json:"finished_at,omitempty"`
	Logs       []workflowLogResponse `json:"logs,omitempty"`
}

// workflowRunListedView is a run in lists: no event, no logs.
type workflowRunListedView struct {
	ID         int64           `json:"id"`
	WorkflowID int64           `json:"workflow_id"`
	Version    int64           `json:"version"`
	Trigger    string          `json:"trigger"`
	Status     string          `json:"status"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	StartedBy  string          `json:"started_by,omitempty"`
	CreatedAt  int64           `json:"created_at"`
	StartedAt  int64           `json:"started_at,omitempty"`
	FinishedAt int64           `json:"finished_at,omitempty"`
}

// workflowRunUserView is a run as the user who started it sees it.
type workflowRunUserView struct {
	ID         int64           `json:"id"`
	WorkflowID int64           `json:"workflow_id"`
	Trigger    string          `json:"trigger"`
	Status     string          `json:"status"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	CreatedAt  int64           `json:"created_at"`
	StartedAt  int64           `json:"started_at,omitempty"`
	FinishedAt int64           `json:"finished_at,omitempty"`
}

type workflowLogResponse struct {
	Time    int64           `json:"time"`
	Level   string          `json:"level"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func listedRunView(run systemdb.WorkflowRun) workflowRunListedView {
	return workflowRunListedView{
		ID: run.ID, WorkflowID: run.WorkflowID, Version: run.Version, Trigger: run.Trigger, Status: run.Status,
		Output: systemdb.RawJSON(run.Output), Error: run.Error, StartedBy: run.StartedBy,
		CreatedAt: run.CreatedAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}

func userRunView(run systemdb.WorkflowRun) workflowRunUserView {
	return workflowRunUserView{
		ID: run.ID, WorkflowID: run.WorkflowID, Trigger: run.Trigger, Status: run.Status,
		Output: systemdb.RawJSON(run.Output), Error: run.Error,
		CreatedAt: run.CreatedAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}

func (server *Server) adminRunView(ctx context.Context, run systemdb.WorkflowRun) (workflowRunView, error) {
	logs, err := server.system.WorkflowLogs(ctx, run.ID)
	if err != nil {
		return workflowRunView{}, err
	}
	view := workflowRunView{
		ID: run.ID, WorkflowID: run.WorkflowID, Version: run.Version, Trigger: run.Trigger, Status: run.Status,
		Event: systemdb.RawJSON(run.Event), Output: systemdb.RawJSON(run.Output), Error: run.Error, StartedBy: run.StartedBy,
		CreatedAt: run.CreatedAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
		Logs: make([]workflowLogResponse, 0, len(logs)),
	}
	for _, entry := range logs {
		view.Logs = append(view.Logs, workflowLogResponse{Time: entry.Time, Level: entry.Level, Message: entry.Message, Data: systemdb.RawJSON(entry.Data)})
	}
	return view, nil
}

func (server *Server) workflowRoutes() {
	server.mux.HandleFunc("GET /api/workflows/sdk", server.handleWorkflowSDK)
	server.mux.HandleFunc("GET /api/databases/{database}/workflows/{name}", server.handleGetWorkflow)
	server.mux.HandleFunc("PATCH /api/databases/{database}/workflows/{name}", server.handleUpdateWorkflow)
	server.mux.HandleFunc("DELETE /api/databases/{database}/workflows/{name}", server.handleDeleteWorkflow)
	server.mux.HandleFunc("POST /api/databases/{database}/workflows/{name}/versions", server.handleUploadWorkflowVersion)
	server.mux.HandleFunc("POST /api/databases/{database}/workflows/{name}/publish", server.handlePublishWorkflow)
	server.mux.HandleFunc("GET /api/databases/{database}/workflows/{name}/versions/{version}/source", server.handleWorkflowSource)
	server.mux.HandleFunc("PUT /api/databases/{database}/workflows/{name}/secrets/{key}", server.handleSetWorkflowSecret)
	server.mux.HandleFunc("DELETE /api/databases/{database}/workflows/{name}/secrets/{key}", server.handleDeleteWorkflowSecret)
	server.mux.HandleFunc("PUT /api/databases/{database}/workflows/{name}/vars/{key}", server.handleSetWorkflowVar)
	server.mux.HandleFunc("DELETE /api/databases/{database}/workflows/{name}/vars/{key}", server.handleDeleteWorkflowVar)
	server.mux.HandleFunc("POST /api/databases/{database}/workflows/{name}/runs", server.handleStartWorkflowRun)
	server.mux.HandleFunc("GET /api/databases/{database}/workflows/{name}/runs", server.handleListWorkflowRuns)
	server.mux.HandleFunc("GET /api/databases/{database}/workflows/{name}/runs/{runID}", server.handleGetWorkflowRun)
	server.mux.HandleFunc("POST /api/databases/{database}/workflows/{name}/runs/{runID}/cancel", server.handleCancelWorkflowRun)
	server.mux.HandleFunc(webhookPathPrefix+"{database}/{name}/{path...}", server.handleWorkflowWebhook)
}

func currentWorkflowSDKInfo() sdkInfo {
	return sdkInfo{Version: wfsdk.Version(), MinMajor: wfsdk.MinMajor}
}

func (server *Server) handleWorkflowSDK(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireUserID(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   wfsdk.Version(),
		"min_major": wfsdk.MinMajor,
		"files":     wfsdk.Files(),
	})
}

// workflowWebhookURL is the path a webhook trigger answers on.
func workflowWebhookURL(workflow systemdb.Workflow, path string) string {
	return webhookPathPrefix + url.PathEscape(workflow.DatabaseName) + "/" + workflow.Name + "/" + path
}

// triggerResponses renders triggers as their declarations plus name (and
// url for webhooks); manualOnly keeps only manual triggers.
func triggerResponses(workflow systemdb.Workflow, triggers []wfruntime.Trigger, manualOnly bool) []map[string]any {
	responses := make([]map[string]any, 0, len(triggers))
	for _, trigger := range triggers {
		if manualOnly && trigger.Kind != wfruntime.KindManual {
			continue
		}
		encoded, err := json.Marshal(trigger)
		if err != nil {
			continue
		}
		var response map[string]any
		if err := json.Unmarshal(encoded, &response); err != nil {
			continue
		}
		if trigger.Kind == wfruntime.KindWebhook {
			response["url"] = workflowWebhookURL(workflow, trigger.Path)
		}
		responses = append(responses, response)
	}
	return responses
}

func (server *Server) workflowResponse(ctx context.Context, workflow systemdb.Workflow) (workflowResponse, error) {
	triggers, err := server.workflows.publishedTriggers(ctx, workflow)
	if err != nil {
		return workflowResponse{}, err
	}
	workflow.PermissionLevel = resourcePermissionLevel(true)
	response := workflowResponse{Workflow: workflow, Triggers: triggerResponses(workflow, triggers, false)}
	last, ok, err := server.system.LatestWorkflowRun(ctx, workflow.ID)
	if err != nil {
		return workflowResponse{}, err
	}
	if ok {
		view := listedRunView(last)
		response.LastRun = &view
	}
	return response, nil
}

// handleListWorkflows serves GET /api/databases/{db}/workflows: admins
// get every workflow; anyone else the enabled, published workflows with a
// manual trigger they may run, reduced to what starting one needs.
func (server *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request, actorID, dbName string) {
	workflows, err := server.system.Workflows(r.Context(), dbName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if server.isAdmin(r.Context(), actorID) {
		responses := make([]workflowResponse, 0, len(workflows))
		for _, workflow := range workflows {
			response, err := server.workflowResponse(r.Context(), workflow)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			responses = append(responses, response)
		}
		writeJSON(w, http.StatusOK, responses)
		return
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	runnable := make([]runnableWorkflowResponse, 0, len(workflows))
	for _, workflow := range workflows {
		if !workflow.Enabled || workflow.PublishedVersion == 0 || !canRunWorkflow(perms, actorID, false, workflow.ID) {
			continue
		}
		triggers, err := server.workflows.publishedTriggers(r.Context(), workflow)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		manual := triggerResponses(workflow, triggers, true)
		if len(manual) == 0 {
			continue
		}
		runnable = append(runnable, runnableWorkflowResponse{
			ID: workflow.ID, DatabaseName: workflow.DatabaseName, Name: workflow.Name, Title: workflow.Title,
			Enabled: workflow.Enabled, PublishedVersion: workflow.PublishedVersion,
			PermissionLevel: resourcePermissionLevel(false), Triggers: manual,
		})
	}
	writeJSON(w, http.StatusOK, runnable)
}

// workflowByPath loads the workflow named in the URL, writing 404 itself.
func (server *Server) workflowByPath(w http.ResponseWriter, r *http.Request) (systemdb.Workflow, bool) {
	workflow, err := server.system.WorkflowByName(r.Context(), r.PathValue("database"), r.PathValue("name"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, errors.New("workflow not found"))
		return systemdb.Workflow{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return systemdb.Workflow{}, false
	}
	return workflow, true
}

// adminWorkflow authenticates an admin and loads the workflow in the URL.
func (server *Server) adminWorkflow(w http.ResponseWriter, r *http.Request) (string, systemdb.Workflow, bool) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return "", systemdb.Workflow{}, false
	}
	if !server.requireAdmin(w, r, actorID) {
		return "", systemdb.Workflow{}, false
	}
	workflow, ok := server.workflowByPath(w, r)
	return actorID, workflow, ok
}

func (server *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	_, workflow, ok := server.adminWorkflow(w, r)
	if !ok {
		return
	}
	response, err := server.workflowResponse(r.Context(), workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	versions, err := server.system.WorkflowVersions(r.Context(), workflow.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	secrets, err := server.system.WorkflowSecrets(r.Context(), workflow.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	vars, err := server.system.WorkflowVars(r.Context(), workflow.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	secretNames := make([]string, 0, len(secrets))
	for name := range secrets {
		secretNames = append(secretNames, name)
	}
	sort.Strings(secretNames)
	writeJSON(w, http.StatusOK, workflowDetailResponse{
		Workflow: response,
		Versions: workflowVersionResponses(workflow, versions),
		Secrets:  secretNames,
		Vars:     vars,
		SDK:      currentWorkflowSDKInfo(),
	})
}

func workflowVersionResponses(workflow systemdb.Workflow, versions []systemdb.WorkflowVersion) []workflowVersionResponse {
	responses := make([]workflowVersionResponse, 0, len(versions))
	for _, version := range versions {
		response := workflowVersionResponse{WorkflowVersion: version, Published: version.Version == workflow.PublishedVersion, Supported: true}
		if err := wfsdk.Check(version.SDK); err != nil {
			response.Supported = false
			response.SDKError = err.Error()
		}
		responses = append(responses, response)
	}
	return responses
}

// readWorkflowTriggers compiles a bundle and reads and validates its
// triggers; failures are the bundle's fault (422).
func (server *Server) readWorkflowTriggers(ctx context.Context, workflow systemdb.Workflow, version int64, bundle appbundle.Bundle) (string, error) {
	program, err := wfruntime.Compile("workflow.js", string(bundle.Dist[appbundle.Workflow.RequiredDist]))
	if err != nil {
		return "", err
	}
	raw, err := wfruntime.ReadTriggers(ctx, program, wfruntime.Info{Database: workflow.DatabaseName, Workflow: workflow.Name, Version: version})
	if err != nil {
		return "", fmt.Errorf("read triggers: %w", err)
	}
	triggers, err := wfruntime.ParseTriggers(raw, catalogSchema{server: server, database: workflow.DatabaseName})
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(triggers)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// catalogSchema answers trigger validation from the live catalog.
type catalogSchema struct {
	server   *Server
	database string
}

func (schema catalogSchema) TableFields(tableName string) ([]string, bool) {
	tableMeta, ok := schema.server.catalogSnapshot().Table(schema.database, tableName)
	if !ok {
		return nil, false
	}
	names := []string{}
	for _, field := range tableMeta.ActiveFields() {
		names = append(names, field.Name)
	}
	return names, true
}

func (server *Server) handleUploadWorkflowVersion(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireAdmin(w, r, actorID) {
		return
	}
	dbName, name := r.PathValue("database"), r.PathValue("name")
	if err := appbundle.ValidName(name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, appbundle.MaxCompressedBytes)
	archive, err := io.ReadAll(r.Body)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("bundle is larger than %d bytes", appbundle.MaxCompressedBytes))
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read bundle: %w", err))
		return
	}
	bundle, err := appbundle.Workflow.Parse(archive)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if bundle.Manifest.Database != dbName || bundle.Manifest.Name != name {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%s names %s/%s, but the upload targets %s/%s", appbundle.WorkflowManifestName, bundle.Manifest.Database, bundle.Manifest.Name, dbName, name))
		return
	}
	if err := wfsdk.Check(bundle.Manifest.SDK); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if _, err := wfruntime.Compile("workflow.js", string(bundle.Dist[appbundle.Workflow.RequiredDist])); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	workflow, err := server.system.WorkflowByName(r.Context(), dbName, name)
	created := false
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if !server.databaseExists(dbName) {
			writeError(w, http.StatusNotFound, fmt.Errorf("database %q not found", dbName))
			return
		}
		workflow, err = server.system.CreateWorkflow(r.Context(), systemdb.Workflow{DatabaseName: dbName, Name: name, Title: bundle.Manifest.Title, CreatorID: actorID})
		if err != nil {
			writeError(w, http.StatusConflict, fmt.Errorf("create workflow %s: %w", name, err))
			return
		}
		created = true
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	publish := r.URL.Query().Get("publish") == "1" || r.URL.Query().Get("publish") == "true"
	var triggersJSON string
	if publish {
		// Read the triggers before storing anything: an invalid bundle is
		// rejected as a whole.
		triggersJSON, err = server.readWorkflowTriggers(r.Context(), workflow, workflow.LatestVersion+1, bundle)
		if err != nil {
			if created {
				_ = server.system.DeleteWorkflow(r.Context(), workflow.ID)
			}
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
	}
	workflow, version, err := server.system.AddWorkflowVersion(r.Context(), workflow.ID, bundle.Manifest.Title, systemdb.WorkflowVersion{
		SDK:       bundle.Manifest.SDK,
		Message:   r.URL.Query().Get("message"),
		CreatedBy: actorID,
	}, archive)
	if err == nil && publish {
		workflow, err = server.system.PublishWorkflowVersion(r.Context(), workflow.ID, version.Version, triggersJSON)
	}
	if err != nil {
		if created {
			_ = server.system.DeleteWorkflow(r.Context(), workflow.ID)
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.mirrorVersion(r.Context(), actorID, appbundle.Workflow, workflow.DatabaseName, workflow.Name, archive, mirroredVersion{
		Version: version.Version, PublishedVersion: workflow.PublishedVersion, SDK: version.SDK,
		Message: version.Message, CreatedBy: version.CreatedBy, CreatedAt: version.CreatedAt,
	})
	response, err := server.workflowResponse(r.Context(), workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{
		"workflow": response,
		"version":  workflowVersionResponses(workflow, []systemdb.WorkflowVersion{version})[0],
	})
}

func (server *Server) handlePublishWorkflow(w http.ResponseWriter, r *http.Request) {
	actorID, workflow, ok := server.adminWorkflow(w, r)
	if !ok {
		return
	}
	var request struct {
		Version int64 `json:"version"`
	}
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	version, err := server.system.WorkflowVersion(r.Context(), workflow.ID, request.Version)
	if errors.Is(err, systemdb.ErrWorkflowVersionNotFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("workflow %s has no version %d", workflow.Name, request.Version))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := wfsdk.Check(version.SDK); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	archive, err := server.system.WorkflowVersionBundle(r.Context(), workflow.ID, version.Version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	bundle, err := appbundle.Workflow.Parse(archive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Triggers are read again on every publish: they may name tables, which
	// can have changed since the version was uploaded.
	triggersJSON, err := server.readWorkflowTriggers(r.Context(), workflow, version.Version, bundle)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	published, err := server.system.PublishWorkflowVersion(r.Context(), workflow.ID, version.Version, triggersJSON)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.workflows.forgetTriggers(workflow.ID, version.Version)
	server.mirrorPublish(r.Context(), actorID, appbundle.Workflow, workflow.DatabaseName, workflow.Name, published.PublishedVersion)
	response, err := server.workflowResponse(r.Context(), published)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (server *Server) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	_, workflow, ok := server.adminWorkflow(w, r)
	if !ok {
		return
	}
	var settings systemdb.WorkflowSettings
	if err := readJSON(r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := systemdb.ValidateWorkflowSettings(settings); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	updated, err := server.system.UpdateWorkflowSettings(r.Context(), workflow.ID, settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response, err := server.workflowResponse(r.Context(), updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (server *Server) handleWorkflowSource(w http.ResponseWriter, r *http.Request) {
	_, workflow, ok := server.adminWorkflow(w, r)
	if !ok {
		return
	}
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("version must be a positive number"))
		return
	}
	archive, err := server.system.WorkflowVersionBundle(r.Context(), workflow.ID, version)
	if errors.Is(err, systemdb.ErrWorkflowVersionNotFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("workflow %s has no version %d", workflow.Name, version))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	source, err := appbundle.Workflow.Source(archive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-v%d-source.tar.gz"`, workflow.Name, version))
	_, _ = w.Write(source)
}

func (server *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	actorID, workflow, ok := server.adminWorkflow(w, r)
	if !ok {
		return
	}
	server.workflows.cancelWorkflow(workflow.ID)
	if err := server.system.DeleteWorkflow(r.Context(), workflow.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.workflows.forgetWorkflow(workflow.ID)
	server.mirrorDelete(r.Context(), actorID, appbundle.Workflow, workflow.DatabaseName, workflow.Name)
	w.WriteHeader(http.StatusNoContent)
}

type workflowValueRequest struct {
	Value string `json:"value"`
}

func (server *Server) workflowKeyRequest(w http.ResponseWriter, r *http.Request, withValue bool) (systemdb.Workflow, string, string, bool) {
	_, workflow, ok := server.adminWorkflow(w, r)
	if !ok {
		return systemdb.Workflow{}, "", "", false
	}
	key := r.PathValue("key")
	if !workflowKeyPattern.MatchString(key) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("name %q must match %s", key, workflowKeyPattern))
		return systemdb.Workflow{}, "", "", false
	}
	if !withValue {
		return workflow, key, "", true
	}
	var request workflowValueRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return systemdb.Workflow{}, "", "", false
	}
	return workflow, key, request.Value, true
}

func (server *Server) handleSetWorkflowSecret(w http.ResponseWriter, r *http.Request) {
	workflow, key, value, ok := server.workflowKeyRequest(w, r, true)
	if !ok {
		return
	}
	if err := server.system.SetWorkflowSecret(r.Context(), workflow.ID, key, value); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleDeleteWorkflowSecret(w http.ResponseWriter, r *http.Request) {
	workflow, key, _, ok := server.workflowKeyRequest(w, r, false)
	if !ok {
		return
	}
	removed, err := server.system.DeleteWorkflowSecret(r.Context(), workflow.ID, key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, fmt.Errorf("workflow %s has no secret %q", workflow.Name, key))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleSetWorkflowVar(w http.ResponseWriter, r *http.Request) {
	workflow, key, value, ok := server.workflowKeyRequest(w, r, true)
	if !ok {
		return
	}
	if err := server.system.SetWorkflowVar(r.Context(), workflow.ID, key, value); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleDeleteWorkflowVar(w http.ResponseWriter, r *http.Request) {
	workflow, key, _, ok := server.workflowKeyRequest(w, r, false)
	if !ok {
		return
	}
	removed, err := server.system.DeleteWorkflowVar(r.Context(), workflow.ID, key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, fmt.Errorf("workflow %s has no var %q", workflow.Name, key))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Runs.

type startWorkflowRunRequest struct {
	Trigger string          `json:"trigger,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Wait    bool            `json:"wait,omitempty"`
}

func (server *Server) handleStartWorkflowRun(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	workflow, ok := server.workflowByPath(w, r)
	if !ok {
		return
	}
	// Starting the manual trigger is the one workflow capability that can be
	// granted. The run executes as the workflow's own subject.
	isAdmin := server.isAdmin(r.Context(), actorID)
	if !isAdmin {
		perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if !canRunWorkflow(perms, actorID, false, workflow.ID) {
			writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
			return
		}
		if !workflow.Enabled {
			// Disabling is the admin's switch; a run grant does not bypass it.
			writeError(w, http.StatusConflict, errors.New("workflow is disabled"))
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkflowInputBytes)
	var request startWorkflowRunRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if workflow.PublishedVersion == 0 {
		writeError(w, http.StatusConflict, errors.New("workflow has no published version"))
		return
	}
	triggers, err := server.workflows.publishedTriggers(r.Context(), workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	trigger, err := pickManualTrigger(triggers, request.Trigger)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input := map[string]any{}
	if len(request.Input) > 0 && string(request.Input) != "null" {
		decoder := json.NewDecoder(strings.NewReader(string(request.Input)))
		decoder.UseNumber()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("input must be a JSON object: %w", err))
			return
		}
	}
	if err := wfruntime.CheckManualInput(trigger, input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	event := map[string]any{"input": input}
	if user, err := server.system.User(r.Context(), actorID); err == nil {
		event["startedBy"] = map[string]any{"id": user.ID, "email": user.Email, "display_name": user.DisplayName}
	}
	run, err := server.workflows.enqueue(r.Context(), workflow, trigger.Name, event, actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	status := http.StatusCreated
	if request.Wait {
		status = http.StatusAccepted
		waitCtx, cancel := context.WithTimeout(r.Context(), manualRunWaitLimit)
		finished := server.workflows.wait(waitCtx, run.ID)
		cancel()
		if finished {
			status = http.StatusOK
		}
		if reloaded, err := server.system.WorkflowRun(r.Context(), workflow.ID, run.ID); err == nil {
			run = reloaded
		}
	}
	server.writeRun(w, r, status, run, isAdmin)
}

func pickManualTrigger(triggers []wfruntime.Trigger, name string) (wfruntime.Trigger, error) {
	var manual []wfruntime.Trigger
	for _, trigger := range triggers {
		if trigger.Kind == wfruntime.KindManual {
			manual = append(manual, trigger)
		}
		if name != "" && trigger.Name == name {
			if trigger.Kind != wfruntime.KindManual {
				return wfruntime.Trigger{}, fmt.Errorf("trigger %q is a %s trigger; only manual triggers can be started", name, trigger.Kind)
			}
			return trigger, nil
		}
	}
	switch {
	case name != "":
		return wfruntime.Trigger{}, fmt.Errorf("the published version has no trigger %q", name)
	case len(manual) == 1:
		return manual[0], nil
	case len(manual) == 0:
		return wfruntime.Trigger{}, errors.New("the published version has no manual trigger")
	default:
		return wfruntime.Trigger{}, errors.New("the published version has several manual triggers; name one with \"trigger\"")
	}
}

func (server *Server) writeRun(w http.ResponseWriter, r *http.Request, status int, run systemdb.WorkflowRun, isAdmin bool) {
	if !isAdmin {
		writeJSON(w, status, userRunView(run))
		return
	}
	view, err := server.adminRunView(r.Context(), run)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, status, view)
}

func (server *Server) handleListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	_, workflow, ok := server.adminWorkflow(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	filter := systemdb.WorkflowRunFilter{Status: query.Get("status")}
	for name, target := range map[string]*int64{"before": &filter.Before} {
		if text := query.Get(name); text != "" {
			value, err := strconv.ParseInt(text, 10, 64)
			if err != nil || value <= 0 {
				writeError(w, http.StatusBadRequest, fmt.Errorf("%s must be a positive number", name))
				return
			}
			*target = value
		}
	}
	if text := query.Get("limit"); text != "" {
		limit, err := strconv.Atoi(text)
		if err != nil || limit <= 0 {
			writeError(w, http.StatusBadRequest, errors.New("limit must be a positive number"))
			return
		}
		filter.Limit = limit
	}
	switch filter.Status {
	case "", systemdb.WorkflowRunQueued, systemdb.WorkflowRunRunning, systemdb.WorkflowRunSucceeded, systemdb.WorkflowRunFailed, systemdb.WorkflowRunCancelled:
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown run status %q", filter.Status))
		return
	}
	runs, err := server.system.WorkflowRuns(r.Context(), workflow.ID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	views := make([]workflowRunListedView, 0, len(runs))
	for _, run := range runs {
		views = append(views, listedRunView(run))
	}
	writeJSON(w, http.StatusOK, views)
}

// runByPath loads the run named in the URL for an admin, or for the user
// who started it; everyone else gets 404, so run ids reveal nothing.
func (server *Server) runByPath(w http.ResponseWriter, r *http.Request, adminOnly bool) (systemdb.WorkflowRun, bool, bool) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return systemdb.WorkflowRun{}, false, false
	}
	isAdmin := server.isAdmin(r.Context(), actorID)
	if adminOnly && !isAdmin {
		writeError(w, http.StatusForbidden, errAdminRequired)
		return systemdb.WorkflowRun{}, false, false
	}
	workflow, ok := server.workflowByPath(w, r)
	if !ok {
		return systemdb.WorkflowRun{}, false, false
	}
	runID, ok := pathID(r, "runID")
	if !ok {
		writeError(w, http.StatusNotFound, systemdb.ErrWorkflowRunNotFound)
		return systemdb.WorkflowRun{}, false, false
	}
	run, err := server.system.WorkflowRun(r.Context(), workflow.ID, runID)
	if errors.Is(err, systemdb.ErrWorkflowRunNotFound) || (err == nil && !isAdmin && run.StartedBy != actorID) {
		writeError(w, http.StatusNotFound, systemdb.ErrWorkflowRunNotFound)
		return systemdb.WorkflowRun{}, false, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return systemdb.WorkflowRun{}, false, false
	}
	return run, isAdmin, true
}

func (server *Server) handleGetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	run, isAdmin, ok := server.runByPath(w, r, false)
	if !ok {
		return
	}
	server.writeRun(w, r, http.StatusOK, run, isAdmin)
}

func (server *Server) handleCancelWorkflowRun(w http.ResponseWriter, r *http.Request) {
	run, _, ok := server.runByPath(w, r, true)
	if !ok {
		return
	}
	if run.Status != systemdb.WorkflowRunQueued && run.Status != systemdb.WorkflowRunRunning {
		writeError(w, http.StatusConflict, fmt.Errorf("run %d already %s", run.ID, run.Status))
		return
	}
	if err := server.workflows.cancel(r.Context(), run.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if reloaded, err := server.system.WorkflowRun(r.Context(), run.WorkflowID, run.ID); err == nil {
		run = reloaded
	}
	server.writeRun(w, r, http.StatusOK, run, true)
}

// handleWorkflowWebhook serves ANY /api/hooks/{db}/{name}/{path...}. There
// is no session: the workflow code authenticates the request (signatures,
// shared secrets) and its return value is the HTTP response.
func (server *Server) handleWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	notFound := func() { writeError(w, http.StatusNotFound, errors.New("no webhook at this address")) }
	workflow, err := server.system.WorkflowByName(r.Context(), r.PathValue("database"), r.PathValue("name"))
	if err != nil || !workflow.Enabled || workflow.PublishedVersion == 0 {
		notFound()
		return
	}
	triggers, err := server.workflows.publishedTriggers(r.Context(), workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	path := r.PathValue("path")
	var trigger *wfruntime.Trigger
	for index := range triggers {
		if triggers[index].Kind == wfruntime.KindWebhook && triggers[index].Path == path {
			trigger = &triggers[index]
			break
		}
	}
	if trigger == nil {
		notFound()
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("webhook bodies are limited to %d bytes", maxWebhookBodyBytes))
		return
	}
	query := map[string]any{}
	for key, values := range r.URL.Query() {
		items := make([]any, len(values))
		for index, value := range values {
			items[index] = value
		}
		query[key] = items
	}
	headers := map[string]any{}
	for name, values := range r.Header {
		headers[strings.ToLower(name)] = strings.Join(values, ", ")
	}
	event := map[string]any{"request": map[string]any{
		"method":  r.Method,
		"path":    path,
		"url":     r.URL.RequestURI(),
		"query":   query,
		"headers": headers,
		"body":    body,
	}}
	response, run, err := server.workflows.runWebhook(r.Context(), workflow, trigger.Name, event)
	switch {
	case errors.Is(err, wfruntime.ErrTimeout) || errors.Is(err, context.DeadlineExceeded):
		writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": fmt.Sprintf("the webhook run did not answer within %s", webhookTimeout), "run_id": run.ID})
		return
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "run_id": run.ID})
		return
	}
	for name, value := range response.Headers {
		w.Header().Set(name, value)
	}
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}
