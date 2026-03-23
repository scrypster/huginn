package agent_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
)

// slowBackend simulates an LLM that takes 100ms to respond.
type slowBackend struct{}

var _ backend.Backend = (*slowBackend)(nil)

func (s *slowBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if req.OnToken != nil {
		req.OnToken("hello")
	}
	return &backend.ChatResponse{}, nil
}

func (s *slowBackend) Health(_ context.Context) error   { return nil }
func (s *slowBackend) Shutdown(_ context.Context) error { return nil }
func (s *slowBackend) ContextWindow() int               { return 128_000 }

func TestConcurrentSessionsDoNotBlock(t *testing.T) {
	b := &slowBackend{}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(b, models, nil, nil, stats.NoopCollector{}, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}

	// Create two sessions
	sess1, err := orch.NewSession("session-1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess2, err := orch.NewSession("session-2")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	_ = sess1
	_ = sess2

	var wg sync.WaitGroup
	var completed int64

	start := time.Now()
	for _, id := range []string{"session-1", "session-2"} {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			ctx := context.Background()
			_ = orch.ChatForSession(ctx, sessionID, "hello", nil, nil)
			atomic.AddInt64(&completed, 1)
		}(id)
	}
	wg.Wait()
	elapsed := time.Since(start)

	if atomic.LoadInt64(&completed) != 2 {
		t.Fatalf("expected 2 completions, got %d", completed)
	}
	// Both sessions ran concurrently — should finish in ~100ms, not ~200ms
	if elapsed > 180*time.Millisecond {
		t.Errorf("sessions appear to have serialized: elapsed=%v (want <180ms)", elapsed)
	}
}
