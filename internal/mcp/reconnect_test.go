package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// blockingTransport is a Transport whose Receive blocks until the context is
// cancelled. Sending a ping to a client backed by this transport causes the
// health probe to block for the full cbProbeTimeout duration rather than
// returning an error immediately. This keeps the probe goroutine alive long
// enough for the test to observe a stable registry state.
type blockingTransport struct{}

func (b *blockingTransport) Send(_ context.Context, _ []byte) error { return nil }
func (b *blockingTransport) Receive(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (b *blockingTransport) Close() error { return nil }

// TestServerManager_WatchServer_Reconnect verifies that watchServer reconnects
// to a server that goes down and then comes back up, re-registering tools.
func TestServerManager_WatchServer_Reconnect(t *testing.T) {
	var factoryCalls atomic.Int32

	// First factory call uses a mock transport that immediately fails health
	// probes (no queued responses), triggering a reconnect. The second call uses
	// a blockingTransport so the health probe blocks for cbProbeTimeout instead
	// of failing immediately — this keeps tool_v2 stably in the registry for
	// long enough to observe, preventing a reconnect storm in slow CI.
	factory := func(_ context.Context, cfg MCPServerConfig) (*MCPClient, []MCPTool, error) {
		n := factoryCalls.Add(1)
		if n == 1 {
			// Initial connect — immediately-failing transport triggers reconnect.
			client := NewMCPClient(&mockTransport{})
			return client, []MCPTool{{Name: "tool_v1", Description: "v1"}}, nil
		}
		// Reconnect — use a blocking transport so the probe doesn't fail
		// again instantly, giving the test a stable window to check the registry.
		client := NewMCPClient(&blockingTransport{})
		return client, []MCPTool{{Name: "tool_v2", Description: "v2"}}, nil
	}

	cfgs := []MCPServerConfig{{Name: "reconnect-test", Command: "cat"}}
	mgr := NewServerManager(cfgs, WithClientFactory(factory),
		WithRestartBackoff(5*time.Millisecond, 50*time.Millisecond),
		WithHealthInterval(50*time.Millisecond))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	mgr.StartAll(ctx, reg)

	// Wait for the watchServer goroutine to detect the health failure, reconnect,
	// and re-register the new tools. The blocking transport keeps tool_v2 stable.
	deadline := time.After(2 * time.Second)
	for {
		if factoryCalls.Load() >= 2 {
			if _, ok := reg.Get("tool_v2"); ok {
				break
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for reconnect; factory calls = %d", factoryCalls.Load())
		case <-time.After(20 * time.Millisecond):
		}
	}

	// After reconnect, tool_v2 should be registered and tool_v1 should be gone.
	if _, ok := reg.Get("tool_v2"); !ok {
		t.Error("expected tool_v2 to be registered after reconnect")
	}
	if _, ok := reg.Get("tool_v1"); ok {
		t.Error("expected tool_v1 to be unregistered after reconnect")
	}

	mgr.StopAll(ctx)
}

// TestProbeHealth_FallsBackToListTools verifies that when ping returns
// MethodNotFound (-32601), probedWithListTools is set to true and subsequent
// probes use ListTools directly.
func TestProbeHealth_FallsBackToListTools(t *testing.T) {
	tr := &trackingTransport{}
	client := NewMCPClient(tr)
	ms := &managedServer{
		cfg:    MCPServerConfig{Name: "fallback-test"},
		client: client,
	}

	// Queue MethodNotFound for ping, then a successful ListTools response.
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Error:   &RPCError{Code: MethodNotFoundCode, Message: "Method not found"},
	})
	toolsResult, _ := json.Marshal(MCPToolsListResult{Tools: []MCPTool{{Name: "probe_tool"}}})
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      2,
		Result:  toolsResult,
	})

	mgr := NewServerManager(nil)
	err := mgr.probeHealth(context.Background(), ms)
	if err != nil {
		t.Fatalf("expected nil error after fallback, got: %v", err)
	}

	mgr.mu.Lock()
	fallback := ms.probedWithListTools
	mgr.mu.Unlock()
	if !fallback {
		t.Fatal("expected probedWithListTools to be true after MethodNotFound")
	}

	// Second probe should skip ping entirely and go to ListTools.
	toolsResult2, _ := json.Marshal(MCPToolsListResult{Tools: []MCPTool{}})
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      3,
		Result:  toolsResult2,
	})

	err = mgr.probeHealth(context.Background(), ms)
	if err != nil {
		t.Fatalf("expected nil error on second probe, got: %v", err)
	}

	// Verify total sends: ping(1) + ListTools(1) + ListTools(1) = 3.
	tr.mu.Lock()
	count := len(tr.sends)
	tr.mu.Unlock()
	if count != 3 {
		t.Errorf("expected 3 total sends (ping + 2x ListTools), got %d", count)
	}
}

// trackingTransport is a mock Transport that tracks sends and queues responses.
type trackingTransport struct {
	mu       sync.Mutex
	sends    [][]byte
	responds [][]byte
	closed   bool
}

func (tt *trackingTransport) Send(_ context.Context, msg []byte) error {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.sends = append(tt.sends, msg)
	return nil
}

func (tt *trackingTransport) Receive(_ context.Context) ([]byte, error) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if len(tt.responds) == 0 {
		return nil, fmt.Errorf("no queued response")
	}
	resp := tt.responds[0]
	tt.responds = tt.responds[1:]
	return resp, nil
}

func (tt *trackingTransport) Close() error {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.closed = true
	return nil
}

func (tt *trackingTransport) queueResponse(resp Response) {
	data, _ := json.Marshal(resp)
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.responds = append(tt.responds, data)
}
