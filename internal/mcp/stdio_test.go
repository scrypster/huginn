package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
)

// TestEchoProcess tests StdioTransport with cat (echo behavior on Unix)
func TestEchoProcess(t *testing.T) {
	// cat is available on Unix systems and echoes back what it receives
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr, err := mcp.NewStdioTransport(ctx, "cat", nil, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer tr.Close()

	// Send a message
	msg := []byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`)
	if err := tr.Send(ctx, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Receive echo
	received, err := tr.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}

	if string(received) != string(msg) {
		t.Errorf("expected %s, got %s", msg, received)
	}
}

// TestClosedProcess tests sending to a closed transport
func TestClosedProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr, err := mcp.NewStdioTransport(ctx, "cat", nil, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}

	// Close the transport
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Try to send to closed transport
	msg := []byte(`{"test":"data"}`)
	if err := tr.Send(ctx, msg); err == nil {
		t.Error("expected error when sending to closed transport")
	}
}

// TestIdempotentClose tests that Close can be called multiple times
func TestIdempotentClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr, err := mcp.NewStdioTransport(ctx, "cat", nil, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}

	// First close
	if err := tr.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second close should not error
	if err := tr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Third close should also not error
	if err := tr.Close(); err != nil {
		t.Fatalf("third Close: %v", err)
	}
}

// TestReceive_AlreadyCancelledContext tests that Receive returns immediately
// when the context is already cancelled, without spawning a goroutine.
func TestReceive_AlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	tr, err := mcp.NewStdioTransport(ctx, "cat", nil, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer tr.Close()

	// Cancel before calling Receive
	cancel()

	start := time.Now()
	_, err = tr.Receive(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error for cancelled context")
	}
	// Should return almost immediately (no goroutine spawned)
	if elapsed > 100*time.Millisecond {
		t.Errorf("Receive took %v, expected near-instant return for pre-cancelled context", elapsed)
	}
}

// TestContextCancel tests that Receive respects context cancellation
func TestContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	tr, err := mcp.NewStdioTransport(ctx, "cat", nil, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer tr.Close()

	// Cancel the context before receiving
	cancel()

	// Receive should fail due to context cancellation
	_, err = tr.Receive(ctx)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestNewStdioTransport_InvalidCommand tests that NewStdioTransport fails with invalid command
func TestNewStdioTransport_InvalidCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	tr, err := mcp.NewStdioTransport(ctx, "/nonexistent/command/that/does/not/exist", nil, nil)
	if err == nil {
		t.Error("expected error for nonexistent command")
		if tr != nil {
			tr.Close()
		}
	}
}

// TestStdioTransport_SendAfterClose tests sending to a closed transport
func TestStdioTransport_SendAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr, err := mcp.NewStdioTransport(ctx, "cat", nil, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}

	tr.Close()

	msg := []byte(`{"test":"data"}`)
	err = tr.Send(ctx, msg)
	if err == nil {
		t.Error("expected error when sending to closed transport")
	}
}

// TestStdioTransport_MultipleMessages tests sending and receiving multiple messages
func TestStdioTransport_MultipleMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr, err := mcp.NewStdioTransport(ctx, "cat", nil, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer tr.Close()

	messages := [][]byte{
		[]byte(`{"id":1,"method":"test1"}`),
		[]byte(`{"id":2,"method":"test2"}`),
		[]byte(`{"id":3,"method":"test3"}`),
	}

	for _, msg := range messages {
		if err := tr.Send(ctx, msg); err != nil {
			t.Fatalf("Send: %v", err)
		}
		received, err := tr.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		if string(received) != string(msg) {
			t.Errorf("expected %s, got %s", msg, received)
		}
	}
}

// TestStdioTransport_ReceiveWithTimeout tests that Receive respects context timeout
func TestStdioTransport_ReceiveWithTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use sleep command which will hang until timeout
	tr, err := mcp.NewStdioTransport(ctx, "sleep", []string{"10"}, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer tr.Close()

	// Create a context with short timeout
	receiveCtx, receiveCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer receiveCancel()

	_, err = tr.Receive(receiveCtx)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestStdioTransport_EnvironmentVariables tests that environment variables are passed
func TestStdioTransport_EnvironmentVariables(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use env command to print TEST_VAR
	tr, err := mcp.NewStdioTransport(ctx, "sh", []string{"-c", "echo $TEST_VAR"}, []string{"TEST_VAR=success"})
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer tr.Close()

	received, err := tr.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}

	if !stringContains(string(received), "success") {
		t.Errorf("expected 'success' in output, got: %s", received)
	}
}
