package workflow

import (
	"context"
	"errors"
)

type ScheduleTriggerNode struct{}

func (ScheduleTriggerNode) Info() NodeInfo {
	return NodeInfo{
		Type:        "time.schedule",
		DisplayName: "Schedule",
		Description: "Exposes a backend schedule tick as a workflow trigger event.",
		Inputs: []Port{{
			Name: "scheduled_at",
			Type: "int64",
		}},
		Outputs: []Port{{
			Name: "scheduled_at",
			Type: "int64",
		}},
		Stateless: true,
		Trigger:   true,
	}
}

func (ScheduleTriggerNode) Run(_ context.Context, input map[string]any, _ RuntimeInfo) (map[string]any, error) {
	if input["scheduled_at"] == nil {
		return nil, errors.New("scheduled_at is required")
	}
	return map[string]any{"scheduled_at": input["scheduled_at"]}, nil
}
