package runnerhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"autable/internal/egress"
)

func testResolver(valid string) TokenResolver {
	return func(_ context.Context, token string) (string, bool, error) {
		if token == valid {
			return "db", true, nil
		}
		return "", false, nil
	}
}

func startHub(t *testing.T, hub *Hub) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func dialRunner(t *testing.T, url, token string, hello HelloMessage) *websocket.Conn {
	t.Helper()
	header := http.Header{"Authorization": {"Bearer " + token}}
	ws, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if err := ws.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	return ws
}

func waitForRunner(t *testing.T, hub *Hub, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, runner := range hub.Runners("db") {
			if runner.Name == name {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("runner %q never registered", name)
}

func testRequest() egress.Request {
	return egress.Request{Method: "POST", URL: "http://intranet.test/api", Headers: [][2]string{{"X-Token", "t"}}, Body: []byte("payload")}
}

func TestHubRejectsBadTokens(t *testing.T) {
	hub := New(testResolver("good"))
	url := startHub(t, hub)

	header := http.Header{"Authorization": {"Bearer wrong"}}
	_, response, err := websocket.DefaultDialer.Dial(url, header)
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got err=%v response=%v", err, response)
	}
	if _, response, err = websocket.DefaultDialer.Dial(url, nil); err == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without token, got err=%v response=%v", err, response)
	}
}

func TestHubSendsFetchJobsAndReturnsResponses(t *testing.T) {
	hub := New(testResolver("good"))
	url := startHub(t, hub)
	ws := dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet", Version: "v1"})
	waitForRunner(t, hub, "intranet")

	go func() {
		var job JobMessage
		if err := ws.ReadJSON(&job); err != nil || job.Kind != "fetch" {
			return
		}
		ws.WriteJSON(ResultMessage{Kind: "result", JobID: job.JobID, Response: &egress.Response{
			Status:  200,
			URL:     job.Request.URL,
			Headers: [][2]string{{"x-seen", job.Request.Headers[0][1]}},
			Body:    append([]byte("echo:"), job.Request.Body...),
		}})
	}()

	response, err := hub.Fetch(context.Background(), "db", "intranet", testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != 200 || string(response.Body) != "echo:payload" || response.Headers[0][1] != "t" {
		t.Fatalf("unexpected response: %#v", response)
	}
	statuses := hub.Runners("db")
	if len(statuses) != 1 || statuses[0].Name != "intranet" || statuses[0].Version != "v1" {
		t.Fatalf("unexpected runner statuses: %#v", statuses)
	}
	if others := hub.Runners("other"); len(others) != 0 {
		t.Fatalf("expected runner to be scoped to its database, got %#v", others)
	}
	if _, err := hub.Fetch(context.Background(), "other", "intranet", testRequest()); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected runners to be scoped to their database, got %v", err)
	}
}

func TestHubReturnsRunnerErrors(t *testing.T) {
	hub := New(testResolver("good"))
	url := startHub(t, hub)
	ws := dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet"})
	waitForRunner(t, hub, "intranet")

	go func() {
		var job JobMessage
		if err := ws.ReadJSON(&job); err != nil {
			return
		}
		ws.WriteJSON(ResultMessage{Kind: "result", JobID: job.JobID, Error: "dial tcp: connection refused"})
	}()

	_, err := hub.Fetch(context.Background(), "db", "intranet", testRequest())
	if err == nil || !strings.Contains(err.Error(), `runner "intranet": dial tcp: connection refused`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHubFailsUnknownRunners(t *testing.T) {
	hub := New(testResolver("good"))
	startHub(t, hub)
	if _, err := hub.Fetch(context.Background(), "db", "intranet", testRequest()); err == nil || !strings.Contains(err.Error(), `runner "intranet" is not connected`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHubFailsInFlightJobsOnDisconnect(t *testing.T) {
	hub := New(testResolver("good"))
	url := startHub(t, hub)
	ws := dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet"})
	waitForRunner(t, hub, "intranet")

	go func() {
		var job JobMessage
		if err := ws.ReadJSON(&job); err != nil {
			return
		}
		ws.Close()
	}()

	_, err := hub.Fetch(context.Background(), "db", "intranet", testRequest())
	if err == nil || !strings.Contains(err.Error(), "disconnected while the request was running") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHubTimesOutSilentRunners(t *testing.T) {
	hub := New(testResolver("good"))
	hub.slack = 0
	url := startHub(t, hub)
	dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet"})
	waitForRunner(t, hub, "intranet")

	request := testRequest()
	request.TimeoutMs = 50
	_, err := hub.Fetch(context.Background(), "db", "intranet", request)
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHubDisconnectDatabaseDropsRunners(t *testing.T) {
	hub := New(testResolver("good"))
	url := startHub(t, hub)
	ws := dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet"})
	waitForRunner(t, hub, "intranet")

	hub.DisconnectDatabase("db")

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var message ResultMessage
	if err := ws.ReadJSON(&message); err == nil {
		t.Fatal("expected connection to be closed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(hub.Runners("db")) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected no runners, got %#v", hub.Runners("db"))
}
