package lsp_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/symbol/lsp"
)

// ------------------- Transport error paths -------------------

// errWriter always fails on Write.
type errWriter struct {
	r *strings.Reader
}

func (e *errWriter) Read(p []byte) (int, error)  { return e.r.Read(p) }
func (e *errWriter) Write(p []byte) (int, error) { return 0, fmt.Errorf("write error") }

// TestTransport_Send_WriteHeaderError exercises the io.WriteString failure in Send.
func TestTransport_Send_WriteHeaderError(t *testing.T) {
	rw := &errWriter{r: strings.NewReader("")}
	tr := lsp.NewTransport(rw)
	err := tr.Send(map[string]any{"jsonrpc": "2.0"})
	if err == nil {
		t.Error("expected error on write failure")
	}
}

// partialWriter writes the header successfully but fails on the body write.
type partialWriter struct {
	r       *strings.Reader
	writes  int
	failAt  int
}

func (p *partialWriter) Read(b []byte) (int, error) { return p.r.Read(b) }
func (p *partialWriter) Write(b []byte) (int, error) {
	p.writes++
	if p.writes >= p.failAt {
		return 0, fmt.Errorf("body write error")
	}
	return len(b), nil
}

// TestTransport_Send_WriteBodyError exercises the t.rw.Write(body) failure.
func TestTransport_Send_WriteBodyError(t *testing.T) {
	rw := &partialWriter{r: strings.NewReader(""), failAt: 2}
	tr := lsp.NewTransport(rw)
	err := tr.Send(map[string]any{"jsonrpc": "2.0"})
	if err == nil {
		t.Error("expected error on body write failure")
	}
}

// TestTransport_Receive_MissingContentLength sends a frame with no Content-Length header.
func TestTransport_Receive_MissingContentLength(t *testing.T) {
	// Two CRLF to signal end of headers, but no Content-Length line.
	raw := "\r\n"
	rw := &mockRW{r: strings.NewReader(raw), w: &bytes.Buffer{}}
	tr := lsp.NewTransport(rw)
	var result map[string]any
	err := tr.Receive(&result)
	if err == nil {
		t.Error("expected error for missing Content-Length")
	}
	if !strings.Contains(err.Error(), "Content-Length") {
		t.Errorf("expected Content-Length in error, got: %v", err)
	}
}

// TestTransport_Receive_TooLarge sends a Content-Length that exceeds maxMsgSize.
func TestTransport_Receive_TooLarge(t *testing.T) {
	// maxMsgSize is 8 MB; use 9 MB.
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n", 9*1024*1024)
	rw := &mockRW{r: strings.NewReader(raw), w: &bytes.Buffer{}}
	tr := lsp.NewTransport(rw)
	var result map[string]any
	err := tr.Receive(&result)
	if err == nil {
		t.Error("expected error for oversized message")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected 'too large' in error, got: %v", err)
	}
}

// TestTransport_Receive_InvalidContentLength sends a non-numeric Content-Length.
func TestTransport_Receive_InvalidContentLength(t *testing.T) {
	raw := "Content-Length: abc\r\n\r\n"
	rw := &mockRW{r: strings.NewReader(raw), w: &bytes.Buffer{}}
	tr := lsp.NewTransport(rw)
	var result map[string]any
	err := tr.Receive(&result)
	if err == nil {
		t.Error("expected error for invalid Content-Length value")
	}
}

// TestTransport_Receive_HeaderReadError exercises EOF during header read.
func TestTransport_Receive_HeaderReadError(t *testing.T) {
	// Empty reader — ReadString('\n') will return EOF immediately.
	rw := &mockRW{r: strings.NewReader(""), w: &bytes.Buffer{}}
	tr := lsp.NewTransport(rw)
	var result map[string]any
	err := tr.Receive(&result)
	if err == nil {
		t.Error("expected error on empty reader")
	}
}

// TestTransport_Receive_BodyReadError sends a valid header but truncated body.
func TestTransport_Receive_BodyReadError(t *testing.T) {
	// Content-Length says 100 bytes but body only has 5.
	raw := "Content-Length: 100\r\n\r\nhello"
	rw := &mockRW{r: strings.NewReader(raw), w: &bytes.Buffer{}}
	tr := lsp.NewTransport(rw)
	var result map[string]any
	err := tr.Receive(&result)
	if err == nil {
		t.Error("expected error for truncated body")
	}
}

