package server

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/permissions"
)

// permissionPromptTimeout bounds how long a pending WS permission_request
// entry survives if the client never replies (e.g. tab closed, network
// drop). It must be longer than the gate's own promptFuncTimeout (30s,
// internal/permissions) so the gate's timeout — which produces the honest
// "Permission request timed out." denial — always fires first; this is a
// second-line cleanup so pendingPermissions never leaks entries.
var permissionPromptTimeout = 40 * time.Second

// pendingPermissionEntry pairs a response channel with the request it answers,
// so handlePermissionResponse can resolve "always_agent" scope grants against
// the right agent + tool without the client having to echo them back.
type pendingPermissionEntry struct {
	ch  chan permissions.Decision
	req permissions.PermissionRequest
}

// permissionPrompts tracks in-flight WS permission_request round-trips,
// keyed by request ID. This is the local (browser WS) analogue of
// permissions.Gate's relayChans (which serves HuginnCloud's remote relay
// path) — kept separate because it needs the request's AgentName/ToolName at
// resolution time to persist an "always allow for this agent" grant.
type permissionPrompts struct {
	mu      sync.Mutex
	pending map[string]pendingPermissionEntry
}

func newPermissionPrompts() *permissionPrompts {
	return &permissionPrompts{pending: make(map[string]pendingPermissionEntry)}
}

// register records a pending request and returns the channel its decision
// arrives on. onExpire, when non-nil, is invoked if the entry is swept by the
// timeout above — the client was shown a banner that can no longer be
// answered, so it needs to be told to take it down.
func (p *permissionPrompts) register(id string, req permissions.PermissionRequest, onExpire func(id string, req permissions.PermissionRequest)) chan permissions.Decision {
	ch := make(chan permissions.Decision, 1)
	p.mu.Lock()
	p.pending[id] = pendingPermissionEntry{ch: ch, req: req}
	p.mu.Unlock()

	time.AfterFunc(permissionPromptTimeout, func() {
		p.mu.Lock()
		entry, ok := p.pending[id]
		if ok {
			delete(p.pending, id)
		}
		p.mu.Unlock()
		if ok {
			select {
			case entry.ch <- permissions.Deny:
			default:
			}
			if onExpire != nil {
				onExpire(id, entry.req)
			}
		}
	})
	return ch
}

// resolve delivers a decision for a pending request ID. Returns the request
// it resolved (so the caller can act on AgentName/ToolName for persistence)
// and whether the ID was known.
func (p *permissionPrompts) resolve(id string, decision permissions.Decision) (permissions.PermissionRequest, bool) {
	p.mu.Lock()
	entry, ok := p.pending[id]
	if ok {
		delete(p.pending, id)
	}
	p.mu.Unlock()
	if !ok {
		return permissions.PermissionRequest{}, false
	}
	select {
	case entry.ch <- decision:
	default:
	}
	return entry.req, true
}

// BroadcastWSToSession sends a message to WS clients subscribed to sessionID.
// No-op when wsHub is nil (e.g. tests, early startup).
func (s *Server) BroadcastWSToSession(sessionID string, msg WSMessage) {
	if s.wsHub == nil || sessionID == "" {
		return
	}
	s.wsHub.broadcastToSession(sessionID, msg)
}

// PermissionPromptFunc returns a permissions.Gate-compatible promptFunc that
// bridges a tool-permission decision to the web UI over WebSocket: it emits a
// "permission_request" event scoped to the requesting session and blocks
// until the browser answers with a "permission_response" WS message (handled
// by handlePermissionResponse), or the gate's own timeout elapses.
//
// req.SessionID must be set (propagated by RunLoopConfig.SessionID /
// PermissionRequest.SessionID) for the browser to receive the prompt — when
// empty, there's no session to target, so this denies immediately with a
// clear reason rather than blocking forever with no way to reach the user.
func (s *Server) PermissionPromptFunc() func(permissions.PermissionRequest) permissions.Decision {
	return func(req permissions.PermissionRequest) permissions.Decision {
		if req.SessionID == "" {
			slog.Warn("permission prompt: no session_id on request; denying", "tool", req.ToolName, "agent", req.AgentName)
			return permissions.Deny
		}

		id, err := permissions.NewRelayRequestID()
		if err != nil {
			slog.Error("permission prompt: failed to generate request id", "err", err)
			return permissions.Deny
		}

		// Nobody is listening on this session's WebSocket — no browser tab,
		// headless run, or the client disconnected. Blocking here would stall
		// the agent's turn for the gate's full promptFuncTimeout on EVERY
		// bash call with no possible answer, so fail closed immediately with
		// the same Deny the timeout would eventually produce.
		if !s.hasSessionSubscribers(req.SessionID) {
			slog.Warn("permission prompt: no client subscribed to session; denying without waiting",
				"tool", req.ToolName, "agent", req.AgentName, "session", req.SessionID)
			return permissions.Deny
		}

		ch := s.permPrompts.register(id, req, s.broadcastPermissionCancelled)

		command := ""
		if v, ok := req.Args["command"].(string); ok {
			command = v
		}
		s.BroadcastWSToSession(req.SessionID, WSMessage{
			Type:      "permission_request",
			SessionID: req.SessionID,
			Payload: map[string]any{
				"id":         id,
				"request_id": id,
				"tool":       req.ToolName,
				"agent":      req.AgentName,
				"command":    sanitizePromptText(command),
				"args":       sanitizePromptText(fmt.Sprintf("%v", req.Args)),
				"summary":    sanitizePromptText(permissions.FormatRequest(req)),
			},
		})

		return <-ch
	}
}

// promptTextMaxRunes bounds the command/args/summary strings sent to the
// permission banner. The banner is a fixed-height strip in the chat column;
// an unbounded command (a heredoc, a minified payload) would push the
// Allow/Deny buttons off screen, and a very long string is not what the
// human needs to make the decision anyway.
const promptTextMaxRunes = 400

