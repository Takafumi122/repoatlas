package httpapi

import "net/http"

func NewRouter(h *Handler, allowedOrigins []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.HandleHealthz)
	mux.HandleFunc("POST /api/v1/analyses", h.HandleCreateAnalysis)
	mux.HandleFunc("GET /api/v1/analyses/{id}", h.HandleGetAnalysis)
	mux.HandleFunc("GET /api/v1/analyses/{id}/graph", h.HandleGetAnalysisGraph)
	mux.HandleFunc("GET /api/v1/analyses/{id}/impact", h.HandleGetAnalysisImpact)
	mux.HandleFunc("GET /api/v1/analyses/{id}/evidence", h.HandleGetAnalysisEvidence)
	mux.HandleFunc("POST /api/v1/pull-requests/analyses", h.HandleCreatePRAnalysis)
	mux.HandleFunc("GET /api/v1/pull-requests/analyses/{id}", h.HandleGetPRAnalysis)
	mux.HandleFunc("GET /api/v1/pull-requests/analyses/{id}/impact", h.HandleGetPRAnalysisImpact)
	mux.HandleFunc("GET /api/v1/demo/graph", h.HandleGetDemoGraph)

	mux.HandleFunc("POST /api/v1/architecture-diffs", h.HandleCreateArchitectureDiff)
	mux.HandleFunc("GET /api/v1/architecture-diffs/{id}", h.HandleGetArchitectureDiff)
	mux.HandleFunc("GET /api/v1/architecture-diffs/{id}/graph", h.HandleGetArchitectureDiffGraph)

	logged := StructuredLoggingMiddleware(h.logger, h.ipExtractor, mux)
	return CORSMiddleware(allowedOrigins, logged)
}
