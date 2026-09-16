package api

import (
	"context"
	"strings"
	"testing"

	"autable/internal/workflow"
)

func TestWorkflowGetUserReadsTheAccountBehindAnID(t *testing.T) {
	server, system := newTestServer(t)
	testSessionCookie(t, system, "Ada")
	service := server.workflowAutableService()

	output, err := service.GetUser(context.Background(), map[string]any{"user_id": " Ada "}, workflow.RuntimeInfo{CreatorID: "owner"})
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	for key, want := range map[string]any{
		"id":            "Ada",
		"email":         "ada@example.com",
		"display_name":  "Ada",
		"provider":      "password",
		"provider_name": "password",
	} {
		if output[key] != want {
			t.Fatalf("output[%q] = %#v, want %#v (full: %#v)", key, output[key], want, output)
		}
	}
	if _, exposed := output["password_hash"]; exposed {
		t.Fatalf("password hash must not reach workflow scripts: %#v", output)
	}

	for name, testCase := range map[string]struct {
		input   map[string]any
		info    workflow.RuntimeInfo
		message string
	}{
		"missing creator": {input: map[string]any{"user_id": "Ada"}, info: workflow.RuntimeInfo{}, message: "workflow creator is required"},
		"missing user id": {input: map[string]any{}, info: workflow.RuntimeInfo{CreatorID: "owner"}, message: "user_id is required"},
		"unknown user":    {input: map[string]any{"user_id": "nobody"}, info: workflow.RuntimeInfo{CreatorID: "owner"}, message: `user "nobody" not found`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.GetUser(context.Background(), testCase.input, testCase.info)
			if err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("error = %v, want it to mention %q", err, testCase.message)
			}
		})
	}
}
