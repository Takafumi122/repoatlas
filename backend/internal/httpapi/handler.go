package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/diff"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
	"github.com/repoatlas/repoatlas/backend/internal/githubclient"
	"github.com/repoatlas/repoatlas/backend/internal/store"
)

var commitSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

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

	lang := run.Language
	if lang == "" {
		lang = "python"
	}
	granularity := run.GraphGranularity
	if granularity == "" {
		if lang == "go" {
			granularity = "package"
		} else {
			granularity = "file"
		}
	}

	resp := AnalysisStatusResponse{
		ID:               run.ID,
		Status:           run.Status,
		Stage:            run.Stage,
		Language:         lang,
		GraphGranularity: granularity,
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

	isGo := run.Language == "go" || (len(files) > 0 && (files[0].Language == "go" || files[0].NodeKind == "package"))
	lang := "python"
	granularity := "file"
	if isGo {
		lang = "go"
		granularity = "package"
	}

	nodes := make([]GraphNode, len(files))
	for i, f := range files {
		var nodeID, ghURL string
		if isGo {
			pkgPath := f.PackageImportPath
			if pkgPath == "" {
				pkgPath = f.Path
			}
			nodeID = domain.NodeIDForPackage(pkgPath)
			if f.DirectoryPath == "" || f.DirectoryPath == "." {
				ghURL = fmt.Sprintf("https://github.com/%s/%s/tree/%s",
					run.Repository.Owner,
					run.Repository.Name,
					run.CommitSHA,
				)
			} else {
				ghURL = fmt.Sprintf("https://github.com/%s/%s/tree/%s/%s",
					run.Repository.Owner,
					run.Repository.Name,
					run.CommitSHA,
					f.DirectoryPath,
				)
			}
		} else {
			nodeID = domain.NodeIDForPath(f.Path)
			ghURL = fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s",
				run.Repository.Owner,
				run.Repository.Name,
				run.CommitSHA,
				f.Path,
			)
		}

		nodeKind := f.NodeKind
		if nodeKind == "" {
			if isGo {
				nodeKind = "package"
			} else {
				nodeKind = "file"
			}
		}

		nodes[i] = GraphNode{
			ID:                nodeID,
			Path:              f.Path,
			NodeKind:          nodeKind,
			Language:          lang,
			Functions:         f.FunctionsCount,
			Classes:           f.ClassesCount,
			InDegree:          f.InDegree,
			OutDegree:         f.OutDegree,
			IsEntrypoint:      f.IsEntrypoint,
			ImportanceScore:   f.ImportanceScore,
			GitHubURL:         ghURL,
			ModulePath:        f.ModulePath,
			PackageImportPath: f.PackageImportPath,
			DirectoryPath:     f.DirectoryPath,
			PackageName:       f.PackageName,
			FilesCount:        f.FilesCount,
			MethodsCount:      f.MethodsCount,
			TypesCount:        f.TypesCount,
			Files:             f.Files,
		}
	}

	graphEdges := make([]GraphEdge, len(edges))
	internalEdges := make([]graph.Edge, len(edges))
	for i, e := range edges {
		kind := e.Kind
		if kind == "" {
			kind = "import"
		}

		var srcID, dstID string
		if isGo {
			srcID = domain.NodeIDForPackage(e.SourcePath)
			dstID = domain.NodeIDForPackage(e.TargetPath)
		} else {
			srcID = domain.NodeIDForPath(e.SourcePath)
			dstID = domain.NodeIDForPath(e.TargetPath)
		}

		graphEdges[i] = GraphEdge{
			Source: srcID,
			Target: dstID,
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
		AnalysisID:       run.ID,
		Language:         lang,
		GraphGranularity: granularity,
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

func (h *Handler) HandleCreatePRAnalysis(w http.ResponseWriter, r *http.Request) {
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

	var req CreatePRAnalysisRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, domain.NewAppError(domain.ErrCodeInvalidPullRequestURL, http.StatusBadRequest, "リクエストJSONが不正です", err))
		return
	}

	parsedURL, err := githubclient.ParsePullRequestURL(req.URL)
	if err != nil {
		writeError(w, err)
		return
	}

	resolveCtx, cancel := context.WithTimeout(r.Context(), h.resolveTimeout)
	defer cancel()

	snap, err := h.ghClient.GetPullRequest(resolveCtx, parsedURL.Owner, parsedURL.Repo, parsedURL.PullNumber)
	if err != nil {
		writeError(w, err)
		return
	}

	pr, cached, err := h.store.CreateOrGetPRAnalysis(r.Context(), snap, h.analyzerVersion)
	if err != nil {
		writeError(w, err)
		return
	}

	status := http.StatusAccepted
	if cached && pr.Status == domain.StatusCompleted {
		status = http.StatusOK
	}

	writeJSON(w, status, CreatePRAnalysisResponse{
		PRAnalysisID: pr.ID,
		Status:       pr.Status,
		Cached:       cached,
	})
}

