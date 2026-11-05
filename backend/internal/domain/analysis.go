package domain

import (
	"errors"
	"fmt"
	"time"
)

var ErrLeaseLost = errors.New("worker lease ownership lost or expired")

type AnalysisStatus string

const (
	StatusQueued    AnalysisStatus = "queued"
	StatusRunning   AnalysisStatus = "running"
	StatusCompleted AnalysisStatus = "completed"
	StatusFailed    AnalysisStatus = "failed"
)

type AnalysisStage string

const (
	StageQueued            AnalysisStage = "queued"
	StageFetching          AnalysisStage = "fetching"
	StageIndexing          AnalysisStage = "indexing"
	StageParsing           AnalysisStage = "parsing"
	StageResolving         AnalysisStage = "resolving"
	StageResolvingMetadata AnalysisStage = "resolving_metadata"
	StageAnalyzingHead     AnalysisStage = "analyzing_head"
	StageAnalyzingImpact   AnalysisStage = "analyzing_impact"
	StagePersisting        AnalysisStage = "persisting"
	StageCompleted         AnalysisStage = "completed"
	StageFailed            AnalysisStage = "failed"
)

type Repository struct {
	ID                 int64     `json:"-"`
	GitHubRepositoryID int64     `json:"-"`
	Owner              string    `json:"owner"`
	Name               string    `json:"name"`
	DefaultBranch      string    `json:"-"`
	HTMLURL            string    `json:"html_url"`
	CreatedAt          time.Time `json:"-"`
	UpdatedAt          time.Time `json:"-"`
}

type AnalysisRun struct {
	ID                     string         `json:"id"`
	RepositoryID           int64          `json:"-"`
	Repository             Repository     `json:"repository"`
	CommitSHA              string         `json:"commit_sha"`
	AnalyzerVersion        string         `json:"-"`
	Language               string         `json:"language,omitempty"`
	GraphGranularity       string         `json:"graph_granularity,omitempty"`
	Status                 AnalysisStatus `json:"status"`
	Stage                  AnalysisStage  `json:"stage"`
	AttemptCount           int            `json:"-"`
	LeaseOwner             *string        `json:"-"`
	LeaseExpiresAt         *time.Time     `json:"-"`
	ErrorCode              *string        `json:"-"`
	ErrorMessage           *string        `json:"-"`
	FilesCount             int            `json:"-"`
	InternalEdgesCount     int            `json:"-"`
	ExternalImportsCount   int            `json:"-"`
	UnresolvedImportsCount int            `json:"-"`
	CreatedAt              time.Time      `json:"created_at"`
	StartedAt              *time.Time     `json:"-"`
	CompletedAt            *time.Time     `json:"completed_at"`
}

type ErrorCode string

const (
	ErrCodeInvalidRepositoryURL  ErrorCode = "INVALID_REPOSITORY_URL"
	ErrCodeInvalidPullRequestURL ErrorCode = "INVALID_PULL_REQUEST_URL"
	ErrCodeRepositoryNotFound    ErrorCode = "REPOSITORY_NOT_FOUND"
	ErrCodePullRequestNotFound   ErrorCode = "PULL_REQUEST_NOT_FOUND"
	ErrCodeRepositoryNotPublic   ErrorCode = "REPOSITORY_NOT_PUBLIC"
	ErrCodeRepoTooLarge          ErrorCode = "REPO_TOO_LARGE"
	ErrCodePRTooManyFiles        ErrorCode = "PR_TOO_MANY_FILES"
	ErrCodePRHeadSHAChanged      ErrorCode = "PR_HEAD_SHA_CHANGED"
	ErrCodeGitHubRateLimited     ErrorCode = "GITHUB_RATE_LIMITED"
	ErrCodeRateLimited           ErrorCode = "RATE_LIMITED"
	ErrCodeQueueLimitExceeded    ErrorCode = "QUEUE_LIMIT_EXCEEDED"
	ErrCodeAnalysisNotReady      ErrorCode = "ANALYSIS_NOT_READY"
	ErrCodeAnalysisNotFound      ErrorCode = "ANALYSIS_NOT_FOUND"
	ErrCodeInvalidArchive        ErrorCode = "INVALID_ARCHIVE"
	ErrCodeInternalError         ErrorCode = "INTERNAL_ERROR"
)

type AppError struct {
	Code       ErrorCode
	Message    string
	HTTPStatus int
	RetryAfter *time.Time
	Err        error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NewAppError(code ErrorCode, httpStatus int, message string, err error) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		Err:        err,
	}
}
