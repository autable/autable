package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"autable/internal/runnercli"
	"autable/internal/version"
	"autable/internal/workflow/nodes"
)

func main() {
	endpoint := flag.String("endpoint", "", "autable server URL, e.g. wss://autable.example.com")
	token := flag.String("token", os.Getenv("AUTABLE_RUNNER_TOKEN"), "runner token (defaults to AUTABLE_RUNNER_TOKEN)")
	name := flag.String("name", defaultName(), "runner name workflow instances bind to (defaults to hostname)")
	maxJobs := flag.Int("max-jobs", 4, "maximum concurrent jobs")
	showVersion := flag.Bool("version", false, "print autable-runner version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.String())
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := runnercli.Run(ctx, runnercli.Options{
		Endpoint: *endpoint,
		Token:    *token,
		Name:     *name,
		MaxJobs:  *maxJobs,
		Logger:   slog.Default(),
	}, nodes.Remote())
	if err != nil {
		slog.Error("autable-runner stopped", "error", err)
		os.Exit(1)
	}
}

func defaultName() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "runner"
	}
	return hostname
}
