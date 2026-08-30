package mcp_test

// Tests that ServerManager.StartAll (and the watchServer reconnect path)
// tag registered MCP tools with the server's configured name as a provider,
// so applyToolbelt's provider-wholesale grants (toolbelt entry with
// provider: "<mcp server name>") can reach them — symmetric with built-in
// connection providers like "github_cli"/"bitbucket". Before this change,
// MCP tools registered under raw names with no provider tag, so only an
// explicit LocalTools entry (never a toolbelt grant) could reach them.

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
	"github.com/scrypster/huginn/internal/tools"
)

func TestStartAll_TagsToolsWithServerName(t *testing.T) {
	factory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		tr := &MockTransport{}
		client := mcp.NewMCPClient(tr)
		toolList := []mcp.MCPTool{
			{Name: "browser_navigate", Description: "navigate", InputSchema: mcp.MCPInputSchema{Type: "object"}},
			{Name: "browser_take_screenshot", Description: "screenshot", InputSchema: mcp.MCPInputSchema{Type: "object"}},
		}
		return client, toolList, nil
	}

	cfgs := []mcp.MCPServerConfig{{Name: "playwright", Command: "playwright-mcp"}}
	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(factory))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.StartAll(ctx, reg)

	for _, name := range []string{"browser_navigate", "browser_take_screenshot"} {
		if got := reg.ProviderFor(name); got != "playwright" {
			t.Errorf("ProviderFor(%q) = %q, want %q", name, got, "playwright")
		}
	}

	// A toolbelt-style provider grant should now surface these tools, not
	// just an explicit LocalTools name list.
	schemas := reg.AllSchemasForProviders([]string{"playwright"})
	if len(schemas) != 2 {
		t.Fatalf("AllSchemasForProviders([\"playwright\"]) returned %d schemas, want 2", len(schemas))
	}
}
