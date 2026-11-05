package githubclient

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

var (
	ownerRegex = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	repoRegex  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

type ParsedRepoURL struct {
	Owner   string
	Repo    string
	HTMLURL string
}

type ParsedPRURL struct {
	Owner      string
	Repo       string
	PullNumber int
	HTMLURL    string
}

// ParsePullRequestURL strictly validates a public GitHub pull request URL of the form:
// https://github.com/{owner}/{repo}/pull/{pull_number}
func ParsePullRequestURL(raw string) (ParsedPRURL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ParsedPRURL{}, invalidPRURLError("GitHubのPull Request URLを入力してください")
	}

	if strings.ContainsAny(trimmed, "?#\\") {
		return ParsedPRURL{}, invalidPRURLError("URLにクエリやフラグメントを含めることはできません")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return ParsedPRURL{}, invalidPRURLError("不正なURL形式です")
	}

	if u.Scheme != "https" {
		return ParsedPRURL{}, invalidPRURLError("https://github.com/{owner}/{repo}/pull/{number} 形式で指定してください")
	}
	if u.User != nil {
		return ParsedPRURL{}, invalidPRURLError("URLに認証情報を含めることはできません")
	}
	if u.Host != "github.com" || u.Port() != "" {
		return ParsedPRURL{}, invalidPRURLError("ホストは github.com のみ対応しています")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return ParsedPRURL{}, invalidPRURLError("追加パラメータのないPull Request URLを指定してください")
	}

	rawPath := u.EscapedPath()
	if rawPath == "" || strings.Contains(rawPath, "//") || strings.Contains(strings.ToLower(rawPath), "%2f") || strings.Contains(strings.ToLower(rawPath), "%5c") {
		return ParsedPRURL{}, invalidPRURLError("不正なPull Requestパスです")
	}

	cleanPath := strings.TrimPrefix(u.Path, "/")
	cleanPath = strings.TrimSuffix(cleanPath, "/")

	parts := strings.Split(cleanPath, "/")
	if len(parts) != 4 || parts[2] != "pull" {
		return ParsedPRURL{}, invalidPRURLError("https://github.com/{owner}/{repo}/pull/{number} の形式で指定してください")
	}

	owner := parts[0]
	repo := parts[1]
	if strings.HasSuffix(repo, ".git") {
		repo = strings.TrimSuffix(repo, ".git")
	}

	if !ownerRegex.MatchString(owner) {
		return ParsedPRURL{}, invalidPRURLError("GitHubのオーナー名が不正です")
	}
	if !repoRegex.MatchString(repo) || repo == "." || repo == ".." {
		return ParsedPRURL{}, invalidPRURLError("GitHubのリポジトリ名が不正です")
	}

	pullNumberStr := parts[3]
	pullNumber, err := strconv.Atoi(pullNumberStr)
	if err != nil || pullNumber <= 0 {
		return ParsedPRURL{}, invalidPRURLError("Pull Request番号が不正です")
	}

	return ParsedPRURL{
		Owner:      owner,
		Repo:       repo,
		PullNumber: pullNumber,
		HTMLURL:    fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, pullNumber),
	}, nil
}

// ParseRepositoryURL strictly validates a public GitHub repository URL of the form:
// https://github.com/{owner}/{repo} (allowing trailing "/" and ".git").
func ParseRepositoryURL(raw string) (ParsedRepoURL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ParsedRepoURL{}, invalidURLError("GitHubの公開リポジトリURLを入力してください")
	}

	if strings.ContainsAny(trimmed, "?#\\") {
		return ParsedRepoURL{}, invalidURLError("URLにクエリやフラグメントを含めることはできません")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return ParsedRepoURL{}, invalidURLError("不正なURL形式です")
	}

	if u.Scheme != "https" {
		return ParsedRepoURL{}, invalidURLError("https://github.com/{owner}/{repo} 形式で指定してください")
	}
	if u.User != nil {
		return ParsedRepoURL{}, invalidURLError("URLに認証情報を含めることはできません")
	}
	if u.Host != "github.com" || u.Port() != "" {
		return ParsedRepoURL{}, invalidURLError("ホストは github.com のみ対応しています")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return ParsedRepoURL{}, invalidURLError("追加パラメータのないリポジトリURLを指定してください")
	}

	// Check raw path to reject encoded traversal or double slashes
	rawPath := u.EscapedPath()
	if rawPath == "" || strings.Contains(rawPath, "//") || strings.Contains(strings.ToLower(rawPath), "%2f") || strings.Contains(strings.ToLower(rawPath), "%5c") {
		return ParsedRepoURL{}, invalidURLError("不正なリポジトリパスです")
	}

	cleanPath := strings.TrimPrefix(u.Path, "/")
	cleanPath = strings.TrimSuffix(cleanPath, "/")

	parts := strings.Split(cleanPath, "/")
	if len(parts) != 2 {
		return ParsedRepoURL{}, invalidURLError("https://github.com/{owner}/{repo} の形式で指定してください")
	}

	owner := parts[0]
	repo := parts[1]
	if strings.HasSuffix(repo, ".git") {
		repo = strings.TrimSuffix(repo, ".git")
	}

	if !ownerRegex.MatchString(owner) {
		return ParsedRepoURL{}, invalidURLError("GitHubのオーナー名が不正です")
	}
	if !repoRegex.MatchString(repo) || repo == "." || repo == ".." {
		return ParsedRepoURL{}, invalidURLError("GitHubのリポジトリ名が不正です")
	}

	return ParsedRepoURL{
		Owner:   owner,
		Repo:    repo,
		HTMLURL: "https://github.com/" + owner + "/" + repo,
	}, nil
}

func invalidURLError(msg string) *domain.AppError {
	return domain.NewAppError(domain.ErrCodeInvalidRepositoryURL, http.StatusBadRequest, msg, nil)
}

func invalidPRURLError(msg string) *domain.AppError {
	return domain.NewAppError(domain.ErrCodeInvalidPullRequestURL, http.StatusBadRequest, msg, nil)
}
