package mcp

import (
	"context"
	"testing"
	"time"
)

// TestHTTPTransport_CloseUnblocksReceive verifies that calling Close on the
// transport unblocks a goroutine blocked in Receive. Without this, recvLoop
// goroutines leak when the MCP connection is closed.
func TestHTTPTransport_CloseUnblocksReceive(t *testing.T) {
	tr := NewHTTPTransport("http://localhost:9999", "")

	errc := make(chan error, 1)
	go func() {
		_, err := tr.Receive(context.Background())
		errc <- err
	}()

	// Give the goroutine time to block in Receive.
	time.Sleep(10 * time.Millisecond)

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-errc:
		if err == nil {
			t.Error("Receive should return an error after Close, got nil")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Receive did not unblock after Close — goroutine leak")
	}
}

// TestHTTPTransport_CloseIdempotent verifies that calling Close more than once
// does not panic (e.g. closing an already-closed channel).
func TestHTTPTransport_CloseIdempotent(t *testing.T) {
	tr := NewHTTPTransport("http://localhost:9999", "")
	if err := tr.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
