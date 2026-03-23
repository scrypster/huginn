package mcp_test

// client_concurrent_demux_test.go — Tests for MCPClient request demultiplexer.
//
// Verifies that concurrent CallTool invocations on a single MCPClient each
// receive the correct response matched by ID, even when responses arrive in a
// different order than requests were sent.

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

// orderedTransport is a Transport that serves pre-queued responses.
// Unlike MockTransport it supports concurrent Send/Receive safely
// (no Mutex needed for Send since we don't track sent messages).
type orderedTransport struct {
	mu        sync.Mutex
	responses [][]byte
	done      chan struct{}
}

func (t *orderedTransport) Send(_ context.Context, _ []byte) error {
	return nil
}

func (t *orderedTransport) Receive(_ context.Context) ([]byte, error) {
	for {
		t.mu.Lock()
		if len(t.responses) > 0 {
			resp := t.responses[0]
			t.responses = t.responses[1:]
			t.mu.Unlock()
			return resp, nil
		}
		t.mu.Unlock()
		// Poll — in real usage the transport blocks on I/O.
		select {
		case <-t.done:
			return nil, fmt.Errorf("transport closed")
		case <-time.After(1 * time.Millisecond):
		}
	}
}

func (t *orderedTransport) Close() error {
	select {
	case <-t.done:
	default:
		close(t.done)
	}
	return nil
}

func (t *orderedTransport) push(resp []byte) {
	t.mu.Lock()
	t.responses = append(t.responses, resp)
	t.mu.Unlock()
}

func buildConcurrentToolResponse(id int, text string) []byte {
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
			"isError": false,
		},
	})
	return resp
}

// TestMCPClient_ConcurrentCallTool_DemuxByID verifies that N goroutines can
// call CallTool concurrently on a single MCPClient and each receives the
// response that matches its request ID — even when responses arrive out of order.
//
// Correctness guarantee: no goroutine should receive a response intended for a
// different request, and no response should be lost.
func TestMCPClient_ConcurrentCallTool_DemuxByID(t *testing.T) {
	const n = 10

	tr := &orderedTransport{done: make(chan struct{})}
	defer tr.Close()

	c := mcp.NewMCPClient(tr)

	// Build the expected response set keyed by ID.
	// IDs start at 1 (since nextID begins at 0 and is pre-incremented).
	expectedByID := make(map[int]string, n)
	for i := 1; i <= n; i++ {
		text := fmt.Sprintf("result-for-id-%d", i)
		expectedByID[i] = text
		tr.push(buildConcurrentToolResponse(i, text))
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		gotTexts []string
		errs    []error
	)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := c.CallTool(ctx, fmt.Sprintf("tool_%d", idx), nil)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("goroutine %d: %w", idx, err))
				return
			}
			if len(result.Content) == 0 {
				errs = append(errs, fmt.Errorf("goroutine %d: empty content", idx))
				return
			}
			gotTexts = append(gotTexts, result.Content[0].Text)
		}(i)
	}

	wg.Wait()

	if len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
		t.FailNow()
	}

	if len(gotTexts) != n {
		t.Fatalf("expected %d results, got %d", n, len(gotTexts))
	}

	// Every expected text must appear exactly once (the goroutine→ID mapping is
	// non-deterministic, but the full set must be covered with no duplicates).
	seen := make(map[string]int, n)
	for _, text := range gotTexts {
		seen[text]++
	}
	for _, expectedText := range expectedByID {
		count := seen[expectedText]
		if count != 1 {
			t.Errorf("expected text %q to appear exactly once, got count=%d", expectedText, count)
		}
	}
}

// TestMCPClient_RecvLoopTerminatesOnTransportError verifies that when the
// transport returns an error, all pending callers are unblocked with an error.
func TestMCPClient_RecvLoopTerminatesOnTransportError(t *testing.T) {
	tr := &orderedTransport{done: make(chan struct{})}

	c := mcp.NewMCPClient(tr)

	// Start a CallTool — it will block waiting for a response.
	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := c.CallTool(ctx, "blocked_tool", nil)
		errCh <- err
	}()

	// Give the goroutine time to register its pending entry and start waiting.
	time.Sleep(30 * time.Millisecond)

	// Close the transport — recvLoop will get an error and close all pending channels.
	tr.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error when transport is closed, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout: CallTool did not unblock after transport closed")
	}
}

