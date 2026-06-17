package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"codetable/internal/auth"
	"codetable/internal/config"
	"codetable/internal/formruntime"
	"codetable/internal/history"
	"codetable/internal/metadata"
	"codetable/internal/permission"
	"codetable/internal/systemdb"
	"codetable/internal/table"
	"codetable/internal/workflow"
)

type Server struct {
	catalogMu        sync.RWMutex
	catalog          metadata.Catalog
	metadataPath     string
	openDatabase     func(context.Context, string, string) error
	codeFiles        codeFileStore
	system           *systemdb.DB
	tables           *table.Service
	history          history.Store
	runner           *workflow.Runner
	oidc             []config.OIDCProvider
	workflowWorkers  map[string]*workflowEventWorker
	workflowWorker   context.Context
	workflowWorkerMu sync.Mutex
	mux              *http.ServeMux
}

type codeFileStore interface {
	SaveWorkflowScript(context.Context, systemdb.WorkflowDefinition) error
	LoadWorkflowScript(context.Context, systemdb.WorkflowDefinition) (string, bool, error)
	SaveFormScript(context.Context, systemdb.FormDefinition) error
	LoadFormScript(context.Context, systemdb.FormDefinition) (string, bool, error)
}

type createDatabaseRequest struct {
	Name       string `json:"name"`
	SQLitePath string `json:"sqlite_path"`
}

type createRowRequest struct {
	Values map[string]any `json:"values"`
}

type rowResponse struct {
	RecordID int64          `json:"record_id"`
	Values   map[string]any `json:"values"`
}

type rowHistoryResponse struct {
	HistoryKey string `json:"history_key"`
	history.RowChange
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

type oidcProviderResponse struct {
	Name      string   `json:"name"`
	IssuerURL string   `json:"issuer_url"`
	Scopes    []string `json:"scopes"`
}

type oidcEmailClaims struct {
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified,omitempty"`
}

type workflowRunRequest struct {
	Inputs map[string]any `json:"inputs"`
}

type workflowRunResponse struct {
	HistoryKey string              `json:"history_key"`
	Run        history.WorkflowRun `json:"run"`
}

type workflowDefinitionResponse struct {
	ID              int64             `json:"id"`
	DatabaseName    string            `json:"database_name"`
	Name            string            `json:"name"`
	Script          string            `json:"script"`
	CreatorID       string            `json:"creator_id,omitempty"`
	Secrets         map[string]int    `json:"secrets"`
	Variables       map[string]string `json:"variables"`
	PermissionLevel permission.Level  `json:"permission_level,omitempty"`
	CreatedAt       int64             `json:"created_at"`
	UpdatedAt       int64             `json:"updated_at"`
}

type workflowEventKind string

const (
	workflowEventRowChange workflowEventKind = "row_change"
	workflowEventSchedule  workflowEventKind = "schedule"
)

type workflowEvent struct {
	Kind         workflowEventKind
	DatabaseName string
	HistoryKey   string
	RowChange    history.RowChange
	ScheduledAt  time.Time
}

type workflowEventWorker struct {
	dbName string
	events chan workflowEvent
}

type roleGrantsRequest struct {
	Grants []permission.Grant `json:"grants"`
}

type roleMembersRequest struct {
	Members []string `json:"members"`
}

type publishedFormSubmitRequest struct {
	Values map[string]any `json:"values"`
}

const (
	sessionCookieName   = "codetable_session"
	oidcStateCookieName = "codetable_oidc_state"
	sessionTTL          = 14 * 24 * time.Hour
	oidcStateTTL        = 10 * time.Minute
)

func NewServer(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store) *Server {
	return NewServerWithWorkflowRunner(
		catalog,
		system,
		tables,
		historyStore,
		workflow.NewRunner(historyStore, workflow.EchoNode{}, workflow.NewRecordChangedTriggerNode(historyStore), workflow.ScheduleTriggerNode{}, workflow.NewDingTalkRobotNode()),
	)
}

func NewServerWithWorkflowRunner(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store, runner *workflow.Runner) *Server {
	return NewServerWithWorkflowRunnerAndOIDC(catalog, system, tables, historyStore, runner, nil)
}

func NewServerWithOIDCProviders(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store, providers []config.OIDCProvider) *Server {
	return NewServerWithWorkflowRunnerAndOIDC(
		catalog,
		system,
		tables,
		historyStore,
		workflow.NewRunner(historyStore, workflow.EchoNode{}, workflow.NewRecordChangedTriggerNode(historyStore), workflow.ScheduleTriggerNode{}, workflow.NewDingTalkRobotNode()),
		providers,
	)
}

func NewServerWithWorkflowRunnerAndOIDC(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store, runner *workflow.Runner, providers []config.OIDCProvider) *Server {
	server := &Server{
		catalog: catalog,
		system:  system,
		tables:  tables,
		history: historyStore,
		runner:  runner,
		oidc:    append([]config.OIDCProvider(nil), providers...),
		mux:     http.NewServeMux(),
	}
	server.tables.SetRowChangeHandler(server.dispatchRowChangeEvent)
	server.registerWorkflowTableNodes()
	server.routes()
	return server
}

func (server *Server) EnableMetadataWrites(path string) {
	server.metadataPath = path
}

func (server *Server) SetDatabaseOpener(openDatabase func(context.Context, string, string) error) {
	server.openDatabase = openDatabase
}

func (server *Server) SetCodeFileStore(store codeFileStore) {
	server.codeFiles = store
}

func (server *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	server.mux.ServeHTTP(w, r)
}

func (server *Server) routes() {
	server.mux.HandleFunc("POST /api/auth/register", server.handleRegister)
	server.mux.HandleFunc("POST /api/auth/login", server.handleLogin)
	server.mux.HandleFunc("GET /api/auth/oidc/providers", server.handleOIDCProviders)
	server.mux.HandleFunc("GET /api/auth/oidc/", server.handleOIDC)
	server.mux.HandleFunc("GET /api/auth/me", server.handleMe)
	server.mux.HandleFunc("POST /api/auth/logout", server.handleLogout)
	server.mux.HandleFunc("GET /api/metadata", server.handleMetadata)
	server.mux.HandleFunc("POST /api/permissions/grants", server.handleSaveGrant)
	server.mux.HandleFunc("POST /api/tables/", server.handleCreateRow)
	server.mux.HandleFunc("PATCH /api/tables/", server.handleUpdateRow)
	server.mux.HandleFunc("DELETE /api/tables/", server.handleDeleteRow)
	server.mux.HandleFunc("GET /api/tables/", server.handleGetTable)
	server.mux.HandleFunc("POST /api/databases", server.handleCreateDatabase)
	server.mux.HandleFunc("GET /api/databases/", server.handleGetDatabaseResource)
	server.mux.HandleFunc("POST /api/databases/", server.handlePostDatabaseResource)
	server.mux.HandleFunc("PUT /api/databases/", server.handlePutDatabaseResource)
	server.mux.HandleFunc("GET /api/workflow/nodes", server.handleWorkflowNodes)
	server.mux.HandleFunc("POST /api/workflows", server.handleSaveWorkflow)
	server.mux.HandleFunc("POST /api/workflows/", server.handleRunWorkflow)
	server.mux.HandleFunc("GET /api/workflows/", server.handleGetWorkflow)
	server.mux.HandleFunc("POST /api/forms", server.handleSaveForm)
	server.mux.HandleFunc("POST /api/forms/", server.handlePostFormAction)
	server.mux.HandleFunc("GET /api/forms/", server.handleGetForm)
	server.mux.HandleFunc("GET /api/published/forms/", server.handleGetPublishedForm)
	server.mux.HandleFunc("POST /api/published/forms/", server.handleSubmitPublishedForm)
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

func (server *Server) handleOIDCProviders(w http.ResponseWriter, _ *http.Request) {
	providers := make([]oidcProviderResponse, 0, len(server.oidc))
	for _, provider := range server.oidc {
		providers = append(providers, oidcProviderResponse{
			Name:      provider.Name,
			IssuerURL: provider.IssuerURL,
			Scopes:    oidcScopes(provider),
		})
	}
	writeJSON(w, http.StatusOK, providers)
}

func (server *Server) handleOIDC(w http.ResponseWriter, r *http.Request) {
	providerName, action, ok := parseOIDCPath(r.URL.Path)
	if !ok || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	provider, ok := server.oidcProvider(providerName)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("oidc provider %q not found", providerName))
		return
	}
	switch action {
	case "start":
		server.handleOIDCStart(w, r, provider)
	case "callback":
		server.handleOIDCCallback(w, r, provider)
	default:
		http.NotFound(w, r)
	}
}

