package table

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"autable/internal/history"
	"autable/internal/jsruntime"
	"autable/internal/metadata"
	"autable/internal/permission"

	"github.com/dop251/goja"
)

var (
	ErrPermissionDenied = errors.New("permission denied")
	ErrDeletedField     = errors.New("field is soft-deleted")
)

type Row struct {
	RecordID int64
	Values   map[string]any
}

type RowChangeHandler func(ctx context.Context, historyKey string, change history.RowChange)

type RowListOptions struct {
	ViewName string
	Query    *metadata.ViewQuery
	Sorts    []metadata.ViewSort
	Limit    int
}

type RowRepository interface {
	EnsureTable(ctx context.Context, dbName string, tableMeta metadata.Table) error
	CreateRow(ctx context.Context, dbName string, tableMeta metadata.Table, values map[string]any) (Row, error)
	UpdateRow(ctx context.Context, dbName string, tableMeta metadata.Table, recordID int64, values map[string]any) (Row, error)
	DeleteRow(ctx context.Context, dbName string, tableMeta metadata.Table, recordID int64) (Row, error)
	Row(ctx context.Context, dbName string, tableMeta metadata.Table, recordID int64) (Row, error)
	RestoreRow(ctx context.Context, dbName string, tableMeta metadata.Table, row Row) error
	Rows(ctx context.Context, dbName string, tableMeta metadata.Table, views ...metadata.ResolvedView) ([]Row, error)
}

// FileBinder permanently binds an uploaded file to the record whose file
// cell references it; a file can be bound to exactly one record, once, and
// only by the user who uploaded it.
type FileBinder interface {
	BindFileToRecord(ctx context.Context, actorID string, fileID int64, dbName, tableName string, recordID int64) error
}

type Service struct {
	mu          sync.RWMutex
	rows        RowRepository
	history     history.Store
	rowChangeFn RowChangeHandler
	fileBinder  FileBinder
}

func NewServiceWithRepository(historyStore history.Store, rows RowRepository) *Service {
	return &Service{
		rows:    rows,
		history: historyStore,
	}
}

func (service *Service) SetRowChangeHandler(handler RowChangeHandler) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.rowChangeFn = handler
}

// SetFileBinder enables writes to file fields; without it any non-null file
// cell value is rejected.
func (service *Service) SetFileBinder(binder FileBinder) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.fileBinder = binder
}

// bindFileValues locks every referenced file to this record. Binding is
// one-shot: a file already bound to another record fails the whole write.
func (service *Service) bindFileValues(ctx context.Context, tableMeta metadata.Table, actorID string, dbName string, recordID int64, values map[string]any) error {
	service.mu.RLock()
	binder := service.fileBinder
	service.mu.RUnlock()
	for _, field := range tableMeta.Fields {
		if field.Deleted || field.Type != "file" {
			continue
		}
		value, ok := values[field.Name]
		if !ok || value == nil {
			continue
		}
		fileID, ok := value.(int64)
		if !ok || fileID <= 0 {
			return fmt.Errorf("field %q requires a positive file id, got %v", field.Name, value)
		}
		if binder == nil {
			return fmt.Errorf("field %q cannot store files because file binding is not configured", field.Name)
		}
		if err := binder.BindFileToRecord(ctx, actorID, fileID, dbName, tableMeta.Name, recordID); err != nil {
			return fmt.Errorf("field %q: %w", field.Name, err)
		}
	}
	return nil
}

