package relay_test

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

// TestWakeNotifier_WatchClosesOnCancel verifies that the Watch channel
// is closed when the context is cancelled.
func TestWakeNotifier_WatchClosesOnCancel(t *testing.T) {
	wn := relay.NewWakeNotifier()
	ctx, cancel := context.WithCancel(context.Background())
	ch := wn.Watch(ctx)

	// Cancel the context
	cancel()

	// The channel should be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed, but got a value")
		}
		// Success: channel is closed
	case <-time.After(500 * time.Millisecond):
		t.Fatal("channel not closed within 500ms after context cancel")
	}
}

// TestWakeNotifier_WatchMultipleCalls verifies that Watch can be called
// multiple times and returns independent channels.
func TestWakeNotifier_WatchMultipleCalls(t *testing.T) {
	wn := relay.NewWakeNotifier()
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())

	ch1 := wn.Watch(ctx1)
	ch2 := wn.Watch(ctx2)

	// Both channels should be distinct
	cancel1()
	cancel2()

	// Both should eventually close
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("ch1 expected to be closed")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("ch1 not closed within 500ms")
	}

	select {
	case _, ok := <-ch2:
		if ok {
			t.Error("ch2 expected to be closed")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("ch2 not closed within 500ms")
	}
}
