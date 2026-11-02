package httpapi

import (
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
	ID          string                `json:"id"`
	Status      domain.AnalysisStatus `json:"status"`
	Stage       domain.AnalysisStage  `json:"stage"`
	Repository  RepositorySummary     `json:"repository"`
	CommitSHA   string                `json:"commit_sha"`
	Error       AnalysisErrorField    `json:"error"`
	CreatedAt   string                `json:"created_at"`
	CompletedAt *string               `json:"completed_at"`
}

type GraphSummary struct {
	Files             int `json:"files"`
	InternalEdges     int `json:"internal_edges"`
	ExternalImports   int `json:"external_imports"`
	UnresolvedImports int `json:"unresolved_imports"`
}

type GraphNode struct {
	ID              string  `json:"id"`
	Path            string  `json:"path"`
	Functions       int     `json:"functions"`
	Classes         int     `json:"classes"`
	InDegree        int     `json:"in_degree"`
	OutDegree       int     `json:"out_degree"`
	IsEntrypoint    bool    `json:"is_entrypoint"`
	ImportanceScore float64 `json:"importance_score"`
	GitHubURL       string  `json:"github_url"`
}

type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

type GraphResponse struct {
	Demo               bool                     `json:"demo,omitempty"`
	AnalysisID         string                   `json:"analysis_id"`
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

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}
