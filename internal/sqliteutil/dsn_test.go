package sqliteutil

import (
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDSNEnablesWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sqlite")
	db, err := gorm.Open(sqlite.Open(DSN(path)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	handle, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	defer handle.Close()

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
	var synchronous int
	if err := db.Raw("PRAGMA synchronous").Scan(&synchronous).Error; err != nil {
		t.Fatalf("synchronous: %v", err)
	}
	if synchronous != 1 {
		t.Fatalf("synchronous = %d, want 1 (NORMAL)", synchronous)
	}
	var busyTimeout int
	if err := db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}
}
