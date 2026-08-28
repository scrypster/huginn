package server

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/claudecode/approvals"
)

// handleClaudeApprove answers a PreToolUse hook from a Claude Code agent.
//
// It ALWAYS responds 200 with a decision object: the hook parses this body, and
// a non-200 would make it fall back to its own deny with a less useful reason.
// Refusal is expressed in the payload, not the status code.
//
// This handler DOES block, for up to approvalDeadline (285s), while a human
// decides. That is the point of the feature and it inverts this function's
// previous contract. The bound is what keeps it safe: 285s is below the hook's
// own client timeout of 290s, which is below Claude Code's 300s hook timeout.
// A hook killed by Claude Code fails OPEN, so the handler must always answer
// first. Never introduce a wait here that is not bounded by approvalDeadline.
//
// Consequence worth knowing: the agent's session semaphore is held for the
// whole wait. One agent can be frozen for five minutes; others are unaffected.
func (s *Server) handleClaudeApprove(w http.ResponseWriter, r *http.Request) {
	// The server's global WriteTimeout (server.go) is far shorter than
	// approvalDeadline, and Go sets that deadline before the handler runs and
	// never resets it. Without extending it here, a decision made after the
	// global timeout is written to a dead connection: the human sees "approved",
	// the hook sees a transport error and denies, and nothing records that the
	// two disagreed. Extend it for this request only.
	if err := http.NewResponseController(w).SetWriteDeadline(
		time.Now().Add(approvalDeadline + 15*time.Second)); err != nil {
		slog.Error("claudecode: cannot extend the approval write deadline; "+
			"a late approval will not reach the hook", "err", err)
	}

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

	// tool_input is deliberately absent. The hook no longer sends it and this
	// struct no longer names it: it was decoded and discarded here, while the
	// route's withMaxBody(1 MiB) cap turned a large Write/Edit/Bash payload
	// into a decode failure and therefore a DENY — a permission decision that
	// depended on payload size. Unknown fields are ignored by encoding/json,
	// so an older hook binary still sending it decodes fine (it just has to
	// fit the cap). If richer policy ever needs the input, forward a bounded
	// excerpt and raise the cap deliberately.
	var req struct {
		ToolName  string `json:"tool_name"`
		ToolUseID string `json:"tool_use_id"`
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		Summary   string `json:"summary"`
		Excerpt   string `json:"excerpt"`
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

	if s.approvals == nil {
		slog.Error("claudecode: approval store is not wired; denying",
			"agent", agent.Name, "tool", req.ToolName)
		respondApprove(w, "deny", "Huginn: approvals are unavailable; denying "+req.ToolName)
		return
	}

	if s.approvals.Remembered(agent.Name, req.ToolName, req.Summary) {
		slog.Info("claudecode: tool call allowed by a remembered command",
			"agent", agent.Name, "tool", req.ToolName)
		respondApprove(w, "allow", "Huginn: you previously always-allowed this exact command")
		return
	}

	pending, err := s.approvals.Register(approvals.Request{
		AgentName: agent.Name,
		ToolName:  req.ToolName,
		Summary:   req.Summary,
		Excerpt:   req.Excerpt,
		CWD:       req.CWD,
		ToolUseID: req.ToolUseID,
	})
	if err != nil {
		slog.Warn("claudecode: cannot register approval; denying",
			"agent", agent.Name, "tool", req.ToolName, "err", err)
		respondApprove(w, "deny", "Huginn: too many approvals are already pending")
		return
	}

	slog.Info("claudecode: tool call awaiting approval",
		"agent", agent.Name, "tool", req.ToolName, "id", pending.ID)
	s.broadcastApprovalChange()

	decision := s.approvals.Wait(r.Context(), pending)
	s.broadcastApprovalChange()

	if decision == approvals.AllowCommand {
		s.approvals.Remember(agent.Name, req.ToolName, req.Summary)
	}

	if !decision.Allowed() {
		if s.auditLog != nil {
			s.auditLog.Log("tool_permission", req.ToolName, false, "claude_approval_denied")
		}
		slog.Info("claudecode: tool call denied",
			"agent", agent.Name, "tool", req.ToolName, "id", pending.ID)
		respondApprove(w, "deny", "Huginn: "+req.ToolName+" was not approved")
		return
	}

	slog.Info("claudecode: tool call approved by a human",
		"agent", agent.Name, "tool", req.ToolName, "id", pending.ID)
	respondApprove(w, "allow", "Huginn: "+req.ToolName+" was approved")
}

// broadcastApprovalChange tells every connected client that the pending set
// changed. The message is a HINT, not the data: clients respond by re-fetching
// GET /api/v1/claude/approvals, which is authoritative.
//
// This matters because WSHub.broadcast DROPS on a full channel. If the message
// carried the card itself, a drop would lose a card until the next reconnect.
// As a hint, any later message heals the drift, and a dropped hint only costs
// a delayed card — which then ages out to a deny, the safe direction.
func (s *Server) broadcastApprovalChange() {
	if s.wsHub == nil {
		return
	}
	s.wsHub.broadcast(WSMessage{Type: "claude_approvals_changed"})
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
//
// VERIFIED — the session id is stable across --resume, so this literal match
// holds for turn 2 and every turn after it. Per Claude Code's documentation,
// `--resume <uuid>` does NOT fork by default: the transcript file and the
// session id are unchanged, and the PreToolUse hook payload carries the
// ORIGINAL uuid. Forking is opt-in via --fork-session, which Huginn never
// passes (BuildArgs in internal/claudecode/delegate.go emits only
// --session-id on turn 1 and --resume thereafter). This was review Finding 6;
// it is closed. Do not re-open it, and do not "defensively" start matching on
// anything other than the bound id — that would widen the gate.
func (s *Server) agentForClaudeSession(sessionID string) (agents.AgentDef, bool) {
	if sessionID == "" {
		return agents.AgentDef{}, false
	}
	// agentLoader is nil in production — only tests set it — so fall back to
	// the real loader exactly as every other agent lookup does. Without this
	// the handler could never find an agent and denied every approval.
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, err := loader()
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
