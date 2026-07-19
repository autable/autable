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
	"strconv"
	"strings"
	"sync"
	"time"

	"autable/internal/metadata"
	"autable/internal/sqliteutil"
	"autable/internal/table"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrUnknownDatabase = errors.New("unknown database")

const (
	recordIDColumn  = "ct_record_id"
	createdAtColumn = "ct_created_at"
	updatedAtColumn = "ct_updated_at"
)

type Repository struct {
	mu  sync.RWMutex
	dbs map[string]*gorm.DB
}

func OpenCatalog(ctx context.Context, catalog metadata.Catalog, dataPath string) (*Repository, error) {
	repository := &Repository{dbs: map[string]*gorm.DB{}}
	for _, database := range catalog.Databases {
		if err := repository.OpenDatabase(ctx, database.Name, filepath.Join(dataPath, database.Name+".sqlite")); err != nil {
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
	db, err := gorm.Open(sqlite.Open(sqliteutil.DSN(path)), &gorm.Config{})
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
	tableName := physicalTableName(tableMeta.Name)
	migrationMeta, err := migrationTableMetadata(db, tableName, tableMeta)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Table(tableName).AutoMigrate(dynamicModel(migrationMeta))
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
			Order(clause.OrderByColumn{Column: clause.Column{Name: recordIDColumn}, Desc: true}).
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
		Where(map[string]any{recordIDColumn: recordID}).
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
		Where(map[string]any{recordIDColumn: recordID}).
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
		Where(map[string]any{recordIDColumn: recordID}).
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
			Where(map[string]any{recordIDColumn: row.RecordID}).
			Take(&record).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		values := insertValues(row.Values)
		values[recordIDColumn] = row.RecordID
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Table(physicalTableName(tableMeta.Name)).Create(values).Error
		}
		return tx.Table(physicalTableName(tableMeta.Name)).
			Where(map[string]any{recordIDColumn: row.RecordID}).
			Updates(updateValues(row.Values)).Error
	})
}

func (repository *Repository) Rows(ctx context.Context, dbName string, tableMeta metadata.Table, views ...metadata.ResolvedView) ([]table.Row, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return nil, err
	}
	if err := repository.EnsureTable(ctx, dbName, tableMeta); err != nil {
		return nil, err
	}
	query := db.WithContext(ctx).Table(physicalTableName(tableMeta.Name))
	if len(views) > 0 {
		if views[0].Query != nil {
			whereSQL, args, err := compileViewQuery(tableMeta, *views[0].Query)
			if err != nil {
				return nil, err
			}
			if whereSQL != "" {
				query = query.Where(whereSQL, args...)
			}
		}
		for _, sortDef := range views[0].Sorts {
			field, ok := tableMeta.Field(sortDef.Field)
			if !ok || field.Deleted {
				return nil, fmt.Errorf("unknown view sort field %q", sortDef.Field)
			}
			if sortDef.Direction != "asc" && sortDef.Direction != "desc" {
				return nil, fmt.Errorf("unsupported view sort direction %q", sortDef.Direction)
			}
			query = query.Order(clause.OrderByColumn{
				Column: clause.Column{Name: viewQueryColumnName(sortDef.Field)},
				Desc:   sortDef.Direction == "desc",
			})
		}
		if views[0].Limit > 0 {
			query = query.Limit(views[0].Limit)
			// GORM emits OFFSET without LIMIT when only Offset is set, which
			// SQLite rejects — so the offset is tied to a positive limit.
			if views[0].Offset > 0 {
				query = query.Offset(views[0].Offset)
			}
		}
	}
	var records []map[string]any
	err = query.
		Order(clause.OrderByColumn{Column: clause.Column{Name: recordIDColumn}}).
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

