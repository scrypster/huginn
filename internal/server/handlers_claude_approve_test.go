package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func postApprove(t *testing.T, s *Server, body string) (int, string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claude/approve", strings.NewReader(body))
	// httptest.NewRequest defaults RemoteAddr to a non-loopback address
	// (192.0.2.1:1234), which handleClaudeApprove now denies on principle.
	// Tests that want to exercise logic past the loopback check must look
	// like a real local hook call.
	req.RemoteAddr = "127.0.0.1:12345"
	s.handleClaudeApprove(rec, req)
	var out struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&out)
	return rec.Code, out.Decision, out.Reason
}

func TestApproveDeniesUnknownSession(t *testing.T) {
	s := &Server{}
	code, decision, reason := postApprove(t, s,
		`{"tool_name":"Write","session_id":"nobody","tool_use_id":"t1"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the hook needs a parseable body even on refusal", code)
	}
	if decision != "deny" {
		t.Errorf("decision = %q, want deny for a session bound to no agent", decision)
	}
	if reason == "" {
		t.Error("reason must be populated so Claude Code can tell the user why")
	}
}

func TestApproveDeniesMalformedBody(t *testing.T) {
	s := &Server{}
	_, decision, reason := postApprove(t, s, `not json`)
	if decision != "deny" {
		t.Errorf("decision = %q, want deny", decision)
	}
	// Assert the reason too, not just the decision: a handler that ignores
	// json.Decode's error and falls through to the unbound-session deny would
	// still pass on decision alone (Server{} has a nil agentLoader either
	// way), but it would carry the wrong reason — "no agent bound" instead of
	// "could not parse". Pinning the reason proves the parse error was
	// actually detected, not stumbled into.
	if !strings.Contains(reason, "could not parse") {
		t.Errorf("reason = %q, want it to name the parse failure", reason)
	}
}

func TestApproveDeniesNonLoopback(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Write","session_id":"nobody","tool_use_id":"t1"}`))
	// Deliberately NOT overriding RemoteAddr — httptest.NewRequest's default
	// (192.0.2.1:1234) is a real, non-loopback IP. This is the case the
	// loopback check exists for: a request that did not originate from the
	// local hook process.
	s.handleClaudeApprove(rec, req)

	var out struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&out)

	if out.Decision != "deny" {
		t.Errorf("decision = %q, want deny for a non-loopback remote address", out.Decision)
	}
	if !strings.Contains(out.Reason, "localhost") {
		t.Errorf("reason = %q, want it to name the loopback restriction", out.Reason)
	}
}

