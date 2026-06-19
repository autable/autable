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
)

type workflowCodeTableService struct {
	server *Server
}

func (server *Server) workflowCodeTableService() workflowCodeTableService {
	return workflowCodeTableService{server: server}
}

func (service workflowCodeTableService) CreateRow(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.runRow(ctx, "create", input, info)
}

func (service workflowCodeTableService) UpdateRow(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.runRow(ctx, "update", input, info)
}

func (service workflowCodeTableService) UpsertRow(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.runRow(ctx, "upsert", input, info)
}

func (service workflowCodeTableService) DeleteRow(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.runRow(ctx, "delete", input, info)
}

func (service workflowCodeTableService) ListRows(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return service.runRow(ctx, "list", input, info)
}

func (service workflowCodeTableService) runRow(ctx context.Context, kind string, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	if info.CreatorID == "" {
		return nil, errors.New("workflow creator is required")
	}
	server := service.server
	dbName, tableName, err := workflowTableTarget(input, info)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "create":
		values, err := workflowValuesInput(input)
		if err != nil {
			return nil, err
		}
		row, err := server.createTableRowAs(ctx, info.CreatorID, dbName, tableName, values)
		return workflowRowOutput(row, err)
	case "update":
		recordID, err := workflowRecordIDInput(input)
		if err != nil {
			return nil, err
		}
		values, err := workflowValuesInput(input)
		if err != nil {
			return nil, err
		}
		row, err := server.updateTableRowAs(ctx, info.CreatorID, dbName, tableName, recordID, values)
		return workflowRowOutput(row, err)
	case "upsert":
		values, err := workflowValuesInput(input)
		if err != nil {
			return nil, err
		}
		matchField, err := workflowMatchFieldInput(input, values)
		if err != nil {
			return nil, err
		}
		row, operation, err := server.upsertTableRowAs(ctx, info.CreatorID, dbName, tableName, matchField, values)
		return workflowRowMutationOutput(row, operation, err)
	case "delete":
		recordID, err := workflowRecordIDInput(input)
		if err != nil {
			return nil, err
		}
		row, err := server.deleteTableRowAs(ctx, info.CreatorID, dbName, tableName, recordID)
		return workflowRowOutput(row, err)
	default:
		viewName, _ := input["view"].(string)
		rows, err := server.listTableRowsAs(ctx, info.CreatorID, dbName, tableName, viewName)
		if err != nil {
			return nil, err
		}
		output := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			output = append(output, workflowRowRecord(row))
		}
		return map[string]any{"rows": output}, nil
	}
}

func workflowTableTarget(input map[string]any, info workflow.RuntimeInfo) (string, string, error) {
	dbName := info.DatabaseName
	if value, ok := input["database"].(string); ok && value != "" {
		dbName = value
	}
	tableName, _ := input["table"].(string)
	if dbName == "" {
		return "", "", errors.New("database is required")
	}
	if tableName == "" {
		return "", "", errors.New("table is required")
	}
	return dbName, tableName, nil
}

func workflowValuesInput(input map[string]any) (map[string]any, error) {
	values, ok := input["values"].(map[string]any)
	if !ok {
		return nil, errors.New("values is required")
	}
	return values, nil
}

func workflowMatchFieldInput(input map[string]any, values map[string]any) (string, error) {
	matchField, _ := input["match_field"].(string)
	if matchField == "" {
		return "", errors.New("match_field is required")
	}
	if _, ok := values[matchField]; !ok {
		return "", fmt.Errorf("values.%s is required", matchField)
	}
	return matchField, nil
}

func workflowRecordIDInput(input map[string]any) (int64, error) {
	switch value := input["record_id"].(type) {
	case int:
		return int64(value), nil
	case int64:
		return value, nil
	case float64:
		return int64(value), nil
	case json.Number:
		return value.Int64()
	default:
		return 0, errors.New("record_id is required")
	}
}

func (server *Server) upsertTableRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID string, isDatabaseOwner bool, dbName string, tableName string, matchField string, values map[string]any) (table.Row, string, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return table.Row{}, "", fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	matchFieldMeta, ok := tableMeta.Field(matchField)
	if !ok || strings.HasPrefix(matchFieldMeta.Name, "ct_") {
		return table.Row{}, "", fmt.Errorf("%w: %s", metadata.ErrUnknownField, matchField)
	}
	matchValue, err := workflowNormalizeComparableValue(matchFieldMeta, values[matchField])
	if err != nil {
		return table.Row{}, "", err
	}
	rows, err := server.tables.Rows(ctx, catalog, perms, actorID, isDatabaseOwner, dbName, tableName, "")
	if err != nil {
		return table.Row{}, "", err
	}
	for _, row := range rows {
		if workflowValuesEqual(row.Values[matchField], matchValue) {
			changed, err := workflowRowValuesChanged(tableMeta, row.Values, values)
			if err != nil {
				return table.Row{}, "", err
			}
			if !changed {
				return row, "noop", nil
			}
			updated, err := server.tables.UpdateRow(ctx, catalog, perms, actorID, isDatabaseOwner, dbName, tableName, row.RecordID, values)
			return updated, "update", err
		}
	}
	created, err := server.tables.CreateRow(ctx, catalog, perms, actorID, isDatabaseOwner, dbName, tableName, values)
	return created, "create", err
}

func workflowRowValuesChanged(tableMeta metadata.Table, existing map[string]any, values map[string]any) (bool, error) {
	for fieldName, nextValue := range values {
		field, ok := tableMeta.Field(fieldName)
		if !ok {
			return false, fmt.Errorf("%w: %s", metadata.ErrUnknownField, fieldName)
		}
		normalizedValue, err := workflowNormalizeComparableValue(field, nextValue)
		if err != nil {
			return false, err
		}
		if !workflowValuesEqual(existing[fieldName], normalizedValue) {
			return true, nil
		}
	}
	return false, nil
}

func workflowNormalizeComparableValue(field metadata.Field, value any) (any, error) {
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

func workflowValuesEqual(left any, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return fmt.Sprint(left) == fmt.Sprint(right)
}

func workflowRowOutput(row table.Row, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	return map[string]any{"record": workflowRowRecord(row)}, nil
}

func workflowRowMutationOutput(row table.Row, operation string, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	return map[string]any{"record": workflowRowRecord(row), "operation": operation}, nil
}

func workflowRowRecord(row table.Row) map[string]any {
	return map[string]any{
		"record_id": row.RecordID,
		"values":    row.Values,
	}
}
