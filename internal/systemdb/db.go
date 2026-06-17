package systemdb

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codetable/internal/auth"
	"codetable/internal/permission"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	orm *gorm.DB
}

type WorkflowDefinition struct {
	ID              int64             `json:"id"`
	DatabaseName    string            `json:"database_name"`
	Name            string            `json:"name"`
	Script          string            `json:"script"`
	CreatorID       string            `json:"creator_id,omitempty"`
	Secrets         map[string]string `json:"secrets"`
	Variables       map[string]string `json:"variables"`
	PermissionLevel permission.Level  `json:"permission_level,omitempty" gorm:"-"`
	CreatedAt       int64             `json:"created_at"`
	UpdatedAt       int64             `json:"updated_at"`
}

type FormDefinition struct {
	ID              int64            `json:"id"`
	DatabaseName    string           `json:"database_name"`
	Name            string           `json:"name"`
	Script          string           `json:"script"`
	PublishedToken  string           `json:"published_token,omitempty"`
	PermissionLevel permission.Level `json:"permission_level,omitempty" gorm:"-"`
	CreatedAt       int64            `json:"created_at"`
	UpdatedAt       int64            `json:"updated_at"`
}

type RoleDefinition struct {
	ID           int64              `json:"id"`
	DatabaseName string             `json:"database_name"`
	Name         string             `json:"name"`
	SubjectID    string             `json:"subject_id"`
	Grants       []permission.Grant `json:"grants"`
	Members      []string           `json:"members"`
	CreatedAt    int64              `json:"created_at"`
	UpdatedAt    int64              `json:"updated_at"`
}

type userModel struct {
	ID           string `gorm:"primaryKey"`
	Email        string `gorm:"uniqueIndex;not null"`
	Provider     string `gorm:"not null"`
	ProviderName string `gorm:"not null"`
	Subject      string `gorm:"not null"`
	PasswordHash []byte
	CreatedAt    int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64 `gorm:"autoUpdateTime:milli"`
}

type sessionModel struct {
	TokenHash string `gorm:"primaryKey"`
	UserID    string `gorm:"index;not null"`
	ExpiresAt int64  `gorm:"index;not null"`
	CreatedAt int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt int64  `gorm:"autoUpdateTime:milli"`
}

type permissionGrantModel struct {
	ID        int64            `gorm:"primaryKey;autoIncrement"`
	SubjectID string           `gorm:"uniqueIndex:idx_permission_target;not null"`
	Scope     permission.Scope `gorm:"uniqueIndex:idx_permission_target;not null"`
	Resource  string           `gorm:"uniqueIndex:idx_permission_target;not null"`
	Field     string           `gorm:"uniqueIndex:idx_permission_target;not null;default:''"`
	Level     permission.Level `gorm:"not null"`
}

type workflowModel struct {
	ID            int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName  string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Name          string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Script        string `gorm:"not null"`
	CreatorID     string `gorm:"index;not null;default:''"`
	SecretsJSON   string `gorm:"not null"`
	VariablesJSON string `gorm:"not null"`
	CreatedAt     int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt     int64  `gorm:"autoUpdateTime:milli"`
}

type formModel struct {
	ID             int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName   string `gorm:"uniqueIndex:idx_form_database_name;not null"`
	Name           string `gorm:"uniqueIndex:idx_form_database_name;not null"`
	Script         string `gorm:"not null"`
	PublishedToken string `gorm:"index"`
	CreatedAt      int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt      int64  `gorm:"autoUpdateTime:milli"`
}

type roleModel struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName string `gorm:"uniqueIndex:idx_role_database_name;not null"`
	Name         string `gorm:"uniqueIndex:idx_role_database_name;not null"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli"`
}

type roleMemberModel struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName string `gorm:"uniqueIndex:idx_role_member;not null"`
	RoleName     string `gorm:"uniqueIndex:idx_role_member;not null"`
	UserID       string `gorm:"uniqueIndex:idx_role_member;not null"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli"`
}

func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	orm, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	db := &DB{orm: orm.WithContext(ctx)}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error {
	handle, err := db.orm.DB()
	if err != nil {
		return err
	}
	return handle.Close()
}

