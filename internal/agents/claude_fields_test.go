package agents

import (
	"encoding/json"
	"testing"
)

func TestAgentConfigRoundTripsClaudeFields(t *testing.T) {
	in := AgentDef{
		Name:            "Elena",
		Provider:        "claude-code",
		ClaudeSessionID: "11111111-2222-3333-4444-555555555555",
		ClaudeCWD:       "/Users/dev/project",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out AgentDef
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ClaudeSessionID != in.ClaudeSessionID {
		t.Errorf("ClaudeSessionID = %q, want %q", out.ClaudeSessionID, in.ClaudeSessionID)
	}
	if out.ClaudeCWD != in.ClaudeCWD {
		t.Errorf("ClaudeCWD = %q, want %q", out.ClaudeCWD, in.ClaudeCWD)
	}
}

func TestClaudeFieldsAreOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(AgentDef{Name: "Native", Provider: "anthropic"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, k := range []string{"claude_session_id", "claude_cwd"} {
		if contains(s, k) {
			t.Errorf("native agent JSON contains %q; both fields must be omitempty: %s", k, s)
		}
	}
}

func contains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
