package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// wrapReceiveErr — tested indirectly through Initialize/ListTools/CallTool
// with io.EOF and io.ErrUnexpectedEOF on the receive side.
// These tests ensure the wrapReceiveErr branch for io.EOF on Initialize is hit.
// ---------------------------------------------------------------------------

func TestMCPClient_Initialize_EOFOnReceive(t *testing.T) {
	tr := &MockTransport{
		// The Send call will succeed (no err on send), but Receive returns io.EOF.
		toSend: nil,
		err:    io.EOF,
	}
	c := mcp.NewMCPClient(tr)
	err := c.Initialize(context.Background())
	if err == nil {
		t.Error("expected error for EOF on initialize receive")
	}
	if !stringContains(err.Error(), "server disconnected") && !stringContains(err.Error(), "EOF") {
		t.Errorf("expected disconnect message, got: %v", err)
	}
}

func TestMCPClient_Initialize_UnexpectedEOFOnReceive(t *testing.T) {
	tr := &MockTransport{
		toSend: nil,
		err:    io.ErrUnexpectedEOF,
	}
	c := mcp.NewMCPClient(tr)
	err := c.Initialize(context.Background())
	if err == nil {
		t.Error("expected error for unexpected EOF on initialize receive")
	}
}

// ---------------------------------------------------------------------------
// defaultClientFactory — exercises NewStdioTransport via the default factory
// by calling NewServerManager without custom factory.
// ---------------------------------------------------------------------------

func TestDefaultClientFactory_StdioTransport_ValidCommand(t *testing.T) {
	// "cat" is available on macOS/Linux and can be started as a process.
	// The factory will start it, try to Initialize (which will fail because
	// cat doesn't speak JSON-RPC), so the server will be logged as unavailable.
	cfgs := []mcp.MCPServerConfig{
		{Name: "cat-test", Command: "cat"},
	}

	manager := mcp.NewServerManager(cfgs)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// This exercises defaultClientFactory -> NewStdioTransport -> cmd.Start()
	// It will fail at Initialize() since cat doesn't speak JSON-RPC, but
	// that exercises NewStdioTransport's happy path (process started).
	manager.StartAll(ctx, reg)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	manager.StopAll(stopCtx)
}

func TestDefaultClientFactory_InvalidCommand(t *testing.T) {
	// A command that doesn't exist — NewStdioTransport should fail at Start().
	cfgs := []mcp.MCPServerConfig{
		{Name: "bad-cmd", Command: "/nonexistent/command/xyz"},
	}

	manager := mcp.NewServerManager(cfgs)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Should log the error and continue without panicking.
	manager.StartAll(ctx, reg)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()
	manager.StopAll(stopCtx)
}

// ---------------------------------------------------------------------------
// watchServer — exercises the ListTools healthy branch (ctx cancel path)
// ---------------------------------------------------------------------------

func TestServerManager_WatchServer_ContextCancel_WhileHealthy(t *testing.T) {
	callCount := 0

	mockFactory := func(ctx context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		callCount++
		// Create a transport that serves enough responses for one ListTools call
		// (used by watchServer to check health).
		tr := &MockTransport{
			toSend: [][]byte{
				buildToolsListResponse(1, []map[string]any{}),
				// Second ListTools will block (no more responses), so we rely on ctx cancel.
			},
		}
		client := mcp.NewMCPClient(tr)
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "watch-test", Command: "cat"},
	}

	manager := mcp.NewServerManager(cfgs,
		mcp.WithClientFactory(mockFactory),
		mcp.WithRestartBackoff(10*time.Millisecond, 50*time.Millisecond),
	)
	reg := tools.NewRegistry()

	// Start then quickly cancel to exercise the watchServer ctx.Done() paths.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	manager.StartAll(ctx, reg)

	// Let watchServer run for a bit (it will succeed one ListTools then wait 30s).
	time.Sleep(50 * time.Millisecond)
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()
	manager.StopAll(stopCtx)
}

// ---------------------------------------------------------------------------
// watchServer — exercises the restart path (ListTools fails → restart)
// ---------------------------------------------------------------------------

