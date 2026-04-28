package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// threadMessageRow is the JSON shape for message-level thread queries
// (workforce contract endpoints GET /api/v1/messages/:id/thread and
// GET /api/v1/containers/:id/threads).
type threadMessageRow struct {
	ID                  string                      `json:"id"`
	ContainerID         string                      `json:"container_id"`
	Seq                 int64                       `json:"seq"`
	Ts                  time.Time                   `json:"ts"`
	Role                string                      `json:"role"`
	Content             string                      `json:"content"`
	Agent               string                      `json:"agent"`
	ToolName            string                      `json:"tool_name,omitempty"`
	ParentMessageID     string                      `json:"parent_message_id,omitempty"`
	TriggeringMessageID string                      `json:"triggering_message_id,omitempty"`
	ThreadReplyCount    int                         `json:"thread_reply_count"`
	ToolCalls           []session.PersistedToolCall `json:"tool_calls,omitempty"`
}

// MessageThreadResponse is the JSON body returned by GET /api/v1/messages/:id/thread.
// DelegationChain lists the to_agent values of all delegations in the session ordered
// by created_at ASC. It is session-scoped (not thread-scoped) because the delegations
// table has no direct FK to the parent thread. Always a non-nil slice.
type MessageThreadResponse struct {
	Messages        []threadMessageRow `json:"messages"`
	ThreadID        string             `json:"thread_id,omitempty"`
	SessionID       string             `json:"session_id,omitempty"`
	DelegationChain []string           `json:"delegation_chain"`
}

// handleGetMessageThread returns all reply messages for a given parent message ID,
// ordered by seq ASC.
//
//	GET /api/v1/messages/{id}/thread
func (s *Server) handleGetMessageThread(w http.ResponseWriter, r *http.Request) {
	messageID := r.PathValue("id")
	if messageID == "" {
		jsonError(w, http.StatusBadRequest, "message id is required")
		return
	}

	if s.db == nil {
		// No SQLite DB wired — return empty array (e.g. in tests using file-backed store).
		jsonOK(w, MessageThreadResponse{Messages: []threadMessageRow{}, DelegationChain: []string{}})
		return
	}

	rdb := s.db.Read()
	if rdb == nil {
		jsonError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	// First, resolve the thread_id and session_id for this message.
	var threadID, sessionID string
	threadRow := rdb.QueryRowContext(r.Context(), `
	    SELECT id, CASE WHEN parent_type = 'session' THEN parent_id ELSE '' END
	    FROM threads WHERE parent_msg_id = ? LIMIT 1`,
		messageID,
	)
	// Scan errors are intentionally ignored: if the thread row does not exist yet
	// (in-flight delegation or invalid message ID), threadID and sessionID remain
	// empty strings. An empty sessionID disables the delegation chain lookup below.
	_ = threadRow.Scan(&threadID, &sessionID)

	// Query messages that belong to the thread container for this parent message.
	// We only return thread-scoped messages (container_type='thread') so that
	// follow-up synthesis messages posted to the main session channel don't leak
	// into the thread panel. Internal bookkeeping roles (cost, system) are
	// excluded so only user-visible messages appear.
	rows, err := rdb.QueryContext(r.Context(), `
		SELECT id, container_id, seq, ts, role, content,
		       COALESCE(agent, ''),
		       COALESCE(tool_name, ''),
		       COALESCE(parent_message_id, ''),
		       COALESCE(triggering_message_id, ''),
		       COALESCE(thread_reply_count, 0),
		       COALESCE(tool_calls_json, '')
		FROM messages
		WHERE container_type = 'thread'
		  AND container_id IN (
		      SELECT id FROM threads WHERE parent_msg_id = ?
		  )
		  AND role NOT IN ('cost', 'system')
		ORDER BY seq ASC`,
		messageID,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "query thread: "+err.Error())
		return
	}
	defer rows.Close()

	msgs, err := scanThreadMessageRows(rows)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "scan thread: "+err.Error())
		return
	}

	delegationChain := []string{}
	if s.delegationStore != nil && sessionID != "" {
		recs, err2 := s.delegationStore.ListDelegationsBySession(sessionID, 50, 0)
		if err2 == nil {
			for _, r := range recs {
				delegationChain = append(delegationChain, r.ToAgent)
			}
		}
	}
	jsonOK(w, MessageThreadResponse{
		Messages:        msgs,
		ThreadID:        threadID,
		SessionID:       sessionID,
		DelegationChain: delegationChain,
	})
}

