package mcp

import (
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// TestMCPManager_Reconnect_CleansOldTools verifies that when a server reconnects
// with a different set of tools, the old tools are removed from the registry
// and only the new tools remain.
func TestMCPManager_Reconnect_CleansOldTools(t *testing.T) {
	reg := tools.NewRegistry()

	// First connection: server registers 3 tools.
	firstTools := []MCPTool{
		{Name: "tool_alpha", Description: "alpha"},
		{Name: "tool_beta", Description: "beta"},
		{Name: "tool_gamma", Description: "gamma"},
	}

	// Second connection: server returns 2 different tools.
	secondTools := []MCPTool{
		{Name: "tool_delta", Description: "delta"},
		{Name: "tool_epsilon", Description: "epsilon"},
	}

	m := NewServerManager(nil)

	// Simulate first connection registration.
	// Use a dummy client — the manager only needs it for adapter creation.
	dummyClient := &MCPClient{}

	m.mu.Lock()
	m.registerServerTools("my-server", dummyClient, firstTools, reg)
	m.mu.Unlock()

	// Verify the 3 first tools are registered.
	for _, mt := range firstTools {
		if _, ok := reg.Get(mt.Name); !ok {
			t.Errorf("expected tool %q to be registered after first connect", mt.Name)
		}
	}

	// Simulate reconnect: server returns 2 different tools.
	m.mu.Lock()
	m.registerServerTools("my-server", dummyClient, secondTools, reg)
	m.mu.Unlock()

	// Old tools should be gone.
	for _, mt := range firstTools {
		if _, ok := reg.Get(mt.Name); ok {
			t.Errorf("stale tool %q should have been removed after reconnect", mt.Name)
		}
	}

	// New tools should be present.
	for _, mt := range secondTools {
		if _, ok := reg.Get(mt.Name); !ok {
			t.Errorf("expected tool %q to be registered after reconnect", mt.Name)
		}
	}
}
