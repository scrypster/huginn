package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPTransport_ListTools_AfterInitialize verifies that calling ListTools
// via an MCPClient backed by HTTPTransport works correctly after Initialize.
//
// Regression: sendRequest signals recvLoop's cond BEFORE calling transport.Send,
// so recvLoop can call transport.Receive() before the HTTP response is available
// in t.pending, producing "no pending response (call Send first)".
//
// This test reproduces the race by calling ListTools immediately after Initialize.
func TestHTTPTransport_ListTools_AfterInitialize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"result": map[string]any{
					"tools": []map[string]any{
						{"name": "test_tool", "description": "A test tool", "inputSchema": map[string]any{"type": "object"}},
					},
				},
			})
		default:
			http.Error(w, "unknown method", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, "test-token")
	c := NewMCPClient(tr)

	ctx := context.Background()

	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v — this is the race condition bug", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "test_tool" {
		t.Errorf("tool name: got %q, want test_tool", tools[0].Name)
	}
}

// TestHTTPTransport_BlockingReceive verifies that Receive blocks until Send
// provides a response, rather than immediately returning an error.
// This is required for recvLoop compatibility (recvLoop may call Receive before Send).
func TestHTTPTransport_BlockingReceive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": "from-blocking"})
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, "")

	// Start Receive in a goroutine BEFORE Send — it must block.
	done := make(chan []byte, 1)
	go func() {
		b, err := tr.Receive(context.Background())
		if err != nil {
			t.Errorf("Receive: %v", err)
			close(done)
			return
		}
		done <- b
	}()

	// Send after a brief yield to ensure Receive goroutine is waiting.
	// (The test must tolerate both orderings — Send before/after Receive starts.)
	if err := tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","method":"ping","id":1}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case b := <-done:
		if len(b) == 0 {
			t.Error("expected non-empty response")
		}
	case <-context.Background().Done():
		// context.Background() never cancels — covered by test timeout
	}
}
