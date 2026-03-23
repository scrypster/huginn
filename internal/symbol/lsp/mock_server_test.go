package lsp

// mock_server_test.go — integration tests using in-process pipe-based mock LSP servers.
// These tests exercise the full Client+Transport stack without requiring any real LSP
// binary to be installed.  The file lives in package lsp (white-box) so it can share
// the internalPipeRW helper already defined in manager_boost95_test.go.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock LSP server helper
// ---------------------------------------------------------------------------

// mockRequest is used by the mock server to decode incoming JSON-RPC frames.
type mockRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"` // nil for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// sendFrame writes a single LSP-framed JSON-RPC message to w.
func sendFrame(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sendFrame marshal: %w", err)
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// recvFrame reads one LSP-framed message from r and decodes it into dst.
func recvFrame(br *bufio.Reader, dst any) error {
	contentLength := -1
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return fmt.Errorf("recvFrame header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			n := 0
			if _, err := fmt.Sscanf(strings.TrimPrefix(line, "Content-Length: "), "%d", &n); err != nil {
				return fmt.Errorf("recvFrame bad Content-Length: %w", err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return fmt.Errorf("recvFrame: missing Content-Length")
	}
	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(br, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, dst)
}

// mockLSPServer runs a minimal JSON-RPC/LSP server that responds to the standard
// lifecycle and query methods used by Client.  It reads from r and writes to w.
// It exits when it encounters a read error (typically when the pipe is closed).
func mockLSPServer(r io.Reader, w io.Writer) {
	br := bufio.NewReader(r)
	for {
		var req mockRequest
		if err := recvFrame(br, &req); err != nil {
			// pipe closed or client done — stop serving
			return
		}

		// Notifications have no id field; we just acknowledge and continue.
		if req.ID == nil {
			// "initialized" and similar notifications need no response.
			continue
		}

		switch req.Method {
		case "initialize":
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"capabilities": map[string]any{
						"definitionProvider":   true,
						"workspaceSymbolProvider": true,
					},
				},
			}
			if err := sendFrame(w, resp); err != nil {
				return
			}

		case "textDocument/definition":
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": []map[string]any{
					{
						"uri": "file:///mock/project/main.go",
						"range": map[string]any{
							"start": map[string]int{"line": 4, "character": 0},
							"end":   map[string]int{"line": 4, "character": 8},
						},
					},
				},
			}
			if err := sendFrame(w, resp); err != nil {
				return
			}

		case "workspace/symbol":
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": []map[string]any{
					{
						"name": "MyFuncA",
						"kind": 12,
						"location": map[string]any{
							"uri": "file:///mock/project/a.go",
							"range": map[string]any{
								"start": map[string]int{"line": 10, "character": 0},
								"end":   map[string]int{"line": 10, "character": 7},
							},
						},
					},
					{
						"name": "MyFuncB",
						"kind": 12,
						"location": map[string]any{
							"uri": "file:///mock/project/b.go",
							"range": map[string]any{
								"start": map[string]int{"line": 20, "character": 0},
								"end":   map[string]int{"line": 20, "character": 7},
							},
						},
					},
				},
			}
			if err := sendFrame(w, resp); err != nil {
				return
			}

		default:
			// Unknown method — send a generic success so the client doesn't block.
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  nil,
			}
			if err := sendFrame(w, resp); err != nil {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Pipe wiring helper
// ---------------------------------------------------------------------------

// newMockClient creates a *Client wired to an in-process mockLSPServer goroutine.
// The caller receives the *Client ready for use.  Cleanup (pipe closure) is
// registered on t so nothing leaks after the test.
func newMockClient(t *testing.T) *Client {
	t.Helper()
	clientPipe, serverPipe := newInternalPipe()
	t.Cleanup(func() {
		clientPipe.reader.Close()
		clientPipe.writer.Close()
		serverPipe.reader.Close()
		serverPipe.writer.Close()
	})
	go mockLSPServer(serverPipe.reader, serverPipe.writer)
	return NewClient(clientPipe, "go")
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestLSPClient_Initialize_Success verifies that the full Initialize handshake
// (initialize request → response → initialized notification) completes without
// error when backed by the mock server.
func TestLSPClient_Initialize_Success(t *testing.T) {
	client := newMockClient(t)
	if err := client.Initialize("file:///mock/project"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if !client.initDone {
		t.Error("expected initDone to be true after successful Initialize")
	}
}

// TestLSPClient_TextDocumentDefinition verifies that TextDocumentDefinition
// returns the canned Location from the mock server with the expected URI and range.
func TestLSPClient_TextDocumentDefinition(t *testing.T) {
	client := newMockClient(t)
	if err := client.Initialize("file:///mock/project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	locs, err := client.TextDocumentDefinition("file:///mock/project/main.go", 10, 5)
	if err != nil {
		t.Fatalf("TextDocumentDefinition: %v", err)
	}
	if len(locs) == 0 {
		t.Fatal("expected at least one location, got none")
	}

	got := locs[0]
	if got.URI != "file:///mock/project/main.go" {
		t.Errorf("URI: want %q, got %q", "file:///mock/project/main.go", got.URI)
	}
	if got.Range.Start.Line != 4 {
		t.Errorf("Range.Start.Line: want 4, got %d", got.Range.Start.Line)
	}
	if got.Range.Start.Character != 0 {
		t.Errorf("Range.Start.Character: want 0, got %d", got.Range.Start.Character)
	}
}

// TestLSPClient_WorkspaceSymbols verifies that WorkspaceSymbol returns the two
// canned SymbolInformation entries from the mock server with the expected names.
func TestLSPClient_WorkspaceSymbols(t *testing.T) {
	client := newMockClient(t)
	if err := client.Initialize("file:///mock/project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	syms, err := client.WorkspaceSymbol("MyFunc")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(syms))
	}
	if syms[0].Name != "MyFuncA" {
		t.Errorf("syms[0].Name: want %q, got %q", "MyFuncA", syms[0].Name)
	}
	if syms[1].Name != "MyFuncB" {
		t.Errorf("syms[1].Name: want %q, got %q", "MyFuncB", syms[1].Name)
	}
}

// TestLSPClient_ContextTimeout verifies that if the server side delays its
// response and the reader side is closed (simulating a timeout/cancel), the
// client receives an error rather than blocking forever.
func TestLSPClient_ContextTimeout(t *testing.T) {
	clientPipe, serverPipe := newInternalPipe()
	t.Cleanup(func() {
		clientPipe.reader.Close()
		clientPipe.writer.Close()
		serverPipe.reader.Close()
		serverPipe.writer.Close()
	})

	// Slow server: respond to initialize normally, then delay 500ms before
	// responding to the next request.  We close the client's read side after
	// 50ms to simulate a deadline/cancellation.
	go func() {
		br := bufio.NewReader(serverPipe.reader)

		// Handle initialize.
		var initReq mockRequest
		if err := recvFrame(br, &initReq); err != nil {
			return
		}
		initResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      initReq.ID,
			"result":  map[string]any{"capabilities": map[string]any{}},
		}
		if err := sendFrame(serverPipe.writer, initResp); err != nil {
			return
		}

		// Drain initialized notification.
		var notif mockRequest
		if err := recvFrame(br, &notif); err != nil {
			return
		}

		// Drain the next request (definition/symbol) but pause before replying.
		var slowReq mockRequest
		if err := recvFrame(br, &slowReq); err != nil {
			return
		}
		// Simulate a slow backend: sleep 500ms.
		time.Sleep(500 * time.Millisecond)

		// By this point the client's read side has been closed (after 50ms),
		// so any write here will fail silently — that's fine.
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      slowReq.ID,
			"result":  nil,
		}
		_ = sendFrame(serverPipe.writer, resp)
	}()

	client := NewClient(clientPipe, "go")
	if err := client.Initialize("file:///mock/project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Cancel the "context" by closing the client's read pipe after 50ms —
	// this mimics what a context.WithTimeout would do by interrupting the
	// blocking Receive call.
	go func() {
		time.Sleep(50 * time.Millisecond)
		clientPipe.reader.Close()
	}()

	_, err := client.TextDocumentDefinition("file:///mock/project/main.go", 1, 1)
	if err == nil {
		t.Error("expected an error from the timed-out receive, got nil")
	}
}

// TestLSPClient_ConcurrentRequests verifies that 5 independent client instances
// (each with its own pipe-based mock server) all complete successfully.  The
// real Client is not safe for concurrent callers sharing a single connection
// (it is sequential: Send → Receive).  This test therefore launches 5 separate
// client+mockServer pairs concurrently and asserts that every one resolves
// without error — exercising the full stack under parallel goroutine pressure.
func TestLSPClient_ConcurrentRequests(t *testing.T) {
	const n = 5
	type result struct {
		syms []SymbolInformation
		err  error
	}
	results := make(chan result, n)

	for i := 0; i < n; i++ {
		go func() {
			clientPipe, serverPipe := newInternalPipe()
			defer func() {
				clientPipe.reader.Close()
				clientPipe.writer.Close()
				serverPipe.reader.Close()
				serverPipe.writer.Close()
			}()
			go mockLSPServer(serverPipe.reader, serverPipe.writer)
			c := NewClient(clientPipe, "go")
			if err := c.Initialize("file:///mock/project"); err != nil {
				results <- result{nil, err}
				return
			}
			syms, err := c.WorkspaceSymbol("MyFunc")
			results <- result{syms, err}
		}()
	}

	for i := 0; i < n; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, r.err)
			continue
		}
		if len(r.syms) != 2 {
			t.Errorf("goroutine %d: expected 2 symbols, got %d", i, len(r.syms))
		}
	}
}