func (db *DB) Migrate(ctx context.Context) error {
	if err := db.dropIncompatibleTimestampTables(ctx); err != nil {
		return err
	}
	return db.orm.WithContext(ctx).AutoMigrate(
		&userModel{},
		&sessionModel{},
		&permissionGrantModel{},
		&workflowModel{},
		&formModel{},
		&roleModel{},
		&roleMemberModel{},
	)
}

func (db *DB) dropIncompatibleTimestampTables(ctx context.Context) error {
	for _, spec := range []struct {
		model   any
		columns []string
	}{
		{model: &userModel{}, columns: []string{"created_at", "updated_at"}},
		{model: &sessionModel{}, columns: []string{"expires_at", "created_at", "updated_at"}},
		{model: &workflowModel{}, columns: []string{"created_at", "updated_at"}},
		{model: &formModel{}, columns: []string{"created_at", "updated_at"}},
		{model: &roleModel{}, columns: []string{"created_at", "updated_at"}},
		{model: &roleMemberModel{}, columns: []string{"created_at", "updated_at"}},
	} {
		drop, err := db.hasIncompatibleTimestampColumn(ctx, spec.model, spec.columns)
		if err != nil {
			return err
		}
		if drop {
			if err := db.orm.WithContext(ctx).Migrator().DropTable(spec.model); err != nil {
				return err
			}
		}
	}
	return nil
}

func (db *DB) hasIncompatibleTimestampColumn(ctx context.Context, model any, columns []string) (bool, error) {
	migrator := db.orm.WithContext(ctx).Migrator()
	if !migrator.HasTable(model) {
		return false, nil
	}
	columnTypes, err := migrator.ColumnTypes(model)
	if err != nil {
		return false, err
	}
	wanted := map[string]struct{}{}
	for _, column := range columns {
		wanted[column] = struct{}{}
	}
	for _, columnType := range columnTypes {
		name := strings.ToLower(columnType.Name())
		if _, ok := wanted[name]; !ok {
			continue
		}
		dbType := strings.ToUpper(columnType.DatabaseTypeName())
		if !strings.Contains(dbType, "INT") {
			return true, nil
		}
	}
	tableName, err := db.tableName(model)
	if err != nil {
		return false, err
	}
	row, err := db.firstRowValues(ctx, tableName, columns)
	if err != nil {
		return false, err
	}
	if row == nil {
		return false, nil
	}
	for column := range wanted {
		if hasIncompatibleTimestampValue(row[column]) {
			return true, nil
		}
	}
	return false, nil
}

func (db *DB) firstRowValues(ctx context.Context, tableName string, columns []string) (map[string]any, error) {
	rows, err := db.orm.WithContext(ctx).Table(tableName).Select(columns).Limit(1).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]any, len(names))
	destinations := make([]any, len(names))
	for index := range values {
		destinations[index] = &values[index]
	}
	if err := rows.Scan(destinations...); err != nil {
		return nil, err
	}
	result := map[string]any{}
	for index, name := range names {
		result[strings.ToLower(name)] = values[index]
	}
	return result, rows.Err()
}

func (db *DB) tableName(model any) (string, error) {
	statement := &gorm.Statement{DB: db.orm}
	if err := statement.Parse(model); err != nil {
		return "", err
	}
	return statement.Schema.Table, nil
}

func hasIncompatibleTimestampValue(value any) bool {
	if value == nil {
		return false
	}
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return false
	default:
		return true
	}
}

func (db *DB) UpsertUserByEmail(ctx context.Context, user auth.User) (auth.User, error) {
	if user.Email == "" {
		return auth.User{}, errors.New("email is required")
	}

	var existing userModel
	err := db.orm.WithContext(ctx).Where(&userModel{Email: user.Email}).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return auth.User{}, err
	}
	if err == nil {
		user.ID = existing.ID
	}

	model := userToModel(user)
	err = db.orm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "email"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"provider",
			"provider_name",
			"subject",
			"password_hash",
			"updated_at",
		}),
	}).Create(&model).Error
	if err != nil {
		return auth.User{}, err
	}
	return modelToUser(model), nil
}

func (db *DB) UserByEmail(ctx context.Context, email string) (auth.User, error) {
	normalized, err := auth.NormalizeEmail(email)
	if err != nil {
		return auth.User{}, err
	}
	var model userModel
	if err := db.orm.WithContext(ctx).Where(&userModel{Email: normalized}).First(&model).Error; err != nil {
		return auth.User{}, err
	}
	return modelToUser(model), nil
}

