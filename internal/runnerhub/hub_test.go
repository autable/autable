package runnerhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"autable/internal/workflow"
)

func testValidator(valid string) TokenValidator {
	return func(_ context.Context, token string) (bool, error) {
		return token == valid, nil
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
		if _, ok := hub.NodeTypes(name); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("runner %q never registered", name)
}

func echoJob(runnerName string) workflow.RemoteJob {
	return workflow.RemoteJob{
		Input: map[string]any{"value": "Ada"},
		Runtime: workflow.RuntimeInfo{
			WorkflowID: 7,
			RunID:      "run-1",
			InstanceID: "remote_echo",
			NodeType:   "echo",
			Secrets:    map[string]string{"app_secret": "s3cret"},
		},
	}
}

func TestHubRejectsBadTokens(t *testing.T) {
	hub := New(testValidator("good"), 0)
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

func TestHubDispatchesJobsAndReturnsResults(t *testing.T) {
	hub := New(testValidator("good"), 0)
	url := startHub(t, hub)
	ws := dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet", Version: "v1", NodeTypes: []string{"echo"}})
	waitForRunner(t, hub, "intranet")

	go func() {
		var job JobMessage
		if err := ws.ReadJSON(&job); err != nil {
			return
		}
		ws.WriteJSON(ResultMessage{Kind: "result", JobID: job.JobID, Output: map[string]any{
			"echoed":      job.Input["value"],
			"secret_seen": job.Secrets["app_secret"],
			"instance":    job.Runtime.InstanceID,
		}})
	}()

	output, err := hub.Dispatch(context.Background(), "intranet", echoJob("intranet"))
	if err != nil {
		t.Fatal(err)
	}
	if output["echoed"] != "Ada" || output["secret_seen"] != "s3cret" || output["instance"] != "remote_echo" {
		t.Fatalf("unexpected output: %#v", output)
	}

	statuses := hub.Runners()
	if len(statuses) != 1 || statuses[0].Name != "intranet" || statuses[0].Version != "v1" {
		t.Fatalf("unexpected runner statuses: %#v", statuses)
	}
}

func TestHubReturnsRunnerErrors(t *testing.T) {
	hub := New(testValidator("good"), 0)
	url := startHub(t, hub)
	ws := dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet", NodeTypes: []string{"echo"}})
	waitForRunner(t, hub, "intranet")

	go func() {
		var job JobMessage
		if err := ws.ReadJSON(&job); err != nil {
			return
		}
		ws.WriteJSON(ResultMessage{Kind: "result", JobID: job.JobID, Error: "kingdee gateway exploded"})
	}()

	_, err := hub.Dispatch(context.Background(), "intranet", echoJob("intranet"))
	if err == nil || !strings.Contains(err.Error(), "kingdee gateway exploded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHubFailsUnknownRunnersAndNodes(t *testing.T) {
	hub := New(testValidator("good"), 0)
	url := startHub(t, hub)

	if _, err := hub.Dispatch(context.Background(), "intranet", echoJob("intranet")); err == nil || !strings.Contains(err.Error(), `runner "intranet" is not connected`) {
		t.Fatalf("unexpected error: %v", err)
	}

	dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet", NodeTypes: []string{"other"}})
	waitForRunner(t, hub, "intranet")
	if _, err := hub.Dispatch(context.Background(), "intranet", echoJob("intranet")); err == nil || !strings.Contains(err.Error(), `does not support node "echo"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHubFailsInFlightJobsOnDisconnect(t *testing.T) {
	hub := New(testValidator("good"), 0)
	url := startHub(t, hub)
	ws := dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet", NodeTypes: []string{"echo"}})
	waitForRunner(t, hub, "intranet")

	go func() {
		var job JobMessage
		if err := ws.ReadJSON(&job); err != nil {
			return
		}
		ws.Close()
	}()

	_, err := hub.Dispatch(context.Background(), "intranet", echoJob("intranet"))
	if err == nil || !strings.Contains(err.Error(), "disconnected while the job was running") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHubTimesOutSilentJobs(t *testing.T) {
	hub := New(testValidator("good"), 50*time.Millisecond)
	url := startHub(t, hub)
	dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet", NodeTypes: []string{"echo"}})
	waitForRunner(t, hub, "intranet")

	_, err := hub.Dispatch(context.Background(), "intranet", echoJob("intranet"))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHubDisconnectAllDropsRunners(t *testing.T) {
	hub := New(testValidator("good"), 0)
	url := startHub(t, hub)
	ws := dialRunner(t, url, "good", HelloMessage{Kind: "hello", Name: "intranet", NodeTypes: []string{"echo"}})
	waitForRunner(t, hub, "intranet")

	hub.DisconnectAll()

	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var message ResultMessage
	if err := ws.ReadJSON(&message); err == nil {
		t.Fatal("expected connection to be closed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(hub.Runners()) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected no runners, got %#v", hub.Runners())
}
