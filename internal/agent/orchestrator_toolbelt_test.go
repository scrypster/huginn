package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// testTool is a minimal tools.Tool implementation for applyToolbelt tests.
type testTool struct {
	schema   backend.Tool
	provider string
}

func (t *testTool) Name() string        { return t.schema.Function.Name }
func (t *testTool) Description() string { return "" }
func (t *testTool) Permission() tools.PermissionLevel {
	return tools.PermWrite
}
func (t *testTool) Schema() backend.Tool { return t.schema }
func (t *testTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}

// newTestToolsReg builds a tool registry with "github" and "slack" tagged tools.
func newTestToolsReg() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(&testTool{
		schema: backend.Tool{Type: "function", Function: backend.ToolFunction{Name: "github_list_prs"}},
	})
	reg.Register(&testTool{
		schema: backend.Tool{Type: "function", Function: backend.ToolFunction{Name: "slack_post"}},
	})
	reg.TagTools([]string{"github_list_prs"}, "github")
	reg.TagTools([]string{"slack_post"}, "slack")
	return reg
}

// agentWithToolbelt builds an Agent whose Toolbelt includes the given providers.
// approval=true sets ApprovalGate on all entries.
func agentWithToolbelt(providers []string, approval bool) *agents.Agent {
	tb := make([]agents.ToolbeltEntry, len(providers))
	for i, p := range providers {
		tb[i] = agents.ToolbeltEntry{
			ConnectionID: "conn-" + p,
			Provider:     p,
			ApprovalGate: approval,
		}
	}
	return &agents.Agent{Name: "test-agent", IsDefault: true, Toolbelt: tb}
}

func TestApplyToolbelt_EmptyGetNoSchemas(t *testing.T) {
	reg := newTestToolsReg() // has external tools but no builtins
	ag := &agents.Agent{Name: "test-agent"} // no toolbelt, no local tools

	schemas, _ := applyToolbelt(ag, reg, nil)
	if len(schemas) != 0 {
		t.Errorf("expected 0 schemas with default-deny, got %d", len(schemas))
	}
}

func TestApplyToolbelt_FilteredToProvider(t *testing.T) {
	reg := newTestToolsReg()
	ag := agentWithToolbelt([]string{"github"}, false)

	schemas, _ := applyToolbelt(ag, reg, nil)
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema for github provider, got %d", len(schemas))
	}
	if schemas[0].Function.Name != "github_list_prs" {
		t.Errorf("expected github_list_prs, got %q", schemas[0].Function.Name)
	}
}

func TestApplyToolbelt_MultipleProviders(t *testing.T) {
	reg := newTestToolsReg()
	ag := agentWithToolbelt([]string{"github", "slack"}, false)

	schemas, _ := applyToolbelt(ag, reg, nil)
	if len(schemas) != 2 {
		t.Errorf("expected 2 schemas for github+slack, got %d", len(schemas))
	}
}

func TestApplyToolbelt_UnknownProvider(t *testing.T) {
	reg := newTestToolsReg()
	ag := agentWithToolbelt([]string{"unknown-provider"}, false)

	schemas, _ := applyToolbelt(ag, reg, nil)
	if len(schemas) != 0 {
		t.Errorf("expected 0 schemas for unknown provider, got %d", len(schemas))
	}
}

// TestApplyToolbelt_SetsWatchedProviders verifies that when an agent has a
// toolbelt entry with ApprovalGate=true, that provider becomes "watched" in
// the gate. We test this by observing that Check() prompts (and denies since
// promptFunc returns Deny) for the watched provider.
func TestApplyToolbelt_SetsWatchedProviders(t *testing.T) {
	reg := newTestToolsReg()
	ag := agentWithToolbelt([]string{"github"}, true /* ApprovalGate=true */)

	prompted := false
	gate := permissions.NewGate(true /* skipAll */, func(_ permissions.PermissionRequest) permissions.Decision {
		prompted = true
		return permissions.Deny
	})

	_, agentGate := applyToolbelt(ag, reg, gate)

	// With skipAll=true and "github" watched, a PermWrite call for a github
	// tool should still be prompted (not skipped).
	agentGate.Check(permissions.PermissionRequest{
		ToolName: "github_list_prs",
		Level:    tools.PermWrite,
		Provider: "github",
	})

	if !prompted {
		t.Error("expected gate to prompt for watched provider github, but it did not")
	}
}

// TestApplyToolbelt_NoWatchedWhenGateFalse verifies that when ApprovalGate=false,
// the provider is NOT watched and skipAll mode skips the prompt entirely.
func TestApplyToolbelt_NoWatchedWhenGateFalse(t *testing.T) {
	reg := newTestToolsReg()
	ag := agentWithToolbelt([]string{"github"}, false /* ApprovalGate=false */)

	prompted := false
	gate := permissions.NewGate(true /* skipAll */, func(_ permissions.PermissionRequest) permissions.Decision {
		prompted = true
		return permissions.Deny
	})

	_, agentGate := applyToolbelt(ag, reg, gate)

	// With skipAll=true and "github" NOT watched, a PermWrite call should
	// be allowed without prompting.
	allowed := agentGate.Check(permissions.PermissionRequest{
		ToolName: "github_list_prs",
		Level:    tools.PermWrite,
		Provider: "github",
	})

	if prompted {
		t.Error("expected no prompt when ApprovalGate=false, but promptFunc was called")
	}
	if !allowed {
		t.Error("expected tool to be allowed in skipAll mode without ApprovalGate, but it was denied")
	}
}
