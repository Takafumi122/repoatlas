package jobs

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

type failingGHClient struct {
	err error
}

func (f *failingGHClient) Resolve(_ context.Context, _, _ string) (githubclient.RepositorySnapshot, error) {
	return githubclient.RepositorySnapshot{}, f.err
}

func (f *failingGHClient) ResolveCommit(_ context.Context, _, _, _ string) (githubclient.RepositorySnapshot, error) {
	return githubclient.RepositorySnapshot{}, f.err
}

func (f *failingGHClient) GetPullRequest(_ context.Context, _, _ string, _ int) (domain.PullRequestSnapshot, error) {
	return domain.PullRequestSnapshot{}, f.err
}

func (f *failingGHClient) GetPullRequestFiles(_ context.Context, _, _ string, _ int, _ int) ([]domain.PullRequestFile, error) {
	return nil, f.err
}

func (f *failingGHClient) DownloadArchive(_ context.Context, _ githubclient.RepositorySnapshot) (io.ReadCloser, error) {
	return nil, f.err
}

type slowZipGHClient struct {
	delay   time.Duration
	zipData []byte
}

func (s *slowZipGHClient) Resolve(_ context.Context, _, _ string) (githubclient.RepositorySnapshot, error) {
	return githubclient.RepositorySnapshot{}, nil
}

func (s *slowZipGHClient) ResolveCommit(_ context.Context, _, _, _ string) (githubclient.RepositorySnapshot, error) {
	return githubclient.RepositorySnapshot{}, nil
}

func (s *slowZipGHClient) GetPullRequest(_ context.Context, _, _ string, _ int) (domain.PullRequestSnapshot, error) {
	return domain.PullRequestSnapshot{}, nil
}

func (s *slowZipGHClient) GetPullRequestFiles(_ context.Context, _, _ string, _ int, _ int) ([]domain.PullRequestFile, error) {
	return nil, nil
}

func (s *slowZipGHClient) DownloadArchive(ctx context.Context, _ githubclient.RepositorySnapshot) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(s.delay):
		return io.NopCloser(bytes.NewReader(s.zipData)), nil
	}
}

func makeMinimalZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("repo-sha/main.py")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, "def main():\n    return 1\n")
	_ = zw.Close()
	return buf.Bytes()
}

func TestWorker_LeaseRecoveryAndFailureHandling(t *testing.T) {
	st := store.NewMemoryStore()
	snap := githubclient.RepositorySnapshot{
		GitHubID:      101,
		Owner:         "psf",
		Name:          "requests",
		DefaultBranch: "main",
		CommitSHA:     "0123456789abcdef0123456789abcdef01234567",
		HTMLURL:       "https://github.com/psf/requests",
	}

	run, cached, err := st.CreateOrGetAnalysis(context.Background(), snap, "v1.0.0")
	if err != nil || cached {
		t.Fatalf("CreateOrGetAnalysis failed: cached=%v err=%v", cached, err)
	}

	gh := &failingGHClient{
		err: domain.NewAppError(domain.ErrCodeRepoTooLarge, http.StatusRequestEntityTooLarge, "ZIPサイズ超過", errors.New("too large")),
	}
	proc := NewProcessor(st, gh, analyzer.NewPythonAnalyzer(), ProcessorOptions{
		LeaseDuration:   50 * time.Millisecond,
		AnalysisTimeout: 5 * time.Second,
	})
	worker := NewWorker(st, proc, 1, 10*time.Millisecond)

	claimed, stepErr := worker.StepOnce(context.Background())
	if !claimed || stepErr == nil {
		t.Fatalf("expected claimed=true and error from failing archive download, got claimed=%v err=%v", claimed, stepErr)
	}

	updated, err := st.GetAnalysis(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetAnalysis error: %v", err)
	}
	if updated.Status != domain.StatusFailed || updated.ErrorCode == nil || *updated.ErrorCode != string(domain.ErrCodeRepoTooLarge) {
		t.Fatalf("expected failed status with REPO_TOO_LARGE, got %+v", updated)
	}
}

