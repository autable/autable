package metadata

import "testing"

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
