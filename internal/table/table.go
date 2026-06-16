package table

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"codetable/internal/history"
	"codetable/internal/metadata"
	"codetable/internal/permission"
)

var (
	ErrPermissionDenied = errors.New("permission denied")
	ErrDeletedField     = errors.New("field is soft-deleted")
)

type Row struct {
	RecordID int64
	Values   map[string]any
}

type RowRepository interface {
	CreateRow(ctx context.Context, dbName, tableName string, values map[string]any) (Row, error)
	UpdateRow(ctx context.Context, dbName, tableName string, recordID int64, values map[string]any) (Row, error)
	DeleteRow(ctx context.Context, dbName, tableName string, recordID int64) (Row, error)
	Rows(ctx context.Context, dbName, tableName string) ([]Row, error)
}

type Service struct {
	rows    RowRepository
	history history.Store
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

func (service *Service) CreateRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID, dbName, tableName string, values map[string]any) (Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Row{}, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName
	if err := validateWritableFields(tableMeta, perms, actorID, resource, values); err != nil {
		return Row{}, err
	}
	for _, field := range tableMeta.ActiveFields() {
		if field.Required {
			if _, ok := values[field.Name]; !ok {
				return Row{}, fmt.Errorf("required field %q is missing", field.Name)
			}
		}
	}

	row, err := service.rows.CreateRow(ctx, dbName, tableName, cloneValues(values))
	if err != nil {
		return Row{}, err
	}
	_, err = history.SaveRowChange(ctx, service.history, history.RowChange{
		Database:  dbName,
		Table:     tableName,
		RecordID:  row.RecordID,
		Timestamp: time.Now().UTC(),
		Operation: "create",
		Values:    cloneValues(row.Values),
		ActorID:   actorID,
	})
	if err != nil {
		return Row{}, err
	}
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

	row, err := service.rows.UpdateRow(ctx, dbName, tableName, recordID, cloneValues(values))
	if err != nil {
		return Row{}, err
	}
	_, err = history.SaveRowChange(ctx, service.history, history.RowChange{
		Database:  dbName,
		Table:     tableName,
		RecordID:  row.RecordID,
		Timestamp: time.Now().UTC(),
		Operation: "update",
		Values:    cloneValues(row.Values),
		ActorID:   actorID,
	})
	if err != nil {
		return Row{}, err
	}
	return row, nil
}

func (service *Service) DeleteRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID, dbName, tableName string, recordID int64) (Row, error) {
	if _, ok := catalog.Table(dbName, tableName); !ok {
		return Row{}, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName
	if !perms.CanWriteResource(actorID, permission.ScopeTable, resource) {
		return Row{}, fmt.Errorf("%w: %s", ErrPermissionDenied, resource)
	}

	row, err := service.rows.DeleteRow(ctx, dbName, tableName, recordID)
	if err != nil {
		return Row{}, err
	}
	_, err = history.SaveRowChange(ctx, service.history, history.RowChange{
		Database:  dbName,
		Table:     tableName,
		RecordID:  row.RecordID,
		Timestamp: time.Now().UTC(),
		Operation: "delete",
		Values:    cloneValues(row.Values),
		ActorID:   actorID,
	})
	if err != nil {
		return Row{}, err
	}
	return row, nil
}

func (service *Service) Rows(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID, dbName, tableName, viewName string) ([]Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return nil, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}

	rows, err := service.rows.Rows(ctx, dbName, tableName)
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

func (repository *MemoryRowRepository) CreateRow(_ context.Context, dbName, tableName string, values map[string]any) (Row, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableName
	repository.nextID[resource]++
	recordID := repository.nextID[resource]
	row := Row{RecordID: recordID, Values: cloneValues(values)}
	if repository.rows[resource] == nil {
		repository.rows[resource] = map[int64]Row{}
	}
	repository.rows[resource][recordID] = row
	return row, nil
}

func (repository *MemoryRowRepository) UpdateRow(_ context.Context, dbName, tableName string, recordID int64, values map[string]any) (Row, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableName
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

func (repository *MemoryRowRepository) DeleteRow(_ context.Context, dbName, tableName string, recordID int64) (Row, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableName
	row, ok := repository.rows[resource][recordID]
	if !ok {
		return Row{}, fmt.Errorf("row %s.%d not found", resource, recordID)
	}
	delete(repository.rows[resource], recordID)
	return Row{RecordID: row.RecordID, Values: cloneValues(row.Values)}, nil
}

func (repository *MemoryRowRepository) Rows(_ context.Context, dbName, tableName string) ([]Row, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	resource := dbName + "." + tableName
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

func cloneValues(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
