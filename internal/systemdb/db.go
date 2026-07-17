package systemdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"autable/internal/auth"
	"autable/internal/permission"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	orm *gorm.DB
}

type WorkflowDefinition struct {
	ID           int64             `json:"id"`
	DatabaseName string            `json:"database_name"`
	Name         string            `json:"name"`
	Script       string            `json:"script"`
	Enabled      bool              `json:"enabled"`
	CreatorID    string            `json:"creator_id,omitempty"`
	Secrets      map[string]string `json:"secrets"`
	Variables    map[string]string `json:"variables"`
	Runners      map[string]string `json:"runners"`
	// HistoryRetentionDays is nil to keep run history forever, 0 to keep
	// none, and a positive count to keep that many days.
	HistoryRetentionDays *int64           `json:"history_retention_days"`
	PermissionLevel      permission.Level `json:"permission_level,omitempty" gorm:"-"`
	CreatedAt            int64            `json:"created_at"`
	UpdatedAt            int64            `json:"updated_at"`
}

type FormDefinition struct {
	ID              int64            `json:"id"`
	DatabaseName    string           `json:"database_name"`
	Name            string           `json:"name"`
	Script          string           `json:"script"`
	PublishedToken  string           `json:"published_token,omitempty"`
	CreatorID       string           `json:"creator_id,omitempty"`
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
	Members      []RoleMember       `json:"members"`
	CreatedAt    int64              `json:"created_at"`
	UpdatedAt    int64              `json:"updated_at"`
}

type RoleMember struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type userModel struct {
	ID           string `gorm:"primaryKey"`
	Email        string `gorm:"uniqueIndex;not null"`
	DisplayName  string `gorm:"not null"`
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

type databaseOwnerModel struct {
	DatabaseName string `gorm:"primaryKey;not null"`
	OwnerID      string `gorm:"primaryKey;not null"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli"`
}

type workflowModel struct {
	ID                   int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName         string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Name                 string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Script               string `gorm:"not null"`
	Enabled              bool   `gorm:"not null;default:true"`
	CreatorID            string `gorm:"index;not null;default:''"`
	SecretsJSON          string `gorm:"not null"`
	VariablesJSON        string `gorm:"not null"`
	RunnersJSON          string `gorm:"not null;default:'{}'"`
	HistoryRetentionDays *int64
	CreatedAt            int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt            int64 `gorm:"autoUpdateTime:milli"`
}

type fileModel struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	Name        string `gorm:"not null"`
	Size        int64  `gorm:"not null"`
	ContentType string `gorm:"not null;default:''"`
	CreatorID   string `gorm:"index;not null;default:''"`
	CreatedAt   int64  `gorm:"autoCreateTime:milli"`
}