func TestWorker_StaleWorkerFencingAndMaxAttempts(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	snap := githubclient.RepositorySnapshot{
		GitHubID:      202,
		Owner:         "pallets",
		Name:          "itsdangerous",
		DefaultBranch: "main",
		CommitSHA:     "abcdef0123456789abcdef0123456789abcdef01",
		HTMLURL:       "https://github.com/pallets/itsdangerous",
	}

	run, _, err := st.CreateOrGetAnalysis(ctx, snap, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	// Worker 1 claims with very short lease (20ms)
	claim1, ok, err := ClaimNext(ctx, st, "worker-1", 20*time.Millisecond)
	if err != nil || !ok || claim1.AttemptCount != 1 {
		t.Fatalf("claim 1 failed: %+v err=%v", claim1, err)
	}

	// Wait for Worker 1's lease to expire
	time.Sleep(35 * time.Millisecond)

	// Worker 2 re-claims expired job (attempt 2)
	claim2, ok, err := ClaimNext(ctx, st, "worker-2", 5*time.Second)
	if err != nil || !ok || claim2.AttemptCount != 2 {
		t.Fatalf("claim 2 failed: %+v err=%v", claim2, err)
	}

	// Stale Worker 1 attempts to overwrite completion or fail the job -> must be fenced with ErrLeaseLost!
	err = st.CompleteAnalysis(ctx, run.ID, "worker-1", graph.BuildOutput{FilesCount: 99})
	if !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("expected stale worker CompleteAnalysis to return ErrLeaseLost, got %v", err)
	}
	err = st.FailAnalysis(ctx, run.ID, "worker-1", "STALE", "stale failure")
	if !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("expected stale worker FailAnalysis to return ErrLeaseLost, got %v", err)
	}

	// Legitimate Worker 2 completes the analysis
	if err := st.CompleteAnalysis(ctx, run.ID, "worker-2", graph.BuildOutput{FilesCount: 1}); err != nil {
		t.Fatalf("expected worker-2 CompleteAnalysis to succeed, got %v", err)
	}

	finalRun, _ := st.GetAnalysis(ctx, run.ID)
	if finalRun.Status != domain.StatusCompleted || finalRun.FilesCount != 1 {
		t.Fatalf("unexpected final run state: %+v", finalRun)
	}
}

func TestWorker_HeartbeatAndGracefulShutdown(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	snap := githubclient.RepositorySnapshot{
		GitHubID:      303,
		Owner:         "pallets",
		Name:          "click",
		DefaultBranch: "main",
		CommitSHA:     "9999999999abcdef0123456789abcdef01234567",
		HTMLURL:       "https://github.com/pallets/click",
	}

	run, _, err := st.CreateOrGetAnalysis(ctx, snap, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	// Lease is 60ms, heartbeat is 15ms, download takes 90ms (longer than initial lease!)
	// Without heartbeat, the lease would expire before 90ms; with heartbeat, it stays alive and completes.
	gh := &slowZipGHClient{
		delay:   90 * time.Millisecond,
		zipData: makeMinimalZip(t),
	}
	proc := NewProcessor(st, gh, analyzer.NewPythonAnalyzer(), ProcessorOptions{
		LeaseDuration:     60 * time.Millisecond,
		HeartbeatInterval: 15 * time.Millisecond,
		AnalysisTimeout:   5 * time.Second,
	})
	worker := NewWorker(st, proc, 1, 10*time.Millisecond)

	runCtx, cancel := context.WithCancel(ctx)
	var done int32
	go func() {
		_ = worker.Run(runCtx)
		atomic.StoreInt32(&done, 1)
	}()

	// Wait 30ms so the worker claims the job and is mid-flight, then trigger graceful shutdown
	time.Sleep(30 * time.Millisecond)
	cancel()

	// Wait for worker.Run to drain in-flight job and exit
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&done) == 0 && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	if atomic.LoadInt32(&done) == 0 {
		t.Fatal("worker did not shut down gracefully in time")
	}

	completedRun, err := st.GetAnalysis(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completedRun.Status != domain.StatusCompleted {
		t.Fatalf("expected in-flight job to complete during graceful shutdown, got status=%s", completedRun.Status)
	}
}

type mockPRGHClient struct {
	prSnap   domain.PullRequestSnapshot
	prFiles  []domain.PullRequestFile
	zipBytes []byte
}

func (m *mockPRGHClient) Resolve(_ context.Context, _, _ string) (githubclient.RepositorySnapshot, error) {
	return githubclient.RepositorySnapshot{
		GitHubID:      m.prSnap.Repository.GitHubRepositoryID,
		Owner:         m.prSnap.Repository.Owner,
		Name:          m.prSnap.Repository.Name,
		DefaultBranch: m.prSnap.Repository.DefaultBranch,
		CommitSHA:     m.prSnap.HeadSHA,
		HTMLURL:       m.prSnap.Repository.HTMLURL,
	}, nil
}

func (m *mockPRGHClient) ResolveCommit(_ context.Context, _, _, commitSHA string) (githubclient.RepositorySnapshot, error) {
	return githubclient.RepositorySnapshot{
		GitHubID:      m.prSnap.Repository.GitHubRepositoryID,
		Owner:         m.prSnap.Repository.Owner,
		Name:          m.prSnap.Repository.Name,
		DefaultBranch: m.prSnap.Repository.DefaultBranch,
		CommitSHA:     commitSHA,
		HTMLURL:       m.prSnap.Repository.HTMLURL,
	}, nil
}

func (m *mockPRGHClient) GetPullRequest(_ context.Context, _, _ string, _ int) (domain.PullRequestSnapshot, error) {
	return m.prSnap, nil
}

func (m *mockPRGHClient) GetPullRequestFiles(_ context.Context, _, _ string, _ int, _ int) ([]domain.PullRequestFile, error) {
	return m.prFiles, nil
}

func (m *mockPRGHClient) DownloadArchive(_ context.Context, _ githubclient.RepositorySnapshot) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.zipBytes)), nil
}

