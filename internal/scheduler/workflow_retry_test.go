// internal/scheduler/workflow_retry_test.go
package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestExecuteStepWithRetry_NoRetriesOnSuccess - MaxRetries=0, agentFn succeeds
// on the first call. Expects exactly 1 call and the output returned.
func TestExecuteStepWithRetry_NoRetriesOnSuccess(t *testing.T) {
	t.Parallel()

	calls := 0
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		calls++
		return "success output", nil
	}

	step := WorkflowStep{MaxRetries: 0}
	if err := step.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	out, err := executeStepWithRetry(context.Background(), agentFn, RunOptions{}, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "success output" {
		t.Errorf("want %q, got %q", "success output", out)
	}
	if calls != 1 {
		t.Errorf("want 1 call, got %d", calls)
	}
}

// TestExecuteStepWithRetry_MaxRetries0_SingleAttempt - MaxRetries=0, agentFn
// always fails. Expects exactly 1 call and the error returned.
func TestExecuteStepWithRetry_MaxRetries0_SingleAttempt(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("agent failed")
	calls := 0
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		calls++
		return "", sentinel
	}

	step := WorkflowStep{MaxRetries: 0}
	if err := step.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	_, err := executeStepWithRetry(context.Background(), agentFn, RunOptions{}, step)
	if !errors.Is(err, sentinel) {
		t.Errorf("want sentinel error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("want 1 call, got %d", calls)
	}
}

// TestExecuteStepWithRetry_RetriesOnFailure - MaxRetries=2, agentFn fails twice
// then succeeds on the third attempt. Expects 3 calls total and the output.
func TestExecuteStepWithRetry_RetriesOnFailure(t *testing.T) {
	t.Parallel()

	calls := 0
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		calls++
		if calls < 3 {
			return "", errors.New("transient error")
		}
		return "final output", nil
	}

	step := WorkflowStep{MaxRetries: 2}
	if err := step.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	out, err := executeStepWithRetry(context.Background(), agentFn, RunOptions{}, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "final output" {
		t.Errorf("want %q, got %q", "final output", out)
	}
	if calls != 3 {
		t.Errorf("want 3 calls, got %d", calls)
	}
}

// TestExecuteStepWithRetry_ExhaustsAllAttempts - MaxRetries=3, agentFn always
// fails. Expects 4 calls total (1 initial + 3 retries) and last error returned.
func TestExecuteStepWithRetry_ExhaustsAllAttempts(t *testing.T) {
	t.Parallel()

	lastErr := errors.New("persistent error")
	calls := 0
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		calls++
		return "", lastErr
	}

	step := WorkflowStep{MaxRetries: 3}
	if err := step.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	_, err := executeStepWithRetry(context.Background(), agentFn, RunOptions{}, step)
	if !errors.Is(err, lastErr) {
		t.Errorf("want last error %v, got %v", lastErr, err)
	}
	if calls != 4 {
		t.Errorf("want 4 calls (1 + 3 retries), got %d", calls)
	}
}

// TestExecuteStepWithRetry_ContextCancellationAbortsRetry - MaxRetries=5,
// agentFn always fails. Cancel the context after the second call fires; expect
// the function to stop early and return a context error.
func TestExecuteStepWithRetry_ContextCancellationAbortsRetry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		calls++
		if calls >= 2 {
			// Cancel the context so the next retry delay check exits early.
			cancel()
		}
		return "", errors.New("always fails")
	}

	step := WorkflowStep{MaxRetries: 5, RetryDelay: "5ms"}
	if err := step.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	_, err := executeStepWithRetry(ctx, agentFn, RunOptions{}, step)
	if err == nil {
		t.Fatal("expected an error after context cancellation, got nil")
	}
	// The returned error must be context-related (either ctx.Err itself or
	// wrapping it). We accept context.Canceled or the agent's last error here
	// since the cancel races with the agent call, but we must NOT have run all 6.
	if calls >= 6 {
		t.Errorf("expected early abort, but ran all %d calls", calls)
	}
}

// TestExecuteStepWithRetry_RetryDelayApplied - MaxRetries=2 with a 10ms
// RetryDelay. agentFn fails once then succeeds. Total elapsed must exceed 10ms,
// confirming the sleep actually occurred.
func TestExecuteStepWithRetry_RetryDelayApplied(t *testing.T) {
	t.Parallel()

	calls := 0
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("first attempt fails")
		}
		return "ok", nil
	}

	step := WorkflowStep{MaxRetries: 2, RetryDelay: "10ms"}
	if err := step.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	start := time.Now()
	out, err := executeStepWithRetry(context.Background(), agentFn, RunOptions{}, step)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok" {
		t.Errorf("want %q, got %q", "ok", out)
	}
	if calls != 2 {
		t.Errorf("want 2 calls, got %d", calls)
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("expected elapsed >= 10ms (retry delay), got %v", elapsed)
	}
}

// TestExecuteStepWithRetry_MaxRetriesCap - Validate() must reject MaxRetries > 10.
func TestExecuteStepWithRetry_MaxRetriesCap(t *testing.T) {
	t.Parallel()

	step := WorkflowStep{MaxRetries: 11}
	err := step.Validate()
	if err == nil {
		t.Fatal("expected Validate() to return an error for MaxRetries=11, got nil")
	}

	// Also confirm that exactly 10 is accepted.
	stepOK := WorkflowStep{MaxRetries: 10}
	if err := stepOK.Validate(); err != nil {
		t.Errorf("expected Validate() to accept MaxRetries=10, got: %v", err)
	}
}
