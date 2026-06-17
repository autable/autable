package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codetable/internal/workflow"
)

func TestDingTalkRobotNodeSendsWebhookTextMessage(t *testing.T) {
	ctx := context.Background()
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected json content type, got %q", got)
		}
		query := r.URL.Query()
		if query.Get("access_token") != "robot-token" {
			t.Fatalf("unexpected access token: %q", query.Get("access_token"))
		}
		if query.Has("timestamp") || query.Has("sign") {
			t.Fatalf("unexpected signed query values: %s", r.URL.RawQuery)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	node := NewDingTalkRobotNodeForTest(server.Client(), server.URL)
	output, err := node.Run(ctx, map[string]any{
		"content":     "Codetable alert",
		"at_user_ids": []any{"user-a", "user-b"},
		"at_all":      true,
	}, workflow.RuntimeInfo{
		Secrets: map[string]string{
			"access_token": "robot-token",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output["status_code"] != http.StatusOK || output["errcode"] != float64(0) || output["errmsg"] != "ok" {
		t.Fatalf("unexpected output: %#v", output)
	}
	if outputContainsSecret(output, "robot-token") {
		t.Fatalf("node output leaked secret values: %#v", output)
	}
	text, ok := requestBody["text"].(map[string]any)
	if !ok || text["content"] != "Codetable alert" {
		t.Fatalf("unexpected text body: %#v", requestBody)
	}
	at, ok := requestBody["at"].(map[string]any)
	if !ok || at["isAtAll"] != true {
		t.Fatalf("unexpected at body: %#v", requestBody)
	}
	atUserIDs, ok := at["atUserIds"].([]any)
	if !ok || len(atUserIDs) != 2 || atUserIDs[0] != "user-a" || atUserIDs[1] != "user-b" {
		t.Fatalf("unexpected at user ids: %#v", at["atUserIds"])
	}
}

func TestDingTalkRobotNodeRequiresSecretsAndContent(t *testing.T) {
	node := NewDingTalkRobotNodeForTest(nil, "http://127.0.0.1/robot/send")
	if _, err := node.Run(context.Background(), map[string]any{"content": "hello"}, workflow.RuntimeInfo{}); err == nil {
		t.Fatal("expected missing access_token error")
	}
	if _, err := node.Run(context.Background(), map[string]any{}, workflow.RuntimeInfo{Secrets: map[string]string{"access_token": "token"}}); err == nil {
		t.Fatal("expected missing content error")
	}
}

func TestDingTalkRobotNodeIsAvailableInNodeInfos(t *testing.T) {
	runner := workflow.NewRunner(nil, NewDingTalkRobotNode())
	infos := runner.NodeInfos()
	if len(infos) != 1 || infos[0].Type != "dingtalk.robot.send" {
		t.Fatalf("expected dingtalk node info, got %#v", infos)
	}
	if len(infos[0].Secrets) != 1 || infos[0].Secrets[0].Name != "access_token" {
		t.Fatalf("expected dingtalk secret metadata, got %#v", infos[0].Secrets)
	}
	if infos[0].Documentation["en-US"] == "" || infos[0].Documentation["zh-CN"] == "" {
		t.Fatalf("expected embedded documentation, got %#v", infos[0].Documentation)
	}
}

func outputContainsSecret(value any, secret string) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return strings.Contains(string(encoded), secret)
}