func (service *Service) CreateRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID string, isDatabaseOwner bool, dbName, tableName string, values map[string]any) (Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Row{}, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName
	if !isDatabaseOwner && !perms.CanCreateRecord(actorID, resource) {
		return Row{}, fmt.Errorf("%w: %s", ErrPermissionDenied, resource)
	}
	if err := validateWritableFields(tableMeta, perms, actorID, isDatabaseOwner, resource, values); err != nil {
		return Row{}, err
	}
	storedValues, err := normalizeInputValues(tableMeta, values)
	if err != nil {
		return Row{}, err
	}

	row, err := service.rows.CreateRow(ctx, dbName, tableMeta, storedValues)
	if err != nil {
		return Row{}, err
	}
	if err := service.bindFileValues(ctx, tableMeta, actorID, dbName, row.RecordID, storedValues); err != nil {
		_, _ = service.rows.DeleteRow(ctx, dbName, tableMeta, row.RecordID)
		return Row{}, err
	}
	storedValues, err = calculateFormulaValues(tableMeta, row.RecordID, row.Values)
	if err != nil {
		_, _ = service.rows.DeleteRow(ctx, dbName, tableMeta, row.RecordID)
		return Row{}, err
	}
	row, err = service.rows.UpdateRow(ctx, dbName, tableMeta, row.RecordID, storedValues)
	if err != nil {
		_, _ = service.rows.DeleteRow(ctx, dbName, tableMeta, row.RecordID)
		return Row{}, err
	}
	change := history.RowChange{
		Database:  dbName,
		Table:     tableName,
		RecordID:  row.RecordID,
		Timestamp: time.Now().UTC().UnixMilli(),
		Operation: "create",
		Values:    cloneValues(row.Values),
		Diff:      rowDiff(nil, row.Values),
		ActorID:   actorID,
	}
	historyKey, err := history.SaveRowChange(ctx, service.history, change)
	if err != nil {
		_, _ = service.rows.DeleteRow(ctx, dbName, tableMeta, row.RecordID)
		return Row{}, err
	}
	service.notifyRowChange(ctx, historyKey, change)
	return row, nil
}

func (service *Service) UpdateRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID string, isDatabaseOwner bool, dbName, tableName string, recordID int64, values map[string]any) (Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Row{}, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName
	if err := validateWritableFields(tableMeta, perms, actorID, isDatabaseOwner, resource, values); err != nil {
		return Row{}, err
	}

	existing, err := service.rows.Row(ctx, dbName, tableMeta, recordID)
	if err != nil {
		return Row{}, err
	}
	nextValues := cloneValues(existing.Values)
	normalizedValues, err := normalizeInputValues(tableMeta, values)
	if err != nil {
		return Row{}, err
	}
	for key, value := range normalizedValues {
		nextValues[key] = value
	}
	nextValues, err = calculateFormulaValues(tableMeta, recordID, nextValues)
	if err != nil {
		return Row{}, err
	}
	if err := service.bindFileValues(ctx, tableMeta, actorID, dbName, recordID, normalizedValues); err != nil {
		return Row{}, err
	}
	updated, err := service.rows.UpdateRow(ctx, dbName, tableMeta, recordID, nextValues)
	if err != nil {
		return Row{}, err
	}
	change := history.RowChange{
		Database:  dbName,
		Table:     tableName,
		RecordID:  updated.RecordID,
		Timestamp: time.Now().UTC().UnixMilli(),
		Operation: "update",
		Values:    cloneValues(updated.Values),
		Diff:      rowDiff(existing.Values, updated.Values),
		ActorID:   actorID,
	}
	historyKey, err := history.SaveRowChange(ctx, service.history, change)
	if err != nil {
		_ = service.rows.RestoreRow(ctx, dbName, tableMeta, existing)
		return Row{}, err
	}
	service.notifyRowChange(ctx, historyKey, change)
	return updated, nil
}

