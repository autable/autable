package workflow

import (
	"context"
	"testing"
	"time"

	"codetable/internal/history"
)

func TestRecordChangedTriggerNodeLoadsRowHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	ts := time.Unix(42, 0).UTC()
	key, err := history.SaveRowChange(ctx, store, history.RowChange{
		Database:  "db",
		Table:     "contacts",
		RecordID:  12,
		Timestamp: ts,
		Values:    map[string]any{"name": "Ada"},
		ActorID:   "u1",
	})
	if err != nil {
		t.Fatal(err)
	}

	node := NewRecordChangedTriggerNode(store)
	info := node.Info()
	if !info.Trigger || !info.Stateless || info.Type != "table.record.changed" {
		t.Fatalf("unexpected node info: %#v", info)
	}

	output, err := node.Run(ctx, map[string]any{"history_key": key}, RuntimeInfo{})
	if err != nil {
		t.Fatal(err)
	}
	record, ok := output["record"].(TriggerRecord)
	if !ok {
		t.Fatalf("expected trigger record output, got %#v", output["record"])
	}
	if record.HistoryKey != key || record.Database != "db" || record.Table != "contacts" || record.RecordID != 12 || record.Timestamp != ts.UnixNano() {
		t.Fatalf("unexpected trigger record: %#v", record)
	}
	values, ok := output["values"].(map[string]any)
	if !ok || values["name"] != "Ada" || output["actor_id"] != "u1" {
		t.Fatalf("unexpected trigger output: %#v", output)
	}
}

func TestRecordChangedTriggerNodeRequiresHistoryKey(t *testing.T) {
	node := NewRecordChangedTriggerNode(history.NewMemoryStore())
	if _, err := node.Run(context.Background(), map[string]any{}, RuntimeInfo{}); err == nil {
		t.Fatal("expected missing history_key error")
	}
}
