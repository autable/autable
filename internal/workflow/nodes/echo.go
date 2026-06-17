package nodes

import (
	"context"

	"codetable/internal/workflow"
)

type EchoNode struct{}

func (EchoNode) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "echo",
		DisplayName:   "Echo",
		Description:   "Returns its input unchanged.",
		Documentation: documentation("echo"),
		Inputs:        []workflow.Port{{Name: "value", Type: "any"}},
		Outputs:       []workflow.Port{{Name: "value", Type: "any"}},
		Stateless:     true,
	}
}

func (EchoNode) Run(_ context.Context, input map[string]any, _ workflow.RuntimeInfo) (map[string]any, error) {
	return input, nil
}

var _ workflow.Node = EchoNode{}
