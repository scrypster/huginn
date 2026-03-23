package mcp

import (
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

func TestRegisterServerTools_ShrinkUnregistersStale(t *testing.T) {
	mgr := NewServerManager(nil)
	reg := tools.NewRegistry()

	// Simulate first connection with 3 tools.
	firstTools := []MCPTool{
		{Name: "tool_a", Description: "A"},
		{Name: "tool_b", Description: "B"},
		{Name: "tool_c", Description: "C"},
	}
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	mgr.registerServerTools("test-server", client, firstTools, reg)

	// Verify all 3 tools are registered.
	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected tool %q to be registered after first connect", name)
		}
	}

	// Simulate reconnection with only 1 tool (shrunk set).
	reconnectTools := []MCPTool{
		{Name: "tool_a", Description: "A"},
	}
	mgr.registerServerTools("test-server", client, reconnectTools, reg)

	// tool_a should still be registered.
	if _, ok := reg.Get("tool_a"); !ok {
		t.Error("expected tool_a to remain registered")
	}

	// tool_b and tool_c should have been unregistered.
	if _, ok := reg.Get("tool_b"); ok {
		t.Error("expected tool_b to be unregistered after shrink")
	}
	if _, ok := reg.Get("tool_c"); ok {
		t.Error("expected tool_c to be unregistered after shrink")
	}
}

func TestRegisterServerTools_DifferentServersIndependent(t *testing.T) {
	mgr := NewServerManager(nil)
	reg := tools.NewRegistry()

	tr := &mockTransport{}
	client := NewMCPClient(tr)

	// Server A registers tool_x.
	mgr.registerServerTools("server-a", client, []MCPTool{
		{Name: "tool_x", Description: "X from A"},
	}, reg)

	// Server B registers tool_y.
	mgr.registerServerTools("server-b", client, []MCPTool{
		{Name: "tool_y", Description: "Y from B"},
	}, reg)

	// Reconnect server A with empty tools — should not affect server B's tool_y.
	mgr.registerServerTools("server-a", client, nil, reg)

	if _, ok := reg.Get("tool_x"); ok {
		t.Error("expected tool_x to be unregistered after server-a reconnect with empty set")
	}
	if _, ok := reg.Get("tool_y"); !ok {
		t.Error("expected tool_y to remain registered (owned by server-b)")
	}
}
