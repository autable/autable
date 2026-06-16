package permission

type Level int

const (
	None Level = iota
	Read
	Write
)

type Scope string

const (
	ScopeDatabase Scope = "database"
	ScopeTable    Scope = "table"
	ScopeField    Scope = "field"
	ScopeWorkflow Scope = "workflow"
	ScopeForm     Scope = "form"
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

func (set Set) FieldLevel(subjectID, resource, field string) Level {
	tableLevel := None
	fieldLevel := None
	hasFieldGrant := false
	for _, grant := range set.grants {
		if grant.SubjectID != subjectID || grant.Resource != resource {
			continue
		}
		switch grant.Scope {
		case ScopeTable:
			if grant.Level > tableLevel {
				tableLevel = grant.Level
			}
		case ScopeField:
			if grant.Field == field {
				hasFieldGrant = true
				if grant.Level > fieldLevel {
					fieldLevel = grant.Level
				}
			}
		}
	}
	if hasFieldGrant {
		return fieldLevel
	}
	return tableLevel
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
	return set.FieldLevel(subjectID, resource, field) >= Read
}

func (set Set) CanWriteField(subjectID, resource, field string) bool {
	return set.FieldLevel(subjectID, resource, field) >= Write
}

func (set Set) CanReadResource(subjectID string, scope Scope, resource string) bool {
	return set.ResourceLevel(subjectID, scope, resource) >= Read
}

func (set Set) CanWriteResource(subjectID string, scope Scope, resource string) bool {
	return set.ResourceLevel(subjectID, scope, resource) >= Write
}

func (level Level) String() string {
	switch level {
	case None:
		return "none"
	case Read:
		return "read"
	case Write:
		return "write"
	default:
		return "unknown"
	}
}
