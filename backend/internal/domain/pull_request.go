package domain

import (
	"time"
)

type PullRequestFileStatus string

const (
	PRFileAdded    PullRequestFileStatus = "added"
	PRFileModified PullRequestFileStatus = "modified"
	PRFileRemoved  PullRequestFileStatus = "removed"
	PRFileRenamed  PullRequestFileStatus = "renamed"
)

type PullRequestFile struct {
	ID               int64                 `json:"id"`
	PRAnalysisID     string                `json:"pr_analysis_id"`
	Path             string                `json:"path"`
	PreviousPath     string                `json:"previous_path,omitempty"`
	Status           PullRequestFileStatus `json:"status"`
	Additions        int                   `json:"additions"`
	Deletions        int                   `json:"deletions"`
	Changes          int                   `json:"changes"`
	IsPython         bool                  `json:"is_python"`
	IsAnalyzed       bool                  `json:"is_analyzed"`
	UnanalyzedReason string                `json:"unanalyzed_reason,omitempty"`
}

type PRImpactSummary struct {
	ChangedFilesCount    int    `json:"changed_files_count"`
	DirectImpactCount    int    `json:"direct_impact_count"`
	IndirectImpactCount  int    `json:"indirect_impact_count"`
	TotalAffectedCount   int    `json:"total_affected_count"`
	UnanalyzedFilesCount int    `json:"unanalyzed_files_count"`
	AnalysisCompleteness string `json:"analysis_completeness"` // "complete" or "partial"
}

type PullRequestSnapshot struct {
	Repository Repository `json:"repository"`
	PullNumber int        `json:"pull_number"`
	Title      string     `json:"title"`
	State      string     `json:"state"`
	BaseSHA    string     `json:"base_sha"`
	HeadSHA    string     `json:"head_sha"`
	BaseRef    string     `json:"base_ref"`
	HeadRef    string     `json:"head_ref"`
}

type PullRequestAnalysis struct {
	ID                   string         `json:"id"`
	RepositoryID         int64          `json:"-"`
	Repository           Repository     `json:"repository"`
	PullNumber           int            `json:"pull_number"`
	Title                string         `json:"title"`
	State                string         `json:"state"`
	BaseSHA              string         `json:"base_sha"`
	HeadSHA              string         `json:"head_sha"`
	BaseRef              string         `json:"base_ref"`
	HeadRef              string         `json:"head_ref"`
	AnalyzerVersion      string         `json:"analyzer_version"`
	Status               AnalysisStatus `json:"status"`
	Stage                AnalysisStage  `json:"stage"`
	AttemptCount         int            `json:"-"`
	LeaseOwner           *string        `json:"-"`
	LeaseExpiresAt       *time.Time     `json:"-"`
	HeadAnalysisID       *string        `json:"head_analysis_id,omitempty"`
	BaseAnalysisID       *string        `json:"base_analysis_id,omitempty"`
	ErrorCode            *string        `json:"error_code,omitempty"`
	ErrorMessage         *string        `json:"error_message,omitempty"`
	ChangedFilesCount    int            `json:"changed_files_count"`
	DirectImpactCount    int            `json:"direct_impact_count"`
	IndirectImpactCount  int            `json:"indirect_impact_count"`
	TotalAffectedCount   int            `json:"total_affected_count"`
	UnanalyzedFilesCount int            `json:"unanalyzed_files_count"`
	AnalysisCompleteness string         `json:"analysis_completeness"`
	CreatedAt            time.Time      `json:"created_at"`
	StartedAt            *time.Time     `json:"started_at,omitempty"`
	CompletedAt          *time.Time     `json:"completed_at,omitempty"`
}

type PRNodeVisualRole string

const (
	PRVisualRoleChanged        PRNodeVisualRole = "changed"
	PRVisualRoleDirectImpact   PRNodeVisualRole = "direct_impact"
	PRVisualRoleIndirectImpact PRNodeVisualRole = "indirect_impact"
	PRVisualRoleUnaffected     PRNodeVisualRole = "unaffected"
	PRVisualRoleUnanalyzed     PRNodeVisualRole = "unanalyzed"
)

type PRAffectedFile struct {
	NodeID                     string           `json:"node_id"`
	Path                       string           `json:"path"`
	Depth                      int              `json:"depth"`
	ImpactType                 string           `json:"impact_type"` // "direct" or "indirect"
	SourceChangedFiles         []string         `json:"source_changed_files"`
	RepresentativePath         []string         `json:"representative_dependency_path"`
	InDegree                   int              `json:"in_degree"`
	OutDegree                  int              `json:"out_degree"`
	ImportanceScore            float64          `json:"importance_score"`
	VisualRole                 PRNodeVisualRole `json:"visual_role"`
}
