package get

import (
	"context"
	"strings"
	"testing"

	"autable/internal/workflow"
)

type fakeService struct {
	input map[string]any
	info  workflow.RuntimeInfo
}

func (service *fakeService) GetUser(_ context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	service.input = input
	service.info = info
	return map[string]any{"id": input["user_id"], "email": "ada@example.com"}, nil
}

func (service *fakeService) CreateRow(context.Context, map[string]any, workflow.RuntimeInfo) (map[string]any, error) {
	return nil, nil
}

func (service *fakeService) UpdateRow(context.Context, map[string]any, workflow.RuntimeInfo) (map[string]any, error) {
	return nil, nil
}

func (service *fakeService) UpsertRow(context.Context, map[string]any, workflow.RuntimeInfo) (map[string]any, error) {
	return nil, nil
}

func (service *fakeService) DeleteRow(context.Context, map[string]any, workflow.RuntimeInfo) (map[string]any, error) {
	return nil, nil
}

func (service *fakeService) ListRows(context.Context, map[string]any, workflow.RuntimeInfo) (map[string]any, error) {
	return nil, nil
}

func (service *fakeService) CreateFields(context.Context, map[string]any, workflow.RuntimeInfo) (map[string]any, error) {
	return nil, nil
}

func TestNodeDelegatesToTheService(t *testing.T) {
	service := &fakeService{}
	output, err := NewNode(service).Run(context.Background(), map[string]any{"user_id": "u-1"}, workflow.RuntimeInfo{CreatorID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if service.input["user_id"] != "u-1" || service.info.CreatorID != "owner" {
		t.Fatalf("service capture: input=%#v info=%#v", service.input, service.info)
	}
	if output["email"] != "ada@example.com" {
		t.Fatalf("output = %#v", output)
	}
}

func TestNodeInfoDocumentsBothLanguages(t *testing.T) {
	info := NewNode(&fakeService{}).Info()
	if info.Type != "user.get" || len(info.Inputs) != 1 || info.Inputs[0].Name != "user_id" {
		t.Fatalf("info = %#v", info)
	}
	for _, language := range []string{"en-US", "zh-CN"} {
		if strings.TrimSpace(info.Documentation[language]) == "" {
			t.Fatalf("documentation for %s is empty", language)
		}
	}
}
