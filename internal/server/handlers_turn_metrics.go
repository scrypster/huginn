package server

import (
	"net/http"
	"strconv"

	"github.com/scrypster/huginn/internal/turnmetrics"
)

// turnMetricsReader is the minimal interface handleTurnMetrics requires.
// *turnmetrics.Writer satisfies this via Recent(); kept as an interface so
// tests can stub it without a real SQLite DB.
type turnMetricsReader interface {
	Recent(limit int) (turnmetrics.TurnsResponse, error)
}

// SetTurnMetricsReader wires the turn-latency telemetry source used by
// GET /api/v1/metrics/turns. Called from main.go once the writer/migration
// are set up; nil (default) makes the endpoint report 503.
func (s *Server) SetTurnMetricsReader(r turnMetricsReader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnMetrics = r
}

// handleTurnMetrics returns recent per-turn latency rows plus a per-model
// p50/p95 summary, for perf dashboards and agents to consume.
//
//	GET /api/v1/metrics/turns[?limit=N]
//
// No query language — limit is the only parameter, clamped to [1, 1000]
// (see turnmetrics.Writer.Recent). Metadata only: no token or message
// content is ever stored in turn_metrics, so this endpoint is safe to expose
// to the same authenticated audience as the rest of /api/v1.
func (s *Server) handleTurnMetrics(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	reader := s.turnMetrics
	s.mu.Unlock()
	if reader == nil {
		jsonError(w, http.StatusServiceUnavailable, "turn metrics not available")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			jsonError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	resp, err := reader.Recent(limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to load turn metrics")
		return
	}
	jsonOK(w, resp)
}
