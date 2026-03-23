package server

import (
	"context"
	"testing"
	"time"
)

// TestWSClient_ContextCancelledOnUnregister verifies that a wsClient's
// per-connection context is cancelled when unregisterClient is called.
func TestWSClient_ContextCancelledOnUnregister(t *testing.T) {
	hub := newWSHub()

	ctx, cancel := context.WithCancel(context.Background())
	client := &wsClient{
		send:   make(chan WSMessage, 4),
		ctx:    ctx,
		cancel: cancel,
	}

	hub.registerWithSession(client, "test-session")

	// Context should be active before unregistering.
	select {
	case <-ctx.Done():
		t.Fatal("expected context to be active before unregister")
	default:
		// correct — context not yet cancelled
	}

	hub.unregisterClient(client)

	// Context should be cancelled after unregistering.
	select {
	case <-ctx.Done():
		// correct — context cancelled
	case <-time.After(100 * time.Millisecond):
		t.Error("expected context to be cancelled after unregisterClient, but it was not")
	}
}

// TestWSClient_ContextNilCancel_UnregisterIsSafe verifies that unregisterClient
// does not panic when the client's cancel function is nil (e.g. legacy clients).
func TestWSClient_ContextNilCancel_UnregisterIsSafe(t *testing.T) {
	hub := newWSHub()

	client := &wsClient{
		send:   make(chan WSMessage, 4),
		ctx:    nil,
		cancel: nil,
	}

	hub.registerWithSession(client, "")
	// Should not panic with nil cancel.
	hub.unregisterClient(client)
}

// TestWSHub_Stop_CancelsAllClientContexts verifies that hub.stop() cancels
// the per-connection contexts of all registered clients.
func TestWSHub_Stop_CancelsAllClientContexts(t *testing.T) {
	hub := newWSHub()
	go hub.run()

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())

	c1 := &wsClient{send: make(chan WSMessage, 4), ctx: ctx1, cancel: cancel1}
	c2 := &wsClient{send: make(chan WSMessage, 4), ctx: ctx2, cancel: cancel2}

	hub.registerWithSession(c1, "session-A")
	hub.registerWithSession(c2, "session-B")

	// Both contexts should be active before stopping.
	select {
	case <-ctx1.Done():
		t.Fatal("ctx1 should not be cancelled before hub stop")
	default:
	}
	select {
	case <-ctx2.Done():
		t.Fatal("ctx2 should not be cancelled before hub stop")
	default:
	}

	hub.stop()

	// Both contexts should be cancelled after hub.stop().
	deadline := time.After(200 * time.Millisecond)

	select {
	case <-ctx1.Done():
	case <-deadline:
		t.Error("ctx1 was not cancelled after hub.stop()")
	}

	select {
	case <-ctx2.Done():
	case <-time.After(200 * time.Millisecond):
		t.Error("ctx2 was not cancelled after hub.stop()")
	}
}

// TestWSHub_Stop_WithNilCancel_DoesNotPanic verifies that hub.stop() is safe
// when some clients have nil cancel functions.
func TestWSHub_Stop_WithNilCancel_DoesNotPanic(t *testing.T) {
	hub := newWSHub()
	go hub.run()

	// Register a client with no cancel function.
	c := &wsClient{
		send:   make(chan WSMessage, 4),
		ctx:    nil,
		cancel: nil,
	}
	hub.registerWithSession(c, "")

	// Should not panic.
	hub.stop()
}

// TestParseBoolPayload covers all branches of the helper.
func TestParseBoolPayload_AllBranches(t *testing.T) {
	tests := []struct {
		input    any
		expected bool
	}{
		{true, true},
		{false, false},
		{float64(1), true},
		{float64(0), false},
		{int(1), true},
		{int(0), false},
		{"true", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{"yes", false},
		{nil, false},
		{[]byte("true"), false},
	}

	for _, tc := range tests {
		result := parseBoolPayload(tc.input)
		if result != tc.expected {
			t.Errorf("parseBoolPayload(%v) = %v, want %v", tc.input, result, tc.expected)
		}
	}
}
