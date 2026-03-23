package backend

import (
	"fmt"
	"sync"
	"time"
)

// circuitState represents the three states of a circuit breaker.
type circuitState int

const (
	circuitClosed   circuitState = iota // normal operation, requests flow through
	circuitOpen                         // upstream is degraded, requests are rejected
	circuitHalfOpen                     // probe mode: one request allowed to test recovery
)

const (
	cbDefaultThreshold    = 5               // consecutive failures before opening
	cbDefaultResetTimeout = 30 * time.Second // wait before moving open → half-open
)

// circuitBreaker tracks consecutive backend failures and short-circuits
// requests when the upstream appears degraded. It implements the classic
// closed → open → half-open → closed state machine.
//
// Thread-safety: all methods are safe for concurrent use.
type circuitBreaker struct {
	mu           sync.Mutex
	state        circuitState
	failures     int       // consecutive failures in the closed state
	threshold    int       // failure count that triggers open
	resetTimeout time.Duration
	openedAt     time.Time // when the breaker last opened
}

func newCircuitBreaker() *circuitBreaker {
	return &circuitBreaker{
		threshold:    cbDefaultThreshold,
		resetTimeout: cbDefaultResetTimeout,
	}
}

// Allow returns true if a request should be attempted.
// In half-open state, only one concurrent probe is allowed; subsequent callers
// see the breaker as open until the probe completes.
func (cb *circuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case circuitClosed:
		return true
	case circuitOpen:
		if time.Since(cb.openedAt) >= cb.resetTimeout {
			// Transition to half-open to probe recovery.
			cb.state = circuitHalfOpen
			return true
		}
		return false
	case circuitHalfOpen:
		// Block concurrent probes: only the first caller gets through.
		return false
	}
	return false
}

// RecordSuccess transitions the breaker to closed and resets the failure count.
func (cb *circuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = circuitClosed
}

// RecordFailure increments the failure counter. When the threshold is reached
// the breaker opens. Half-open failures immediately re-open.
func (cb *circuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.state == circuitHalfOpen || cb.failures >= cb.threshold {
		cb.state = circuitOpen
		cb.openedAt = time.Now()
		cb.failures = 0
	}
}

// State returns the current circuit state as a human-readable string.
func (cb *circuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case circuitOpen:
		return "open"
	case circuitHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = fmt.Errorf("chat completion: circuit breaker open (upstream degraded)")
