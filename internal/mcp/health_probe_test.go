package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// mockTransport implements Transport for testing.
type mockTransport struct {
	mu       sync.Mutex
	sends    [][]byte
	responds [][]byte // queued responses for Receive calls
	closed   bool
}

func (m *mockTransport) Send(_ context.Context, msg []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, msg)
	return nil
}

func (m *mockTransport) Receive(_ context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.responds) == 0 {
		return nil, fmt.Errorf("no queued response")
	}
	resp := m.responds[0]
	m.responds = m.responds[1:]
	return resp, nil
}

func (m *mockTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockTransport) queueResponse(resp Response) {
	data, _ := json.Marshal(resp)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responds = append(m.responds, data)
}

func TestProbeHealth_PingSuccess(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)
	ms := &managedServer{
		cfg:    MCPServerConfig{Name: "test-server"},
		client: client,
	}

	// Queue a successful ping response.
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  json.RawMessage(`{}`),
	})

	mgr := NewServerManager(nil)
	err := mgr.probeHealth(context.Background(), ms)
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
	if ms.probedWithListTools {
		t.Error("expected probedWithListTools to remain false after successful ping")
	}
}

func TestProbeHealth_PingMethodNotFound_FallbackToListTools(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)
	ms := &managedServer{
		cfg:    MCPServerConfig{Name: "test-server"},
		client: client,
	}

	// Queue a MethodNotFound error for ping.
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Error:   &RPCError{Code: MethodNotFoundCode, Message: "Method not found"},
	})

	// Queue a successful ListTools response.
	toolsResult, _ := json.Marshal(MCPToolsListResult{Tools: []MCPTool{{Name: "test_tool"}}})
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      2,
		Result:  toolsResult,
	})

	mgr := NewServerManager(nil)
	err := mgr.probeHealth(context.Background(), ms)
	if err != nil {
		t.Errorf("expected nil error after fallback, got: %v", err)
	}
	if !ms.probedWithListTools {
		t.Error("expected probedWithListTools to be true after MethodNotFound fallback")
	}

	// Second probe should skip ping and go directly to ListTools.
	toolsResult2, _ := json.Marshal(MCPToolsListResult{Tools: []MCPTool{}})
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      3,
		Result:  toolsResult2,
	})

	err = mgr.probeHealth(context.Background(), ms)
	if err != nil {
		t.Errorf("expected nil error on second probe, got: %v", err)
	}

	// Verify only ListTools was called (no ping send for the second probe).
	tr.mu.Lock()
	sendCount := len(tr.sends)
	tr.mu.Unlock()
	// First probe: 1 ping send + 1 ListTools send = 2
	// Second probe: 1 ListTools send = 1
	// Total: 3
	if sendCount != 3 {
		t.Errorf("expected 3 total sends (ping + ListTools + ListTools), got %d", sendCount)
	}
}

func TestProbeHealth_PingOtherError_Propagated(t *testing.T) {
	tr := &mockTransport{}
	client := NewMCPClient(tr)
	ms := &managedServer{
		cfg:    MCPServerConfig{Name: "test-server"},
		client: client,
	}

	// Queue a non-MethodNotFound RPC error.
	tr.queueResponse(Response{
		JSONRPC: "2.0",
		ID:      1,
		Error:   &RPCError{Code: -32000, Message: "internal error"},
	})

	mgr := NewServerManager(nil)
	err := mgr.probeHealth(context.Background(), ms)
	if err == nil {
		t.Fatal("expected error to be propagated")
	}
	if ms.probedWithListTools {
		t.Error("should not fall back to ListTools for non-MethodNotFound errors")
	}
}
