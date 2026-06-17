package table

import (
	"context"
	"errors"
	"fmt"
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
	if change.Diff["name"].Old != nil || change.Diff["name"].New != "Ada" || change.Diff["email"].New != "ada@example.com" {
		t.Fatalf("unexpected create diff: %#v", change.Diff)
	}
}

func TestCreateRowNotifiesHistoryBackedRowChange(t *testing.T) {
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
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})
	var notifiedKey string
	var notifiedChange history.RowChange
	service.SetRowChangeHandler(func(_ context.Context, historyKey string, change history.RowChange) {
		notifiedKey = historyKey
		notifiedChange = change
	})

	if _, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if notifiedKey == "" || notifiedChange.Operation != "create" || notifiedChange.Diff["name"].New != "Ada" {
		t.Fatalf("unexpected row change notification: key=%q change=%#v", notifiedKey, notifiedChange)
	}
	entry, err := store.Get(ctx, notifiedKey)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := history.DecodeRowChange(entry)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RecordID != notifiedChange.RecordID || saved.Values["name"] != "Ada" {
		t.Fatalf("notification did not reference saved history: saved=%#v notified=%#v", saved, notifiedChange)
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

func TestCreateRowHonorsFieldOverrideOfTableWrite(t *testing.T) {
	ctx := context.Background()
	service := NewService(history.NewMemoryStore())
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
	perms := permission.New(
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeTable,
			Resource:  "db.contacts",
			Level:     permission.Write,
		},
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeField,
			Resource:  "db.contacts",
			Field:     "email",
			Level:     permission.Read,
		},
	)

	if _, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	_, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{
		"name":  "Grace",
		"email": "grace@example.com",
	})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected field override permission error, got %v", err)
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
	if _, ok := change.Diff["name"]; ok {
		t.Fatalf("unchanged field should not be in diff: %#v", change.Diff)
	}
	if change.Diff["email"].Old != "ada@example.com" || change.Diff["email"].New != "ada@codetable.test" {
		t.Fatalf("unexpected update diff: %#v", change.Diff)
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

func TestUpdateRowHonorsFieldOverrideOfTableWrite(t *testing.T) {
	ctx := context.Background()
	service := NewService(history.NewMemoryStore())
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
	writePerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})
	fieldReadPerms := permission.New(
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeTable,
			Resource:  "db.contacts",
			Level:     permission.Write,
		},
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeField,
			Resource:  "db.contacts",
			Field:     "email",
			Level:     permission.Read,
		},
	)
	row, err := service.CreateRow(ctx, catalog, writePerms, "u1", "db", "contacts", map[string]any{
		"name":  "Ada",
		"email": "ada@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.UpdateRow(ctx, catalog, fieldReadPerms, "u1", "db", "contacts", row.RecordID, map[string]any{"name": "Grace"}); err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdateRow(ctx, catalog, fieldReadPerms, "u1", "db", "contacts", row.RecordID, map[string]any{"email": "blocked@example.com"})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected field override permission error, got %v", err)
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
	if change.Diff["name"].Old != "Ada" || change.Diff["name"].New != nil {
		t.Fatalf("unexpected delete diff: %#v", change.Diff)
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

func TestCreateRowRollsBackWhenHistoryWriteFails(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRowRepository()
	service := NewServiceWithRepository(failingHistoryStore{}, repository)
	catalog := testTableCatalog()
	perms := testWritePerms()

	if _, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{"name": "Ada"}); err == nil {
		t.Fatal("expected history failure")
	}
	rows, err := repository.Rows(ctx, "db", "contacts")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected create rollback when history fails, got %#v", rows)
	}
}

