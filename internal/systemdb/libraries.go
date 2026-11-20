package systemdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"gorm.io/gorm"
)

// Libraries: instance-wide TypeScript modules that workflows (and other
// libraries) import as "@lib/<name>" and load at run time, always in the
// library's published version. See docs/design/workflows-v2.md. A library
// is versioned like a workflow; PublishedVersion selects the version every
// importing run gets (0 while none is published).

// Library is a shared code library.
type Library struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	PublishedVersion int64  `json:"published_version"`
	LatestVersion    int64  `json:"latest_version"`
	CreatorID        string `json:"creator_id,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

// LibraryVersion is one immutable upload of a library, without its bundle.
// Imports lists the libraries the version's code requires, recorded when it
// is published.
type LibraryVersion struct {
	LibraryID int64    `json:"library_id"`
	Version   int64    `json:"version"`
	SDK       string   `json:"sdk"`
	Message   string   `json:"message"`
	Size      int64    `json:"size"`
	Imports   []string `json:"imports"`
	CreatedBy string   `json:"created_by"`
	CreatedAt int64    `json:"created_at"`
}

// LibraryImporter is a published workflow or library version that imports
// a library.
type LibraryImporter struct {
	// Kind is "workflow" or "library".
	Kind         string `json:"kind"`
	DatabaseName string `json:"database_name,omitempty"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	Version      int64  `json:"version"`
}

type libraryModel struct {
	ID               int64  `gorm:"primaryKey;autoIncrement"`
	Name             string `gorm:"uniqueIndex;not null"`
	Title            string `gorm:"not null;default:''"`
	PublishedVersion int64  `gorm:"not null;default:0"`
	LatestVersion    int64  `gorm:"not null;default:0"`
	CreatorID        string `gorm:"index;not null;default:''"`
	CreatedAt        int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt        int64  `gorm:"autoUpdateTime:milli"`
}

type libraryVersionModel struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	LibraryID   int64  `gorm:"uniqueIndex:idx_library_version;not null"`
	Version     int64  `gorm:"uniqueIndex:idx_library_version;not null"`
	SDK         string `gorm:"not null;default:''"`
	Message     string `gorm:"not null;default:''"`
	Size        int64  `gorm:"not null;default:0"`
	Bundle      []byte `gorm:"not null"`
	ImportsJSON string `gorm:"not null;default:''"`
	CreatedBy   string `gorm:"not null;default:''"`
	CreatedAt   int64  `gorm:"autoCreateTime:milli"`
}

// ErrLibraryVersionNotFound reports a version number the library does not have.
var ErrLibraryVersionNotFound = errors.New("library version not found")

func (db *DB) Library(ctx context.Context, id int64) (Library, error) {
	var model libraryModel
	if err := db.orm.WithContext(ctx).First(&model, id).Error; err != nil {
		return Library{}, err
	}
	return modelToLibrary(model), nil
}

// LibraryByName returns gorm.ErrRecordNotFound for an unknown name.
func (db *DB) LibraryByName(ctx context.Context, name string) (Library, error) {
	var model libraryModel
	if err := db.orm.WithContext(ctx).Where(&libraryModel{Name: name}).First(&model).Error; err != nil {
		return Library{}, err
	}
	return modelToLibrary(model), nil
}

func (db *DB) Libraries(ctx context.Context) ([]Library, error) {
	var models []libraryModel
	if err := db.orm.WithContext(ctx).Order("name").Find(&models).Error; err != nil {
		return nil, err
	}
	libraries := make([]Library, 0, len(models))
	for _, model := range models {
		libraries = append(libraries, modelToLibrary(model))
	}
	return libraries, nil
}

// CreateLibrary inserts a new library without versions.
func (db *DB) CreateLibrary(ctx context.Context, library Library) (Library, error) {
	if library.Name == "" {
		return Library{}, errors.New("name is required")
	}
	model := libraryModel{Name: library.Name, Title: library.Title, CreatorID: library.CreatorID}
	if err := db.orm.WithContext(ctx).Create(&model).Error; err != nil {
		return Library{}, err
	}
	return modelToLibrary(model), nil
}

