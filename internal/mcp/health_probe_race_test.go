package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// TestServerManager_ConcurrentHealthProbes tests for race conditions in
// probeHealth when multiple goroutines probe the same server concurrently.
// The test uses separate clients per goroutine to avoid the inherent sequencing
// requirement of the recvLoop (which matches responses to requests by ID).
func TestServerManager_ConcurrentHealthProbes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const n = 10
	cfg := MCPServerConfig{Name: "test-server"}
	manager := NewServerManager([]MCPServerConfig{cfg}, WithHealthInterval(10*time.Millisecond))

	// Create n separate managed servers each with their own transport/client,
	// so concurrent probes do not share a single request ID space.
	mss := make([]*managedServer, n)
	for i := 0; i < n; i++ {
		tr := &mockTransport{}
		tr.queueResponse(Response{
			JSONRPC: "2.0",
			ID:      1, // first request on each fresh client gets ID 1
			Result:  json.RawMessage(`{}`),
		})
		mss[i] = &managedServer{
			cfg:    cfg,
			client: NewMCPClient(tr),
		}
	}

	manager.mu.Lock()
	manager.clients = append(manager.clients, mss...)
	manager.mu.Unlock()

	// Simulate n concurrent probes, each on a separate managed server.
	var wg sync.WaitGroup
	var errCount int32
	for i := 0; i < n; i++ {
		ms := mss[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := manager.probeHealth(ctx, ms); err != nil {
				atomic.AddInt32(&errCount, 1)
			}
		}()
	}
	wg.Wait()

	// All n probes should succeed.
	if atomic.LoadInt32(&errCount) != 0 {
		t.Errorf("expected 0 probe errors, got %d", errCount)
	}
}

// TestServerManager_ProbeFallbackStateSynchronization verifies that when
// a server doesn't support ping, the fallback state is correctly synchronized
// across sequential probes.
func TestServerManager_ProbeFallbackStateSynchronization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := &mockTransport{}
	client := NewMCPClient(tr)

	cfg := MCPServerConfig{Name: "test-server"}
	manager := NewServerManager([]MCPServerConfig{cfg})

	manager.mu.Lock()
	ms := &managedServer{
		cfg:    cfg,
		client: client,
	}
	manager.clients = append(manager.clients, ms)
	manager.mu.Unlock()

	// First probe: ping fails with MethodNotFound, falls back to ListTools.
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Error:   &RPCError{Code: MethodNotFoundCode, Message: "Method not found"},
	})
	toolsResult, _ := json.Marshal(MCPToolsListResult{Tools: []MCPTool{{Name: "test"}}})
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      2,
		Result:  toolsResult,
	})

	err := manager.probeHealth(ctx, ms)
	if err != nil {
		t.Fatalf("first probe failed: %v", err)
	}

	manager.mu.Lock()
	if !ms.probedWithListTools {
		t.Error("expected probedWithListTools=true after fallback")
	}
	manager.mu.Unlock()

	// Second probe should skip ping and go directly to ListTools.
	toolsResult2, _ := json.Marshal(MCPToolsListResult{Tools: []MCPTool{}})
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      3,
		Result:  toolsResult2,
	})

	err = manager.probeHealth(ctx, ms)
	if err != nil {
		t.Fatalf("second probe failed: %v", err)
	}

	// First probe: 2 sends (ping + ListTools). Second probe: 1 send (ListTools only).
	tr.mu.Lock()
	sendCount := len(tr.sends)
	tr.mu.Unlock()
	if sendCount != 3 {
		t.Errorf("expected 3 total sends (ping+listtools+listtools), got %d", sendCount)
	}
}

// TestServerManager_WatchServerGoroutineLeakOnFactoryHang verifies that
// the watchServer goroutine doesn't leak if factory never returns.
func TestServerManager_WatchServerGoroutineLeakOnFactoryHang(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Create a factory that hangs forever.
	factoryCalled := make(chan struct{}, 1)
	cfg := MCPServerConfig{Name: "hang-server", Command: "cat"}
	manager := NewServerManager([]MCPServerConfig{cfg}, WithClientFactory(func(innerCtx context.Context, cfg MCPServerConfig) (*MCPClient, []MCPTool, error) {
		select {
		case factoryCalled <- struct{}{}:
		default:
		}
		// Hang until context cancels.
		<-innerCtx.Done()
		return nil, nil, innerCtx.Err()
	}))

	reg := tools.NewRegistry()
	// StartAll calls the factory synchronously; it blocks until ctx expires.
	// The factory sends to factoryCalled (buffered) before blocking on ctx.Done().
	// After 1s ctx expires, factory returns, StartAll returns.
	manager.StartAll(ctx, reg)

	// factoryCalled must have been sent to (factory was called).
	select {
	case <-factoryCalled:
		// good
	default:
		t.Error("factory was never called by StartAll")
	}

	// Context already expired; cancel is a no-op but safe to call.
	cancel()
	// If we reach here without deadlock, test passes.
}

// TestServerManager_HealthProbeContextTimeout verifies that a context timeout
// in probeHealth is properly propagated.
func TestServerManager_HealthProbeContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Use a slow transport that blocks on Receive longer than the context allows.
	slowTransport := &slowMockTransport{
		sendDelay:    0,
		receiveDelay: 500 * time.Millisecond,
	}
	client := NewMCPClient(slowTransport)

	cfg := MCPServerConfig{Name: "test-server"}
	manager := NewServerManager([]MCPServerConfig{cfg})

	manager.mu.Lock()
	ms := &managedServer{
		cfg:    cfg,
		client: client,
	}
	manager.clients = append(manager.clients, ms)
	manager.mu.Unlock()

	// probeHealth should return context error, not wait for slow ping.
	err := manager.probeHealth(ctx, ms)
	if err == nil {
		t.Fatal("expected timeout error from context")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

// TestMCPClient_InitializeTimeoutMidStream verifies that Initialize properly
// respects context timeout even mid-stream.
func TestMCPClient_InitializeTimeoutMidStream(t *testing.T) {
	// Create a transport that delays Receive beyond the context timeout.
	slowTransport := &slowMockTransport{
		sendDelay:    0,
		receiveDelay: 500 * time.Millisecond,
	}
	client := NewMCPClient(slowTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Initialize should timeout during Receive.
	err := client.Initialize(ctx)
	if err == nil {
		t.Fatal("expected timeout error during Initialize")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		if !containsStr(err.Error(), "timeout") && !containsStr(err.Error(), "deadline") {
			t.Fatalf("expected timeout error, got: %v", err)
		}
	}
}

// containsStr reports whether s contains substr.
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// slowMockTransport delays Send/Receive operations to simulate a slow server.
type slowMockTransport struct {
	sendDelay    time.Duration
	receiveDelay time.Duration
}

func (t *slowMockTransport) Send(ctx context.Context, data []byte) error {
	select {
	case <-time.After(t.sendDelay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *slowMockTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case <-time.After(t.receiveDelay):
		resp := `{"jsonrpc":"2.0","id":1,"result":{}}`
		return []byte(resp), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *slowMockTransport) Close() error { return nil }
