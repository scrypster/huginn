package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/tools"
)

func TestDebugLoop_PassesOnFirstRun(t *testing.T) {
	passRunner := func(_ context.Context, _, _ string, _ time.Duration) tools.TestResult {
		return tools.TestResult{Passed: true}
	}
	o := newTestOrchestratorForDebugLoop(t)
	err := o.DebugLoop(context.Background(), "go test ./...", 3, t.TempDir(), 5*time.Second, nil, nil, nil, passRunner)
	if err != nil {
		t.Fatalf("expected nil when tests pass: %v", err)
	}
}

func TestDebugLoop_ExhaustsMaxAttempts(t *testing.T) {
	failRunner := func(_ context.Context, _, _ string, _ time.Duration) tools.TestResult {
		return tools.TestResult{Passed: false, Failed: []string{"TestAlwaysFails"}}
	}
	o := newTestOrchestratorForDebugLoop(t)
	err := o.DebugLoop(context.Background(), "go test ./...", 2, t.TempDir(), 5*time.Second, nil, nil, nil, failRunner)
	if err == nil {
		t.Fatal("expected error after max attempts")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("expected attempt count in error: %v", err)
	}
}

func TestDebugLoop_DefaultMaxAttempts_Is3(t *testing.T) {
	var attempts int64
	failRunner := func(_ context.Context, _, _ string, _ time.Duration) tools.TestResult {
		atomic.AddInt64(&attempts, 1)
		return tools.TestResult{Passed: false}
	}
	o := newTestOrchestratorForDebugLoop(t)
	_ = o.DebugLoop(context.Background(), "go test ./...", 0, t.TempDir(), 5*time.Second, nil, nil, nil, failRunner)
	if atomic.LoadInt64(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestDebugLoop_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls int64
	runner := func(rctx context.Context, _, _ string, _ time.Duration) tools.TestResult {
		if atomic.AddInt64(&calls, 1) == 1 {
			cancel()
		}
		return tools.TestResult{Passed: false}
	}
	o := newTestOrchestratorForDebugLoop(t)
	err := o.DebugLoop(ctx, "go test ./...", 10, t.TempDir(), 5*time.Second, nil, nil, nil, runner)
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// newTestOrchestratorForDebugLoop creates a minimal orchestrator for debug loop tests.
func newTestOrchestratorForDebugLoop(t *testing.T) *Orchestrator {
	t.Helper()
	models := &modelconfig.Models{
		Reasoner: "test-model",
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "ok", DoneReason: "stop"},
		},
	}

	sc := stats.NoopCollector{}
	o, err := NewOrchestrator(mb, models, nil, nil, sc, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	return o
}
