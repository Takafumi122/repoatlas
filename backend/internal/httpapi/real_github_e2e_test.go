package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/jobs"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

func openE2EPostgresStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	ctx := context.Background()

	epPort := uint32(15433)
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
		t.Fatalf("failed to start PostgreSQL 16 for real GitHub E2E: %v", err)
	}
	t.Cleanup(func() {
		_ = epg.Stop()
	})

	dbURL := fmt.Sprintf("postgres://repoatlas:repoatlas@127.0.0.1:%d/repoatlas?sslmode=disable", epPort)
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})

	for _, mig := range []string{"000001_init.up.sql", "000002_lease_owner.up.sql", "000003_pr_and_evidence.up.sql", "000004_phase6_arch_diff_and_go.up.sql"} {
		sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", mig))
		if err != nil {
			t.Fatalf("read migration %s: %v", mig, err)
		}
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("exec migration %s: %v", mig, err)
		}
	}

	return store.NewPostgresStore(pool)
}

func TestRealGitHubEndToEnd_ItsDangerousAndRequests(t *testing.T) {
	if os.Getenv("RUN_REAL_GITHUB_E2E") != "1" {
		t.Skip("skipping live GitHub E2E test (set RUN_REAL_GITHUB_E2E=1 to execute)")
	}

	pgStore := openE2EPostgresStore(t)
	realGH := githubclient.NewRESTClient(githubclient.RESTClientOptions{
		Token:   os.Getenv("GITHUB_TOKEN"),
		Timeout: 45 * time.Second,
	})
	az := analyzer.NewPythonAnalyzer()
	proc := jobs.NewProcessor(pgStore, realGH, az, jobs.ProcessorOptions{
		AnalysisTimeout: 90 * time.Second,
	})
	worker := jobs.NewWorker(pgStore, proc, 1, 100*time.Millisecond)

	h := NewHandler(pgStore, realGH, "v1.0.0")
	router := NewRouter(h, []string{"http://localhost:3000"})

	repos := []string{
		"https://github.com/pallets/itsdangerous",
		"https://github.com/psf/requests",
	}

	for _, repoURL := range repos {
		t.Run(repoURL, func(t *testing.T) {
			// 1. POST /api/v1/analyses
			req := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"`+repoURL+`"}`))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("POST %s expected 202, got %d: %s", repoURL, rec.Code, rec.Body.String())
			}

			var createResp CreateAnalysisResponse
			if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
				t.Fatal(err)
			}
			analysisID := createResp.AnalysisID

			// 2. Worker claims and processes real GitHub archive
			claimed, err := worker.StepOnce(context.Background())
			if err != nil || !claimed {
				t.Fatalf("worker StepOnce for %s failed: claimed=%v err=%v", repoURL, claimed, err)
			}

			// 3. GET /api/v1/analyses/{id}/graph
			gReq := httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID+"/graph", nil)
			gRec := httptest.NewRecorder()
			router.ServeHTTP(gRec, gReq)
			if gRec.Code != http.StatusOK {
				t.Fatalf("GET graph for %s expected 200, got %d: %s", repoURL, gRec.Code, gRec.Body.String())
			}

			var graphResp GraphResponse
			if err := json.NewDecoder(gRec.Body).Decode(&graphResp); err != nil {
				t.Fatal(err)
			}

			if graphResp.Summary.Files == 0 || graphResp.Summary.InternalEdges == 0 {
				t.Fatalf("expected non-zero files and internal edges for %s, got %+v", repoURL, graphResp.Summary)
			}
			t.Logf("[%s] SHA=%s Files=%d InternalEdges=%d ExternalImports=%d UnresolvedImports=%d Recommended=%v",
				repoURL,
				graphResp.CommitSHA,
				graphResp.Summary.Files,
				graphResp.Summary.InternalEdges,
				graphResp.Summary.ExternalImports,
				graphResp.Summary.UnresolvedImports,
				graphResp.RecommendedFileIDs,
			)

			// 4. Re-POST same repository -> 200 OK cached=true
			cacheReq := httptest.NewRequest(http.MethodPost, "/api/v1/analyses", strings.NewReader(`{"repository_url":"`+repoURL+`"}`))
			cacheRec := httptest.NewRecorder()
			router.ServeHTTP(cacheRec, cacheReq)
			if cacheRec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK on cache hit for %s, got %d", repoURL, cacheRec.Code)
			}
			var cacheResp CreateAnalysisResponse
			_ = json.NewDecoder(cacheRec.Body).Decode(&cacheResp)
			if !cacheResp.Cached || cacheResp.Status != domain.StatusCompleted || cacheResp.AnalysisID != analysisID {
				t.Fatalf("expected cached completed response for %s, got %+v", repoURL, cacheResp)
			}
		})
	}
}

func TestRealGitHubEndToEnd_PRAnalysis_ItsDangerous296(t *testing.T) {
	if os.Getenv("RUN_REAL_GITHUB_E2E") != "1" {
		t.Skip("skipping real GitHub E2E test; set RUN_REAL_GITHUB_E2E=1 to run")
	}

	st := store.NewMemoryStore()
	realGH := githubclient.NewRESTClient(githubclient.RESTClientOptions{})
	az := analyzer.NewPythonAnalyzer()
	proc := jobs.NewProcessor(st, realGH, az, jobs.ProcessorOptions{
		AnalysisTimeout: 90 * time.Second,
	})
	worker := jobs.NewWorker(st, proc, 1, 100*time.Millisecond)

	h := NewHandler(st, realGH, "v1.0.0")
	router := NewRouter(h, []string{"http://localhost:3000"})

	prURL := "https://github.com/pallets/itsdangerous/pull/296"

	// 1. POST /api/v1/pull-requests/analyses
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pull-requests/analyses", strings.NewReader(`{"url":"`+prURL+`"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST PR analysis expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var createResp CreatePRAnalysisResponse
	if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
		t.Fatal(err)
	}
	prAnalysisID := createResp.PRAnalysisID

	// 2. Worker claims and processes real GitHub PR archive
	claimed, err := worker.StepOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("worker StepOnce for PR failed: claimed=%v err=%v", claimed, err)
	}

	// 3. GET /api/v1/pull-requests/analyses/{id}/impact
	iReq := httptest.NewRequest(http.MethodGet, "/api/v1/pull-requests/analyses/"+prAnalysisID+"/impact", nil)
	iRec := httptest.NewRecorder()
	router.ServeHTTP(iRec, iReq)
	if iRec.Code != http.StatusOK {
		t.Fatalf("GET PR impact expected 200, got %d: %s", iRec.Code, iRec.Body.String())
	}

	var impactResp PRImpactResponse
	if err := json.NewDecoder(iRec.Body).Decode(&impactResp); err != nil {
		t.Fatal(err)
	}

	t.Logf("[Real PR 296] Title=%q BaseSHA=%s HeadSHA=%s ChangedFiles=%d DirectImpact=%d IndirectImpact=%d TotalAffected=%d",
		impactResp.Title,
		impactResp.BaseSHA,
		impactResp.HeadSHA,
		impactResp.Summary.ChangedFilesCount,
		impactResp.Summary.DirectImpactCount,
		impactResp.Summary.IndirectImpactCount,
		impactResp.Summary.TotalAffectedCount,
	)

	// Validate expected properties of pallets/itsdangerous PR 296
	if impactResp.PullNumber != 296 {
		t.Errorf("expected pull number 296, got %d", impactResp.PullNumber)
	}
	if impactResp.BaseSHA != "3b762645dab2250933311f8612989a4075100659" {
		t.Errorf("unexpected base SHA: %s", impactResp.BaseSHA)
	}
	if impactResp.HeadSHA != "177196d3a5c7ad37b8ccbd0f88445aced07f1ca0" {
		t.Errorf("unexpected head SHA: %s", impactResp.HeadSHA)
	}
	if impactResp.Summary.ChangedFilesCount == 0 {
		t.Errorf("expected changed files > 0")
	}

	// 4. Re-POST same PR URL -> 200 OK cached=true
	cReq := httptest.NewRequest(http.MethodPost, "/api/v1/pull-requests/analyses", strings.NewReader(`{"url":"`+prURL+`"}`))
	cRec := httptest.NewRecorder()
	router.ServeHTTP(cRec, cReq)
	if cRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on PR cache hit, got %d", cRec.Code)
	}
	var cResp CreatePRAnalysisResponse
	_ = json.NewDecoder(cRec.Body).Decode(&cResp)
	if !cResp.Cached || cResp.PRAnalysisID != prAnalysisID {
		t.Fatalf("expected cached=true for PR, got %+v", cResp)
	}
}
