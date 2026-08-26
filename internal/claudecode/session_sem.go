package claudecode

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
)

// Turns are serialised per Claude Code SESSION, not per backend instance.
//
// The backend is rebuilt for every turn — deliberately, so the system prompt
// is reassembled from the agent's current prompt, skills and notepad — which
// means a semaphore stored on the instance guards nothing: two concurrent
// turns for one agent each get their own instance, their own empty channel,
// and run two `claude` processes against the SAME session id. The transcript
// is that session's only source of truth, so two writers corrupt it.
//
// Hence a package-level registry keyed by session id, with the slot outliving
// any single backend.
type sessionSem struct {
	ch chan struct{}
	// refs counts holders AND waiters, so an entry is only dropped when nobody
	// can still be using it. Without it the map grows one entry per session
	// for the life of the process.
	refs int
}

var sessionSems = struct {
	mu sync.Mutex
	m  map[string]*sessionSem
}{m: map[string]*sessionSem{}}

// semInstanceSeq names the private slot handed to a backend with no session
// id. Such a turn does not resume anything — the CLI starts a fresh session —
// so it shares no transcript with anyone and must not queue behind unrelated
// agents. Collapsing every empty id onto one key would serialise the whole
// server through a single slot.
var semInstanceSeq atomic.Uint64

// semKeyFor returns the serialisation key for a session id. An empty id gets a
// unique private key instead of the shared "" bucket.
func semKeyFor(sessionID string) string {
	if sessionID == "" {
		return "instance:" + strconv.FormatUint(semInstanceSeq.Add(1), 10)
	}
	return "session:" + sessionID
}

// acquireSession blocks until this session's single turn slot is free, then
// returns the function that releases it.
//
// Context-aware on purpose: a queued caller abandons the wait when ITS OWN
// context is cancelled, which a sync.Mutex cannot do and which would otherwise
// pin that caller behind an unrelated turn. On cancellation it returns an
// error and holds nothing — it never acquires a token it then has to release.
func acquireSession(ctx context.Context, key string) (release func(), err error) {
	sem := checkoutSem(key)
	select {
	case sem.ch <- struct{}{}:
	case <-ctx.Done():
		returnSem(key)
		return nil, ctx.Err()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			<-sem.ch
			returnSem(key)
		})
	}, nil
}

// checkoutSem returns the slot for key, creating it on first use, and records
// this caller as a user of it.
func checkoutSem(key string) *sessionSem {
	sessionSems.mu.Lock()
	defer sessionSems.mu.Unlock()
	s, ok := sessionSems.m[key]
	if !ok {
		s = &sessionSem{ch: make(chan struct{}, 1)}
		sessionSems.m[key] = s
	}
	s.refs++
	return s
}

// returnSem drops this caller's claim, deleting the slot once nobody holds or
// awaits it. Deletion is safe precisely then: the channel is empty and any
// later arrival creates a fresh, equally empty slot.
func returnSem(key string) {
	sessionSems.mu.Lock()
	defer sessionSems.mu.Unlock()
	s, ok := sessionSems.m[key]
	if !ok {
		return
	}
	s.refs--
	if s.refs <= 0 {
		delete(sessionSems.m, key)
	}
}
