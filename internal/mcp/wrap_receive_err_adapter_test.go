package mcp_test

// hardening_iter7_test.go — Iteration 7 hardening tests for mcp package.
// Covers:
//   - wrapReceiveErr non-EOF path (passes arbitrary errors through unchanged)
//   - watchServer panic recovery (goroutine-level panic does not crash the process)
//   - Concurrent MCPClient calls (race detector coverage)
//   - MCPToolAdapter.Execute with all-non-text content (empty output path)
//   - MCPToolAdapter.Execute with mixed text/non-text content (partial output)
//   - MCPClient with nil args in CallTool
//   - StopAll is idempotent on an unstarted manager

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

// ---------------------------------------------------------------------------
// wrapReceiveErr — non-EOF path
// The function must pass non-EOF errors through unchanged (same pointer identity
// or at least same message). We test this by injecting a custom error and
// verifying it propagates correctly through ListTools.
// ---------------------------------------------------------------------------

func TestMCPClient_ListTools_ArbitraryReceiveError(t *testing.T) {
	// After init, set a non-EOF error on Receive. wrapReceiveErr should pass
	// it through as-is (the non-EOF branch just returns err directly).
	customErr := fmt.Errorf("custom transport error: pipe broken")
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
	}
	c := mcp.NewMCPClient(tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Inject our custom error for the next Receive call (ListTools).
	tr.mu.Lock()
	tr.err = customErr
	tr.mu.Unlock()

	_, err := c.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error from ListTools")
	}
	// The error should contain our custom message (wrapReceiveErr passes it through).
	if !stringContains(err.Error(), "custom transport error") {
		t.Errorf("expected custom error to propagate, got: %v", err)
	}
}

func TestMCPClient_CallTool_ArbitraryReceiveError(t *testing.T) {
	customErr := fmt.Errorf("arbitrary: dial tcp timeout")
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1)},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tr.mu.Lock()
	tr.err = customErr
	tr.mu.Unlock()

	_, err := c.CallTool(context.Background(), "tool", nil)
	if err == nil {
		t.Fatal("expected error from CallTool")
	}
	if !stringContains(err.Error(), "arbitrary") {
		t.Errorf("expected custom error to propagate, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// watchServer panic recovery
// We inject a factory that returns a client whose ListTools panics, then verify
// the manager does not crash and StopAll completes cleanly.
// ---------------------------------------------------------------------------

// panicTransport is a Transport whose Send panics to simulate unexpected panics.
type panicTransport struct{}

func (p *panicTransport) Send(_ context.Context, _ []byte) error {
	panic("simulated panic in transport")
}
func (p *panicTransport) Receive(_ context.Context) ([]byte, error) {
	panic("simulated panic in transport receive")
}
func (p *panicTransport) Close() error { return nil }

func TestServerManager_WatchServer_PanicRecovery(t *testing.T) {
	// The factory returns a client backed by a panicking transport.
	// watchServer calls client.ListTools which will panic.
	// The recover() in watchServer should catch this and log it.
	panicFactory := func(ctx context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		client := mcp.NewMCPClient(&panicTransport{})
		return client, []mcp.MCPTool{}, nil
	}

	cfgs := []mcp.MCPServerConfig{{Name: "panic-server", Command: "cat"}}
	manager := mcp.NewServerManager(cfgs,
		mcp.WithClientFactory(panicFactory),
		mcp.WithRestartBackoff(10*time.Millisecond, 30*time.Millisecond),
	)
	reg := tools.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	manager.StartAll(ctx, reg)

	// Give watchServer time to run and hit the panic path.
	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	// StopAll must not deadlock or panic.
	done := make(chan struct{})
	go func() {
		manager.StopAll(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StopAll timed out after watchServer panic")
	}
}

// ---------------------------------------------------------------------------
// Concurrent MCPClient calls (race detector)
// Multiple goroutines call ListTools and CallTool simultaneously to verify
// the atomic ID increment and transport locking are race-free.
// ---------------------------------------------------------------------------

func TestMCPClient_ConcurrentCalls_RaceFree(t *testing.T) {
	// Build enough responses for N concurrent ListTools calls.
	const goroutines = 5
	responses := make([][]byte, goroutines+1) // +1 for init
	responses[0] = buildInitResponse(1)
	for i := 1; i <= goroutines; i++ {
		responses[i] = buildToolsListResponse(i+1, []map[string]any{})
	}

	tr := &MockTransport{toSend: responses}
	c := mcp.NewMCPClient(tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// ListTools may fail if responses are exhausted; that's OK for race testing.
			_, _ = c.ListTools(context.Background())
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// MCPToolAdapter.Execute — all-non-text content (empty output)
// When all content items are non-text (e.g. image/resource), the output should
// be empty string, not an error.
// ---------------------------------------------------------------------------

func TestMCPToolAdapter_Execute_AllNonTextContent(t *testing.T) {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "image", "text": ""},
				{"type": "resource", "text": ""},
			},
			"isError": false,
		},
	})
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1), resp},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tool := mcp.MCPTool{Name: "img_tool", InputSchema: mcp.MCPInputSchema{Type: "object"}}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	result := adapter.Execute(context.Background(), nil)
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Error)
	}
	if result.Output != "" {
		t.Errorf("expected empty output for all-non-text content, got: %q", result.Output)
	}
}