// handleGetContainerThreads returns all root messages in a container that have
// at least one thread reply, ordered by seq ASC.
//
//	GET /api/v1/containers/{id}/threads
func (s *Server) handleGetContainerThreads(w http.ResponseWriter, r *http.Request) {
	containerID := r.PathValue("id")
	if containerID == "" {
		jsonError(w, http.StatusBadRequest, "container id is required")
		return
	}

	if s.db == nil {
		jsonOK(w, []threadMessageRow{})
		return
	}

	rdb := s.db.Read()
	if rdb == nil {
		jsonError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	// Join with the threads table to get the delegated sub-agent name
	// (threads.agent_name) instead of relying on the parent message's agent.
	// This ensures the badge shows "Sam" (who ran the thread) not "Tom" (who
	// authored the @mention).
	rows, err := rdb.QueryContext(r.Context(), `
		SELECT m.id, m.container_id, m.seq, m.ts, m.role, m.content,
		       COALESCE(t.agent_name, COALESCE(m.agent, '')),
		       COALESCE(m.tool_name, ''),
		       COALESCE(m.parent_message_id, ''),
		       COALESCE(m.triggering_message_id, ''),
		       COALESCE(m.thread_reply_count, 0),
		       COALESCE(m.tool_calls_json, '')
		FROM messages m
		LEFT JOIN threads t ON t.parent_msg_id = m.id
		WHERE m.container_type = 'session' AND m.container_id = ?
		  AND (m.parent_message_id IS NULL OR m.parent_message_id = '')
		  AND COALESCE(m.thread_reply_count, 0) > 0
		ORDER BY m.seq ASC`,
		containerID,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "query container threads: "+err.Error())
		return
	}
	defer rows.Close()

	msgs, err := scanThreadMessageRows(rows)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "scan container threads: "+err.Error())
		return
	}
	jsonOK(w, msgs)
}

// scanThreadMessageRows scans SQL rows into a []threadMessageRow.
// Returns a non-nil empty slice when there are no rows.
func scanThreadMessageRows(rows *sql.Rows) ([]threadMessageRow, error) {
	var out []threadMessageRow
	for rows.Next() {
		var m threadMessageRow
		var tsStr string
		var toolCallsJSON string
		if err := rows.Scan(
			&m.ID, &m.ContainerID, &m.Seq, &tsStr,
			&m.Role, &m.Content, &m.Agent, &m.ToolName,
			&m.ParentMessageID, &m.TriggeringMessageID,
			&m.ThreadReplyCount, &toolCallsJSON,
		); err != nil {
			return nil, err
		}
		if t, e := time.Parse(time.RFC3339, tsStr); e == nil {
			m.Ts = t.UTC()
		}
		if toolCallsJSON != "" {
			_ = json.Unmarshal([]byte(toolCallsJSON), &m.ToolCalls)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []threadMessageRow{}
	}
	return out, nil
}

// threadToResponse converts a live *threadmgr.Thread to its JSON-safe wrapper.
// NOTE: update when Thread fields change.
func threadToResponse(t *threadmgr.Thread) ThreadResponse {
	return ThreadResponse{
		ID:              t.ID,
		SessionID:       t.SessionID,
		AgentID:         t.AgentID,
		Task:            t.Task,
		Rationale:       t.Rationale,
		ParentMessageID: t.ParentMessageID,
		Status:          t.Status,
		DependsOn:       t.DependsOn,
		StartedAt:       t.StartedAt,
		CompletedAt:     t.CompletedAt,
		Summary:         t.Summary,
		TokensUsed:      t.TokensUsed,
		TokenBudget:     t.TokenBudget,
	}
}

// ThreadResponse wraps threadmgr.Thread for API serialization.
type ThreadResponse struct {
	ID              string                   `json:"id"`
	SessionID       string                   `json:"session_id"`
	AgentID         string                   `json:"agent_id"`
	Task            string                   `json:"task"`
	Rationale       string                   `json:"rationale,omitempty"`
	ParentMessageID string                   `json:"parent_message_id,omitempty"`
	Status          threadmgr.ThreadStatus   `json:"status"`
	DependsOn       []string                 `json:"depends_on"`
	StartedAt       time.Time                `json:"started_at"`
	CompletedAt     time.Time                `json:"completed_at"`
	Summary         *threadmgr.FinishSummary `json:"summary,omitempty"`
	TokensUsed      int                      `json:"tokens_used"`
	TokenBudget     int                      `json:"token_budget"`
}

// handleGetThread returns a single thread by ID.
func (s *Server) handleGetThread(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	threadID := r.PathValue("thread_id")
	if sessionID == "" || threadID == "" {
		http.Error(w, "session id and thread id required", http.StatusBadRequest)
		return
	}

	// Check session exists
	if _, err := s.store.Load(sessionID); err != nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}

	// If thread manager is not available, return not found
	if s.tm == nil {
		jsonError(w, http.StatusNotFound, "thread not found")
		return
	}

	t, ok := s.tm.Get(threadID)
	if !ok || t.SessionID != sessionID {
		jsonError(w, http.StatusNotFound, "thread not found")
		return
	}
	jsonOK(w, threadToResponse(t))
}

