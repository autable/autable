package systemdb

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"autable/internal/auth"
	"autable/internal/permission"
	"gorm.io/gorm"
)

func TestUserUpsertUsesEmailFallback(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	passwordUser, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:       "person@example.com",
		DisplayName: "Person Example",
		Password:    "correct horse",
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
		DisplayName:  "Renamed Person",
	})
	if err != nil {
		t.Fatal(err)
	}
	upserted, err := db.UpsertUserByEmail(ctx, oidcUser)
	if err != nil {
		t.Fatal(err)
	}
	if upserted.ID != inserted.ID {
		t.Fatalf("expected email upsert to keep existing user id %q, got %q", inserted.ID, upserted.ID)
	}

	loaded, err := db.UserByEmail(ctx, "person@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Provider != auth.ProviderOIDC || loaded.Subject != "sub-123" {
		t.Fatalf("unexpected loaded user: %#v", loaded)
	}
	if loaded.DisplayName != "Renamed Person" {
		t.Fatalf("expected display name to be updated, got %q", loaded.DisplayName)
	}
}

func TestUserUpsertAllowsDuplicateDisplayName(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	first, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:       "first@example.com",
		DisplayName: "Shared Name",
		Password:    "correct horse",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertUserByEmail(ctx, first); err != nil {
		t.Fatal(err)
	}

	second, err := auth.NewPasswordUser(auth.PasswordRegistration{
		Email:       "second@example.com",
		DisplayName: "Shared Name",
		Password:    "correct horse",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertUserByEmail(ctx, second); err != nil {
		t.Fatal(err)
	}
}

func TestSearchUsersByEmailAndDisplayName(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	for _, item := range []struct {
		email       string
		displayName string
	}{
		{email: "Ada@example.com", displayName: "Ada Lovelace"},
		{email: "grace@example.com", displayName: "Grace Hopper"},
		{email: "linus@example.com", displayName: "Linus Torvalds"},
	} {
		user, err := auth.NewPasswordUser(auth.PasswordRegistration{
			Email:       item.email,
			DisplayName: item.displayName,
			Password:    "correct horse",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.UpsertUserByEmail(ctx, user); err != nil {
			t.Fatal(err)
		}
	}

	users, err := db.SearchUsers(ctx, "EXAMPLE", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Email != "ada@example.com" || users[1].Email != "grace@example.com" {
		t.Fatalf("expected first two users sorted by email, got %#v", users)
	}
	users, err = db.SearchUsers(ctx, "lin", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Email != "linus@example.com" {
		t.Fatalf("expected linus match, got %#v", users)
	}
	users, err = db.SearchUsers(ctx, "hop", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].DisplayName != "Grace Hopper" {
		t.Fatalf("expected hopper match, got %#v", users)
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
		Email:       "person@example.com",
		DisplayName: "Person Example",
		Password:    "correct horse",
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
		Email:       "person@example.com",
		DisplayName: "Person Example",
		Password:    "correct horse",
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
		Level:     permission.FieldAll,
	}
	if err := db.SaveGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}

	perms, err := db.GrantsForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanUpdateField("u1", "db.contacts", "email") {
		t.Fatal("expected persisted grant to allow field write")
	}
	if perms.CanUpdateField("u1", "db.contacts", "name") {
		t.Fatal("did not expect grant to apply to another field")
	}
}

func TestAppVersionsPublishAndDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	app, err := db.CreateApp(ctx, AppDefinition{DatabaseName: "workspace", Name: "intake", Title: "Intake", CreatorID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if app.ID == 0 || app.LatestVersion != 0 || app.PublishedVersion != 0 || app.CreatedAt <= 0 {
		t.Fatalf("unexpected new app: %#v", app)
	}
	app, first, err := db.AddAppVersion(ctx, app.ID, "", AppVersion{SDK: "1.0", Message: "first", CreatedBy: "u1"}, []byte("one"), false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || first.Size != 3 || app.LatestVersion != 1 || app.PublishedVersion != 0 || app.Title != "Intake" {
		t.Fatalf("unexpected first version: %#v %#v", app, first)
	}
	app, second, err := db.AddAppVersion(ctx, app.ID, "Intake 2", AppVersion{SDK: "1.0", CreatedBy: "u2"}, []byte("two!"), true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 || app.PublishedVersion != 2 || app.Title != "Intake 2" {
		t.Fatalf("expected publishing upload, got %#v %#v", app, second)
	}
	versions, err := db.AppVersions(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Version != 2 || versions[1].Message != "first" {
		t.Fatalf("expected newest-first versions, got %#v", versions)
	}
	bundle, err := db.AppVersionBundle(ctx, app.ID, 1)
	if err != nil || string(bundle) != "one" {
		t.Fatalf("unexpected bundle %q, %v", bundle, err)
	}
	app, err = db.PublishAppVersion(ctx, app.ID, 1)
	if err != nil || app.PublishedVersion != 1 {
		t.Fatalf("expected rollback to version 1, got %#v, %v", app, err)
	}
	if _, err := db.PublishAppVersion(ctx, app.ID, 9); !errors.Is(err, ErrAppVersionNotFound) {
		t.Fatalf("expected missing version error, got %v", err)
	}
	if err := db.SaveGrant(ctx, permission.Grant{SubjectID: "u2", Scope: permission.ScopeApp, Resource: fmt.Sprint(app.ID), Level: permission.Read}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteApp(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppVersionBundle(ctx, app.ID, 1); !errors.Is(err, ErrAppVersionNotFound) {
		t.Fatalf("expected versions to be deleted, got %v", err)
	}
	grants, err := db.GrantListForSubject(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Fatalf("expected app grants to be deleted, got %#v", grants)
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
		{Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.FieldAll},
		{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "email", Level: permission.Read},
		{Scope: permission.ScopeField, Resource: "workspace.contacts", Field: "secret", Level: permission.None},
		{Scope: permission.ScopeWorkflow, Resource: "9", Level: permission.None},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Grants) != 2 {
		t.Fatalf("expected field set and field read grants, got %#v", role.Grants)
	}
	perms, err := db.GrantsForSubject(ctx, role.SubjectID)
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanUpdateField(role.SubjectID, "workspace.contacts", "name") {
		t.Fatal("expected field set write grant")
	}
	if !perms.CanReadField(role.SubjectID, "workspace.contacts", "email") {
		t.Fatal("expected field read grant")
	}
	if !perms.CanUpdateField(role.SubjectID, "workspace.contacts", "secret") {
		t.Fatal("expected field set write grant to apply to secret")
	}
	role, err = db.ReplaceRoleMembers(ctx, "workspace", "editor", []RoleMember{
		{Type: "user", ID: "u1"},
		{Type: "user", ID: "u2"},
		{Type: "user", ID: "u1"},
		{Type: "user", ID: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Members) != 2 || role.Members[0] != (RoleMember{Type: "user", ID: "u1"}) || role.Members[1] != (RoleMember{Type: "user", ID: "u2"}) {
		t.Fatalf("expected de-duplicated role members, got %#v", role.Members)
	}
	effectivePerms, err := db.EffectiveGrantsForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !effectivePerms.CanUpdateField("u1", "workspace.contacts", "name") {
		t.Fatal("expected role member to inherit field set write grant")
	}
	if !effectivePerms.CanReadField("u1", "workspace.contacts", "email") {
		t.Fatal("expected role member to inherit field read grant")
	}
	if !effectivePerms.CanUpdateField("u1", "workspace.contacts", "secret") {
		t.Fatal("expected role member to inherit field set write grant")
	}
	role, err = db.ReplaceRoleMembers(ctx, "workspace", "editor", []RoleMember{{Type: "workflow", ID: "7"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Members) != 1 || role.Members[0] != (RoleMember{Type: "workflow", ID: "7"}) {
		t.Fatalf("expected workflow role member to round-trip raw id, got %#v", role.Members)
	}
	workflowPerms, err := db.EffectiveGrantsForSubject(ctx, WorkflowSubjectID(7))
	if err != nil {
		t.Fatal(err)
	}
	if !workflowPerms.CanReadField(WorkflowSubjectID(7), "workspace.contacts", "email") {
		t.Fatal("expected workflow role member to inherit role grants")
	}

	role, err = db.ReplaceRoleGrants(ctx, "workspace", "editor", []permission.Grant{
		{Scope: permission.ScopeApp, Resource: "3", Level: permission.Read},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Grants) != 1 || role.Grants[0].Scope != permission.ScopeApp {
		t.Fatalf("expected replacement grants, got %#v", role.Grants)
	}
	roles, err := db.Roles(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Name != "editor" {
		t.Fatalf("unexpected roles: %#v", roles)
	}
	role, err = db.ReplaceRoleMembers(ctx, "workspace", "editor", []RoleMember{{Type: "user", ID: "u2"}})
	if err != nil {
		t.Fatal(err)
	}
	effectivePerms, err = db.EffectiveGrantsForSubject(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if effectivePerms.CanReadResource("u1", permission.ScopeApp, "3") {
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

func TestBindFileToRecordIsOneShot(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	file, err := db.CreateFile(ctx, FileRecord{Name: "a.pdf", DatabaseName: "db", TableName: "contacts", CreatorID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if file.RecordID != 0 {
		t.Fatalf("expected unbound file, got %#v", file)
	}

	if err := db.BindFileToRecord(ctx, "bob", file.ID, "db", "contacts", 7); err == nil {
		t.Fatal("expected binding by a non-uploader to fail")
	}
	if err := db.BindFileToRecord(ctx, "alice", file.ID, "db", "contacts", 7); err != nil {
		t.Fatal(err)
	}
	if err := db.BindFileToRecord(ctx, "alice", file.ID, "db", "contacts", 7); err != nil {
		t.Fatalf("expected rebinding to the same record to be a no-op, got %v", err)
	}
	if err := db.BindFileToRecord(ctx, "bob", file.ID, "db", "contacts", 7); err != nil {
		t.Fatalf("expected the already-bound no-op to pass for other actors, got %v", err)
	}
	if err := db.BindFileToRecord(ctx, "alice", file.ID, "db", "contacts", 8); err == nil {
		t.Fatal("expected binding to a second record to fail")
	}
	if err := db.BindFileToRecord(ctx, "alice", file.ID, "db", "orders", 7); err == nil {
		t.Fatal("expected binding to another table to fail")
	}
	if err := db.BindFileToRecord(ctx, "alice", file.ID+99, "db", "contacts", 7); err == nil {
		t.Fatal("expected binding a missing file to fail")
	}

	bound, err := db.File(ctx, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bound.RecordID != 7 {
		t.Fatalf("expected file bound to record 7, got %#v", bound)
	}
	if bound.CreatedAt <= 0 {
		t.Fatalf("expected a created_at timestamp, got %#v", bound)
	}
}
