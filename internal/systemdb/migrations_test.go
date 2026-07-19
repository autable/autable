package systemdb

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"autable/internal/permission"

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

// createOldDatabase builds a database with historical GORM models plus one
// saved workflow, exactly as the 0.1.18 release would have left it.
func createOldDatabase(t *testing.T, path string, models ...any) {
	t.Helper()
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := orm.AutoMigrate(append([]any{&userModel{}, &permissionGrantModel{}}, models...)...); err != nil {
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

// interimRunnerTokenModel is the single-global-token table shape created by
// interim builds before runner tokens became database-scoped.
type interimRunnerTokenModel struct {
	ID        int64  `gorm:"primaryKey"`
	TokenHash string `gorm:"not null"`
	CreatedAt int64  `gorm:"autoCreateTime:milli"`
}

func (interimRunnerTokenModel) TableName() string { return "runner_token_models" }

func TestMigrateReplacesInterimRunnerTokenTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	createOldDatabase(t, path, &preRunnersWorkflowModel{}, &interimRunnerTokenModel{})

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("expected interim database to open, got %v", err)
	}
	defer db.Close()
	if version := schemaVersion(t, db); version != currentSchemaVersion() {
		t.Fatalf("expected schema version %d, got %d", currentSchemaVersion(), version)
	}

	token, err := db.ResetRunnerToken(ctx, "db")
	if err != nil {
		t.Fatal(err)
	}
	if dbName, ok, err := db.LookupRunnerToken(ctx, token); err != nil || !ok || dbName != "db" {
		t.Fatalf("expected rebuilt token table to work, got %q ok=%v err=%v", dbName, ok, err)
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
	// Simulate drift: the version row claims 0, but the runners column
	// already exists, so the 0 → 1 migration must fail instead of being
	// silently skipped.
	if err := db.orm.Model(&schemaVersionModel{}).Where("id = ?", schemaVersionRowID).Update("version", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(ctx, path)
	if err == nil || !strings.Contains(err.Error(), "schema migration 0 → 1") {
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

func TestMigrateRewritesLegacyFieldGrantLevels(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	createOldDatabase(t, path, &preRunnersWorkflowModel{})

	// Seed pre-bitmask grants: level 2 on field scopes meant full write.
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	legacyGrants := []permissionGrantModel{
		{SubjectID: "u1", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Field: "", Level: 2},
		{SubjectID: "u1", Scope: permission.ScopeField, Resource: "db.contacts", Field: "email", Level: 2},
		{SubjectID: "u1", Scope: permission.ScopeField, Resource: "db.contacts", Field: "name", Level: 1},
		{SubjectID: "u1", Scope: permission.ScopeView, Resource: "db.contacts", Field: "active", Level: 2},
	}
	if err := orm.Create(&legacyGrants).Error; err != nil {
		t.Fatal(err)
	}
	handle, err := orm.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("expected legacy database to open, got %v", err)
	}
	defer db.Close()

	grants, err := db.GrantListForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	levels := map[string]permission.Level{}
	for _, grant := range grants {
		levels[string(grant.Scope)+"|"+grant.Field] = grant.Level
	}
	if levels["field_set|"] != permission.FieldAll {
		t.Fatalf("expected field_set level 7, got %#v", levels)
	}
	if levels["field|email"] != permission.FieldAll {
		t.Fatalf("expected field write level 7, got %#v", levels)
	}
	if levels["field|name"] != permission.FieldRead {
		t.Fatalf("expected field read level unchanged, got %#v", levels)
	}
	if levels["view|active"] != permission.Write {
		t.Fatalf("expected view level untouched, got %#v", levels)
	}
	if levels["field_add|"] != permission.Write {
		t.Fatalf("expected seeded field_add grant for full field_set holders, got %#v", levels)
	}
}
