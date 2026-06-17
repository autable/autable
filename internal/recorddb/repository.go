package recorddb

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

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

func OpenCatalog(ctx context.Context, catalog metadata.Catalog) (*Repository, error) {
	repository := &Repository{dbs: map[string]*gorm.DB{}}
	for _, database := range catalog.Databases {
		if err := repository.OpenDatabase(ctx, database.Name, database.SQLitePath); err != nil {
			_ = repository.Close()
			return nil, err
		}
		for _, tableMeta := range database.Tables {
			if err := repository.EnsureTable(ctx, database.Name, tableMeta); err != nil {
				_ = repository.Close()
				return nil, err
			}
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
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.dbs[name] = db
	return nil
}

func (repository *Repository) EnsureTable(ctx context.Context, dbName string, tableMeta metadata.Table) error {
	db, err := repository.database(dbName)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Table(physicalTableName(tableMeta.Name)).AutoMigrate(dynamicModel(tableMeta))
}

func (repository *Repository) CreateRow(ctx context.Context, dbName string, tableMeta metadata.Table, values map[string]any) (table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return table.Row{}, err
	}
	if err := repository.EnsureTable(ctx, dbName, tableMeta); err != nil {
		return table.Row{}, err
	}
	var saved map[string]any
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(physicalTableName(tableMeta.Name)).Create(insertValues(values)).Error; err != nil {
			return err
		}
		var row map[string]any
		if err := tx.Table(physicalTableName(tableMeta.Name)).
			Order(clause.OrderByColumn{Column: clause.Column{Name: "record_id"}, Desc: true}).
			Limit(1).
			Take(&row).
			Error; err != nil {
			return err
		}
		saved = row
		return nil
	})
	if err != nil {
		return table.Row{}, err
	}
	return mapToRow(tableMeta, saved), nil
}

func (repository *Repository) UpdateRow(ctx context.Context, dbName string, tableMeta metadata.Table, recordID int64, values map[string]any) (table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return table.Row{}, err
	}
	if err := repository.EnsureTable(ctx, dbName, tableMeta); err != nil {
		return table.Row{}, err
	}
	result := db.WithContext(ctx).
		Table(physicalTableName(tableMeta.Name)).
		Where(map[string]any{"record_id": recordID}).
		Updates(updateValues(values))
	if result.Error != nil {
		return table.Row{}, result.Error
	}
	if result.RowsAffected == 0 {
		return table.Row{}, gorm.ErrRecordNotFound
	}
	return repository.Row(ctx, dbName, tableMeta, recordID)
}

func (repository *Repository) DeleteRow(ctx context.Context, dbName string, tableMeta metadata.Table, recordID int64) (table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return table.Row{}, err
	}
	row, err := repository.Row(ctx, dbName, tableMeta, recordID)
	if err != nil {
		return table.Row{}, err
	}
	if err := db.WithContext(ctx).
		Table(physicalTableName(tableMeta.Name)).
		Where(map[string]any{"record_id": recordID}).
		Delete(&map[string]any{}).
		Error; err != nil {
		return table.Row{}, err
	}
	return row, nil
}

func (repository *Repository) Row(ctx context.Context, dbName string, tableMeta metadata.Table, recordID int64) (table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return table.Row{}, err
	}
	if err := repository.EnsureTable(ctx, dbName, tableMeta); err != nil {
		return table.Row{}, err
	}
	var record map[string]any
	err = db.WithContext(ctx).
		Table(physicalTableName(tableMeta.Name)).
		Where(map[string]any{"record_id": recordID}).
		Take(&record).
		Error
	if err != nil {
		return table.Row{}, err
	}
	return mapToRow(tableMeta, record), nil
}

func (repository *Repository) RestoreRow(ctx context.Context, dbName string, tableMeta metadata.Table, row table.Row) error {
	db, err := repository.database(dbName)
	if err != nil {
		return err
	}
	if err := repository.EnsureTable(ctx, dbName, tableMeta); err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record map[string]any
		err := tx.Table(physicalTableName(tableMeta.Name)).
			Where(map[string]any{"record_id": row.RecordID}).
			Take(&record).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		values := insertValues(row.Values)
		values["record_id"] = row.RecordID
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Table(physicalTableName(tableMeta.Name)).Create(values).Error
		}
		return tx.Table(physicalTableName(tableMeta.Name)).
			Where(map[string]any{"record_id": row.RecordID}).
			Updates(updateValues(row.Values)).Error
	})
}

func (repository *Repository) Rows(ctx context.Context, dbName string, tableMeta metadata.Table) ([]table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return nil, err
	}
	if err := repository.EnsureTable(ctx, dbName, tableMeta); err != nil {
		return nil, err
	}
	var records []map[string]any
	err = db.WithContext(ctx).
		Table(physicalTableName(tableMeta.Name)).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "record_id"}}).
		Find(&records).
		Error
	if err != nil {
		return nil, err
	}

	rows := make([]table.Row, 0, len(records))
	for _, record := range records {
		rows = append(rows, mapToRow(tableMeta, record))
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

func dynamicModel(tableMeta metadata.Table) any {
	fields := []reflect.StructField{
		{
			Name: "RecordID",
			Type: reflect.TypeOf(int64(0)),
			Tag:  `gorm:"primaryKey;autoIncrement;column:record_id"`,
		},
		{
			Name: "CreatedAt",
			Type: reflect.TypeOf(int64(0)),
			Tag:  `gorm:"autoCreateTime:milli;not null;column:created_at"`,
		},
		{
			Name: "UpdatedAt",
			Type: reflect.TypeOf(int64(0)),
			Tag:  `gorm:"autoUpdateTime:milli;not null;column:updated_at"`,
		},
	}
	for index, field := range tableMeta.ActiveFields() {
		fields = append(fields, reflect.StructField{
			Name: fmt.Sprintf("Field%d", index),
			Type: goType(field.StorageType()),
			Tag:  reflect.StructTag(fmt.Sprintf(`gorm:"column:%s;type:%s"`, field.Name, sqliteType(field.StorageType()))),
		})
	}
	modelType := reflect.StructOf(fields)
	return reflect.New(modelType).Interface()
}

func goType(fieldType string) reflect.Type {
	switch fieldType {
	case "int":
		return reflect.TypeOf(int64(0))
	case "float":
		return reflect.TypeOf(float64(0))
	default:
		return reflect.TypeOf("")
	}
}

func sqliteType(fieldType string) string {
	switch fieldType {
	case "int":
		return "integer"
	case "float":
		return "real"
	default:
		return "text"
	}
}

func insertValues(values map[string]any) map[string]any {
	next := cloneValues(values)
	delete(next, "created_at")
	delete(next, "updated_at")
	now := time.Now().UTC().UnixMilli()
	next["created_at"] = now
	next["updated_at"] = now
	return next
}

func updateValues(values map[string]any) map[string]any {
	next := cloneValues(values)
	delete(next, "created_at")
	delete(next, "updated_at")
	next["updated_at"] = time.Now().UTC().UnixMilli()
	return next
}

func mapToRow(tableMeta metadata.Table, record map[string]any) table.Row {
	recordID := int64Value(record["record_id"])
	values := map[string]any{}
	for _, field := range tableMeta.ActiveFields() {
		values[field.Name] = record[field.Name]
	}
	return table.Row{RecordID: recordID, Values: values}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case uint64:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func physicalTableName(logicalName string) string {
	if isSafeIdentifier(logicalName) {
		return logicalName
	}
	sum := sha1.Sum([]byte(logicalName))
	return "ct_" + hex.EncodeToString(sum[:8])
}

func isSafeIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || index > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}
