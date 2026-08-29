package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/claudecode/approvals"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// auditedApprovalServer returns a Server whose audit log is a real SQLite file,
// plus a func that reads the most recent audit row back.
func auditedApprovalServer(t *testing.T, deadline time.Duration) (*Server, func(t *testing.T) (action, resource string, allowed bool, reason string)) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	s := &Server{auditLog: newAuditLogger(db)}
	s.approvals = approvals.New(deadline)
	t.Cleanup(s.approvals.Close)
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess",
		}}}, nil
	}

	read := func(t *testing.T) (string, string, bool, string) {
		t.Helper()
		s.auditLog.Close() // flushes
		row := db.Read().QueryRow(
			`SELECT action, resource, allowed, reason FROM audit_log ORDER BY id DESC LIMIT 1`)
		var action, resource string
		var allowedInt int
		var reason *string
		if err := row.Scan(&action, &resource, &allowedInt, &reason); err != nil {
			t.Fatalf("no audit row was written at all: %v", err)
		}
		r := ""
		if reason != nil {
			r = *reason
		}
		return action, resource, allowedInt == 1, r
	}
	return s, read
}

// TestApproveAuditsEveryOutcome is the accountability test.
//
// Only the deny path was audited, and it used one reason — claude_approval_denied
// — for BOTH a human clicking Deny and the deadline expiring. Nothing at all
// was written when a human ALLOWED, which is the outcome that grants a tool
// the agent's --allowedTools does not contain, and the one with no other
// record anywhere: Claude Code logs its own denials, never its allows.
func TestApproveAuditsEveryOutcome(t *testing.T) {
	for _, tc := range []struct {
		name       string
		decision   approvals.Decision
		wantAllow  bool
		wantReason string
	}{
		{"human allow", approvals.Allow, true, "claude_approval_allowed"},
		{"allow and remember the command", approvals.AllowCommand, true, "claude_approval_allowed_command"},
		{"allow and promote the tool", approvals.AllowTool, true, "claude_approval_allowed_tool"},
		{"human deny", approvals.Deny, false, "claude_approval_denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, readAudit := auditedApprovalServer(t, 10*time.Second)
			got := decideApproveInFlight(t, s,
				`{"tool_name":"Bash","session_id":"sess","summary":"go test ./..."}`, tc.decision)
			wantDecision := "deny"
			if tc.wantAllow {
				wantDecision = "allow"
			}
			if got != wantDecision {
				t.Fatalf("hook decision = %q, want %q", got, wantDecision)
			}

			action, resource, allowed, reason := readAudit(t)
			if action != "tool_permission" {
				t.Errorf("action = %q, want tool_permission", action)
			}
			if resource != "Bash" {
				t.Errorf("resource = %q, want the tool name", resource)
			}
			if allowed != tc.wantAllow {
				t.Errorf("allowed = %v, want %v", allowed, tc.wantAllow)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q — the audit trail must distinguish this "+
					"outcome from the others", reason, tc.wantReason)
			}
		})
	}
}

// TestApproveAuditsDeadlineAsPromptTimeout separates the two denials that used
// to be indistinguishable. "A human refused this" and "nobody was watching"
// are different facts about the operator, and the design doc names this exact
// string for the second one.
func TestApproveAuditsDeadlineAsPromptTimeout(t *testing.T) {
	s, readAudit := auditedApprovalServer(t, 80*time.Millisecond)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"ls"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	s.handleClaudeApprove(rr, req)

	action, resource, allowed, reason := readAudit(t)
	if action != "tool_permission" || resource != "Bash" {
		t.Errorf("action/resource = %q/%q, want tool_permission/Bash", action, resource)
	}
	if allowed {
		t.Error("allowed = true for an expired approval; nobody ever answered it")
	}
	if reason != "prompt_timeout" {
		t.Errorf("reason = %q, want prompt_timeout — an expired prompt is not a human "+
			"denial, and recording it as one misreports what the operator did", reason)
	}
}

// TestStopAuditsShutdownDenials pins spec §12 gap 3.
//
// Server.Stop closed the audit log BEFORE releasing parked approvals, so every
// denial produced by approvals.Close() landed in a channel whose drain
// goroutine had already exited and was dropped silently. A tool call was
// refused and the audit trail had no idea — observed live: a real gated Bash
// call was denied by a shutdown and left no row.
//
// It also pins gap 5: a shutdown denial must not be recorded as
// prompt_timeout. Both mean "no human answered", but only one means the
// deadline actually elapsed, and an operator reading the log needs to tell a
// five-minute no-show from a server that went away.
func TestStopAuditsShutdownDenials(t *testing.T) {
	// A deadline long enough that nothing can time out during this test, so a
	// row can only come from the shutdown path.
	s, read := auditedApprovalServer(t, 10*time.Minute)

	done := make(chan struct{})
	go func() {
		defer close(done)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/claude/approve",
			strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"echo hi"}`))
		req.RemoteAddr = "127.0.0.1:1234"
		s.handleClaudeApprove(rr, req)
	}()

	// Wait until the handler is genuinely parked, not merely dispatched.
	var parked bool
	for i := 0; i < 400; i++ {
		if len(s.approvals.List()) == 1 {
			parked = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !parked {
		t.Fatal("handler never registered a pending approval")
	}

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler still parked after Stop returned")
	}

	action, resource, allowed, reason := read(t)
	if action != "tool_permission" || resource != "Bash" {
		t.Fatalf("audit row = (%s,%s), want (tool_permission,Bash)", action, resource)
	}
	if allowed {
		t.Fatal("shutdown denial was audited as ALLOWED")
	}
	if reason == "prompt_timeout" {
		t.Fatal("shutdown denial recorded as prompt_timeout; the deadline had 10 minutes left")
	}
	if reason != "claude_approval_server_shutdown" {
		t.Fatalf("reason = %q, want claude_approval_server_shutdown", reason)
	}
}
