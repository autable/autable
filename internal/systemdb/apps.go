package systemdb

import (
	"context"
	"errors"
	"fmt"

	"autable/internal/permission"

	"gorm.io/gorm"
)

// AppDefinition is a hosted web app. Its builds live in AppVersion rows;
// PublishedVersion selects the one users get (0 while none is published).
type AppDefinition struct {
	ID               int64            `json:"id"`
	DatabaseName     string           `json:"database_name"`
	Name             string           `json:"name"`
	Title            string           `json:"title"`
	PublishedVersion int64            `json:"published_version"`
	LatestVersion    int64            `json:"latest_version"`
	CreatorID        string           `json:"creator_id,omitempty"`
	PermissionLevel  permission.Level `json:"permission_level,omitempty"`
	CreatedAt        int64            `json:"created_at"`
	UpdatedAt        int64            `json:"updated_at"`
}

// AppVersion describes one immutable upload of an app, without its bundle.
type AppVersion struct {
	AppID     int64  `json:"app_id"`
	Version   int64  `json:"version"`
	SDK       string `json:"sdk"`
	Message   string `json:"message"`
	Size      int64  `json:"size"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
}

type appModel struct {
	ID               int64  `gorm:"primaryKey;autoIncrement"`
	DatabaseName     string `gorm:"uniqueIndex:idx_app_database_name;not null"`
	Name             string `gorm:"uniqueIndex:idx_app_database_name;not null"`
	Title            string `gorm:"not null;default:''"`
	PublishedVersion int64  `gorm:"not null;default:0"`
	LatestVersion    int64  `gorm:"not null;default:0"`
	CreatorID        string `gorm:"index;not null;default:''"`
	CreatedAt        int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt        int64  `gorm:"autoUpdateTime:milli"`
}

type appVersionModel struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	AppID     int64  `gorm:"uniqueIndex:idx_app_version;not null"`
	Version   int64  `gorm:"uniqueIndex:idx_app_version;not null"`
	SDK       string `gorm:"not null;default:''"`
	Message   string `gorm:"not null;default:''"`
	Size      int64  `gorm:"not null;default:0"`
	Bundle    []byte `gorm:"not null"`
	CreatedBy string `gorm:"not null;default:''"`
	CreatedAt int64  `gorm:"autoCreateTime:milli"`
}

// ErrAppVersionNotFound reports a version number the app does not have.
var ErrAppVersionNotFound = errors.New("app version not found")

func (db *DB) App(ctx context.Context, id int64) (AppDefinition, error) {
	var model appModel
	if err := db.orm.WithContext(ctx).First(&model, id).Error; err != nil {
		return AppDefinition{}, err
	}
	return modelToApp(model), nil
}

func (db *DB) AppByName(ctx context.Context, databaseName, name string) (AppDefinition, error) {
	var model appModel
	if err := db.orm.WithContext(ctx).Where(&appModel{DatabaseName: databaseName, Name: name}).First(&model).Error; err != nil {
		return AppDefinition{}, err
	}
	return modelToApp(model), nil
}

func (db *DB) Apps(ctx context.Context, databaseName string) ([]AppDefinition, error) {
	var models []appModel
	if err := db.orm.WithContext(ctx).Where(&appModel{DatabaseName: databaseName}).Order("name").Find(&models).Error; err != nil {
		return nil, err
	}
	apps := make([]AppDefinition, 0, len(models))
	for _, model := range models {
		apps = append(apps, modelToApp(model))
	}
	return apps, nil
}

// CreateApp inserts a new app without versions.
func (db *DB) CreateApp(ctx context.Context, app AppDefinition) (AppDefinition, error) {
	if app.DatabaseName == "" || app.Name == "" {
		return AppDefinition{}, errors.New("database_name and name are required")
	}
	model := appModel{DatabaseName: app.DatabaseName, Name: app.Name, Title: app.Title, CreatorID: app.CreatorID}
	if err := db.orm.WithContext(ctx).Create(&model).Error; err != nil {
		return AppDefinition{}, err
	}
	return modelToApp(model), nil
}

// AddAppVersion stores a bundle as the app's next version and updates the
// app title. With publish set, the new version is published in the same
// transaction.
func (db *DB) AddAppVersion(ctx context.Context, appID int64, title string, version AppVersion, bundle []byte, publish bool) (AppDefinition, AppVersion, error) {
	var saved appVersionModel
	var app appModel
	err := db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&app, appID).Error; err != nil {
			return err
		}
		saved = appVersionModel{
			AppID:     appID,
			Version:   app.LatestVersion + 1,
			SDK:       version.SDK,
			Message:   version.Message,
			Size:      int64(len(bundle)),
			Bundle:    bundle,
			CreatedBy: version.CreatedBy,
		}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		app.LatestVersion = saved.Version
		if title != "" {
			app.Title = title
		}
		if publish {
			app.PublishedVersion = saved.Version
		}
		return tx.Save(&app).Error
	})
	if err != nil {
		return AppDefinition{}, AppVersion{}, err
	}
	return modelToApp(app), modelToAppVersion(saved), nil
}

func (db *DB) AppVersions(ctx context.Context, appID int64) ([]AppVersion, error) {
	var models []appVersionModel
	if err := db.orm.WithContext(ctx).
		Omit("Bundle").
		Where(&appVersionModel{AppID: appID}).
		Order("version DESC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	versions := make([]AppVersion, 0, len(models))
	for _, model := range models {
		versions = append(versions, modelToAppVersion(model))
	}
	return versions, nil
}

func (db *DB) AppVersion(ctx context.Context, appID, version int64) (AppVersion, error) {
	var model appVersionModel
	err := db.orm.WithContext(ctx).Omit("Bundle").Where(&appVersionModel{AppID: appID, Version: version}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AppVersion{}, ErrAppVersionNotFound
	}
	if err != nil {
		return AppVersion{}, err
	}
	return modelToAppVersion(model), nil
}

func (db *DB) AppVersionBundle(ctx context.Context, appID, version int64) ([]byte, error) {
	var model appVersionModel
	err := db.orm.WithContext(ctx).Select("bundle").Where(&appVersionModel{AppID: appID, Version: version}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAppVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	return model.Bundle, nil
}

// PublishAppVersion points the app at an existing version.
func (db *DB) PublishAppVersion(ctx context.Context, appID, version int64) (AppDefinition, error) {
	var app appModel
	err := db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&appVersionModel{}).Where(&appVersionModel{AppID: appID, Version: version}).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w: %d", ErrAppVersionNotFound, version)
		}
		if err := tx.First(&app, appID).Error; err != nil {
			return err
		}
		app.PublishedVersion = version
		return tx.Save(&app).Error
	})
	if err != nil {
		return AppDefinition{}, err
	}
	return modelToApp(app), nil
}

// DeleteApp removes the app, every version, and every grant on it.
func (db *DB) DeleteApp(ctx context.Context, id int64) error {
	return db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&appVersionModel{AppID: id}).Delete(&appVersionModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where(&permissionGrantModel{Scope: permission.ScopeApp, Resource: fmt.Sprint(id)}).Delete(&permissionGrantModel{}).Error; err != nil {
			return err
		}
		return tx.Delete(&appModel{}, id).Error
	})
}

func modelToApp(model appModel) AppDefinition {
	return AppDefinition{
		ID:               model.ID,
		DatabaseName:     model.DatabaseName,
		Name:             model.Name,
		Title:            model.Title,
		PublishedVersion: model.PublishedVersion,
		LatestVersion:    model.LatestVersion,
		CreatorID:        model.CreatorID,
		CreatedAt:        model.CreatedAt,
		UpdatedAt:        model.UpdatedAt,
	}
}

func modelToAppVersion(model appVersionModel) AppVersion {
	return AppVersion{
		AppID:     model.AppID,
		Version:   model.Version,
		SDK:       model.SDK,
		Message:   model.Message,
		Size:      model.Size,
		CreatedBy: model.CreatedBy,
		CreatedAt: model.CreatedAt,
	}
}
