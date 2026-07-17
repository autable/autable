package table_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"autable/internal/history"
	"autable/internal/metadata"
	"autable/internal/permission"
	"autable/internal/recorddb"
	"autable/internal/table"
)

func TestCreateRowAssignsRecordIDAndWritesHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "email", Type: "string"},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, store, catalog)
	perms := permission.New(
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeViewSet, Resource: "db.contacts", Level: permission.Write},
	)

	row, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{
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
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, store, catalog)
	perms := permission.New(
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeViewSet, Resource: "db.contacts", Level: permission.Write},
	)
	var notifiedKey string
	var notifiedChange history.RowChange
	service.SetRowChangeHandler(func(_ context.Context, historyKey string, change history.RowChange) {
		notifiedKey = historyKey
		notifiedChange = change
	})

	if _, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"name": "Ada"}); err != nil {
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
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "legacy", Type: "string", Deleted: true},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, history.NewMemoryStore(), catalog)
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write})

	_, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"legacy": "x"})
	if !errors.Is(err, table.ErrDeletedField) {
		t.Fatalf("expected deleted field error, got %v", err)
	}
}

func TestCreateRowEnforcesFieldWritePermission(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, history.NewMemoryStore(), catalog)
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Read,
	}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write})

	_, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"name": "Ada"})
	if !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected permission error, got %v", err)
	}
}

func TestCreateRowRequiresRecordCreatePermission(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, history.NewMemoryStore(), catalog)
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	})

	_, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"name": "Ada"})
	if !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected record create permission error, got %v", err)
	}
}

func TestCreateRowHonorsPartialFieldWriteGrant(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "email", Type: "string"},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, history.NewMemoryStore(), catalog)
	perms := permission.New(
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeField,
			Resource:  "db.contacts",
			Field:     "name",
			Level:     permission.Write,
		},
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeField,
			Resource:  "db.contacts",
			Field:     "email",
			Level:     permission.Read,
		},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
	)

	if _, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	_, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{
		"name":  "Grace",
		"email": "grace@example.com",
	})
	if !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected field override permission error, got %v", err)
	}
}

func TestUpdateRowMergesValuesAndWritesHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "email", Type: "string"},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, store, catalog)
	perms := permission.New(
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeViewSet, Resource: "db.contacts", Level: permission.Write},
	)

	row, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{
		"name":  "Ada",
		"email": "ada@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateRow(ctx, catalog, perms, "u1", false, "db", "contacts", row.RecordID, map[string]any{
		"email": "ada@autable.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Values["name"] != "Ada" || updated.Values["email"] != "ada@autable.test" {
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
	if change.Values["email"] != "ada@autable.test" || change.Values["name"] != "Ada" {
		t.Fatalf("unexpected update history: %#v", change)
	}
	if _, ok := change.Diff["name"]; ok {
		t.Fatalf("unchanged field should not be in diff: %#v", change.Diff)
	}
	if change.Diff["email"].Old != "ada@example.com" || change.Diff["email"].New != "ada@autable.test" {
		t.Fatalf("unexpected update diff: %#v", change.Diff)
	}
}

func TestUpdateRowRejectsRecordIDAndReadOnlyField(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, history.NewMemoryStore(), catalog)
	writePerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write})
	readPerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Read,
	})
	row, err := service.CreateRow(ctx, catalog, writePerms, "u1", false, "db", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.UpdateRow(ctx, catalog, writePerms, "u1", false, "db", "contacts", row.RecordID, map[string]any{"ct_record_id": 99})
	if !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected ct_record_id permission error, got %v", err)
	}
	_, err = service.UpdateRow(ctx, catalog, readPerms, "u1", false, "db", "contacts", row.RecordID, map[string]any{"name": "Grace"})
	if !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected read-only permission error, got %v", err)
	}
}

