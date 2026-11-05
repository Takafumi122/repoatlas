package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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

	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		addr := cfg.APIAddr
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		c := &http.Client{Timeout: 2 * time.Second}
		resp, err := c.Get("http://" + addr + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	logger := httpapi.NewStructuredLogger(os.Stdout)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var st store.Store
	if strings.EqualFold(cfg.DatabaseURL, "memory") {
		logger.Info("using in-memory store (DATABASE_URL=memory)")
		mem := store.NewMemoryStore()
		mem.SetMaxQueuedJobs(cfg.MaxQueuedJobs)
		st = mem
	} else {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("connect postgres failed", "error", err.Error())
			os.Exit(1)
		}
		defer pool.Close()

		if err := pool.Ping(ctx); err != nil {
			logger.Warn("initial postgres ping failed", "error", err.Error())
		}

		pgStore := store.NewPostgresStore(pool)
		pgStore.SetMaxQueuedJobs(cfg.MaxQueuedJobs)

		if !strings.EqualFold(os.Getenv("AUTO_MIGRATE"), "false") {
			for _, migDir := range []string{"migrations", "/app/migrations"} {
				if info, statErr := os.Stat(migDir); statErr == nil && info.IsDir() {
					if migErr := pgStore.ApplyMigrations(ctx, migDir); migErr != nil {
						logger.Warn("auto migration warning", "dir", migDir, "error", migErr.Error())
					} else {
						logger.Info("database migrations verified", "dir", migDir)
					}
					break
				}
			}
		}

		st = pgStore
	}

	var gh githubclient.Client = githubclient.NewRESTClient(githubclient.RESTClientOptions{
		Token:   cfg.GitHubToken,
		Timeout: 30 * time.Second,
	})
	if !cfg.IsProduction() && cfg.MockGitHubFixtureDir != "" {
		gh = githubclient.NewFixtureFallbackClient(gh, cfg.MockGitHubFixtureDir)
	}

	if strings.EqualFold(os.Getenv("RUN_EMBEDDED_WORKER"), "true") || strings.EqualFold(cfg.DatabaseURL, "memory") {
		az := analyzer.NewCompositeAnalyzer()
		proc := jobs.NewProcessor(st, gh, az, jobs.ProcessorOptions{
			AnalysisTimeout: cfg.AnalysisTimeout,
		})
		worker := jobs.NewWorker(st, proc, cfg.WorkerConcurrency, cfg.WorkerPollInterval)
		go func() {
			logger.Info("embedded worker started", "concurrency", cfg.WorkerConcurrency)
			_ = worker.Run(ctx)
		}()
	}

	ipExt := httpapi.NewIPExtractor(cfg.TrustedProxies)
	limiter := httpapi.NewIPRateLimiter(cfg.RateLimitCount, cfg.RateLimitWindow)

	handler := httpapi.NewHandler(st, gh, cfg.AnalyzerVersion, httpapi.HandlerOptions{
		RateLimiter:    limiter,
		IPExtractor:    ipExt,
		ResolveTimeout: 20 * time.Second,
		Logger:         logger,
	})
	router := httpapi.NewRouter(handler, cfg.AllowedOrigins)

	srv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("RepoAtlas API listening", "addr", cfg.APIAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "error", err.Error())
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down RepoAtlas API...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
