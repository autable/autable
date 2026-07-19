package metadata

import (
	"autable/internal/repository"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type Catalog struct {
	Databases []Database `yaml:"databases" json:"databases"`
}

type Database struct {
	Name                    string  `yaml:"name" json:"name"`
	Tables                  []Table `yaml:"tables" json:"tables"`
	PermissionLevel         int     `yaml:"-" json:"permission_level"`
	WorkflowPermissionLevel int     `yaml:"-" json:"workflow_permission_level"`
	FormPermissionLevel     int     `yaml:"-" json:"form_permission_level"`
}

type Table struct {
	Name                    string  `yaml:"name" json:"name"`
	DisplayName             string  `yaml:"display_name" json:"display_name"`
	Fields                  []Field `yaml:"fields" json:"fields"`
	Views                   []View  `yaml:"views" json:"views"`
	PermissionLevel         int     `yaml:"-" json:"permission_level"`
	DatabasePermissionLevel int     `yaml:"-" json:"database_permission_level"`
	FieldPermissionLevel    int     `yaml:"-" json:"field_permission_level"`
	ViewPermissionLevel     int     `yaml:"-" json:"view_permission_level"`
}

type Field struct {
	Name            string `yaml:"name" json:"name"`
	Type            string `yaml:"type" json:"type"`
	ValueType       string `yaml:"value_type,omitempty" json:"value_type,omitempty"`
	Formula         string `yaml:"formula,omitempty" json:"formula,omitempty"`
	RelationTable   string `yaml:"relation_table,omitempty" json:"relation_table,omitempty"`
	Deleted         bool   `yaml:"deleted" json:"deleted"`
	PermissionLevel int    `yaml:"-" json:"permission_level,omitempty"`
}

type View struct {
	Name            string     `yaml:"name" json:"name"`
	DisplayName     string     `yaml:"display_name" json:"display_name"`
	BaseView        string     `yaml:"base_view" json:"base_view,omitempty"`
	Query           *ViewQuery `yaml:"query,omitempty" json:"query,omitempty"`
	Sorts           []ViewSort `yaml:"sorts" json:"sorts"`
	PermissionLevel int        `yaml:"-" json:"permission_level,omitempty"`
}

type ViewQuery struct {
	Combinator string          `yaml:"combinator" json:"combinator"`
	Rules      []ViewQueryRule `yaml:"rules" json:"rules"`
	Not        bool            `yaml:"not,omitempty" json:"not,omitempty"`
}

type ViewQueryRule struct {
	Field      string          `yaml:"field,omitempty" json:"field,omitempty"`
	Operator   string          `yaml:"operator,omitempty" json:"operator,omitempty"`
	Value      any             `yaml:"value,omitempty" json:"value,omitempty"`
	Combinator string          `yaml:"combinator,omitempty" json:"combinator,omitempty"`
	Rules      []ViewQueryRule `yaml:"rules,omitempty" json:"rules,omitempty"`
	Not        bool            `yaml:"not,omitempty" json:"not,omitempty"`
}

type ViewSort struct {
	Field     string `yaml:"field" json:"field"`
	Direction string `yaml:"direction" json:"direction"`
}

type ResolvedView struct {
	Name   string     `json:"name"`
	Query  *ViewQuery `json:"query,omitempty"`
	Sorts  []ViewSort `json:"sorts"`
	Limit  int        `json:"limit,omitempty"`
	Offset int        `json:"offset,omitempty"`
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

func LoadOrCreate(path string) (Catalog, error) {
	catalog, err := Load(path)
	if err == nil {
		return catalog, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Catalog{}, err
	}
	catalog = Catalog{}
	if err := Save(path, catalog); err != nil {
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
	return repository.WriteFileAtomic(path, data, 0o644)
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
		for _, table := range db.Tables {
			for _, field := range table.Fields {
				if field.Type == "relation" {
					if _, ok := seenTables[field.RelationTable]; !ok {
						return fmt.Errorf("database %q table %q relation field %q target table %q is unknown", db.Name, table.Name, field.Name, field.RelationTable)
					}
				}
			}
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
			if err := validateFieldTypesUnchanged(existing, table); err != nil {
				return Catalog{}, err
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

func (catalog Catalog) MergeTable(dbName, tableName string, patch Table) (Catalog, error) {
	existing, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Catalog{}, fmt.Errorf("database %q table %q not found", dbName, tableName)
	}
	if patch.Name == "" {
		patch.Name = tableName
	}
	if patch.Name != tableName {
		return Catalog{}, errors.New("table name cannot be changed")
	}
	next := existing
	next.Name = tableName
	if patch.DisplayName != "" {
		next.DisplayName = patch.DisplayName
	}
	if patch.Fields != nil {
		next.Fields = mergeFields(existing.Fields, patch.Fields)
	}
	if patch.Views != nil {
		next.Views = mergeViews(existing.Views, patch.Views)
	}
	return catalog.UpdateTable(dbName, tableName, next)
}

func mergeFields(existing []Field, patch []Field) []Field {
	merged := slices.Clone(existing)
	for _, field := range patch {
		index := slices.IndexFunc(merged, func(existingField Field) bool {
			return existingField.Name == field.Name
		})
		if index < 0 {
			merged = append(merged, field)
			continue
		}
		merged[index] = field
	}
	return merged
}

func mergeViews(existing []View, patch []View) []View {
	merged := slices.Clone(existing)
	for _, view := range patch {
		index := slices.IndexFunc(merged, func(existingView View) bool {
			return existingView.Name == view.Name
		})
		if index < 0 {
			merged = append(merged, view)
			continue
		}
		merged[index] = view
	}
	return merged
}

func (catalog Catalog) MoveFieldToStart(dbName, tableName, fieldName string) (Catalog, error) {
	return catalog.moveField(dbName, tableName, fieldName, 0)
}

func (catalog Catalog) MoveFieldBefore(dbName, tableName, fieldName, targetFieldName string) (Catalog, error) {
	return catalog.moveFieldRelative(dbName, tableName, fieldName, targetFieldName, false)
}

func (catalog Catalog) MoveFieldAfter(dbName, tableName, fieldName, targetFieldName string) (Catalog, error) {
	return catalog.moveFieldRelative(dbName, tableName, fieldName, targetFieldName, true)
}

func (catalog Catalog) moveFieldRelative(dbName, tableName, fieldName, targetFieldName string, after bool) (Catalog, error) {
	if targetFieldName == "" {
		return Catalog{}, errors.New("target field is required")
	}
	if fieldName == targetFieldName {
		return Catalog{}, errors.New("field cannot be moved relative to itself")
	}
	table, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Catalog{}, fmt.Errorf("database %q table %q not found", dbName, tableName)
	}
	fields := slices.Clone(table.Fields)
	sourceIndex := slices.IndexFunc(fields, func(field Field) bool { return field.Name == fieldName })
	if sourceIndex < 0 {
		return Catalog{}, fmt.Errorf("field %q not found", fieldName)
	}
	moved := fields[sourceIndex]
	fields = slices.Delete(fields, sourceIndex, sourceIndex+1)
	targetIndex := slices.IndexFunc(fields, func(field Field) bool { return field.Name == targetFieldName })
	if targetIndex < 0 {
		return Catalog{}, fmt.Errorf("target field %q not found", targetFieldName)
	}
	if after {
		targetIndex++
	}
	fields = slices.Insert(fields, targetIndex, moved)
	table.Fields = fields
	return catalog.UpdateTable(dbName, tableName, table)
}

func (catalog Catalog) moveField(dbName, tableName, fieldName string, index int) (Catalog, error) {
	if fieldName == "" {
		return Catalog{}, errors.New("field is required")
	}
	table, ok := catalog.Table(dbName, tableName)
	if !ok {
		return Catalog{}, fmt.Errorf("database %q table %q not found", dbName, tableName)
	}
	fields := slices.Clone(table.Fields)
	sourceIndex := slices.IndexFunc(fields, func(field Field) bool { return field.Name == fieldName })
	if sourceIndex < 0 {
		return Catalog{}, fmt.Errorf("field %q not found", fieldName)
	}
	moved := fields[sourceIndex]
	fields = slices.Delete(fields, sourceIndex, sourceIndex+1)
	fields = slices.Insert(fields, index, moved)
	table.Fields = fields
	return catalog.UpdateTable(dbName, tableName, table)
}

func validateFieldTypesUnchanged(existing Table, next Table) error {
	existingTypes := map[string]Field{}
	for _, field := range existing.Fields {
		existingTypes[field.Name] = field
	}
	for _, field := range next.Fields {
		existingField, ok := existingTypes[field.Name]
		if !ok {
			continue
		}
		if field.Type != existingField.Type {
			return fmt.Errorf("field %q type cannot be changed", field.Name)
		}
		if field.Type == "formula" && field.ValueType != existingField.ValueType {
			return fmt.Errorf("formula field %q value_type cannot be changed", field.Name)
		}
	}
	return nil
}

func (table Table) validate(dbName string, tableIndex int) error {
	if table.Name == "" {
		return fmt.Errorf("database %q tables[%d].name is required", dbName, tableIndex)
	}
	seenFields := map[string]struct{}{}
	for _, field := range table.Fields {
		if field.Name == "" {
			return fmt.Errorf("database %q table %q contains a field without a name", dbName, table.Name)
		}
		if strings.HasPrefix(field.Name, "ct_") {
			return fmt.Errorf("database %q table %q field %q uses reserved prefix ct_", dbName, table.Name, field.Name)
		}
		if _, ok := seenFields[field.Name]; ok {
			return fmt.Errorf("database %q table %q field %q is duplicated or reserved", dbName, table.Name, field.Name)
		}
		seenFields[field.Name] = struct{}{}
		if field.Type == "" {
			return fmt.Errorf("database %q table %q field %q type is required", dbName, table.Name, field.Name)
		}
		if field.Type != "int" && field.Type != "float" && field.Type != "string" && field.Type != "formula" && field.Type != "relation" && field.Type != "file" {
			return fmt.Errorf("database %q table %q field %q type %q is unsupported", dbName, table.Name, field.Name, field.Type)
		}
		if field.Type == "relation" && strings.TrimSpace(field.RelationTable) == "" {
			return fmt.Errorf("database %q table %q relation field %q relation_table is required", dbName, table.Name, field.Name)
		}
		if field.Type != "relation" && strings.TrimSpace(field.RelationTable) != "" {
			return fmt.Errorf("database %q table %q field %q relation_table is only allowed on relation fields", dbName, table.Name, field.Name)
		}
		if field.Type == "formula" && strings.TrimSpace(field.Formula) == "" {
			return fmt.Errorf("database %q table %q formula field %q formula is required", dbName, table.Name, field.Name)
		}
		if field.Type == "formula" && !isStoredFieldType(field.ValueType) {
			return fmt.Errorf("database %q table %q formula field %q value_type is required", dbName, table.Name, field.Name)
		}
		if field.Type != "formula" && strings.TrimSpace(field.Formula) != "" {
			return fmt.Errorf("database %q table %q field %q formula is only allowed on formula fields", dbName, table.Name, field.Name)
		}
		if field.Type != "formula" && field.ValueType != "" {
			return fmt.Errorf("database %q table %q field %q value_type is only allowed on formula fields", dbName, table.Name, field.Name)
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
		if view.Name == AllViewName {
			return fmt.Errorf("database %q table %q view name %q is reserved for the built-in unfiltered view", dbName, table.Name, AllViewName)
		}
		if _, ok := views[view.Name]; ok {
			return fmt.Errorf("database %q table %q view %q is duplicated", dbName, table.Name, view.Name)
		}
		views[view.Name] = view
		if err := table.validateViewQuery(dbName, view.Name, view.Query); err != nil {
			return err
		}
		for _, sort := range view.Sorts {
			field, ok := table.Field(sort.Field)
			if !ok {
				return fmt.Errorf("database %q table %q view %q sort field %q is unknown", dbName, table.Name, view.Name, sort.Field)
			}
			if field.Deleted {
				return fmt.Errorf("database %q table %q view %q sort field %q is deleted", dbName, table.Name, view.Name, sort.Field)
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

func (table Table) validateViewQuery(dbName, viewName string, query *ViewQuery) error {
	if query == nil {
		return nil
	}
	if strings.TrimSpace(query.Combinator) == "" && len(query.Rules) == 0 && !query.Not {
		return nil
	}
	if !isViewQueryCombinator(query.Combinator) {
		return fmt.Errorf("database %q table %q view %q query combinator %q is unsupported", dbName, table.Name, viewName, query.Combinator)
	}
	for _, rule := range query.Rules {
		if err := table.validateViewQueryRule(dbName, viewName, rule); err != nil {
			return err
		}
	}
	return nil
}

func (table Table) validateViewQueryRule(dbName, viewName string, rule ViewQueryRule) error {
	if rule.Combinator != "" || len(rule.Rules) > 0 {
		group := ViewQuery{Combinator: rule.Combinator, Rules: rule.Rules, Not: rule.Not}
		return table.validateViewQuery(dbName, viewName, &group)
	}
	field, ok := table.Field(rule.Field)
	if !ok {
		return fmt.Errorf("database %q table %q view %q query field %q is unknown", dbName, table.Name, viewName, rule.Field)
	}
	if field.Deleted {
		return fmt.Errorf("database %q table %q view %q query field %q is deleted", dbName, table.Name, viewName, rule.Field)
	}
	if !isViewQueryOperator(rule.Operator) {
		return fmt.Errorf("database %q table %q view %q query operator %q is unsupported", dbName, table.Name, viewName, rule.Operator)
	}
	return nil
}

func isViewQueryCombinator(combinator string) bool {
	return combinator == "and" || combinator == "or"
}

func isViewQueryOperator(operator string) bool {
	return slices.Contains([]string{
		"=",
		"!=",
		"<",
		"<=",
		">",
		">=",
		"contains",
		"beginsWith",
		"endsWith",
		"doesNotContain",
		"doesNotBeginWith",
		"doesNotEndWith",
		"null",
		"notNull",
	}, operator)
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
	if name == "ct_record_id" {
		return Field{Name: "ct_record_id", Type: "int"}, true
	}
	for _, field := range table.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

func (field Field) StorageType() string {
	if field.Type == "formula" {
		return field.ValueType
	}
	// Relation cells store the target record ID; file cells store the
	// uploaded file ID.
	if field.Type == "relation" || field.Type == "file" {
		return "int"
	}
	return field.Type
}

func isStoredFieldType(fieldType string) bool {
	return fieldType == "int" || fieldType == "float" || fieldType == "string"
}

func (table Table) View(name string) (View, bool) {
	for _, view := range table.Views {
		if view.Name == name {
			return view, true
		}
	}
	return View{}, false
}

// AllViewName is the built-in unfiltered view covering every row. It is not
// stored in table metadata; access to it is granted like any other view.
const AllViewName = "all"

func (table Table) ResolveView(name string) (ResolvedView, error) {
	return table.resolveView(name, map[string]bool{})
}

func (table Table) resolveView(name string, visiting map[string]bool) (ResolvedView, error) {
	if name == AllViewName {
		return ResolvedView{Name: AllViewName}, nil
	}
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
		resolved.Query = cloneViewQuery(base.Query)
		resolved.Sorts = append(resolved.Sorts, base.Sorts...)
	}
	resolved.Query = combineViewQueries(resolved.Query, view.Query)
	resolved.Sorts = append(resolved.Sorts, view.Sorts...)
	return resolved, nil
}

func combineViewQueries(base, child *ViewQuery) *ViewQuery {
	base = cloneViewQuery(base)
	child = cloneViewQuery(child)
	if isEmptyViewQuery(base) {
		return child
	}
	if isEmptyViewQuery(child) {
		return base
	}
	return &ViewQuery{
		Combinator: "and",
		Rules: []ViewQueryRule{
			viewQueryToRule(*base),
			viewQueryToRule(*child),
		},
	}
}

func cloneViewQuery(query *ViewQuery) *ViewQuery {
	if query == nil {
		return nil
	}
	cloned := &ViewQuery{
		Combinator: query.Combinator,
		Rules:      cloneViewQueryRules(query.Rules),
		Not:        query.Not,
	}
	return cloned
}

func cloneViewQueryRules(rules []ViewQueryRule) []ViewQueryRule {
	if len(rules) == 0 {
		return nil
	}
	cloned := make([]ViewQueryRule, len(rules))
	for index, rule := range rules {
		cloned[index] = rule
		cloned[index].Rules = cloneViewQueryRules(rule.Rules)
	}
	return cloned
}

func isEmptyViewQuery(query *ViewQuery) bool {
	return query == nil || (strings.TrimSpace(query.Combinator) == "" && len(query.Rules) == 0 && !query.Not)
}

func viewQueryToRule(query ViewQuery) ViewQueryRule {
	return ViewQueryRule{
		Combinator: query.Combinator,
		Rules:      cloneViewQueryRules(query.Rules),
		Not:        query.Not,
	}
}

var (
	ErrUnknownField = errors.New("unknown field")
	ErrUnknownView  = errors.New("unknown view")
	ErrViewCycle    = errors.New("view cycle")
)
