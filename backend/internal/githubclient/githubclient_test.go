package githubclient

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

func TestParseRepositoryURL(t *testing.T) {
	validCases := []struct {
		name     string
		input    string
		wantOwn  string
		wantRepo string
		wantHTML string
	}{
		{
			name:     "standard url",
			input:    "https://github.com/psf/requests",
			wantOwn:  "psf",
			wantRepo: "requests",
			wantHTML: "https://github.com/psf/requests",
		},
		{
			name:     "trailing slash",
			input:    "https://github.com/pallets/flask/",
			wantOwn:  "pallets",
			wantRepo: "flask",
			wantHTML: "https://github.com/pallets/flask",
		},
		{
			name:     "trailing .git",
			input:    "https://github.com/tiangolo/fastapi.git",
			wantOwn:  "tiangolo",
			wantRepo: "fastapi",
			wantHTML: "https://github.com/tiangolo/fastapi",
		},
		{
			name:     "trailing .git and slash with dots/hyphens",
			input:    "  https://github.com/org-1/my_repo.v2.git/  ",
			wantOwn:  "org-1",
			wantRepo: "my_repo.v2",
			wantHTML: "https://github.com/org-1/my_repo.v2",
		},
	}

	for _, tc := range validCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRepositoryURL(tc.input)
			if err != nil {
				t.Fatalf("expected valid URL %q, got error: %v", tc.input, err)
			}
			if got.Owner != tc.wantOwn || got.Repo != tc.wantRepo || got.HTMLURL != tc.wantHTML {
				t.Fatalf("got %+v, want owner=%s repo=%s html=%s", got, tc.wantOwn, tc.wantRepo, tc.wantHTML)
			}
		})
	}

	invalidCases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"http scheme", "http://github.com/psf/requests"},
		{"evil host suffix", "https://github.com.evil.example/psf/requests"},
		{"other host", "https://gitlab.com/psf/requests"},
		{"with port", "https://github.com:8443/psf/requests"},
		{"with credentials", "https://user:token@github.com/psf/requests"},
		{"with query", "https://github.com/psf/requests?tab=readme"},
		{"with fragment", "https://github.com/psf/requests#readme"},
		{"extra path", "https://github.com/psf/requests/tree/main"},
		{"only owner", "https://github.com/psf"},
		{"path traversal", "https://github.com/psf/../requests"},
		{"encoded slash", "https://github.com/psf/req%2fuests"},
		{"invalid owner leading hyphen", "https://github.com/-badowner/requests"},
		{"invalid owner trailing hyphen", "https://github.com/badowner-/requests"},
		{"invalid owner underscore", "https://github.com/bad_owner/requests"},
		{"dot repo", "https://github.com/psf/."},
		{"dotdot repo", "https://github.com/psf/.."},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRepositoryURL(tc.input)
			if err == nil {
				t.Fatalf("expected error for invalid URL %q, got nil", tc.input)
			}
			var appErr *domain.AppError
			if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeInvalidRepositoryURL {
				t.Fatalf("expected ErrCodeInvalidRepositoryURL, got %v", err)
			}
		})
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestRESTClient(opts RESTClientOptions, handler http.HandlerFunc) *RESTClient {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		resp := rec.Result()
		resp.Request = req
		return resp, nil
	})
	opts.BaseURL = "https://api.github.com"
	opts.HTTPClient = &http.Client{Transport: rt}
	return NewRESTClient(opts)
}

