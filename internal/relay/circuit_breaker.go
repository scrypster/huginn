package relay

import (
	"log/slog"
	"sync"
	"time"
)

// cbState is the three-state lifecycle of a CircuitBreaker.
type cbState int

const (
	cbClosed   cbState = iota // normal — dial attempts are allowed
	cbOpen                    // tripped — dial attempts are blocked until openUntil
	cbHalfOpen                // probing — one dial attempt allowed; success resets, failure re-opens
)

// cbThreshold is the number of consecutive dial+hello failures required to
// trip the circuit from Closed to Open. Value chosen to avoid tripping on
// transient network hiccups while still protecting against sustained outages.
const cbThreshold = 8

// cbOpenDuration is how long the circuit stays Open before transitioning to
// HalfOpen and allowing one probe attempt.
const cbOpenDuration = 120 * time.Second

// CircuitBreaker guards the relay reconnect loop against prolonged connection
// storms. It implements the standard three-state pattern:
//
//	Closed  → Open    : after cbThreshold consecutive failures
//	Open    → HalfOpen: after cbOpenDuration
//	HalfOpen → Closed : on the first successful dial
//	HalfOpen → Open   : on a failed dial (re-opens the circuit)
//
// All methods are safe for concurrent use.
type CircuitBreaker struct {
	mu          sync.Mutex
	state       cbState
	failures    int
	openUntil   time.Time
}

// Allow reports whether a dial attempt should proceed. It transitions
// Open → HalfOpen when the open window has expired.
// Returns false while the circuit is Open.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case cbClosed:
		return true
	case cbOpen:
		if time.Now().After(cb.openUntil) {
			cb.state = cbHalfOpen
			return true
		}
		return false
	case cbHalfOpen:
		// Only one probe is allowed at a time; subsequent callers are blocked.
		return false
	}
	return false
}

// RecordSuccess records a successful dial+hello. Resets failure counter and
// transitions HalfOpen → Closed (or keeps Closed).
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = cbClosed
}

// RecordFailure records a failed dial attempt. When the failure count reaches
// cbThreshold the circuit transitions to Open.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.state == cbHalfOpen || cb.failures >= cbThreshold {
		cb.state = cbOpen
		cb.openUntil = time.Now().Add(cbOpenDuration)
		cb.failures = 0 // reset counter so the next burst gets a fresh budget
		// Log inside the lock: slog is concurrency-safe and this transition
		// happens at most once per failure burst, so lock contention is negligible.
		slog.Warn("relay: circuit breaker opened — relay reconnect paused",
			"consecutive_failures", cbThreshold,
			"retry_after", cb.openUntil.Format(time.RFC3339),
		)
	}
}

// State returns the current circuit breaker state as a human-readable string.
// Return values are "closed", "open", or "half_open".
// All states are safe for concurrent use.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbOpen:
		return "open"
	case cbHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}

// Reset returns the circuit to Closed with a zeroed failure counter.
// Called by ResetBackoff (triggered by detected wake events, user reconnect).
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = cbClosed
	cb.failures = 0
	cb.openUntil = time.Time{}
}
