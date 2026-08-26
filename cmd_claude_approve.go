package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/scrypster/huginn/internal/claudecode"
)

// claudeApproveTimeout bounds how long the http.Client in runClaudeApprove
// waits for Huginn. It does NOT cover the rest of the process: main()'s
// preamble (config.Load(), etc.) runs before runClaudeApprove and outside
// this budget. That preamble only touches local disk, so its practical risk
// is low, but it means the real wall-clock time Claude Code sees before we
// print a decision is slightly more than this constant.
//
// It MUST stay comfortably below claudecode.ClaudeHookTimeoutSecs. Verified
// against the real CLI: when Claude Code's hook timeout fires it KILLS the hook
// and ALLOWS the tool — a timed-out hook fails OPEN. So our deny has to be
// printed before that happens; racing it is not acceptable for a security
// boundary.
const claudeApproveTimeout = (claudecode.ClaudeHookTimeoutSecs - 10) * time.Second

// Compile-time guard: claudeApproveTimeout must stay strictly positive and
// leave real headroom under the hook timeout. A zero Timeout means "no
// timeout" to http.Client, which would silently reintroduce the fail-open
// race this margin exists to prevent. If this line stops compiling, you
// lowered ClaudeHookTimeoutSecs too far — raise it, do not delete this.
const _ = uint(claudecode.ClaudeHookTimeoutSecs - 15)

// runClaudeApprove is the PreToolUse hook body. Claude Code writes the tool
// call to stdin and reads a decision from stdout.
//
// It ALWAYS exits 0 and ALWAYS emits a decision: a crashed or non-zero-exiting
// hook is itself interpreted as a block, but with no reason the user can read.
// Every failure path here emits an explicit deny naming the tool.
func runClaudeApprove(in io.Reader, out io.Writer, endpoint string, timeout time.Duration) int {
	// The read error is intentionally discarded: whatever was read (possibly
	// nothing, possibly a truncated partial read) still goes to json.Unmarshal
	// below, and a read error reliably yields an unparseable/incomplete
	// payload there, which already denies. No path here can turn a read
	// failure into an allow.
	raw, _ := io.ReadAll(in)

	var hook struct {
		ToolName  string          `json:"tool_name"`
		ToolUseID string          `json:"tool_use_id"`
		SessionID string          `json:"session_id"`
		CWD       string          `json:"cwd"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(raw, &hook); err != nil {
		emitDecision(out, "deny", "Huginn could not parse the tool call; denying")
		return 0
	}
	tool := hook.ToolName
	if tool == "" {
		tool = "tool"
	}

	body, err := json.Marshal(map[string]any{
		"tool_name":   hook.ToolName,
		"tool_use_id": hook.ToolUseID,
		"session_id":  hook.SessionID,
		"cwd":         hook.CWD,
		"tool_input":  hook.ToolInput,
	})
	if err != nil {
		emitDecision(out, "deny", fmt.Sprintf("Huginn could not encode the request; %s denied", tool))
		return 0
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		emitDecision(out, "deny", fmt.Sprintf("Huginn unreachable — %s requires approval", tool))
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		emitDecision(out, "deny", fmt.Sprintf("Huginn returned %d — %s requires approval", resp.StatusCode, tool))
		return 0
	}

	var decided struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decided); err != nil {
		emitDecision(out, "deny", fmt.Sprintf("Huginn sent an unreadable decision — %s denied", tool))
		return 0
	}
	if decided.Decision != "allow" {
		reason := decided.Reason
		if reason == "" {
			reason = fmt.Sprintf("Huginn denied %s", tool)
		}
		emitDecision(out, "deny", reason)
		return 0
	}

	emitDecision(out, "allow", decided.Reason)
	return 0
}

func emitDecision(out io.Writer, decision, reason string) {
	payload := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       decision,
			"permissionDecisionReason": reason,
		},
	}
	b, _ := json.Marshal(payload)
	fmt.Fprintln(out, string(b))
}