func TestUpdateRowHonorsPartialFieldWriteGrant(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "email", Type: "string"},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, history.NewMemoryStore(), catalog)
	writePerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write})
	fieldReadPerms := permission.New(
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeField,
			Resource:  "db.contacts",
			Field:     "name",
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
	row, err := service.CreateRow(ctx, catalog, writePerms, "u1", false, "db", "contacts", map[string]any{
		"name":  "Ada",
		"email": "ada@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.UpdateRow(ctx, catalog, fieldReadPerms, "u1", false, "db", "contacts", row.RecordID, map[string]any{"name": "Grace"}); err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdateRow(ctx, catalog, fieldReadPerms, "u1", false, "db", "contacts", row.RecordID, map[string]any{"email": "blocked@example.com"})
	if !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected field override permission error, got %v", err)
	}
}

func TestDeleteRowRequiresRecordDeletePermissionRemovesRowAndWritesHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, store, catalog)
	writePerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "delete", Level: permission.Write})
	readPerms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Read,
	})
	row, err := service.CreateRow(ctx, catalog, writePerms, "u1", false, "db", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteRow(ctx, catalog, readPerms, "u1", false, "db", "contacts", row.RecordID); !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected read-only delete permission error, got %v", err)
	}
	deleted, err := service.DeleteRow(ctx, catalog, writePerms, "u1", false, "db", "contacts", row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Values["name"] != "Ada" {
		t.Fatalf("expected deleted row values, got %#v", deleted)
	}
	rows, err := service.Rows(ctx, catalog, writePerms, "u1", false, "db", "contacts", "")
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
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, store, catalog)
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write})

	first, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"name": "Grace"})
	if err != nil {
		t.Fatal(err)
	}
	if first.RecordID != 1 || second.RecordID != 2 {
		t.Fatalf("expected injected repository to allocate ids, got %d and %d", first.RecordID, second.RecordID)
	}
}

func TestCreateRowRollsBackWhenHistoryWriteFails(t *testing.T) {
	ctx := context.Background()
	catalog := testTableCatalog()
	service, catalog, repository := newSQLiteService(t, failingHistoryStore{}, catalog)
	perms := testWritePerms()

	if _, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"name": "Ada"}); err == nil {
		t.Fatal("expected history failure")
	}
	tableMeta, _ := catalog.Table("db", "contacts")
	rows, err := repository.Rows(ctx, "db", tableMeta)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected create rollback when history fails, got %#v", rows)
	}
}

