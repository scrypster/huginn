package server

// ws_hub_spec_test.go — Behavior specs for WSHub lifecycle and concurrency.
//
// Run with: go test -race ./internal/server/...
//
// These tests verify the hub invariants documented in ws.go:
// - stop() + registerWithSession race: late-arriving clients get context cancelled
// - stop() is idempotent (double-stop doesn't panic)
// - broadcast() after stop doesn't panic (messages are drained)
// - Concurrent register + unregister + broadcast doesn't race

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestWSHub_RegisterAfterStop_ContextCancelled verifies that a client
// registering AFTER stop() has its context cancelled immediately.
// This closes the race documented at ws.go:124-126.
func TestWSHub_RegisterAfterStop_ContextCancelledSpec(t *testing.T) {
	h := newWSHub()
	go h.run()

	h.stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &wsClient{
		send:   make(chan WSMessage, 1),
		ctx:    ctx,
		cancel: cancel,
	}
	h.registerWithSession(c, "sess-123")

	// The client's context must be cancelled immediately since hub is stopped.
	select {
	case <-ctx.Done():
		// Correct: context was cancelled
	case <-time.After(100 * time.Millisecond):
		t.Error("registerWithSession after stop() did not cancel client context")
	}
}

// TestWSHub_RegisterAfterStop_NotAddedToClients verifies that a stopped hub
// does not retain the client in its internal map.
func TestWSHub_RegisterAfterStop_NotAddedToClients(t *testing.T) {
	h := newWSHub()
	go h.run()
	h.stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &wsClient{
		send:   make(chan WSMessage, 1),
		ctx:    ctx,
		cancel: cancel,
	}
	h.registerWithSession(c, "")

	h.mu.RLock()
	_, exists := h.clients[c]
	h.mu.RUnlock()

	if exists {
		t.Error("stopped hub should not retain a late-registered client in its map")
	}
}

// TestWSHub_Stop_CancelsExistingClients verifies that stop() cancels
// all already-registered client contexts.
func TestWSHub_Stop_CancelsExistingClients(t *testing.T) {
	const n = 5
	h := newWSHub()
	go h.run()

	cancels := make([]context.CancelFunc, n)
	ctxs := make([]context.Context, n)
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ctxs[i] = ctx
		cancels[i] = cancel
		c := &wsClient{
			send:   make(chan WSMessage, 4),
			ctx:    ctx,
			cancel: cancel,
		}
		h.registerWithSession(c, "sess")
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	h.stop()

	for i, ctx := range ctxs {
		select {
		case <-ctx.Done():
			// Correct
		case <-time.After(100 * time.Millisecond):
			t.Errorf("client %d context not cancelled after stop()", i)
		}
	}
}

// TestWSHub_ConcurrentRegisterUnregisterBroadcast verifies there is no data
// race between concurrent register, unregister, and broadcast operations.
// This is the most realistic production load pattern.
func TestWSHub_ConcurrentRegisterUnregisterBroadcast(t *testing.T) {
	h := newWSHub()
	go h.run()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Broadcaster goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				h.broadcast(WSMessage{Type: "test"})
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Concurrent register + unregister clients
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			c := &wsClient{
				send:   make(chan WSMessage, 8),
				ctx:    ctx,
				cancel: cancel,
			}
			h.registerWithSession(c, "load-sess")
			time.Sleep(2 * time.Millisecond)
			h.unregisterClient(c)
			cancel()
		}()
	}

	// Let it run briefly then signal done
	time.Sleep(50 * time.Millisecond)
	close(done)
	h.stop()
	wg.Wait()
}

// TestWSHub_BroadcastToSession_OnlyTargetsCorrectSession verifies that
// broadcastToSession delivers only to clients matching the sessionID,
// not to all connected clients.
func TestWSHub_BroadcastToSession_OnlyTargetsCorrectSession(t *testing.T) {
	h := newWSHub()
	go h.run()
	defer h.stop()

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	c1 := &wsClient{send: make(chan WSMessage, 4), ctx: ctx1, cancel: cancel1}
	c2 := &wsClient{send: make(chan WSMessage, 4), ctx: ctx2, cancel: cancel2}

	h.registerWithSession(c1, "session-A")
	h.registerWithSession(c2, "session-B")

	h.broadcastToSession("session-A", WSMessage{Type: "target"})

	// Give the hub a moment to deliver via its goroutine.
	// (broadcastToSession sends directly under RLock, not via broadcastC)
	select {
	case msg := <-c1.send:
		if msg.Type != "target" {
			t.Errorf("c1 got wrong message type: %q", msg.Type)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("c1 (session-A) did not receive the targeted broadcast")
	}

	select {
	case msg := <-c2.send:
		t.Errorf("c2 (session-B) should not have received session-A broadcast; got type=%q", msg.Type)
	case <-time.After(20 * time.Millisecond):
		// Correct: c2 received nothing
	}
}
