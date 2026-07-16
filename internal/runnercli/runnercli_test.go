package runnercli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"autable/internal/runnerhub"
	"autable/internal/workflow"
	"autable/internal/workflow/nodes/echo"
)

func startHub(t *testing.T, hub *runnerhub.Hub) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	t.Cleanup(server.Close)
	return server.URL
}

func waitForRunner(t *testing.T, hub *runnerhub.Hub, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := hub.NodeTypes(name); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("runner %q never registered", name)
}

func validator(valid string) runnerhub.TokenValidator {
	return func(_ context.Context, token string) (bool, error) {
		return token == valid, nil
	}
}

func echoDispatch(hub *runnerhub.Hub, value string) (map[string]any, error) {
	return hub.Dispatch(context.Background(), "intranet", workflow.RemoteJob{
		Input: map[string]any{"value": value},
		Runtime: workflow.RuntimeInfo{
			WorkflowID: 7,
			RunID:      "run-1",
			InstanceID: "remote_echo",
			NodeType:   "echo",
		},
	})
}

func TestRunnerExecutesDispatchedJobs(t *testing.T) {
	hub := runnerhub.New(validator("good"), 0)
	url := startHub(t, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, Options{Endpoint: url, Token: "good", Name: "intranet"}, []workflow.Node{echo.Node{}})
	}()
	waitForRunner(t, hub, "intranet")

	output, err := echoDispatch(hub, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	if output["value"] != "Ada" {
		t.Fatalf("unexpected output: %#v", output)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("expected clean shutdown, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not stop on context cancel")
	}
}

func TestRunnerReconnectsAfterDisconnect(t *testing.T) {
	hub := runnerhub.New(validator("good"), 0)
	url := startHub(t, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Run(ctx, Options{Endpoint: url, Token: "good", Name: "intranet"}, []workflow.Node{echo.Node{}})
	waitForRunner(t, hub, "intranet")

	hub.DisconnectAll()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := hub.NodeTypes("intranet"); !ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitForRunner(t, hub, "intranet")

	output, err := echoDispatch(hub, "again")
	if err != nil {
		t.Fatal(err)
	}
	if output["value"] != "again" {
		t.Fatalf("unexpected output after reconnect: %#v", output)
	}
}

func TestRunnerReportsUnknownNodes(t *testing.T) {
	hub := runnerhub.New(validator("good"), 0)
	url := startHub(t, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Run(ctx, Options{Endpoint: url, Token: "good", Name: "intranet"}, []workflow.Node{echo.Node{}})
	waitForRunner(t, hub, "intranet")

	// The hub blocks unsupported node types before dispatching.
	_, err := hub.Dispatch(context.Background(), "intranet", workflow.RemoteJob{
		Runtime: workflow.RuntimeInfo{NodeType: "kingdee.bill.query"},
	})
	if err == nil || !strings.Contains(err.Error(), `does not support node`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunnerRejectsInvalidOptions(t *testing.T) {
	ctx := context.Background()
	cases := []Options{
		{Token: "t", Name: "n"},
		{Endpoint: "http://localhost", Name: "n"},
		{Endpoint: "http://localhost", Token: "t"},
		{Endpoint: "ftp://localhost", Token: "t", Name: "n"},
	}
	for _, options := range cases {
		if err := Run(ctx, options, []workflow.Node{echo.Node{}}); err == nil {
			t.Fatalf("expected options %#v to be rejected", options)
		}
	}
	if err := Run(ctx, Options{Endpoint: "http://localhost", Token: "t", Name: "n"}, nil); err == nil {
		t.Fatal("expected empty node registry to be rejected")
	}
}

func TestWebsocketEndpointNormalization(t *testing.T) {
	cases := map[string]string{
		"https://autable.example.com":             "wss://autable.example.com/api/runner/ws",
		"http://127.0.0.1:8080":                   "ws://127.0.0.1:8080/api/runner/ws",
		"wss://autable.example.com/":              "wss://autable.example.com/api/runner/ws",
		"wss://autable.example.com/api/runner/ws": "wss://autable.example.com/api/runner/ws",
		"https://autable.example.com/base":        "wss://autable.example.com/base/api/runner/ws",
	}
	for input, expected := range cases {
		actual, err := websocketEndpoint(input)
		if err != nil {
			t.Fatal(err)
		}
		if actual != expected {
			t.Fatalf("websocketEndpoint(%q) = %q, expected %q", input, actual, expected)
		}
	}
}
