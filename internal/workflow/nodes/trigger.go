package nodes

import (
	"context"
	"errors"
	"fmt"

	"codetable/internal/history"
	"codetable/internal/workflow"
)

type RecordChangedTriggerNode struct {
	store history.Store
}

func NewRecordChangedTriggerNode(store history.Store) RecordChangedTriggerNode {
	return RecordChangedTriggerNode{store: store}
}

func (node RecordChangedTriggerNode) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "table.record.changed",
		DisplayName:   "Record changed",
		Description:   "Triggers a workflow from row history events that match the configured table, operations, and fields.",
		Documentation: documentation("table.record.changed"),
		Inputs: []workflow.Port{
			{Name: "table", Type: "string", Description: "Optional table name to listen to."},
			{Name: "operations", Type: "string[]", Description: "Optional create, update, or delete operation filter."},
			{Name: "fields", Type: "string[]", Description: "Optional changed field filter."},
		},
		Outputs: []workflow.Port{
			{Name: "history_key", Type: "string"},
			{Name: "database", Type: "string"},
			{Name: "table", Type: "string"},
			{Name: "record_id", Type: "int64"},
			{Name: "operation", Type: "string"},
			{Name: "record", Type: "TriggerRecord"},
			{Name: "values", Type: "object"},
			{Name: "diff", Type: "object"},
			{Name: "actor_id", Type: "string"},
		},
		Stateless: true,
		Trigger:   true,
	}
}

func (node RecordChangedTriggerNode) RunTrigger(_ context.Context, params map[string]any, event workflow.TriggerEvent, _ workflow.RuntimeInfo) (map[string]any, bool, error) {
	change := event.RowChange
	if event.Kind != "row_change" {
		return nil, false, nil
	}
	if tableName, ok := triggerStringParam(params, "table"); ok && tableName != change.Table {
		return nil, false, nil
	}
	if operations := triggerStringSetParam(params, "operations"); len(operations) > 0 {
		if _, ok := operations[change.Operation]; !ok {
			return nil, false, nil
		}
	}
	if fields := triggerStringSetParam(params, "fields"); len(fields) > 0 {
		matched := false
		for field := range change.Diff {
			if _, ok := fields[field]; ok {
				matched = true
				break
			}
		}
		if !matched {
			return nil, false, nil
		}
	}
	return rowChangeOutput(event.HistoryKey, change), true, nil
}

func (node RecordChangedTriggerNode) Run(ctx context.Context, input map[string]any, _ workflow.RuntimeInfo) (map[string]any, error) {
	historyKey, ok := input["history_key"].(string)
	if !ok || historyKey == "" {
		return nil, errors.New("history_key is required")
	}
	entry, err := node.store.Get(ctx, historyKey)
	if err != nil {
		return nil, fmt.Errorf("load row history: %w", err)
	}
	change, err := history.DecodeRowChange(entry)
	if err != nil {
		return nil, err
	}
	return rowChangeOutput(historyKey, change), nil
}

func rowChangeOutput(historyKey string, change history.RowChange) map[string]any {
	record := workflow.TriggerRecord{
		HistoryKey: historyKey,
		Database:   change.Database,
		Table:      change.Table,
		RecordID:   change.RecordID,
		Timestamp:  change.Timestamp,
	}
	return map[string]any{
		"history_key": historyKey,
		"database":    change.Database,
		"table":       change.Table,
		"record_id":   change.RecordID,
		"operation":   change.Operation,
		"record":      record,
		"values":      change.Values,
		"diff":        change.Diff,
		"actor_id":    change.ActorID,
	}
}

func triggerStringParam(params map[string]any, key string) (string, bool) {
	value, ok := params[key].(string)
	return value, ok && value != ""
}

func triggerStringSetParam(params map[string]any, key string) map[string]struct{} {
	raw, ok := params[key]
	if !ok {
		return nil
	}
	values := map[string]struct{}{}
	switch typed := raw.(type) {
	case []any:
		for _, item := range typed {
			if value, ok := item.(string); ok && value != "" {
				values[value] = struct{}{}
			}
		}
	case []string:
		for _, value := range typed {
			if value != "" {
				values[value] = struct{}{}
			}
		}
	case string:
		if typed != "" {
			values[typed] = struct{}{}
		}
	}
	return values
}

var _ workflow.TriggerNode = RecordChangedTriggerNode{}
