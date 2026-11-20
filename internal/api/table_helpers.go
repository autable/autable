package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"autable/internal/metadata"
	"autable/internal/permission"
	"autable/internal/table"
)

// Table operations shared by the HTTP handlers and the workflow host.

func (server *Server) upsertTableRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID string, isAdmin bool, dbName string, tableName string, matchField string, values map[string]any) (table.Row, string, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return table.Row{}, "", fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	matchFieldMeta, ok := tableMeta.Field(matchField)
	if !ok || strings.HasPrefix(matchFieldMeta.Name, "ct_") {
		return table.Row{}, "", fmt.Errorf("%w: %s", metadata.ErrUnknownField, matchField)
	}
	matchValue, err := normalizeComparableValue(matchFieldMeta, values[matchField])
	if err != nil {
		return table.Row{}, "", err
	}
	// The match runs inside the actor's row set (the view boundary): rows
	// the actor cannot see behave as absent, so upsert can neither update
	// nor probe them.
	rows, err := server.tables.RowsWithOptions(ctx, catalog, perms, actorID, isAdmin, dbName, tableName, table.RowListOptions{
		Query: &metadata.ViewQuery{
			Combinator: "and",
			Rules:      []metadata.ViewQueryRule{{Field: matchField, Operator: "=", Value: matchValue}},
		},
		Limit: 1,
	})
	if err != nil {
		return table.Row{}, "", err
	}
	if len(rows) > 0 {
		row := rows[0]
		changed, err := rowValuesChanged(tableMeta, row.Values, values)
		if err != nil {
			return table.Row{}, "", err
		}
		if !changed {
			return row, "noop", nil
		}
		updated, err := server.tables.UpdateRow(ctx, catalog, perms, actorID, isAdmin, dbName, tableName, row.RecordID, values)
		return updated, "update", err
	}
	created, err := server.tables.CreateRow(ctx, catalog, perms, actorID, isAdmin, dbName, tableName, values)
	return created, "create", err
}

func rowValuesChanged(tableMeta metadata.Table, existing map[string]any, values map[string]any) (bool, error) {
	for fieldName, nextValue := range values {
		field, ok := tableMeta.Field(fieldName)
		if !ok {
			return false, fmt.Errorf("%w: %s", metadata.ErrUnknownField, fieldName)
		}
		normalizedValue, err := normalizeComparableValue(field, nextValue)
		if err != nil {
			return false, err
		}
		if !comparableValuesEqual(existing[fieldName], normalizedValue) {
			return true, nil
		}
	}
	return false, nil
}

