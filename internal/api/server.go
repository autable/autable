package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"autable/internal/auth"
	"autable/internal/config"
	"autable/internal/history"
	"autable/internal/metadata"
	"autable/internal/permission"
	"autable/internal/repositorysync"
	"autable/internal/runnerhub"
	"autable/internal/systemdb"
	"autable/internal/table"
	"autable/internal/workflow"
	"autable/internal/workflow/nodes"
)

type Server struct {
	catalogMu        sync.RWMutex
	catalog          metadata.Catalog
	metadataPath     string
	repositoryPath   string
	openDatabase     func(context.Context, string) error
	codeFiles        codeFileStore
	repositorySync   repositorySyncer
	ai               aiClient
	aiEnabled        bool
	system           *systemdb.DB
	tables           *table.Service
	history          history.Store
	runner           *workflow.Runner
	runnerHub        *runnerhub.Hub
	files            FileStore
	fileUploadLimit  int64
	auth             config.AuthConfig
	publicURL        string
	workflowWorkers    map[string]*workflowEventWorker
	workflowRunWorkers map[int64]*workflowRunWorker
	workflowWorker     context.Context
	workflowWorkerMu   sync.Mutex
	mux              *http.ServeMux
}

type codeFileStore interface {
	SaveWorkflowScript(context.Context, systemdb.WorkflowDefinition) error
	LoadWorkflowScript(context.Context, systemdb.WorkflowDefinition) (string, bool, error)
	DeleteWorkflowScript(context.Context, systemdb.WorkflowDefinition) error
	SaveFormScript(context.Context, systemdb.FormDefinition) error
	LoadFormScript(context.Context, systemdb.FormDefinition) (string, bool, error)
	DeleteFormScript(context.Context, systemdb.FormDefinition) error
	WorkflowScriptPath(systemdb.WorkflowDefinition) string
	FormScriptPath(systemdb.FormDefinition) string
}

type repositorySyncer interface {
	Notify(repositorysync.Change)
}

// FileStore persists uploaded file contents; metadata lives in systemdb.
type FileStore interface {
	Put(ctx context.Context, id int64, name string, contentType string, size int64, body io.Reader) error
	Get(ctx context.Context, id int64, name string) (io.ReadCloser, error)
}

// SetFileStore enables the file upload and download endpoints.
func (server *Server) SetFileStore(files FileStore) {
	server.files = files
}

// SetFileUploadLimit caps a single uploaded file in bytes.
func (server *Server) SetFileUploadLimit(limit int64) {
	if limit > 0 {
		server.fileUploadLimit = limit
	}
}

func (server *Server) uploadLimit() int64 {
	if server.fileUploadLimit > 0 {
		return server.fileUploadLimit
	}
	return defaultFileUploadBytes
}

type createDatabaseRequest struct {
	Name string `json:"name"`
}

type createRowRequest struct {
	Values map[string]any `json:"values"`
}

type upsertRowRequest struct {
	MatchField string         `json:"match_field"`
	Values     map[string]any `json:"values"`
}

type listRowsRequest struct {
	View   string              `json:"view,omitempty"`
	Query  *metadata.ViewQuery `json:"query,omitempty"`
	Sorts  []metadata.ViewSort `json:"sorts,omitempty"`
	Limit  int                 `json:"limit,omitempty"`
	Offset int                 `json:"offset,omitempty"`
	Search string              `json:"search,omitempty"`
}

type rowResponse struct {
	RecordID int64          `json:"record_id"`
	Values   map[string]any `json:"values"`
}

type rowPageResponse struct {
	Rows  []rowResponse `json:"rows"`
	Total int64         `json:"total"`
}

type rowMutationResponse struct {
	Operation string         `json:"operation"`
	RecordID  int64          `json:"record_id"`
	Values    map[string]any `json:"values"`
}

type rowHistoryResponse struct {
	HistoryKey string `json:"history_key"`
	history.RowChange
}

type authRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

type userResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Provider    string `json:"provider"`
}

type oidcProviderResponse struct {
	Name      string   `json:"name"`
	IssuerURL string   `json:"issuer_url"`
	Scopes    []string `json:"scopes"`
}

type authConfigResponse struct {
	PasswordEnabled bool                   `json:"password_enabled"`
	OIDCEnabled     bool                   `json:"oidc_enabled"`
	OIDCProviders   []oidcProviderResponse `json:"oidc_providers"`
	AIEnabled       bool                   `json:"ai_enabled"`
}

type oidcEmailClaims struct {
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified,omitempty"`
	Name          string `json:"name,omitempty"`
}

type workflowRunRequest struct {
	Inputs map[string]any `json:"inputs"`
}

type fieldPositionRequest struct {
	Position string `json:"position,omitempty"`
	Before   string `json:"before,omitempty"`
	After    string `json:"after,omitempty"`
}

type workflowRunResponse struct {
	HistoryKey string              `json:"history_key"`
	Run        history.WorkflowRun `json:"run"`
	Summary    bool                `json:"summary,omitempty"`
}

const (
	defaultWorkflowRunListLimit = 100
	maxWorkflowRunListLimit     = 500
)

type workflowDefinitionResponse struct {
	ID              int64             `json:"id"`
	DatabaseName    string            `json:"database_name"`
	Name            string            `json:"name"`
	Script          string            `json:"script"`
	Enabled         bool              `json:"enabled"`
	CreatorID       string            `json:"creator_id,omitempty"`
	Secrets              map[string]int    `json:"secrets"`
	Variables            map[string]string `json:"variables"`
	Runners              map[string]string `json:"runners"`
	HistoryRetentionDays *int64            `json:"history_retention_days"`
	TimeoutSeconds       *int64            `json:"timeout_seconds"`
	PermissionLevel      permission.Level  `json:"permission_level,omitempty"`
	CreatedAt            int64             `json:"created_at"`
	UpdatedAt            int64             `json:"updated_at"`
}

type roleDefinitionResponse struct {
	ID              int64                        `json:"id"`
	DatabaseName    string                       `json:"database_name"`
	Name            string                       `json:"name"`
	SubjectID       string                       `json:"subject_id"`
	Grants          []permission.Grant           `json:"grants"`
	Members         []systemdb.RoleMember        `json:"members"`
	MemberUsers     []userResponse               `json:"member_users"`
	MemberWorkflows []workflowDefinitionResponse `json:"member_workflows"`
	CreatedAt       int64                        `json:"created_at"`
	UpdatedAt       int64                        `json:"updated_at"`
}

type workflowEventKind string

const (
	workflowEventRowChange workflowEventKind = "row_change"
	workflowEventSchedule  workflowEventKind = "schedule"
	workflowEventWebhook   workflowEventKind = "webhook"
)

type workflowEvent struct {
	Kind         workflowEventKind
	DatabaseName string
	HistoryKey   string
	RowChange    history.RowChange
	ScheduledAt  time.Time
	// WorkflowID targets webhook events at exactly one workflow.
	WorkflowID     int64
	WebhookToken   string
	WebhookPayload map[string]any
	ReceivedAt     int64
}

type workflowEventWorker struct {
	dbName string
	events chan workflowEvent
}

// workflowRunJob is one triggered execution handed from the per-database
// event matcher to the per-workflow run worker.
type workflowRunJob struct {
	definition  workflow.Definition
	inputs      map[string]any
	scheduledAt time.Time
	isSchedule  bool
}

// workflowRunWorker serializes runs of a single workflow: one workflow never
// runs concurrently with itself (upsert-style syncs would race), while
// different workflows run in parallel.
type workflowRunWorker struct {
	jobs chan workflowRunJob
}

type roleGrantsRequest struct {
	Grants []permission.Grant `json:"grants"`
}

type roleMembersRequest struct {
	Members []systemdb.RoleMember `json:"members"`
}

const (
	sessionCookieName   = "autable_session"
	oidcStateCookieName    = "autable_oidc_state"
	oidcRedirectCookieName = "autable_oidc_redirect"
	sessionTTL             = 14 * 24 * time.Hour
	oidcStateTTL           = 10 * time.Minute
)

func NewServer(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store) *Server {
	return NewServerWithWorkflowRunner(
		catalog,
		system,
		tables,
		historyStore,
		nil,
	)
}

func NewServerWithWorkflowRunner(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store, runner *workflow.Runner) *Server {
	return NewServerWithWorkflowRunnerAndAuth(catalog, system, tables, historyStore, runner, defaultServerAuthConfig())
}

func NewServerWithAuthConfig(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store, authConfig config.AuthConfig) *Server {
	return NewServerWithWorkflowRunnerAndAuth(
		catalog,
		system,
		tables,
		historyStore,
		nil,
		authConfig,
	)
}

func NewServerWithWorkflowRunnerAndAuth(catalog metadata.Catalog, system *systemdb.DB, tables *table.Service, historyStore history.Store, runner *workflow.Runner, authConfig config.AuthConfig) *Server {
	server := &Server{
		catalog: catalog,
		system:  system,
		tables:  tables,
		history: historyStore,
		runner:  runner,
		auth:    cloneAuthConfig(authConfig),
		mux:     http.NewServeMux(),
	}
	if runner == nil {
		runner = workflow.NewRunner(historyStore, nodes.All(nodes.Dependencies{
			History: historyStore,
			Autable: server.workflowAutableService(),
		})...)
	} else {
		for _, node := range nodes.AutableNodes(server.workflowAutableService()) {
			runner.Register(node)
		}
	}
	server.runner = runner
	server.runnerHub = runnerhub.New(func(ctx context.Context, token string) (string, bool, error) {
		return system.LookupRunnerToken(ctx, token)
	}, runnerhub.DefaultJobTimeout)
	server.runner.SetRemoteDispatcher(server.runnerHub)
	server.tables.SetRowChangeHandler(server.dispatchRowChangeEvent)
	server.tables.SetIdentityResolver(server.actorQueryIdentity)
	server.routes()
	return server
}

func (server *Server) EnableMetadataWrites(path string) {
	server.metadataPath = path
}

func (server *Server) SetRepositoryPath(path string) {
	server.repositoryPath = path
}

func (server *Server) SetDatabaseOpener(openDatabase func(context.Context, string) error) {
	server.openDatabase = openDatabase
}

func (server *Server) SetCodeFileStore(store codeFileStore) {
	server.codeFiles = store
}

func (server *Server) SetRepositorySync(syncer repositorySyncer) {
	server.repositorySync = syncer
}

func (server *Server) SetAIClient(client aiClient) {
	server.ai = client
	server.aiEnabled = client != nil
}

func (server *Server) SetPublicURL(publicURL string) {
	server.publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
}

func (server *Server) notifyRepositoryChange(ctx context.Context, actorID string, action string, summary string, paths ...string) {
	if server.repositorySync == nil {
		return
	}
	server.repositorySync.Notify(repositorysync.Change{
		ActorID:    actorID,
		ActorLabel: server.repositoryActorLabel(ctx, actorID),
		Action:     action,
		Summary:    summary,
		Paths:      paths,
	})
}

func (server *Server) repositoryActorLabel(ctx context.Context, actorID string) string {
	if actorID == "" || server.system == nil {
		return actorID
	}
	user, err := server.system.User(ctx, actorID)
	if err != nil {
		return actorID
	}
	switch {
	case user.DisplayName != "" && user.Email != "":
		return user.DisplayName + " <" + user.Email + ">"
	case user.Email != "":
		return user.Email
	case user.DisplayName != "":
		return user.DisplayName
	default:
		return actorID
	}
}

func (server *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	server.mux.ServeHTTP(w, r)
}

