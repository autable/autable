package history

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRowHistoryKeysSupportPrefixScan(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	ts1 := time.Unix(10, 0).UTC()
	ts2 := time.Unix(11, 0).UTC()

	secondKey, err := SaveRowChange(ctx, store, RowChange{Database: "db", Table: "contacts", RecordID: 42, Timestamp: ts2, Values: map[string]any{"name": "second"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRowChange(ctx, store, RowChange{Database: "db", Table: "contacts", RecordID: 42, Timestamp: ts1, Values: map[string]any{"name": "first"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRowChange(ctx, store, RowChange{Database: "db", Table: "contacts", RecordID: 43, Timestamp: ts1, Values: map[string]any{"name": "other"}}); err != nil {
		t.Fatal(err)
	}

	entries, err := store.GetPrefix(ctx, RowPrefix("db", "contacts", 42))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected two entries, got %d", len(entries))
	}
	first, err := DecodeRowChange(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Values["name"] != "first" {
		t.Fatalf("expected sorted history by key timestamp, got %#v", first.Values)
	}
	exact, err := store.Get(ctx, secondKey)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DecodeRowChange(exact)
	if err != nil {
		t.Fatal(err)
	}
	if second.Values["name"] != "second" {
		t.Fatalf("unexpected exact history entry: %#v", second.Values)
	}
	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWorkflowHistoryKey(t *testing.T) {
	key := WorkflowKey(7, time.Unix(20, 0).UTC())
	if key != "whistory_00000000000000000007_00000000020000000000" {
		t.Fatalf("unexpected key: %s", key)
	}
}
