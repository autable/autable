package workflow

import (
	"context"
	"errors"
	"fmt"

	"codetable/internal/history"
)

type RecordChangedTriggerNode struct {
	store history.Store
}

func NewRecordChangedTriggerNode(store history.Store) RecordChangedTriggerNode {
	return RecordChangedTriggerNode{store: store}
}

func (node RecordChangedTriggerNode) Info() NodeInfo {
	return NodeInfo{
		Type:        "table.record.changed",
		DisplayName: "Record changed",
		Description: "Loads a row history entry by rhistory key and exposes it as a trigger record.",
		Inputs: []Port{{
			Name: "history_key",
			Type: "string",
		}},
		Outputs: []Port{
			{Name: "record", Type: "TriggerRecord"},
			{Name: "values", Type: "object"},
			{Name: "diff", Type: "object"},
			{Name: "actor_id", Type: "string"},
		},
		Stateless: true,
		Trigger:   true,
	}
}

func (node RecordChangedTriggerNode) Run(ctx context.Context, input map[string]any, _ RuntimeInfo) (map[string]any, error) {
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
	record := TriggerRecord{
		HistoryKey: historyKey,
		Database:   change.Database,
		Table:      change.Table,
		RecordID:   change.RecordID,
		Timestamp:  change.Timestamp.UTC().UnixMilli(),
	}
	return map[string]any{
		"record":   record,
		"values":   change.Values,
		"diff":     change.Diff,
		"actor_id": change.ActorID,
	}, nil
}
