package claudecode

import "sync"

// THE CROSS-TURN --session-id / --resume DECISION LIVES HERE.
//
// `claude --session-id X` is a ONE-SHOT: it can succeed at most once, on the
// very first launch of session X. The moment the CLI is launched with it the
// chance is spent, whether or not the process lived long enough to write
// anything. The two ways of being wrong are NOT symmetric:
//
// Wrongly passing --resume fails ONCE, loudly ("no conversation found"), and
// the agent can be pointed at a fresh session id. Wrongly passing --session-id
// fails PERMANENTLY, because the session really does exist and always will, so
// every future turn collides with it.
//
// Only the first is recoverable, so ambiguity must resolve toward --resume.
//
// AgentBackend used to record that on the instance (markSessionExists), but the
// backend is REBUILT EVERY TURN — deliberately, so the system prompt
// reassembles from the agent's current prompt, skills and notepad. Instance
// state therefore never survived a turn, and the cross-turn answer fell back
// to a heuristic disk probe alone. When that probe was wrong (an unflushed
// transcript, a transcript root the probe did not know about) the agent
// re-claimed an id the CLI already owned and broke permanently — the exact
// unrecoverable direction the reasoning above forbids.
//
// So the record lives at PACKAGE level, keyed by session id, exactly like the
// turn semaphore in session_sem.go and for exactly the same reason: the fact
// outlives any single backend instance.
//
// Scope, stated plainly: this is per-PROCESS. It is not persisted, and it does
// not need to be. Its job is to make the answer durable across the turns of one
// running server, which is where instance state failed. After a restart the set
// is empty and the disk probe answers instead — and the two are combined with
// OR, never AND, so ANY evidence that the session exists resolves to --resume.
//
// Size is bounded by the number of distinct Claude Code sessions one server
// process drives — in practice the number of claude-code agents configured —
// and each entry is one session-id string. Nothing is ever removed, because a
// session that exists never stops existing.
var startedSessions = struct {
	mu sync.Mutex
	m  map[string]struct{}
}{m: map[string]struct{}{}}

// markSessionStarted records that this session's one --session-id chance has
// been spent. Monotonic; nothing ever clears it.
//
// An empty session id is never recorded: a backend with no session id starts a
// fresh CLI session every turn and shares nothing with anyone, so remembering
// "" would make every such backend resume a conversation that does not exist.
func markSessionStarted(sessionID string) {
	if sessionID == "" {
		return
	}
	startedSessions.mu.Lock()
	startedSessions.m[sessionID] = struct{}{}
	startedSessions.mu.Unlock()
}

// sessionStarted reports whether this process has already launched the CLI for
// this session id.
func sessionStarted(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	startedSessions.mu.Lock()
	_, ok := startedSessions.m[sessionID]
	startedSessions.mu.Unlock()
	return ok
}