func TestUpdateRowDoesNotMutateWhenHistoryWriteFails(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRowRepository()
	catalog := testTableCatalog()
	perms := testWritePerms()
	row, err := NewServiceWithRepository(history.NewMemoryStore(), repository).CreateRow(
		ctx,
		catalog,
		perms,
		"u1",
		"db",
		"contacts",
		map[string]any{"name": "Ada"},
	)
	if err != nil {
		t.Fatal(err)
	}
	service := NewServiceWithRepository(failingHistoryStore{}, repository)
	if _, err := service.UpdateRow(ctx, catalog, perms, "u1", "db", "contacts", row.RecordID, map[string]any{"name": "Grace"}); err == nil {
		t.Fatal("expected history failure")
	}
	loaded, err := repository.Row(ctx, "db", "contacts", row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Values["name"] != "Ada" {
		t.Fatalf("expected update to be skipped when history fails, got %#v", loaded)
	}
}

func TestDeleteRowDoesNotMutateWhenHistoryWriteFails(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRowRepository()
	catalog := testTableCatalog()
	perms := testWritePerms()
	row, err := NewServiceWithRepository(history.NewMemoryStore(), repository).CreateRow(
		ctx,
		catalog,
		perms,
		"u1",
		"db",
		"contacts",
		map[string]any{"name": "Ada"},
	)
	if err != nil {
		t.Fatal(err)
	}
	service := NewServiceWithRepository(failingHistoryStore{}, repository)
	if _, err := service.DeleteRow(ctx, catalog, perms, "u1", "db", "contacts", row.RecordID); err == nil {
		t.Fatal("expected history failure")
	}
	loaded, err := repository.Row(ctx, "db", "contacts", row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Values["name"] != "Ada" {
		t.Fatalf("expected delete to be skipped when history fails, got %#v", loaded)
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

func TestFormulaFieldsAreComputedAndNotWritable(t *testing.T) {
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
				{Name: "score", Type: "number"},
				{Name: "score_plus_one", Type: "formula", Formula: "field_score + 1"},
				{Name: "score_band", Type: "formula", Formula: "field_score >= 5 ? 'high' : 'low'"},
			},
			Views: []metadata.View{{
				Name:    "high",
				Filters: []metadata.ViewFilter{{Field: "score_band", Op: "eq", Value: "high"}},
				Sorts:   []metadata.ViewSort{{Field: "score_plus_one", Direction: "desc"}},
			}},
		}},
	}}}
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})

	low, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{
		"name":  "Ada",
		"score": 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(low.Values["score_plus_one"]) != "5" || low.Values["score_band"] != "low" {
		t.Fatalf("expected computed formula values, got %#v", low.Values)
	}
	if _, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{
		"name":           "Blocked",
		"score":          9,
		"score_plus_one": 10,
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected formula create write to be denied, got %v", err)
	}
	high, err := service.CreateRow(ctx, catalog, perms, "u1", "db", "contacts", map[string]any{
		"name":  "Grace",
		"score": 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateRow(ctx, catalog, perms, "u1", "db", "contacts", high.RecordID, map[string]any{
		"score_plus_one": 99,
	}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected formula update write to be denied, got %v", err)
	}

	rows, err := service.Rows(ctx, catalog, perms, "u1", "db", "contacts", "high")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["name"] != "Grace" || rows[0].Values["score_band"] != "high" {
		t.Fatalf("expected formula-filtered high row, got %#v", rows)
	}
	entries, err := store.GetPrefix(ctx, history.RowPrefix("db", "contacts", low.RecordID))
	if err != nil {
		t.Fatal(err)
	}
	change, err := history.DecodeRowChange(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := change.Values["score_plus_one"]; ok {
		t.Fatalf("formula value should not be written to history: %#v", change.Values)
	}
}

type failingHistoryStore struct{}

func (failingHistoryStore) Put(context.Context, string, []byte) error {
	return errors.New("history write failed")
}

func (failingHistoryStore) Get(context.Context, string) (history.Entry, error) {
	return history.Entry{}, history.ErrNotFound
}

func (failingHistoryStore) GetPrefix(context.Context, string) ([]history.Entry, error) {
	return nil, nil
}

func testTableCatalog() metadata.Catalog {
	return metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "text"}},
		}},
	}}}
}

func testWritePerms() permission.Set {
	return permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})
}

func TestRowsRejectsViewsUsingUnreadableFields(t *testing.T) {
	ctx := context.Background()
	service := NewService(history.NewMemoryStore())
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name:       "db",
		SQLitePath: "./db.sqlite",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "text"},
				{Name: "email", Type: "email"},
				{Name: "status", Type: "text"},
			},
			Views: []metadata.View{
				{Name: "active", Filters: []metadata.ViewFilter{{Field: "status", Op: "eq", Value: "active"}}},
				{Name: "email-sort", Sorts: []metadata.ViewSort{{Field: "email", Direction: "asc"}}},
			},
		}},
	}}}
	writerPerms := permission.New(permission.Grant{
		SubjectID: "writer",
		Scope:     permission.ScopeTable,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})
	if _, err := service.CreateRow(ctx, catalog, writerPerms, "writer", "db", "contacts", map[string]any{
		"name":   "Ada",
		"email":  "ada@example.com",
		"status": "active",
	}); err != nil {
		t.Fatal(err)
	}
	readerPerms := permission.New(permission.Grant{
		SubjectID: "reader",
		Scope:     permission.ScopeField,
		Resource:  "db.contacts",
		Field:     "email",
		Level:     permission.Read,
	})

	if _, err := service.Rows(ctx, catalog, readerPerms, "reader", "db", "contacts", "active"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expected unreadable filter permission error, got %v", err)
	}
	rows, err := service.Rows(ctx, catalog, readerPerms, "reader", "db", "contacts", "email-sort")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["email"] != "ada@example.com" {
		t.Fatalf("expected readable email view rows, got %#v", rows)
	}
	if _, ok := rows[0].Values["status"]; ok {
		t.Fatalf("row leaked unreadable status: %#v", rows[0].Values)
	}
}