func TestServerManager_WatchServer_RestartAfterFailure(t *testing.T) {
	var restartCount int32
	var mu sync.Mutex

	mockFactory := func(ctx context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		mu.Lock()
		atomic.AddInt32(&restartCount, 1)
		count := atomic.LoadInt32(&restartCount)
		mu.Unlock()

		if count == 1 {
			// First client: ListTools will fail (no responses in transport).
			tr := &MockTransport{toSend: nil}
			client := mcp.NewMCPClient(tr)
			return client, []mcp.MCPTool{}, nil
		}
		// Subsequent clients: succeed so watchServer stabilizes.
		tr := &MockTransport{
			toSend: [][]byte{
				buildToolsListResponse(1, []map[string]any{}),
			},
		}
		client := mcp.NewMCPClient(tr)
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "restart-test", Command: "cat"},
	}

	manager := mcp.NewServerManager(cfgs,
		mcp.WithClientFactory(mockFactory),
		mcp.WithRestartBackoff(10*time.Millisecond, 50*time.Millisecond),
	)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	manager.StartAll(ctx, reg)

	// Allow time for restart to happen.
	time.Sleep(200 * time.Millisecond)
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()
	manager.StopAll(stopCtx)
}

// ---------------------------------------------------------------------------
// watchServer — backoff cap logic (backoff > maxBackoff)
// ---------------------------------------------------------------------------

func TestServerManager_WatchServer_BackoffCap(t *testing.T) {
	// Factory always fails after the first call (initial start was through StartAll).
	var firstCallDone int32
	var mu sync.Mutex
	mockFactory := func(ctx context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		mu.Lock()
		isFirst := atomic.LoadInt32(&firstCallDone) == 0
		if isFirst {
			atomic.StoreInt32(&firstCallDone, 1)
		}
		mu.Unlock()

		if isFirst {
			// Initial start: succeed.
			tr := &MockTransport{toSend: nil} // ListTools will fail quickly
			return mcp.NewMCPClient(tr), []mcp.MCPTool{}, nil
		}
		// Restart factory always fails to exercise backoff cap.
		return nil, nil, context.DeadlineExceeded
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "backoff-test", Command: "cat"},
	}

	// Set very small backoff so the test completes quickly.
	manager := mcp.NewServerManager(cfgs,
		mcp.WithClientFactory(mockFactory),
		mcp.WithRestartBackoff(5*time.Millisecond, 10*time.Millisecond),
	)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	manager.StartAll(ctx, reg)

	time.Sleep(150 * time.Millisecond)
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()
	manager.StopAll(stopCtx)
}

// ---------------------------------------------------------------------------
// NewStdioTransport — exercises Send on closed transport
// ---------------------------------------------------------------------------

func TestStdioTransport_Send_WhenClosed(t *testing.T) {
	// Start "cat" and immediately close it, then try to Send.
	tr, err := mcp.NewStdioTransport(context.Background(), "cat", nil, nil)
	if err != nil {
		t.Skipf("cat not available: %v", err)
	}
	tr.Close()
	err = tr.Send(context.Background(), []byte(`{"test":true}`))
	if err == nil {
		t.Error("expected error when sending on closed transport")
	}
}

// ---------------------------------------------------------------------------
// NewStdioTransport — Receive with cancelled context
// ---------------------------------------------------------------------------

func TestStdioTransport_Receive_CancelledContext(t *testing.T) {
	tr, err := mcp.NewStdioTransport(context.Background(), "cat", nil, nil)
	if err != nil {
		t.Skipf("cat not available: %v", err)
	}
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already cancelled

	_, err = tr.Receive(ctx)
	if err == nil {
		t.Error("expected error for cancelled context in Receive")
	}
}

// ---------------------------------------------------------------------------
// NewStdioTransport — with env vars
// ---------------------------------------------------------------------------

func TestStdioTransport_WithEnv(t *testing.T) {
	tr, err := mcp.NewStdioTransport(
		context.Background(),
		"cat",
		nil,
		[]string{"MY_TEST_VAR=hello"},
	)
	if err != nil {
		t.Skipf("cat not available: %v", err)
	}
	defer tr.Close()
	// Just verify it started without error.
}

// ---------------------------------------------------------------------------
// watchServer — ctx.Done() while waiting after successful health check
// ---------------------------------------------------------------------------

func TestServerManager_WatchServer_CtxCancelAfterHealthyListTools(t *testing.T) {
	// Provide enough responses so ListTools succeeds, then ctx is cancelled
	// while the watchServer is sleeping in time.After(30s).
	mockFactory := func(ctx context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		listResp, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"tools": []any{}},
		})
		tr := &MockTransport{
			toSend: [][]byte{listResp},
		}
		client := mcp.NewMCPClient(tr)
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "healthy-test", Command: "cat"},
	}

	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(mockFactory))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	manager.StartAll(ctx, reg)

	// Give watchServer time to complete the ListTools health check.
	time.Sleep(50 * time.Millisecond)
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stopCancel()
	manager.StopAll(stopCtx)
}

