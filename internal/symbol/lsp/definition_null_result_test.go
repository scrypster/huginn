package lsp_test

// coverage_boost95_test.go — targeted tests to push lsp package to 95%+.

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/symbol/lsp"
)

// TestClient_TextDocumentDefinition_NullResult95 exercises the resp.Result == nil
// branch in TextDocumentDefinition (returns nil, nil when result is JSON null).
func TestClient_TextDocumentDefinition_NullResult95(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)
		// Handle initialize.
		var initReq map[string]any
		if err := tr.Receive(&initReq); err != nil {
			return
		}
		_ = tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"result":  map[string]any{"capabilities": map[string]any{}},
		})
		var notif map[string]any
		_ = tr.Receive(&notif)

		// Handle definition request; respond with null result.
		var defReq map[string]any
		_ = tr.Receive(&defReq)
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":null}`, defReq["id"])
		raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
		serverPipe.Write([]byte(raw))
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	locs, err := client.TextDocumentDefinition("file:///test.go", 1, 1)
	if err != nil {
		t.Errorf("unexpected error with null result: %v", err)
	}
	if locs != nil {
		t.Errorf("expected nil locs for null result, got %v", locs)
	}
}

// TestClient_WorkspaceSymbol_NullResult95 exercises the resp.Result == nil branch
// in WorkspaceSymbol (returns nil, nil when result is JSON null).
func TestClient_WorkspaceSymbol_NullResult95(t *testing.T) {
	clientPipe, serverPipe := newPipe()
	client := lsp.NewClient(clientPipe, "go")

	go func() {
		tr := lsp.NewTransport(serverPipe)
		var initReq map[string]any
		if err := tr.Receive(&initReq); err != nil {
			return
		}
		_ = tr.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq["id"],
			"result":  map[string]any{"capabilities": map[string]any{}},
		})
		var notif map[string]any
		_ = tr.Receive(&notif)

		var symReq map[string]any
		_ = tr.Receive(&symReq)
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":null}`, symReq["id"])
		raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
		serverPipe.Write([]byte(raw))
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	syms, err := client.WorkspaceSymbol("test")
	if err != nil {
		t.Errorf("unexpected error with null result: %v", err)
	}
	if syms != nil {
		t.Errorf("expected nil syms for null result, got %v", syms)
	}
}

// TestTransport_Send_MarshalError exercises the json.Marshal error path in Send.
// We pass a value that cannot be marshaled to JSON (a channel).
func TestTransport_Send_MarshalError(t *testing.T) {
	rw := &mockRW{r: strings.NewReader(""), w: &bytes.Buffer{}}
	tr := lsp.NewTransport(rw)
	// A channel is not JSON-serializable and will cause json.Marshal to error.
	ch := make(chan int)
	err := tr.Send(ch)
	if err == nil {
		t.Error("expected error marshaling channel")
	}
	if !strings.Contains(err.Error(), "lsp send:") {
		t.Errorf("expected 'lsp send:' prefix, got: %v", err)
	}
}

// TestManager_Start_AlreadyInitialized_IsIdempotent exercises the
// `m.client != nil && m.client.initDone` guard in Start (returns nil early).
// We test this by starting a manager against a real command that we control
// via a pipe. We'll use a goroutine as the "server" to handle initialization.
// Since we can't inject a custom RW into the Manager (it uses exec.Command),
// we test the closest equivalent: the "ErrNotConfigured" path (cmd == "").
func TestManager_Start_ErrNotConfigured(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{})
	err := mgr.Start("/tmp")
	if err != lsp.ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got: %v", err)
	}
}

// TestManager_Definition_NotConfigured covers Manager.Definition when cfg.Command == "".
func TestManager_Definition_NotConfigured(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{})
	_, err := mgr.Definition("file:///test.go", 1, 1)
	if err != lsp.ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got: %v", err)
	}
}

// TestManager_Symbols_NotConfigured95 covers Manager.Symbols when cfg.Command == "".
func TestManager_Symbols_NotConfigured95(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{})
	_, err := mgr.Symbols("test")
	if err != lsp.ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got: %v", err)
	}
}
