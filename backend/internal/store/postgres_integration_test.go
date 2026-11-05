package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
)

func openTestPostgresPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL == "" {
		dbURL = "postgres://repoatlas:repoatlas@localhost:5432/repoatlas?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if pool, err := pgxpool.New(ctx, dbURL); err == nil {
		if pingErr := pool.Ping(ctx); pingErr == nil {
			t.Cleanup(func() { pool.Close() })
			return pool
		}
		pool.Close()
	}

	// Fallback to starting a real ephemeral PostgreSQL 16 instance via embedded-postgres
	epPort := uint32(15432)
	runtimeDir := filepath.Join(t.TempDir(), "epg-runtime")
	dataDir := filepath.Join(t.TempDir(), "epg-data")

	epg := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Version(embeddedpostgres.V16).
			Port(epPort).
			Username("repoatlas").
			Password("repoatlas").
			Database("repoatlas").
			RuntimePath(runtimeDir).
			DataPath(dataDir),
	)
	if err := epg.Start(); err != nil {
		if os.Getenv("REQUIRE_POSTGRES_TEST") == "1" {
			t.Fatalf("failed to start embedded PostgreSQL 16: %v", err)
		}
		t.Skipf("skipping PostgreSQL integration test (cannot connect or start embedded postgres: %v)", err)
	}
	t.Cleanup(func() {
		_ = epg.Stop()
	})

	epURL := fmt.Sprintf("postgres://repoatlas:repoatlas@127.0.0.1:%d/repoatlas?sslmode=disable", epPort)
	epCtx, epCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer epCancel()

	pool, err := pgxpool.New(epCtx, epURL)
	if err != nil {
		t.Fatalf("connect embedded postgres: %v", err)
	}
	if err := pool.Ping(epCtx); err != nil {
		pool.Close()
		t.Fatalf("ping embedded postgres: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})
	return pool
}

func execMigrationFile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, filename string) {
	t.Helper()
	migPath := filepath.Join("..", "..", "migrations", filename)
	sqlBytes, err := os.ReadFile(migPath)
	if err != nil {
		t.Fatalf("read migration %s: %v", filename, err)
	}
	if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
		t.Fatalf("exec migration %s: %v", filename, err)
	}
}

func resetAndMigrateSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	// Test down -> up -> down -> up migration cycle across all 4 migrations
	execMigrationFile(t, ctx, pool, "000004_phase6_arch_diff_and_go.down.sql")
	execMigrationFile(t, ctx, pool, "000003_pr_and_evidence.down.sql")
	execMigrationFile(t, ctx, pool, "000002_lease_owner.down.sql")
	execMigrationFile(t, ctx, pool, "000001_init.down.sql")
	execMigrationFile(t, ctx, pool, "000001_init.up.sql")
	execMigrationFile(t, ctx, pool, "000002_lease_owner.up.sql")
	execMigrationFile(t, ctx, pool, "000003_pr_and_evidence.up.sql")
	execMigrationFile(t, ctx, pool, "000004_phase6_arch_diff_and_go.up.sql")
	execMigrationFile(t, ctx, pool, "000004_phase6_arch_diff_and_go.down.sql")
	execMigrationFile(t, ctx, pool, "000003_pr_and_evidence.down.sql")
	execMigrationFile(t, ctx, pool, "000002_lease_owner.down.sql")
	execMigrationFile(t, ctx, pool, "000001_init.down.sql")
	execMigrationFile(t, ctx, pool, "000001_init.up.sql")
	execMigrationFile(t, ctx, pool, "000002_lease_owner.up.sql")
	execMigrationFile(t, ctx, pool, "000003_pr_and_evidence.up.sql")
	execMigrationFile(t, ctx, pool, "000004_phase6_arch_diff_and_go.up.sql")
}