func (server *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request, provider config.OIDCProvider) {
	state, err := auth.NewSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	setOIDCStateCookie(w, provider.Name, state)
	authURL, err := oidcAuthorizeURL(r, provider, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (server *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request, provider config.OIDCProvider) {
	state := r.URL.Query().Get("state")
	if state == "" {
		writeError(w, http.StatusBadRequest, errors.New("oidc state is required"))
		return
	}
	cookie, err := r.Cookie(oidcStateCookieName)
	if err != nil || cookie.Value != provider.Name+":"+state {
		writeError(w, http.StatusUnauthorized, errors.New("invalid oidc state"))
		return
	}
	clearOIDCStateCookie(w)

	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, errors.New("oidc code is required"))
		return
	}
	oidcProvider, err := oidc.NewProvider(r.Context(), provider.IssuerURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	oauthConfig := oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Endpoint:     oidcProvider.Endpoint(),
		RedirectURL:  oidcCallbackURL(r, provider.Name),
		Scopes:       oidcScopes(provider),
	}
	token, err := oauthConfig.Exchange(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		writeError(w, http.StatusBadGateway, errors.New("oidc id_token is required"))
		return
	}
	idToken, err := oidcProvider.Verifier(&oidc.Config{ClientID: provider.ClientID}).Verify(r.Context(), rawIDToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	claims, err := oidcClaims(r.Context(), oidcProvider, token, idToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if claims.EmailVerified != nil && !*claims.EmailVerified {
		writeError(w, http.StatusUnauthorized, errors.New("oidc email is not verified"))
		return
	}
	user, err := auth.NewOIDCUser(auth.OIDCIdentity{
		ProviderName: provider.Name,
		Subject:      idToken.Subject,
		Email:        claims.Email,
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
	http.Redirect(w, r, "/", http.StatusFound)
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
	actorID, ok, err := server.currentUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, metadata.Catalog{Databases: []metadata.Database{}})
		return
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	catalog, err := server.visibleCatalog(r.Context(), actorID, perms)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

func (server *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var request createDatabaseRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if request.SQLitePath == "" {
		request.SQLitePath = fmt.Sprintf("./data/%s.sqlite", request.Name)
	}
	database := metadata.Database{Name: request.Name, SQLitePath: request.SQLitePath, Tables: []metadata.Table{}}
	if err := server.createDatabase(r.Context(), database); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := server.system.SaveGrant(r.Context(), permission.Grant{
		SubjectID: actorID,
		Scope:     permission.ScopeDatabase,
		Resource:  database.Name,
		Level:     permission.Write,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, database)
}

func (server *Server) handleSaveGrant(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var grant permission.Grant
	if err := readJSON(r, &grant); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	dbName, err := server.grantDatabaseName(r.Context(), grant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !server.requireDatabaseWrite(w, r, actorID, dbName) {
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
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	row, err := server.tables.CreateRow(r.Context(), server.catalogSnapshot(), perms, actorID, dbName, tableName, request.Values)
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
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	row, err := server.tables.UpdateRow(r.Context(), server.catalogSnapshot(), perms, actorID, dbName, tableName, recordID, request.Values)
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

func (server *Server) handleDeleteRow(w http.ResponseWriter, r *http.Request) {
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
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	row, err := server.tables.DeleteRow(r.Context(), server.catalogSnapshot(), perms, actorID, dbName, tableName, recordID)
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
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	rows, err := server.tables.Rows(r.Context(), server.catalogSnapshot(), perms, actorID, dbName, tableName, r.URL.Query().Get("view"))
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
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	tableMeta, ok := server.catalogSnapshot().Table(dbName, tableName)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("table %s.%s not found", dbName, tableName))
		return
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resource := dbName + "." + tableName
	if !canReadRowHistory(perms, actorID, resource, tableMeta) {
		writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
		return
	}
	entries, err := server.history.GetPrefix(r.Context(), history.RowPrefix(dbName, tableName, recordID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	changes := make([]rowHistoryResponse, 0, len(entries))
	for _, entry := range entries {
		change, err := history.DecodeRowChange(entry)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		change.Values = readableHistoryValues(change.Values, perms, actorID, resource, tableMeta)
		changes = append(changes, rowHistoryResponse{HistoryKey: entry.Key, RowChange: change})
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
	if workflow.ID == 0 && !server.requireDatabaseOrTableWrite(w, r, actorID, workflow.DatabaseName) {
		return
	}
	if workflow.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeWorkflow, workflow.ID) {
		return
	}
	if workflow.ID != 0 && !server.requireExistingWorkflowDatabase(w, r, workflow.ID, workflow.DatabaseName) {
		return
	}
	if workflow.ID == 0 {
		workflow.CreatorID = actorID
	}
	saved, err := server.system.SaveWorkflow(r.Context(), workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := server.saveWorkflowScriptFile(r.Context(), saved); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if workflow.ID == 0 {
		if !server.grantResourceOwner(w, r, actorID, permission.ScopeWorkflow, saved.ID) {
			return
		}
	}
	saved = server.workflowWithPermissionLevel(r.Context(), actorID, saved)
	writeJSON(w, http.StatusCreated, workflowResponseFromDefinition(saved))
}

func (server *Server) handleWorkflowNodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, server.runner.NodeInfos())
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
		workflows, err = server.workflowDefinitionsWithFileScripts(r.Context(), workflows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		filtered, err := server.filterReadableWorkflows(r.Context(), actorID, workflows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, workflowResponsesFromDefinitions(filtered))
	case "forms":
		forms, err := server.system.Forms(r.Context(), dbName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		forms, err = server.formDefinitionsWithFileScripts(r.Context(), forms)
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
	case "roles":
		if !server.requireDatabaseWrite(w, r, actorID, dbName) {
			return
		}
		roles, err := server.system.Roles(r.Context(), dbName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, roles)
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
	case "tables":
		if !server.requireDatabaseWrite(w, r, actorID, dbName) {
			return
		}
		var tableMeta metadata.Table
		if err := readJSON(r, &tableMeta); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := server.addTable(dbName, tableMeta); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := server.system.SaveGrant(r.Context(), permission.Grant{
			SubjectID: actorID,
			Scope:     permission.ScopeTable,
			Resource:  dbName + "." + tableMeta.Name,
			Level:     permission.Write,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, tableMeta)
	case "workflows":
		var workflow systemdb.WorkflowDefinition
		if err := readJSON(r, &workflow); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if workflow.ID == 0 && !server.requireDatabaseOrTableWrite(w, r, actorID, dbName) {
			return
		}
		if workflow.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeWorkflow, workflow.ID) {
			return
		}
		if workflow.ID != 0 && !server.requireExistingWorkflowDatabase(w, r, workflow.ID, dbName) {
			return
		}
		workflow.DatabaseName = dbName
		if workflow.ID == 0 {
			workflow.CreatorID = actorID
		}
		saved, err := server.system.SaveWorkflow(r.Context(), workflow)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := server.saveWorkflowScriptFile(r.Context(), saved); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if workflow.ID == 0 {
			if !server.grantResourceOwner(w, r, actorID, permission.ScopeWorkflow, saved.ID) {
				return
			}
		}
		saved = server.workflowWithPermissionLevel(r.Context(), actorID, saved)
		writeJSON(w, http.StatusCreated, workflowResponseFromDefinition(saved))
	case "forms":
		var form systemdb.FormDefinition
		if err := readJSON(r, &form); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if form.ID == 0 && !server.requireDatabaseOrTableWrite(w, r, actorID, dbName) {
			return
		}
		if form.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeForm, form.ID) {
			return
		}
		if form.ID != 0 && !server.requireExistingFormDatabase(w, r, form.ID, dbName) {
			return
		}
		form.DatabaseName = dbName
		saved, err := server.system.SaveForm(r.Context(), form)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := server.saveFormScriptFile(r.Context(), saved); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if form.ID == 0 {
			if !server.grantResourceOwner(w, r, actorID, permission.ScopeForm, saved.ID) {
				return
			}
		}
		saved = server.formWithPermissionLevel(r.Context(), actorID, saved)
		writeJSON(w, http.StatusCreated, saved)
	case "roles":
		if !server.requireDatabaseWrite(w, r, actorID, dbName) {
			return
		}
		var role systemdb.RoleDefinition
		if err := readJSON(r, &role); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		role.DatabaseName = dbName
		saved, err := server.system.SaveRole(r.Context(), role)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, saved)
	default:
		http.NotFound(w, r)
	}
}

func (server *Server) handlePutDatabaseResource(w http.ResponseWriter, r *http.Request) {
	if dbName, tableName, ok := parseDatabaseTablePath(r.URL.Path); ok {
		server.handleUpdateTableMetadata(w, r, dbName, tableName)
		return
	}
	dbName, roleName, action, ok := parseRoleActionPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireDatabaseWrite(w, r, actorID, dbName) {
		return
	}
	var role systemdb.RoleDefinition
	var err error
	switch action {
	case "grants":
		var request roleGrantsRequest
		if err := readJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := server.validateRoleGrants(r.Context(), dbName, request.Grants); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		role, err = server.system.ReplaceRoleGrants(r.Context(), dbName, roleName, request.Grants)
	case "members":
		var request roleMembersRequest
		if err := readJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		role, err = server.system.ReplaceRoleMembers(r.Context(), dbName, roleName, request.Members)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (server *Server) handleUpdateTableMetadata(w http.ResponseWriter, r *http.Request, dbName, tableName string) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireDatabaseOrSpecificTableWrite(w, r, actorID, dbName, tableName) {
		return
	}
	var tableMeta metadata.Table
	if err := readJSON(r, &tableMeta); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if tableMeta.Name == "" {
		tableMeta.Name = tableName
	}
	if err := server.updateTable(dbName, tableName, tableMeta); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, tableMeta)
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
	workflow, err = server.workflowDefinitionWithFileScript(r.Context(), workflow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	workflow = server.workflowWithPermissionLevel(r.Context(), actorID, workflow)
	writeJSON(w, http.StatusOK, workflowResponseFromDefinition(workflow))
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
	workflowDefinition, err = server.workflowDefinitionWithFileScript(r.Context(), workflowDefinition)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	run, key, err := server.runner.Run(r.Context(), workflow.Definition{
		ID:           workflowDefinition.ID,
		DatabaseName: workflowDefinition.DatabaseName,
		Script:       workflowDefinition.Script,
		CreatorID:    workflowDefinition.CreatorID,
		Secrets:      workflowDefinition.Secrets,
		Variables:    workflowDefinition.Variables,
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

func (server *Server) StartWorkflowWorkers(ctx context.Context) {
	server.workflowWorkerMu.Lock()
	defer server.workflowWorkerMu.Unlock()
	if server.workflowWorker != nil {
		return
	}
	server.workflowWorker = ctx
	server.workflowWorkers = map[string]*workflowEventWorker{}
	for _, database := range server.catalogSnapshot().Databases {
		server.startWorkflowWorkerLocked(database.Name)
	}
}

func (server *Server) startWorkflowWorkerLocked(dbName string) *workflowEventWorker {
	if worker, ok := server.workflowWorkers[dbName]; ok {
		return worker
	}
	worker := &workflowEventWorker{
		dbName: dbName,
		events: make(chan workflowEvent, 256),
	}
	server.workflowWorkers[dbName] = worker
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-worker.events:
				server.processWorkflowEvent(ctx, event)
			}
		}
	}(server.workflowWorker)
	return worker
}

func (server *Server) dispatchWorkflowEvent(ctx context.Context, event workflowEvent) {
	if server.enqueueWorkflowEvent(ctx, event) {
		return
	}
	server.processWorkflowEvent(ctx, event)
}

func (server *Server) enqueueWorkflowEvent(ctx context.Context, event workflowEvent) bool {
	server.workflowWorkerMu.Lock()
	if server.workflowWorker == nil {
		server.workflowWorkerMu.Unlock()
		return false
	}
	worker := server.startWorkflowWorkerLocked(event.DatabaseName)
	server.workflowWorkerMu.Unlock()
	select {
	case worker.events <- event:
		return true
	case <-ctx.Done():
		return true
	case <-server.workflowWorker.Done():
		return true
	}
}

func (server *Server) StartWorkflowScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case scheduledAt := <-ticker.C:
				server.dispatchScheduleTickAll(ctx, scheduledAt.UTC())
			}
		}
	}()
}

func (server *Server) dispatchScheduleTickAll(ctx context.Context, scheduledAt time.Time) {
	for _, database := range server.catalogSnapshot().Databases {
		server.dispatchScheduleTick(ctx, database.Name, scheduledAt)
	}
}

func (server *Server) dispatchScheduleTick(ctx context.Context, dbName string, scheduledAt time.Time) {
	server.dispatchWorkflowEvent(ctx, workflowEvent{
		Kind:         workflowEventSchedule,
		DatabaseName: dbName,
		ScheduledAt:  scheduledAt.UTC(),
	})
}

func (server *Server) scheduleTriggerMatches(ctx context.Context, workflowID int64, declaration workflow.TriggerDeclaration, scheduledAt time.Time) bool {
	if declaration.Node != "time.schedule" {
		return false
	}
	latestRun, hasLatestRun, err := server.latestWorkflowRunTimestamp(ctx, workflowID)
	if err != nil {
		return false
	}
	hasSchedule := false
	if intervalMS, ok := triggerInt64Param(declaration.Params, "interval_ms"); ok && intervalMS > 0 {
		hasSchedule = true
		if !hasLatestRun || scheduledAt.Sub(latestRun) >= time.Duration(intervalMS)*time.Millisecond {
			return true
		}
	}
	if dailyAt, ok := triggerStringParam(declaration.Params, "daily_at"); ok {
		hasSchedule = true
		if dailyScheduleDue(scheduledAt, latestRun, hasLatestRun, dailyAt) {
			return true
		}
	}
	return !hasSchedule
}

func (server *Server) latestWorkflowRunTimestamp(ctx context.Context, workflowID int64) (time.Time, bool, error) {
	entries, err := server.history.GetPrefix(ctx, history.WorkflowPrefix(workflowID))
	if err != nil {
		return time.Time{}, false, err
	}
	var latest int64
	for _, entry := range entries {
		run, err := history.DecodeWorkflowRun(entry)
		if err != nil {
			return time.Time{}, false, err
		}
		if run.Timestamp > latest {
			latest = run.Timestamp
		}
	}
	if latest == 0 {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(latest).UTC(), true, nil
}

func dailyScheduleDue(scheduledAt, latestRun time.Time, hasLatestRun bool, dailyAt string) bool {
	parsed, err := time.Parse("15:04", dailyAt)
	if err != nil {
		return false
	}
	scheduledAt = scheduledAt.UTC()
	dueAt := time.Date(scheduledAt.Year(), scheduledAt.Month(), scheduledAt.Day(), parsed.Hour(), parsed.Minute(), 0, 0, time.UTC)
	if scheduledAt.Before(dueAt) {
		return false
	}
	return !hasLatestRun || latestRun.Before(dueAt)
}

func (server *Server) dispatchRowChangeEvent(ctx context.Context, historyKey string, change history.RowChange) {
	server.dispatchWorkflowEvent(ctx, workflowEvent{
		Kind:         workflowEventRowChange,
		DatabaseName: change.Database,
		HistoryKey:   historyKey,
		RowChange:    change,
	})
}

func (server *Server) processWorkflowEvent(ctx context.Context, event workflowEvent) {
	workflows, err := server.system.Workflows(ctx, event.DatabaseName)
	if err != nil {
		return
	}
	workflows, err = server.workflowDefinitionsWithFileScripts(ctx, workflows)
	if err != nil {
		return
	}
	for _, workflowDefinition := range workflows {
		definition := workflow.Definition{
			ID:           workflowDefinition.ID,
			DatabaseName: workflowDefinition.DatabaseName,
			Script:       workflowDefinition.Script,
			CreatorID:    workflowDefinition.CreatorID,
			Secrets:      workflowDefinition.Secrets,
			Variables:    workflowDefinition.Variables,
		}
		declaration, err := server.runner.Trigger(ctx, definition)
		if errors.Is(err, workflow.ErrMissingTrigger) {
			continue
		}
		if err != nil || !server.workflowEventMatches(ctx, workflowDefinition.ID, declaration, event) {
			continue
		}
		inputs := workflowEventInputs(event)
		if event.Kind == workflowEventSchedule {
			_, _, _ = server.runner.RunAt(ctx, definition, inputs, event.ScheduledAt)
			continue
		}
		_, _, _ = server.runner.Run(ctx, definition, inputs)
	}
}

func (server *Server) workflowEventMatches(ctx context.Context, workflowID int64, declaration workflow.TriggerDeclaration, event workflowEvent) bool {
	switch event.Kind {
	case workflowEventSchedule:
		return server.scheduleTriggerMatches(ctx, workflowID, declaration, event.ScheduledAt)
	case workflowEventRowChange:
		return rowChangeTriggerMatches(declaration, event.RowChange)
	default:
		return false
	}
}

func workflowEventInputs(event workflowEvent) map[string]any {
	switch event.Kind {
	case workflowEventSchedule:
		return map[string]any{
			"scheduled_at": event.ScheduledAt.UTC().UnixMilli(),
			"event":        "schedule",
		}
	case workflowEventRowChange:
		change := event.RowChange
		return map[string]any{
			"history_key": event.HistoryKey,
			"database":    change.Database,
			"table":       change.Table,
			"record_id":   change.RecordID,
			"operation":   change.Operation,
			"diff":        change.Diff,
		}
	default:
		return map[string]any{}
	}
}

func rowChangeTriggerMatches(declaration workflow.TriggerDeclaration, change history.RowChange) bool {
	if declaration.Node != "table.record.changed" {
		return false
	}
	if tableName, ok := triggerStringParam(declaration.Params, "table"); ok && tableName != change.Table {
		return false
	}
	if operations := triggerStringSetParam(declaration.Params, "operations"); len(operations) > 0 {
		if _, ok := operations[change.Operation]; !ok {
			return false
		}
	}
	if fields := triggerStringSetParam(declaration.Params, "fields"); len(fields) > 0 {
		for field := range change.Diff {
			if _, ok := fields[field]; ok {
				return true
			}
		}
		return false
	}
	return true
}

func triggerStringParam(params map[string]any, key string) (string, bool) {
	value, ok := params[key].(string)
	return value, ok && value != ""
}

func triggerInt64Param(params map[string]any, key string) (int64, bool) {
	switch value := params[key].(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		return int64(value), true
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func triggerStringSetParam(params map[string]any, key string) map[string]struct{} {
	raw, ok := params[key]
	if !ok {
		return nil
	}
	values := map[string]struct{}{}
	switch typed := raw.(type) {
	case []any:
		for _, item := range typed {
			if value, ok := item.(string); ok && value != "" {
				values[value] = struct{}{}
			}
		}
	case []string:
		for _, value := range typed {
			if value != "" {
				values[value] = struct{}{}
			}
		}
	case string:
		if typed != "" {
			values[typed] = struct{}{}
		}
	}
	return values
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
	if form.ID == 0 && !server.requireDatabaseOrTableWrite(w, r, actorID, form.DatabaseName) {
		return
	}
	if form.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeForm, form.ID) {
		return
	}
	if form.ID != 0 && !server.requireExistingFormDatabase(w, r, form.ID, form.DatabaseName) {
		return
	}
	saved, err := server.system.SaveForm(r.Context(), form)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := server.saveFormScriptFile(r.Context(), saved); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if form.ID == 0 {
		if !server.grantResourceOwner(w, r, actorID, permission.ScopeForm, saved.ID) {
			return
		}
	}
	saved = server.formWithPermissionLevel(r.Context(), actorID, saved)
	writeJSON(w, http.StatusCreated, saved)
}

func (server *Server) handlePostFormAction(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseFormActionPath(r.URL.Path)
	if !ok || action != "publish" {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireResourceWrite(w, r, actorID, permission.ScopeForm, id) {
		return
	}
	form, err := server.system.PublishForm(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	form, err = server.formDefinitionWithFileScript(r.Context(), form)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	form = server.formWithPermissionLevel(r.Context(), actorID, form)
	writeJSON(w, http.StatusOK, form)
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
	form, err = server.formDefinitionWithFileScript(r.Context(), form)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	form = server.formWithPermissionLevel(r.Context(), actorID, form)
	writeJSON(w, http.StatusOK, form)
}

func (server *Server) handleGetPublishedForm(w http.ResponseWriter, r *http.Request) {
	token, ok := parsePublishedFormPath(r.URL.Path, false)
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	form, err := server.system.FormByPublishedToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if !server.requireExplicitResourceRead(w, r, actorID, permission.ScopeForm, form.ID) {
		return
	}
	form, err = server.formDefinitionWithFileScript(r.Context(), form)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	form = server.formWithPermissionLevel(r.Context(), actorID, form)
	writeJSON(w, http.StatusOK, form)
}

func (server *Server) handleSubmitPublishedForm(w http.ResponseWriter, r *http.Request) {
	token, ok := parsePublishedFormPath(r.URL.Path, true)
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	form, err := server.system.FormByPublishedToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if !server.requireExplicitResourceRead(w, r, actorID, permission.ScopeForm, form.ID) {
		return
	}
	var request publishedFormSubmitRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	form, err = server.formDefinitionWithFileScript(r.Context(), form)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	definition, err := formruntime.Evaluate(form.Script)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := server.catalogSnapshot().Table(form.DatabaseName, definition.Table); !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("table %s.%s not found", form.DatabaseName, definition.Table))
		return
	}
	rowValues := make(map[string]any, len(definition.Fields))
	for inputID, fieldName := range definition.Fields {
		rowValues[fieldName] = request.Values[inputID]
	}
	formSubmitPerms := permission.New(permission.Grant{
		SubjectID: actorID,
		Scope:     permission.ScopeTable,
		Resource:  form.DatabaseName + "." + definition.Table,
		Level:     permission.Write,
	})
	row, err := server.tables.CreateRow(r.Context(), server.catalogSnapshot(), formSubmitPerms, actorID, form.DatabaseName, definition.Table, rowValues)
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

func parseTableRowsPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "tables" || parts[4] != "rows" {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func parseFormActionPath(path string) (int64, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "forms" {
		return 0, "", false
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return id, parts[3], true
}

func parsePublishedFormPath(path string, submit bool) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	expectedLen := 4
	if submit {
		expectedLen = 5
	}
	if len(parts) != expectedLen || parts[0] != "api" || parts[1] != "published" || parts[2] != "forms" {
		return "", false
	}
	if submit && parts[4] != "submit" {
		return "", false
	}
	token, err := url.PathUnescape(parts[3])
	if err != nil || token == "" {
		return "", false
	}
	return token, true
}

func parseOIDCPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "auth" || parts[2] != "oidc" {
		return "", "", false
	}
	providerName, err := url.PathUnescape(parts[3])
	if err != nil || providerName == "" {
		return "", "", false
	}
	return providerName, parts[4], true
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
	if parts[3] != "tables" && parts[3] != "workflows" && parts[3] != "forms" && parts[3] != "roles" {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func parseDatabaseTablePath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "databases" || parts[3] != "tables" {
		return "", "", false
	}
	if parts[2] == "" || parts[4] == "" {
		return "", "", false
	}
	return parts[2], parts[4], true
}

func parseRoleActionPath(path string) (string, string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 6 || parts[0] != "api" || parts[1] != "databases" || parts[3] != "roles" {
		return "", "", "", false
	}
	if parts[5] != "grants" && parts[5] != "members" {
		return "", "", "", false
	}
	roleName, err := url.PathUnescape(parts[4])
	if err != nil || roleName == "" {
		return "", "", "", false
	}
	return parts[2], roleName, parts[5], true
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

func (server *Server) catalogSnapshot() metadata.Catalog {
	server.catalogMu.RLock()
	defer server.catalogMu.RUnlock()
	return server.catalog
}

func (server *Server) visibleCatalog(ctx context.Context, actorID string, perms permission.Set) (metadata.Catalog, error) {
	catalog := server.catalogSnapshot()
	visible := metadata.Catalog{Databases: []metadata.Database{}}
	for _, database := range catalog.Databases {
		dbVisible := perms.ResourceLevel(actorID, permission.ScopeDatabase, database.Name) >= permission.Read
		dbWritable := perms.ResourceLevel(actorID, permission.ScopeDatabase, database.Name) >= permission.Write
		tables := make([]metadata.Table, 0, len(database.Tables))
		for _, tableMeta := range database.Tables {
			if dbWritable || canSeeTableMetadata(perms, actorID, database.Name, tableMeta) {
				tables = append(tables, visibleTableMetadata(perms, actorID, database.Name, tableMeta))
				dbVisible = true
			}
		}
		if !dbVisible {
			resourceVisible, err := server.hasVisibleDatabaseResource(ctx, actorID, perms, database.Name)
			if err != nil {
				return metadata.Catalog{}, err
			}
			dbVisible = resourceVisible
		}
		if dbVisible {
			database.Tables = tables
			visible.Databases = append(visible.Databases, database)
		}
	}
	return visible, nil
}

func visibleTableMetadata(perms permission.Set, actorID, dbName string, tableMeta metadata.Table) metadata.Table {
	resource := dbName + "." + tableMeta.Name
	dbLevel := perms.ResourceLevel(actorID, permission.ScopeDatabase, dbName)
	tableLevel := perms.ResourceLevel(actorID, permission.ScopeTable, resource)
	annotated := tableMeta
	annotated.PermissionLevel = int(maxPermissionLevel(dbLevel, tableLevel))
	if dbLevel >= permission.Write {
		annotated.Fields = annotateFieldPermissionLevels(perms, actorID, resource, dbLevel, annotated.Fields)
		return annotated
	}
	visible := annotated
	visible.Fields = make([]metadata.Field, 0, len(tableMeta.Fields))
	for _, field := range tableMeta.ActiveFields() {
		fieldLevel := perms.FieldLevel(actorID, resource, field.Name)
		if fieldLevel >= permission.Read {
			field.PermissionLevel = int(fieldLevel)
			visible.Fields = append(visible.Fields, field)
		}
	}
	visible.Views = make([]metadata.View, 0, len(tableMeta.Views))
	for _, view := range tableMeta.Views {
		resolved, err := tableMeta.ResolveView(view.Name)
		if err != nil {
			continue
		}
		if viewFieldsReadable(perms, actorID, resource, resolved.Filters, resolved.Sorts) {
			visible.Views = append(visible.Views, view)
		}
	}
	return visible
}

func annotateFieldPermissionLevels(perms permission.Set, actorID, resource string, dbLevel permission.Level, fields []metadata.Field) []metadata.Field {
	annotated := make([]metadata.Field, 0, len(fields))
	for _, field := range fields {
		field.PermissionLevel = int(maxPermissionLevel(dbLevel, perms.FieldLevel(actorID, resource, field.Name)))
		annotated = append(annotated, field)
	}
	return annotated
}

func maxPermissionLevel(left, right permission.Level) permission.Level {
	if left > right {
		return left
	}
	return right
}

func viewFieldsReadable(perms permission.Set, actorID, resource string, filters []metadata.ViewFilter, sorts []metadata.ViewSort) bool {
	for _, filter := range filters {
		if !perms.CanReadField(actorID, resource, filter.Field) {
			return false
		}
	}
	for _, sortDef := range sorts {
		if !perms.CanReadField(actorID, resource, sortDef.Field) {
			return false
		}
	}
	return true
}

func canSeeTableMetadata(perms permission.Set, actorID, dbName string, tableMeta metadata.Table) bool {
	resource := dbName + "." + tableMeta.Name
	if perms.ResourceLevel(actorID, permission.ScopeTable, resource) >= permission.Read {
		return true
	}
	for _, field := range tableMeta.ActiveFields() {
		if perms.FieldLevel(actorID, resource, field.Name) >= permission.Read {
			return true
		}
	}
	return false
}

func (server *Server) hasVisibleDatabaseResource(ctx context.Context, actorID string, perms permission.Set, dbName string) (bool, error) {
	workflows, err := server.system.Workflows(ctx, dbName)
	if err != nil {
		return false, err
	}
	for _, workflow := range workflows {
		if perms.CanReadResource(actorID, permission.ScopeWorkflow, resourceID(workflow.ID)) {
			return true, nil
		}
	}
	forms, err := server.system.Forms(ctx, dbName)
	if err != nil {
		return false, err
	}
	for _, form := range forms {
		if perms.CanReadResource(actorID, permission.ScopeForm, resourceID(form.ID)) {
			return true, nil
		}
	}
	return false, nil
}

func (server *Server) createDatabase(ctx context.Context, database metadata.Database) error {
	if server.metadataPath == "" {
		return errors.New("metadata writes are not configured")
	}
	server.catalogMu.Lock()
	defer server.catalogMu.Unlock()
	next, err := server.catalog.AddDatabase(database)
	if err != nil {
		return err
	}
	if server.openDatabase != nil {
		if err := server.openDatabase(ctx, database.Name, database.SQLitePath); err != nil {
			return err
		}
	}
	if err := metadata.Save(server.metadataPath, next); err != nil {
		return err
	}
	server.catalog = next
	return nil
}

func (server *Server) addTable(dbName string, tableMeta metadata.Table) error {
	if server.metadataPath == "" {
		return errors.New("metadata writes are not configured")
	}
	server.catalogMu.Lock()
	defer server.catalogMu.Unlock()
	next, err := server.catalog.AddTable(dbName, tableMeta)
	if err != nil {
		return err
	}
	if err := metadata.Save(server.metadataPath, next); err != nil {
		return err
	}
	server.catalog = next
	return nil
}

func (server *Server) updateTable(dbName, tableName string, tableMeta metadata.Table) error {
	if server.metadataPath == "" {
		return errors.New("metadata writes are not configured")
	}
	server.catalogMu.Lock()
	defer server.catalogMu.Unlock()
	next, err := server.catalog.UpdateTable(dbName, tableName, tableMeta)
	if err != nil {
		return err
	}
	if err := metadata.Save(server.metadataPath, next); err != nil {
		return err
	}
	server.catalog = next
	return nil
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

func (server *Server) requireExplicitResourceRead(w http.ResponseWriter, r *http.Request, actorID string, scope permission.Scope, id int64) bool {
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return false
	}
	if perms.ResourceLevel(actorID, scope, resourceID(id)) < permission.Read {
		writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
		return false
	}
	return true
}

func (server *Server) requireDatabaseWrite(w http.ResponseWriter, r *http.Request, actorID string, dbName string) bool {
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return false
	}
	if perms.ResourceLevel(actorID, permission.ScopeDatabase, dbName) < permission.Write {
		writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
		return false
	}
	return true
}

func (server *Server) grantDatabaseName(ctx context.Context, grant permission.Grant) (string, error) {
	switch grant.Scope {
	case permission.ScopeDatabase:
		if grant.Resource == "" {
			return "", errors.New("grant database resource is required")
		}
		return grant.Resource, nil
	case permission.ScopeTable, permission.ScopeField:
		dbName, _, ok := strings.Cut(grant.Resource, ".")
		if !ok || dbName == "" {
			return "", fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		return dbName, nil
	case permission.ScopeWorkflow:
		id, err := strconv.ParseInt(grant.Resource, 10, 64)
		if err != nil {
			return "", fmt.Errorf("grant workflow resource %q must be an id", grant.Resource)
		}
		workflow, err := server.system.Workflow(ctx, id)
		if err != nil {
			return "", err
		}
		return workflow.DatabaseName, nil
	case permission.ScopeForm:
		id, err := strconv.ParseInt(grant.Resource, 10, 64)
		if err != nil {
			return "", fmt.Errorf("grant form resource %q must be an id", grant.Resource)
		}
		form, err := server.system.Form(ctx, id)
		if err != nil {
			return "", err
		}
		return form.DatabaseName, nil
	default:
		return "", fmt.Errorf("unsupported grant scope %q", grant.Scope)
	}
}

func (server *Server) validateRoleGrants(ctx context.Context, dbName string, grants []permission.Grant) error {
	for _, grant := range grants {
		if grant.Level == permission.None && grant.Scope != permission.ScopeField {
			continue
		}
		grantDBName, err := server.grantDatabaseName(ctx, grant)
		if err != nil {
			return err
		}
		if grantDBName != dbName {
			return fmt.Errorf("grant resource %q belongs to database %q, not %q", grant.Resource, grantDBName, dbName)
		}
		if err := server.validateGrantResource(ctx, dbName, grant); err != nil {
			return err
		}
	}
	return nil
}

func (server *Server) validateGrantResource(ctx context.Context, dbName string, grant permission.Grant) error {
	switch grant.Scope {
	case permission.ScopeDatabase:
		if grant.Resource != dbName {
			return fmt.Errorf("database grant resource must be %q", dbName)
		}
		if _, ok := server.catalogSnapshot().Database(dbName); !ok {
			return fmt.Errorf("database %q not found", dbName)
		}
	case permission.ScopeTable:
		_, tableName, ok := strings.Cut(grant.Resource, ".")
		if !ok || tableName == "" {
			return fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		if _, ok := server.catalogSnapshot().Table(dbName, tableName); !ok {
			return fmt.Errorf("table %s.%s not found", dbName, tableName)
		}
	case permission.ScopeField:
		_, tableName, ok := strings.Cut(grant.Resource, ".")
		if !ok || tableName == "" {
			return fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		tableMeta, ok := server.catalogSnapshot().Table(dbName, tableName)
		if !ok {
			return fmt.Errorf("table %s.%s not found", dbName, tableName)
		}
		if grant.Field == "" {
			return errors.New("field grant requires field")
		}
		field, ok := tableMeta.Field(grant.Field)
		if !ok || field.Deleted || field.Name == "record_id" {
			return fmt.Errorf("field %s.%s.%s not found", dbName, tableName, grant.Field)
		}
	case permission.ScopeWorkflow:
		workflow, err := workflowByGrantResource(ctx, server.system, grant.Resource)
		if err != nil {
			return err
		}
		if workflow.DatabaseName != dbName {
			return fmt.Errorf("workflow %s belongs to database %q, not %q", grant.Resource, workflow.DatabaseName, dbName)
		}
	case permission.ScopeForm:
		form, err := formByGrantResource(ctx, server.system, grant.Resource)
		if err != nil {
			return err
		}
		if form.DatabaseName != dbName {
			return fmt.Errorf("form %s belongs to database %q, not %q", grant.Resource, form.DatabaseName, dbName)
		}
	default:
		return fmt.Errorf("unsupported grant scope %q", grant.Scope)
	}
	return nil
}

func workflowByGrantResource(ctx context.Context, system *systemdb.DB, resource string) (systemdb.WorkflowDefinition, error) {
	id, err := strconv.ParseInt(resource, 10, 64)
	if err != nil {
		return systemdb.WorkflowDefinition{}, fmt.Errorf("grant workflow resource %q must be an id", resource)
	}
	return system.Workflow(ctx, id)
}

func formByGrantResource(ctx context.Context, system *systemdb.DB, resource string) (systemdb.FormDefinition, error) {
	id, err := strconv.ParseInt(resource, 10, 64)
	if err != nil {
		return systemdb.FormDefinition{}, fmt.Errorf("grant form resource %q must be an id", resource)
	}
	return system.Form(ctx, id)
}

func (server *Server) requireDatabaseOrTableWrite(w http.ResponseWriter, r *http.Request, actorID string, dbName string) bool {
	if dbName == "" {
		writeError(w, http.StatusBadRequest, errors.New("database_name is required"))
		return false
	}
	dbMeta, ok := server.catalogSnapshot().Database(dbName)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("database %q not found", dbName))
		return false
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return false
	}
	if perms.ResourceLevel(actorID, permission.ScopeDatabase, dbName) >= permission.Write {
		return true
	}
	for _, tableMeta := range dbMeta.Tables {
		if perms.ResourceLevel(actorID, permission.ScopeTable, dbName+"."+tableMeta.Name) >= permission.Write {
			return true
		}
	}
	writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
	return false
}

func (server *Server) requireDatabaseOrSpecificTableWrite(w http.ResponseWriter, r *http.Request, actorID string, dbName string, tableName string) bool {
	if dbName == "" || tableName == "" {
		writeError(w, http.StatusBadRequest, errors.New("database and table are required"))
		return false
	}
	if _, ok := server.catalogSnapshot().Table(dbName, tableName); !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("table %s.%s not found", dbName, tableName))
		return false
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return false
	}
	if perms.ResourceLevel(actorID, permission.ScopeDatabase, dbName) >= permission.Write {
		return true
	}
	if perms.ResourceLevel(actorID, permission.ScopeTable, dbName+"."+tableName) >= permission.Write {
		return true
	}
	writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
	return false
}

func (server *Server) requireResourceWrite(w http.ResponseWriter, r *http.Request, actorID string, scope permission.Scope, id int64) bool {
	return server.requireResourceLevel(w, r, actorID, scope, id, permission.Write)
}

func (server *Server) requireExistingWorkflowDatabase(w http.ResponseWriter, r *http.Request, id int64, dbName string) bool {
	if dbName == "" {
		writeError(w, http.StatusBadRequest, errors.New("database_name is required"))
		return false
	}
	workflowDefinition, err := server.system.Workflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return false
	}
	if workflowDefinition.DatabaseName != dbName {
		writeError(w, http.StatusBadRequest, fmt.Errorf("workflow %d belongs to database %q, not %q", id, workflowDefinition.DatabaseName, dbName))
		return false
	}
	return true
}

func (server *Server) requireExistingFormDatabase(w http.ResponseWriter, r *http.Request, id int64, dbName string) bool {
	if dbName == "" {
		writeError(w, http.StatusBadRequest, errors.New("database_name is required"))
		return false
	}
	form, err := server.system.Form(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return false
	}
	if form.DatabaseName != dbName {
		writeError(w, http.StatusBadRequest, fmt.Errorf("form %d belongs to database %q, not %q", id, form.DatabaseName, dbName))
		return false
	}
	return true
}

func (server *Server) requireResourceLevel(w http.ResponseWriter, r *http.Request, actorID string, scope permission.Scope, id int64, level permission.Level) bool {
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return false
	}
	if server.canManageDatabaseResource(r.Context(), perms, actorID, scope, id) {
		return true
	}
	if perms.ResourceLevel(actorID, scope, resourceID(id)) < level {
		writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
		return false
	}
	return true
}

func (server *Server) canManageDatabaseResource(ctx context.Context, perms permission.Set, actorID string, scope permission.Scope, id int64) bool {
	var dbName string
	switch scope {
	case permission.ScopeWorkflow:
		workflow, err := server.system.Workflow(ctx, id)
		if err != nil {
			return false
		}
		dbName = workflow.DatabaseName
	case permission.ScopeForm:
		form, err := server.system.Form(ctx, id)
		if err != nil {
			return false
		}
		dbName = form.DatabaseName
	default:
		return false
	}
	return perms.ResourceLevel(actorID, permission.ScopeDatabase, dbName) >= permission.Write
}

func (server *Server) filterReadableWorkflows(ctx context.Context, actorID string, workflows []systemdb.WorkflowDefinition) ([]systemdb.WorkflowDefinition, error) {
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return nil, err
	}
	filtered := make([]systemdb.WorkflowDefinition, 0, len(workflows))
	for _, workflow := range workflows {
		if perms.ResourceLevel(actorID, permission.ScopeDatabase, workflow.DatabaseName) >= permission.Write ||
			perms.CanReadResource(actorID, permission.ScopeWorkflow, resourceID(workflow.ID)) {
			workflow.PermissionLevel = workflowPermissionLevel(perms, actorID, workflow)
			filtered = append(filtered, workflow)
		}
	}
	return filtered, nil
}

func (server *Server) filterReadableForms(ctx context.Context, actorID string, forms []systemdb.FormDefinition) ([]systemdb.FormDefinition, error) {
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return nil, err
	}
	filtered := make([]systemdb.FormDefinition, 0, len(forms))
	for _, form := range forms {
		if perms.ResourceLevel(actorID, permission.ScopeDatabase, form.DatabaseName) >= permission.Write ||
			perms.CanReadResource(actorID, permission.ScopeForm, resourceID(form.ID)) {
			form.PermissionLevel = formPermissionLevel(perms, actorID, form)
			filtered = append(filtered, form)
		}
	}
	return filtered, nil
}

func (server *Server) workflowWithPermissionLevel(ctx context.Context, actorID string, workflow systemdb.WorkflowDefinition) systemdb.WorkflowDefinition {
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return workflow
	}
	workflow.PermissionLevel = workflowPermissionLevel(perms, actorID, workflow)
	return workflow
}

func (server *Server) formWithPermissionLevel(ctx context.Context, actorID string, form systemdb.FormDefinition) systemdb.FormDefinition {
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return form
	}
	form.PermissionLevel = formPermissionLevel(perms, actorID, form)
	return form
}

func workflowPermissionLevel(perms permission.Set, actorID string, workflow systemdb.WorkflowDefinition) permission.Level {
	if perms.ResourceLevel(actorID, permission.ScopeDatabase, workflow.DatabaseName) >= permission.Write {
		return permission.Write
	}
	return perms.ResourceLevel(actorID, permission.ScopeWorkflow, resourceID(workflow.ID))
}

func formPermissionLevel(perms permission.Set, actorID string, form systemdb.FormDefinition) permission.Level {
	if perms.ResourceLevel(actorID, permission.ScopeDatabase, form.DatabaseName) >= permission.Write {
		return permission.Write
	}
	return perms.ResourceLevel(actorID, permission.ScopeForm, resourceID(form.ID))
}

func canReadRowHistory(perms permission.Set, actorID, resource string, tableMeta metadata.Table) bool {
	if perms.CanReadField(actorID, resource, "record_id") {
		return true
	}
	for _, field := range tableMeta.ActiveFields() {
		if perms.CanReadField(actorID, resource, field.Name) {
			return true
		}
	}
	return false
}

func readableHistoryValues(values map[string]any, perms permission.Set, actorID, resource string, tableMeta metadata.Table) map[string]any {
	readable := map[string]any{}
	for fieldName, value := range values {
		field, ok := tableMeta.Field(fieldName)
		if !ok || field.Deleted {
			continue
		}
		if perms.CanReadField(actorID, resource, fieldName) {
			readable[fieldName] = value
		}
	}
	return readable
}

func (server *Server) saveWorkflowScriptFile(ctx context.Context, workflow systemdb.WorkflowDefinition) error {
	if server.codeFiles == nil {
		return nil
	}
	return server.codeFiles.SaveWorkflowScript(ctx, workflow)
}

func (server *Server) saveFormScriptFile(ctx context.Context, form systemdb.FormDefinition) error {
	if server.codeFiles == nil {
		return nil
	}
	return server.codeFiles.SaveFormScript(ctx, form)
}

func (server *Server) workflowDefinitionsWithFileScripts(ctx context.Context, workflows []systemdb.WorkflowDefinition) ([]systemdb.WorkflowDefinition, error) {
	if server.codeFiles == nil {
		return workflows, nil
	}
	loaded := make([]systemdb.WorkflowDefinition, 0, len(workflows))
	for _, workflow := range workflows {
		updated, err := server.workflowDefinitionWithFileScript(ctx, workflow)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, updated)
	}
	return loaded, nil
}

