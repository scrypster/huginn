// Package approvals holds Claude Code tool-approval requests that are waiting
// on a human.
//
// A pending approval is a LIVE WAITER: a blocked HTTP request from a
// PreToolUse hook belonging to a `claude` process that is a child of Huginn.
// If Huginn dies that child dies, the turn dies, and the entry describes
// something that no longer exists. That is why this store is in-memory and
// deliberately not persisted — durable entries would only resurrect cards
// nobody can answer.
//
// Every path that cannot produce a genuine human Allow must produce Deny.
package approvals

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Decision is what a human, or the deadline, returned.
type Decision int

const (
	// Deny is the zero value ON PURPOSE. Any path that forgets to set a
	// decision denies rather than allows.
	Deny Decision = iota
	Allow
	AllowCommand // allow, and remember this exact command for this agent
	AllowTool    // allow, and promote the tool into the agent's config
)

// Allowed reports whether the decision permits the tool call.
func (d Decision) Allowed() bool { return d != Deny }

// maxPending bounds how many approvals may be in flight at once across all
// agents. Each one holds a goroutine and an HTTP connection for up to the
// store deadline, so this is a real resource bound. At the cap Register
// errors and the caller denies — never queue, because a queued request burns
// its own clock while hidden and would auto-deny anyway.
const maxPending = 64

// ErrTooManyPending is returned by Register at maxPending.
var ErrTooManyPending = errors.New("too many pending approvals")

// rememberableTool is the only tool for which exact-command memory applies.
// It needs a stable identifying argument, and Bash.command is the only gated
// tool that has one — Write and Edit carry different content every call, so a
// remembered decision would either never hit or match far too broadly.
const rememberableTool = "Bash"

// Request describes a tool call awaiting approval. Summary and Excerpt are
// already truncated by the hook; this package never truncates and never
// rejects on size, because permission must not depend on payload size.
type Request struct {
	AgentName string
	ToolName  string
	Summary   string
	Excerpt   string
	CWD       string
	ToolUseID string
}

// Pending is a registered request. Callers hold it across Wait.
type Pending struct {
	ID      string
	Request Request

	ch        chan Decision
	expiresAt time.Time
}

// PendingView is the JSON shape returned to the browser.
type PendingView struct {
	ID        string `json:"id"`
	AgentName string `json:"agent_name"`
	ToolName  string `json:"tool_name"`
	Summary   string `json:"summary"`
	Excerpt   string `json:"excerpt"`
	CWD       string `json:"cwd"`
	// RemainingMS is computed at call time. An absolute expiry would let
	// client clock skew make a card look expired when it is not.
	RemainingMS int64 `json:"remaining_ms"`
	CanRemember bool  `json:"can_remember"`
}

// Store holds pending approvals and the exact-command memory.
type Store struct {
	mu      sync.Mutex
	pending map[string]*Pending
	// deadline is a FIELD, not a package constant. internal/permissions.Gate
	// makes its prompt timeout a package var, which is exactly why its 30s
	// could not be reconciled with this feature's 285s. Do not reintroduce
	// that constraint: tests need to inject a short deadline.
	deadline time.Duration
	memory   *cmdMemory
	closed   bool
}

// New returns a Store whose requests expire after deadline.
func New(deadline time.Duration) *Store {
	return &Store{
		pending:  make(map[string]*Pending),
		deadline: deadline,
		memory:   newCmdMemory(maxRememberedPerAgent),
	}
}

// Close releases every waiter with Deny. Safe to call more than once.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	for id, p := range s.pending {
		close(p.ch)
		delete(s.pending, id)
	}
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Register creates a pending entry. The caller must call Wait on the result.
func (s *Store) Register(req Request) (*Pending, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrTooManyPending
	}
	if len(s.pending) >= maxPending {
		return nil, ErrTooManyPending
	}
	p := &Pending{
		ID:      id,
		Request: req,
		// Buffered: Deliver must never block if the waiter has already gone.
		ch:        make(chan Decision, 1),
		expiresAt: time.Now().Add(s.deadline),
	}
	s.pending[id] = p
	return p, nil
}

// Wait blocks until a decision arrives, the deadline expires, or ctx is done.
// Every non-delivery outcome is Deny.
func (s *Store) Wait(ctx context.Context, p *Pending) Decision {
	timer := time.NewTimer(time.Until(p.expiresAt))
	defer timer.Stop()

	select {
	case got, ok := <-p.ch:
		if !ok {
			return Deny // channel closed by Close()
		}
		return got
	case <-timer.C:
	case <-ctx.Done():
	}

	// The deadline or the context fired. Whether we actually get to deny
	// depends on whether a Deliver beat us to the entry, and that question is
	// only answerable under the lock.
	s.mu.Lock()
	_, stillPending := s.pending[p.ID]
	delete(s.pending, p.ID)
	s.mu.Unlock()

	if stillPending {
		return Deny
	}

	// The entry was already claimed by a Deliver. Because Deliver sends while
	// holding the mutex, that send has completed by the time we observed the
	// entry gone, so this receive always finds the value. The delivered
	// decision is authoritative — do not override a human's answer with Deny
	// just because the timer also fired.
	select {
	case got, ok := <-p.ch:
		if ok {
			return got
		}
	default:
	}
	return Deny
}

// Deliver hands a decision to a waiter. It reports false when the id is
// unknown — already decided, already expired, or never existed.
//
// The send happens while the mutex is HELD, which is what makes the claim and
// the handoff atomic. Without that, Wait's select could commit to its deadline
// branch while Deliver still saw the entry in the map, and Deliver would
// return true for a decision nobody ever consumed. The send cannot block: the
// channel is buffered with capacity 1 and the delete above guarantees exactly
// one send per pending entry.
func (s *Store) Deliver(id string, d Decision) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[id]
	if !ok {
		return false
	}
	delete(s.pending, id)
	p.ch <- d
	return true
}

// List returns every pending request, newest last, with remaining time
// computed now.
func (s *Store) List() []PendingView {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make([]PendingView, 0, len(s.pending))
	for _, p := range s.pending {
		remaining := p.expiresAt.Sub(now).Milliseconds()
		if remaining < 0 {
			remaining = 0
		}
		out = append(out, PendingView{
			ID:          p.ID,
			AgentName:   p.Request.AgentName,
			ToolName:    p.Request.ToolName,
			Summary:     p.Request.Summary,
			Excerpt:     p.Request.Excerpt,
			CWD:         p.Request.CWD,
			RemainingMS: remaining,
			CanRemember: p.Request.ToolName == rememberableTool,
		})
	}
	return out
}

// Remembered reports whether this agent previously chose "always allow this
// exact command".
func (s *Store) Remembered(agentName, toolName, command string) bool {
	if toolName != rememberableTool {
		return false
	}
	return s.memory.has(agentName, toolName, command)
}

// Remember records an exact-command allow for this agent.
func (s *Store) Remember(agentName, toolName, command string) {
	if toolName != rememberableTool {
		return
	}
	s.memory.add(agentName, toolName, command)
}
