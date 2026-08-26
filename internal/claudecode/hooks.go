package claudecode

import "encoding/json"

// BuildHookSettings produces the inline JSON passed to `claude --settings`,
// registering one PreToolUse hook per gated tool.
//
// ONE ENTRY PER TOOL, not a combined matcher: a single-tool matcher is the only
// form verified against the CLI, and per-tool entries need no assumption about
// regex support.
//
// THE TWO LISTS OVERLAP BY DESIGN — do not "optimise away" a hook entry for a
// tool that is also in --allowedTools.
//
// A PreToolUse hook fires before any permission-mode check, regardless of
// whether the tool is allowlisted. So a tool in BOTH lists runs the hook AND is
// allowed: that is the documented audit-trail pattern ("let this run
// unattended, but record every call"), and deleting the apparently redundant
// hook entry deletes the audit trail. The default wiring overlaps heavily —
// AllowedTools is ag.ClaudeAllowedTools and GatedTools defaults to
// Bash/Write/Edit/NotebookEdit/WebFetch/Task — so this is the normal case, not
// an exotic one.
//
// Stated plainly, because it is easy to assume otherwise: --allowedTools and
// this hook produce THE SAME effective permission set. The approval endpoint
// allows exactly the tools in ClaudeAllowedTools, which is also what is passed
// to --allowedTools. The hook's contribution is a log entry and a
// human-readable deny reason, not additional restriction.
//
// A tool in NEITHER list is neither pre-authorised nor gated, and never invokes
// the hook. That is what keeps fail-closed tolerable: if Huginn is unreachable,
// the agent degrades to its pre-authorised capability instead of stopping dead.
//
// ClaudeHookTimeoutSecs bounds how long Claude Code waits for our approval
// hook. It is set EXPLICITLY rather than relying on the CLI's default,
// because a hook that times out FAILS OPEN: verified against the real CLI,
// a killed PreToolUse hook results in the tool being ALLOWED, with no entry
// in permission_denials. Callers that run `huginn claude-approve` (see
// cmd_claude_approve.go) must keep their own client timeout well below this
// so the hook always gets to print an explicit deny first.
const ClaudeHookTimeoutSecs = 30

// Returns "" when nothing is gated, so the caller omits --settings entirely.
func BuildHookSettings(gatedTools []string, hookCommand string) (string, error) {
	if len(gatedTools) == 0 {
		return "", nil
	}

	type hookCmd struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	type entry struct {
		Matcher string    `json:"matcher"`
		Hooks   []hookCmd `json:"hooks"`
	}

	entries := make([]entry, 0, len(gatedTools))
	for _, tool := range gatedTools {
		if tool == "" {
			continue
		}
		entries = append(entries, entry{
			Matcher: tool,
			Hooks:   []hookCmd{{Type: "command", Command: hookCommand, Timeout: ClaudeHookTimeoutSecs}},
		})
	}
	if len(entries) == 0 {
		return "", nil
	}

	payload := map[string]any{"hooks": map[string]any{"PreToolUse": entries}}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
