package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/jobs"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

type mockGitHubClient struct {
	snapshot    githubclient.RepositorySnapshot
	resolveErr  error
	archiveZip  []byte
	archiveErr  error
	archiveFunc func(snap githubclient.RepositorySnapshot) ([]byte, error)
}

func (m *mockGitHubClient) Resolve(_ context.Context, _, _ string) (githubclient.RepositorySnapshot, error) {
	if m.resolveErr != nil {
		return githubclient.RepositorySnapshot{}, m.resolveErr
	}
	return m.snapshot, nil
}

func (m *mockGitHubClient) ResolveCommit(_ context.Context, _, _, commitSHA string) (githubclient.RepositorySnapshot, error) {
	if m.resolveErr != nil {
		return githubclient.RepositorySnapshot{}, m.resolveErr
	}
	s := m.snapshot
	s.CommitSHA = commitSHA
	return s, nil
}

func (m *mockGitHubClient) GetPullRequest(_ context.Context, _, _ string, pullNumber int) (domain.PullRequestSnapshot, error) {
	if m.resolveErr != nil {
		return domain.PullRequestSnapshot{}, m.resolveErr
	}
	return domain.PullRequestSnapshot{
		Repository: domain.Repository{
			GitHubRepositoryID: m.snapshot.GitHubID,
			Owner:              m.snapshot.Owner,
			Name:               m.snapshot.Name,
			DefaultBranch:      m.snapshot.DefaultBranch,
			HTMLURL:            m.snapshot.HTMLURL,
		},
		PullNumber: pullNumber,
		Title:      "PR Title",
		State:      "open",
		BaseSHA:    "0000000000111111111122222222223333333333",
		HeadSHA:    m.snapshot.CommitSHA,
		BaseRef:    "main",
		HeadRef:    "feature",
	}, nil
}

func (m *mockGitHubClient) GetPullRequestFiles(_ context.Context, _, _ string, _ int, _ int) ([]domain.PullRequestFile, error) {
	if m.resolveErr != nil {
		return nil, m.resolveErr
	}
	return []domain.PullRequestFile{
		{
			Path:       "app/models.py",
			Status:     domain.PRFileModified,
			Additions:  1,
			Deletions:  0,
			Changes:    1,
			IsPython:   true,
			IsAnalyzed: true,
		},
	}, nil
}

