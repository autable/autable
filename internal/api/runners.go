package api

import (
	"fmt"
	"net/http"
	"sort"

	"autable/internal/runnerhub"
)

type runnerTokenResponse struct {
	Exists    bool  `json:"exists"`
	CreatedAt int64 `json:"created_at,omitempty"`
}

type runnersResponse struct {
	Token   runnerTokenResponse      `json:"token"`
	Runners []runnerhub.RunnerStatus `json:"runners"`
}

type runnerTokenResetResponse struct {
	Token     string `json:"token"`
	CreatedAt int64  `json:"created_at"`
}

// handleDatabaseRunners serves GET /api/databases/{db}/runners to admins:
// the database's connected runners and its token metadata. Runners are
// database-scoped egress proxies for workflow fetch calls.
func (server *Server) handleDatabaseRunners(w http.ResponseWriter, r *http.Request, actorID, dbName string) {
	if !server.requireAdmin(w, r, actorID) {
		return
	}
	createdAt, exists, err := server.system.RunnerTokenCreatedAt(r.Context(), dbName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response := runnersResponse{
		Token:   runnerTokenResponse{Exists: exists, CreatedAt: createdAt},
		Runners: server.runnerHub.Runners(dbName),
	}
	sort.Slice(response.Runners, func(i, j int) bool {
		if response.Runners[i].Name != response.Runners[j].Name {
			return response.Runners[i].Name < response.Runners[j].Name
		}
		return response.Runners[i].ConnectedAt < response.Runners[j].ConnectedAt
	})
	writeJSON(w, http.StatusOK, response)
}

// handleResetDatabaseRunnerToken serves POST /api/databases/{db}/runners:
// an admin rotates the database's runner token, which drops its connected
// runners.
func (server *Server) handleResetDatabaseRunnerToken(w http.ResponseWriter, r *http.Request, actorID, dbName string) {
	if !server.requireAdmin(w, r, actorID) {
		return
	}
	if !server.databaseExists(dbName) {
		writeError(w, http.StatusNotFound, fmt.Errorf("database %q not found", dbName))
		return
	}
	token, err := server.system.ResetRunnerToken(r.Context(), dbName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.runnerHub.DisconnectDatabase(dbName)
	createdAt, _, err := server.system.RunnerTokenCreatedAt(r.Context(), dbName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, runnerTokenResetResponse{Token: token, CreatedAt: createdAt})
}
