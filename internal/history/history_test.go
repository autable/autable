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
	ts1 := time.Unix(10, 0).UTC().UnixMilli()
	ts2 := time.Unix(11, 0).UTC().UnixMilli()
	ts3 := time.Unix(12, 0).UTC().UnixMilli()

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
	if _, err := SaveRowChange(ctx, store, RowChange{Database: "db", Table: "contacts", RecordID: 42, Timestamp: ts3, Values: map[string]any{"name": "third"}}); err != nil {
		t.Fatal(err)
	}

	entries, err := store.GetPrefix(ctx, RowPrefix("db", "contacts", 42))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected three entries, got %d", len(entries))
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
	limited, err := store.GetPrefixLimit(ctx, RowPrefix("db", "contacts", 42), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 {
		t.Fatalf("expected two limited entries, got %d", len(limited))
	}
	limitedFirst, err := DecodeRowChange(limited[0])
	if err != nil {
		t.Fatal(err)
	}
	limitedSecond, err := DecodeRowChange(limited[1])
	if err != nil {
		t.Fatal(err)
	}
	if limitedFirst.Values["name"] != "second" || limitedSecond.Values["name"] != "third" {
		t.Fatalf("expected latest entries in key order, got %#v %#v", limitedFirst.Values, limitedSecond.Values)
	}
	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWorkflowHistoryKey(t *testing.T) {
	key := WorkflowKey(7, time.Unix(20, 0).UTC().UnixMilli())
	if key != "whistory_00000000000000000007_00000000000000020000" {
		t.Fatalf("unexpected key: %s", key)
	}
}

func TestHistoryTimestampsStayMillisecondIntsWithoutOverwriting(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	ts := time.UnixMilli(1781604000000).UTC().UnixMilli()

	firstRowKey, err := SaveRowChange(ctx, store, RowChange{Database: "db", Table: "contacts", RecordID: 42, Timestamp: ts, Values: map[string]any{"name": "first"}})
	if err != nil {
		t.Fatal(err)
	}
	secondRowKey, err := SaveRowChange(ctx, store, RowChange{Database: "db", Table: "contacts", RecordID: 42, Timestamp: ts, Values: map[string]any{"name": "second"}})
	if err != nil {
		t.Fatal(err)
	}
	if firstRowKey == secondRowKey {
		t.Fatalf("expected unique millisecond row history keys, got %q", firstRowKey)
	}
	rowEntries, err := store.GetPrefix(ctx, RowPrefix("db", "contacts", 42))
	if err != nil {
		t.Fatal(err)
	}
	if len(rowEntries) != 2 {
		t.Fatalf("expected two row history entries, got %d", len(rowEntries))
	}
	secondRow, err := DecodeRowChange(rowEntries[1])
	if err != nil {
		t.Fatal(err)
	}
	if secondRow.Timestamp != ts+1 {
		t.Fatalf("expected second row timestamp to increment by 1 ms, got %d", secondRow.Timestamp)
	}

	firstWorkflowKey, err := SaveWorkflowRun(ctx, store, WorkflowRun{WorkflowID: 7, Timestamp: ts, Steps: []StepRecord{}})
	if err != nil {
		t.Fatal(err)
	}
	secondWorkflowKey, err := SaveWorkflowRun(ctx, store, WorkflowRun{WorkflowID: 7, Timestamp: ts, Steps: []StepRecord{}})
	if err != nil {
		t.Fatal(err)
	}
	if firstWorkflowKey == secondWorkflowKey {
		t.Fatalf("expected unique millisecond workflow history keys, got %q", firstWorkflowKey)
	}
	workflowEntries, err := store.GetPrefix(ctx, WorkflowPrefix(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(workflowEntries) != 2 {
		t.Fatalf("expected two workflow history entries, got %d", len(workflowEntries))
	}
	secondWorkflow, err := DecodeWorkflowRun(workflowEntries[1])
	if err != nil {
		t.Fatal(err)
	}
	if secondWorkflow.Timestamp != ts+1 {
		t.Fatalf("expected second workflow timestamp to increment by 1 ms, got %d", secondWorkflow.Timestamp)
	}
}
