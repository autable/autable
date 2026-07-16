package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

	"autable/internal/api"
	"autable/internal/backup"
	"autable/internal/codefiles"
	"autable/internal/config"
	"autable/internal/history"
	"autable/internal/metadata"
	"autable/internal/recorddb"
	"autable/internal/repository"
	"autable/internal/repositorysync"
	"autable/internal/systemdb"
	"autable/internal/table"
	"autable/internal/version"
	"autable/internal/webui"
)

func main() {
	configPath := flag.String("config", "config.yml", "path to autable config.yml")
	showVersion := flag.Bool("version", false, "print autable version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.String())
		return
	}

	if err := run(context.Background(), *configPath); err != nil {
		slog.Error("autable stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if cfg.Repository.IsEnabled() {
		if err := repository.EnsureGitRepository(ctx, repository.GitOptions{
			Path:         cfg.Repository.Path,
			RemoteURL:    cfg.Repository.RemoteURL,
			RemoteBranch: cfg.Repository.RemoteBranch,
		}); err != nil {
			return err
		}
	}
	repoLayout := repository.NewLayout(cfg.Repository.Path)
	metadataPath := repoLayout.MetadataPath()
	catalog, err := metadata.LoadOrCreate(metadataPath)
	if err != nil {
		return err
	}
	system, err := systemdb.Open(ctx, cfg.SystemDBPath())
	if err != nil {
		return err
	}
	defer system.Close()

	historyStore, err := history.OpenLevelDB(cfg.HistoryPath())
	if err != nil {
		return err
	}
	defer historyStore.Close()

	rowRepository, err := recorddb.OpenCatalog(ctx, catalog, cfg.Data.Path)
	if err != nil {
		return err
	}
	defer rowRepository.Close()

	address := cfg.Server.Address
	if address == "" {
		address = "127.0.0.1:8080"
	}
	startPprofServer(ctx, cfg.Debug.PprofAddress)
	var repoSync *repositorysync.Service
	if cfg.Repository.IsEnabled() {
		repoSync = repositorysync.New(repositorysync.Options{
			Root:        cfg.Repository.Path,
			RemoteURL:   cfg.Repository.RemoteURL,
			Branch:      cfg.Repository.RemoteBranch,
			Debounce:    configDuration(cfg.Repository.Sync.Debounce, 2*time.Second),
			PushTimeout: configDuration(cfg.Repository.Sync.PushTimeout, 30*time.Second),
			AuthorName:  cfg.Repository.Sync.AuthorName,
			AuthorEmail: cfg.Repository.Sync.AuthorEmail,
		})
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := repoSync.Shutdown(shutdownCtx); err != nil {
				slog.Warn("repository sync shutdown failed", "error", err)
			}
		}()
		repoSync.Notify(repositorysync.Change{
			ActorID: "system",
			Action:  "repository.start",
			Summary: "started repository sync",
			Paths:   []string{metadataPath},
		})
	} else {
		slog.Info("repository git management is disabled")
	}
	if cfg.Backup.Enabled {
		s3Uploader, err := backup.NewS3Uploader(ctx, backup.S3Options{
			Endpoint:        cfg.Backup.S3.Endpoint,
			Region:          cfg.Backup.S3.Region,
			Bucket:          cfg.Backup.S3.Bucket,
			AccessKeyID:     cfg.Backup.S3.AccessKeyID,
			SecretAccessKey: cfg.Backup.S3.SecretAccessKey,
			ForcePathStyle:  cfg.Backup.S3.ForcePathStyle,
		})
		if err != nil {
			return err
		}
		backupService := backup.NewService(backup.Options{
			DataPath:       cfg.Data.Path,
			RepositoryPath: cfg.Repository.Path,
			Catalog:        catalog,
			IncludeLevelDB: cfg.Backup.IncludeLevelDB,
			TmpDir:         cfg.Backup.TmpDir,
			ObjectPrefix:   cfg.Backup.S3.Prefix,
			Uploader:       s3Uploader,
		}, configDuration(cfg.Backup.Interval, 24*time.Hour), historyStore)
		backupService.Start(ctx)
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := backupService.Stop(shutdownCtx); err != nil {
				slog.Warn("backup service shutdown failed", "error", err)
			}
		}()
	}
	server := api.NewServerWithAuthConfig(
		catalog,
		system,
		table.NewServiceWithRepository(historyStore, rowRepository),
		historyStore,
		cfg.Auth,
	)
	server.SetPublicURL(cfg.Server.PublicURL)
	server.EnableMetadataWrites(metadataPath)
	server.SetRepositoryPath(cfg.Repository.Path)
	server.SetDatabaseOpener(func(ctx context.Context, name string) error {
		return rowRepository.OpenDatabase(ctx, name, cfg.DatabasePath(name))
	})
	server.SetCodeFileStore(codefiles.NewStore(cfg.Repository.Path))
	if repoSync != nil {
		server.SetRepositorySync(repoSync)
	}
	if cfg.AI.Enabled {
		aiWorkerURL := cfg.AI.WorkerURL
		if aiWorkerURL == "" {
			aiWorkerURL = os.Getenv("AUTABLE_AI_WORKER_URL")
		}
		if aiWorkerURL != "" {
			server.SetAIClient(api.NewAIHTTPClient(aiWorkerURL))
		} else {
			slog.Warn("AI is enabled but no worker URL is configured")
		}
	}
	server.StartWorkflowWorkers(ctx)
	server.StartWorkflowScheduler(ctx, 15*time.Second)
	slog.Info("autable listening", "address", address)
	return http.ListenAndServe(address, webui.Handler(server))
}

func startPprofServer(ctx context.Context, address string) {
	if address == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	server := &http.Server{Addr: address, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Warn("pprof server shutdown failed", "error", err)
		}
	}()
	go func() {
		slog.Info("pprof listening", "address", address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("pprof server stopped", "error", err)
		}
	}()
}

func configDuration(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		slog.Warn("invalid duration config, using default", "value", value, "default", fallback)
		return fallback
	}
	return duration
}