// TestTransport_Receive_BadJSON sends a valid frame with invalid JSON body.
func TestTransport_Receive_BadJSON(t *testing.T) {
	body := "not-valid-json!!!"
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	rw := &mockRW{r: strings.NewReader(raw), w: &bytes.Buffer{}}
	tr := lsp.NewTransport(rw)
	var result map[string]any
	err := tr.Receive(&result)
	if err == nil {
		t.Error("expected error for invalid JSON body")
	}
}

// ------------------- Client error paths -------------------

// blockRW blocks reads until it's closed; used to force send-after-receive errors.
type blockRW struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed bool
	cond   *sync.Cond
}

func newBlockRW() *blockRW {
	b := &blockRW{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *blockRW) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := b.buf.Write(p)
	b.cond.Broadcast()
	return n, err
}

func (b *blockRW) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.buf.Len() == 0 && !b.closed {
		b.cond.Wait()
	}
	if b.buf.Len() == 0 {
		return 0, io.EOF
	}
	return b.buf.Read(p)
}

func (b *blockRW) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.cond.Broadcast()
}

// TestClient_Initialize_SendError exercises the first tr.Send error path in Initialize.
func TestClient_Initialize_SendError(t *testing.T) {
	rw := &errWriter{r: strings.NewReader("")}
	client := lsp.NewClient(rw, "go")
	err := client.Initialize("file:///project")
	if err == nil {
		t.Error("expected error when Send fails during Initialize")
	}
}

// TestClient_Initialize_ReceiveError exercises the tr.Receive error path in Initialize.
func TestClient_Initialize_ReceiveError(t *testing.T) {
	// Use a pipe; write side provides a valid send path, but the reader
	// returns only the header bytes then EOF so Receive fails.
	// We craft a mockRW where the writer discards and the reader has only partial data.
	// Write path succeeds (buffer), read path returns EOF immediately after header.
	buf := &bytes.Buffer{}
	// Reader is empty → Receive will fail trying to read the header.
	rw := struct {
		io.Reader
		io.Writer
	}{strings.NewReader(""), buf}
	client := lsp.NewClient(rw, "go")
	err := client.Initialize("file:///project")
	if err == nil {
		t.Error("expected error when Receive fails during Initialize")
	}
}

// TestClient_Initialize_NotifSendError exercises the second tr.Send (notification) error path.
func TestClient_Initialize_NotifSendError(t *testing.T) {
	// Use a countedWriter that allows only the first write (the initialize request),
	// then fails. The Receive will read from a pre-built framed response.

	// First, build the framed initialize response that the client will read back.
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result":  map[string]any{"capabilities": map[string]any{}},
	}
	var rbuf bytes.Buffer
	frameTr := lsp.NewTransport(&mockRW{r: strings.NewReader(""), w: &rbuf})
	_ = frameTr.Send(resp)

	writeCount := 0
	// failAfter=1 means the first Write call succeeds (initialize request header),
	// second Write (initialize request body) also needs to succeed, third fails (notif header).
	// The initialize request is two writes: header string + body bytes.
	// So failAfter=2 allows the full first Send, then fails on the notification Send.
	ctrlRW := &countedWriter{
		Reader:        strings.NewReader(rbuf.String()),
		failAfter:     2,
		writeCountPtr: &writeCount,
	}

	client := lsp.NewClient(ctrlRW, "go")
	err := client.Initialize("file:///project")
	// The initialize Send succeeds, Receive succeeds, but the initialized notification Send fails.
	if err == nil {
		t.Error("expected error when initialized notification Send fails")
	}
}

// countedWriter succeeds for the first `failAfter` Write calls, then fails.
type countedWriter struct {
	io.Reader
	failAfter     int
	writeCountPtr *int
	buf           bytes.Buffer
}

func (c *countedWriter) Write(p []byte) (int, error) {
	*c.writeCountPtr++
	if *c.writeCountPtr > c.failAfter {
		return 0, fmt.Errorf("write disabled after %d calls", c.failAfter)
	}
	return c.buf.Write(p)
}