func (m *mockGitHubClient) DownloadArchive(_ context.Context, snap githubclient.RepositorySnapshot) (io.ReadCloser, error) {
	if m.archiveErr != nil {
		return nil, m.archiveErr
	}
	if m.archiveFunc != nil {
		data, err := m.archiveFunc(snap)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return io.NopCloser(bytes.NewReader(m.archiveZip)), nil
}

func buildFixtureZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := map[string]string{
		"owner-repo-deadbeef/app/main.py":    "import requests\nimport app.service\nfrom app.models import User\n\ndef main():\n    return 1\n",
		"owner-repo-deadbeef/app/service.py": "from .models import User\nimport app.models\n\nclass Service:\n    pass\n",
		"owner-repo-deadbeef/app/models.py":  "class User:\n    pass\n",
	}
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestAPIAndWorkerEndToEndFlow(t *testing.T) {
	const sha = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	memStore := store.NewMemoryStore()
	mockGH := &mockGitHubClient{
		snapshot: githubclient.RepositorySnapshot{
			GitHubID:      42,
			Owner:         "owner",
			Name:          "repo",
			DefaultBranch: "main",
			CommitSHA:     sha,
			HTMLURL:       "https://github.com/owner/repo",
		},
		archiveZip: buildFixtureZip(t),
	}

	h := NewHandler(memStore, mockGH, "v1.0.0")
	router := NewRouter(h, []string{"http://localhost:3000"})

	// 1. GET /healthz
	{
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("healthz expected 200, got %d", rec.Code)
		}
	}

	// 2. GET /api/v1/demo/graph
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/demo/graph", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("demo graph expected 200, got %d", rec.Code)
		}
		var demoResp GraphResponse
		if err := json.NewDecoder(rec.Body).Decode(&demoResp); err != nil {
			t.Fatalf("decode demo graph: %v", err)
		}
		if !demoResp.Demo || len(demoResp.Nodes) == 0 || len(demoResp.Edges) == 0 {
			t.Fatalf("unexpected demo response: %+v", demoResp)
		}
	}

	// 3. POST /api/v1/analyses with invalid URL -> 400
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://evil.com/owner/repo"}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on invalid URL, got %d", rec.Code)
		}
	}

	// 4. POST /api/v1/analyses -> 202 queued
	var analysisID string
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://github.com/owner/repo"}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
		}
		var createResp CreateAnalysisResponse
		if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		if createResp.Cached || createResp.Status != domain.StatusQueued || createResp.AnalysisID == "" {
			t.Fatalf("unexpected create response: %+v", createResp)
		}
		analysisID = createResp.AnalysisID
	}

	// 5. Concurrent duplicate POST before completion -> 202 with same analysis_id
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://github.com/owner/repo.git"}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 on duplicate in-flight request, got %d", rec.Code)
		}
		var dupResp CreateAnalysisResponse
		_ = json.NewDecoder(rec.Body).Decode(&dupResp)
		if dupResp.AnalysisID != analysisID || dupResp.Cached {
			t.Fatalf("expected convergence to same analysis_id %s, got %+v", analysisID, dupResp)
		}
	}

	// 6. GET /api/v1/analyses/{id}/graph while queued -> 409 ANALYSIS_NOT_READY
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID+"/graph", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 ANALYSIS_NOT_READY, got %d", rec.Code)
		}
		var errResp ErrorResponse
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp.Error.Code != string(domain.ErrCodeAnalysisNotReady) {
			t.Fatalf("expected ANALYSIS_NOT_READY error code, got %+v", errResp)
		}
	}

	// 7. Run worker StepOnce to process the job
	az := analyzer.NewPythonAnalyzer()
	proc := jobs.NewProcessor(memStore, mockGH, az, jobs.ProcessorOptions{})
	worker := jobs.NewWorker(memStore, proc, 1, 0)

	claimed, err := worker.StepOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("worker StepOnce failed: claimed=%v err=%v", claimed, err)
	}

	// 8. GET /api/v1/analyses/{id} -> completed
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 status check, got %d", rec.Code)
		}
		var statusResp AnalysisStatusResponse
		if err := json.NewDecoder(rec.Body).Decode(&statusResp); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if statusResp.Status != domain.StatusCompleted || statusResp.CommitSHA != sha || statusResp.CompletedAt == nil {
			t.Fatalf("unexpected status response: %+v", statusResp)
		}
	}

	// 9. GET /api/v1/analyses/{id}/graph -> 200 with full graph
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID+"/graph", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 graph, got %d: %s", rec.Code, rec.Body.String())
		}
		var graphResp GraphResponse
		if err := json.NewDecoder(rec.Body).Decode(&graphResp); err != nil {
			t.Fatalf("decode graph: %v", err)
		}
		if graphResp.Summary.Files != 3 || graphResp.Summary.InternalEdges != 3 || graphResp.Summary.ExternalImports != 1 {
			t.Fatalf("unexpected graph summary: %+v", graphResp.Summary)
		}
		if len(graphResp.Nodes) != 3 || len(graphResp.Edges) != 3 {
			t.Fatalf("unexpected nodes/edges count: nodes=%d edges=%d", len(graphResp.Nodes), len(graphResp.Edges))
		}
		if len(graphResp.RecommendedFileIDs) == 0 {
			t.Fatalf("expected recommended_file_ids to be populated")
		}
	}

	// 10. Re-request POST /api/v1/analyses -> 200 OK cached=true
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://github.com/owner/repo"}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on cache hit, got %d", rec.Code)
		}
		var cacheResp CreateAnalysisResponse
		_ = json.NewDecoder(rec.Body).Decode(&cacheResp)
		if !cacheResp.Cached || cacheResp.Status != domain.StatusCompleted || cacheResp.AnalysisID != analysisID {
			t.Fatalf("unexpected cache response: %+v", cacheResp)
		}
	}
}

