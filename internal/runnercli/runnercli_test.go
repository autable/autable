package runnercli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"autable/internal/egress"
	"autable/internal/runnerhub"
)

func startHub(t *testing.T, hub *runnerhub.Hub) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	t.Cleanup(server.Close)
	return server.URL
}

func waitForRunner(t *testing.T, hub *runnerhub.Hub, name string, present bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, runner := range hub.Runners("db") {
			found = found || runner.Name == name
		}
		if found == present {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("runner %q presence never became %v", name, present)
}

func validator(valid string) runnerhub.TokenResolver {
	return func(_ context.Context, token string) (string, bool, error) {
		if token == valid {
			return "db", true, nil
		}
		return "", false, nil
	}
}

func TestRunnerPerformsFetchJobs(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen", r.Header.Get("X-Token"))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello " + r.Method))
	}))
	defer target.Close()
	hub := runnerhub.New(validator("good"))
	url := startHub(t, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Endpoint: url, Token: "good", Name: "intranet", MaxJobs: 2})
	}()
	waitForRunner(t, hub, "intranet", true)

	response, err := hub.Fetch(context.Background(), "db", "intranet", egress.Request{Method: "PUT", URL: target.URL + "/x", Headers: [][2]string{{"X-Token", "t"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusCreated || string(response.Body) != "hello PUT" {
		t.Fatalf("unexpected response %#v", response)
	}
	seen := false
	for _, header := range response.Headers {
		seen = seen || (header[0] == "x-seen" && header[1] == "t")
	}
	if !seen {
		t.Fatalf("expected request headers to reach the target, got %#v", response.Headers)
	}
	if _, err := hub.Fetch(context.Background(), "db", "intranet", egress.Request{URL: "http://127.0.0.1:1/unreachable", TimeoutMs: 2000}); err == nil || !strings.Contains(err.Error(), `runner "intranet"`) {
		t.Fatalf("expected the runner's transport error, got %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("expected a clean shutdown, got %v", err)
	}
}

func TestRunnerReconnectsAfterDisconnect(t *testing.T) {
	hub := runnerhub.New(validator("good"))
	url := startHub(t, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, Options{Endpoint: url, Token: "good", Name: "intranet"}) }()
	waitForRunner(t, hub, "intranet", true)
	hub.DisconnectDatabase("db")
	waitForRunner(t, hub, "intranet", false)
	waitForRunner(t, hub, "intranet", true)
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
		if err := Run(ctx, options); err == nil {
			t.Fatalf("expected options %#v to be rejected", options)
		}
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

func TestLogURLDropsCredentials(t *testing.T) {
	if got := logURL("https://user:pw@example.com/a?token=s#f"); got != "https://example.com/a" {
		t.Fatalf("unexpected log url %q", got)
	}
}
