package systemdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"autable/internal/permission"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Workflows: see docs/design/workflows-v2.md. A workflow is a versioned
// code bundle like an app; PublishedVersion selects the version new runs
// use (0 while none is published).

const (
	DefaultWorkflowTimeoutSeconds   = 600
	DefaultWorkflowRetentionDays    = 30
	WorkflowRunQueued               = "queued"
	WorkflowRunRunning              = "running"
	WorkflowRunSucceeded            = "succeeded"
	WorkflowRunFailed               = "failed"
	WorkflowRunCancelled            = "cancelled"
	maxWorkflowRunListLimit         = 500
	defaultWorkflowRunListLimit     = 50
	workflowRunDeleteBatchSize      = 500
	workflowLogInsertBatchSize      = 200
	workflowSettingsTimeoutMaximum  = 24 * 60 * 60
	workflowSettingsRetentionMaxDay = 3650
)

type Workflow struct {
	ID                   int64            `json:"id"`
	DatabaseName         string           `json:"database_name"`
	Name                 string           `json:"name"`
	Title                string           `json:"title"`
	PublishedVersion     int64            `json:"published_version"`
	LatestVersion        int64            `json:"latest_version"`
	Enabled              bool             `json:"enabled"`
	TimeoutSeconds       int64            `json:"timeout_seconds"`
	HistoryRetentionDays int64            `json:"history_retention_days"`
	CreatorID            string           `json:"creator_id,omitempty"`
	PermissionLevel      permission.Level `json:"permission_level,omitempty"`
	CreatedAt            int64            `json:"created_at"`
	UpdatedAt            int64            `json:"updated_at"`
}

// WorkflowVersion is one immutable upload, without its bundle. Triggers is
// the JSON trigger list read when the version was last published, empty
// before that; Libraries lists the libraries ("@lib/<name>") its code
// imports, recorded at the same time.
type WorkflowVersion struct {
	WorkflowID int64    `json:"workflow_id"`
	Version    int64    `json:"version"`
	SDK        string   `json:"sdk"`
	Message    string   `json:"message"`
	Size       int64    `json:"size"`
	Triggers   string   `json:"-"`
	Libraries  []string `json:"libraries"`
	CreatedBy  string   `json:"created_by"`
	CreatedAt  int64    `json:"created_at"`
}

// WorkflowRun is one execution of a workflow. Event and Output are JSON
// (empty when absent); so is Libraries, the libraries the run loaded with
// their versions ([{"name","version"}]).
type WorkflowRun struct {
	ID         int64  `json:"id"`
	WorkflowID int64  `json:"workflow_id"`
	Version    int64  `json:"version"`
	Trigger    string `json:"trigger"`
	Status     string `json:"status"`
	Event      string `json:"-"`
	Output     string `json:"-"`
	Error      string `json:"error,omitempty"`
	StartedBy  string `json:"started_by,omitempty"`
	Libraries  string `json:"-"`
	CreatedAt  int64  `json:"created_at"`
	StartedAt  int64  `json:"started_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}

// WorkflowLog is one log entry of a run. Data is JSON, empty when absent.
type WorkflowLog struct {
	RunID   int64  `json:"-"`
	Time    int64  `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Data    string `json:"-"`
}

// WorkflowSettings changes the given settings; nil fields stay.
type WorkflowSettings struct {
	Enabled              *bool  `json:"enabled,omitempty"`
	TimeoutSeconds       *int64 `json:"timeout_seconds,omitempty"`
	HistoryRetentionDays *int64 `json:"history_retention_days,omitempty"`
}

// WorkflowRunFilter selects runs of one workflow, newest first.
type WorkflowRunFilter struct {
	Status string
	Limit  int
	// Before returns only runs with a smaller id (pagination).
	Before int64
}

type workflowModel struct {
	ID                   int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName         string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Name                 string `gorm:"uniqueIndex:idx_workflow_database_name;not null"`
	Title                string `gorm:"not null;default:''"`
	PublishedVersion     int64  `gorm:"not null;default:0"`
	LatestVersion        int64  `gorm:"not null;default:0"`
	Enabled              bool   `gorm:"not null;default:true"`
	TimeoutSeconds       int64  `gorm:"not null;default:600"`
	HistoryRetentionDays int64  `gorm:"not null;default:30"`
	CreatorID            string `gorm:"index;not null;default:''"`
	CreatedAt            int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt            int64  `gorm:"autoUpdateTime:milli"`
}

