package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

// maxSessionHistory is the maximum number of messages retained in a session's
// in-memory history. When the limit is exceeded, the oldest non-system messages
// are trimmed to prevent unbounded memory growth and context-window overflow.
const maxSessionHistory = 100

// Session holds all state for a single conversation session.
// Each session has its own mutex so concurrent sessions don't block each other.
type Session struct {
	ID       string
	mu       sync.Mutex        // guards state, history, lastUsed, running
	state    State
	history  []backend.Message
	lastUsed time.Time         // updated on every access; used for idle-TTL eviction
	running  int               // number of active goroutines using this session

	// compactMu serializes background history compaction for this session so
	// two async compactions can never race each other (compactHistoryAsync
	// holds it for the duration of a compaction). snapshotHistory waits on it
	// so the *next* turn always observes fully-compacted history, while the
	// turn that triggered the compaction never has to wait on it itself.
	compactMu sync.Mutex

	// idleCh is created when a run begins; closed when running drops to 0.
	// WaitForIdle receives on this channel to be notified of idleness.
	idleCh chan struct{}

	// swarmCancel and swarmDone support in-progress swarm cancellation.
	swarmCancel context.CancelFunc
	swarmDone   chan struct{}
}

// newSession creates a new Session with the given ID in the idle state.
func newSession(id string) *Session {
	return &Session{
		ID:       id,
		state:    StateIdle,
		lastUsed: time.Now(),
	}
}

// setState sets the session's state under the session lock.
func (s *Session) setState(st State) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
}

// getState returns the session's current state under the session lock.
func (s *Session) getState() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// snapshotHistory returns a copy of the session's history under the session lock.
// It first waits for any in-flight background compaction (compactHistoryAsync)
// to finish, so callers always see compacted history rather than racing ahead
// of it — the wait lands on the *next* turn, never the turn that scheduled
// the compaction (which has already returned by the time compaction starts).
func (s *Session) snapshotHistory() []backend.Message {
	s.compactMu.Lock()
	s.compactMu.Unlock() //nolint:staticcheck // deliberate lock/unlock as a wait barrier
	return s.snapshotHistoryRaw()
}

// snapshotHistoryRaw returns a copy of the session's history under the
// session lock WITHOUT waiting on compactMu. Only compactHistory (which runs
// while already holding compactMu, directly or via compactHistoryAsync) may
// call this — snapshotHistory would self-deadlock there since sync.Mutex is
// not reentrant.
func (s *Session) snapshotHistoryRaw() []backend.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]backend.Message, len(s.history))
	copy(cp, s.history)
	return cp
}

// appendHistory appends messages to the session's history under the session lock.
// After appending, if the total exceeds maxSessionHistory, the oldest non-system
// messages are trimmed to keep memory bounded. The first message (system prompt,
// if present) is always preserved.
func (s *Session) appendHistory(msgs ...backend.Message) {
	s.mu.Lock()
	s.history = append(s.history, msgs...)
	if len(s.history) > maxSessionHistory {
		// Preserve the system message at index 0 (if present), then keep only
		// the most recent maxSessionHistory-1 non-system messages.
		if len(s.history) > 0 && s.history[0].Role == "system" {
			keep := s.history[len(s.history)-(maxSessionHistory-1):]
			trimmed := make([]backend.Message, 1, maxSessionHistory)
			trimmed[0] = s.history[0]
			trimmed = append(trimmed, keep...)
			slog.Debug("agent: session history trimmed",
				"session_id", s.ID,
				"before", len(s.history),
				"after", len(trimmed))
			s.history = trimmed
		} else {
			keep := s.history[len(s.history)-maxSessionHistory:]
			slog.Debug("agent: session history trimmed (no system msg)",
				"session_id", s.ID,
				"before", len(s.history),
				"after", len(keep))
			s.history = keep
		}
	}
	s.mu.Unlock()
}

// replaceHistory replaces the session's history atomically under the session lock.
func (s *Session) replaceHistory(msgs []backend.Message) {
	s.mu.Lock()
	s.history = msgs
	s.mu.Unlock()
}

// touchLastUsed updates lastUsed to the current time under the session lock.
func (s *Session) touchLastUsed() {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

// getLastUsed returns the last-used timestamp under the session lock.
func (s *Session) getLastUsed() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastUsed
}

// incRunning increments the running counter and updates lastUsed.
func (s *Session) incRunning() {
	s.mu.Lock()
	s.running++
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

// decRunning decrements the running counter and updates lastUsed.
func (s *Session) decRunning() {
	s.mu.Lock()
	s.running--
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

// isRunning returns true when at least one goroutine is actively using the session.
func (s *Session) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running > 0
}

// tryBeginRun atomically claims the session's exclusive run slot.
// Returns true if this caller now owns the slot; false if already running.
// On success the caller MUST call endRun when the run completes.
func (s *Session) tryBeginRun() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running > 0 {
		return false
	}
	s.running++
	s.lastUsed = time.Now()
	s.idleCh = make(chan struct{})
	return true
}

// endRun releases the exclusive run slot. Must be called exactly once after
// a successful tryBeginRun. Closes idleCh so WaitForIdle unblocks.
func (s *Session) endRun() {
	s.mu.Lock()
	s.running--
	s.lastUsed = time.Now()
	ch := s.idleCh
	if s.running == 0 {
		s.idleCh = nil
	}
	s.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// setActiveSwarm stores the cancel function and done channel for an in-progress swarm.
func (s *Session) setActiveSwarm(cancel context.CancelFunc, done chan struct{}) {
	s.mu.Lock()
	s.swarmCancel = cancel
	s.swarmDone = done
	s.mu.Unlock()
}

// clearActiveSwarm clears the stored swarm cancel/done fields.
func (s *Session) clearActiveSwarm() {
	s.mu.Lock()
	s.swarmCancel = nil
	s.swarmDone = nil
	s.mu.Unlock()
}

// cancelSwarm cancels an in-progress swarm and returns the done channel.
// Returns nil if no swarm is active.
func (s *Session) cancelSwarm() <-chan struct{} {
	s.mu.Lock()
	cancel := s.swarmCancel
	done := s.swarmDone
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return done
}

// WaitForIdle blocks until the session has no active run (running == 0) or
// until ctx is done. Returns true if the session became idle; false on ctx timeout.
// If the session is already idle when called, returns true immediately.
func (s *Session) WaitForIdle(ctx context.Context) bool {
	s.mu.Lock()
	if s.running == 0 {
		s.mu.Unlock()
		return true
	}
	ch := s.idleCh
	s.mu.Unlock()

	if ch == nil {
		return true
	}
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}

// RunQueueWaitCeiling is how long a single beginExclusiveRun attempt waits
// for a busy session's exclusive run slot before giving up FOR THAT
// ATTEMPT and returning false. This is NOT a hard failure ceiling — a
// queued turn behind a genuinely slow predecessor (a slow local model can
// take minutes) is expected to keep retrying past it (see ChatWithAgent's
// caller loop in runWSChat), never to persist an error just because one
// wait attempt elapsed. Exported var (not const) so tests can shrink it to
// exercise the retry path without waiting 120s.
var RunQueueWaitCeiling = 120 * time.Second

// beginExclusiveRun claims the exclusive run slot, waiting if another run
// is in progress. Returns true if this caller now owns the slot (caller
// MUST endRun). Returns false if ctx expires first.
func (s *Session) beginExclusiveRun(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, RunQueueWaitCeiling)
		defer cancel()
	}
	for {
		if s.tryBeginRun() {
			return true
		}
		if !s.WaitForIdle(ctx) {
			return false
		}
	}
}
