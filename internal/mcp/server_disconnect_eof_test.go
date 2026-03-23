package mcp

import (
	"context"
	"errors"
	"io"
	"testing"
)

// eofTransport is a mock Transport that returns io.EOF from Receive(),
// simulating a server process that exits mid-request.
type eofTransport struct{}

func (e *eofTransport) Send(_ context.Context, _ []byte) error { return nil }
func (e *eofTransport) Receive(_ context.Context) ([]byte, error) {
	return nil, io.EOF
}
func (e *eofTransport) Close() error { return nil }

// TestMCPClient_CallTool_EOF_ReturnsError verifies that when the MCP server
// exits mid-request (Receive returns io.EOF), CallTool returns a clear error
// describing the disconnect rather than a raw io.EOF or a silent empty result.
//
// Bug: Receive() on StdioTransport returns io.EOF unchanged when the server
// process exits. CallTool propagates it directly. Callers checking errors.Is(err,
// io.EOF) do not get a meaningful message about which server disconnected.
//
// Fix: wrap io.EOF from Receive with a "server disconnected" message so callers
// get an actionable error rather than a raw EOF.
func TestMCPClient_CallTool_EOF_ReturnsError(t *testing.T) {
	client := NewMCPClient(&eofTransport{})

	_, err := client.CallTool(context.Background(), "some_tool", nil)
	if err == nil {
		t.Fatal("expected error when server returns EOF, got nil")
	}

	// The error must wrap io.EOF so callers can detect server disconnects.
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected errors.Is(err, io.EOF) to be true, got: %v", err)
	}

	// The error message must mention disconnect context, not just "EOF".
	// Raw "EOF" alone is not actionable — callers cannot tell which server died.
	msg := err.Error()
	if msg == "EOF" {
		t.Errorf("error message %q is too terse; expected wrapped 'server disconnected' or similar", msg)
	}
	t.Logf("got error: %v", err)
}

// isDisconnectError reports whether the error originates from a server disconnect.
func isDisconnectError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return false
}

// TestMCPClient_Initialize_EOF_ReturnsError verifies the same for Initialize.
func TestMCPClient_Initialize_EOF_ReturnsError(t *testing.T) {
	client := NewMCPClient(&eofTransport{})

	err := client.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error when server returns EOF during initialize, got nil")
	}
	t.Logf("got expected error: %v", err)
}

// TestMCPClient_ListTools_EOF_ReturnsError verifies the same for ListTools.
func TestMCPClient_ListTools_EOF_ReturnsError(t *testing.T) {
	client := NewMCPClient(&eofTransport{})

	_, err := client.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error when server returns EOF during list tools, got nil")
	}
	t.Logf("got expected error: %v", err)
}