type workflowVersionModel struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	WorkflowID   int64  `gorm:"uniqueIndex:idx_workflow_version;not null"`
	Version      int64  `gorm:"uniqueIndex:idx_workflow_version;not null"`
	SDK          string `gorm:"not null;default:''"`
	Message      string `gorm:"not null;default:''"`
	Size         int64  `gorm:"not null;default:0"`
	Bundle       []byte `gorm:"not null"`
	TriggersJSON string `gorm:"not null;default:''"`
	// LibrariesJSON is added to existing tables by AutoMigrate (it has a
	// default); empty for versions published before libraries existed.
	LibrariesJSON string `gorm:"not null;default:''"`
	CreatedBy     string `gorm:"not null;default:''"`
	CreatedAt     int64  `gorm:"autoCreateTime:milli"`
}

type workflowSecretModel struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	WorkflowID int64  `gorm:"uniqueIndex:idx_workflow_secret;not null"`
	Name       string `gorm:"uniqueIndex:idx_workflow_secret;not null"`
	Value      string `gorm:"not null;default:''"`
	UpdatedAt  int64  `gorm:"autoUpdateTime:milli"`
}

type workflowVarModel struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	WorkflowID int64  `gorm:"uniqueIndex:idx_workflow_var;not null"`
	Name       string `gorm:"uniqueIndex:idx_workflow_var;not null"`
	Value      string `gorm:"not null;default:''"`
	UpdatedAt  int64  `gorm:"autoUpdateTime:milli"`
}

type workflowRunModel struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	WorkflowID int64  `gorm:"index:idx_workflow_run_workflow;not null"`
	Version    int64  `gorm:"not null;default:0"`
	Trigger    string `gorm:"not null;default:''"`
	Status     string `gorm:"index;not null;default:'queued'"`
	EventJSON  string `gorm:"not null;default:''"`
	OutputJSON string `gorm:"not null;default:''"`
	Error      string `gorm:"not null;default:''"`
	StartedBy  string `gorm:"index;not null;default:''"`
	// LibrariesJSON is added to existing tables by AutoMigrate (it has a
	// default).
	LibrariesJSON string `gorm:"not null;default:''"`
	CreatedAt     int64  `gorm:"autoCreateTime:milli"`
	StartedAt     int64  `gorm:"not null;default:0"`
	FinishedAt    int64  `gorm:"index;not null;default:0"`
}

type workflowLogModel struct {
	ID       int64  `gorm:"primaryKey;autoIncrement"`
	RunID    int64  `gorm:"index;not null"`
	Time     int64  `gorm:"not null;default:0"`
	Level    string `gorm:"not null;default:'info'"`
	Message  string `gorm:"not null;default:''"`
	DataJSON string `gorm:"not null;default:''"`
}

// ErrWorkflowVersionNotFound reports a version number the workflow does not have.
var ErrWorkflowVersionNotFound = errors.New("workflow version not found")

// ErrWorkflowRunNotFound reports a run id the workflow does not have.
var ErrWorkflowRunNotFound = errors.New("workflow run not found")

func WorkflowSubjectID(workflowID int64) string {
	return "workflow:" + strconv.FormatInt(workflowID, 10)
}

// ValidateWorkflowSettings checks the ranges settings may take.
func ValidateWorkflowSettings(settings WorkflowSettings) error {
	if settings.TimeoutSeconds != nil && (*settings.TimeoutSeconds < 1 || *settings.TimeoutSeconds > workflowSettingsTimeoutMaximum) {
		return fmt.Errorf("timeout_seconds must be between 1 and %d, got %d", workflowSettingsTimeoutMaximum, *settings.TimeoutSeconds)
	}
	if settings.HistoryRetentionDays != nil && (*settings.HistoryRetentionDays < 1 || *settings.HistoryRetentionDays > workflowSettingsRetentionMaxDay) {
		return fmt.Errorf("history_retention_days must be between 1 and %d, got %d", workflowSettingsRetentionMaxDay, *settings.HistoryRetentionDays)
	}
	return nil
}

func (db *DB) Workflow(ctx context.Context, id int64) (Workflow, error) {
	var model workflowModel
	if err := db.orm.WithContext(ctx).First(&model, id).Error; err != nil {
		return Workflow{}, err
	}
	return modelToWorkflow(model), nil
}

