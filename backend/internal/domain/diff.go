package domain

import "time"

type DiffStatus string

const (
	DiffStatusQueued    DiffStatus = "queued"
	DiffStatusRunning   DiffStatus = "running"
	DiffStatusCompleted DiffStatus = "completed"
	DiffStatusFailed    DiffStatus = "failed"
)

type NodeChangeType string

const (
	NodeChangeAdded     NodeChangeType = "added"
	NodeChangeRemoved   NodeChangeType = "removed"
	NodeChangeModified  NodeChangeType = "modified"
	NodeChangeUnchanged NodeChangeType = "unchanged"
)

type EdgeChangeType string

const (
	EdgeChangeAdded     EdgeChangeType = "added"
	EdgeChangeRemoved   EdgeChangeType = "removed"
	EdgeChangeUnchanged EdgeChangeType = "unchanged"
)

type MetricValue struct {
	Base float64 `json:"base"`
	Head float64 `json:"head"`
	Diff float64 `json:"diff"`
}

type ArchitectureMetricsDiff struct {
	TotalNodes       MetricValue `json:"total_nodes"`
	TotalEdges       MetricValue `json:"total_edges"`
	AddedNodes       int         `json:"added_nodes"`
	RemovedNodes     int         `json:"removed_nodes"`
	ModifiedNodes    int         `json:"modified_nodes"`
	AddedEdges       int         `json:"added_edges"`
	RemovedEdges     int         `json:"removed_edges"`
	CyclicSCCCount   MetricValue `json:"cyclic_scc_count"`
	CyclicNodesCount MetricValue `json:"cyclic_nodes_count"`
	AvgInDegree      MetricValue `json:"avg_in_degree"`
	AvgOutDegree     MetricValue `json:"avg_out_degree"`
	MaxInDegree      MetricValue `json:"max_in_degree"`
	MaxOutDegree     MetricValue `json:"max_out_degree"`
}

type DiffSummary struct {
	BaseCommitSHA      string                  `json:"base_commit_sha"`
	HeadCommitSHA      string                  `json:"head_commit_sha"`
	Language           string                  `json:"language"`
	Granularity        string                  `json:"granularity"`
	TotalBaseNodes     int                     `json:"total_base_nodes"`
	TotalHeadNodes     int                     `json:"total_head_nodes"`
	TotalBaseEdges     int                     `json:"total_base_edges"`
	TotalHeadEdges     int                     `json:"total_head_edges"`
	AddedNodesCount    int                     `json:"added_nodes_count"`
	RemovedNodesCount  int                     `json:"removed_nodes_count"`
	ModifiedNodesCount int                     `json:"modified_nodes_count"`
	AddedEdgesCount    int                     `json:"added_edges_count"`
	RemovedEdgesCount  int                     `json:"removed_edges_count"`
	BaseCyclicSCCs     int                     `json:"base_cyclic_sccs"`
	HeadCyclicSCCs     int                     `json:"head_cyclic_sccs"`
	Metrics            ArchitectureMetricsDiff `json:"metrics"`
}

type ArchitectureDiffAnalysis struct {
	ID                   string         `json:"id"`
	RepositoryID         int64          `json:"-"`
	Repository           Repository     `json:"repository"`
	BaseSHA              string         `json:"base_sha"`
	HeadSHA              string         `json:"head_sha"`
	BaseAnalysisID       *string        `json:"base_analysis_id,omitempty"`
	HeadAnalysisID       *string        `json:"head_analysis_id,omitempty"`
	AnalyzerVersion      string         `json:"-"`
	DiffAlgorithmVersion string         `json:"-"`
	Status               DiffStatus     `json:"status"`
	Stage                string         `json:"stage"`
	AttemptCount         int            `json:"-"`
	LeaseOwner           *string        `json:"-"`
	LeaseExpiresAt       *time.Time     `json:"-"`
	ErrorCode            *string        `json:"-"`
	ErrorMessage         *string        `json:"-"`
	DiffSummary          *DiffSummary   `json:"diff_summary,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	CompletedAt          *time.Time     `json:"completed_at,omitempty"`
}
