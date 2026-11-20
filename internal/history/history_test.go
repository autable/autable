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

func TestDeleteLegacyWorkflowRuns(t *testing.T) {
	ctx := context.Background()
	store, err := OpenLevelDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	for _, key := range []string{"whistory_00000000000000000007_00000000000000001000", "whistory_00000000000000000008_00000000000000002000"} {
		if err := store.Put(ctx, key, []byte(`{"workflow_id":7,"steps":[]}`)); err != nil {
			t.Fatal(err)
		}
	}
	rowKey, err := SaveRowChange(ctx, store, RowChange{Database: "db", Table: "contacts", RecordID: 1, Values: map[string]any{"name": "kept"}})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := DeleteLegacyWorkflowRuns(ctx, store)
	if err != nil || deleted != 2 {
		t.Fatalf("expected both legacy runs deleted, got %d, %v", deleted, err)
	}
	if entries, _ := store.GetPrefix(ctx, "whistory_"); len(entries) != 0 {
		t.Fatalf("expected no legacy runs left, got %#v", entries)
	}
	if _, err := store.Get(ctx, rowKey); err != nil {
		t.Fatalf("expected row history to survive, got %v", err)
	}
	if deleted, err := DeleteLegacyWorkflowRuns(ctx, store); err != nil || deleted != 0 {
		t.Fatalf("expected a second cleanup to do nothing, got %d, %v", deleted, err)
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

}

func TestDeletePrefixBeforeRemovesOlderKeys(t *testing.T) {
	ctx := context.Background()
	stores := map[string]Store{
		"memory": NewMemoryStore(),
	}
	leveldbStore, err := OpenLevelDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := leveldbStore.Close(); err != nil {
			t.Fatal(err)
		}
	})
	stores["leveldb"] = leveldbStore

	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			for _, key := range []string{"a_7_1000", "a_7_2000", "a_7_3000", "a_8_1000"} {
				if err := store.Put(ctx, key, []byte("{}")); err != nil {
					t.Fatal(err)
				}
			}

			deleted, err := store.DeletePrefixBefore(ctx, "a_7_", "a_7_3000")
			if err != nil {
				t.Fatal(err)
			}
			if deleted != 2 {
				t.Fatalf("expected 2 deleted runs, got %d", deleted)
			}
			remaining, err := store.GetPrefix(ctx, "a_7_")
			if err != nil {
				t.Fatal(err)
			}
			if len(remaining) != 1 || remaining[0].Key != "a_7_3000" {
				t.Fatalf("expected only the cutoff run to remain, got %#v", remaining)
			}
			other, err := store.GetPrefix(ctx, "a_8_")
			if err != nil {
				t.Fatal(err)
			}
			if len(other) != 1 {
				t.Fatalf("expected workflow 8 history untouched, got %#v", other)
			}

			deleted, err = store.DeletePrefixBefore(ctx, "a_7_", "a_7_3000")
			if err != nil {
				t.Fatal(err)
			}
			if deleted != 0 {
				t.Fatalf("expected idempotent second delete, got %d", deleted)
			}
		})
	}
}

func TestLevelDBStoreCompactsAfterDeletion(t *testing.T) {
	ctx := context.Background()
	store, err := OpenLevelDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if err := store.Put(ctx, "a_1_1000", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeletePrefixBefore(ctx, "a_1_", "a_1_2000"); err != nil {
		t.Fatal(err)
	}
	if err := store.Compact(ctx); err != nil {
		t.Fatal(err)
	}
}