func (server *Server) routes() {
	server.mux.HandleFunc("GET /api/auth/config", server.handleAuthConfig)
	server.mux.HandleFunc("POST /api/auth/register", server.handleRegister)
	server.mux.HandleFunc("POST /api/auth/login", server.handleLogin)
	server.mux.HandleFunc("GET /api/auth/oidc/{provider}/start", server.handleOIDCStartRoute)
	server.mux.HandleFunc("GET /api/auth/oidc/{provider}/callback", server.handleOIDCCallbackRoute)
	server.mux.HandleFunc("GET /api/auth/me", server.handleMe)
	server.mux.HandleFunc("POST /api/auth/logout", server.handleLogout)
	server.mux.HandleFunc("GET /api/ai/auth/status", server.handleAIAuthStatus)
	server.mux.HandleFunc("POST /api/ai/auth/start", server.handleAIAuthStart)
	server.mux.HandleFunc("GET /api/ai/options", server.handleAIOptions)
	server.mux.HandleFunc("POST /api/ai/suggest-script", server.handleAISuggestScript)
	server.mux.HandleFunc("GET /api/users", server.handleUsers)
	server.mux.HandleFunc("GET /api/metadata", server.handleMetadata)
	server.mux.HandleFunc("POST /api/permissions/grants", server.handleSaveGrant)
	server.mux.HandleFunc("POST /api/tables/{database}/{table}/rows", server.tableRoute(server.handleCreateRow))
	server.mux.HandleFunc("POST /api/tables/{database}/{table}/rows/upsert", server.tableRoute(server.handleUpsertRow))
	server.mux.HandleFunc("POST /api/tables/{database}/{table}/rows/query", server.tableRoute(server.handleQueryRows))
	server.mux.HandleFunc("POST /api/tables/{database}/{table}/rows/page", server.tableRoute(server.handleQueryRowsPage))
	server.mux.HandleFunc("POST /api/tables/{database}/{table}/fields", server.tableRoute(server.handleCreateFields))
	server.mux.HandleFunc("PATCH /api/tables/{database}/{table}/rows/{recordID}", server.handleUpdateRow)
	server.mux.HandleFunc("DELETE /api/tables/{database}/{table}/rows/{recordID}", server.handleDeleteRow)
	server.mux.HandleFunc("GET /api/tables/{database}/{table}/rows", server.tableRoute(server.handleListRows))
	server.mux.HandleFunc("GET /api/tables/{database}/{table}/rows/{recordID}/history", server.handleRowHistory)
	server.mux.HandleFunc("POST /api/databases", server.handleCreateDatabase)
	server.mux.HandleFunc("POST /api/databases/{database}/workflows/{workflowID}/webhook", server.handleWorkflowWebhookRoute)
	server.mux.HandleFunc("GET /api/databases/{database}/{resource}", server.handleGetDatabaseResource)
	server.mux.HandleFunc("POST /api/databases/{database}/{resource}", server.handlePostDatabaseResource)
	server.mux.HandleFunc("PUT /api/databases/{database}/tables/{table}", server.handleUpdateTableMetadataRoute)
	server.mux.HandleFunc("PUT /api/databases/{database}/roles/{role}/{action}", server.handleRoleAction)
	server.mux.HandleFunc("PATCH /api/databases/{database}/tables/{table}/fields/{field}/position", server.handleMoveFieldPosition)
	server.mux.HandleFunc("GET /api/workflow/nodes", server.handleWorkflowNodes)
	server.mux.HandleFunc("GET /api/runner/ws", server.runnerHub.ServeWS)
	server.mux.HandleFunc("POST /api/workflows", server.handleSaveWorkflow)
	server.mux.HandleFunc("POST /api/workflows/{workflowID}/runs", server.handleRunWorkflow)
	server.mux.HandleFunc("GET /api/workflows/{workflowID}", server.handleGetWorkflow)
	server.mux.HandleFunc("GET /api/workflows/{workflowID}/runs", server.handleWorkflowRunsRoute)
	server.mux.HandleFunc("GET /api/workflows/{workflowID}/runs/{historyKey}", server.handleWorkflowRunRoute)
	server.mux.HandleFunc("DELETE /api/workflows/{workflowID}", server.handleDeleteWorkflow)
	server.mux.HandleFunc("POST /api/files", server.handleUploadFile)
	server.mux.HandleFunc("POST /api/files/metadata", server.handleFileMetadata)
	server.mux.HandleFunc("GET /api/files/{fileID}", server.handleDownloadFile)
	server.mux.HandleFunc("POST /api/forms", server.handleSaveForm)
	server.mux.HandleFunc("POST /api/forms/{formID}/{action}", server.handlePostFormAction)
	server.mux.HandleFunc("GET /api/forms/{formID}", server.handleGetForm)
	server.mux.HandleFunc("DELETE /api/forms/{formID}", server.handleDeleteForm)
	server.mux.HandleFunc("GET /api/published/forms/{token}", server.handleGetPublishedForm)
}

// tableRoute adapts a {database}/{table} handler to the wildcard mux.
func (server *Server) tableRoute(handler func(http.ResponseWriter, *http.Request, string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, r.PathValue("database"), r.PathValue("table"))
	}
}

// pathID parses a numeric wildcard segment; ids are always positive.
func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func (server *Server) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, authConfigResponse{
		PasswordEnabled: server.auth.Password.Enabled,
		OIDCEnabled:     server.auth.OIDC.Enabled,
		OIDCProviders:   server.publicOIDCProviders(),
		AIEnabled:       server.aiEnabled,
	})
}

func (server *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !server.auth.Password.Enabled {
		writeError(w, http.StatusNotFound, errors.New("password auth is disabled"))
		return
	}
	var request authRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:       request.Email,
		DisplayName: request.DisplayName,
		Password:    request.Password,
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
	if !server.auth.Password.Enabled {
		writeError(w, http.StatusNotFound, errors.New("password auth is disabled"))
		return
	}
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

func (server *Server) handleOIDCStartRoute(w http.ResponseWriter, r *http.Request) {
	provider, ok := server.routedOIDCProvider(w, r)
	if !ok {
		return
	}
	server.handleOIDCStart(w, r, provider)
}

func (server *Server) handleOIDCCallbackRoute(w http.ResponseWriter, r *http.Request) {
	provider, ok := server.routedOIDCProvider(w, r)
	if !ok {
		return
	}
	server.handleOIDCCallback(w, r, provider)
}

func (server *Server) routedOIDCProvider(w http.ResponseWriter, r *http.Request) (config.OIDCProvider, bool) {
	if !server.auth.OIDC.Enabled {
		http.NotFound(w, r)
		return config.OIDCProvider{}, false
	}
	providerName := r.PathValue("provider")
	provider, ok := server.oidcProvider(providerName)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("oidc provider %q not found", providerName))
		return config.OIDCProvider{}, false
	}
	return provider, true
}

func (server *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request, provider config.OIDCProvider) {
	state, err := auth.NewSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	setOIDCStateCookie(w, provider.Name, state)
	if redirect := safeLocalRedirect(r.URL.Query().Get("redirect")); redirect != "" {
		setOIDCRedirectCookie(w, redirect)
	}
	callbackURL, err := oidcCallbackURL(server.publicURL, provider.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	authURL, err := oidcAuthorizeURL(provider, state, callbackURL)
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
	callbackURL, err := oidcCallbackURL(server.publicURL, provider.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	oauthConfig := oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Endpoint:     oidcProvider.Endpoint(),
		RedirectURL:  callbackURL,
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
		DisplayName:  claims.Name,
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
	target := "/"
	if cookie, err := r.Cookie(oidcRedirectCookieName); err == nil {
		if decoded, err := url.QueryUnescape(cookie.Value); err == nil {
			if redirect := safeLocalRedirect(decoded); redirect != "" {
				target = redirect
			}
		}
	}
	clearOIDCRedirectCookie(w)
	http.Redirect(w, r, target, http.StatusFound)
}

// safeLocalRedirect keeps post-login redirects on this site: only rooted
// paths pass, anything absolute or protocol-relative is dropped.
func safeLocalRedirect(target string) string {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.Contains(target, "\\") {
		return ""
	}
	return target
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

func (server *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireUserID(w, r); !ok {
		return
	}
	users, err := server.system.SearchUsers(r.Context(), r.URL.Query().Get("query"), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response := make([]userResponse, 0, len(users))
	for _, user := range users {
		response = append(response, toUserResponse(user))
	}
	writeJSON(w, http.StatusOK, response)
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
	database := metadata.Database{Name: request.Name, Tables: []metadata.Table{}}
	if err := server.createDatabase(r.Context(), database); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := server.system.SaveDatabaseOwner(r.Context(), database.Name, actorID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.notifyRepositoryChange(r.Context(), actorID, "metadata.database.create", "created database "+database.Name, server.metadataPath)
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
	if grant.Level != permission.None {
		if err := server.validateGrantResource(r.Context(), dbName, grant); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if !server.requireDatabaseOwner(w, r, actorID, dbName) {
		return
	}
	if err := server.deleteConflictingGrants(r.Context(), grant); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := server.system.SaveGrant(r.Context(), grant); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, grant)
}

func (server *Server) handleCreateRow(w http.ResponseWriter, r *http.Request, dbName, tableName string) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var request createRowRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	row, err := server.createTableRowAs(r.Context(), actorID, dbName, tableName, request.Values)
	if err != nil {
		writeTableMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rowResponse{RecordID: row.RecordID, Values: row.Values})
}

func (server *Server) handleUpsertRow(w http.ResponseWriter, r *http.Request, dbName, tableName string) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var request upsertRowRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	row, operation, err := server.upsertTableRowAs(r.Context(), actorID, dbName, tableName, request.MatchField, request.Values)
	if err != nil {
		writeTableMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rowMutationResponse{Operation: operation, RecordID: row.RecordID, Values: row.Values})
}

func (server *Server) handleQueryRows(w http.ResponseWriter, r *http.Request, dbName, tableName string) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var request listRowsRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := server.listTableRowsAs(r.Context(), actorID, dbName, tableName, table.RowListOptions{
		ViewName: request.View,
		Query:    request.Query,
		Sorts:    request.Sorts,
		Limit:    request.Limit,
		Offset:   request.Offset,
		Search:   request.Search,
	})
	if err != nil {
		writeTableMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rowResponses(rows))
}

func (server *Server) handleQueryRowsPage(w http.ResponseWriter, r *http.Request, dbName, tableName string) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var request listRowsRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rows, total, err := server.listTableRowsPageAs(r.Context(), actorID, dbName, tableName, table.RowListOptions{
		ViewName: request.View,
		Query:    request.Query,
		Sorts:    request.Sorts,
		Limit:    request.Limit,
		Offset:   request.Offset,
		Search:   request.Search,
	})
	if err != nil {
		writeTableMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rowPageResponse{Rows: rowResponses(rows), Total: total})
}

func (server *Server) handleCreateFields(w http.ResponseWriter, r *http.Request, dbName, tableName string) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var request map[string]any
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	fields, err := workflowFieldsInput(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	mutation, err := server.createTableFieldsAs(r.Context(), actorID, dbName, tableName, fields)
	if err != nil {
		writeTableMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowFieldMutationResponse(mutation))
}

func (server *Server) createTableRowAs(ctx context.Context, actorID string, dbName string, tableName string, values map[string]any) (table.Row, error) {
	perms, isOwner, err := server.tablePermissions(ctx, actorID, dbName)
	if err != nil {
		return table.Row{}, err
	}
	return server.tables.CreateRow(ctx, server.catalogSnapshot(), perms, actorID, isOwner, dbName, tableName, values)
}

func (server *Server) updateTableRowAs(ctx context.Context, actorID string, dbName string, tableName string, recordID int64, values map[string]any) (table.Row, error) {
	perms, isOwner, err := server.tablePermissions(ctx, actorID, dbName)
	if err != nil {
		return table.Row{}, err
	}
	return server.tables.UpdateRow(ctx, server.catalogSnapshot(), perms, actorID, isOwner, dbName, tableName, recordID, values)
}

func (server *Server) deleteTableRowAs(ctx context.Context, actorID string, dbName string, tableName string, recordID int64) (table.Row, error) {
	perms, isOwner, err := server.tablePermissions(ctx, actorID, dbName)
	if err != nil {
		return table.Row{}, err
	}
	return server.tables.DeleteRow(ctx, server.catalogSnapshot(), perms, actorID, isOwner, dbName, tableName, recordID)
}

func (server *Server) listTableRowsAs(ctx context.Context, actorID string, dbName string, tableName string, options table.RowListOptions) ([]table.Row, error) {
	perms, isOwner, err := server.tablePermissions(ctx, actorID, dbName)
	if err != nil {
		return nil, err
	}
	return server.tables.RowsWithOptions(ctx, server.catalogSnapshot(), perms, actorID, isOwner, dbName, tableName, options)
}

func (server *Server) listTableRowsPageAs(ctx context.Context, actorID string, dbName string, tableName string, options table.RowListOptions) ([]table.Row, int64, error) {
	perms, isOwner, err := server.tablePermissions(ctx, actorID, dbName)
	if err != nil {
		return nil, 0, err
	}
	return server.tables.RowsPageWithOptions(ctx, server.catalogSnapshot(), perms, actorID, isOwner, dbName, tableName, options)
}

// actorQueryIdentity is the value $current_user resolves to in view
// queries: the user's email, or the raw subject id for non-user subjects
// (workflows, roles), which normally matches no rows.
func (server *Server) actorQueryIdentity(ctx context.Context, actorID string) string {
	user, err := server.system.User(ctx, actorID)
	if err == nil && user.Email != "" {
		return user.Email
	}
	return actorID
}

func (server *Server) upsertTableRowAs(ctx context.Context, actorID string, dbName string, tableName string, matchField string, values map[string]any) (table.Row, string, error) {
	perms, isOwner, err := server.tablePermissions(ctx, actorID, dbName)
	if err != nil {
		return table.Row{}, "", err
	}
	return server.upsertTableRow(ctx, server.catalogSnapshot(), perms, actorID, isOwner, dbName, tableName, matchField, values)
}

func (server *Server) createTableFieldsAs(ctx context.Context, actorID string, dbName string, tableName string, fields []metadata.Field) (workflowFieldMutation, error) {
	perms, isOwner, err := server.tablePermissions(ctx, actorID, dbName)
	if err != nil {
		return workflowFieldMutation{}, err
	}
	resource := dbName + "." + tableName
	if !isOwner {
		if !perms.CanAddFields(actorID, resource) {
			return workflowFieldMutation{}, table.ErrPermissionDenied
		}
		for _, field := range fields {
			if field.Type == "formula" {
				return workflowFieldMutation{}, fmt.Errorf("%w: formula fields require database owner", table.ErrPermissionDenied)
			}
		}
	}
	return server.addTableFields(ctx, dbName, tableName, fields)
}

func (server *Server) tablePermissions(ctx context.Context, actorID string, dbName string) (permission.Set, bool, error) {
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return permission.Set{}, false, err
	}
	isOwner, err := server.system.IsDatabaseOwner(ctx, actorID, dbName)
	if err != nil {
		return permission.Set{}, false, err
	}
	return perms, isOwner, nil
}