// handleReplyThread sends input to a blocked thread (thread status == blocked).
func (s *Server) handleReplyThread(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	threadID := r.PathValue("thread_id")
	if sessionID == "" || threadID == "" {
		http.Error(w, "session id and thread id required", http.StatusBadRequest)
		return
	}

	// Check session exists
	if _, err := s.store.Load(sessionID); err != nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}

	// If thread manager is not available, return error
	if s.tm == nil {
		jsonError(w, http.StatusServiceUnavailable, "thread manager not available")
		return
	}

	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if body.Input == "" {
		jsonError(w, http.StatusBadRequest, "input is required")
		return
	}

	// withMaxBody(64<<10) is applied at the route level — input size already bounded.
	sent, found := s.tm.TrySendInput(threadID, sessionID, body.Input)
	if !found {
		jsonError(w, http.StatusNotFound, "thread not found")
		return
	}
	if !sent {
		jsonError(w, http.StatusConflict, "thread is not waiting for input")
		return
	}
	jsonOK(w, map[string]string{"status": "input delivered"})
}

// handleCancelThread cancels a running thread.
func (s *Server) handleCancelThread(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	threadID := r.PathValue("thread_id")
	if sessionID == "" || threadID == "" {
		http.Error(w, "session id and thread id required", http.StatusBadRequest)
		return
	}

	// Check session exists
	if _, err := s.store.Load(sessionID); err != nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}

	// If thread manager is not available, return error
	if s.tm == nil {
		jsonError(w, http.StatusServiceUnavailable, "thread manager not available")
		return
	}

	_, found := s.tm.CancelIfOwned(threadID, sessionID)
	if !found {
		jsonError(w, http.StatusNotFound, "thread not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCreateThread creates a new delegated thread (starts a task on an agent).
// Returns 202 Accepted because thread execution is asynchronous — the thread may
// transition to error status shortly after creation if the agent fails.
func (s *Server) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}

	// Tighten body size to 16KB before decoding (route already has 64KB withMaxBody;
	// this enforces a tighter semantic limit on thread creation payloads).
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)

	// Load session first — session existence is always validated regardless of
	// orchestrator/tm availability, so callers get 404 for bad session IDs.
	sess, err := s.store.Load(sessionID)
	if err != nil || sess == nil {
		jsonError(w, http.StatusNotFound, "session not found")
		return
	}

	// Orchestrator is needed to spawn the thread goroutine.
	if s.orch == nil {
		jsonError(w, http.StatusServiceUnavailable, "orchestrator unavailable")
		return
	}

	// Thread manager must be configured.
	if s.tm == nil {
		jsonError(w, http.StatusServiceUnavailable, "thread manager not available")
		return
	}

	var body struct {
		AgentID   string   `json:"agent_id"`
		Task      string   `json:"task"`
		Rationale string   `json:"rationale,omitempty"`
		DependsOn []string `json:"depends_on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if body.AgentID == "" {
		jsonError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if body.Task == "" {
		jsonError(w, http.StatusBadRequest, "task is required")
		return
	}
	// Semantic field limit — separate from the route-level 64KB body limit.
	if len(body.Task) > 8*1024 {
		jsonError(w, http.StatusUnprocessableEntity, "task exceeds 8KB limit")
		return
	}
	if len(body.Rationale) > 2*1024 {
		jsonError(w, http.StatusUnprocessableEntity, "rationale exceeds 2KB limit")
		return
	}
	// Cap depends_on to prevent abuse and validate entry format.
	const maxDependsOn = 64
	if len(body.DependsOn) > maxDependsOn {
		jsonError(w, http.StatusUnprocessableEntity, "depends_on exceeds 64-entry limit")
		return
	}
	for _, dep := range body.DependsOn {
		if len(dep) == 0 || len(dep) > 64 {
			jsonError(w, http.StatusUnprocessableEntity, "invalid depends_on entry: must be 1-64 chars")
			return
		}
	}

	// Validate agent_id exists in config (best-effort; TOCTOU noted below).
	// Uses s.agentLoader — same path as resolveAgent.
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	if cfg, loadErr := loader(); loadErr == nil && cfg != nil {
		found := false
		for _, def := range cfg.Agents {
			if strings.EqualFold(def.Name, body.AgentID) {
				found = true
				break
			}
		}
		if !found {
			jsonError(w, http.StatusUnprocessableEntity, "agent_id not found in agent config")
			return
		}
		// TOCTOU: agent validated here but may be deleted before SpawnThread executes.
		// SpawnThread re-validates agent at execution time and transitions thread to
		// StatusError gracefully if the agent is no longer available.
	}

	// Create the thread record (synchronous — validates thread limits and dependencies).
	t, createErr := s.tm.Create(threadmgr.CreateParams{
		SessionID: sessionID,
		AgentID:   body.AgentID,
		Task:      body.Task,
		Rationale: body.Rationale,
		DependsOn: body.DependsOn,
		SpaceID:   sess.SpaceID(),
	})
	if createErr != nil {
		if errors.Is(createErr, threadmgr.ErrThreadLimitReached) {
			// 429 Too Many Requests: clear, actionable user-facing message.
			// Also broadcast a WS error so any open session connections surface this
			// to the user immediately (e.g. thread panel or notification area).
			activeCount := s.tm.ActiveCount(sessionID)
			msg := createErr.Error()
			s.wsHub.broadcastToSession(sessionID, WSMessage{
				Type:      "error",
				Content:   msg,
				SessionID: sessionID,
				Payload: map[string]any{
					"code":         "thread_limit_reached",
					"active_count": activeCount,
					"max_threads":  threadmgr.DefaultMaxThreadsPerSession,
				},
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"code":         "thread_limit_reached",
				"active_count": activeCount,
				"max_threads":  threadmgr.DefaultMaxThreadsPerSession,
				"message":      "Thread limit reached. Complete or cancel existing threads before creating new ones.",
			})
			return
		}
		jsonError(w, http.StatusUnprocessableEntity, createErr.Error())
		return
	}

	// Spawn the thread goroutine using the server lifecycle context.
	// s.ctx is intentionally used (not r.Context()) so the thread continues
	// running after the HTTP response is sent.
	broadcastFn := threadmgr.BroadcastFn(func(sid, msgType string, payload map[string]any) {
		s.BroadcastToSession(sid, msgType, payload)
	})
	s.orch.SpawnThread(s.ctx, t.ID, sess, s.tm, broadcastFn, &s.spawnWg, s.ca)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted) // 202: thread created and spawning initiated
	_ = json.NewEncoder(w).Encode(threadToResponse(t))
}

// handleArchiveThread marks a thread as archived for the given session.
// Archived threads are hidden from the default list view but their messages
// are preserved. Returns 409 if the thread is currently active (thinking/tooling),
// 404 if not found, and 200 {"archived": threadID} on success.
//
//	POST /api/v1/sessions/{sessionID}/threads/{threadID}/archive
func (s *Server) handleArchiveThread(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	threadID := r.PathValue("thread_id")

	if sessionID == "" {
		jsonError(w, http.StatusBadRequest, "sessionID is required")
		return
	}
	if threadID == "" {
		jsonError(w, http.StatusBadRequest, "threadID is required")
		return
	}

	if s.tm == nil {
		jsonError(w, http.StatusServiceUnavailable, "thread manager not initialized")
		return
	}

	// Verify the thread belongs to the requested session before archiving.
	t, ok := s.tm.Get(threadID)
	if !ok {
		jsonError(w, http.StatusNotFound, "thread not found")
		return
	}
	if t.SessionID != sessionID {
		jsonError(w, http.StatusNotFound, "thread not found")
		return
	}

	if err := s.tm.ArchiveThread(threadID); err != nil {
		switch {
		case errors.Is(err, threadmgr.ErrThreadNotFound):
			jsonError(w, http.StatusNotFound, "thread not found")
		case errors.Is(err, threadmgr.ErrThreadActive):
			jsonError(w, http.StatusConflict, "cannot archive an active thread")
		default:
			jsonError(w, http.StatusInternalServerError, "archive thread: "+err.Error())
		}
		return
	}

	jsonOK(w, map[string]string{"archived": threadID})
}