// TestLSPManager_StartStop_Integration tests the full client lifecycle using
// the white-box Manager helper (fakeInitializedManager) to bypass the need for
// a real exec.Command.  It exercises Initialize → WorkspaceSymbol → stop()
// in sequence, verifying that the manager's client state transitions correctly.
func TestLSPManager_StartStop_Integration(t *testing.T) {
	// Wire a fresh client to a mock server.
	clientPipe, serverPipe := newInternalPipe()
	t.Cleanup(func() {
		clientPipe.reader.Close()
		clientPipe.writer.Close()
		serverPipe.reader.Close()
		serverPipe.writer.Close()
	})

	go mockLSPServer(serverPipe.reader, serverPipe.writer)

	// Build a Manager whose internal client is wired to our mock server,
	// bypassing exec.Command entirely (white-box access to unexported fields).
	mgr := &Manager{
		cfg:  ServerConfig{Command: "fake"},
		lang: "go",
	}
	mgr.client = NewClient(clientPipe, "go")

	// Before Initialize, initDone must be false.
	if mgr.client.initDone {
		t.Fatal("expected initDone=false before Initialize")
	}

	// Initialize via the client directly (Manager.Start requires a real process).
	if err := mgr.client.Initialize("file:///mock/project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !mgr.client.initDone {
		t.Fatal("expected initDone=true after Initialize")
	}

	// Query via Manager.Symbols (delegates to client.WorkspaceSymbol).
	syms, err := mgr.Symbols("MyFunc")
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	if len(syms) != 2 {
		t.Errorf("expected 2 symbols, got %d", len(syms))
	}

	// Stop the manager.  Because no real exec.Cmd was ever started (mgr.cmd == nil)
	// the internal stop() guard returns early without clearing the client pointer —
	// that is the correct and expected behaviour for a manager that was never fully
	// started via Start().  We just assert that Stop does not return an error.
	if err := mgr.Stop(); err != nil {
		t.Fatalf("Stop returned unexpected error: %v", err)
	}
}

