package field

import (
	"context"

	"autable/internal/workflow"
	"autable/internal/workflow/nodes/autable"
)

type CreateNode struct {
	service autable.Service
}

func NewCreateNode(service autable.Service) CreateNode {
	return CreateNode{service: service}
}

func (node CreateNode) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "table.field.create",
		DisplayName:   "Create table fields",
		Description:   "Adds missing fields to a table through the server metadata API using the workflow creator permissions.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "database", Type: "string", Description: "Optional database name. Defaults to the workflow database."},
			{Name: "table", Type: "string", Description: "Target table name."},
			{Name: "fields", Type: "string[] | object[]", Description: "Fields to ensure. Strings default to string fields; objects support name and type."},
		},
		Outputs: []workflow.Port{
			{Name: "created", Type: "Field[]"},
			{Name: "restored", Type: "Field[]"},
			{Name: "existing", Type: "Field[]"},
			{Name: "fields", Type: "Field[]"},
		},
		Stateless: true,
	}
}

func (node CreateNode) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return node.service.CreateFields(ctx, input, info)
}

var _ workflow.Node = CreateNode{}
