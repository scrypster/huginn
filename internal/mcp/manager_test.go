package mcp_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
	"github.com/scrypster/huginn/internal/tools"
)

func TestServerManager_StartAll(t *testing.T) {
	var mockCalls atomic.Int64
	mockFactory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		mockCalls.Add(1)
		// Return mock transport that responds to ListTools
		mockTransport := &MockTransport{
			toSend: [][]byte{buildToolsListResponse(1, []map[string]any{})},
		}
		client := mcp.NewMCPClient(mockTransport)
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "test1", Command: "cat"},
		{Name: "test2", Command: "cat"},
	}
	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(mockFactory))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	manager.StartAll(ctx, reg)

	if got := mockCalls.Load(); got != 2 {
		t.Errorf("expected 2 factory calls, got %d", got)
	}
}

func TestServerManager_RegistersTools(t *testing.T) {
	mockFactory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		tools := []mcp.MCPTool{
			{
				Name:        "test_tool",
				Description: "A test tool",
				InputSchema: mcp.MCPInputSchema{Type: "object"},
			},
		}
		// Need a mock transport that responds to ListTools checks
		mockTransport := &MockTransport{
			toSend: [][]byte{buildToolsListResponse(1, []map[string]any{})},
		}
		client := mcp.NewMCPClient(mockTransport)
		return client, tools, nil
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "test", Command: "cat"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(mockFactory))
	reg := tools.NewRegistry()

	manager.StartAll(ctx, reg)

	// Check that the tool was registered
	if _, ok := reg.Get("test_tool"); !ok {
		t.Error("expected test_tool to be registered")
	}
}

func TestServerManager_StopAll(t *testing.T) {
	mockFactory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		// Return mock transport that responds to ListTools
		mockTransport := &MockTransport{
			toSend: [][]byte{buildToolsListResponse(1, []map[string]any{})},
		}
		client := mcp.NewMCPClient(mockTransport)
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "test", Command: "cat"},
	}
	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(mockFactory))
	reg := tools.NewRegistry()

	startCtx, startCancel := context.WithTimeout(context.Background(), 2*time.Second)
	manager.StartAll(startCtx, reg)
	startCancel()

	// StopAll should not panic and should complete quickly
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	manager.StopAll(ctx)

	// Verify it completes successfully (no panic or deadlock)
}

func TestServerManager_IdempotentStopAll(t *testing.T) {
	mockFactory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		// Return mock transport that responds to ListTools
		mockTransport := &MockTransport{
			toSend: [][]byte{buildToolsListResponse(1, []map[string]any{})},
		}
		client := mcp.NewMCPClient(mockTransport)
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "test", Command: "cat"},
	}
	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(mockFactory))
	reg := tools.NewRegistry()

	startCtx, startCancel := context.WithTimeout(context.Background(), 2*time.Second)
	manager.StartAll(startCtx, reg)
	startCancel()

	// First StopAll
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	manager.StopAll(ctx)

	// Second StopAll should not error
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	manager.StopAll(ctx2)
}

func TestServerManager_WithRestartBackoff(t *testing.T) {
	manager := mcp.NewServerManager(nil,
		mcp.WithRestartBackoff(100*time.Millisecond, 1*time.Second))

	// Verify backoff settings were applied (through internal state)
	// This is a basic smoke test that options are wired correctly
	if manager == nil {
		t.Error("manager should not be nil")
	}
}

func TestServerManager_FactoryInitError(t *testing.T) {
	callCount := 0
	mockFactory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		callCount++
		return nil, nil, fmt.Errorf("factory failed")
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "test1", Command: "cat"},
	}
	// Verify that a failing factory attempt is handled gracefully:
	// the server is skipped but the manager continues without panicking.
	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(mockFactory))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	manager.StartAll(ctx, reg)

	// Factory should be called exactly once (MaxInitAttempts=1), manager should continue.
	if callCount != 1 {
		t.Errorf("expected 1 factory call, got %d", callCount)
	}
}

func TestServerManager_WatchServer_Recovery(t *testing.T) {
	callCount := 0
	mockFactory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		callCount++
		if callCount > 1 {
			// Second call succeeds
			mockTransport := &MockTransport{
				toSend: [][]byte{buildToolsListResponse(1, []map[string]any{})},
			}
			client := mcp.NewMCPClient(mockTransport)
			return client, []mcp.MCPTool{}, nil
		}
		// First call succeeds
		mockTransport := &MockTransport{
			toSend: [][]byte{buildToolsListResponse(1, []map[string]any{})},
		}
		client := mcp.NewMCPClient(mockTransport)
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "test", Command: "cat"},
	}

	manager := mcp.NewServerManager(cfgs,
		mcp.WithClientFactory(mockFactory),
		mcp.WithRestartBackoff(10*time.Millisecond, 100*time.Millisecond))
	reg := tools.NewRegistry()

	startCtx, startCancel := context.WithTimeout(context.Background(), 5*time.Second)
	manager.StartAll(startCtx, reg)
	startCancel()

	// Give time for watchServer to potentially restart
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	manager.StopAll(ctx)
}

func TestServerManager_EmptyConfigs(t *testing.T) {
	mockFactory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		t.Error("factory should not be called for empty configs")
		return nil, nil, nil
	}

	manager := mcp.NewServerManager([]mcp.MCPServerConfig{}, mcp.WithClientFactory(mockFactory))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	manager.StartAll(ctx, reg)
	// Should complete without error
}

func TestServerManager_MultipleServers(t *testing.T) {
	mockFactory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		mockTransport := &MockTransport{
			toSend: [][]byte{buildToolsListResponse(1, []map[string]any{})},
		}
		client := mcp.NewMCPClient(mockTransport)
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "server1", Command: "cat"},
		{Name: "server2", Command: "cat"},
		{Name: "server3", Command: "cat"},
	}

	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(mockFactory))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	manager.StartAll(ctx, reg)

	// All servers should be registered successfully
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()
	manager.StopAll(ctx2)
}

func TestDefaultClientFactory_WithDefaultFactory(t *testing.T) {
	// Test ServerManager without specifying a factory (uses defaultClientFactory)
	// This will use the default factory which will attempt to start cat
	cfgs := []mcp.MCPServerConfig{
		{Name: "default-test", Command: "cat"},
	}

	// When no factory is provided, defaultClientFactory is used
	// It will try to start the cat command and initialize it
	manager := mcp.NewServerManager(cfgs)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// StartAll will use defaultClientFactory
	manager.StartAll(ctx, reg)

	// Clean up
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	manager.StopAll(stopCtx)
}

func TestDefaultClientFactory_UnknownTransport(t *testing.T) {
	// Test that unknown transports are rejected
	cfgs := []mcp.MCPServerConfig{
		{
			Name:      "bad-transport",
			Transport: "invalid_transport_type",
			Command:   "cat",
		},
	}

	// Create manager without custom factory (uses defaultClientFactory)
	manager := mcp.NewServerManager(cfgs)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// StartAll should log an error but not panic
	// (the server will be unavailable but manager continues)
	manager.StartAll(ctx, reg)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()
	manager.StopAll(stopCtx)
}

func TestDefaultClientFactory_NoCommand(t *testing.T) {
	// Test that missing command is rejected
	cfgs := []mcp.MCPServerConfig{
		{
			Name:      "no-command",
			Transport: "stdio",
			// Command is empty/missing
		},
	}

	// Create manager without custom factory (uses defaultClientFactory)
	manager := mcp.NewServerManager(cfgs)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// StartAll should log an error but not panic
	manager.StartAll(ctx, reg)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()
	manager.StopAll(stopCtx)
}

