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
	GetPrefixLimit(ctx context.Context, prefix string, limit int) ([]Entry, error)
}

type Entry struct {
	Key   string
	Value []byte
}

type RowChange struct {
	Database  string         `json:"database"`
	Table     string         `json:"table"`
	RecordID  int64          `json:"record_id"`
	Timestamp int64          `json:"timestamp"`
	Operation string         `json:"operation,omitempty"`
	Values    map[string]any `json:"values"`
	Diff      RowDiff        `json:"diff,omitempty"`
	ActorID   string         `json:"actor_id,omitempty"`
}

type RowDiff map[string]FieldDiff

type FieldDiff struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type WorkflowRun struct {
	WorkflowID int64          `json:"workflow_id"`
	Timestamp  int64          `json:"timestamp"`
	Inputs     map[string]any `json:"inputs,omitempty"`
	Outputs    map[string]any `json:"outputs,omitempty"`
	Steps      []StepRecord   `json:"steps"`
	Error      string         `json:"error,omitempty"`
}

type StepRecord struct {
	NodeID   string         `json:"node_id"`
	NodeType string         `json:"node_type,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
	Output   map[string]any `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
}

func RowKey(database, table string, recordID int64, timestamp int64) string {
	return fmt.Sprintf("rhistory_%s_%s_%020d_%020d", clean(database), clean(table), recordID, timestamp)
}

func RowPrefix(database, table string, recordID int64) string {
	return fmt.Sprintf("rhistory_%s_%s_%020d_", clean(database), clean(table), recordID)
}

func WorkflowKey(workflowID int64, timestamp int64) string {
	return fmt.Sprintf("whistory_%020d_%020d", workflowID, timestamp)
}

func WorkflowPrefix(workflowID int64) string {
	return fmt.Sprintf("whistory_%020d_", workflowID)
}

func SaveRowChange(ctx context.Context, store Store, change RowChange) (string, error) {
	if change.Timestamp == 0 {
		change.Timestamp = time.Now().UTC().UnixMilli()
	}
	key, timestamp, err := uniqueHistoryKey(ctx, store, change.Timestamp, func(timestamp int64) string {
		return RowKey(change.Database, change.Table, change.RecordID, timestamp)
	})
	if err != nil {
		return "", err
	}
	change.Timestamp = timestamp
	value, err := json.Marshal(change)
	if err != nil {
		return "", err
	}
	return key, store.Put(ctx, key, value)
}

func SaveWorkflowRun(ctx context.Context, store Store, run WorkflowRun) (string, error) {
	if run.Timestamp == 0 {
		run.Timestamp = time.Now().UTC().UnixMilli()
	}
	key, timestamp, err := uniqueHistoryKey(ctx, store, run.Timestamp, func(timestamp int64) string {
		return WorkflowKey(run.WorkflowID, timestamp)
	})
	if err != nil {
		return "", err
	}
	run.Timestamp = timestamp
	value, err := json.Marshal(run)
	if err != nil {
		return "", err
	}
	return key, store.Put(ctx, key, value)
}

func uniqueHistoryKey(ctx context.Context, store Store, timestamp int64, keyForTimestamp func(int64) string) (string, int64, error) {
	for {
		key := keyForTimestamp(timestamp)
		if _, err := store.Get(ctx, key); errors.Is(err, ErrNotFound) {
			return key, timestamp, nil
		} else if err != nil {
			return "", 0, err
		}
		timestamp++
	}
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