func (h *Handler) HandleGetPRAnalysis(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pr, err := h.store.GetPRAnalysis(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	var completedAtStr *string
	if pr.CompletedAt != nil {
		s := pr.CompletedAt.UTC().Format(time.RFC3339)
		completedAtStr = &s
	}

	resp := PRAnalysisStatusResponse{
		ID:     pr.ID,
		Status: pr.Status,
		Stage:  pr.Stage,
		Repository: RepositorySummary{
			Owner:   pr.Repository.Owner,
			Name:    pr.Repository.Name,
			HTMLURL: pr.Repository.HTMLURL,
		},
		PullNumber:     pr.PullNumber,
		Title:          pr.Title,
		State:          pr.State,
		BaseSHA:        pr.BaseSHA,
		HeadSHA:        pr.HeadSHA,
		BaseRef:        pr.BaseRef,
		HeadRef:        pr.HeadRef,
		HeadAnalysisID: pr.HeadAnalysisID,
		Error: AnalysisErrorField{
			Code:    pr.ErrorCode,
			Message: pr.ErrorMessage,
		},
		Summary: domain.PRImpactSummary{
			ChangedFilesCount:    pr.ChangedFilesCount,
			DirectImpactCount:    pr.DirectImpactCount,
			IndirectImpactCount:  pr.IndirectImpactCount,
			TotalAffectedCount:   pr.TotalAffectedCount,
			UnanalyzedFilesCount: pr.UnanalyzedFilesCount,
			AnalysisCompleteness: pr.AnalysisCompleteness,
		},
		CreatedAt:   pr.CreatedAt.UTC().Format(time.RFC3339),
		CompletedAt: completedAtStr,
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleGetPRAnalysisImpact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pr, files, err := h.store.GetPRAnalysisImpact(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	if pr.Status != domain.StatusCompleted {
		writeError(w, domain.NewAppError(
			domain.ErrCodeAnalysisNotReady,
			http.StatusConflict,
			"PR影響解析はまだ完了していません",
			nil,
		))
		return
	}

	if pr.HeadAnalysisID == nil {
		writeError(w, domain.NewAppError(
			domain.ErrCodeInternalError,
			http.StatusInternalServerError,
			"HEADコミットの解析データが見つかりません",
			nil,
		))
		return
	}

	_, headFiles, headEdges, err := h.store.GetAnalysisGraph(r.Context(), *pr.HeadAnalysisID)
	if err != nil {
		writeError(w, err)
		return
	}

	impactResult := graph.AnalyzeMultiSourceImpact(headFiles, headEdges, files, 100)

	var unanalyzed []domain.PullRequestFile
	for _, f := range files {
		if !f.IsAnalyzed {
			unanalyzed = append(unanalyzed, f)
		}
	}
	if unanalyzed == nil {
		unanalyzed = []domain.PullRequestFile{}
	}

	resp := PRImpactResponse{
		PRAnalysisID:   pr.ID,
		HeadAnalysisID: *pr.HeadAnalysisID,
		Repository: RepositorySummary{
			Owner:   pr.Repository.Owner,
			Name:    pr.Repository.Name,
			HTMLURL: pr.Repository.HTMLURL,
		},
		PullNumber:      pr.PullNumber,
		Title:           pr.Title,
		BaseSHA:         pr.BaseSHA,
		HeadSHA:         pr.HeadSHA,
		Summary:         impactResult.Summary,
		ChangedFiles:    files,
		AffectedFiles:   impactResult.AffectedFiles,
		AffectedEdges:   impactResult.AffectedEdges,
		UnanalyzedFiles: unanalyzed,
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleGetAnalysisEvidence(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sourcePath := r.URL.Query().Get("source_path")
	targetPath := r.URL.Query().Get("target_path")
	unresolvedOnly := r.URL.Query().Get("unresolved_only") == "true"

	evidences, err := h.store.GetImportEvidences(r.Context(), id, sourcePath, targetPath, unresolvedOnly)
	if err != nil {
		writeError(w, err)
		return
	}

	refs, _ := h.store.GetImportReferences(r.Context(), id)

	writeJSON(w, http.StatusOK, EvidenceResponse{
		AnalysisID: id,
		Count:      len(evidences),
		Evidences:  evidences,
		Imports:    refs,
	})
}

func (h *Handler) HandleCreateArchitectureDiff(w http.ResponseWriter, r *http.Request) {
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

	var req CreateArchitectureDiffRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, domain.NewAppError(domain.ErrCodeInvalidRepositoryURL, http.StatusBadRequest, "リクエストJSONが不正です", err))
		return
	}

	baseSHA := strings.TrimSpace(req.BaseSHA)
	headSHA := strings.TrimSpace(req.HeadSHA)
	if !commitSHAPattern.MatchString(baseSHA) || !commitSHAPattern.MatchString(headSHA) {
		writeError(w, domain.NewAppError(domain.ErrCodeInvalidRepositoryURL, http.StatusBadRequest, "BASEおよびHEADコミットSHAは40文字の16進数で指定してください", nil))
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

	// Trigger or get base snapshot analysis
	baseSnap := snap
	baseSnap.CommitSHA = baseSHA
	_, _, err = h.store.CreateOrGetAnalysis(r.Context(), baseSnap, h.analyzerVersion)
	if err != nil {
		writeError(w, err)
		return
	}

	// Trigger or get head snapshot analysis
	headSnap := snap
	headSnap.CommitSHA = headSHA
	_, _, err = h.store.CreateOrGetAnalysis(r.Context(), headSnap, h.analyzerVersion)
	if err != nil {
		writeError(w, err)
		return
	}

	diffAnalysis, cached, err := h.store.CreateOrGetArchitectureDiff(r.Context(), snap, baseSHA, headSHA, h.analyzerVersion, "v1.0.0")
	if err != nil {
		writeError(w, err)
		return
	}

	status := http.StatusAccepted
	if cached && diffAnalysis.Status == domain.DiffStatusCompleted {
		status = http.StatusOK
	}

	writeJSON(w, status, CreateArchitectureDiffResponse{
		DiffID: diffAnalysis.ID,
		Status: diffAnalysis.Status,
		Cached: cached,
	})
}

func (h *Handler) HandleGetArchitectureDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	diffAnalysis, err := h.store.GetArchitectureDiff(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	// If queued or running, check if underlying base & head runs are now completed
	if diffAnalysis.Status != domain.DiffStatusCompleted && diffAnalysis.Status != domain.DiffStatusFailed {
		baseSnap := githubclient.RepositorySnapshot{
			GitHubID:      diffAnalysis.Repository.GitHubRepositoryID,
			Owner:         diffAnalysis.Repository.Owner,
			Name:          diffAnalysis.Repository.Name,
			DefaultBranch: diffAnalysis.Repository.DefaultBranch,
			HTMLURL:       diffAnalysis.Repository.HTMLURL,
			CommitSHA:     diffAnalysis.BaseSHA,
		}
		headSnap := baseSnap
		headSnap.CommitSHA = diffAnalysis.HeadSHA

		baseRun, _, bErr := h.store.CreateOrGetAnalysis(r.Context(), baseSnap, diffAnalysis.AnalyzerVersion)
		headRun, _, hErr := h.store.CreateOrGetAnalysis(r.Context(), headSnap, diffAnalysis.AnalyzerVersion)

		if bErr == nil && hErr == nil {
			if baseRun.Status == domain.StatusFailed {
				_ = h.store.FailArchitectureDiff(r.Context(), id, "", "BASE_ANALYSIS_FAILED", "BASEコミットの解析が失敗しました")
				diffAnalysis.Status = domain.DiffStatusFailed
				code := "BASE_ANALYSIS_FAILED"
				msg := "BASEコミットの解析が失敗しました"
				diffAnalysis.ErrorCode = &code
				diffAnalysis.ErrorMessage = &msg
			} else if headRun.Status == domain.StatusFailed {
				_ = h.store.FailArchitectureDiff(r.Context(), id, "", "HEAD_ANALYSIS_FAILED", "HEADコミットの解析が失敗しました")
				diffAnalysis.Status = domain.DiffStatusFailed
				code := "HEAD_ANALYSIS_FAILED"
				msg := "HEADコミットの解析が失敗しました"
				diffAnalysis.ErrorCode = &code
				diffAnalysis.ErrorMessage = &msg
			} else if baseRun.Status == domain.StatusCompleted && headRun.Status == domain.StatusCompleted {
				_, baseFiles, baseEdges, err1 := h.store.GetAnalysisGraph(r.Context(), baseRun.ID)
				_, headFiles, headEdges, err2 := h.store.GetAnalysisGraph(r.Context(), headRun.ID)
				if err1 == nil && err2 == nil {
					baseLang := baseRun.Language
					if baseLang == "" {
						baseLang = "python"
					}
					headLang := headRun.Language
					if headLang == "" {
						headLang = "python"
					}
					if baseLang != headLang {
						_ = h.store.FailArchitectureDiff(r.Context(), id, "", "LANGUAGE_MISMATCH", "BASEとHEADで言語が異なります")
						diffAnalysis.Status = domain.DiffStatusFailed
						code := "LANGUAGE_MISMATCH"
						msg := "BASEとHEADで言語が異なります"
						diffAnalysis.ErrorCode = &code
						diffAnalysis.ErrorMessage = &msg
					} else {
						diffRes := diff.ComputeGraphDiff(
							diffAnalysis.BaseSHA, diffAnalysis.HeadSHA,
							baseLang, baseRun.GraphGranularity,
							baseFiles, baseEdges,
							headFiles, headEdges,
						)
						_ = h.store.CompleteArchitectureDiff(r.Context(), id, "", diffRes.Summary)
						diffAnalysis.Status = domain.DiffStatusCompleted
						diffAnalysis.Stage = "completed"
						diffAnalysis.DiffSummary = &diffRes.Summary
					}
				}
			}
		}
	}

	var completedAtStr *string
	if diffAnalysis.CompletedAt != nil {
		s := diffAnalysis.CompletedAt.UTC().Format(time.RFC3339)
		completedAtStr = &s
	}

	resp := ArchitectureDiffStatusResponse{
		ID:     diffAnalysis.ID,
		Status: diffAnalysis.Status,
		Stage:  diffAnalysis.Stage,
		Repository: RepositorySummary{
			Owner:   diffAnalysis.Repository.Owner,
			Name:    diffAnalysis.Repository.Name,
			HTMLURL: diffAnalysis.Repository.HTMLURL,
		},
		BaseSHA: diffAnalysis.BaseSHA,
		HeadSHA: diffAnalysis.HeadSHA,
		Error: AnalysisErrorField{
			Code:    diffAnalysis.ErrorCode,
			Message: diffAnalysis.ErrorMessage,
		},
		Summary:     diffAnalysis.DiffSummary,
		CreatedAt:   diffAnalysis.CreatedAt.UTC().Format(time.RFC3339),
		CompletedAt: completedAtStr,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleGetArchitectureDiffGraph(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	diffAnalysis, err := h.store.GetArchitectureDiff(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	baseSnap := githubclient.RepositorySnapshot{
		GitHubID:      diffAnalysis.Repository.GitHubRepositoryID,
		Owner:         diffAnalysis.Repository.Owner,
		Name:          diffAnalysis.Repository.Name,
		DefaultBranch: diffAnalysis.Repository.DefaultBranch,
		HTMLURL:       diffAnalysis.Repository.HTMLURL,
		CommitSHA:     diffAnalysis.BaseSHA,
	}
	headSnap := baseSnap
	headSnap.CommitSHA = diffAnalysis.HeadSHA

	baseRun, _, err1 := h.store.CreateOrGetAnalysis(r.Context(), baseSnap, diffAnalysis.AnalyzerVersion)
	headRun, _, err2 := h.store.CreateOrGetAnalysis(r.Context(), headSnap, diffAnalysis.AnalyzerVersion)
	if err1 != nil {
		writeError(w, err1)
		return
	}
	if err2 != nil {
		writeError(w, err2)
		return
	}

	if baseRun.Status != domain.StatusCompleted || headRun.Status != domain.StatusCompleted {
		writeError(w, domain.NewAppError(
			domain.ErrCodeAnalysisNotReady,
			http.StatusConflict,
			"BASEまたはHEADコミットの解析がまだ完了していません",
			nil,
		))
		return
	}

	_, baseFiles, baseEdges, err1 := h.store.GetAnalysisGraph(r.Context(), baseRun.ID)
	_, headFiles, headEdges, err2 := h.store.GetAnalysisGraph(r.Context(), headRun.ID)
	if err1 != nil {
		writeError(w, err1)
		return
	}
	if err2 != nil {
		writeError(w, err2)
		return
	}

	baseLang := baseRun.Language
	if baseLang == "" {
		baseLang = "python"
	}
	headLang := headRun.Language
	if headLang == "" {
		headLang = "python"
	}
	if baseLang != headLang {
		writeError(w, domain.NewAppError(
			domain.ErrCodeInvalidRepositoryURL,
			http.StatusBadRequest,
			fmt.Sprintf("BASE(%s)とHEAD(%s)の言語が異なります", baseLang, headLang),
			nil,
		))
		return
	}

	granularity := baseRun.GraphGranularity
	if granularity == "" {
		if baseLang == "go" {
			granularity = "package"
		} else {
			granularity = "file"
		}
	}

	diffRes := diff.ComputeGraphDiff(
		diffAnalysis.BaseSHA,
		diffAnalysis.HeadSHA,
		baseLang,
		granularity,
		baseFiles,
		baseEdges,
		headFiles,
		headEdges,
	)

	if diffAnalysis.Status != domain.DiffStatusCompleted {
		_ = h.store.CompleteArchitectureDiff(r.Context(), id, "", diffRes.Summary)
	}

	resp := ArchitectureDiffGraphResponse{
		DiffID: diffAnalysis.ID,
		Repository: RepositorySummary{
			Owner:   diffAnalysis.Repository.Owner,
			Name:    diffAnalysis.Repository.Name,
			HTMLURL: diffAnalysis.Repository.HTMLURL,
		},
		BaseSHA:      diffAnalysis.BaseSHA,
		HeadSHA:      diffAnalysis.HeadSHA,
		Language:     baseLang,
		Granularity:  granularity,
		Status:       domain.DiffStatusCompleted,
		Summary:      diffRes.Summary,
		Nodes:        diffRes.Nodes,
		Edges:        diffRes.Edges,
		CycleChanges: diffRes.CycleChanges,
		Warnings:     diffRes.Warnings,
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
