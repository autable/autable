package upsert

import (
	"context"
	"testing"

	"autable/internal/workflow"
)

type fakeAutableService struct {
	operation string
	input     map[string]any
	info      workflow.RuntimeInfo
}

func (service *fakeAutableService) CreateRow(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.capture("create", input, info)
}

func (service *fakeAutableService) UpdateRow(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.capture("update", input, info)
}

func (service *fakeAutableService) UpsertRow(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.capture("upsert", input, info)
}

func (service *fakeAutableService) DeleteRow(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.capture("delete", input, info)
}

func (service *fakeAutableService) ListRows(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.capture("list", input, info)
}

func (service *fakeAutableService) CreateFields(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.capture("fields", input, info)
}

func (service *fakeAutableService) capture(operation string, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	service.operation = operation
	service.input = input
	service.info = info
	return map[string]any{"operation": "update"}, nil
}

func TestNodeCallsService(t *testing.T) {
	service := &fakeAutableService{}
	node := NewNode(service)
	output, err := node.Run(context.Background(), map[string]any{
		"table":       "contacts",
		"match_field": "external_id",
		"values":      map[string]any{"external_id": "remote-1"},
	}, workflow.RuntimeInfo{DatabaseName: "db", CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if service.operation != "upsert" || service.input["match_field"] != "external_id" || service.info.CreatorID != "owner" {
		t.Fatalf("unexpected service capture: operation=%q input=%#v info=%#v", service.operation, service.input, service.info)
	}
	if output["operation"] != "update" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestNodeInfo(t *testing.T) {
	info := NewNode(&fakeAutableService{}).Info()
	if info.Type != "table.row.upsert" || len(info.Inputs) != 4 || info.Inputs[2].Name != "match_field" {
		t.Fatalf("unexpected upsert node info: %#v", info)
	}
	if len(info.Outputs) != 2 || info.Outputs[1].Name != "operation" {
		t.Fatalf("unexpected upsert outputs: %#v", info.Outputs)
	}
	if info.Documentation["en-US"] == "" || info.Documentation["zh-CN"] == "" {
		t.Fatalf("expected embedded documentation, got %#v", info.Documentation)
	}
}
