package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/claudecode/approvals"
)

// handleListClaudeApprovals returns every approval waiting on a human.
//
// This is the AUTHORITATIVE source for the browser. Clients replace their list
// with this response rather than merging, so a client that was disconnected,
// backgrounded, or freshly opened converges without reconciliation logic.
//
// RemainingMS is computed server-side on every call. An absolute expiry would
// let client clock skew make a card look expired when it is not.
func (s *Server) handleListClaudeApprovals(w http.ResponseWriter, r *http.Request) {
	list := []approvals.PendingView{}
	if s.approvals != nil {
		list = s.approvals.List()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"approvals": list})
}

// decisionFromString maps the wire value to a Decision.
//
// Unknown strings are an ERROR, never a default. Defaulting an unrecognised
// value to Deny would silently discard a pending entry on a typo, and
// defaulting to Allow would be a vulnerability. Reject and leave the entry
// alone so the human can try again.
func decisionFromString(v string) (approvals.Decision, bool) {
	switch v {
	case "deny":
		return approvals.Deny, true
	case "allow":
		return approvals.Allow, true
	case "allow_command":
		return approvals.AllowCommand, true
	case "allow_tool":
		return approvals.AllowTool, true
	}
	return approvals.Deny, false
}

// handleDecideClaudeApproval delivers a human decision to a waiting hook.
//
// SECURITY: this is the endpoint that turns "denied" into "allowed". It is
// registered in the AUTHENTICATED block in server.go and must stay there.
// Unlike /claude/approve — which is unauthenticated because the hook is a
// local child process with no token — this one is reached by a browser and
// carries the user's session.
func (s *Server) handleDecideClaudeApproval(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	decision, ok := decisionFromString(req.Decision)
	if !ok {
		http.Error(w, "unrecognised decision", http.StatusBadRequest)
		return
	}

	// Capture the agent and tool from the pending entry before delivering,
	// because Deliver removes it and AllowTool needs both names to promote.
	//
	// This takes the store lock twice, and the gap is tolerated deliberately:
	// if the entry expires in between, Deliver returns false, we 404 below, and
	// the captured names are discarded before the promotion branch. The map is
	// the single source of truth for both calls, so there is no interleaving
	// where List sees an entry that Deliver then wrongly promotes.
	var agentName, toolName string
	for _, v := range s.approvals.List() {
		if v.ID == req.ID {
			agentName, toolName = v.AgentName, v.ToolName
			break
		}
	}

	if !s.approvals.Deliver(req.ID, decision) {
		http.Error(w, "no such pending approval", http.StatusNotFound)
		return
	}

	if decision == approvals.AllowTool && agentName != "" && toolName != "" {
		if err := s.promoteClaudeTool(agentName, toolName); err != nil {
			// The tool call itself is already allowed — only the persistence
			// failed. Log loudly; do not fail the request, because the human's
			// decision has already been delivered and cannot be recalled.
			slog.Error("claudecode: could not promote tool into agent config",
				"agent", agentName, "tool", toolName, "err", err)
		}
	}

	s.broadcastApprovalChange()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// promoteClaudeTool permanently adds tool to an agent's ClaudeAllowedTools.
//
// This is a PRIVILEGE ESCALATION performed by a browser click: afterwards the
// tool is never gated for this agent again, and no card ever appears for it.
// The UI must confirm it separately from a one-click Allow. In Phase 1 the
// only undo is editing the agent config file.
//
// It is idempotent: an already-allowed tool writes nothing.
func (s *Server) promoteClaudeTool(agentName, tool string) error {
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, err := loader()
	if err != nil {
		return err
	}
	for i := range cfg.Agents {
		if cfg.Agents[i].Name != agentName {
			continue
		}
		if toolAllowed(cfg.Agents[i].ClaudeAllowedTools, tool) {
			return nil
		}
		cfg.Agents[i].ClaudeAllowedTools = append(cfg.Agents[i].ClaudeAllowedTools, tool)
		saver := s.agentSaver
		if saver == nil {
			saver = agents.SaveAgents
		}
		return saver(cfg)
	}
	return nil
}
