package table

import (
	"context"
	"errors"
	"fmt"
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

type Service struct {
	mu      sync.Mutex
	nextID  map[string]int64
	rows    map[string]map[int64]Row
	history history.Store
}

func NewService(historyStore history.Store) *Service {
	return &Service{
		nextID:  map[string]int64{},
		rows:    map[string]map[int64]Row{},
		history: historyStore,
	}
}

func (service *Service) CreateRow(ctx context.Context, catalog metadata.Catalog, perms permission.Set, actorID, dbName, tableName string, values map[string]any) (Row, error) {
	tableMeta, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Row{}, fmt.Errorf("table %s.%s not found", dbName, tableName)
	}
	resource := dbName + "." + tableName
	for fieldName := range values {
		field, ok := tableMeta.Field(fieldName)
		if !ok {
			return Row{}, fmt.Errorf("%w: %s", metadata.ErrUnknownField, fieldName)
		}
		if field.Deleted {
			return Row{}, fmt.Errorf("%w: %s", ErrDeletedField, fieldName)
		}
		if !perms.CanWriteField(actorID, resource, fieldName) {
			return Row{}, fmt.Errorf("%w: %s", ErrPermissionDenied, fieldName)
		}
	}
	for _, field := range tableMeta.ActiveFields() {
		if field.Required {
			if _, ok := values[field.Name]; !ok {
				return Row{}, fmt.Errorf("required field %q is missing", field.Name)
			}
		}
	}

	service.mu.Lock()
	defer service.mu.Unlock()

	recordID := service.nextRecordID(resource)
	row := Row{RecordID: recordID, Values: cloneValues(values)}
	if service.rows[resource] == nil {
		service.rows[resource] = map[int64]Row{}
	}
	service.rows[resource][recordID] = row

	_, err := history.SaveRowChange(ctx, service.history, history.RowChange{
		Database:  dbName,
		Table:     tableName,
		RecordID:  recordID,
		Timestamp: time.Now().UTC(),
		Values:    cloneValues(values),
		ActorID:   actorID,
	})
	if err != nil {
		return Row{}, err
	}
	return row, nil
}

func (service *Service) nextRecordID(resource string) int64 {
	service.nextID[resource]++
	return service.nextID[resource]
}

func cloneValues(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
