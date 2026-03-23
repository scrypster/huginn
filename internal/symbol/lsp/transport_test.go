package lsp_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/symbol/lsp"
)

type mockRW struct {
	r *strings.Reader
	w *bytes.Buffer
}

func (m *mockRW) Read(p []byte) (int, error)  { return m.r.Read(p) }
func (m *mockRW) Write(p []byte) (int, error) { return m.w.Write(p) }

func frameMsg(body string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

func TestTransport_Send_WritesContentLength(t *testing.T) {
	buf := &bytes.Buffer{}
	rw := &mockRW{r: strings.NewReader(""), w: buf}
	tr := lsp.NewTransport(rw)
	msg := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}
	if err := tr.Send(msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	written := buf.String()
	if !strings.HasPrefix(written, "Content-Length: ") {
		t.Errorf("expected Content-Length header")
	}
	if !strings.Contains(written, "\r\n\r\n") {
		t.Errorf("expected CRLF separator")
	}
}

func TestTransport_Receive_ReadsFrame(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{}}`
	raw := frameMsg(body)
	rw := &mockRW{r: strings.NewReader(raw), w: &bytes.Buffer{}}
	tr := lsp.NewTransport(rw)
	var result map[string]any
	if err := tr.Receive(&result); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if result["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0")
	}
}

func TestTransport_RoundTrip(t *testing.T) {
	written := &bytes.Buffer{}
	writeRW := &mockRW{r: strings.NewReader(""), w: written}
	writeTr := lsp.NewTransport(writeRW)
	msg := map[string]any{"jsonrpc": "2.0", "id": 42, "method": "textDocument/definition"}
	if err := writeTr.Send(msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	readRW := &mockRW{r: strings.NewReader(written.String()), w: &bytes.Buffer{}}
	readTr := lsp.NewTransport(readRW)
	var got map[string]any
	if err := readTr.Receive(&got); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got["method"] != "textDocument/definition" {
		t.Errorf("method mismatch: %v", got["method"])
	}
}
