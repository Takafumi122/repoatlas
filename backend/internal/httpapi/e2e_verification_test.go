package httpapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/httpapi"
	"github.com/repoatlas/repoatlas/backend/internal/jobs"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

// TestE2E_FullTenVerificationItems runs the end-to-end verification against real PostgreSQL 16.
func TestE2E_FullTenVerificationItems(t *testing.T) {
	ctx := context.Background()
	var pool *pgxpool.Pool

	if dbURL := os.Getenv("TEST_DATABASE_URL"); dbURL != "" {
		pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
		if p, err := pgxpool.New(pingCtx, dbURL); err == nil {
			if err := p.Ping(pingCtx); err == nil {
				pool = p
			} else {
				p.Close()
			}
		}
		pingCancel()
	}

	if pool == nil {
		epPort := uint32(15434)
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
			t.Fatalf("failed to start embedded PostgreSQL 16: %v", err)
		}
		t.Cleanup(func() {
			_ = epg.Stop()
		})

		epURL := fmt.Sprintf("postgres://repoatlas:repoatlas@127.0.0.1:%d/repoatlas?sslmode=disable", epPort)
		p, err := pgxpool.New(ctx, epURL)
		if err != nil {
			t.Fatalf("connect to embedded PostgreSQL 16: %v", err)
		}
		pool = p
	}
	defer pool.Close()

	// Clean and run all 4 migrations up
	for _, mig := range []string{
		"000004_phase6_arch_diff_and_go.down.sql",
		"000003_pr_and_evidence.down.sql",
		"000002_lease_owner.down.sql",
		"000001_init.down.sql",
		"000001_init.up.sql",
		"000002_lease_owner.up.sql",
		"000003_pr_and_evidence.up.sql",
		"000004_phase6_arch_diff_and_go.up.sql",
	} {
		sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", mig))
		if err != nil {
			t.Fatalf("read migration %s: %v", mig, err)
		}
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("exec migration %s: %v", mig, err)
		}
	}

	st := store.NewPostgresStore(pool)

	// Create mock GitHub Client with in-memory Go & Python repo archives
	baseGoZip := createGoZip(map[string]string{
		"go.mod": "module github.com/testorg/gomicro\ngo 1.22\n",
		"cmd/server/main.go": `package main
import (
	"fmt"
	"github.com/testorg/gomicro/internal/api"
)
func main() { fmt.Println(api.Run()) }
`,
		"internal/api/api.go": `package api
import "github.com/testorg/gomicro/internal/store"
func Run() string { return store.Get() }
`,
		"internal/store/store.go": `package store
func Get() string { return "data" }
`,
	})

	headGoZip := createGoZip(map[string]string{
		"go.mod": "module github.com/testorg/gomicro\ngo 1.22\n",
		"cmd/server/main.go": `package main
import (
	"fmt"
	"github.com/testorg/gomicro/internal/api"
)
func main() { fmt.Println(api.Run()) }
`,
		"internal/api/api.go": `package api
import (
	"github.com/testorg/gomicro/internal/store"
	"github.com/testorg/gomicro/internal/cache"
)
func Run() string { return cache.Get() + store.Get() }
`,
		"internal/cache/cache.go": `package cache
import "github.com/testorg/gomicro/internal/api" // Creates SCC cycle!
func Get() string { return "cached:" }
`,
		"internal/store/store.go": `package store
func Get() string { return "data" }
`,
	})

	const baseSHA = "1111000011110000111100001111000011110001"
	const headSHA = "2222000022220000222200002222000022220002"

	mockGH := &mockFullE2EGHClient{
		archives: map[string][]byte{
			baseSHA: baseGoZip,
			headSHA: headGoZip,
		},
	}

	az := analyzer.NewCompositeAnalyzer()
	proc := jobs.NewProcessor(st, mockGH, az, jobs.ProcessorOptions{
		AnalysisTimeout: 30 * time.Second,
	})
	worker := jobs.NewWorker(st, proc, 1, 50*time.Millisecond)

	h := httpapi.NewHandler(st, mockGH, "v2.0.0")
	router := httpapi.NewRouter(h, []string{"*"})
	srv := httptest.NewServer(router)
	defer srv.Close()

	client := srv.Client()

	// -------------------------------------------------------------------------
	// Item 1: 公開Goリポジトリの解析 (Trigger Analysis)
	// -------------------------------------------------------------------------
	t.Run("Item 1: Analysis Request for Public Go Repository", func(t *testing.T) {
		body := fmt.Sprintf(`{"repository_url":"https://github.com/testorg/gomicro"}`)
		mockGH.currentSHA = baseSHA

		resp, err := client.Post(srv.URL+"/api/v1/analyses", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /analyses failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted, got %d", resp.StatusCode)
		}

		var createResp httpapi.CreateAnalysisResponse
		_ = json.NewDecoder(resp.Body).Decode(&createResp)
		if createResp.AnalysisID == "" || createResp.Status != domain.StatusQueued {
			t.Fatalf("unexpected create response: %+v", createResp)
		}

		// Let worker process the queued job
		processed, err := worker.StepOnce(ctx)
		if err != nil || !processed {
			t.Fatalf("worker failed to process job: processed=%v err=%v", processed, err)
		}

		// Verify status completed
		stResp, err := client.Get(srv.URL + "/api/v1/analyses/" + createResp.AnalysisID)
		if err != nil {
			t.Fatalf("GET /analyses/{id} failed: %v", err)
		}
		defer stResp.Body.Close()
		var statusResp httpapi.AnalysisStatusResponse
		_ = json.NewDecoder(stResp.Body).Decode(&statusResp)
		if statusResp.Status != domain.StatusCompleted {
			t.Fatalf("expected completed status, got %s", statusResp.Status)
		}
		t.Logf("[Item 1 PASS] Go repo analyzed successfully: analysis_id=%s", createResp.AnalysisID)
	})

	// -------------------------------------------------------------------------
	// Item 2: Package Graphの保存と表示 (Retrieve Package Graph)
	// -------------------------------------------------------------------------
	var baseAnalysisID string
	t.Run("Item 2: Package Graph Persistence and Retrieval", func(t *testing.T) {
		// Get analysis ID for baseSHA
		run, err := st.GetAnalysis(ctx, "") // we will query from DB
		_ = run

		var id string
		err = pool.QueryRow(ctx, `SELECT id::text FROM analysis_runs WHERE commit_sha = $1`, baseSHA).Scan(&id)
		if err != nil {
			t.Fatalf("query analysis ID: %v", err)
		}
		baseAnalysisID = id

		gResp, err := client.Get(srv.URL + "/api/v1/analyses/" + baseAnalysisID + "/graph")
		if err != nil {
			t.Fatalf("GET /analyses/{id}/graph failed: %v", err)
		}
		defer gResp.Body.Close()
		if gResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", gResp.StatusCode)
		}

		var graphResp httpapi.GraphResponse
		_ = json.NewDecoder(gResp.Body).Decode(&graphResp)
		if graphResp.Language != "go" || graphResp.GraphGranularity != "package" {
			t.Fatalf("expected language=go granularity=package, got lang=%s gran=%s", graphResp.Language, graphResp.GraphGranularity)
		}
		if len(graphResp.Nodes) != 3 {
			t.Fatalf("expected 3 package nodes, got %d", len(graphResp.Nodes))
		}
		t.Logf("[Item 2 PASS] Package Graph retrieved with %d package nodes and %d edges", len(graphResp.Nodes), len(graphResp.Edges))
	})

	// -------------------------------------------------------------------------
	// Item 3: Guided Code Tour (Recommended viewing order)
	// -------------------------------------------------------------------------
	t.Run("Item 3: Guided Code Tour for Go Packages", func(t *testing.T) {
		gResp, err := client.Get(srv.URL + "/api/v1/analyses/" + baseAnalysisID + "/graph")
		if err != nil {
			t.Fatalf("GET graph: %v", err)
		}
		defer gResp.Body.Close()

		var graphResp httpapi.GraphResponse
		_ = json.NewDecoder(gResp.Body).Decode(&graphResp)
		if len(graphResp.GuidedTour) == 0 {
			t.Fatalf("expected guided_tour steps, got 0")
		}
		if len(graphResp.RecommendedFileIDs) == 0 {
			t.Fatalf("expected recommended_file_ids, got 0")
		}
		t.Logf("[Item 3 PASS] Guided Code Tour generated %d steps. Step 1: %s (%s)",
			len(graphResp.GuidedTour), graphResp.GuidedTour[0].NodeID, graphResp.GuidedTour[0].Reason)
	})

	// -------------------------------------------------------------------------
	// Item 4: Impact Analysis (Package-level BFS Impact)
	// -------------------------------------------------------------------------
	t.Run("Item 4: Package-level Impact Analysis", func(t *testing.T) {
		impURL := fmt.Sprintf("%s/api/v1/analyses/%s/impact?target=internal/store", srv.URL, baseAnalysisID)
		resp, err := client.Get(impURL)
		if err != nil {
			t.Fatalf("GET impact: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var impactResp graph.ImpactAnalysisResult
		_ = json.NewDecoder(resp.Body).Decode(&impactResp)
		if impactResp.TotalAffected != 2 {
			t.Fatalf("expected 2 affected packages (api and cmd/server), got %d", impactResp.TotalAffected)
		}
		t.Logf("[Item 4 PASS] Impact Analysis on 'internal/store' reached %d packages", impactResp.TotalAffected)
	})

	// -------------------------------------------------------------------------
	// Item 5: PR Impact Analysis (Pull Request Impact)
	// -------------------------------------------------------------------------
	t.Run("Item 5: Pull Request Impact Analysis", func(t *testing.T) {
		mockGH.prSnap = domain.PullRequestSnapshot{
			Repository: domain.Repository{
				GitHubRepositoryID: 7777,
				Owner:              "testorg",
				Name:               "gomicro",
				DefaultBranch:      "main",
				HTMLURL:            "https://github.com/testorg/gomicro",
			},
			PullNumber: 10,
			Title:      "Update store implementation",
			State:      "open",
			BaseSHA:    baseSHA,
			HeadSHA:    baseSHA,
		}
		mockGH.prFiles = []domain.PullRequestFile{
			{
				Path:       "internal/store/store.go",
				Status:     domain.PRFileModified,
				Additions:  10,
				Deletions:  2,
				Changes:    12,
				IsPython:   false,
				IsAnalyzed: true,
			},
		}

		reqBody := `{"url":"https://github.com/testorg/gomicro/pull/10"}`
		resp, err := client.Post(srv.URL+"/api/v1/pull-requests/analyses", "application/json", strings.NewReader(reqBody))
		if err != nil {
			t.Fatalf("POST PR analysis: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("expected 202, got %d", resp.StatusCode)
		}

		var prCreateResp httpapi.CreatePRAnalysisResponse
		_ = json.NewDecoder(resp.Body).Decode(&prCreateResp)

		// Process with worker
		processed, err := worker.StepOnce(ctx)
		if err != nil || !processed {
			t.Fatalf("worker failed to process PR analysis: %v", err)
		}

		// Query PR Impact
		impResp, err := client.Get(srv.URL + "/api/v1/pull-requests/analyses/" + prCreateResp.PRAnalysisID + "/impact")
		if err != nil {
			t.Fatalf("GET PR impact: %v", err)
		}
		defer impResp.Body.Close()
		if impResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", impResp.StatusCode)
		}

		var prImpact httpapi.PRImpactResponse
		_ = json.NewDecoder(impResp.Body).Decode(&prImpact)
		if prImpact.PullNumber != 10 {
			t.Fatalf("expected PR #10, got %d", prImpact.PullNumber)
		}
		t.Logf("[Item 5 PASS] PR Impact Analysis complete: ChangedFiles=%d DirectImpact=%d",
			prImpact.Summary.ChangedFilesCount, prImpact.Summary.DirectImpactCount)
	})

	// -------------------------------------------------------------------------
	// Item 6: Import Evidence (Evidence Query)
	// -------------------------------------------------------------------------
	t.Run("Item 6: Import Evidence Retrieval", func(t *testing.T) {
		evURL := fmt.Sprintf("%s/api/v1/analyses/%s/evidence", srv.URL, baseAnalysisID)
		resp, err := client.Get(evURL)
		if err != nil {
			t.Fatalf("GET evidence: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var evResp httpapi.EvidenceResponse
		_ = json.NewDecoder(resp.Body).Decode(&evResp)
		if len(evResp.Imports) == 0 && evResp.Count == 0 {
			t.Fatalf("expected import evidence or references, got empty")
		}
		t.Logf("[Item 6 PASS] Import Evidence retrieved with %d imports", len(evResp.Imports))
	})

	// -------------------------------------------------------------------------
	// Item 7: BASE/HEAD Architecture Diff
	// -------------------------------------------------------------------------
	var diffAnalysisID string
	t.Run("Item 7: BASE/HEAD Architecture Diff Calculation", func(t *testing.T) {
		// First analyze head commit
		mockGH.currentSHA = headSHA
		_, _ = client.Post(srv.URL+"/api/v1/analyses", "application/json", strings.NewReader(`{"repository_url":"https://github.com/testorg/gomicro"}`))
		_, _ = worker.StepOnce(ctx)

		// Request Architecture Diff
		diffReqBody := fmt.Sprintf(`{
			"repository_url": "https://github.com/testorg/gomicro",
			"base_sha": "%s",
			"head_sha": "%s"
		}`, baseSHA, headSHA)

		resp, err := client.Post(srv.URL+"/api/v1/architecture-diffs", "application/json", strings.NewReader(diffReqBody))
		if err != nil {
			t.Fatalf("POST /architecture-diffs: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			t.Fatalf("expected 200/202, got %d", resp.StatusCode)
		}

		var diffResp httpapi.CreateArchitectureDiffResponse
		_ = json.NewDecoder(resp.Body).Decode(&diffResp)
		diffAnalysisID = diffResp.DiffID

		// Get Diff Graph
		dgResp, err := client.Get(srv.URL + "/api/v1/architecture-diffs/" + diffAnalysisID + "/graph")
		if err != nil {
			t.Fatalf("GET diff graph: %v", err)
		}
		defer dgResp.Body.Close()
		if dgResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for diff graph, got %d", dgResp.StatusCode)
		}

		var diffGraph httpapi.ArchitectureDiffGraphResponse
		_ = json.NewDecoder(dgResp.Body).Decode(&diffGraph)
		if diffGraph.Summary.AddedNodesCount != 1 {
			t.Fatalf("expected 1 added node (internal/cache), got %d", diffGraph.Summary.AddedNodesCount)
		}
		t.Logf("[Item 7 PASS] Architecture Diff calculated: NodesAdded=%d EdgesAdded=%d",
			diffGraph.Summary.AddedNodesCount, diffGraph.Summary.AddedEdgesCount)
	})

	// -------------------------------------------------------------------------
	// Item 8: SCC循環依存の変化 (Tarjan SCC Cycle Evolution)
	// -------------------------------------------------------------------------
	t.Run("Item 8: SCC Cycle Evolution in Architecture Diff", func(t *testing.T) {
		dgResp, err := client.Get(srv.URL + "/api/v1/architecture-diffs/" + diffAnalysisID + "/graph")
		if err != nil {
			t.Fatalf("GET diff graph: %v", err)
		}
		defer dgResp.Body.Close()

		var diffGraph httpapi.ArchitectureDiffGraphResponse
		_ = json.NewDecoder(dgResp.Body).Decode(&diffGraph)

		// In HEAD, cache -> api and api -> cache creates a 2-node cycle!
		if len(diffGraph.CycleChanges.AddedCycles) != 1 {
			t.Fatalf("expected 1 added cycle between api and cache, got %d: %+v",
				len(diffGraph.CycleChanges.AddedCycles), diffGraph.CycleChanges)
		}
		cycle := diffGraph.CycleChanges.AddedCycles[0]
		if len(cycle) != 2 {
			t.Fatalf("expected 2 nodes in added cycle, got %d: %v", len(cycle), cycle)
		}
		t.Logf("[Item 8 PASS] Tarjan SCC detected new cycle: %v", cycle)
	})

	// -------------------------------------------------------------------------
	// Item 9: キャッシュの再利用 (Cache Reuse on identical requests)
	// -------------------------------------------------------------------------
	t.Run("Item 9: Cache Reuse on Repeated Requests", func(t *testing.T) {
		// Repeated Analysis POST
		mockGH.currentSHA = baseSHA
		body := `{"repository_url":"https://github.com/testorg/gomicro"}`
		resp, err := client.Post(srv.URL+"/api/v1/analyses", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST analyses: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on cached analysis, got %d", resp.StatusCode)
		}
		var aResp httpapi.CreateAnalysisResponse
		_ = json.NewDecoder(resp.Body).Decode(&aResp)
		if !aResp.Cached || aResp.Status != domain.StatusCompleted {
			t.Fatalf("expected cached completed analysis, got %+v", aResp)
		}

		// Repeated Diff POST
		diffReqBody := fmt.Sprintf(`{
			"repository_url": "https://github.com/testorg/gomicro",
			"base_sha": "%s",
			"head_sha": "%s"
		}`, baseSHA, headSHA)
		dResp, err := client.Post(srv.URL+"/api/v1/architecture-diffs", "application/json", strings.NewReader(diffReqBody))
		if err != nil {
			t.Fatalf("POST diff: %v", err)
		}
		defer dResp.Body.Close()
		if dResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on cached diff, got %d", dResp.StatusCode)
		}
		var cdResp httpapi.CreateArchitectureDiffResponse
		_ = json.NewDecoder(dResp.Body).Decode(&cdResp)
		if !cdResp.Cached || cdResp.Status != domain.DiffStatusCompleted {
			t.Fatalf("expected cached completed diff, got %+v", cdResp)
		}
		t.Logf("[Item 9 PASS] Both analysis and architecture diff returned 200 OK with cached: true")
	})

	// -------------------------------------------------------------------------
	// Item 10: Worker再起動後の復旧 (Expired Lease Recovery & Heartbeat Fencing)
	// -------------------------------------------------------------------------
	t.Run("Item 10: Worker Crash Recovery via Expired Lease", func(t *testing.T) {
		// Insert an abandoned in-flight job with expired lease
		orphanedSHA := "9999000099990000999900009999000099990009"
		mockGH.archives[orphanedSHA] = baseGoZip

		var repoID int64
		_ = pool.QueryRow(ctx, `SELECT id FROM repositories WHERE owner = 'testorg' AND name = 'gomicro'`).Scan(&repoID)

		expiredTime := time.Now().Add(-10 * time.Minute)
		oldWorkerToken := "crashed-worker-token-xyz"
		var orphanedID string
		err := pool.QueryRow(ctx, `
			INSERT INTO analysis_runs (
				repository_id, commit_sha, analyzer_version, status, stage, attempt_count, lease_owner, lease_expires_at
			)
			VALUES ($1, $2, 'v2.0.0', 'running', 'fetching', 1, $3, $4)
			RETURNING id::text
		`, repoID, orphanedSHA, oldWorkerToken, expiredTime).Scan(&orphanedID)
		if err != nil {
			t.Fatalf("insert orphaned run: %v", err)
		}

		// A new worker starts up and executes StepOnce
		newWorker := jobs.NewWorker(st, proc, 1, 50*time.Millisecond)
		recovered, err := newWorker.StepOnce(ctx)
		if err != nil || !recovered {
			t.Fatalf("new worker failed to recover orphaned job: recovered=%v err=%v", recovered, err)
		}

		// Verify orphaned job was completed by the new worker
		run, err := st.GetAnalysis(ctx, orphanedID)
		if err != nil || run.Status != domain.StatusCompleted {
			t.Fatalf("expected recovered job to reach completed, got status=%s (err=%v)", run.Status, err)
		}
		if run.AttemptCount != 2 {
			t.Fatalf("expected attempt_count=2 after recovery, got %d", run.AttemptCount)
		}
		t.Logf("[Item 10 PASS] Crashed worker job recovered: attempt_count=%d status=%s", run.AttemptCount, run.Status)
	})
}

type mockFullE2EGHClient struct {
	archives   map[string][]byte
	currentSHA string
	prSnap     domain.PullRequestSnapshot
	prFiles    []domain.PullRequestFile
}

func (m *mockFullE2EGHClient) Resolve(_ context.Context, owner, repo string) (githubclient.RepositorySnapshot, error) {
	sha := m.currentSHA
	if sha == "" {
		sha = "1111000011110000111100001111000011110001"
	}
	return githubclient.RepositorySnapshot{
		GitHubID:      7777,
		Owner:         owner,
		Name:          repo,
		DefaultBranch: "main",
		CommitSHA:     sha,
		HTMLURL:       fmt.Sprintf("https://github.com/%s/%s", owner, repo),
	}, nil
}

func (m *mockFullE2EGHClient) ResolveCommit(_ context.Context, owner, repo, commitSHA string) (githubclient.RepositorySnapshot, error) {
	return githubclient.RepositorySnapshot{
		GitHubID:      7777,
		Owner:         owner,
		Name:          repo,
		DefaultBranch: "main",
		CommitSHA:     commitSHA,
		HTMLURL:       fmt.Sprintf("https://github.com/%s/%s", owner, repo),
	}, nil
}

func (m *mockFullE2EGHClient) GetPullRequest(_ context.Context, _, _ string, _ int) (domain.PullRequestSnapshot, error) {
	return m.prSnap, nil
}

func (m *mockFullE2EGHClient) GetPullRequestFiles(_ context.Context, _, _ string, _ int, _ int) ([]domain.PullRequestFile, error) {
	return m.prFiles, nil
}

func (m *mockFullE2EGHClient) DownloadArchive(_ context.Context, snap githubclient.RepositorySnapshot) (io.ReadCloser, error) {
	data, ok := m.archives[snap.CommitSHA]
	if !ok {
		return nil, fmt.Errorf("archive for commit %s not found", snap.CommitSHA)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func createGoZip(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for path, content := range files {
		w, _ := zw.Create("repo-snap/" + path)
		_, _ = io.WriteString(w, content)
	}
	_ = zw.Close()
	return buf.Bytes()
}