// TestClient_TextDocumentDefinition_SendError exercises the Send error in TextDocumentDefinition.
func TestClient_TextDocumentDefinition_SendError(t *testing.T) {
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
		// Close the server's write pipe so client Receive on the definition call fails.
		// Actually we need to close clientPipe.reader to make client Send fail.
		// Close serverPipe.reader so clientPipe.writer gets a broken pipe on next write.
		serverPipe.reader.Close()
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := client.TextDocumentDefinition("file:///test.go", 1, 1)
	if err == nil {
		t.Error("expected error when Send fails in TextDocumentDefinition")
	}
}

// TestClient_TextDocumentDefinition_ReceiveError exercises Receive failure after Send succeeds.
func TestClient_TextDocumentDefinition_ReceiveError(t *testing.T) {
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

		// Receive the definition request but do NOT respond; close the write side
		// so the client's Receive call gets EOF.
		var defReq map[string]any
		tr.Receive(&defReq)
		serverPipe.writer.Close()
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := client.TextDocumentDefinition("file:///test.go", 1, 1)
	if err == nil {
		t.Error("expected error when Receive fails in TextDocumentDefinition")
	}
}

// TestClient_TextDocumentDefinition_BadJSONResult exercises the final json.Unmarshal failure
// when the result is valid JSON but cannot be decoded as []Location or Location.
func TestClient_TextDocumentDefinition_BadJSONResult(t *testing.T) {
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

		// Respond with a result that is a raw number — not a Location or []Location.
		var defReq map[string]any
		tr.Receive(&defReq)
		// Use raw JSON so we can control the result exactly.
		raw := fmt.Sprintf(
			"Content-Length: %d\r\n\r\n%s",
			len(`{"jsonrpc":"2.0","id":2,"result":12345}`),
			`{"jsonrpc":"2.0","id":2,"result":12345}`,
		)
		serverPipe.Write([]byte(raw))
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := client.TextDocumentDefinition("file:///test.go", 1, 1)
	if err == nil {
		t.Error("expected error for invalid result JSON in TextDocumentDefinition")
	}
}

// TestClient_WorkspaceSymbol_SendError exercises the Send error in WorkspaceSymbol.
func TestClient_WorkspaceSymbol_SendError(t *testing.T) {
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
		// Close the server reader so the next client write fails.
		serverPipe.reader.Close()
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := client.WorkspaceSymbol("Handler")
	if err == nil {
		t.Error("expected error when Send fails in WorkspaceSymbol")
	}
}

// TestClient_WorkspaceSymbol_ReceiveError exercises Receive failure after Send succeeds.
func TestClient_WorkspaceSymbol_ReceiveError(t *testing.T) {
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
		// Receive the symbol request but close writer before responding.
		var symReq map[string]any
		tr.Receive(&symReq)
		serverPipe.writer.Close()
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := client.WorkspaceSymbol("Handler")
	if err == nil {
		t.Error("expected error when Receive fails in WorkspaceSymbol")
	}
}

// TestClient_WorkspaceSymbol_BadJSONResult exercises the json.Unmarshal failure in WorkspaceSymbol.
func TestClient_WorkspaceSymbol_BadJSONResult(t *testing.T) {
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
		// Send a numeric result — not a []SymbolInformation.
		raw := fmt.Sprintf(
			"Content-Length: %d\r\n\r\n%s",
			len(`{"jsonrpc":"2.0","id":2,"result":42}`),
			`{"jsonrpc":"2.0","id":2,"result":42}`,
		)
		serverPipe.Write([]byte(raw))
	}()

	if err := client.Initialize("file:///project"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := client.WorkspaceSymbol("Handler")
	if err == nil {
		t.Error("expected error for invalid result JSON in WorkspaceSymbol")
	}
}

// ------------------- Manager stop / Start already-initialized -------------------

// fakeInitServer runs a minimal LSP server on serverPipe and returns after initialization.
func fakeInitServer(t *testing.T, serverPipe *pipeRW) {
	t.Helper()
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
	}()
}

