package trigger

import (
	"context"
	"strings"
	"testing"

	"autable/internal/workflow"
)

func testInfo(token string) workflow.RuntimeInfo {
	return workflow.RuntimeInfo{Secrets: map[string]string{"token": token}}
}

func TestWebhookTriggerMatchesValidToken(t *testing.T) {
	output, matched, err := (Node{}).RunTrigger(context.Background(), nil, workflow.TriggerEvent{
		Kind:    "webhook",
		Webhook: workflow.WebhookEvent{Token: "s3cret", Payload: map[string]any{"result": "agree"}, ReceivedAt: 42},
	}, testInfo("s3cret"))
	if err != nil || !matched {
		t.Fatalf("expected match, got matched=%v err=%v", matched, err)
	}
	payload := output["payload"].(map[string]any)
	if payload["result"] != "agree" || output["received_at"] != int64(42) || output["event"] != "webhook" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestWebhookTriggerRejects(t *testing.T) {
	if _, matched, err := (Node{}).RunTrigger(context.Background(), nil, workflow.TriggerEvent{Kind: "schedule"}, testInfo("s3cret")); matched || err != nil {
		t.Fatalf("expected other event kinds to be ignored, got matched=%v err=%v", matched, err)
	}
	if _, matched, err := (Node{}).RunTrigger(context.Background(), nil, workflow.TriggerEvent{
		Kind:    "webhook",
		Webhook: workflow.WebhookEvent{Token: "wrong"},
	}, testInfo("s3cret")); matched || err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected token mismatch error, got matched=%v err=%v", matched, err)
	}
	if _, matched, err := (Node{}).RunTrigger(context.Background(), nil, workflow.TriggerEvent{
		Kind:    "webhook",
		Webhook: workflow.WebhookEvent{Token: "anything"},
	}, testInfo("")); matched || err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected unconfigured token error, got matched=%v err=%v", matched, err)
	}
	if output, matched, err := (Node{}).RunTrigger(context.Background(), nil, workflow.TriggerEvent{
		Kind:    "webhook",
		Webhook: workflow.WebhookEvent{Token: "s3cret"},
	}, testInfo("s3cret")); !matched || err != nil || output["payload"] == nil {
		t.Fatalf("expected empty payload to default to an object, got %#v err=%v", output, err)
	}
}