func (server *Server) workflowDefinitionWithFileScript(ctx context.Context, workflow systemdb.WorkflowDefinition) (systemdb.WorkflowDefinition, error) {
	if server.codeFiles == nil {
		return workflow, nil
	}
	script, ok, err := server.codeFiles.LoadWorkflowScript(ctx, workflow)
	if err != nil {
		return systemdb.WorkflowDefinition{}, err
	}
	if ok {
		workflow.Script = script
	}
	return workflow, nil
}

func workflowResponsesFromDefinitions(workflows []systemdb.WorkflowDefinition) []workflowDefinitionResponse {
	responses := make([]workflowDefinitionResponse, 0, len(workflows))
	for _, workflow := range workflows {
		responses = append(responses, workflowResponseFromDefinition(workflow))
	}
	return responses
}

func workflowResponseFromDefinition(workflow systemdb.WorkflowDefinition) workflowDefinitionResponse {
	return workflowDefinitionResponse{
		ID:              workflow.ID,
		DatabaseName:    workflow.DatabaseName,
		Name:            workflow.Name,
		Script:          workflow.Script,
		CreatorID:       workflow.CreatorID,
		Secrets:         secretLengths(workflow.Secrets),
		Variables:       workflow.Variables,
		PermissionLevel: workflow.PermissionLevel,
		CreatedAt:       workflow.CreatedAt,
		UpdatedAt:       workflow.UpdatedAt,
	}
}