func normalizeComparableValue(field metadata.Field, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch field.StorageType() {
	case "string":
		return fmt.Sprint(value), nil
	case "int":
		switch typed := value.(type) {
		case int:
			return int64(typed), nil
		case int64:
			return typed, nil
		case float64:
			if float64(int64(typed)) != typed {
				return nil, fmt.Errorf("expected integer, got %v", value)
			}
			return int64(typed), nil
		case json.Number:
			return typed.Int64()
		case string:
			parsed, err := json.Number(typed).Int64()
			if err != nil {
				return nil, err
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("expected integer, got %T", value)
		}
	case "float":
		switch typed := value.(type) {
		case int:
			return float64(typed), nil
		case int64:
			return float64(typed), nil
		case float64:
			return typed, nil
		case json.Number:
			return typed.Float64()
		case string:
			parsed, err := json.Number(typed).Float64()
			if err != nil {
				return nil, err
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("expected number, got %T", value)
		}
	default:
		return nil, fmt.Errorf("unsupported field type %q", field.StorageType())
	}
}

func comparableValuesEqual(left any, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return fmt.Sprint(left) == fmt.Sprint(right)
}

type fieldMutation struct {
	Created  []metadata.Field
	Restored []metadata.Field
	Existing []metadata.Field
	Fields   []metadata.Field
}

func fieldMutationResponse(mutation fieldMutation) map[string]any {
	return map[string]any{
		"created":  fieldsOutput(mutation.Created),
		"restored": fieldsOutput(mutation.Restored),
		"existing": fieldsOutput(mutation.Existing),
		"fields":   fieldsOutput(mutation.Fields),
	}
}

func fieldsInput(input map[string]any) ([]metadata.Field, error) {
	rawFields, ok := input["fields"]
	if !ok {
		return nil, errors.New("fields is required")
	}
	var fields []metadata.Field
	switch values := rawFields.(type) {
	case []any:
		fields = make([]metadata.Field, 0, len(values))
		for index, value := range values {
			field, err := fieldInput(value)
			if err != nil {
				return nil, fmt.Errorf("fields[%d]: %w", index, err)
			}
			fields = append(fields, field)
		}
	case []string:
		fields = make([]metadata.Field, 0, len(values))
		for _, value := range values {
			fields = append(fields, metadata.Field{Name: value, Type: "string"})
		}
	case map[string]any:
		fields = make([]metadata.Field, 0, len(values))
		for name, rawType := range values {
			fieldType := "string"
			if value, ok := rawType.(string); ok && strings.TrimSpace(value) != "" {
				fieldType = value
			}
			fields = append(fields, metadata.Field{Name: name, Type: fieldType})
		}
	default:
		return nil, errors.New("fields must be an array or object")
	}
	if len(fields) == 0 {
		return nil, errors.New("fields must not be empty")
	}
	for index := range fields {
		fields[index].Name = strings.TrimSpace(fields[index].Name)
		fields[index].Type = strings.TrimSpace(fields[index].Type)
		if fields[index].Type == "" {
			fields[index].Type = "string"
		}
		if fields[index].Name == "" {
			return nil, fmt.Errorf("fields[%d].name is required", index)
		}
		if strings.HasPrefix(fields[index].Name, "ct_") {
			return nil, fmt.Errorf("fields[%d].name %q uses reserved prefix ct_", index, fields[index].Name)
		}
		if reason := unsafeFieldNameReason(fields[index].Name); reason != "" {
			return nil, fmt.Errorf("fields[%d].name %q is unsafe: %s", index, fields[index].Name, reason)
		}
		if fields[index].Type != "string" && fields[index].Type != "int" && fields[index].Type != "float" {
			return nil, fmt.Errorf("fields[%d].type %q is unsupported", index, fields[index].Type)
		}
	}
	return fields, nil
}

func unsafeFieldNameReason(name string) string {
	for _, char := range name {
		if char == '.' {
			return "must not contain ."
		}
		if char == ';' {
			return "must not contain ;"
		}
		if char == '`' {
			return "must not contain `"
		}
		if char < 0x20 || char == 0x7f {
			return "must not contain control characters"
		}
	}
	return ""
}

func fieldInput(value any) (metadata.Field, error) {
	switch typed := value.(type) {
	case string:
		return metadata.Field{Name: typed, Type: "string"}, nil
	case map[string]any:
		field := metadata.Field{
			Name: stringInput(typed, "name"),
			Type: stringInput(typed, "type"),
		}
		return field, nil
	default:
		return metadata.Field{}, errors.New("field must be a string or object")
	}
}

// addTableFields creates or restores the requested fields. Without
// mayChange (anyone but an admin) it only confirms fields that already
// exist and fails before touching anything when one would be created or
// restored.
func (server *Server) addTableFields(ctx context.Context, dbName string, tableName string, fields []metadata.Field, mayChange bool) (fieldMutation, error) {
	if server.metadataPath == "" {
		return fieldMutation{}, errors.New("metadata writes are not configured")
	}
	server.catalogMu.Lock()
	defer server.catalogMu.Unlock()

	tableMeta, ok := server.catalog.Table(dbName, tableName)
	if !ok {
		return fieldMutation{}, fmt.Errorf("database %q table %q not found", dbName, tableName)
	}
	// The field slice is shared with the live catalog: edit a copy, so a
	// rejected or failed change leaves the catalog untouched.
	tableMeta.Fields = append([]metadata.Field(nil), tableMeta.Fields...)
	mutation := fieldMutation{}
	for _, field := range fields {
		existingIndex := -1
		var existing metadata.Field
		for index, candidate := range tableMeta.Fields {
			if strings.EqualFold(candidate.Name, field.Name) {
				existingIndex = index
				existing = candidate
				break
			}
		}
		if existingIndex == -1 {
			tableMeta.Fields = append(tableMeta.Fields, field)
			mutation.Created = append(mutation.Created, field)
			continue
		}
		if existing.Type != field.Type {
			return fieldMutation{}, fmt.Errorf("field %q already exists with type %q", field.Name, existing.Type)
		}
		if existing.Deleted {
			existing.Deleted = false
			tableMeta.Fields[existingIndex] = existing
			mutation.Restored = append(mutation.Restored, existing)
			continue
		}
		mutation.Existing = append(mutation.Existing, existing)
	}
	mutation.Fields = tableMeta.ActiveFields()
	if !mayChange && (len(mutation.Created) > 0 || len(mutation.Restored) > 0) {
		return fieldMutation{}, fmt.Errorf("%w: adding fields requires an admin", table.ErrPermissionDenied)
	}
	if len(mutation.Created) == 0 && len(mutation.Restored) == 0 {
		if err := server.tables.EnsureTable(ctx, server.catalog, dbName, tableName); err != nil {
			return fieldMutation{}, err
		}
		return mutation, nil
	}
	next, err := server.catalog.UpdateTable(dbName, tableName, tableMeta)
	if err != nil {
		return fieldMutation{}, err
	}
	if err := metadata.Save(server.metadataPath, next); err != nil {
		return fieldMutation{}, err
	}
	server.catalog = next
	if err := server.tables.EnsureTable(ctx, next, dbName, tableName); err != nil {
		return fieldMutation{}, err
	}
	return mutation, nil
}

func fieldsOutput(fields []metadata.Field) []map[string]any {
	output := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		output = append(output, map[string]any{
			"name": field.Name,
			"type": field.Type,
		})
	}
	return output
}

func stringInput(values map[string]any, key string) string {
	switch value := values[key].(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	default:
		return ""
	}
}