type runnerTokenModel struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName string `gorm:"uniqueIndex;not null"`
	TokenHash    string `gorm:"uniqueIndex;not null"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
}

type formModel struct {
	ID             int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName   string `gorm:"uniqueIndex:idx_form_database_name;not null"`
	Name           string `gorm:"uniqueIndex:idx_form_database_name;not null"`
	Script         string `gorm:"not null"`
	PublishedToken string `gorm:"index"`
	CreatorID      string `gorm:"index;not null;default:''"`
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
	SubjectType  string `gorm:"uniqueIndex:idx_role_member;not null;default:'user'"`
	SubjectID    string `gorm:"uniqueIndex:idx_role_member;not null"`
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
	if err := db.runSchemaMigrations(ctx); err != nil {
		return err
	}
	return db.orm.WithContext(ctx).AutoMigrate(
		&userModel{},
		&sessionModel{},
		&permissionGrantModel{},
		&databaseOwnerModel{},
		&workflowModel{},
		&formModel{},
		&roleModel{},
		&roleMemberModel{},
		&runnerTokenModel{},
		&fileModel{},
	)
}

func (db *DB) UpsertUserByEmail(ctx context.Context, user auth.User) (auth.User, error) {
	if user.Email == "" {
		return auth.User{}, errors.New("email is required")
	}
	displayName, err := auth.NormalizeDisplayName(user.DisplayName)
	if err != nil {
		return auth.User{}, err
	}
	user.DisplayName = displayName

	var existing userModel
	err = db.orm.WithContext(ctx).Where(&userModel{Email: user.Email}).First(&existing).Error
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
			"display_name",
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

func (db *DB) SearchUsers(ctx context.Context, query string, limit int) ([]auth.User, error) {
	if limit <= 0 {
		limit = 20
	}
	normalized := strings.ToLower(strings.TrimSpace(query))
	var models []userModel
	request := db.orm.WithContext(ctx).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "email"}}).
		Limit(limit)
	if normalized != "" {
		like := "%" + normalized + "%"
		request = request.Where("lower(email) LIKE ? OR lower(display_name) LIKE ?", like, like)
	}
	if err := request.Find(&models).Error; err != nil {
		return nil, err
	}
	users := make([]auth.User, 0, len(models))
	for _, model := range models {
		users = append(users, modelToUser(model))
	}
	return users, nil
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

func (db *DB) DeleteGrant(ctx context.Context, subjectID string, scope permission.Scope, resource string, fields ...string) error {
	query := db.orm.WithContext(ctx).
		Where(&permissionGrantModel{SubjectID: subjectID, Scope: scope, Resource: resource})
	if len(fields) > 0 {
		query = query.Where("field IN ?", fields)
	}
	return query.Delete(&permissionGrantModel{}).Error
}

func (db *DB) SaveDatabaseOwner(ctx context.Context, dbName, ownerID string) error {
	model := databaseOwnerModel{DatabaseName: dbName, OwnerID: ownerID}
	return db.orm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "database_name"}, {Name: "owner_id"}},
		DoNothing: true,
	}).Create(&model).Error
}

// OwnsAnyDatabase reports whether the user owns at least one database;
// database owners are the only role allowed to manage the runner token.
func (db *DB) OwnsAnyDatabase(ctx context.Context, userID string) (bool, error) {
	var count int64
	err := db.orm.WithContext(ctx).
		Model(&databaseOwnerModel{}).
		Where(&databaseOwnerModel{OwnerID: userID}).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (db *DB) IsDatabaseOwner(ctx context.Context, userID, dbName string) (bool, error) {
	var count int64
	err := db.orm.WithContext(ctx).
		Model(&databaseOwnerModel{}).
		Where(&databaseOwnerModel{DatabaseName: dbName, OwnerID: userID}).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
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
	if workflow.ID == 0 {
		workflow.Enabled = true
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

func (db *DB) DeleteWorkflow(ctx context.Context, id int64) error {
	return db.orm.WithContext(ctx).Delete(&workflowModel{}, id).Error
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
	if form.ID != 0 {
		existing, err := db.Form(ctx, form.ID)
		if err != nil {
			return FormDefinition{}, err
		}
		form.CreatorID = existing.CreatorID
		if form.PublishedToken == "" {
			form.PublishedToken = existing.PublishedToken
		}
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

func (db *DB) UnpublishForm(ctx context.Context, id int64) (FormDefinition, error) {
	var model formModel
	if err := db.orm.WithContext(ctx).First(&model, id).Error; err != nil {
		return FormDefinition{}, err
	}
	model.PublishedToken = ""
	if err := db.orm.WithContext(ctx).Save(&model).Error; err != nil {
		return FormDefinition{}, err
	}
	return modelToForm(model), nil
}

func (db *DB) DeleteForm(ctx context.Context, id int64) error {
	return db.orm.WithContext(ctx).Delete(&formModel{}, id).Error
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
			if grant.Level == permission.None {
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
	SubjectType  string `json:"subject_type"`
	SubjectID    string `json:"subject_id"`
}

func (db *DB) ReplaceRoleMembers(ctx context.Context, databaseName, roleName string, members []RoleMember) (RoleDefinition, error) {
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
			member.Type = strings.TrimSpace(member.Type)
			member.ID = strings.TrimSpace(member.ID)
			if member.Type == "" {
				member.Type = "user"
			}
			if member.ID == "" {
				continue
			}
			subjectID := roleMemberSubjectID(member)
			key := member.Type + ":" + subjectID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			model := roleMemberModel{DatabaseName: databaseName, RoleName: roleName, SubjectType: member.Type, SubjectID: subjectID}
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

func (db *DB) RoleMemberships(ctx context.Context, subjectID string) ([]RoleMembership, error) {
	var models []roleMemberModel
	err := db.orm.WithContext(ctx).
		Where(&roleMemberModel{SubjectID: subjectID}).
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
			SubjectType:  model.SubjectType,
			SubjectID:    model.SubjectID,
		})
	}
	return memberships, nil
}

func RoleSubjectID(databaseName, roleName string) string {
	return "role:" + databaseName + ":" + roleName
}

func WorkflowSubjectID(workflowID int64) string {
	return "workflow:" + strconv.FormatInt(workflowID, 10)
}

func roleMemberSubjectID(member RoleMember) string {
	if member.Type == "workflow" {
		workflowID, err := strconv.ParseInt(member.ID, 10, 64)
		if err == nil {
			return WorkflowSubjectID(workflowID)
		}
	}
	return member.ID
}

func roleMemberFromModel(model roleMemberModel) RoleMember {
	if model.SubjectType == "workflow" {
		return RoleMember{Type: model.SubjectType, ID: strings.TrimPrefix(model.SubjectID, "workflow:")}
	}
	return RoleMember{Type: model.SubjectType, ID: model.SubjectID}
}

func userToModel(user auth.User) userModel {
	return userModel{
		ID:           user.ID,
		Email:        user.Email,
		DisplayName:  user.DisplayName,
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
		DisplayName:  model.DisplayName,
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
	runners, err := json.Marshal(emptyStringMap(workflow.Runners))
	if err != nil {
		return workflowModel{}, err
	}
	return workflowModel{
		ID:                   workflow.ID,
		DatabaseName:         workflow.DatabaseName,
		Name:                 workflow.Name,
		Script:               workflow.Script,
		Enabled:              workflow.Enabled,
		CreatorID:            workflow.CreatorID,
		SecretsJSON:          string(secrets),
		VariablesJSON:        string(variables),
		RunnersJSON:          string(runners),
		HistoryRetentionDays: workflow.HistoryRetentionDays,
		CreatedAt:            workflow.CreatedAt,
		UpdatedAt:            workflow.UpdatedAt,
	}, nil
}

func modelToWorkflow(model workflowModel) (WorkflowDefinition, error) {
	workflow := WorkflowDefinition{
		ID:                   model.ID,
		DatabaseName:         model.DatabaseName,
		Name:                 model.Name,
		Script:               model.Script,
		Enabled:              model.Enabled,
		CreatorID:            model.CreatorID,
		HistoryRetentionDays: model.HistoryRetentionDays,
		CreatedAt:            model.CreatedAt,
		UpdatedAt:            model.UpdatedAt,
	}
	if err := json.Unmarshal([]byte(model.SecretsJSON), &workflow.Secrets); err != nil {
		return WorkflowDefinition{}, err
	}
	if err := json.Unmarshal([]byte(model.VariablesJSON), &workflow.Variables); err != nil {
		return WorkflowDefinition{}, err
	}
	if err := json.Unmarshal([]byte(model.RunnersJSON), &workflow.Runners); err != nil {
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
		CreatorID:      form.CreatorID,
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
		CreatorID:      model.CreatorID,
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

func (db *DB) roleMembers(ctx context.Context, databaseName, roleName string) ([]RoleMember, error) {
	var models []roleMemberModel
	err := db.orm.WithContext(ctx).
		Where(&roleMemberModel{DatabaseName: databaseName, RoleName: roleName}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "subject_type"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "subject_id"}}).
		Find(&models).
		Error
	if err != nil {
		return nil, err
	}
	members := make([]RoleMember, 0, len(models))
	for _, model := range models {
		members = append(members, roleMemberFromModel(model))
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

type FileRecord struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	CreatorID   string `json:"creator_id,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

