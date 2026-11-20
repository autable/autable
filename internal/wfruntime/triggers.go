package wfruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"autable/internal/cron"
)

// Trigger kinds.
const (
	KindSchedule      = "schedule"
	KindRecordChanged = "recordChanged"
	KindWebhook       = "webhook"
	KindManual        = "manual"
)

// ManualField is one input of a manual trigger's form.
type ManualField struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	Type  string `json:"type"`
}

// Trigger is one validated trigger declaration with its name. Its JSON
// form is the declaration plus "name" (see workflows-host-api.md).
type Trigger struct {
	Name string
	Kind string
	// schedule
	Cron     string
	Timezone string
	// recordChanged
	Table            string
	Operations       []string
	ChangedFields    []string
	IncludeOwnWrites bool
	// webhook
	Path string
	// manual
	InputFields []ManualField
}

type triggerJSON struct {
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	Cron             string   `json:"cron,omitempty"`
	Timezone         string   `json:"timezone,omitempty"`
	Table            string   `json:"table,omitempty"`
	Operations       []string `json:"operations,omitempty"`
	Fields           any      `json:"fields,omitempty"`
	IncludeOwnWrites bool     `json:"includeOwnWrites,omitempty"`
	Path             string   `json:"path,omitempty"`
}

func (trigger Trigger) MarshalJSON() ([]byte, error) {
	out := triggerJSON{
		Name: trigger.Name, Kind: trigger.Kind, Cron: trigger.Cron, Timezone: trigger.Timezone,
		Table: trigger.Table, Operations: trigger.Operations, IncludeOwnWrites: trigger.IncludeOwnWrites, Path: trigger.Path,
	}
	switch trigger.Kind {
	case KindRecordChanged:
		if len(trigger.ChangedFields) > 0 {
			out.Fields = trigger.ChangedFields
		}
	case KindManual:
		out.Fields = trigger.InputFields
		if trigger.InputFields == nil {
			out.Fields = []ManualField{}
		}
	}
	return json.Marshal(out)
}

func (trigger *Trigger) UnmarshalJSON(data []byte) error {
	var raw struct {
		triggerJSON
		Fields json.RawMessage `json:"fields,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*trigger = Trigger{
		Name: raw.Name, Kind: raw.Kind, Cron: raw.Cron, Timezone: raw.Timezone, Table: raw.Table,
		Operations: raw.Operations, IncludeOwnWrites: raw.IncludeOwnWrites, Path: raw.Path,
	}
	if len(raw.Fields) == 0 || string(raw.Fields) == "null" {
		return nil
	}
	if raw.Kind == KindManual {
		return json.Unmarshal(raw.Fields, &trigger.InputFields)
	}
	return json.Unmarshal(raw.Fields, &trigger.ChangedFields)
}

// Schema answers what ParseTriggers needs to know about the workflow's
// database: the active fields of a table, ok false when it does not exist.
type Schema interface {
	TableFields(table string) ([]string, bool)
}

var (
	triggerNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	webhookPathPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+(/[A-Za-z0-9._~-]+)*$`)
	allowedKeys        = map[string]map[string]bool{
		KindSchedule:      {"kind": true, "cron": true, "timezone": true},
		KindRecordChanged: {"kind": true, "table": true, "operations": true, "fields": true, "includeOwnWrites": true},
		KindWebhook:       {"kind": true, "path": true},
		KindManual:        {"kind": true, "fields": true},
	}
	operations = []string{"create", "update", "delete"}
)

// ParseTriggers validates the JSON of a `triggers` export and returns the
// triggers sorted by name.
func ParseTriggers(triggersJSON string, schema Schema) ([]Trigger, error) {
	var declarations map[string]json.RawMessage
	if err := json.Unmarshal([]byte(triggersJSON), &declarations); err != nil {
		return nil, fmt.Errorf("triggers must be an object of trigger declarations: %w", err)
	}
	names := make([]string, 0, len(declarations))
	for name := range declarations {
		names = append(names, name)
	}
	sort.Strings(names)
	triggers := make([]Trigger, 0, len(names))
	paths := map[string]string{}
	for _, name := range names {
		trigger, err := parseTrigger(name, declarations[name], schema)
		if err != nil {
			return nil, fmt.Errorf("trigger %q: %w", name, err)
		}
		if trigger.Kind == KindWebhook {
			if other, ok := paths[trigger.Path]; ok {
				return nil, fmt.Errorf("triggers %q and %q both use webhook path %q", other, name, trigger.Path)
			}
			paths[trigger.Path] = name
		}
		triggers = append(triggers, trigger)
	}
	return triggers, nil
}