func (db *DB) WorkflowByName(ctx context.Context, databaseName, name string) (Workflow, error) {
	var model workflowModel
	if err := db.orm.WithContext(ctx).Where(&workflowModel{DatabaseName: databaseName, Name: name}).First(&model).Error; err != nil {
		return Workflow{}, err
	}
	return modelToWorkflow(model), nil
}

func (db *DB) Workflows(ctx context.Context, databaseName string) ([]Workflow, error) {
	var models []workflowModel
	if err := db.orm.WithContext(ctx).Where(&workflowModel{DatabaseName: databaseName}).Order("name").Find(&models).Error; err != nil {
		return nil, err
	}
	workflows := make([]Workflow, 0, len(models))
	for _, model := range models {
		workflows = append(workflows, modelToWorkflow(model))
	}
	return workflows, nil
}

// CreateWorkflow inserts a new, enabled workflow without versions.
func (db *DB) CreateWorkflow(ctx context.Context, workflow Workflow) (Workflow, error) {
	if workflow.DatabaseName == "" || workflow.Name == "" {
		return Workflow{}, errors.New("database_name and name are required")
	}
	model := workflowModel{
		DatabaseName:         workflow.DatabaseName,
		Name:                 workflow.Name,
		Title:                workflow.Title,
		Enabled:              true,
		TimeoutSeconds:       DefaultWorkflowTimeoutSeconds,
		HistoryRetentionDays: DefaultWorkflowRetentionDays,
		CreatorID:            workflow.CreatorID,
	}
	if err := db.orm.WithContext(ctx).Create(&model).Error; err != nil {
		return Workflow{}, err
	}
	return modelToWorkflow(model), nil
}

// AddWorkflowVersion stores a bundle as the workflow's next version and
// updates the title. Publishing is a separate step (PublishWorkflowVersion),
// because it needs the bundle's triggers.
func (db *DB) AddWorkflowVersion(ctx context.Context, workflowID int64, title string, version WorkflowVersion, bundle []byte) (Workflow, WorkflowVersion, error) {
	var saved workflowVersionModel
	var workflow workflowModel
	err := db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&workflow, workflowID).Error; err != nil {
			return err
		}
		saved = workflowVersionModel{
			WorkflowID: workflowID,
			Version:    workflow.LatestVersion + 1,
			SDK:        version.SDK,
			Message:    version.Message,
			Size:       int64(len(bundle)),
			Bundle:     bundle,
			CreatedBy:  version.CreatedBy,
		}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		workflow.LatestVersion = saved.Version
		if title != "" {
			workflow.Title = title
		}
		return tx.Save(&workflow).Error
	})
	if err != nil {
		return Workflow{}, WorkflowVersion{}, err
	}
	return modelToWorkflow(workflow), modelToWorkflowVersion(saved), nil
}

func (db *DB) WorkflowVersions(ctx context.Context, workflowID int64) ([]WorkflowVersion, error) {
	var models []workflowVersionModel
	if err := db.orm.WithContext(ctx).
		Omit("Bundle").
		Where(&workflowVersionModel{WorkflowID: workflowID}).
		Order("version DESC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	versions := make([]WorkflowVersion, 0, len(models))
	for _, model := range models {
		versions = append(versions, modelToWorkflowVersion(model))
	}
	return versions, nil
}

func (db *DB) WorkflowVersion(ctx context.Context, workflowID, version int64) (WorkflowVersion, error) {
	var model workflowVersionModel
	err := db.orm.WithContext(ctx).Omit("Bundle").Where(&workflowVersionModel{WorkflowID: workflowID, Version: version}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return WorkflowVersion{}, ErrWorkflowVersionNotFound
	}
	if err != nil {
		return WorkflowVersion{}, err
	}
	return modelToWorkflowVersion(model), nil
}

func (db *DB) WorkflowVersionBundle(ctx context.Context, workflowID, version int64) ([]byte, error) {
	var model workflowVersionModel
	err := db.orm.WithContext(ctx).Select("bundle").Where(&workflowVersionModel{WorkflowID: workflowID, Version: version}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWorkflowVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	return model.Bundle, nil
}

// PublishWorkflowVersion points the workflow at an existing version and
// stores the triggers read from it and the libraries it imports.
func (db *DB) PublishWorkflowVersion(ctx context.Context, workflowID, version int64, triggersJSON string, libraries []string) (Workflow, error) {
	if triggersJSON == "" {
		return Workflow{}, errors.New("published versions need their triggers")
	}
	librariesJSON, err := encodeImports(libraries)
	if err != nil {
		return Workflow{}, err
	}
	var workflow workflowModel
	err = db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&workflowVersionModel{}).
			Where(&workflowVersionModel{WorkflowID: workflowID, Version: version}).
			Updates(map[string]any{"triggers_json": triggersJSON, "libraries_json": librariesJSON})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: %d", ErrWorkflowVersionNotFound, version)
		}
		if err := tx.First(&workflow, workflowID).Error; err != nil {
			return err
		}
		workflow.PublishedVersion = version
		return tx.Save(&workflow).Error
	})
	if err != nil {
		return Workflow{}, err
	}
	return modelToWorkflow(workflow), nil
}