func (repository *Repository) CountRows(ctx context.Context, dbName string, tableMeta metadata.Table, views ...metadata.ResolvedView) (int64, error) {
	db, err := repository.database(dbName)
	if err != nil {
		return 0, err
	}
	if err := repository.EnsureTable(ctx, dbName, tableMeta); err != nil {
		return 0, err
	}
	query := db.WithContext(ctx).Table(physicalTableName(tableMeta.Name))
	if len(views) > 0 && views[0].Query != nil {
		whereSQL, args, err := compileViewQuery(tableMeta, *views[0].Query)
		if err != nil {
			return 0, err
		}
		if whereSQL != "" {
			query = query.Where(whereSQL, args...)
		}
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func compileViewQuery(tableMeta metadata.Table, query metadata.ViewQuery) (string, []any, error) {
	if len(query.Rules) == 0 {
		return "", nil, nil
	}
	combinator := strings.ToUpper(query.Combinator)
	if combinator != "AND" && combinator != "OR" {
		return "", nil, fmt.Errorf("unsupported view query combinator %q", query.Combinator)
	}
	parts := make([]string, 0, len(query.Rules))
	args := []any{}
	for _, rule := range query.Rules {
		ruleSQL, ruleArgs, err := compileViewQueryRule(tableMeta, rule)
		if err != nil {
			return "", nil, err
		}
		if ruleSQL == "" {
			continue
		}
		parts = append(parts, ruleSQL)
		args = append(args, ruleArgs...)
	}
	if len(parts) == 0 {
		return "", nil, nil
	}
	sql := "(" + strings.Join(parts, " "+combinator+" ") + ")"
	if query.Not {
		sql = "NOT " + sql
	}
	return sql, args, nil
}

func compileViewQueryRule(tableMeta metadata.Table, rule metadata.ViewQueryRule) (string, []any, error) {
	if rule.Combinator != "" || len(rule.Rules) > 0 {
		return compileViewQuery(tableMeta, metadata.ViewQuery{Combinator: rule.Combinator, Rules: rule.Rules, Not: rule.Not})
	}
	field, ok := tableMeta.Field(rule.Field)
	if !ok || field.Deleted {
		return "", nil, fmt.Errorf("unknown view query field %q", rule.Field)
	}
	column := quoteSQLiteIdentifier(viewQueryColumnName(rule.Field))
	switch rule.Operator {
	case "=":
		value, err := normalizeViewQueryValue(field, rule.Value)
		if err != nil {
			return "", nil, err
		}
		return column + " = ?", []any{value}, nil
	case "!=":
		value, err := normalizeViewQueryValue(field, rule.Value)
		if err != nil {
			return "", nil, err
		}
		return column + " <> ?", []any{value}, nil
	case "<", "<=", ">", ">=":
		value, err := normalizeViewQueryValue(field, rule.Value)
		if err != nil {
			return "", nil, err
		}
		return column + " " + rule.Operator + " ?", []any{value}, nil
	case "contains":
		return "LOWER(CAST(" + column + " AS TEXT)) LIKE ? ESCAPE '\\'", []any{"%" + escapeSQLiteLike(strings.ToLower(fmt.Sprint(rule.Value))) + "%"}, nil
	case "beginsWith":
		return "LOWER(CAST(" + column + " AS TEXT)) LIKE ? ESCAPE '\\'", []any{escapeSQLiteLike(strings.ToLower(fmt.Sprint(rule.Value))) + "%"}, nil
	case "endsWith":
		return "LOWER(CAST(" + column + " AS TEXT)) LIKE ? ESCAPE '\\'", []any{"%" + escapeSQLiteLike(strings.ToLower(fmt.Sprint(rule.Value)))}, nil
	case "doesNotContain":
		return "(" + column + " IS NULL OR LOWER(CAST(" + column + " AS TEXT)) NOT LIKE ? ESCAPE '\\')", []any{"%" + escapeSQLiteLike(strings.ToLower(fmt.Sprint(rule.Value))) + "%"}, nil
	case "doesNotBeginWith":
		return "(" + column + " IS NULL OR LOWER(CAST(" + column + " AS TEXT)) NOT LIKE ? ESCAPE '\\')", []any{escapeSQLiteLike(strings.ToLower(fmt.Sprint(rule.Value))) + "%"}, nil
	case "doesNotEndWith":
		return "(" + column + " IS NULL OR LOWER(CAST(" + column + " AS TEXT)) NOT LIKE ? ESCAPE '\\')", []any{"%" + escapeSQLiteLike(strings.ToLower(fmt.Sprint(rule.Value)))}, nil
	case "null":
		return "(" + column + " IS NULL OR TRIM(CAST(" + column + " AS TEXT)) = '')", nil, nil
	case "notNull":
		return "(" + column + " IS NOT NULL AND TRIM(CAST(" + column + " AS TEXT)) <> '')", nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported view query operator %q", rule.Operator)
	}
}

func normalizeViewQueryValue(field metadata.Field, value any) (any, error) {
	switch field.StorageType() {
	case "int":
		switch typed := value.(type) {
		case int:
			return int64(typed), nil
		case int64:
			return typed, nil
		case float64:
			return int64(typed), nil
		case string:
			parsed, err := strconv.ParseInt(typed, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid int query value for field %q: %w", field.Name, err)
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("invalid int query value for field %q", field.Name)
		}
	case "float":
		switch typed := value.(type) {
		case int:
			return float64(typed), nil
		case int64:
			return float64(typed), nil
		case float64:
			return typed, nil
		case string:
			parsed, err := strconv.ParseFloat(typed, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid float query value for field %q: %w", field.Name, err)
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("invalid float query value for field %q", field.Name)
		}
	default:
		return fmt.Sprint(value), nil
	}
}

func viewQueryColumnName(fieldName string) string {
	if fieldName == "ct_record_id" {
		return recordIDColumn
	}
	return fieldName
}

func quoteSQLiteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func escapeSQLiteLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
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
			Tag:  `gorm:"primaryKey;autoIncrement;column:ct_record_id"`,
		},
		{
			Name: "CreatedAt",
			Type: reflect.TypeOf(int64(0)),
			Tag:  `gorm:"autoCreateTime:milli;not null;column:ct_created_at"`,
		},
		{
			Name: "UpdatedAt",
			Type: reflect.TypeOf(int64(0)),
			Tag:  `gorm:"autoUpdateTime:milli;not null;column:ct_updated_at"`,
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

func migrationTableMetadata(db *gorm.DB, tableName string, tableMeta metadata.Table) (metadata.Table, error) {
	if !db.Migrator().HasTable(tableName) {
		return tableMeta, nil
	}
	columnTypes, err := db.Migrator().ColumnTypes(tableName)
	if err != nil {
		return metadata.Table{}, err
	}
	columns := map[string]struct{}{}
	for _, columnType := range columnTypes {
		columns[strings.ToLower(columnType.Name())] = struct{}{}
	}
	filtered := tableMeta
	filtered.Fields = make([]metadata.Field, 0, len(tableMeta.Fields))
	for _, field := range tableMeta.Fields {
		if field.Deleted {
			filtered.Fields = append(filtered.Fields, field)
			continue
		}
		if _, ok := columns[strings.ToLower(field.Name)]; ok {
			continue
		}
		filtered.Fields = append(filtered.Fields, field)
	}
	return filtered, nil
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
	delete(next, createdAtColumn)
	delete(next, updatedAtColumn)
	now := time.Now().UTC().UnixMilli()
	next[createdAtColumn] = now
	next[updatedAtColumn] = now
	return next
}

func updateValues(values map[string]any) map[string]any {
	next := cloneValues(values)
	delete(next, createdAtColumn)
	delete(next, updatedAtColumn)
	next[updatedAtColumn] = time.Now().UTC().UnixMilli()
	return next
}

func mapToRow(tableMeta metadata.Table, record map[string]any) table.Row {
	recordID := int64Value(record[recordIDColumn])
	values := map[string]any{}
	for _, field := range tableMeta.ActiveFields() {
		values[field.Name] = recordValue(record, field.Name)
	}
	return table.Row{RecordID: recordID, Values: values}
}

func recordValue(record map[string]any, fieldName string) any {
	if value, ok := record[fieldName]; ok {
		return value
	}
	for key, value := range record {
		if strings.EqualFold(key, fieldName) {
			return value
		}
	}
	return nil
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
