package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

func TestMemoryStore_PRAndEvidenceOperations(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore()

	// 1. Create or get initial repository snapshot
	repoSnap := githubclient.RepositorySnapshot{
		Owner:         "pallets",
		Name:          "itsdangerous",
		HTMLURL:       "https://github.com/pallets/itsdangerous",
		DefaultBranch: "main",
		CommitSHA:     "1234567890abcdef1234567890abcdef12345678",
	}
	_, _, err := s.CreateOrGetAnalysis(ctx, repoSnap, "v1.0.0")
	if err != nil {
		t.Fatalf("CreateOrGetAnalysis failed: %v", err)
	}

	// 2. Create PR Analysis
	prSnap := domain.PullRequestSnapshot{
		Repository: domain.Repository{
			Owner:   "pallets",
			Name:    "itsdangerous",
			HTMLURL: "https://github.com/pallets/itsdangerous",
		},
		PullNumber: 352,
		Title:      "Update signer verification",
		BaseSHA:    "abcdef1234567890abcdef1234567890abcdef12",
		HeadSHA:    "1234567890abcdef1234567890abcdef12345678",
		BaseRef:    "main",
		HeadRef:    "feature-branch",
	}

	pr, cached, err := s.CreateOrGetPRAnalysis(ctx, prSnap, "v1.0.0")
	if err != nil {
		t.Fatalf("CreateOrGetPRAnalysis failed: %v", err)
	}
	if cached {
		t.Errorf("expected cached=false for new run")
	}
	if pr.Status != domain.StatusQueued {
		t.Errorf("expected status queued, got %s", pr.Status)
	}

	// 3. Test Deduplication (still queued/running -> returns same ID, cached=false)
	pr2, cached2, err := s.CreateOrGetPRAnalysis(ctx, prSnap, "v1.0.0")
	if err != nil {
		t.Fatalf("second CreateOrGetPRAnalysis failed: %v", err)
	}
	if cached2 {
		t.Errorf("expected cached=false for in-progress run")
	}
	if pr2.ID != pr.ID {
		t.Errorf("expected duplicate to return same ID, got %s vs %s", pr2.ID, pr.ID)
	}

	// 4. Test ClaimNextPRAnalysis
	claimed, ok, err := s.ClaimNextPRAnalysis(ctx, "worker-1", 5*time.Second)
	if err != nil {
		t.Fatalf("ClaimNextPRAnalysis failed: %v", err)
	}
	if !ok || claimed == nil {
		t.Fatalf("expected claimed PR run, got ok=%v, claimed=%v", ok, claimed)
	}
	if claimed.ID != pr.ID {
		t.Errorf("expected claimed ID %s, got %s", pr.ID, claimed.ID)
	}
	if claimed.Status != domain.StatusRunning {
		t.Errorf("expected status running, got %s", claimed.Status)
	}

	// 5. Test ExtendPRAnalysisLease
	err = s.ExtendPRAnalysisLease(ctx, pr.ID, "worker-1", 10*time.Second)
	if err != nil {
		t.Fatalf("ExtendPRAnalysisLease failed: %v", err)
	}

	// 6. Test CompletePRAnalysis
	headAnalysisID := "11111111-2222-3333-4444-555555555555"
	files := []domain.PullRequestFile{
		{
			Path:       "src/itsdangerous/signer.py",
			Status:     domain.PRFileModified,
			Additions:  10,
			Deletions:  2,
			Changes:    12,
			IsPython:   true,
			IsAnalyzed: true,
		},
		{
			Path:             "README.md",
			Status:           domain.PRFileModified,
			Additions:        1,
			Deletions:        0,
			Changes:          1,
			IsPython:         false,
			IsAnalyzed:       false,
			UnanalyzedReason: "non_python_file",
		},
	}
	summary := domain.PRImpactSummary{
		ChangedFilesCount:    2,
		DirectImpactCount:    1,
		IndirectImpactCount:  1,
		TotalAffectedCount:   2,
		UnanalyzedFilesCount: 1,
		AnalysisCompleteness: "complete",
	}

	err = s.CompletePRAnalysis(ctx, pr.ID, "worker-1", headAnalysisID, files, summary)
	if err != nil {
		t.Fatalf("CompletePRAnalysis failed: %v", err)
	}

	// 7. Verify Completed State & Impact query
	completedPR, prFiles, err := s.GetPRAnalysisImpact(ctx, pr.ID)
	if err != nil {
		t.Fatalf("GetPRAnalysisImpact failed: %v", err)
	}
	if completedPR.Status != domain.StatusCompleted {
		t.Errorf("expected status completed, got %s", completedPR.Status)
	}
	if len(prFiles) != 2 {
		t.Errorf("expected 2 files, got %d", len(prFiles))
	}
	// 8. Test Cache Hit after completion
	pr3, cached3, err := s.CreateOrGetPRAnalysis(ctx, prSnap, "v1.0.0")
	if err != nil {
		t.Fatalf("third CreateOrGetPRAnalysis failed: %v", err)
	}
	if !cached3 {
		t.Errorf("expected cached=true for completed run")
	}
	if pr3.ID != pr.ID {
		t.Errorf("expected same ID, got %s vs %s", pr3.ID, pr.ID)
	}
}
