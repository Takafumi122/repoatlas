package githubclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

var shaRegex = regexp.MustCompile(`^[0-9a-f]{40}$`)

var defaultAllowedHosts = map[string]bool{
	"api.github.com":                       true,
	"codeload.github.com":                  true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

type RESTClientOptions struct {
	BaseURL          string
	Token            string
	HTTPClient       *http.Client
	Timeout          time.Duration
	AllowedHosts     map[string]bool
	AllowHTTPForTest bool
	MaxRetries       int
	RetryBaseDelay   time.Duration
}

type RESTClient struct {
	baseURL          string
	token            string
	httpClient       *http.Client
	allowedHosts     map[string]bool
	allowHTTPForTest bool
	maxRetries       int
	retryBaseDelay   time.Duration
}

func NewRESTClient(opts RESTClientOptions) *RESTClient {
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	allowed := make(map[string]bool, len(defaultAllowedHosts))
	for k, v := range defaultAllowedHosts {
		allowed[k] = v
	}
	for k, v := range opts.AllowedHosts {
		allowed[strings.ToLower(k)] = v
	}

	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2
	}
	retryBaseDelay := opts.RetryBaseDelay
	if retryBaseDelay <= 0 {
		retryBaseDelay = 200 * time.Millisecond
	}

	c := &RESTClient{
		baseURL:          baseURL,
		token:            strings.TrimSpace(opts.Token),
		allowedHosts:     allowed,
		allowHTTPForTest: opts.AllowHTTPForTest,
		maxRetries:       maxRetries,
		retryBaseDelay:   retryBaseDelay,
	}

	baseTransport := http.DefaultTransport
	if opts.HTTPClient != nil && opts.HTTPClient.Transport != nil {
		baseTransport = opts.HTTPClient.Transport
	}

	c.httpClient = &http.Client{
		Timeout:   timeout,
		Transport: baseTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if err := c.validateRequestTarget(req.URL); err != nil {
				return err
			}
			// Never forward Authorization header to a different host
			if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}

	return c
}

