package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
)

// TestMCPClient_ConcurrentIndependentClients verifies that many independent
// MCPClient instances can each call CallTool concurrently without interfering
// with each other. Each client has its own transport.
func TestMCPClient_ConcurrentIndependentClients(t *testing.T) {
	const n = 20
	var wg sync.WaitGroup
	var errCount int64

	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()

			toolResult := map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": fmt.Sprintf("result-%d", i)},
					},
					"isError": false,
				},
			}
			toolResultData, _ := json.Marshal(toolResult)

			tr := &MockTransport{
				toSend: [][]byte{
					buildInitResponse(1),
					toolResultData,
				},
			}
			c := mcp.NewMCPClient(tr)
			if err := c.Initialize(context.Background()); err != nil {
				atomic.AddInt64(&errCount, 1)
				t.Logf("goroutine %d Initialize: %v", i, err)
				return
			}
			result, err := c.CallTool(context.Background(), "tool", map[string]any{"n": i})
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				t.Logf("goroutine %d CallTool: %v", i, err)
				return
			}
			want := fmt.Sprintf("result-%d", i)
			if len(result.Content) == 0 || result.Content[0].Text != want {
				atomic.AddInt64(&errCount, 1)
				t.Logf("goroutine %d: got %v, want text=%q", i, result.Content, want)
			}
		}()
	}

	wg.Wait()
	if errCount > 0 {
		t.Errorf("%d goroutines reported errors (see log output above)", errCount)
	}
}

// TestMCPClient_FailedCallDoesNotAffectNext verifies that a CallTool error
// on one call does not prevent the client from making a successful subsequent call.
func TestMCPClient_FailedCallDoesNotAffectNext(t *testing.T) {
	successResult := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "after-failure"},
			},
			"isError": false,
		},
	}
	successData, _ := json.Marshal(successResult)

	// Queue: init response, then an RPC error, then a success.
	errorResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"error":   map[string]any{"code": -32000, "message": "first call failed"},
	}
	errorData, _ := json.Marshal(errorResp)

	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			errorData,
			successData,
		},
	}
	c := mcp.NewMCPClient(tr)

	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// First call should fail with an RPC error.
	_, err := c.CallTool(context.Background(), "first_tool", nil)
	if err == nil {
		t.Fatal("expected error on first call")
	}

	// Second call should succeed.
	result, err := c.CallTool(context.Background(), "second_tool", nil)
	if err != nil {
		t.Fatalf("expected success on second call, got: %v", err)
	}
	if len(result.Content) == 0 || result.Content[0].Text != "after-failure" {
		t.Errorf("unexpected result: %+v", result)
	}
}

// TestMCPClient_IDsMonotonicallyIncrease verifies that message IDs increment
// across multiple sequential calls on the same client.
func TestMCPClient_IDsMonotonicallyIncrease(t *testing.T) {
	listResp1 := buildToolsListResponse(2, []map[string]any{})
	listResp2 := buildToolsListResponse(3, []map[string]any{})
	listResp3 := buildToolsListResponse(4, []map[string]any{})

	tr := &MockTransport{
		toSend: [][]byte{
			buildInitResponse(1),
			listResp1,
			listResp2,
			listResp3,
		},
	}
	c := mcp.NewMCPClient(tr)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := c.ListTools(context.Background()); err != nil {
			t.Fatalf("ListTools call %d: %v", i, err)
		}
	}

	tr.mu.Lock()
	received := tr.received
	tr.mu.Unlock()

	// received[0] = initialize, received[1] = notifications/initialized, received[2..4] = ListTools
	prevID := -1
	for idx, msg := range received {
		var req map[string]any
		if err := json.Unmarshal(msg, &req); err != nil {
			continue
		}
		rawID, ok := req["id"]
		if !ok {
			continue // notification (no id)
		}
		// JSON numbers unmarshal as float64.
		id := int(rawID.(float64))
		if prevID >= 0 && id <= prevID {
			t.Errorf("message %d: id=%d not greater than previous id=%d", idx, id, prevID)
		}
		prevID = id
	}
}

// TestMCPClient_MultipleToolCallsSequential verifies that sequential CallTool
// calls on the same client each get the correct response matched by ID.
func TestMCPClient_MultipleToolCallsSequential(t *testing.T) {
	results := []string{"alpha", "beta", "gamma"}

	var responses [][]byte
	responses = append(responses, buildInitResponse(1))
	for i, text := range results {
		id := i + 2 // IDs start at 2 after initialize=1
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
				"isError": false,
			},
		}
		data, _ := json.Marshal(resp)
		responses = append(responses, data)
	}

	tr := &MockTransport{toSend: responses}
	c := mcp.NewMCPClient(tr)

	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	for i, want := range results {
		result, err := c.CallTool(context.Background(), fmt.Sprintf("tool_%d", i), nil)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if len(result.Content) == 0 || result.Content[0].Text != want {
			t.Errorf("call %d: got %v, want text=%q", i, result.Content, want)
		}
	}
}
