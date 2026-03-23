package backend

import (
	"testing"
	"time"
)

func TestCircuitBreaker_InitiallyClosed(t *testing.T) {
	cb := newCircuitBreaker()
	if !cb.Allow() {
		t.Error("new circuit breaker should allow requests")
	}
	if cb.State() != "closed" {
		t.Errorf("initial state: want closed, got %q", cb.State())
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := newCircuitBreaker()
	// Record failures up to threshold - 1; should still be closed.
	for i := 0; i < cbDefaultThreshold-1; i++ {
		cb.RecordFailure()
	}
	if cb.State() != "closed" {
		t.Errorf("before threshold: want closed, got %q", cb.State())
	}
	// One more failure should open it.
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Errorf("after threshold: want open, got %q", cb.State())
	}
	if cb.Allow() {
		t.Error("open circuit breaker should reject requests")
	}
}

func TestCircuitBreaker_SuccessResetsClosed(t *testing.T) {
	cb := newCircuitBreaker()
	for i := 0; i < cbDefaultThreshold; i++ {
		cb.RecordFailure()
	}
	if cb.State() != "open" {
		t.Fatalf("setup: want open, got %q", cb.State())
	}
	cb.RecordSuccess()
	if cb.State() != "closed" {
		t.Errorf("after success: want closed, got %q", cb.State())
	}
	if !cb.Allow() {
		t.Error("closed circuit breaker should allow requests")
	}
}

func TestCircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	cb := newCircuitBreaker()
	cb.threshold = 1
	cb.resetTimeout = 10 * time.Millisecond
	cb.RecordFailure() // opens immediately (threshold=1)
	if cb.State() != "open" {
		t.Fatalf("want open, got %q", cb.State())
	}

	// Before timeout: still open.
	if cb.Allow() {
		t.Error("should be blocked before reset timeout")
	}

	time.Sleep(20 * time.Millisecond) // past reset timeout

	// First Allow() after timeout transitions to half-open and returns true.
	if !cb.Allow() {
		t.Error("first Allow() after timeout should return true (probe request)")
	}
	if cb.State() != "half-open" {
		t.Errorf("after timeout Allow: want half-open, got %q", cb.State())
	}

	// Second concurrent Allow() in half-open is blocked.
	if cb.Allow() {
		t.Error("second Allow() in half-open should be blocked (only one probe)")
	}
}

func TestCircuitBreaker_HalfOpenProbeSuccess_ClosesBreakerAgain(t *testing.T) {
	cb := newCircuitBreaker()
	cb.threshold = 1
	cb.resetTimeout = 10 * time.Millisecond
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	cb.Allow() // transition to half-open

	cb.RecordSuccess()
	if cb.State() != "closed" {
		t.Errorf("after probe success: want closed, got %q", cb.State())
	}
	if !cb.Allow() {
		t.Error("closed breaker should allow after probe success")
	}
}

func TestCircuitBreaker_HalfOpenProbeFailure_ReOpens(t *testing.T) {
	cb := newCircuitBreaker()
	cb.threshold = 1
	cb.resetTimeout = 10 * time.Millisecond
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	cb.Allow() // transition to half-open

	cb.RecordFailure() // probe failed → re-open immediately
	if cb.State() != "open" {
		t.Errorf("after probe failure: want open, got %q", cb.State())
	}
	if cb.Allow() {
		t.Error("re-opened breaker should block requests")
	}
}

func TestCircuitBreaker_FailureCountResetOnOpen(t *testing.T) {
	cb := newCircuitBreaker()
	for i := 0; i < cbDefaultThreshold; i++ {
		cb.RecordFailure()
	}
	// Opening resets failure counter so half-open probes count from zero.
	cb.RecordSuccess() // close it
	// Failures before threshold should still be handled gracefully.
	for i := 0; i < cbDefaultThreshold-1; i++ {
		cb.RecordFailure()
	}
	if cb.State() != "closed" {
		t.Errorf("below threshold after re-close: want closed, got %q", cb.State())
	}
}
