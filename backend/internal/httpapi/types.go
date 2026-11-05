package httpapi

import (
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/diff"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

type CreateAnalysisRequest struct {
	RepositoryURL string `json:"repository_url"`
}

type CreateAnalysisResponse struct {
	AnalysisID string                `json:"analysis_id"`
	Status     domain.AnalysisStatus `json:"status"`
	Cached     bool                  `json:"cached"`
}

type AnalysisErrorField struct {
	Code    *string `json:"code"`
	Message *string `json:"message"`
}

type RepositorySummary struct {
	Owner   string `json:"owner"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

type AnalysisStatusResponse struct {
	ID               string                `json:"id"`
	Status           domain.AnalysisStatus `json:"status"`
	Stage            domain.AnalysisStage  `json:"stage"`
	Language         string                `json:"language,omitempty"`
	GraphGranularity string                `json:"graph_granularity,omitempty"`
	Repository       RepositorySummary     `json:"repository"`
	CommitSHA        string                `json:"commit_sha"`
	Error            AnalysisErrorField    `json:"error"`
	CreatedAt        string                `json:"created_at"`
	CompletedAt      *string               `json:"completed_at"`
}

type GraphSummary struct {
	Files             int `json:"files"`
	InternalEdges     int `json:"internal_edges"`
	ExternalImports   int `json:"external_imports"`
	UnresolvedImports int `json:"unresolved_imports"`
}

type GraphNode struct {
	ID                string                   `json:"id"`
	Path              string                   `json:"path"`
	NodeKind          string                   `json:"node_kind,omitempty"` // "file" or "package"
	Language          string                   `json:"language,omitempty"`  // "python" or "go"
	Functions         int                      `json:"functions"`
	Classes           int                      `json:"classes"`
	InDegree          int                      `json:"in_degree"`
	OutDegree         int                      `json:"out_degree"`
	IsEntrypoint      bool                     `json:"is_entrypoint"`
	ImportanceScore   float64                  `json:"importance_score"`
	GitHubURL         string                   `json:"github_url"`
	ModulePath        string                   `json:"module_path,omitempty"`
	PackageImportPath string                   `json:"package_import_path,omitempty"`
	DirectoryPath     string                   `json:"directory_path,omitempty"`
	PackageName       string                   `json:"package_name,omitempty"`
	FilesCount        int                      `json:"files_count,omitempty"`
	MethodsCount      int                      `json:"methods_count,omitempty"`
	TypesCount        int                      `json:"types_count,omitempty"`
	Files             []domain.PackageFileInfo `json:"files,omitempty"`
}

type GraphEdge struct {
	Source        string `json:"source"`
	Target        string `json:"target"`
	Kind          string `json:"kind"`
	EvidenceCount int    `json:"evidence_count,omitempty"`
}

type CreatePRAnalysisRequest struct {
	URL string `json:"url"`
}

type CreatePRAnalysisResponse struct {
	PRAnalysisID string                `json:"pr_analysis_id"`
	Status       domain.AnalysisStatus `json:"status"`
	Cached       bool                  `json:"cached"`
}

type PRAnalysisStatusResponse struct {
	ID             string                 `json:"id"`
	Status         domain.AnalysisStatus  `json:"status"`
	Stage          domain.AnalysisStage   `json:"stage"`
	Repository     RepositorySummary      `json:"repository"`
	PullNumber     int                    `json:"pull_number"`
	Title          string                 `json:"title"`
	State          string                 `json:"state"`
	BaseSHA        string                 `json:"base_sha"`
	HeadSHA        string                 `json:"head_sha"`
	BaseRef        string                 `json:"base_ref"`
	HeadRef        string                 `json:"head_ref"`
	HeadAnalysisID *string                `json:"head_analysis_id,omitempty"`
	Error          AnalysisErrorField     `json:"error"`
	Summary        domain.PRImpactSummary `json:"summary"`
	CreatedAt      string                 `json:"created_at"`
	CompletedAt    *string                `json:"completed_at,omitempty"`
}

type PRImpactResponse struct {
	PRAnalysisID    string                   `json:"pr_analysis_id"`
	HeadAnalysisID  string                   `json:"head_analysis_id"`
	Repository      RepositorySummary        `json:"repository"`
	PullNumber      int                      `json:"pull_number"`
	Title           string                   `json:"title"`
	BaseSHA         string                   `json:"base_sha"`
	HeadSHA         string                   `json:"head_sha"`
	Summary         domain.PRImpactSummary   `json:"summary"`
	ChangedFiles    []domain.PullRequestFile `json:"changed_files"`
	AffectedFiles   []domain.PRAffectedFile  `json:"affected_files"`
	AffectedEdges   []graph.Edge             `json:"affected_edges"`
	UnanalyzedFiles []domain.PullRequestFile `json:"unanalyzed_files"`
}

type EvidenceResponse struct {
	AnalysisID string                   `json:"analysis_id"`
	Count      int                      `json:"count"`
	Evidences  []domain.ImportEvidence  `json:"evidences,omitempty"`
	Imports    []domain.ImportReference `json:"imports,omitempty"`
}

type GraphResponse struct {
	Demo               bool                     `json:"demo,omitempty"`
	AnalysisID         string                   `json:"analysis_id"`
	Language           string                   `json:"language,omitempty"`
	GraphGranularity   string                   `json:"graph_granularity,omitempty"`
	Repository         RepositorySummary        `json:"repository"`
	CommitSHA          string                   `json:"commit_sha"`
	Summary            GraphSummary             `json:"summary"`
	Nodes              []GraphNode              `json:"nodes"`
	Edges              []GraphEdge              `json:"edges"`
	RecommendedFileIDs []string                 `json:"recommended_file_ids"`
	GuidedTour         []graph.GuidedTourStep   `json:"guided_tour,omitempty"`
	Architecture       *graph.ArchitectureGraph `json:"architecture,omitempty"`
	Warnings           []string                 `json:"warnings"`
}

type CreateArchitectureDiffRequest struct {
	RepositoryURL string `json:"repository_url"`
	BaseSHA       string `json:"base_sha"`
	HeadSHA       string `json:"head_sha"`
}

type CreateArchitectureDiffResponse struct {
	DiffID string            `json:"diff_id"`
	Status domain.DiffStatus `json:"status"`
	Cached bool              `json:"cached"`
}

type ArchitectureDiffStatusResponse struct {
	ID          string              `json:"id"`
	Status      domain.DiffStatus   `json:"status"`
	Stage       string              `json:"stage"`
	Repository  RepositorySummary   `json:"repository"`
	BaseSHA     string              `json:"base_sha"`
	HeadSHA     string              `json:"head_sha"`
	Error       AnalysisErrorField  `json:"error"`
	Summary     *domain.DiffSummary `json:"summary,omitempty"`
	CreatedAt   string              `json:"created_at"`
	CompletedAt *string             `json:"completed_at,omitempty"`
}

type ArchitectureDiffGraphResponse struct {
	DiffID       string             `json:"diff_id"`
	Repository   RepositorySummary  `json:"repository"`
	BaseSHA      string             `json:"base_sha"`
	HeadSHA      string             `json:"head_sha"`
	Language     string             `json:"language"`
	Granularity  string             `json:"granularity"`
	Status       domain.DiffStatus  `json:"status"`
	Summary      domain.DiffSummary `json:"summary"`
	Nodes        []diff.DiffNode    `json:"nodes"`
	Edges        []diff.DiffEdge    `json:"edges"`
	CycleChanges diff.CycleChanges  `json:"cycle_changes"`
	Warnings     []string           `json:"warnings"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}
