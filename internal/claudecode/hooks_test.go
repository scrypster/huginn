package claudecode

import (
	"encoding/json"
	"testing"
)

func TestBuildHookSettingsOneEntryPerGatedTool(t *testing.T) {
	out, err := BuildHookSettings([]string{"Write", "Bash"}, "/usr/local/bin/huginn claude-approve")
	if err != nil {
		t.Fatalf("BuildHookSettings: %v", err)
	}
	var got struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
					Timeout int    `json:"timeout"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Hooks.PreToolUse) != 2 {
		t.Fatalf("got %d PreToolUse entries, want 2 — one per gated tool", len(got.Hooks.PreToolUse))
	}
	seen := map[string]bool{}
	for _, e := range got.Hooks.PreToolUse {
		seen[e.Matcher] = true
		if len(e.Hooks) != 1 || e.Hooks[0].Type != "command" {
			t.Errorf("entry %q malformed: %+v", e.Matcher, e)
		}
		if e.Hooks[0].Command != "/usr/local/bin/huginn claude-approve" {
			t.Errorf("command = %q", e.Hooks[0].Command)
		}
		// Verified against the real CLI: a PreToolUse hook that Claude Code
		// kills for exceeding its timeout results in the tool being ALLOWED
		// (fails OPEN), not blocked. Every emitted hook entry must therefore
		// carry an explicit timeout so `huginn claude-approve` is guaranteed
		// a window to print its own deny first. Do not drop this assertion.
		if e.Hooks[0].Timeout != ClaudeHookTimeoutSecs {
			t.Errorf("entry %q timeout = %d, want %d (explicit, so a killed hook can never silently fail open)", e.Matcher, e.Hooks[0].Timeout, ClaudeHookTimeoutSecs)
		}
	}
	if !seen["Write"] || !seen["Bash"] {
		t.Errorf("matchers = %v, want Write and Bash", seen)
	}
}

func TestBuildHookSettingsNoGatedToolsMeansNoHooks(t *testing.T) {
	out, err := BuildHookSettings(nil, "irrelevant")
	if err != nil {
		t.Fatalf("BuildHookSettings: %v", err)
	}
	if out != "" {
		t.Errorf("got %q, want empty so --settings is omitted entirely when nothing is gated", out)
	}
}
