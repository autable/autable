package field

import (
	"context"
	"testing"

	"autable/internal/workflow"
)

type fakeTableFieldRunner struct {
	input map[string]any
	info  workflow.RuntimeInfo
}

func (runner *fakeTableFieldRunner) CreateFields(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	runner.input = input
	runner.info = info
	return map[string]any{"created": []map[string]any{{"name": "email", "type": "string"}}}, nil
}

func (runner *fakeTableFieldRunner) CreateRow(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return runner.CreateFields(context.Background(), input, info)
}

func (runner *fakeTableFieldRunner) UpdateRow(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return runner.CreateFields(context.Background(), input, info)
}

func (runner *fakeTableFieldRunner) UpsertRow(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return runner.CreateFields(context.Background(), input, info)
}

func (runner *fakeTableFieldRunner) DeleteRow(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return runner.CreateFields(context.Background(), input, info)
}

func (runner *fakeTableFieldRunner) ListRows(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return runner.CreateFields(context.Background(), input, info)
}

func TestTableFieldNodeCallsRunner(t *testing.T) {
	runner := &fakeTableFieldRunner{}
	node := NewCreateNode(runner)
	output, err := node.Run(context.Background(), map[string]any{
		"table":  "contacts",
		"fields": []any{"email"},
	}, workflow.RuntimeInfo{DatabaseName: "db", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if runner.input["table"] != "contacts" || runner.info.DatabaseName != "db" || runner.info.CreatorID != "owner" {
		t.Fatalf("unexpected runner capture: input=%#v info=%#v", runner.input, runner.info)
	}
	if created, ok := output["created"].([]map[string]any); !ok || len(created) != 1 || created[0]["name"] != "email" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestTableFieldNodeInfo(t *testing.T) {
	info := NewCreateNode(&fakeTableFieldRunner{}).Info()
	if info.Type != "table.field.create" || len(info.Inputs) != 3 || info.Inputs[2].Name != "fields" {
		t.Fatalf("unexpected field node info: %#v", info)
	}
	if info.Documentation["en-US"] == "" || info.Documentation["zh-CN"] == "" {
		t.Fatalf("expected embedded documentation, got %#v", info.Documentation)
	}
}