func (db *DB) User(ctx context.Context, id string) (auth.User, error) {
	var model userModel
	if err := db.orm.WithContext(ctx).First(&model, &userModel{ID: id}).Error; err != nil {
		return auth.User{}, err
	}
	return modelToUser(model), nil
}

func (db *DB) CreateSession(ctx context.Context, userID string, ttl time.Duration) (auth.Session, error) {
	token, err := auth.NewSessionToken()
	if err != nil {
		return auth.Session{}, err
	}
	session := auth.Session{
		Token:     token,
		UserID:    userID,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	model := sessionModel{
		TokenHash: auth.HashSessionToken(token),
		UserID:    userID,
		ExpiresAt: session.ExpiresAt.UTC().UnixMilli(),
	}
	if err := db.orm.WithContext(ctx).Create(&model).Error; err != nil {
		return auth.Session{}, err
	}
	return session, nil
}

func (db *DB) UserBySessionToken(ctx context.Context, token string) (auth.User, auth.Session, error) {
	var model sessionModel
	err := db.orm.WithContext(ctx).First(&model, &sessionModel{TokenHash: auth.HashSessionToken(token)}).Error
	if err != nil {
		return auth.User{}, auth.Session{}, err
	}
	session := auth.Session{Token: token, UserID: model.UserID, ExpiresAt: time.UnixMilli(model.ExpiresAt).UTC()}
	if model.ExpiresAt <= time.Now().UTC().UnixMilli() {
		_ = db.DeleteSession(ctx, token)
		return auth.User{}, session, gorm.ErrRecordNotFound
	}
	user, err := db.User(ctx, model.UserID)
	if err != nil {
		return auth.User{}, auth.Session{}, err
	}
	return user, session, nil
}

func (db *DB) DeleteSession(ctx context.Context, token string) error {
	return db.orm.WithContext(ctx).Delete(&sessionModel{TokenHash: auth.HashSessionToken(token)}).Error
}

func (db *DB) SaveGrant(ctx context.Context, grant permission.Grant) error {
	model := grantToModel(grant)
	return db.orm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "subject_id"},
			{Name: "scope"},
			{Name: "resource"},
			{Name: "field"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"level"}),
	}).Create(&model).Error
}

func (db *DB) GrantsForSubject(ctx context.Context, subjectID string) (permission.Set, error) {
	grants, err := db.GrantListForSubject(ctx, subjectID)
	if err != nil {
		return permission.Set{}, err
	}
	return permission.New(grants...), nil
}

func (db *DB) EffectiveGrantsForSubject(ctx context.Context, subjectID string) (permission.Set, error) {
	grants, err := db.GrantListForSubject(ctx, subjectID)
	if err != nil {
		return permission.Set{}, err
	}
	memberships, err := db.RoleMemberships(ctx, subjectID)
	if err != nil {
		return permission.Set{}, err
	}
	for _, membership := range memberships {
		roleGrants, err := db.GrantListForSubject(ctx, RoleSubjectID(membership.DatabaseName, membership.RoleName))
		if err != nil {
			return permission.Set{}, err
		}
		for _, grant := range roleGrants {
			grant.SubjectID = subjectID
			grants = append(grants, grant)
		}
	}
	return permission.New(grants...), nil
}

func (db *DB) GrantListForSubject(ctx context.Context, subjectID string) ([]permission.Grant, error) {
	var models []permissionGrantModel
	err := db.orm.WithContext(ctx).
		Where(&permissionGrantModel{SubjectID: subjectID}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "resource"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "field"}}).
		Find(&models).Error
	if err != nil {
		return nil, err
	}

	grants := make([]permission.Grant, 0, len(models))
	for _, model := range models {
		grants = append(grants, modelToGrant(model))
	}
	return grants, nil
}

