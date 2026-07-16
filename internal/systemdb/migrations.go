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

// migrations[i] upgrades a version-i database to version i+1. Each runs
// unconditionally inside a transaction: at version i the schema state is
// exact, so migrations never probe for tables or columns, and manual drift
// fails loudly instead of being skipped. Append only; never edit or reorder
// released migrations. Raw SQL is allowed here.
var migrations = []func(orm *gorm.DB) error{
	// 0 → 1: role members were re-keyed from user_id to subject_id; the
	// legacy table is dropped and AutoMigrate recreates the current shape.
	func(orm *gorm.DB) error {
		return orm.Migrator().DropTable(&roleMemberModel{})
	},
	// 1 → 2: per-instance runner bindings. SQLite requires an explicit
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

// ensureSchemaVersion returns the recorded schema version, establishing a
// baseline for databases that predate version tracking. The baseline
// inference is the only place allowed to inspect the schema; once the
// version row exists it is the sole source of truth.
func (db *DB) ensureSchemaVersion(orm *gorm.DB) (int64, error) {
	var record schemaVersionModel
	err := orm.First(&record, schemaVersionRowID).Error
	if err == nil {
		return record.Version, nil
	}
	if err != gorm.ErrRecordNotFound {
		return 0, err
	}
	version := db.baselineSchemaVersion(orm)
	record = schemaVersionModel{ID: schemaVersionRowID, Version: version}
	if err := orm.Create(&record).Error; err != nil {
		return 0, err
	}
	return version, nil
}

func (db *DB) baselineSchemaVersion(orm *gorm.DB) int64 {
	migrator := orm.Migrator()
	// Leftover state table from a pre-release migration mechanism that
	// never shipped; the baseline below re-derives what it recorded.
	_ = migrator.DropTable("schema_migration_models")
	if !migrator.HasTable(&userModel{}) {
		// Fresh database: AutoMigrate creates the final schema directly.
		return currentSchemaVersion()
	}
	if migrator.HasColumn(&roleMemberModel{}, "user_id") {
		return 0
	}
	if migrator.HasTable(&workflowModel{}) && !migrator.HasColumn(&workflowModel{}, "runners_json") {
		return 1
	}
	return currentSchemaVersion()
}