// UpdateWorkflowSettings applies the non-nil settings.
func (db *DB) UpdateWorkflowSettings(ctx context.Context, workflowID int64, settings WorkflowSettings) (Workflow, error) {
	if err := ValidateWorkflowSettings(settings); err != nil {
		return Workflow{}, err
	}
	var workflow workflowModel
	err := db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&workflow, workflowID).Error; err != nil {
			return err
		}
		if settings.Enabled != nil {
			workflow.Enabled = *settings.Enabled
		}
		if settings.TimeoutSeconds != nil {
			workflow.TimeoutSeconds = *settings.TimeoutSeconds
		}
		if settings.HistoryRetentionDays != nil {
			workflow.HistoryRetentionDays = *settings.HistoryRetentionDays
		}
		return tx.Save(&workflow).Error
	})
	if err != nil {
		return Workflow{}, err
	}
	return modelToWorkflow(workflow), nil
}

// DeleteWorkflow removes the workflow with its versions, secrets, vars,
// runs and logs, the grants on it, and the grants and role memberships of
// its subject.
func (db *DB) DeleteWorkflow(ctx context.Context, id int64) error {
	subjectID := WorkflowSubjectID(id)
	return db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, model := range []any{&workflowVersionModel{}, &workflowSecretModel{}, &workflowVarModel{}} {
			if err := tx.Where("workflow_id = ?", id).Delete(model).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("run_id IN (?)", tx.Model(&workflowRunModel{}).Select("id").Where("workflow_id = ?", id)).Delete(&workflowLogModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("workflow_id = ?", id).Delete(&workflowRunModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where(&permissionGrantModel{Scope: permission.ScopeWorkflow, Resource: strconv.FormatInt(id, 10)}).Delete(&permissionGrantModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where(&permissionGrantModel{SubjectID: subjectID}).Delete(&permissionGrantModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where(&roleMemberModel{SubjectID: subjectID}).Delete(&roleMemberModel{}).Error; err != nil {
			return err
		}
		return tx.Delete(&workflowModel{}, id).Error
	})
}

// Secrets and vars.

func (db *DB) SetWorkflowSecret(ctx context.Context, workflowID int64, name, value string) error {
	return db.orm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workflow_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&workflowSecretModel{WorkflowID: workflowID, Name: name, Value: value}).Error
}

// DeleteWorkflowSecret reports whether the secret existed.
func (db *DB) DeleteWorkflowSecret(ctx context.Context, workflowID int64, name string) (bool, error) {
	result := db.orm.WithContext(ctx).Where(&workflowSecretModel{WorkflowID: workflowID, Name: name}).Delete(&workflowSecretModel{})
	return result.RowsAffected > 0, result.Error
}

func (db *DB) WorkflowSecrets(ctx context.Context, workflowID int64) (map[string]string, error) {
	var models []workflowSecretModel
	if err := db.orm.WithContext(ctx).Where(&workflowSecretModel{WorkflowID: workflowID}).Find(&models).Error; err != nil {
		return nil, err
	}
	values := make(map[string]string, len(models))
	for _, model := range models {
		values[model.Name] = model.Value
	}
	return values, nil
}

func (db *DB) SetWorkflowVar(ctx context.Context, workflowID int64, name, value string) error {
	return db.orm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workflow_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&workflowVarModel{WorkflowID: workflowID, Name: name, Value: value}).Error
}

// DeleteWorkflowVar reports whether the var existed.
func (db *DB) DeleteWorkflowVar(ctx context.Context, workflowID int64, name string) (bool, error) {
	result := db.orm.WithContext(ctx).Where(&workflowVarModel{WorkflowID: workflowID, Name: name}).Delete(&workflowVarModel{})
	return result.RowsAffected > 0, result.Error
}

func (db *DB) WorkflowVars(ctx context.Context, workflowID int64) (map[string]string, error) {
	var models []workflowVarModel
	if err := db.orm.WithContext(ctx).Where(&workflowVarModel{WorkflowID: workflowID}).Find(&models).Error; err != nil {
		return nil, err
	}
	values := make(map[string]string, len(models))
	for _, model := range models {
		values[model.Name] = model.Value
	}
	return values, nil
}

// Runs.

// CreateWorkflowRun stores a new run in the queued state.
func (db *DB) CreateWorkflowRun(ctx context.Context, run WorkflowRun) (WorkflowRun, error) {
	model := workflowRunModel{
		WorkflowID: run.WorkflowID,
		Version:    run.Version,
		Trigger:    run.Trigger,
		Status:     WorkflowRunQueued,
		EventJSON:  run.Event,
		StartedBy:  run.StartedBy,
	}
	if err := db.orm.WithContext(ctx).Create(&model).Error; err != nil {
		return WorkflowRun{}, err
	}
	return modelToWorkflowRun(model), nil
}

// StartWorkflowRun moves a queued run to running; it reports false when
// the run is no longer queued (for example cancelled meanwhile).
func (db *DB) StartWorkflowRun(ctx context.Context, runID, startedAt int64) (bool, error) {
	result := db.orm.WithContext(ctx).Model(&workflowRunModel{}).
		Where("id = ? AND status = ?", runID, WorkflowRunQueued).
		Updates(map[string]any{"status": WorkflowRunRunning, "started_at": startedAt})
	return result.RowsAffected == 1, result.Error
}

// FinishWorkflowRun records the outcome of a queued or running run and the
// libraries it loaded (JSON, may be empty); it reports false when the run
// had already finished.
func (db *DB) FinishWorkflowRun(ctx context.Context, runID int64, status, outputJSON, errorText, librariesJSON string, finishedAt int64) (bool, error) {
	switch status {
	case WorkflowRunSucceeded, WorkflowRunFailed, WorkflowRunCancelled:
	default:
		return false, fmt.Errorf("%q is not a final run status", status)
	}
	result := db.orm.WithContext(ctx).Model(&workflowRunModel{}).
		Where("id = ? AND status IN ?", runID, []string{WorkflowRunQueued, WorkflowRunRunning}).
		Updates(map[string]any{"status": status, "output_json": outputJSON, "error": errorText, "libraries_json": librariesJSON, "finished_at": finishedAt})
	return result.RowsAffected == 1, result.Error
}

// FailInterruptedWorkflowRuns marks every queued or running run failed,
// except the runs in keep; called at startup, when the only live runs are
// the ones this process queued before starting.
func (db *DB) FailInterruptedWorkflowRuns(ctx context.Context, message string, now int64, keep ...int64) (int64, error) {
	query := db.orm.WithContext(ctx).Model(&workflowRunModel{}).
		Where("status IN ?", []string{WorkflowRunQueued, WorkflowRunRunning})
	if len(keep) > 0 {
		query = query.Where("id NOT IN ?", keep)
	}
	result := query.Updates(map[string]any{"status": WorkflowRunFailed, "error": message, "finished_at": now})
	return result.RowsAffected, result.Error
}

func (db *DB) WorkflowRun(ctx context.Context, workflowID, runID int64) (WorkflowRun, error) {
	var model workflowRunModel
	err := db.orm.WithContext(ctx).Where("id = ? AND workflow_id = ?", runID, workflowID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return WorkflowRun{}, ErrWorkflowRunNotFound
	}
	if err != nil {
		return WorkflowRun{}, err
	}
	return modelToWorkflowRun(model), nil
}

// WorkflowRuns lists runs newest first, without their events.
func (db *DB) WorkflowRuns(ctx context.Context, workflowID int64, filter WorkflowRunFilter) ([]WorkflowRun, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultWorkflowRunListLimit
	}
	limit = min(limit, maxWorkflowRunListLimit)
	query := db.orm.WithContext(ctx).Omit("EventJSON").Where("workflow_id = ?", workflowID)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Before > 0 {
		query = query.Where("id < ?", filter.Before)
	}
	var models []workflowRunModel
	if err := query.Order("id DESC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	runs := make([]WorkflowRun, 0, len(models))
	for _, model := range models {
		runs = append(runs, modelToWorkflowRun(model))
	}
	return runs, nil
}

// LatestWorkflowRun returns the newest run; ok is false when there is none.
func (db *DB) LatestWorkflowRun(ctx context.Context, workflowID int64) (WorkflowRun, bool, error) {
	runs, err := db.WorkflowRuns(ctx, workflowID, WorkflowRunFilter{Limit: 1})
	if err != nil || len(runs) == 0 {
		return WorkflowRun{}, false, err
	}
	return runs[0], true, nil
}

func (db *DB) AddWorkflowLogs(ctx context.Context, logs []WorkflowLog) error {
	if len(logs) == 0 {
		return nil
	}
	models := make([]workflowLogModel, 0, len(logs))
	for _, entry := range logs {
		models = append(models, workflowLogModel{RunID: entry.RunID, Time: entry.Time, Level: entry.Level, Message: entry.Message, DataJSON: entry.Data})
	}
	return db.orm.WithContext(ctx).CreateInBatches(&models, workflowLogInsertBatchSize).Error
}

func (db *DB) WorkflowLogs(ctx context.Context, runID int64) ([]WorkflowLog, error) {
	var models []workflowLogModel
	if err := db.orm.WithContext(ctx).Where("run_id = ?", runID).Order("id").Find(&models).Error; err != nil {
		return nil, err
	}
	logs := make([]WorkflowLog, 0, len(models))
	for _, model := range models {
		logs = append(logs, WorkflowLog{RunID: model.RunID, Time: model.Time, Level: model.Level, Message: model.Message, Data: model.DataJSON})
	}
	return logs, nil
}

// DeleteFinishedWorkflowRunsBefore deletes the workflow's finished runs
// (and their logs) that finished before cutoff. Queued and running runs
// are never deleted.
func (db *DB) DeleteFinishedWorkflowRunsBefore(ctx context.Context, workflowID, cutoff int64) (int, error) {
	deleted := 0
	for {
		var ids []int64
		if err := db.orm.WithContext(ctx).Model(&workflowRunModel{}).
			Where("workflow_id = ? AND status IN ? AND finished_at < ?", workflowID, []string{WorkflowRunSucceeded, WorkflowRunFailed, WorkflowRunCancelled}, cutoff).
			Limit(workflowRunDeleteBatchSize).
			Pluck("id", &ids).Error; err != nil {
			return deleted, err
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		err := db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("run_id IN ?", ids).Delete(&workflowLogModel{}).Error; err != nil {
				return err
			}
			return tx.Where("id IN ?", ids).Delete(&workflowRunModel{}).Error
		})
		if err != nil {
			return deleted, err
		}
		deleted += len(ids)
	}
}

