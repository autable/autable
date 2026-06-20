package upsert

import (
	"context"

	"autable/internal/workflow"
	"autable/internal/workflow/nodes/autable"
)

type Node struct {
	service autable.Service
}

func NewNode(service autable.Service) Node {
	return Node{service: service}
}

func (node Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "table.row.upsert",
		DisplayName:   "Update or create row",
		Description:   "Updates the first row matching a field value, or creates a row when no match exists.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "database", Type: "string"},
			{Name: "table", Type: "string"},
			{Name: "match_field", Type: "string"},
			{Name: "values", Type: "object"},
		},
		Outputs: []workflow.Port{
			{Name: "record", Type: "RowRecord"},
			{Name: "operation", Type: "string"},
		},
		Stateless: true,
	}
}

func (node Node) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return node.service.UpsertRow(ctx, input, info)
}

var _ workflow.Node = Node{}
