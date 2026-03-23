package mcp

// manager_gaps_test.go — additional coverage for ServerManager internals.
// Uses the internal (non-_test) package so we can access unexported fields
// directly (managedServer, probeHealth, watchServer, registeredTools, etc.).

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestManager(cfgs []MCPServerConfig, opts ...ManagerOption) *ServerManager {
	return NewServerManager(cfgs, opts...)
}

func makePingOKResponse(id int) []byte {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{},
	})
	return resp
}

func makeMethodNotFoundResponse(id int) []byte {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    MethodNotFoundCode,
			"message": "method not found",
		},
	})
	return resp
}

func makeListToolsOKResponse(id int) []byte {
	result, _ := json.Marshal(MCPToolsListResult{Tools: []MCPTool{}})
	resp, _ := json.Marshal(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
	return resp
}

// ---------------------------------------------------------------------------
// TestManagerGaps_RegisterServerTools_RegistersAndTracks
// ---------------------------------------------------------------------------

// TestManagerGaps_RegisterServerTools_RegistersAndTracks verifies that
// registerServerTools adds tools to the registry and records them internally.
func TestManagerGaps_RegisterServerTools_RegistersAndTracks(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	mcpTools := []MCPTool{
		{Name: "tool_a", Description: "Tool A", InputSchema: MCPInputSchema{Type: "object"}},
		{Name: "tool_b", Description: "Tool B", InputSchema: MCPInputSchema{Type: "object"}},
	}

	reg := tools.NewRegistry()
	mgr := newTestManager(nil)

	mgr.mu.Lock()
	mgr.registerServerTools("myserver", client, mcpTools, reg)
	mgr.mu.Unlock()

	for _, name := range []string{"tool_a", "tool_b"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected %q to be registered in registry", name)
		}
	}
	mgr.mu.Lock()
	tracked := mgr.registeredTools["myserver"]
	mgr.mu.Unlock()
	if len(tracked) != 2 {
		t.Errorf("expected 2 tracked tools, got %d", len(tracked))
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_RegisterServerTools_ReplacesStaleTools
// ---------------------------------------------------------------------------

// TestManagerGaps_RegisterServerTools_ReplacesStaleTools verifies that when
// a server reconnects with a different tool set, stale tools are unregistered
// and new tools are registered.
func TestManagerGaps_RegisterServerTools_ReplacesStaleTools(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	reg := tools.NewRegistry()
	mgr := newTestManager(nil)

	// First registration.
	firstTools := []MCPTool{
		{Name: "old_tool", Description: "Old", InputSchema: MCPInputSchema{Type: "object"}},
	}
	mgr.mu.Lock()
	mgr.registerServerTools("srv", client, firstTools, reg)
	mgr.mu.Unlock()

	if _, ok := reg.Get("old_tool"); !ok {
		t.Fatal("expected old_tool to be registered after first registration")
	}

	// Second registration with a different tool.
	secondTools := []MCPTool{
		{Name: "new_tool", Description: "New", InputSchema: MCPInputSchema{Type: "object"}},
	}
	mgr.mu.Lock()
	mgr.registerServerTools("srv", client, secondTools, reg)
	mgr.mu.Unlock()

	if _, ok := reg.Get("old_tool"); ok {
		t.Error("old_tool should have been unregistered")
	}
	if _, ok := reg.Get("new_tool"); !ok {
		t.Error("new_tool should be registered after second registration")
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_RegisterServerTools_EmptyToolSet
// ---------------------------------------------------------------------------

// TestManagerGaps_RegisterServerTools_EmptyToolSet verifies that registering
// an empty tool set clears the previous set without panicking.
func TestManagerGaps_RegisterServerTools_EmptyToolSet(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	reg := tools.NewRegistry()
	mgr := newTestManager(nil)

	// Register a tool first.
	mgr.mu.Lock()
	mgr.registerServerTools("srv", client, []MCPTool{
		{Name: "some_tool", Description: "x", InputSchema: MCPInputSchema{Type: "object"}},
	}, reg)
	mgr.mu.Unlock()

	// Now register empty — old tool must be removed.
	mgr.mu.Lock()
	mgr.registerServerTools("srv", client, []MCPTool{}, reg)
	mgr.mu.Unlock()

	if _, ok := reg.Get("some_tool"); ok {
		t.Error("some_tool should have been removed after empty registration")
	}
	mgr.mu.Lock()
	tracked := mgr.registeredTools["srv"]
	mgr.mu.Unlock()
	if len(tracked) != 0 {
		t.Errorf("expected 0 tracked tools, got %d", len(tracked))
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_ProbeHealth_PingSuccess_DoesNotSetFallback
// ---------------------------------------------------------------------------

// TestManagerGaps_ProbeHealth_PingSuccess_DoesNotSetFallback checks that a
// successful ping does not flip probedWithListTools.
func TestManagerGaps_ProbeHealth_PingSuccess_DoesNotSetFallback(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	ms := &managedServer{
		cfg:    MCPServerConfig{Name: "ok-server"},
		client: client,
	}
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  json.RawMessage(`{}`),
	})

	mgr := newTestManager(nil)
	if err := mgr.probeHealth(context.Background(), ms); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	mgr.mu.Lock()
	fallback := ms.probedWithListTools
	mgr.mu.Unlock()
	if fallback {
		t.Error("probedWithListTools should remain false after a successful ping")
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_ProbeHealth_ListToolsFallback_SetsProbedFlag
// ---------------------------------------------------------------------------

// TestManagerGaps_ProbeHealth_ListToolsFallback_SetsProbedFlag verifies that
// when ping returns MethodNotFound, probedWithListTools is set to true.
func TestManagerGaps_ProbeHealth_ListToolsFallback_SetsProbedFlag(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	ms := &managedServer{
		cfg:    MCPServerConfig{Name: "noping-server"},
		client: client,
	}

	// Ping returns MethodNotFound.
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Error:   &RPCError{Code: MethodNotFoundCode, Message: "method not found"},
	})
	// ListTools succeeds.
	listResult, _ := json.Marshal(MCPToolsListResult{Tools: []MCPTool{{Name: "t1"}}})
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      2,
		Result:  listResult,
	})

	mgr := newTestManager(nil)
	if err := mgr.probeHealth(context.Background(), ms); err != nil {
		t.Fatalf("expected nil error after fallback, got %v", err)
	}

	mgr.mu.Lock()
	flag := ms.probedWithListTools
	mgr.mu.Unlock()
	if !flag {
		t.Error("probedWithListTools should be true after MethodNotFound fallback")
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_ProbeHealth_AlreadyFallback_SkipsPing
// ---------------------------------------------------------------------------

// TestManagerGaps_ProbeHealth_AlreadyFallback_SkipsPing verifies that when
// probedWithListTools is already true, probeHealth skips ping and calls
// ListTools directly.
func TestManagerGaps_ProbeHealth_AlreadyFallback_SkipsPing(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	ms := &managedServer{
		cfg:                 MCPServerConfig{Name: "fallback-server"},
		client:              client,
		probedWithListTools: true, // already in fallback mode
	}

	listResult, _ := json.Marshal(MCPToolsListResult{Tools: []MCPTool{}})
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  listResult,
	})

	mgr := newTestManager(nil)
	if err := mgr.probeHealth(context.Background(), ms); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Only one send (ListTools), no ping.
	tr.mu.Lock()
	n := len(tr.sends)
	tr.mu.Unlock()
	if n != 1 {
		t.Errorf("expected 1 send (ListTools only, no ping), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_ProbeHealth_OtherRPCError_Propagated
// ---------------------------------------------------------------------------

// TestManagerGaps_ProbeHealth_OtherRPCError_Propagated verifies that a
// non-MethodNotFound RPC error is returned as-is without touching fallback.
func TestManagerGaps_ProbeHealth_OtherRPCError_Propagated(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	ms := &managedServer{
		cfg:    MCPServerConfig{Name: "error-server"},
		client: client,
	}
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Error:   &RPCError{Code: -32000, Message: "server error"},
	})

	mgr := newTestManager(nil)
	err := mgr.probeHealth(context.Background(), ms)
	if err == nil {
		t.Fatal("expected error to be propagated")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T: %v", err, err)
	}
	if rpcErr.Code != -32000 {
		t.Errorf("expected code -32000, got %d", rpcErr.Code)
	}
	mgr.mu.Lock()
	fallback := ms.probedWithListTools
	mgr.mu.Unlock()
	if fallback {
		t.Error("probedWithListTools should remain false for non-MethodNotFound errors")
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_WatchServer_ReconnectsOnUnhealthy
// ---------------------------------------------------------------------------

// TestManagerGaps_WatchServer_ReconnectsOnUnhealthy verifies that watchServer
// calls the factory and re-registers tools when a health probe fails.
func TestManagerGaps_WatchServer_ReconnectsOnUnhealthy(t *testing.T) {
	reconnected := make(chan struct{}, 1)

	// First factory call: return a client whose ping always fails.
	unhealthyTr := &mockTransport{}
	unhealthyClient := NewMCPClient(unhealthyTr)

	// Second factory call (reconnect): return a healthy client and signal.
	healthyTr := &mockTransport{}
	healthyClient := NewMCPClient(healthyTr)

	// Queue an always-failing ping for the unhealthy client.
	// We queue many so the health ticker can drain them.
	for i := 0; i < 20; i++ {
		unhealthyTr.queueResponse(Response{
			JSONRPC: "2.0",
			ID:      i + 1,
			Error:   &RPCError{Code: -32000, Message: "dead"},
		})
	}

	callCount := 0
	var mu sync.Mutex
	cfg := MCPServerConfig{Name: "reconnect-srv"}
	factory := func(_ context.Context, _ MCPServerConfig) (*MCPClient, []MCPTool, error) {
		mu.Lock()
		callCount++
		n := callCount
		mu.Unlock()
		if n == 1 {
			return unhealthyClient, []MCPTool{}, nil
		}
		// Signal reconnect happened.
		select {
		case reconnected <- struct{}{}:
		default:
		}
		return healthyClient, []MCPTool{}, nil
	}

	mgr := newTestManager([]MCPServerConfig{cfg},
		WithClientFactory(factory),
		WithHealthInterval(20*time.Millisecond),
		WithRestartBackoff(10*time.Millisecond, 50*time.Millisecond),
	)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	mgr.StartAll(ctx, reg)

	// Wait for reconnect signal or timeout.
	select {
	case <-reconnected:
		// reconnect happened — test passes
	case <-time.After(2 * time.Second):
		t.Error("watchServer did not attempt to reconnect within 2s")
	}

	mgr.StopAll(context.Background())
}

// ---------------------------------------------------------------------------
// TestManagerGaps_WatchServer_BackoffIncreasesOnRepeatedFailure
// ---------------------------------------------------------------------------

// TestManagerGaps_WatchServer_BackoffIncreasesOnRepeatedFailure checks that
// the manager does not panic or deadlock when the factory keeps failing.
func TestManagerGaps_WatchServer_BackoffIncreasesOnRepeatedFailure(t *testing.T) {
	factoryCalls := 0
	var mu sync.Mutex

	// Always-failing unhealthy transport.
	badTr := &mockTransport{}
	badClient := NewMCPClient(badTr)
	for i := 0; i < 50; i++ {
		badTr.queueResponse(Response{
			JSONRPC: "2.0",
			ID:      i + 1,
			Error:   &RPCError{Code: -32000, Message: "dead"},
		})
	}

	cfg := MCPServerConfig{Name: "failing-srv"}
	factory := func(_ context.Context, _ MCPServerConfig) (*MCPClient, []MCPTool, error) {
		mu.Lock()
		n := factoryCalls
		factoryCalls++
		mu.Unlock()
		if n == 0 {
			return badClient, []MCPTool{}, nil
		}
		return nil, nil, errors.New("factory keeps failing")
	}

	mgr := newTestManager([]MCPServerConfig{cfg},
		WithClientFactory(factory),
		WithHealthInterval(10*time.Millisecond),
		WithRestartBackoff(5*time.Millisecond, 20*time.Millisecond),
	)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	mgr.StartAll(ctx, reg)
	<-ctx.Done()

	// At least one reconnect attempt should have been made.
	mu.Lock()
	calls := factoryCalls
	mu.Unlock()
	if calls < 2 {
		t.Errorf("expected at least 2 factory calls (1 initial + at least 1 retry), got %d", calls)
	}

	mgr.StopAll(context.Background())
}

// ---------------------------------------------------------------------------
// TestManagerGaps_WatchServer_ContextCancelStops
// ---------------------------------------------------------------------------

// TestManagerGaps_WatchServer_ContextCancelStops verifies that cancelling the
// context passed to watchServer causes the goroutine to exit promptly.
func TestManagerGaps_WatchServer_ContextCancelStops(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	cfg := MCPServerConfig{Name: "ctx-cancel-srv"}
	mgr := newTestManager(nil,
		WithHealthInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	ms := &managedServer{
		cfg:    cfg,
		client: client,
	}

	done := make(chan struct{})
	reg := tools.NewRegistry()
	go func() {
		mgr.watchServer(ctx, ms, reg)
		close(done)
	}()

	// Cancel quickly; goroutine should exit.
	cancel()
	select {
	case <-done:
		// watchServer exited — pass
	case <-time.After(2 * time.Second):
		t.Error("watchServer did not exit after context cancellation within 2s")
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_RegisterServerTools_MultipleServersIndependent
// ---------------------------------------------------------------------------

// TestManagerGaps_RegisterServerTools_MultipleServersIndependent verifies that
// tools from different servers are tracked independently and do not interfere.
func TestManagerGaps_RegisterServerTools_MultipleServersIndependent(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	reg := tools.NewRegistry()
	mgr := newTestManager(nil)

	toolsA := []MCPTool{{Name: "a_tool", Description: "A", InputSchema: MCPInputSchema{Type: "object"}}}
	toolsB := []MCPTool{{Name: "b_tool", Description: "B", InputSchema: MCPInputSchema{Type: "object"}}}

	mgr.mu.Lock()
	mgr.registerServerTools("server_a", client, toolsA, reg)
	mgr.registerServerTools("server_b", client, toolsB, reg)
	mgr.mu.Unlock()

	// Both tools should be present.
	if _, ok := reg.Get("a_tool"); !ok {
		t.Error("a_tool should be registered")
	}
	if _, ok := reg.Get("b_tool"); !ok {
		t.Error("b_tool should be registered")
	}

	// Re-registering server_a should NOT remove server_b's tools.
	newToolsA := []MCPTool{{Name: "a_new_tool", Description: "A2", InputSchema: MCPInputSchema{Type: "object"}}}
	mgr.mu.Lock()
	mgr.registerServerTools("server_a", client, newToolsA, reg)
	mgr.mu.Unlock()

	if _, ok := reg.Get("a_tool"); ok {
		t.Error("a_tool should have been removed after server_a re-registration")
	}
	if _, ok := reg.Get("b_tool"); !ok {
		t.Error("b_tool should still be registered (different server)")
	}
	if _, ok := reg.Get("a_new_tool"); !ok {
		t.Error("a_new_tool should be registered")
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_WithHealthInterval_Applied
// ---------------------------------------------------------------------------

// TestManagerGaps_WithHealthInterval_Applied verifies that the WithHealthInterval
// option correctly overrides the default health interval.
func TestManagerGaps_WithHealthInterval_Applied(t *testing.T) {
	mgr := newTestManager(nil, WithHealthInterval(42*time.Millisecond))
	if mgr.healthInterval != 42*time.Millisecond {
		t.Errorf("expected healthInterval=42ms, got %v", mgr.healthInterval)
	}
}

// ---------------------------------------------------------------------------
// TestManagerGaps_StopAll_NilClients_NoPanic
// ---------------------------------------------------------------------------

// TestManagerGaps_StopAll_NilClients_NoPanic ensures StopAll on an empty
// manager doesn't panic.
func TestManagerGaps_StopAll_NilClients_NoPanic(t *testing.T) {
	mgr := newTestManager(nil)
	// Should not panic.
	mgr.StopAll(context.Background())
}

// ---------------------------------------------------------------------------
// TestManagerGaps_ProbeHealth_ListTools_Fails
// ---------------------------------------------------------------------------

// TestManagerGaps_ProbeHealth_ListTools_Fails verifies that when probedWithListTools
// is true and ListTools itself fails, the error is propagated.
func TestManagerGaps_ProbeHealth_ListTools_Fails(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)

	ms := &managedServer{
		cfg:                 MCPServerConfig{Name: "bad-list-srv"},
		client:              client,
		probedWithListTools: true,
	}

	// Return a transport error on the next Receive.
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Error:   &RPCError{Code: -32001, Message: "list tools error"},
	})

	mgr := newTestManager(nil)
	err := mgr.probeHealth(context.Background(), ms)
	if err == nil {
		t.Fatal("expected error when ListTools fails, got nil")
	}
}
