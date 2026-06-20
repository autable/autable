package create

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
		Type:          "table.row.create",
		DisplayName:   "Create row",
		Description:   "Creates a table row through the server table API using the workflow creator permissions.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "database", Type: "string"},
			{Name: "table", Type: "string"},
			{Name: "values", Type: "object"},
		},
		Outputs:   []workflow.Port{{Name: "record", Type: "RowRecord"}},
		Stateless: true,
	}
}

func (node Node) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return node.service.CreateRow(ctx, input, info)
}

var _ workflow.Node = Node{}
