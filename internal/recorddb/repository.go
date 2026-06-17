package recorddb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"codetable/internal/metadata"
	"codetable/internal/table"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrUnknownDatabase = errors.New("unknown database")

type Repository struct {
	mu  sync.RWMutex
	dbs map[string]*gorm.DB
}

type Record struct {
	ID        int64   `gorm:"primaryKey;autoIncrement"`
	RecordID  int64   `gorm:"uniqueIndex:idx_record_table_record_id;not null;column:record_id"`
	TableName string  `gorm:"uniqueIndex:idx_record_table_record_id;index;not null"`
	Values    JSONMap `gorm:"type:json;not null"`
	CreatedAt int64   `gorm:"autoCreateTime:milli;not null"`
	UpdatedAt int64   `gorm:"autoUpdateTime:milli;not null"`
}

func OpenCatalog(ctx context.Context, catalog metadata.Catalog) (*Repository, error) {
	repository := &Repository{dbs: map[string]*gorm.DB{}}
	for _, database := range catalog.Databases {
		if err := repository.OpenDatabase(ctx, database.Name, database.SQLitePath); err != nil {
			_ = repository.Close()
			return nil, err
		}
	}
	return repository, nil
}

func (repository *Repository) OpenDatabase(ctx context.Context, name, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return err
	}
	if err := dropIncompatibleRecordTimestampTable(ctx, db); err != nil {
		handle, closeErr := db.DB()
		if closeErr == nil {
			_ = handle.Close()
		}
		return err
	}
	if err := db.WithContext(ctx).AutoMigrate(&Record{}); err != nil {
		handle, closeErr := db.DB()
		if closeErr == nil {
			_ = handle.Close()
		}
		return err
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.dbs[name] = db
	return nil
}

func dropIncompatibleRecordTimestampTable(ctx context.Context, db *gorm.DB) error {
	migrator := db.WithContext(ctx).Migrator()
	if !migrator.HasTable(&Record{}) {
		return nil
	}
	columnTypes, err := migrator.ColumnTypes(&Record{})
	if err != nil {
		return err
	}
	for _, columnType := range columnTypes {
		name := strings.ToLower(columnType.Name())
		if name != "created_at" && name != "updated_at" {
			continue
		}
		if !strings.Contains(strings.ToUpper(columnType.DatabaseTypeName()), "INT") {
			return migrator.DropTable(&Record{})
		}
	}
	return nil
}

func (repository *Repository) CreateRow(ctx context.Context, dbName, tableName string, values map[string]any) (table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return table.Row{}, err
	}
	var record Record
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var latest Record
		err := tx.
			Where(&Record{TableName: tableName}).
			Order(clause.OrderByColumn{Column: clause.Column{Name: "record_id"}, Desc: true}).
			First(&latest).
			Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		record = Record{
			RecordID:  latest.RecordID + 1,
			TableName: tableName,
			Values:    JSONMap(cloneValues(values)),
		}
		return tx.Create(&record).Error
	})
	if err != nil {
		return table.Row{}, err
	}
	return table.Row{RecordID: record.RecordID, Values: record.Values.Plain()}, nil
}

func (repository *Repository) UpdateRow(ctx context.Context, dbName, tableName string, recordID int64, values map[string]any) (table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return table.Row{}, err
	}
	var record Record
	err = db.WithContext(ctx).
		Where(&Record{RecordID: recordID, TableName: tableName}).
		First(&record).
		Error
	if err != nil {
		return table.Row{}, err
	}
	nextValues := record.Values.Plain()
	for key, value := range values {
		nextValues[key] = value
	}
	record.Values = JSONMap(nextValues)
	if err := db.WithContext(ctx).Save(&record).Error; err != nil {
		return table.Row{}, err
	}
	return table.Row{RecordID: record.RecordID, Values: record.Values.Plain()}, nil
}

func (repository *Repository) DeleteRow(ctx context.Context, dbName, tableName string, recordID int64) (table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return table.Row{}, err
	}
	var record Record
	err = db.WithContext(ctx).
		Where(&Record{RecordID: recordID, TableName: tableName}).
		First(&record).
		Error
	if err != nil {
		return table.Row{}, err
	}
	if err := db.WithContext(ctx).Delete(&record).Error; err != nil {
		return table.Row{}, err
	}
	return table.Row{RecordID: record.RecordID, Values: record.Values.Plain()}, nil
}

func (repository *Repository) Row(ctx context.Context, dbName, tableName string, recordID int64) (table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return table.Row{}, err
	}
	var record Record
	err = db.WithContext(ctx).
		Where(&Record{RecordID: recordID, TableName: tableName}).
		First(&record).
		Error
	if err != nil {
		return table.Row{}, err
	}
	return table.Row{RecordID: record.RecordID, Values: record.Values.Plain()}, nil
}

func (repository *Repository) RestoreRow(ctx context.Context, dbName, tableName string, row table.Row) error {
	db, err := repository.database(dbName)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record Record
		err := tx.
			Where(&Record{RecordID: row.RecordID, TableName: tableName}).
			First(&record).
			Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&Record{
				RecordID:  row.RecordID,
				TableName: tableName,
				Values:    JSONMap(cloneValues(row.Values)),
			}).Error
		}
		record.Values = JSONMap(cloneValues(row.Values))
		return tx.Save(&record).Error
	})
}

func (repository *Repository) Rows(ctx context.Context, dbName, tableName string) ([]table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return nil, err
	}
	var records []Record
	err = db.WithContext(ctx).
		Where(&Record{TableName: tableName}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "record_id"}}).
		Find(&records).
		Error
	if err != nil {
		return nil, err
	}

	rows := make([]table.Row, 0, len(records))
	for _, record := range records {
		rows = append(rows, table.Row{RecordID: record.RecordID, Values: record.Values.Plain()})
	}
	return rows, nil
}

func (repository *Repository) Close() error {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	var closeErr error
	for name, db := range repository.dbs {
		handle, err := db.DB()
		if err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if err := handle.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("%s: %w", name, err))
		}
		delete(repository.dbs, name)
	}
	return closeErr
}

func (repository *Repository) database(name string) (*gorm.DB, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()

	db, ok := repository.dbs[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownDatabase, name)
	}
	return db, nil
}

func cloneValues(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
