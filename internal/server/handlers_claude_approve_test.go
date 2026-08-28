package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/claudecode/approvals"
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
	// agentLoader is nil here, which now reaches the production loader, so
	// point it at an empty private home rather than the developer's real one.
	isolateAgentsHome(t)
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

// isolateAgentsHome points agents.LoadAgents at a private directory for the
// duration of the test, so the production loader can be exercised without
// reading — or creating anything in — the developer's real ~/.huginn.
// Returns the agents directory to write agent files into.
func isolateAgentsHome(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HUGINN_HOME", base)
	dir := filepath.Join(base, ".huginn", "agents")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir agents dir: %v", err)
	}
	return dir
}

// TestApproveResolvesTheAgentThroughTheProductionLoader pins the WIRED case.
//
// agentLoader is a test seam: nothing in production ever sets it, so it is
// always nil there. The handler used to treat nil as "no agents exist" and
// deny every approval — and the suite could not see it, because every test
// injected a loader and the ones that did not were asserting denials that a
// nil loader produces for free. So this test deliberately leaves agentLoader
// nil, exactly as production does, and asserts the loader being PRESENT is
// what makes approval work.
func TestApproveResolvesTheAgentThroughTheProductionLoader(t *testing.T) {
	dir := isolateAgentsHome(t)
	const sessionID = "11111111-2222-3333-4444-555555555555"
	agentFile := filepath.Join(dir, "Codey.json")
	body := `{"name":"Codey","provider":"claude-code",` +
		`"claude_session_id":"` + sessionID + `",` +
		`"claude_allowed_tools":["Read"]}`
	if err := os.WriteFile(agentFile, []byte(body), 0o600); err != nil {
		t.Fatalf("write agent file: %v", err)
	}

	s := &Server{} // agentLoader deliberately nil — this IS production.

	_, decision, reason := postApprove(t, s,
		`{"tool_name":"Read","session_id":"`+sessionID+`","tool_use_id":"t1"}`)
	if decision != "allow" {
		t.Fatalf("decision = %q (%s), want allow — the handler never found the agent on disk", decision, reason)
	}
	// The allow reason names the agent, which is only possible if the agent
	// was actually loaded rather than the request being waved through.
	if !strings.Contains(reason, "Codey") {
		t.Errorf("reason = %q, want it to name the agent the decision came from", reason)
	}

	// Same wiring, same agent, a tool that is NOT allowlisted: still denied.
	// Proves the fallback loader did not turn into a blanket allow.
	_, decision, reason = postApprove(t, s,
		`{"tool_name":"Bash","session_id":"`+sessionID+`","tool_use_id":"t2"}`)
	if decision != "deny" {
		t.Errorf("decision = %q, want deny for a tool outside claude_allowed_tools", decision)
	}
	if strings.Contains(reason, "No Huginn agent is bound") {
		t.Errorf("denied for the wrong reason (%q): the agent was found, the TOOL was not allowed", reason)
	}
}

// TestApproveDeniesUnknownSessionWithAgentsOnDisk is the negative half of the
// test above: with the production loader wired and real agents present, a
// session bound to none of them is still denied.
func TestApproveDeniesUnknownSessionWithAgentsOnDisk(t *testing.T) {
	dir := isolateAgentsHome(t)
	body := `{"name":"Codey","provider":"claude-code","claude_session_id":"SOME-OTHER-SESSION","claude_allowed_tools":["Read"]}`
	if err := os.WriteFile(filepath.Join(dir, "Codey.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write agent file: %v", err)
	}

	s := &Server{}
	_, decision, reason := postApprove(t, s,
		`{"tool_name":"Read","session_id":"unbound-session","tool_use_id":"t1"}`)
	if decision != "deny" {
		t.Fatalf("decision = %q, want deny for a session no agent claims", decision)
	}
	if !strings.Contains(reason, "No Huginn agent is bound") {
		t.Errorf("reason = %q, want it to name the unbound session", reason)
	}
}

func TestApproveBlocksThenAllowsOnDeliver(t *testing.T) {
	s := &Server{}
	s.approvals = approvals.New(2 * time.Second)
	defer s.approvals.Close()
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess", ClaudeAllowedTools: []string{"Read"},
		}}}, nil
	}

	done := make(chan string, 1)
	go func() {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/claude/approve",
			strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"ls"}`))
		req.RemoteAddr = "127.0.0.1:1234"
		s.handleClaudeApprove(rr, req)
		var got map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &got)
		done <- got["decision"]
	}()

	var id string
	for i := 0; i < 200; i++ {
		if v := s.approvals.List(); len(v) == 1 {
			id = v[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("handler never registered a pending approval")
	}
	if !s.approvals.Deliver(id, approvals.Allow) {
		t.Fatal("Deliver failed")
	}
	if got := <-done; got != "allow" {
		t.Fatalf("decision = %q, want allow", got)
	}
}

func TestApproveDeniesOnDeadline(t *testing.T) {
	s := &Server{}
	// Non-zero: a zero deadline denies instantly for the wrong reason and this
	// test would pass against a handler that never blocks at all.
	s.approvals = approvals.New(80 * time.Millisecond)
	defer s.approvals.Close()
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess",
		}}}, nil
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"ls"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	start := time.Now()
	s.handleClaudeApprove(rr, req)
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Fatalf("handler returned after %v; it did not block", elapsed)
	}
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["decision"] != "deny" {
		t.Fatalf("decision = %q, want deny", got["decision"])
	}
}

func TestApproveAllowlistedToolNeverRegisters(t *testing.T) {
	s := &Server{}
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess", ClaudeAllowedTools: []string{"Bash"},
		}}}, nil
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	s.handleClaudeApprove(rr, req)
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["decision"] != "allow" {
		t.Fatalf("decision = %q, want allow", got["decision"])
	}
	if v := s.approvals.List(); len(v) != 0 {
		t.Fatalf("allowlisted tool registered %d pending approvals, want 0", len(v))
	}
}

func TestApproveRememberedCommandNeverRegisters(t *testing.T) {
	s := &Server{}
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	s.approvals.Remember("codey", "Bash", "ls -la")
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess",
		}}}, nil
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"ls -la"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	s.handleClaudeApprove(rr, req)
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["decision"] != "allow" {
		t.Fatalf("decision = %q, want allow", got["decision"])
	}
	if v := s.approvals.List(); len(v) != 0 {
		t.Fatalf("remembered command registered %d pending approvals, want 0", len(v))
	}
}

func TestApproveNilStoreDenies(t *testing.T) {
	s := &Server{}
	s.approvals = nil // production misconfiguration: must fail CLOSED
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess",
		}}}, nil
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	s.handleClaudeApprove(rr, req)
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["decision"] != "deny" {
		t.Fatalf("decision = %q, want deny", got["decision"])
	}
}
