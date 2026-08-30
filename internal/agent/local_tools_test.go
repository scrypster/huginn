package agent

import (
	"context"
	"strings"
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

func (t *localTestTool) Name() string                      { return t.name }
func (t *localTestTool) Description() string               { return "" }
func (t *localTestTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *localTestTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *localTestTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}

func TestApplyToolbelt_EmptyConfigNoToolsWhenNoneRegistered(t *testing.T) {
	reg := buildLocalTestRegistry()
	ag := &agents.Agent{Name: "test"} // empty LocalTools + empty Toolbelt — registry has no delegation tools so step 4 is a no-op

	schemas, _ := applyToolbelt(ag, reg, nil, nil)
	if len(schemas) != 0 {
		t.Errorf("expected 0 schemas with default-deny, got %d", len(schemas))
	}
}

func TestApplyToolbelt_LocalToolsWildcardReturnsAllBuiltins(t *testing.T) {
	reg := buildLocalTestRegistry()
	ag := &agents.Agent{Name: "test", LocalTools: []string{"*"}}

	schemas, _ := applyToolbelt(ag, reg, nil, nil)
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

	schemas, _ := applyToolbelt(ag, reg, nil, nil)
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

	schemas, _ := applyToolbelt(ag, reg, nil, nil)
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
	schemas, _ := applyToolbelt(ag, reg, nil, nil)

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
	schemas, _ := applyToolbelt(ag, reg, nil, nil)

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

	schemas, _ := applyToolbelt(ag, reg, nil, nil)
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

	schemas, _ := applyToolbelt(ag, reg, nil, nil)

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

	schemas, _ := applyToolbelt(ag, reg, nil, nil)

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

	schemas, _ := applyToolbelt(ag, reg, nil, nil)

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

	schemas, _ := applyToolbelt(ag, reg, nil, nil)

	seen := map[string]int{}
	for _, s := range schemas {
		seen[s.Function.Name]++
	}
	for _, name := range []string{"delegate_to_agent", "list_team_status", "recall_thread_result"} {
		if seen[name] > 1 {
			t.Errorf("delegation tool %q appears %d times in schemas, want exactly 1", name, seen[name])
		}
		if seen[name] == 0 {
			t.Errorf("delegation tool %q missing from wildcard schemas (want exactly 1)", name)
		}
	}
}

func TestApplyToolbelt_WildcardIncludesGitHubCLI(t *testing.T) {
	reg := tools.NewRegistry()
	for _, name := range []string{"gh_issue_list", "gh_pr_list", "bash"} {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools([]string{"gh_issue_list", "gh_pr_list"}, "github_cli")
	reg.TagTools([]string{"bash"}, "builtin")

	ag := &agents.Agent{
		Name:     "Astra",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "*", ConnectionID: "*"}},
	}
	schemas, _ := applyToolbelt(ag, reg, nil, nil)
	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	if !names["gh_issue_list"] || !names["gh_pr_list"] {
		t.Fatalf("wildcard toolbelt missing github CLI schemas: %v", names)
	}
	if names["bash"] {
		t.Error("wildcard toolbelt must not grant bash without local_tools")
	}
}

