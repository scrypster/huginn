package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// handleClaudeApprove answers a PreToolUse hook from a Claude Code agent.
//
// It ALWAYS responds 200 with a decision object: the hook parses this body, and
// a non-200 would make it fall back to its own deny with a less useful reason.
// Refusal is expressed in the payload, not the status code.
//
// This handler must never block on anything unbounded — see cmd_claude_approve.go
// for the client-side timing constraint (claudeApproveTimeout, currently
// ClaudeHookTimeoutSecs-10 = 20s) that this response has to beat. Everything
// here is in-memory config lookups, so there is nothing to bound.
func (s *Server) handleClaudeApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ToolName  string          `json:"tool_name"`
		ToolUseID string          `json:"tool_use_id"`
		SessionID string          `json:"session_id"`
		CWD       string          `json:"cwd"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondApprove(w, "deny", "Huginn could not parse the approval request")
		return
	}

	agentName, ok := s.agentForClaudeSession(req.SessionID)
	if !ok {
		slog.Warn("claudecode: approval request for an unbound session",
			"session_id", req.SessionID, "tool", req.ToolName)
		respondApprove(w, "deny",
			"No Huginn agent is bound to this Claude Code session")
		return
	}

	// v1: any tool that reached the hook was NOT pre-authorised, so it needs a
	// human. Surfacing it in the approval UI is wired in a follow-up; until
	// then an un-preauthorised tool is denied with a legible reason rather than
	// silently allowed.
	slog.Info("claudecode: tool call requires approval",
		"agent", agentName, "tool", req.ToolName, "tool_use_id", req.ToolUseID)
	respondApprove(w, "deny",
		"Huginn: "+req.ToolName+" is not in this agent's allowed tools")
}

func respondApprove(w http.ResponseWriter, decision, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"decision": decision,
		"reason":   reason,
	})
}

// agentForClaudeSession maps a Claude Code session id to the agent bound to it.
func (s *Server) agentForClaudeSession(sessionID string) (string, bool) {
	if sessionID == "" || s.agentLoader == nil {
		return "", false
	}
	cfg, err := s.agentLoader()
	if err != nil || cfg == nil {
		return "", false
	}
	for _, a := range cfg.Agents {
		if a.ClaudeSessionID != "" && a.ClaudeSessionID == sessionID {
			return a.Name, true
		}
	}
	return "", false
}
