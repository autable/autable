package systemdb

import (
	"context"
	"errors"
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

func TestSearchUsersByEmail(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	for _, email := range []string{"Ada@example.com", "grace@example.com", "linus@example.com"} {
		user, err := auth.NewPasswordUser(auth.PasswordRegistration{
			Email:    email,
			Password: "correct horse",
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
		Script:       "function render(api, root) { root.append(api.input({ field: 'email' }), api.submit('Save')); return { table: 'contacts' }; }",
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
		Script:       "function render(api, root) { root.append(api.input({ field: 'email' }), api.submit('Save')); return { table: 'contacts' }; }",
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
		Script:       "function render(api, root) { root.append(api.input({ field: 'name' }), api.submit('Save')); return { table: 'contacts' }; }",
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
		{Scope: permission.ScopeFieldSet, Resource: "workspace.contacts", Level: permission.Write},
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
	if !perms.CanWriteField(role.SubjectID, "workspace.contacts", "name") {
		t.Fatal("expected field set write grant")
	}
	if !perms.CanReadField(role.SubjectID, "workspace.contacts", "email") {
		t.Fatal("expected field read grant")
	}
	if !perms.CanWriteField(role.SubjectID, "workspace.contacts", "secret") {
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
	if !effectivePerms.CanWriteField("u1", "workspace.contacts", "name") {
		t.Fatal("expected role member to inherit field set write grant")
	}
	if !effectivePerms.CanReadField("u1", "workspace.contacts", "email") {
		t.Fatal("expected role member to inherit field read grant")
	}
	if !effectivePerms.CanWriteField("u1", "workspace.contacts", "secret") {
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
	role, err = db.ReplaceRoleMembers(ctx, "workspace", "editor", []RoleMember{{Type: "user", ID: "u2"}})
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
