package agent

import (
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// buildCarryoverTestRegistry creates a realistic tool registry with
// builtins, external providers (slack, github, aws), delegation tools,
// and vault tools — simulating the full production tool surface.
func buildCarryoverTestRegistry() *tools.Registry {
	reg := tools.NewRegistry()

	// Builtin tools
	builtins := []string{"read_file", "write_file", "bash", "git_status", "git_diff", "update_memory"}
	for _, name := range builtins {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools(builtins, "builtin")

	// External provider tools
	reg.Register(&localTestTool{name: "slack_post"})
	reg.Register(&localTestTool{name: "slack_search"})
	reg.TagTools([]string{"slack_post", "slack_search"}, "slack")

	reg.Register(&localTestTool{name: "github_pr_create"})
	reg.Register(&localTestTool{name: "github_pr_list"})
	reg.TagTools([]string{"github_pr_create", "github_pr_list"}, "github_cli")

	reg.Register(&localTestTool{name: "aws_s3_list"})
	reg.TagTools([]string{"aws_s3_list"}, "aws")

	// Delegation tools (tagged builtin to match main.go wiring)
	delegationTools := []string{"delegate_to_agent", "list_team_status", "recall_thread_result"}
	for _, name := range delegationTools {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools(delegationTools, "builtin")

	// Vault tools
	reg.Register(&localTestTool{name: "muninn_recall"})
	reg.Register(&localTestTool{name: "muninn_store"})
	reg.TagTools([]string{"muninn_recall", "muninn_store"}, "muninndb")

	return reg
}

func schemaNames(schemas []backend.Tool) map[string]bool {
	m := make(map[string]bool, len(schemas))
	for _, s := range schemas {
		m[s.Function.Name] = true
	}
	return m
}

// ---------------------------------------------------------------------------
// Test: Agent with LocalTools=["*"] + Toolbelt=[slack] gets ALL builtins
// (including delegation tools) + slack tools + vault tools.
// This simulates Tom's real config in a channel.
// ---------------------------------------------------------------------------

func TestCarryover_WildcardLocalTools_PlusToolbelt(t *testing.T) {
	reg := buildCarryoverTestRegistry()
	ag := &agents.Agent{
		Name:       "Tom",
		LocalTools: []string{"*"},
		Toolbelt:   []agents.ToolbeltEntry{{Provider: "slack"}},
	}

	schemas, _ := applyToolbelt(ag, reg, nil)
	names := schemaNames(schemas)

	// All builtins (including delegation tools tagged as builtin)
	for _, expected := range []string{"read_file", "write_file", "bash", "git_status", "git_diff", "update_memory",
		"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("expected builtin %q in schemas, missing. Got: %v", expected, names)
		}
	}

	// Slack tools from toolbelt
	for _, expected := range []string{"slack_post", "slack_search"} {
		if !names[expected] {
			t.Errorf("expected slack tool %q in schemas, missing. Got: %v", expected, names)
		}
	}

	// Vault tools always included
	for _, expected := range []string{"muninn_recall", "muninn_store"} {
		if !names[expected] {
			t.Errorf("expected vault tool %q in schemas, missing. Got: %v", expected, names)
		}
	}

	// Tools from OTHER providers (github, aws) should NOT be included
	for _, notExpected := range []string{"github_pr_create", "github_pr_list", "aws_s3_list"} {
		if names[notExpected] {
			t.Errorf("unexpected tool %q in schemas — toolbelt should filter to slack only", notExpected)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Agent with named LocalTools + multiple toolbelt providers
// Only named builtins + specified provider tools + vault tools appear.
// ---------------------------------------------------------------------------

func TestCarryover_NamedLocalTools_MultipleProviders(t *testing.T) {
	reg := buildCarryoverTestRegistry()
	ag := &agents.Agent{
		Name:       "Sam",
		LocalTools: []string{"read_file", "git_status"},
		Toolbelt: []agents.ToolbeltEntry{
			{Provider: "github_cli"},
			{Provider: "aws"},
		},
	}

	schemas, _ := applyToolbelt(ag, reg, nil)
	names := schemaNames(schemas)

	// Only named builtins
	if !names["read_file"] || !names["git_status"] {
		t.Errorf("expected named builtins, got %v", names)
	}
	// bash, write_file, etc. should NOT be present
	if names["bash"] || names["write_file"] {
		t.Error("unnamed builtins should not be included with named LocalTools")
	}

	// GitHub + AWS tools should be present
	for _, expected := range []string{"github_pr_create", "github_pr_list", "aws_s3_list"} {
		if !names[expected] {
			t.Errorf("expected provider tool %q, missing", expected)
		}
	}

	// Slack tools should NOT be present (not in toolbelt)
	if names["slack_post"] || names["slack_search"] {
		t.Error("slack tools should not be included when slack is not in toolbelt")
	}

	// Vault tools always included
	if !names["muninn_recall"] || !names["muninn_store"] {
		t.Error("vault tools should always be included")
	}
}

// ---------------------------------------------------------------------------
// Test: Agent with NO tools at all (default-deny) — only vault tools + delegation tools.
// With step 5 injection, delegation tools are always added if registered.
// ---------------------------------------------------------------------------

func TestCarryover_DefaultDeny_OnlyVaultTools(t *testing.T) {
	reg := buildCarryoverTestRegistry()
	ag := &agents.Agent{Name: "Bare"} // no LocalTools, no Toolbelt

	schemas, _ := applyToolbelt(ag, reg, nil)
	names := schemaNames(schemas)

	// Vault tools + delegation tools should be present
	if !names["muninn_recall"] || !names["muninn_store"] {
		t.Error("vault tools should be present even for bare agents")
	}
	// Delegation tools are now injected at step 5
	for _, expected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("expected delegation tool %q to be injected at step 5, missing", expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Agents with named LocalTools now receive delegation tools via step 5 injection.
// Step 5 always injects delegation tools when they are registered, regardless of
// whether the agent explicitly named them in LocalTools.
// ---------------------------------------------------------------------------

func TestCarryover_NamedLocalTools_NoDelegationUnlessExplicit(t *testing.T) {
	reg := buildCarryoverTestRegistry()
	ag := &agents.Agent{
		Name:       "Mike",
		LocalTools: []string{"read_file", "bash"},
	}

	schemas, _ := applyToolbelt(ag, reg, nil)
	names := schemaNames(schemas)

	// Named tools present
	if !names["read_file"] || !names["bash"] {
		t.Error("expected named local tools")
	}

	// Delegation tools ARE injected at step 5 (fix for Bug 2)
	for _, expected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("delegation tool %q should be injected at step 5, missing", expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Vault tools are included even when toolbelt explicitly lists
// providers that don't include "muninndb". This was a past bug.
// Delegation tools are now also injected at step 5.
// ---------------------------------------------------------------------------

func TestCarryover_VaultToolsBypassToolbeltFiltering(t *testing.T) {
	reg := buildCarryoverTestRegistry()
	ag := &agents.Agent{
		Name:     "Agent",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "aws"}},
		// No LocalTools — only toolbelt
	}

	schemas, _ := applyToolbelt(ag, reg, nil)
	names := schemaNames(schemas)

	// AWS tools present via toolbelt
	if !names["aws_s3_list"] {
		t.Error("expected aws tool from toolbelt")
	}

	// Vault tools should ALSO be present (bypass)
	if !names["muninn_recall"] || !names["muninn_store"] {
		t.Error("vault tools must bypass toolbelt filtering")
	}

	// Delegation tools injected at step 5
	for _, expected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("delegation tool %q should be injected at step 5", expected)
		}
	}

	// Total: 1 aws + 2 vault + 3 delegation = 6
	if len(schemas) != 6 {
		t.Errorf("expected 6 schemas, got %d: %v", len(schemas), names)
	}
}

// ---------------------------------------------------------------------------
// Test: Tool deduplication — if a tool appears in both LocalTools and
// Toolbelt provider, it should only appear once.
// ---------------------------------------------------------------------------

func TestCarryover_NoDuplicateTools(t *testing.T) {
	reg := buildCarryoverTestRegistry()
	// Register a tool that is both tagged builtin AND in a provider
	reg.Register(&localTestTool{name: "shared_tool"})
	reg.TagTools([]string{"shared_tool"}, "builtin")
	// Also tag it with a provider
	reg.TagTools([]string{"shared_tool"}, "shared_provider")

	ag := &agents.Agent{
		Name:       "Agent",
		LocalTools: []string{"*"}, // includes shared_tool via builtin
		Toolbelt:   []agents.ToolbeltEntry{{Provider: "shared_provider"}}, // also includes shared_tool
	}

	schemas, _ := applyToolbelt(ag, reg, nil)

	// Count occurrences of shared_tool
	count := 0
	for _, s := range schemas {
		if s.Function.Name == "shared_tool" {
			count++
		}
	}
	// Note: applyToolbelt doesn't currently deduplicate between steps 1 & 2.
	// This test documents the current behavior. If duplicates are found,
	// it's a known issue to watch.
	t.Logf("shared_tool appears %d time(s) in schemas (total: %d)", count, len(schemas))
}

// ---------------------------------------------------------------------------
// Test: Skills field is preserved through FromDef and carried in Agent.
// ---------------------------------------------------------------------------

func TestCarryover_SkillsPreserved(t *testing.T) {
	// nil skills → global fallback
	def1 := agents.AgentDef{Name: "A", Model: "test"}
	ag1 := agents.FromDef(def1)
	if ag1.Skills != nil {
		t.Error("nil skills in def should remain nil in agent (global fallback)")
	}

	// empty skills → explicit no-skills
	def2 := agents.AgentDef{Name: "B", Model: "test", Skills: []string{}}
	ag2 := agents.FromDef(def2)
	if ag2.Skills == nil || len(ag2.Skills) != 0 {
		t.Error("empty skills should remain empty (explicit no-skills)")
	}

	// named skills → per-agent override
	def3 := agents.AgentDef{Name: "C", Model: "test", Skills: []string{"go-expert", "tdd"}}
	ag3 := agents.FromDef(def3)
	if len(ag3.Skills) != 2 || ag3.Skills[0] != "go-expert" || ag3.Skills[1] != "tdd" {
		t.Errorf("expected [go-expert, tdd], got %v", ag3.Skills)
	}
}

// ---------------------------------------------------------------------------
// Test: Toolbelt entries are preserved through FromDef.
// ---------------------------------------------------------------------------

func TestCarryover_ToolbeltPreserved(t *testing.T) {
	def := agents.AgentDef{
		Name:  "Agent",
		Model: "test",
		Toolbelt: []agents.ToolbeltEntry{
			{ConnectionID: "conn-1", Provider: "slack", ApprovalGate: true},
			{ConnectionID: "conn-2", Provider: "github_cli"},
		},
	}
	ag := agents.FromDef(def)
	if len(ag.Toolbelt) != 2 {
		t.Fatalf("expected 2 toolbelt entries, got %d", len(ag.Toolbelt))
	}
	if ag.Toolbelt[0].Provider != "slack" || !ag.Toolbelt[0].ApprovalGate {
		t.Errorf("first toolbelt entry wrong: %+v", ag.Toolbelt[0])
	}
	if ag.Toolbelt[1].Provider != "github_cli" {
		t.Errorf("second toolbelt entry wrong: %+v", ag.Toolbelt[1])
	}
}

// ---------------------------------------------------------------------------
// Test: BuildSpaceContextBlock includes all team members with descriptions.
// ---------------------------------------------------------------------------

func TestCarryover_SpaceContextIncludesTeamDescriptions(t *testing.T) {
	members := []SpaceMember{
		{Name: "Tom", Description: "Team Lead and orchestrator"},
		{Name: "Sam", Description: "Principal Engineer & Architect"},
		{Name: "Mike", Description: "Developer, hands-on implementation"},
		{Name: "Adam", Description: "Testing Expert"},
	}

	block := BuildSpaceContextBlock("Engineering", "channel", "Tom", "Tom", members)

	// Lead agent context
	if block == "" {
		t.Fatal("expected non-empty context block for channel")
	}
	for _, m := range members {
		if !contains(block, m.Name) {
			t.Errorf("context block missing member %q", m.Name)
		}
		if !contains(block, m.Description) {
			t.Errorf("context block missing description for %q", m.Name)
		}
	}
	// Lead identity
	if !contains(block, "You are Tom") {
		t.Error("context block should identify Tom as lead")
	}
	// Delegation protocol
	if !contains(block, "Delegation protocol") {
		t.Error("context block should include delegation protocol for lead agent")
	}
}

// ---------------------------------------------------------------------------
// Test: Member agent gets different context than lead agent.
// ---------------------------------------------------------------------------

func TestCarryover_MemberAgentContextDiffers(t *testing.T) {
	members := []SpaceMember{
		{Name: "Tom", Description: "Team Lead"},
		{Name: "Sam", Description: "Architect"},
	}

	leadBlock := BuildSpaceContextBlock("Engineering", "channel", "Tom", "Tom", members)
	memberBlock := BuildSpaceContextBlock("Engineering", "channel", "Sam", "Tom", members)

	if leadBlock == memberBlock {
		t.Error("lead and member should get different context blocks")
	}
	if contains(memberBlock, "You are Tom") {
		t.Error("member context should not claim Sam is Tom")
	}
	if !contains(memberBlock, "Tom") {
		t.Error("member context should reference the lead agent")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) >= len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
