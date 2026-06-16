package workflow

import (
	"context"
	"testing"
	"time"

	"codetable/internal/history"
)

func TestRunnerExecutesJavaScriptAndPersistsWorkflowHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	runner := NewRunner(store, EchoNode{})

	run, key, err := runner.Run(ctx, Definition{
		ID:        7,
		Script:    `function run(info) { const echoed = info.node("echo", { value: info.inputs.name }); return { message: echoed.value + "-" + info.variables.suffix }; }`,
		Variables: map[string]string{"suffix": "done"},
	}, map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if key == "" {
		t.Fatal("expected history key")
	}
	if run.Outputs["message"] != "Ada-done" {
		t.Fatalf("unexpected outputs: %#v", run.Outputs)
	}
	if len(run.Steps) != 1 || run.Steps[0].NodeID != "echo" || run.Steps[0].Output["value"] != "Ada" {
		t.Fatalf("unexpected steps: %#v", run.Steps)
	}

	entries, err := store.GetPrefix(ctx, history.WorkflowPrefix(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one workflow history entry, got %d", len(entries))
	}
	saved, err := history.DecodeWorkflowRun(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if saved.Outputs["message"] != "Ada-done" {
		t.Fatalf("unexpected saved run: %#v", saved)
	}
}

func TestRunnerPersistsFailedRuns(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	runner := NewRunner(store)

	run, _, err := runner.Run(ctx, Definition{
		ID:     9,
		Script: `function run(info) { return info.node("missing", { value: 1 }); }`,
	}, nil)
	if err == nil {
		t.Fatal("expected missing node error")
	}
	if run.Error == "" || len(run.Steps) != 1 || run.Steps[0].Error == "" {
		t.Fatalf("expected failed run details, got %#v", run)
	}
	entries, err := store.GetPrefix(ctx, history.WorkflowPrefix(9))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected failed run to be persisted, got %d", len(entries))
	}
}

func TestRunnerExecutesRecordChangedTriggerNode(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	historyKey, err := history.SaveRowChange(ctx, store, history.RowChange{
		Database:  "db",
		Table:     "contacts",
		RecordID:  5,
		Timestamp: time.Unix(99, 0).UTC(),
		Values:    map[string]any{"name": "Ada"},
		ActorID:   "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(store, NewRecordChangedTriggerNode(store))

	run, _, err := runner.Run(ctx, Definition{
		ID:     10,
		Script: `function run(info) { const changed = info.node("table.record.changed", { history_key: info.inputs.history_key }); return { record_id: changed.record.record_id, name: changed.values.name, actor: changed.actor_id }; }`,
	}, map[string]any{"history_key": historyKey})
	if err != nil {
		t.Fatal(err)
	}
	if run.Outputs["record_id"] != int64(5) || run.Outputs["name"] != "Ada" || run.Outputs["actor"] != "u1" {
		t.Fatalf("unexpected trigger workflow outputs: %#v", run.Outputs)
	}
	if len(run.Steps) != 1 || run.Steps[0].NodeID != "table.record.changed" {
		t.Fatalf("unexpected trigger workflow steps: %#v", run.Steps)
	}
}