func (db *DB) SaveWorkflow(ctx context.Context, workflow WorkflowDefinition) (WorkflowDefinition, error) {
	if workflow.DatabaseName == "" {
		return WorkflowDefinition{}, errors.New("database_name is required")
	}
	if workflow.ID != 0 {
		existing, err := db.Workflow(ctx, workflow.ID)
		if err != nil {
			return WorkflowDefinition{}, err
		}
		workflow.CreatorID = existing.CreatorID
		workflow.Secrets = mergeStringMaps(existing.Secrets, workflow.Secrets)
	}
	model, err := workflowToModel(workflow)
	if err != nil {
		return WorkflowDefinition{}, err
	}
	if workflow.ID == 0 {
		if err := db.orm.WithContext(ctx).Create(&model).Error; err != nil {
			return WorkflowDefinition{}, err
		}
		return modelToWorkflow(model)
	}
	if err := db.orm.WithContext(ctx).Save(&model).Error; err != nil {
		return WorkflowDefinition{}, err
	}
	return modelToWorkflow(model)
}

func (db *DB) Workflow(ctx context.Context, id int64) (WorkflowDefinition, error) {
	var model workflowModel
	if err := db.orm.WithContext(ctx).First(&model, id).Error; err != nil {
		return WorkflowDefinition{}, err
	}
	return modelToWorkflow(model)
}

func (db *DB) Workflows(ctx context.Context, databaseName string) ([]WorkflowDefinition, error) {
	var models []workflowModel
	err := db.orm.WithContext(ctx).
		Where(&workflowModel{DatabaseName: databaseName}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Find(&models).
		Error
	if err != nil {
		return nil, err
	}
	workflows := make([]WorkflowDefinition, 0, len(models))
	for _, model := range models {
		workflow, err := modelToWorkflow(model)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, workflow)
	}
	return workflows, nil
}

func (db *DB) SaveForm(ctx context.Context, form FormDefinition) (FormDefinition, error) {
	if form.DatabaseName == "" {
		return FormDefinition{}, errors.New("database_name is required")
	}
	if form.ID != 0 && form.PublishedToken == "" {
		existing, err := db.Form(ctx, form.ID)
		if err != nil {
			return FormDefinition{}, err
		}
		form.PublishedToken = existing.PublishedToken
	}
	model := formToModel(form)
	if form.ID == 0 {
		if err := db.orm.WithContext(ctx).Create(&model).Error; err != nil {
			return FormDefinition{}, err
		}
		return modelToForm(model), nil
	}
	if err := db.orm.WithContext(ctx).Save(&model).Error; err != nil {
		return FormDefinition{}, err
	}
	return modelToForm(model), nil
}

func (db *DB) Form(ctx context.Context, id int64) (FormDefinition, error) {
	var model formModel
	if err := db.orm.WithContext(ctx).First(&model, id).Error; err != nil {
		return FormDefinition{}, err
	}
	return modelToForm(model), nil
}

func (db *DB) FormByPublishedToken(ctx context.Context, token string) (FormDefinition, error) {
	if token == "" {
		return FormDefinition{}, gorm.ErrRecordNotFound
	}
	var model formModel
	if err := db.orm.WithContext(ctx).Where(&formModel{PublishedToken: token}).First(&model).Error; err != nil {
		return FormDefinition{}, err
	}
	return modelToForm(model), nil
}

func (db *DB) PublishForm(ctx context.Context, id int64) (FormDefinition, error) {
	form, err := db.Form(ctx, id)
	if err != nil {
		return FormDefinition{}, err
	}
	if form.PublishedToken == "" {
		form.PublishedToken = uuid.NewString()
	}
	return db.SaveForm(ctx, form)
}

