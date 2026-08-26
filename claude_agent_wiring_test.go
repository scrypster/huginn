package main

import (
	"os"
	"path/filepath"
	"testing"

	agentslib "github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/claudecode"
)

// TestGatedToolsForUsesTheAgentsExplicitList: a configured list wins outright,
// with no silent union against the defaults.
func TestGatedToolsForUsesTheAgentsExplicitList(t *testing.T) {
	ag := &agentslib.Agent{ClaudeGatedTools: []string{"Bash"}}
	got := gatedToolsFor(ag)
	if len(got) != 1 || got[0] != "Bash" {
		t.Fatalf("gatedToolsFor = %v, want exactly [Bash]", got)
	}
}

// TestGatedToolsForNeverReturnsEmpty is the security-critical case.
//
// BuildHookSettings emits NO PreToolUse hooks for an empty list, so an agent
// with no configured gating would run every tool — Bash included — with no
// approval round-trip at all. This test fails if the empty case returns nil or
// an empty slice, and it proves the fallback actually produces hooks.
func TestGatedToolsForNeverReturnsEmpty(t *testing.T) {
	cases := map[string]*agentslib.Agent{
		"nil agent":         nil,
		"unset gated tools": {Name: "Unconfigured"},
		"empty gated tools": {Name: "Empty", ClaudeGatedTools: []string{}},
		// LocalTools must not be consulted: it is Huginn's namespace, and its
		// "*" wildcard means "all Huginn builtins", never "no Claude gating".
		"wildcard local tools": {Name: "Wild", LocalTools: []string{"*"}},
	}
	for name, ag := range cases {
		t.Run(name, func(t *testing.T) {
			got := gatedToolsFor(ag)
			if len(got) == 0 {
				t.Fatal("gatedToolsFor returned an empty set: BuildHookSettings would emit no hooks and the agent would run ungated")
			}
			settings, err := claudecode.BuildHookSettings(got, "huginn claude-approve")
			if err != nil {
				t.Fatalf("BuildHookSettings: %v", err)
			}
			if settings == "" {
				t.Fatal("the fallback gated set produced no --settings payload, i.e. no approval hook")
			}
			var sawBash bool
			for _, tool := range got {
				if tool == "Bash" {
					sawBash = true
				}
			}
			if !sawBash {
				t.Errorf("default gated set does not gate Bash: %v", got)
			}
		})
	}
}

// TestGatedToolsForDoesNotAliasTheAgentSlice: the returned slice must be a
// copy, or a caller mutating it would rewrite the agent's stored policy.
func TestGatedToolsForDoesNotAliasTheAgentSlice(t *testing.T) {
	ag := &agentslib.Agent{ClaudeGatedTools: []string{"Bash", "Write"}}
	got := gatedToolsFor(ag)
	got[0] = "Read"
	if ag.ClaudeGatedTools[0] != "Bash" {
		t.Fatalf("agent's gated tools mutated through the returned slice: %v", ag.ClaudeGatedTools)
	}

	def := gatedToolsFor(&agentslib.Agent{})
	def[0] = "Read"
	if claudecode.DefaultGatedTools[0] == "Read" {
		t.Fatal("package-level DefaultGatedTools mutated through the returned slice")
	}
}

func TestClaudeSessionExistsUnder(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-Users-dev-project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "11111111-2222-3333-4444-555555555555"
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if !claudeSessionExistsUnder(root, id) {
		t.Error("an existing transcript nested under a project dir was not found; the first turn would try to claim an id the CLI already owns")
	}
	if claudeSessionExistsUnder(root, "99999999-9999-4999-8999-999999999999") {
		t.Error("reported a session that does not exist; the turn would --resume a session Claude Code has never created")
	}
	if claudeSessionExistsUnder(root, "") {
		t.Error("an empty session id must never count as existing")
	}
	if claudeSessionExistsUnder("", id) {
		t.Error("an empty root must never count as existing")
	}
	if claudeSessionExistsUnder(filepath.Join(root, "missing"), id) {
		t.Error("a missing root must not report the session as existing")
	}
}

func TestClaudeSessionIDsOf(t *testing.T) {
	if got := claudeSessionIDsOf(nil); got != nil {
		t.Errorf("nil config = %v, want nil", got)
	}
	cfg := &agentslib.AgentsConfig{Agents: []agentslib.AgentDef{
		{Name: "Bound", ClaudeSessionID: "sess-1"},
		{Name: "Native"},
		{Name: "AlsoBound", ClaudeSessionID: "sess-2"},
	}}
	got := claudeSessionIDsOf(cfg)
	if len(got) != 2 || got[0] != "sess-1" || got[1] != "sess-2" {
		t.Fatalf("claudeSessionIDsOf = %v, want [sess-1 sess-2]", got)
	}
}
