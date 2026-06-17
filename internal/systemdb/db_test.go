package systemdb

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"codetable/internal/auth"
	"codetable/internal/permission"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type oldWorkflowTimestampModel struct {
	ID            int64 `gorm:"primaryKey;autoIncrement"`
	DatabaseName  string
	Name          string
	Script        string
	CreatorID     string
	SecretsJSON   string
	VariablesJSON string
	CreatedAt     string `gorm:"type:integer"`
	UpdatedAt     string `gorm:"type:integer"`
}

func (oldWorkflowTimestampModel) TableName() string {
	return "workflow_models"
}

func TestUserUpsertUsesEmailFallback(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	passwordUser, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:    "person@example.com",
		Password: "correct horse",
	})
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := db.UpsertUserByEmail(ctx, passwordUser)
	if err != nil {
		t.Fatal(err)
	}

	oidcUser, err := auth.NewOIDCUser(auth.OIDCIdentity{
		ProviderName: "main",
		Subject:      "sub-123",
		Email:        "PERSON@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	upserted, err := db.UpsertUserByEmail(ctx, oidcUser)
	if err != nil {
		t.Fatal(err)
	}
	if upserted.ID != inserted.ID {
		t.Fatalf("expected email fallback to keep existing user id %q, got %q", inserted.ID, upserted.ID)
	}

	loaded, err := db.UserByEmail(ctx, "person@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Provider != auth.ProviderOIDC || loaded.Subject != "sub-123" {
		t.Fatalf("unexpected loaded user: %#v", loaded)
	}
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "system.sqlite")

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestOpenDropsIncompatibleTimestampTables(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "system.sqlite")
	raw, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.WithContext(ctx).AutoMigrate(&oldWorkflowTimestampModel{}); err != nil {
		t.Fatal(err)
	}
	if err := raw.WithContext(ctx).Create(&oldWorkflowTimestampModel{
		DatabaseName:  "workspace",
		Name:          "legacy",
		Script:        "function run() {}",
		SecretsJSON:   "{}",
		VariablesJSON: "{}",
		CreatedAt:     "2026-06-17 08:25:13.733763+08:00",
		UpdatedAt:     "2026-06-17 08:25:13.733763+08:00",
	}).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := raw.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	})
	workflows, err := db.Workflows(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 0 {
		t.Fatalf("expected incompatible workflow table to be dropped, got %#v", workflows)
	}
	saved, err := db.SaveWorkflow(ctx, WorkflowDefinition{
		DatabaseName: "workspace",
		Name:         "current",
		Script:       "function run() {}",
		Secrets:      map[string]string{},
		Variables:    map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.CreatedAt <= 0 || saved.UpdatedAt <= 0 {
		t.Fatalf("expected millisecond workflow timestamps after schema rebuild, got %#v", saved)
	}
}

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	user, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:    "person@example.com",
		Password: "correct horse",
	})
	if err != nil {
		t.Fatal(err)
	}
	user, err = db.UpsertUserByEmail(ctx, user)
	if err != nil {
		t.Fatal(err)
	}

	session, err := db.CreateSession(ctx, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if session.Token == "" {
		t.Fatal("expected raw session token")
	}
	loaded, loadedSession, err := db.UserBySessionToken(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != user.ID || loadedSession.UserID != user.ID {
		t.Fatalf("unexpected session user: %#v %#v", loaded, loadedSession)
	}
	if err := db.DeleteSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.UserBySessionToken(ctx, session.Token); err == nil {
		t.Fatal("expected deleted session to be unavailable")
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	user, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:    "person@example.com",
		Password: "correct horse",
	})
	if err != nil {
		t.Fatal(err)
	}
	user, err = db.UpsertUserByEmail(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession(ctx, user.ID, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.UserBySessionToken(ctx, session.Token); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected expired session to be rejected, got %v", err)
	}
}

func TestPermissionGrantPersistence(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	grant := permission.Grant{
		SubjectID: "u1",
		Scope:     permission.ScopeField,
		Resource:  "db.contacts",
		Field:     "email",
		Level:     permission.Write,
	}
	if err := db.SaveGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}

	perms, err := db.GrantsForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanWriteField("u1", "db.contacts", "email") {
		t.Fatal("expected persisted grant to allow field write")
	}
	if perms.CanWriteField("u1", "db.contacts", "name") {
		t.Fatal("did not expect grant to apply to another field")
	}
}

