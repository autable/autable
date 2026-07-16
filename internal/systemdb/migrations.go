package systemdb

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// schemaVersionModel is the single row recording the database schema version.
type schemaVersionModel struct {
	ID      int64 `gorm:"primaryKey"`
	Version int64 `gorm:"not null"`
}

const schemaVersionRowID = 1

// Version 0 is the schema of the 0.1.8 release, the last release before
// version tracking; databases older than 0.1.8 are not supported.
//
// migrations[i] upgrades a version-i database to version i+1. Each runs
// unconditionally inside a transaction: at version i the schema state is
// exact, so migrations never probe for tables or columns, and manual drift
// fails loudly instead of being skipped. Append only; never edit or reorder
// released migrations. Raw SQL is allowed here.
var migrations = []func(orm *gorm.DB) error{
	// 0 → 1: per-instance runner bindings. SQLite requires an explicit
	// default to add a NOT NULL column to a populated table, which
	// AutoMigrate does not emit.
	func(orm *gorm.DB) error {
		return orm.Exec("ALTER TABLE `workflow_models` ADD `runners_json` text NOT NULL DEFAULT '{}'").Error
	},
}

func currentSchemaVersion() int64 {
	return int64(len(migrations))
}

func (db *DB) runSchemaMigrations(ctx context.Context) error {
	orm := db.orm.WithContext(ctx)
	if err := orm.AutoMigrate(&schemaVersionModel{}); err != nil {
		return err
	}
	version, err := db.ensureSchemaVersion(orm)
	if err != nil {
		return err
	}
	if version > currentSchemaVersion() {
		return fmt.Errorf("database schema version %d is newer than this binary (%d)", version, currentSchemaVersion())
	}
	for ; version < currentSchemaVersion(); version++ {
		err := orm.Transaction(func(tx *gorm.DB) error {
			if err := migrations[version](tx); err != nil {
				return err
			}
			return tx.Model(&schemaVersionModel{}).
				Where("id = ?", schemaVersionRowID).
				Update("version", version+1).Error
		})
		if err != nil {
			return fmt.Errorf("schema migration %d → %d: %w", version, version+1, err)
		}
	}
	return nil
}

// ensureSchemaVersion returns the recorded schema version. A database
// without a version row is either brand new (no tables yet, created at the
// current version by AutoMigrate) or a 0.1.8 deployment (version 0).
func (db *DB) ensureSchemaVersion(orm *gorm.DB) (int64, error) {
	var record schemaVersionModel
	err := orm.First(&record, schemaVersionRowID).Error
	if err == nil {
		return record.Version, nil
	}
	if err != gorm.ErrRecordNotFound {
		return 0, err
	}
	version := int64(0)
	if !orm.Migrator().HasTable(&userModel{}) {
		version = currentSchemaVersion()
	}
	record = schemaVersionModel{ID: schemaVersionRowID, Version: version}
	if err := orm.Create(&record).Error; err != nil {
		return 0, err
	}
	return version, nil
}
