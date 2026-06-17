package api

import (
	"context"
	"encoding/json"
	"errors"

	"codetable/internal/table"
	"codetable/internal/workflow"
)

type workflowTableNode struct {
	server *Server
	kind   string
}

func (node workflowTableNode) Info() workflow.NodeInfo {
	switch node.kind {
	case "create":
		return workflow.NodeInfo{
			Type:        "table.row.create",
			DisplayName: "Create row",
			Description: "Creates a table row through the server table API using the workflow creator permissions.",
			Inputs: []workflow.Port{
				{Name: "database", Type: "string"},
				{Name: "table", Type: "string"},
				{Name: "values", Type: "object"},
			},
			Outputs:   []workflow.Port{{Name: "record", Type: "RowRecord"}},
			Stateless: true,
		}
	case "update":
		return workflow.NodeInfo{
			Type:        "table.row.update",
			DisplayName: "Update row",
			Description: "Updates a table row through the server table API using the workflow creator permissions.",
			Inputs: []workflow.Port{
				{Name: "database", Type: "string"},
				{Name: "table", Type: "string"},
				{Name: "record_id", Type: "int64"},
				{Name: "values", Type: "object"},
			},
			Outputs:   []workflow.Port{{Name: "record", Type: "RowRecord"}},
			Stateless: true,
		}
	case "delete":
		return workflow.NodeInfo{
			Type:        "table.row.delete",
			DisplayName: "Delete row",
			Description: "Deletes a table row through the server table API using the workflow creator permissions.",
			Inputs: []workflow.Port{
				{Name: "database", Type: "string"},
				{Name: "table", Type: "string"},
				{Name: "record_id", Type: "int64"},
			},
			Outputs:   []workflow.Port{{Name: "record", Type: "RowRecord"}},
			Stateless: true,
		}
	default:
		return workflow.NodeInfo{
			Type:        "table.row.list",
			DisplayName: "List rows",
			Description: "Lists table rows through the server table API using the workflow creator permissions.",
			Inputs: []workflow.Port{
				{Name: "database", Type: "string"},
				{Name: "table", Type: "string"},
				{Name: "view", Type: "string"},
			},
			Outputs:   []workflow.Port{{Name: "rows", Type: "RowRecord[]"}},
			Stateless: true,
		}
	}
}

func (node workflowTableNode) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	if info.CreatorID == "" {
		return nil, errors.New("workflow creator is required")
	}
	dbName, tableName, err := workflowTableTarget(input, info)
	if err != nil {
		return nil, err
	}
	perms, err := node.server.system.EffectiveGrantsForSubject(ctx, info.CreatorID)
	if err != nil {
		return nil, err
	}
	catalog := node.server.catalogSnapshot()
	switch node.kind {
	case "create":
		values, err := workflowValuesInput(input)
		if err != nil {
			return nil, err
		}
		row, err := node.server.tables.CreateRow(ctx, catalog, perms, info.CreatorID, dbName, tableName, values)
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
		row, err := node.server.tables.UpdateRow(ctx, catalog, perms, info.CreatorID, dbName, tableName, recordID, values)
		return workflowRowOutput(row, err)
	case "delete":
		recordID, err := workflowRecordIDInput(input)
		if err != nil {
			return nil, err
		}
		row, err := node.server.tables.DeleteRow(ctx, catalog, perms, info.CreatorID, dbName, tableName, recordID)
		return workflowRowOutput(row, err)
	default:
		viewName, _ := input["view"].(string)
		rows, err := node.server.tables.Rows(ctx, catalog, perms, info.CreatorID, dbName, tableName, viewName)
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

func workflowRowOutput(row table.Row, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	return map[string]any{"record": workflowRowRecord(row)}, nil
}

func workflowRowRecord(row table.Row) map[string]any {
	return map[string]any{
		"record_id": row.RecordID,
		"values":    row.Values,
	}
}

func (server *Server) registerWorkflowTableNodes() {
	server.runner.Register(workflowTableNode{server: server, kind: "create"})
	server.runner.Register(workflowTableNode{server: server, kind: "update"})
	server.runner.Register(workflowTableNode{server: server, kind: "delete"})
	server.runner.Register(workflowTableNode{server: server, kind: "list"})
}

var _ workflow.Node = workflowTableNode{}
