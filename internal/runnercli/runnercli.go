// Package runnercli is autable-runner: an egress proxy that connects to
// the server and performs the HTTP requests workflows send through it
// (fetch with `via`). It executes no workflow code.
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

	"autable/internal/egress"
	"autable/internal/runnerhub"
	"autable/internal/version"
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

// Run connects to the server and performs fetch jobs until ctx is
// cancelled, reconnecting with exponential backoff on any disconnect.
func Run(ctx context.Context, options Options) error {
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
	client := egress.NewClient()

	wait := reconnectMinWait
	for {
		connectedAt := time.Now()
		err := serveConnection(ctx, endpoint, options, client)
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

func serveConnection(ctx context.Context, endpoint string, options Options, client *http.Client) error {
	header := http.Header{"Authorization": {"Bearer " + options.Token}}
	ws, response, err := websocket.DefaultDialer.DialContext(ctx, endpoint, header)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("server rejected the runner token: %w", err)
		}
		return err
	}
	defer ws.Close()

	hello := runnerhub.HelloMessage{Kind: "hello", Name: options.Name, Version: version.String()}
	writeMu := &sync.Mutex{}
	if err := writeJSON(ws, writeMu, hello); err != nil {
		return err
	}
	options.Logger.Info("runner connected", "endpoint", endpoint, "name", options.Name)

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
		if job.Kind != "fetch" || job.JobID == "" {
			continue
		}
		select {
		case jobs <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		go func(job runnerhub.JobMessage) {
			defer func() { <-jobs }()
			result := executeJob(ctx, client, options.Logger, job)
			if err := writeJSON(ws, writeMu, result); err != nil {
				options.Logger.Warn("job result could not be delivered", "job_id", job.JobID, "error", err)
			}
		}(job)
	}
}

func executeJob(ctx context.Context, client *http.Client, logger *slog.Logger, job runnerhub.JobMessage) runnerhub.ResultMessage {
	started := time.Now()
	response, err := egress.Do(ctx, client, job.Request)
	elapsed := time.Since(started).Round(time.Millisecond)
	if err != nil {
		logger.Warn("fetch failed", "job_id", job.JobID, "method", job.Request.Method, "url", logURL(job.Request.URL), "elapsed", elapsed, "error", err)
		return runnerhub.ResultMessage{Kind: "result", JobID: job.JobID, Error: err.Error()}
	}
	logger.Info("fetch finished", "job_id", job.JobID, "method", job.Request.Method, "url", logURL(job.Request.URL), "status", response.Status, "elapsed", elapsed)
	return runnerhub.ResultMessage{Kind: "result", JobID: job.JobID, Response: &response}
}

// logURL drops the query and fragment, which often carry credentials.
func logURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "(invalid url)"
	}
	parsed.RawQuery, parsed.Fragment, parsed.User = "", "", nil
	return parsed.String()
}

func writeJSON(ws *websocket.Conn, mu *sync.Mutex, value any) error {
	mu.Lock()
	defer mu.Unlock()
	ws.SetWriteDeadline(time.Now().Add(writeWait))
	return ws.WriteJSON(value)
}