func writeTableMutationError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, table.ErrPermissionDenied) {
		status = http.StatusForbidden
	}
	if errors.Is(err, table.ErrRecordNotFound) {
		status = http.StatusNotFound
	}
	writeError(w, status, err)
}

func (server *Server) handleUpdateRow(w http.ResponseWriter, r *http.Request) {
	dbName, tableName := r.PathValue("database"), r.PathValue("table")
	recordID, ok := pathID(r, "recordID")
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
	row, err := server.updateTableRowAs(r.Context(), actorID, dbName, tableName, recordID, request.Values)
	if err != nil {
		writeTableMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rowResponse{RecordID: row.RecordID, Values: row.Values})
}

func (server *Server) handleDeleteRow(w http.ResponseWriter, r *http.Request) {
	dbName, tableName := r.PathValue("database"), r.PathValue("table")
	recordID, ok := pathID(r, "recordID")
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
	row, err := server.deleteTableRowAs(r.Context(), actorID, dbName, tableName, recordID)
	if err != nil {
		writeTableMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rowResponse{RecordID: row.RecordID, Values: row.Values})
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
	temporarySorts, err := parseTemporaryRowSorts(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := server.listTableRowsAs(r.Context(), actorID, dbName, tableName, table.RowListOptions{
		ViewName: r.URL.Query().Get("view"),
		Sorts:    temporarySorts,
	})
	if err != nil {
		writeTableMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rowResponses(rows))
}

func rowResponses(rows []table.Row) []rowResponse {
	response := make([]rowResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, rowResponse{RecordID: row.RecordID, Values: row.Values})
	}
	return response
}

func parseTemporaryRowSorts(query url.Values) ([]metadata.ViewSort, error) {
	sortField := query.Get("sort_field")
	sortDirection := query.Get("sort_direction")
	if sortField == "" && sortDirection == "" {
		return nil, nil
	}
	if sortField == "" || sortDirection == "" {
		return nil, errors.New("sort_field and sort_direction are required together")
	}
	if sortDirection != "asc" && sortDirection != "desc" {
		return nil, fmt.Errorf("unsupported sort direction %q", sortDirection)
	}
	return []metadata.ViewSort{{Field: sortField, Direction: sortDirection}}, nil
}

func (server *Server) handleRowHistory(w http.ResponseWriter, r *http.Request) {
	dbName, tableName := r.PathValue("database"), r.PathValue("table")
	recordID, ok := pathID(r, "recordID")
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := server.catalogSnapshot().Table(dbName, tableName); !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("table %s.%s not found", dbName, tableName))
		return
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	isOwner, err := server.system.IsDatabaseOwner(r.Context(), actorID, dbName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resource := dbName + "." + tableName
	// Row history is row-level: the record must be inside the actor's row
	// set, and historical values are redacted to readable fields — the
	// same double gate as live reads.
	if !isOwner {
		inRowSet, err := server.tables.RowInActorRowSet(r.Context(), server.catalogSnapshot(), perms, actorID, false, dbName, tableName, recordID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if !inRowSet {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: %d", table.ErrRecordNotFound, recordID))
			return
		}
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
		change.Values = readableHistoryValues(change.Values, perms, actorID, isOwner, resource)
		change.Diff = readableHistoryDiff(change.Diff, perms, actorID, isOwner, resource)
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
	if workflow.ID == 0 && !server.requireDatabaseOrSetWrite(w, r, actorID, workflow.DatabaseName, permission.ScopeWorkflowSet) {
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
	if err := server.validateRunnerBindings(r.Context(), workflow); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if workflow.HistoryRetentionDays != nil && *workflow.HistoryRetentionDays < 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("history_retention_days must be at least 0, got %d", *workflow.HistoryRetentionDays))
		return
	}
	if workflow.TimeoutSeconds != nil && *workflow.TimeoutSeconds <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("timeout_seconds must be greater than 0, got %d", *workflow.TimeoutSeconds))
		return
	}
	saved, err := server.saveWorkflowDefinition(r.Context(), actorID, workflow)
	if err != nil {
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
	dbName, resource := r.PathValue("database"), r.PathValue("resource")
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	switch resource {
	case "runners":
		server.handleDatabaseRunners(w, r, actorID, dbName)
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
		if !server.requireDatabaseOwner(w, r, actorID, dbName) {
			return
		}
		roles, err := server.system.Roles(r.Context(), dbName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		responses, err := server.roleResponses(r.Context(), roles)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, responses)
	default:
		http.NotFound(w, r)
	}
}

func (server *Server) handlePostDatabaseResource(w http.ResponseWriter, r *http.Request) {
	dbName, resource := r.PathValue("database"), r.PathValue("resource")
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	switch resource {
	case "runners":
		server.handleResetDatabaseRunnerToken(w, r, actorID, dbName)
	case "tables":
		if !server.requireDatabaseOwner(w, r, actorID, dbName) {
			return
		}
		var tableMeta metadata.Table
		if err := readJSON(r, &tableMeta); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := server.addTable(r.Context(), dbName, tableMeta); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		server.notifyRepositoryChange(r.Context(), actorID, "metadata.table.create", "created table "+dbName+"/"+tableMeta.Name, server.metadataPath)
		writeJSON(w, http.StatusCreated, tableMeta)
	case "workflows":
		var workflow systemdb.WorkflowDefinition
		if err := readJSON(r, &workflow); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if workflow.ID == 0 && !server.requireDatabaseOrSetWrite(w, r, actorID, dbName, permission.ScopeWorkflowSet) {
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
		if err := server.validateRunnerBindings(r.Context(), workflow); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if workflow.HistoryRetentionDays != nil && *workflow.HistoryRetentionDays < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("history_retention_days must be at least 0, got %d", *workflow.HistoryRetentionDays))
			return
		}
		if workflow.TimeoutSeconds != nil && *workflow.TimeoutSeconds <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("timeout_seconds must be greater than 0, got %d", *workflow.TimeoutSeconds))
			return
		}
		saved, err := server.saveWorkflowDefinition(r.Context(), actorID, workflow)
		if err != nil {
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
		if form.ID == 0 && !server.requireDatabaseOrSetWrite(w, r, actorID, dbName, permission.ScopeFormSet) {
			return
		}
		if form.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeForm, form.ID) {
			return
		}
		if form.ID != 0 && !server.requireExistingFormDatabase(w, r, form.ID, dbName) {
			return
		}
		form.DatabaseName = dbName
		if form.ID == 0 {
			form.CreatorID = actorID
		}
		saved, err := server.saveFormDefinition(r.Context(), actorID, form)
		if err != nil {
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
		if !server.requireDatabaseOwner(w, r, actorID, dbName) {
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
		response, err := server.roleResponse(r.Context(), saved)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, response)
	default:
		http.NotFound(w, r)
	}
}

func (server *Server) handleUpdateTableMetadataRoute(w http.ResponseWriter, r *http.Request) {
	server.handleUpdateTableMetadata(w, r, r.PathValue("database"), r.PathValue("table"))
}

