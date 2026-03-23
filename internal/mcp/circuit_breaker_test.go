package mcp

// circuit_breaker_test.go tests the consecutive-failure circuit breaker added
// to managedServer / ServerManager.  These tests run in the internal package so
// they can access unexported fields directly.

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// cbStateStr returns a human-readable string for a CB state integer.
func cbStateStr(s int) string {
	switch s {
	case cbClosed:
		return "closed"
	case cbOpen:
		return "open"
	case cbHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// newCBTestManager creates a bare ServerManager for unit testing CB helpers.
func newCBTestManager() *ServerManager {
	return &ServerManager{}
}

// ---------------------------------------------------------------------------
// Unit tests for cbShouldAllow / cbRecordFailure / cbRecordSuccess helpers
// ---------------------------------------------------------------------------

// TestCB_ClosedToOpen verifies that cbFailureThreshold consecutive failures trip the circuit.
func TestCB_ClosedToOpen(t *testing.T) {
	mgr := newCBTestManager()
	ms := &managedServer{cfg: MCPServerConfig{Name: "test"}}

	// Record cbFailureThreshold-1 failures — circuit must remain closed.
	for i := 0; i < cbFailureThreshold-1; i++ {
		mgr.cbRecordFailure(ms)
		if ms.cbState != cbClosed {
			t.Fatalf("after %d failures expected closed, got %s", i+1, cbStateStr(ms.cbState))
		}
	}
	// One more failure should open the circuit.
	mgr.cbRecordFailure(ms)
	if ms.cbState != cbOpen {
		t.Fatalf("expected open after %d failures, got %s", cbFailureThreshold, cbStateStr(ms.cbState))
	}
	if ms.cbOpenAt.IsZero() {
		t.Fatal("cbOpenAt must be set when transitioning to open")
	}
}

// TestCB_OpenBlocksAttempts verifies that cbShouldAllow returns false
// when the circuit is open and the open period has not expired.
func TestCB_OpenBlocksAttempts(t *testing.T) {
	mgr := newCBTestManager()
	ms := &managedServer{
		cfg:      MCPServerConfig{Name: "test"},
		cbState:  cbOpen,
		cbOpenAt: time.Now().Add(30 * time.Second), // still open (won't expire yet)
	}

	allow := mgr.cbShouldAllow(ms)
	if allow {
		t.Fatal("expected cbShouldAllow to return false when circuit is open")
	}
}

// TestCB_OpenToHalfOpen verifies that once the open period expires,
// cbShouldAllow transitions the circuit to half-open and allows a probe.
func TestCB_OpenToHalfOpen(t *testing.T) {
	mgr := newCBTestManager()
	ms := &managedServer{
		cfg:      MCPServerConfig{Name: "test"},
		cbState:  cbOpen,
		cbOpenAt: time.Now().Add(-cbOpenDuration - time.Millisecond), // already expired
	}

	allow := mgr.cbShouldAllow(ms)
	if !allow {
		t.Fatal("expected allow=true after open period expires")
	}
	if ms.cbState != cbHalfOpen {
		t.Fatalf("expected half-open after expired open period, got %s", cbStateStr(ms.cbState))
	}
}

// TestCB_HalfOpenSuccessCloses verifies that a successful attempt in half-open
// state transitions the circuit back to closed.
func TestCB_HalfOpenSuccessCloses(t *testing.T) {
	mgr := newCBTestManager()
	ms := &managedServer{
		cfg:      MCPServerConfig{Name: "test"},
		cbState:  cbHalfOpen,
		cbOpenAt: time.Now().Add(-time.Millisecond),
	}

	mgr.cbRecordSuccess(ms)
	if ms.cbState != cbClosed {
		t.Fatalf("expected closed after success in half-open, got %s", cbStateStr(ms.cbState))
	}
	if ms.cbFailures != 0 {
		t.Fatalf("expected cbFailures=0 after success, got %d", ms.cbFailures)
	}
}

// TestCB_HalfOpenFailureReopens verifies that a failed attempt in half-open
// state transitions the circuit back to open.
func TestCB_HalfOpenFailureReopens(t *testing.T) {
	mgr := newCBTestManager()
	ms := &managedServer{
		cfg:      MCPServerConfig{Name: "test"},
		cbState:  cbHalfOpen,
		cbOpenAt: time.Now().Add(-time.Millisecond),
	}

	mgr.cbRecordFailure(ms)
	if ms.cbState != cbOpen {
		t.Fatalf("expected open after failure in half-open, got %s", cbStateStr(ms.cbState))
	}
	if ms.cbOpenAt.IsZero() {
		t.Fatal("cbOpenAt must be set when re-opening from half-open")
	}
}

// TestCB_SuccessResetFailureCount verifies that a success resets the failure counter
// so the circuit stays closed after intermittent failures below the threshold.
func TestCB_SuccessResetFailureCount(t *testing.T) {
	mgr := newCBTestManager()
	ms := &managedServer{cfg: MCPServerConfig{Name: "test"}}

	// Record cbFailureThreshold-1 failures (one below threshold) then a success.
	for i := 0; i < cbFailureThreshold-1; i++ {
		mgr.cbRecordFailure(ms)
	}
	mgr.cbRecordSuccess(ms)

	// Circuit should remain closed and failures zeroed.
	if ms.cbState != cbClosed {
		t.Fatalf("expected closed after success, got %s", cbStateStr(ms.cbState))
	}
	if ms.cbFailures != 0 {
		t.Fatalf("expected 0 failures after success, got %d", ms.cbFailures)
	}

	// Additional failures from zero should not open until threshold again.
	for i := 0; i < cbFailureThreshold-1; i++ {
		mgr.cbRecordFailure(ms)
	}
	if ms.cbState != cbClosed {
		t.Fatalf("expected closed after %d failures post-reset, got %s",
			cbFailureThreshold-1, cbStateStr(ms.cbState))
	}
}

// TestCB_ErrCircuitOpenSentinel verifies the exported error value is correct.
func TestCB_ErrCircuitOpenSentinel(t *testing.T) {
	if ErrCircuitOpen == nil {
		t.Fatal("ErrCircuitOpen must not be nil")
	}
	if ErrCircuitOpen.Error() == "" {
		t.Fatal("ErrCircuitOpen must have a non-empty message")
	}
	// Must be usable with errors.Is.
	wrapped := fmt.Errorf("outer: %w", ErrCircuitOpen)
	if !errors.Is(wrapped, ErrCircuitOpen) {
		t.Fatal("errors.Is(wrapped, ErrCircuitOpen) must return true")
	}
}