func (db *DB) Forms(ctx context.Context, databaseName string) ([]FormDefinition, error) {
	var models []formModel
	err := db.orm.WithContext(ctx).
		Where(&formModel{DatabaseName: databaseName}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Find(&models).
		Error
	if err != nil {
		return nil, err
	}
	forms := make([]FormDefinition, 0, len(models))
	for _, model := range models {
		forms = append(forms, modelToForm(model))
	}
	return forms, nil
}

func (db *DB) SaveRole(ctx context.Context, role RoleDefinition) (RoleDefinition, error) {
	if role.DatabaseName == "" {
		return RoleDefinition{}, errors.New("database_name is required")
	}
	if role.Name == "" {
		return RoleDefinition{}, errors.New("role name is required")
	}
	model := roleToModel(role)
	if role.ID == 0 {
		if err := db.orm.WithContext(ctx).Create(&model).Error; err != nil {
			return RoleDefinition{}, err
		}
		return db.roleDefinition(ctx, model)
	}
	if err := db.orm.WithContext(ctx).Save(&model).Error; err != nil {
		return RoleDefinition{}, err
	}
	return db.roleDefinition(ctx, model)
}

func (db *DB) Role(ctx context.Context, databaseName, roleName string) (RoleDefinition, error) {
	var model roleModel
	if err := db.orm.WithContext(ctx).
		Where(&roleModel{DatabaseName: databaseName, Name: roleName}).
		First(&model).Error; err != nil {
		return RoleDefinition{}, err
	}
	return db.roleDefinition(ctx, model)
}

func (db *DB) Roles(ctx context.Context, databaseName string) ([]RoleDefinition, error) {
	var models []roleModel
	err := db.orm.WithContext(ctx).
		Where(&roleModel{DatabaseName: databaseName}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Find(&models).
		Error
	if err != nil {
		return nil, err
	}
	roles := make([]RoleDefinition, 0, len(models))
	for _, model := range models {
		role, err := db.roleDefinition(ctx, model)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

func (db *DB) ReplaceRoleGrants(ctx context.Context, databaseName, roleName string, grants []permission.Grant) (RoleDefinition, error) {
	role, err := db.Role(ctx, databaseName, roleName)
	if err != nil {
		return RoleDefinition{}, err
	}
	subjectID := RoleSubjectID(databaseName, roleName)
	err = db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&permissionGrantModel{SubjectID: subjectID}).Delete(&permissionGrantModel{}).Error; err != nil {
			return err
		}
		for _, grant := range grants {
			if grant.Level == permission.None && grant.Scope != permission.ScopeField {
				continue
			}
			grant.SubjectID = subjectID
			model := grantToModel(grant)
			if err := tx.Create(&model).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return RoleDefinition{}, err
	}
	return db.Role(ctx, role.DatabaseName, role.Name)
}

type RoleMembership struct {
	DatabaseName string `json:"database_name"`
	RoleName     string `json:"role_name"`
	UserID       string `json:"user_id"`
}

func (db *DB) ReplaceRoleMembers(ctx context.Context, databaseName, roleName string, members []string) (RoleDefinition, error) {
	role, err := db.Role(ctx, databaseName, roleName)
	if err != nil {
		return RoleDefinition{}, err
	}
	err = db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&roleMemberModel{DatabaseName: databaseName, RoleName: roleName}).Delete(&roleMemberModel{}).Error; err != nil {
			return err
		}
		seen := map[string]struct{}{}
		for _, member := range members {
			if member == "" {
				continue
			}
			if _, ok := seen[member]; ok {
				continue
			}
			seen[member] = struct{}{}
			model := roleMemberModel{DatabaseName: databaseName, RoleName: roleName, UserID: member}
			if err := tx.Create(&model).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return RoleDefinition{}, err
	}
	return db.Role(ctx, role.DatabaseName, role.Name)
}

func (db *DB) RoleMemberships(ctx context.Context, userID string) ([]RoleMembership, error) {
	var models []roleMemberModel
	err := db.orm.WithContext(ctx).
		Where(&roleMemberModel{UserID: userID}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "database_name"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "role_name"}}).
		Find(&models).
		Error
	if err != nil {
		return nil, err
	}
	memberships := make([]RoleMembership, 0, len(models))
	for _, model := range models {
		memberships = append(memberships, RoleMembership{
			DatabaseName: model.DatabaseName,
			RoleName:     model.RoleName,
			UserID:       model.UserID,
		})
	}
	return memberships, nil
}

func RoleSubjectID(databaseName, roleName string) string {
	return "role:" + databaseName + ":" + roleName
}

func userToModel(user auth.User) userModel {
	return userModel{
		ID:           user.ID,
		Email:        user.Email,
		Provider:     string(user.Provider),
		ProviderName: user.ProviderName,
		Subject:      user.Subject,
		PasswordHash: user.PasswordHash,
	}
}

func modelToUser(model userModel) auth.User {
	return auth.User{
		ID:           model.ID,
		Email:        model.Email,
		Provider:     auth.Provider(model.Provider),
		ProviderName: model.ProviderName,
		Subject:      model.Subject,
		PasswordHash: model.PasswordHash,
	}
}

func grantToModel(grant permission.Grant) permissionGrantModel {
	return permissionGrantModel{
		SubjectID: grant.SubjectID,
		Scope:     grant.Scope,
		Resource:  grant.Resource,
		Field:     grant.Field,
		Level:     grant.Level,
	}
}

