package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/claudecode"
	"github.com/scrypster/huginn/internal/config"
)

// THE PRETOOLUSE EXIT-CODE CONTRACT — read this before changing anything here.
//
// Claude Code interprets a PreToolUse hook's exit code as follows:
//
//	0  — stdout is parsed as JSON for a permission decision. An explicit
//	     {"permissionDecision":"deny"} BLOCKS the tool. This is the path this
//	     command takes, and it is empirically verified against the real CLI.
//	     Exit 0 with no parseable decision means "normal permission flow" —
//	     it neither approves nor blocks.
//	2  — blocking error. THE ONLY EXIT CODE THAT BLOCKS BY ITSELF.
//	any other (1, 127, killed, …) — NON-BLOCKING error. The error is logged
//	     and THE ACTION PROCEEDS. A hook that dies before it prints does not
//	     block anything; it fails OPEN.
//
// So the invariant this file exists to hold is: the `huginn claude-approve`
// process must ALWAYS print a decision and exit 0, and when it cannot print at
// all it must exit 2 — the only remaining way to say "no".
//
// Corollary: nothing on this command's route may os.Exit non-zero. That is why
// main() dispatches `claude-approve` before it loads config or does anything
// else that can call fatalf, and why claudeApproveMain recovers from panics.

// claudeApproveTimeout bounds how long the http.Client in runClaudeApprove
// waits for Huginn. It does NOT cover the rest of the process: claudeApproveMain
// resolves the endpoint before runClaudeApprove and outside this budget. That
// step only reads local disk (and only when --endpoint was not supplied, which
// the hook command Huginn generates always supplies), so its practical risk is
// low, but it means the real wall-clock time Claude Code sees before we print a
// decision is slightly more than this constant.
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

// claudeApprovePath is the approval endpoint's path on the Huginn server.
const claudeApprovePath = "/api/v1/claude/approve"

// claudeApproveMain is the WHOLE of the `huginn claude-approve` subcommand: it
// is dispatched from the very top of main(), before flag parsing, before
// config.Load(), before the logger — before anything that can os.Exit non-zero.
// See the exit-code contract above for why that ordering is load-bearing.
//
// args is os.Args after the subcommand name. It accepts --endpoint <url>
// (equivalently --endpoint=<url>, and the single-dash spellings), which is what
// the generated hook command always passes: the endpoint carries the server's
// REAL bound address, so a web_ui.port of 0 (dynamic allocation) still reaches
// the running server. Without it we fall back to a best-effort config read,
// and a config we cannot read yields an explicit deny rather than a crash.
//
// The named return plus deferred recover is the last-resort guard: an
// unexpected panic anywhere below still prints a deny and exits 0. Only a
// process that cannot print at all exits 2.
func claudeApproveMain(args []string, in io.Reader, out io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			code = emitDecision(out, "deny",
				fmt.Sprintf("Huginn's approval hook crashed (%v); denying", r))
		}
	}()

	endpoint := claudeApproveEndpoint(args)
	if endpoint == "" {
		// Reachable when --endpoint was omitted AND ~/.huginn/config.json is
		// missing/corrupt. Historically this route ran config.Load() inside
		// main()'s preamble and fatalf'd — exit 1, no decision printed, tool
		// ALLOWED. Denying here is the whole point of this branch.
		return emitDecision(out, "deny",
			"Huginn could not determine its approval endpoint (no --endpoint and unreadable config); denying")
	}
	return runClaudeApprove(in, out, endpoint, claudeApproveTimeout)
}

