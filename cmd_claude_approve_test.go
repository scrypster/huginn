package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const hookStdin = `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_use_id":"tu1","session_id":"s1","cwd":"/tmp","tool_input":{"file_path":"/tmp/x","content":"hi"}}`

func decision(t *testing.T, out string) (string, string) {
	t.Helper()
	var d struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &d); err != nil {
		t.Fatalf("hook output is not valid JSON: %v\n%s", err, out)
	}
	if d.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", d.HookSpecificOutput.HookEventName)
	}
	return d.HookSpecificOutput.PermissionDecision, d.HookSpecificOutput.PermissionDecisionReason
}

func TestClaudeApproveAllows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"decision":"allow"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	code := runClaudeApprove(strings.NewReader(hookStdin), &out, srv.URL, 5*time.Second)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 — a non-zero exit is itself a block signal", code)
	}
	if d, _ := decision(t, out.String()); d != "allow" {
		t.Errorf("decision = %q, want allow", d)
	}
}

func TestClaudeApproveDenies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"decision":"deny","reason":"user declined"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	runClaudeApprove(strings.NewReader(hookStdin), &out, srv.URL, 5*time.Second)
	d, reason := decision(t, out.String())
	if d != "deny" {
		t.Errorf("decision = %q, want deny", d)
	}
	if !strings.Contains(reason, "user declined") {
		t.Errorf("reason = %q, want it to carry the server's reason", reason)
	}
}

func TestClaudeApproveDeniesWhenHuginnUnreachable(t *testing.T) {
	var out bytes.Buffer
	// Port 1 is reserved and will refuse instantly.
	code := runClaudeApprove(strings.NewReader(hookStdin), &out, "http://127.0.0.1:1", 2*time.Second)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	d, reason := decision(t, out.String())
	if d != "deny" {
		t.Fatalf("decision = %q, want deny — an unreachable Huginn must NEVER approve", d)
	}
	if !strings.Contains(reason, "Write") {
		t.Errorf("reason = %q, want it to name the tool so the user knows what was blocked", reason)
	}
}

func TestClaudeApproveDeniesOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte(`{"decision":"allow"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	start := time.Now()
	runClaudeApprove(strings.NewReader(hookStdin), &out, srv.URL, 300*time.Millisecond)
	if el := time.Since(start); el > 1500*time.Millisecond {
		t.Errorf("took %v; the timeout was not honoured", el)
	}
	if d, _ := decision(t, out.String()); d != "deny" {
		t.Errorf("decision = %q, want deny on timeout", d)
	}
}

func TestClaudeApproveDeniesOnGarbageStdin(t *testing.T) {
	var out bytes.Buffer
	runClaudeApprove(strings.NewReader("not json"), &out, "http://127.0.0.1:1", time.Second)
	if d, _ := decision(t, out.String()); d != "deny" {
		t.Errorf("decision = %q, want deny — unparseable input must not approve", d)
	}
}
