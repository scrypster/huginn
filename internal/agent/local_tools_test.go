package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// buildLocalTestRegistry creates a registry with named builtin tools + one external tool.
func buildLocalTestRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	// builtin tools
	for _, name := range []string{"read_file", "bash", "git_status"} {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools([]string{"read_file", "bash", "git_status"}, "builtin")
	// external tool
	reg.Register(&localTestTool{name: "slack_post"})
	reg.TagTools([]string{"slack_post"}, "slack")
	return reg
}

type localTestTool struct{ name string }

func (t *localTestTool) Name() string                  { return t.name }
func (t *localTestTool) Description() string           { return "" }
func (t *localTestTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *localTestTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *localTestTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}

func TestApplyToolbelt_DefaultDenyNoLocalNoExternal(t *testing.T) {
	reg := buildLocalTestRegistry()
	ag := &agents.Agent{Name: "test"} // empty LocalTools + empty Toolbelt

	schemas, _ := applyToolbelt(ag, reg, nil)
	if len(schemas) != 0 {
		t.Errorf("expected 0 schemas with default-deny, got %d", len(schemas))
	}
}

func TestApplyToolbelt_LocalToolsWildcardReturnsAllBuiltins(t *testing.T) {
	reg := buildLocalTestRegistry()
	ag := &agents.Agent{Name: "test", LocalTools: []string{"*"}}

	schemas, _ := applyToolbelt(ag, reg, nil)
	if len(schemas) != 3 {
		t.Errorf("expected 3 builtin schemas, got %d", len(schemas))
	}
	// Verify slack_post NOT included
	for _, s := range schemas {
		if s.Function.Name == "slack_post" {
			t.Error("slack_post should not be in local tools")
		}
	}
}

func TestApplyToolbelt_LocalToolsNamedListReturnsOnlyNamed(t *testing.T) {
	reg := buildLocalTestRegistry()
	ag := &agents.Agent{Name: "test", LocalTools: []string{"read_file", "git_status"}}

	schemas, _ := applyToolbelt(ag, reg, nil)
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	if !names["read_file"] || !names["git_status"] {
		t.Errorf("expected read_file and git_status, got %v", names)
	}
	if names["bash"] {
		t.Error("bash should not be included")
	}
}

func TestApplyToolbelt_ExternalToolbeltOnlyReturnsExternal(t *testing.T) {
	reg := buildLocalTestRegistry()
	ag := &agents.Agent{
		Name:     "test",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "slack"}},
	}

	schemas, _ := applyToolbelt(ag, reg, nil)
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema (slack_post), got %d", len(schemas))
	}
	if schemas[0].Function.Name != "slack_post" {
		t.Errorf("expected slack_post, got %q", schemas[0].Function.Name)
	}
}

// TestApplyToolbelt_WildcardIncludesDelegationToolsWhenTaggedBuiltin verifies
// that delegation tools (delegate_to_agent, list_team_status, recall_thread_result)
// are visible to agents with LocalTools: ["*"] when tagged as "builtin".
// This is critical for channel-based delegation to work.
func TestApplyToolbelt_WildcardIncludesDelegationToolsWhenTaggedBuiltin(t *testing.T) {
	reg := buildLocalTestRegistry()
	// Add delegation tools and tag them as builtin (matches main.go wiring)
	for _, name := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools([]string{"delegate_to_agent", "list_team_status", "recall_thread_result"}, "builtin")

	ag := &agents.Agent{Name: "Tom", LocalTools: []string{"*"}}
	schemas, _ := applyToolbelt(ag, reg, nil)

	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	for _, expected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("expected %q in schemas with LocalTools=[*], got %v", expected, names)
		}
	}
}

// TestApplyToolbelt_DelegationToolsInjectedEvenWhenUntagged verifies that
// step 5 injection works regardless of tagging. Untagged delegation tools are
// now injected because they are registered (the tagging only affects step 1).
// This validates that step 5 is not dependent on the "builtin" tag.
func TestApplyToolbelt_DelegationToolsInjectedEvenWhenUntagged(t *testing.T) {
	reg := buildLocalTestRegistry()
	// Add delegation tools WITHOUT tagging them
	for _, name := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		reg.Register(&localTestTool{name: name})
	}

	ag := &agents.Agent{Name: "Tom", LocalTools: []string{"read_file"}}
	schemas, _ := applyToolbelt(ag, reg, nil)

	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	// Even though they're untagged, step 5 injection should include them
	for _, expected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("untagged delegation tool %q should still be injected at step 5", expected)
		}
	}
	// Original named tool must still be present
	if !names["read_file"] {
		t.Errorf("original named tool read_file should be present, got names=%v", names)
	}
}

