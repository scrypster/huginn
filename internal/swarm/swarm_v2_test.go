package swarm_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
)

// TestSwarmResult_Populated verifies that Run returns SwarmResults with
// AgentName and Output populated from emitted EventToken events.
func TestSwarmResult_Populated(t *testing.T) {
	s := swarm.NewSwarm(4)
	go func() { for range s.Events() {} }()

	tasks := []swarm.SwarmTask{
		{
			ID:   "res-1",
			Name: "ResultAgent",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: "hello "})
				emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: "world"})
				return nil
			},
		},
	}

	results, _, _, _, err := s.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.AgentName != "ResultAgent" {
		t.Errorf("expected AgentName='ResultAgent', got %q", r.AgentName)
	}
	if r.Output != "hello world" {
		t.Errorf("expected Output='hello world', got %q", r.Output)
	}
	if r.Duration <= 0 {
		t.Error("expected positive Duration")
	}
	if r.Err != nil {
		t.Errorf("expected nil Err, got %v", r.Err)
	}
}

// TestRetry_SucceedsAfterFailures verifies that a task retries after transient
// errors and eventually succeeds.
func TestRetry_SucceedsAfterFailures(t *testing.T) {
	var attempts int32
	s := swarm.NewSwarm(1)
	go func() { for range s.Events() {} }()

	tasks := []swarm.SwarmTask{
		{
			ID:         "retry-ok",
			Name:       "RetryOK",
			MaxRetries: 3,
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				n := atomic.AddInt32(&attempts, 1)
				if n < 3 {
					return fmt.Errorf("transient error attempt %d", n)
				}
				emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: "success"})
				return nil
			},
		},
	}

	results, taskErrors, _, _, err := s.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(taskErrors) != 0 {
		t.Errorf("expected 0 task errors, got %d", len(taskErrors))
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if results[0].Output != "success" {
		t.Errorf("expected output 'success', got %q", results[0].Output)
	}
}

// TestRetry_RespectsMaxRetries verifies that when a task always fails,
// MaxRetries=2 means exactly 3 total attempts.
func TestRetry_RespectsMaxRetries(t *testing.T) {
	var attempts int32
	s := swarm.NewSwarm(1)
	go func() { for range s.Events() {} }()

	tasks := []swarm.SwarmTask{
		{
			ID:         "retry-fail",
			Name:       "RetryFail",
			MaxRetries: 2,
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				atomic.AddInt32(&attempts, 1)
				return fmt.Errorf("always fails")
			},
		},
	}

	_, taskErrors, _, _, err := s.Run(context.Background(), tasks)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(taskErrors) != 1 {
		t.Errorf("expected 1 task error, got %d", len(taskErrors))
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 total attempts (1 + MaxRetries=2), got %d", attempts)
	}
}

// TestRetry_IsRetryableFalse verifies that IsRetryable returning false
// stops retries immediately.
func TestRetry_IsRetryableFalse(t *testing.T) {
	var attempts int32
	s := swarm.NewSwarm(1)
	go func() { for range s.Events() {} }()

	tasks := []swarm.SwarmTask{
		{
			ID:         "no-retry",
			Name:       "NoRetry",
			MaxRetries: 5,
			IsRetryable: func(err error) bool {
				return false // never retry
			},
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				atomic.AddInt32(&attempts, 1)
				return fmt.Errorf("non-retryable")
			},
		},
	}

	_, _, _, _, err := s.Run(context.Background(), tasks)
	if err == nil {
		t.Fatal("expected error")
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retries), got %d", attempts)
	}
}

// TestRetry_ContextCancelStopsRetry verifies that cancelling the context
// during retry delay prevents further retries.
func TestRetry_ContextCancelStopsRetry(t *testing.T) {
	var attempts int32
	ctx, cancel := context.WithCancel(context.Background())

	s := swarm.NewSwarm(1)
	go func() { for range s.Events() {} }()

	tasks := []swarm.SwarmTask{
		{
			ID:         "cancel-retry",
			Name:       "CancelRetry",
			MaxRetries: 10,
			RetryDelay: 2 * time.Second, // long delay; context will cancel first
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				n := atomic.AddInt32(&attempts, 1)
				if n == 1 {
					// Cancel context after first attempt
					cancel()
				}
				return fmt.Errorf("fail")
			},
		},
	}

	_, _, _, _, _ = s.Run(ctx, tasks)
	// Should have only 1 attempt because context is cancelled during RetryDelay.
	if atomic.LoadInt32(&attempts) > 2 {
		t.Errorf("expected at most 2 attempts (context cancel during delay), got %d", attempts)
	}
}

// TestMergeConcatenate verifies MergeConcatenate joins outputs with separator.
func TestMergeConcatenate(t *testing.T) {
	results := []swarm.SwarmResult{
		{AgentName: "A", Output: "alpha"},
		{AgentName: "B", Output: "beta"},
		{AgentName: "C", Output: "gamma"},
	}
	merged, err := swarm.MergeResults(results, swarm.MergeConcatenate, "\n", nil)
	if err != nil {
		t.Fatalf("MergeResults: %v", err)
	}
	if merged != "alpha\nbeta\ngamma" {
		t.Errorf("unexpected merge result: %q", merged)
	}
}

