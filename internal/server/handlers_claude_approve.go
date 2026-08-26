package server

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"

	"github.com/scrypster/huginn/internal/agents"
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
	// This route is intentionally unauthenticated (see its registration in
	// server.go) because the hook client never sends a token. That safety
	// margin must not depend on web_ui.bind staying 127.0.0.1 — that's a
	// user-editable value in a different file, set by someone who has no idea
	// this endpoint exists. So check the actual peer address ourselves, before
	// touching the body, and fail closed on anything we can't positively
	// confirm is loopback.
	if !isLoopbackAddr(r.RemoteAddr) {
		slog.Warn("claudecode: approval request from non-loopback address",
			"remote_addr", r.RemoteAddr)
		respondApprove(w, "deny",
			"Huginn: approval requests are only accepted from localhost")
		return
	}

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

	agent, ok := s.agentForClaudeSession(req.SessionID)
	if !ok {
		slog.Warn("claudecode: approval request for an unbound session",
			"session_id", req.SessionID, "tool", req.ToolName)
		respondApprove(w, "deny",
			"No Huginn agent is bound to this Claude Code session")
		return
	}

	if toolAllowed(agent.ClaudeAllowedTools, req.ToolName) {
		slog.Info("claudecode: tool call allowed",
			"agent", agent.Name, "tool", req.ToolName, "tool_use_id", req.ToolUseID)
		respondApprove(w, "allow",
			"Huginn: "+req.ToolName+" is in "+agent.Name+"'s allowed tools")
		return
	}

	slog.Info("claudecode: tool call requires approval",
		"agent", agent.Name, "tool", req.ToolName, "tool_use_id", req.ToolUseID)
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

// isLoopbackAddr reports whether addr (an http.Request.RemoteAddr, e.g.
// "127.0.0.1:1234" or "[::1]:1234") names a loopback IP. Anything that
// doesn't parse cleanly to a loopback address returns false — this is a
// fail-closed check, never assume local on a malformed or missing address.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// toolAllowed reports whether tool is granted to an agent by its
// ClaudeAllowedTools allowlist. Empty/nil = default-deny. Exact matches only —
// deliberately NO wildcard support: this gate protects tool execution by an
// unattended agent, so it must never be one character away from "allow
// everything". ClaudeAllowedTools holds Claude Code CLI tool names ("Bash",
// "Write", ...), a namespace distinct from and not interchangeable with
// agents.AgentDef.LocalTools (Huginn's own builtin tool names) — do not widen
// this to accept LocalTools or its "*" semantics.
func toolAllowed(claudeAllowedTools []string, tool string) bool {
	for _, t := range claudeAllowedTools {
		if t == tool {
			return true
		}
	}
	return false
}

// agentForClaudeSession maps a Claude Code session id to the agent bound to it.
func (s *Server) agentForClaudeSession(sessionID string) (agents.AgentDef, bool) {
	if sessionID == "" || s.agentLoader == nil {
		return agents.AgentDef{}, false
	}
	cfg, err := s.agentLoader()
	if err != nil || cfg == nil {
		return agents.AgentDef{}, false
	}
	for _, a := range cfg.Agents {
		if a.ClaudeSessionID != "" && a.ClaudeSessionID == sessionID {
			return a, true
		}
	}
	return agents.AgentDef{}, false
}
