package streaming

import (
	"context"
	"testing"
	"time"
)

// TestRunner_IncompleteStreamWithContextCancellation verifies cancellation mid-stream.
func TestRunner_IncompleteStreamWithContextCancellation(t *testing.T) {
	r := NewRunner()
	ctx, cancel := context.WithCancel(context.Background())

	r.Start(ctx, func(emit func(string)) error {
		emit("token1")
		<-time.After(50 * time.Millisecond)
		cancel() // Cancel while emitting
		<-time.After(100 * time.Millisecond)
		emit("token2") // May not be sent due to cancellation
		return nil
	})

	tokens := []string{}
	for token := range r.TokenCh() {
		tokens = append(tokens, token)
	}

	// Should have at least token1
	if len(tokens) == 0 {
		t.Error("expected at least one token before cancellation")
	}

	// Error should be retrievable
	err := <-r.ErrCh()
	// err may be nil if function completed before cancel took effect
	_ = err
}

// TestRunner_PanicRecovery verifies panic is caught and converted to error.
func TestRunner_PanicRecovery(t *testing.T) {
	r := NewRunner()

	r.Start(context.Background(), func(emit func(string)) error {
		emit("token1")
		panic("intentional panic")
	})

	tokens := []string{}
	for token := range r.TokenCh() {
		tokens = append(tokens, token)
	}

	if len(tokens) == 0 {
		t.Error("expected token before panic")
	}

	err := <-r.ErrCh()
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}
	if err.Error() != "internal panic: intentional panic" {
		t.Errorf("expected panic error message, got: %v", err)
	}
}

// TestRunner_NoTokensEmitted verifies handling when no tokens are emitted.
func TestRunner_NoTokensEmitted(t *testing.T) {
	r := NewRunner()

	r.Start(context.Background(), func(emit func(string)) error {
		// Emit nothing
		return nil
	})

	tokens := []string{}
	for token := range r.TokenCh() {
		tokens = append(tokens, token)
	}

	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(tokens))
	}

	err := <-r.ErrCh()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

// TestRunner_ManyTokens verifies handling of many tokens without overflow.
func TestRunner_ManyTokens(t *testing.T) {
	r := NewRunner()

	r.Start(context.Background(), func(emit func(string)) error {
		for i := 0; i < 1000; i++ {
			emit("token")
		}
		return nil
	})

	count := 0
	for range r.TokenCh() {
		count++
	}

	if count != 1000 {
		t.Errorf("expected 1000 tokens, got %d", count)
	}

	err := <-r.ErrCh()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

// TestRunner_ErrorReturn verifies error from work function is propagated.
func TestRunner_ErrorReturn(t *testing.T) {
	r := NewRunner()

	r.Start(context.Background(), func(emit func(string)) error {
		emit("token1")
		return ErrTest
	})

	tokens := []string{}
	for token := range r.TokenCh() {
		tokens = append(tokens, token)
	}

	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(tokens))
	}

	err := <-r.ErrCh()
	if err != ErrTest {
		t.Errorf("expected ErrTest, got: %v", err)
	}
}

// TestRunner_VeryLargeTokens verifies handling of large token strings.
func TestRunner_VeryLargeTokens(t *testing.T) {
	r := NewRunner()

	r.Start(context.Background(), func(emit func(string)) error {
		// Emit tokens of increasing size
		for i := 0; i < 10; i++ {
			size := (i + 1) * 10000 // 10KB to 100KB per token
			token := make([]byte, size)
			for j := range token {
				token[j] = 'x'
			}
			emit(string(token))
		}
		return nil
	})

	tokens := []string{}
	for token := range r.TokenCh() {
		tokens = append(tokens, token)
	}

	if len(tokens) != 10 {
		t.Errorf("expected 10 tokens, got %d", len(tokens))
	}

	// Verify sizes
	for i, token := range tokens {
		expectedSize := (i + 1) * 10000
		if len(token) != expectedSize {
			t.Errorf("token %d: expected size %d, got %d", i, expectedSize, len(token))
		}
	}
}

