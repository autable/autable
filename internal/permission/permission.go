package permission

type Level int

const (
	None Level = iota
	Read
	Write
)

// Field grant levels are bitmasks: read, update (modify existing rows), and
// create (fill the field when creating a row) are granted independently.
// The legacy field levels map onto the bits: 1 (read) is FieldRead, and 2
// (write) was migrated to FieldAll when the bitmask was introduced.
const (
	FieldRead   Level = 1
	FieldUpdate Level = 2
	FieldCreate Level = 4
	FieldAll    Level = FieldRead | FieldUpdate | FieldCreate
)

type Scope string

const (
	ScopeFieldSet    Scope = "field_set"
	ScopeField       Scope = "field"
	ScopeRecord      Scope = "record"
	ScopeViewSet     Scope = "view_set"
	ScopeView        Scope = "view"
	ScopeWorkflowSet Scope = "workflow_set"
	ScopeWorkflow    Scope = "workflow"
	ScopeFormSet     Scope = "form_set"
	ScopeForm        Scope = "form"
	// ScopeFile guards viewing and uploading files bound to a table; the
	// resource is db.table.
	ScopeFile Scope = "file"
	// ScopeFieldAdd is a metadata permission: adding non-formula fields to
	// the table (resource db.table). Independent from data-level field bits.
	ScopeFieldAdd Scope = "field_add"
	// ScopeFieldModify is a metadata permission: changing one field's
	// non-formula definition (resource db.table, Field = field name).
	ScopeFieldModify Scope = "field_modify"
)

type Grant struct {
	SubjectID string `json:"subject_id"`
	Scope     Scope  `json:"scope"`
	Resource  string `json:"resource"`
	Field     string `json:"field"`
	Level     Level  `json:"level"`
}

type Set struct {
	grants []Grant
}

func New(grants ...Grant) Set {
	return Set{grants: grants}
}

// FieldLevel returns the union of the field permission bits granted for the
// field, combining table-wide field_set grants with per-field grants.
func (set Set) FieldLevel(subjectID, resource, field string) Level {
	level := None
	for _, grant := range set.grants {
		if grant.SubjectID != subjectID || grant.Resource != resource {
			continue
		}
		switch grant.Scope {
		case ScopeFieldSet:
			level |= grant.Level
		case ScopeField:
			if grant.Field == field {
				level |= grant.Level
			}
		}
	}
	return level
}

// FieldSetLevel returns the union of the field permission bits granted by
// table-wide field_set grants alone.
func (set Set) FieldSetLevel(subjectID, resource string) Level {
	level := None
	for _, grant := range set.grants {
		if grant.SubjectID == subjectID && grant.Resource == resource && grant.Scope == ScopeFieldSet {
			level |= grant.Level
		}
	}
	return level
}

func (set Set) ViewLevel(subjectID, resource, view string) Level {
	level := None
	for _, grant := range set.grants {
		if grant.SubjectID != subjectID || grant.Resource != resource {
			continue
		}
		switch grant.Scope {
		case ScopeViewSet:
			level = maxLevel(level, grant.Level)
		case ScopeView:
			if grant.Field == view {
				level = maxLevel(level, grant.Level)
			}
		}
	}
	return level
}

func (set Set) RecordLevel(subjectID, resource, action string) Level {
	level := None
	for _, grant := range set.grants {
		if grant.SubjectID != subjectID || grant.Scope != ScopeRecord || grant.Resource != resource || grant.Field != action {
			continue
		}
		level = maxLevel(level, grant.Level)
	}
	return level
}

func (set Set) ResourceLevel(subjectID string, scope Scope, resource string) Level {
	level := None
	for _, grant := range set.grants {
		if grant.SubjectID != subjectID || grant.Scope != scope || grant.Resource != resource {
			continue
		}
		if grant.Level > level {
			level = grant.Level
		}
	}
	return level
}

func (set Set) CanReadField(subjectID, resource, field string) bool {
	return set.FieldLevel(subjectID, resource, field)&FieldRead != 0
}

// CanUpdateField reports whether the field may be modified on existing rows.
func (set Set) CanUpdateField(subjectID, resource, field string) bool {
	return set.FieldLevel(subjectID, resource, field)&FieldUpdate != 0
}

// CanCreateField reports whether the field may be filled when creating rows.
func (set Set) CanCreateField(subjectID, resource, field string) bool {
	return set.FieldLevel(subjectID, resource, field)&FieldCreate != 0
}

// CanAddFields is the metadata permission for adding non-formula fields.
func (set Set) CanAddFields(subjectID, resource string) bool {
	return set.ResourceLevel(subjectID, ScopeFieldAdd, resource) >= Write
}

// CanModifyField is the metadata permission for changing one field's
// non-formula definition.
func (set Set) CanModifyField(subjectID, resource, field string) bool {
	for _, grant := range set.grants {
		if grant.SubjectID == subjectID && grant.Scope == ScopeFieldModify && grant.Resource == resource && grant.Field == field && grant.Level >= Write {
			return true
		}
	}
	return false
}

func (set Set) CanReadView(subjectID, resource, view string) bool {
	return set.ViewLevel(subjectID, resource, view) >= Read
}

func (set Set) CanWriteView(subjectID, resource, view string) bool {
	return set.ViewLevel(subjectID, resource, view) >= Write
}

func (set Set) CanCreateRecord(subjectID, resource string) bool {
	return set.RecordLevel(subjectID, resource, "create") >= Write
}

func (set Set) CanDeleteRecord(subjectID, resource string) bool {
	return set.RecordLevel(subjectID, resource, "delete") >= Write
}

func (set Set) CanReadResource(subjectID string, scope Scope, resource string) bool {
	return set.ResourceLevel(subjectID, scope, resource) >= Read
}

func (set Set) CanWriteResource(subjectID string, scope Scope, resource string) bool {
	return set.ResourceLevel(subjectID, scope, resource) >= Write
}

func maxLevel(left, right Level) Level {
	if left > right {
		return left
	}
	return right
}

func (level Level) String() string {
	switch level {
	case None:
		return "none"
	case Read:
		return "read"
	case Write:
		return "write"
	}
	if level > Write && level <= FieldAll {
		parts := []string{}
		if level&FieldRead != 0 {
			parts = append(parts, "read")
		}
		if level&FieldUpdate != 0 {
			parts = append(parts, "update")
		}
		if level&FieldCreate != 0 {
			parts = append(parts, "create")
		}
		joined := ""
		for i, part := range parts {
			if i > 0 {
				joined += "|"
			}
			joined += part
		}
		return joined
	}
	return "unknown"
}
