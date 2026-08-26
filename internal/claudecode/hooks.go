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
// Returns "" when nothing is gated, so the caller omits --settings entirely.
func BuildHookSettings(gatedTools []string, hookCommand string) (string, error) {
	if len(gatedTools) == 0 {
		return "", nil
	}

	type hookCmd struct {
		Type    string `json:"type"`
		Command string `json:"command"`
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
			Hooks:   []hookCmd{{Type: "command", Command: hookCommand}},
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
