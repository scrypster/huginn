package mcp

// Verifies that watchServer's reconnect path re-tags the newly registered
// tool set with the server's configured name, mirroring the pattern used by
// TestServerManager_WatchServer_Reconnect (reconnect_test.go) but asserting
// on registry.ProviderFor instead of just presence.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

func TestManager_WatchServer_ReconnectRetagsProvider(t *testing.T) {
	var factoryCalls atomic.Int32

	factory := func(_ context.Context, cfg MCPServerConfig) (*MCPClient, []MCPTool, error) {
		n := factoryCalls.Add(1)
		if n == 1 {
			// Initial connect — immediately-failing transport triggers reconnect.
			client := NewMCPClient(&mockTransport{})
			return client, []MCPTool{{Name: "browser_navigate", Description: "v1"}}, nil
		}
		// Reconnect — blocking transport keeps the new tool stably registered
		// long enough for the test to observe it.
		client := NewMCPClient(&blockingTransport{})
		return client, []MCPTool{{Name: "browser_click", Description: "v2"}}, nil
	}

	cfgs := []MCPServerConfig{{Name: "playwright", Command: "cat"}}
	mgr := NewServerManager(cfgs, WithClientFactory(factory),
		WithRestartBackoff(5*time.Millisecond, 50*time.Millisecond),
		WithHealthInterval(50*time.Millisecond))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	mgr.StartAll(ctx, reg)

	deadline := time.After(2 * time.Second)
	for {
		if factoryCalls.Load() >= 2 {
			if _, ok := reg.Get("browser_click"); ok {
				break
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for reconnect; factory calls = %d", factoryCalls.Load())
		case <-time.After(20 * time.Millisecond):
		}
	}

	if got := reg.ProviderFor("browser_click"); got != "playwright" {
		t.Errorf("ProviderFor(browser_click) after reconnect = %q, want %q", got, "playwright")
	}
	if _, ok := reg.Get("browser_navigate"); ok {
		t.Error("expected stale tool browser_navigate to be unregistered after reconnect")
	}

	mgr.StopAll(ctx)
}
