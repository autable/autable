package recorddb

import (
	"context"
	"errors"
	"testing"

	"autable/internal/metadata"
	"autable/internal/table"
)

func TestRepositoryCreatesOneSQLiteFilePerMetadataDatabase(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{
		{Name: "sales"},
		{Name: "ops"},
	}}

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	contacts := contactsTable()

	if _, err := repository.CreateRow(ctx, "sales", contacts, map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateRow(ctx, "ops", contacts, map[string]any{"name": "Grace"}); err != nil {
		t.Fatal(err)
	}

	salesRows, err := repository.Rows(ctx, "sales", contacts)
	if err != nil {
		t.Fatal(err)
	}
	opsRows, err := repository.Rows(ctx, "ops", contacts)
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

func TestRepositoryRowsAppliesViewQueryInSQLite(t *testing.T) {
	ctx := context.Background()
	tableMeta := metadata.Table{
		Name: "contacts",
		Fields: []metadata.Field{
			{Name: "name", Type: "string"},
			{Name: "status", Type: "string"},
			{Name: "priority", Type: "int"},
		},
	}
	repository := openTestRepository(t, ctx, tableMeta)
	for _, values := range []map[string]any{
		{"name": "Ada", "status": "active", "priority": int64(1)},
		{"name": "Grace", "status": "active", "priority": int64(3)},
		{"name": "Linus", "status": "archived", "priority": int64(4)},
	} {
		if _, err := repository.CreateRow(ctx, "workspace", tableMeta, values); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := repository.Rows(ctx, "workspace", tableMeta, metadata.ResolvedView{
		Query: &metadata.ViewQuery{
			Combinator: "and",
			Rules: []metadata.ViewQueryRule{
				{Field: "status", Operator: "=", Value: "active"},
				{
					Combinator: "or",
					Rules: []metadata.ViewQueryRule{
						{Field: "name", Operator: "contains", Value: "a"},
						{Field: "priority", Operator: ">=", Value: 2},
					},
				},
			},
		},
		Sorts: []metadata.ViewSort{{Field: "priority", Direction: "desc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Values["name"] != "Grace" || rows[1].Values["name"] != "Ada" {
		t.Fatalf("unexpected SQL-filtered rows: %#v", rows)
	}
}

func TestRepositoryRowsEscapesViewQueryValues(t *testing.T) {
	ctx := context.Background()
	tableMeta := metadata.Table{
		Name:   "contacts",
		Fields: []metadata.Field{{Name: "name", Type: "string"}},
	}
	repository := openTestRepository(t, ctx, tableMeta)
	for _, values := range []map[string]any{
		{"name": "Ada"},
		{"name": "A%"},
		{"name": "A_"},
	} {
		if _, err := repository.CreateRow(ctx, "workspace", tableMeta, values); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := repository.Rows(ctx, "workspace", tableMeta, metadata.ResolvedView{
		Query: &metadata.ViewQuery{
			Combinator: "and",
			Rules:      []metadata.ViewQueryRule{{Field: "name", Operator: "contains", Value: "%"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["name"] != "A%" {
		t.Fatalf("LIKE wildcard value should be literal, got %#v", rows)
	}
}

func TestRepositoryRowsRejectsUnsafeViewFields(t *testing.T) {
	ctx := context.Background()
	tableMeta := metadata.Table{
		Name:   "contacts",
		Fields: []metadata.Field{{Name: "name", Type: "string"}},
	}
	repository := openTestRepository(t, ctx, tableMeta)
	if _, err := repository.CreateRow(ctx, "workspace", tableMeta, map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}

	_, err := repository.Rows(ctx, "workspace", tableMeta, metadata.ResolvedView{
		Query: &metadata.ViewQuery{
			Combinator: "and",
			Rules:      []metadata.ViewQueryRule{{Field: "name OR 1=1", Operator: "=", Value: "Ada"}},
		},
	})
	if err == nil {
		t.Fatal("expected unknown query field to be rejected")
	}
	_, err = repository.Rows(ctx, "workspace", tableMeta, metadata.ResolvedView{
		Sorts: []metadata.ViewSort{{Field: "name desc; drop table contacts", Direction: "asc"}},
	})
	if err == nil {
		t.Fatal("expected unknown sort field to be rejected")
	}
}

func TestRepositoryPersistsRowsAcrossReopenWithRealColumns(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	contacts := contactsTable()

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	row, err := repository.CreateRow(ctx, "workspace", contacts, map[string]any{"name": "Ada", "email": "ada@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := repository.database("workspace")
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := db.Table("contacts").Where(map[string]any{recordIDColumn: row.RecordID}).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored["name"] != "Ada" || stored["email"] != "ada@example.com" {
		t.Fatalf("expected real field columns, got %#v", stored)
	}
	if _, ok := stored["values"]; ok {
		t.Fatalf("records must not use values json column anymore: %#v", stored)
	}
	if int64Value(stored[createdAtColumn]) <= 0 || int64Value(stored[updatedAtColumn]) <= 0 {
		t.Fatalf("expected millisecond integer timestamps, got %#v", stored)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})
	loaded, err := reopened.Row(ctx, "workspace", contacts, row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RecordID != row.RecordID || loaded.Values["name"] != "Ada" {
		t.Fatalf("unexpected persisted row: %#v", loaded)
	}
}

func TestRepositorySupportsUnsafeLogicalTableNames(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	tableMeta := metadata.Table{Name: "测试表", Fields: []metadata.Field{{Name: "name", Type: "string"}, {Name: "count", Type: "int"}}}

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	row, err := repository.CreateRow(ctx, "workspace", tableMeta, map[string]any{"name": "Ada", "count": int64(2)})
	if err != nil {
		t.Fatal(err)
	}
	if row.Values["name"] != "Ada" || row.Values["count"] != int64(2) {
		t.Fatalf("unexpected unsafe table row: %#v", row)
	}
	db, err := repository.database("workspace")
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := db.Table(physicalTableName(tableMeta.Name)).Where(map[string]any{recordIDColumn: row.RecordID}).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored["name"] != "Ada" || int64Value(stored["count"]) != 2 {
		t.Fatalf("expected real columns in physical table, got %#v", stored)
	}
}

func TestRepositorySupportsExternalFieldsNamedCreatedAndUpdated(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	tableMeta := metadata.Table{Name: "b表", Fields: []metadata.Field{
		{Name: "Created", Type: "string"},
		{Name: "Updated", Type: "string"},
		{Name: "Created By", Type: "string"},
	}}

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	row, err := repository.CreateRow(ctx, "workspace", tableMeta, map[string]any{
		"Created":    "remote-created",
		"Updated":    "remote-updated",
		"Created By": "robot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.Values["Created"] != "remote-created" || row.Values["Updated"] != "remote-updated" || row.Values["Created By"] != "robot" {
		t.Fatalf("unexpected external field values: %#v", row.Values)
	}
}

func TestRepositoryAllowsUserRecordIDField(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	tableMeta := metadata.Table{Name: "tasks", Fields: []metadata.Field{{Name: "record_id", Type: "string"}}}

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	row, err := repository.CreateRow(ctx, "workspace", tableMeta, map[string]any{"record_id": "remote-1"})
	if err != nil {
		t.Fatal(err)
	}
	if row.RecordID != 1 || row.Values["record_id"] != "remote-1" {
		t.Fatalf("unexpected record_id field row: %#v", row)
	}
	db, err := repository.database("workspace")
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := db.Table("tasks").Where(map[string]any{recordIDColumn: row.RecordID}).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if int64Value(stored[recordIDColumn]) != 1 || stored["record_id"] != "remote-1" {
		t.Fatalf("expected system ct_record_id and user record_id columns, got %#v", stored)
	}
}

func TestRepositoryAutoMigrateHandlesExistingPhysicalExternalField(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	db, err := repository.database("workspace")
	if err != nil {
		t.Fatal(err)
	}
	tableName := physicalTableName("b表")
	if err := db.Exec(`CREATE TABLE ` + tableName + ` (ct_record_id integer primary key autoincrement, ct_created_at integer not null, ct_updated_at integer not null, Created text)`).Error; err != nil {
		t.Fatal(err)
	}

	tableMeta := metadata.Table{Name: "b表", Fields: []metadata.Field{{Name: "Created", Type: "string"}, {Name: "Title", Type: "string"}}}
	if err := repository.EnsureTable(ctx, "workspace", tableMeta); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn(tableName, "Created") || !db.Migrator().HasColumn(tableName, "Title") {
		t.Fatal("expected existing Created and new Title columns")
	}
}

func TestRepositoryReadsExistingPhysicalColumnWithDifferentCase(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	db, err := repository.database("workspace")
	if err != nil {
		t.Fatal(err)
	}
	tableName := physicalTableName("b表")
	if err := db.Exec(`CREATE TABLE ` + tableName + ` (ct_record_id integer primary key autoincrement, ct_created_at integer not null, ct_updated_at integer not null, created text)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO ` + tableName + ` (ct_created_at, ct_updated_at, created) VALUES (1, 1, 'remote-created')`).Error; err != nil {
		t.Fatal(err)
	}

	tableMeta := metadata.Table{Name: "b表", Fields: []metadata.Field{{Name: "Created", Type: "string"}}}
	rows, err := repository.Rows(ctx, "workspace", tableMeta)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["Created"] != "remote-created" {
		t.Fatalf("expected case-insensitive physical column read, got %#v", rows)
	}
}

func TestRepositoryAllocatesRecordIDsPerTableAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	contacts := contactsTable()
	projects := metadata.Table{Name: "projects", Fields: []metadata.Field{{Name: "name", Type: "string"}}}

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	contact, err := repository.CreateRow(ctx, "workspace", contacts, map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := repository.CreateRow(ctx, "workspace", projects, map[string]any{"name": "Apollo"})
	if err != nil {
		t.Fatal(err)
	}
	if contact.RecordID != 1 || project.RecordID != 1 {
		t.Fatalf("expected each table to start at record_id 1, got contact=%d project=%d", contact.RecordID, project.RecordID)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})
	nextContact, err := reopened.CreateRow(ctx, "workspace", contacts, map[string]any{"name": "Grace"})
	if err != nil {
		t.Fatal(err)
	}
	nextProject, err := reopened.CreateRow(ctx, "workspace", projects, map[string]any{"name": "Gemini"})
	if err != nil {
		t.Fatal(err)
	}
	if nextContact.RecordID != 2 || nextProject.RecordID != 2 {
		t.Fatalf("expected each table to continue independently, got contact=%d project=%d", nextContact.RecordID, nextProject.RecordID)
	}
}

func TestRepositoryRestoresRowsWithOriginalRecordID(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	contacts := contactsTable()

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	row, err := repository.CreateRow(ctx, "workspace", contacts, map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.DeleteRow(ctx, "workspace", contacts, row.RecordID); err != nil {
		t.Fatal(err)
	}
	if err := repository.RestoreRow(ctx, "workspace", contacts, table.Row{
		RecordID: row.RecordID,
		Values:   map[string]any{"name": "Ada restored", "email": "ada@example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	restored, err := repository.Row(ctx, "workspace", contacts, row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.RecordID != row.RecordID || restored.Values["name"] != "Ada restored" {
		t.Fatalf("unexpected restored row: %#v", restored)
	}
	next, err := repository.CreateRow(ctx, "workspace", contacts, map[string]any{"name": "Grace"})
	if err != nil {
		t.Fatal(err)
	}
	if next.RecordID != row.RecordID+1 {
		t.Fatalf("expected record_id to continue after restore, got %d after %d", next.RecordID, row.RecordID)
	}
}

func TestRepositoryUpdateRowReplacesProvidedValuesAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	contacts := contactsTable()

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	row, err := repository.CreateRow(ctx, "workspace", contacts, map[string]any{
		"name":  "Ada",
		"email": "ada@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repository.UpdateRow(ctx, "workspace", contacts, row.RecordID, map[string]any{
		"name":  "Ada",
		"email": "ada@autable.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Values["name"] != "Ada" || updated.Values["email"] != "ada@autable.test" {
		t.Fatalf("unexpected updated row: %#v", updated)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})
	loaded, err := reopened.Row(ctx, "workspace", contacts, row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Values["name"] != "Ada" || loaded.Values["email"] != "ada@autable.test" {
		t.Fatalf("unexpected persisted update: %#v", loaded)
	}
}

func TestRepositoryDeleteRowRemovesPersistedRecord(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}}
	contacts := contactsTable()

	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	row, err := repository.CreateRow(ctx, "workspace", contacts, map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := repository.DeleteRow(ctx, "workspace", contacts, row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Values["name"] != "Ada" {
		t.Fatalf("unexpected deleted row: %#v", deleted)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})
	rows, err := reopened.Rows(ctx, "workspace", contacts)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected deleted row to stay deleted after reopen, got %#v", rows)
	}
}

func openTestRepository(t *testing.T, ctx context.Context, tableMeta metadata.Table) *Repository {
	t.Helper()
	dataDir := t.TempDir()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:   "workspace",
		Tables: []metadata.Table{tableMeta},
	}}}
	repository, err := OpenCatalog(ctx, catalog, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil && !errors.Is(err, ErrUnknownDatabase) {
			t.Fatal(err)
		}
	})
	return repository
}

func contactsTable() metadata.Table {
	return metadata.Table{
		Name: "contacts",
		Fields: []metadata.Field{
			{Name: "name", Type: "string"},
			{Name: "email", Type: "string"},
		},
	}
}