func TestWorkflowDefinitionStoresSecretsAndVariablesAsJSON(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	saved, err := db.SaveWorkflow(ctx, WorkflowDefinition{
		DatabaseName: "workspace",
		Name:         "notify",
		Script:       "function run() {}",
		CreatorID:    "creator",
		Secrets:      map[string]string{"TOKEN": "secret"},
		Variables:    map[string]string{"CHANNEL": "ops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 0 {
		t.Fatal("expected autoincrement workflow id")
	}
	if saved.CreatedAt <= 0 || saved.UpdatedAt <= 0 {
		t.Fatalf("expected workflow millisecond timestamps, got %#v", saved)
	}

	loaded, err := db.Workflow(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CreatorID != "creator" || loaded.Secrets["TOKEN"] != "secret" || loaded.Variables["CHANNEL"] != "ops" {
		t.Fatalf("unexpected workflow JSON fields: %#v", loaded)
	}
	updated, err := db.SaveWorkflow(ctx, WorkflowDefinition{
		ID:           saved.ID,
		DatabaseName: "workspace",
		Name:         "notify",
		Script:       "function run() { return {}; }",
		CreatorID:    "attacker",
		Secrets:      map[string]string{},
		Variables:    map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreatorID != "creator" {
		t.Fatalf("expected workflow creator to be immutable, got %#v", updated)
	}
	if updated.Secrets["TOKEN"] != "secret" {
		t.Fatalf("expected omitted workflow secret to be preserved, got %#v", updated.Secrets)
	}
	list, err := db.Workflows(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("unexpected workflow list: %#v", list)
	}
}

func TestFormDefinitionAutoincrementsID(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	saved, err := db.SaveForm(ctx, FormDefinition{
		DatabaseName: "workspace",
		Name:         "contact-intake",
		Script:       "function render(api, root) { root.append(api.input({ name: 'email' }), api.submit('Save')); return { table: 'contacts', fields: { email: 'email' } }; }",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != 1 {
		t.Fatalf("expected first form id to be 1, got %d", saved.ID)
	}
	if saved.CreatedAt <= 0 || saved.UpdatedAt <= 0 {
		t.Fatalf("expected form millisecond timestamps, got %#v", saved)
	}
	list, err := db.Forms(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("unexpected form list: %#v", list)
	}
}

func TestFormPublishTokenPersists(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	saved, err := db.SaveForm(ctx, FormDefinition{
		DatabaseName: "workspace",
		Name:         "contact-intake",
		Script:       "function render(api, root) { root.append(api.input({ name: 'email' }), api.submit('Save')); return { table: 'contacts', fields: { email: 'email' } }; }",
	})
	if err != nil {
		t.Fatal(err)
	}
	published, err := db.PublishForm(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if published.PublishedToken == "" {
		t.Fatal("expected published token")
	}
	republished, err := db.PublishForm(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if republished.PublishedToken != published.PublishedToken {
		t.Fatalf("expected publish to be stable, got %q then %q", published.PublishedToken, republished.PublishedToken)
	}
	loaded, err := db.FormByPublishedToken(ctx, published.PublishedToken)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != saved.ID {
		t.Fatalf("unexpected published form lookup: %#v", loaded)
	}
	updated, err := db.SaveForm(ctx, FormDefinition{
		ID:           saved.ID,
		DatabaseName: "workspace",
		Name:         "contact-intake",
		Script:       "function render(api, root) { root.append(api.input({ name: 'name' }), api.submit('Save')); return { table: 'contacts', fields: { name: 'name' } }; }",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.PublishedToken != published.PublishedToken {
		t.Fatalf("expected update to preserve published token, got %#v", updated)
	}
}

func TestRoleDefinitionStoresReplaceableGrants(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	role, err := db.SaveRole(ctx, RoleDefinition{DatabaseName: "workspace", Name: "editor"})
	if err != nil {
		t.Fatal(err)
	}
	if role.ID == 0 || role.SubjectID != "role:workspace:editor" {
		t.Fatalf("unexpected role: %#v", role)
	}
	if role.CreatedAt <= 0 || role.UpdatedAt <= 0 {
		t.Fatalf("expected role millisecond timestamps, got %#v", role)
	}

	role, err = db.ReplaceRoleGrants(ctx, "workspace", "editor", []permission.Grant{
		{Scope: permission.ScopeTable, Resource: "workspace.contacts", Level: permission.Write},
		{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "email", Level: permission.Read},
		{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "secret", Level: permission.None},
		{Scope: permission.ScopeWorkflow, Resource: "9", Level: permission.None},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Grants) != 3 {
		t.Fatalf("expected table, field read, and field none grants, got %#v", role.Grants)
	}
	perms, err := db.GrantsForSubject(ctx, role.SubjectID)
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanWriteField(role.SubjectID, "workspace.contacts", "name") {
		t.Fatal("expected table write grant")
	}
	if !perms.CanReadField(role.SubjectID, "workspace.contacts", "email") {
		t.Fatal("expected field read grant")
	}
	if perms.CanReadField(role.SubjectID, "workspace.contacts", "secret") {
		t.Fatal("expected field none grant to override table write")
	}
	role, err = db.ReplaceRoleMembers(ctx, "workspace", "editor", []string{"u1", "u2", "u1", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Members) != 2 || role.Members[0] != "u1" || role.Members[1] != "u2" {
		t.Fatalf("expected de-duplicated role members, got %#v", role.Members)
	}
	effectivePerms, err := db.EffectiveGrantsForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !effectivePerms.CanWriteField("u1", "workspace.contacts", "name") {
		t.Fatal("expected role member to inherit table write grant")
	}
	if !effectivePerms.CanReadField("u1", "workspace.contacts", "email") {
		t.Fatal("expected role member to inherit field read grant")
	}
	if effectivePerms.CanReadField("u1", "workspace.contacts", "secret") {
		t.Fatal("expected role member to inherit field none grant")
	}

	role, err = db.ReplaceRoleGrants(ctx, "workspace", "editor", []permission.Grant{
		{Scope: permission.ScopeForm, Resource: "3", Level: permission.Read},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Grants) != 1 || role.Grants[0].Scope != permission.ScopeForm {
		t.Fatalf("expected replacement grants, got %#v", role.Grants)
	}
	roles, err := db.Roles(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Name != "editor" {
		t.Fatalf("unexpected roles: %#v", roles)
	}
	role, err = db.ReplaceRoleMembers(ctx, "workspace", "editor", []string{"u2"})
	if err != nil {
		t.Fatal(err)
	}
	effectivePerms, err = db.EffectiveGrantsForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if effectivePerms.CanReadResource("u1", permission.ScopeForm, "3") {
		t.Fatal("expected removed role member to lose role grants")
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "system.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return db
}
