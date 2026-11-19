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

// Version 0 is the schema of the 0.1.18 release, the last release before
// version tracking; databases older than 0.1.18 are not supported.
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
	// 1 → 2: runner tokens became database-scoped. Interim builds created
	// the table with a single global row; the old token is meaningless
	// under the new semantics, so the table is dropped and AutoMigrate
	// recreates the per-database shape (owners reset tokens afterwards).
	func(orm *gorm.DB) error {
		return orm.Exec("DROP TABLE IF EXISTS `runner_token_models`").Error
	},
	// 2 → 3: field grant levels became bitmasks (read=1, update=2,
	// create=4). Legacy level 2 meant full write access, which is 7 in
	// bits; leaving it untouched would silently mean "update only".
	func(orm *gorm.DB) error {
		return orm.Exec("UPDATE `permission_grant_models` SET `level` = 7 WHERE `scope` IN ('field', 'field_set') AND `level` = 2").Error
	},
	// 3 → 4: adding fields moved from "full field_set grant" to the
	// dedicated field_add metadata scope. Subjects holding a full-bit
	// field_set grant (the pre-split full-access semantics, e.g. workflow
	// subjects relying on ensure_fields) keep their ability via a seeded
	// field_add grant.
	func(orm *gorm.DB) error {
		return orm.Exec(
			"INSERT INTO `permission_grant_models` (`subject_id`, `scope`, `resource`, `field`, `level`) " +
				"SELECT `subject_id`, 'field_add', `resource`, '', 2 FROM `permission_grant_models` source " +
				"WHERE `scope` = 'field_set' AND `level` = 7 " +
				"AND NOT EXISTS (SELECT 1 FROM `permission_grant_models` existing " +
				"WHERE existing.`subject_id` = source.`subject_id` AND existing.`scope` = 'field_add' " +
				"AND existing.`resource` = source.`resource` AND existing.`field` = '')").Error
	},
	// 4 → 5: view grants became read-only (view definitions are managed by
	// the database owner); legacy write levels collapse to read.
	func(orm *gorm.DB) error {
		return orm.Exec("UPDATE `permission_grant_models` SET `level` = 1 WHERE `scope` IN ('view', 'view_set') AND `level` > 1").Error
	},
	// 5 → 6: forms were replaced by hosted apps. Form grants would name
	// ids of a table that no longer exists, so they go with it. Form
	// scripts already written to the repository's form/ directory are left
	// alone. IF EXISTS: a database upgraded straight from version 0 by an
	// old-shape fixture may never have had the table.
	func(orm *gorm.DB) error {
		if err := orm.Exec("DELETE FROM `permission_grant_models` WHERE `scope` IN ('form', 'form_set')").Error; err != nil {
			return err
		}
		return orm.Exec("DROP TABLE IF EXISTS `form_models`").Error
	},
	// 6 → 7: autable became admin-gated. Admins come from the config file,
	// so database ownership is gone. Structure is admin-only, so the
	// field_add / field_modify metadata grants and workflow_set grants go.
	// Old workflow grants meant viewing or editing the workflow, not
	// running it; they are deleted rather than reinterpreted, and admins
	// grant run deliberately. App grants keep only their open level.
	// Data-scope grants (including those of workflow subjects) are kept.
	func(orm *gorm.DB) error {
		if err := orm.Exec("DROP TABLE `database_owner_models`").Error; err != nil {
			return err
		}
		if err := orm.Exec("DELETE FROM `permission_grant_models` WHERE `scope` IN ('field_add', 'field_modify', 'workflow_set', 'workflow')").Error; err != nil {
			return err
		}
		return orm.Exec("UPDATE `permission_grant_models` SET `level` = 1 WHERE `scope` IN ('app', 'app_set') AND `level` > 1").Error
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
// current version by AutoMigrate) or a 0.1.18 deployment (version 0).
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
