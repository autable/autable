// Package runnerhub accepts runner connections and sends them fetch jobs.
// A runner is an egress proxy for workflows (docs/design/workflows-v2.md):
// it performs the HTTP requests a workflow's fetch(..., { via }) names and
// executes no code.
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

	"autable/internal/egress"
)

const (
	// readTimeout closes connections whose runner stopped pinging.
	readTimeout = 90 * time.Second
	writeWait   = 10 * time.Second
	// jobSlack is added to a request's own timeout before the hub gives up
	// on a runner's answer.
	jobSlack = 15 * time.Second
	// maxMessageBytes bounds one frame: a capped body, base64-encoded,
	// plus headers.
	maxMessageBytes = egress.MaxResponseBytes*4/3 + 1<<20
)

// HelloMessage is the first frame a runner sends after connecting.
type HelloMessage struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// JobMessage carries one fetch from the server to a runner.
type JobMessage struct {
	Kind    string         `json:"kind"`
	JobID   string         `json:"job_id"`
	Request egress.Request `json:"request"`
}

// ResultMessage carries the response, or the error, back from a runner.
type ResultMessage struct {
	Kind     string           `json:"kind"`
	JobID    string           `json:"job_id"`
	Response *egress.Response `json:"response,omitempty"`
	Error    string           `json:"error,omitempty"`
}

// RunnerStatus describes one live runner connection.
type RunnerStatus struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	ConnectedAt int64  `json:"connected_at"`
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
	connectedAt int64
	send        chan any
	closed      chan struct{}
	closeOnce   sync.Once
}

// Hub accepts runner connections and dispatches fetch jobs to them.
type Hub struct {
	resolve  TokenResolver
	upgrader websocket.Upgrader
	// slack is how long past the request's own timeout the hub waits.
	slack time.Duration

	mu    sync.Mutex
	conns map[*runnerConn]struct{}
	jobs  map[string]*pendingJob
}

func New(resolve TokenResolver) *Hub {
	return &Hub{
		resolve: resolve,
		slack:   jobSlack,
		conns:   map[*runnerConn]struct{}{},
		jobs:    map[string]*pendingJob{},
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
	ws.SetReadLimit(maxMessageBytes)
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
	conn := &runnerConn{
		hub:         hub,
		ws:          ws,
		database:    database,
		name:        hello.Name,
		version:     hello.Version,
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
		job.result <- ResultMessage{Error: fmt.Sprintf("runner %q disconnected while the request was running", conn.name)}
	}
}

// Fetch has the named runner of the database perform the request.
func (hub *Hub) Fetch(ctx context.Context, database, runnerName string, request egress.Request) (egress.Response, error) {
	conn, ok := hub.pick(database, runnerName)
	if !ok {
		return egress.Response{}, fmt.Errorf("runner %q is not connected", runnerName)
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

	select {
	case conn.send <- JobMessage{Kind: "fetch", JobID: jobID, Request: request}:
	case <-conn.closed:
		return egress.Response{}, fmt.Errorf("runner %q is not connected", runnerName)
	case <-ctx.Done():
		return egress.Response{}, ctx.Err()
	}

	wait := request.Timeout() + hub.slack
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case result := <-pending.result:
		if result.Error != "" {
			return egress.Response{}, fmt.Errorf("runner %q: %s", runnerName, result.Error)
		}
		if result.Response == nil {
			return egress.Response{}, fmt.Errorf("runner %q returned no response", runnerName)
		}
		if len(result.Response.Body) > egress.MaxResponseBytes {
			return egress.Response{}, egress.ErrResponseTooLarge
		}
		return *result.Response, nil
	case <-timer.C:
		return egress.Response{}, fmt.Errorf("runner %q did not answer within %s", runnerName, wait)
	case <-ctx.Done():
		return egress.Response{}, ctx.Err()
	}
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
		statuses = append(statuses, RunnerStatus{Name: conn.name, Version: conn.version, ConnectedAt: conn.connectedAt})
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

// pick returns the most recently connected runner with the name.
func (hub *Hub) pick(databaseName, runnerName string) (*runnerConn, bool) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	var picked *runnerConn
	for conn := range hub.conns {
		if conn.database == databaseName && conn.name == runnerName && (picked == nil || conn.connectedAt > picked.connectedAt) {
			picked = conn
		}
	}
	return picked, picked != nil
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
