package api

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"autable/internal/metadata"
)

func (server *Server) createDatabase(ctx context.Context, database metadata.Database) error {
	if server.metadataPath == "" {
		return errors.New("metadata writes are not configured")
	}
	server.catalogMu.Lock()
	defer server.catalogMu.Unlock()
	next, err := server.catalog.AddDatabase(database)
	if err != nil {
		return err
	}
	if server.openDatabase != nil {
		if err := server.openDatabase(ctx, database.Name); err != nil {
			return err
		}
	}
	if err := metadata.Save(server.metadataPath, next); err != nil {
		return err
	}
	server.catalog = next
	return nil
}

func (server *Server) addTable(ctx context.Context, dbName string, tableMeta metadata.Table) error {
	if server.metadataPath == "" {
		return errors.New("metadata writes are not configured")
	}
	server.catalogMu.Lock()
	defer server.catalogMu.Unlock()
	next, err := server.catalog.AddTable(dbName, tableMeta)
	if err != nil {
		return err
	}
	if err := server.tables.SyncTable(ctx, next, dbName, tableMeta.Name); err != nil {
		return err
	}
	if err := metadata.Save(server.metadataPath, next); err != nil {
		return err
	}
	server.catalog = next
	return nil
}

func (server *Server) updateTable(ctx context.Context, dbName, tableName string, tableMeta metadata.Table) (metadata.Table, error) {
	if server.metadataPath == "" {
		return metadata.Table{}, errors.New("metadata writes are not configured")
	}
	server.catalogMu.Lock()
	defer server.catalogMu.Unlock()
	existing, _ := server.catalog.Table(dbName, tableName)
	next, err := server.catalog.MergeTable(dbName, tableName, tableMeta)
	if err != nil {
		return metadata.Table{}, err
	}
	merged, _ := next.Table(dbName, tableName)
	// Recomputing formulas rewrites every row — on large tables that is the
	// dominant cost of a metadata save. View or plain-field edits only need
	// the physical table brought up to date.
	if formulaDefinitionsChanged(existing, merged) {
		if err := server.tables.SyncTable(ctx, next, dbName, tableName); err != nil {
			return metadata.Table{}, err
		}
	} else if err := server.tables.EnsureTable(ctx, next, dbName, tableName); err != nil {
		return metadata.Table{}, err
	}
	if err := metadata.Save(server.metadataPath, next); err != nil {
		return metadata.Table{}, err
	}
	server.catalog = next
	updated, _ := next.Table(dbName, tableName)
	return updated, nil
}

func formulaDefinitionsChanged(existing, next metadata.Table) bool {
	type formulaDef struct {
		Formula   string
		ValueType string
	}
	collect := func(tableMeta metadata.Table) map[string]formulaDef {
		defs := map[string]formulaDef{}
		for _, field := range tableMeta.ActiveFields() {
			if field.Type == "formula" {
				defs[field.Name] = formulaDef{Formula: field.Formula, ValueType: field.ValueType}
			}
		}
		return defs
	}
	return !maps.Equal(collect(existing), collect(next))
}

func (server *Server) moveField(ctx context.Context, dbName, tableName, fieldName string, request fieldPositionRequest) (metadata.Table, error) {
	if server.metadataPath == "" {
		return metadata.Table{}, errors.New("metadata writes are not configured")
	}
	targets := 0
	if request.Position != "" {
		targets++
	}
	if request.Before != "" {
		targets++
	}
	if request.After != "" {
		targets++
	}
	if targets != 1 {
		return metadata.Table{}, errors.New("field position must specify exactly one of position, before, or after")
	}
	server.catalogMu.Lock()
	defer server.catalogMu.Unlock()
	var (
		next metadata.Catalog
		err  error
	)
	switch {
	case request.Position != "":
		if request.Position != "start" {
			return metadata.Table{}, errors.New("field position must be start")
		}
		next, err = server.catalog.MoveFieldToStart(dbName, tableName, fieldName)
	case request.Before != "":
		next, err = server.catalog.MoveFieldBefore(dbName, tableName, fieldName, request.Before)
	case request.After != "":
		next, err = server.catalog.MoveFieldAfter(dbName, tableName, fieldName, request.After)
	}
	if err != nil {
		return metadata.Table{}, err
	}
	tableMeta, ok := next.Table(dbName, tableName)
	if !ok {
		return metadata.Table{}, fmt.Errorf("database %q table %q not found", dbName, tableName)
	}
	if err := metadata.Save(server.metadataPath, next); err != nil {
		return metadata.Table{}, err
	}
	server.catalog = next
	return tableMeta, nil
}
