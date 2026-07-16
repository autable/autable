package systemdb

import (
	"context"
	"strings"
	"testing"
)

func TestRunnerTokensAreDatabaseScoped(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, exists, err := db.RunnerTokenCreatedAt(ctx, "db"); err != nil || exists {
		t.Fatalf("expected no token before reset, exists=%v err=%v", exists, err)
	}
	if _, ok, err := db.LookupRunnerToken(ctx, "atr_anything"); err != nil || ok {
		t.Fatalf("expected lookup to fail before reset, ok=%v err=%v", ok, err)
	}

	token, err := db.ResetRunnerToken(ctx, "db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "atr_") || len(token) < 40 {
		t.Fatalf("unexpected token format: %q", token)
	}
	otherToken, err := db.ResetRunnerToken(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	if otherToken == token {
		t.Fatal("expected distinct tokens per database")
	}

	dbName, ok, err := db.LookupRunnerToken(ctx, token)
	if err != nil || !ok || dbName != "db" {
		t.Fatalf("expected token to resolve to db, got %q ok=%v err=%v", dbName, ok, err)
	}
	dbName, ok, err = db.LookupRunnerToken(ctx, otherToken)
	if err != nil || !ok || dbName != "other" {
		t.Fatalf("expected token to resolve to other, got %q ok=%v err=%v", dbName, ok, err)
	}
	if createdAt, exists, err := db.RunnerTokenCreatedAt(ctx, "db"); err != nil || !exists || createdAt == 0 {
		t.Fatalf("expected token metadata, createdAt=%d exists=%v err=%v", createdAt, exists, err)
	}

	replacement, err := db.ResetRunnerToken(ctx, "db")
	if err != nil {
		t.Fatal(err)
	}
	if replacement == token {
		t.Fatal("expected reset to generate a new token")
	}
	if _, ok, err := db.LookupRunnerToken(ctx, token); err != nil || ok {
		t.Fatalf("expected old token to be invalidated, ok=%v err=%v", ok, err)
	}
	if dbName, ok, err := db.LookupRunnerToken(ctx, replacement); err != nil || !ok || dbName != "db" {
		t.Fatalf("expected replacement token to resolve, got %q ok=%v err=%v", dbName, ok, err)
	}
	// Resetting one database must not affect another.
	if dbName, ok, err := db.LookupRunnerToken(ctx, otherToken); err != nil || !ok || dbName != "other" {
		t.Fatalf("expected other database token to survive, got %q ok=%v err=%v", dbName, ok, err)
	}

	if _, err := db.ResetRunnerToken(ctx, ""); err == nil {
		t.Fatal("expected empty database name to be rejected")
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
