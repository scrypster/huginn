package streaming_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/streaming"
)

// TestRunner_BufferBackpressure emits more tokens than the 256-slot buffer
// and verifies all tokens are eventually received (no deadlock, no drop).
func TestRunner_BufferBackpressure(t *testing.T) {
	const total = 400 // exceeds 256-slot buffer
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		for i := 0; i < total; i++ {
			emit("x")
		}
		return nil
	})

	var count int
	for range r.TokenCh() {
		count++
	}
	if err := <-r.ErrCh(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != total {
		t.Errorf("expected %d tokens, got %d", total, count)
	}
}

// TestRunner_TokenOrdering verifies that tokens arrive in the order emitted.
func TestRunner_TokenOrdering(t *testing.T) {
	words := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		for _, w := range words {
			emit(w)
		}
		return nil
	})

	var got []string
	for tok := range r.TokenCh() {
		got = append(got, tok)
	}
	if err := <-r.ErrCh(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(words) {
		t.Fatalf("expected %d tokens, got %d", len(words), len(got))
	}
	for i, w := range words {
		if got[i] != w {
			t.Errorf("position %d: expected %q, got %q", i, w, got[i])
		}
	}
}

// TestRunner_LargePayloadToken verifies that tokens containing large strings
// pass through correctly without truncation.
func TestRunner_LargePayloadToken(t *testing.T) {
	bigPayload := strings.Repeat("A", 64*1024) // 64 KB token
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		emit(bigPayload)
		return nil
	})

	var received string
	for tok := range r.TokenCh() {
		received += tok
	}
	if err := <-r.ErrCh(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received != bigPayload {
		t.Errorf("large payload mismatch: got len=%d, want len=%d", len(received), len(bigPayload))
	}
}

// TestRunner_ContextCancelledBeforeStart cancels context before Start is
// called and verifies the fn observes cancellation.
func TestRunner_ContextCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Start

	r := streaming.NewRunner()
	r.Start(ctx, func(emit func(string)) error {
		// Attempt to emit — select should pick ctx.Done() immediately.
		emit("might-not-send")
		return ctx.Err()
	})

	for range r.TokenCh() {
	}
	err := <-r.ErrCh()
	if err == nil {
		t.Error("expected an error from cancelled context, got nil")
	}
}

// TestRunner_ConcurrentRunners verifies that multiple independent runners
// work simultaneously without interfering with each other.
func TestRunner_ConcurrentRunners(t *testing.T) {
	const numRunners = 8
	var wg sync.WaitGroup
	results := make([]string, numRunners)

	for i := 0; i < numRunners; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := streaming.NewRunner()
			r.Start(context.Background(), func(emit func(string)) error {
				emit("runner")
				emit("-")
				emit(string(rune('0' + i)))
				return nil
			})
			var sb strings.Builder
			for tok := range r.TokenCh() {
				sb.WriteString(tok)
			}
			if err := <-r.ErrCh(); err != nil {
				t.Errorf("runner %d: unexpected error: %v", i, err)
			}
			results[i] = sb.String()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent runners timed out")
	}

	for i, res := range results {
		expected := "runner-" + string(rune('0'+i))
		if res != expected {
			t.Errorf("runner %d: expected %q, got %q", i, expected, res)
		}
	}
}

// TestRunner_PanicWithNilError verifies that a panic with a nil value is
// recovered and reported as an error.
func TestRunner_PanicNilValue(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		var p any = nil
		panic(p)
	})

	for range r.TokenCh() {
	}
	err := <-r.ErrCh()
	if err == nil {
		t.Error("expected error from nil panic, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected 'panic' in error message, got: %v", err)
	}
}
