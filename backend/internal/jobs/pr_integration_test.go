package jobs_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/jobs"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

type mockComprehensiveGHClient struct {
	prSnapFunc        func(ctx context.Context, owner, repo string, pullNumber int) (domain.PullRequestSnapshot, error)
	prFilesFunc       func(ctx context.Context, owner, repo string, pullNumber int, maxFiles int) ([]domain.PullRequestFile, error)
	downloadFunc      func(ctx context.Context, snap githubclient.RepositorySnapshot) (io.ReadCloser, error)
	downloadCallCount int
}

func (m *mockComprehensiveGHClient) Resolve(_ context.Context, _, _ string) (githubclient.RepositorySnapshot, error) {
	return githubclient.RepositorySnapshot{}, nil
}

func (m *mockComprehensiveGHClient) ResolveCommit(_ context.Context, _, _, _ string) (githubclient.RepositorySnapshot, error) {
	return githubclient.RepositorySnapshot{}, nil
}

func (m *mockComprehensiveGHClient) GetPullRequest(ctx context.Context, owner, repo string, pullNumber int) (domain.PullRequestSnapshot, error) {
	if m.prSnapFunc != nil {
		return m.prSnapFunc(ctx, owner, repo, pullNumber)
	}
	return domain.PullRequestSnapshot{}, nil
}

func (m *mockComprehensiveGHClient) GetPullRequestFiles(ctx context.Context, owner, repo string, pullNumber int, maxFiles int) ([]domain.PullRequestFile, error) {
	if m.prFilesFunc != nil {
		return m.prFilesFunc(ctx, owner, repo, pullNumber, maxFiles)
	}
	return nil, nil
}

func (m *mockComprehensiveGHClient) DownloadArchive(ctx context.Context, snap githubclient.RepositorySnapshot) (io.ReadCloser, error) {
	m.downloadCallCount++
	if m.downloadFunc != nil {
		return m.downloadFunc(ctx, snap)
	}
	return nil, nil
}