func modelToWorkflow(model workflowModel) Workflow {
	return Workflow{
		ID:                   model.ID,
		DatabaseName:         model.DatabaseName,
		Name:                 model.Name,
		Title:                model.Title,
		PublishedVersion:     model.PublishedVersion,
		LatestVersion:        model.LatestVersion,
		Enabled:              model.Enabled,
		TimeoutSeconds:       model.TimeoutSeconds,
		HistoryRetentionDays: model.HistoryRetentionDays,
		CreatorID:            model.CreatorID,
		CreatedAt:            model.CreatedAt,
		UpdatedAt:            model.UpdatedAt,
	}
}

func modelToWorkflowVersion(model workflowVersionModel) WorkflowVersion {
	return WorkflowVersion{
		WorkflowID: model.WorkflowID,
		Version:    model.Version,
		SDK:        model.SDK,
		Message:    model.Message,
		Size:       model.Size,
		Triggers:   model.TriggersJSON,
		Libraries:  decodeImports(model.LibrariesJSON),
		CreatedBy:  model.CreatedBy,
		CreatedAt:  model.CreatedAt,
	}
}

func modelToWorkflowRun(model workflowRunModel) WorkflowRun {
	return WorkflowRun{
		ID:         model.ID,
		WorkflowID: model.WorkflowID,
		Version:    model.Version,
		Trigger:    model.Trigger,
		Status:     model.Status,
		Event:      model.EventJSON,
		Output:     model.OutputJSON,
		Error:      model.Error,
		StartedBy:  model.StartedBy,
		Libraries:  model.LibrariesJSON,
		CreatedAt:  model.CreatedAt,
		StartedAt:  model.StartedAt,
		FinishedAt: model.FinishedAt,
	}
}

// RawJSON returns stored JSON as a json.RawMessage, nil when empty.
func RawJSON(stored string) json.RawMessage {
	if stored == "" {
		return nil
	}
	return json.RawMessage(stored)
}