// AddLibraryVersion stores a bundle as the library's next version and
// updates its title. Publishing is a separate step (PublishLibraryVersion).
func (db *DB) AddLibraryVersion(ctx context.Context, libraryID int64, title string, version LibraryVersion, bundle []byte) (Library, LibraryVersion, error) {
	var saved libraryVersionModel
	var library libraryModel
	err := db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&library, libraryID).Error; err != nil {
			return err
		}
		saved = libraryVersionModel{
			LibraryID: libraryID,
			Version:   library.LatestVersion + 1,
			SDK:       version.SDK,
			Message:   version.Message,
			Size:      int64(len(bundle)),
			Bundle:    bundle,
			CreatedBy: version.CreatedBy,
		}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		library.LatestVersion = saved.Version
		if title != "" {
			library.Title = title
		}
		return tx.Save(&library).Error
	})
	if err != nil {
		return Library{}, LibraryVersion{}, err
	}
	return modelToLibrary(library), modelToLibraryVersion(saved), nil
}

func (db *DB) LibraryVersions(ctx context.Context, libraryID int64) ([]LibraryVersion, error) {
	var models []libraryVersionModel
	if err := db.orm.WithContext(ctx).Omit("Bundle").Where(&libraryVersionModel{LibraryID: libraryID}).Order("version DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	versions := make([]LibraryVersion, 0, len(models))
	for _, model := range models {
		versions = append(versions, modelToLibraryVersion(model))
	}
	return versions, nil
}

func (db *DB) LibraryVersion(ctx context.Context, libraryID, version int64) (LibraryVersion, error) {
	var model libraryVersionModel
	err := db.orm.WithContext(ctx).Omit("Bundle").Where(&libraryVersionModel{LibraryID: libraryID, Version: version}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return LibraryVersion{}, ErrLibraryVersionNotFound
	}
	if err != nil {
		return LibraryVersion{}, err
	}
	return modelToLibraryVersion(model), nil
}

func (db *DB) LibraryVersionBundle(ctx context.Context, libraryID, version int64) ([]byte, error) {
	var model libraryVersionModel
	err := db.orm.WithContext(ctx).Select("bundle").Where(&libraryVersionModel{LibraryID: libraryID, Version: version}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrLibraryVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	return model.Bundle, nil
}

// PublishLibraryVersion points the library at an existing version and
// records the libraries that version imports.
func (db *DB) PublishLibraryVersion(ctx context.Context, libraryID, version int64, imports []string) (Library, error) {
	encoded, err := encodeImports(imports)
	if err != nil {
		return Library{}, err
	}
	var library libraryModel
	err = db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&libraryVersionModel{}).
			Where(&libraryVersionModel{LibraryID: libraryID, Version: version}).
			Update("imports_json", encoded)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: %d", ErrLibraryVersionNotFound, version)
		}
		if err := tx.First(&library, libraryID).Error; err != nil {
			return err
		}
		library.PublishedVersion = version
		return tx.Save(&library).Error
	})
	if err != nil {
		return Library{}, err
	}
	return modelToLibrary(library), nil
}

// DeleteLibrary removes the library and every version.
func (db *DB) DeleteLibrary(ctx context.Context, id int64) error {
	return db.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&libraryVersionModel{LibraryID: id}).Delete(&libraryVersionModel{}).Error; err != nil {
			return err
		}
		return tx.Delete(&libraryModel{}, id).Error
	})
}

