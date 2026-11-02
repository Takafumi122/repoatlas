package store

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *PostgresStore) CountActiveAnalyses(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM analysis_runs
		WHERE status IN ('queued', 'running')
	`).Scan(&count)
	return count, err
}

func (s *PostgresStore) CreateOrGetAnalysis(ctx context.Context, snap githubclient.RepositorySnapshot, analyzerVersion string) (domain.AnalysisRun, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AnalysisRun{}, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var repoID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO repositories (github_repository_id, owner, name, default_branch, html_url, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (github_repository_id) DO UPDATE
		SET owner = EXCLUDED.owner,
		    name = EXCLUDED.name,
		    default_branch = EXCLUDED.default_branch,
		    html_url = EXCLUDED.html_url,
		    updated_at = now()
		RETURNING id
	`, snap.GitHubID, snap.Owner, snap.Name, snap.DefaultBranch, snap.HTMLURL).Scan(&repoID)
	if err != nil {
		return domain.AnalysisRun{}, false, fmt.Errorf("upsert repository: %w", err)
	}

	var runID string
	var status string
	var stage string
	var createdAt time.Time
	var completedAt *time.Time

	// First check if a snapshot run already exists so cache hits and duplicate in-flight requests
	// are never rejected by the pending queue cap.
	existingErr := tx.QueryRow(ctx, `
		SELECT id::text, status, stage, created_at, completed_at
		FROM analysis_runs
		WHERE repository_id = $1 AND commit_sha = $2 AND analyzer_version = $3
	`, repoID, snap.CommitSHA, analyzerVersion).Scan(&runID, &status, &stage, &createdAt, &completedAt)

	inserted := false
	if errors.Is(existingErr, pgx.ErrNoRows) {
		if s.maxQueuedJobs > 0 {
			// Acquire transaction-scoped advisory lock so concurrent POSTs across multiple API processes
			// cannot race past maxQueuedJobs under READ COMMITTED isolation.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(739001)`); err != nil {
				return domain.AnalysisRun{}, false, fmt.Errorf("acquire queue limit lock: %w", err)
			}
			var activeCount int
			if err := tx.QueryRow(ctx, `
				SELECT COUNT(*)
				FROM analysis_runs
				WHERE status IN ('queued', 'running')
			`).Scan(&activeCount); err != nil {
				return domain.AnalysisRun{}, false, fmt.Errorf("count active analyses: %w", err)
			}
			if activeCount >= s.maxQueuedJobs {
				return domain.AnalysisRun{}, false, domain.NewAppError(
					domain.ErrCodeQueueLimitExceeded,
					http.StatusTooManyRequests,
					"現在解析待ちのジョブが上限に達しています。しばらく待ってから再試行してください",
					nil,
				)
			}
		}

		insertErr := tx.QueryRow(ctx, `
			INSERT INTO analysis_runs (repository_id, commit_sha, analyzer_version, status, stage)
			VALUES ($1, $2, $3, 'queued', 'queued')
			ON CONFLICT (repository_id, commit_sha, analyzer_version) DO NOTHING
			RETURNING id::text, status, stage, created_at, completed_at
		`, repoID, snap.CommitSHA, analyzerVersion).Scan(&runID, &status, &stage, &createdAt, &completedAt)

		if errors.Is(insertErr, pgx.ErrNoRows) {
			inserted = false
			if err := tx.QueryRow(ctx, `
				SELECT id::text, status, stage, created_at, completed_at
				FROM analysis_runs
				WHERE repository_id = $1 AND commit_sha = $2 AND analyzer_version = $3
			`, repoID, snap.CommitSHA, analyzerVersion).Scan(&runID, &status, &stage, &createdAt, &completedAt); err != nil {
				return domain.AnalysisRun{}, false, fmt.Errorf("select existing analysis_run: %w", err)
			}
		} else if insertErr != nil {
			return domain.AnalysisRun{}, false, fmt.Errorf("insert analysis_run: %w", insertErr)
		} else {
			inserted = true
		}
	} else if existingErr != nil {
		return domain.AnalysisRun{}, false, fmt.Errorf("query existing analysis_run: %w", existingErr)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.AnalysisRun{}, false, fmt.Errorf("commit tx: %w", err)
	}

	run := domain.AnalysisRun{
		ID:              runID,
		RepositoryID:    repoID,
		CommitSHA:       snap.CommitSHA,
		AnalyzerVersion: analyzerVersion,
		Status:          domain.AnalysisStatus(status),
		Stage:           domain.AnalysisStage(stage),
		CreatedAt:       createdAt,
		CompletedAt:     completedAt,
		Repository: domain.Repository{
			ID:                 repoID,
			GitHubRepositoryID: snap.GitHubID,
			Owner:              snap.Owner,
			Name:               snap.Name,
			DefaultBranch:      snap.DefaultBranch,
			HTMLURL:            snap.HTMLURL,
		},
	}

	cached := !inserted && run.Status == domain.StatusCompleted
	return run, cached, nil
}

