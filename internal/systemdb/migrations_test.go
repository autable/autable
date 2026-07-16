package systemdb

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// preRunnersWorkflowModel is the workflow model as shipped before
// runners_json existed; building old databases through GORM reproduces the
// exact table shape live deployments have.
type preRunnersWorkflowModel struct {
	ID            int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName  string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Name          string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Script        string `gorm:"not null"`
	Enabled       bool   `gorm:"not null;default:true"`
	CreatorID     string `gorm:"index;not null;default:''"`
	SecretsJSON   string `gorm:"not null"`
	VariablesJSON string `gorm:"not null"`
	CreatedAt     int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt     int64  `gorm:"autoUpdateTime:milli"`
}

func (preRunnersWorkflowModel) TableName() string { return "workflow_models" }

// legacyRoleMemberModel is the role member model before it was re-keyed.
type legacyRoleMemberModel struct {
	ID     int64 `gorm:"primaryKey;autoIncrement"`
	RoleID int64
	UserID string
}

func (legacyRoleMemberModel) TableName() string { return "role_member_models" }

// createOldDatabase builds a database with historical GORM models plus one
// saved workflow, exactly as an old release would have left it.
func createOldDatabase(t *testing.T, path string, models ...any) {
	t.Helper()
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := orm.AutoMigrate(append([]any{&userModel{}}, models...)...); err != nil {
		t.Fatal(err)
	}
	workflow := preRunnersWorkflowModel{
		DatabaseName:  "db",
		Name:          "legacy-sync",
		Script:        "function instances() {}",
		Enabled:       true,
		CreatorID:     "u1",
		SecretsJSON:   `{"a.token":"s"}`,
		VariablesJSON: `{"a.channel":"ops"}`,
	}
	if err := orm.Create(&workflow).Error; err != nil {
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

func schemaVersion(t *testing.T, db *DB) int64 {
	t.Helper()
	var record schemaVersionModel
	if err := db.orm.First(&record, schemaVersionRowID).Error; err != nil {
		t.Fatal(err)
	}
	return record.Version
}

func TestMigrateUpgradesPreRunnersDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	createOldDatabase(t, path, &preRunnersWorkflowModel{})

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("expected existing database to open, got %v", err)
	}
	defer db.Close()
	if version := schemaVersion(t, db); version != currentSchemaVersion() {
		t.Fatalf("expected schema version %d, got %d", currentSchemaVersion(), version)
	}

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

func TestMigrateUpgradesLegacyRoleMemberDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	createOldDatabase(t, path, &preRunnersWorkflowModel{}, &legacyRoleMemberModel{})

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if version := schemaVersion(t, db); version != currentSchemaVersion() {
		t.Fatalf("expected schema version %d, got %d", currentSchemaVersion(), version)
	}
	if db.orm.Migrator().HasColumn(&roleMemberModel{}, "user_id") {
		t.Fatal("expected the legacy role member table to be replaced")
	}
	if !db.orm.Migrator().HasColumn(&workflowModel{}, "runners_json") {
		t.Fatal("expected the runners column to be added")
	}
}

func TestFreshDatabaseStartsAtCurrentVersion(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	_ = ctx
	if version := schemaVersion(t, db); version != currentSchemaVersion() {
		t.Fatalf("expected fresh database at version %d, got %d", currentSchemaVersion(), version)
	}
}

func TestMigrationDriftFailsLoudly(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate drift: the version row claims 1, but the runners column
	// already exists, so the 1 → 2 migration must fail instead of being
	// silently skipped.
	if err := db.orm.Model(&schemaVersionModel{}).Where("id = ?", schemaVersionRowID).Update("version", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(ctx, path)
	if err == nil || !strings.Contains(err.Error(), "schema migration 1 → 2") {
		t.Fatalf("expected drift to fail loudly, got %v", err)
	}
}

func TestNewerDatabaseVersionIsRejected(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.orm.Model(&schemaVersionModel{}).Where("id = ?", schemaVersionRowID).Update("version", currentSchemaVersion()+5).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(ctx, path)
	if err == nil || !strings.Contains(err.Error(), "newer than this binary") {
		t.Fatalf("expected newer schema version to be rejected, got %v", err)
	}
}

func TestReopeningUpgradedDatabaseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	createOldDatabase(t, path, &preRunnersWorkflowModel{})

	for range 2 {
		db, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if version := schemaVersion(t, db); version != currentSchemaVersion() {
			t.Fatalf("expected schema version %d, got %d", currentSchemaVersion(), version)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
