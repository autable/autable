package systemdb

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"codetable/internal/auth"
	"codetable/internal/permission"
	"gorm.io/gorm"
)

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
		Script:       "export default async function run() {}",
		Secrets:      map[string]string{"TOKEN": "secret"},
		Variables:    map[string]string{"CHANNEL": "ops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 0 {
		t.Fatal("expected autoincrement workflow id")
	}

	loaded, err := db.Workflow(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Secrets["TOKEN"] != "secret" || loaded.Variables["CHANNEL"] != "ops" {
		t.Fatalf("unexpected workflow JSON fields: %#v", loaded)
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
		Script:       "root.append(api.input({ name: 'email' }))",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != 1 {
		t.Fatalf("expected first form id to be 1, got %d", saved.ID)
	}
	list, err := db.Forms(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("unexpected form list: %#v", list)
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

	role, err = db.ReplaceRoleGrants(ctx, "workspace", "editor", []permission.Grant{
		{Scope: permission.ScopeTable, Resource: "workspace.contacts", Level: permission.Write},
		{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "email", Level: permission.Read},
		{Scope: permission.ScopeWorkflow, Resource: "9", Level: permission.None},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Grants) != 2 {
		t.Fatalf("expected two persisted grants, got %#v", role.Grants)
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