func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create("repo-head/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPRAnalysis_Fork_Deleted_Renamed_AndUpdateHandling(t *testing.T) {
	ctx := context.Background()

	// 1. Setup repository structure for HEAD commit:
	// - app/core.py (unchanged)
	// - app/new_service.py (renamed from app/old_service.py; imports app.core)
	// - app/client.py (imports app.new_service; Depth 1 dependent)
	// - app/main.py (imports app.client; Depth 2 dependent)
	zipBytes := createTestZip(t, map[string]string{
		"app/core.py":        "class Core:\n    pass\n",
		"app/new_service.py": "from app.core import Core\nclass Service:\n    pass\n",
		"app/client.py":      "from app.new_service import Service\nclass Client:\n    pass\n",
		"app/main.py":        "from app.client import Client\ndef run():\n    return Client()\n",
	})

	baseSHA := "0000000000111111111122222222223333333333"
	headSHA := "9999999999888888888877777777776666666666"

	// Mock Fork PR snapshot: Fork repo user "fork-dev" targeting "upstream-org/repo"
	prSnap := domain.PullRequestSnapshot{
		Repository: domain.Repository{
			GitHubRepositoryID: 8888,
			Owner:              "upstream-org",
			Name:               "repo",
			DefaultBranch:      "main",
			HTMLURL:            "https://github.com/upstream-org/repo",
		},
		PullNumber: 42,
		Title:      "Refactor services from fork",
		State:      "open",
		BaseSHA:    baseSHA,
		HeadSHA:    headSHA,
		BaseRef:    "main",
		HeadRef:    "fork-dev:feature-refactor",
	}

	prFiles := []domain.PullRequestFile{
		// Renamed python file: app/old_service.py -> app/new_service.py
		{
			Path:         "app/new_service.py",
			PreviousPath: "app/old_service.py",
			Status:       domain.PRFileRenamed,
			Additions:    5,
			Deletions:    2,
			Changes:      7,
			IsPython:     true,
			IsAnalyzed:   true,
		},
		// Removed python file: app/deprecated.py (does not exist in HEAD)
		{
			Path:             "app/deprecated.py",
			Status:           domain.PRFileRemoved,
			Additions:        0,
			Deletions:        50,
			Changes:          50,
			IsPython:         true,
			IsAnalyzed:       false,
			UnanalyzedReason: "file_removed_in_pr",
		},
		// Non-python file: README.md
		{
			Path:             "README.md",
			Status:           domain.PRFileModified,
			Additions:        10,
			Deletions:        0,
			Changes:          10,
			IsPython:         false,
			IsAnalyzed:       false,
			UnanalyzedReason: "non_python_file",
		},
	}

	gh := &mockComprehensiveGHClient{
		prSnapFunc: func(_ context.Context, _, _ string, _ int) (domain.PullRequestSnapshot, error) {
			return prSnap, nil
		},
		prFilesFunc: func(_ context.Context, _, _ string, _ int, _ int) ([]domain.PullRequestFile, error) {
			return prFiles, nil
		},
		downloadFunc: func(_ context.Context, _ githubclient.RepositorySnapshot) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(zipBytes)), nil
		},
	}

	st := store.NewMemoryStore()
	proc := jobs.NewProcessor(st, gh, analyzer.NewPythonAnalyzer(), jobs.ProcessorOptions{
		AnalysisTimeout: 5 * time.Second,
	})
	worker := jobs.NewWorker(st, proc, 1, 10*time.Millisecond)

	// Step 1: Create and Process PR analysis run
	prRun, cached, err := st.CreateOrGetPRAnalysis(ctx, prSnap, "v1.0.0")
	if err != nil || cached {
		t.Fatalf("CreateOrGetPRAnalysis failed: cached=%v err=%v", cached, err)
	}

	claimed, err := worker.StepOnce(ctx)
	if err != nil || !claimed {
		t.Fatalf("worker.StepOnce failed: claimed=%v err=%v", claimed, err)
	}

	// Verify PR analysis results
	completedPR, impactFiles, err := st.GetPRAnalysisImpact(ctx, prRun.ID)
	if err != nil {
		t.Fatalf("GetPRAnalysisImpact failed: %v", err)
	}

	if completedPR.Status != domain.StatusCompleted {
		t.Fatalf("expected completed status, got %s", completedPR.Status)
	}

	// Verify Fork metadata retained
	if completedPR.HeadRef != "fork-dev:feature-refactor" {
		t.Errorf("expected Fork head ref 'fork-dev:feature-refactor', got %s", completedPR.HeadRef)
	}

	// Verify Impact Summary
	// Changed files = 3 (1 renamed, 1 removed, 1 non-python)
	// Direct impact (Depth 1) = app/client.py (imports app.new_service)
	// Indirect impact (Depth 2) = app/main.py (imports app.client)
	// Total affected = 2 unique files
	// Unanalyzed = 2 (app/deprecated.py removed, README.md non-python)
	if completedPR.ChangedFilesCount != 3 {
		t.Errorf("expected 3 changed files, got %d", completedPR.ChangedFilesCount)
	}
	if completedPR.DirectImpactCount != 1 {
		t.Errorf("expected 1 direct impact, got %d", completedPR.DirectImpactCount)
	}
	if completedPR.IndirectImpactCount != 1 {
		t.Errorf("expected 1 indirect impact, got %d", completedPR.IndirectImpactCount)
	}
	if completedPR.TotalAffectedCount != 2 {
		t.Errorf("expected 2 total affected, got %d", completedPR.TotalAffectedCount)
	}
	if completedPR.UnanalyzedFilesCount != 2 {
		t.Errorf("expected 2 unanalyzed files, got %d", completedPR.UnanalyzedFilesCount)
	}

	// Verify files list details
	if len(impactFiles) != 3 {
		t.Fatalf("expected 3 files in impact, got %d", len(impactFiles))
	}
	for _, f := range impactFiles {
		if f.Path == "app/new_service.py" {
			if f.Status != domain.PRFileRenamed || f.PreviousPath != "app/old_service.py" || !f.IsAnalyzed {
				t.Errorf("unexpected renamed file state: %+v", f)
			}
		}
		if f.Path == "app/deprecated.py" {
			if f.Status != domain.PRFileRemoved || f.IsAnalyzed || f.UnanalyzedReason != "file_removed_in_pr" {
				t.Errorf("unexpected removed file state: %+v", f)
			}
		}
	}

	// Step 2: Verify Existing Cache Interoperability
	// If a new PR arrives pointing to the SAME head commit (or re-analyzing),
	// it should reuse the existing HEAD analysis run without re-downloading the archive!
	initialDownloadCount := gh.downloadCallCount
	prSnap2 := prSnap
	prSnap2.PullNumber = 43
	prSnap2.Title = "Duplicate PR pointing to same head SHA"

	prRun2, _, err := st.CreateOrGetPRAnalysis(ctx, prSnap2, "v1.0.0")
	if err != nil {
		t.Fatalf("CreateOrGetPRAnalysis for pr2 failed: %v", err)
	}

	claimed2, err := worker.StepOnce(ctx)
	if err != nil || !claimed2 {
		t.Fatalf("worker.StepOnce for pr2 failed: %v", err)
	}

	completedPR2, err := st.GetPRAnalysis(ctx, prRun2.ID)
	if err != nil || completedPR2.Status != domain.StatusCompleted {
		t.Fatalf("expected pr2 completed, got %+v err=%v", completedPR2, err)
	}

	if completedPR2.HeadAnalysisID == nil || completedPR.HeadAnalysisID == nil || *completedPR2.HeadAnalysisID != *completedPR.HeadAnalysisID {
		t.Errorf("expected pr2 to reuse same head analysis run ID: %v vs %v", completedPR2.HeadAnalysisID, completedPR.HeadAnalysisID)
	}

	// Verify downloadArchive was NOT called again for pr2 because HEAD was already cached!
	if gh.downloadCallCount != initialDownloadCount {
		t.Errorf("expected downloadArchive count to remain %d, got %d (cache was not reused!)", initialDownloadCount, gh.downloadCallCount)
	}

	// Step 3: Test PR Mid-flight Update Detection
	// If the PR's HEAD SHA changes while fetching files, the processor must reject the run
	// with ErrCodePRHeadSHAChanged
	prSnap3 := prSnap
	prSnap3.PullNumber = 44
	prSnap3.HeadSHA = "3333333333333333333333333333333333333333"

	// Mock GetPullRequest returning updated SHA different from prSnap3.HeadSHA
	gh.prSnapFunc = func(_ context.Context, _, _ string, _ int) (domain.PullRequestSnapshot, error) {
		updated := prSnap3
		updated.HeadSHA = "4444444444444444444444444444444444444444" // SHA changed!
		return updated, nil
	}

	prRun3, _, err := st.CreateOrGetPRAnalysis(ctx, prSnap3, "v1.0.0")
	if err != nil {
		t.Fatalf("CreateOrGetPRAnalysis for pr3 failed: %v", err)
	}

	claimed3, err := worker.StepOnce(ctx)
	if err == nil {
		t.Fatalf("expected error on mid-flight SHA change, got nil")
	}
	if !claimed3 {
		t.Fatalf("expected job to be claimed")
	}

	failedPR3, err := st.GetPRAnalysis(ctx, prRun3.ID)
	if err != nil {
		t.Fatalf("GetPRAnalysis failed: %v", err)
	}
	if failedPR3.Status != domain.StatusFailed {
		t.Errorf("expected failed status on SHA change, got %s", failedPR3.Status)
	}
	if failedPR3.ErrorCode == nil || *failedPR3.ErrorCode != string(domain.ErrCodePRHeadSHAChanged) {
		t.Errorf("expected error code %s, got %v", domain.ErrCodePRHeadSHAChanged, failedPR3.ErrorCode)
	}
}
