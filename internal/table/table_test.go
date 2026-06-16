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