// TestMergeConcatenate_SkipsEmpty verifies empty outputs are excluded.
func TestMergeConcatenate_SkipsEmpty(t *testing.T) {
	results := []swarm.SwarmResult{
		{AgentName: "A", Output: "alpha"},
		{AgentName: "B", Output: ""},
		{AgentName: "C", Output: "gamma"},
	}
	merged, err := swarm.MergeResults(results, swarm.MergeConcatenate, "|", nil)
	if err != nil {
		t.Fatalf("MergeResults: %v", err)
	}
	if merged != "alpha|gamma" {
		t.Errorf("unexpected merge result: %q", merged)
	}
}

// TestMergeStructured verifies MergeStructured includes headers.
func TestMergeStructured(t *testing.T) {
	results := []swarm.SwarmResult{
		{AgentName: "Agent1", Output: "output1"},
		{AgentName: "Agent2", Output: "output2"},
		{AgentName: "Agent3", Output: "output3"},
	}
	merged, err := swarm.MergeResults(results, swarm.MergeStructured, "", nil)
	if err != nil {
		t.Fatalf("MergeResults: %v", err)
	}
	// MergeStructured now includes duration and cost: "=== AgentN (Xs, $Y) ==="
	for _, name := range []string{"Agent1", "Agent2", "Agent3"} {
		if !containsString(merged, "=== "+name+" (") {
			t.Errorf("expected header for %q in merged output", name)
		}
	}
}

// TestMergeLLMSummarize_NilFn verifies that MergeLLMSummarize returns error
// when mergeFn is nil.
func TestMergeLLMSummarize_NilFn(t *testing.T) {
	results := []swarm.SwarmResult{{AgentName: "A", Output: "x"}}
	_, err := swarm.MergeResults(results, swarm.MergeLLMSummarize, "", nil)
	if err == nil {
		t.Fatal("expected error for nil mergeFn")
	}
}

// TestMergeLLMSummarize_WithFn verifies that MergeLLMSummarize delegates to the fn.
func TestMergeLLMSummarize_WithFn(t *testing.T) {
	results := []swarm.SwarmResult{
		{AgentName: "A", Output: "a"},
		{AgentName: "B", Output: "b"},
	}
	merged, err := swarm.MergeResults(results, swarm.MergeLLMSummarize, "", func(rs []swarm.SwarmResult) (string, error) {
		return fmt.Sprintf("summarized %d results", len(rs)), nil
	})
	if err != nil {
		t.Fatalf("MergeResults: %v", err)
	}
	if merged != "summarized 2 results" {
		t.Errorf("unexpected result: %q", merged)
	}
}

// TestRunWithProgress_Callback verifies that the progress callback fires
// once per task with incrementing completed count.
func TestRunWithProgress_Callback(t *testing.T) {
	const n = 5
	s := swarm.NewSwarm(n)
	go func() { for range s.Events() {} }()

	tasks := make([]swarm.SwarmTask, n)
	for i := range tasks {
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("prog-%d", i),
			Name: fmt.Sprintf("ProgTask%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: "ok"})
				return nil
			},
		}
	}

	var maxCompleted int32
	var callCount int32
	results, _, err := s.RunWithProgress(context.Background(), tasks, func(completed, total int, latest swarm.SwarmResult) {
		atomic.AddInt32(&callCount, 1)
		c := int32(completed)
		for {
			old := atomic.LoadInt32(&maxCompleted)
			if c <= old || atomic.CompareAndSwapInt32(&maxCompleted, old, c) {
				break
			}
		}
		if total != n {
			t.Errorf("expected total=%d, got %d", n, total)
		}
	})
	if err != nil {
		t.Fatalf("RunWithProgress: %v", err)
	}
	if len(results) != n {
		t.Errorf("expected %d results, got %d", n, len(results))
	}
	if atomic.LoadInt32(&callCount) != int32(n) {
		t.Errorf("expected %d callback calls, got %d", n, callCount)
	}
	if atomic.LoadInt32(&maxCompleted) != int32(n) {
		t.Errorf("expected final completed=%d, got %d", n, maxCompleted)
	}
}

// TestRun_ReturnsSwarmResults verifies that the results slice length
// matches the task count and each result has the correct AgentID.
func TestRun_ReturnsSwarmResults(t *testing.T) {
	const n = 4
	s := swarm.NewSwarm(n)
	go func() { for range s.Events() {} }()

	tasks := make([]swarm.SwarmTask, n)
	for i := range tasks {
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("sr-%d", i),
			Name: fmt.Sprintf("SRTask%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return nil
			},
		}
	}

	results, _, _, _, err := s.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != n {
		t.Fatalf("expected %d results, got %d", n, len(results))
	}
	// Verify result order matches task order.
	for i, r := range results {
		expected := fmt.Sprintf("sr-%d", i)
		if r.AgentID != expected {
			t.Errorf("result[%d].AgentID = %q, want %q", i, r.AgentID, expected)
		}
	}
}

// TestSwarmResult_ErrorTask verifies that a failed task's result has Err set.
func TestSwarmResult_ErrorTask(t *testing.T) {
	s := swarm.NewSwarm(2)
	go func() { for range s.Events() {} }()

	sentinel := errors.New("task failed")
	tasks := []swarm.SwarmTask{
		{
			ID:   "err-res",
			Name: "ErrorRes",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return sentinel
			},
		},
	}

	results, _, _, _, err := s.Run(context.Background(), tasks)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !errors.Is(results[0].Err, sentinel) {
		t.Errorf("expected sentinel error in result, got %v", results[0].Err)
	}
}

// TestMergeUnknownStrategy verifies that an unknown strategy returns an error.
func TestMergeUnknownStrategy(t *testing.T) {
	_, err := swarm.MergeResults(nil, swarm.MergeStrategy(99), "", nil)
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}