func (db *DB) CreateFile(ctx context.Context, file FileRecord) (FileRecord, error) {
	if strings.TrimSpace(file.Name) == "" {
		return FileRecord{}, errors.New("file name is required")
	}
	if file.Size < 0 {
		return FileRecord{}, errors.New("file size must not be negative")
	}
	model := fileModel{
		Name:        file.Name,
		Size:        file.Size,
		ContentType: file.ContentType,
		CreatorID:   file.CreatorID,
	}
	if err := db.orm.WithContext(ctx).Create(&model).Error; err != nil {
		return FileRecord{}, err
	}
	return fileRecordFromModel(model), nil
}

var ErrFileNotFound = errors.New("file not found")

func (db *DB) File(ctx context.Context, id int64) (FileRecord, error) {
	var model fileModel
	err := db.orm.WithContext(ctx).First(&model, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return FileRecord{}, fmt.Errorf("%w: %d", ErrFileNotFound, id)
	}
	if err != nil {
		return FileRecord{}, err
	}
	return fileRecordFromModel(model), nil
}

func fileRecordFromModel(model fileModel) FileRecord {
	return FileRecord{
		ID:          model.ID,
		Name:        model.Name,
		Size:        model.Size,
		ContentType: model.ContentType,
		CreatorID:   model.CreatorID,
		CreatedAt:   model.CreatedAt,
	}
}