// LibraryImporters lists the published workflow versions and published
// library versions whose recorded imports include name, sorted by kind,
// database and name.
func (db *DB) LibraryImporters(ctx context.Context, name string) ([]LibraryImporter, error) {
	pattern := "%" + mustJSONString(name) + "%"
	importers := []LibraryImporter{}

	type workflowRow struct {
		DatabaseName     string
		Name             string
		Title            string
		PublishedVersion int64
		LibrariesJSON    string
	}
	var workflows []workflowRow
	if err := db.orm.WithContext(ctx).Table("workflow_models").
		Select("workflow_models.database_name, workflow_models.name, workflow_models.title, workflow_models.published_version, workflow_version_models.libraries_json").
		Joins("JOIN workflow_version_models ON workflow_version_models.workflow_id = workflow_models.id AND workflow_version_models.version = workflow_models.published_version").
		Where("workflow_models.published_version > 0 AND workflow_version_models.libraries_json LIKE ?", pattern).
		Order("workflow_models.database_name, workflow_models.name").
		Scan(&workflows).Error; err != nil {
		return nil, err
	}
	for _, row := range workflows {
		if slices.Contains(decodeImports(row.LibrariesJSON), name) {
			importers = append(importers, LibraryImporter{Kind: "workflow", DatabaseName: row.DatabaseName, Name: row.Name, Title: row.Title, Version: row.PublishedVersion})
		}
	}

	type libraryRow struct {
		Name             string
		Title            string
		PublishedVersion int64
		ImportsJSON      string
	}
	var libraries []libraryRow
	if err := db.orm.WithContext(ctx).Table("library_models").
		Select("library_models.name, library_models.title, library_models.published_version, library_version_models.imports_json").
		Joins("JOIN library_version_models ON library_version_models.library_id = library_models.id AND library_version_models.version = library_models.published_version").
		Where("library_models.published_version > 0 AND library_version_models.imports_json LIKE ?", pattern).
		Order("library_models.name").
		Scan(&libraries).Error; err != nil {
		return nil, err
	}
	for _, row := range libraries {
		if row.Name != name && slices.Contains(decodeImports(row.ImportsJSON), name) {
			importers = append(importers, LibraryImporter{Kind: "library", Name: row.Name, Title: row.Title, Version: row.PublishedVersion})
		}
	}
	return importers, nil
}

// PublishedLibraryImports returns the imports recorded for the published
// version of every library, keyed by library name, for cycle checks.
func (db *DB) PublishedLibraryImports(ctx context.Context) (map[string][]string, error) {
	type row struct {
		Name        string
		ImportsJSON string
	}
	var rows []row
	if err := db.orm.WithContext(ctx).Table("library_models").
		Select("library_models.name, library_version_models.imports_json").
		Joins("JOIN library_version_models ON library_version_models.library_id = library_models.id AND library_version_models.version = library_models.published_version").
		Where("library_models.published_version > 0").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	imports := make(map[string][]string, len(rows))
	for _, entry := range rows {
		imports[entry.Name] = decodeImports(entry.ImportsJSON)
	}
	return imports, nil
}

func encodeImports(imports []string) (string, error) {
	if len(imports) == 0 {
		return "", nil
	}
	sorted := slices.Clone(imports)
	slices.Sort(sorted)
	encoded, err := json.Marshal(slices.Compact(sorted))
	return string(encoded), err
}

func decodeImports(stored string) []string {
	if stored == "" {
		return []string{}
	}
	var imports []string
	if err := json.Unmarshal([]byte(stored), &imports); err != nil || imports == nil {
		return []string{}
	}
	return imports
}

func mustJSONString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func modelToLibrary(model libraryModel) Library {
	return Library{
		ID:               model.ID,
		Name:             model.Name,
		Title:            model.Title,
		PublishedVersion: model.PublishedVersion,
		LatestVersion:    model.LatestVersion,
		CreatorID:        model.CreatorID,
		CreatedAt:        model.CreatedAt,
		UpdatedAt:        model.UpdatedAt,
	}
}

func modelToLibraryVersion(model libraryVersionModel) LibraryVersion {
	return LibraryVersion{
		LibraryID: model.LibraryID,
		Version:   model.Version,
		SDK:       model.SDK,
		Message:   model.Message,
		Size:      model.Size,
		Imports:   decodeImports(model.ImportsJSON),
		CreatedBy: model.CreatedBy,
		CreatedAt: model.CreatedAt,
	}
}
