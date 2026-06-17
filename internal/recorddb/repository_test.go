package recorddb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codetable/internal/metadata"
	"codetable/internal/table"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type oldRecordTimestampModel struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	RecordID  int64  `gorm:"column:record_id"`
	Table     string `gorm:"column:table_name"`
	Values    JSONMap
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (oldRecordTimestampModel) TableName() string {
	return "records"
}

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
	db, err := reopened.database("workspace")
	if err != nil {
		t.Fatal(err)
	}
	var stored Record
	if err := db.First(&stored, &Record{RecordID: row.RecordID, TableName: "contacts"}).Error; err != nil {
		t.Fatal(err)
	}
	if stored.CreatedAt <= 0 || stored.UpdatedAt <= 0 {
		t.Fatalf("expected millisecond integer timestamps, got created=%d updated=%d", stored.CreatedAt, stored.UpdatedAt)
	}
}

func TestRepositoryDropsIncompatibleTimestampTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.sqlite")
	raw, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.WithContext(ctx).AutoMigrate(&oldRecordTimestampModel{}); err != nil {
		t.Fatal(err)
	}
	if err := raw.WithContext(ctx).Create(&oldRecordTimestampModel{
		RecordID: 1,
		Table:    "contacts",
		Values:   JSONMap{"name": "Legacy"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := raw.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: path}}}
	repository, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	rows, err := repository.Rows(ctx, "workspace", "contacts")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected incompatible records table to be dropped, got %#v", rows)
	}
	row, err := repository.CreateRow(ctx, "workspace", "contacts", map[string]any{"name": "Current"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := repository.database("workspace")
	if err != nil {
		t.Fatal(err)
	}
	var stored Record
	if err := db.First(&stored, &Record{RecordID: row.RecordID, TableName: "contacts"}).Error; err != nil {
		t.Fatal(err)
	}
	if stored.CreatedAt <= 0 || stored.UpdatedAt <= 0 {
		t.Fatalf("expected millisecond row timestamps after schema rebuild, got %#v", stored)
	}
}

func TestRepositoryAllocatesRecordIDsPerTableAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.sqlite")
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: path}}}

	repository, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	contact, err := repository.CreateRow(ctx, "workspace", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := repository.CreateRow(ctx, "workspace", "projects", map[string]any{"name": "Apollo"})
	if err != nil {
		t.Fatal(err)
	}
	if contact.RecordID != 1 || project.RecordID != 1 {
		t.Fatalf("expected each table to start at record_id 1, got contact=%d project=%d", contact.RecordID, project.RecordID)
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
	nextContact, err := reopened.CreateRow(ctx, "workspace", "contacts", map[string]any{"name": "Grace"})
	if err != nil {
		t.Fatal(err)
	}
	nextProject, err := reopened.CreateRow(ctx, "workspace", "projects", map[string]any{"name": "Gemini"})
	if err != nil {
		t.Fatal(err)
	}
	if nextContact.RecordID != 2 || nextProject.RecordID != 2 {
		t.Fatalf("expected each table to continue independently, got contact=%d project=%d", nextContact.RecordID, nextProject.RecordID)
	}
}

func TestRepositoryRestoresRowsWithOriginalRecordID(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.sqlite")
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace", SQLitePath: path}}}

	repository, err := OpenCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	row, err := repository.CreateRow(ctx, "workspace", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.DeleteRow(ctx, "workspace", "contacts", row.RecordID); err != nil {
		t.Fatal(err)
	}
	if err := repository.RestoreRow(ctx, "workspace", "contacts", table.Row{
		RecordID: row.RecordID,
		Values:   map[string]any{"name": "Ada restored"},
	}); err != nil {
		t.Fatal(err)
	}
	restored, err := repository.Row(ctx, "workspace", "contacts", row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.RecordID != row.RecordID || restored.Values["name"] != "Ada restored" {
		t.Fatalf("unexpected restored row: %#v", restored)
	}
	next, err := repository.CreateRow(ctx, "workspace", "contacts", map[string]any{"name": "Grace"})
	if err != nil {
		t.Fatal(err)
	}
	if next.RecordID != row.RecordID+1 {
		t.Fatalf("expected record_id to continue after restore, got %d after %d", next.RecordID, row.RecordID)
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
