package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/claudecode"
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

// TestClaudeApproveTimeoutMarginIsPositiveAndSafe states, in the test suite
// (not just a comment), the invariant the compile-time guard in
// cmd_claude_approve.go also enforces: claudeApproveTimeout must be strictly
// positive (a zero time.Duration means "no timeout" to http.Client, which
// would silently reintroduce the fail-open race) and must leave at least 10s
// of headroom under claudecode.ClaudeHookTimeoutSecs, since Claude Code kills
// and ALLOWS a hook that exceeds its own timeout.
func TestClaudeApproveTimeoutMarginIsPositiveAndSafe(t *testing.T) {
	if claudeApproveTimeout <= 0 {
		t.Fatalf("claudeApproveTimeout = %v, want > 0 — a zero timeout means http.Client waits forever, reintroducing the fail-open race", claudeApproveTimeout)
	}
	hookTimeout := time.Duration(claudecode.ClaudeHookTimeoutSecs) * time.Second
	margin := hookTimeout - claudeApproveTimeout
	if margin < 10*time.Second {
		t.Errorf("margin between ClaudeHookTimeoutSecs (%v) and claudeApproveTimeout (%v) = %v, want >= 10s", hookTimeout, claudeApproveTimeout, margin)
	}
}

// TestClaudeApproveDeniesOnMalformedOrUnexpectedResponses covers the deny
// branches that were previously correct by inspection only: a non-200
// status, an empty 200 body, an invalid-JSON 200 body, a well-formed 200
// body missing the "decision" field, and a 200 body whose decision does not
// exactly match "allow" (case matters — the check is an exact string
// comparison, not case-insensitive). These are exactly the branches that
// must never regress into an accidental allow.
func TestClaudeApproveDeniesOnMalformedOrUnexpectedResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "server error", status: http.StatusInternalServerError, body: ""},
		{name: "empty 200 body", status: http.StatusOK, body: ""},
		{name: "invalid JSON body", status: http.StatusOK, body: "{"},
		{name: "valid JSON missing decision field", status: http.StatusOK, body: `{"reason":"no decision key here"}`},
		{name: "wrong-case decision must not match", status: http.StatusOK, body: `{"decision":"ALLOW"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			var out bytes.Buffer
			code := runClaudeApprove(strings.NewReader(hookStdin), &out, srv.URL, 5*time.Second)
			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			d, _ := decision(t, out.String())
			if d != "deny" {
				t.Errorf("decision = %q, want deny for response {status:%d body:%q}", d, tt.status, tt.body)
			}
		})
	}
}

func TestSummarizeBashSendsCommandVerbatim(t *testing.T) {
	sum, exc := summarizeToolInput("Bash", map[string]any{
		"command":     "go test ./...",
		"description": "run tests",
	})
	if sum != "go test ./..." {
		t.Fatalf("summary = %q, want the command verbatim", sum)
	}
	if exc != "" {
		t.Fatalf("excerpt = %q, want empty for Bash", exc)
	}
}

func TestSummarizeBashTruncatesLongCommand(t *testing.T) {
	long := strings.Repeat("x", maxCommandBytes+500)
	sum, _ := summarizeToolInput("Bash", map[string]any{"command": long})
	if len(sum) > maxCommandBytes {
		t.Fatalf("summary len = %d, want <= %d", len(sum), maxCommandBytes)
	}
}

func TestSummarizeWriteSendsPathAndBoundedContent(t *testing.T) {
	sum, exc := summarizeToolInput("Write", map[string]any{
		"file_path": "/tmp/a.ts",
		"content":   strings.Repeat("y", maxExcerptBytes+500),
	})
	if sum != "/tmp/a.ts" {
		t.Fatalf("summary = %q, want the file path", sum)
	}
	if len(exc) > maxExcerptBytes {
		t.Fatalf("excerpt len = %d, want <= %d", len(exc), maxExcerptBytes)
	}
}

func TestSummarizeEditUsesNewString(t *testing.T) {
	sum, exc := summarizeToolInput("Edit", map[string]any{
		"file_path":  "/tmp/b.go",
		"new_string": "package main",
	})
	if sum != "/tmp/b.go" {
		t.Fatalf("summary = %q, want the file path", sum)
	}
	if exc != "package main" {
		t.Fatalf("excerpt = %q, want the new_string", exc)
	}
}

func TestSummarizeUnknownToolFallsBackToBoundedJSON(t *testing.T) {
	sum, exc := summarizeToolInput("Task", map[string]any{
		"file_path": "/tmp/t.md",
		"prompt":    strings.Repeat("z", maxExcerptBytes+500),
	})
	if sum != "/tmp/t.md" {
		t.Fatalf("summary = %q, want the file_path", sum)
	}
	if len(exc) > maxExcerptBytes {
		t.Fatalf("excerpt len = %d, want <= %d", len(exc), maxExcerptBytes)
	}
	if !strings.Contains(exc, "prompt") {
		t.Fatalf("excerpt = %q, want a JSON excerpt naming the prompt key", exc)
	}
}

func TestSummarizeNilInputIsSafe(t *testing.T) {
	sum, exc := summarizeToolInput("Bash", nil)
	if sum != "" || exc != "" {
		t.Fatalf("got (%q,%q), want empty strings for nil input", sum, exc)
	}
}

func TestRunClaudeApproveForwardsSummary(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]string{"decision": "allow", "reason": "ok"})
	}))
	defer srv.Close()

	in := strings.NewReader(`{"tool_name":"Bash","session_id":"s","tool_input":{"command":"ls -la"}}`)
	var out bytes.Buffer
	if code := runClaudeApprove(in, &out, srv.URL, 5*time.Second); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("server got unparseable body: %v", err)
	}
	if sent["summary"] != "ls -la" {
		t.Fatalf("summary forwarded = %v, want %q", sent["summary"], "ls -la")
	}
}

func TestSummarizeNonStringValuesDoNotPanic(t *testing.T) {
	sum, exc := summarizeToolInput("Bash", map[string]any{"command": 42})
	if sum != "" {
		t.Fatalf("summary = %q, want empty when command is not a string", sum)
	}
	_ = exc
	// Write with a non-string file_path must also survive.
	if _, _ = summarizeToolInput("Write", map[string]any{"file_path": []any{1, 2}, "content": "x"}); false {
		t.Fatal("unreachable")
	}
}

func TestSummarizeClipsLongFilePath(t *testing.T) {
	long := strings.Repeat("p", maxCommandBytes+500)
	sum, _ := summarizeToolInput("Write", map[string]any{"file_path": long, "content": "x"})
	if len(sum) > maxCommandBytes {
		t.Fatalf("summary len = %d, want <= %d", len(sum), maxCommandBytes)
	}
}

func TestRunClaudeApproveToleratesNonObjectToolInput(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]string{"decision": "allow", "reason": "ok"})
	}))
	defer srv.Close()

	in := strings.NewReader(`{"tool_name":"Bash","session_id":"s","tool_input":"not-an-object"}`)
	var out bytes.Buffer
	if code := runClaudeApprove(in, &out, srv.URL, 5*time.Second); code != 0 {
		t.Fatalf("exit = %d, want 0 — a malformed tool_input must not break the request", code)
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("server got unparseable body: %v", err)
	}
	if sent["tool_name"] != "Bash" {
		t.Fatalf("tool_name = %v, want Bash — the request must still be forwarded", sent["tool_name"])
	}
}
