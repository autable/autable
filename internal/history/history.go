package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("history entry not found")

type Store interface {
	Put(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) (Entry, error)
	GetPrefix(ctx context.Context, prefix string) ([]Entry, error)
}

type Entry struct {
	Key   string
	Value []byte
}

type RowChange struct {
	Database  string         `json:"database"`
	Table     string         `json:"table"`
	RecordID  int64          `json:"record_id"`
	Timestamp time.Time      `json:"timestamp"`
	Values    map[string]any `json:"values"`
	ActorID   string         `json:"actor_id,omitempty"`
}

type WorkflowRun struct {
	WorkflowID int64          `json:"workflow_id"`
	Timestamp  time.Time      `json:"timestamp"`
	Inputs     map[string]any `json:"inputs,omitempty"`
	Outputs    map[string]any `json:"outputs,omitempty"`
	Steps      []StepRecord   `json:"steps"`
	Error      string         `json:"error,omitempty"`
}

type StepRecord struct {
	NodeID string         `json:"node_id"`
	Input  map[string]any `json:"input,omitempty"`
	Output map[string]any `json:"output,omitempty"`
	Error  string         `json:"error,omitempty"`
}

func RowKey(database, table string, recordID int64, ts time.Time) string {
	return fmt.Sprintf("rhistory_%s_%s_%020d_%020d", clean(database), clean(table), recordID, ts.UTC().UnixNano())
}

func RowPrefix(database, table string, recordID int64) string {
	return fmt.Sprintf("rhistory_%s_%s_%020d_", clean(database), clean(table), recordID)
}

func WorkflowKey(workflowID int64, ts time.Time) string {
	return fmt.Sprintf("whistory_%020d_%020d", workflowID, ts.UTC().UnixNano())
}

func WorkflowPrefix(workflowID int64) string {
	return fmt.Sprintf("whistory_%020d_", workflowID)
}

func SaveRowChange(ctx context.Context, store Store, change RowChange) (string, error) {
	if change.Timestamp.IsZero() {
		change.Timestamp = time.Now().UTC()
	}
	key := RowKey(change.Database, change.Table, change.RecordID, change.Timestamp)
	value, err := json.Marshal(change)
	if err != nil {
		return "", err
	}
	return key, store.Put(ctx, key, value)
}

func SaveWorkflowRun(ctx context.Context, store Store, run WorkflowRun) (string, error) {
	if run.Timestamp.IsZero() {
		run.Timestamp = time.Now().UTC()
	}
	key := WorkflowKey(run.WorkflowID, run.Timestamp)
	value, err := json.Marshal(run)
	if err != nil {
		return "", err
	}
	return key, store.Put(ctx, key, value)
}

func DecodeRowChange(entry Entry) (RowChange, error) {
	var change RowChange
	err := json.Unmarshal(entry.Value, &change)
	return change, err
}

func DecodeWorkflowRun(entry Entry) (WorkflowRun, error) {
	var run WorkflowRun
	err := json.Unmarshal(entry.Value, &run)
	return run, err
}

func clean(value string) string {
	return strings.NewReplacer("_", "-", "/", "-", "\\", "-").Replace(value)
}

func ParseRecordIDFromRowKey(key string) (int64, error) {
	parts := strings.Split(key, "_")
	if len(parts) < 5 || parts[0] != "rhistory" {
		return 0, fmt.Errorf("invalid row history key %q", key)
	}
	return strconv.ParseInt(parts[len(parts)-2], 10, 64)
}
