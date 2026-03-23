package mcp_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/mcp"
)

// idEchoTransport parses each sent request to extract the JSON-RPC ID, then
// returns a pre-built response template with that ID substituted. This ensures
// the MCPClient's ID-match check always passes without hardcoding IDs.
type idEchoTransport struct {
	lastID      atomic.Int64
	makeResp    func(id int) []byte
	initDone    bool
	initResp    func(id int) []byte
	notifIgnore bool // true after init, to skip the "initialized" notification
}

func (t *idEchoTransport) Send(_ context.Context, msg []byte) error {
	var req struct {
		ID     *int   `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(msg, &req); err == nil && req.ID != nil {
		t.lastID.Store(int64(*req.ID))
	}
	return nil
}

func (t *idEchoTransport) Receive(_ context.Context) ([]byte, error) {
	id := int(t.lastID.Load())
	if !t.initDone {
		t.initDone = true
		return t.initResp(id), nil
	}
	return t.makeResp(id), nil
}

func (t *idEchoTransport) Close() error { return nil }

func makeInitResp(id int) []byte {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "bench", "version": "1.0"},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func makeListToolsResp(id, toolCount int) []byte {
	tools := make([]map[string]any, toolCount)
	for i := 0; i < toolCount; i++ {
		tools[i] = map[string]any{
			"name":        "tool_" + string(rune('a'+i%26)),
			"description": "Benchmark tool",
			"inputSchema": map[string]any{"type": "object"},
		}
	}
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"tools": tools,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func makeCallToolResp(id int) []byte {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "result from tool"},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// BenchmarkMCPClientListTools benchmarks the ListTools RPC round-trip using
// a mock transport that echoes back the request ID.
func BenchmarkMCPClientListTools(b *testing.B) {
	tr := &idEchoTransport{
		initResp: makeInitResp,
		makeResp: func(id int) []byte { return makeListToolsResp(id, 10) },
	}
	client := mcp.NewMCPClient(tr)
	if err := client.Initialize(context.Background()); err != nil {
		b.Fatalf("initialize: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.ListTools(context.Background())
		if err != nil {
			b.Fatalf("ListTools: %v", err)
		}
	}
}

// BenchmarkMCPClientCallTool benchmarks the CallTool RPC round-trip.
func BenchmarkMCPClientCallTool(b *testing.B) {
	tr := &idEchoTransport{
		initResp: makeInitResp,
		makeResp: func(id int) []byte { return makeCallToolResp(id) },
	}
	client := mcp.NewMCPClient(tr)
	if err := client.Initialize(context.Background()); err != nil {
		b.Fatalf("initialize: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.CallTool(context.Background(), "test_tool", map[string]any{"key": "value"})
		if err != nil {
			b.Fatalf("CallTool: %v", err)
		}
	}
}
