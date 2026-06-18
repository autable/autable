package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"codetable/internal/metadata"
	"codetable/internal/permission"
	"codetable/internal/table"
	"codetable/internal/workflow"
	"codetable/internal/workflow/nodes"
)

type workflowFieldMutation struct {
	Created  []metadata.Field
	Restored []metadata.Field
	Existing []metadata.Field
	Fields   []metadata.Field
}

func (server *Server) RunWorkflowTableFieldNode(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	if info.CreatorID == "" {
		return nil, errors.New("workflow creator is required")
	}
	dbName, tableName, err := workflowTableTarget(input, info)
	if err != nil {
		return nil, err
	}
	fields, err := workflowFieldsInput(input)
	if err != nil {
		return nil, err
	}
	perms, err := server.system.EffectiveGrantsForSubject(ctx, info.CreatorID)
	if err != nil {
		return nil, err
	}
	resource := dbName + "." + tableName
	if perms.ResourceLevel(info.CreatorID, permission.ScopeDatabase, dbName) < permission.Write &&
		perms.ResourceLevel(info.CreatorID, permission.ScopeTable, resource) < permission.Write {
		return nil, table.ErrPermissionDenied
	}
	mutation, err := server.addTableFields(ctx, dbName, tableName, fields)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"created":  workflowFieldsOutput(mutation.Created),
		"restored": workflowFieldsOutput(mutation.Restored),
		"existing": workflowFieldsOutput(mutation.Existing),
		"fields":   workflowFieldsOutput(mutation.Fields),
	}, nil
}

func workflowFieldsInput(input map[string]any) ([]metadata.Field, error) {
	rawFields, ok := input["fields"]
	if !ok {
		return nil, errors.New("fields is required")
	}
	var fields []metadata.Field
	switch values := rawFields.(type) {
	case []any:
		fields = make([]metadata.Field, 0, len(values))
		for index, value := range values {
			field, err := workflowFieldInput(value)
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
		if fields[index].Type != "string" && fields[index].Type != "int" && fields[index].Type != "float" {
			return nil, fmt.Errorf("fields[%d].type %q is unsupported", index, fields[index].Type)
		}
	}
	return fields, nil
}

func workflowFieldInput(value any) (metadata.Field, error) {
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

func (server *Server) addTableFields(ctx context.Context, dbName string, tableName string, fields []metadata.Field) (workflowFieldMutation, error) {
	if server.metadataPath == "" {
		return workflowFieldMutation{}, errors.New("metadata writes are not configured")
	}
	server.catalogMu.Lock()
	defer server.catalogMu.Unlock()

	tableMeta, ok := server.catalog.Table(dbName, tableName)
	if !ok {
		return workflowFieldMutation{}, fmt.Errorf("database %q table %q not found", dbName, tableName)
	}
	mutation := workflowFieldMutation{}
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
			return workflowFieldMutation{}, fmt.Errorf("field %q already exists with type %q", field.Name, existing.Type)
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
	if len(mutation.Created) == 0 && len(mutation.Restored) == 0 {
		if err := server.tables.EnsureTable(ctx, server.catalog, dbName, tableName); err != nil {
			return workflowFieldMutation{}, err
		}
		return mutation, nil
	}
	next, err := server.catalog.UpdateTable(dbName, tableName, tableMeta)
	if err != nil {
		return workflowFieldMutation{}, err
	}
	if err := metadata.Save(server.metadataPath, next); err != nil {
		return workflowFieldMutation{}, err
	}
	server.catalog = next
	if err := server.tables.EnsureTable(ctx, next, dbName, tableName); err != nil {
		return workflowFieldMutation{}, err
	}
	return mutation, nil
}

func workflowFieldsOutput(fields []metadata.Field) []map[string]any {
	output := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		output = append(output, map[string]any{
			"name": field.Name,
			"type": field.Type,
		})
	}
	return output
}

func (server *Server) registerWorkflowTableFieldNodes() {
	server.runner.Register(nodes.NewTableFieldNode(server))
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
