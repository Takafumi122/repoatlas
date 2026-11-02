package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var secretPatternRegex = regexp.MustCompile(`(?i)(ghp_[A-Za-z0-9_]+|github_pat_[A-Za-z0-9_]+|gho_[A-Za-z0-9_]+|Bearer\s+[A-Za-z0-9._\-]+)`)

func SanitizeLogString(s string) string {
	return secretPatternRegex.ReplaceAllString(s, "[REDACTED]")
}

func NewStructuredLogger(w io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			lowerKey := strings.ToLower(a.Key)
			if strings.Contains(lowerKey, "token") ||
				strings.Contains(lowerKey, "authorization") ||
				strings.Contains(lowerKey, "secret") ||
				strings.Contains(lowerKey, "password") {
				return slog.String(a.Key, "[REDACTED]")
			}
			if a.Value.Kind() == slog.KindString {
				return slog.String(a.Key, SanitizeLogString(a.Value.String()))
			}
			return a
		},
	})
	return slog.New(handler)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func StructuredLoggingMiddleware(logger *slog.Logger, ipExtractor *IPExtractor, next http.Handler) http.Handler {
	if logger == nil {
		logger = NewStructuredLogger(io.Discard)
	}
	if ipExtractor == nil {
		ipExtractor = NewIPExtractor(nil)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		logger.Info("http_request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("client_ip", ipExtractor.ClientIP(r)),
		)
	})
}

func CORSMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	originSet := make(map[string]bool, len(allowedOrigins))
	allowAll := false
	for _, o := range allowedOrigins {
		trimmed := strings.TrimSpace(o)
		if trimmed == "*" {
			allowAll = true
		} else if trimmed != "" {
			originSet[trimmed] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowAll || originSet[origin]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