// TestMCPClient_SequentialCallsNeverReadAhead verifies that recvLoop does not
// consume a response for call N+1 before call N+1 has registered its pending entry.
// This is the core correctness guarantee of the cond-variable–based wait design.
func TestMCPClient_SequentialCallsNeverReadAhead(t *testing.T) {
	const count = 5
	tr := &orderedTransport{done: make(chan struct{})}
	defer tr.Close()

	// Push all responses upfront; recvLoop must only read each one after the
	// corresponding pending entry is registered.
	for i := 1; i <= count; i++ {
		tr.push(buildConcurrentToolResponse(i, fmt.Sprintf("val-%d", i)))
	}

	c := mcp.NewMCPClient(tr)

	for i := 0; i < count; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		result, err := c.CallTool(ctx, "tool", nil)
		cancel()
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if len(result.Content) == 0 {
			t.Fatalf("call %d: empty content", i)
		}
		want := fmt.Sprintf("val-%d", i+1)
		if result.Content[0].Text != want {
			t.Errorf("call %d: got %q, want %q", i, result.Content[0].Text, want)
		}
	}
}

// TestMCPClient_ContextCancellation verifies that cancelling the context while
// waiting for a response unblocks the caller and the pending entry is cleaned up.
func TestMCPClient_ContextCancellation(t *testing.T) {
	// Use a transport that never produces a response.
	tr := &orderedTransport{done: make(chan struct{})}
	defer tr.Close()

	c := mcp.NewMCPClient(tr)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.CallTool(ctx, "no_response_tool", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if ctx.Err() == nil {
		t.Error("expected context to be done")
	}
}

// TestMCPClient_ValidateToolSchema_FiltersInvalidTools verifies that registerServerTools
// (invoked inside StartAll) skips malformed tools without panicking, and only
// registers well-formed tools.
func TestMCPClient_ValidateToolSchema_FiltersInvalidTools(t *testing.T) {
	validTools := []mcp.MCPTool{
		{Name: "valid_tool", Description: "ok", InputSchema: mcp.MCPInputSchema{Type: "object"}},
		{Name: "", Description: "no name"},                                                        // invalid: empty name → skipped
		{Name: "bad name!", Description: "spaces"},                                                // invalid: bad characters → skipped
		{Name: "another-valid", Description: "also ok", InputSchema: mcp.MCPInputSchema{Type: "object"}},
	}

	factory := func(_ context.Context, cfg mcp.MCPServerConfig) (*mcp.MCPClient, []mcp.MCPTool, error) {
		tr := &orderedTransport{done: make(chan struct{})}
		// The client will never be used for tool calls in this test.
		go func() {
			<-tr.done
		}()
		client := mcp.NewMCPClient(tr)
		return client, validTools, nil
	}

	manager := mcp.NewServerManager(
		[]mcp.MCPServerConfig{{Name: "test-server", Command: "dummy"}},
		mcp.WithClientFactory(factory),
	)

	reg := tools.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Must not panic. Invalid tools are skipped with slog.Warn.
	manager.StartAll(ctx, reg)

	// valid_tool and another-valid should be registered; bad tools must be absent.
	if _, ok := reg.Get("valid_tool"); !ok {
		t.Error("expected valid_tool to be registered")
	}
	if _, ok := reg.Get("another-valid"); !ok {
		t.Error("expected another-valid to be registered")
	}
	if _, ok := reg.Get(""); ok {
		t.Error("empty-name tool should NOT be registered")
	}
	if _, ok := reg.Get("bad name!"); ok {
		t.Error("bad-chars tool should NOT be registered")
	}

	manager.StopAll(ctx)
}