func TestApplyToolbelt_EmptyBeltExcludesGitHubCLI(t *testing.T) {
	reg := tools.NewRegistry()
	for _, name := range []string{"gh_issue_create", "gh_issue_list", "gh_pr_list", "github"} {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools([]string{"gh_issue_create", "gh_issue_list", "gh_pr_list"}, "github_cli")
	reg.TagTools([]string{"github"}, "github")
	for _, name := range []string{"delegate_to_agent", "wait_for_threads", "list_team_status", "recall_thread_result"} {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools([]string{"delegate_to_agent", "wait_for_threads", "list_team_status", "recall_thread_result"}, "builtin")

	ag := &agents.Agent{Name: "Reggie", LocalTools: nil, Toolbelt: nil}
	schemas, _ := applyToolbelt(ag, reg, nil, nil)
	var names []string
	for _, s := range schemas {
		n := s.Function.Name
		names = append(names, n)
		low := strings.ToLower(n)
		if low == "gh_issue_create" || strings.HasPrefix(low, "gh_") || low == "github" || strings.Contains(low, "github") {
			t.Fatalf("empty LocalTools+Toolbelt leaked github schema %q; schemas=%v", n, names)
		}
	}
	// Delegation may still inject.
	if !containsName(names, "delegate_to_agent") {
		t.Fatalf("expected delegate_to_agent injection, got %v", names)
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestApplyToolbelt_EmptyBelt_ProductionTaggingNoGitHub(t *testing.T) {
	// Same register+tag order as oneshot.DefaultToolRegistry / init_tools.
	reg := tools.NewRegistry()
	for _, name := range tools.GitHubCLIToolNames() {
		reg.Register(&localTestTool{name: name})
	}
	reg.TagTools(tools.GitHubCLIToolNames(), "github_cli")
	reg.TagTools(tools.BuiltinToolNames(), "builtin")
	if p := reg.ProviderFor("gh_issue_create"); p == "builtin" {
		t.Fatal("gh_issue_create must not be tagged builtin")
	}
	if p := reg.ProviderFor("gh_issue_create"); p != "github_cli" {
		t.Fatalf("gh_issue_create provider = %q, want github_cli", p)
	}

	ag := &agents.Agent{Name: "Reggie", LocalTools: nil, Toolbelt: nil}
	schemas, _ := applyToolbelt(ag, reg, nil, nil)
	for _, s := range schemas {
		n := strings.ToLower(s.Function.Name)
		if n == "gh_issue_create" || strings.HasPrefix(n, "gh_") || n == "github" || strings.Contains(n, "github") {
			t.Fatalf("production tagging leaked %q into empty-belt schemas", s.Function.Name)
		}
	}
}

func TestApplyToolbelt_CreateAgentGrantWall(t *testing.T) {
	reg := buildLocalTestRegistry()
	reg.Register(&localTestTool{name: "create_agent"})
	// Intentionally not tagged builtin — God Mode must not receive hire.

	star, _ := applyToolbelt(&agents.Agent{Name: "Astra", LocalTools: []string{"*"}}, reg, nil, nil)
	if hasSchema(star, "create_agent") {
		t.Fatal("local_tools ['*'] must NOT include create_agent")
	}

	none, _ := applyToolbelt(&agents.Agent{Name: "Sam"}, reg, nil, nil)
	if hasSchema(none, "create_agent") {
		t.Fatal("specialist without grant: schema must be absent")
	}

	named, _ := applyToolbelt(&agents.Agent{Name: "Sam", LocalTools: []string{"read_file"}}, reg, nil, nil)
	if hasSchema(named, "create_agent") {
		t.Fatal("named specialist list without grant: schema must be absent")
	}

	winston, _ := applyToolbelt(&agents.Agent{Name: "Winston", LocalTools: []string{"create_agent"}}, reg, nil, nil)
	if !hasSchema(winston, "create_agent") {
		t.Fatal("Winston with named grant must see create_agent")
	}
}

func TestApplyToolbelt_ToolbeltWildcardDoesNotGrantCreateAgent(t *testing.T) {
	reg := buildLocalTestRegistry()
	reg.Register(&localTestTool{name: "create_agent"})
	steve, _ := applyToolbelt(&agents.Agent{
		Name:       "Steve",
		LocalTools: []string{"bash"},
		Toolbelt:   []agents.ToolbeltEntry{{Provider: "*", ConnectionID: "*"}},
	}, reg, nil, nil)
	if hasSchema(steve, "create_agent") {
		t.Fatal("toolbelt ['*'] must NOT include create_agent")
	}
	god, _ := applyToolbelt(&agents.Agent{Name: "hiregod", LocalTools: []string{"*"}}, reg, nil, nil)
	if hasSchema(god, "create_agent") {
		t.Fatal("local_tools ['*'] must NOT include create_agent")
	}
	winston, _ := applyToolbelt(&agents.Agent{
		Name:       "Winston",
		LocalTools: []string{"create_agent"},
		Toolbelt:   []agents.ToolbeltEntry{{Provider: "*", ConnectionID: "*"}},
	}, reg, nil, nil)
	if !hasSchema(winston, "create_agent") {
		t.Fatal("named create_agent grant must survive toolbelt wildcard")
	}
}

func TestBuiltinToolNamesOmitsCreateAgent(t *testing.T) {
	for _, n := range tools.BuiltinToolNames() {
		if n == "create_agent" {
			t.Fatal("create_agent must not be in BuiltinToolNames")
		}
	}
}

func hasSchema(schemas []backend.Tool, name string) bool {
	for _, s := range schemas {
		if s.Function.Name == name {
			return true
		}
	}
	return false
}

func TestApplyToolbelt_TierLowOmitsDelegate(t *testing.T) {
	reg := buildDelegationTestRegistry()
	ag := &agents.Agent{Name: "Tiny", ModelID: "qwen2.5-coder:7b", LocalTools: []string{"read_file"}}
	schemas, _ := applyToolbelt(ag, reg, nil, nil)
	if hasSchema(schemas, "delegate_to_agent") {
		t.Fatal("TierLow must not receive delegate_to_agent")
	}
	if hasSchema(schemas, "wait_for_threads") || hasSchema(schemas, "list_team_status") {
		t.Fatal("TierLow must not receive delegation tools")
	}
	if !hasSchema(schemas, "read_file") {
		t.Fatal("named local tool must remain")
	}
}

func TestApplyToolbelt_TierLowStarOmitsDelegate(t *testing.T) {
	reg := buildDelegationTestRegistry()
	ag := &agents.Agent{Name: "Tiny", ModelID: "qwen2.5-coder:7b", LocalTools: []string{"*"}}
	schemas, _ := applyToolbelt(ag, reg, nil, nil)
	if hasSchema(schemas, "delegate_to_agent") {
		t.Fatal("TierLow LocalTools [*] must not keep delegate_to_agent from step 1")
	}
	if !hasSchema(schemas, "read_file") {
		t.Fatal("wildcard builtins other than delegation must remain")
	}
}

func TestApplyToolbelt_HighTierWinstonKeepsCreateAgentAndDelegate(t *testing.T) {
	reg := buildDelegationTestRegistry()
	reg.Register(&localTestTool{name: "create_agent"})
	ag := &agents.Agent{
		Name:       "Winston",
		ModelID:    "claude-sonnet-4",
		LocalTools: []string{"create_agent"},
	}
	schemas, _ := applyToolbelt(ag, reg, nil, nil)
	if !hasSchema(schemas, "create_agent") {
		t.Fatal("Winston named create_agent grant must remain")
	}
	if !hasSchema(schemas, "delegate_to_agent") {
		t.Fatal("high-tier Winston must still receive delegate_to_agent")
	}
}