func secretLengths(secrets map[string]string) map[string]int {
	lengths := map[string]int{}
	for key, value := range secrets {
		lengths[key] = len(value)
	}
	return lengths
}

func (server *Server) formDefinitionsWithFileScripts(ctx context.Context, forms []systemdb.FormDefinition) ([]systemdb.FormDefinition, error) {
	if server.codeFiles == nil {
		return forms, nil
	}
	loaded := make([]systemdb.FormDefinition, 0, len(forms))
	for _, form := range forms {
		updated, err := server.formDefinitionWithFileScript(ctx, form)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, updated)
	}
	return loaded, nil
}

func (server *Server) formDefinitionWithFileScript(ctx context.Context, form systemdb.FormDefinition) (systemdb.FormDefinition, error) {
	if server.codeFiles == nil {
		return form, nil
	}
	script, ok, err := server.codeFiles.LoadFormScript(ctx, form)
	if err != nil {
		return systemdb.FormDefinition{}, err
	}
	if ok {
		form.Script = script
	}
	return form, nil
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

func (server *Server) oidcProvider(name string) (config.OIDCProvider, bool) {
	for _, provider := range server.oidc {
		if provider.Name == name {
			return provider, true
		}
	}
	return config.OIDCProvider{}, false
}

func oidcScopes(provider config.OIDCProvider) []string {
	if len(provider.Scopes) == 0 {
		return []string{"openid", "email", "profile"}
	}
	scopes := append([]string(nil), provider.Scopes...)
	for _, scope := range scopes {
		if scope == "openid" {
			return scopes
		}
	}
	return append([]string{"openid"}, scopes...)
}

func setOIDCStateCookie(w http.ResponseWriter, providerName, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    providerName + ":" + state,
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(oidcStateTTL),
	})
}

func clearOIDCStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    "",
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	})
}

func oidcAuthorizeURL(r *http.Request, provider config.OIDCProvider, state string) (string, error) {
	issuerURL := strings.TrimRight(provider.IssuerURL, "/")
	if issuerURL == "" {
		return "", errors.New("oidc issuer_url is required")
	}
	authorizeURL, err := url.Parse(issuerURL + "/authorize")
	if err != nil {
		return "", err
	}
	query := authorizeURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", provider.ClientID)
	query.Set("redirect_uri", oidcCallbackURL(r, provider.Name))
	query.Set("scope", strings.Join(oidcScopes(provider), " "))
	query.Set("state", state)
	authorizeURL.RawQuery = query.Encode()
	return authorizeURL.String(), nil
}

func oidcCallbackURL(r *http.Request, providerName string) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return (&url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/api/auth/oidc/" + url.PathEscape(providerName) + "/callback",
	}).String()
}

func oidcClaims(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, idToken *oidc.IDToken) (oidcEmailClaims, error) {
	var claims oidcEmailClaims
	if err := idToken.Claims(&claims); err != nil {
		return oidcEmailClaims{}, err
	}
	if claims.Email != "" {
		return claims, nil
	}
	userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		return oidcEmailClaims{}, err
	}
	if err := userInfo.Claims(&claims); err != nil {
		return oidcEmailClaims{}, err
	}
	if claims.Email == "" {
		return oidcEmailClaims{}, errors.New("oidc email is required")
	}
	return claims, nil
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
