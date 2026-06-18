package table

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"codetable/internal/history"
	"codetable/internal/metadata"
	"codetable/internal/permission"

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

type RowRepository interface {
	EnsureTable(ctx context.Context, dbName string, tableMeta metadata.Table) error
	CreateRow(ctx context.Context, dbName string, tableMeta metadata.Table, values map[string]any) (Row, error)
	UpdateRow(ctx context.Context, dbName string, tableMeta metadata.Table, recordID int64, values map[string]any) (Row, error)
	DeleteRow(ctx context.Context, dbName string, tableMeta metadata.Table, recordID int64) (Row, error)
	Row(ctx context.Context, dbName string, tableMeta metadata.Table, recordID int64) (Row, error)
	RestoreRow(ctx context.Context, dbName string, tableMeta metadata.Table, row Row) error
	Rows(ctx context.Context, dbName string, tableMeta metadata.Table) ([]Row, error)
}

type Service struct {
	mu          sync.RWMutex
	rows        RowRepository
	history     history.Store
	rowChangeFn RowChangeHandler
}

func NewService(historyStore history.Store) *Service {
	return NewServiceWithRepository(historyStore, NewMemoryRowRepository())
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

func (service *Service) CreateRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID, dbName, tableName string, values map[string]any) (Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Row{}, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName
	if err := validateWritableFields(tableMeta, perms, actorID, resource, values); err != nil {
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

func (service *Service) UpdateRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID, dbName, tableName string, recordID int64, values map[string]any) (Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Row{}, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName
	if err := validateWritableFields(tableMeta, perms, actorID, resource, values); err != nil {
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

func (service *Service) DeleteRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID, dbName, tableName string, recordID int64) (Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Row{}, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName
	if !perms.CanWriteResource(actorID, permission.ScopeTable, resource) {
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

func (service *Service) Rows(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID, dbName, tableName, viewName string) ([]Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return nil, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}

	rows, err := service.rows.Rows(ctx, dbName, tableMeta)
	if err != nil {
		return nil, err
	}
	if viewName != "" {
		resolved, err := tableMeta.ResolveView(viewName)
		if err != nil {
			return nil, err
		}
		resource := dbName + "." + tableName
		if !viewFieldsReadable(perms, actorID, resource, resolved.Filters, resolved.Sorts) {
			return nil, fmt.Errorf("%w: view %s", ErrPermissionDenied, viewName)
		}
		rows = applyFilters(rows, resolved.Filters)
		applySorts(rows, resolved.Sorts)
	}

	resource := dbName + "." + tableName
	filtered := make([]Row, 0, len(rows))
	for _, row := range rows {
		values := map[string]any{}
		for fieldName, value := range row.Values {
			if perms.CanReadField(actorID, resource, fieldName) {
				values[fieldName] = value
			}
		}
		filtered = append(filtered, Row{RecordID: row.RecordID, Values: values})
	}
	return filtered, nil
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

func viewFieldsReadable(perms permission.Set, actorID, resource string, filters []metadata.ViewFilter, sorts []metadata.ViewSort) bool {
	for _, filter := range filters {
		if !perms.CanReadField(actorID, resource, filter.Field) {
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

func validateWritableFields(tableMeta metadata.Table, perms permission.Set, actorID, resource string, values map[string]any) error {
	for fieldName := range values {
		field, ok := tableMeta.Field(fieldName)
		if !ok {
			return fmt.Errorf("%w: %s", metadata.ErrUnknownField, fieldName)
		}
		if field.Name == "record_id" {
			return fmt.Errorf("%w: %s", ErrPermissionDenied, fieldName)
		}
		if field.Deleted {
			return fmt.Errorf("%w: %s", ErrDeletedField, fieldName)
		}
		if field.Type == "formula" {
			return fmt.Errorf("%w: %s", ErrPermissionDenied, fieldName)
		}
		if !perms.CanWriteField(actorID, resource, fieldName) {
			return fmt.Errorf("%w: %s", ErrPermissionDenied, fieldName)
		}
	}
	return nil
}

type MemoryRowRepository struct {
	mu     sync.Mutex
	nextID map[string]int64
	rows   map[string]map[int64]Row
}

func NewMemoryRowRepository() *MemoryRowRepository {
	return &MemoryRowRepository{
		nextID: map[string]int64{},
		rows:   map[string]map[int64]Row{},
	}
}

func (repository *MemoryRowRepository) EnsureTable(_ context.Context, _ string, _ metadata.Table) error {
	return nil
}

func (repository *MemoryRowRepository) CreateRow(_ context.Context, dbName string, tableMeta metadata.Table, values map[string]any) (Row, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableMeta.Name
	repository.nextID[resource]++
	recordID := repository.nextID[resource]
	row := Row{RecordID: recordID, Values: cloneValues(values)}
	if repository.rows[resource] == nil {
		repository.rows[resource] = map[int64]Row{}
	}
	repository.rows[resource][recordID] = row
	return row, nil
}

func (repository *MemoryRowRepository) UpdateRow(_ context.Context, dbName string, tableMeta metadata.Table, recordID int64, values map[string]any) (Row, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableMeta.Name
	row, ok := repository.rows[resource][recordID]
	if !ok {
		return Row{}, fmt.Errorf("row %s.%d not found", resource, recordID)
	}
	nextValues := cloneValues(row.Values)
	for key, value := range values {
		nextValues[key] = value
	}
	row.Values = nextValues
	repository.rows[resource][recordID] = row
	return Row{RecordID: row.RecordID, Values: cloneValues(row.Values)}, nil
}

func (repository *MemoryRowRepository) DeleteRow(_ context.Context, dbName string, tableMeta metadata.Table, recordID int64) (Row, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableMeta.Name
	row, ok := repository.rows[resource][recordID]
	if !ok {
		return Row{}, fmt.Errorf("row %s.%d not found", resource, recordID)
	}
	delete(repository.rows[resource], recordID)
	return Row{RecordID: row.RecordID, Values: cloneValues(row.Values)}, nil
}

func (repository *MemoryRowRepository) Row(_ context.Context, dbName string, tableMeta metadata.Table, recordID int64) (Row, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableMeta.Name
	row, ok := repository.rows[resource][recordID]
	if !ok {
		return Row{}, fmt.Errorf("row %s.%d not found", resource, recordID)
	}
	return Row{RecordID: row.RecordID, Values: cloneValues(row.Values)}, nil
}

func (repository *MemoryRowRepository) RestoreRow(_ context.Context, dbName string, tableMeta metadata.Table, row Row) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableMeta.Name
	if repository.rows[resource] == nil {
		repository.rows[resource] = map[int64]Row{}
	}
	repository.rows[resource][row.RecordID] = Row{RecordID: row.RecordID, Values: cloneValues(row.Values)}
	if repository.nextID[resource] < row.RecordID {
		repository.nextID[resource] = row.RecordID
	}
	return nil
}

func (repository *MemoryRowRepository) Rows(_ context.Context, dbName string, tableMeta metadata.Table) ([]Row, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableMeta.Name
	rows := make([]Row, 0, len(repository.rows[resource]))
	for _, row := range repository.rows[resource] {
		rows = append(rows, Row{RecordID: row.RecordID, Values: cloneValues(row.Values)})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].RecordID < rows[j].RecordID
	})
	return rows, nil
}

func applyFilters(rows []Row, filters []metadata.ViewFilter) []Row {
	filtered := rows[:0]
	for _, row := range rows {
		if rowMatchesFilters(row, filters) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func rowMatchesFilters(row Row, filters []metadata.ViewFilter) bool {
	for _, filter := range filters {
		value := rowValue(row, filter.Field)
		switch filter.Op {
		case "eq":
			if fmt.Sprint(value) != fmt.Sprint(filter.Value) {
				return false
			}
		case "contains":
			if !strings.Contains(strings.ToLower(fmt.Sprint(value)), strings.ToLower(fmt.Sprint(filter.Value))) {
				return false
			}
		case "not_empty":
			if strings.TrimSpace(fmt.Sprint(value)) == "" || value == nil {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func applySorts(rows []Row, sorts []metadata.ViewSort) {
	sort.SliceStable(rows, func(i, j int) bool {
		for _, sortDef := range sorts {
			left := fmt.Sprint(rowValue(rows[i], sortDef.Field))
			right := fmt.Sprint(rowValue(rows[j], sortDef.Field))
			if left == right {
				continue
			}
			if sortDef.Direction == "desc" {
				return left > right
			}
			return left < right
		}
		return rows[i].RecordID < rows[j].RecordID
	})
}

func rowValue(row Row, field string) any {
	if field == "record_id" {
		return row.RecordID
	}
	return row.Values[field]
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
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := runtime.Set("field_record_id", recordID); err != nil {
		return nil, err
	}
	if err := runtime.Set("var_now", now.UnixMilli()); err != nil {
		return nil, err
	}
	if err := runtime.Set("var_today", today.UnixMilli()); err != nil {
		return nil, err
	}
	for key, value := range values {
		if err := runtime.Set("field_"+key, value); err != nil {
			return nil, err
		}
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
		if oldOK && newOK && reflect.DeepEqual(oldValue, newValue) {
			continue
		}
		diff[key] = history.FieldDiff{Old: oldValue, New: newValue}
	}
	return diff
}
