package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/compact"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
)

// blockingCompactStrategy is a compact.CompactionStrategy whose Compact call
// blocks until release is closed, simulating a slow summarization LLM call.
// It also tracks the number of concurrently in-flight Compact calls so tests
// can assert compactions for the same session never race.
type blockingCompactStrategy struct {
	release chan struct{}

	mu          sync.Mutex
	calls       int
	inFlight    int
	maxInFlight int
}

func (s *blockingCompactStrategy) ShouldCompact(_ []backend.Message, _ int) bool {
	return true
}

func (s *blockingCompactStrategy) Compact(_ context.Context, _ []backend.Message, _ int, _ backend.Backend, _ string) ([]backend.Message, error) {
	s.mu.Lock()
	s.calls++
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	s.mu.Unlock()

	<-s.release

	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()

	return []backend.Message{{Role: "system", Content: "compacted"}}, nil
}

func newAsyncCompactTestOrchestrator(t *testing.T, strat *blockingCompactStrategy) *Orchestrator {
	t.Helper()
	comp := compact.New(compact.Config{Mode: compact.ModeAlways, Strategy: strat})
	models := &modelconfig.Models{Reasoner: "test-model"}
	mb := &mockBackend{responses: []*backend.ChatResponse{{Content: "ok", DoneReason: "stop"}}}
	return mustNewOrchestrator(t, mb, models, nil, nil, stats.NoopCollector{}, comp)
}

// TestCompactHistoryAsync_DoesNotBlockCaller verifies that compactHistoryAsync
// returns immediately even when the configured compaction strategy is slow
// (e.g. a full summarization LLM call) — DEFECT B's requirement that the
// current turn's reply never waits on compaction.
func TestCompactHistoryAsync_DoesNotBlockCaller(t *testing.T) {
	strat := &blockingCompactStrategy{release: make(chan struct{})}
	defer close(strat.release)
	o := newAsyncCompactTestOrchestrator(t, strat)
	sess := o.defaultSession()
	sess.appendHistory(backend.Message{Role: "user", Content: "hi"})

	start := time.Now()
	o.compactHistoryAsync(sess)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("compactHistoryAsync blocked the caller for %v; expected near-instant return", elapsed)
	}

	// The compaction should not have completed yet — history is unchanged.
	sess.mu.Lock()
	got := len(sess.history)
	sess.mu.Unlock()
	if got != 1 {
		t.Fatalf("expected history untouched while compaction is still in flight, got %d messages", got)
	}
}

// TestCompactHistoryAsync_NextTurnWaitsForInFlightCompaction verifies the
// documented invariant: the CURRENT turn's reply never waits on compaction,
// but the NEXT turn (via snapshotHistory) does — so it always observes the
// compacted result rather than racing ahead of it.
func TestCompactHistoryAsync_NextTurnWaitsForInFlightCompaction(t *testing.T) {
	strat := &blockingCompactStrategy{release: make(chan struct{})}
	o := newAsyncCompactTestOrchestrator(t, strat)
	sess := o.defaultSession()
	sess.appendHistory(backend.Message{Role: "user", Content: "hi"})

	o.compactHistoryAsync(sess)

	// Give the goroutine a moment to enter Compact() and block on release.
	deadline := time.After(2 * time.Second)
	for {
		strat.mu.Lock()
		inFlight := strat.inFlight
		strat.mu.Unlock()
		if inFlight > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("compaction never started")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// snapshotHistory must block until the compaction completes.
	done := make(chan []backend.Message, 1)
	go func() {
		done <- sess.snapshotHistory()
	}()

	select {
	case <-done:
		t.Fatal("snapshotHistory returned before in-flight compaction finished")
	case <-time.After(100 * time.Millisecond):
		// expected: still blocked
	}

	close(strat.release)

	select {
	case got := <-done:
		if len(got) != 1 || got[0].Content != "compacted" {
			t.Fatalf("expected compacted history after wait, got %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("snapshotHistory did not unblock after compaction completed")
	}
}

// TestCompactHistoryAsync_SerializesConcurrentCompactions verifies two
// compactions scheduled for the same session never run Compact() concurrently.
func TestCompactHistoryAsync_SerializesConcurrentCompactions(t *testing.T) {
	strat := &blockingCompactStrategy{release: make(chan struct{})}
	o := newAsyncCompactTestOrchestrator(t, strat)
	sess := o.defaultSession()
	sess.appendHistory(backend.Message{Role: "user", Content: "hi"})

	o.compactHistoryAsync(sess)
	o.compactHistoryAsync(sess)

	time.Sleep(50 * time.Millisecond)
	close(strat.release)

	// Wait for both to complete by acquiring compactMu once it's free.
	sess.compactMu.Lock()
	sess.compactMu.Unlock()

	strat.mu.Lock()
	maxInFlight := strat.maxInFlight
	strat.mu.Unlock()
	if maxInFlight > 1 {
		t.Fatalf("expected compactions to be serialized, but %d ran concurrently", maxInFlight)
	}
}