func TestWorker_PRAnalysis(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	// Prepare multi-file zip: app/main.py imports app/utils.py
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w1, _ := zw.Create("repo-head/app/utils.py")
	_, _ = io.WriteString(w1, "def helper():\n    return 42\n")
	w2, _ := zw.Create("repo-head/app/main.py")
	_, _ = io.WriteString(w2, "from app.utils import helper\ndef run():\n    return helper()\n")
	_ = zw.Close()

	prSnap := domain.PullRequestSnapshot{
		Repository: domain.Repository{
			GitHubRepositoryID: 303,
			Owner:              "org",
			Name:               "repo",
			DefaultBranch:      "main",
			HTMLURL:            "https://github.com/org/repo",
		},
		PullNumber: 10,
		Title:      "Update helper",
		State:      "open",
		BaseSHA:    "0000000000111111111122222222223333333333",
		HeadSHA:    "1111111111222222222233333333334444444444",
		BaseRef:    "main",
		HeadRef:    "update-helper",
	}

	prFiles := []domain.PullRequestFile{
		{
			Path:       "app/utils.py",
			Status:     domain.PRFileModified,
			Additions:  2,
			Deletions:  1,
			Changes:    3,
			IsPython:   true,
			IsAnalyzed: true,
		},
	}

	gh := &mockPRGHClient{
		prSnap:   prSnap,
		prFiles:  prFiles,
		zipBytes: buf.Bytes(),
	}

	proc := NewProcessor(st, gh, analyzer.NewPythonAnalyzer(), ProcessorOptions{
		AnalysisTimeout: 5 * time.Second,
	})
	worker := NewWorker(st, proc, 1, 10*time.Millisecond)

	prAnalysis, cached, err := st.CreateOrGetPRAnalysis(ctx, prSnap, "v2.0.0")
	if err != nil || cached {
		t.Fatalf("CreateOrGetPRAnalysis error: cached=%v err=%v", cached, err)
	}

	claimed, err := worker.StepOnce(ctx)
	if err != nil || !claimed {
		t.Fatalf("StepOnce PR failed: claimed=%v err=%v", claimed, err)
	}

	completedPR, err := st.GetPRAnalysis(ctx, prAnalysis.ID)
	if err != nil {
		t.Fatalf("GetPRAnalysis error: %v", err)
	}
	if completedPR.Status != domain.StatusCompleted {
		t.Fatalf("expected PR completed, got %s", completedPR.Status)
	}
	if completedPR.TotalAffectedCount != 1 || completedPR.DirectImpactCount != 1 {
		t.Fatalf("expected 1 direct affected file (app/main.py), got total=%d direct=%d", completedPR.TotalAffectedCount, completedPR.DirectImpactCount)
	}

	impactPR, files, err := st.GetPRAnalysisImpact(ctx, prAnalysis.ID)
	if err != nil {
		t.Fatalf("GetPRAnalysisImpact error: %v", err)
	}
	if impactPR.ID != prAnalysis.ID || len(files) != 1 {
		t.Fatalf("unexpected impact: files=%d", len(files))
	}
}
