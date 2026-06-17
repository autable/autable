package metadata

import (
	"path/filepath"
	"testing"
)

func TestCatalogValidateRejectsUserRecordID(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name:       "main",
		SQLitePath: "./main.sqlite",
		Tables: []Table{{
			Name: "tasks",
			Fields: []Field{
				{Name: "record_id", Type: "text"},
			},
		}},
	}}}

	if err := catalog.Validate(); err == nil {
		t.Fatal("expected reserved field validation error")
	}
}

func TestActiveFieldsPreservesSoftDeletedMetadata(t *testing.T) {
	table := Table{Fields: []Field{
		{Name: "name", Type: "text"},
		{Name: "legacy", Type: "text", Deleted: true},
	}}

	active := table.ActiveFields()
	if len(active) != 1 || active[0].Name != "name" {
		t.Fatalf("unexpected active fields: %#v", active)
	}
	if _, ok := table.Field("legacy"); !ok {
		t.Fatal("soft-deleted field should remain addressable in metadata")
	}
}

func TestResolveViewComposesBaseView(t *testing.T) {
	table := Table{
		Name: "contacts",
		Fields: []Field{
			{Name: "status", Type: "text"},
			{Name: "name", Type: "text"},
		},
		Views: []View{
			{
				Name:    "active",
				Filters: []ViewFilter{{Field: "status", Op: "eq", Value: "active"}},
				Sorts:   []ViewSort{{Field: "name", Direction: "asc"}},
			},
			{
				Name:     "active-review",
				BaseView: "active",
				Filters:  []ViewFilter{{Field: "name", Op: "contains", Value: "Ada"}},
				Sorts:    []ViewSort{{Field: "record_id", Direction: "desc"}},
			},
		},
	}

	if err := table.validate("db", 0); err != nil {
		t.Fatal(err)
	}
	resolved, err := table.ResolveView("active-review")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Filters) != 2 {
		t.Fatalf("expected composed filters, got %#v", resolved.Filters)
	}
	if len(resolved.Sorts) != 2 {
		t.Fatalf("expected composed sorts, got %#v", resolved.Sorts)
	}
}

func TestValidateRejectsViewCycles(t *testing.T) {
	table := Table{
		Name:   "contacts",
		Fields: []Field{{Name: "name", Type: "text"}},
		Views: []View{
			{Name: "a", BaseView: "b"},
			{Name: "b", BaseView: "a"},
		},
	}

	if err := table.validate("db", 0); err == nil {
		t.Fatal("expected view cycle validation error")
	}
}

func TestAddDatabaseAddTableAndSave(t *testing.T) {
	catalog := Catalog{}
	catalog, err := catalog.AddDatabase(Database{Name: "workspace", SQLitePath: "./data/workspace.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err = catalog.AddTable("workspace", Table{
		Name:        "contacts",
		DisplayName: "Contacts",
		Fields:      []Field{{Name: "name", Type: "text"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.AddTable("workspace", Table{Name: "contacts", Fields: []Field{{Name: "email", Type: "email"}}}); err == nil {
		t.Fatal("expected duplicate table validation error")
	}

	path := filepath.Join(t.TempDir(), "metadata", "main.yml")
	if err := Save(path, catalog); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	table, ok := loaded.Table("workspace", "contacts")
	if !ok || table.Fields[0].Name != "name" {
		t.Fatalf("unexpected loaded catalog: %#v", loaded)
	}
}

func TestUpdateTableCanSoftDeleteFieldAndAddBasedView(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name:       "workspace",
		SQLitePath: "./data/workspace.sqlite",
		Tables: []Table{{
			Name: "contacts",
			Fields: []Field{
				{Name: "name", Type: "text"},
				{Name: "status", Type: "text"},
				{Name: "legacy", Type: "text"},
			},
			Views: []View{{
				Name:    "active",
				Filters: []ViewFilter{{Field: "status", Op: "eq", Value: "active"}},
			}},
		}},
	}}}

	updated, err := catalog.UpdateTable("workspace", "contacts", Table{
		Name: "contacts",
		Fields: []Field{
			{Name: "name", Type: "text"},
			{Name: "status", Type: "text"},
			{Name: "legacy", Type: "text", Deleted: true},
			{Name: "email", Type: "email"},
		},
		Views: []View{
			{Name: "active", Filters: []ViewFilter{{Field: "status", Op: "eq", Value: "active"}}},
			{Name: "active-by-name", BaseView: "active", Sorts: []ViewSort{{Field: "name", Direction: "asc"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	table, ok := updated.Table("workspace", "contacts")
	if !ok {
		t.Fatal("expected updated table")
	}
	if _, ok := table.Field("email"); !ok {
		t.Fatalf("expected added email field, got %#v", table.Fields)
	}
	activeFields := table.ActiveFields()
	if len(activeFields) != 3 {
		t.Fatalf("expected legacy to be soft-deleted, got %#v", activeFields)
	}
	resolved, err := table.ResolveView("active-by-name")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Filters) != 1 || len(resolved.Sorts) != 1 {
		t.Fatalf("expected based view composition, got %#v", resolved)
	}
}

func TestUpdateTableRejectsFieldTypeChange(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name:       "workspace",
		SQLitePath: "./data/workspace.sqlite",
		Tables: []Table{{
			Name:   "contacts",
			Fields: []Field{{Name: "priority", Type: "text"}},
		}},
	}}}

	if _, err := catalog.UpdateTable("workspace", "contacts", Table{
		Name:   "contacts",
		Fields: []Field{{Name: "priority", Type: "number"}},
	}); err == nil {
		t.Fatal("expected field type change to be rejected")
	}

	updated, err := catalog.UpdateTable("workspace", "contacts", Table{
		Name: "contacts",
		Fields: []Field{
			{Name: "priority", Type: "text"},
			{Name: "status", Type: "text"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	table, ok := updated.Table("workspace", "contacts")
	if !ok || len(table.Fields) != 2 {
		t.Fatalf("expected adding a new field to be allowed, got %#v", table)
	}
}

func TestFormulaFieldValidationAndEditableExpression(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name:       "workspace",
		SQLitePath: "./data/workspace.sqlite",
		Tables: []Table{{
			Name: "contacts",
			Fields: []Field{
				{Name: "score", Type: "number"},
				{Name: "score_plus_one", Type: "formula", Formula: "field_score + 1"},
			},
		}},
	}}}

	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	updated, err := catalog.UpdateTable("workspace", "contacts", Table{
		Name: "contacts",
		Fields: []Field{
			{Name: "score", Type: "number"},
			{Name: "score_plus_one", Type: "formula", Formula: "field_score + 2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	table, ok := updated.Table("workspace", "contacts")
	if !ok {
		t.Fatal("expected updated table")
	}
	field, ok := table.Field("score_plus_one")
	if !ok || field.Formula != "field_score + 2" {
		t.Fatalf("expected formula expression update, got %#v", field)
	}

	invalid := Catalog{Databases: []Database{{
		Name:       "workspace",
		SQLitePath: "./data/workspace.sqlite",
		Tables: []Table{{
			Name:   "contacts",
			Fields: []Field{{Name: "bad_formula", Type: "formula"}},
		}},
	}}}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected empty formula validation error")
	}

	invalid = Catalog{Databases: []Database{{
		Name:       "workspace",
		SQLitePath: "./data/workspace.sqlite",
		Tables: []Table{{
			Name:   "contacts",
			Fields: []Field{{Name: "name", Type: "text", Formula: "field_score + 1"}},
		}},
	}}}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected formula on non-formula field validation error")
	}
}
