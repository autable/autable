package trigger

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	"autable/internal/workflow"
)

type Node struct{}

func (Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "webhook.trigger",
		DisplayName:   "Webhook",
		Description:   "Triggers a workflow from an authenticated POST to the workflow's webhook URL.",
		Documentation: Documentation(),
		Outputs: []workflow.Port{
			{Name: "payload", Type: "object", Description: "The payload object from the webhook request body."},
			{Name: "received_at", Type: "int64", Description: "Millisecond timestamp the webhook was received."},
			{Name: "event", Type: "string"},
		},
		Secrets: []workflow.Port{
			{Name: "token", Type: "string", Description: "Shared secret the caller must send in the request body; the webhook is disabled until it is configured."},
		},
		Stateless: true,
		Trigger:   true,
	}
}

func (Node) RunTrigger(_ context.Context, _ map[string]any, event workflow.TriggerEvent, info workflow.RuntimeInfo) (map[string]any, bool, error) {
	if event.Kind != "webhook" {
		return nil, false, nil
	}
	configured := strings.TrimSpace(info.Secrets["token"])
	if configured == "" {
		return nil, false, errors.New("webhook token secret is not configured")
	}
	if subtle.ConstantTimeCompare([]byte(configured), []byte(event.Webhook.Token)) != 1 {
		return nil, false, errors.New("webhook token does not match")
	}
	payload := event.Webhook.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	return map[string]any{
		"payload":     payload,
		"received_at": event.Webhook.ReceivedAt,
		"event":       "webhook",
	}, true, nil
}

func (Node) Run(_ context.Context, _ map[string]any, _ workflow.RuntimeInfo) (map[string]any, error) {
	return nil, errors.New("webhook.trigger runs from inbound webhooks; declare it in trigger(info)")
}

var _ workflow.TriggerNode = Node{}
