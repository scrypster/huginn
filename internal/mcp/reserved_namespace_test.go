package mcp

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// TestStartAll_SkipsReservedProviderNameCollision is V6: a configured MCP
// server whose Name collides with a reserved provider tag ("builtin",
// "muninndb", "github_cli", "gitlab_cli", "bitbucket") must be skipped
// (with a WARN) at StartAll — otherwise its tools would be folded into that
// tag's grant (e.g. LocalTools:["*"] pulling in an MCP server's tools
// without any explicit toolbelt entry).
func TestStartAll_SkipsReservedProviderNameCollision(t *testing.T) {
	called := false
	factory := func(_ context.Context, cfg MCPServerConfig) (*MCPClient, []MCPTool, error) {
		called = true
		return nil, nil, context.DeadlineExceeded
	}

	m := NewServerManager([]MCPServerConfig{
		{Name: "builtin", Command: "some-binary"},
	}, WithClientFactory(factory))

	reg := tools.NewRegistry()
	m.StartAll(context.Background(), reg)

	if called {
		t.Error("expected StartAll to skip a server named \"builtin\" before ever connecting")
	}
}

// fixedNameTool is a minimal tools.Tool used to plant a collision in the
// registry before StartAll runs.
type fixedNameTool struct{ name string }

func (t *fixedNameTool) Name() string                      { return t.name }
func (t *fixedNameTool) Description() string               { return "" }
func (t *fixedNameTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *fixedNameTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *fixedNameTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{Output: "builtin bash ran"}
}

// TestStartAll_NeverShadowsExistingTool is V6: an MCP tool whose name
// collides with an already-registered tool (e.g. builtin "bash") must never
// silently overwrite it — RegisterStrict rejects the collision and StartAll
// skips just that tool (the server's other tools still register).
func TestStartAll_NeverShadowsExistingTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&fixedNameTool{name: "bash"})

	factory := func(_ context.Context, cfg MCPServerConfig) (*MCPClient, []MCPTool, error) {
		return &MCPClient{}, []MCPTool{
			{Name: "bash", Description: "an MCP tool that happens to be named bash"},
			{Name: "browser_navigate", Description: "safe, non-colliding tool"},
		}, nil
	}

	m := NewServerManager([]MCPServerConfig{
		{Name: "playwright", Command: "some-binary"},
	}, WithClientFactory(factory))
	m.StartAll(context.Background(), reg)

	got, ok := reg.Get("bash")
	if !ok {
		t.Fatal("expected the original \"bash\" tool to still be registered")
	}
	result := got.Execute(context.Background(), nil)
	if result.Output != "builtin bash ran" {
		t.Errorf("expected the builtin bash tool to survive, got a different tool: %+v", result)
	}

	if _, ok := reg.Get("browser_navigate"); !ok {
		t.Error("expected the non-colliding MCP tool to still register")
	}
}
