package codefiles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"autable/internal/repository"
	"autable/internal/systemdb"
)

type Store struct {
	root string
}

func NewStore(root string) *Store {
	return &Store{root: root}
}

func (store *Store) SaveWorkflowScript(ctx context.Context, workflow systemdb.WorkflowDefinition) error {
	return store.writeScript(ctx, repository.WorkflowDir, workflow.DatabaseName, workflow.Name, workflow.Script)
}

func (store *Store) LoadWorkflowScript(ctx context.Context, workflow systemdb.WorkflowDefinition) (string, bool, error) {
	return store.readScript(ctx, repository.WorkflowDir, workflow.DatabaseName, workflow.Name)
}

func (store *Store) DeleteWorkflowScript(ctx context.Context, workflow systemdb.WorkflowDefinition) error {
	return store.deleteScript(ctx, repository.WorkflowDir, workflow.DatabaseName, workflow.Name)
}

func (store *Store) SaveFormScript(ctx context.Context, form systemdb.FormDefinition) error {
	return store.writeScript(ctx, repository.FormDir, form.DatabaseName, form.Name, form.Script)
}

func (store *Store) LoadFormScript(ctx context.Context, form systemdb.FormDefinition) (string, bool, error) {
	return store.readScript(ctx, repository.FormDir, form.DatabaseName, form.Name)
}

func (store *Store) DeleteFormScript(ctx context.Context, form systemdb.FormDefinition) error {
	return store.deleteScript(ctx, repository.FormDir, form.DatabaseName, form.Name)
}

func (store *Store) WorkflowScriptPath(workflow systemdb.WorkflowDefinition) string {
	return store.scriptPath(repository.WorkflowDir, workflow.DatabaseName, workflow.Name)
}

func (store *Store) FormScriptPath(form systemdb.FormDefinition) string {
	return store.scriptPath(repository.FormDir, form.DatabaseName, form.Name)
}

func (store *Store) writeScript(ctx context.Context, kind, databaseName string, name, script string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if store.root == "" {
		return nil
	}
	if databaseName == "" {
		return fmt.Errorf("%s database name is required", kind)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s name is required", kind)
	}

	dir := filepath.Join(store.root, kind, safeSegment(databaseName))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return repository.WriteFileAtomic(store.scriptPath(kind, databaseName, name), []byte(script), 0o644)
}

func (store *Store) readScript(ctx context.Context, kind, databaseName string, name string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if store.root == "" || databaseName == "" || strings.TrimSpace(name) == "" {
		return "", false, nil
	}

	path := store.scriptPath(kind, databaseName, name)
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), true, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	return "", false, nil
}

func (store *Store) deleteScript(ctx context.Context, kind, databaseName string, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if store.root == "" || databaseName == "" || strings.TrimSpace(name) == "" {
		return nil
	}
	err := os.Remove(store.scriptPath(kind, databaseName, name))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (store *Store) scriptPath(kind, databaseName string, name string) string {
	return filepath.Join(store.root, kind, safeSegment(databaseName), safeSegment(name)+".js")
}

var unsafeSegment = regexp.MustCompile(`[^\pL\pN._-]+`)

func safeSegment(value string) string {
	segment := unsafeSegment.ReplaceAllString(strings.TrimSpace(value), "-")
	segment = strings.Trim(segment, ".-")
	if segment == "" {
		return "unnamed"
	}
	return segment
}
