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
