package permission

type Level int

const (
	None Level = iota
	Read
	Write
)

type Scope string

const (
	ScopeFieldSet    Scope = "field_set"
	ScopeField       Scope = "field"
	ScopeViewSet     Scope = "view_set"
	ScopeView        Scope = "view"
	ScopeWorkflowSet Scope = "workflow_set"
	ScopeWorkflow    Scope = "workflow"
	ScopeFormSet     Scope = "form_set"
	ScopeForm        Scope = "form"
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
	level := None
	for _, grant := range set.grants {
		if grant.SubjectID != subjectID || grant.Resource != resource {
			continue
		}
		switch grant.Scope {
		case ScopeFieldSet:
			level = maxLevel(level, grant.Level)
		case ScopeField:
			if grant.Field == field {
				level = maxLevel(level, grant.Level)
			}
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

func (set Set) CanReadView(subjectID, resource, view string) bool {
	return set.ViewLevel(subjectID, resource, view) >= Read
}

func (set Set) CanWriteView(subjectID, resource, view string) bool {
	return set.ViewLevel(subjectID, resource, view) >= Write
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
	default:
		return "unknown"
	}
}
