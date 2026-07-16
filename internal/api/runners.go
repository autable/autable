package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"autable/internal/runnerhub"
	"autable/internal/systemdb"
	"autable/internal/workflow"
	"autable/internal/workflow/nodes"
)

type runnerTokenResponse struct {
	Exists    bool  `json:"exists"`
	CreatedAt int64 `json:"created_at,omitempty"`
}

type runnersResponse struct {
	// Token metadata is only included for the database owner.
	Token *runnerTokenResponse `json:"token,omitempty"`
	// CanManage reports whether the caller may view and reset the token.
	CanManage       bool                     `json:"can_manage"`
	Runners         []runnerhub.RunnerStatus `json:"runners"`
	RemoteNodeTypes []string                 `json:"remote_node_types"`
}

type runnerTokenResetResponse struct {
	Token     string `json:"token"`
	CreatedAt int64  `json:"created_at"`
}

// handleDatabaseRunners serves GET /api/databases/{db}/runners: the
// database's connected runners for anyone signed in, plus token metadata for
// the database owner. Runners are database-scoped resources.
func (server *Server) handleDatabaseRunners(w http.ResponseWriter, r *http.Request, actorID, dbName string) {
	response := runnersResponse{
		CanManage:       server.isDatabaseOwner(r.Context(), actorID, dbName),
		Runners:         server.runnerHub.Runners(dbName),
		RemoteNodeTypes: remoteNodeTypes(),
	}
	if response.CanManage {
		createdAt, exists, err := server.system.RunnerTokenCreatedAt(r.Context(), dbName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		response.Token = &runnerTokenResponse{Exists: exists, CreatedAt: createdAt}
	}
	sort.Slice(response.Runners, func(i, j int) bool {
		if response.Runners[i].Name != response.Runners[j].Name {
			return response.Runners[i].Name < response.Runners[j].Name
		}
		return response.Runners[i].ConnectedAt < response.Runners[j].ConnectedAt
	})
	for _, runner := range response.Runners {
		sort.Strings(runner.NodeTypes)
	}
	writeJSON(w, http.StatusOK, response)
}

// handleResetDatabaseRunnerToken serves POST /api/databases/{db}/runners:
// the database owner rotates the database's runner token, which drops its
// connected runners.
func (server *Server) handleResetDatabaseRunnerToken(w http.ResponseWriter, r *http.Request, actorID, dbName string) {
	if !server.isDatabaseOwner(r.Context(), actorID, dbName) {
		writeError(w, http.StatusForbidden, errors.New("only the database owner can reset the runner token"))
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

// validateRunnerBindings rejects bindings to undeclared instances, trigger
// instances, and node types that cannot execute on a remote runner.
func (server *Server) validateRunnerBindings(ctx context.Context, definition systemdb.WorkflowDefinition) error {
	if len(definition.Runners) == 0 {
		return nil
	}
	instances, err := server.runner.Instances(ctx, workflow.Definition{
		ID:           definition.ID,
		DatabaseName: definition.DatabaseName,
		Script:       definition.Script,
	})
	if err != nil {
		return err
	}
	remote := map[string]bool{}
	for _, node := range nodes.Remote() {
		remote[node.Info().Type] = true
	}
	nodeInfos := map[string]workflow.NodeInfo{}
	for _, info := range server.runner.NodeInfos() {
		nodeInfos[info.Type] = info
	}
	for instanceID, runnerName := range definition.Runners {
		if runnerName == "" {
			return fmt.Errorf("workflow instance %q runner name must not be empty", instanceID)
		}
		declaration, ok := instances[instanceID]
		if !ok {
			return fmt.Errorf("workflow instance %q is not declared", instanceID)
		}
		if nodeInfos[declaration.Node].Trigger {
			return fmt.Errorf("trigger instance %q cannot be bound to a remote runner", instanceID)
		}
		if !remote[declaration.Node] {
			return fmt.Errorf("node %q of instance %q cannot execute on a remote runner", declaration.Node, instanceID)
		}
	}
	return nil
}

func remoteNodeTypes() []string {
	types := make([]string, 0)
	for _, node := range nodes.Remote() {
		types = append(types, node.Info().Type)
	}
	sort.Strings(types)
	return types
}