func modelToGrant(model permissionGrantModel) permission.Grant {
	return permission.Grant{
		SubjectID: model.SubjectID,
		Scope:     model.Scope,
		Resource:  model.Resource,
		Field:     model.Field,
		Level:     model.Level,
	}
}

func workflowToModel(workflow WorkflowDefinition) (workflowModel, error) {
	secrets, err := json.Marshal(emptyStringMap(workflow.Secrets))
	if err != nil {
		return workflowModel{}, err
	}
	variables, err := json.Marshal(emptyStringMap(workflow.Variables))
	if err != nil {
		return workflowModel{}, err
	}
	return workflowModel{
		ID:            workflow.ID,
		DatabaseName:  workflow.DatabaseName,
		Name:          workflow.Name,
		Script:        workflow.Script,
		CreatorID:     workflow.CreatorID,
		SecretsJSON:   string(secrets),
		VariablesJSON: string(variables),
		CreatedAt:     workflow.CreatedAt,
		UpdatedAt:     workflow.UpdatedAt,
	}, nil
}

func modelToWorkflow(model workflowModel) (WorkflowDefinition, error) {
	workflow := WorkflowDefinition{
		ID:           model.ID,
		DatabaseName: model.DatabaseName,
		Name:         model.Name,
		Script:       model.Script,
		CreatorID:    model.CreatorID,
		CreatedAt:    model.CreatedAt,
		UpdatedAt:    model.UpdatedAt,
	}
	if err := json.Unmarshal([]byte(model.SecretsJSON), &workflow.Secrets); err != nil {
		return WorkflowDefinition{}, err
	}
	if err := json.Unmarshal([]byte(model.VariablesJSON), &workflow.Variables); err != nil {
		return WorkflowDefinition{}, err
	}
	return workflow, nil
}

func formToModel(form FormDefinition) formModel {
	return formModel{
		ID:             form.ID,
		DatabaseName:   form.DatabaseName,
		Name:           form.Name,
		Script:         form.Script,
		PublishedToken: form.PublishedToken,
		CreatedAt:      form.CreatedAt,
		UpdatedAt:      form.UpdatedAt,
	}
}

func modelToForm(model formModel) FormDefinition {
	return FormDefinition{
		ID:             model.ID,
		DatabaseName:   model.DatabaseName,
		Name:           model.Name,
		Script:         model.Script,
		PublishedToken: model.PublishedToken,
		CreatedAt:      model.CreatedAt,
		UpdatedAt:      model.UpdatedAt,
	}
}

func roleToModel(role RoleDefinition) roleModel {
	return roleModel{
		ID:           role.ID,
		DatabaseName: role.DatabaseName,
		Name:         role.Name,
		CreatedAt:    role.CreatedAt,
		UpdatedAt:    role.UpdatedAt,
	}
}

func (db *DB) roleDefinition(ctx context.Context, model roleModel) (RoleDefinition, error) {
	subjectID := RoleSubjectID(model.DatabaseName, model.Name)
	grants, err := db.GrantListForSubject(ctx, subjectID)
	if err != nil {
		return RoleDefinition{}, err
	}
	members, err := db.roleMembers(ctx, model.DatabaseName, model.Name)
	if err != nil {
		return RoleDefinition{}, err
	}
	return RoleDefinition{
		ID:           model.ID,
		DatabaseName: model.DatabaseName,
		Name:         model.Name,
		SubjectID:    subjectID,
		Grants:       grants,
		Members:      members,
		CreatedAt:    model.CreatedAt,
		UpdatedAt:    model.UpdatedAt,
	}, nil
}

func (db *DB) roleMembers(ctx context.Context, databaseName, roleName string) ([]string, error) {
	var models []roleMemberModel
	err := db.orm.WithContext(ctx).
		Where(&roleMemberModel{DatabaseName: databaseName, RoleName: roleName}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "user_id"}}).
		Find(&models).
		Error
	if err != nil {
		return nil, err
	}
	members := make([]string, 0, len(models))
	for _, model := range models {
		members = append(members, model.UserID)
	}
	return members, nil
}

func emptyStringMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}

func mergeStringMaps(base map[string]string, updates map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range updates {
		merged[key] = value
	}
	return merged
}
