package lsp_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/symbol/lsp"
)

func TestManager_Start_NotConfigured(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{})
	err := mgr.Start("/tmp/test-project")
	if err != lsp.ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestManager_Start_BadCommand(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{Command: "/nonexistent/binary-xyz"})
	err := mgr.Start("/tmp/test-project")
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}

func TestManager_Definition_NotStarted(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{Command: "cat"})
	// client is nil because Start was never called
	_, err := mgr.Definition("file:///test.go", 1, 1)
	if err == nil {
		t.Error("expected error when not started")
	}
}

func TestManager_Symbols_NotStarted(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{Command: "cat"})
	_, err := mgr.Symbols("test")
	if err == nil {
		t.Error("expected error when not started")
	}
}

func TestClient_WorkspaceSymbol_NotInitialized(t *testing.T) {
	buf := &bytes.Buffer{}
	rw := &mockRW{r: strings.NewReader(""), w: buf}
	client := lsp.NewClient(rw, "go")

	_, err := client.WorkspaceSymbol("test")
	if err == nil {
		t.Error("expected error when not initialized")
	}
}

func TestClient_TextDocumentDefinition_NullResult(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)

		// Handle initialize
		var initReq map[string]any
		tr.Receive(&initReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"result":  map[string]any{"capabilities": map[string]any{}},
		})

		// Handle initialized notification
		var notif map[string]any
		tr.Receive(&notif)

		// Handle definition request — return null result
		var defReq map[string]any
		tr.Receive(&defReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      defReq["id"],
			"result":  nil,
		})
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	locs, err := client.TextDocumentDefinition("file:///test.go", 1, 1)
	if err != nil {
		t.Fatalf("TextDocumentDefinition: %v", err)
	}
	if locs != nil {
		t.Errorf("expected nil locations for null result, got %v", locs)
	}
}

func TestClient_WorkspaceSymbol_NullResult(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)

		var initReq map[string]any
		tr.Receive(&initReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"result":  map[string]any{"capabilities": map[string]any{}},
		})

		var notif map[string]any
		tr.Receive(&notif)

		var symReq map[string]any
		tr.Receive(&symReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      symReq["id"],
			"result":  nil,
		})
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	syms, err := client.WorkspaceSymbol("test")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	if syms != nil {
		t.Errorf("expected nil symbols for null result, got %v", syms)
	}
}

func TestClient_Initialize_RPCError(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)

		var initReq map[string]any
		tr.Receive(&initReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"error":   map[string]any{"code": -32600, "message": "invalid request"},
		})
	}()

	err := client.Initialize("file:///project")
	if err == nil {
		t.Error("expected error from RPC error response")
	}
	if !strings.Contains(err.Error(), "invalid request") {
		t.Errorf("expected 'invalid request' in error, got: %v", err)
	}
}

func TestClient_TextDocumentDefinition_RPCError(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)

		// Handle initialize successfully
		var initReq map[string]any
		tr.Receive(&initReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"result":  map[string]any{"capabilities": map[string]any{}},
		})

		var notif map[string]any
		tr.Receive(&notif)

		// Return error for definition request
		var defReq map[string]any
		tr.Receive(&defReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      defReq["id"],
			"error":   map[string]any{"code": -32000, "message": "server error"},
		})
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := client.TextDocumentDefinition("file:///test.go", 1, 1)
	if err == nil {
		t.Error("expected error from RPC error response")
	}
}

func TestClient_WorkspaceSymbol_RPCError(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)

		var initReq map[string]any
		tr.Receive(&initReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"result":  map[string]any{"capabilities": map[string]any{}},
		})

		var notif map[string]any
		tr.Receive(&notif)

		var symReq map[string]any
		tr.Receive(&symReq)
		tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      symReq["id"],
			"error":   map[string]any{"code": -32001, "message": "symbol lookup failed"},
		})
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := client.WorkspaceSymbol("test")
	if err == nil {
		t.Error("expected error from RPC error response")
	}
}