// sanitizePromptText makes an arbitrary tool argument safe to render in the
// permission banner. The banner is a Vue text interpolation (no v-html), so
// markup cannot execute — but control characters can still forge line breaks
// and hide the tail of a command behind terminal-style escapes, and an
// unbounded string wrecks the layout. Reuses sanitizeStatusText (the same
// scrub the tool status line uses: control characters to spaces, whitespace
// collapsed, truncation on a rune boundary so the JSON stays valid UTF-8) and
// adds an explicit ellipsis — unlike a status line, a human is approving this
// text, so they must be able to see that it was cut short.
func sanitizePromptText(s string) string {
	// len(s)+1 is always >= the rune count, so this scrubs without truncating.
	clean := sanitizeStatusText(s, len(s)+1)
	if r := []rune(clean); len(r) > promptTextMaxRunes {
		return string(r[:promptTextMaxRunes]) + "…"
	}
	return clean
}

// broadcastPermissionCancelled tells the UI to take down a permission banner
// that can no longer be answered. Without it the banner stays on screen after
// the gate has already denied the call on timeout, and its buttons silently do
// nothing (handlePermissionResponse no longer knows the ID) — the worst kind of
// approval UI, one that looks live and isn't.
func (s *Server) broadcastPermissionCancelled(id string, req permissions.PermissionRequest) {
	s.BroadcastWSToSession(req.SessionID, WSMessage{
		Type:      "permission_cancelled",
		SessionID: req.SessionID,
		Payload: map[string]any{
			"id":         id,
			"request_id": id,
			"tool":       req.ToolName,
			"agent":      req.AgentName,
			"reason":     "timeout",
		},
	})
}

// hasSessionSubscribers reports whether any WS client would receive a
// broadcastToSession for sessionID (an exact session match, or a wildcard
// client registered with an empty session ID). A nil hub means the server
// has no WS transport at all, so nothing can ever answer a prompt.
func (s *Server) hasSessionSubscribers(sessionID string) bool {
	if s.wsHub == nil {
		return false
	}
	return s.wsHub.hasSessionSubscribers(sessionID)
}

// handlePermissionResponse processes an inbound "permission_response" WS
// message: {id, scope} where scope is "once" | "always_agent" | "deny"
// (older clients may instead send {id, approved: bool} — treated as
// once/deny). The waiting promptFunc is released first, then scope
// "always_agent" persists the grant to the agent's ApprovedTools. That order
// is deliberate: persistAgent does disk I/O and toolbelt validation, and the
// gate abandons the prompt after promptFuncTimeout — making the agent's turn
// wait on a config write would risk timing the approval out. The in-memory
// AllowAll already covers the rest of this run; the persisted grant only
// affects future runs, so a failed write costs one extra prompt later, not a
// wrong decision now.
func (s *Server) handlePermissionResponse(msg WSMessage) {
	id := payloadString(msg.Payload, "id")
	if id == "" {
		id = payloadString(msg.Payload, "request_id")
	}
	if id == "" || s.permPrompts == nil {
		return
	}

	scope := payloadString(msg.Payload, "scope")
	if scope == "" {
		if parseBoolPayload(msg.Payload["approved"]) {
			scope = "once"
		} else {
			scope = "deny"
		}
	}

	var decision permissions.Decision
	switch scope {
	case "always_agent", "always_agent_all":
		decision = permissions.AllowAll
	case "once":
		decision = permissions.Allow
	default:
		decision = permissions.Deny
	}

	req, ok := s.permPrompts.resolve(id, decision)
	if !ok {
		return
	}

	if scope == "always_agent_all" && req.AgentName != "" {
		// MJ's "approve everything for this agent": the wildcard grant
		// suppresses exec prompting for every tool this agent runs, so the
		// human isn't asked once per tool. Persisted like any other grant;
		// removable as a "*" chip in the agent editor.
		if err := s.grantApprovedTool(req.AgentName, "*"); err != nil {
			slog.Error("permission prompt: failed to persist always-all grant",
				"agent", req.AgentName, "err", err)
		}
	} else if scope == "always_agent" && req.AgentName != "" && req.ToolName != "" {
		if err := s.grantApprovedTool(req.AgentName, req.ToolName); err != nil {
			slog.Error("permission prompt: failed to persist always-allow grant",
				"agent", req.AgentName, "tool", req.ToolName, "err", err)
		}
	}

	s.logEntityAudit("tool_permission", fmt.Sprintf("%s: %s", scope, req.ToolName), map[string]any{
		"tool":  req.ToolName,
		"agent": req.AgentName,
		"scope": scope,
	})
}

// grantApprovedTool adds toolName to agentName's persisted ApprovedTools list
// (idempotent) and saves it through the same path as a normal agent-config
// edit, so the change is picked up immediately (notifyAgentsChanged) and
// survives restarts.
func (s *Server) grantApprovedTool(agentName, toolName string) error {
	cfg, err := agents.LoadAgents()
	if err != nil {
		return fmt.Errorf("load agents: %w", err)
	}
	if cfg == nil {
		return fmt.Errorf("agent %q not found", agentName)
	}
	var def *agents.AgentDef
	for i := range cfg.Agents {
		if cfg.Agents[i].Name == agentName {
			def = &cfg.Agents[i]
			break
		}
	}
	if def == nil {
		return fmt.Errorf("agent %q not found", agentName)
	}
	for _, t := range def.ApprovedTools {
		if t == toolName {
			return nil // already granted
		}
	}
	def.ApprovedTools = append(def.ApprovedTools, toolName)
	_, err = s.persistAgent(*def, def.Name)
	return err
}
