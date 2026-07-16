package api

import (
	"context"
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
	Token           runnerTokenResponse      `json:"token"`
	Runners         []runnerhub.RunnerStatus `json:"runners"`
	RemoteNodeTypes []string                 `json:"remote_node_types"`
}

type runnerTokenResetResponse struct {
	Token     string `json:"token"`
	CreatedAt int64  `json:"created_at"`
}

func (server *Server) handleListRunners(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireUserID(w, r); !ok {
		return
	}
	createdAt, exists, err := server.system.RunnerTokenCreatedAt(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	runners := server.runnerHub.Runners()
	sort.Slice(runners, func(i, j int) bool {
		if runners[i].Name != runners[j].Name {
			return runners[i].Name < runners[j].Name
		}
		return runners[i].ConnectedAt < runners[j].ConnectedAt
	})
	for _, runner := range runners {
		sort.Strings(runner.NodeTypes)
	}
	writeJSON(w, http.StatusOK, runnersResponse{
		Token:           runnerTokenResponse{Exists: exists, CreatedAt: createdAt},
		Runners:         runners,
		RemoteNodeTypes: remoteNodeTypes(),
	})
}

func (server *Server) handleResetRunnerToken(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireUserID(w, r); !ok {
		return
	}
	token, err := server.system.ResetRunnerToken(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	server.runnerHub.DisconnectAll()
	createdAt, _, err := server.system.RunnerTokenCreatedAt(r.Context())
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
