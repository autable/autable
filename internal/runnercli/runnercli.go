package runnercli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"autable/internal/runnerhub"
	"autable/internal/version"
	"autable/internal/workflow"
)

const (
	pingInterval     = 30 * time.Second
	readTimeout      = 90 * time.Second
	writeWait        = 10 * time.Second
	reconnectMinWait = time.Second
	reconnectMaxWait = time.Minute
)

type Options struct {
	// Endpoint is the autable server base URL (http, https, ws, or wss);
	// the runner WebSocket path is appended when missing.
	Endpoint string
	Token    string
	Name     string
	MaxJobs  int
	Logger   *slog.Logger
}

// Run connects to the server and executes dispatched jobs until ctx is
// cancelled, reconnecting with exponential backoff on any disconnect.
func Run(ctx context.Context, options Options, nodes []workflow.Node) error {
	if options.Endpoint == "" {
		return errors.New("endpoint is required")
	}
	if options.Token == "" {
		return errors.New("token is required")
	}
	if options.Name == "" {
		return errors.New("runner name is required")
	}
	if options.MaxJobs <= 0 {
		options.MaxJobs = 4
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	endpoint, err := websocketEndpoint(options.Endpoint)
	if err != nil {
		return err
	}
	registry := map[string]workflow.Node{}
	for _, node := range nodes {
		registry[node.Info().Type] = node
	}
	if len(registry) == 0 {
		return errors.New("at least one node is required")
	}

	wait := reconnectMinWait
	for {
		connectedAt := time.Now()
		err := serveConnection(ctx, endpoint, options, registry)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(connectedAt) > reconnectMaxWait {
			wait = reconnectMinWait
		}
		options.Logger.Warn("runner disconnected", "error", err, "retry_in", wait)
		jitter := time.Duration(rand.Int64N(int64(wait) / 2))
		select {
		case <-time.After(wait + jitter):
		case <-ctx.Done():
			return nil
		}
		wait = min(wait*2, reconnectMaxWait)
	}
}

func websocketEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("endpoint scheme %q is not supported", parsed.Scheme)
	}
	if !strings.HasSuffix(parsed.Path, "/api/runner/ws") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/api/runner/ws"
	}
	return parsed.String(), nil
}

func serveConnection(ctx context.Context, endpoint string, options Options, registry map[string]workflow.Node) error {
	header := http.Header{"Authorization": {"Bearer " + options.Token}}
	ws, response, err := websocket.DefaultDialer.DialContext(ctx, endpoint, header)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("server rejected the runner token: %w", err)
		}
		return err
	}
	defer ws.Close()

	nodeTypes := make([]string, 0, len(registry))
	for nodeType := range registry {
		nodeTypes = append(nodeTypes, nodeType)
	}
	hello := runnerhub.HelloMessage{Kind: "hello", Name: options.Name, Version: version.String(), NodeTypes: nodeTypes}
	writeMu := &sync.Mutex{}
	if err := writeJSON(ws, writeMu, hello); err != nil {
		return err
	}
	options.Logger.Info("runner connected", "endpoint", endpoint, "name", options.Name, "nodes", len(nodeTypes))

	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				writeMu.Lock()
				err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
				writeMu.Unlock()
				if err != nil {
					ws.Close()
					return
				}
			case <-done:
				return
			case <-ctx.Done():
				ws.Close()
				return
			}
		}
	}()

	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(readTimeout))
	})
	jobs := make(chan struct{}, options.MaxJobs)
	for {
		ws.SetReadDeadline(time.Now().Add(readTimeout))
		var job runnerhub.JobMessage
		if err := ws.ReadJSON(&job); err != nil {
			return err
		}
		if job.Kind != "job" || job.JobID == "" {
			continue
		}
		select {
		case jobs <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		go func(job runnerhub.JobMessage) {
			defer func() { <-jobs }()
			result := executeJob(ctx, registry, options.Logger, job)
			if err := writeJSON(ws, writeMu, result); err != nil {
				options.Logger.Warn("job result could not be delivered", "job_id", job.JobID, "error", err)
			}
		}(job)
	}
}

func executeJob(ctx context.Context, registry map[string]workflow.Node, logger *slog.Logger, job runnerhub.JobMessage) runnerhub.ResultMessage {
	node, ok := registry[job.NodeType]
	if !ok {
		return runnerhub.ResultMessage{Kind: "result", JobID: job.JobID, Error: fmt.Sprintf("node %q is not registered on this runner", job.NodeType)}
	}
	logger.Info("job started", "job_id", job.JobID, "node", job.NodeType, "instance", job.Runtime.InstanceID)
	output, err := runNode(ctx, node, job)
	if err != nil {
		logger.Warn("job failed", "job_id", job.JobID, "node", job.NodeType, "error", err)
		return runnerhub.ResultMessage{Kind: "result", JobID: job.JobID, Error: err.Error()}
	}
	logger.Info("job finished", "job_id", job.JobID, "node", job.NodeType)
	return runnerhub.ResultMessage{Kind: "result", JobID: job.JobID, Output: output}
}

func runNode(ctx context.Context, node workflow.Node, job runnerhub.JobMessage) (output map[string]any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("node %q panicked: %v", job.NodeType, recovered)
		}
	}()
	return node.Run(ctx, job.Input, workflow.RuntimeInfo{
		WorkflowID:   job.Runtime.WorkflowID,
		DatabaseName: job.Runtime.DatabaseName,
		RunID:        job.Runtime.RunID,
		InstanceID:   job.Runtime.InstanceID,
		NodeType:     job.NodeType,
		CreatorID:    job.Runtime.CreatorID,
		Secrets:      job.Secrets,
		Variables:    job.Variables,
	})
}

func writeJSON(ws *websocket.Conn, mu *sync.Mutex, value any) error {
	mu.Lock()
	defer mu.Unlock()
	ws.SetWriteDeadline(time.Now().Add(writeWait))
	return ws.WriteJSON(value)
}