// ---------------------------------------------------------------------------
// MCPToolAdapter.Execute — mixed text/empty text content
// Items with type="text" but text="" should be filtered out (empty string
// does not contribute a part).
// ---------------------------------------------------------------------------

func TestMCPToolAdapter_Execute_EmptyTextFiltered(t *testing.T) {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": ""},       // empty text, should be filtered
				{"type": "text", "text": "line 1"}, // kept
				{"type": "text", "text": ""},       // empty text, should be filtered
			},
			"isError": false,
		},
	})
	tr := &MockTransport{
		toSend: [][]byte{buildInitResponse(1), resp},
	}
	c := mcp.NewMCPClient(tr)
	c.Initialize(context.Background())

	tool := mcp.MCPTool{Name: "filter_tool", InputSchema: mcp.MCPInputSchema{Type: "object"}}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	result := adapter.Execute(context.Background(), nil)
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Error)
	}
	if result.Output != "line 1" {
		t.Errorf("expected 'line 1', got: %q", result.Output)
	}
}

// ---------------------------------------------------------------------------
// StopAll is idempotent on an unstarted manager (no clients added)
// ---------------------------------------------------------------------------

func TestServerManager_StopAll_Unstarted(t *testing.T) {
	manager := mcp.NewServerManager([]mcp.MCPServerConfig{})
	// StopAll on a manager that never had StartAll called must not panic.
	manager.StopAll(context.Background())
	manager.StopAll(context.Background()) // second call must also be safe
}

// ---------------------------------------------------------------------------
// MCPToolAdapter.Schema — with empty properties (no required fields)
// ---------------------------------------------------------------------------

func TestMCPToolAdapter_Schema_EmptyProperties(t *testing.T) {
	tr := &MockTransport{toSend: [][]byte{}}
	c := mcp.NewMCPClient(tr)
	tool := mcp.MCPTool{
		Name:        "no_params_tool",
		Description: "A tool with no parameters",
		InputSchema: mcp.MCPInputSchema{
			Type:       "object",
			Properties: nil,
			Required:   nil,
		},
	}
	adapter := mcp.NewMCPToolAdapter(c, tool)

	schema := adapter.Schema()
	if schema.Function.Name != "no_params_tool" {
		t.Errorf("Name = %q", schema.Function.Name)
	}
	if len(schema.Function.Parameters.Properties) != 0 {
		t.Errorf("expected 0 properties, got %d", len(schema.Function.Parameters.Properties))
	}
	if len(schema.Function.Parameters.Required) != 0 {
		t.Errorf("expected 0 required, got %d", len(schema.Function.Parameters.Required))
	}
}

// ---------------------------------------------------------------------------
// Multiple servers in StartAll — all registered
// ---------------------------------------------------------------------------

func TestServerManager_StartAll_MultipleServers_AllFail(t *testing.T) {
	// All servers fail at factory time; none should panic.
	failFactory := func(ctx context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		return nil, nil, fmt.Errorf("server %q unavailable", cfg.Name)
	}

	cfgs := []mcp.MCPServerConfig{
		{Name: "srv1", Command: "nonexistent"},
		{Name: "srv2", Command: "nonexistent"},
		{Name: "srv3", Command: "nonexistent"},
	}
	manager := mcp.NewServerManager(cfgs, mcp.WithClientFactory(failFactory))
	reg := tools.NewRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	manager.StartAll(ctx, reg)

	// No tools should be registered.
	list := reg.All()
	if len(list) != 0 {
		t.Errorf("expected 0 registered tools, got %d", len(list))
	}

	manager.StopAll(context.Background())
}

// ---------------------------------------------------------------------------
// MCPServerConfig serialization
// ---------------------------------------------------------------------------

func TestMCPServerConfig_JSONRoundtrip(t *testing.T) {
	cfg := mcp.MCPServerConfig{
		Name:      "test-server",
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-everything"},
		Transport: "stdio",
		URL:       "",
		Env:       []string{"NODE_ENV=production"},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var cfg2 mcp.MCPServerConfig
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg2.Name != cfg.Name {
		t.Errorf("Name: got %q, want %q", cfg2.Name, cfg.Name)
	}
	if cfg2.Command != cfg.Command {
		t.Errorf("Command: got %q, want %q", cfg2.Command, cfg.Command)
	}
	if len(cfg2.Args) != len(cfg.Args) {
		t.Errorf("Args: got %v, want %v", cfg2.Args, cfg.Args)
	}
	if cfg2.Transport != cfg.Transport {
		t.Errorf("Transport: got %q, want %q", cfg2.Transport, cfg.Transport)
	}
	if len(cfg2.Env) != len(cfg.Env) || cfg2.Env[0] != cfg.Env[0] {
		t.Errorf("Env: got %v, want %v", cfg2.Env, cfg.Env)
	}
}
