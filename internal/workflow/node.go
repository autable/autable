package workflow

import (
	"context"

	"autable/internal/history"
)

type NodeInfo struct {
	Type          string            `json:"type"`
	DisplayName   string            `json:"display_name"`
	Description   string            `json:"description,omitempty"`
	Documentation map[string]string `json:"documentation,omitempty"`
	Inputs        []Port            `json:"inputs"`
	Outputs       []Port            `json:"outputs"`
	Variables     []Port            `json:"variables,omitempty"`
	Secrets       []Port            `json:"secrets,omitempty"`
	Stateless     bool              `json:"stateless"`
	Trigger       bool              `json:"trigger"`
}

type Port struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type Node interface {
	Info() NodeInfo
	Run(ctx context.Context, input map[string]any, info RuntimeInfo) (map[string]any, error)
}

type TriggerNode interface {
	Node
	RunTrigger(ctx context.Context, params map[string]any, event TriggerEvent, info RuntimeInfo) (map[string]any, bool, error)
}

type TriggerEvent struct {
	Kind        string
	HistoryKey  string
	RowChange   history.RowChange
	ScheduledAt int64
}

type RuntimeInfo struct {
	WorkflowID   int64
	DatabaseName string
	RunID        string
	InstanceID   string
	NodeType     string
	CreatorID    string
	Secrets      map[string]string
	Variables    map[string]string
}

type TriggerRecord struct {
	HistoryKey string `json:"history_key"`
	Database   string `json:"database"`
	Table      string `json:"table"`
	RecordID   int64  `json:"record_id"`
	Timestamp  int64  `json:"timestamp"`
}
