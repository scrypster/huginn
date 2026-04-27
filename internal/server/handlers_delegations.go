package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/scrypster/huginn/internal/session"
)

// InsertDelegation persists a delegation record via the server's delegation store.
// It is a no-op when delegationStore is nil (e.g. in-memory / test mode without DB).
func (s *Server) InsertDelegation(rec session.DelegationRecord) error {
	if s.delegationStore == nil {
		return nil
	}
	return s.delegationStore.InsertDelegation(rec)
}

// handleListDelegations returns delegations for a session, newest first.
//
// Query params:
//   - limit  (int, 1-200, default 50)
//   - offset (int, ≥0, default 0)
func (s *Server) handleListDelegations(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		jsonError(w, http.StatusBadRequest, "session id required")
		return
	}

	if s.store != nil {
		if _, err := s.store.Load(sessionID); err != nil {
			jsonError(w, http.StatusNotFound, "session not found")
			return
		}
	}

	if s.delegationStore == nil {
		// Delegation persistence not available — return empty list rather than 500.
		jsonOK(w, []any{})
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n < 1 {
				n = 1
			} else if n > 200 {
				n = 200
			}
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	records, err := s.delegationStore.ListDelegationsBySession(sessionID, limit, offset)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to list delegations")
		return
	}

	type delegationJSON struct {
		ID          string  `json:"id"`
		SessionID   string  `json:"session_id"`
		ThreadID    string  `json:"thread_id"`
		FromAgent   string  `json:"from_agent"`
		ToAgent     string  `json:"to_agent"`
		Task        string  `json:"task"`
		Status      string  `json:"status"`
		Result      string  `json:"result"`
		CreatedAt   string  `json:"created_at"`
		StartedAt   string  `json:"started_at"`
		CompletedAt *string `json:"completed_at"`
	}

	out := make([]delegationJSON, 0, len(records))
	for _, d := range records {
		dj := delegationJSON{
			ID:        d.ID,
			SessionID: d.SessionID,
			ThreadID:  d.ThreadID,
			FromAgent: d.FromAgent,
			ToAgent:   d.ToAgent,
			Task:      d.Task,
			Status:    d.Status,
			Result:    d.Result,
			CreatedAt: d.CreatedAt.UTC().Format("2006-01-02T15:04:05.999Z07:00"),
			StartedAt: d.StartedAt.UTC().Format("2006-01-02T15:04:05.999Z07:00"),
		}
		if d.CompletedAt != nil {
			s := d.CompletedAt.UTC().Format("2006-01-02T15:04:05.999Z07:00")
			dj.CompletedAt = &s
		}
		out = append(out, dj)
	}
	jsonOK(w, out)
}

// handleGetDelegation returns a single delegation by ID.
// Enforces cross-session security: the delegation's session_id must match
// the session_id in the URL path; mismatches return 404.
func (s *Server) handleGetDelegation(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	delegationID := r.PathValue("delegation_id")
	if sessionID == "" || delegationID == "" {
		jsonError(w, http.StatusBadRequest, "session id and delegation id required")
		return
	}

	if s.store != nil {
		if _, err := s.store.Load(sessionID); err != nil {
			jsonError(w, http.StatusNotFound, "session not found")
			return
		}
	}

	if s.delegationStore == nil {
		jsonError(w, http.StatusNotFound, "delegation not found")
		return
	}

	d, err := s.delegationStore.GetDelegation(delegationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "delegation not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "failed to get delegation")
		return
	}

	// Cross-session guard: prevent retrieving another session's delegation by ID.
	if d.SessionID != sessionID {
		jsonError(w, http.StatusNotFound, "delegation not found")
		return
	}

	type delegationJSON struct {
		ID          string  `json:"id"`
		SessionID   string  `json:"session_id"`
		ThreadID    string  `json:"thread_id"`
		FromAgent   string  `json:"from_agent"`
		ToAgent     string  `json:"to_agent"`
		Task        string  `json:"task"`
		Status      string  `json:"status"`
		Result      string  `json:"result"`
		CreatedAt   string  `json:"created_at"`
		StartedAt   string  `json:"started_at"`
		CompletedAt *string `json:"completed_at"`
	}

	dj := delegationJSON{
		ID:        d.ID,
		SessionID: d.SessionID,
		ThreadID:  d.ThreadID,
		FromAgent: d.FromAgent,
		ToAgent:   d.ToAgent,
		Task:      d.Task,
		Status:    d.Status,
		Result:    d.Result,
		CreatedAt: d.CreatedAt.UTC().Format("2006-01-02T15:04:05.999Z07:00"),
		StartedAt: d.StartedAt.UTC().Format("2006-01-02T15:04:05.999Z07:00"),
	}
	if d.CompletedAt != nil {
		s := d.CompletedAt.UTC().Format("2006-01-02T15:04:05.999Z07:00")
		dj.CompletedAt = &s
	}
	jsonOK(w, dj)
}