func parseTrigger(name string, raw json.RawMessage, schema Schema) (Trigger, error) {
	if !triggerNamePattern.MatchString(name) {
		return Trigger{}, fmt.Errorf("the name must match %s", triggerNamePattern)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil || keys == nil {
		return Trigger{}, errors.New("a declaration must be an object; use the SDK's trigger helpers")
	}
	var kind string
	if err := json.Unmarshal(keys["kind"], &kind); err != nil || kind == "" {
		return Trigger{}, errors.New("the declaration has no kind; use the SDK's trigger helpers")
	}
	allowed, ok := allowedKeys[kind]
	if !ok {
		return Trigger{}, fmt.Errorf("unknown trigger kind %q", kind)
	}
	for key := range keys {
		if !allowed[key] {
			return Trigger{}, fmt.Errorf("unknown key %q for a %s trigger", key, kind)
		}
	}
	var declaration struct {
		Kind             string          `json:"kind"`
		Cron             string          `json:"cron"`
		Timezone         *string         `json:"timezone"`
		Table            string          `json:"table"`
		Operations       []string        `json:"operations"`
		Fields           json.RawMessage `json:"fields"`
		IncludeOwnWrites *bool           `json:"includeOwnWrites"`
		Path             string          `json:"path"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&declaration); err != nil {
		return Trigger{}, fmt.Errorf("invalid declaration: %w", err)
	}
	trigger := Trigger{Name: name, Kind: kind}
	switch kind {
	case KindSchedule:
		trigger.Cron = strings.TrimSpace(declaration.Cron)
		trigger.Timezone = "UTC"
		if declaration.Timezone != nil {
			trigger.Timezone = *declaration.Timezone
		}
		if _, err := cron.Parse(trigger.Cron, trigger.Timezone); err != nil {
			return Trigger{}, err
		}
	case KindRecordChanged:
		trigger.Table = declaration.Table
		fields, ok := schema.TableFields(trigger.Table)
		if !ok {
			return Trigger{}, fmt.Errorf("table %q does not exist", trigger.Table)
		}
		trigger.Operations = operations
		if declaration.Operations != nil {
			if len(declaration.Operations) == 0 {
				return Trigger{}, errors.New("operations must not be empty")
			}
			seen := map[string]bool{}
			trigger.Operations = nil
			for _, operation := range operations {
				for _, declared := range declaration.Operations {
					if declared == operation && !seen[operation] {
						seen[operation] = true
						trigger.Operations = append(trigger.Operations, operation)
					}
				}
			}
			for _, declared := range declaration.Operations {
				if !seen[declared] {
					return Trigger{}, fmt.Errorf("unknown operation %q (use create, update or delete)", declared)
				}
			}
		}
		if len(declaration.Fields) > 0 && string(declaration.Fields) != "null" {
			if err := json.Unmarshal(declaration.Fields, &trigger.ChangedFields); err != nil || len(trigger.ChangedFields) == 0 {
				return Trigger{}, errors.New("fields must be a non-empty list of field names")
			}
			known := map[string]bool{}
			for _, field := range fields {
				known[field] = true
			}
			for _, field := range trigger.ChangedFields {
				if !known[field] {
					return Trigger{}, fmt.Errorf("table %q has no field %q", trigger.Table, field)
				}
			}
		}
		if declaration.IncludeOwnWrites != nil {
			trigger.IncludeOwnWrites = *declaration.IncludeOwnWrites
		}
	case KindWebhook:
		trigger.Path = declaration.Path
		if len(trigger.Path) > 200 || !webhookPathPattern.MatchString(trigger.Path) {
			return Trigger{}, fmt.Errorf("webhook path %q must be segments of letters, digits and ._~- separated by /", trigger.Path)
		}
	case KindManual:
		if len(declaration.Fields) > 0 && string(declaration.Fields) != "null" {
			fieldDecoder := json.NewDecoder(bytes.NewReader(declaration.Fields))
			fieldDecoder.DisallowUnknownFields()
			if err := fieldDecoder.Decode(&trigger.InputFields); err != nil {
				return Trigger{}, fmt.Errorf("fields must be a list of {name, label?, type}: %w", err)
			}
		}
		seen := map[string]bool{}
		for _, field := range trigger.InputFields {
			if strings.TrimSpace(field.Name) == "" {
				return Trigger{}, errors.New("every input field needs a name")
			}
			if seen[field.Name] {
				return Trigger{}, fmt.Errorf("input field %q appears twice", field.Name)
			}
			seen[field.Name] = true
			switch field.Type {
			case "string", "number", "boolean":
			default:
				return Trigger{}, fmt.Errorf("input field %q has type %q; use string, number or boolean", field.Name, field.Type)
			}
		}
	}
	return trigger, nil
}

// CheckManualInput checks the declared fields that are present in input.
func CheckManualInput(trigger Trigger, input map[string]any) error {
	for _, field := range trigger.InputFields {
		value, ok := input[field.Name]
		if !ok || value == nil {
			continue
		}
		valid := false
		switch field.Type {
		case "string":
			_, valid = value.(string)
		case "number":
			switch value.(type) {
			case float64, json.Number, int, int64:
				valid = true
			}
		case "boolean":
			_, valid = value.(bool)
		}
		if !valid {
			return fmt.Errorf("input %q must be a %s", field.Name, field.Type)
		}
	}
	return nil
}
