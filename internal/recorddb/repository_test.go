package recorddb

import (
	"context"
	"path/filepath"
	"testing"

	"codetable/internal/metadata"
)

func TestRepositoryCreatesOneSQLiteFilePerMetadataDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{Name: "sales", SQLitePath: filepath.Join(dir, "sales.sqlite")},
		{Name: "ops", SQLitePath: filepath.Join(dir, "ops.sqlite")},
	}}

	repository, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})

	if _, err := repository.CreateRow(ctx, "sales", "contacts", map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateRow(ctx, "ops", "contacts", map[string]any{"name": "Grace"}); err != nil {
		t.Fatal(err)
	}

	salesRows, err := repository.Rows(ctx, "sales", "contacts")
	if err != nil {
		t.Fatal(err)
	}
	opsRows, err := repository.Rows(ctx, "ops", "contacts")
	if err != nil {
		t.Fatal(err)
	}
	if len(salesRows) != 1 || salesRows[0].Values["name"] != "Ada" {
		t.Fatalf("unexpected sales rows: %#v", salesRows)
	}
	if len(opsRows) != 1 || opsRows[0].Values["name"] != "Grace" {
		t.Fatalf("unexpected ops rows: %#v", opsRows)
	}
}

func TestRepositoryPersistsRowsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.sqlite")
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: path}}}

	repository, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	row, err := repository.CreateRow(ctx, "workspace", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})
	loaded, err := reopened.Row(ctx, "workspace", "contacts", row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RecordID != row.RecordID || loaded.Values["name"] != "Ada" {
		t.Fatalf("unexpected persisted row: %#v", loaded)
	}
}

func TestRepositoryUpdateRowMergesValuesAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.sqlite")
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: path}}}

	repository, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	row, err := repository.CreateRow(ctx, "workspace", "contacts", map[string]any{
		"name":  "Ada",
		"email": "ada@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repository.UpdateRow(ctx, "workspace", "contacts", row.RecordID, map[string]any{
		"email": "ada@codetable.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Values["name"] != "Ada" || updated.Values["email"] != "ada@codetable.test" {
		t.Fatalf("unexpected merged row: %#v", updated)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})
	loaded, err := reopened.Row(ctx, "workspace", "contacts", row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Values["name"] != "Ada" || loaded.Values["email"] != "ada@codetable.test" {
		t.Fatalf("unexpected persisted update: %#v", loaded)
	}
}

func TestRepositoryDeleteRowRemovesPersistedRecord(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.sqlite")
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: path}}}

	repository, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	row, err := repository.CreateRow(ctx, "workspace", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := repository.DeleteRow(ctx, "workspace", "contacts", row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Values["name"] != "Ada" {
		t.Fatalf("unexpected deleted row: %#v", deleted)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})
	rows, err := reopened.Rows(ctx, "workspace", "contacts")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected deleted row to stay deleted after reopen, got %#v", rows)
	}
}
