package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codetable/internal/auth"
	"codetable/internal/history"
	"codetable/internal/metadata"
	"codetable/internal/permission"
	"codetable/internal/systemdb"
	"codetable/internal/table"
	"codetable/internal/workflow"
)

type Server struct {
	catalog metadata.Catalog
	system  *systemdb.DB
	tables  *table.Service
	history history.Store
	runner  *workflow.Runner
	mux     *http.ServeMux
}

type createRowRequest struct {
	Values map[string]any `json:"values"`
}

type rowResponse struct {
	RecordID int64          `json:"record_id"`
	Values   map[string]any `json:"values"`
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Provider string `json:"provider"`
}

type workflowRunRequest struct {
	Inputs map[string]any `json:"inputs"`
}

type workflowRunResponse struct {
	HistoryKey string              `json:"history_key"`
	Run        history.WorkflowRun `json:"run"`
}

const (
	sessionCookieName = "codetable_session"
	sessionTTL        = 14 * 24 * time.Hour
)

func NewServer(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store) *Server {
	return NewServerWithWorkflowRunner(
		catalog,
		system,
		tables,
		historyStore,
		workflow.NewRunner(historyStore, workflow.EchoNode{}, workflow.NewRecordChangedTriggerNode(historyStore)),
	)
}

func NewServerWithWorkflowRunner(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store, runner *workflow.Runner) *Server {
	server := &Server{
		catalog: catalog,
		system:  system,
		tables:  tables,
		history: historyStore,
		runner:  runner,
		mux:     http.NewServeMux(),
	}
	server.routes()
	return server
}

func (server *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	server.mux.ServeHTTP(w, r)
}

func (server *Server) routes() {
	server.mux.HandleFunc("POST /api/auth/register", server.handleRegister)
	server.mux.HandleFunc("POST /api/auth/login", server.handleLogin)
	server.mux.HandleFunc("GET /api/auth/me", server.handleMe)
	server.mux.HandleFunc("POST /api/auth/logout", server.handleLogout)
	server.mux.HandleFunc("GET /api/metadata", server.handleMetadata)
	server.mux.HandleFunc("POST /api/permissions/grants", server.handleSaveGrant)
	server.mux.HandleFunc("POST /api/tables/", server.handleCreateRow)
	server.mux.HandleFunc("PATCH /api/tables/", server.handleUpdateRow)
	server.mux.HandleFunc("GET /api/tables/", server.handleGetTable)
	server.mux.HandleFunc("GET /api/databases/", server.handleGetDatabaseResource)
	server.mux.HandleFunc("POST /api/databases/", server.handlePostDatabaseResource)
	server.mux.HandleFunc("POST /api/workflows", server.handleSaveWorkflow)
	server.mux.HandleFunc("POST /api/workflows/", server.handleRunWorkflow)
	server.mux.HandleFunc("GET /api/workflows/", server.handleGetWorkflow)
	server.mux.HandleFunc("POST /api/forms", server.handleSaveForm)
	server.mux.HandleFunc("GET /api/forms/", server.handleGetForm)
}

