package store

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

func (s *PostgresStore) CreateOrGetPRAnalysis(ctx context.Context, snap domain.PullRequestSnapshot, analyzerVersion string) (domain.PullRequestAnalysis, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PullRequestAnalysis{}, false, fmt.Errorf("begin create pr analysis tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Ensure repository exists
	var repoID int64
	var repoOwner, repoName, defaultBranch, htmlURL string
	err = tx.QueryRow(ctx, `
		INSERT INTO repositories (github_repository_id, owner, name, default_branch, html_url)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (github_repository_id) DO UPDATE
		SET owner = EXCLUDED.owner,
		    name = EXCLUDED.name,
		    html_url = EXCLUDED.html_url,
		    updated_at = now()
		RETURNING id, owner, name, default_branch, html_url
	`, snap.Repository.GitHubRepositoryID, snap.Repository.Owner, snap.Repository.Name, snap.Repository.DefaultBranch, snap.Repository.HTMLURL).
		Scan(&repoID, &repoOwner, &repoName, &defaultBranch, &htmlURL)
	if err != nil {
		return domain.PullRequestAnalysis{}, false, fmt.Errorf("upsert repository for pr: %w", err)
	}

	repo := domain.Repository{
		ID:                 repoID,
		GitHubRepositoryID: snap.Repository.GitHubRepositoryID,
		Owner:              repoOwner,
		Name:               repoName,
		DefaultBranch:      defaultBranch,
		HTMLURL:            htmlURL,
	}

	// Check existing PR analysis with same (repository_id, pull_number, base_sha, head_sha, analyzer_version)
	existing, found, err := s.getPRAnalysisBySnapshotTx(ctx, tx, repoID, snap.PullNumber, snap.BaseSHA, snap.HeadSHA, analyzerVersion, repo)
	if err != nil {
		return domain.PullRequestAnalysis{}, false, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return domain.PullRequestAnalysis{}, false, fmt.Errorf("commit existing pr analysis tx: %w", err)
		}
		if existing.Status == domain.StatusCompleted {
			return existing, true, nil
		}
		return existing, false, nil
	}

	// Check active queue limit
	if s.maxQueuedJobs > 0 {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(739001)`); err != nil {
			return domain.PullRequestAnalysis{}, false, fmt.Errorf("acquire pr queue advisory lock: %w", err)
		}
		activeCount, err := s.countActiveAnalysesTx(ctx, tx)
		if err != nil {
			return domain.PullRequestAnalysis{}, false, fmt.Errorf("count active analyses: %w", err)
		}
		if activeCount >= s.maxQueuedJobs {
			return domain.PullRequestAnalysis{}, false, domain.NewAppError(
				domain.ErrCodeQueueLimitExceeded,
				http.StatusTooManyRequests,
				"現在解析キューが混み合っています。しばらく待ってから再試行してください",
				nil,
			)
		}
	}

	// Insert new PR analysis run
	var prRun domain.PullRequestAnalysis
	prRun.Repository = repo
	prRun.RepositoryID = repoID
	prRun.PullNumber = snap.PullNumber
	prRun.Title = snap.Title
	prRun.State = snap.State
	prRun.BaseSHA = snap.BaseSHA
	prRun.HeadSHA = snap.HeadSHA
	prRun.BaseRef = snap.BaseRef
	prRun.HeadRef = snap.HeadRef
	prRun.AnalyzerVersion = analyzerVersion
	prRun.Status = domain.StatusQueued
	prRun.Stage = domain.StageQueued

	err = tx.QueryRow(ctx, `
		INSERT INTO pull_request_analyses (
			repository_id, pull_number, title, state,
			base_sha, head_sha, base_ref, head_ref,
			analyzer_version, status, stage
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at
	`, repoID, snap.PullNumber, snap.Title, snap.State,
		snap.BaseSHA, snap.HeadSHA, snap.BaseRef, snap.HeadRef,
		analyzerVersion, string(domain.StatusQueued), string(domain.StageQueued)).
		Scan(&prRun.ID, &prRun.CreatedAt)
	if err != nil {
		return domain.PullRequestAnalysis{}, false, fmt.Errorf("insert pull_request_analysis: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.PullRequestAnalysis{}, false, fmt.Errorf("commit create pr analysis tx: %w", err)
	}

	return prRun, false, nil
}

func (s *PostgresStore) getPRAnalysisBySnapshotTx(ctx context.Context, tx pgx.Tx, repoID int64, pullNumber int, baseSHA, headSHA, analyzerVersion string, repo domain.Repository) (domain.PullRequestAnalysis, bool, error) {
	row := tx.QueryRow(ctx, `
		SELECT
			id, pull_number, title, state,
			base_sha, head_sha, base_ref, head_ref,
			analyzer_version, status, stage, attempt_count,
			lease_owner, lease_expires_at, head_analysis_id, base_analysis_id,
			error_code, error_message,
			changed_files_count, direct_impact_count, indirect_impact_count,
			total_affected_count, unanalyzed_files_count,
			created_at, started_at, completed_at
		FROM pull_request_analyses
		WHERE repository_id = $1
		  AND pull_number = $2
		  AND base_sha = $3
		  AND head_sha = $4
		  AND analyzer_version = $5
	`, repoID, pullNumber, baseSHA, headSHA, analyzerVersion)

	run, err := scanPRAnalysisRow(row, repo)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PullRequestAnalysis{}, false, nil
	}
	if err != nil {
		return domain.PullRequestAnalysis{}, false, fmt.Errorf("query existing pr analysis: %w", err)
	}
	return run, true, nil
}

func (s *PostgresStore) GetPRAnalysis(ctx context.Context, id string) (domain.PullRequestAnalysis, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			pr.id, pr.pull_number, pr.title, pr.state,
			pr.base_sha, pr.head_sha, pr.base_ref, pr.head_ref,
			pr.analyzer_version, pr.status, pr.stage, pr.attempt_count,
			pr.lease_owner, pr.lease_expires_at, pr.head_analysis_id, pr.base_analysis_id,
			pr.error_code, pr.error_message,
			pr.changed_files_count, pr.direct_impact_count, pr.indirect_impact_count,
			pr.total_affected_count, pr.unanalyzed_files_count,
			pr.created_at, pr.started_at, pr.completed_at,
			repo.id, repo.github_repository_id, repo.owner, repo.name, repo.default_branch, repo.html_url
		FROM pull_request_analyses pr
		JOIN repositories repo ON repo.id = pr.repository_id
		WHERE pr.id = $1::uuid
	`, id)

	var run domain.PullRequestAnalysis
	var repo domain.Repository
	var status, stage string
	var headAnalysisIDPtr, baseAnalysisIDPtr *string

	err := row.Scan(
		&run.ID, &run.PullNumber, &run.Title, &run.State,
		&run.BaseSHA, &run.HeadSHA, &run.BaseRef, &run.HeadRef,
		&run.AnalyzerVersion, &status, &stage, &run.AttemptCount,
		&run.LeaseOwner, &run.LeaseExpiresAt, &headAnalysisIDPtr, &baseAnalysisIDPtr,
		&run.ErrorCode, &run.ErrorMessage,
		&run.ChangedFilesCount, &run.DirectImpactCount, &run.IndirectImpactCount,
		&run.TotalAffectedCount, &run.UnanalyzedFilesCount,
		&run.CreatedAt, &run.StartedAt, &run.CompletedAt,
		&repo.ID, &repo.GitHubRepositoryID, &repo.Owner, &repo.Name, &repo.DefaultBranch, &repo.HTMLURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PullRequestAnalysis{}, domain.NewAppError(
			domain.ErrCodeAnalysisNotFound,
			http.StatusNotFound,
			"指定されたPull Request解析が見つかりません",
			nil,
		)
	}
	if err != nil {
		return domain.PullRequestAnalysis{}, fmt.Errorf("get pull request analysis %s: %w", id, err)
	}

	run.Status = domain.AnalysisStatus(status)
	run.Stage = domain.AnalysisStage(stage)
	run.Repository = repo
	run.RepositoryID = repo.ID
	run.HeadAnalysisID = headAnalysisIDPtr
	run.BaseAnalysisID = baseAnalysisIDPtr
	if run.UnanalyzedFilesCount > 0 {
		run.AnalysisCompleteness = "partial"
	} else {
		run.AnalysisCompleteness = "complete"
	}
	return run, nil
}

func (s *PostgresStore) ClaimNextPRAnalysis(ctx context.Context, leaseOwner string, leaseDuration time.Duration) (*domain.PullRequestAnalysis, bool, error) {
	if leaseOwner == "" {
		leaseOwner = newUUIDv4()
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin claim pr tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Expire exhausted runs
	_, err = tx.Exec(ctx, `
		UPDATE pull_request_analyses
		SET status = 'failed',
		    stage = 'failed',
		    error_code = 'MAX_ATTEMPTS_EXCEEDED',
		    error_message = 'PR解析ジョブの試行回数が上限を超えました',
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    completed_at = now()
		WHERE status = 'running'
		  AND lease_expires_at < now()
		  AND attempt_count >= 3
	`)
	if err != nil {
		return nil, false, fmt.Errorf("expire exhausted pr runs: %w", err)
	}

	leaseSeconds := leaseDuration.Seconds()
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}

	var claimedID string
	err = tx.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM pull_request_analyses
			WHERE (status = 'queued')
			   OR (status = 'running' AND lease_expires_at < now() AND attempt_count < 3)
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE pull_request_analyses pr
		SET status = 'running',
		    stage = 'fetching',
		    lease_owner = $1,
		    lease_expires_at = now() + make_interval(secs => $2),
		    attempt_count = pr.attempt_count + 1,
		    started_at = COALESCE(pr.started_at, now()),
		    error_code = NULL,
		    error_message = NULL
		FROM candidate
		WHERE pr.id = candidate.id
		RETURNING pr.id
	`, leaseOwner, leaseSeconds).Scan(&claimedID)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("claim next pr analysis: %w", err)
	}

	row := tx.QueryRow(ctx, `
		SELECT
			pr.id, pr.pull_number, pr.title, pr.state,
			pr.base_sha, pr.head_sha, pr.base_ref, pr.head_ref,
			pr.analyzer_version, pr.status, pr.stage, pr.attempt_count,
			pr.lease_owner, pr.lease_expires_at, pr.head_analysis_id, pr.base_analysis_id,
			pr.error_code, pr.error_message,
			pr.changed_files_count, pr.direct_impact_count, pr.indirect_impact_count,
			pr.total_affected_count, pr.unanalyzed_files_count,
			pr.created_at, pr.started_at, pr.completed_at,
			repo.id, repo.github_repository_id, repo.owner, repo.name, repo.default_branch, repo.html_url
		FROM pull_request_analyses pr
		JOIN repositories repo ON repo.id = pr.repository_id
		WHERE pr.id = $1::uuid
	`, claimedID)

	run, err := scanPRAnalysisRowWithRepo(row)
	if err != nil {
		return nil, false, fmt.Errorf("read claimed pr analysis %s: %w", claimedID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit claim pr tx: %w", err)
	}

	return &run, true, nil
}

func (s *PostgresStore) ExtendPRAnalysisLease(ctx context.Context, id string, leaseOwner string, leaseDuration time.Duration) error {
	leaseSeconds := leaseDuration.Seconds()
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE pull_request_analyses
		SET lease_expires_at = now() + make_interval(secs => $3)
		WHERE id = $1::uuid
		  AND status = 'running'
		  AND lease_owner = $2
	`, id, leaseOwner, leaseSeconds)
	if err != nil {
		return fmt.Errorf("extend pr lease: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}

func (s *PostgresStore) UpdatePRAnalysisStage(ctx context.Context, id string, leaseOwner string, stage domain.AnalysisStage, leaseDuration time.Duration) error {
	leaseSeconds := leaseDuration.Seconds()
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE pull_request_analyses
		SET stage = $3,
		    lease_expires_at = now() + make_interval(secs => $4)
		WHERE id = $1::uuid
		  AND status = 'running'
		  AND lease_owner = $2
	`, id, leaseOwner, string(stage), leaseSeconds)
	if err != nil {
		return fmt.Errorf("update pr stage: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}

func (s *PostgresStore) CompletePRAnalysis(ctx context.Context, id string, leaseOwner string, headAnalysisID string, files []domain.PullRequestFile, summary domain.PRImpactSummary) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin complete pr tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Verify lease ownership under row lock
	var currentStatus string
	var currentOwner *string
	err = tx.QueryRow(ctx, `
		SELECT status, lease_owner
		FROM pull_request_analyses
		WHERE id = $1::uuid
		FOR UPDATE
	`, id).Scan(&currentStatus, &currentOwner)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock pull_request_analysis: %w", err)
	}
	if currentStatus != string(domain.StatusRunning) {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && (currentOwner == nil || *currentOwner != leaseOwner) {
		return domain.ErrLeaseLost
	}

	// Delete existing files from previous attempt
	if _, err := tx.Exec(ctx, `DELETE FROM pull_request_files WHERE pr_analysis_id = $1::uuid`, id); err != nil {
		return fmt.Errorf("delete old pull_request_files: %w", err)
	}

	// Insert files
	for _, f := range files {
		var prevPath *string
		if f.PreviousPath != "" {
			prevPath = &f.PreviousPath
		}
		var reason *string
		if f.UnanalyzedReason != "" {
			reason = &f.UnanalyzedReason
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO pull_request_files (
				pr_analysis_id, path, previous_path, status,
				additions, deletions, changes, is_python, is_analyzed, unanalyzed_reason
			)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, id, f.Path, prevPath, string(f.Status), f.Additions, f.Deletions, f.Changes, f.IsPython, f.IsAnalyzed, reason)
		if err != nil {
			return fmt.Errorf("insert pull_request_file %s: %w", f.Path, err)
		}
	}

	var headIDPtr *string
	if headAnalysisID != "" {
		headIDPtr = &headAnalysisID
	}

	tag, err := tx.Exec(ctx, `
		UPDATE pull_request_analyses
		SET status = 'completed',
		    stage = 'completed',
		    head_analysis_id = $2::uuid,
		    changed_files_count = $3,
		    direct_impact_count = $4,
		    indirect_impact_count = $5,
		    total_affected_count = $6,
		    unanalyzed_files_count = $7,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    completed_at = now()
		WHERE id = $1::uuid
	`, id, headIDPtr, summary.ChangedFilesCount, summary.DirectImpactCount,
		summary.IndirectImpactCount, summary.TotalAffectedCount, summary.UnanalyzedFilesCount)
	if err != nil {
		return fmt.Errorf("update completed pr analysis: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) FailPRAnalysis(ctx context.Context, id string, leaseOwner string, code string, message string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE pull_request_analyses
		SET status = 'failed',
		    stage = 'failed',
		    error_code = $3,
		    error_message = $4,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    completed_at = now()
		WHERE id = $1::uuid
		  AND (lease_owner = $2 OR $2 = '' OR lease_owner IS NULL)
	`, id, leaseOwner, code, message)
	if err != nil {
		return fmt.Errorf("fail pr analysis: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}

func (s *PostgresStore) GetPRAnalysisImpact(ctx context.Context, id string) (domain.PullRequestAnalysis, []domain.PullRequestFile, error) {
	prRun, err := s.GetPRAnalysis(ctx, id)
	if err != nil {
		return domain.PullRequestAnalysis{}, nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			id, pr_analysis_id::text, path, COALESCE(previous_path, ''),
			status, additions, deletions, changes, is_python, is_analyzed,
			COALESCE(unanalyzed_reason, '')
		FROM pull_request_files
		WHERE pr_analysis_id = $1::uuid
		ORDER BY path ASC
	`, id)
	if err != nil {
		return domain.PullRequestAnalysis{}, nil, fmt.Errorf("query pull_request_files: %w", err)
	}
	defer rows.Close()

	var files []domain.PullRequestFile
	for rows.Next() {
		var f domain.PullRequestFile
		var statusStr string
		err := rows.Scan(
			&f.ID, &f.PRAnalysisID, &f.Path, &f.PreviousPath,
			&statusStr, &f.Additions, &f.Deletions, &f.Changes,
			&f.IsPython, &f.IsAnalyzed, &f.UnanalyzedReason,
		)
		if err != nil {
			return domain.PullRequestAnalysis{}, nil, fmt.Errorf("scan pull_request_file: %w", err)
		}
		f.Status = domain.PullRequestFileStatus(statusStr)
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return domain.PullRequestAnalysis{}, nil, fmt.Errorf("iterate pull_request_files: %w", err)
	}
	if files == nil {
		files = []domain.PullRequestFile{}
	}

	return prRun, files, nil
}

func scanPRAnalysisRow(row pgx.Row, repo domain.Repository) (domain.PullRequestAnalysis, error) {
	var run domain.PullRequestAnalysis
	var status, stage string
	var headAnalysisIDPtr, baseAnalysisIDPtr *string

	err := row.Scan(
		&run.ID, &run.PullNumber, &run.Title, &run.State,
		&run.BaseSHA, &run.HeadSHA, &run.BaseRef, &run.HeadRef,
		&run.AnalyzerVersion, &status, &stage, &run.AttemptCount,
		&run.LeaseOwner, &run.LeaseExpiresAt, &headAnalysisIDPtr, &baseAnalysisIDPtr,
		&run.ErrorCode, &run.ErrorMessage,
		&run.ChangedFilesCount, &run.DirectImpactCount, &run.IndirectImpactCount,
		&run.TotalAffectedCount, &run.UnanalyzedFilesCount,
		&run.CreatedAt, &run.StartedAt, &run.CompletedAt,
	)
	if err != nil {
		return domain.PullRequestAnalysis{}, err
	}
	run.Status = domain.AnalysisStatus(status)
	run.Stage = domain.AnalysisStage(stage)
	run.Repository = repo
	run.RepositoryID = repo.ID
	run.HeadAnalysisID = headAnalysisIDPtr
	run.BaseAnalysisID = baseAnalysisIDPtr
	if run.UnanalyzedFilesCount > 0 {
		run.AnalysisCompleteness = "partial"
	} else {
		run.AnalysisCompleteness = "complete"
	}
	return run, nil
}

func scanPRAnalysisRowWithRepo(row pgx.Row) (domain.PullRequestAnalysis, error) {
	var run domain.PullRequestAnalysis
	var repo domain.Repository
	var status, stage string
	var headAnalysisIDPtr, baseAnalysisIDPtr *string

	err := row.Scan(
		&run.ID, &run.PullNumber, &run.Title, &run.State,
		&run.BaseSHA, &run.HeadSHA, &run.BaseRef, &run.HeadRef,
		&run.AnalyzerVersion, &status, &stage, &run.AttemptCount,
		&run.LeaseOwner, &run.LeaseExpiresAt, &headAnalysisIDPtr, &baseAnalysisIDPtr,
		&run.ErrorCode, &run.ErrorMessage,
		&run.ChangedFilesCount, &run.DirectImpactCount, &run.IndirectImpactCount,
		&run.TotalAffectedCount, &run.UnanalyzedFilesCount,
		&run.CreatedAt, &run.StartedAt, &run.CompletedAt,
		&repo.ID, &repo.GitHubRepositoryID, &repo.Owner, &repo.Name, &repo.DefaultBranch, &repo.HTMLURL,
	)
	if err != nil {
		return domain.PullRequestAnalysis{}, err
	}
	run.Status = domain.AnalysisStatus(status)
	run.Stage = domain.AnalysisStage(stage)
	run.Repository = repo
	run.RepositoryID = repo.ID
	run.HeadAnalysisID = headAnalysisIDPtr
	run.BaseAnalysisID = baseAnalysisIDPtr
	if run.UnanalyzedFilesCount > 0 {
		run.AnalysisCompleteness = "partial"
	} else {
		run.AnalysisCompleteness = "complete"
	}
	return run, nil
}
