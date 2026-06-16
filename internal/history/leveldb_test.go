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
		Timestamp: time.Unix(1, 0).UTC(),
		Values:    map[string]any{"name": "Ada"},
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := store.GetPrefix(ctx, RowPrefix("db", "contacts", 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
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
}
