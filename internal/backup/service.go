package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"autable/internal/history"
)

type Service struct {
	options  Options
	interval time.Duration
	history  *history.LevelDBStore
	running  atomic.Bool
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
}

func NewService(options Options, interval time.Duration, historyStore *history.LevelDBStore) *Service {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		options:  options,
		interval: interval,
		history:  historyStore,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (service *Service) Start(ctx context.Context) {
	go service.loop(ctx)
}

func (service *Service) Stop(ctx context.Context) error {
	service.once.Do(func() {
		close(service.stop)
	})
	select {
	case <-service.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (service *Service) RunOnce(ctx context.Context) (Result, error) {
	if !service.running.CompareAndSwap(false, true) {
		return Result{}, errors.New("backup already running")
	}
	defer service.running.Store(false)
	return Run(ctx, service.options, service.history)
}

func (service *Service) loop(ctx context.Context) {
	defer close(service.done)
	timer := time.NewTimer(service.interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-service.stop:
			return
		case <-timer.C:
			result, err := service.RunOnce(ctx)
			if err != nil {
				slog.Error("backup failed", "error", err)
			} else {
				slog.Info("backup uploaded", "key", result.Key, "size_bytes", result.SizeBytes, "duration", result.FinishedAt.Sub(result.StartedAt))
			}
			timer.Reset(service.interval)
		}
	}
}

func Run(ctx context.Context, options Options, historyStore *history.LevelDBStore) (Result, error) {
	if options.Uploader == nil {
		return Result{}, errors.New("backup uploader is required")
	}
	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now().UTC()
	}
	startedAt := now
	name := "autable-backup-" + now.Format("20060102T150405Z")
	tmpRoot := options.TmpDir
	if tmpRoot == "" {
		tmpRoot = filepath.Join(os.TempDir(), "autable-backups")
	}
	workDir := filepath.Join(tmpRoot, name)
	if err := os.RemoveAll(workDir); err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return Result{}, err
	}

	manifest := Manifest{
		Version:        1,
		CreatedAt:      now.Format(time.RFC3339),
		IncludeLevelDB: options.IncludeLevelDB,
		DataPath:       options.DataPath,
		RepositoryPath: options.RepositoryPath,
	}
	if err := backupSQLiteFiles(ctx, options, workDir, &manifest); err != nil {
		return Result{}, err
	}
	if options.IncludeLevelDB {
		if historyStore == nil {
			return Result{}, errors.New("leveldb backup requested but history store is nil")
		}
		if err := exportLevelDB(ctx, historyStore, workDir, &manifest); err != nil {
			return Result{}, err
		}
	}
	if err := writeManifest(filepath.Join(workDir, "manifest.json"), manifest); err != nil {
		return Result{}, err
	}

	archivePath := filepath.Join(tmpRoot, name+".tar.gz")
	if err := createTarGzip(ctx, workDir, archivePath); err != nil {
		return Result{}, err
	}
	defer os.Remove(archivePath)
	info, err := os.Stat(archivePath)
	if err != nil {
		return Result{}, err
	}
	key := backupObjectKey(options, name+".tar.gz")
	if err := options.Uploader.Upload(ctx, archivePath, key); err != nil {
		return Result{}, err
	}
	return Result{
		Key:        key,
		SizeBytes:  info.Size(),
		StartedAt:  startedAt,
		FinishedAt: time.Now().UTC(),
	}, nil
}

func backupSQLiteFiles(ctx context.Context, options Options, workDir string, manifest *Manifest) error {
	sqliteDir := filepath.Join(workDir, "sqlite")
	systemSource := filepath.Join(options.DataPath, "system.sqlite")
	if err := backupSQLiteIfExists(ctx, systemSource, filepath.Join(sqliteDir, "system.sqlite"), "sqlite/system.sqlite", "system_sqlite", manifest); err != nil {
		return err
	}
	for _, database := range options.Catalog.Databases {
		source := filepath.Join(options.DataPath, database.Name+".sqlite")
		archiveRelativePath := filepath.ToSlash(filepath.Join("sqlite", database.Name+".sqlite"))
		destination := filepath.Join(sqliteDir, database.Name+".sqlite")
		if err := backupSQLiteIfExists(ctx, source, destination, archiveRelativePath, "database_sqlite", manifest); err != nil {
			return err
		}
	}
	return nil
}

func backupSQLiteIfExists(ctx context.Context, source string, destination string, archiveRelativePath string, kind string, manifest *Manifest) error {
	if _, err := os.Stat(source); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := backupSQLiteFile(ctx, source, destination); err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		return err
	}
	manifest.Files = append(manifest.Files, ManifestFile{Path: archiveRelativePath, Kind: kind, SizeBytes: info.Size()})
	return nil
}

func exportLevelDB(ctx context.Context, historyStore *history.LevelDBStore, workDir string, manifest *Manifest) error {
	relativePath := "leveldb"
	destination := filepath.Join(workDir, relativePath)
	if err := exportLevelDBSnapshot(ctx, historyStore, destination); err != nil {
		return err
	}
	sizeBytes, err := directorySize(destination)
	if err != nil {
		return err
	}
	manifest.Files = append(manifest.Files, ManifestFile{Path: relativePath, Kind: "leveldb_directory", SizeBytes: sizeBytes})
	return nil
}

func directorySize(path string) (int64, error) {
	var sizeBytes int64
	err := filepath.WalkDir(path, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		sizeBytes += info.Size()
		return nil
	})
	return sizeBytes, err
}

func backupObjectKey(options Options, fileName string) string {
	prefix := cleanS3KeyPrefix(options.ObjectPrefix)
	if prefix == "" {
		return fileName
	}
	return strings.TrimSuffix(prefix, "/") + "/" + fileName
}
