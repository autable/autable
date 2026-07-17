package metadata

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestCatalogValidateRejectsReservedCTPrefix(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name: "main",
		Tables: []Table{{
			Name: "tasks",
			Fields: []Field{
				{Name: "ct_record_id", Type: "string"},
			},
		}},
	}}}

	if err := catalog.Validate(); err == nil {
		t.Fatal("expected reserved field validation error")
	}
}

func fieldNames(fields []Field) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	return names
}

func TestCatalogValidateAllowsUserRecordID(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name: "main",
		Tables: []Table{{
			Name: "tasks",
			Fields: []Field{
				{Name: "record_id", Type: "string"},
			},
		}},
	}}}

	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestActiveFieldsPreservesSoftDeletedMetadata(t *testing.T) {
	table := Table{Fields: []Field{
		{Name: "name", Type: "string"},
		{Name: "legacy", Type: "string", Deleted: true},
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
			{Name: "status", Type: "string"},
			{Name: "name", Type: "string"},
		},
		Views: []View{
			{
				Name: "active",
				Query: &ViewQuery{
					Combinator: "and",
					Rules:      []ViewQueryRule{{Field: "status", Operator: "=", Value: "active"}},
				},
				Sorts: []ViewSort{{Field: "name", Direction: "asc"}},
			},
			{
				Name:     "active-review",
				BaseView: "active",
				Query: &ViewQuery{
					Combinator: "and",
					Rules:      []ViewQueryRule{{Field: "name", Operator: "contains", Value: "Ada"}},
				},
				Sorts: []ViewSort{{Field: "ct_record_id", Direction: "desc"}},
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
	if resolved.Query == nil || resolved.Query.Combinator != "and" || len(resolved.Query.Rules) != 2 {
		t.Fatalf("expected composed query, got %#v", resolved.Query)
	}
	if len(resolved.Sorts) != 2 {
		t.Fatalf("expected composed sorts, got %#v", resolved.Sorts)
	}
}

func TestValidateRejectsViewCycles(t *testing.T) {
	table := Table{
		Name:   "contacts",
		Fields: []Field{{Name: "name", Type: "string"}},
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
	catalog, err := catalog.AddDatabase(Database{Name: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err = catalog.AddTable("workspace", Table{
		Name:        "contacts",
		DisplayName: "Contacts",
		Fields:      []Field{{Name: "name", Type: "string"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.AddTable("workspace", Table{Name: "contacts", Fields: []Field{{Name: "email", Type: "string"}}}); err == nil {
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

func TestLoadOrCreateCreatesMissingCatalogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata", "main.yml")
	catalog, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Databases) != 0 {
		t.Fatalf("expected empty catalog, got %#v", catalog)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Databases) != 0 {
		t.Fatalf("expected saved empty catalog, got %#v", loaded)
	}
}

func TestUpdateTableCanSoftDeleteFieldAndAddBasedView(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name: "workspace",
		Tables: []Table{{
			Name: "contacts",
			Fields: []Field{
				{Name: "name", Type: "string"},
				{Name: "status", Type: "string"},
				{Name: "legacy", Type: "string"},
			},
			Views: []View{{
				Name: "active",
				Query: &ViewQuery{
					Combinator: "and",
					Rules:      []ViewQueryRule{{Field: "status", Operator: "=", Value: "active"}},
				},
			}},
		}},
	}}}

	updated, err := catalog.UpdateTable("workspace", "contacts", Table{
		Name: "contacts",
		Fields: []Field{
			{Name: "name", Type: "string"},
			{Name: "status", Type: "string"},
			{Name: "legacy", Type: "string", Deleted: true},
			{Name: "email", Type: "string"},
		},
		Views: []View{
			{
				Name: "active",
				Query: &ViewQuery{
					Combinator: "and",
					Rules:      []ViewQueryRule{{Field: "status", Operator: "=", Value: "active"}},
				},
			},
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
	if resolved.Query == nil || len(resolved.Query.Rules) != 1 || len(resolved.Sorts) != 1 {
		t.Fatalf("expected based view composition, got %#v", resolved)
	}
}

func TestUpdateTableRejectsFieldTypeChange(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name: "workspace",
		Tables: []Table{{
			Name:   "contacts",
			Fields: []Field{{Name: "priority", Type: "string"}},
		}},
	}}}

	if _, err := catalog.UpdateTable("workspace", "contacts", Table{
		Name:   "contacts",
		Fields: []Field{{Name: "priority", Type: "float"}},
	}); err == nil {
		t.Fatal("expected field type change to be rejected")
	}

	updated, err := catalog.UpdateTable("workspace", "contacts", Table{
		Name: "contacts",
		Fields: []Field{
			{Name: "priority", Type: "string"},
			{Name: "status", Type: "string"},
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

func TestMoveFieldPreservesUnmentionedFields(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name: "workspace",
		Tables: []Table{{
			Name: "contacts",
			Fields: []Field{
				{Name: "name", Type: "string"},
				{Name: "hidden", Type: "string"},
				{Name: "email", Type: "string"},
				{Name: "status", Type: "string"},
			},
		}},
	}}}

	updated, err := catalog.MoveFieldBefore("workspace", "contacts", "status", "name")
	if err != nil {
		t.Fatal(err)
	}
	table, ok := updated.Table("workspace", "contacts")
	if !ok {
		t.Fatal("expected contacts table")
	}
	if got := fieldNames(table.Fields); !slices.Equal(got, []string{"status", "name", "hidden", "email"}) {
		t.Fatalf("unexpected field order: %#v", got)
	}

	updated, err = updated.MoveFieldAfter("workspace", "contacts", "status", "email")
	if err != nil {
		t.Fatal(err)
	}
	table, _ = updated.Table("workspace", "contacts")
	if got := fieldNames(table.Fields); !slices.Equal(got, []string{"name", "hidden", "email", "status"}) {
		t.Fatalf("unexpected field order: %#v", got)
	}

	updated, err = updated.MoveFieldToStart("workspace", "contacts", "email")
	if err != nil {
		t.Fatal(err)
	}
	table, _ = updated.Table("workspace", "contacts")
	if got := fieldNames(table.Fields); !slices.Equal(got, []string{"email", "name", "hidden", "status"}) {
		t.Fatalf("unexpected field order: %#v", got)
	}
}

func TestFormulaFieldValidationAndEditableExpression(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name: "workspace",
		Tables: []Table{{
			Name: "contacts",
			Fields: []Field{
				{Name: "score", Type: "float"},
				{Name: "score_plus_one", Type: "formula", ValueType: "float", Formula: `fields["score"] + 1`},
			},
		}},
	}}}

	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	updated, err := catalog.UpdateTable("workspace", "contacts", Table{
		Name: "contacts",
		Fields: []Field{
			{Name: "score", Type: "float"},
			{Name: "score_plus_one", Type: "formula", ValueType: "float", Formula: `fields["score"] + 2`},
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
	if !ok || field.Formula != `fields["score"] + 2` {
		t.Fatalf("expected formula expression update, got %#v", field)
	}
	if _, err := catalog.UpdateTable("workspace", "contacts", Table{
		Name: "contacts",
		Fields: []Field{
			{Name: "score", Type: "float"},
			{Name: "score_plus_one", Type: "formula", ValueType: "string", Formula: `String(fields["score"] + 2)`},
		},
	}); err == nil {
		t.Fatal("expected formula value_type change to be rejected")
	}

	invalid := Catalog{Databases: []Database{{
		Name: "workspace",
		Tables: []Table{{
			Name:   "contacts",
			Fields: []Field{{Name: "bad_formula", Type: "formula"}},
		}},
	}}}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected empty formula validation error")
	}

	invalid = Catalog{Databases: []Database{{
		Name: "workspace",
		Tables: []Table{{
			Name:   "contacts",
			Fields: []Field{{Name: "name", Type: "string", Formula: `fields["score"] + 1`}},
		}},
	}}}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected formula on non-formula field validation error")
	}
}

func TestFileFieldValidationAndStorageType(t *testing.T) {
	catalog := Catalog{Databases: []Database{{
		Name: "workspace",
		Tables: []Table{{
			Name: "contacts",
			Fields: []Field{
				{Name: "name", Type: "string"},
				{Name: "attachment", Type: "file"},
			},
		}},
	}}}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	if storage := (Field{Type: "file"}).StorageType(); storage != "int" {
		t.Fatalf("expected file fields to store the file id as int, got %q", storage)
	}

	invalid := Catalog{Databases: []Database{{
		Name: "workspace",
		Tables: []Table{{
			Name: "contacts",
			Fields: []Field{
				{Name: "attachment", Type: "file", RelationTable: "other"},
			},
		}},
	}}}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected relation_table on a file field to be rejected")
	}
}