func TestUpdateRowDoesNotMutateWhenHistoryWriteFails(t *testing.T) {
	ctx := context.Background()
	catalog := testTableCatalog()
	createService, catalog, repository := newSQLiteService(t, history.NewMemoryStore(), catalog)
	perms := testWritePerms()
	row, err := createService.CreateRow(
		ctx,
		catalog,
		perms,
		"u1",
		false, "db",
		"contacts",
		map[string]any{"name": "Ada"},
	)
	if err != nil {
		t.Fatal(err)
	}
	service := table.NewServiceWithRepository(failingHistoryStore{}, repository)
	if _, err := service.UpdateRow(ctx, catalog, perms, "u1", false, "db", "contacts", row.RecordID, map[string]any{"name": "Grace"}); err == nil {
		t.Fatal("expected history failure")
	}
	tableMeta, _ := catalog.Table("db", "contacts")
	loaded, err := repository.Row(ctx, "db", tableMeta, row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Values["name"] != "Ada" {
		t.Fatalf("expected update to be skipped when history fails, got %#v", loaded)
	}
}

func TestDeleteRowDoesNotMutateWhenHistoryWriteFails(t *testing.T) {
	ctx := context.Background()
	catalog := testTableCatalog()
	createService, catalog, repository := newSQLiteService(t, history.NewMemoryStore(), catalog)
	perms := testWritePerms()
	row, err := createService.CreateRow(
		ctx,
		catalog,
		perms,
		"u1",
		false, "db",
		"contacts",
		map[string]any{"name": "Ada"},
	)
	if err != nil {
		t.Fatal(err)
	}
	service := table.NewServiceWithRepository(failingHistoryStore{}, repository)
	if _, err := service.DeleteRow(ctx, catalog, perms, "u1", true, "db", "contacts", row.RecordID); err == nil {
		t.Fatal("expected history failure")
	}
	tableMeta, _ := catalog.Table("db", "contacts")
	loaded, err := repository.Row(ctx, "db", tableMeta, row.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Values["name"] != "Ada" {
		t.Fatalf("expected delete to be skipped when history fails, got %#v", loaded)
	}
}

func TestRowsAppliesComposedViewQueryAndSorts(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "status", Type: "string"},
			},
			Views: []metadata.View{
				{
					Name: "active",
					Query: &metadata.ViewQuery{
						Combinator: "and",
						Rules:      []metadata.ViewQueryRule{{Field: "status", Operator: "=", Value: "active"}},
					},
				},
				{
					Name:     "active-a",
					BaseView: "active",
					Query: &metadata.ViewQuery{
						Combinator: "and",
						Rules:      []metadata.ViewQueryRule{{Field: "name", Operator: "contains", Value: "a"}},
					},
					Sorts: []metadata.ViewSort{{Field: "name", Direction: "desc"}},
				},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, history.NewMemoryStore(), catalog)
	perms := permission.New(
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeViewSet, Resource: "db.contacts", Level: permission.Write},
	)
	for _, values := range []map[string]any{
		{"name": "Ada", "status": "active"},
		{"name": "Grace", "status": "active"},
		{"name": "Linus", "status": "archived"},
	} {
		if _, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", values); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := service.Rows(ctx, catalog, perms, "u1", false, "db", "contacts", "active-a")
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
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "提交人（人员）", Type: "string"},
				{Name: "score", Type: "float"},
				{Name: "score_plus_one", Type: "formula", ValueType: "float", Formula: `fields["score"] + 1`},
				{Name: "score_band", Type: "formula", ValueType: "string", Formula: `fields["score"] >= 5 ? 'high' : 'low'`},
				{Name: "row_label", Type: "formula", ValueType: "string", Formula: "'row-' + record_id"},
				{Name: "stable_json", Type: "formula", ValueType: "string", Formula: `stableStringify({ b: fields["score"], a: fields["name"] })`},
				{Name: "submitter_label", Type: "formula", ValueType: "string", Formula: `fields["提交人（人员）"] + " / " + fields["name"]`},
			},
			Views: []metadata.View{{
				Name: "high",
				Query: &metadata.ViewQuery{
					Combinator: "and",
					Rules:      []metadata.ViewQueryRule{{Field: "score_band", Operator: "=", Value: "high"}},
				},
				Sorts: []metadata.ViewSort{{Field: "score_plus_one", Direction: "desc"}},
			}},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, store, catalog)
	perms := permission.New(
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
		permission.Grant{SubjectID: "u1", Scope: permission.ScopeViewSet, Resource: "db.contacts", Level: permission.Write},
	)

	low, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{
		"name":    "Ada",
		"提交人（人员）": "张三",
		"score":   4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(low.Values["score_plus_one"]) != "5" || low.Values["score_band"] != "low" || low.Values["row_label"] != "row-1" || low.Values["stable_json"] != `{"a":"Ada","b":4}` || low.Values["submitter_label"] != "张三 / Ada" {
		t.Fatalf("expected computed formula values, got %#v", low.Values)
	}
	if _, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{
		"name":           "Blocked",
		"score":          9,
		"score_plus_one": 10,
	}); !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected formula create write to be denied, got %v", err)
	}
	high, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{
		"name":  "Grace",
		"score": 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateRow(ctx, catalog, perms, "u1", false, "db", "contacts", high.RecordID, map[string]any{
		"score_plus_one": 99,
	}); !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected formula update write to be denied, got %v", err)
	}

	rows, err := service.Rows(ctx, catalog, perms, "u1", false, "db", "contacts", "high")
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
	if fmt.Sprint(change.Values["score_plus_one"]) != "5" || change.Values["score_band"] != "low" {
		t.Fatalf("expected persisted formula values in row history, got %#v", change.Values)
	}
}

