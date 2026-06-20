package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"autable/internal/api"
	"autable/internal/codefiles"
	"autable/internal/config"
	"autable/internal/history"
	"autable/internal/metadata"
	"autable/internal/recorddb"
	"autable/internal/repository"
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
	server := api.NewServerWithOIDCProviders(
		catalog,
		system,
		table.NewServiceWithRepository(historyStore, rowRepository),
		historyStore,
		cfg.OIDC.Providers,
	)
	server.EnableMetadataWrites(metadataPath)
	server.SetDatabaseOpener(func(ctx context.Context, name string) error {
		return rowRepository.OpenDatabase(ctx, name, cfg.DatabasePath(name))
	})
	server.SetCodeFileStore(codefiles.NewStore(cfg.Repository.Path))
	server.StartWorkflowWorkers(ctx)
	server.StartWorkflowScheduler(ctx, 15*time.Second)
	slog.Info("autable listening", "address", address)
	return http.ListenAndServe(address, webui.Handler(server))
}