func TestRateLimit_TrustedProxy_QueueLimit_And_LogRedaction(t *testing.T) {
	const sha1 = "1111111111111111111111111111111111111111"
	const sha2 = "2222222222222222222222222222222222222222"

	t.Run("untrusted proxy cannot spoof X-Forwarded-For, trusted proxy extracts real client IP", func(t *testing.T) {
		untrustedExt := NewIPExtractor(nil)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", nil)
		req.RemoteAddr = "203.0.113.5:54321"
		req.Header.Set("X-Forwarded-For", "198.51.100.99")
		if got := untrustedExt.ClientIP(req); got != "203.0.113.5" {
			t.Fatalf("expected untrusted peer IP 203.0.113.5, got %s", got)
		}

		trustedExt := NewIPExtractor([]string{"10.0.0.0/8", "127.0.0.1"})
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", nil)
		req2.RemoteAddr = "10.1.2.3:12345"
		req2.Header.Set("X-Forwarded-For", "198.51.100.77, 10.0.0.2")
		if got := trustedExt.ClientIP(req2); got != "198.51.100.77" {
			t.Fatalf("expected client IP 198.51.100.77 through trusted proxy, got %s", got)
		}
	})

	t.Run("IP rate limiter returns 429 with Retry-After", func(t *testing.T) {
		memStore := store.NewMemoryStore()
		mockGH := &mockGitHubClient{
			snapshot: githubclient.RepositorySnapshot{
				GitHubID:      50,
				Owner:         "owner",
				Name:          "repo",
				DefaultBranch: "main",
				CommitSHA:     sha1,
				HTMLURL:       "https://github.com/owner/repo",
			},
		}
		limiter := NewIPRateLimiter(2, 0)
		h := NewHandler(memStore, mockGH, "v1.0.0", HandlerOptions{
			RateLimiter: limiter,
		})
		router := NewRouter(h, []string{"*"})

		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://github.com/owner/repo"}`))
			req.RemoteAddr = "192.0.2.10:4000"
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("request %d expected 202, got %d", i+1, rec.Code)
			}
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://github.com/owner/repo"}`))
		req.RemoteAddr = "192.0.2.10:4000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 on 3rd request from same IP, got %d", rec.Code)
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Fatal("expected Retry-After header on 429 response")
		}
	})

	t.Run("max queued jobs limit returns 429 QUEUE_LIMIT_EXCEEDED for new jobs", func(t *testing.T) {
		memStore := store.NewMemoryStore()
		memStore.SetMaxQueuedJobs(1)

		mockGH := &mockGitHubClient{
			snapshot: githubclient.RepositorySnapshot{
				GitHubID:      60,
				Owner:         "owner",
				Name:          "repo1",
				DefaultBranch: "main",
				CommitSHA:     sha1,
				HTMLURL:       "https://github.com/owner/repo1",
			},
		}
		h := NewHandler(memStore, mockGH, "v1.0.0")
		router := NewRouter(h, []string{"*"})

		// First job succeeds (queued = 1)
		req1 := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://github.com/owner/repo1"}`))
		rec1 := httptest.NewRecorder()
		router.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusAccepted {
			t.Fatalf("expected 202 for first job, got %d", rec1.Code)
		}

		// Duplicate request for the same job still succeeds (converges to existing run)
		reqDup := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://github.com/owner/repo1"}`))
		recDup := httptest.NewRecorder()
		router.ServeHTTP(recDup, reqDup)
		if recDup.Code != http.StatusAccepted {
			t.Fatalf("expected 202 for duplicate request on existing queued job, got %d", recDup.Code)
		}

		// Second distinct job exceeds maxQueuedJobs=1 -> 429 QUEUE_LIMIT_EXCEEDED
		mockGH.snapshot = githubclient.RepositorySnapshot{
			GitHubID:      61,
			Owner:         "owner",
			Name:          "repo2",
			DefaultBranch: "main",
			CommitSHA:     sha2,
			HTMLURL:       "https://github.com/owner/repo2",
		}
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://github.com/owner/repo2"}`))
		rec2 := httptest.NewRecorder()
		router.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 when queue limit exceeded, got %d: %s", rec2.Code, rec2.Body.String())
		}
	})

	t.Run("structured logger redacts github tokens and authorization secrets", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := NewStructuredLogger(&logBuf)
		logger.Info("auth check",
			"github_token", "ghp_1234567890abcdefSECRET",
			"detail", "header Bearer ghp_9999999999abcdefSECRET and github_pat_11ABCDEF",
		)
		out := logBuf.String()
		if strings.Contains(out, "ghp_") || strings.Contains(out, "github_pat_") || strings.Contains(out, "SECRET") {
			t.Fatalf("secret leaked into structured log output: %s", out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Fatalf("expected [REDACTED] placeholder in log output: %s", out)
		}
	})

	t.Run("GET demo graph and impact endpoint return guided_tour, architecture, and BFS impact", func(t *testing.T) {
		memStore := store.NewMemoryStore()
		h := NewHandler(memStore, &mockGitHubClient{}, "v1.0.0")
		router := NewRouter(h, []string{"*"})

		demoReq := httptest.NewRequest(http.MethodGet, "/api/v1/demo/graph", nil)
		demoRec := httptest.NewRecorder()
		router.ServeHTTP(demoRec, demoReq)
		if demoRec.Code != http.StatusOK {
			t.Fatalf("expected 200 for demo graph, got %d", demoRec.Code)
		}
		var demoGraph GraphResponse
		if err := json.NewDecoder(demoRec.Body).Decode(&demoGraph); err != nil {
			t.Fatalf("decode demo graph: %v", err)
		}
		if len(demoGraph.GuidedTour) == 0 {
			t.Fatal("expected guided_tour in demo graph response")
		}
		if demoGraph.GuidedTour[0].Path != "app/main.py" {
			t.Fatalf("expected guided_tour step 1 to be app/main.py, got %s", demoGraph.GuidedTour[0].Path)
		}
		if demoGraph.Architecture == nil || len(demoGraph.Architecture.Nodes) == 0 {
			t.Fatal("expected architecture nodes in demo graph response")
		}

		// GET /api/v1/analyses/demo-python-web-starter/impact?target=app/db.py&max_depth=2
		impReq := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/demo-python-web-starter/impact?target=app/db.py&max_depth=2", nil)
		impRec := httptest.NewRecorder()
		router.ServeHTTP(impRec, impReq)
		if impRec.Code != http.StatusOK {
			t.Fatalf("expected 200 for impact endpoint, got %d: %s", impRec.Code, impRec.Body.String())
		}
		var impBody struct {
			TargetPath    string `json:"target_path"`
			TotalAffected int    `json:"total_affected"`
			DirectCount   int    `json:"direct_count"`
			IndirectCount int    `json:"indirect_count"`
		}
		if err := json.NewDecoder(impRec.Body).Decode(&impBody); err != nil {
			t.Fatalf("decode impact body: %v", err)
		}
		if impBody.TargetPath != "app/db.py" || impBody.DirectCount != 2 || impBody.TotalAffected < 3 {
			t.Fatalf("unexpected impact body for app/db.py: %+v", impBody)
		}
	})
}

func buildGoFixtureZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := map[string]string{
		"owner-repo-deadbeef/go.mod": "module github.com/owner/repo\n\ngo 1.22\n",
		"owner-repo-deadbeef/cmd/server/main.go": `package main

import (
	"fmt"
	"github.com/owner/repo/internal/service"
)

func main() {
	fmt.Println(service.Run())
}
`,
		"owner-repo-deadbeef/internal/service/service.go": `package service

import "github.com/owner/repo/internal/store"

type Service struct{}

func (s *Service) Execute() error {
	return nil
}

func Run() string {
	return store.Fetch()
}
`,
		"owner-repo-deadbeef/internal/store/store.go": `package store

type DB struct{}

func Fetch() string {
	return "ok"
}
`,
	}
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestGoAnalysisAndEvidenceFlow(t *testing.T) {
	const sha = "c0ffee11c0ffee11c0ffee11c0ffee11c0ffee11"
	memStore := store.NewMemoryStore()
	mockGH := &mockGitHubClient{
		snapshot: githubclient.RepositorySnapshot{
			GitHubID:      88,
			Owner:         "owner",
			Name:          "repo",
			DefaultBranch: "main",
			CommitSHA:     sha,
			HTMLURL:       "https://github.com/owner/repo",
		},
		archiveZip: buildGoFixtureZip(t),
	}

	h := NewHandler(memStore, mockGH, "v1.0.0")
	router := NewRouter(h, []string{"*"})

	// 1. POST /api/v1/analyses
	var analysisID string
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"https://github.com/owner/repo"}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
		}
		var createResp CreateAnalysisResponse
		if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		analysisID = createResp.AnalysisID
	}

	// 2. Process with Worker using CompositeAnalyzer
	compAnalyzer := analyzer.NewCompositeAnalyzer()
	proc := jobs.NewProcessor(memStore, mockGH, compAnalyzer, jobs.ProcessorOptions{})
	worker := jobs.NewWorker(memStore, proc, 1, 0)
	claimed, err := worker.StepOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("worker failed to claim job: claimed=%v err=%v", claimed, err)
	}

	// 3. GET /api/v1/analyses/{id}
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var status AnalysisStatusResponse
		if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if status.Status != domain.StatusCompleted || status.Language != "go" || status.GraphGranularity != "package" {
			t.Fatalf("unexpected status: %+v", status)
		}
	}

	// 4. GET /api/v1/analyses/{id}/graph
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID+"/graph", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var graphResp GraphResponse
		if err := json.NewDecoder(rec.Body).Decode(&graphResp); err != nil {
			t.Fatalf("decode graph: %v", err)
		}
		if graphResp.Language != "go" || graphResp.GraphGranularity != "package" {
			t.Fatalf("expected go / package, got %s / %s", graphResp.Language, graphResp.GraphGranularity)
		}
		if len(graphResp.Nodes) != 3 {
			t.Fatalf("expected 3 package nodes, got %d", len(graphResp.Nodes))
		}
		// Check that nodes have node_kind = "package" and valid package details
		foundMain := false
		for _, n := range graphResp.Nodes {
			if n.NodeKind != "package" {
				t.Fatalf("expected node_kind 'package', got %s", n.NodeKind)
			}
			if n.PackageName == "main" {
				foundMain = true
				if !n.IsEntrypoint {
					t.Fatalf("expected main package to be entrypoint")
				}
			}
		}
		if !foundMain {
			t.Fatalf("main package not found in graph nodes")
		}
	}

	// 5. GET /api/v1/analyses/{id}/evidence
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID+"/evidence", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for evidence, got %d: %s", rec.Code, rec.Body.String())
		}
		var evResp EvidenceResponse
		if err := json.NewDecoder(rec.Body).Decode(&evResp); err != nil {
			t.Fatalf("decode evidence: %v", err)
		}
		if len(evResp.Imports) < 2 {
			t.Fatalf("expected at least 2 import evidence records, got %d", len(evResp.Imports))
		}
		// Verify source_file, source_package, target_package
		for _, imp := range evResp.Imports {
			if imp.SourcePath == "" || imp.LineNumber == 0 {
				t.Fatalf("invalid import evidence item: %+v", imp)
			}
		}
	}
}

