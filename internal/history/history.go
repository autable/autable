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
	// DeletePrefixBefore deletes every key that starts with prefix and
	// sorts lexicographically below end, returning how many were deleted.
	DeletePrefixBefore(ctx context.Context, prefix string, end string) (int, error)
}

// Compacter is implemented by stores that can reclaim space after deletions.
type Compacter interface {
	Compact(ctx context.Context) error
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

func RowKey(database, table string, recordID int64, timestamp int64) string {
	return fmt.Sprintf("rhistory_%s_%s_%020d_%020d", clean(database), clean(table), recordID, timestamp)
}

func RowPrefix(database, table string, recordID int64) string {
	return fmt.Sprintf("rhistory_%s_%s_%020d_", clean(database), clean(table), recordID)
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

// legacyWorkflowRunPrefix starts the keys of node-workflow run history
// (workflows v1), which workflows v2 replaced with run records in the
// system database.
const legacyWorkflowRunPrefix = "whistory_"

// DeleteLegacyWorkflowRuns removes the run history of node workflows and
// compacts the store when anything was deleted. It runs at every startup
// and is a cheap no-op once the keys are gone (see docs/design/workflows-v2.md,
// migration 7 → 8).
func DeleteLegacyWorkflowRuns(ctx context.Context, store Store) (int, error) {
	deleted, err := store.DeletePrefixBefore(ctx, legacyWorkflowRunPrefix, legacyWorkflowRunPrefix+"\xff")
	if err != nil || deleted == 0 {
		return deleted, err
	}
	if compacter, ok := store.(Compacter); ok {
		if err := compacter.Compact(ctx); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
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
