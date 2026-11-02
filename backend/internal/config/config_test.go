package config

import (
	"testing"
	"time"
)

func TestLoad_RailwayPortAndProductionGuards(t *testing.T) {
	t.Setenv("API_ADDR", "")
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_PRIVATE_URL", "postgres://u:p@postgres.railway.internal:5432/railway?sslmode=disable")
	t.Setenv("DATABASE_URL", "postgres://u:p@public.proxy.rlwy.net:12345/railway")
	t.Setenv("RAILWAY_ENVIRONMENT_NAME", "production")
	t.Setenv("APP_ENV", "")
	t.Setenv("MOCK_GITHUB_FIXTURE_DIR", "/app/testdata")
	t.Setenv("POST_ANALYSES_RATE_LIMIT_PER_MIN", "12")

	cfg := Load()

	if cfg.APIAddr != ":9090" {
		t.Fatalf("expected APIAddr :9090 from Railway PORT, got %s", cfg.APIAddr)
	}
	if cfg.DatabaseURL != "postgres://u:p@postgres.railway.internal:5432/railway?sslmode=disable" {
		t.Fatalf("expected DATABASE_PRIVATE_URL to take precedence, got %s", cfg.DatabaseURL)
	}
	if !cfg.IsProduction() {
		t.Fatalf("expected IsProduction()=true when RAILWAY_ENVIRONMENT_NAME=production, got AppEnv=%s", cfg.AppEnv)
	}
	if cfg.MockGitHubFixtureDir != "" {
		t.Fatalf("expected MockGitHubFixtureDir to be disabled in production, got %q", cfg.MockGitHubFixtureDir)
	}
	if cfg.RateLimitCount != 12 || cfg.RateLimitWindow != time.Minute {
		t.Fatalf("expected 12 req/min rate limit, got count=%d window=%v", cfg.RateLimitCount, cfg.RateLimitWindow)
	}
}
