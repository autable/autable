package schedule

import (
	"context"
	"testing"

	"autable/internal/workflow"
)

func TestScheduleTriggerNodeEchoesScheduledAt(t *testing.T) {
	node := Node{}
	info := node.Info()
	if !info.Trigger || !info.Stateless || info.Type != "time.schedule" {
		t.Fatalf("unexpected node info: %#v", info)
	}
	if len(info.Inputs) != 2 || info.Inputs[0].Name != "interval_ms" || info.Outputs[0].Name != "scheduled_at" {
		t.Fatalf("unexpected schedule ports: %#v", info)
	}
	if info.Documentation["en-US"] == "" || info.Documentation["zh-CN"] == "" {
		t.Fatalf("expected schedule docs: %#v", info.Documentation)
	}
	triggerOutput, matched, err := node.RunTrigger(context.Background(), nil, workflow.TriggerEvent{Kind: "schedule", ScheduledAt: 123}, workflow.RuntimeInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if !matched || triggerOutput["scheduled_at"] != int64(123) || triggerOutput["event"] != "schedule" {
		t.Fatalf("unexpected schedule trigger output: matched=%t output=%#v", matched, triggerOutput)
	}
	output, err := node.Run(context.Background(), map[string]any{"scheduled_at": int64(123)}, workflow.RuntimeInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if output["scheduled_at"] != int64(123) {
		t.Fatalf("unexpected schedule output: %#v", output)
	}
}

func TestScheduleTriggerNodeRequiresScheduledAt(t *testing.T) {
	if _, err := (Node{}).Run(context.Background(), map[string]any{}, workflow.RuntimeInfo{}); err == nil {
		t.Fatal("expected scheduled_at error")
	}
}
