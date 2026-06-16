package metadata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

type Catalog struct {
	Databases []Database `yaml:"databases" json:"databases"`
}

type Database struct {
	Name       string  `yaml:"name" json:"name"`
	SQLitePath string  `yaml:"sqlite_path" json:"sqlite_path"`
	Tables     []Table `yaml:"tables" json:"tables"`
}

type Table struct {
	Name        string  `yaml:"name" json:"name"`
	DisplayName string  `yaml:"display_name" json:"display_name"`
	Fields      []Field `yaml:"fields" json:"fields"`
	Views       []View  `yaml:"views" json:"views"`
}

type Field struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Required bool   `yaml:"required" json:"required"`
	Deleted  bool   `yaml:"deleted" json:"deleted"`
}

type View struct {
	Name        string       `yaml:"name" json:"name"`
	DisplayName string       `yaml:"display_name" json:"display_name"`
	BaseView    string       `yaml:"base_view" json:"base_view,omitempty"`
	Filters     []ViewFilter `yaml:"filters" json:"filters"`
	Sorts       []ViewSort   `yaml:"sorts" json:"sorts"`
}

type ViewFilter struct {
	Field string `yaml:"field" json:"field"`
	Op    string `yaml:"op" json:"op"`
	Value any    `yaml:"value" json:"value,omitempty"`
}

type ViewSort struct {
	Field     string `yaml:"field" json:"field"`
	Direction string `yaml:"direction" json:"direction"`
}

type ResolvedView struct {
	Name    string       `json:"name"`
	Filters []ViewFilter `json:"filters"`
	Sorts   []ViewSort   `json:"sorts"`
}

func Load(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}

	var catalog Catalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return Catalog{}, err
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func Save(path string, catalog Catalog) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(catalog)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (catalog Catalog) Validate() error {
	seenDBs := map[string]struct{}{}
	for dbIndex, db := range catalog.Databases {
		if db.Name == "" {
			return fmt.Errorf("databases[%d].name is required", dbIndex)
		}
		if _, ok := seenDBs[db.Name]; ok {
			return fmt.Errorf("database %q is duplicated", db.Name)
		}
		seenDBs[db.Name] = struct{}{}
		if db.SQLitePath == "" {
			return fmt.Errorf("database %q sqlite_path is required", db.Name)
		}

		seenTables := map[string]struct{}{}
		for tableIndex, table := range db.Tables {
			if err := table.validate(db.Name, tableIndex); err != nil {
				return err
			}
			if _, ok := seenTables[table.Name]; ok {
				return fmt.Errorf("database %q table %q is duplicated", db.Name, table.Name)
			}
			seenTables[table.Name] = struct{}{}
		}
	}
	return nil
}

func (catalog Catalog) Database(name string) (Database, bool) {
	for _, db := range catalog.Databases {
		if db.Name == name {
			return db, true
		}
	}
	return Database{}, false
}

func (catalog Catalog) AddDatabase(database Database) (Catalog, error) {
	if database.Name == "" {
		return Catalog{}, errors.New("database name is required")
	}
	if database.SQLitePath == "" {
		return Catalog{}, errors.New("database sqlite_path is required")
	}
	if _, ok := catalog.Database(database.Name); ok {
		return Catalog{}, fmt.Errorf("database %q already exists", database.Name)
	}
	next := Catalog{Databases: slices.Clone(catalog.Databases)}
	next.Databases = append(next.Databases, database)
	if err := next.Validate(); err != nil {
		return Catalog{}, err
	}
	return next, nil
}

func (catalog Catalog) AddTable(dbName string, table Table) (Catalog, error) {
	if table.Name == "" {
		return Catalog{}, errors.New("table name is required")
	}
	next := Catalog{Databases: slices.Clone(catalog.Databases)}
	for dbIndex, db := range next.Databases {
		if db.Name != dbName {
			continue
		}
		for _, existing := range db.Tables {
			if existing.Name == table.Name {
				return Catalog{}, fmt.Errorf("database %q table %q already exists", dbName, table.Name)
			}
		}
		db.Tables = append(slices.Clone(db.Tables), table)
		next.Databases[dbIndex] = db
		if err := next.Validate(); err != nil {
			return Catalog{}, err
		}
		return next, nil
	}
	return Catalog{}, fmt.Errorf("database %q not found", dbName)
}

func (catalog Catalog) UpdateTable(dbName, tableName string, table Table) (Catalog, error) {
	if tableName == "" {
		return Catalog{}, errors.New("table name is required")
	}
	if table.Name == "" {
		table.Name = tableName
	}
	if table.Name != tableName {
		return Catalog{}, errors.New("table name cannot be changed")
	}
	next := Catalog{Databases: slices.Clone(catalog.Databases)}
	for dbIndex, db := range next.Databases {
		if db.Name != dbName {
			continue
		}
		tables := slices.Clone(db.Tables)
		for tableIndex, existing := range tables {
			if existing.Name != tableName {
				continue
			}
			tables[tableIndex] = table
			db.Tables = tables
			next.Databases[dbIndex] = db
			if err := next.Validate(); err != nil {
				return Catalog{}, err
			}
			return next, nil
		}
		return Catalog{}, fmt.Errorf("database %q table %q not found", dbName, tableName)
	}
	return Catalog{}, fmt.Errorf("database %q not found", dbName)
}

