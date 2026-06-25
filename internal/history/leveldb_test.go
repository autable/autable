package history

import (
	"context"
	"testing"
	"time"
)

func TestLevelDBStorePersistsPrefixScannableHistory(t *testing.T) {
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

	key, err := SaveRowChange(ctx, store, RowChange{
		Database:  "db",
		Table:     "contacts",
		RecordID:  1,
		Timestamp: time.Unix(1, 0).UTC().UnixMilli(),
		Values:    map[string]any{"name": "Ada"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRowChange(ctx, store, RowChange{
		Database:  "db",
		Table:     "contacts",
		RecordID:  1,
		Timestamp: time.Unix(2, 0).UTC().UnixMilli(),
		Values:    map[string]any{"name": "Grace"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveRowChange(ctx, store, RowChange{
		Database:  "db",
		Table:     "contacts",
		RecordID:  1,
		Timestamp: time.Unix(3, 0).UTC().UnixMilli(),
		Values:    map[string]any{"name": "Linus"},
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := store.GetPrefix(ctx, RowPrefix("db", "contacts", 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected three entries, got %d", len(entries))
	}
	change, err := DecodeRowChange(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if change.Values["name"] != "Ada" {
		t.Fatalf("unexpected change: %#v", change)
	}
	exact, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	exactChange, err := DecodeRowChange(exact)
	if err != nil {
		t.Fatal(err)
	}
	if exactChange.RecordID != 1 {
		t.Fatalf("unexpected exact change: %#v", exactChange)
	}
	limited, err := store.GetPrefixLimit(ctx, RowPrefix("db", "contacts", 1), 2)
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
	if limitedFirst.Values["name"] != "Grace" || limitedSecond.Values["name"] != "Linus" {
		t.Fatalf("expected latest entries in key order, got %#v %#v", limitedFirst.Values, limitedSecond.Values)
	}
}

func TestLevelDBStoreGetsPrefixKeysWithoutReadingValues(t *testing.T) {
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

	keys := []string{WorkflowKey(7, 1), WorkflowKey(7, 2), WorkflowKey(7, 3)}
	for _, key := range keys {
		if err := store.Put(ctx, key, []byte("{not json")); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Put(ctx, WorkflowKey(8, 3), []byte("{not json")); err != nil {
		t.Fatal(err)
	}

	latest, err := store.GetPrefixKeysLimit(ctx, WorkflowPrefix(7), 2)
	if err != nil {
		t.Fatal(err)
	}
	expected := keys[1:]
	if len(latest) != len(expected) || latest[0] != expected[0] || latest[1] != expected[1] {
		t.Fatalf("unexpected keys: %#v", latest)
	}
}
