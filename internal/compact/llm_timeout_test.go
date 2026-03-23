package compact

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

// stalledBackend blocks on ChatCompletion until the context is cancelled.
type stalledBackend struct{}

func (s *stalledBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (s *stalledBackend) Health(ctx context.Context) error    { return nil }
func (s *stalledBackend) Shutdown(ctx context.Context) error  { return nil }
func (s *stalledBackend) ContextWindow() int                  { return 8192 }

// TestLLMStrategy_CallLLM_Timeout verifies that a hung backend does not block
// LLM compaction indefinitely.
//
// Bug: callLLM() passes the parent context to ChatCompletion. If the parent
// context has no deadline, a hung backend blocks all compaction forever.
//
// Fix: wrap callLLM in context.WithTimeout (e.g. 30s) so it always returns
// within the timeout and falls back to extractive compaction.
func TestLLMStrategy_CallLLM_Timeout(t *testing.T) {
	t.Parallel()

	strat := NewLLMStrategy(0.8)

	messages := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}

	// Use a very short context deadline so the stalled LLM call is cancelled
	// quickly. The compaction must detect the cancellation and fall back to
	// extractive rather than hanging until the full llmCompactionTimeout (30s).
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	result, err := strat.Compact(ctx, messages, 1000, &stalledBackend{}, "test-model")
	elapsed := time.Since(start)

	// Must complete (fallback to extractive) — not hang.
	if err != nil {
		t.Fatalf("expected no error (fallback to extractive), got: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty result from fallback")
	}

	// Must complete quickly — the parent context cancelled at 200ms, so the
	// LLM call should abort and fall back to extractive well within 1 second.
	if elapsed > time.Second {
		t.Errorf("Compact took %v — expected to abort and fall back within 1s", elapsed)
	}
	t.Logf("Compact completed in %v after context cancellation", elapsed)
}
