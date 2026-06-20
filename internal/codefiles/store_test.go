package codefiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"autable/internal/systemdb"
)

func TestStoreWritesWorkflowAndFormScripts(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())

	workflow := systemdb.WorkflowDefinition{
		ID:           1,
		DatabaseName: "workspace",
		Name:         "welcome contact",
		Script:       "function run(info) { return info.inputs; }",
	}
	if err := store.SaveWorkflowScript(ctx, workflow); err != nil {
		t.Fatal(err)
	}
	workflowScript, err := os.ReadFile(filepath.Join(store.root, "workflow", "workspace", "welcome-contact.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(workflowScript) != workflow.Script {
		t.Fatalf("unexpected workflow script: %s", workflowScript)
	}

	form := systemdb.FormDefinition{
		ID:           2,
		DatabaseName: "workspace",
		Name:         "quick status",
		Script:       "function render(api, root) { root.append(api.input({ field: 'email' }), api.submit('Save')); return { table: 'contacts' }; }",
	}
	if err := store.SaveFormScript(ctx, form); err != nil {
		t.Fatal(err)
	}
	formScript, err := os.ReadFile(filepath.Join(store.root, "form", "workspace", "quick-status.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(formScript) != form.Script {
		t.Fatalf("unexpected form script: %s", formScript)
	}
}

func TestStorePreservesReadableUnicodeNames(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())

	workflow := systemdb.WorkflowDefinition{
		ID:           1,
		DatabaseName: "客户库",
		Name:         "欢迎 联系人",
		Script:       "function run() {}",
	}
	if err := store.SaveWorkflowScript(ctx, workflow); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.root, "workflow", "客户库", "欢迎-联系人.js")); err != nil {
		t.Fatalf("expected readable unicode workflow path, got %v", err)
	}
}

func TestStoreDeletesScriptFiles(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	workflow := systemdb.WorkflowDefinition{
		ID:           1,
		DatabaseName: "workspace",
		Name:         "notify",
		Script:       "old",
	}
	if err := store.SaveWorkflowScript(ctx, workflow); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteWorkflowScript(ctx, workflow); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.root, "workflow", "workspace", "notify.js")); !os.IsNotExist(err) {
		t.Fatalf("expected workflow script to be removed, got %v", err)
	}
}

func TestStoreLoadsScriptFilesByCurrentPath(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	workflow := systemdb.WorkflowDefinition{
		ID:           1,
		DatabaseName: "workspace",
		Name:         "notify",
		Script:       "database copy",
	}
	if err := store.SaveWorkflowScript(ctx, workflow); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.WorkflowScriptPath(workflow), []byte("file copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	script, ok, err := store.LoadWorkflowScript(ctx, workflow)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || script != "file copy" {
		t.Fatalf("expected workflow script from file, got ok=%v script=%q", ok, script)
	}
}
