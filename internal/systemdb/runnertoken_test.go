package systemdb

import (
	"context"
	"strings"
	"testing"
)

func TestRunnerTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, exists, err := db.RunnerTokenCreatedAt(ctx); err != nil || exists {
		t.Fatalf("expected no token before reset, exists=%v err=%v", exists, err)
	}
	if valid, err := db.ValidateRunnerToken(ctx, "atr_anything"); err != nil || valid {
		t.Fatalf("expected validation to fail before reset, valid=%v err=%v", valid, err)
	}

	token, err := db.ResetRunnerToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "atr_") || len(token) < 40 {
		t.Fatalf("unexpected token format: %q", token)
	}
	createdAt, exists, err := db.RunnerTokenCreatedAt(ctx)
	if err != nil || !exists || createdAt == 0 {
		t.Fatalf("expected token metadata, createdAt=%d exists=%v err=%v", createdAt, exists, err)
	}
	if valid, err := db.ValidateRunnerToken(ctx, token); err != nil || !valid {
		t.Fatalf("expected token to validate, valid=%v err=%v", valid, err)
	}
	if valid, err := db.ValidateRunnerToken(ctx, token+"x"); err != nil || valid {
		t.Fatalf("expected wrong token to fail, valid=%v err=%v", valid, err)
	}

	replacement, err := db.ResetRunnerToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if replacement == token {
		t.Fatal("expected reset to generate a new token")
	}
	if valid, err := db.ValidateRunnerToken(ctx, token); err != nil || valid {
		t.Fatalf("expected old token to be invalidated, valid=%v err=%v", valid, err)
	}
	if valid, err := db.ValidateRunnerToken(ctx, replacement); err != nil || !valid {
		t.Fatalf("expected replacement token to validate, valid=%v err=%v", valid, err)
	}
}

func TestWorkflowRunnersRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	saved, err := db.SaveWorkflow(ctx, WorkflowDefinition{
		DatabaseName: "db",
		Name:         "sync",
		Script:       "function instances() {}",
		Runners:      map[string]string{"pull_orders": "intranet"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := db.Workflow(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Runners["pull_orders"] != "intranet" {
		t.Fatalf("unexpected runners: %#v", loaded.Runners)
	}

	saved.Runners = map[string]string{}
	if _, err := db.SaveWorkflow(ctx, saved); err != nil {
		t.Fatal(err)
	}
	loaded, err = db.Workflow(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Runners) != 0 {
		t.Fatalf("expected runners to be replaced on save, got %#v", loaded.Runners)
	}
}