func TestSyncTableRecomputesFormulaFieldsWithoutHistory(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "score", Type: "int"},
				{Name: "score_formula", Type: "formula", ValueType: "int", Formula: `fields["score"] + 1`},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, store, catalog)
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write})

	row, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"score": 4})
	if err != nil {
		t.Fatal(err)
	}
	if row.Values["score_formula"] != int64(5) {
		t.Fatalf("expected initial formula value, got %#v", row.Values)
	}
	updatedCatalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "score", Type: "int"},
				{Name: "score_formula", Type: "formula", ValueType: "int", Formula: `fields["score"] + 2`},
			},
		}},
	}}}
	if err := service.SyncTable(ctx, updatedCatalog, "db", "contacts"); err != nil {
		t.Fatal(err)
	}
	rows, err := service.Rows(ctx, updatedCatalog, perms, "u1", false, "db", "contacts", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["score_formula"] != int64(6) {
		t.Fatalf("expected recomputed formula value, got %#v", rows)
	}
	entries, err := store.GetPrefix(ctx, history.RowPrefix("db", "contacts", row.RecordID))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("formula system recompute must not write history, got %d entries", len(entries))
	}
}

func TestFormulaErrorsClearValueInsteadOfFailingWrite(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "score", Type: "int"},
				{Name: "aaa", Type: "formula", ValueType: "int", Formula: `fields["score"] / 2`},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, store, catalog)
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write})

	row, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"score": 1})
	if err != nil {
		t.Fatal(err)
	}
	if row.Values["aaa"] != nil {
		t.Fatalf("expected invalid int formula to be cleared, got %#v", row.Values)
	}

	row, err = service.UpdateRow(ctx, catalog, perms, "u1", false, "db", "contacts", row.RecordID, map[string]any{"score": 4})
	if err != nil {
		t.Fatal(err)
	}
	if row.Values["aaa"] != int64(2) {
		t.Fatalf("expected valid formula update, got %#v", row.Values)
	}

	updatedCatalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "score", Type: "int"},
				{Name: "aaa", Type: "formula", ValueType: "int", Formula: `fields["score"] / 8`},
			},
		}},
	}}}
	if err := service.SyncTable(ctx, updatedCatalog, "db", "contacts"); err != nil {
		t.Fatal(err)
	}
	rows, err := service.Rows(ctx, updatedCatalog, perms, "u1", false, "db", "contacts", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["aaa"] != nil {
		t.Fatalf("expected formula recompute error to clear value, got %#v", rows)
	}
}

func TestInvalidTypedFieldInputClearsValueInsteadOfKeepingOldValue(t *testing.T) {
	ctx := context.Background()
	store := history.NewMemoryStore()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "12", Type: "int"},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, store, catalog)
	perms := permission.New(permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeFieldSet,
		Resource:  "db.contacts",
		Level:     permission.Write,
	}, permission.Grant{SubjectID: "u1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write})

	row, err := service.CreateRow(ctx, catalog, perms, "u1", false, "db", "contacts", map[string]any{"12": "5"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateRow(ctx, catalog, perms, "u1", false, "db", "contacts", row.RecordID, map[string]any{"12": "0.5"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Values["12"] != nil {
		t.Fatalf("expected invalid int input to clear value, got %#v", updated.Values)
	}
	entries, err := store.GetPrefix(ctx, history.RowPrefix("db", "contacts", row.RecordID))
	if err != nil {
		t.Fatal(err)
	}
	change, err := history.DecodeRowChange(entries[len(entries)-1])
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(change.Diff["12"].Old) != "5" || change.Diff["12"].New != nil {
		t.Fatalf("expected history to record value cleared, got %#v", change.Diff)
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

func (failingHistoryStore) GetPrefixLimit(context.Context, string, int) ([]history.Entry, error) {
	return nil, nil
}

func (failingHistoryStore) GetPrefixKeysLimit(context.Context, string, int) ([]string, error) {
	return nil, nil
}

func (failingHistoryStore) DeletePrefixBefore(context.Context, string, string) (int, error) {
	return 0, errors.New("history delete failed")
}

func testTableCatalog() metadata.Catalog {
	return metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name:   "contacts",
			Fields: []metadata.Field{{Name: "name", Type: "string"}},
		}},
	}}}
}

func testWritePerms() permission.Set {
	return permission.New(
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeFieldSet,
			Resource:  "db.contacts",
			Level:     permission.Write,
		},
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeRecord,
			Resource:  "db.contacts",
			Field:     "create",
			Level:     permission.Write,
		},
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeRecord,
			Resource:  "db.contacts",
			Field:     "delete",
			Level:     permission.Write,
		},
		permission.Grant{
			SubjectID: "u1",
			Scope:     permission.ScopeViewSet,
			Resource:  "db.contacts",
			Level:     permission.Write,
		},
	)
}