func (server *Server) handleRoleAction(w http.ResponseWriter, r *http.Request) {
	dbName, roleName, action := r.PathValue("database"), r.PathValue("role"), r.PathValue("action")
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if !server.requireDatabaseOwner(w, r, actorID, dbName) {
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
		if err := server.validateRoleMembers(r.Context(), dbName, request.Members); err != nil {
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
	response, err := server.roleResponse(r.Context(), role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (server *Server) handleMoveFieldPosition(w http.ResponseWriter, r *http.Request) {
	dbName, tableName, fieldName := r.PathValue("database"), r.PathValue("table"), r.PathValue("field")
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := server.requireAuthorized(w, r, actorID, accessRequest{
		Action:   accessWriteFieldSet,
		Database: dbName,
		Table:    tableName,
	}); !ok {
		return
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	isOwner, err := server.system.IsDatabaseOwner(r.Context(), actorID, dbName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var request fieldPositionRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	tableMeta, err := server.moveField(r.Context(), dbName, tableName, fieldName, request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	server.notifyRepositoryChange(r.Context(), actorID, "metadata.field.move", "moved field "+dbName+"/"+tableName+"/"+fieldName, server.metadataPath)
	writeJSON(w, http.StatusOK, visibleTableMetadata(perms, actorID, dbName, isOwner, tableMeta))
}

func (server *Server) handleUpdateTableMetadata(w http.ResponseWriter, r *http.Request, dbName, tableName string) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var tableMeta metadata.Table
	if err := readJSON(r, &tableMeta); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	existingTable, ok := server.catalogSnapshot().Table(dbName, tableName)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("table %s.%s not found", dbName, tableName))
		return
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	isOwner, err := server.system.IsDatabaseOwner(r.Context(), actorID, dbName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if tableMeta.Fields != nil && !server.authorizeFieldMetadataPatch(w, actorID, isOwner, dbName, tableName, perms, existingTable, tableMeta.Fields) {
		return
	}
	if tableMeta.Views != nil {
		// Clients echo table metadata back including the injected built-in
		// "all" view; it is not a stored view, so it is stripped before
		// authorization and merge (mergeViews leaves omitted views alone).
		views := make([]metadata.View, 0, len(tableMeta.Views))
		for _, view := range tableMeta.Views {
			if view.Name != metadata.AllViewName {
				views = append(views, view)
			}
		}
		tableMeta.Views = views
	}
	if tableMeta.Views != nil && !server.authorizeViewMetadataPatch(w, actorID, isOwner, dbName, tableName, perms, existingTable, tableMeta.Views) {
		return
	}
	if tableMeta.Name == "" {
		tableMeta.Name = tableName
	}
	updated, err := server.updateTable(r.Context(), dbName, tableName, tableMeta)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	server.notifyRepositoryChange(r.Context(), actorID, "metadata.table.update", "updated table metadata "+dbName+"/"+tableName, server.metadataPath)
	writeJSON(w, http.StatusOK, visibleTableMetadata(perms, actorID, dbName, isOwner, updated))
}

// authorizeFieldMetadataPatch enforces the metadata layer: field_add for
// new fields, per-field field_modify for definition changes, and owner-only
// for anything involving formulas or deletion. Formulas evaluate over the
// whole row server-side, so letting anyone else define them would bypass
// field read permissions.
func (server *Server) authorizeFieldMetadataPatch(w http.ResponseWriter, actorID string, isOwner bool, dbName, tableName string, perms permission.Set, existing metadata.Table, fields []metadata.Field) bool {
	if isOwner {
		return true
	}
	resource := dbName + "." + tableName
	for _, field := range fields {
		existingField, ok := existing.Field(field.Name)
		switch {
		case !ok:
			if field.Type == "formula" {
				writeError(w, http.StatusForbidden, fmt.Errorf("formula field %q requires database owner", field.Name))
				return false
			}
			if !perms.CanAddFields(actorID, resource) {
				writeError(w, http.StatusForbidden, fmt.Errorf("adding field %q requires the field_add permission", field.Name))
				return false
			}
		case existingField.Deleted != field.Deleted:
			writeError(w, http.StatusForbidden, fmt.Errorf("delete field %q requires database owner", field.Name))
			return false
		case fieldDefinitionChanged(existingField, field):
			if existingField.Type == "formula" || field.Type == "formula" {
				writeError(w, http.StatusForbidden, fmt.Errorf("formula field %q requires database owner", field.Name))
				return false
			}
			if !perms.CanModifyField(actorID, resource, field.Name) {
				writeError(w, http.StatusForbidden, fmt.Errorf("modifying field %q requires the field_modify permission", field.Name))
				return false
			}
		}
	}
	return true
}

func fieldDefinitionChanged(existing, incoming metadata.Field) bool {
	return existing.Type != incoming.Type ||
		existing.ValueType != incoming.ValueType ||
		existing.Formula != incoming.Formula ||
		existing.RelationTable != incoming.RelationTable
}

// authorizeViewMetadataPatch: view definitions are the row-level permission
// boundary, so changing them is database-owner-only. Unchanged views pass so
// non-owners with field rights can still PUT the full table metadata.
func (server *Server) authorizeViewMetadataPatch(w http.ResponseWriter, _ string, isOwner bool, _, _ string, _ permission.Set, existing metadata.Table, views []metadata.View) bool {
	if isOwner || viewDefinitionsEqual(existing.Views, views) {
		return true
	}
	writeError(w, http.StatusForbidden, errors.New("changing views requires database owner"))
	return false
}

// viewDefinitionsEqual compares view definitions ignoring the response-only
// permission level annotation the frontend echoes back.
func viewDefinitionsEqual(existing, incoming []metadata.View) bool {
	normalize := func(views []metadata.View) []metadata.View {
		next := make([]metadata.View, len(views))
		for i, view := range views {
			view.PermissionLevel = 0
			if view.Sorts == nil {
				view.Sorts = []metadata.ViewSort{}
			}
			next[i] = view
		}
		return next
	}
	left, err := json.Marshal(normalize(existing))
	if err != nil {
		return false
	}
	right, err := json.Marshal(normalize(incoming))
	if err != nil {
		return false
	}
	return bytes.Equal(left, right)
}

func (server *Server) handleWorkflowRunsRoute(w http.ResponseWriter, r *http.Request) {
	workflowID, ok := pathID(r, "workflowID")
	if !ok {
		http.NotFound(w, r)
		return
	}
	server.handleWorkflowRuns(w, r, workflowID)
}

func (server *Server) handleWorkflowRunRoute(w http.ResponseWriter, r *http.Request) {
	workflowID, ok := pathID(r, "workflowID")
	historyKey := r.PathValue("historyKey")
	if !ok || historyKey == "" {
		http.NotFound(w, r)
		return
	}
	server.handleWorkflowRun(w, r, workflowID, historyKey)
}

func (server *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "workflowID")
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
	id, ok := pathID(r, "workflowID")
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := server.requireAuthorized(w, r, actorID, accessRequest{Action: accessWriteWorkflow, WorkflowID: id}); !ok {
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
		ID:                   workflowDefinition.ID,
		DatabaseName:         workflowDefinition.DatabaseName,
		Script:               workflowDefinition.Script,
		CreatorID:            systemdb.WorkflowSubjectID(workflowDefinition.ID),
		Secrets:              workflowDefinition.Secrets,
		Variables:            workflowDefinition.Variables,
		Runners:              workflowDefinition.Runners,
		HistoryRetentionDays: workflowDefinition.HistoryRetentionDays,
		TimeoutSeconds:       workflowDefinition.TimeoutSeconds,
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
	keys, err := server.history.GetPrefixKeysLimit(r.Context(), history.WorkflowPrefix(workflowID), workflowRunListLimit(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	runs := make([]workflowRunResponse, 0, len(keys))
	for _, key := range keys {
		parsedWorkflowID, timestamp, err := history.ParseWorkflowKey(key)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if parsedWorkflowID != workflowID {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("workflow history key %q does not match workflow %d", key, workflowID))
			return
		}
		runs = append(runs, workflowRunResponse{HistoryKey: key, Run: history.WorkflowRun{
			WorkflowID: workflowID,
			Timestamp:  timestamp,
			Steps:      []history.StepRecord{},
		}, Summary: true})
	}
	writeJSON(w, http.StatusOK, runs)
}

func (server *Server) handleWorkflowRun(w http.ResponseWriter, r *http.Request, workflowID int64, historyKey string) {
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
	parsedWorkflowID, _, err := history.ParseWorkflowKey(historyKey)
	if err != nil || parsedWorkflowID != workflowID {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid workflow history key %q", historyKey))
		return
	}
	entry, err := server.history.Get(r.Context(), historyKey)
	if errors.Is(err, history.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	run, err := history.DecodeWorkflowRun(entry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowRunResponse{HistoryKey: historyKey, Run: run})
}

func (server *Server) StartWorkflowWorkers(ctx context.Context) {
	server.workflowWorkerMu.Lock()
	defer server.workflowWorkerMu.Unlock()
	if server.workflowWorker != nil {
		return
	}
	server.workflowWorker = ctx
	server.workflowWorkers = map[string]*workflowEventWorker{}
	server.workflowRunWorkers = map[int64]*workflowRunWorker{}
	for _, database := range server.catalogSnapshot().Databases {
		server.startWorkflowWorkerLocked(database.Name)
	}
}

func (server *Server) startWorkflowRunWorkerLocked(workflowID int64) *workflowRunWorker {
	if worker, ok := server.workflowRunWorkers[workflowID]; ok {
		return worker
	}
	worker := &workflowRunWorker{jobs: make(chan workflowRunJob, 256)}
	server.workflowRunWorkers[workflowID] = worker
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-worker.jobs:
				server.executeWorkflowRun(ctx, job)
			}
		}
	}(server.workflowWorker)
	return worker
}

// enqueueWorkflowRun hands the run to the workflow's own worker so a slow
// workflow only delays itself. Without workers (tests, shutdown) the run
// executes inline.
func (server *Server) enqueueWorkflowRun(ctx context.Context, job workflowRunJob) {
	server.workflowWorkerMu.Lock()
	if server.workflowWorker == nil {
		server.workflowWorkerMu.Unlock()
		server.executeWorkflowRun(ctx, job)
		return
	}
	worker := server.startWorkflowRunWorkerLocked(job.definition.ID)
	workerCtx := server.workflowWorker
	server.workflowWorkerMu.Unlock()
	select {
	case worker.jobs <- job:
	case <-ctx.Done():
	case <-workerCtx.Done():
	}
}

func (server *Server) executeWorkflowRun(ctx context.Context, job workflowRunJob) {
	if job.isSchedule {
		_, _, _ = server.runner.RunAt(ctx, job.definition, job.inputs, job.scheduledAt)
		return
	}
	_, _, _ = server.runner.Run(ctx, job.definition, job.inputs)
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

// StartHistoryCleanup deletes expired workflow run history immediately and
// then on the given interval, compacting the store after any deletion.
func (server *Server) StartHistoryCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := server.CleanupWorkflowHistory(ctx); err != nil && ctx.Err() == nil {
				slog.Error("workflow history cleanup failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// CleanupWorkflowHistory deletes run history older than each workflow's
// retention window; workflows without a retention setting keep everything.
func (server *Server) CleanupWorkflowHistory(ctx context.Context) error {
	now := time.Now().UTC().UnixMilli()
	deleted := 0
	for _, database := range server.catalogSnapshot().Databases {
		workflows, err := server.system.Workflows(ctx, database.Name)
		if err != nil {
			return err
		}
		for _, workflowDefinition := range workflows {
			if workflowDefinition.HistoryRetentionDays == nil {
				continue
			}
			prefix := history.WorkflowPrefix(workflowDefinition.ID)
			cutoff := now - *workflowDefinition.HistoryRetentionDays*24*time.Hour.Milliseconds()
			end := history.WorkflowKey(workflowDefinition.ID, cutoff)
			if *workflowDefinition.HistoryRetentionDays == 0 {
				// Zero keeps nothing: also drop runs recorded this instant.
				end = prefix + "\xff"
			}
			count, err := server.history.DeletePrefixBefore(ctx, prefix, end)
			if err != nil {
				return err
			}
			deleted += count
		}
	}
	if deleted == 0 {
		return nil
	}
	if compacter, ok := server.history.(history.Compacter); ok {
		if err := compacter.Compact(ctx); err != nil {
			return err
		}
	}
	slog.Info("workflow history cleanup finished", "deleted", deleted)
	return nil
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
	keys, err := server.history.GetPrefixKeysLimit(ctx, history.WorkflowPrefix(workflowID), 1)
	if err != nil {
		return time.Time{}, false, err
	}
	var latest int64
	for _, key := range keys {
		parsedWorkflowID, timestamp, err := history.ParseWorkflowKey(key)
		if err != nil {
			return time.Time{}, false, err
		}
		if parsedWorkflowID != workflowID {
			return time.Time{}, false, fmt.Errorf("workflow history key %q does not match workflow %d", key, workflowID)
		}
		if timestamp > latest {
			latest = timestamp
		}
	}
	if latest == 0 {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(latest).UTC(), true, nil
}

func workflowRunListLimit(r *http.Request) int {
	limit := defaultWorkflowRunListLimit
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			limit = parsed
		}
	}
	if limit <= 0 {
		return defaultWorkflowRunListLimit
	}
	if limit > maxWorkflowRunListLimit {
		return maxWorkflowRunListLimit
	}
	return limit
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
		if !workflowDefinition.Enabled {
			continue
		}
		definition := workflow.Definition{
			ID:                   workflowDefinition.ID,
			DatabaseName:         workflowDefinition.DatabaseName,
			Script:               workflowDefinition.Script,
			CreatorID:            systemdb.WorkflowSubjectID(workflowDefinition.ID),
			Secrets:              workflowDefinition.Secrets,
			Variables:            workflowDefinition.Variables,
			Runners:              workflowDefinition.Runners,
			HistoryRetentionDays: workflowDefinition.HistoryRetentionDays,
			TimeoutSeconds:       workflowDefinition.TimeoutSeconds,
		}
		declaration, err := server.runner.Trigger(ctx, definition)
		if errors.Is(err, workflow.ErrMissingTrigger) {
			continue
		}
		if err != nil {
			slog.Debug("workflow trigger declaration failed", "workflow", workflowDefinition.ID, "error", err)
			continue
		}
		if !server.workflowEventMayRun(ctx, workflowDefinition.ID, declaration, event) {
			continue
		}
		triggerEvent, ok := server.workflowTriggerEventForSubject(ctx, definition.CreatorID, event)
		if !ok {
			slog.Debug("workflow trigger event withheld: subject cannot read the change",
				"workflow", workflowDefinition.ID, "table", event.RowChange.Table, "subject", definition.CreatorID)
			continue
		}
		inputs, matched, err := server.runner.TriggerRunInputs(ctx, definition, declaration, triggerEvent)
		if err != nil {
			slog.Debug("workflow trigger inputs failed", "workflow", workflowDefinition.ID, "error", err)
			continue
		}
		if !matched {
			continue
		}
		server.enqueueWorkflowRun(ctx, workflowRunJob{
			definition:  definition,
			inputs:      inputs,
			scheduledAt: event.ScheduledAt,
			isSchedule:  event.Kind == workflowEventSchedule,
		})
	}
}

func (server *Server) workflowEventMayRun(ctx context.Context, workflowID int64, declaration workflow.TriggerDeclaration, event workflowEvent) bool {
	switch event.Kind {
	case workflowEventSchedule:
		return server.scheduleTriggerMatches(ctx, workflowID, declaration, event.ScheduledAt)
	case workflowEventRowChange:
		return declaration.Node == "table.record.changed"
	case workflowEventWebhook:
		return event.WorkflowID == workflowID && declaration.Node == "webhook.trigger"
	default:
		return false
	}
}

func workflowTriggerEvent(event workflowEvent) workflow.TriggerEvent {
	switch event.Kind {
	case workflowEventSchedule:
		return workflow.TriggerEvent{
			Kind:        "schedule",
			ScheduledAt: event.ScheduledAt.UTC().UnixMilli(),
		}
	case workflowEventRowChange:
		return workflow.TriggerEvent{
			Kind:       "row_change",
			HistoryKey: event.HistoryKey,
			RowChange:  event.RowChange,
		}
	case workflowEventWebhook:
		return workflow.TriggerEvent{
			Kind: "webhook",
			Webhook: workflow.WebhookEvent{
				Token:      event.WebhookToken,
				Payload:    event.WebhookPayload,
				ReceivedAt: event.ReceivedAt,
			},
		}
	default:
		return workflow.TriggerEvent{}
	}
}

func (server *Server) workflowTriggerEventForSubject(ctx context.Context, subjectID string, event workflowEvent) (workflow.TriggerEvent, bool) {
	switch event.Kind {
	case workflowEventSchedule, workflowEventWebhook:
		return workflowTriggerEvent(event), true
	case workflowEventRowChange:
		change, ok := server.readableRowChangeForSubject(ctx, subjectID, event.RowChange)
		if !ok {
			return workflow.TriggerEvent{}, false
		}
		next := event
		next.RowChange = change
		return workflowTriggerEvent(next), true
	default:
		return workflow.TriggerEvent{}, false
	}
}

func (server *Server) readableRowChangeForSubject(ctx context.Context, subjectID string, change history.RowChange) (history.RowChange, bool) {
	perms, isOwner, err := server.tablePermissions(ctx, subjectID, change.Database)
	if err != nil {
		return history.RowChange{}, false
	}
	tableMeta, ok := server.catalogSnapshot().Table(change.Database, change.Table)
	if !ok {
		return history.RowChange{}, false
	}
	resource := change.Database + "." + change.Table
	readable := map[string]struct{}{}
	for _, field := range tableMeta.ActiveFields() {
		if isOwner || perms.FieldLevel(subjectID, resource, field.Name) >= permission.Read {
			readable[field.Name] = struct{}{}
		}
	}
	if len(readable) == 0 {
		return history.RowChange{}, false
	}
	filteredValues := filterRowChangeValues(change.Values, readable)
	filteredDiff := filterRowChangeDiff(change.Diff, readable)
	if len(filteredValues) == 0 && len(filteredDiff) == 0 {
		return history.RowChange{}, false
	}
	change.Values = filteredValues
	change.Diff = filteredDiff
	return change, true
}

func filterRowChangeValues(values map[string]any, readable map[string]struct{}) map[string]any {
	filtered := map[string]any{}
	for field, value := range values {
		if _, ok := readable[field]; ok {
			filtered[field] = value
		}
	}
	return filtered
}

func filterRowChangeDiff(diff history.RowDiff, readable map[string]struct{}) history.RowDiff {
	filtered := history.RowDiff{}
	for field, value := range diff {
		if _, ok := readable[field]; ok {
			filtered[field] = value
		}
	}
	return filtered
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
	if form.ID == 0 && !server.requireDatabaseOrSetWrite(w, r, actorID, form.DatabaseName, permission.ScopeFormSet) {
		return
	}
	if form.ID != 0 && !server.requireResourceWrite(w, r, actorID, permission.ScopeForm, form.ID) {
		return
	}
	if form.ID != 0 && !server.requireExistingFormDatabase(w, r, form.ID, form.DatabaseName) {
		return
	}
	if form.ID == 0 {
		form.CreatorID = actorID
	}
	saved, err := server.saveFormDefinition(r.Context(), actorID, form)
	if err != nil {
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
	id, ok := pathID(r, "formID")
	action := r.PathValue("action")
	if !ok || (action != "publish" && action != "unpublish") {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := server.requireAuthorized(w, r, actorID, accessRequest{Action: accessWriteForm, FormID: id}); !ok {
		return
	}
	var form systemdb.FormDefinition
	var err error
	if action == "publish" {
		form, err = server.system.PublishForm(r.Context(), id)
	} else {
		form, err = server.system.UnpublishForm(r.Context(), id)
	}
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

func (server *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "workflowID")
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := server.requireAuthorized(w, r, actorID, accessRequest{Action: accessDeleteWorkflow, WorkflowID: id}); !ok {
		return
	}
	workflow, err := server.system.Workflow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err := server.system.DeleteWorkflow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if server.codeFiles != nil {
		if err := server.codeFiles.DeleteWorkflowScript(r.Context(), workflow); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		server.notifyRepositoryChange(r.Context(), actorID, "workflow.delete", "deleted workflow "+workflow.DatabaseName+"/"+workflow.Name, server.codeFiles.WorkflowScriptPath(workflow))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleDeleteForm(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "formID")
	if !ok {
		http.NotFound(w, r)
		return
	}
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := server.requireAuthorized(w, r, actorID, accessRequest{Action: accessDeleteForm, FormID: id}); !ok {
		return
	}
	form, err := server.system.Form(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err := server.system.DeleteForm(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if server.codeFiles != nil {
		if err := server.codeFiles.DeleteFormScript(r.Context(), form); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		server.notifyRepositoryChange(r.Context(), actorID, "form.delete", "deleted form "+form.DatabaseName+"/"+form.Name, server.codeFiles.FormScriptPath(form))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleGetForm(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "formID")
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
	token := r.PathValue("token")
	if token == "" {
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










const maxWebhookBodyBytes = 1 << 20

// handleWorkflowWebhookRoute is registered with a wildcard pattern; webhooks
// authenticate with their shared token instead of a session.
func (server *Server) handleWorkflowWebhookRoute(w http.ResponseWriter, r *http.Request) {
	workflowID, err := strconv.ParseInt(r.PathValue("workflowID"), 10, 64)
	if err != nil || workflowID <= 0 {
		http.NotFound(w, r)
		return
	}
	server.handleWorkflowWebhook(w, r, r.PathValue("database"), workflowID)
}

type workflowWebhookRequest struct {
	Token   string         `json:"token"`
	Payload map[string]any `json:"payload"`
}

// handleWorkflowWebhook lets external systems push events into a workflow.
// The shared token configured on the webhook trigger instance is the only
// credential; no session is involved.
func (server *Server) handleWorkflowWebhook(w http.ResponseWriter, r *http.Request, dbName string, workflowID int64) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
	var request workflowWebhookRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if request.Token == "" {
		writeError(w, http.StatusUnauthorized, errors.New("webhook token is required"))
		return
	}
	workflowDefinition, err := server.system.Workflow(r.Context(), workflowID)
	if err != nil || workflowDefinition.DatabaseName != dbName {
		writeError(w, http.StatusNotFound, fmt.Errorf("workflow %d not found in database %q", workflowID, dbName))
		return
	}
	if !workflowDefinition.Enabled {
		writeError(w, http.StatusBadRequest, errors.New("workflow is disabled"))
		return
	}
	workflowDefinition, err = server.workflowDefinitionWithFileScript(r.Context(), workflowDefinition)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	definition := workflow.Definition{
		ID:                   workflowDefinition.ID,
		DatabaseName:         workflowDefinition.DatabaseName,
		Script:               workflowDefinition.Script,
		CreatorID:            systemdb.WorkflowSubjectID(workflowDefinition.ID),
		Secrets:              workflowDefinition.Secrets,
		Variables:            workflowDefinition.Variables,
		Runners:              workflowDefinition.Runners,
		HistoryRetentionDays: workflowDefinition.HistoryRetentionDays,
		TimeoutSeconds:       workflowDefinition.TimeoutSeconds,
	}
	declaration, err := server.runner.Trigger(r.Context(), definition)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if declaration.Node != "webhook.trigger" {
		writeError(w, http.StatusBadRequest, errors.New("workflow trigger is not a webhook"))
		return
	}
	configured := strings.TrimSpace(definition.Secrets[declaration.Instance+".token"])
	if configured == "" {
		writeError(w, http.StatusUnauthorized, errors.New("webhook token is not configured on the trigger instance"))
		return
	}
	if subtle.ConstantTimeCompare([]byte(configured), []byte(request.Token)) != 1 {
		writeError(w, http.StatusUnauthorized, errors.New("invalid webhook token"))
		return
	}
	server.dispatchWorkflowEvent(r.Context(), workflowEvent{
		Kind:           workflowEventWebhook,
		DatabaseName:   dbName,
		WorkflowID:     workflowID,
		WebhookToken:   request.Token,
		WebhookPayload: request.Payload,
		ReceivedAt:     time.Now().UTC().UnixMilli(),
	})
	// 200 instead of 202: webhook senders like DingTalk's HTTP connector
	// treat any non-200 status as a delivery failure and retry.
	writeJSON(w, http.StatusOK, map[string]any{"accepted": true})
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
		isOwner, err := server.system.IsDatabaseOwner(ctx, actorID, database.Name)
		if err != nil {
			return metadata.Catalog{}, err
		}
		dbLevel := permission.None
		if isOwner {
			dbLevel = permission.Write
		}
		workflowSetLevel := perms.ResourceLevel(actorID, permission.ScopeWorkflowSet, database.Name)
		formSetLevel := perms.ResourceLevel(actorID, permission.ScopeFormSet, database.Name)
		dbVisible := isOwner
		tables := make([]metadata.Table, 0, len(database.Tables))
		for _, tableMeta := range database.Tables {
			if isOwner || canSeeTableMetadata(perms, actorID, database.Name, tableMeta) {
				tables = append(tables, visibleTableMetadata(perms, actorID, database.Name, isOwner, tableMeta))
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
			database.PermissionLevel = int(dbLevel)
			database.WorkflowPermissionLevel = int(maxPermissionLevel(dbLevel, workflowSetLevel))
			database.FormPermissionLevel = int(maxPermissionLevel(dbLevel, formSetLevel))
			database.Tables = tables
			visible.Databases = append(visible.Databases, database)
		}
	}
	return visible, nil
}

func visibleTableMetadata(perms permission.Set, actorID, dbName string, isOwner bool, tableMeta metadata.Table) metadata.Table {
	resource := dbName + "." + tableMeta.Name
	dbLevel := permission.None
	if isOwner {
		dbLevel = permission.Write
	}
	// Field grant levels are bitmasks; collapse them to the legacy 0/1/2
	// scale for the table-level UI hints (2 = may add fields).
	fieldSetHint := permission.None
	if perms.CanAddFields(actorID, resource) {
		fieldSetHint = permission.Write
	} else if perms.FieldSetLevel(actorID, resource) != permission.None {
		fieldSetHint = permission.Read
	}
	// View management is owner-only; view grants only confer read, so the
	// view-set hint is clamped to read for non-owners.
	viewSetLevel := perms.ResourceLevel(actorID, permission.ScopeViewSet, resource)
	viewSetHint := minPermissionLevel(viewSetLevel, permission.Read)
	annotated := tableMeta
	annotated.PermissionLevel = int(maxPermissionLevel(dbLevel, maxPermissionLevel(fieldSetHint, viewSetHint)))
	annotated.DatabasePermissionLevel = int(dbLevel)
	annotated.FieldPermissionLevel = int(maxPermissionLevel(dbLevel, fieldSetHint))
	annotated.ViewPermissionLevel = int(dbLevel)
	if dbLevel >= permission.Write {
		annotated.Fields = annotateFieldPermissionLevels(perms, actorID, resource, dbLevel, annotated.Fields)
		// The built-in unfiltered view is served as an ordinary view entry
		// so clients need no special casing.
		annotated.Views = annotateViewPermissionLevels(perms, actorID, resource, dbLevel,
			append([]metadata.View{{Name: metadata.AllViewName, Sorts: []metadata.ViewSort{}}}, annotated.Views...))
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
	visible.Views = make([]metadata.View, 0, len(tableMeta.Views)+1)
	if perms.CanReadView(actorID, resource, metadata.AllViewName) {
		visible.Views = append(visible.Views, metadata.View{Name: metadata.AllViewName, Sorts: []metadata.ViewSort{}, PermissionLevel: int(permission.Read)})
	}
	for _, view := range tableMeta.Views {
		viewLevel := maxPermissionLevel(viewSetLevel, perms.ViewLevel(actorID, resource, view.Name))
		if viewLevel < permission.Read {
			continue
		}
		resolved, err := tableMeta.ResolveView(view.Name)
		if err != nil {
			continue
		}
		if viewFieldsReadable(perms, actorID, resource, resolved.Query, resolved.Sorts) {
			view.PermissionLevel = int(minPermissionLevel(viewLevel, permission.Read))
			visible.Views = append(visible.Views, view)
		}
	}
	return visible
}

func annotateFieldPermissionLevels(perms permission.Set, actorID, resource string, dbLevel permission.Level, fields []metadata.Field) []metadata.Field {
	annotated := make([]metadata.Field, 0, len(fields))
	for _, field := range fields {
		level := perms.FieldLevel(actorID, resource, field.Name)
		if dbLevel >= permission.Write {
			level = permission.FieldAll
		}
		field.PermissionLevel = int(level)
		annotated = append(annotated, field)
	}
	return annotated
}

func annotateViewPermissionLevels(perms permission.Set, actorID, resource string, dbLevel permission.Level, views []metadata.View) []metadata.View {
	annotated := make([]metadata.View, 0, len(views))
	for _, view := range views {
		level := minPermissionLevel(perms.ViewLevel(actorID, resource, view.Name), permission.Read)
		view.PermissionLevel = int(maxPermissionLevel(dbLevel, level))
		annotated = append(annotated, view)
	}
	return annotated
}

func minPermissionLevel(left, right permission.Level) permission.Level {
	if left < right {
		return left
	}
	return right
}

func maxPermissionLevel(left, right permission.Level) permission.Level {
	if left > right {
		return left
	}
	return right
}

func viewFieldsReadable(perms permission.Set, actorID, resource string, query *metadata.ViewQuery, sorts []metadata.ViewSort) bool {
	for _, field := range viewQueryFields(query) {
		// System columns (ct_record_id) travel with every row response, so
		// referencing them in queries/sorts needs no field grant.
		if strings.HasPrefix(field, "ct_") {
			continue
		}
		if !perms.CanReadField(actorID, resource, field) {
			return false
		}
	}
	for _, sortDef := range sorts {
		if strings.HasPrefix(sortDef.Field, "ct_") {
			continue
		}
		if !perms.CanReadField(actorID, resource, sortDef.Field) {
			return false
		}
	}
	return true
}

func viewQueryFields(query *metadata.ViewQuery) []string {
	if query == nil {
		return nil
	}
	fields := []string{}
	for _, rule := range query.Rules {
		fields = append(fields, viewQueryRuleFields(rule)...)
	}
	return fields
}

func viewQueryRuleFields(rule metadata.ViewQueryRule) []string {
	if rule.Combinator != "" || len(rule.Rules) > 0 {
		fields := []string{}
		for _, child := range rule.Rules {
			fields = append(fields, viewQueryRuleFields(child)...)
		}
		return fields
	}
	return []string{rule.Field}
}

func canSeeTableMetadata(perms permission.Set, actorID, dbName string, tableMeta metadata.Table) bool {
	resource := dbName + "." + tableMeta.Name
	if perms.ResourceLevel(actorID, permission.ScopeFieldSet, resource) >= permission.Read ||
		perms.ResourceLevel(actorID, permission.ScopeViewSet, resource) >= permission.Read {
		return true
	}
	for _, field := range tableMeta.ActiveFields() {
		if perms.FieldLevel(actorID, resource, field.Name) >= permission.Read {
			return true
		}
	}
	for _, view := range tableMeta.Views {
		if perms.ViewLevel(actorID, resource, view.Name) >= permission.Read {
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
		if perms.CanReadResource(actorID, permission.ScopeWorkflowSet, dbName) ||
			perms.CanReadResource(actorID, permission.ScopeWorkflow, resourceID(workflow.ID)) {
			return true, nil
		}
	}
	forms, err := server.system.Forms(ctx, dbName)
	if err != nil {
		return false, err
	}
	for _, form := range forms {
		if perms.CanReadResource(actorID, permission.ScopeFormSet, dbName) ||
			perms.CanReadResource(actorID, permission.ScopeForm, resourceID(form.ID)) {
			return true, nil
		}
	}
	return false, nil
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
	switch scope {
	case permission.ScopeWorkflow:
	case permission.ScopeForm:
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported resource scope %q", scope))
		return false
	}
	perms, err := server.system.EffectiveGrantsForSubject(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return false
	}
	if perms.CanReadResource(actorID, scope, resourceID(id)) {
		return true
	}
	writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
	return false
}

func (server *Server) requireDatabaseOwner(w http.ResponseWriter, r *http.Request, actorID string, dbName string) bool {
	_, ok := server.requireAuthorized(w, r, actorID, accessRequest{Action: accessManageDatabase, Database: dbName})
	return ok
}

func (server *Server) grantDatabaseName(ctx context.Context, grant permission.Grant) (string, error) {
	switch grant.Scope {
	case permission.ScopeFieldSet, permission.ScopeField, permission.ScopeRecord, permission.ScopeViewSet, permission.ScopeView, permission.ScopeFile, permission.ScopeFieldAdd, permission.ScopeFieldModify:
		dbName, _, ok := strings.Cut(grant.Resource, ".")
		if !ok || dbName == "" {
			return "", fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		return dbName, nil
	case permission.ScopeWorkflowSet, permission.ScopeFormSet:
		if grant.Resource == "" {
			return "", fmt.Errorf("grant %s resource is required", grant.Scope)
		}
		return grant.Resource, nil
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
	if err := server.validateExclusiveGrants(ctx, grants); err != nil {
		return err
	}
	for _, grant := range grants {
		if grant.Level == permission.None {
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

func (server *Server) deleteConflictingGrants(ctx context.Context, grant permission.Grant) error {
	if grant.Level == permission.None {
		return nil
	}
	switch grant.Scope {
	case permission.ScopeFieldSet:
		return server.system.DeleteGrant(ctx, grant.SubjectID, permission.ScopeField, grant.Resource)
	case permission.ScopeField:
		return server.system.DeleteGrant(ctx, grant.SubjectID, permission.ScopeFieldSet, grant.Resource)
	case permission.ScopeViewSet:
		return server.system.DeleteGrant(ctx, grant.SubjectID, permission.ScopeView, grant.Resource)
	case permission.ScopeView:
		return server.system.DeleteGrant(ctx, grant.SubjectID, permission.ScopeViewSet, grant.Resource)
	case permission.ScopeWorkflowSet:
		workflows, err := server.system.Workflows(ctx, grant.Resource)
		if err != nil {
			return err
		}
		for _, workflow := range workflows {
			if err := server.system.DeleteGrant(ctx, grant.SubjectID, permission.ScopeWorkflow, resourceID(workflow.ID)); err != nil {
				return err
			}
		}
	case permission.ScopeWorkflow:
		id, err := parseGrantResourceID(grant.Resource)
		if err != nil {
			return err
		}
		workflow, err := server.system.Workflow(ctx, id)
		if err != nil {
			return err
		}
		return server.system.DeleteGrant(ctx, grant.SubjectID, permission.ScopeWorkflowSet, workflow.DatabaseName)
	case permission.ScopeFormSet:
		forms, err := server.system.Forms(ctx, grant.Resource)
		if err != nil {
			return err
		}
		for _, form := range forms {
			if err := server.system.DeleteGrant(ctx, grant.SubjectID, permission.ScopeForm, resourceID(form.ID)); err != nil {
				return err
			}
		}
	case permission.ScopeForm:
		id, err := parseGrantResourceID(grant.Resource)
		if err != nil {
			return err
		}
		form, err := server.system.Form(ctx, id)
		if err != nil {
			return err
		}
		return server.system.DeleteGrant(ctx, grant.SubjectID, permission.ScopeFormSet, form.DatabaseName)
	}
	return nil
}

func (server *Server) validateExclusiveGrants(ctx context.Context, grants []permission.Grant) error {
	fieldSets := map[string]struct{}{}
	fields := map[string]struct{}{}
	viewSets := map[string]struct{}{}
	views := map[string]struct{}{}
	workflowSets := map[string]struct{}{}
	workflows := map[string]struct{}{}
	formSets := map[string]struct{}{}
	forms := map[string]struct{}{}
	for _, grant := range grants {
		if grant.Level == permission.None {
			continue
		}
		switch grant.Scope {
		case permission.ScopeFieldSet:
			fieldSets[grant.Resource] = struct{}{}
		case permission.ScopeField:
			fields[grant.Resource] = struct{}{}
		case permission.ScopeViewSet:
			viewSets[grant.Resource] = struct{}{}
		case permission.ScopeView:
			views[grant.Resource] = struct{}{}
		case permission.ScopeWorkflowSet:
			workflowSets[grant.Resource] = struct{}{}
		case permission.ScopeWorkflow:
			id, err := parseGrantResourceID(grant.Resource)
			if err != nil {
				return err
			}
			workflow, err := server.system.Workflow(ctx, id)
			if err != nil {
				return err
			}
			workflows[workflow.DatabaseName] = struct{}{}
		case permission.ScopeFormSet:
			formSets[grant.Resource] = struct{}{}
		case permission.ScopeForm:
			id, err := parseGrantResourceID(grant.Resource)
			if err != nil {
				return err
			}
			form, err := server.system.Form(ctx, id)
			if err != nil {
				return err
			}
			forms[form.DatabaseName] = struct{}{}
		}
	}
	if overlapKey(fieldSets, fields) != "" {
		return errors.New("field set and field grants are mutually exclusive")
	}
	if overlapKey(viewSets, views) != "" {
		return errors.New("view set and view grants are mutually exclusive")
	}
	if overlapKey(workflowSets, workflows) != "" {
		return errors.New("workflow set and workflow grants are mutually exclusive")
	}
	if overlapKey(formSets, forms) != "" {
		return errors.New("form set and form grants are mutually exclusive")
	}
	return nil
}

func overlapKey(left, right map[string]struct{}) string {
	for key := range left {
		if _, ok := right[key]; ok {
			return key
		}
	}
	return ""
}

func (server *Server) validateRoleMembers(ctx context.Context, dbName string, members []systemdb.RoleMember) error {
	seen := map[string]struct{}{}
	for _, member := range members {
		member.Type = strings.TrimSpace(member.Type)
		member.ID = strings.TrimSpace(member.ID)
		if member.Type == "" {
			member.Type = "user"
		}
		if member.ID == "" {
			continue
		}
		key := member.Type + ":" + member.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		switch member.Type {
		case "user":
			if _, err := server.system.User(ctx, member.ID); err != nil {
				return fmt.Errorf("role member user %q not found", member.ID)
			}
		case "workflow":
			workflowID, err := strconv.ParseInt(member.ID, 10, 64)
			if err != nil {
				return fmt.Errorf("role member workflow %q must be an id", member.ID)
			}
			workflow, err := server.system.Workflow(ctx, workflowID)
			if err != nil {
				return fmt.Errorf("role member workflow %q not found", member.ID)
			}
			if workflow.DatabaseName != dbName {
				return fmt.Errorf("role member workflow %q belongs to database %q, not %q", member.ID, workflow.DatabaseName, dbName)
			}
		default:
			return fmt.Errorf("role member type %q is unsupported", member.Type)
		}
	}
	return nil
}

func (server *Server) validateGrantResource(ctx context.Context, dbName string, grant permission.Grant) error {
	switch grant.Scope {
	case permission.ScopeField, permission.ScopeFieldSet:
		if grant.Level < permission.None || grant.Level > permission.FieldAll {
			return fmt.Errorf("field grant level must be a bitmask between 0 and %d, got %d", permission.FieldAll, grant.Level)
		}
	case permission.ScopeView, permission.ScopeViewSet:
		// Views are the row-level boundary and only the owner defines
		// them; a view grant can only confer read.
		if grant.Level < permission.None || grant.Level > permission.Read {
			return fmt.Errorf("view grants are read-only, got level %d", grant.Level)
		}
	default:
		if grant.Level < permission.None || grant.Level > permission.Write {
			return fmt.Errorf("grant level must be between 0 and %d, got %d", permission.Write, grant.Level)
		}
	}
	switch grant.Scope {
	case permission.ScopeFieldSet:
		if grant.Field != "" {
			return errors.New("field set grant cannot include field")
		}
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
		if !ok || field.Deleted || strings.HasPrefix(field.Name, "ct_") {
			return fmt.Errorf("field %s.%s.%s not found", dbName, tableName, grant.Field)
		}
	case permission.ScopeRecord:
		if grant.Level != permission.Write {
			return errors.New("record grant requires write level")
		}
		_, tableName, ok := strings.Cut(grant.Resource, ".")
		if !ok || tableName == "" {
			return fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		if _, ok := server.catalogSnapshot().Table(dbName, tableName); !ok {
			return fmt.Errorf("table %s.%s not found", dbName, tableName)
		}
		if grant.Field != "create" && grant.Field != "delete" {
			return errors.New("record grant field must be create or delete")
		}
	case permission.ScopeViewSet:
		if grant.Field != "" {
			return errors.New("view set grant cannot include field")
		}
		_, tableName, ok := strings.Cut(grant.Resource, ".")
		if !ok || tableName == "" {
			return fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		if _, ok := server.catalogSnapshot().Table(dbName, tableName); !ok {
			return fmt.Errorf("table %s.%s not found", dbName, tableName)
		}
	case permission.ScopeView:
		_, tableName, ok := strings.Cut(grant.Resource, ".")
		if !ok || tableName == "" {
			return fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		tableMeta, ok := server.catalogSnapshot().Table(dbName, tableName)
		if !ok {
			return fmt.Errorf("table %s.%s not found", dbName, tableName)
		}
		if grant.Field == "" {
			return errors.New("view grant requires field")
		}
		found := grant.Field == metadata.AllViewName
		for _, view := range tableMeta.Views {
			if view.Name == grant.Field {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("view %s.%s.%s not found", dbName, tableName, grant.Field)
		}
	case permission.ScopeFile:
		if grant.Field != "" {
			return errors.New("file grant cannot include field")
		}
		_, tableName, ok := strings.Cut(grant.Resource, ".")
		if !ok || tableName == "" {
			return fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		if _, ok := server.catalogSnapshot().Table(dbName, tableName); !ok {
			return fmt.Errorf("table %s.%s not found", dbName, tableName)
		}
	case permission.ScopeFieldAdd:
		if grant.Field != "" {
			return errors.New("field add grant cannot include field")
		}
		_, tableName, ok := strings.Cut(grant.Resource, ".")
		if !ok || tableName == "" {
			return fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		if _, ok := server.catalogSnapshot().Table(dbName, tableName); !ok {
			return fmt.Errorf("table %s.%s not found", dbName, tableName)
		}
	case permission.ScopeFieldModify:
		_, tableName, ok := strings.Cut(grant.Resource, ".")
		if !ok || tableName == "" {
			return fmt.Errorf("grant resource %q must be db.table", grant.Resource)
		}
		tableMeta, ok := server.catalogSnapshot().Table(dbName, tableName)
		if !ok {
			return fmt.Errorf("table %s.%s not found", dbName, tableName)
		}
		if grant.Field == "" {
			return errors.New("field modify grant requires field")
		}
		field, ok := tableMeta.Field(grant.Field)
		if !ok || field.Deleted || strings.HasPrefix(field.Name, "ct_") {
			return fmt.Errorf("field %s.%s.%s not found", dbName, tableName, grant.Field)
		}
		if field.Type == "formula" {
			return errors.New("formula fields are managed by the database owner only")
		}
	case permission.ScopeWorkflowSet:
		if grant.Resource != dbName {
			return fmt.Errorf("workflow set grant resource must be %q", dbName)
		}
		if grant.Field != "" {
			return errors.New("workflow set grant cannot include field")
		}
	case permission.ScopeFormSet:
		if grant.Resource != dbName {
			return fmt.Errorf("form set grant resource must be %q", dbName)
		}
		if grant.Field != "" {
			return errors.New("form set grant cannot include field")
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
	action := accessReadWorkflow
	switch scope {
	case permission.ScopeWorkflow:
		if level >= permission.Write {
			action = accessWriteWorkflow
		}
	case permission.ScopeForm:
		action = accessReadForm
		if level >= permission.Write {
			action = accessWriteForm
		}
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported resource scope %q", scope))
		return false
	}
	_, ok := server.requireAuthorized(w, r, actorID, accessRequest{
		Action:     action,
		WorkflowID: id,
		FormID:     id,
	})
	return ok
}

func (server *Server) filterReadableWorkflows(ctx context.Context, actorID string, workflows []systemdb.WorkflowDefinition) ([]systemdb.WorkflowDefinition, error) {
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return nil, err
	}
	filtered := make([]systemdb.WorkflowDefinition, 0, len(workflows))
	for _, workflow := range workflows {
		isOwner, err := server.system.IsDatabaseOwner(ctx, actorID, workflow.DatabaseName)
		if err != nil {
			return nil, err
		}
		if isOwner ||
			perms.ResourceLevel(actorID, permission.ScopeWorkflowSet, workflow.DatabaseName) >= permission.Read ||
			perms.CanReadResource(actorID, permission.ScopeWorkflow, resourceID(workflow.ID)) {
			workflow.PermissionLevel = server.workflowPermissionLevel(ctx, perms, actorID, workflow)
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
		isOwner, err := server.system.IsDatabaseOwner(ctx, actorID, form.DatabaseName)
		if err != nil {
			return nil, err
		}
		if isOwner ||
			perms.ResourceLevel(actorID, permission.ScopeFormSet, form.DatabaseName) >= permission.Read ||
			perms.CanReadResource(actorID, permission.ScopeForm, resourceID(form.ID)) {
			form.PermissionLevel = server.formPermissionLevel(ctx, perms, actorID, form)
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
	workflow.PermissionLevel = server.workflowPermissionLevel(ctx, perms, actorID, workflow)
	return workflow
}

func (server *Server) formWithPermissionLevel(ctx context.Context, actorID string, form systemdb.FormDefinition) systemdb.FormDefinition {
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return form
	}
	form.PermissionLevel = server.formPermissionLevel(ctx, perms, actorID, form)
	return form
}

func (server *Server) workflowPermissionLevel(ctx context.Context, perms permission.Set, actorID string, workflow systemdb.WorkflowDefinition) permission.Level {
	if server.isDatabaseOwner(ctx, actorID, workflow.DatabaseName) {
		return permission.Write
	}
	return maxPermissionLevel(
		perms.ResourceLevel(actorID, permission.ScopeWorkflowSet, workflow.DatabaseName),
		perms.ResourceLevel(actorID, permission.ScopeWorkflow, resourceID(workflow.ID)),
	)
}

func (server *Server) formPermissionLevel(ctx context.Context, perms permission.Set, actorID string, form systemdb.FormDefinition) permission.Level {
	if server.isDatabaseOwner(ctx, actorID, form.DatabaseName) {
		return permission.Write
	}
	return maxPermissionLevel(
		perms.ResourceLevel(actorID, permission.ScopeFormSet, form.DatabaseName),
		perms.ResourceLevel(actorID, permission.ScopeForm, resourceID(form.ID)),
	)
}

func readableHistoryValues(values map[string]any, perms permission.Set, actorID string, isDatabaseOwner bool, resource string) map[string]any {
	readable := make(map[string]any, len(values))
	for fieldName, value := range values {
		if isDatabaseOwner || perms.CanReadField(actorID, resource, fieldName) {
			readable[fieldName] = value
		}
	}
	return readable
}

func readableHistoryDiff(diff history.RowDiff, perms permission.Set, actorID string, isDatabaseOwner bool, resource string) history.RowDiff {
	if diff == nil {
		return nil
	}
	readable := make(history.RowDiff, len(diff))
	for fieldName, fieldDiff := range diff {
		if isDatabaseOwner || perms.CanReadField(actorID, resource, fieldName) {
			readable[fieldName] = fieldDiff
		}
	}
	return readable
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

func (server *Server) saveWorkflowDefinition(ctx context.Context, actorID string, workflow systemdb.WorkflowDefinition) (systemdb.WorkflowDefinition, error) {
	var previous *systemdb.WorkflowDefinition
	if workflow.ID != 0 {
		existing, err := server.system.Workflow(ctx, workflow.ID)
		if err != nil {
			return systemdb.WorkflowDefinition{}, err
		}
		previous = &existing
	}
	saved, err := server.system.SaveWorkflow(ctx, workflow)
	if err != nil {
		return systemdb.WorkflowDefinition{}, err
	}
	if server.codeFiles == nil {
		return saved, nil
	}
	paths := []string{server.codeFiles.WorkflowScriptPath(saved)}
	if err := server.codeFiles.SaveWorkflowScript(ctx, saved); err != nil {
		return systemdb.WorkflowDefinition{}, err
	}
	if previous != nil && (previous.DatabaseName != saved.DatabaseName || previous.Name != saved.Name) {
		paths = append(paths, server.codeFiles.WorkflowScriptPath(*previous))
		if err := server.codeFiles.DeleteWorkflowScript(ctx, *previous); err != nil {
			return systemdb.WorkflowDefinition{}, err
		}
	}
	summary := "saved workflow " + saved.DatabaseName + "/" + saved.Name
	action := "workflow.save"
	if previous == nil {
		summary = "created workflow " + saved.DatabaseName + "/" + saved.Name
		action = "workflow.create"
	} else if previous.DatabaseName != saved.DatabaseName || previous.Name != saved.Name {
		summary = "renamed workflow " + previous.DatabaseName + "/" + previous.Name + " to " + saved.DatabaseName + "/" + saved.Name
		action = "workflow.rename"
	}
	server.notifyRepositoryChange(ctx, actorID, action, summary, paths...)
	return saved, nil
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
		Enabled:         workflow.Enabled,
		CreatorID:       workflow.CreatorID,
		Secrets:              secretLengths(workflow.Secrets),
		Variables:            workflow.Variables,
		Runners:              workflow.Runners,
		HistoryRetentionDays: workflow.HistoryRetentionDays,
		TimeoutSeconds:       workflow.TimeoutSeconds,
		PermissionLevel:      workflow.PermissionLevel,
		CreatedAt:            workflow.CreatedAt,
		UpdatedAt:            workflow.UpdatedAt,
	}
}

func (server *Server) roleResponses(ctx context.Context, roles []systemdb.RoleDefinition) ([]roleDefinitionResponse, error) {
	responses := make([]roleDefinitionResponse, 0, len(roles))
	for _, role := range roles {
		response, err := server.roleResponse(ctx, role)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func (server *Server) roleResponse(ctx context.Context, role systemdb.RoleDefinition) (roleDefinitionResponse, error) {
	memberUsers := make([]userResponse, 0, len(role.Members))
	memberWorkflows := make([]workflowDefinitionResponse, 0, len(role.Members))
	for _, member := range role.Members {
		switch member.Type {
		case "user":
			user, err := server.system.User(ctx, member.ID)
			if err != nil {
				return roleDefinitionResponse{}, err
			}
			memberUsers = append(memberUsers, toUserResponse(user))
		case "workflow":
			workflowID, err := strconv.ParseInt(member.ID, 10, 64)
			if err != nil {
				return roleDefinitionResponse{}, err
			}
			workflow, err := server.system.Workflow(ctx, workflowID)
			if err != nil {
				return roleDefinitionResponse{}, err
			}
			memberWorkflows = append(memberWorkflows, workflowResponseFromDefinition(workflow))
		}
	}
	return roleDefinitionResponse{
		ID:              role.ID,
		DatabaseName:    role.DatabaseName,
		Name:            role.Name,
		SubjectID:       role.SubjectID,
		Grants:          role.Grants,
		Members:         role.Members,
		MemberUsers:     memberUsers,
		MemberWorkflows: memberWorkflows,
		CreatedAt:       role.CreatedAt,
		UpdatedAt:       role.UpdatedAt,
	}, nil
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

func (server *Server) saveFormDefinition(ctx context.Context, actorID string, form systemdb.FormDefinition) (systemdb.FormDefinition, error) {
	var previous *systemdb.FormDefinition
	if form.ID != 0 {
		existing, err := server.system.Form(ctx, form.ID)
		if err != nil {
			return systemdb.FormDefinition{}, err
		}
		previous = &existing
	}
	saved, err := server.system.SaveForm(ctx, form)
	if err != nil {
		return systemdb.FormDefinition{}, err
	}
	if server.codeFiles == nil {
		return saved, nil
	}
	paths := []string{server.codeFiles.FormScriptPath(saved)}
	if err := server.codeFiles.SaveFormScript(ctx, saved); err != nil {
		return systemdb.FormDefinition{}, err
	}
	if previous != nil && (previous.DatabaseName != saved.DatabaseName || previous.Name != saved.Name) {
		paths = append(paths, server.codeFiles.FormScriptPath(*previous))
		if err := server.codeFiles.DeleteFormScript(ctx, *previous); err != nil {
			return systemdb.FormDefinition{}, err
		}
	}
	summary := "saved form " + saved.DatabaseName + "/" + saved.Name
	action := "form.save"
	if previous == nil {
		summary = "created form " + saved.DatabaseName + "/" + saved.Name
		action = "form.create"
	} else if previous.DatabaseName != saved.DatabaseName || previous.Name != saved.Name {
		summary = "renamed form " + previous.DatabaseName + "/" + previous.Name + " to " + saved.DatabaseName + "/" + saved.Name
		action = "form.rename"
	}
	server.notifyRepositoryChange(ctx, actorID, action, summary, paths...)
	return saved, nil
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

func parseGrantResourceID(resource string) (int64, error) {
	id, err := strconv.ParseInt(resource, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("grant resource %q must be an id", resource)
	}
	return id, nil
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

func defaultServerAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		Password: config.PasswordAuthConfig{Enabled: true},
	}
}

func cloneAuthConfig(authConfig config.AuthConfig) config.AuthConfig {
	authConfig.OIDC.Providers = append([]config.OIDCProvider(nil), authConfig.OIDC.Providers...)
	return authConfig
}

func (server *Server) publicOIDCProviders() []oidcProviderResponse {
	if !server.auth.OIDC.Enabled {
		return []oidcProviderResponse{}
	}
	providers := make([]oidcProviderResponse, 0, len(server.auth.OIDC.Providers))
	for _, provider := range server.auth.OIDC.Providers {
		providers = append(providers, oidcProviderResponse{
			Name:      provider.Name,
			IssuerURL: provider.IssuerURL,
			Scopes:    oidcScopes(provider),
		})
	}
	return providers
}

func (server *Server) oidcProvider(name string) (config.OIDCProvider, bool) {
	for _, provider := range server.auth.OIDC.Providers {
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

func setOIDCRedirectCookie(w http.ResponseWriter, redirect string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcRedirectCookieName,
		Value:    url.QueryEscape(redirect),
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(oidcStateTTL),
	})
}

func clearOIDCRedirectCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcRedirectCookieName,
		Value:    "",
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
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

func oidcAuthorizeURL(provider config.OIDCProvider, state string, callbackURL string) (string, error) {
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
	query.Set("redirect_uri", callbackURL)
	query.Set("scope", strings.Join(oidcScopes(provider), " "))
	query.Set("state", state)
	authorizeURL.RawQuery = query.Encode()
	return authorizeURL.String(), nil
}

func oidcCallbackURL(publicURL string, providerName string) (string, error) {
	if strings.TrimSpace(publicURL) == "" {
		return "", errors.New("server public url is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("server public url must be an absolute URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/auth/oidc/" + url.PathEscape(providerName) + "/callback"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func oidcClaims(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, idToken *oidc.IDToken) (oidcEmailClaims, error) {
	var claims oidcEmailClaims
	if err := idToken.Claims(&claims); err != nil {
		return oidcEmailClaims{}, err
	}
	if claims.Email != "" && claims.Name != "" {
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
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Provider:    string(user.Provider),
	}
}

func ContextWithUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userContextKey{}, userID)
}

type userContextKey struct{}

const defaultFileUploadBytes = 20 << 20

// multipart form overhead on top of the file itself: boundaries plus the
// binding fields.
const uploadFormOverheadBytes = 1 << 20

func (server *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	if server.files == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("file storage is not configured"))
		return
	}
	limit := server.uploadLimit()
	r.Body = http.MaxBytesReader(w, r.Body, limit+uploadFormOverheadBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read multipart file field: %w", err))
		return
	}
	defer file.Close()
	if header.Size > limit {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("file is %d bytes, the upload limit is %d bytes", header.Size, limit))
		return
	}
	dbName := strings.TrimSpace(r.FormValue("database_name"))
	tableName := strings.TrimSpace(r.FormValue("table_name"))
	if dbName == "" || tableName == "" {
		writeError(w, http.StatusBadRequest, errors.New("database_name and table_name are required"))
		return
	}
	recordID := int64(0)
	if recordText := strings.TrimSpace(r.FormValue("record_id")); recordText != "" {
		recordID, err = strconv.ParseInt(recordText, 10, 64)
		if err != nil || recordID < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("record_id must be a non-negative integer, got %q", recordText))
			return
		}
	}
	if _, ok := server.catalogSnapshot().Table(dbName, tableName); !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("table %s.%s does not exist", dbName, tableName))
		return
	}
	if !server.canAccessTableFiles(r.Context(), actorID, dbName, tableName) {
		writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	record, err := server.system.CreateFile(r.Context(), systemdb.FileRecord{
		Name:         sanitizeFileName(header.Filename),
		Size:         header.Size,
		ContentType:  contentType,
		CreatorID:    actorID,
		DatabaseName: dbName,
		TableName:    tableName,
		RecordID:     recordID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := server.files.Put(r.Context(), record.ID, record.Name, record.ContentType, record.Size, file); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("store file object: %w", err))
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

type fileMetadataRequest struct {
	IDs []int64 `json:"ids"`
}

func (server *Server) handleFileMetadata(w http.ResponseWriter, r *http.Request) {
	actorID, ok := server.requireUserID(w, r)
	if !ok {
		return
	}
	var request fileMetadataRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	records := make([]systemdb.FileRecord, 0, len(request.IDs))
	for _, id := range request.IDs {
		record, err := server.system.File(r.Context(), id)
		if errors.Is(err, systemdb.ErrFileNotFound) {
			continue
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if !server.canViewFile(r.Context(), actorID, record) {
			continue
		}
		records = append(records, record)
	}
	writeJSON(w, http.StatusOK, records)
}

func (server *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	actorID, hasSession, err := server.currentUserID(r)
	if err != nil || !hasSession {
		// A browser navigating to a shared file link goes through the login
		// page and comes back; API clients keep getting a plain 401.
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/login?redirect="+url.QueryEscape(r.URL.Path), http.StatusFound)
			return
		}
		writeError(w, http.StatusUnauthorized, errors.New("authentication is required"))
		return
	}
	id, ok := pathID(r, "fileID")
	if !ok {
		http.NotFound(w, r)
		return
	}
	record, err := server.system.File(r.Context(), id)
	if errors.Is(err, systemdb.ErrFileNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !server.canViewFile(r.Context(), actorID, record) {
		writeError(w, http.StatusForbidden, table.ErrPermissionDenied)
		return
	}
	if server.files == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("file storage is not configured"))
		return
	}
	body, err := server.files.Get(r.Context(), record.ID, record.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("load file object: %w", err))
		return
	}
	defer body.Close()
	disposition := "attachment"
	if previewableContentType(record.ContentType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", record.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(record.Size, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": record.Name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// previewableContentType lists types browsers render safely inline. SVG is
// deliberately excluded: it can carry scripts and would run on our origin.
func previewableContentType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", "image/avif":
		return true
	default:
		return false
	}
}

// canViewFile allows downloads only for files already bound to a record:
// permissions are mandatory, so an upload that no row references yet is
// visible to nobody. The viewer additionally needs table file access.
// canViewFile is row-level: besides the table file grant, the record the
// file is bound to must be inside the actor's row set.
func (server *Server) canViewFile(ctx context.Context, actorID string, record systemdb.FileRecord) bool {
	if record.RecordID <= 0 {
		return false
	}
	if !server.canAccessTableFiles(ctx, actorID, record.DatabaseName, record.TableName) {
		return false
	}
	if server.isDatabaseOwner(ctx, actorID, record.DatabaseName) {
		return true
	}
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return false
	}
	inRowSet, err := server.tables.RowInActorRowSet(ctx, server.catalogSnapshot(), perms, actorID, false, record.DatabaseName, record.TableName, record.RecordID)
	return err == nil && inRowSet
}

// canAccessTableFiles reports whether the actor may work with files of the
// given table: the database owner always may, everyone else needs a
// file-scope grant on the table.
func (server *Server) canAccessTableFiles(ctx context.Context, actorID string, dbName string, tableName string) bool {
	if dbName == "" {
		return false
	}
	if server.isDatabaseOwner(ctx, actorID, dbName) {
		return true
	}
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return false
	}
	resource := tableResource(dbName, tableName)
	return resource != "" && perms.CanReadResource(actorID, permission.ScopeFile, resource)
}

// sanitizeFileName keeps the original filename as the S3 object name while
// stripping path segments and control characters.
func sanitizeFileName(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}
