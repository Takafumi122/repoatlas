package githubclient

import (
	"context"
	"io"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

type RepositorySnapshot struct {
	GitHubID      int64
	Owner         string
	Name          string
	DefaultBranch string
	CommitSHA     string
	HTMLURL       string
}

type Client interface {
	Resolve(ctx context.Context, owner, repo string) (RepositorySnapshot, error)
	ResolveCommit(ctx context.Context, owner, repo, commitSHA string) (RepositorySnapshot, error)
	GetPullRequest(ctx context.Context, owner, repo string, pullNumber int) (domain.PullRequestSnapshot, error)
	GetPullRequestFiles(ctx context.Context, owner, repo string, pullNumber int, maxFiles int) ([]domain.PullRequestFile, error)
	DownloadArchive(ctx context.Context, snapshot RepositorySnapshot) (io.ReadCloser, error)
}
