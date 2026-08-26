package claudecode

import "encoding/json"

// BuildHookSettings produces the inline JSON passed to `claude --settings`,
// registering one PreToolUse hook per gated tool.
//
// ONE ENTRY PER TOOL, not a combined matcher: a single-tool matcher is the only
// form verified against the CLI, and per-tool entries need no assumption about
// regex support.
//
// Tools NOT listed here are pre-authorised via --allowedTools and never invoke
// the hook. That is what makes fail-closed safe: if Huginn is unreachable, only
// the tools that always required a human are blocked, and the agent degrades to
// its pre-authorised capability instead of stopping dead.
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