func (s *PostgresStore) GetAnalysis(ctx context.Context, id string) (domain.AnalysisRun, error) {
	if !uuidRegex.MatchString(id) {
		return domain.AnalysisRun{}, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定された解析IDが見つかりません", nil)
	}

	var run domain.AnalysisRun
	var status, stage string
	err := s.pool.QueryRow(ctx, `
		SELECT
			a.id::text,
			a.repository_id,
			a.commit_sha,
			a.analyzer_version,
			a.status,
			a.stage,
			a.attempt_count,
			a.lease_owner,
			a.lease_expires_at,
			a.error_code,
			a.error_message,
			a.files_count,
			a.internal_edges_count,
			a.external_imports_count,
			a.unresolved_imports_count,
			a.created_at,
			a.started_at,
			a.completed_at,
			r.id,
			r.github_repository_id,
			r.owner,
			r.name,
			r.default_branch,
			r.html_url
		FROM analysis_runs a
		JOIN repositories r ON r.id = a.repository_id
		WHERE a.id = $1::uuid
	`, id).Scan(
		&run.ID,
		&run.RepositoryID,
		&run.CommitSHA,
		&run.AnalyzerVersion,
		&status,
		&stage,
		&run.AttemptCount,
		&run.LeaseOwner,
		&run.LeaseExpiresAt,
		&run.ErrorCode,
		&run.ErrorMessage,
		&run.FilesCount,
		&run.InternalEdgesCount,
		&run.ExternalImportsCount,
		&run.UnresolvedImportsCount,
		&run.CreatedAt,
		&run.StartedAt,
		&run.CompletedAt,
		&run.Repository.ID,
		&run.Repository.GitHubRepositoryID,
		&run.Repository.Owner,
		&run.Repository.Name,
		&run.Repository.DefaultBranch,
		&run.Repository.HTMLURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AnalysisRun{}, domain.NewAppError(domain.ErrCodeAnalysisNotFound, http.StatusNotFound, "指定された解析IDが見つかりません", nil)
	}
	if err != nil {
		return domain.AnalysisRun{}, fmt.Errorf("get analysis: %w", err)
	}
	run.Status = domain.AnalysisStatus(status)
	run.Stage = domain.AnalysisStage(stage)
	return run, nil
}

func (s *PostgresStore) ClaimNextAnalysis(ctx context.Context, leaseOwner string, leaseDuration time.Duration) (*domain.AnalysisRun, bool, error) {
	if leaseOwner == "" {
		leaseOwner = newUUIDv4()
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin claim tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Mark expired runs that reached attempt limit as failed
	_, err = tx.Exec(ctx, `
		UPDATE analysis_runs
		SET status = 'failed',
		    stage = 'failed',
		    error_code = 'MAX_ATTEMPTS_EXCEEDED',
		    error_message = '解析ジョブの試行回数が上限を超えました',
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    completed_at = now()
		WHERE status = 'running'
		  AND lease_expires_at < now()
		  AND attempt_count >= 3
	`)
	if err != nil {
		return nil, false, fmt.Errorf("expire exhausted runs: %w", err)
	}

	leaseSeconds := leaseDuration.Seconds()
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}

	var claimedID string
	err = tx.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM analysis_runs
			WHERE attempt_count < 3
			  AND (status = 'queued'
			   OR (status = 'running' AND lease_expires_at < now()))
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE analysis_runs a
		SET status = 'running',
		    stage = 'fetching',
		    attempt_count = attempt_count + 1,
		    started_at = COALESCE(started_at, now()),
		    lease_owner = $2,
		    lease_expires_at = now() + ($1 * interval '1 second')
		FROM candidate
		WHERE a.id = candidate.id
		  AND a.attempt_count < 3
		RETURNING a.id::text
	`, leaseSeconds, leaseOwner).Scan(&claimedID)

	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Commit(ctx)
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("claim analysis_run: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit claim tx: %w", err)
	}

	run, err := s.GetAnalysis(ctx, claimedID)
	if err != nil {
		return nil, false, err
	}
	return &run, true, nil
}

func (s *PostgresStore) ExtendAnalysisLease(ctx context.Context, id string, leaseOwner string, leaseDuration time.Duration) error {
	leaseSeconds := leaseDuration.Seconds()
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE analysis_runs
		SET lease_expires_at = now() + ($3 * interval '1 second')
		WHERE id = $1::uuid
		  AND status = 'running'
		  AND lease_owner = $2
	`, id, leaseOwner, leaseSeconds)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}

func (s *PostgresStore) UpdateAnalysisStage(ctx context.Context, id string, leaseOwner string, stage domain.AnalysisStage, leaseDuration time.Duration) error {
	leaseSeconds := leaseDuration.Seconds()
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE analysis_runs
		SET stage = $3,
		    lease_expires_at = now() + ($4 * interval '1 second')
		WHERE id = $1::uuid
		  AND status = 'running'
		  AND ($2 = '' OR lease_owner = $2)
	`, id, leaseOwner, string(stage), leaseSeconds)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}

func (s *PostgresStore) FailAnalysis(ctx context.Context, id string, leaseOwner string, code string, message string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE analysis_runs
		SET status = 'failed',
		    stage = 'failed',
		    error_code = $2,
		    error_message = $3,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    completed_at = now()
		WHERE id = $1::uuid
		  AND status = 'running'
		  AND ($4 = '' OR lease_owner = $4)
	`, id, code, message, leaseOwner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}
