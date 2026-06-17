package nodes

import (
	"context"

	"codetable/internal/workflow"
)

type TableRowRunner interface {
	RunWorkflowTableNode(ctx context.Context, kind string, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error)
}

type TableRowNode struct {
	runner TableRowRunner
	kind   string
}

func NewTableRowNode(runner TableRowRunner, kind string) TableRowNode {
	return TableRowNode{runner: runner, kind: kind}
}

func (node TableRowNode) Info() workflow.NodeInfo {
	switch node.kind {
	case "create":
		return workflow.NodeInfo{
			Type:          "table.row.create",
			DisplayName:   "Create row",
			Description:   "Creates a table row through the server table API using the workflow creator permissions.",
			Documentation: documentation("table.row.create"),
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
			Type:          "table.row.update",
			DisplayName:   "Update row",
			Description:   "Updates a table row through the server table API using the workflow creator permissions.",
			Documentation: documentation("table.row.update"),
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
			Type:          "table.row.delete",
			DisplayName:   "Delete row",
			Description:   "Deletes a table row through the server table API using the workflow creator permissions.",
			Documentation: documentation("table.row.delete"),
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
			Type:          "table.row.list",
			DisplayName:   "List rows",
			Description:   "Lists table rows through the server table API using the workflow creator permissions.",
			Documentation: documentation("table.row.list"),
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

func (node TableRowNode) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return node.runner.RunWorkflowTableNode(ctx, node.kind, input, info)
}

var _ workflow.Node = TableRowNode{}
