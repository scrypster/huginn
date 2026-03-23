package server

import (
	"encoding/json"
	"net/http"
)

// handleCreateWorkstream handles POST /api/v1/workstreams.
// Creates a new workstream and returns it as JSON (201 Created).
func (s *Server) handleCreateWorkstream(w http.ResponseWriter, r *http.Request) {
	if s.workstreamStore == nil {
		jsonError(w, 503, "workstream store not configured")
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid request body: "+err.Error())
		return
	}
	if body.Name == "" {
		jsonError(w, 400, "name is required")
		return
	}
	ws, err := s.workstreamStore.Create(r.Context(), body.Name, body.Description)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonCreated(w, ws)
}

// handleListWorkstreams handles GET /api/v1/workstreams.
// Returns all workstreams as a JSON array.
func (s *Server) handleListWorkstreams(w http.ResponseWriter, r *http.Request) {
	if s.workstreamStore == nil {
		jsonError(w, 503, "workstream store not configured")
		return
	}
	list, err := s.workstreamStore.List(r.Context())
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, list)
}

// handleGetWorkstream handles GET /api/v1/workstreams/{id}.
// Returns a single workstream by ID.
func (s *Server) handleGetWorkstream(w http.ResponseWriter, r *http.Request) {
	if s.workstreamStore == nil {
		jsonError(w, 503, "workstream store not configured")
		return
	}
	id := r.PathValue("id")
	ws, err := s.workstreamStore.Get(r.Context(), id)
	if err != nil {
		jsonError(w, 404, err.Error())
		return
	}
	jsonOK(w, ws)
}

// handleDeleteWorkstream handles DELETE /api/v1/workstreams/{id}.
// Removes a workstream and its session associations.
func (s *Server) handleDeleteWorkstream(w http.ResponseWriter, r *http.Request) {
	if s.workstreamStore == nil {
		jsonError(w, 503, "workstream store not configured")
		return
	}
	id := r.PathValue("id")
	if err := s.workstreamStore.Delete(r.Context(), id); err != nil {
		jsonError(w, 404, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTagWorkstreamSession handles POST /api/v1/workstreams/{id}/sessions.
// Associates a session with a workstream. Body: {"session_id": "..."}.
func (s *Server) handleTagWorkstreamSession(w http.ResponseWriter, r *http.Request) {
	if s.workstreamStore == nil {
		jsonError(w, 503, "workstream store not configured")
		return
	}
	id := r.PathValue("id")
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid request body: "+err.Error())
		return
	}
	if body.SessionID == "" {
		jsonError(w, 400, "session_id is required")
		return
	}
	if err := s.workstreamStore.TagSession(r.Context(), id, body.SessionID); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{"workstream_id": id, "session_id": body.SessionID})
}

// handleListWorkstreamSessions handles GET /api/v1/workstreams/{id}/sessions.
// Returns session IDs associated with a workstream.
func (s *Server) handleListWorkstreamSessions(w http.ResponseWriter, r *http.Request) {
	if s.workstreamStore == nil {
		jsonError(w, 503, "workstream store not configured")
		return
	}
	id := r.PathValue("id")
	ids, err := s.workstreamStore.ListSessions(r.Context(), id)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, ids)
}
