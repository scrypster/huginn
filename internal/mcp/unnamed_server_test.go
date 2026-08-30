package mcp

import (
	"context"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// TestStartAll_SkipsUnnamedServer is V5's defense-in-depth: even though
// config.Validate already rejects an empty Name at PUT /api/v1/config,
// StartAll must also skip (with a WARN) any config-source entry that slips
// through with an empty name — TagTools(names, "") would otherwise tag its
// tools with the empty provider string, which the permission gate can never
// key allowedProviders/baseWatchedProviders on (an ungated server).
func TestStartAll_SkipsUnnamedServer(t *testing.T) {
	var mu sync.Mutex
	var calledFor []string
	factory := func(_ context.Context, cfg MCPServerConfig) (*MCPClient, []MCPTool, error) {
		mu.Lock()
		calledFor = append(calledFor, cfg.Name)
		mu.Unlock()
		// Fail every connect attempt — StartAll should still have attempted
		// (or not attempted) the connect before we assert; the important
		// signal here is whether the factory was invoked at all.
		return nil, nil, context.DeadlineExceeded
	}

	m := NewServerManager([]MCPServerConfig{
		{Name: "", Command: "some-binary"},
		{Name: "playwright", Command: "some-binary"},
	}, WithClientFactory(factory))

	reg := tools.NewRegistry()
	m.StartAll(context.Background(), reg)

	mu.Lock()
	defer mu.Unlock()
	for _, name := range calledFor {
		if name == "" {
			t.Error("factory was invoked for the unnamed server — StartAll must skip it before connecting")
		}
	}
	if len(calledFor) != 1 || calledFor[0] != "playwright" {
		t.Errorf("expected exactly one connect attempt for %q, got %v", "playwright", calledFor)
	}
}