func (server *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var request authRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:    request.Email,
		Password: request.Password,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user, err = server.system.UpsertUserByEmail(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	session, err := server.system.CreateSession(r.Context(), user.ID, sessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	setSessionCookie(w, session)
	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

func (server *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request authRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user, err := server.system.UserByEmail(r.Context(), request.Email)
	if err != nil || !user.CheckPassword(request.Password) {
		writeError(w, http.StatusUnauthorized, errors.New("invalid email or password"))
		return
	}
	session, err := server.system.CreateSession(r.Context(), user.ID, sessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	setSessionCookie(w, session)
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (server *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok, err := server.currentUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("not authenticated"))
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (server *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		_ = server.system.DeleteSession(r.Context(), cookie.Value)
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (server *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, server.catalog)
}

func (server *Server) handleSaveGrant(w http.ResponseWriter, r *http.Request) {
	var grant permission.Grant
	if err := readJSON(r, &grant); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := server.system.SaveGrant(r.Context(), grant); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, grant)
}

func (server *Server) handleCreateRow(w http.ResponseWriter, r *http.Request) {
	dbName, tableName, ok := parseTableRowsPath(r.URL.Path)
	if !ok || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	actorID, ok, err := server.currentUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication is required"))
		return
	}

	var request createRowRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	perms, err := server.system.GrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	row, err := server.tables.CreateRow(r.Context(), server.catalog, perms, actorID, dbName, tableName, request.Values)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, table.ErrPermissionDenied) {
			status = http.StatusForbidden
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusCreated, rowResponse{RecordID: row.RecordID, Values: row.Values})
}

func (server *Server) handleUpdateRow(w http.ResponseWriter, r *http.Request) {
	dbName, tableName, recordID, ok := parseTableRowPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok, err := server.currentUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication is required"))
		return
	}

	var request createRowRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	perms, err := server.system.GrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	row, err := server.tables.UpdateRow(r.Context(), server.catalog, perms, actorID, dbName, tableName, recordID, request.Values)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, table.ErrPermissionDenied) {
			status = http.StatusForbidden
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, rowResponse{RecordID: row.RecordID, Values: row.Values})
}

func (server *Server) handleGetTable(w http.ResponseWriter, r *http.Request) {
	if dbName, tableName, ok := parseTableRowsPath(r.URL.Path); ok {
		server.handleListRows(w, r, dbName, tableName)
		return
	}
	server.handleRowHistory(w, r)
}

func (server *Server) handleListRows(w http.ResponseWriter, r *http.Request, dbName, tableName string) {
	actorID, ok, err := server.currentUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication is required"))
		return
	}
	perms, err := server.system.GrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	rows, err := server.tables.Rows(r.Context(), server.catalog, perms, actorID, dbName, tableName, r.URL.Query().Get("view"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, table.ErrPermissionDenied) {
			status = http.StatusForbidden
		}
		writeError(w, status, err)
		return
	}
	response := make([]rowResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, rowResponse{RecordID: row.RecordID, Values: row.Values})
	}
	writeJSON(w, http.StatusOK, response)
}

