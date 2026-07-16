package systemdb

import (
	"context"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// createPreRunnersDatabase builds a system database with the workflow schema
// that shipped before runners_json existed, plus one saved workflow.
func createPreRunnersDatabase(t *testing.T, path string) {
	t.Helper()
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ddl := `CREATE TABLE workflow_models (
		id integer PRIMARY KEY AUTOINCREMENT,
		database_name text NOT NULL,
		name text NOT NULL,
		script text NOT NULL,
		enabled numeric NOT NULL DEFAULT true,
		creator_id text NOT NULL DEFAULT "",
		secrets_json text NOT NULL,
		variables_json text NOT NULL,
		created_at integer,
		updated_at integer
	)`
	if err := orm.Exec(ddl).Error; err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO workflow_models
		(database_name, name, script, enabled, creator_id, secrets_json, variables_json, created_at, updated_at)
		VALUES ("db", "legacy-sync", "function instances() {}", true, "u1", '{"a.token":"s"}', '{"a.channel":"ops"}', 1, 1)`
	if err := orm.Exec(insert).Error; err != nil {
		t.Fatal(err)
	}
	handle, err := orm.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateUpgradesPreRunnersDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	createPreRunnersDatabase(t, path)

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("expected existing database to open, got %v", err)
	}
	defer db.Close()

	workflows, err := db.Workflows(ctx, "db")
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 {
		t.Fatalf("expected the legacy workflow to survive, got %#v", workflows)
	}
	legacy := workflows[0]
	if legacy.Name != "legacy-sync" || legacy.Secrets["a.token"] != "s" || legacy.Variables["a.channel"] != "ops" {
		t.Fatalf("unexpected legacy workflow: %#v", legacy)
	}
	if len(legacy.Runners) != 0 {
		t.Fatalf("expected legacy workflow without runner bindings, got %#v", legacy.Runners)
	}

	legacy.Runners = map[string]string{"pull": "intranet"}
	if _, err := db.SaveWorkflow(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	reloaded, err := db.Workflow(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Runners["pull"] != "intranet" {
		t.Fatalf("expected runners to persist after upgrade, got %#v", reloaded.Runners)
	}
}

func TestMigrationsRecordOnceAndReopenCleanly(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	createPreRunnersDatabase(t, path)

	for range 2 {
		db, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		var records []schemaMigrationModel
		if err := db.orm.WithContext(ctx).Order("id").Find(&records).Error; err != nil {
			t.Fatal(err)
		}
		if len(records) != len(schemaMigrations) {
			t.Fatalf("expected %d applied migrations, got %#v", len(schemaMigrations), records)
		}
		for index, record := range records {
			if record.ID != schemaMigrations[index].id || record.Name != schemaMigrations[index].name || record.AppliedAt == 0 {
				t.Fatalf("unexpected migration record: %#v", record)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
