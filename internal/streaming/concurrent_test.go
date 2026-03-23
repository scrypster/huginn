package streaming_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/streaming"
)

// TestRunner_Concurrent_MultipleRunners verifies multiple runners can operate simultaneously.
func TestRunner_Concurrent_MultipleRunners(t *testing.T) {
	const numRunners = 10
	var wg sync.WaitGroup
	results := make([]string, numRunners)
	var mu sync.Mutex

	for i := 0; i < numRunners; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r := streaming.NewRunner()
			r.Start(context.Background(), func(emit func(string)) error {
				emit("runner")
				emit("_")
				emit(string(rune('0' + idx)))
				return nil
			})

			var tokens []string
			for tok := range r.TokenCh() {
				tokens = append(tokens, tok)
			}
			err := <-r.ErrCh()
			if err != nil {
				t.Errorf("runner %d error: %v", idx, err)
			}

			result := ""
			for _, tok := range tokens {
				result += tok
			}
			mu.Lock()
			results[idx] = result
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify each runner produced its expected output
	for i, result := range results {
		expected := "runner_" + string(rune('0'+i))
		if result != expected {
			t.Errorf("runner %d: expected %q, got %q", i, expected, result)
		}
	}
}

// TestRunner_Concurrent_RapidFire verifies many tokens emitted rapidly are all captured.
func TestRunner_Concurrent_RapidFire(t *testing.T) {
	r := streaming.NewRunner()
	const numTokens = 1000

	r.Start(context.Background(), func(emit func(string)) error {
		for i := 0; i < numTokens; i++ {
			emit("x")
		}
		return nil
	})

	var count int
	for range r.TokenCh() {
		count++
	}
	<-r.ErrCh()

	if count != numTokens {
		t.Errorf("expected %d tokens, got %d", numTokens, count)
	}
}

// TestRunner_Concurrent_SlowConsumer verifies runner works when consumer is slow.
func TestRunner_Concurrent_SlowConsumer(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		for i := 0; i < 100; i++ {
			emit("token")
		}
		return nil
	})

	var count int
	for tok := range r.TokenCh() {
		if tok != "token" {
			t.Errorf("expected 'token', got %q", tok)
		}
		count++
		// Simulate slow consumer
		time.Sleep(1 * time.Millisecond)
	}
	err := <-r.ErrCh()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if count != 100 {
		t.Errorf("expected 100 tokens, got %d", count)
	}
}

// TestRunner_Concurrent_MultipleReaders verifies only one reader consumes tokens.
// (This is expected behavior: once a channel is closed, multiple readers get EOF)
func TestRunner_Concurrent_MultipleReaders(t *testing.T) {
	r := streaming.NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		emit("first")
		emit("second")
		emit("third")
		return nil
	})

	tokenCh := r.TokenCh()
	var reader1Tokens []string
	var reader2Tokens []string

	// First reader consumes all tokens
	for tok := range tokenCh {
		reader1Tokens = append(reader1Tokens, tok)
	}

	// Second reader on same channel gets nothing (channel closed)
	for tok := range tokenCh {
		reader2Tokens = append(reader2Tokens, tok)
	}

	err := <-r.ErrCh()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(reader1Tokens) != 3 {
		t.Errorf("expected 3 tokens for reader1, got %d", len(reader1Tokens))
	}
	if len(reader2Tokens) != 0 {
		t.Errorf("expected 0 tokens for reader2 (channel already closed), got %d", len(reader2Tokens))
	}
}

// TestRunner_Concurrent_ContextCancel_MultipleGoroutines verifies proper cleanup on cancel.
func TestRunner_Concurrent_ContextCancel_MultipleGoroutines(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	const numRunners = 5

	for i := 0; i < numRunners; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := streaming.NewRunner()
			r.Start(ctx, func(emit func(string)) error {
				for j := 0; j < 1000; j++ {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
						emit("tok")
					}
				}
				return nil
			})

			for range r.TokenCh() {
			}
			<-r.ErrCh()
		}()
	}

	// Let some runners start emitting
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Wait for all to finish
	wg.Wait()
}

// TestRunner_BufferSize verifies the token channel buffer size is adequate.
// With 256 buffer, bursts up to 256 should not block.
func TestRunner_BufferSize_Burst(t *testing.T) {
	r := streaming.NewRunner()
	const burstSize = 256

	done := make(chan bool)
	go func() {
		r.Start(context.Background(), func(emit func(string)) error {
			// Emit tokens without reading — should not deadlock with 256 buffer
			for i := 0; i < burstSize; i++ {
				emit(string(rune('0' + (i % 10))))
			}
			return nil
		})
		done <- true
	}()

	// Wait for emission to complete
	<-done

	// Now consume
	var count int
	for range r.TokenCh() {
		count++
	}
	<-r.ErrCh()

	if count != burstSize {
		t.Errorf("expected %d tokens after burst, got %d", burstSize, count)
	}
}
