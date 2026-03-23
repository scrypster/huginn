package agent

import (
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
)

// newTestOrch creates a new Orchestrator for eviction tests.
func newTestOrch(t *testing.T) *Orchestrator {
	t.Helper()
	return mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)
}

// TestEvictIdleSessions_evictsStaleNonDefault verifies that sessions idle for longer
// than the configured TTL are removed from the orchestrator's sessions map.
func TestEvictIdleSessions_evictsStaleNonDefault(t *testing.T) {
	o := newTestOrch(t)

	// Create a non-default session that is already "stale".
	sess, err := o.NewSession("")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	// Wind back lastUsed so it appears old.
	sess.mu.Lock()
	sess.lastUsed = time.Now().Add(-3 * time.Hour)
	sess.mu.Unlock()

	// Verify the session exists before eviction.
	if _, ok := o.GetSession(sess.ID); !ok {
		t.Fatal("session should exist before eviction")
	}

	// Reset lastUsed after GetSession touched it.
	sess.mu.Lock()
	sess.lastUsed = time.Now().Add(-3 * time.Hour)
	sess.mu.Unlock()

	o.evictIdleSessions(1 * time.Hour)

	if _, ok := o.sessions[sess.ID]; ok {
		t.Fatal("stale session should have been evicted")
	}
}

// TestEvictIdleSessions_preservesDefaultSession verifies that the default session is
// never evicted, even if it appears idle.
func TestEvictIdleSessions_preservesDefaultSession(t *testing.T) {
	o := newTestOrch(t)

	// Wind back the default session's lastUsed timestamp.
	o.mu.Lock()
	defSess := o.sessions[o.defaultSessionID]
	o.mu.Unlock()

	defSess.mu.Lock()
	defSess.lastUsed = time.Now().Add(-10 * time.Hour)
	defSess.mu.Unlock()

	o.evictIdleSessions(1 * time.Minute)

	o.mu.Lock()
	_, ok := o.sessions[o.defaultSessionID]
	o.mu.Unlock()
	if !ok {
		t.Fatal("default session must never be evicted")
	}
}

// TestEvictIdleSessions_preservesActiveSession verifies that an active session
// (running > 0) is never evicted regardless of idle duration.
func TestEvictIdleSessions_preservesActiveSession(t *testing.T) {
	o := newTestOrch(t)

	sess, err := o.NewSession("")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Mark session as running.
	sess.incRunning()

	// Wind back lastUsed to look stale.
	sess.mu.Lock()
	sess.lastUsed = time.Now().Add(-5 * time.Hour)
	sess.mu.Unlock()

	o.evictIdleSessions(1 * time.Minute)

	o.mu.Lock()
	_, ok := o.sessions[sess.ID]
	o.mu.Unlock()
	if !ok {
		t.Fatal("active session must not be evicted")
	}

	// Cleanup: decrement running so the session is properly idle.
	sess.decRunning()
}

// TestEvictIdleSessions_keepsRecentSession verifies that a recently-used session is
// not evicted even when eviction runs.
func TestEvictIdleSessions_keepsRecentSession(t *testing.T) {
	o := newTestOrch(t)

	sess, err := o.NewSession("")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	// Session was just created (lastUsed = now).

	o.evictIdleSessions(2 * time.Hour)

	o.mu.Lock()
	_, ok := o.sessions[sess.ID]
	o.mu.Unlock()
	if !ok {
		t.Fatal("recently-used session should not be evicted")
	}
}
