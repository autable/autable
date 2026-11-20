package wfruntime

import (
	"encoding/json"
	"strings"
	"testing"
)

type fakeSchema map[string][]string

func (schema fakeSchema) TableFields(table string) ([]string, bool) {
	fields, ok := schema[table]
	return fields, ok
}

var testSchema = fakeSchema{"订单": {"状态", "数量"}}

func TestParseTriggers(t *testing.T) {
	triggers, err := ParseTriggers(`{
		"daily": {"kind": "schedule", "cron": "0 9 * * 1-5", "timezone": "Asia/Shanghai"},
		"changed": {"kind": "recordChanged", "table": "订单", "operations": ["update", "create", "update"], "fields": ["状态"], "includeOwnWrites": true},
		"any": {"kind": "recordChanged", "table": "订单"},
		"hook": {"kind": "webhook", "path": "callbacks/v1"},
		"start": {"kind": "manual", "fields": [{"name": "orderId", "label": "Order", "type": "number"}]},
		"plain": {"kind": "manual"}
	}`, testSchema)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, trigger := range triggers {
		names = append(names, trigger.Name)
	}
	if strings.Join(names, ",") != "any,changed,daily,hook,plain,start" {
		t.Fatalf("expected triggers sorted by name, got %v", names)
	}
	byName := map[string]Trigger{}
	for _, trigger := range triggers {
		byName[trigger.Name] = trigger
	}
	if changed := byName["changed"]; strings.Join(changed.Operations, ",") != "create,update" || changed.ChangedFields[0] != "状态" || !changed.IncludeOwnWrites {
		t.Fatalf("unexpected recordChanged trigger %#v", changed)
	}
	if anyChange := byName["any"]; strings.Join(anyChange.Operations, ",") != "create,update,delete" || anyChange.IncludeOwnWrites {
		t.Fatalf("expected default operations and no own writes, got %#v", anyChange)
	}
	if byName["daily"].Timezone != "Asia/Shanghai" || byName["start"].InputFields[0].Type != "number" {
		t.Fatalf("unexpected triggers %#v", byName)
	}

	// JSON round trip keeps the declaration shape.
	encoded, err := json.Marshal(triggers)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"fields":["状态"]`) || !strings.Contains(string(encoded), `"fields":[{"name":"orderId","label":"Order","type":"number"}]`) || !strings.Contains(string(encoded), `{"name":"plain","kind":"manual","fields":[]}`) {
		t.Fatalf("unexpected JSON %s", encoded)
	}
	var decoded []Trigger
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded[1].ChangedFields[0] != "状态" || decoded[5].InputFields[0].Name != "orderId" {
		t.Fatalf("unexpected decoded triggers %#v", decoded)
	}
}

func TestParseTriggersRejectsInvalidDeclarations(t *testing.T) {
	cases := map[string]string{
		`[]`:                               "must be an object",
		`{"bad name": {"kind": "manual"}}`: "name must match",
		`{"t": "schedule"}`:                "must be an object",
		`{"t": {"cron": "* * * * *"}}`:     "no kind",
		`{"t": {"kind": "cron"}}`:          "unknown trigger kind",
		`{"t": {"kind": "schedule", "cron": "* * * * *", "tz": "UTC"}}`:                                           `unknown key "tz"`,
		`{"t": {"kind": "schedule", "cron": "61 * * * *"}}`:                                                       "outside 0-59",
		`{"t": {"kind": "schedule", "cron": "* * * * *", "timezone": "Nowhere"}}`:                                 "unknown timezone",
		`{"t": {"kind": "recordChanged", "table": "missing"}}`:                                                    "does not exist",
		`{"t": {"kind": "recordChanged", "table": "订单", "operations": ["upsert"]}}`:                               "unknown operation",
		`{"t": {"kind": "recordChanged", "table": "订单", "operations": []}}`:                                       "must not be empty",
		`{"t": {"kind": "recordChanged", "table": "订单", "fields": ["备注"]}}`:                                       `has no field "备注"`,
		`{"t": {"kind": "webhook", "path": "/leading"}}`:                                                          "webhook path",
		`{"t": {"kind": "webhook", "path": "a"}, "u": {"kind": "webhook", "path": "a"}}`:                          "both use webhook path",
		`{"t": {"kind": "manual", "fields": [{"name": "x", "type": "date"}]}}`:                                    `type "date"`,
		`{"t": {"kind": "manual", "fields": [{"name": "x", "type": "string", "default": 1}]}}`:                    "unknown field",
		`{"t": {"kind": "manual", "fields": [{"name": "x", "type": "string"}, {"name": "x", "type": "number"}]}}`: "appears twice",
	}
	for input, want := range cases {
		if _, err := ParseTriggers(input, testSchema); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseTriggers(%s): expected error containing %q, got %v", input, want, err)
		}
	}
}

func TestCheckManualInput(t *testing.T) {
	trigger := Trigger{Kind: KindManual, InputFields: []ManualField{{Name: "n", Type: "number"}, {Name: "s", Type: "string"}, {Name: "b", Type: "boolean"}}}
	if err := CheckManualInput(trigger, map[string]any{"n": 1.5, "s": "x", "b": true, "extra": []any{}}); err != nil {
		t.Fatal(err)
	}
	if err := CheckManualInput(trigger, map[string]any{}); err != nil {
		t.Fatalf("expected absent fields to be fine, got %v", err)
	}
	if err := CheckManualInput(trigger, map[string]any{"n": "1"}); err == nil || !strings.Contains(err.Error(), `"n" must be a number`) {
		t.Fatalf("expected a type error, got %v", err)
	}
}
