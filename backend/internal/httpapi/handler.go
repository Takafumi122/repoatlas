package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

const maxRequestBodyBytes = 16 * 1024 // 16 KiB

type HandlerOptions struct {
	RateLimiter    *IPRateLimiter
	IPExtractor    *IPExtractor
	ResolveTimeout time.Duration
	Logger         *slog.Logger
}

type Handler struct {
	store           store.Store
	ghClient        githubclient.Client
	analyzerVersion string
	rateLimiter     *IPRateLimiter
	ipExtractor     *IPExtractor
	resolveTimeout  time.Duration
	logger          *slog.Logger
}

func NewHandler(st store.Store, gh githubclient.Client, analyzerVersion string, opts ...HandlerOptions) *Handler {
	if analyzerVersion == "" {
		analyzerVersion = "v1.0.0"
	}
	var opt HandlerOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	ipExt := opt.IPExtractor
	if ipExt == nil {
		ipExt = NewIPExtractor(nil)
	}
	resolveTimeout := opt.ResolveTimeout
	if resolveTimeout <= 0 {
		resolveTimeout = 20 * time.Second
	}
	logger := opt.Logger
	if logger == nil {
		logger = NewStructuredLogger(io.Discard)
	}
	return &Handler{
		store:           st,
		ghClient:        gh,
		analyzerVersion: analyzerVersion,
		rateLimiter:     opt.RateLimiter,
		ipExtractor:     ipExt,
		resolveTimeout:  resolveTimeout,
		logger:          logger,
	}
}

func (h *Handler) HandleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) HandleCreateAnalysis(w http.ResponseWriter, r *http.Request) {
	if h.rateLimiter != nil {
		clientIP := h.ipExtractor.ClientIP(r)
		allowed, retryAfter := h.rateLimiter.Allow(clientIP)
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			writeError(w, domain.NewAppError(
				domain.ErrCodeRateLimited,
				http.StatusTooManyRequests,
				fmt.Sprintf("解析リクエストのレート制限に達しました（%d秒後に再試行してください）", retryAfter),
				nil,
			))
			return
		}
	}

	var req CreateAnalysisRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, domain.NewAppError(domain.ErrCodeInvalidRepositoryURL, http.StatusBadRequest, "リクエストJSONが不正です", err))
		return
	}

	parsedURL, err := githubclient.ParseRepositoryURL(req.RepositoryURL)
	if err != nil {
		writeError(w, err)
		return
	}

	resolveCtx, cancel := context.WithTimeout(r.Context(), h.resolveTimeout)
	defer cancel()

	snap, err := h.ghClient.Resolve(resolveCtx, parsedURL.Owner, parsedURL.Repo)
	if err != nil {
		writeError(w, err)
		return
	}

	run, cached, err := h.store.CreateOrGetAnalysis(r.Context(), snap, h.analyzerVersion)
	if err != nil {
		writeError(w, err)
		return
	}

	status := http.StatusAccepted
	if cached && run.Status == domain.StatusCompleted {
		status = http.StatusOK
	}

	writeJSON(w, status, CreateAnalysisResponse{
		AnalysisID: run.ID,
		Status:     run.Status,
		Cached:     cached,
	})
}

