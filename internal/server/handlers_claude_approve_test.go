package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postApprove(t *testing.T, s *Server, body string) (int, string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claude/approve", strings.NewReader(body))
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
	_, decision, _ := postApprove(t, s, `not json`)
	if decision != "deny" {
		t.Errorf("decision = %q, want deny", decision)
	}
}
