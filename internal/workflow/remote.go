package workflow

import "context"

// RemoteJob is one node execution dispatched to a remote runner.
type RemoteJob struct {
	Input   map[string]any
	Runtime RuntimeInfo
}

// RemoteDispatcher executes node jobs on connected remote runners.
type RemoteDispatcher interface {
	// Dispatch blocks until the named runner returns the node output or the
	// job fails (runner not connected, disconnect mid-flight, timeout).
	Dispatch(ctx context.Context, runnerName string, job RemoteJob) (map[string]any, error)
	// NodeTypes reports the node types advertised by the named runner of one
	// database and whether that runner has at least one live connection.
	// Runners are database-scoped resources.
	NodeTypes(databaseName, runnerName string) ([]string, bool)
}
