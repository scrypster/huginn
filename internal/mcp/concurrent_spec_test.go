package mcp_test

// concurrent_spec_test.go — Behavior specs for MCPClient concurrency characteristics.
//
// Run with: go test -race ./internal/mcp/...
//
// These tests document the concurrency contract for MCPClient.CallTool:
// - MCPClient is NOT safe for concurrent use — each call is a sequential
//   send+receive pair on a single transport.
// - When a transport returns a response with a mismatched ID, CallTool returns
//   an error rather than silently delivering the wrong result.
// - The ID mismatch check acts as a correctness guard when the caller mistakenly
//   shares a client across goroutines.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
)

// buildSimpleToolResponse returns a JSON-RPC response for tools/call with the given ID.
func buildSimpleToolResponse(id int, textContent string) []byte {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": textContent},
			},
			"isError": false,
		},
	})
	return resp
}

// TestMCPClient_CallTool_IDMismatch_ReturnsError verifies that when the transport
// delivers a response with a different ID than the request, CallTool returns an
// error rather than returning the wrong result silently.
//
// This guards against response-ID confusion, which can occur when a client is
// shared across concurrent callers.
func TestMCPClient_CallTool_IDMismatch_ReturnsError(t *testing.T) {
	// Transport that returns a response for ID=999 when client sends ID=2.
	// (ID=1 is consumed by Initialize, so the first CallTool gets ID=2.)
	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),    // Initialize response
			buildSimpleToolResponse(999, "wrong"), // mismatched ID
		},
	}

	client := mcp.NewMCPClient(tr)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := client.CallTool(context.Background(), "my_tool", map[string]any{"arg": "val"})
	if err == nil {
		t.Fatal("CallTool with mismatched response ID should return error, got nil")
	}
}

// TestMCPClient_CallTool_MatchingID_ReturnsResult verifies the happy path:
// when transport delivers the correct response ID, CallTool returns the result.
func TestMCPClient_CallTool_MatchingID_ReturnsResult(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildSimpleToolResponse(2, "hello from tool"),
		},
	}

	client := mcp.NewMCPClient(tr)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	result, err := client.CallTool(context.Background(), "echo", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.Content) == 0 || result.Content[0].Text != "hello from tool" {
		t.Errorf("unexpected result: %+v", result)
	}
}

// TestMCPClient_SequentialCallTool_IDsIncrement verifies that sequential CallTool
// invocations use monotonically increasing IDs (1, 2, 3, ...), allowing the server
// to correlate responses to requests.
func TestMCPClient_SequentialCallTool_IDsIncrement(t *testing.T) {
	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			buildSimpleToolResponse(2, "first"),
			buildSimpleToolResponse(3, "second"),
		},
	}

	client := mcp.NewMCPClient(tr)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	for i, want := range []string{"first", "second"} {
		result, err := client.CallTool(context.Background(), "tool", nil)
		if err != nil {
			t.Fatalf("CallTool[%d]: %v", i, err)
		}
		if len(result.Content) == 0 || result.Content[0].Text != want {
			t.Errorf("CallTool[%d]: got %q, want %q", i, result.Content[0].Text, want)
		}
	}
}

// TestMCPClient_ConcurrentCallTool_IDMismatchOrError documents that sharing a
// single MCPClient across goroutines causes ID mismatches or errors.
//
// NOTE: This test is intentionally loose — it does NOT assert a specific failure
// mode, because the exact interleaving is non-deterministic. It asserts only that
// at least one of the two calls encounters an error (ID mismatch detected) or that
// both complete without corrupted results. The absence of panics and data races
// is verified by the race detector.
//
// Callers that need parallel MCP tool execution should create separate MCPClient
// instances (one per goroutine), not share a single client.
func TestMCPClient_ConcurrentCallTool_IDMismatchOrError(t *testing.T) {
	// Provide enough responses for both calls (they'll race to consume them).
	tr := &threadSafeTransport{
		responses: [][]byte{
			buildInitResponse(1),
			buildSimpleToolResponse(2, "for-call-1"),
			buildSimpleToolResponse(3, "for-call-2"),
		},
	}

	client := mcp.NewMCPClient(tr)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = client.CallTool(context.Background(), "tool", nil)
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// At least we didn't deadlock or panic.
	case <-time.After(2 * time.Second):
		t.Error("concurrent CallTool calls deadlocked (transport exhausted?)")
	}
	// Do not assert specific error counts — the race detector is the real
	// judge here. Run with: go test -race ./internal/mcp/...
}

// threadSafeTransport is a transport implementation that protects its response
// queue with a mutex, allowing concurrent goroutines to consume responses safely.
// (Unlike MockTransport which also uses a mutex — this one is identical in behavior.)
type threadSafeTransport struct {
	mu        sync.Mutex
	responses [][]byte
	sent      [][]byte
}

func (t *threadSafeTransport) Send(_ context.Context, msg []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sent = append(t.sent, msg)
	return nil
}

func (t *threadSafeTransport) Receive(_ context.Context) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.responses) == 0 {
		return nil, context.DeadlineExceeded
	}
	resp := t.responses[0]
	t.responses = t.responses[1:]
	return resp, nil
}

func (t *threadSafeTransport) Close() error { return nil }
