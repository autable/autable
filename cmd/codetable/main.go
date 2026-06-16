package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"

	"codetable/internal/api"
	"codetable/internal/config"
	"codetable/internal/history"
	"codetable/internal/metadata"
	"codetable/internal/systemdb"
	"codetable/internal/table"
)

func main() {
	configPath := flag.String("config", "config.yml", "path to codetable config.yml")
	metadataPath := flag.String("metadata", "metadata/main.yml", "path to table metadata yaml")
	flag.Parse()

	if err := run(context.Background(), *configPath, *metadataPath); err != nil {
		slog.Error("codetable stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath, metadataPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	catalog, err := metadata.Load(metadataPath)
	if err != nil {
		return err
	}
	system, err := systemdb.Open(ctx, cfg.SystemDB.Path)
	if err != nil {
		return err
	}
	defer system.Close()

	historyStore, err := history.OpenLevelDB(cfg.History.Path)
	if err != nil {
		return err
	}
	defer historyStore.Close()

	address := cfg.Server.Address
	if address == "" {
		address = "127.0.0.1:8080"
	}
	server := api.NewServer(catalog, system, table.NewService(historyStore), historyStore)
	slog.Info("codetable listening", "address", address)
	return http.ListenAndServe(address, server)
}
