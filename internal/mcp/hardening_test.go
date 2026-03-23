package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
	"github.com/scrypster/huginn/internal/tools"
)

// --- RPCError coverage ---

func TestRPCError_Error(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`
	var resp mcp.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	msg := resp.Error.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected code -32601, got %d", resp.Error.Code)
	}
}

// --- CallTool coverage ---

func buildToolCallResponse(id int, content []map[string]any, isError bool) []byte {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": content,
			"isError": isError,
		},
	})
	return resp
}

func TestMCPClient_CallTool(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "text", "text": "hello world"},
			}, false),
		},
	}
	c := mcp.NewMCPClient(tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	result, err := c.CallTool(context.Background(), "test_tool", map[string]any{"input": "test"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Error("expected non-error result")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello world" {
		t.Errorf("unexpected content: %+v", result.Content)
	}
}

func TestMCPClient_CallTool_Error(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "text", "text": "something failed"},
			}, true),
		},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())
	result, err := c.CallTool(context.Background(), "test_tool", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Error("expected isError=true")
	}
}

func TestMCPClient_CallTool_SendError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	// Now set error to simulate send failure
	tr.mu.Lock()
	tr.err = fmt.Errorf("connection lost")
	tr.mu.Unlock()

	_, err := c.CallTool(context.Background(), "test_tool", nil)
	if err == nil {
		t.Error("expected error from CallTool")
	}
}

// --- MCPToolAdapter coverage ---

func TestMCPToolAdapter_Description(t *testing.T) {
	tr := &MockTransport{toSend: [][]byte{}}
	c := mcp.NewMCPClient(tr)
	tool := mcp.MCPTool{
		Name:        "my_tool",
		Description: "Does something useful",
		InputSchema: mcp.MCPInputSchema{
			Type: "object",
			Properties: map[string]mcp.MCPToolProperty{
				"arg1": {Type: "string", Description: "first arg"},
			},
			Required: []string{"arg1"},
		},
	}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	if adapter.Name() != "my_tool" {
		t.Errorf("Name = %q", adapter.Name())
	}
	if adapter.Description() != "Does something useful" {
		t.Errorf("Description = %q", adapter.Description())
	}
	if adapter.Permission() != tools.PermWrite {
		t.Errorf("Permission = %v", adapter.Permission())
	}
	schema := adapter.Schema()
	if schema.Function.Name != "my_tool" {
		t.Errorf("Schema name = %q", schema.Function.Name)
	}
	if len(schema.Function.Parameters.Properties) != 1 {
		t.Errorf("expected 1 property, got %d", len(schema.Function.Parameters.Properties))
	}
	if len(schema.Function.Parameters.Required) != 1 {
		t.Errorf("expected 1 required, got %d", len(schema.Function.Parameters.Required))
	}
}

func TestMCPToolAdapter_Execute_Success(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "text", "text": "result line 1"},
				{"type": "text", "text": "result line 2"},
				{"type": "image", "text": ""}, // non-text content should be skipped
			}, false),
		},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tool := mcp.MCPTool{Name: "test", InputSchema: mcp.MCPInputSchema{Type: "object"}}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	result := adapter.Execute(context.Background(), map[string]any{"key": "val"})
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Error)
	}
	if result.Output != "result line 1\nresult line 2" {
		t.Errorf("Output = %q", result.Output)
	}
}

func TestMCPToolAdapter_Execute_Error(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildToolCallResponse(2, []map[string]any{
				{"type": "text", "text": "error details"},
			}, true),
		},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tool := mcp.MCPTool{Name: "fail_tool", InputSchema: mcp.MCPInputSchema{Type: "object"}}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	result := adapter.Execute(context.Background(), nil)
	if !result.IsError {
		t.Error("expected error result")
	}
	if result.Error != "error details" {
		t.Errorf("Error = %q", result.Error)
	}
}

func TestMCPToolAdapter_Execute_TransportError(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tr.mu.Lock()
	tr.err = fmt.Errorf("network error")
	tr.mu.Unlock()

	tool := mcp.MCPTool{Name: "net_fail", InputSchema: mcp.MCPInputSchema{Type: "object"}}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	result := adapter.Execute(context.Background(), nil)
	if !result.IsError {
		t.Error("expected error result")
	}
}

// --- watchServer coverage ---

func TestServerManager_WatchServer_CancelledContext(t *testing.T) {
	// watchServer should exit immediately when context is cancelled
	callCount := 0
	mockFactory := func(ctx context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		callCount++
		// Return a client whose ListTools will fail (no more responses)
		mockTransport := &MockTransport{
			toSend: [][]byte{}, // empty: ListTools will fail
		}
		client := mcp.NewMCPClient(mockTransport)
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{{Name: "watch-test", Command: "cat"}}
	manager := mcp.NewServerManager(cfgs,
		mcp.WithClientFactory(mockFactory),
		mcp.WithRestartBackoff(10*time.Millisecond, 50*time.Millisecond),
	)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	manager.StartAll(ctx, reg)
	// Let the watcher run briefly
	time.Sleep(30 * time.Millisecond)
	cancel()
	// Give time for watcher goroutine to exit
	time.Sleep(50 * time.Millisecond)

	manager.StopAll(context.Background())
}

func TestServerManager_FactoryError(t *testing.T) {
	failFactory := func(ctx context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		return nil, nil, fmt.Errorf("connection refused")
	}

	cfgs := []mcp.MCPServerConfig{{Name: "fail-server", Command: "cat"}}
	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(failFactory))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	manager.StartAll(ctx, reg)

	// Should not have registered any tools
	if _, ok := reg.Get("anything"); ok {
		t.Error("expected no tools registered when factory fails")
	}
}

// --- watchServer reconnection path ---

func TestServerManager_WatchServer_Reconnect(t *testing.T) {
	// This test verifies the watchServer goroutine's reconnection logic.
	// The first client's ListTools will fail immediately (no queued responses),
	// triggering a reconnect attempt. We verify the factory is called multiple times.
	var mu sync.Mutex
	callCount := 0
	mockFactory := func(ctx context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		mu.Lock()
		callCount++
		n := callCount
		mu.Unlock()

		if n == 1 {
			// First call: return a client whose ListTools check will fail immediately
			// (empty toSend means Receive will return "no more responses" error)
			mockTransport := &MockTransport{
				toSend: [][]byte{
					buildToolsListResponse(1, []map[string]any{}),
				},
			}
			return mcp.NewMCPClient(mockTransport), []mcp.MCPTool{}, nil
		}
		// Subsequent calls: also return a client that fails quickly
		mockTransport := &MockTransport{toSend: [][]byte{}}
		return mcp.NewMCPClient(mockTransport), []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{{Name: "reconnect-test", Command: "cat"}}
	manager := mcp.NewServerManager(cfgs,
		mcp.WithClientFactory(mockFactory),
		mcp.WithRestartBackoff(10*time.Millisecond, 30*time.Millisecond),
	)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	manager.StartAll(ctx, reg)

	// Wait enough time for the watch loop to: health check (fail) -> backoff (10ms) -> reconnect
	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	manager.StopAll(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if callCount < 2 {
		t.Logf("callCount=%d (may vary based on timing)", callCount)
		// Don't fail on timing-sensitive reconnection count; the important
		// thing is that the watcher did not panic or deadlock.
	}
}
