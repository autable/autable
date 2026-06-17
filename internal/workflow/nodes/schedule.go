package nodes

import (
	"context"
	"errors"

	"codetable/internal/workflow"
)

type ScheduleTriggerNode struct{}

func (ScheduleTriggerNode) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "time.schedule",
		DisplayName:   "Schedule",
		Description:   "Triggers a workflow from backend schedule ticks that match interval or daily parameters.",
		Documentation: documentation("time.schedule"),
		Inputs: []workflow.Port{
			{Name: "interval_ms", Type: "int64", Description: "Optional minimum interval between runs."},
			{Name: "daily_at", Type: "string", Description: "Optional UTC HH:mm daily run time."},
		},
		Outputs: []workflow.Port{
			{Name: "scheduled_at", Type: "int64"},
			{Name: "event", Type: "string"},
		},
		Stateless: true,
		Trigger:   true,
	}
}

func (ScheduleTriggerNode) RunTrigger(_ context.Context, _ map[string]any, event workflow.TriggerEvent, _ workflow.RuntimeInfo) (map[string]any, bool, error) {
	if event.Kind != "schedule" || event.ScheduledAt == 0 {
		return nil, false, nil
	}
	return map[string]any{"scheduled_at": event.ScheduledAt, "event": "schedule"}, true, nil
}

func (ScheduleTriggerNode) Run(_ context.Context, input map[string]any, _ workflow.RuntimeInfo) (map[string]any, error) {
	if input["scheduled_at"] == nil {
		return nil, errors.New("scheduled_at is required")
	}
	return map[string]any{"scheduled_at": input["scheduled_at"]}, nil
}

var _ workflow.TriggerNode = ScheduleTriggerNode{}
