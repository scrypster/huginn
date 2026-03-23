package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// sessionActiveState is the JSON response for GET /api/v1/sessions/{id}/active-state.
// It gives clients enough information to reconstruct display state after a WS reconnect.
type sessionActiveState struct {
	SessionID      string             `json:"session_id"`
	ActiveThreads  []threadMessageRow `json:"active_threads"`
	LastSeq        int64              `json:"last_seq"`
	InFlightTasks  []any              `json:"in_flight_tasks"`
	SwarmState     any                `json:"swarm_state"`
}

// handleSessionActiveState returns the current session state for WS reconnect.
// It queries the messages table for active thread roots and the last seq number.
//
//	GET /api/v1/sessions/{id}/active-state
func (s *Server) handleSessionActiveState(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		jsonError(w, http.StatusBadRequest, "session id is required")
		return
	}

	// Verify the session exists.
	if !s.store.Exists(sessionID) {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}

	// Load swarm state snapshot (stored when swarm_complete fires, evicted after 1h).
	var swarmState any
	if snap, ok := s.swarmSnapshots.Load(sessionID); ok {
		if entry, ok := snap.(swarmSnapshotEntry); ok {
			swarmState = entry.payload
		}
	}

	resp := sessionActiveState{
		SessionID:     sessionID,
		ActiveThreads: []threadMessageRow{},
		InFlightTasks: []any{},
		SwarmState:    swarmState,
	}

	if s.db != nil {
		rdb := s.db.Read()
		if rdb != nil {
			// MAX(seq) for messages in this session container.
			var lastSeq int64
			rdb.QueryRowContext(r.Context(), // nolint:errcheck — zero is fine on error
				`SELECT COALESCE(MAX(seq), 0) FROM messages WHERE container_id = ?`,
				sessionID,
			).Scan(&lastSeq) // nolint:errcheck
			resp.LastSeq = lastSeq

			// Active thread roots: messages with thread replies.
			rows, err := rdb.QueryContext(r.Context(), `
				SELECT id, container_id, seq, ts, role, content,
				       COALESCE(agent, ''),
				       COALESCE(parent_message_id, ''),
				       COALESCE(triggering_message_id, ''),
				       COALESCE(thread_reply_count, 0)
				FROM messages
				WHERE container_id = ?
				  AND (parent_message_id IS NULL OR parent_message_id = '')
				  AND COALESCE(thread_reply_count, 0) > 0
				ORDER BY seq ASC`,
				sessionID,
			)
			if err == nil {
				defer rows.Close()
				if threads, scanErr := scanThreadMessageRows(rows); scanErr == nil && threads != nil {
					resp.ActiveThreads = threads
				}
			}
		}
	}

	jsonOK(w, resp)
}

// ActiveStateResponse represents the current active session and thread state.
type ActiveStateResponse struct {
	ActiveSessionID string    `json:"active_session_id,omitempty"`
	ActiveAgentID   string    `json:"active_agent_id,omitempty"`
	LastActivityAt  time.Time `json:"last_activity_at"`
	ThreadsRunning  int       `json:"threads_running"`
}

// handleActiveState returns the current active state (active session, threads in progress, etc.).
// This endpoint helps clients recover their state after a disconnect/reconnect.
func (s *Server) handleActiveState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	activeSessionID := s.cfg.ActiveSessionID
	activeAgent := s.cfg.ActiveAgent
	s.mu.Unlock()

	// Resolve the agent name from the registry using the stored agent name.
	var activeAgentID string
	if activeAgent != "" {
		if reg := s.orch.GetAgentRegistry(); reg != nil {
			if ag, ok := reg.ByName(activeAgent); ok {
				activeAgentID = ag.Name
			}
		}
	}

	// Sum running threads across all sessions via thread manager.
	var threadsRunning int
	if s.tm != nil && activeSessionID != "" {
		threadsRunning = s.tm.ActiveCount(activeSessionID)
	}

	response := ActiveStateResponse{
		ActiveSessionID: activeSessionID,
		ActiveAgentID:   activeAgentID,
		LastActivityAt:  time.Now().UTC(),
		ThreadsRunning:  threadsRunning,
	}

	jsonOK(w, response)
}

// handleRestoreActiveState allows clients to restore the active session/agent state
// during recovery from network disconnects.
func (s *Server) handleRestoreActiveState(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		AgentID   string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Validate session exists if provided
	if body.SessionID != "" {
		if _, err := s.store.Load(body.SessionID); err != nil {
			jsonError(w, http.StatusNotFound, "session not found")
			return
		}
	}

	// Validate agent exists if provided
	if body.AgentID != "" {
		reg := s.orch.GetAgentRegistry()
		if reg == nil {
			jsonError(w, http.StatusNotFound, "agent not found")
			return
		}
		if _, ok := reg.ByName(body.AgentID); !ok {
			jsonError(w, http.StatusNotFound, "agent not found")
			return
		}
	}

	// Persist active session/agent to config under the server mutex.
	s.mu.Lock()
	s.cfg.ActiveAgent = body.AgentID
	s.cfg.ActiveSessionID = body.SessionID
	saveErr := s.cfg.Save()
	s.mu.Unlock()
	if saveErr != nil {
		jsonError(w, http.StatusInternalServerError, "persist config: "+saveErr.Error())
		return
	}

	response := ActiveStateResponse{
		ActiveSessionID: body.SessionID,
		ActiveAgentID:   body.AgentID,
		LastActivityAt:  time.Now().UTC(),
		ThreadsRunning:  0,
	}

	jsonOK(w, response)
}
