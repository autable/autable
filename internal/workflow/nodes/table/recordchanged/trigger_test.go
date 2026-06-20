package recordchanged

import (
	"context"
	"testing"
	"time"

	"autable/internal/history"
	"autable/internal/workflow"
)

func TestRecordChangedTriggerNodeLoadsRowHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	ts := time.Unix(42, 0).UTC()
	key, err := history.SaveRowChange(ctx, store, history.RowChange{
		Database:  "db",
		Table:     "contacts",
		RecordID:  12,
		Timestamp: ts.UnixMilli(),
		Values:    map[string]any{"name": "Ada"},
		Diff:      history.RowDiff{"name": {Old: nil, New: "Ada"}},
		ActorID:   "u1",
	})
	if err != nil {
		t.Fatal(err)
	}

	node := NewNode(store)
	info := node.Info()
	if !info.Trigger || !info.Stateless || info.Type != "table.record.changed" {
		t.Fatalf("unexpected node info: %#v", info)
	}
	if len(info.Inputs) != 3 || info.Inputs[0].Name != "table" || info.Outputs[0].Name != "history_key" {
		t.Fatalf("unexpected trigger node ports: %#v", info)
	}
	if info.Documentation["en-US"] == "" || info.Documentation["zh-CN"] == "" {
		t.Fatalf("expected trigger docs: %#v", info.Documentation)
	}

	output, err := node.Run(ctx, map[string]any{"history_key": key}, workflow.RuntimeInfo{})
	if err != nil {
		t.Fatal(err)
	}
	record, ok := output["record"].(workflow.TriggerRecord)
	if !ok {
		t.Fatalf("expected trigger record output, got %#v", output["record"])
	}
	if record.HistoryKey != key || record.Database != "db" || record.Table != "contacts" || record.RecordID != 12 || record.Timestamp != ts.UnixMilli() {
		t.Fatalf("unexpected trigger record: %#v", record)
	}
	values, ok := output["values"].(map[string]any)
	if !ok || values["name"] != "Ada" || output["actor_id"] != "u1" {
		t.Fatalf("unexpected trigger output: %#v", output)
	}
	diff, ok := output["diff"].(history.RowDiff)
	if !ok || diff["name"].New != "Ada" {
		t.Fatalf("unexpected trigger diff: %#v", output["diff"])
	}
}

func TestRecordChangedTriggerNodeRunsEventWithParams(t *testing.T) {
	node := NewNode(history.NewMemoryStore())
	output, matched, err := node.RunTrigger(context.Background(), map[string]any{
		"table":      "contacts",
		"operations": []any{"update"},
		"fields":     []any{"status"},
	}, workflow.TriggerEvent{
		Kind:       "row_change",
		HistoryKey: "history-key",
		RowChange: history.RowChange{
			Database:  "db",
			Table:     "contacts",
			RecordID:  12,
			Operation: "update",
			Values:    map[string]any{"status": "Done"},
			Diff:      history.RowDiff{"status": {Old: "Todo", New: "Done"}},
			ActorID:   "u1",
		},
	}, workflow.RuntimeInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if !matched || output["history_key"] != "history-key" || output["operation"] != "update" || output["table"] != "contacts" {
		t.Fatalf("unexpected trigger event output: matched=%t output=%#v", matched, output)
	}
}

func TestRecordChangedTriggerNodeSkipsUnmatchedEvent(t *testing.T) {
	node := NewNode(history.NewMemoryStore())
	output, matched, err := node.RunTrigger(context.Background(), map[string]any{
		"table": "contacts",
	}, workflow.TriggerEvent{
		Kind: "row_change",
		RowChange: history.RowChange{
			Table:     "projects",
			Operation: "update",
		},
	}, workflow.RuntimeInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if matched || output != nil {
		t.Fatalf("expected unmatched event, got matched=%t output=%#v", matched, output)
	}
}

func TestRecordChangedTriggerNodeRequiresHistoryKey(t *testing.T) {
	node := NewNode(history.NewMemoryStore())
	if _, err := node.Run(context.Background(), map[string]any{}, workflow.RuntimeInfo{}); err == nil {
		t.Fatal("expected missing history_key error")
	}
}
