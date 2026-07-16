package systemdb

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// schemaMigrationModel records which versioned migrations have been applied.
type schemaMigrationModel struct {
	ID        int64  `gorm:"primaryKey"`
	Name      string `gorm:"not null"`
	AppliedAt int64  `gorm:"not null"`
}

type schemaMigration struct {
	id   int64
	name string
	run  func(orm *gorm.DB) error
}

// schemaMigrations run exactly once each, in order, before AutoMigrate.
// They cover changes AutoMigrate cannot apply to existing data and may use
// raw SQL. Append only; never edit or reorder applied migrations.
var schemaMigrations = []schemaMigration{
	{id: 1, name: "drop legacy role members keyed by user_id", run: dropLegacyRoleMembers},
	{id: 2, name: "add workflow runners_json column", run: addWorkflowRunnersColumn},
}

func (db *DB) runSchemaMigrations(ctx context.Context) error {
	orm := db.orm.WithContext(ctx)
	if err := orm.AutoMigrate(&schemaMigrationModel{}); err != nil {
		return err
	}
	for _, migration := range schemaMigrations {
		var count int64
		if err := orm.Model(&schemaMigrationModel{}).Where("id = ?", migration.id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		err := orm.Transaction(func(tx *gorm.DB) error {
			if err := migration.run(tx); err != nil {
				return err
			}
			return tx.Create(&schemaMigrationModel{
				ID:        migration.id,
				Name:      migration.name,
				AppliedAt: time.Now().UTC().UnixMilli(),
			}).Error
		})
		if err != nil {
			return fmt.Errorf("schema migration %d (%s): %w", migration.id, migration.name, err)
		}
	}
	return nil
}

func dropLegacyRoleMembers(orm *gorm.DB) error {
	migrator := orm.Migrator()
	if migrator.HasColumn(&roleMemberModel{}, "user_id") {
		return migrator.DropTable(&roleMemberModel{})
	}
	return nil
}

// addWorkflowRunnersColumn backfills existing workflow tables: SQLite cannot
// add a NOT NULL column without a default, which AutoMigrate would attempt.
func addWorkflowRunnersColumn(orm *gorm.DB) error {
	migrator := orm.Migrator()
	if !migrator.HasTable(&workflowModel{}) || migrator.HasColumn(&workflowModel{}, "runners_json") {
		return nil
	}
	return orm.Exec("ALTER TABLE `workflow_models` ADD `runners_json` text NOT NULL DEFAULT '{}'").Error
}
