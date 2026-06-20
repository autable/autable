package echo

import (
	"context"

	"autable/internal/workflow"
)

type Node struct{}

func (Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "echo",
		DisplayName:   "Echo",
		Description:   "Returns its input unchanged.",
		Documentation: Documentation(),
		Inputs:        []workflow.Port{{Name: "value", Type: "any"}},
		Outputs:       []workflow.Port{{Name: "value", Type: "any"}},
		Stateless:     true,
	}
}

func (Node) Run(_ context.Context, input map[string]any, _ workflow.RuntimeInfo) (map[string]any, error) {
	return input, nil
}

var _ workflow.Node = Node{}
