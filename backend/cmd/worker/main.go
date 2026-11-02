package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer"
	"github.com/repoatlas/repoatlas/backend/internal/config"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/httpapi"
	"github.com/repoatlas/repoatlas/backend/internal/jobs"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

func main() {
	cfg := config.Load()
	logger := httpapi.NewStructuredLogger(os.Stdout)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect postgres failed", "error", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Warn("initial postgres ping failed", "error", err.Error())
	}

	st := store.NewPostgresStore(pool)
	// Single-migration execution policy: by default only API runs AUTO_MIGRATE unless Worker explicitly sets AUTO_MIGRATE=true.
	if strings.EqualFold(os.Getenv("AUTO_MIGRATE"), "true") {
		for _, migDir := range []string{"migrations", "/app/migrations"} {
			if info, statErr := os.Stat(migDir); statErr == nil && info.IsDir() {
				if migErr := st.ApplyMigrations(ctx, migDir); migErr != nil {
					logger.Warn("auto migration warning", "dir", migDir, "error", migErr.Error())
				}
				break
			}
		}
	}
	var gh githubclient.Client = githubclient.NewRESTClient(githubclient.RESTClientOptions{
		Token:   cfg.GitHubToken,
		Timeout: 60 * time.Second,
	})
	if !cfg.IsProduction() && cfg.MockGitHubFixtureDir != "" {
		gh = githubclient.NewFixtureFallbackClient(gh, cfg.MockGitHubFixtureDir)
	}
	az := analyzer.NewPythonAnalyzer()

	proc := jobs.NewProcessor(st, gh, az, jobs.ProcessorOptions{
		AnalysisTimeout: cfg.AnalysisTimeout,
	})
	worker := jobs.NewWorker(st, proc, cfg.WorkerConcurrency, cfg.WorkerPollInterval)

	logger.Info("RepoAtlas Worker started", "concurrency", cfg.WorkerConcurrency, "poll_interval", cfg.WorkerPollInterval.String())
	if err := worker.Run(ctx); err != nil {
		logger.Error("worker error", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("RepoAtlas Worker stopped cleanly.")
}
