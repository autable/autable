package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"autable/internal/history"
)

type testEchoNode struct{}

type creatorCaptureNode struct {
	creatorID string
}

type testRowChangeTriggerNode struct {
	store history.Store
}

type configCaptureNode struct {
	captures map[string]RuntimeInfo
}

func (testEchoNode) Info() NodeInfo {
	return NodeInfo{
		Type:        "echo",
		DisplayName: "Echo",
		Inputs:      []Port{{Name: "value", Type: "any"}},
		Outputs:     []Port{{Name: "value", Type: "any"}},
		Stateless:   true,
	}
}

func (testEchoNode) Run(_ context.Context, input map[string]any, _ RuntimeInfo) (map[string]any, error) {
	return input, nil
}

func (node testRowChangeTriggerNode) Info() NodeInfo {
	return NodeInfo{
		Type:        "table.record.changed",
		DisplayName: "Record changed",
		Inputs:      []Port{{Name: "table", Type: "string"}},
		Outputs:     []Port{{Name: "history_key", Type: "string"}},
		Stateless:   true,
		Trigger:     true,
	}
}

func (node testRowChangeTriggerNode) RunTrigger(_ context.Context, params map[string]any, event TriggerEvent, _ RuntimeInfo) (map[string]any, bool, error) {
	if event.Kind != "row_change" {
		return nil, false, nil
	}
	if tableName, ok := params["table"].(string); ok && tableName != "" && tableName != event.RowChange.Table {
		return nil, false, nil
	}
	record := TriggerRecord{
		HistoryKey: event.HistoryKey,
		Database:   event.RowChange.Database,
		Table:      event.RowChange.Table,
		RecordID:   event.RowChange.RecordID,
		Timestamp:  event.RowChange.Timestamp,
	}
	return map[string]any{
		"history_key": event.HistoryKey,
		"operation":   event.RowChange.Operation,
		"record":      record,
		"values":      event.RowChange.Values,
		"diff":        event.RowChange.Diff,
		"actor_id":    event.RowChange.ActorID,
	}, true, nil
}

func (node testRowChangeTriggerNode) Run(ctx context.Context, input map[string]any, _ RuntimeInfo) (map[string]any, error) {
	historyKey, ok := input["history_key"].(string)
	if !ok || historyKey == "" {
		return nil, errors.New("history_key is required")
	}
	entry, err := node.store.Get(ctx, historyKey)
	if err != nil {
		return nil, fmt.Errorf("load row history: %w", err)
	}
	change, err := history.DecodeRowChange(entry)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"record": TriggerRecord{
			HistoryKey: historyKey,
			Database:   change.Database,
			Table:      change.Table,
			RecordID:   change.RecordID,
			Timestamp:  change.Timestamp,
		},
		"values":   change.Values,
		"diff":     change.Diff,
		"actor_id": change.ActorID,
	}, nil
}

func (node *creatorCaptureNode) Info() NodeInfo {
	return NodeInfo{
		Type:        "creator.capture",
		DisplayName: "Creator capture",
		Inputs:      []Port{},
		Outputs:     []Port{{Name: "creator_id", Type: "string"}},
		Stateless:   true,
	}
}

func (node *creatorCaptureNode) Run(_ context.Context, _ map[string]any, info RuntimeInfo) (map[string]any, error) {
	node.creatorID = info.CreatorID
	return map[string]any{"creator_id": info.CreatorID}, nil
}

func (node *configCaptureNode) Info() NodeInfo {
	return NodeInfo{
		Type:        "config.capture",
		DisplayName: "Config capture",
		Inputs:      []Port{},
		Outputs: []Port{
			{Name: "instance_id", Type: "string"},
			{Name: "channel", Type: "string"},
			{Name: "token", Type: "string"},
		},
		Stateless: true,
	}
}

func (node *configCaptureNode) Run(_ context.Context, _ map[string]any, info RuntimeInfo) (map[string]any, error) {
	if node.captures == nil {
		node.captures = map[string]RuntimeInfo{}
	}
	node.captures[info.InstanceID] = info
	return map[string]any{
		"instance_id": info.InstanceID,
		"channel":     info.Variables["channel"],
		"token":       info.Secrets["token"],
	}, nil
}