func TestArchitectureDiffFlow(t *testing.T) {
	const baseSHA = "1111111111111111111111111111111111111111"
	const headSHA = "2222222222222222222222222222222222222222"

	baseZip := func() []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		_ = zw.Close()
		zw = zip.NewWriter(&buf)
		w1, _ := zw.Create("repo-base/app/main.py")
		_, _ = io.WriteString(w1, "import app.core\n\ndef main(): pass\n")
		w2, _ := zw.Create("repo-base/app/core.py")
		_, _ = io.WriteString(w2, "def run(): pass\n")
		_ = zw.Close()
		return buf.Bytes()
	}()

	headZip := func() []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w1, _ := zw.Create("repo-head/app/main.py")
		_, _ = io.WriteString(w1, "import app.core\nimport app.metrics\n\ndef main(): pass\n")
		w2, _ := zw.Create("repo-head/app/core.py")
		_, _ = io.WriteString(w2, "def run(): pass\n")
		w3, _ := zw.Create("repo-head/app/metrics.py")
		_, _ = io.WriteString(w3, "def track(): pass\n")
		_ = zw.Close()
		return buf.Bytes()
	}()

	memStore := store.NewMemoryStore()
	mockGH := &mockGitHubClient{
		snapshot: githubclient.RepositorySnapshot{
			GitHubID:      99,
			Owner:         "testorg",
			Name:          "diffrepo",
			DefaultBranch: "main",
			HTMLURL:       "https://github.com/testorg/diffrepo",
		},
		archiveFunc: func(snap githubclient.RepositorySnapshot) ([]byte, error) {
			if snap.CommitSHA == baseSHA {
				return baseZip, nil
			}
			return headZip, nil
		},
	}

	h := NewHandler(memStore, mockGH, "v1.0.0")
	router := NewRouter(h, []string{"*"})

	// 1. POST /api/v1/architecture-diffs
	var diffID string
	{
		body := fmt.Sprintf(`{"repository_url":"https://github.com/testorg/diffrepo","base_sha":"%s","head_sha":"%s"}`, baseSHA, headSHA)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/architecture-diffs", strings.NewReader(body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
		}
		var diffResp CreateArchitectureDiffResponse
		if err := json.NewDecoder(rec.Body).Decode(&diffResp); err != nil {
			t.Fatalf("decode diff response: %v", err)
		}
		diffID = diffResp.DiffID
		if diffID == "" {
			t.Fatal("empty diff id")
		}
	}

	// 2. Process both base and head jobs
	compAnalyzer := analyzer.NewCompositeAnalyzer()
	proc := jobs.NewProcessor(memStore, mockGH, compAnalyzer, jobs.ProcessorOptions{})
	worker := jobs.NewWorker(memStore, proc, 1, 0)

	// Step once for base
	claimed1, err := worker.StepOnce(context.Background())
	if err != nil || !claimed1 {
		t.Fatalf("worker step 1 failed: %v", err)
	}
	// Step once for head
	claimed2, err := worker.StepOnce(context.Background())
	if err != nil || !claimed2 {
		t.Fatalf("worker step 2 failed: %v", err)
	}

	// 3. GET /api/v1/architecture-diffs/{id} -> should transition to completed
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/architecture-diffs/"+diffID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var statusResp ArchitectureDiffStatusResponse
		if err := json.NewDecoder(rec.Body).Decode(&statusResp); err != nil {
			t.Fatalf("decode diff status: %v", err)
		}
		if statusResp.Status != domain.DiffStatusCompleted {
			t.Fatalf("expected diff status completed, got %s", statusResp.Status)
		}
		if statusResp.Summary == nil {
			t.Fatal("expected diff summary")
		}
		if statusResp.Summary.AddedNodesCount != 1 || statusResp.Summary.AddedEdgesCount != 1 {
			t.Fatalf("expected 1 added node (metrics.py) and 1 added edge, got %+v", statusResp.Summary)
		}
	}

	// 4. GET /api/v1/architecture-diffs/{id}/graph
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/architecture-diffs/"+diffID+"/graph", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var graphResp ArchitectureDiffGraphResponse
		if err := json.NewDecoder(rec.Body).Decode(&graphResp); err != nil {
			t.Fatalf("decode diff graph: %v", err)
		}
		if len(graphResp.Nodes) != 3 {
			t.Fatalf("expected 3 diff nodes, got %d", len(graphResp.Nodes))
		}
		// Verify metrics.py is added, main.py is modified, core.py is unchanged
		foundMetricsAdded := false
		foundMainModified := false
		foundCoreUnchanged := false
		for _, n := range graphResp.Nodes {
			if n.Path == "app/metrics.py" && n.ChangeType == domain.NodeChangeAdded {
				foundMetricsAdded = true
			}
			if n.Path == "app/main.py" && n.ChangeType == domain.NodeChangeModified {
				foundMainModified = true
			}
			if n.Path == "app/core.py" && n.ChangeType == domain.NodeChangeUnchanged {
				foundCoreUnchanged = true
			}
		}
		if !foundMetricsAdded || !foundMainModified || !foundCoreUnchanged {
			t.Fatalf("unexpected node changes: metrics=%v main=%v core=%v", foundMetricsAdded, foundMainModified, foundCoreUnchanged)
		}
	}

	// 5. Re-request POST /api/v1/architecture-diffs -> 200 OK cached=true
	{
		body := fmt.Sprintf(`{"repository_url":"https://github.com/testorg/diffrepo","base_sha":"%s","head_sha":"%s"}`, baseSHA, headSHA)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/architecture-diffs", strings.NewReader(body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on cached diff, got %d", rec.Code)
		}
		var cacheResp CreateArchitectureDiffResponse
		_ = json.NewDecoder(rec.Body).Decode(&cacheResp)
		if !cacheResp.Cached || cacheResp.Status != domain.DiffStatusCompleted || cacheResp.DiffID != diffID {
			t.Fatalf("unexpected cached diff response: %+v", cacheResp)
		}
	}
}
