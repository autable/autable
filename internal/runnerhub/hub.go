package runnerhub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"autable/internal/workflow"
)

const (
	// readTimeout closes connections whose runner stopped pinging.
	readTimeout = 90 * time.Second
	writeWait   = 10 * time.Second
	// DefaultJobTimeout bounds a single dispatched node execution.
	DefaultJobTimeout = 10 * time.Minute
)

// HelloMessage is the first frame a runner sends after connecting.
type HelloMessage struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	NodeTypes []string `json:"node_types"`
}

// JobMessage carries one node execution from the server to a runner.
type JobMessage struct {
	Kind      string            `json:"kind"`
	JobID     string            `json:"job_id"`
	NodeType  string            `json:"node_type"`
	Input     map[string]any    `json:"input,omitempty"`
	Secrets   map[string]string `json:"secrets,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
	Runtime   RuntimeMessage    `json:"runtime"`
}

// RuntimeMessage is the wire form of workflow.RuntimeInfo without the secret
// and variable maps, which travel as JobMessage fields.
type RuntimeMessage struct {
	WorkflowID   int64  `json:"workflow_id"`
	DatabaseName string `json:"database_name"`
	RunID        string `json:"run_id"`
	InstanceID   string `json:"instance_id"`
	CreatorID    string `json:"creator_id,omitempty"`
}

// ResultMessage carries a node output or error back from a runner.
type ResultMessage struct {
	Kind   string         `json:"kind"`
	JobID  string         `json:"job_id"`
	Output map[string]any `json:"output,omitempty"`
	Error  string         `json:"error,omitempty"`
}

// RunnerStatus describes one live runner connection.
type RunnerStatus struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	NodeTypes   []string `json:"node_types"`
	ConnectedAt int64    `json:"connected_at"`
}

// TokenResolver resolves a presented runner token to the database the
// runner serves; ok is false for unknown tokens. Runners are
// database-scoped: the token decides the database, so a runner cannot
// claim another database's name space.
type TokenResolver func(ctx context.Context, token string) (database string, ok bool, err error)

type pendingJob struct {
	result chan ResultMessage
	conn   *runnerConn
}

type runnerConn struct {
	hub         *Hub
	ws          *websocket.Conn
	database    string
	name        string
	version     string
	nodeTypes   map[string]bool
	connectedAt int64
	send        chan any
	closed      chan struct{}
	closeOnce   sync.Once
}

// Hub accepts runner connections and dispatches node jobs to them. It
// implements workflow.RemoteDispatcher.
type Hub struct {
	resolve    TokenResolver
	jobTimeout time.Duration
	upgrader   websocket.Upgrader

	mu    sync.Mutex
	conns map[*runnerConn]struct{}
	jobs  map[string]*pendingJob
}

func New(resolve TokenResolver, jobTimeout time.Duration) *Hub {
	if jobTimeout <= 0 {
		jobTimeout = DefaultJobTimeout
	}
	return &Hub{
		resolve:    resolve,
		jobTimeout: jobTimeout,
		conns:      map[*runnerConn]struct{}{},
		jobs:       map[string]*pendingJob{},
	}
}

// ServeWS upgrades an authenticated runner connection and serves it until it
// disconnects.
func (hub *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		http.Error(w, "runner token is required", http.StatusUnauthorized)
		return
	}
	database, ok, err := hub.resolve(r.Context(), token)
	if err != nil {
		http.Error(w, "resolve runner token", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "invalid runner token", http.StatusUnauthorized)
		return
	}
	ws, err := hub.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn, err := hub.register(ws, database)
	if err != nil {
		ws.Close()
		return
	}
	go conn.writeLoop()
	conn.readLoop()
	hub.unregister(conn)
}

func (hub *Hub) register(ws *websocket.Conn, database string) (*runnerConn, error) {
	ws.SetReadDeadline(time.Now().Add(readTimeout))
	var hello HelloMessage
	if err := ws.ReadJSON(&hello); err != nil {
		return nil, err
	}
	if hello.Kind != "hello" || hello.Name == "" {
		return nil, errors.New("runner hello with a name is required")
	}
	nodeTypes := map[string]bool{}
	for _, nodeType := range hello.NodeTypes {
		nodeTypes[nodeType] = true
	}
	conn := &runnerConn{
		hub:         hub,
		ws:          ws,
		database:    database,
		name:        hello.Name,
		version:     hello.Version,
		nodeTypes:   nodeTypes,
		connectedAt: time.Now().UTC().UnixMilli(),
		send:        make(chan any, 16),
		closed:      make(chan struct{}),
	}
	hub.mu.Lock()
	hub.conns[conn] = struct{}{}
	hub.mu.Unlock()
	return conn, nil
}

func (hub *Hub) unregister(conn *runnerConn) {
	conn.close()
	hub.mu.Lock()
	delete(hub.conns, conn)
	var orphaned []*pendingJob
	for jobID, job := range hub.jobs {
		if job.conn == conn {
			orphaned = append(orphaned, job)
			delete(hub.jobs, jobID)
		}
	}
	hub.mu.Unlock()
	for _, job := range orphaned {
		job.result <- ResultMessage{Error: fmt.Sprintf("runner %q disconnected while the job was running", conn.name)}
	}
}

// Dispatch implements workflow.RemoteDispatcher; the job's database decides
// which database's runners are eligible.
func (hub *Hub) Dispatch(ctx context.Context, runnerName string, job workflow.RemoteJob) (map[string]any, error) {
	conn, ok := hub.pick(job.Runtime.DatabaseName, runnerName)
	if !ok {
		return nil, fmt.Errorf("runner %q is not connected", runnerName)
	}
	if !conn.nodeTypes[job.Runtime.NodeType] {
		return nil, fmt.Errorf("runner %q does not support node %q", runnerName, job.Runtime.NodeType)
	}
	jobID := uuid.NewString()
	pending := &pendingJob{result: make(chan ResultMessage, 1), conn: conn}
	hub.mu.Lock()
	hub.jobs[jobID] = pending
	hub.mu.Unlock()
	defer func() {
		hub.mu.Lock()
		delete(hub.jobs, jobID)
		hub.mu.Unlock()
	}()

	message := JobMessage{
		Kind:      "job",
		JobID:     jobID,
		NodeType:  job.Runtime.NodeType,
		Input:     job.Input,
		Secrets:   job.Runtime.Secrets,
		Variables: job.Runtime.Variables,
		Runtime: RuntimeMessage{
			WorkflowID:   job.Runtime.WorkflowID,
			DatabaseName: job.Runtime.DatabaseName,
			RunID:        job.Runtime.RunID,
			InstanceID:   job.Runtime.InstanceID,
			CreatorID:    job.Runtime.CreatorID,
		},
	}
	select {
	case conn.send <- message:
	case <-conn.closed:
		return nil, fmt.Errorf("runner %q is not connected", runnerName)
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	timer := time.NewTimer(hub.jobTimeout)
	defer timer.Stop()
	select {
	case result := <-pending.result:
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return result.Output, nil
	case <-timer.C:
		return nil, fmt.Errorf("runner %q job timed out after %s", runnerName, hub.jobTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// NodeTypes implements workflow.RemoteDispatcher.
func (hub *Hub) NodeTypes(databaseName, runnerName string) ([]string, bool) {
	conn, ok := hub.pick(databaseName, runnerName)
	if !ok {
		return nil, false
	}
	nodeTypes := make([]string, 0, len(conn.nodeTypes))
	for nodeType := range conn.nodeTypes {
		nodeTypes = append(nodeTypes, nodeType)
	}
	return nodeTypes, true
}

// Runners lists the live runner connections serving one database.
func (hub *Hub) Runners(databaseName string) []RunnerStatus {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	statuses := make([]RunnerStatus, 0, len(hub.conns))
	for conn := range hub.conns {
		if conn.database != databaseName {
			continue
		}
		nodeTypes := make([]string, 0, len(conn.nodeTypes))
		for nodeType := range conn.nodeTypes {
			nodeTypes = append(nodeTypes, nodeType)
		}
		statuses = append(statuses, RunnerStatus{
			Name:        conn.name,
			Version:     conn.version,
			NodeTypes:   nodeTypes,
			ConnectedAt: conn.connectedAt,
		})
	}
	return statuses
}

// DisconnectDatabase drops the database's runner connections; used when its
// token is reset.
func (hub *Hub) DisconnectDatabase(databaseName string) {
	hub.mu.Lock()
	conns := make([]*runnerConn, 0, len(hub.conns))
	for conn := range hub.conns {
		if conn.database == databaseName {
			conns = append(conns, conn)
		}
	}
	hub.mu.Unlock()
	for _, conn := range conns {
		conn.close()
	}
}

func (hub *Hub) pick(databaseName, runnerName string) (*runnerConn, bool) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for conn := range hub.conns {
		if conn.database == databaseName && conn.name == runnerName {
			return conn, true
		}
	}
	return nil, false
}

func (conn *runnerConn) readLoop() {
	conn.ws.SetPingHandler(func(payload string) error {
		conn.ws.SetReadDeadline(time.Now().Add(readTimeout))
		return conn.ws.WriteControl(websocket.PongMessage, []byte(payload), time.Now().Add(writeWait))
	})
	for {
		conn.ws.SetReadDeadline(time.Now().Add(readTimeout))
		var result ResultMessage
		if err := conn.ws.ReadJSON(&result); err != nil {
			return
		}
		if result.Kind != "result" || result.JobID == "" {
			continue
		}
		conn.hub.mu.Lock()
		pending, ok := conn.hub.jobs[result.JobID]
		if ok {
			delete(conn.hub.jobs, result.JobID)
		}
		conn.hub.mu.Unlock()
		if ok {
			pending.result <- result
		}
	}
}

func (conn *runnerConn) writeLoop() {
	for {
		select {
		case message := <-conn.send:
			conn.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.ws.WriteJSON(message); err != nil {
				conn.close()
				return
			}
		case <-conn.closed:
			return
		}
	}
}

func (conn *runnerConn) close() {
	conn.closeOnce.Do(func() {
		close(conn.closed)
		conn.ws.Close()
	})
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found || token == "" {
		return "", false
	}
	return token, true
}