// claudeApproveEndpoint resolves the approval URL for this invocation.
//
// Order: explicit --endpoint (always present in the hook command Huginn
// generates, and carrying the server's actual bound address) → a best-effort
// read of ~/.huginn/config.json for a hand-run invocation. Every error is
// swallowed into "" so the caller denies; NOTHING here may exit.
func claudeApproveEndpoint(args []string) string {
	if ep := endpointFromArgs(args); ep != "" {
		return ep
	}
	cfg, err := config.Load()
	if err != nil || cfg == nil || cfg.WebUI.Port == 0 {
		// A zero port means the server allocates dynamically, so config
		// cannot tell us where it landed — only --endpoint can. Guessing a
		// port here would deny every gated tool while the server looks
		// perfectly healthy, which is Finding 2 all over again.
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d%s", cfg.WebUI.Port, claudeApprovePath)
}

// endpointFromArgs pulls --endpoint out of the subcommand's arguments,
// accepting "--endpoint url", "--endpoint=url" and their single-dash forms.
func endpointFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := strings.TrimLeft(a, "-")
		if a == name { // not a flag at all
			continue
		}
		if k, v, ok := strings.Cut(name, "="); ok {
			if k == "endpoint" {
				return v
			}
			continue
		}
		if name == "endpoint" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

const (
	// maxCommandBytes bounds a forwarded Bash command.
	maxCommandBytes = 4 << 10
	// maxExcerptBytes bounds every other forwarded content excerpt.
	maxExcerptBytes = 2 << 10
)

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func inputString(in map[string]any, key string) string {
	v, _ := in[key].(string)
	return v
}

// summarizeToolInput reduces a tool_input to a bounded, human-readable pair.
//
// TRUNCATION HAPPENS HERE, IN THE HOOK, ON PURPOSE. An earlier version
// forwarded the whole tool_input, and a large Write blew the route's body cap,
// failed to decode, and DENIED an explicitly allowlisted tool — a permission
// decision that depended on payload size. Bounding it at the source is what
// makes that impossible. Never forward the raw input, and never let the server
// reject on size.
func summarizeToolInput(toolName string, in map[string]any) (summary, excerpt string) {
	if in == nil {
		return "", ""
	}
	switch toolName {
	case "Bash":
		return clip(inputString(in, "command"), maxCommandBytes), ""
	case "Write":
		return inputString(in, "file_path"), clip(inputString(in, "content"), maxExcerptBytes)
	case "Edit", "NotebookEdit":
		return inputString(in, "file_path"), clip(inputString(in, "new_string"), maxExcerptBytes)
	case "WebFetch":
		return inputString(in, "url"), ""
	}
	summary = inputString(in, "file_path")
	if summary == "" {
		summary = inputString(in, "url")
	}
	b, err := json.Marshal(in)
	if err != nil {
		return summary, ""
	}
	return summary, clip(string(b), maxExcerptBytes)
}

// runClaudeApprove is the PreToolUse hook body. Claude Code writes the tool
// call to stdin and reads a decision from stdout.
//
// It ALWAYS emits a decision and returns 0 when it managed to print it. A
// non-zero-exiting hook is NOT interpreted as a block — see the exit-code
// contract at the top of this file — so the printed deny is the only thing
// that actually stops a tool. The single exception is a stdout we cannot write
// to: then there is no decision to read, and 2 (the one blocking exit code) is
// the only remaining way to refuse.
func runClaudeApprove(in io.Reader, out io.Writer, endpoint string, timeout time.Duration) int {
	// The read error is intentionally discarded: whatever was read (possibly
	// nothing, possibly a truncated partial read) still goes to json.Unmarshal
	// below, and a read error reliably yields an unparseable/incomplete
	// payload there, which already denies. No path here can turn a read
	// failure into an allow.
	raw, _ := io.ReadAll(in)

	var hook struct {
		ToolName  string         `json:"tool_name"`
		ToolUseID string         `json:"tool_use_id"`
		SessionID string         `json:"session_id"`
		CWD       string         `json:"cwd"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if err := json.Unmarshal(raw, &hook); err != nil {
		return emitDecision(out, "deny", "Huginn could not parse the tool call; denying")
	}
	tool := hook.ToolName
	if tool == "" {
		tool = "tool"
	}

	// tool_input is forwarded as a BOUNDED SUMMARY, never raw. See
	// summarizeToolInput for why the truncation lives here rather than on the
	// server: permission must never depend on payload size.
	summary, excerpt := summarizeToolInput(hook.ToolName, hook.ToolInput)
	body, err := json.Marshal(map[string]any{
		"tool_name":   hook.ToolName,
		"tool_use_id": hook.ToolUseID,
		"session_id":  hook.SessionID,
		"cwd":         hook.CWD,
		"summary":     summary,
		"excerpt":     excerpt,
	})
	if err != nil {
		return emitDecision(out, "deny", fmt.Sprintf("Huginn could not encode the request; %s denied", tool))
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return emitDecision(out, "deny", fmt.Sprintf("Huginn unreachable — %s requires approval", tool))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return emitDecision(out, "deny", fmt.Sprintf("Huginn returned %d — %s requires approval", resp.StatusCode, tool))
	}

	var decided struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decided); err != nil {
		return emitDecision(out, "deny", fmt.Sprintf("Huginn sent an unreadable decision — %s denied", tool))
	}
	if decided.Decision != "allow" {
		reason := decided.Reason
		if reason == "" {
			reason = fmt.Sprintf("Huginn denied %s", tool)
		}
		return emitDecision(out, "deny", reason)
	}

	return emitDecision(out, "allow", decided.Reason)
}

// emitDecision prints the decision and returns the process exit code to use.
//
// It returns 0 when the decision was written — exit 0 plus a JSON decision on
// stdout is the path Claude Code actually parses, and the only way to deny
// with a reason a human can read.
//
// It returns 2 when the decision could NOT be written. 2 is the only exit code
// Claude Code treats as blocking; every other non-zero code is a "non-blocking
// error" that lets the tool run. So when we have no voice, 2 is the voice.
func emitDecision(out io.Writer, decision, reason string) int {
	payload := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       decision,
			"permissionDecisionReason": reason,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return 2
	}
	if _, err := fmt.Fprintln(out, string(b)); err != nil {
		return 2
	}
	return 0
}