func (server *Server) handleRowHistory(w http.ResponseWriter, r *http.Request) {
	dbName, tableName, recordID, ok := parseRowHistoryPath(r.URL.Path)
	if !ok || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	entries, err := server.history.GetPrefix(r.Context(), history.RowPrefix(dbName, tableName, recordID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	changes := make([]history.RowChange, 0, len(entries))
	for _, entry := range entries {
		change, err := history.DecodeRowChange(entry)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		changes = append(changes, change)
	}
	writeJSON(w, http.StatusOK, changes)
}

func (server *Server) handleSaveWorkflow(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var workflow systemdb.WorkflowDefinition
	if err := readJSON(r, &workflow); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if workflow.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeWorkflow, workflow.ID) {
		return
	}
	saved, err := server.system.SaveWorkflow(r.Context(), workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if workflow.ID == 0 {
		if !server.grantResourceOwner(w, r, actorID, permission.ScopeWorkflow, saved.ID) {
			return
		}
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (server *Server) handleGetDatabaseResource(w http.ResponseWriter, r *http.Request) {
	dbName, resource, ok := parseDatabaseResourcePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	switch resource {
	case "workflows":
		workflows, err := server.system.Workflows(r.Context(), dbName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		filtered, err := server.filterReadableWorkflows(r.Context(), actorID, workflows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, filtered)
	case "forms":
		forms, err := server.system.Forms(r.Context(), dbName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		filtered, err := server.filterReadableForms(r.Context(), actorID, forms)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, filtered)
	default:
		http.NotFound(w, r)
	}
}

func (server *Server) handlePostDatabaseResource(w http.ResponseWriter, r *http.Request) {
	dbName, resource, ok := parseDatabaseResourcePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	switch resource {
	case "workflows":
		var workflow systemdb.WorkflowDefinition
		if err := readJSON(r, &workflow); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if workflow.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeWorkflow, workflow.ID) {
			return
		}
		workflow.DatabaseName = dbName
		saved, err := server.system.SaveWorkflow(r.Context(), workflow)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if workflow.ID == 0 {
			if !server.grantResourceOwner(w, r, actorID, permission.ScopeWorkflow, saved.ID) {
				return
			}
		}
		writeJSON(w, http.StatusCreated, saved)
	case "forms":
		var form systemdb.FormDefinition
		if err := readJSON(r, &form); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if form.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeForm, form.ID) {
			return
		}
		form.DatabaseName = dbName
		saved, err := server.system.SaveForm(r.Context(), form)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if form.ID == 0 {
			if !server.grantResourceOwner(w, r, actorID, permission.ScopeForm, saved.ID) {
				return
			}
		}
		writeJSON(w, http.StatusCreated, saved)
	default:
		http.NotFound(w, r)
	}
}

func (server *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	if id, ok := parseWorkflowRunsPath(r.URL.Path); ok {
		server.handleWorkflowRuns(w, r, id)
		return
	}
	id, ok := parseIDPath(r.URL.Path, "/api/workflows/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireResourceRead(w, r, actorID, permission.ScopeWorkflow, id) {
		return
	}
	workflow, err := server.system.Workflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, workflow)
}

func (server *Server) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	id, ok := parseWorkflowRunsPath(r.URL.Path)
	if !ok || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireResourceWrite(w, r, actorID, permission.ScopeWorkflow, id) {
		return
	}
	var request workflowRunRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workflowDefinition, err := server.system.Workflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	run, key, err := server.runner.Run(r.Context(), workflow.Definition{
		ID:        workflowDefinition.ID,
		Script:    workflowDefinition.Script,
		Secrets:   workflowDefinition.Secrets,
		Variables: workflowDefinition.Variables,
	}, request.Inputs)
	status := http.StatusCreated
	if err != nil {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, workflowRunResponse{HistoryKey: key, Run: run})
}

func (server *Server) handleWorkflowRuns(w http.ResponseWriter, r *http.Request, workflowID int64) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireResourceRead(w, r, actorID, permission.ScopeWorkflow, workflowID) {
		return
	}
	entries, err := server.history.GetPrefix(r.Context(), history.WorkflowPrefix(workflowID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	runs := make([]workflowRunResponse, 0, len(entries))
	for _, entry := range entries {
		run, err := history.DecodeWorkflowRun(entry)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		runs = append(runs, workflowRunResponse{HistoryKey: entry.Key, Run: run})
	}
	writeJSON(w, http.StatusOK, runs)
}

func (server *Server) handleSaveForm(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var form systemdb.FormDefinition
	if err := readJSON(r, &form); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if form.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeForm, form.ID) {
		return
	}
	saved, err := server.system.SaveForm(r.Context(), form)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if form.ID == 0 {
		if !server.grantResourceOwner(w, r, actorID, permission.ScopeForm, saved.ID) {
			return
		}
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (server *Server) handleGetForm(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(r.URL.Path, "/api/forms/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireResourceRead(w, r, actorID, permission.ScopeForm, id) {
		return
	}
	form, err := server.system.Form(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, form)
}

func parseTableRowsPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "tables" || parts[4] != "rows" {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func parseRowHistoryPath(path string) (string, string, int64, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 7 || parts[0] != "api" || parts[1] != "tables" || parts[4] != "rows" || parts[6] != "history" {
		return "", "", 0, false
	}
	recordID, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil {
		return "", "", 0, false
	}
	return parts[2], parts[3], recordID, true
}

func parseTableRowPath(path string) (string, string, int64, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 6 || parts[0] != "api" || parts[1] != "tables" || parts[4] != "rows" {
		return "", "", 0, false
	}
	recordID, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil {
		return "", "", 0, false
	}
	return parts[2], parts[3], recordID, true
}

func parseDatabaseResourcePath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "databases" {
		return "", "", false
	}
	if parts[3] != "workflows" && parts[3] != "forms" {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func parseWorkflowRunsPath(path string) (int64, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "workflows" || parts[3] != "runs" {
		return 0, false
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	return id, err == nil
}

func parseIDPath(path, prefix string) (int64, bool) {
	rawID := strings.TrimPrefix(path, prefix)
	if rawID == "" || rawID == path || strings.Contains(rawID, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	return id, err == nil
}

func readJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (server *Server) requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	actorID, ok, err := server.currentUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return "", false
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication is required"))
		return "", false
	}
	return actorID, true
}

func (server *Server) requireResourceRead(w http.ResponseWriter, r *http.Request, actorID string, scope permission.Scope, id int64) bool {
	return server.requireResourceLevel(w, r, actorID, scope, id, permission.Read)
}

func (server *Server) requireResourceWrite(w http.ResponseWriter, r *http.Request, actorID string, scope permission.Scope, id int64) bool {
	return server.requireResourceLevel(w, r, actorID, scope, id, permission.Write)
}

func (server *Server) requireResourceLevel(w http.ResponseWriter, r *http.Request, actorID string, scope permission.Scope, id int64, level permission.Level) bool {
	perms, err := server.system.GrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return false
	}
	if perms.ResourceLevel(actorID, scope, resourceID(id)) < level {
		writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
		return false
	}
	return true
}

func (server *Server) filterReadableWorkflows(ctx context.Context, actorID string, workflows []systemdb.WorkflowDefinition) ([]systemdb.WorkflowDefinition, error) {
	perms, err := server.system.GrantsForSubject(ctx, actorID)
	if err != nil {
		return nil, err
	}
	filtered := make([]systemdb.WorkflowDefinition, 0, len(workflows))
	for _, workflow := range workflows {
		if perms.CanReadResource(actorID, permission.ScopeWorkflow, resourceID(workflow.ID)) {
			filtered = append(filtered, workflow)
		}
	}
	return filtered, nil
}

func (server *Server) filterReadableForms(ctx context.Context, actorID string, forms []systemdb.FormDefinition) ([]systemdb.FormDefinition, error) {
	perms, err := server.system.GrantsForSubject(ctx, actorID)
	if err != nil {
		return nil, err
	}
	filtered := make([]systemdb.FormDefinition, 0, len(forms))
	for _, form := range forms {
		if perms.CanReadResource(actorID, permission.ScopeForm, resourceID(form.ID)) {
			filtered = append(filtered, form)
		}
	}
	return filtered, nil
}

func (server *Server) grantResourceOwner(w http.ResponseWriter, r *http.Request, actorID string, scope permission.Scope, id int64) bool {
	if err := server.system.SaveGrant(r.Context(), permission.Grant{
		SubjectID: actorID,
		Scope:     scope,
		Resource:  resourceID(id),
		Level:     permission.Write,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return false
	}
	return true
}

func resourceID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func (server *Server) currentUserID(r *http.Request) (string, bool, error) {
	if userID := r.Header.Get("X-Codetable-User"); userID != "" {
		return userID, true, nil
	}
	user, ok, err := server.currentUser(r)
	if err != nil || !ok {
		return "", ok, err
	}
	return user.ID, true, nil
}

func (server *Server) currentUser(r *http.Request) (auth.User, bool, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return auth.User{}, false, nil
		}
		return auth.User{}, false, err
	}
	user, _, err := server.system.UserBySessionToken(r.Context(), cookie.Value)
	if err != nil {
		return auth.User{}, false, err
	}
	return user, true, nil
}

func setSessionCookie(w http.ResponseWriter, session auth.Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	})
}

func toUserResponse(user auth.User) userResponse {
	return userResponse{
		ID:       user.ID,
		Email:    user.Email,
		Provider: string(user.Provider),
	}
}

func ContextWithUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userContextKey{}, userID)
}

type userContextKey struct{}