func (service *Service) DeleteRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID string, isDatabaseOwner bool, dbName, tableName string, recordID int64) (Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Row{}, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName
	if !isDatabaseOwner && !perms.CanDeleteRecord(actorID, resource) {
		return Row{}, fmt.Errorf("%w: %s", ErrPermissionDenied, resource)
	}

	row, err := service.rows.DeleteRow(ctx, dbName, tableMeta, recordID)
	if err != nil {
		return Row{}, err
	}
	change := history.RowChange{
		Database:  dbName,
		Table:     tableName,
		RecordID:  row.RecordID,
		Timestamp: time.Now().UTC().UnixMilli(),
		Operation: "delete",
		Values:    cloneValues(row.Values),
		Diff:      rowDiff(row.Values, nil),
		ActorID:   actorID,
	}
	historyKey, err := history.SaveRowChange(ctx, service.history, change)
	if err != nil {
		_ = service.rows.RestoreRow(ctx, dbName, tableMeta, row)
		return Row{}, err
	}
	service.notifyRowChange(ctx, historyKey, change)
	return row, nil
}

func (service *Service) Rows(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID string, isDatabaseOwner bool, dbName, tableName, viewName string, temporarySorts ...metadata.ViewSort) ([]Row, error) {
	return service.RowsWithOptions(ctx, catalog, perms, actorID, isDatabaseOwner, dbName, tableName, RowListOptions{
		ViewName: viewName,
		Sorts:    temporarySorts,
	})
}

func (service *Service) RowsWithOptions(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID string, isDatabaseOwner bool, dbName, tableName string, options RowListOptions) ([]Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return nil, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName

	var resolved metadata.ResolvedView
	if options.ViewName != "" {
		var err error
		resolved, err = tableMeta.ResolveView(options.ViewName)
		if err != nil {
			return nil, err
		}
		if !perms.CanReadView(actorID, resource, options.ViewName) && !isDatabaseOwner {
			return nil, fmt.Errorf("%w: view %s", ErrPermissionDenied, options.ViewName)
		}
		if !isDatabaseOwner && !viewFieldsReadable(perms, actorID, resource, resolved.Query, resolved.Sorts) {
			return nil, fmt.Errorf("%w: view %s", ErrPermissionDenied, options.ViewName)
		}
	}
	if options.Query != nil {
		if !isDatabaseOwner && !viewFieldsReadable(perms, actorID, resource, options.Query, nil) {
			return nil, fmt.Errorf("%w: query", ErrPermissionDenied)
		}
		resolved.Query = combineRuntimeQueries(resolved.Query, options.Query)
	}
	if len(options.Sorts) > 0 {
		for _, sortDef := range options.Sorts {
			field, ok := tableMeta.Field(sortDef.Field)
			if !ok || field.Deleted {
				return nil, fmt.Errorf("unknown temporary sort field %q", sortDef.Field)
			}
			if sortDef.Direction != "asc" && sortDef.Direction != "desc" {
				return nil, fmt.Errorf("unsupported temporary sort direction %q", sortDef.Direction)
			}
		}
		if !isDatabaseOwner && !viewFieldsReadable(perms, actorID, resource, nil, options.Sorts) {
			return nil, fmt.Errorf("%w: sort", ErrPermissionDenied)
		}
		resolved.Sorts = options.Sorts
	}
	if options.Limit < 0 {
		return nil, fmt.Errorf("limit must be greater than or equal to 0")
	}
	resolved.Limit = options.Limit
	rows, err := service.rows.Rows(ctx, dbName, tableMeta, resolved)
	if err != nil {
		return nil, err
	}

	filtered := make([]Row, 0, len(rows))
	for _, row := range rows {
		values := map[string]any{}
		for fieldName, value := range row.Values {
			if isDatabaseOwner || perms.CanReadField(actorID, resource, fieldName) {
				values[fieldName] = value
			}
		}
		filtered = append(filtered, Row{RecordID: row.RecordID, Values: values})
	}
	return filtered, nil
}

func combineRuntimeQueries(left *metadata.ViewQuery, right *metadata.ViewQuery) *metadata.ViewQuery {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return &metadata.ViewQuery{
		Combinator: "and",
		Rules: []metadata.ViewQueryRule{
			{Combinator: left.Combinator, Rules: left.Rules, Not: left.Not},
			{Combinator: right.Combinator, Rules: right.Rules, Not: right.Not},
		},
	}
}