func (c *RESTClient) validateRequestTarget(u *url.URL) error {
	if u == nil {
		return errors.New("nil request URL")
	}
	if u.Scheme != "https" && !(c.allowHTTPForTest && u.Scheme == "http") {
		return fmt.Errorf("disallowed redirect scheme: %s", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if !c.allowedHosts[host] {
		return fmt.Errorf("disallowed redirect host: %s", host)
	}
	return nil
}

type githubRepoResponse struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Private       bool   `json:"private"`
	Visibility    string `json:"visibility"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	Size          int64  `json:"size"` // in KB
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type githubCommitResponse struct {
	SHA string `json:"sha"`
}

func (c *RESTClient) Resolve(ctx context.Context, owner, repo string) (RepositorySnapshot, error) {
	parsed, err := ParseRepositoryURL("https://github.com/" + owner + "/" + repo)
	if err != nil {
		return RepositorySnapshot{}, err
	}

	repoEndpoint := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, url.PathEscape(parsed.Owner), url.PathEscape(parsed.Repo))
	resp, err := c.doWithRetry(ctx, repoEndpoint, "application/vnd.github+json")
	if err != nil {
		return RepositorySnapshot{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return RepositorySnapshot{}, mapGitHubHTTPError(resp)
	}

	var repoMeta githubRepoResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&repoMeta); err != nil {
		return RepositorySnapshot{}, domain.NewAppError(
			domain.ErrCodeInternalError,
			http.StatusBadGateway,
			"GitHubメタデータの解析に失敗しました",
			err,
		)
	}

	if repoMeta.Private || (repoMeta.Visibility != "" && repoMeta.Visibility != "public") {
		return RepositorySnapshot{}, domain.NewAppError(
			domain.ErrCodeRepositoryNotPublic,
			http.StatusUnprocessableEntity,
			"非公開リポジトリは解析できません。公開リポジトリを指定してください",
			nil,
		)
	}

	// GitHub repository size is reported in KB; 100 MiB = 102,400 KB
	if repoMeta.Size > 102400 {
		return RepositorySnapshot{}, domain.NewAppError(
			domain.ErrCodeRepoTooLarge,
			http.StatusRequestEntityTooLarge,
			"リポジトリサイズが大きすぎます（上限100MiB）",
			nil,
		)
	}

	defaultBranch := strings.TrimSpace(repoMeta.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	commitEndpoint := fmt.Sprintf("%s/repos/%s/%s/commits/%s",
		c.baseURL,
		url.PathEscape(parsed.Owner),
		url.PathEscape(parsed.Repo),
		url.PathEscape(defaultBranch),
	)
	commitResp, err := c.doWithRetry(ctx, commitEndpoint, "application/vnd.github+json")
	if err != nil {
		return RepositorySnapshot{}, err
	}
	defer commitResp.Body.Close()

	if commitResp.StatusCode != http.StatusOK {
		return RepositorySnapshot{}, mapGitHubHTTPError(commitResp)
	}

	var commitMeta githubCommitResponse
	if err := json.NewDecoder(io.LimitReader(commitResp.Body, 1<<20)).Decode(&commitMeta); err != nil {
		return RepositorySnapshot{}, domain.NewAppError(
			domain.ErrCodeInternalError,
			http.StatusBadGateway,
			"GitHubコミット情報の解析に失敗しました",
			err,
		)
	}

	sha := strings.ToLower(strings.TrimSpace(commitMeta.SHA))
	if !shaRegex.MatchString(sha) {
		return RepositorySnapshot{}, domain.NewAppError(
			domain.ErrCodeInternalError,
			http.StatusBadGateway,
			"GitHubから有効なコミットSHAを取得できませんでした",
			nil,
		)
	}

	ownerLogin := repoMeta.Owner.Login
	if ownerLogin == "" {
		ownerLogin = parsed.Owner
	}
	repoName := repoMeta.Name
	if repoName == "" {
		repoName = parsed.Repo
	}
	htmlURL := repoMeta.HTMLURL
	if htmlURL == "" {
		htmlURL = "https://github.com/" + ownerLogin + "/" + repoName
	}

	return RepositorySnapshot{
		GitHubID:      repoMeta.ID,
		Owner:         ownerLogin,
		Name:          repoName,
		DefaultBranch: defaultBranch,
		CommitSHA:     sha,
		HTMLURL:       htmlURL,
	}, nil
}

func (c *RESTClient) DownloadArchive(ctx context.Context, snapshot RepositorySnapshot) (io.ReadCloser, error) {
	parsed, err := ParseRepositoryURL("https://github.com/" + snapshot.Owner + "/" + snapshot.Name)
	if err != nil {
		return nil, err
	}
	if !shaRegex.MatchString(snapshot.CommitSHA) {
		return nil, domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "不正なコミットSHAです", nil)
	}

	archiveEndpoint := fmt.Sprintf("%s/repos/%s/%s/zipball/%s",
		c.baseURL,
		url.PathEscape(parsed.Owner),
		url.PathEscape(parsed.Repo),
		url.PathEscape(snapshot.CommitSHA),
	)

	resp, err := c.doWithRetry(ctx, archiveEndpoint, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, mapGitHubHTTPError(resp)
	}

	if resp.ContentLength > MaxCompressedArchiveBytes {
		resp.Body.Close()
		return nil, domain.NewAppError(
			domain.ErrCodeRepoTooLarge,
			http.StatusRequestEntityTooLarge,
			"アーカイブの圧縮サイズが上限（50MiB）を超えています",
			nil,
		)
	}

	return resp.Body, nil
}

func (c *RESTClient) doWithRetry(ctx context.Context, endpoint string, accept string) (*http.Response, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, domain.NewAppError(domain.ErrCodeInternalError, http.StatusInternalServerError, "リクエストURLの構築に失敗しました", err)
	}
	if err := c.validateRequestTarget(u); err != nil {
		return nil, domain.NewAppError(domain.ErrCodeInvalidRepositoryURL, http.StatusBadGateway, "許可されていない接続先へのアクセスを拒否しました", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.retryBaseDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return nil, mapTransportError(ctx.Err())
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, domain.NewAppError(domain.ErrCodeInternalError, http.StatusInternalServerError, "HTTPリクエスト作成に失敗しました", err)
		}
		req.Header.Set("User-Agent", "RepoAtlas/1.0")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, mapTransportError(err)
			}
			// Do not retry redirect validation errors
			if strings.Contains(err.Error(), "disallowed redirect") || strings.Contains(err.Error(), "too many redirects") {
				return nil, domain.NewAppError(domain.ErrCodeInvalidRepositoryURL, http.StatusBadGateway, "不正なリダイレクト先を検出したため中断しました", err)
			}
			continue
		}

		// Never retry 403/429 rate limits; only retry transient 502/503/504
		if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout {
			resp.Body.Close()
			lastErr = fmt.Errorf("github upstream status %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	return nil, mapTransportError(lastErr)
}

func mapTransportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.NewAppError(domain.ErrCodeInternalError, http.StatusGatewayTimeout, "GitHub APIへの接続がタイムアウトしました", err)
	}
	if errors.Is(err, context.Canceled) {
		return domain.NewAppError(domain.ErrCodeInternalError, http.StatusRequestTimeout, "リクエストがキャンセルされました", err)
	}
	return domain.NewAppError(domain.ErrCodeInternalError, http.StatusBadGateway, "GitHub APIとの通信に失敗しました", err)
}

func mapGitHubHTTPError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusNotFound:
		return domain.NewAppError(
			domain.ErrCodeRepositoryNotFound,
			http.StatusNotFound,
			"指定されたGitHub公開リポジトリまたはブランチが見つかりません",
			nil,
		)
	case http.StatusTooManyRequests, http.StatusForbidden:
		remaining := resp.Header.Get("X-RateLimit-Remaining")
		retryAfter := resp.Header.Get("Retry-After")
		resetHeader := resp.Header.Get("X-RateLimit-Reset")

		if resp.StatusCode == http.StatusTooManyRequests || remaining == "0" || retryAfter != "" {
			msg := "GitHub APIのレート制限に達しました。しばらく待ってから再試行してください"
			var resetTime *time.Time
			if resetHeader != "" {
				if unixSec, err := strconv.ParseInt(resetHeader, 10, 64); err == nil {
					t := time.Unix(unixSec, 0).UTC()
					resetTime = &t
					msg = fmt.Sprintf("GitHub APIのレート制限に達しました（解除予定: %s UTC）", t.Format("15:04:05"))
				}
			} else if retryAfter != "" {
				if sec, err := strconv.Atoi(retryAfter); err == nil {
					t := time.Now().UTC().Add(time.Duration(sec) * time.Second)
					resetTime = &t
					msg = fmt.Sprintf("GitHub APIのレート制限に達しました（約%d秒後に再試行可能）", sec)
				}
			}
			appErr := domain.NewAppError(domain.ErrCodeGitHubRateLimited, http.StatusTooManyRequests, msg, nil)
			appErr.RetryAfter = resetTime
			return appErr
		}

		return domain.NewAppError(
			domain.ErrCodeRepositoryNotPublic,
			http.StatusUnprocessableEntity,
			"このリポジトリにはアクセスできません（非公開またはアクセス制限）",
			nil,
		)
	default:
		return domain.NewAppError(
			domain.ErrCodeInternalError,
			http.StatusBadGateway,
			fmt.Sprintf("GitHub APIから予期しないステータス（%d）が返されました", resp.StatusCode),
			nil,
		)
	}
}