func newSQLiteService(t *testing.T, store history.Store, catalog metadata.Catalog) (*table.Service, metadata.Catalog, *recorddb.Repository) {
	t.Helper()
	repository, err := recorddb.OpenCatalog(context.Background(), catalog, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return table.NewServiceWithRepository(store, repository), catalog, repository
}

func TestRowsRejectsViewsUsingUnreadableFields(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "email", Type: "string"},
				{Name: "status", Type: "string"},
			},
			Views: []metadata.View{
				{
					Name: "active",
					Query: &metadata.ViewQuery{
						Combinator: "and",
						Rules:      []metadata.ViewQueryRule{{Field: "status", Operator: "=", Value: "active"}},
					},
				},
				{Name: "email-sort", Sorts: []metadata.ViewSort{{Field: "email", Direction: "asc"}}},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, history.NewMemoryStore(), catalog)
	writerPerms := permission.New(
		permission.Grant{SubjectID: "writer", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
		permission.Grant{SubjectID: "writer", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
		permission.Grant{SubjectID: "writer", Scope: permission.ScopeViewSet, Resource: "db.contacts", Level: permission.Write},
	)
	if _, err := service.CreateRow(ctx, catalog, writerPerms, "writer", false, "db", "contacts", map[string]any{
		"name":   "Ada",
		"email":  "ada@example.com",
		"status": "active",
	}); err != nil {
		t.Fatal(err)
	}
	readerPerms := permission.New(
		permission.Grant{SubjectID: "reader", Scope: permission.ScopeField, Resource: "db.contacts", Field: "email", Level: permission.Read},
		permission.Grant{SubjectID: "reader", Scope: permission.ScopeViewSet, Resource: "db.contacts", Level: permission.Read},
	)

	if _, err := service.Rows(ctx, catalog, readerPerms, "reader", false, "db", "contacts", "active"); !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected unreadable filter permission error, got %v", err)
	}
	rows, err := service.Rows(ctx, catalog, readerPerms, "reader", false, "db", "contacts", "email-sort")
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

func TestRowsWithOptionsRejectsUnreadableQueryFields(t *testing.T) {
	ctx := context.Background()
	catalog := metadata.Catalog{Databases: []metadata.Database{{
		Name: "db",
		Tables: []metadata.Table{{
			Name: "contacts",
			Fields: []metadata.Field{
				{Name: "name", Type: "string"},
				{Name: "email", Type: "string"},
				{Name: "status", Type: "string"},
			},
		}},
	}}}
	service, catalog, _ := newSQLiteService(t, history.NewMemoryStore(), catalog)
	writerPerms := permission.New(
		permission.Grant{SubjectID: "writer", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.Write},
		permission.Grant{SubjectID: "writer", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
	)
	if _, err := service.CreateRow(ctx, catalog, writerPerms, "writer", false, "db", "contacts", map[string]any{
		"name":   "Ada",
		"email":  "ada@example.com",
		"status": "active",
	}); err != nil {
		t.Fatal(err)
	}
	readerPerms := permission.New(
		permission.Grant{SubjectID: "reader", Scope: permission.ScopeField, Resource: "db.contacts", Field: "email", Level: permission.Read},
	)
	rows, err := service.RowsWithOptions(ctx, catalog, readerPerms, "reader", false, "db", "contacts", table.RowListOptions{
		Query: &metadata.ViewQuery{Combinator: "and", Rules: []metadata.ViewQueryRule{{Field: "email", Operator: "=", Value: "ada@example.com"}}},
		Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["email"] != "ada@example.com" {
		t.Fatalf("expected readable query rows, got %#v", rows)
	}
	if _, err := service.RowsWithOptions(ctx, catalog, readerPerms, "reader", false, "db", "contacts", table.RowListOptions{
		Query: &metadata.ViewQuery{Combinator: "and", Rules: []metadata.ViewQueryRule{{Field: "status", Operator: "=", Value: "active"}}},
	}); !errors.Is(err, table.ErrPermissionDenied) {
		t.Fatalf("expected unreadable query field permission error, got %v", err)
	}
}