func TestApplyToolbelt_BothLocalAndExternal(t *testing.T) {
	reg := buildLocalTestRegistry()
	ag := &agents.Agent{
		Name:       "test",
		LocalTools: []string{"read_file"},
		Toolbelt:   []agents.ToolbeltEntry{{Provider: "slack"}},
	}

	schemas, _ := applyToolbelt(ag, reg, nil)
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	if !names["read_file"] || !names["slack_post"] {
		t.Errorf("expected read_file and slack_post, got %v", names)
	}
}

// buildDelegationTestRegistry creates a registry with builtin tools, external tools,
// AND delegation tools registered and tagged "builtin" — matching main.go wiring.
func buildDelegationTestRegistry() *tools.Registry {
	reg := buildLocalTestRegistry() // read_file, bash, git_status (builtin), slack_post (slack)
	for _, name := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools([]string{"delegate_to_agent", "list_team_status", "recall_thread_result"}, "builtin")
	return reg
}

// TestApplyToolbelt_NamedLocalToolsAlwaysIncludesDelegationTools is the primary
// regression test for Bug 2: agents with a named LocalTools list must still
// receive delegation tools so the LLM can call delegate_to_agent.
func TestApplyToolbelt_NamedLocalToolsAlwaysIncludesDelegationTools(t *testing.T) {
	reg := buildDelegationTestRegistry()
	ag := &agents.Agent{Name: "Max", LocalTools: []string{"read_file", "bash"}}

	schemas, _ := applyToolbelt(ag, reg, nil)

	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	for _, expected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("expected delegation tool %q in schemas with named LocalTools, got names=%v", expected, names)
		}
	}
	// Original named tools must still be present
	if !names["read_file"] || !names["bash"] {
		t.Errorf("expected original local tools read_file and bash, got names=%v", names)
	}
}

// TestApplyToolbelt_EmptyLocalToolsAlwaysIncludesDelegationTools verifies that
// even agents with NO local tools configured receive delegation tools (Bug 2).
func TestApplyToolbelt_EmptyLocalToolsAlwaysIncludesDelegationTools(t *testing.T) {
	reg := buildDelegationTestRegistry()
	ag := &agents.Agent{Name: "Max"} // empty LocalTools

	schemas, _ := applyToolbelt(ag, reg, nil)

	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	for _, expected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if !names[expected] {
			t.Errorf("expected delegation tool %q even with empty LocalTools, got names=%v", expected, names)
		}
	}
}

// TestApplyToolbelt_DelegationToolsNotInjectedWhenNotRegistered ensures the
// step 5 injection is a safe no-op when delegation tools are not in the registry
// (e.g., TUI mode or test environments that don't register them).
func TestApplyToolbelt_DelegationToolsNotInjectedWhenNotRegistered(t *testing.T) {
	reg := buildLocalTestRegistry() // no delegation tools registered
	ag := &agents.Agent{Name: "Max", LocalTools: []string{"read_file"}}

	schemas, _ := applyToolbelt(ag, reg, nil)

	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	for _, unexpected := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if names[unexpected] {
			t.Errorf("delegation tool %q should NOT be injected when not registered, got names=%v", unexpected, names)
		}
	}
}

// TestApplyToolbelt_WildcardDeduplicatesDelegationTools ensures that when
// LocalTools=["*"] (which already includes delegation via AllBuiltinSchemas),
// step 5 does not produce duplicate entries.
func TestApplyToolbelt_WildcardDeduplicatesDelegationTools(t *testing.T) {
	reg := buildDelegationTestRegistry()
	ag := &agents.Agent{Name: "Max", LocalTools: []string{"*"}}

	schemas, _ := applyToolbelt(ag, reg, nil)

	seen := map[string]int{}
	for _, s := range schemas {
		seen[s.Function.Name]++
	}
	for _, name := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if seen[name] > 1 {
			t.Errorf("delegation tool %q appears %d times in schemas, want exactly 1", name, seen[name])
		}
	}
}
