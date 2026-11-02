package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv               string
	DatabaseURL          string
	GitHubToken          string
	APIAddr              string
	AllowedOrigins       []string
	AnalyzerVersion      string
	WorkerConcurrency    int
	WorkerPollInterval   time.Duration
	MaxQueuedJobs        int
	RateLimitCount       int
	RateLimitWindow      time.Duration
	RateLimitPerHour     int
	TrustedProxies       []string
	AnalysisTimeout      time.Duration
	MockGitHubFixtureDir string
}

// IsProduction returns true when running in a production environment
// (APP_ENV=production or RAILWAY_ENVIRONMENT_NAME=production).
func (c Config) IsProduction() bool {
	return strings.EqualFold(c.AppEnv, "production")
}

func Load() Config {
	appEnv := getEnv("APP_ENV", "")
	if appEnv == "" {
		if rwEnv := strings.TrimSpace(os.Getenv("RAILWAY_ENVIRONMENT_NAME")); rwEnv != "" {
			appEnv = rwEnv
		} else {
			appEnv = "development"
		}
	}

	// Prefer DATABASE_PRIVATE_URL when provided (Railway internal network), fallback to DATABASE_URL
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_PRIVATE_URL"))
	if dbURL == "" {
		dbURL = getEnv("DATABASE_URL", "postgres://repoatlas:repoatlas@localhost:5432/repoatlas?sslmode=disable")
	}

	githubToken := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))

	// Support Railway's injected PORT environment variable when API_ADDR is not explicitly set
	apiAddr := strings.TrimSpace(os.Getenv("API_ADDR"))
	if apiAddr == "" {
		if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
			if strings.HasPrefix(port, ":") {
				apiAddr = port
			} else {
				apiAddr = ":" + port
			}
		} else {
			apiAddr = ":8080"
		}
	}

	originsRaw := getEnv("ALLOWED_ORIGINS", "http://localhost:3000")
	var origins []string
	for _, o := range strings.Split(originsRaw, ",") {
		trimmed := strings.TrimSpace(o)
		if trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	analyzerVersion := getEnv("ANALYZER_VERSION", "v1.0.0")

	concurrency := 1
	if cStr := os.Getenv("WORKER_CONCURRENCY"); cStr != "" {
		if c, err := strconv.Atoi(cStr); err == nil && c > 0 {
			concurrency = c
		}
	}

	pollInterval := 2 * time.Second
	if pStr := os.Getenv("WORKER_POLL_INTERVAL"); pStr != "" {
		if d, err := time.ParseDuration(pStr); err == nil && d > 0 {
			pollInterval = d
		}
	}

	maxQueuedJobs := 20
	if qStr := os.Getenv("MAX_QUEUED_JOBS"); qStr != "" {
		if q, err := strconv.Atoi(qStr); err == nil && q > 0 {
			maxQueuedJobs = q
		}
	}

	rateLimitPerHour := 60
	if rStr := os.Getenv("RATE_LIMIT_PER_HOUR"); rStr != "" {
		if r, err := strconv.Atoi(rStr); err == nil && r > 0 {
			rateLimitPerHour = r
		}
	}

	rateLimitCount := rateLimitPerHour
	rateLimitWindow := time.Hour
	if perMinStr := os.Getenv("POST_ANALYSES_RATE_LIMIT_PER_MIN"); perMinStr != "" {
		if pm, err := strconv.Atoi(perMinStr); err == nil && pm > 0 {
			rateLimitCount = pm
			rateLimitWindow = time.Minute
		}
	}

	var trustedProxies []string
	if tpRaw := os.Getenv("TRUSTED_PROXIES"); tpRaw != "" {
		for _, p := range strings.Split(tpRaw, ",") {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				trustedProxies = append(trustedProxies, trimmed)
			}
		}
	}

	analysisTimeout := 90 * time.Second
	if tStr := os.Getenv("ANALYSIS_TIMEOUT"); tStr != "" {
		if d, err := time.ParseDuration(tStr); err == nil && d > 0 {
			analysisTimeout = d
		}
	}

	// Security: Never allow E2E fixture directory fallback in production
	mockFixtureDir := strings.TrimSpace(os.Getenv("MOCK_GITHUB_FIXTURE_DIR"))
	if strings.EqualFold(appEnv, "production") {
		mockFixtureDir = ""
	}

	return Config{
		AppEnv:               appEnv,
		DatabaseURL:          dbURL,
		GitHubToken:          githubToken,
		APIAddr:              apiAddr,
		AllowedOrigins:       origins,
		AnalyzerVersion:      analyzerVersion,
		WorkerConcurrency:    concurrency,
		WorkerPollInterval:   pollInterval,
		MaxQueuedJobs:        maxQueuedJobs,
		RateLimitCount:       rateLimitCount,
		RateLimitWindow:      rateLimitWindow,
		RateLimitPerHour:     rateLimitPerHour,
		TrustedProxies:       trustedProxies,
		AnalysisTimeout:      analysisTimeout,
		MockGitHubFixtureDir: mockFixtureDir,
	}
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
