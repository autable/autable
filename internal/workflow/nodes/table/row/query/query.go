package query

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
		Type:          "table.row.query",
		DisplayName:   "Query rows",
		Description:   "Queries table rows with view, filter, sort, and limit options through the server table API using the workflow creator permissions.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "database", Type: "string"},
			{Name: "table", Type: "string"},
			{Name: "view", Type: "string"},
			{Name: "query", Type: "object"},
			{Name: "sorts", Type: "object[]"},
			{Name: "limit", Type: "int"},
			{Name: "offset", Type: "int"},
			{Name: "search", Type: "string"},
		},
		Outputs:   []workflow.Port{{Name: "rows", Type: "RowRecord[]"}},
		Stateless: true,
	}
}

func (node Node) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return node.service.ListRows(ctx, input, info)
}

var _ workflow.Node = Node{}