// TestLSPTransport_MalformedHeader verifies that Transport.Receive returns an
// error when the incoming frame has no Content-Length header.
func TestLSPTransport_MalformedHeader(t *testing.T) {
	// Deliberately omit Content-Length; send only the empty-line header terminator.
	// The Transport should detect the missing header and return an error.
	raw := "X-Custom-Header: value\r\n\r\n"
	rw := struct {
		io.Reader
		io.Writer
	}{strings.NewReader(raw), io.Discard}

	tr := NewTransport(rw)
	var result map[string]any
	err := tr.Receive(&result)
	if err == nil {
		t.Fatal("expected error for frame missing Content-Length, got nil")
	}
	if !strings.Contains(err.Error(), "Content-Length") {
		t.Errorf("expected 'Content-Length' in error message, got: %v", err)
	}
}

// TestLSPTransport_TruncatedBody verifies that Transport.Receive returns an
// error when the frame body is shorter than the declared Content-Length.
func TestLSPTransport_TruncatedBody(t *testing.T) {
	// Declare 100 bytes but only supply 20.
	header := "Content-Length: 100\r\n\r\n"
	body := strings.Repeat("x", 20) // only 20 bytes
	raw := header + body

	rw := struct {
		io.Reader
		io.Writer
	}{strings.NewReader(raw), io.Discard}

	tr := NewTransport(rw)
	var result map[string]any
	err := tr.Receive(&result)
	if err == nil {
		t.Fatal("expected error for truncated body, got nil")
	}
}
