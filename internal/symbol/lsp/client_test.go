package lsp_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/symbol/lsp"
)

// pipeRW is a bidirectional pipe for testing client-server communication.
type pipeRW struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (p *pipeRW) Read(b []byte) (int, error)  { return p.reader.Read(b) }
func (p *pipeRW) Write(b []byte) (int, error) { return p.writer.Write(b) }

func newPipe() (*pipeRW, *pipeRW) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	return &pipeRW{reader: r1, writer: w2}, &pipeRW{reader: r2, writer: w1}
}

func TestClient_Initialize(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	// Mock server: respond to initialize request
	go func() {
		var req map[string]any
		tr := lsp.NewTransport(serverPipe)
		if err := tr.Receive(&req); err != nil {
			t.Errorf("server receive: %v", err)
			return
		}
		if method, ok := req["method"].(string); !ok || method != "initialize" {
			t.Errorf("expected initialize method")
		}

		// Send initialize response
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]any{
				"capabilities": map[string]any{},
			},
		}
		if err := tr.Send(resp); err != nil {
			t.Errorf("server send initialize response: %v", err)
			return
		}

		// Receive initialized notification
		var notif map[string]any
		if err := tr.Receive(&notif); err != nil {
			t.Errorf("server receive initialized: %v", err)
			return
		}
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
}

func TestClient_TextDocumentDefinition(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)

		// Receive initialize
		var initReq map[string]any
		if err := tr.Receive(&initReq); err != nil {
			t.Errorf("server receive init: %v", err)
			return
		}
		initResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"result": map[string]any{
				"capabilities": map[string]any{},
			},
		}
		if err := tr.Send(initResp); err != nil {
			t.Errorf("server send init: %v", err)
			return
		}

		// Receive initialized notification
		var notif map[string]any
		if err := tr.Receive(&notif); err != nil {
			t.Errorf("server receive initialized: %v", err)
			return
		}

		// Receive definition request
		var defReq map[string]any
		if err := tr.Receive(&defReq); err != nil {
			t.Errorf("server receive definition: %v", err)
			return
		}
		if method, ok := defReq["method"].(string); !ok || method != "textDocument/definition" {
			t.Errorf("expected textDocument/definition")
		}

		// Send definition response
		defResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      defReq["id"],
			"result": []map[string]any{
				{
					"uri": "file:///project/main.go",
					"range": map[string]any{
						"start": map[string]int{"line": 9, "character": 0},
						"end":   map[string]int{"line": 9, "character": 10},
					},
				},
			},
		}
		if err := tr.Send(defResp); err != nil {
			t.Errorf("server send definition: %v", err)
		}
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	locs, err := client.TextDocumentDefinition("file:///project/main.go", 42, 15)
	if err != nil {
		t.Fatalf("TextDocumentDefinition: %v", err)
	}
	if len(locs) == 0 {
		t.Errorf("expected at least one location")
	}
	if len(locs) > 0 && locs[0].URI != "file:///project/main.go" {
		t.Errorf("expected main.go in URI")
	}
}

func TestClient_WorkspaceSymbol(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)

		// Receive initialize
		var initReq map[string]any
		if err := tr.Receive(&initReq); err != nil {
			t.Errorf("server receive init: %v", err)
			return
		}
		initResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"result": map[string]any{
				"capabilities": map[string]any{},
			},
		}
		if err := tr.Send(initResp); err != nil {
			t.Errorf("server send init: %v", err)
			return
		}

		// Receive initialized notification
		var notif map[string]any
		if err := tr.Receive(&notif); err != nil {
			t.Errorf("server receive initialized: %v", err)
			return
		}

		// Receive symbol request
		var symReq map[string]any
		if err := tr.Receive(&symReq); err != nil {
			t.Errorf("server receive symbol: %v", err)
			return
		}
		if method, ok := symReq["method"].(string); !ok || method != "workspace/symbol" {
			t.Errorf("expected workspace/symbol")
		}

		// Send symbol response
		symResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      symReq["id"],
			"result": []map[string]any{
				{
					"name": "Handler",
					"kind": 12,
					"location": map[string]any{
						"uri": "file:///project/server.go",
						"range": map[string]any{
							"start": map[string]int{"line": 20, "character": 0},
							"end":   map[string]int{"line": 20, "character": 7},
						},
					},
				},
			},
		}
		if err := tr.Send(symResp); err != nil {
			t.Errorf("server send symbol: %v", err)
		}
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	syms, err := client.WorkspaceSymbol("Handler")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	if len(syms) == 0 {
		t.Errorf("expected at least one symbol")
	}
	if len(syms) > 0 && syms[0].Name != "Handler" {
		t.Errorf("expected Handler symbol")
	}
}

func TestClient_NotInitialized_Error(t *testing.T) {
	buf := &bytes.Buffer{}
	rw := &mockRW{r: strings.NewReader(""), w: buf}
	client := lsp.NewClient(rw, "go")

	_, err := client.TextDocumentDefinition("file:///test.go", 1, 1)
	if err == nil {
		t.Errorf("expected error when not initialized")
	}
}

func TestClient_TextDocumentDefinition_SingleLocation(t *testing.T) {
	// Test that single location response is properly wrapped in a slice
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)

		// Receive and respond to initialize
		var initReq map[string]any
		tr.Receive(&initReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"result": map[string]any{
				"capabilities": map[string]any{},
			},
		})

		// Receive initialized
		var notif map[string]any
		tr.Receive(&notif)

		// Receive definition request and respond with single location (not array)
		var defReq map[string]any
		tr.Receive(&defReq)

		defResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      defReq["id"],
			"result": map[string]any{
				"uri": "file:///project/main.go",
				"range": map[string]any{
					"start": map[string]int{"line": 10, "character": 0},
					"end":   map[string]int{"line": 10, "character": 5},
				},
			},
		}
		tr.Send(defResp)
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	locs, err := client.TextDocumentDefinition("file:///project/main.go", 42, 15)
	if err != nil {
		t.Fatalf("TextDocumentDefinition: %v", err)
	}
	if len(locs) != 1 {
		t.Errorf("expected exactly 1 location, got %d", len(locs))
	}
	if locs[0].URI != "file:///project/main.go" {
		t.Errorf("expected main.go")
	}
}
