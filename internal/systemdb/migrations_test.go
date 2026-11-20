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

// nodeWorkflowModel is the node-based workflow model as shipped from schema
// version 1 until workflows v2 replaced it (schema version 8).
type nodeWorkflowModel struct {
	ID                   int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName         string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Name                 string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Script               string `gorm:"not null"`
	Enabled              bool   `gorm:"not null;default:true"`
	CreatorID            string `gorm:"index;not null;default:''"`
	SecretsJSON          string `gorm:"not null"`
	VariablesJSON        string `gorm:"not null"`
	RunnersJSON          string `gorm:"not null;default:'{}'"`
	HistoryRetentionDays *int64
	TimeoutSeconds       *int64
	CreatedAt            int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt            int64 `gorm:"autoUpdateTime:milli"`
}

func (nodeWorkflowModel) TableName() string { return "workflow_models" }

// migrateTo runs the schema migrations up to version on a raw database,
// without the AutoMigrate of the current models that Open performs.
func migrateTo(t *testing.T, orm *gorm.DB, version int) {
	t.Helper()
	previous := migrations
	migrations = migrations[:version]
	defer func() { migrations = previous }()
	if err := (&DB{orm: orm}).runSchemaMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// databaseOwnerModel is the database ownership table as shipped from the
// 0.1.18 release until autable became admin-gated (schema version 7).
type databaseOwnerModel struct {
	DatabaseName string `gorm:"primaryKey;not null"`
	OwnerID      string `gorm:"primaryKey;not null"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli"`
}

// createOldDatabase builds a database with historical GORM models plus one
// saved workflow, exactly as the 0.1.18 release would have left it.
func createOldDatabase(t *testing.T, path string, models ...any) {
	t.Helper()
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := orm.AutoMigrate(append([]any{&userModel{}, &permissionGrantModel{}, &databaseOwnerModel{}, &roleModel{}, &roleMemberModel{}}, models...)...); err != nil {
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

	// 0 → 1 adds runners_json to the populated table.
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	migrateTo(t, orm, 1)
	var legacy nodeWorkflowModel
	if err := orm.First(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if legacy.Name != "legacy-sync" || legacy.SecretsJSON != `{"a.token":"s"}` || legacy.VariablesJSON != `{"a.channel":"ops"}` || legacy.RunnersJSON != "{}" {
		t.Fatalf("unexpected legacy workflow after 0 → 1: %#v", legacy)
	}
	handle, err := orm.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}

	// The full upgrade then replaces node workflows (7 → 8).
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("expected existing database to open, got %v", err)
	}
	defer db.Close()
	if version := schemaVersion(t, db); version != currentSchemaVersion() {
		t.Fatalf("expected schema version %d, got %d", currentSchemaVersion(), version)
	}
	workflows, err := db.Workflows(ctx, "db")
	if err != nil || len(workflows) != 0 {
		t.Fatalf("expected node workflows to be gone, got %#v, %v", workflows, err)
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
	// Simulate drift: the version row claims 6, but the database owner
	// table that version 6 always has does not exist, so the 6 → 7
	// migration must fail instead of being silently skipped.
	if err := db.orm.Model(&schemaVersionModel{}).Where("id = ?", schemaVersionRowID).Update("version", 6).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(ctx, path)
	if err == nil || !strings.Contains(err.Error(), "schema migration 6 → 7") {
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
	if levels["view|active"] != permission.Read {
		t.Fatalf("expected view level collapsed to read, got %#v", levels)
	}
	// 3 → 4 seeded field_add for full field_set holders; 6 → 7 removed the
	// scope again when structure became admin-only.
	if _, ok := levels["field_add|"]; ok {
		t.Fatalf("expected no field_add grant after the full upgrade, got %#v", levels)
	}
}

// formModel is the form table as shipped before apps replaced forms.
type formModel struct {
	ID             int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName   string `gorm:"uniqueIndex:idx_form_database_name;not null"`
	Name           string `gorm:"uniqueIndex:idx_form_database_name;not null"`
	Script         string `gorm:"not null"`
	PublishedToken string `gorm:"index"`
	CreatorID      string `gorm:"index;not null;default:''"`
	CreatedAt      int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt      int64  `gorm:"autoUpdateTime:milli"`
}

func TestMigrateDropsFormsAndTheirGrants(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")

	// A version-5 database: the current models minus apps, plus forms.
	previous := migrations
	migrations = migrations[:5]
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db := &DB{orm: orm}
	if err := db.runSchemaMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	migrations = previous
	if err := orm.AutoMigrate(&userModel{}, &permissionGrantModel{}, &databaseOwnerModel{}, &nodeWorkflowModel{}, &roleModel{}, &roleMemberModel{}, &formModel{}); err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&formModel{DatabaseName: "db", Name: "intake", Script: "function render() {}", CreatorID: "u1"}).Error; err != nil {
		t.Fatal(err)
	}
	grants := []permissionGrantModel{
		{SubjectID: "u1", Scope: "form", Resource: "1", Level: permission.Write},
		{SubjectID: "u2", Scope: "form_set", Resource: "db", Level: permission.Read},
		{SubjectID: "u2", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldRead},
	}
	if err := orm.Create(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("expected version-5 database to open, got %v", err)
	}
	defer upgraded.Close()
	if got := schemaVersion(t, upgraded); got != currentSchemaVersion() {
		t.Fatalf("expected schema version %d, got %d", currentSchemaVersion(), got)
	}
	if upgraded.orm.Migrator().HasTable("form_models") {
		t.Fatal("expected form_models to be dropped")
	}
	var remaining []permissionGrantModel
	if err := upgraded.orm.Order("id").Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].Scope != permission.ScopeFieldSet {
		t.Fatalf("expected only the field set grant to survive, got %#v", remaining)
	}
	if _, err := upgraded.CreateApp(ctx, AppDefinition{DatabaseName: "db", Name: "intake"}); err != nil {
		t.Fatalf("expected apps table after upgrade, got %v", err)
	}
}

func TestMigrateRemovesOwnershipAndStructureGrants(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")

	// A version-6 database: the version-6 models, including database owners.
	previous := migrations
	migrations = migrations[:6]
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db := &DB{orm: orm}
	if err := db.runSchemaMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	migrations = previous
	if err := orm.AutoMigrate(
		&userModel{},
		&sessionModel{},
		&permissionGrantModel{},
		&databaseOwnerModel{},
		&nodeWorkflowModel{},
		&appModel{},
		&appVersionModel{},
		&roleModel{},
		&roleMemberModel{},
		&runnerTokenModel{},
		&fileModel{},
	); err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&userModel{ID: "u1", Email: "u1@example.com", DisplayName: "u1", Provider: "password", ProviderName: "password", Subject: "u1@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&[]databaseOwnerModel{{DatabaseName: "db", OwnerID: "u1"}, {DatabaseName: "other", OwnerID: "u2"}}).Error; err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&nodeWorkflowModel{DatabaseName: "db", Name: "sync", Script: "function instances() {}", Enabled: true, CreatorID: "u1", SecretsJSON: "{}", VariablesJSON: "{}", RunnersJSON: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&appModel{DatabaseName: "db", Name: "intake", Title: "Intake", CreatorID: "u1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&roleModel{DatabaseName: "db", Name: "sync"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&roleMemberModel{DatabaseName: "db", RoleName: "sync", SubjectType: "workflow", SubjectID: "workflow:1"}).Error; err != nil {
		t.Fatal(err)
	}
	grants := []permissionGrantModel{
		// Structure grants that no longer exist.
		{SubjectID: "u2", Scope: "field_add", Resource: "db.contacts", Level: permission.Write},
		{SubjectID: "u2", Scope: "field_modify", Resource: "db.contacts", Field: "email", Level: permission.Write},
		{SubjectID: "u2", Scope: "workflow_set", Resource: "db", Level: permission.Read},
		{SubjectID: "workflow:1", Scope: "field_add", Resource: "db.contacts", Level: permission.Write},
		// Old workflow grants meant view or edit, not run.
		{SubjectID: "u2", Scope: permission.ScopeWorkflow, Resource: "1", Level: permission.Write},
		{SubjectID: "u3", Scope: permission.ScopeWorkflow, Resource: "1", Level: permission.Read},
		// App grants keep only their open level.
		{SubjectID: "u2", Scope: permission.ScopeApp, Resource: "1", Level: permission.Write},
		{SubjectID: "u3", Scope: permission.ScopeApp, Resource: "1", Level: permission.Read},
		{SubjectID: "u4", Scope: permission.ScopeAppSet, Resource: "db", Level: permission.Write},
		// Data grants survive untouched, including workflow and role subjects.
		{SubjectID: "u2", Scope: permission.ScopeField, Resource: "db.contacts", Field: "email", Level: permission.FieldRead | permission.FieldUpdate},
		{SubjectID: "u2", Scope: permission.ScopeView, Resource: "db.contacts", Field: "all", Level: permission.Read},
		{SubjectID: "workflow:1", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll},
		{SubjectID: "workflow:1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
		{SubjectID: "workflow:1", Scope: permission.ScopeViewSet, Resource: "db.contacts", Level: permission.Read},
		{SubjectID: "workflow:1", Scope: permission.ScopeFile, Resource: "db.contacts", Level: permission.Read},
		{SubjectID: "role:db:sync", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "delete", Level: permission.Write},
	}
	if err := orm.Create(&grants).Error; err != nil {
		t.Fatal(err)
	}
	// Only 6 → 7 is under test here; 7 → 8 has its own test.
	migrateTo(t, orm, 7)
	upgraded := db
	defer upgraded.Close()
	if got := schemaVersion(t, upgraded); got != 7 {
		t.Fatalf("expected schema version 7, got %d", got)
	}
	if upgraded.orm.Migrator().HasTable("database_owner_models") {
		t.Fatal("expected database_owner_models to be dropped")
	}

	var remaining []permissionGrantModel
	if err := upgraded.orm.Order("id").Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	got := map[string]permission.Level{}
	for _, grant := range remaining {
		got[grant.SubjectID+"|"+string(grant.Scope)+"|"+grant.Resource+"|"+grant.Field] = grant.Level
	}
	want := map[string]permission.Level{
		"u2|app|1|":                              permission.Open,
		"u3|app|1|":                              permission.Open,
		"u4|app_set|db|":                         permission.Open,
		"u2|field|db.contacts|email":             permission.FieldRead | permission.FieldUpdate,
		"u2|view|db.contacts|all":                permission.Read,
		"workflow:1|field_set|db.contacts|":      permission.FieldAll,
		"workflow:1|record|db.contacts|create":   permission.Write,
		"workflow:1|view_set|db.contacts|":       permission.Read,
		"workflow:1|file|db.contacts|":           permission.Read,
		"role:db:sync|record|db.contacts|delete": permission.Write,
	}
	if len(got) != len(want) {
		t.Fatalf("expected grants %#v, got %#v", want, got)
	}
	for key, level := range want {
		if got[key] != level {
			t.Fatalf("expected %s at level %d, got %#v", key, level, got)
		}
	}

	// Everything else is intact.
	user, err := upgraded.User(ctx, "u1")
	if err != nil || user.Email != "u1@example.com" {
		t.Fatalf("expected user to survive, got %#v, %v", user, err)
	}
	var workflows []nodeWorkflowModel
	if err := upgraded.orm.Find(&workflows).Error; err != nil || len(workflows) != 1 || workflows[0].Name != "sync" {
		t.Fatalf("expected workflow to survive, got %#v, %v", workflows, err)
	}
	app, err := upgraded.AppByName(ctx, "db", "intake")
	if err != nil || app.Title != "Intake" {
		t.Fatalf("expected app to survive, got %#v, %v", app, err)
	}
	effective, err := upgraded.EffectiveGrantsForSubject(ctx, "workflow:1")
	if err != nil {
		t.Fatal(err)
	}
	if !effective.CanCreateRecord("workflow:1", "db.contacts") || !effective.CanDeleteRecord("workflow:1", "db.contacts") {
		t.Fatal("expected the workflow subject to keep its direct and role data grants")
	}
}

func TestMigrateReplacesNodeWorkflows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")

	// A version-7 database: the version-7 models, with node workflows.
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	migrateTo(t, orm, 7)
	if err := orm.AutoMigrate(
		&userModel{},
		&sessionModel{},
		&permissionGrantModel{},
		&nodeWorkflowModel{},
		&appModel{},
		&appVersionModel{},
		&roleModel{},
		&roleMemberModel{},
		&runnerTokenModel{},
		&fileModel{},
	); err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&userModel{ID: "u1", Email: "u1@example.com", DisplayName: "u1", Provider: "password", ProviderName: "password", Subject: "u1@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	retention := int64(7)
	if err := orm.Create(&[]nodeWorkflowModel{
		{DatabaseName: "db", Name: "sync", Script: "function instances() {}", Enabled: true, CreatorID: "u1", SecretsJSON: `{"a.token":"s"}`, VariablesJSON: "{}", RunnersJSON: `{"a":"intranet"}`, HistoryRetentionDays: &retention},
		{DatabaseName: "db", Name: "notify", Script: "function instances() {}", Enabled: false, CreatorID: "u1", SecretsJSON: "{}", VariablesJSON: "{}", RunnersJSON: "{}"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&appModel{DatabaseName: "db", Name: "intake", Title: "Intake", CreatorID: "u1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&roleModel{DatabaseName: "db", Name: "sync"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&[]roleMemberModel{
		{DatabaseName: "db", RoleName: "sync", SubjectType: "workflow", SubjectID: "workflow:1"},
		{DatabaseName: "db", RoleName: "sync", SubjectType: "user", SubjectID: "u1"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := orm.Create(&runnerTokenModel{DatabaseName: "db", TokenHash: "hash"}).Error; err != nil {
		t.Fatal(err)
	}
	grants := []permissionGrantModel{
		// Grants held by old workflow subjects would pass to new ids.
		{SubjectID: "workflow:1", Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll},
		{SubjectID: "workflow:1", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "create", Level: permission.Write},
		{SubjectID: "workflow:2", Scope: permission.ScopeView, Resource: "db.contacts", Field: "all", Level: permission.Read},
		// Run grants name old ids.
		{SubjectID: "u1", Scope: permission.ScopeWorkflow, Resource: "1", Level: permission.Run},
		{SubjectID: "role:db:sync", Scope: permission.ScopeWorkflow, Resource: "2", Level: permission.Run},
		// Everything else stays.
		{SubjectID: "u1", Scope: permission.ScopeField, Resource: "db.contacts", Field: "email", Level: permission.FieldRead},
		{SubjectID: "role:db:sync", Scope: permission.ScopeRecord, Resource: "db.contacts", Field: "delete", Level: permission.Write},
		{SubjectID: "u1", Scope: permission.ScopeApp, Resource: "1", Level: permission.Open},
	}
	if err := orm.Create(&grants).Error; err != nil {
		t.Fatal(err)
	}
	handle, err := orm.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("expected version-7 database to open, got %v", err)
	}
	defer upgraded.Close()
	if got := schemaVersion(t, upgraded); got != 8 || currentSchemaVersion() < 8 {
		t.Fatalf("expected schema version 8, got %d", got)
	}
	migrator := upgraded.orm.Migrator()
	if migrator.HasColumn(&workflowModel{}, "script") || migrator.HasColumn(&workflowModel{}, "secrets_json") {
		t.Fatal("expected the node workflow table to be replaced")
	}
	for _, model := range []any{&workflowVersionModel{}, &workflowSecretModel{}, &workflowVarModel{}, &workflowRunModel{}, &workflowLogModel{}} {
		if !migrator.HasTable(model) {
			t.Fatalf("expected %T to exist", model)
		}
	}
	if workflows, err := upgraded.Workflows(ctx, "db"); err != nil || len(workflows) != 0 {
		t.Fatalf("expected no workflows after the upgrade, got %#v, %v", workflows, err)
	}

	var remaining []permissionGrantModel
	if err := upgraded.orm.Order("id").Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	got := map[string]permission.Level{}
	for _, grant := range remaining {
		got[grant.SubjectID+"|"+string(grant.Scope)+"|"+grant.Resource+"|"+grant.Field] = grant.Level
	}
	want := map[string]permission.Level{
		"u1|field|db.contacts|email":             permission.FieldRead,
		"role:db:sync|record|db.contacts|delete": permission.Write,
		"u1|app|1|":                              permission.Open,
	}
	if len(got) != len(want) {
		t.Fatalf("expected grants %#v, got %#v", want, got)
	}
	for key, level := range want {
		if got[key] != level {
			t.Fatalf("expected %s at level %d, got %#v", key, level, got)
		}
	}
	role, err := upgraded.Role(ctx, "db", "sync")
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Members) != 1 || role.Members[0] != (RoleMember{Type: "user", ID: "u1"}) {
		t.Fatalf("expected only the user member to remain, got %#v", role.Members)
	}

	// A new workflow gets id 1 again and must not inherit anything.
	created, err := upgraded.CreateWorkflow(ctx, Workflow{DatabaseName: "db", Name: "sync"})
	if err != nil {
		t.Fatal(err)
	}
	perms, err := upgraded.EffectiveGrantsForSubject(ctx, WorkflowSubjectID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if perms.CanCreateRecord(WorkflowSubjectID(created.ID), "db.contacts") || perms.CanReadField(WorkflowSubjectID(created.ID), "db.contacts", "email") {
		t.Fatalf("expected the new workflow %d to start without grants", created.ID)
	}
	if !created.Enabled || created.TimeoutSeconds != DefaultWorkflowTimeoutSeconds || created.HistoryRetentionDays != DefaultWorkflowRetentionDays {
		t.Fatalf("unexpected defaults on the new workflow: %#v", created)
	}
	// Unrelated data is intact.
	if user, err := upgraded.User(ctx, "u1"); err != nil || user.Email != "u1@example.com" {
		t.Fatalf("expected user to survive, got %#v, %v", user, err)
	}
	if app, err := upgraded.AppByName(ctx, "db", "intake"); err != nil || app.Title != "Intake" {
		t.Fatalf("expected app to survive, got %#v, %v", app, err)
	}
	if _, ok, err := upgraded.RunnerTokenCreatedAt(ctx, "db"); err != nil || !ok {
		t.Fatalf("expected the runner token to survive, got %v %v", ok, err)
	}
}