func TestApproveDeniesNonLoopback_IPv6(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Write","session_id":"nobody","tool_use_id":"t1"}`))
	req.RemoteAddr = "2001:db8::1:1234" // not loopback, and not host:port-shaped
	s.handleClaudeApprove(rec, req)

	var out struct {
		Decision string `json:"decision"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out.Decision != "deny" {
		t.Errorf("decision = %q, want deny for an unparseable/non-loopback RemoteAddr", out.Decision)
	}
}

func TestApproveAllowsLoopbackIPv6(t *testing.T) {
	s := serverWithAgent(t, "sess-1", []string{"Write"}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Write","session_id":"sess-1","tool_use_id":"t1"}`))
	req.RemoteAddr = "[::1]:54321"
	s.handleClaudeApprove(rec, req)

	var out struct {
		Decision string `json:"decision"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out.Decision != "allow" {
		t.Errorf("decision = %q, want allow for an IPv6 loopback caller with a permitted tool", out.Decision)
	}
}

// serverWithAgent returns a *Server whose agentLoader serves a single agent
// bound to sessionID with the given ClaudeAllowedTools (the Claude Code CLI
// tool-name allowlist checked by handleClaudeApprove) and localTools (the
// unrelated Huginn-builtin-tool allowlist — pass nil unless a test needs it,
// e.g. to prove the two are decoupled).
func serverWithAgent(t *testing.T, sessionID string, claudeAllowedTools, localTools []string) *Server {
	t.Helper()
	return &Server{
		agentLoader: func() (*agents.AgentsConfig, error) {
			return &agents.AgentsConfig{
				Agents: []agents.AgentDef{
					{
						Name:               "claude-agent",
						ClaudeSessionID:    sessionID,
						ClaudeAllowedTools: claudeAllowedTools,
						LocalTools:         localTools,
					},
				},
			}, nil
		},
	}
}

func TestApproveDeniesToolNotInAllowlist(t *testing.T) {
	s := serverWithAgent(t, "sess-1", []string{"Write"}, nil)
	code, decision, reason := postApprove(t, s,
		`{"tool_name":"Bash","session_id":"sess-1","tool_use_id":"t1"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if decision != "deny" {
		t.Errorf("decision = %q, want deny — Bash is not in this agent's ClaudeAllowedTools", decision)
	}
	if !strings.Contains(reason, "Bash") {
		t.Errorf("reason = %q, want it to name the denied tool", reason)
	}
}

// TestApproveAllowsToolInAllowlist is the mirror of
// TestApproveDeniesToolNotInAllowlist: same bound session, but the requested
// tool IS in the agent's ClaudeAllowedTools. Only running both proves the
// branch discriminates on the tool name rather than denying unconditionally.
func TestApproveAllowsToolInAllowlist(t *testing.T) {
	s := serverWithAgent(t, "sess-1", []string{"Write"}, nil)
	code, decision, reason := postApprove(t, s,
		`{"tool_name":"Write","session_id":"sess-1","tool_use_id":"t1"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if decision != "allow" {
		t.Errorf("decision = %q, want allow — Write is in this agent's ClaudeAllowedTools", decision)
	}
	if reason == "" {
		t.Error("reason must be populated even on allow")
	}
}

func TestApproveDeniesWhenClaudeAllowedToolsEmpty(t *testing.T) {
	s := serverWithAgent(t, "sess-1", nil, nil)
	_, decision, _ := postApprove(t, s,
		`{"tool_name":"Write","session_id":"sess-1","tool_use_id":"t1"}`)
	if decision != "deny" {
		t.Errorf("decision = %q, want deny — nil ClaudeAllowedTools is default-deny", decision)
	}
}

// TestApproveDeniesClaudeAllowedToolsWildcard is the regression test for the
// finding this whole gate exists to fix: ClaudeAllowedTools must NEVER honour
// "*". Do not "restore" wildcard support here — a wildcard on an unattended
// agent's tool gate is a privilege-escalation path (see the field's doc
// comment in internal/agents/config.go).
func TestApproveDeniesClaudeAllowedToolsWildcard(t *testing.T) {
	s := serverWithAgent(t, "sess-1", []string{"*"}, nil)
	_, decision, reason := postApprove(t, s,
		`{"tool_name":"Bash","session_id":"sess-1","tool_use_id":"t1"}`)
	if decision != "deny" {
		t.Errorf(`decision = %q, want deny — ClaudeAllowedTools:["*"] must not grant Bash`, decision)
	}
	if !strings.Contains(reason, "Bash") {
		t.Errorf("reason = %q, want it to name the denied tool", reason)
	}
}

// TestApproveDeniesDespiteLocalToolsWildcard proves ClaudeAllowedTools and
// LocalTools are fully decoupled: an agent whose Huginn-builtin-tool
// allowlist is wide open ("*") gets no Claude Code CLI tool access at all
// unless ClaudeAllowedTools separately grants it. Before ClaudeAllowedTools
// existed, this endpoint mistakenly checked LocalTools directly, so
// LocalTools:["*"] (meaning "all Huginn tools") would have granted every
// Claude Code CLI tool, including Bash — this test is what catches that
// conflation if it's ever reintroduced.
func TestApproveDeniesDespiteLocalToolsWildcard(t *testing.T) {
	s := serverWithAgent(t, "sess-1", nil, []string{"*"})
	_, decision, _ := postApprove(t, s,
		`{"tool_name":"Bash","session_id":"sess-1","tool_use_id":"t1"}`)
	if decision != "deny" {
		t.Errorf(`decision = %q, want deny — LocalTools:["*"] must not leak into Claude Code tool approval`, decision)
	}
}