func (h *Handler) HandleGetAnalysis(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.store.GetAnalysis(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	var completedAtStr *string
	if run.CompletedAt != nil {
		s := run.CompletedAt.UTC().Format(time.RFC3339)
		completedAtStr = &s
	}

	resp := AnalysisStatusResponse{
		ID:     run.ID,
		Status: run.Status,
		Stage:  run.Stage,
		Repository: RepositorySummary{
			Owner:   run.Repository.Owner,
			Name:    run.Repository.Name,
			HTMLURL: run.Repository.HTMLURL,
		},
		CommitSHA: run.CommitSHA,
		Error: AnalysisErrorField{
			Code:    run.ErrorCode,
			Message: run.ErrorMessage,
		},
		CreatedAt:   run.CreatedAt.UTC().Format(time.RFC3339),
		CompletedAt: completedAtStr,
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleGetAnalysisGraph(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, files, edges, err := h.store.GetAnalysisGraph(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	nodes := make([]GraphNode, len(files))
	for i, f := range files {
		ghURL := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s",
			run.Repository.Owner,
			run.Repository.Name,
			run.CommitSHA,
			f.Path,
		)
		nodes[i] = GraphNode{
			ID:              domain.NodeIDForPath(f.Path),
			Path:            f.Path,
			Functions:       f.FunctionsCount,
			Classes:         f.ClassesCount,
			InDegree:        f.InDegree,
			OutDegree:       f.OutDegree,
			IsEntrypoint:    f.IsEntrypoint,
			ImportanceScore: f.ImportanceScore,
			GitHubURL:       ghURL,
		}
	}

	graphEdges := make([]GraphEdge, len(edges))
	internalEdges := make([]graph.Edge, len(edges))
	for i, e := range edges {
		kind := e.Kind
		if kind == "" {
			kind = "import"
		}
		graphEdges[i] = GraphEdge{
			Source: domain.NodeIDForPath(e.SourcePath),
			Target: domain.NodeIDForPath(e.TargetPath),
			Kind:   kind,
		}
		internalEdges[i] = graph.Edge{
			SourcePath: e.SourcePath,
			TargetPath: e.TargetPath,
			Kind:       kind,
		}
	}

	recommended := graph.SelectRecommendedFileIDs(files, 5)
	guidedTour := graph.BuildGuidedTour(files, internalEdges, 10)
	archGraph := graph.BuildArchitectureGraph(files, internalEdges)

	resp := GraphResponse{
		AnalysisID: run.ID,
		Repository: RepositorySummary{
			Owner:   run.Repository.Owner,
			Name:    run.Repository.Name,
			HTMLURL: run.Repository.HTMLURL,
		},
		CommitSHA: run.CommitSHA,
		Summary: GraphSummary{
			Files:             run.FilesCount,
			InternalEdges:     run.InternalEdgesCount,
			ExternalImports:   run.ExternalImportsCount,
			UnresolvedImports: run.UnresolvedImportsCount,
		},
		Nodes:              nodes,
		Edges:              graphEdges,
		RecommendedFileIDs: recommended,
		GuidedTour:         guidedTour,
		Architecture:       &archGraph,
		Warnings: []string{
			"Static import graph; dynamic imports are not included.",
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleGetAnalysisImpact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target := r.URL.Query().Get("target")
	if target == "" {
		writeError(w, domain.NewAppError(
			domain.ErrCodeInvalidRepositoryURL,
			http.StatusBadRequest,
			"targetクエリパラメータ（ファイルパスまたはノードID）が必要です",
			nil,
		))
		return
	}

	maxDepth := 3
	if rawDepth := r.URL.Query().Get("max_depth"); rawDepth != "" {
		parsed, err := strconv.Atoi(rawDepth)
		if err != nil || parsed < 1 || parsed > 50 {
			writeError(w, domain.NewAppError(
				domain.ErrCodeInvalidRepositoryURL,
				http.StatusBadRequest,
				"max_depthは1〜50の整数で指定してください",
				err,
			))
			return
		}
		maxDepth = parsed
	}

	var files []domain.SourceFile
	var internalEdges []graph.Edge

	if id == "demo" || id == "demo-python-web-starter" {
		files, internalEdges = demoFilesAndEdges()
	} else {
		_, storedFiles, storedEdges, err := h.store.GetAnalysisGraph(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		files = storedFiles
		internalEdges = make([]graph.Edge, len(storedEdges))
		for i, e := range storedEdges {
			internalEdges[i] = graph.Edge{
				SourcePath: e.SourcePath,
				TargetPath: e.TargetPath,
				Kind:       e.Kind,
			}
		}
	}

	impact, found := graph.AnalyzeImpact(files, internalEdges, target, maxDepth)
	if !found {
		writeError(w, domain.NewAppError(
			domain.ErrCodeAnalysisNotFound,
			http.StatusNotFound,
			"指定されたファイルが解析グラフ内に見つかりません",
			nil,
		))
		return
	}

	writeJSON(w, http.StatusOK, impact)
}

func demoFilesAndEdges() ([]domain.SourceFile, []graph.Edge) {
	files := []domain.SourceFile{
		{Path: "app/__init__.py", FunctionsCount: 1, ClassesCount: 0, InDegree: 1, OutDegree: 2, IsEntrypoint: false, ImportanceScore: 3.0},
		{Path: "app/auth.py", FunctionsCount: 3, ClassesCount: 1, InDegree: 2, OutDegree: 2, IsEntrypoint: false, ImportanceScore: 5.0},
		{Path: "app/config.py", FunctionsCount: 2, ClassesCount: 1, InDegree: 3, OutDegree: 0, IsEntrypoint: false, ImportanceScore: 6.0},
		{Path: "app/db.py", FunctionsCount: 4, ClassesCount: 1, InDegree: 2, OutDegree: 1, IsEntrypoint: false, ImportanceScore: 4.5},
		{Path: "app/main.py", FunctionsCount: 2, ClassesCount: 0, InDegree: 0, OutDegree: 3, IsEntrypoint: true, ImportanceScore: 4.5},
		{Path: "app/models.py", FunctionsCount: 1, ClassesCount: 3, InDegree: 2, OutDegree: 1, IsEntrypoint: false, ImportanceScore: 4.5},
		{Path: "app/routes.py", FunctionsCount: 5, ClassesCount: 0, InDegree: 1, OutDegree: 3, IsEntrypoint: false, ImportanceScore: 3.5},
	}
	edges := []graph.Edge{
		{SourcePath: "app/__init__.py", TargetPath: "app/config.py", Kind: "import"},
		{SourcePath: "app/__init__.py", TargetPath: "app/routes.py", Kind: "import"},
		{SourcePath: "app/auth.py", TargetPath: "app/config.py", Kind: "import"},
		{SourcePath: "app/auth.py", TargetPath: "app/models.py", Kind: "import"},
		{SourcePath: "app/db.py", TargetPath: "app/config.py", Kind: "import"},
		{SourcePath: "app/main.py", TargetPath: "app/__init__.py", Kind: "import"},
		{SourcePath: "app/main.py", TargetPath: "app/auth.py", Kind: "import"},
		{SourcePath: "app/main.py", TargetPath: "app/db.py", Kind: "import"},
		{SourcePath: "app/models.py", TargetPath: "app/db.py", Kind: "import"},
		{SourcePath: "app/routes.py", TargetPath: "app/auth.py", Kind: "import"},
		{SourcePath: "app/routes.py", TargetPath: "app/models.py", Kind: "import"},
	}
	return files, edges
}

func (h *Handler) HandleGetDemoGraph(w http.ResponseWriter, _ *http.Request) {
	demoSHA := "8f4b2c9e1a3d5f7b9c0e2a4d6f8b0c2e4a6d8f0a"
	baseBlob := "https://github.com/repoatlas-demo/python-web-starter/blob/" + demoSHA + "/"
	demoFiles, demoEdges := demoFilesAndEdges()
	demoTour := graph.BuildGuidedTour(demoFiles, demoEdges, 10)
	demoArch := graph.BuildArchitectureGraph(demoFiles, demoEdges)

	resp := GraphResponse{
		Demo:       true,
		AnalysisID: "demo-python-web-starter",
		Repository: RepositorySummary{
			Owner:   "repoatlas-demo",
			Name:    "python-web-starter",
			HTMLURL: "https://github.com/repoatlas-demo/python-web-starter",
		},
		CommitSHA: demoSHA,
		Summary: GraphSummary{
			Files:             7,
			InternalEdges:     11,
			ExternalImports:   4,
			UnresolvedImports: 1,
		},
		Nodes: []GraphNode{
			{
				ID:              "file:app/__init__.py",
				Path:            "app/__init__.py",
				Functions:       1,
				Classes:         0,
				InDegree:        1,
				OutDegree:       2,
				IsEntrypoint:    false,
				ImportanceScore: 3.0,
				GitHubURL:       baseBlob + "app/__init__.py",
			},
			{
				ID:              "file:app/auth.py",
				Path:            "app/auth.py",
				Functions:       3,
				Classes:         1,
				InDegree:        2,
				OutDegree:       2,
				IsEntrypoint:    false,
				ImportanceScore: 5.0,
				GitHubURL:       baseBlob + "app/auth.py",
			},
			{
				ID:              "file:app/config.py",
				Path:            "app/config.py",
				Functions:       2,
				Classes:         1,
				InDegree:        3,
				OutDegree:       0,
				IsEntrypoint:    false,
				ImportanceScore: 6.0,
				GitHubURL:       baseBlob + "app/config.py",
			},
			{
				ID:              "file:app/db.py",
				Path:            "app/db.py",
				Functions:       4,
				Classes:         1,
				InDegree:        2,
				OutDegree:       1,
				IsEntrypoint:    false,
				ImportanceScore: 4.5,
				GitHubURL:       baseBlob + "app/db.py",
			},
			{
				ID:              "file:app/main.py",
				Path:            "app/main.py",
				Functions:       2,
				Classes:         0,
				InDegree:        0,
				OutDegree:       3,
				IsEntrypoint:    true,
				ImportanceScore: 4.5,
				GitHubURL:       baseBlob + "app/main.py",
			},
			{
				ID:              "file:app/models.py",
				Path:            "app/models.py",
				Functions:       1,
				Classes:         3,
				InDegree:        2,
				OutDegree:       1,
				IsEntrypoint:    false,
				ImportanceScore: 4.5,
				GitHubURL:       baseBlob + "app/models.py",
			},
			{
				ID:              "file:app/routes.py",
				Path:            "app/routes.py",
				Functions:       5,
				Classes:         0,
				InDegree:        1,
				OutDegree:       3,
				IsEntrypoint:    false,
				ImportanceScore: 3.5,
				GitHubURL:       baseBlob + "app/routes.py",
			},
		},
		Edges: []GraphEdge{
			{Source: "file:app/__init__.py", Target: "file:app/config.py", Kind: "import"},
			{Source: "file:app/__init__.py", Target: "file:app/routes.py", Kind: "import"},
			{Source: "file:app/auth.py", Target: "file:app/config.py", Kind: "import"},
			{Source: "file:app/auth.py", Target: "file:app/models.py", Kind: "import"},
			{Source: "file:app/db.py", Target: "file:app/config.py", Kind: "import"},
			{Source: "file:app/main.py", Target: "file:app/__init__.py", Kind: "import"},
			{Source: "file:app/main.py", Target: "file:app/auth.py", Kind: "import"},
			{Source: "file:app/main.py", Target: "file:app/db.py", Kind: "import"},
			{Source: "file:app/models.py", Target: "file:app/db.py", Kind: "import"},
			{Source: "file:app/routes.py", Target: "file:app/auth.py", Kind: "import"},
			{Source: "file:app/routes.py", Target: "file:app/models.py", Kind: "import"},
		},
		RecommendedFileIDs: []string{
			"file:app/config.py",
			"file:app/auth.py",
			"file:app/db.py",
			"file:app/main.py",
			"file:app/models.py",
		},
		GuidedTour:   demoTour,
		Architecture: &demoArch,
		Warnings: []string{
			"Demo dataset (bundled static fixture). Real GitHub analysis results may vary.",
			"Static import graph; dynamic imports are not included.",
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, err error) {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		status := appErr.HTTPStatus
		if status <= 0 {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, ErrorResponse{
			Error: ErrorBody{
				Code:    string(appErr.Code),
				Message: appErr.Message,
			},
		})
		return
	}

	writeJSON(w, http.StatusInternalServerError, ErrorResponse{
		Error: ErrorBody{
			Code:    string(domain.ErrCodeInternalError),
			Message: "サーバー内部でエラーが発生しました",
		},
	})
}
