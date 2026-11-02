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
	// Test down -> up -> down -> up migration cycle
	execMigrationFile(t, ctx, pool, "000002_lease_owner.down.sql")
	execMigrationFile(t, ctx, pool, "000001_init.down.sql")
	execMigrationFile(t, ctx, pool, "000001_init.up.sql")
	execMigrationFile(t, ctx, pool, "000002_lease_owner.up.sql")
	execMigrationFile(t, ctx, pool, "000002_lease_owner.down.sql")
	execMigrationFile(t, ctx, pool, "000001_init.down.sql")
	execMigrationFile(t, ctx, pool, "000001_init.up.sql")
	execMigrationFile(t, ctx, pool, "000002_lease_owner.up.sql")
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
}
