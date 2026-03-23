package streaming_test

// hardening_h5_iter4_test.go — Hardening pass iteration 4 (streaming).
//
// Areas covered:
//  1. Runner: panic in WorkFn is recovered and returned as error
//  2. Runner: context cancel stops token delivery (no goroutine leak)
//  3. Runner: tokens are delivered in order
//  4. Runner: nil error is sent to ErrCh on success
//  5. Runner: ErrCh receives error when WorkFn returns non-nil error
//  6. Runner: TokenCh is closed before ErrCh fires (proper sequencing)
//  7. Runner: zero tokens — TokenCh closes immediately, ErrCh = nil
//  8. Runner: concurrent use of multiple runners doesn't interfere

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/streaming"
)

// ── 1. Panic in WorkFn is recovered ──────────────────────────────────────────

func TestH5_Runner_Panic_Recovered(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		panic("something went wrong")
	})

	// Drain tokens.
	for range r.TokenCh() {
	}

	err := <-r.ErrCh()
	if err == nil {
		t.Fatal("want error from panic, got nil")
	}
	if !containsStr(err.Error(), "panic") {
		t.Errorf("want 'panic' in error, got %q", err.Error())
	}
}

// ── 2. Context cancel stops token delivery ────────────────────────────────────

func TestH5_Runner_ContextCancel_StopsDelivery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := streaming.NewRunner()

	started := make(chan struct{})
	r.Start(ctx, func(emit func(string)) error {
		close(started)
		// Emit tokens continuously until context cancels.
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				emit(fmt.Sprintf("token-%d", i))
			}
		}
	})

	<-started
	// Cancel context after a brief delay.
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Drain — should not block forever.
	done := make(chan struct{})
	go func() {
		for range r.TokenCh() {
		}
		<-r.ErrCh()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not terminate after context cancel")
	}
}

// ── 3. Tokens are delivered in order ─────────────────────────────────────────

func TestH5_Runner_TokensInOrder(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		for i := 0; i < 5; i++ {
			emit(fmt.Sprintf("%d", i))
		}
		return nil
	})

	var tokens []string
	for tok := range r.TokenCh() {
		tokens = append(tokens, tok)
	}
	<-r.ErrCh()

	expected := []string{"0", "1", "2", "3", "4"}
	if len(tokens) != len(expected) {
		t.Fatalf("want %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token[%d]: want %q, got %q", i, expected[i], tok)
		}
	}
}

// ── 4. Nil error on success ───────────────────────────────────────────────────

func TestH5_Runner_NilErrorOnSuccess(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		emit("hello")
		return nil
	})
	for range r.TokenCh() {
	}
	err := <-r.ErrCh()
	if err != nil {
		t.Errorf("want nil error, got %v", err)
	}
}

// ── 5. Non-nil error from WorkFn ─────────────────────────────────────────────

func TestH5_Runner_ErrorFromWorkFn(t *testing.T) {
	want := errors.New("work failed")
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		emit("partial")
		return want
	})
	for range r.TokenCh() {
	}
	err := <-r.ErrCh()
	if !errors.Is(err, want) {
		t.Errorf("want %v, got %v", want, err)
	}
}

// ── 6. TokenCh is closed before ErrCh fires ──────────────────────────────────

func TestH5_Runner_TokenChClosedBeforeErrCh(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		emit("tok")
		return nil
	})

	// Drain TokenCh fully.
	for range r.TokenCh() {
	}
	// ErrCh should be immediately readable now.
	select {
	case err := <-r.ErrCh():
		_ = err // success
	case <-time.After(2 * time.Second):
		t.Error("ErrCh not readable after TokenCh closed")
	}
}

// ── 7. Zero tokens — TokenCh closes, ErrCh = nil ─────────────────────────────

func TestH5_Runner_ZeroTokens(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		// No tokens emitted.
		return nil
	})
	count := 0
	for range r.TokenCh() {
		count++
	}
	if count != 0 {
		t.Errorf("want 0 tokens, got %d", count)
	}
	err := <-r.ErrCh()
	if err != nil {
		t.Errorf("want nil error, got %v", err)
	}
}

// ── 8. Multiple concurrent runners don't interfere ───────────────────────────

func TestH5_Runner_Concurrent(t *testing.T) {
	const n = 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]string, 0, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		id := fmt.Sprintf("runner-%d", i)
		go func(id string) {
			defer wg.Done()
			r := streaming.NewRunner()
			r.Start(context.Background(), func(emit func(string)) error {
				emit(id)
				return nil
			})
			for tok := range r.TokenCh() {
				mu.Lock()
				results = append(results, tok)
				mu.Unlock()
			}
			<-r.ErrCh()
		}(id)
	}
	wg.Wait()

	if len(results) != n {
		t.Errorf("want %d results, got %d", n, len(results))
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
