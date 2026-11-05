package githubclient

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

// FixtureFallbackClient wraps a primary Client (such as RESTClient) and intercepts
// requests for owner == "fixture" to serve local testdata fixtures without external GitHub access.
type FixtureFallbackClient struct {
	primary    Client
	fixtureDir string
}

func NewFixtureFallbackClient(primary Client, fixtureDir string) *FixtureFallbackClient {
	return &FixtureFallbackClient{
		primary:    primary,
		fixtureDir: fixtureDir,
	}
}

func (c *FixtureFallbackClient) Resolve(ctx context.Context, owner, repo string) (RepositorySnapshot, error) {
	if strings.EqualFold(owner, "fixture") && c.fixtureDir != "" {
		dirName := strings.ReplaceAll(repo, "-", "_")
		targetDir := filepath.Join(c.fixtureDir, dirName)
		info, err := os.Stat(targetDir)
		if err != nil || !info.IsDir() {
			return RepositorySnapshot{}, domain.NewAppError(
				domain.ErrCodeRepositoryNotFound,
				http.StatusNotFound,
				"指定されたフィクスチャリポジトリが見つかりません",
				nil,
			)
		}
		return RepositorySnapshot{
			GitHubID:      900001,
			Owner:         "fixture",
			Name:          repo,
			DefaultBranch: "main",
			CommitSHA:     "1111111111222222222233333333334444444444",
			HTMLURL:       "https://github.com/fixture/" + repo,
		}, nil
	}
	return c.primary.Resolve(ctx, owner, repo)
}

func (c *FixtureFallbackClient) ResolveCommit(ctx context.Context, owner, repo, commitSHA string) (RepositorySnapshot, error) {
	if strings.EqualFold(owner, "fixture") && c.fixtureDir != "" {
		snap, err := c.Resolve(ctx, owner, repo)
		if err != nil {
			return RepositorySnapshot{}, err
		}
		snap.CommitSHA = commitSHA
		return snap, nil
	}
	return c.primary.ResolveCommit(ctx, owner, repo, commitSHA)
}

func (c *FixtureFallbackClient) GetPullRequest(ctx context.Context, owner, repo string, pullNumber int) (domain.PullRequestSnapshot, error) {
	if strings.EqualFold(owner, "fixture") && c.fixtureDir != "" {
		snap, err := c.Resolve(ctx, owner, repo)
		if err != nil {
			return domain.PullRequestSnapshot{}, err
		}
		return domain.PullRequestSnapshot{
			Repository: domain.Repository{
				GitHubRepositoryID: snap.GitHubID,
				Owner:              snap.Owner,
				Name:               snap.Name,
				DefaultBranch:      snap.DefaultBranch,
				HTMLURL:            snap.HTMLURL,
			},
			PullNumber: pullNumber,
			Title:      "Fixture PR for " + repo,
			State:      "open",
			BaseSHA:    "0000000000111111111122222222223333333333",
			HeadSHA:    "1111111111222222222233333333334444444444",
			BaseRef:    "main",
			HeadRef:    "feature-pr",
		}, nil
	}
	return c.primary.GetPullRequest(ctx, owner, repo, pullNumber)
}

func (c *FixtureFallbackClient) GetPullRequestFiles(ctx context.Context, owner, repo string, pullNumber int, maxFiles int) ([]domain.PullRequestFile, error) {
	if strings.EqualFold(owner, "fixture") && c.fixtureDir != "" {
		return []domain.PullRequestFile{
			{
				Path:       "app/models.py",
				Status:     domain.PRFileModified,
				Additions:  5,
				Deletions:  2,
				Changes:    7,
				IsPython:   true,
				IsAnalyzed: true,
			},
			{
				Path:             "README.md",
				Status:           domain.PRFileModified,
				Additions:        3,
				Deletions:        0,
				Changes:          3,
				IsPython:         false,
				IsAnalyzed:       false,
				UnanalyzedReason: "non_python_file",
			},
		}, nil
	}
	return c.primary.GetPullRequestFiles(ctx, owner, repo, pullNumber, maxFiles)
}

func (c *FixtureFallbackClient) DownloadArchive(ctx context.Context, snapshot RepositorySnapshot) (io.ReadCloser, error) {
	if strings.EqualFold(snapshot.Owner, "fixture") && c.fixtureDir != "" {
		dirName := strings.ReplaceAll(snapshot.Name, "-", "_")
		targetDir := filepath.Join(c.fixtureDir, dirName)
		zipBytes, err := buildZipFromDirectory(targetDir, fmt.Sprintf("%s-%s-%s", snapshot.Owner, snapshot.Name, snapshot.CommitSHA[:7]))
		if err != nil {
			return nil, domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusInternalServerError, "フィクスチャZIPの生成に失敗しました", err)
		}
		return io.NopCloser(bytes.NewReader(zipBytes)), nil
	}
	return c.primary.DownloadArchive(ctx, snapshot)
}

func buildZipFromDirectory(rootDir string, prefix string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.Walk(rootDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootDir, p)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		entryName := prefix + "/" + filepath.ToSlash(rel)
		w, err := zw.Create(entryName)
		if err != nil {
			return err
		}
		_, err = w.Write(content)
		return err
	})
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