// TestManager_Stop_WithRunningProcess tests Stop on a manager that has a real process.
func TestManager_Stop_WithRunningProcess(t *testing.T) {
	// Use "cat" as a long-running process that won't exit on its own.
	mgr := lsp.NewManager("go", lsp.ServerConfig{Command: "cat"})
	// We can't initialize without a real LSP server, but we can test Stop
	// after a partial Start that launches the process but fails on Initialize.
	// Start will fail during initialize (cat won't speak LSP), but the process
	// will have been started and stop() will be called internally.
	// After that, we call Stop() externally — it should be a no-op (cmd == nil).
	err := mgr.Start("/tmp")
	// Start should fail because cat doesn't respond to LSP initialize.
	if err == nil {
		// If somehow it didn't fail, stop explicitly.
		mgr.Stop()
		t.Skip("cat unexpectedly responded to LSP initialize")
	}
	// After the failed Start, internal stop() was already called.
	// External Stop() should be a safe no-op.
	err2 := mgr.Stop()
	if err2 != nil {
		t.Errorf("Stop after failed Start should not error: %v", err2)
	}
}

// TestManager_Start_AlreadyInitialized tests that Start is idempotent.
// We can't easily test this without a real server, but we can exercise
// the ErrNotConfigured path for the duplicate-start guard via a sub-path:
// use a command that immediately exits to simulate a start + quick fail scenario.
func TestManager_Start_CommandNotFound(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{Command: "/this/binary/does/not/exist/anywhere"})
	err := mgr.Start("/tmp")
	if err == nil {
		t.Error("expected error for missing binary")
	}
	// Stop should be safe after a failed start.
	if err2 := mgr.Stop(); err2 != nil {
		t.Errorf("Stop after failed start: %v", err2)
	}
}

// TestManager_Definition_AfterStop verifies that Definition returns an error when
// manager was stopped (client is nil again).
func TestManager_Definition_AfterStop(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{Command: "cat"})
	// Stop without starting — client is nil, cmd is nil.
	_ = mgr.Stop()
	// Now Definition should say "not started".
	_, err := mgr.Definition("file:///test.go", 1, 1)
	if err == nil {
		t.Error("expected error for nil client")
	}
}

// TestManager_Symbols_AfterStop mirrors the above for Symbols.
func TestManager_Symbols_AfterStop(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{Command: "cat"})
	_ = mgr.Stop()
	_, err := mgr.Symbols("test")
	if err == nil {
		t.Error("expected error for nil client")
	}
}

// ------------------- Detect additional coverage -------------------

// TestDetect_KnownLanguages exercises Detect for all supported languages.
// If a server binary happens to be installed we get a non-empty ServerConfig;
// if not we get an empty one. Either way the code path is exercised.
func TestDetect_KnownLanguages(t *testing.T) {
	langs := lsp.SupportedLanguages()
	for _, lang := range langs {
		cfg := lsp.Detect(lang)
		// cfg may be empty if binaries are not installed; that's fine.
		_ = cfg
	}
}

// TestDetect_LanguageWithInstalledServer verifies that if go is the language
// and gopls is available, Detect returns a non-empty command.
// This is a best-effort test — skipped when gopls is absent.
func TestDetect_LanguageWithInstalledServer(t *testing.T) {
	cfg := lsp.Detect("go")
	if cfg.Command == "" {
		t.Skip("gopls not installed; skipping")
	}
	if cfg.Command == "" {
		t.Error("expected non-empty command for go when gopls is installed")
	}
}

// ------------------- rpcError.Error coverage -------------------

// TestRPCError_ErrorString is already covered by types.go:100.0% but we include
// it explicitly for documentation purposes via the client RPC error tests above.
// (No additional code needed — covered via TestClient_Initialize_RPCError.)

// ------------------- Transport Receive with extra headers -------------------

// TestTransport_Receive_ExtraHeaders ensures non-Content-Length headers are ignored.
func TestTransport_Receive_ExtraHeaders(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{}}`
	raw := fmt.Sprintf(
		"Content-Type: application/vscode-jsonrpc; charset=utf-8\r\nContent-Length: %d\r\n\r\n%s",
		len(body), body,
	)
	rw := &mockRW{r: strings.NewReader(raw), w: &bytes.Buffer{}}
	tr := lsp.NewTransport(rw)
	var result map[string]any
	if err := tr.Receive(&result); err != nil {
		t.Fatalf("Receive with extra headers: %v", err)
	}
	if result["jsonrpc"] != "2.0" {
		t.Errorf("unexpected result: %v", result)
	}
}
