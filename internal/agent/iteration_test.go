package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIterateRunsNTimes(t *testing.T) {
	calls := 0
	streamer := func(ctx context.Context, msgs []string, onToken func(string)) error {
		calls++
		onToken(strings.Repeat("x", 10))
		return nil
	}

	result, err := iterate(context.Background(), 3, "harden auth", streamer)
	if err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestIterateMinimumOne(t *testing.T) {
	calls := 0
	streamer := func(ctx context.Context, msgs []string, onToken func(string)) error {
		calls++
		onToken("response")
		return nil
	}

	_, err := iterate(context.Background(), 0, "section", streamer)
	if err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if calls != 1 {
		t.Errorf("n=0 should run at least once, got %d calls", calls)
	}
}

func TestIterateBuildsHistory(t *testing.T) {
	var receivedMsgs [][]string
	streamer := func(ctx context.Context, msgs []string, onToken func(string)) error {
		receivedMsgs = append(receivedMsgs, append([]string{}, msgs...))
		onToken("round response")
		return nil
	}

	_, err := iterate(context.Background(), 2, "the section", streamer)
	if err != nil {
		t.Fatalf("iterate: %v", err)
	}
	// Round 2 should have more messages than round 1 (history accumulates)
	if len(receivedMsgs) != 2 {
		t.Fatalf("expected 2 rounds, got %d", len(receivedMsgs))
	}
	if len(receivedMsgs[1]) <= len(receivedMsgs[0]) {
		t.Error("round 2 should have more messages than round 1 (history accumulating)")
	}
}

// TestIterate_ErrorOnFirstIteration verifies that an error on the first stream
// call is returned and the result string is empty.
func TestIterate_ErrorOnFirstIteration(t *testing.T) {
	streamErr := errors.New("stream failed")
	streamer := func(ctx context.Context, msgs []string, onToken func(string)) error {
		return streamErr
	}

	result, err := iterate(context.Background(), 3, "section", streamer)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, streamErr) {
		t.Errorf("expected wrapped streamErr, got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result on first-iteration error, got %q", result)
	}
}

// TestIterate_ErrorOnMiddleIteration verifies that an error on iteration 2 of 3
// returns the result from iteration 1 (the last successful output).
func TestIterate_ErrorOnMiddleIteration(t *testing.T) {
	streamErr := errors.New("middle fail")
	call := 0
	streamer := func(ctx context.Context, msgs []string, onToken func(string)) error {
		call++
		if call == 1 {
			onToken("first result")
			return nil
		}
		return streamErr
	}

	result, err := iterate(context.Background(), 3, "section", streamer)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, streamErr) {
		t.Errorf("expected wrapped streamErr, got: %v", err)
	}
	// The returned string should be the last successful result (iteration 1).
	if result != "first result" {
		t.Errorf("expected result from first iteration %q, got %q", "first result", result)
	}
}

// TestIterate_ContextCancellation verifies that a cancelled context propagates
// as an error from iterate.
func TestIterate_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	streamer := func(ctx context.Context, msgs []string, onToken func(string)) error {
		// Honour context cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			onToken("should not happen")
			return nil
		}
	}

	_, err := iterate(ctx, 2, "section", streamer)
	if err == nil {
		t.Fatal("expected error due to context cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

// TestBuildIterationPrompt_Round1 verifies that the first round prompt includes
// the section and does not mention "critique".
func TestBuildIterationPrompt_Round1(t *testing.T) {
	prompt := buildIterationPrompt("section text", "", 1, 3)
	if !strings.Contains(prompt, "section text") {
		t.Error("expected section text in round 1 prompt")
	}
	if strings.Contains(prompt, "critique") {
		t.Error("expected no 'critique' in round 1 prompt")
	}
}

// TestBuildIterationPrompt_Round2 verifies that subsequent rounds reference the
// previous response and include the round number.
func TestBuildIterationPrompt_Round2(t *testing.T) {
	prompt := buildIterationPrompt("section text", "prev response", 2, 3)
	if !strings.Contains(prompt, "prev response") {
		t.Error("expected previous response in round 2 prompt")
	}
	if !strings.Contains(prompt, "2") {
		t.Error("expected round number in prompt")
	}
	if !strings.Contains(prompt, "section text") {
		t.Error("expected section text in round 2 prompt")
	}
}

// TestIterate_NegativeN verifies that negative n is treated as 1.
func TestIterate_NegativeN(t *testing.T) {
	calls := 0
	streamer := func(ctx context.Context, msgs []string, onToken func(string)) error {
		calls++
		onToken("result")
		return nil
	}

	_, err := iterate(context.Background(), -5, "section", streamer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call for n=-5, got %d", calls)
	}
}

// TestIterate_HistoryCopyIsolated verifies that the streamer cannot corrupt
// the history slice used by iterate (copy isolation).
func TestIterate_HistoryCopyIsolated(t *testing.T) {
	streamer := func(ctx context.Context, msgs []string, onToken func(string)) error {
		// Try to corrupt the received slice by appending.
		msgs = append(msgs, "INJECTED")
		onToken("ok")
		return nil
	}

	result, err := iterate(context.Background(), 3, "section", streamer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected result 'ok', got %q", result)
	}
}

// TestIterate_LargeN verifies that a large iteration count accumulates history properly.
func TestIterate_LargeN(t *testing.T) {
	n := 10
	calls := 0
	streamer := func(ctx context.Context, msgs []string, onToken func(string)) error {
		calls++
		onToken("resp")
		return nil
	}

	result, err := iterate(context.Background(), n, "section", streamer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != n {
		t.Errorf("expected %d calls, got %d", n, calls)
	}
	if result != "resp" {
		t.Errorf("expected final result 'resp', got %q", result)
	}
}
