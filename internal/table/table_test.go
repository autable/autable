package table

import (
	"context"
	"errors"
	"testing"

	"codetable/internal/history"
	"codetable/internal/metadata"
	"codetable/internal/permission"
)

func TestCreateRowAssignsRecordIDAndWritesHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	service := NewService(store)
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "text", Required: true},
				{Name: "email", Type: "email"},
			},
		}},
	}}}
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})

	row, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{
		"name":  "Ada",
		"email": "ada@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.RecordID != 1 {
		t.Fatalf("expected first record_id to be 1, got %d", row.RecordID)
	}

	entries, err := store.GetPrefix(ctx, history.RowPrefix("db", "contacts", 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one history entry, got %d", len(entries))
	}
	change, err := history.DecodeRowChange(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if change.RecordID != 1 || change.Values["name"] != "Ada" {
		t.Fatalf("unexpected history change: %#v", change)
	}
}

func TestCreateRowRejectsDeletedField(t *testing.T) {
	ctx := context.Background()
	service := NewService(history.NewMemoryStore())
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "text"},
				{Name: "legacy", Type: "text", Deleted: true},
			},
		}},
	}}}
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})

	_, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{"legacy": "x"})
	if !errors.Is(err, ErrDeletedField) {
		t.Fatalf("expected deleted field error, got %v", err)
	}
}

func TestCreateRowEnforcesFieldWritePermission(t *testing.T) {
	ctx := context.Background()
	service := NewService(history.NewMemoryStore())
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "text"}},
		}},
	}}}
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Read,
	})

	_, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{"name": "Ada"})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected permission error, got %v", err)
	}
}

func TestUpdateRowMergesValuesAndWritesHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	service := NewService(store)
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "text"},
				{Name: "email", Type: "email"},
			},
		}},
	}}}
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})

	row, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{
		"name":  "Ada",
		"email": "ada@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateRow(ctx, catalog, perms, "u1", "db", "contacts", row.RecordID, map[string]any{
		"email": "ada@codetable.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Values["name"] != "Ada" || updated.Values["email"] != "ada@codetable.test" {
		t.Fatalf("expected merged values, got %#v", updated.Values)
	}
	entries, err := store.GetPrefix(ctx, history.RowPrefix("db", "contacts", row.RecordID))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected create and update history entries, got %d", len(entries))
	}
	change, err := history.DecodeRowChange(entries[1])
	if err != nil {
		t.Fatal(err)
	}
	if change.Values["email"] != "ada@codetable.test" || change.Values["name"] != "Ada" {
		t.Fatalf("unexpected update history: %#v", change)
	}
}

func TestUpdateRowRejectsRecordIDAndReadOnlyField(t *testing.T) {
	ctx := context.Background()
	service := NewService(history.NewMemoryStore())
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "text"}},
		}},
	}}}
	writePerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})
	readPerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Read,
	})
	row, err := service.CreateRow(ctx, catalog, writePerms, "u1", "db", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.UpdateRow(ctx, catalog, writePerms, "u1", "db", "contacts", row.RecordID, map[string]any{"record_id": 99})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected record_id permission error, got %v", err)
	}
	_, err = service.UpdateRow(ctx, catalog, readPerms, "u1", "db", "contacts", row.RecordID, map[string]any{"name": "Grace"})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected read-only permission error, got %v", err)
	}
}

func TestDeleteRowRequiresTableWriteRemovesRowAndWritesHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	service := NewService(store)
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "text"}},
		}},
	}}}
	writePerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})
	readPerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Read,
	})

	row, err := service.CreateRow(ctx, catalog, writePerms, "u1", "db", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteRow(ctx, catalog, readPerms, "u1", "db", "contacts", row.RecordID); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected read-only delete permission error, got %v", err)
	}
	deleted, err := service.DeleteRow(ctx, catalog, writePerms, "u1", "db", "contacts", row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Values["name"] != "Ada" {
		t.Fatalf("expected deleted row values, got %#v", deleted)
	}
	rows, err := service.Rows(ctx, catalog, writePerms, "u1", "db", "contacts", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected row to be deleted, got %#v", rows)
	}
	entries, err := store.GetPrefix(ctx, history.RowPrefix("db", "contacts", row.RecordID))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected create and delete history entries, got %d", len(entries))
	}
	change, err := history.DecodeRowChange(entries[1])
	if err != nil {
		t.Fatal(err)
	}
	if change.Operation != "delete" || change.Values["name"] != "Ada" {
		t.Fatalf("unexpected delete history: %#v", change)
	}
}

func TestCreateRowUsesInjectedRepository(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	repository := NewMemoryRowRepository()
	service := NewServiceWithRepository(store, repository)
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "text"}},
		}},
	}}}
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})

	first, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{"name": "Grace"})
	if err != nil {
		t.Fatal(err)
	}
	if first.RecordID != 1 || second.RecordID != 2 {
		t.Fatalf("expected injected repository to allocate ids, got %d and %d", first.RecordID, second.RecordID)
	}
}

func TestRowsAppliesComposedViewFiltersAndSorts(t *testing.T) {
	ctx := context.Background()
	service := NewService(history.NewMemoryStore())
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "text"},
				{Name: "status", Type: "text"},
			},
			Views: []metadata.View{
				{
					Name:    "active",
					Filters: []metadata.ViewFilter{{Field: "status", Op: "eq", Value: "active"}},
				},
				{
					Name:     "active-a",
					BaseView: "active",
					Filters:  []metadata.ViewFilter{{Field: "name", Op: "contains", Value: "a"}},
					Sorts:    []metadata.ViewSort{{Field: "name", Direction: "desc"}},
				},
			},
		}},
	}}}
	perms := permission.New(
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeTable, Resource: "db.contacts", Level: permission.Write},
	)
	for _, values := range []map[string]any{
		{"name": "Ada", "status": "active"},
		{"name": "Grace", "status": "active"},
		{"name": "Linus", "status": "archived"},
	} {
		if _, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", values); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := service.Rows(ctx, catalog, perms, "u1", "db", "contacts", "active-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two rows, got %#v", rows)
	}
	if rows[0].Values["name"] != "Grace" || rows[1].Values["name"] != "Ada" {
		t.Fatalf("unexpected sorted view rows: %#v", rows)
	}
}