func TestRunnerExecutesJavaScriptAndPersistsWorkflowHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	runner := NewRunner(store, testEchoNode{})

	run, key, err := runner.Run(ctx, Definition{
		ID: 7,
		Script: `
function instances(info) {
  return {
    echo_main: {
      node: "echo",
      variables: [{ name: "suffix", type: "string" }]
    }
  };
}
function run(info) {
  const echoed = info.instance("echo_main").exec({ value: info.inputs.name });
  return { message: echoed.value + "-done" };
}`,
		Variables: map[string]string{"echo_main.suffix": "done"},
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
	if len(run.Steps) != 1 || run.Steps[0].NodeID != "echo_main" || run.Steps[0].NodeType != "echo" || run.Steps[0].Output["value"] != "Ada" {
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

func TestRunnerProvidesStableStringify(t *testing.T) {
	store := history.NewMemoryStore()
	runner := NewRunner(store, testEchoNode{})
	run, _, err := runner.Run(context.Background(), Definition{
		ID: 7,
		Script: `
function instances() { return { echoer: "echo" }; }
function run(info) {
  return { value: stableStringify({ b: 2, a: 1 }) };
}`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Outputs["value"] != `{"a":1,"b":2}` {
		t.Fatalf("unexpected stableStringify output: %#v", run.Outputs)
	}
}

func TestRunnerPersistsFailedRuns(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	runner := NewRunner(store)

	run, _, err := runner.Run(ctx, Definition{
		ID:     9,
		Script: `function instances(info) { return { missing_main: "missing" }; } function run(info) { return info.instance("missing_main").exec({ value: 1 }); }`,
	}, nil)
	if err == nil {
		t.Fatal("expected missing node error")
	}
	if run.Error == "" || len(run.Steps) != 0 {
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

func TestRunnerUsesCreatorIdentityOnlyInsideNodes(t *testing.T) {
	ctx := context.Background()
	capture := &creatorCaptureNode{}
	runner := NewRunner(history.NewMemoryStore(), capture)

	run, _, err := runner.Run(ctx, Definition{
		ID:        15,
		CreatorID: "creator-user",
		Script: `
function instances(info) {
  return { capture: "creator.capture" };
}
function run(info) {
  const captured = info.instance("capture").exec({});
  return {
    has_js_creator: Object.prototype.hasOwnProperty.call(info, "creator_id"),
    node_creator: captured.creator_id
  };
}
`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if capture.creatorID != "creator-user" || run.Outputs["node_creator"] != "creator-user" {
		t.Fatalf("expected node to receive creator identity, got node=%q outputs=%#v", capture.creatorID, run.Outputs)
	}
	if run.Outputs["has_js_creator"] != false {
		t.Fatalf("workflow JS should not receive creator identity directly: %#v", run.Outputs)
	}
}

func TestRunnerScopesVariablesAndSecretsToNodeInstances(t *testing.T) {
	ctx := context.Background()
	capture := &configCaptureNode{}
	runner := NewRunner(history.NewMemoryStore(), capture)

	run, _, err := runner.Run(ctx, Definition{
		ID: 16,
		Script: `
function instances(info) {
  return {
    primary_sender: {
      node: "config.capture",
      variables: [{ name: "channel", type: "string" }],
      secrets: [{ name: "token", type: "string" }]
    },
    fallback_sender: {
      node: "config.capture",
      variables: [{ name: "channel", type: "string" }],
      secrets: [{ name: "token", type: "string" }]
    }
  };
}
function run(info) {
  const primary = info.instance("primary_sender").exec({});
  const fallback = info.instance("fallback_sender").exec({});
  return {
    primary_channel: primary.channel,
    primary_token: primary.token,
    fallback_channel: fallback.channel,
    fallback_token: fallback.token
  };
}`,
		Variables: map[string]string{
			"primary_sender.channel":  "ops",
			"fallback_sender.channel": "sales",
		},
		Secrets: map[string]string{
			"primary_sender.token":  "primary-secret",
			"fallback_sender.token": "fallback-secret",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Outputs["primary_channel"] != "ops" || run.Outputs["fallback_channel"] != "sales" {
		t.Fatalf("unexpected scoped variable outputs: %#v", run.Outputs)
	}
	if run.Outputs["primary_token"] != "primary-secret" || run.Outputs["fallback_token"] != "fallback-secret" {
		t.Fatalf("unexpected scoped secret outputs: %#v", run.Outputs)
	}
	if capture.captures["primary_sender"].Variables["channel"] != "ops" || capture.captures["fallback_sender"].Variables["channel"] != "sales" {
		t.Fatalf("unexpected node runtime captures: %#v", capture.captures)
	}
}

func TestRunnerReadsTriggerDeclaration(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	runner := NewRunner(store, testRowChangeTriggerNode{store: store})

	declaration, err := runner.Trigger(ctx, Definition{
		ID: 11,
		Script: `
function instances(info) {
  return { contacts_changed: "table.record.changed" };
}
function trigger(info) {
  return {
    instance: "contacts_changed",
    params: {
      table: "contacts",
      operations: ["update"],
      fields: ["status"]
    }
  };
}
function run(info) { return {}; }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if declaration.Instance != "contacts_changed" || declaration.Node != "table.record.changed" {
		t.Fatalf("unexpected trigger node: %#v", declaration)
	}
	if declaration.Params["table"] != "contacts" {
		t.Fatalf("unexpected trigger params: %#v", declaration.Params)
	}
	operations, ok := declaration.Params["operations"].([]any)
	if !ok || len(operations) != 1 || operations[0] != "update" {
		t.Fatalf("unexpected trigger operations: %#v", declaration.Params["operations"])
	}
	fields, ok := declaration.Params["fields"].([]any)
	if !ok || len(fields) != 1 || fields[0] != "status" {
		t.Fatalf("unexpected trigger fields: %#v", declaration.Params["fields"])
	}
}

func TestRunnerRunsTriggerNodeIntoWorkflowInputs(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	runner := NewRunner(store, testRowChangeTriggerNode{store: store})
	definition := Definition{
		ID: 11,
		Script: `
function instances(info) {
  return { contacts_changed: "table.record.changed" };
}
function trigger(info) {
  return {
    instance: "contacts_changed",
    params: {
      table: "contacts",
      operations: ["create"],
      fields: ["name"]
    }
  };
}
function run(info) { return info.inputs; }
`,
	}
	declaration, err := runner.Trigger(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	inputs, matched, err := runner.TriggerRunInputs(ctx, definition, declaration, TriggerEvent{
		Kind:       "row_change",
		HistoryKey: "rhistory_db_contacts_00000000000000000005_00000000000000000100",
		RowChange: history.RowChange{
			Database:  "db",
			Table:     "contacts",
			RecordID:  5,
			Operation: "create",
			Values:    map[string]any{"name": "Ada"},
			Diff:      history.RowDiff{"name": {Old: nil, New: "Ada"}},
			ActorID:   "u1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !matched || inputs["history_key"] == "" || inputs["operation"] != "create" {
		t.Fatalf("unexpected trigger inputs: matched=%t inputs=%#v", matched, inputs)
	}
	record, ok := inputs["record"].(TriggerRecord)
	if !ok || record.RecordID != 5 || record.Table != "contacts" {
		t.Fatalf("unexpected trigger record: %#v", inputs["record"])
	}
	run, _, err := runner.Run(ctx, definition, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Steps) != 0 || run.Outputs["operation"] != "create" {
		t.Fatalf("trigger node should feed run inputs without a run step: %#v", run)
	}
}

func TestRunnerRejectsNonTriggerDeclarationNode(t *testing.T) {
	ctx := context.Background()
	runner := NewRunner(history.NewMemoryStore(), testEchoNode{})

	_, err := runner.Trigger(ctx, Definition{
		ID:     12,
		Script: `function instances(info) { return { echo_trigger: "echo" }; } function trigger(info) { return { instance: "echo_trigger" }; } function run(info) { return {}; }`,
	})
	if err == nil {
		t.Fatal("expected non-trigger node error")
	}
}

func TestRunnerRequiresTriggerFunction(t *testing.T) {
	ctx := context.Background()
	runner := NewRunner(history.NewMemoryStore(), testEchoNode{})

	_, err := runner.Trigger(ctx, Definition{
		ID:     13,
		Script: `function instances(info) { return { echo_main: "echo" }; } function run(info) { return {}; }`,
	})
	if err == nil {
		t.Fatal("expected missing trigger function error")
	}
}

func TestRunnerDoesNotNormalizeExportDefaultScripts(t *testing.T) {
	ctx := context.Background()
	runner := NewRunner(history.NewMemoryStore(), testEchoNode{})

	_, _, err := runner.Run(ctx, Definition{
		ID:     14,
		Script: `export default function run(info) { return {}; }`,
	}, nil)
	if err == nil {
		t.Fatal("expected export default script to fail")
	}
}

func TestRunnerRejectsTriggerNodeExecInRun(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	historyKey, err := history.SaveRowChange(ctx, store, history.RowChange{
		Database:  "db",
		Table:     "contacts",
		RecordID:  5,
		Timestamp: time.Unix(99, 0).UTC().UnixMilli(),
		Values:    map[string]any{"name": "Ada"},
		Diff:      history.RowDiff{"name": {Old: nil, New: "Ada"}},
		ActorID:   "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(store, testRowChangeTriggerNode{store: store})

	run, _, err := runner.Run(ctx, Definition{
		ID:     10,
		Script: `function instances(info) { return { changed: "table.record.changed" }; } function run(info) { const changed = info.instance("changed").exec({ history_key: info.inputs.history_key }); return { record_id: changed.record.record_id, name: changed.values.name, actor: changed.actor_id, diff_name: changed.diff.name.new }; }`,
	}, map[string]any{"history_key": historyKey})
	if err == nil {
		t.Fatal("expected trigger node exec to fail")
	}
	if !strings.Contains(err.Error(), `trigger node "table.record.changed" cannot be executed from run`) {
		t.Fatalf("unexpected trigger exec error: %v", err)
	}
	if len(run.Steps) != 1 || run.Steps[0].NodeID != "changed" || run.Steps[0].Error == "" {
		t.Fatalf("unexpected trigger workflow steps: %#v", run.Steps)
	}
}
