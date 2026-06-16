package permission

import "testing"

func TestFieldLevelPermission(t *testing.T) {
	perms := New(
		Grant{SubjectID: "u1", Scope: ScopeTable, Resource: "db.contacts", Level: Read},
		Grant{SubjectID: "u1", Scope: ScopeField, Resource: "db.contacts", Field: "email", Level: Write},
	)

	if !perms.CanReadField("u1", "db.contacts", "name") {
		t.Fatal("expected table read permission to apply")
	}
	if perms.CanWriteField("u1", "db.contacts", "name") {
		t.Fatal("did not expect table read permission to allow writes")
	}
	if !perms.CanWriteField("u1", "db.contacts", "email") {
		t.Fatal("expected field write override")
	}
	if perms.CanReadField("u2", "db.contacts", "email") {
		t.Fatal("did not expect another subject to inherit permissions")
	}
}

func TestFieldGrantOverridesTableDefault(t *testing.T) {
	perms := New(
		Grant{SubjectID: "u1", Scope: ScopeTable, Resource: "db.contacts", Level: Write},
		Grant{SubjectID: "u1", Scope: ScopeField, Resource: "db.contacts", Field: "email", Level: Read},
		Grant{SubjectID: "u1", Scope: ScopeField, Resource: "db.contacts", Field: "secret", Level: None},
	)

	if !perms.CanWriteField("u1", "db.contacts", "name") {
		t.Fatal("expected table write permission to apply without a field grant")
	}
	if !perms.CanReadField("u1", "db.contacts", "email") {
		t.Fatal("expected field read grant to allow reads")
	}
	if perms.CanWriteField("u1", "db.contacts", "email") {
		t.Fatal("did not expect table write to override field read")
	}
	if perms.CanReadField("u1", "db.contacts", "secret") {
		t.Fatal("did not expect table write to override explicit field none")
	}
}

func TestResourceLevelPermission(t *testing.T) {
	perms := New(
		Grant{SubjectID: "u1", Scope: ScopeDatabase, Resource: "workspace", Level: Write},
		Grant{SubjectID: "u1", Scope: ScopeWorkflow, Resource: "7", Level: Write},
		Grant{SubjectID: "u1", Scope: ScopeForm, Resource: "3", Level: Read},
	)

	if !perms.CanWriteResource("u1", ScopeDatabase, "workspace") {
		t.Fatal("expected database write permission")
	}
	if !perms.CanWriteResource("u1", ScopeWorkflow, "7") {
		t.Fatal("expected workflow write permission")
	}
	if !perms.CanReadResource("u1", ScopeWorkflow, "7") {
		t.Fatal("expected workflow write permission to allow reads")
	}
	if perms.CanWriteResource("u1", ScopeForm, "3") {
		t.Fatal("did not expect form read permission to allow writes")
	}
	if perms.CanReadResource("u2", ScopeWorkflow, "7") {
		t.Fatal("did not expect another subject to inherit workflow permission")
	}
	if perms.CanReadResource("u1", ScopeForm, "7") {
		t.Fatal("did not expect workflow resource id to cross scopes")
	}
}