func (table Table) validate(dbName string, tableIndex int) error {
	if table.Name == "" {
		return fmt.Errorf("database %q tables[%d].name is required", dbName, tableIndex)
	}
	seenFields := map[string]struct{}{"record_id": {}}
	for _, field := range table.Fields {
		if field.Name == "" {
			return fmt.Errorf("database %q table %q contains a field without a name", dbName, table.Name)
		}
		if _, ok := seenFields[field.Name]; ok {
			return fmt.Errorf("database %q table %q field %q is duplicated or reserved", dbName, table.Name, field.Name)
		}
		seenFields[field.Name] = struct{}{}
		if field.Type == "" {
			return fmt.Errorf("database %q table %q field %q type is required", dbName, table.Name, field.Name)
		}
	}
	if err := table.validateViews(dbName); err != nil {
		return err
	}
	return nil
}

func (table Table) validateViews(dbName string) error {
	views := map[string]View{}
	for _, view := range table.Views {
		if view.Name == "" {
			return fmt.Errorf("database %q table %q contains a view without a name", dbName, table.Name)
		}
		if _, ok := views[view.Name]; ok {
			return fmt.Errorf("database %q table %q view %q is duplicated", dbName, table.Name, view.Name)
		}
		views[view.Name] = view
		for _, filter := range view.Filters {
			if _, ok := table.Field(filter.Field); !ok {
				return fmt.Errorf("database %q table %q view %q filter field %q is unknown", dbName, table.Name, view.Name, filter.Field)
			}
			if filter.Op == "" {
				return fmt.Errorf("database %q table %q view %q filter op is required", dbName, table.Name, view.Name)
			}
			if !slices.Contains([]string{"eq", "contains", "not_empty"}, filter.Op) {
				return fmt.Errorf("database %q table %q view %q filter op %q is unsupported", dbName, table.Name, view.Name, filter.Op)
			}
		}
		for _, sort := range view.Sorts {
			if _, ok := table.Field(sort.Field); !ok {
				return fmt.Errorf("database %q table %q view %q sort field %q is unknown", dbName, table.Name, view.Name, sort.Field)
			}
			if sort.Direction != "asc" && sort.Direction != "desc" {
				return fmt.Errorf("database %q table %q view %q sort direction must be asc or desc", dbName, table.Name, view.Name)
			}
		}
	}
	for _, view := range table.Views {
		if _, err := table.resolveView(view.Name, map[string]bool{}); err != nil {
			return fmt.Errorf("database %q table %q view %q: %w", dbName, table.Name, view.Name, err)
		}
	}
	return nil
}

func (catalog Catalog) Table(dbName, tableName string) (Table, bool) {
	for _, db := range catalog.Databases {
		if db.Name != dbName {
			continue
		}
		for _, table := range db.Tables {
			if table.Name == tableName {
				return table, true
			}
		}
	}
	return Table{}, false
}

func (table Table) ActiveFields() []Field {
	return slices.DeleteFunc(slices.Clone(table.Fields), func(field Field) bool {
		return field.Deleted
	})
}

func (table Table) Field(name string) (Field, bool) {
	if name == "record_id" {
		return Field{Name: "record_id", Type: "integer", Required: true}, true
	}
	for _, field := range table.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

func (table Table) View(name string) (View, bool) {
	for _, view := range table.Views {
		if view.Name == name {
			return view, true
		}
	}
	return View{}, false
}

func (table Table) ResolveView(name string) (ResolvedView, error) {
	return table.resolveView(name, map[string]bool{})
}

func (table Table) resolveView(name string, visiting map[string]bool) (ResolvedView, error) {
	view, ok := table.View(name)
	if !ok {
		return ResolvedView{}, fmt.Errorf("%w: %s", ErrUnknownView, name)
	}
	if visiting[name] {
		return ResolvedView{}, fmt.Errorf("%w: %s", ErrViewCycle, name)
	}
	visiting[name] = true
	defer delete(visiting, name)

	resolved := ResolvedView{Name: name}
	if view.BaseView != "" {
		base, err := table.resolveView(view.BaseView, visiting)
		if err != nil {
			return ResolvedView{}, err
		}
		resolved.Filters = append(resolved.Filters, base.Filters...)
		resolved.Sorts = append(resolved.Sorts, base.Sorts...)
	}
	resolved.Filters = append(resolved.Filters, view.Filters...)
	resolved.Sorts = append(resolved.Sorts, view.Sorts...)
	return resolved, nil
}

var (
	ErrUnknownField = errors.New("unknown field")
	ErrUnknownView  = errors.New("unknown view")
	ErrViewCycle    = errors.New("view cycle")
)