func (service *Service) SyncTable(ctx context.Context, catalog metadata.Catalog, dbName, tableName string) error {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	if err := service.rows.EnsureTable(ctx, dbName, tableMeta); err != nil {
		return err
	}
	rows, err := service.rows.Rows(ctx, dbName, tableMeta)
	if err != nil {
		return err
	}
	for _, row := range rows {
		nextValues, err := calculateFormulaValues(tableMeta, row.RecordID, cloneValues(row.Values))
		if err != nil {
			return err
		}
		if _, err := service.rows.UpdateRow(ctx, dbName, tableMeta, row.RecordID, nextValues); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) EnsureTable(ctx context.Context, catalog metadata.Catalog, dbName, tableName string) error {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	return service.rows.EnsureTable(ctx, dbName, tableMeta)
}

func viewFieldsReadable(perms permission.Set, actorID, resource string, query *metadata.ViewQuery, sorts []metadata.ViewSort) bool {
	for _, field := range viewQueryFields(query) {
		if !perms.CanReadField(actorID, resource, field) {
			return false
		}
	}
	for _, sortDef := range sorts {
		if !perms.CanReadField(actorID, resource, sortDef.Field) {
			return false
		}
	}
	return true
}

func viewQueryFields(query *metadata.ViewQuery) []string {
	if query == nil {
		return nil
	}
	fields := []string{}
	for _, rule := range query.Rules {
		fields = append(fields, viewQueryRuleFields(rule)...)
	}
	return fields
}

func viewQueryRuleFields(rule metadata.ViewQueryRule) []string {
	if rule.Combinator != "" || len(rule.Rules) > 0 {
		fields := []string{}
		for _, child := range rule.Rules {
			fields = append(fields, viewQueryRuleFields(child)...)
		}
		return fields
	}
	return []string{rule.Field}
}

func validateWritableFields(tableMeta metadata.Table, perms permission.Set, actorID string, isDatabaseOwner bool, resource string, values map[string]any) error {
	for fieldName := range values {
		field, ok := tableMeta.Field(fieldName)
		if !ok {
			return fmt.Errorf("%w: %s", metadata.ErrUnknownField, fieldName)
		}
		if strings.HasPrefix(field.Name, "ct_") {
			return fmt.Errorf("%w: %s", ErrPermissionDenied, fieldName)
		}
		if field.Deleted {
			return fmt.Errorf("%w: %s", ErrDeletedField, fieldName)
		}
		if field.Type == "formula" {
			return fmt.Errorf("%w: %s", ErrPermissionDenied, fieldName)
		}
		if !isDatabaseOwner && !perms.CanWriteField(actorID, resource, fieldName) {
			return fmt.Errorf("%w: %s", ErrPermissionDenied, fieldName)
		}
	}
	return nil
}

func calculateFormulaValues(tableMeta metadata.Table, recordID int64, values map[string]any) (map[string]any, error) {
	nextValues := cloneValues(values)
	for _, field := range tableMeta.Fields {
		if field.Deleted || field.Type != "formula" {
			continue
		}
		value, err := evaluateFormula(field.Formula, recordID, nextValues)
		if err != nil {
			logFormulaValueError(tableMeta.Name, field.Name, err)
			nextValues[field.Name] = nil
			continue
		}
		value, err = normalizeFieldValue(field, value)
		if err != nil {
			logFormulaValueError(tableMeta.Name, field.Name, err)
			nextValues[field.Name] = nil
			continue
		}
		nextValues[field.Name] = value
	}
	return nextValues, nil
}

func normalizeInputValues(tableMeta metadata.Table, values map[string]any) (map[string]any, error) {
	normalized := map[string]any{}
	for key, value := range values {
		field, ok := tableMeta.Field(key)
		if !ok {
			return nil, fmt.Errorf("%w: %s", metadata.ErrUnknownField, key)
		}
		normalizedValue, err := normalizeFieldValue(field, value)
		if err != nil {
			logFieldValueError(tableMeta.Name, key, err)
			normalized[key] = nil
			continue
		}
		normalized[key] = normalizedValue
	}
	return normalized, nil
}

func logFormulaValueError(tableName, fieldName string, err error) {
	slog.Warn("formula field value cleared after calculation error", "table", tableName, "field", fieldName, "error", err)
}

func logFieldValueError(tableName, fieldName string, err error) {
	slog.Warn("field value cleared after conversion error", "table", tableName, "field", fieldName, "error", err)
}

func normalizeFieldValue(field metadata.Field, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch field.StorageType() {
	case "string":
		return fmt.Sprint(value), nil
	case "int":
		return normalizeInt(value)
	case "float":
		return normalizeFloat(value)
	default:
		return nil, fmt.Errorf("unsupported field type %q", field.StorageType())
	}
}

func normalizeInt(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int8:
		return int64(typed), nil
	case int16:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case uint:
		return int64(typed), nil
	case uint8:
		return int64(typed), nil
	case uint16:
		return int64(typed), nil
	case uint32:
		return int64(typed), nil
	case uint64:
		return int64(typed), nil
	case float32:
		if float64(int64(typed)) != float64(typed) {
			return 0, fmt.Errorf("expected integer, got %v", value)
		}
		return int64(typed), nil
	case float64:
		if float64(int64(typed)) != typed {
			return 0, fmt.Errorf("expected integer, got %v", value)
		}
		return int64(typed), nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func normalizeFloat(value any) (float64, error) {
	switch typed := value.(type) {
	case int:
		return float64(typed), nil
	case int8:
		return float64(typed), nil
	case int16:
		return float64(typed), nil
	case int32:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case uint:
		return float64(typed), nil
	case uint8:
		return float64(typed), nil
	case uint16:
		return float64(typed), nil
	case uint32:
		return float64(typed), nil
	case uint64:
		return float64(typed), nil
	case float32:
		return float64(typed), nil
	case float64:
		return typed, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("expected float, got %T", value)
	}
}

func evaluateFormula(expression string, recordID int64, values map[string]any) (any, error) {
	runtime := goja.New()
	if err := jsruntime.InstallStableStringify(runtime); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := runtime.Set("record_id", recordID); err != nil {
		return nil, err
	}
	if err := runtime.Set("now", now.UnixMilli()); err != nil {
		return nil, err
	}
	if err := runtime.Set("today", today.UnixMilli()); err != nil {
		return nil, err
	}
	if err := runtime.Set("fields", cloneValues(values)); err != nil {
		return nil, err
	}
	value, err := runtime.RunString("(" + expression + ")")
	if err != nil {
		return nil, err
	}
	return value.Export(), nil
}

func cloneValues(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func (service *Service) notifyRowChange(ctx context.Context, historyKey string, change history.RowChange) {
	service.mu.RLock()
	handler := service.rowChangeFn
	service.mu.RUnlock()
	if handler != nil {
		handler(ctx, historyKey, change)
	}
}

func rowDiff(oldValues map[string]any, newValues map[string]any) history.RowDiff {
	keys := map[string]struct{}{}
	for key := range oldValues {
		keys[key] = struct{}{}
	}
	for key := range newValues {
		keys[key] = struct{}{}
	}
	diff := history.RowDiff{}
	for key := range keys {
		oldValue, oldOK := oldValues[key]
		newValue, newOK := newValues[key]
		if isNilValue(oldValue) && isNilValue(newValue) {
			continue
		}
		if oldOK && newOK && reflect.DeepEqual(oldValue, newValue) {
			continue
		}
		diff[key] = history.FieldDiff{Old: oldValue, New: newValue}
	}
	return diff
}

func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
