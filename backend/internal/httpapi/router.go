package httpapi

import "net/http"

func NewRouter(h *Handler, allowedOrigins []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.HandleHealthz)
	mux.HandleFunc("POST /api/v1/analyses", h.HandleCreateAnalysis)
	mux.HandleFunc("GET /api/v1/analyses/{id}", h.HandleGetAnalysis)
	mux.HandleFunc("GET /api/v1/analyses/{id}/graph", h.HandleGetAnalysisGraph)
	mux.HandleFunc("GET /api/v1/analyses/{id}/impact", h.HandleGetAnalysisImpact)
	mux.HandleFunc("GET /api/v1/demo/graph", h.HandleGetDemoGraph)

	logged := StructuredLoggingMiddleware(h.logger, h.ipExtractor, mux)
	return CORSMiddleware(allowedOrigins, logged)
}
