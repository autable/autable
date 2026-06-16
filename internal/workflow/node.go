package workflow

import "context"

type NodeInfo struct {
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
	Inputs      []Port `json:"inputs"`
	Outputs     []Port `json:"outputs"`
	Stateless   bool   `json:"stateless"`
	Trigger     bool   `json:"trigger"`
}

type Port struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

type Node interface {
	Info() NodeInfo
	Run(ctx context.Context, input map[string]any, info RuntimeInfo) (map[string]any, error)
}

type RuntimeInfo struct {
	WorkflowID int64
	RunID      string
	Secrets    map[string]string
	Variables  map[string]string
}

type TriggerRecord struct {
	HistoryKey string `json:"history_key"`
	Database   string `json:"database"`
	Table      string `json:"table"`
	RecordID   int64  `json:"record_id"`
	Timestamp  int64  `json:"timestamp"`
}
