package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
)

type Store interface {
	CreateOrGetAnalysis(ctx context.Context, snap githubclient.RepositorySnapshot, analyzerVersion string) (domain.AnalysisRun, bool, error)
	CountActiveAnalyses(ctx context.Context) (int, error)
	GetAnalysis(ctx context.Context, id string) (domain.AnalysisRun, error)
	GetAnalysisGraph(ctx context.Context, id string) (domain.AnalysisRun, []domain.SourceFile, []graph.Edge, error)
	GetImportReferences(ctx context.Context, analysisID string) ([]domain.ImportReference, error)
	ClaimNextAnalysis(ctx context.Context, leaseOwner string, leaseDuration time.Duration) (*domain.AnalysisRun, bool, error)
	ExtendAnalysisLease(ctx context.Context, id string, leaseOwner string, leaseDuration time.Duration) error
	UpdateAnalysisStage(ctx context.Context, id string, leaseOwner string, stage domain.AnalysisStage, leaseDuration time.Duration) error
	CompleteAnalysis(ctx context.Context, id string, leaseOwner string, output graph.BuildOutput) error
	FailAnalysis(ctx context.Context, id string, leaseOwner string, code string, message string) error
	CreateOrGetArchitectureDiff(ctx context.Context, snap githubclient.RepositorySnapshot, baseSHA, headSHA, analyzerVersion, diffAlgVersion string) (domain.ArchitectureDiffAnalysis, bool, error)
	GetArchitectureDiff(ctx context.Context, id string) (domain.ArchitectureDiffAnalysis, error)
	CompleteArchitectureDiff(ctx context.Context, id string, leaseOwner string, summary domain.DiffSummary) error
	FailArchitectureDiff(ctx context.Context, id string, leaseOwner string, code string, message string) error

	// Phase 5 additions:
	GetImportEvidences(ctx context.Context, analysisID string, sourcePath, targetPath string, unresolvedOnly bool) ([]domain.ImportEvidence, error)
	CreateOrGetPRAnalysis(ctx context.Context, snap domain.PullRequestSnapshot, analyzerVersion string) (domain.PullRequestAnalysis, bool, error)
	GetPRAnalysis(ctx context.Context, id string) (domain.PullRequestAnalysis, error)
	ClaimNextPRAnalysis(ctx context.Context, leaseOwner string, leaseDuration time.Duration) (*domain.PullRequestAnalysis, bool, error)
	ExtendPRAnalysisLease(ctx context.Context, id string, leaseOwner string, leaseDuration time.Duration) error
	UpdatePRAnalysisStage(ctx context.Context, id string, leaseOwner string, stage domain.AnalysisStage, leaseDuration time.Duration) error
	CompletePRAnalysis(ctx context.Context, id string, leaseOwner string, headAnalysisID string, files []domain.PullRequestFile, summary domain.PRImpactSummary) error
	FailPRAnalysis(ctx context.Context, id string, leaseOwner string, code string, message string) error
	GetPRAnalysisImpact(ctx context.Context, id string) (domain.PullRequestAnalysis, []domain.PullRequestFile, error)
}

type PostgresStore struct {
	pool          *pgxpool.Pool
	maxQueuedJobs int
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) SetMaxQueuedJobs(max int) {
	s.maxQueuedJobs = max
}

// ApplyMigrations acquires a PostgreSQL transaction-scoped advisory lock and executes
// all *.up.sql files in migrationsDir in lexical order. Because our up migrations use
// IF NOT EXISTS, this is safe to run concurrently from API and Worker startup.
func (s *PostgresStore) ApplyMigrations(ctx context.Context, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", migrationsDir, err)
	}
	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)
	if len(upFiles) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(739002)`); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}

	for _, name := range upFiles {
		sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("exec migration %s: %w", name, err)
		}
	}

	return tx.Commit(ctx)
}
