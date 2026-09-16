package get

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
		Type:          "user.get",
		DisplayName:   "Get user",
		Description:   "Reads one Autable user by id, typically the actor_id a trigger reports, to learn who acted.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "user_id", Type: "string", Description: "The Autable user id, e.g. the actor_id of a row change."},
		},
		Outputs: []workflow.Port{
			{Name: "id", Type: "string", Description: "The user id that was read."},
			{Name: "email", Type: "string", Description: "The user's normalized (lower-case) email."},
			{Name: "display_name", Type: "string", Description: "The user's display name."},
			{Name: "provider", Type: "string", Description: "How the user signs in: password or oidc."},
			{Name: "provider_name", Type: "string", Description: "The provider the account came from: password, or the configured OIDC provider name."},
		},
		Stateless: true,
	}
}

func (node Node) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	return node.service.GetUser(ctx, input, info)
}

var _ workflow.Node = Node{}
