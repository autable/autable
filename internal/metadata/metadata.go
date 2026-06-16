package metadata

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

type Catalog struct {
	Databases []Database `yaml:"databases"`
}

type Database struct {
	Name       string  `yaml:"name"`
	SQLitePath string  `yaml:"sqlite_path"`
	Tables     []Table `yaml:"tables"`
}

type Table struct {
	Name        string  `yaml:"name"`
	DisplayName string  `yaml:"display_name"`
	Fields      []Field `yaml:"fields"`
}

type Field struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Deleted  bool   `yaml:"deleted"`
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

var ErrUnknownField = errors.New("unknown field")