func TestRESTClient_ResolveAndErrors(t *testing.T) {
	const validSHA = "0123456789abcdef0123456789abcdef01234567"

	t.Run("200 success", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/psf/requests", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":12345,"name":"requests","private":false,"visibility":"public","default_branch":"main","html_url":"https://github.com/psf/requests","size":2048,"owner":{"login":"psf"}}`)
		})
		mux.HandleFunc("/repos/psf/requests/commits/main", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"sha":"%s"}`, validSHA)
		})

		client := newTestRESTClient(RESTClientOptions{}, mux.ServeHTTP)

		snap, err := client.Resolve(context.Background(), "psf", "requests")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if snap.GitHubID != 12345 || snap.CommitSHA != validSHA || snap.DefaultBranch != "main" {
			t.Fatalf("unexpected snapshot: %+v", snap)
		}
	})

	t.Run("404 not found", func(t *testing.T) {
		client := newTestRESTClient(RESTClientOptions{}, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})

		_, err := client.Resolve(context.Background(), "psf", "missing")
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeRepositoryNotFound {
			t.Fatalf("expected REPOSITORY_NOT_FOUND, got %v", err)
		}
	})

	t.Run("private repository rejected", func(t *testing.T) {
		client := newTestRESTClient(RESTClientOptions{}, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"id":99,"name":"secret","private":true,"default_branch":"main","owner":{"login":"psf"}}`)
		})

		_, err := client.Resolve(context.Background(), "psf", "secret")
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeRepositoryNotPublic {
			t.Fatalf("expected REPOSITORY_NOT_PUBLIC, got %v", err)
		}
	})

	t.Run("403/429 rate limit no retry loop", func(t *testing.T) {
		var calls int32
		client := newTestRESTClient(RESTClientOptions{MaxRetries: 3}, func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1793400000")
			w.WriteHeader(http.StatusForbidden)
		})

		_, err := client.Resolve(context.Background(), "psf", "requests")
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeGitHubRateLimited {
			t.Fatalf("expected GITHUB_RATE_LIMITED, got %v", err)
		}
		if atomic.LoadInt32(&calls) != 1 {
			t.Fatalf("expected exactly 1 call on rate limit, got %d", calls)
		}
	})

	t.Run("disallowed redirect host blocked", func(t *testing.T) {
		client := newTestRESTClient(RESTClientOptions{}, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example.com/archive.zip", http.StatusFound)
		})

		_, err := client.DownloadArchive(context.Background(), RepositorySnapshot{
			Owner:     "psf",
			Name:      "requests",
			CommitSHA: validSHA,
		})
		if err == nil {
			t.Fatal("expected redirect to evil host to be blocked")
		}
	})

	t.Run("allowed redirect host succeeds and strips Authorization header", func(t *testing.T) {
		var redirectedAuth string
		client := newTestRESTClient(RESTClientOptions{Token: "ghp_secret_token"}, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Host == "api.github.com" {
				http.Redirect(w, r, "https://codeload.github.com/psf/requests/legacy.zip/main", http.StatusFound)
				return
			}
			redirectedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("zip-bytes"))
		})

		rc, err := client.DownloadArchive(context.Background(), RepositorySnapshot{
			Owner:     "psf",
			Name:      "requests",
			CommitSHA: validSHA,
		})
		if err != nil {
			t.Fatalf("unexpected error on allowed redirect: %v", err)
		}
		rc.Close()
		if redirectedAuth != "" {
			t.Fatalf("expected Authorization header to be stripped on cross-host redirect, got %q", redirectedAuth)
		}
	})

	t.Run("timeout handled cleanly", func(t *testing.T) {
		client := newTestRESTClient(RESTClientOptions{
			Timeout:    30 * time.Millisecond,
			MaxRetries: 0,
		}, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(150 * time.Millisecond):
				w.WriteHeader(http.StatusOK)
			}
		})

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := client.Resolve(ctx, "psf", "requests")
		if err == nil {
			t.Fatal("expected timeout error")
		}
	})
}

func TestExtractPythonFilesFromZip(t *testing.T) {
	t.Run("valid zip extracts only regular .py files", func(t *testing.T) {
		buf := buildZip(t, []zipEntrySpec{
			{name: "psf-requests-abcdef/README.md", body: "# readme"},
			{name: "psf-requests-abcdef/src/app/__init__.py", body: "from .core import run\n"},
			{name: "psf-requests-abcdef/src/app/core.py", body: "def run():\n    return 42\n"},
		})

		files, err := ExtractPythonFilesFromZip(bytes.NewReader(buf), DefaultArchiveLimits())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 2 {
			t.Fatalf("expected 2 python files, got %d", len(files))
		}
		if files[0].Path != "src/app/__init__.py" || files[1].Path != "src/app/core.py" {
			t.Fatalf("unexpected extracted paths: %+v", files)
		}
	})

	t.Run("rejects zip slip traversal", func(t *testing.T) {
		buf := buildZip(t, []zipEntrySpec{
			{name: "repo-sha/../../etc/evil.py", body: "import os\n"},
		})
		_, err := ExtractPythonFilesFromZip(bytes.NewReader(buf), DefaultArchiveLimits())
		assertAppErrorCode(t, err, domain.ErrCodeInvalidArchive)
	})

	t.Run("rejects absolute path", func(t *testing.T) {
		buf := buildZip(t, []zipEntrySpec{
			{name: "/etc/passwd.py", body: "x = 1\n"},
		})
		_, err := ExtractPythonFilesFromZip(bytes.NewReader(buf), DefaultArchiveLimits())
		assertAppErrorCode(t, err, domain.ErrCodeInvalidArchive)
	})

	t.Run("rejects symlink entry", func(t *testing.T) {
		buf := buildZip(t, []zipEntrySpec{
			{name: "repo-sha/link.py", body: "../secret.py", mode: os.ModeSymlink | 0777},
		})
		_, err := ExtractPythonFilesFromZip(bytes.NewReader(buf), DefaultArchiveLimits())
		assertAppErrorCode(t, err, domain.ErrCodeInvalidArchive)
	})

	t.Run("rejects single file exceeding limit (zip bomb protection)", func(t *testing.T) {
		limits := DefaultArchiveLimits()
		limits.MaxSingleFileBytes = 100
		buf := buildZip(t, []zipEntrySpec{
			{name: "repo-sha/big.py", body: strings.Repeat("a = 1\n", 50)},
		})
		_, err := ExtractPythonFilesFromZip(bytes.NewReader(buf), limits)
		assertAppErrorCode(t, err, domain.ErrCodeRepoTooLarge)
	})

	t.Run("rejects python file count exceeding limit", func(t *testing.T) {
		limits := DefaultArchiveLimits()
		limits.MaxPythonFiles = 2
		buf := buildZip(t, []zipEntrySpec{
			{name: "repo-sha/a.py", body: "x=1"},
			{name: "repo-sha/b.py", body: "x=2"},
			{name: "repo-sha/c.py", body: "x=3"},
		})
		_, err := ExtractPythonFilesFromZip(bytes.NewReader(buf), limits)
		assertAppErrorCode(t, err, domain.ErrCodeRepoTooLarge)
	})
}

type zipEntrySpec struct {
	name string
	body string
	mode os.FileMode
}

func buildZip(t *testing.T, specs []zipEntrySpec) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, s := range specs {
		hdr := &zip.FileHeader{
			Name:   s.name,
			Method: zip.Deflate,
		}
		if s.mode != 0 {
			hdr.SetMode(s.mode)
		} else {
			hdr.SetMode(0644)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("create zip header: %v", err)
		}
		if _, err := io.WriteString(w, s.body); err != nil {
			t.Fatalf("write zip body: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func assertAppErrorCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", code)
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *domain.AppError, got %T: %v", err, err)
	}
	if appErr.Code != code {
		t.Fatalf("expected error code %s, got %s (%v)", code, appErr.Code, appErr)
	}
}
