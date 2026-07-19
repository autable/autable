package permission

import "testing"

func TestFieldLevelPermission(t *testing.T) {
	perms := New(
		Grant{SubjectID: "u1", Scope: ScopeFieldSet, Resource: "db.contacts", Level: Read},
		Grant{SubjectID: "u1", Scope: ScopeField, Resource: "db.contacts", Field: "email", Level: Write},
	)

	if !perms.CanReadField("u1", "db.contacts", "name") {
		t.Fatal("expected table read permission to apply")
	}
	if perms.CanUpdateField("u1", "db.contacts", "name") {
		t.Fatal("did not expect table read permission to allow writes")
	}
	if !perms.CanUpdateField("u1", "db.contacts", "email") {
		t.Fatal("expected field write override")
	}
	if perms.CanReadField("u2", "db.contacts", "email") {
		t.Fatal("did not expect another subject to inherit permissions")
	}
}

func TestFieldGrantsDoNotOverrideFieldSet(t *testing.T) {
	perms := New(
		Grant{SubjectID: "u1", Scope: ScopeFieldSet, Resource: "db.contacts", Level: Write},
		Grant{SubjectID: "u1", Scope: ScopeField, Resource: "db.contacts", Field: "email", Level: Read},
	)

	if !perms.CanUpdateField("u1", "db.contacts", "name") {
		t.Fatal("expected field set write permission to apply without a field grant")
	}
	if !perms.CanReadField("u1", "db.contacts", "email") {
		t.Fatal("expected field read grant to allow reads")
	}
	if !perms.CanUpdateField("u1", "db.contacts", "email") {
		t.Fatal("expected field set write to remain effective")
	}
}

func TestViewLevelPermission(t *testing.T) {
	perms := New(
		Grant{SubjectID: "u1", Scope: ScopeViewSet, Resource: "db.contacts", Level: Read},
		Grant{SubjectID: "u1", Scope: ScopeView, Resource: "db.contacts", Field: "kanban", Level: Write},
	)

	if !perms.CanReadView("u1", "db.contacts", "list") {
		t.Fatal("expected view set read permission")
	}
	if perms.CanWriteView("u1", "db.contacts", "list") {
		t.Fatal("did not expect view set read permission to allow writes")
	}
	if !perms.CanWriteView("u1", "db.contacts", "kanban") {
		t.Fatal("expected specific view write permission")
	}
}

func TestRecordActionPermission(t *testing.T) {
	perms := New(
		Grant{SubjectID: "u1", Scope: ScopeRecord, Resource: "db.contacts", Field: "create", Level: Write},
		Grant{SubjectID: "u1", Scope: ScopeRecord, Resource: "db.contacts", Field: "delete", Level: Read},
	)

	if !perms.CanCreateRecord("u1", "db.contacts") {
		t.Fatal("expected record create permission")
	}
	if perms.CanDeleteRecord("u1", "db.contacts") {
		t.Fatal("did not expect read-level record delete grant to allow delete")
	}
	if perms.CanCreateRecord("u2", "db.contacts") {
		t.Fatal("did not expect another subject to inherit record permission")
	}
}

func TestResourceLevelPermission(t *testing.T) {
	perms := New(
		Grant{SubjectID: "u1", Scope: ScopeWorkflowSet, Resource: "workspace", Level: Read},
		Grant{SubjectID: "u1", Scope: ScopeWorkflow, Resource: "7", Level: Write},
		Grant{SubjectID: "u1", Scope: ScopeForm, Resource: "3", Level: Read},
	)

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