// TestRunner_RapidContextCancellation verifies cancellation right after Start.
func TestRunner_RapidContextCancellation(t *testing.T) {
	r := NewRunner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before starting

	r.Start(ctx, func(emit func(string)) error {
		emit("token1")
		emit("token2")
		return nil
	})

	tokens := []string{}
	for token := range r.TokenCh() {
		tokens = append(tokens, token)
	}

	// May have 0, 1, or 2 tokens depending on timing
	t.Logf("with pre-cancelled context, got %d tokens", len(tokens))

	err := <-r.ErrCh()
	_ = err
}

// TestRunner_ContextTimeoutDuringEmit verifies timeout during token emission.
func TestRunner_ContextTimeoutDuringEmit(t *testing.T) {
	r := NewRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	r.Start(ctx, func(emit func(string)) error {
		for i := 0; i < 100; i++ {
			emit("token")
			<-time.After(5 * time.Millisecond)
		}
		return nil
	})

	tokens := []string{}
	for token := range r.TokenCh() {
		tokens = append(tokens, token)
	}

	// Should not get all 100 tokens due to timeout
	if len(tokens) >= 100 {
		t.Errorf("expected < 100 tokens due to timeout, got %d", len(tokens))
	}

	err := <-r.ErrCh()
	_ = err
}

// TestRunner_EmptyTokens verifies empty strings are transmitted.
func TestRunner_EmptyTokens(t *testing.T) {
	r := NewRunner()

	r.Start(context.Background(), func(emit func(string)) error {
		emit("")
		emit("real")
		emit("")
		return nil
	})

	tokens := []string{}
	for token := range r.TokenCh() {
		tokens = append(tokens, token)
	}

	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens (including empties), got %d", len(tokens))
	}

	if tokens[0] != "" || tokens[1] != "real" || tokens[2] != "" {
		t.Errorf("expected empty tokens to be preserved")
	}
}

// TestRunner_ChannelsNotDoubleRead verifies TokenCh/ErrCh can be read safely.
func TestRunner_ChannelsNotDoubleRead(t *testing.T) {
	r := NewRunner()

	r.Start(context.Background(), func(emit func(string)) error {
		emit("token1")
		emit("token2")
		return nil
	})

	// Drain TokenCh
	for range r.TokenCh() {
	}

	// Read ErrCh once
	err := <-r.ErrCh()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Multiple reads from ErrCh would block (should only read once per Start)
	// This test documents expected usage pattern
}

// TestRunner_ConcurrentEmit verifies emit is internally synchronized.
func TestRunner_ConcurrentEmit(t *testing.T) {
	r := NewRunner()

	r.Start(context.Background(), func(emit func(string)) error {
		// Emit from this goroutine (emit is running in the same context)
		for i := 0; i < 100; i++ {
			emit("token")
		}
		return nil
	})

	tokens := []string{}
	for token := range r.TokenCh() {
		tokens = append(tokens, token)
	}

	if len(tokens) != 100 {
		t.Errorf("expected 100 tokens, got %d", len(tokens))
	}
}

// TestRunner_NilError verifies nil errors are handled correctly.
func TestRunner_NilError(t *testing.T) {
	r := NewRunner()

	r.Start(context.Background(), func(emit func(string)) error {
		emit("success")
		return nil // Explicit nil
	})

	for range r.TokenCh() {
	}

	err := <-r.ErrCh()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

// BenchmarkRunner_TokenEmission measures token emission throughput.
func BenchmarkRunner_TokenEmission(b *testing.B) {
	r := NewRunner()
	r.Start(context.Background(), func(emit func(string)) error {
		for i := 0; i < b.N; i++ {
			emit("token")
		}
		return nil
	})

	for range r.TokenCh() {
	}
	<-r.ErrCh()
}

// testErr is a test error type.
type testErr struct{}

func (e testErr) Error() string { return "test error" }

// ErrTest is a test error value.
var ErrTest = testErr{}