func TestPostgresIntegration_FullSuite(t *testing.T) {
	pool := openTestPostgresPool(t)
	ctx := context.Background()

	// 1. Verify migrations up/down/up cleanly
	resetAndMigrateSchema(t, ctx, pool)

	st := NewPostgresStore(pool)

	const sha1 = "aaaa0000aaaa0000aaaa0000aaaa0000aaaa0001"
	const sha2 = "bbbb0000bbbb0000bbbb0000bbbb0000bbbb0002"

	snap1 := githubclient.RepositorySnapshot{
		GitHubID:      7001,
		Owner:         "pallets",
		Name:          "itsdangerous",
		DefaultBranch: "main",
		CommitSHA:     sha1,
		HTMLURL:       "https://github.com/pallets/itsdangerous",
	}

	// 2. Concurrent POST deduplication (同時POST時のジョブ重複防止)
	t.Run("concurrent CreateOrGetAnalysis converges to single analysis_run", func(t *testing.T) {
		const workers = 10
		ids := make([]string, workers)
		errs := make([]error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				run, _, err := st.CreateOrGetAnalysis(ctx, snap1, "v1.0.0")
				ids[idx] = run.ID
				errs[idx] = err
			}(i)
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("goroutine %d failed: %v", i, err)
			}
			if ids[i] == "" || ids[i] != ids[0] {
				t.Fatalf("expected all goroutines to get same analysis_id %q, got %q at %d", ids[0], ids[i], i)
			}
		}

		activeCount, err := st.CountActiveAnalyses(ctx)
		if err != nil || activeCount != 1 {
			t.Fatalf("expected exactly 1 active analysis in DB, got %d (err=%v)", activeCount, err)
		}
	})

	// 3. FOR UPDATE SKIP LOCKED concurrent claim (競合claim防止)
	t.Run("concurrent ClaimNextAnalysis with FOR UPDATE SKIP LOCKED never double-claims", func(t *testing.T) {
		snap2 := githubclient.RepositorySnapshot{
			GitHubID:      7002,
			Owner:         "psf",
			Name:          "requests",
			DefaultBranch: "main",
			CommitSHA:     sha2,
			HTMLURL:       "https://github.com/psf/requests",
		}
		_, _, err := st.CreateOrGetAnalysis(ctx, snap2, "v1.0.0")
		if err != nil {
			t.Fatalf("create 2nd analysis: %v", err)
		}

		// There are now 2 queued jobs. Launch 6 concurrent claimers.
		const claimers = 6
		var mu sync.Mutex
		claimedIDs := make(map[string]string) // analysisID -> leaseOwner
		var wg sync.WaitGroup
		for i := 0; i < claimers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				owner := newUUIDv4()
				run, ok, cErr := st.ClaimNextAnalysis(ctx, owner, 60*time.Second)
				if cErr != nil {
					t.Errorf("claim error: %v", cErr)
					return
				}
				if ok && run != nil {
					mu.Lock()
					if prevOwner, dup := claimedIDs[run.ID]; dup {
						t.Errorf("duplicate claim on %s by %s and %s", run.ID, prevOwner, owner)
					}
					claimedIDs[run.ID] = owner
					mu.Unlock()
				}
			}(i)
		}
		wg.Wait()

		if len(claimedIDs) != 2 {
			t.Fatalf("expected exactly 2 distinct jobs claimed, got %d: %+v", len(claimedIDs), claimedIDs)
		}
	})

	// 4. Rollback on error during save & completed cache reuse
	t.Run("transaction rollback on save error and completed cache reuse", func(t *testing.T) {
		run1, _, err := st.CreateOrGetAnalysis(ctx, snap1, "v1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		fullRun1, err := st.GetAnalysis(ctx, run1.ID)
		if err != nil || fullRun1.LeaseOwner == nil {
			t.Fatalf("expected run1 to have lease_owner set, got %+v err=%v", fullRun1, err)
		}
		owner1 := *fullRun1.LeaseOwner

		// Force an error mid-transaction in CompleteAnalysis:
		// First file is valid, but import references a non-existent target file path.
		badOutput := graph.BuildOutput{
			Files: []domain.SourceFile{
				{Path: "src/itsdangerous/__init__.py", FunctionsCount: 2, ClassesCount: 1, IsEntrypoint: false, ImportanceScore: 3.0},
			},
			Imports: []domain.ImportReference{
				{
					SourcePath:     "src/itsdangerous/__init__.py",
					TargetPath:     "src/itsdangerous/does_not_exist.py",
					ImportText:     "from . import does_not_exist",
					ModuleName:     ".does_not_exist",
					LineNumber:     1,
					Classification: domain.ImportInternal,
				},
			},
			FilesCount:         1,
			InternalEdgesCount: 1,
		}

		err = st.CompleteAnalysis(ctx, run1.ID, owner1, badOutput)
		if err == nil {
			t.Fatal("expected CompleteAnalysis with invalid internal target to fail and rollback")
		}

		// Verify rollback: source_files count in DB for run1.ID must be 0, and status must still be 'running'
		var fileCount int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM source_files WHERE analysis_id = $1::uuid`, run1.ID).Scan(&fileCount); err != nil {
			t.Fatal(err)
		}
		if fileCount != 0 {
			t.Fatalf("expected 0 source_files after transaction rollback, got %d", fileCount)
		}

		afterRollback, err := st.GetAnalysis(ctx, run1.ID)
		if err != nil || afterRollback.Status != domain.StatusRunning {
			t.Fatalf("expected status to remain running after rollback, got %+v (err=%v)", afterRollback, err)
		}

		// Now save valid output and verify atomic completion + cache reuse
		goodOutput := graph.BuildOutput{
			Files: []domain.SourceFile{
				{Path: "src/itsdangerous/__init__.py", FunctionsCount: 1, ClassesCount: 0, InDegree: 0, OutDegree: 1, IsEntrypoint: false, ImportanceScore: 0.5},
				{Path: "src/itsdangerous/signer.py", FunctionsCount: 4, ClassesCount: 2, InDegree: 1, OutDegree: 0, IsEntrypoint: false, ImportanceScore: 2.0},
			},
			Edges: []graph.Edge{
				{SourcePath: "src/itsdangerous/__init__.py", TargetPath: "src/itsdangerous/signer.py", Kind: "import"},
			},
			Imports: []domain.ImportReference{
				{
					SourcePath:     "src/itsdangerous/__init__.py",
					TargetPath:     "src/itsdangerous/signer.py",
					ImportText:     "from .signer import Signer",
					ModuleName:     ".signer.Signer",
					LineNumber:     1,
					Classification: domain.ImportInternal,
				},
			},
			FilesCount:         2,
			InternalEdgesCount: 1,
		}

		if err := st.CompleteAnalysis(ctx, run1.ID, owner1, goodOutput); err != nil {
			t.Fatalf("CompleteAnalysis valid output failed: %v", err)
		}

		// Calling CreateOrGetAnalysis again must return cached=true and status=completed
		cachedRun, cached, err := st.CreateOrGetAnalysis(ctx, snap1, "v1.0.0")
		if err != nil || !cached || cachedRun.Status != domain.StatusCompleted || cachedRun.ID != run1.ID {
			t.Fatalf("expected completed cache hit for %s, got cached=%v run=%+v err=%v", run1.ID, cached, cachedRun, err)
		}

		// Verify GetAnalysisGraph returns the 2 files and 1 deduplicated internal edge
		_, files, edges, err := st.GetAnalysisGraph(ctx, run1.ID)
		if err != nil || len(files) != 2 || len(edges) != 1 {
			t.Fatalf("unexpected graph from DB: files=%d edges=%d err=%v", len(files), len(edges), err)
		}
	})

	// 5. Expired lease recovery & stale worker fencing (期限切れリースの回復と古いWorkerの上書き防止)
	t.Run("expired lease recovery, stale worker fencing, and max attempts", func(t *testing.T) {
		snap2 := githubclient.RepositorySnapshot{
			GitHubID:      7002,
			Owner:         "psf",
			Name:          "requests",
			DefaultBranch: "main",
			CommitSHA:     sha2,
			HTMLURL:       "https://github.com/psf/requests",
		}
		run2, _, err := st.CreateOrGetAnalysis(ctx, snap2, "v1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		fullRun2, err := st.GetAnalysis(ctx, run2.ID)
		if err != nil || fullRun2.LeaseOwner == nil {
			t.Fatalf("expected run2 to have lease_owner set, got %+v err=%v", fullRun2, err)
		}
		staleOwner := *fullRun2.LeaseOwner

		// Force lease expiration in PostgreSQL
		_, err = pool.Exec(ctx, `
			UPDATE analysis_runs
			SET lease_expires_at = now() - interval '10 seconds'
			WHERE id = $1::uuid
		`, run2.ID)
		if err != nil {
			t.Fatal(err)
		}

		// New worker claims the expired job -> attempt_count becomes 2, lease_owner becomes "fresh-worker-2"
		reclaimed, ok, err := st.ClaimNextAnalysis(ctx, "fresh-worker-2", 60*time.Second)
		if err != nil || !ok || reclaimed == nil || reclaimed.ID != run2.ID || reclaimed.AttemptCount != 2 {
			t.Fatalf("expected expired job to be reclaimed (attempt 2), got ok=%v run=%+v err=%v", ok, reclaimed, err)
		}

		// Stale worker tries to CompleteAnalysis or FailAnalysis -> must be rejected with ErrLeaseLost
		err = st.CompleteAnalysis(ctx, run2.ID, staleOwner, graph.BuildOutput{})
		if !errors.Is(err, domain.ErrLeaseLost) {
			t.Fatalf("expected ErrLeaseLost for stale worker CompleteAnalysis, got %v", err)
		}
		err = st.FailAnalysis(ctx, run2.ID, staleOwner, "ERR", "stale")
		if !errors.Is(err, domain.ErrLeaseLost) {
			t.Fatalf("expected ErrLeaseLost for stale worker FailAnalysis, got %v", err)
		}

		// Fresh worker extends lease via heartbeat -> succeeds
		if err := st.ExtendAnalysisLease(ctx, run2.ID, "fresh-worker-2", 120*time.Second); err != nil {
			t.Fatalf("ExtendAnalysisLease for active owner failed: %v", err)
		}

		// Now simulate attempt_count = 3 and lease expiring -> next ClaimNextAnalysis marks it failed
		_, err = pool.Exec(ctx, `
			UPDATE analysis_runs
			SET attempt_count = 3,
			    lease_expires_at = now() - interval '10 seconds'
			WHERE id = $1::uuid
		`, run2.ID)
		if err != nil {
			t.Fatal(err)
		}

		noneClaimed, ok, err := st.ClaimNextAnalysis(ctx, "worker-3", 60*time.Second)
		if err != nil || ok || noneClaimed != nil {
			t.Fatalf("expected no job claimed when attempt_count=3 expired, got ok=%v run=%+v err=%v", ok, noneClaimed, err)
		}

		exhaustedRun, err := st.GetAnalysis(ctx, run2.ID)
		if err != nil || exhaustedRun.Status != domain.StatusFailed || exhaustedRun.ErrorCode == nil || *exhaustedRun.ErrorCode != "MAX_ATTEMPTS_EXCEEDED" {
			t.Fatalf("expected exhausted run to be marked failed with MAX_ATTEMPTS_EXCEEDED, got %+v err=%v", exhaustedRun, err)
		}
	})

	// 6. Go package nodes and Architecture Diff in Postgres
	t.Run("postgres stores and retrieves Go packages and architecture diffs", func(t *testing.T) {
		snapGo := githubclient.RepositorySnapshot{
			GitHubID:      7003,
			Owner:         "spf13",
			Name:          "cobra",
			DefaultBranch: "main",
			CommitSHA:     "cccc0000cccc0000cccc0000cccc0000cccc0003",
			HTMLURL:       "https://github.com/spf13/cobra",
		}
		runGo, _, err := st.CreateOrGetAnalysis(ctx, snapGo, "v1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		claimedRun, ok, err := st.ClaimNextAnalysis(ctx, "worker-go", 60*time.Second)
		if err != nil || !ok || claimedRun == nil {
			t.Fatalf("ClaimNextAnalysis failed: ok=%v err=%v", ok, err)
		}
		owner := "worker-go"

		goOutput := graph.BuildOutput{
			Language:         "go",
			GraphGranularity: "package",
			Files: []domain.SourceFile{
				{
					Path:              "cobra",
					NodeKind:          "package",
					Language:          "go",
					ModulePath:        "github.com/spf13/cobra",
					PackageImportPath: "github.com/spf13/cobra",
					DirectoryPath:     ".",
					PackageName:       "cobra",
					FilesCount:        5,
					FunctionsCount:    10,
					MethodsCount:      20,
					TypesCount:        4,
					IsEntrypoint:      false,
					ImportanceScore:   5.0,
				},
			},
			Imports: []domain.ImportReference{
				{
					SourcePath:       "cobra",
					SourceFile:       "command.go",
					SourcePackage:    "github.com/spf13/cobra",
					TargetPackage:    "fmt",
					ImportText:       `import "fmt"`,
					LineNumber:       10,
					ColumnNumber:     1,
					Classification:   domain.ImportExternal,
					BuildConstraints: "none",
				},
			},
			FilesCount: 1,
		}
		if err := st.CompleteAnalysis(ctx, runGo.ID, owner, goOutput); err != nil {
			t.Fatalf("CompleteAnalysis for Go failed: %v", err)
		}

		// Verify import references retrieval
		refs, err := st.GetImportReferences(ctx, runGo.ID)
		if err != nil || len(refs) != 1 {
			t.Fatalf("expected 1 import reference, got %d (err=%v)", len(refs), err)
		}
		if refs[0].SourceFile != "command.go" || refs[0].TargetPackage != "fmt" {
			t.Fatalf("unexpected ref: %+v", refs[0])
		}

		// Test Architecture Diff in Postgres
		diffAnalysis, cached, err := st.CreateOrGetArchitectureDiff(ctx, snapGo, sha1, sha2, "v1.0.0", "v1.0.0")
		if err != nil || cached || diffAnalysis.ID == "" {
			t.Fatalf("CreateOrGetArchitectureDiff failed: cached=%v id=%s err=%v", cached, diffAnalysis.ID, err)
		}

		diffSummary := domain.DiffSummary{
			BaseCommitSHA: sha1,
			HeadCommitSHA: sha2,
			Language:      "go",
			Granularity:   "package",
		}
		if err := st.CompleteArchitectureDiff(ctx, diffAnalysis.ID, "", diffSummary); err != nil {
			t.Fatalf("CompleteArchitectureDiff failed: %v", err)
		}

		getDiff, err := st.GetArchitectureDiff(ctx, diffAnalysis.ID)
		if err != nil || getDiff.Status != domain.DiffStatusCompleted {
			t.Fatalf("expected completed diff from DB, got status=%s err=%v", getDiff.Status, err)
		}
	})

	// 6. PR Analysis persistence, claim, lifecycle, and impact query
	t.Run("PR analysis lifecycle and impact persistence", func(t *testing.T) {
		prSnap := domain.PullRequestSnapshot{
			Repository: domain.Repository{
				GitHubRepositoryID: 8001,
				Owner:              "pallets",
				Name:               "itsdangerous",
				DefaultBranch:      "main",
				HTMLURL:            "https://github.com/pallets/itsdangerous",
			},
			PullNumber: 296,
			Title:      "Catch unhandled ValueError from future timestamp",
			State:      "open",
			BaseSHA:    "3b762645dab2250933311f8612989a4075100659",
			HeadSHA:    "177196d3a5c7ad37b8ccbd0f88445aced07f1ca0",
			BaseRef:    "2.1.x",
			HeadRef:    "main",
		}

		prRun, cached, err := st.CreateOrGetPRAnalysis(ctx, prSnap, "v1.0.0")
		if err != nil {
			t.Fatalf("CreateOrGetPRAnalysis failed: %v", err)
		}
		if cached {
			t.Fatalf("expected cached=false for new PR analysis")
		}
		if prRun.Status != domain.StatusQueued {
			t.Fatalf("expected status=queued, got %s", prRun.Status)
		}

		// Claim the PR analysis
		claimed, ok, err := st.ClaimNextPRAnalysis(ctx, "pr-worker-1", 60*time.Second)
		if err != nil || !ok || claimed == nil || claimed.ID != prRun.ID {
			t.Fatalf("ClaimNextPRAnalysis failed: ok=%v claimed=%+v err=%v", ok, claimed, err)
		}

		// Update stages
		stages := []domain.AnalysisStage{
			domain.StageFetching,
			domain.StageAnalyzingHead,
			domain.StageAnalyzingImpact,
		}
		for _, stage := range stages {
			if err := st.UpdatePRAnalysisStage(ctx, prRun.ID, "pr-worker-1", stage, 60*time.Second); err != nil {
				t.Fatalf("UpdatePRAnalysisStage %s failed: %v", stage, err)
			}
		}

		prFiles := []domain.PullRequestFile{
			{
				Path:       "src/itsdangerous/timed.py",
				Status:     domain.PRFileModified,
				Additions:  5,
				Deletions:  1,
				Changes:    6,
				IsPython:   true,
				IsAnalyzed: true,
			},
			{
				Path:             "CHANGES.rst",
				Status:           domain.PRFileModified,
				Additions:        2,
				Deletions:        0,
				Changes:          2,
				IsPython:         false,
				IsAnalyzed:       false,
				UnanalyzedReason: "non_python_file",
			},
		}
		summary := domain.PRImpactSummary{
			ChangedFilesCount:    2,
			DirectImpactCount:    1,
			IndirectImpactCount:  0,
			TotalAffectedCount:   1,
			UnanalyzedFilesCount: 1,
		}

		if err := st.CompletePRAnalysis(ctx, prRun.ID, "pr-worker-1", "", prFiles, summary); err != nil {
			t.Fatalf("CompletePRAnalysis failed: %v", err)
		}

		impactRun, savedFiles, err := st.GetPRAnalysisImpact(ctx, prRun.ID)
		if err != nil {
			t.Fatalf("GetPRAnalysisImpact failed: %v", err)
		}
		if impactRun.Status != domain.StatusCompleted || len(savedFiles) != 2 {
			t.Fatalf("unexpected PR impact result: status=%s files=%d", impactRun.Status, len(savedFiles))
		}
	})

	// 7. Import Evidence query and verification
	t.Run("Import evidence persistence and query", func(t *testing.T) {
		snap3 := githubclient.RepositorySnapshot{
			GitHubID:      7003,
			Owner:         "testowner",
			Name:          "evidence-repo",
			DefaultBranch: "main",
			CommitSHA:     "cccc0000cccc0000cccc0000cccc0000cccc0003",
			HTMLURL:       "https://github.com/testowner/evidence-repo",
		}
		run3, _, err := st.CreateOrGetAnalysis(ctx, snap3, "v2.0.0")
		if err != nil {
			t.Fatalf("CreateOrGetAnalysis run3 failed: %v", err)
		}
		claimed3, ok, err := st.ClaimNextAnalysis(ctx, "evidence-worker-3", 60*time.Second)
		if err != nil || !ok || claimed3 == nil {
			t.Fatalf("ClaimNextAnalysis run3 failed: ok=%v claimed=%+v err=%v", ok, claimed3, err)
		}
		owner3 := "evidence-worker-3"

		evidenceOutput := graph.BuildOutput{
			Files: []domain.SourceFile{
				{Path: "app/main.py", FunctionsCount: 2, ClassesCount: 0},
				{Path: "app/models.py", FunctionsCount: 1, ClassesCount: 1},
			},
			Edges: []graph.Edge{
				{SourcePath: "app/main.py", TargetPath: "app/models.py", Kind: "import"},
			},
			Imports: []domain.ImportReference{
				{
					SourcePath:       "app/main.py",
					TargetPath:       "app/models.py",
					ImportText:       "from app.models import User",
					ModuleName:       "app.models.User",
					LineNumber:       5,
					StartLine:        5,
					StartColumn:      1,
					EndLine:          5,
					EndColumn:        28,
					ImportKind:       "absolute",
					Classification:   domain.ImportInternal,
					ResolutionReason: "resolved to local module app/models.py",
				},
				{
					SourcePath:       "app/main.py",
					TargetPath:       "",
					ImportText:       "from ..ghost import phantom",
					ModuleName:       "..ghost",
					LineNumber:       10,
					StartLine:        10,
					StartColumn:      1,
					EndLine:          10,
					EndColumn:        28,
					ImportKind:       "relative",
					Classification:   domain.ImportUnresolved,
					ResolutionReason: "relative import beyond root",
				},
			},
			FilesCount:             2,
			InternalEdgesCount:     1,
			ExternalImportsCount:   0,
			UnresolvedImportsCount: 1,
		}

		if err := st.CompleteAnalysis(ctx, run3.ID, owner3, evidenceOutput); err != nil {
			t.Fatalf("CompleteAnalysis with evidence failed: %v", err)
		}

		// Filter by edge
		edgeEvidences, err := st.GetImportEvidences(ctx, run3.ID, "app/main.py", "app/models.py", false)
		if err != nil || len(edgeEvidences) != 1 {
			t.Fatalf("GetImportEvidences edge failed: len=%d err=%v", len(edgeEvidences), err)
		}
		if edgeEvidences[0].ImportStatement != "from app.models import User" {
			t.Fatalf("unexpected edge evidence: %+v", edgeEvidences[0])
		}

		// Filter by unresolved
		unresolved, err := st.GetImportEvidences(ctx, run3.ID, "", "", true)
		if err != nil || len(unresolved) != 1 {
			t.Fatalf("GetImportEvidences unresolved failed: len=%d err=%v", len(unresolved), err)
		}
		if unresolved[0].ResolutionStatus != "unresolved" {
			t.Fatalf("expected unresolved evidence, got %+v", unresolved[0])
		}
	})
}

// TestPostgresIntegration_MigrationAudit_Phase5ToPhase6 performs an explicit audit of the migration path:
// 1. Fresh DB migration across all migrations (000001 -> 000002 -> 000003 -> 000004)
// 2. Migration from an existing Phase 5 DB (000001 + 000002 + 000003 applied with Phase 5 data) to Phase 6 (000004)
// 3. Verifies zero data loss, correct default values on new columns, rollback safety, and re-application.
func TestPostgresIntegration_MigrationAudit_Phase5ToPhase6(t *testing.T) {
	pool := openTestPostgresPool(t)
	ctx := context.Background()

	// Step A: Reset schema to completely clean state
	execMigrationFile(t, ctx, pool, "000004_phase6_arch_diff_and_go.down.sql")
	execMigrationFile(t, ctx, pool, "000003_pr_and_evidence.down.sql")
	execMigrationFile(t, ctx, pool, "000002_lease_owner.down.sql")
	execMigrationFile(t, ctx, pool, "000001_init.down.sql")

	// Step B: Set up a Phase 5 applied DB (Migrations 1, 2, and 3 only)
	execMigrationFile(t, ctx, pool, "000001_init.up.sql")
	execMigrationFile(t, ctx, pool, "000002_lease_owner.up.sql")
	execMigrationFile(t, ctx, pool, "000003_pr_and_evidence.up.sql")

	// Step C: Insert rich Phase 5 data (Python analysis runs, source files, import evidence, and PR analyses)
	var repoID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO repositories (github_repository_id, owner, name, default_branch, html_url)
		VALUES (99991, 'audit-owner', 'audit-py-repo', 'main', 'https://github.com/audit-owner/audit-py-repo')
		RETURNING id
	`).Scan(&repoID)
	if err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	const pyCommitSHA = "1111222233334444555566667777888899990001"
	var pyAnalysisID string
	err = pool.QueryRow(ctx, `
		INSERT INTO analysis_runs (repository_id, commit_sha, analyzer_version, status, stage, files_count, internal_edges_count)
		VALUES ($1, $2, 'v2.0.0', 'completed', 'completed', 2, 1)
		RETURNING id::text
	`, repoID, pyCommitSHA).Scan(&pyAnalysisID)
	if err != nil {
		t.Fatalf("insert analysis_run: %v", err)
	}

	var mainFileID, modFileID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO source_files (analysis_id, path, functions_count, classes_count, in_degree, out_degree, is_entrypoint, importance_score)
		VALUES ($1::uuid, 'app/main.py', 1, 0, 0, 1, true, 4.5)
		RETURNING id
	`, pyAnalysisID).Scan(&mainFileID)
	if err != nil {
		t.Fatalf("insert main.py: %v", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO source_files (analysis_id, path, functions_count, classes_count, in_degree, out_degree, is_entrypoint, importance_score)
		VALUES ($1::uuid, 'app/models.py', 0, 2, 1, 0, false, 3.0)
		RETURNING id
	`, pyAnalysisID).Scan(&modFileID)
	if err != nil {
		t.Fatalf("insert models.py: %v", err)
	}

	var impRefID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO import_references (analysis_id, source_file_id, target_file_id, import_text, module_name, line_number, classification, resolution_note)
		VALUES ($1::uuid, $2, $3, 'from app.models import User', 'app.models.User', 1, 'internal', 'local module')
		RETURNING id
	`, pyAnalysisID, mainFileID, modFileID).Scan(&impRefID)
	if err != nil {
		t.Fatalf("insert import_reference: %v", err)
	}

	var evID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO import_evidence (
			analysis_id, source_file_id, target_file_id,
			import_statement, module_name, start_line, start_column, end_line, end_column,
			import_kind, resolution_status, resolution_reason
		)
		VALUES ($1::uuid, $2, $3, 'from app.models import User', 'app.models.User', 1, 1, 1, 28, 'absolute', 'internal', 'local module')
		RETURNING id
	`, pyAnalysisID, mainFileID, modFileID).Scan(&evID)
	if err != nil {
		t.Fatalf("insert import_evidence: %v", err)
	}

	var prAnalysisID string
	err = pool.QueryRow(ctx, `
		INSERT INTO pull_request_analyses (
			repository_id, pull_number, title, state, base_sha, head_sha, analyzer_version, status, stage
		)
		VALUES (
			$1, 42, 'Feature branch PR', 'open',
			'1111111111111111111111111111111111111111',
			$2,
			'v2.0.0', 'completed', 'completed'
		)
		RETURNING id::text
	`, repoID, pyCommitSHA).Scan(&prAnalysisID)
	if err != nil {
		t.Fatalf("insert pull_request_analysis: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO pull_request_files (pr_analysis_id, path, status, additions, deletions, changes, is_python, is_analyzed)
		VALUES ($1::uuid, 'app/main.py', 'modified', 10, 2, 12, true, true)
	`, prAnalysisID)
	if err != nil {
		t.Fatalf("insert pull_request_file: %v", err)
	}

	// Step D: Apply Phase 6 migration (000004_phase6_arch_diff_and_go.up.sql) onto Phase 5 DB
	execMigrationFile(t, ctx, pool, "000004_phase6_arch_diff_and_go.up.sql")

	// Step E: Verify Phase 5 data retention and new column defaults
	var lang, granularity string
	err = pool.QueryRow(ctx, `SELECT language, graph_granularity FROM analysis_runs WHERE id = $1::uuid`, pyAnalysisID).Scan(&lang, &granularity)
	if err != nil {
		t.Fatalf("query migrated analysis_run: %v", err)
	}
	if lang != "python" || granularity != "file" {
		t.Errorf("expected default language='python', granularity='file', got lang=%s gran=%s", lang, granularity)
	}

	var nodeKind, sfLang string
	var filesCount int
	err = pool.QueryRow(ctx, `SELECT node_kind, language, files_count FROM source_files WHERE id = $1`, mainFileID).Scan(&nodeKind, &sfLang, &filesCount)
	if err != nil {
		t.Fatalf("query migrated source_files: %v", err)
	}
	if nodeKind != "file" || sfLang != "python" || filesCount != 1 {
		t.Errorf("expected default node_kind='file', language='python', files_count=1, got kind=%s lang=%s count=%d", nodeKind, sfLang, filesCount)
	}

	var colNum int
	err = pool.QueryRow(ctx, `SELECT column_number FROM import_references WHERE id = $1`, impRefID).Scan(&colNum)
	if err != nil {
		t.Fatalf("query migrated import_references: %v", err)
	}
	if colNum != 1 {
		t.Errorf("expected default column_number=1, got %d", colNum)
	}

	// Verify import_evidence and PR tables are completely intact
	var countEv int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM import_evidence WHERE analysis_id = $1::uuid`, pyAnalysisID).Scan(&countEv)
	if countEv != 1 {
		t.Errorf("expected 1 import_evidence row, got %d", countEv)
	}

	var countPR int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM pull_request_analyses WHERE id = $1::uuid`, prAnalysisID).Scan(&countPR)
	if countPR != 1 {
		t.Errorf("expected 1 pull_request_analysis row, got %d", countPR)
	}

	// Step F: Verify inserting Phase 6 data (Go package node & architecture_diffs) succeeds
	var goAnalysisID string
	const goCommitSHA = "2222333344445555666677778888999900001112"
	err = pool.QueryRow(ctx, `
		INSERT INTO analysis_runs (repository_id, commit_sha, analyzer_version, status, stage, language, graph_granularity)
		VALUES ($1, $2, 'v2.0.0', 'completed', 'completed', 'go', 'package')
		RETURNING id::text
	`, repoID, goCommitSHA).Scan(&goAnalysisID)
	if err != nil {
		t.Fatalf("insert Go analysis_run: %v", err)
	}

	var goPkgID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO source_files (
			analysis_id, path, functions_count, classes_count, in_degree, out_degree,
			node_kind, language, module_path, package_import_path, directory_path, package_name, files_count
		)
		VALUES (
			$1::uuid, 'cmd/server', 5, 0, 0, 1,
			'package', 'go', 'github.com/audit-owner/audit-go-repo', 'github.com/audit-owner/audit-go-repo/cmd/server', 'cmd/server', 'main', 3
		)
		RETURNING id
	`, goAnalysisID).Scan(&goPkgID)
	if err != nil {
		t.Fatalf("insert Go package: %v", err)
	}

	var diffID string
	err = pool.QueryRow(ctx, `
		INSERT INTO architecture_diffs (
			repository_id, base_sha, head_sha, analyzer_version, diff_algorithm_version, status, stage
		)
		VALUES ($1, $2, $3, 'v2.0.0', 'v1.0.0', 'queued', 'queued')
		RETURNING id::text
	`, repoID, pyCommitSHA, goCommitSHA).Scan(&diffID)
	if err != nil {
		t.Fatalf("insert architecture_diff: %v", err)
	}

	// Step G: Test Rollback (000004_phase6_arch_diff_and_go.down.sql)
	execMigrationFile(t, ctx, pool, "000004_phase6_arch_diff_and_go.down.sql")

	// After rollback, architecture_diffs is dropped, but Phase 5 data remains
	var checkTableExists bool
	_ = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables WHERE table_name = 'architecture_diffs'
		)
	`).Scan(&checkTableExists)
	if checkTableExists {
		t.Errorf("architecture_diffs should be dropped after 000004 rollback")
	}

	// Phase 5 data should still be intact
	var countEvAfterRollback int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM import_evidence WHERE analysis_id = $1::uuid`, pyAnalysisID).Scan(&countEvAfterRollback)
	if countEvAfterRollback != 1 {
		t.Errorf("expected 1 import_evidence after rollback, got %d", countEvAfterRollback)
	}

	// Step H: Test Reapplication of Phase 6 migration
	execMigrationFile(t, ctx, pool, "000004_phase6_arch_diff_and_go.up.sql")

	// Verify architecture_diffs is recreated
	_ = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables WHERE table_name = 'architecture_diffs'
		)
	`).Scan(&checkTableExists)
	if !checkTableExists {
		t.Errorf("architecture_diffs should exist after re-applying 000004")
	}
}

func BenchmarkPostgres_CompleteAnalysis(b *testing.B) {
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		b.Skip("skipping postgres benchmark: TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		b.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	st := NewPostgresStore(pool)

	snap := githubclient.RepositorySnapshot{
		GitHubID:      9999,
		Owner:         "benchorg",
		Name:          "benchrepo",
		DefaultBranch: "main",
		HTMLURL:       "https://github.com/benchorg/benchrepo",
	}

	// Prepare 50 packages with 50 imports
	files := make([]domain.SourceFile, 50)
	imports := make([]domain.ImportReference, 50)
	for j := 0; j < 50; j++ {
		pkgPath := fmt.Sprintf("github.com/benchorg/benchrepo/pkg%d", j)
		files[j] = domain.SourceFile{
			Path:              pkgPath,
			NodeKind:          "package",
			Language:          "go",
			ModulePath:        "github.com/benchorg/benchrepo",
			PackageImportPath: pkgPath,
			DirectoryPath:     fmt.Sprintf("pkg%d", j),
			PackageName:       fmt.Sprintf("pkg%d", j),
			FilesCount:        3,
			FunctionsCount:    10,
			MethodsCount:      5,
			TypesCount:        4,
			ImportanceScore:   float64(50 - j),
		}
		nextPkg := fmt.Sprintf("github.com/benchorg/benchrepo/pkg%d", (j+1)%50)
		imports[j] = domain.ImportReference{
			SourcePath:     pkgPath,
			TargetPath:     nextPkg,
			Classification: domain.ImportInternal,
			ModuleName:     nextPkg,
			ImportText:     fmt.Sprintf(`import "%s"`, nextPkg),
			LineNumber:     5,
		}
	}
	buildOut := graph.BuildOutput{
		Language:         "go",
		GraphGranularity: "package",
		Files:            files,
		Imports:          imports,
	}

	b.ResetTimer()
	prefix := fmt.Sprintf("%016x", time.Now().UnixNano())
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		snap.CommitSHA = fmt.Sprintf("%s%024x", prefix, i+1)
		run, cached, err := st.CreateOrGetAnalysis(ctx, snap, "v1.0.0")
		if err != nil {
			b.Fatalf("create run: %v", err)
		}
		if cached {
			b.Fatalf("unexpected cache hit in benchmark")
		}
		claimed, ok, err := st.ClaimNextAnalysis(ctx, "bench-worker", 30*time.Second)
		if err != nil || !ok || claimed == nil {
			b.Fatalf("claim: ok=%v err=%v", ok, err)
		}

		b.StartTimer()
		err = st.CompleteAnalysis(ctx, run.ID, "bench-worker", buildOut)
		if err != nil {
			b.Fatalf("complete: %v", err)
		}
	}
}
