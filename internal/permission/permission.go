package permission

type Level int

const (
	None Level = iota
	Read
	Write
)

type Scope string

const (
	ScopeTable    Scope = "table"
	ScopeField    Scope = "field"
	ScopeWorkflow Scope = "workflow"
	ScopeForm     Scope = "form"
)

type Grant struct {
	SubjectID string
	Scope     Scope
	Resource  string
	Field     string
	Level     Level
}

type Set struct {
	grants []Grant
}

func New(grants ...Grant) Set {
	return Set{grants: grants}
}

func (set Set) FieldLevel(subjectID, resource, field string) Level {
	level := None
	for _, grant := range set.grants {
		if grant.SubjectID != subjectID || grant.Resource != resource {
			continue
		}
		if grant.Scope == ScopeTable && grant.Level > level {
			level = grant.Level
		}
		if grant.Scope == ScopeField && grant.Field == field && grant.Level > level {
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
