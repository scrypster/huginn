package lsp

// manager_boost95_test.go — internal (white-box) tests for Manager.
// Being in package lsp (not lsp_test) allows direct access to unexported fields.

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// internalPipeRW is a bidirectional pipe for white-box tests.
type internalPipeRW struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (p *internalPipeRW) Read(b []byte) (int, error)  { return p.reader.Read(b) }
func (p *internalPipeRW) Write(b []byte) (int, error) { return p.writer.Write(b) }

func newInternalPipe() (*internalPipeRW, *internalPipeRW) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	return &internalPipeRW{reader: r1, writer: w2}, &internalPipeRW{reader: r2, writer: w1}
}

// fakeInitialized creates a Manager whose client is set to a pre-initialized state
// by bypassing the Start() method (white-box access to unexported fields).
func fakeInitializedManager(t *testing.T) (*Manager, *internalPipeRW) {
	t.Helper()
	clientPipe, serverPipe := newInternalPipe()

	mgr := &Manager{
		cfg:  ServerConfig{Command: "fake"},
		lang: "go",
	}

	mgr.client = NewClient(clientPipe, "go")
	mgr.client.initDone = true

	return mgr, serverPipe
}

// TestManager_Symbols_WithInitializedClient exercises the
// `return m.client.WorkspaceSymbol(query)` branch in Manager.Symbols.
// Also covers the `resp.Result == nil` branch in WorkspaceSymbol by
// omitting the "result" field entirely (json.RawMessage stays nil).
func TestManager_Symbols_WithInitializedClient(t *testing.T) {
	mgr, serverPipe := fakeInitializedManager(t)

	go func() {
		tr := NewTransport(serverPipe)
		var req map[string]any
		if err := tr.Receive(&req); err != nil {
			return
		}
		reqID := req["id"]
		// Omit "result" so json.RawMessage is nil -> exercises resp.Result == nil.
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%v}`, reqID)
		raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
		serverPipe.Write([]byte(raw))
	}()

	syms, err := mgr.Symbols("Foo")
	if err != nil {
		t.Fatalf("Symbols: %v", err)
	}
	if syms != nil {
		t.Errorf("expected nil syms when result field omitted, got %v", syms)
	}
}

// TestManager_Definition_WithInitializedClient exercises the
// `return m.client.TextDocumentDefinition(...)` branch in Manager.Definition.
// Also covers the `resp.Result == nil` branch in TextDocumentDefinition by
// omitting the "result" field entirely (json.RawMessage stays nil).
func TestManager_Definition_WithInitializedClient(t *testing.T) {
	mgr, serverPipe := fakeInitializedManager(t)

	go func() {
		tr := NewTransport(serverPipe)
		var req map[string]any
		if err := tr.Receive(&req); err != nil {
			return
		}
		reqID := req["id"]
		// Omit "result" so json.RawMessage is nil -> exercises resp.Result == nil.
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%v}`, reqID)
		raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
		serverPipe.Write([]byte(raw))
	}()

	locs, err := mgr.Definition("file:///test.go", 1, 1)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if locs != nil {
		t.Errorf("expected nil locs when result field omitted, got %v", locs)
	}
}

// TestManager_Start_AlreadyInitialized exercises the early-return guard in Start
// when the client is already initialized (`m.client != nil && m.client.initDone`).
func TestManager_Start_AlreadyInitialized(t *testing.T) {
	mgr := &Manager{
		cfg:  ServerConfig{Command: "fake"},
		lang: "go",
	}
	clientPipe, _ := newInternalPipe()
	mgr.client = NewClient(clientPipe, "go")
	mgr.client.initDone = true

	// Start should return nil immediately (already initialized).
	err := mgr.Start("/some/root")
	if err != nil {
		t.Errorf("Start with already-initialized client should return nil, got: %v", err)
	}
}

// TestTransport_Send_MarshalError_Internal exercises the json.Marshal error
// in Send when given a non-serializable value (e.g., a channel).
func TestTransport_Send_MarshalError_Internal(t *testing.T) {
	var buf bytes.Buffer
	rw := struct {
		io.Reader
		io.Writer
	}{strings.NewReader(""), &buf}
	tr := NewTransport(rw)
	ch := make(chan int)
	err := tr.Send(ch)
	if err == nil {
		t.Error("expected error marshaling channel")
	}
	if !strings.Contains(err.Error(), "lsp send:") {
		t.Errorf("expected 'lsp send:' prefix, got: %v", err)
	}
}
