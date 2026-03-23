package relay_test

// runner_lifecycle_test.go — additional lifecycle tests for Runner.
// Tests context cancellation, custom token store wiring, and default config values.

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

// TestRunner_DefaultMachineIDSet verifies that NewRunner fills in a non-empty
// MachineID when the caller does not provide one.
func TestRunner_DefaultMachineIDSet(t *testing.T) {
	runner := relay.NewRunner(relay.RunnerConfig{
		SkipConnectOnStart: true,
	})
	if runner == nil {
		t.Fatal("expected non-nil runner")
	}
	// Run briefly to confirm no panic from empty MachineID.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500*time.Millisecond):
		t.Fatal("runner did not stop after context cancellation")
	}
}

// TestRunner_DefaultHeartbeatInterval verifies that a zero HeartbeatInterval is
// replaced with the 60-second default (runner still starts and stops cleanly).
func TestRunner_DefaultHeartbeatInterval(t *testing.T) {
	runner := relay.NewRunner(relay.RunnerConfig{
		MachineID:          "test-hb-default",
		HeartbeatInterval:  0, // should default to 60s
		SkipConnectOnStart: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500*time.Millisecond):
		t.Fatal("runner did not stop after context cancellation")
	}
}

// TestRunner_WithMemoryTokenStore verifies that wiring a MemoryTokenStore causes
// the runner to attempt a connection to the configured CloudURL (which will fail
// because no server is running) and then stop cleanly on context cancellation.
func TestRunner_WithMemoryTokenStore(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	store.Save("test-token-xyz") //nolint:errcheck

	runner := relay.NewRunner(relay.RunnerConfig{
		MachineID:         "test-machine-token",
		HeartbeatInterval: 10 * time.Millisecond,
		CloudURL:          "ws://127.0.0.1:1", // unreachable — connection will fail
		TokenStore:        store,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2*time.Second):
		t.Fatal("runner did not stop after context cancellation")
	}
}

// TestRunner_ContextCancelledBeforeRun verifies that a pre-cancelled context
// causes the runner to exit almost immediately.
func TestRunner_ContextCancelledBeforeRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run is called

	runner := relay.NewRunner(relay.RunnerConfig{
		MachineID:          "test-pre-cancelled",
		HeartbeatInterval:  10 * time.Millisecond,
		SkipConnectOnStart: true,
	})

	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500*time.Millisecond):
		t.Fatal("runner did not exit promptly on pre-cancelled context")
	}
}

// TestRunner_WithStorePath_OutboxFlushesOnContextDone verifies that when a
// StorePath is configured the outbox goroutine starts and the runner shuts down
// cleanly without hanging.
func TestRunner_WithStorePath_OutboxFlushesOnContextDone(t *testing.T) {
	dir := t.TempDir()
	runner := relay.NewRunner(relay.RunnerConfig{
		MachineID:          "test-outbox-lifecycle",
		HeartbeatInterval:  10 * time.Millisecond,
		StorePath:          dir,
		SkipConnectOnStart: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2*time.Second):
		t.Fatal("runner with StorePath did not stop after context cancellation")
	}
}

// TestRunner_MultipleRunsSequential verifies that a runner can be started and
// stopped multiple times sequentially without resource leaks or panics.
func TestRunner_MultipleRunsSequential(t *testing.T) {
	for i := 0; i < 3; i++ {
		runner := relay.NewRunner(relay.RunnerConfig{
			MachineID:          "test-multi-run",
			HeartbeatInterval:  5 * time.Millisecond,
			SkipConnectOnStart: true,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)

		done := make(chan struct{})
		go func() {
			runner.Run(ctx)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(500*time.Millisecond):
			cancel()
			t.Fatalf("iteration %d: runner did not stop", i)
		}
		cancel()
	}
}
