package githubclient

import (
	"context"
	"io"
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
	DownloadArchive(ctx context.Context, snapshot RepositorySnapshot) (io.ReadCloser, error)
}
