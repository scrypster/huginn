# Claude Code Approval UI (Phase 1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a human approve or deny a Claude Code agent's gated tool calls from Huginn's web UI, replacing today's behaviour where a gated-but-not-allowlisted tool is denied every time with no prompt.

**Architecture:** A new in-memory pending-approval store blocks the `PreToolUse` hook's HTTP request until a human decides or a deadline expires. The store is reached from the existing unauthenticated loopback approval endpoint; decisions arrive on two new authenticated endpoints. Push, audit and desktop notifications reuse existing infrastructure. `internal/permissions.Gate` is deliberately not involved.

**Tech Stack:** Go 1.x (stdlib only — `sync`, `container/list`, `crypto/rand`, `net/http`), Vue 3 + TypeScript + Tailwind, vitest.

**Spec:** `docs/planning/2026-08-27-claude-approval-ui-design.md`

## Global Constraints

- **No test may invoke the real `claude` CLI, touch the network, or spend money.**
- `git stash` / `git stash pop` are **forbidden** — a pre-existing `stash@{0}` must not be disturbed.
- `git add` by explicit path only. Never `git add -A`.
- Do not run `go test ./...` repo-wide: non-hermetic tests overwrite the user's real config at `~/.huginn/config.json`. Run only the package you are working in. `internal/server` IS safe to run whole — its `TestMain` sandboxes `HOME`.
- No starting a server, no `pnpm install`, no `make build-frontend`.
- **Approval fails closed.** Any path that cannot obtain a genuine allow must deny.
- `internal/permissions` is **not modified** by this work.
- No writes under `web/` beyond the files named in this plan.
- Implementers must not dispatch their own subagents.
- Every test must be accompanied by a statement of **what the implementer broke to watch it go red.**

## Verified facts this plan depends on

1. A hook that misses its declared `timeout` is **killed and the tool runs anyway** — hooks fail OPEN on timeout.
2. A hook declaring `"timeout": 300` is **not clamped**; it blocked 290s and its deny was honoured.
3. A hook returning `permissionDecision: "allow"` **grants a tool that is not in `--allowedTools`**.

Fact 3 is what makes this feature able to grant rather than only withhold.

## File Structure

**Create:**
- `internal/claudecode/approvals/store.go` — pending store: register, wait, deliver, list. Owns the deadline.
- `internal/claudecode/approvals/memory.go` — exact-command memory, per agent, LRU-capped.
- `internal/claudecode/approvals/store_test.go`, `memory_test.go`
- `internal/server/handlers_claude_approvals.go` — `GET /approvals`, `POST /approve/decide`
- `internal/server/handlers_claude_approvals_test.go`
- `web/src/composables/useClaudeApprovals.ts`
- `web/src/composables/__tests__/useClaudeApprovals.test.ts`
- `web/src/components/ClaudeApprovalCard.vue`
- `web/src/components/__tests__/ClaudeApprovalCard.test.ts`

**Modify:**
- `internal/claudecode/hooks.go` — raise `ClaudeHookTimeoutSecs`; fix the now-false doc comment
- `cmd_claude_approve.go` — forward a truncated `tool_input`
- `internal/server/handlers_claude_approve.go` — block on the store
- `internal/server/server.go:1062` — routes and body cap
- `web/src/views/ChatView.vue` — render cards inline
- `web/src/App.vue` — nav badge
- `docs/features/claude-code-agents.md` — the "denied every time" claim becomes false

---

### Task 1: Pending approval store

**Files:**
- Create: `internal/claudecode/approvals/store.go`
- Create: `internal/claudecode/approvals/memory.go`
- Test: `internal/claudecode/approvals/store_test.go`
- Test: `internal/claudecode/approvals/memory_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: everything below. Later tasks depend on these exact names.

```go
type Decision int
const (
    Deny Decision = iota
    Allow
    AllowCommand
    AllowTool
)
type Request struct { AgentName, ToolName, Summary, Excerpt, CWD, ToolUseID string }
type Pending struct { ID string; Request Request }
type PendingView struct { ID, AgentName, ToolName, Summary, Excerpt, CWD string; RemainingMS int64; CanRemember bool }
func New(deadline time.Duration) *Store
func (s *Store) Register(req Request) (*Pending, error)
func (s *Store) Wait(ctx context.Context, p *Pending) Decision
func (s *Store) Deliver(id string, d Decision) bool
func (s *Store) List() []PendingView
func (s *Store) Remembered(agentName, toolName, command string) bool
func (s *Store) Remember(agentName, toolName, command string)
func (s *Store) Close()
var ErrTooManyPending = errors.New("too many pending approvals")
```

- [ ] **Step 1: Write the failing store tests**

Create `internal/claudecode/approvals/store_test.go`:

```go
package approvals

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func req() Request {
	return Request{AgentName: "codey", ToolName: "Bash", Summary: "go test ./...", CWD: "/tmp"}
}

func TestDeliverAllowUnblocksWait(t *testing.T) {
	s := New(5 * time.Second)
	defer s.Close()
	p, err := s.Register(req())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	go func() {
		// Poll until the waiter is parked, then deliver.
		for i := 0; i < 100; i++ {
			if s.Deliver(p.ID, Allow) {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if got := s.Wait(context.Background(), p); got != Allow {
		t.Fatalf("Wait = %v, want Allow", got)
	}
}

func TestWaitDeniesOnDeadline(t *testing.T) {
	// A NON-ZERO deadline: a zero deadline would deny instantly for the wrong
	// reason and this test would pass against a store that never waits at all.
	s := New(60 * time.Millisecond)
	defer s.Close()
	p, _ := s.Register(req())
	start := time.Now()
	got := s.Wait(context.Background(), p)
	elapsed := time.Since(start)
	if got != Deny {
		t.Fatalf("Wait = %v, want Deny", got)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("Wait returned after %v; it did not actually block", elapsed)
	}
}

func TestDeliverUnknownIDReturnsFalse(t *testing.T) {
	s := New(time.Second)
	defer s.Close()
	if s.Deliver("nope", Allow) {
		t.Fatal("Deliver on unknown id returned true")
	}
}

func TestDeliverAfterDeadlineReturnsFalse(t *testing.T) {
	s := New(20 * time.Millisecond)
	defer s.Close()
	p, _ := s.Register(req())
	if got := s.Wait(context.Background(), p); got != Deny {
		t.Fatalf("Wait = %v, want Deny", got)
	}
	if s.Deliver(p.ID, Allow) {
		t.Fatal("Deliver succeeded after the deadline had already denied")
	}
}

func TestConcurrentDeliverAndExpireProduceOneWinner(t *testing.T) {
	// Run under -race. Exactly one of {delivered decision, deadline deny} must win.
	for i := 0; i < 50; i++ {
		s := New(10 * time.Millisecond)
		p, _ := s.Register(req())
		var wg sync.WaitGroup
		wg.Add(1)
		var delivered bool
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			delivered = s.Deliver(p.ID, Allow)
		}()
		got := s.Wait(context.Background(), p)
		wg.Wait()
		if delivered && got != Allow {
			t.Fatalf("Deliver reported success but Wait returned %v", got)
		}
		if !delivered && got != Deny {
			t.Fatalf("Deliver failed but Wait returned %v", got)
		}
		s.Close()
	}
}

func TestRegisterErrorsAtCap(t *testing.T) {
	s := New(time.Minute)
	defer s.Close()
	for i := 0; i < maxPending; i++ {
		if _, err := s.Register(req()); err != nil {
			t.Fatalf("Register %d failed early: %v", i, err)
		}
	}
	if _, err := s.Register(req()); !errors.Is(err, ErrTooManyPending) {
		t.Fatalf("Register past cap err = %v, want ErrTooManyPending", err)
	}
}

func TestListReportsRemainingMS(t *testing.T) {
	s := New(500 * time.Millisecond)
	defer s.Close()
	if _, err := s.Register(req()); err != nil {
		t.Fatal(err)
	}
	v := s.List()
	if len(v) != 1 {
		t.Fatalf("List len = %d, want 1", len(v))
	}
	if v[0].RemainingMS <= 0 || v[0].RemainingMS > 500 {
		t.Fatalf("RemainingMS = %d, want (0,500]", v[0].RemainingMS)
	}
	if !v[0].CanRemember {
		t.Fatal("CanRemember = false for a Bash request, want true")
	}
}

func TestListCanRememberFalseForNonBash(t *testing.T) {
	s := New(time.Minute)
	defer s.Close()
	r := req()
	r.ToolName = "Write"
	if _, err := s.Register(r); err != nil {
		t.Fatal(err)
	}
	if s.List()[0].CanRemember {
		t.Fatal("CanRemember = true for Write; exact-command memory is Bash-only")
	}
}

func TestIDsAreUnique(t *testing.T) {
	s := New(time.Minute)
	defer s.Close()
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		p, err := s.Register(req())
		if err != nil {
			t.Fatal(err)
		}
		if seen[p.ID] {
			t.Fatalf("duplicate id %q", p.ID)
		}
		seen[p.ID] = true
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/claudecode/approvals/ -run Test -v`
Expected: FAIL — package does not compile, `undefined: New`.

- [ ] **Step 3: Implement the store**

Create `internal/claudecode/approvals/store.go`:

```go
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
// return true for a decision nobody ever consumed — telling the browser
// "approved" for a call that was in fact denied. The send cannot block: the
// channel is buffered with capacity 1 and the delete guarantees exactly one
// send per pending entry.
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
```

- [ ] **Step 4: Write the failing memory tests**

Create `internal/claudecode/approvals/memory_test.go`:

```go
package approvals

import (
	"fmt"
	"testing"
)

func TestMemoryExactMatchOnly(t *testing.T) {
	m := newCmdMemory(10)
	m.add("codey", "Bash", "go test ./...")
	if !m.has("codey", "Bash", "go test ./...") {
		t.Fatal("exact command did not match")
	}
	// One byte different must NOT match. This is the whole security property:
	// no prefix matching, no whitespace collapsing, no case folding.
	for _, other := range []string{
		"go test ./...x",
		"go  test ./...",
		"GO TEST ./...",
		"go test ./... && rm -rf /",
		"go test",
	} {
		if m.has("codey", "Bash", other) {
			t.Fatalf("command %q matched a different remembered command", other)
		}
	}
}

func TestMemoryTrimsOnlyTrailingWhitespace(t *testing.T) {
	m := newCmdMemory(10)
	m.add("codey", "Bash", "go test ./...  ")
	if !m.has("codey", "Bash", "go test ./...") {
		t.Fatal("trailing whitespace should be trimmed on both add and has")
	}
	if m.has("codey", "Bash", "  go test ./...") {
		t.Fatal("LEADING whitespace was trimmed; only trailing may be")
	}
}

func TestMemoryIsPerAgent(t *testing.T) {
	m := newCmdMemory(10)
	m.add("codey", "Bash", "go test ./...")
	if m.has("other", "Bash", "go test ./...") {
		t.Fatal("a remembered command leaked to a different agent")
	}
}

func TestMemoryIsPerTool(t *testing.T) {
	m := newCmdMemory(10)
	m.add("codey", "Bash", "x")
	if m.has("codey", "Write", "x") {
		t.Fatal("a remembered command leaked across tool names")
	}
}

func TestMemoryLRUEvictsOldest(t *testing.T) {
	m := newCmdMemory(3)
	m.add("codey", "Bash", "one")
	m.add("codey", "Bash", "two")
	m.add("codey", "Bash", "three")
	// Touch "one" so "two" becomes least-recently-used.
	if !m.has("codey", "Bash", "one") {
		t.Fatal("one should still be present")
	}
	m.add("codey", "Bash", "four")
	if m.has("codey", "Bash", "two") {
		t.Fatal("two should have been evicted as least-recently-used")
	}
	for _, keep := range []string{"one", "three", "four"} {
		if !m.has("codey", "Bash", keep) {
			t.Fatalf("%q was evicted but should have been kept", keep)
		}
	}
}

func TestMemoryCapIsPerAgent(t *testing.T) {
	m := newCmdMemory(2)
	m.add("a", "Bash", "one")
	m.add("a", "Bash", "two")
	m.add("b", "Bash", "three")
	m.add("b", "Bash", "four")
	for _, c := range []struct{ agent, cmd string }{
		{"a", "one"}, {"a", "two"}, {"b", "three"}, {"b", "four"},
	} {
		if !m.has(c.agent, "Bash", c.cmd) {
			t.Fatalf("%s/%s evicted; the cap must be per agent", c.agent, c.cmd)
		}
	}
}

func TestMemoryReAddDoesNotGrow(t *testing.T) {
	m := newCmdMemory(2)
	m.add("codey", "Bash", "one")
	m.add("codey", "Bash", "one")
	m.add("codey", "Bash", "two")
	if !m.has("codey", "Bash", "one") {
		t.Fatal("re-adding the same command consumed two slots")
	}
	_ = fmt.Sprint()
}
```

- [ ] **Step 5: Run the memory tests to verify they fail**

Run: `go test ./internal/claudecode/approvals/ -run TestMemory -v`
Expected: FAIL — `undefined: newCmdMemory`.

- [ ] **Step 6: Implement the memory**

Create `internal/claudecode/approvals/memory.go`:

```go
package approvals

import (
	"container/list"
	"strings"
	"sync"
)

// maxRememberedPerAgent caps remembered commands per agent, following the
// shape of internal/permissions.Gate.sessionAllowed.
const maxRememberedPerAgent = 1000

// cmdMemory remembers exact commands a human chose to always allow.
//
// Scope is THIS HUGINN PROCESS. Not the chat session, not the Claude session.
// The UI label must say so.
//
// Matching is byte-exact after trailing-whitespace trim, and nothing else. No
// case folding, no whitespace collapsing, no path canonicalisation. Every
// normalisation step is a place where two different commands collapse into one
// key, and that is the entire attack surface. Prefix matching is deliberately
// absent: "npm test" as a prefix would authorise "npm test && curl x | sh".
type cmdMemory struct {
	mu      sync.Mutex
	max     int
	byAgent map[string]*agentMem
}

type agentMem struct {
	order *list.List               // front = most recently used
	items map[string]*list.Element // key -> element
}

func newCmdMemory(max int) *cmdMemory {
	return &cmdMemory{max: max, byAgent: make(map[string]*agentMem)}
}

// key binds the tool name to the command with a NUL separator so a command
// containing the tool name cannot forge a different tool's key.
func memKey(toolName, command string) string {
	return toolName + "\x00" + strings.TrimRight(command, " \t\r\n")
}

func (m *cmdMemory) has(agentName, toolName, command string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	am, ok := m.byAgent[agentName]
	if !ok {
		return false
	}
	el, ok := am.items[memKey(toolName, command)]
	if !ok {
		return false
	}
	am.order.MoveToFront(el)
	return true
}

func (m *cmdMemory) add(agentName, toolName, command string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	am, ok := m.byAgent[agentName]
	if !ok {
		am = &agentMem{order: list.New(), items: make(map[string]*list.Element)}
		m.byAgent[agentName] = am
	}
	k := memKey(toolName, command)
	if el, ok := am.items[k]; ok {
		am.order.MoveToFront(el)
		return
	}
	am.items[k] = am.order.PushFront(k)
	for am.order.Len() > m.max {
		oldest := am.order.Back()
		if oldest == nil {
			break
		}
		am.order.Remove(oldest)
		delete(am.items, oldest.Value.(string))
	}
}
```

- [ ] **Step 7: Run all package tests, including the race detector**

Run: `go test ./internal/claudecode/approvals/ -race -v`
Expected: PASS, no race reports.

- [ ] **Step 8: Commit**

```bash
git add internal/claudecode/approvals/store.go internal/claudecode/approvals/memory.go internal/claudecode/approvals/store_test.go internal/claudecode/approvals/memory_test.go
git commit -m "feat(claudecode): add the pending tool-approval store"
```

---

### Task 2: Raise the hook timeout and forward a truncated tool_input

**Files:**
- Modify: `internal/claudecode/hooks.go:41` and its doc comment at lines 25-32
- Modify: `cmd_claude_approve.go:150-190`
- Test: `cmd_claude_approve_test.go` (add to the existing file)

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: the hook POST body gains `summary` and `excerpt` string fields. `claudecode.ClaudeHookTimeoutSecs` becomes `300`, so `claudeApproveTimeout` derives to 290s.

- [ ] **Step 1: Raise the constant and correct the doc comment**

In `internal/claudecode/hooks.go`, change line 41 from `const ClaudeHookTimeoutSecs = 30` to:

```go
// It is 300 because a human is now in this loop: the approval endpoint blocks
// until someone clicks or the store's 285s deadline expires. VERIFIED against
// the real CLI on 2026-08-27 that a 300s hook timeout is NOT clamped — the
// hook blocked for 290s and its deny was honoured. See the design doc's "Hook
// Blocking Budget" section for the probe; re-run it if the CLI version changes.
const ClaudeHookTimeoutSecs = 300
```

In the same file, replace this now-false paragraph in the block comment above (lines 25-28):

```
// this hook produce THE SAME effective permission set. The approval endpoint
// allows exactly the tools in ClaudeAllowedTools, which is also what is passed
// to --allowedTools. The hook's contribution is a log entry and a
// human-readable deny reason, not additional restriction.
```

with:

```
// this hook produced the same effective permission set, because the approval
// endpoint allowed exactly the tools in ClaudeAllowedTools — the same list
// passed to --allowedTools.
//
// THAT IS NO LONGER TRUE. The endpoint can now block on a human, and a human
// can allow a tool that is NOT in ClaudeAllowedTools. VERIFIED 2026-08-27: a
// PreToolUse hook returning permissionDecision "allow" grants a tool absent
// from --allowedTools (probed with --allowedTools Read and a gated Bash; the
// command ran and permission_denials was empty). So the hook is now a source
// of ADDITIONAL PERMISSION, not only of deny reasons.
```

- [ ] **Step 2: Verify the compile-time guard still holds**

Run: `go build ./... && go vet ./internal/claudecode/`
Expected: builds. `const _ = uint(claudecode.ClaudeHookTimeoutSecs - 15)` in `cmd_claude_approve.go` evaluates to `uint(285)`, which compiles. `claudeApproveTimeout` is now `290 * time.Second`.

- [ ] **Step 3: Write the failing truncation tests**

Add to `cmd_claude_approve_test.go`:

```go
func TestSummarizeBashSendsCommandVerbatim(t *testing.T) {
	sum, exc := summarizeToolInput("Bash", map[string]any{
		"command":     "go test ./...",
		"description": "run tests",
	})
	if sum != "go test ./..." {
		t.Fatalf("summary = %q, want the command verbatim", sum)
	}
	if exc != "" {
		t.Fatalf("excerpt = %q, want empty for Bash", exc)
	}
}

func TestSummarizeBashTruncatesLongCommand(t *testing.T) {
	long := strings.Repeat("x", maxCommandBytes+500)
	sum, _ := summarizeToolInput("Bash", map[string]any{"command": long})
	if len(sum) > maxCommandBytes {
		t.Fatalf("summary len = %d, want <= %d", len(sum), maxCommandBytes)
	}
}

func TestSummarizeWriteSendsPathAndBoundedContent(t *testing.T) {
	sum, exc := summarizeToolInput("Write", map[string]any{
		"file_path": "/tmp/a.ts",
		"content":   strings.Repeat("y", maxExcerptBytes+500),
	})
	if sum != "/tmp/a.ts" {
		t.Fatalf("summary = %q, want the file path", sum)
	}
	if len(exc) > maxExcerptBytes {
		t.Fatalf("excerpt len = %d, want <= %d", len(exc), maxExcerptBytes)
	}
}

func TestSummarizeEditUsesNewString(t *testing.T) {
	sum, exc := summarizeToolInput("Edit", map[string]any{
		"file_path":  "/tmp/b.go",
		"new_string": "package main",
	})
	if sum != "/tmp/b.go" {
		t.Fatalf("summary = %q, want the file path", sum)
	}
	if exc != "package main" {
		t.Fatalf("excerpt = %q, want the new_string", exc)
	}
}

func TestSummarizeUnknownToolFallsBackToBoundedJSON(t *testing.T) {
	sum, exc := summarizeToolInput("Task", map[string]any{
		"prompt": strings.Repeat("z", maxExcerptBytes+500),
	})
	if len(exc) > maxExcerptBytes {
		t.Fatalf("excerpt len = %d, want <= %d", len(exc), maxExcerptBytes)
	}
	_ = sum
}

func TestSummarizeNilInputIsSafe(t *testing.T) {
	sum, exc := summarizeToolInput("Bash", nil)
	if sum != "" || exc != "" {
		t.Fatalf("got (%q,%q), want empty strings for nil input", sum, exc)
	}
}

func TestRunClaudeApproveForwardsSummary(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]string{"decision": "allow", "reason": "ok"})
	}))
	defer srv.Close()

	in := strings.NewReader(`{"tool_name":"Bash","session_id":"s","tool_input":{"command":"ls -la"}}`)
	var out bytes.Buffer
	if code := runClaudeApprove(in, &out, srv.URL, 5*time.Second); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("server got unparseable body: %v", err)
	}
	if sent["summary"] != "ls -la" {
		t.Fatalf("summary forwarded = %v, want %q", sent["summary"], "ls -la")
	}
}
```

Add `"strings"`, `"bytes"`, `"io"`, `"net/http"`, `"net/http/httptest"`, `"encoding/json"`, `"time"` to that file's imports if not already present.

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test . -run 'TestSummarize|TestRunClaudeApproveForwards' -v`
Expected: FAIL — `undefined: summarizeToolInput`.

- [ ] **Step 5: Implement truncation and forwarding**

In `cmd_claude_approve.go`, add:

```go
const (
	// maxCommandBytes bounds a forwarded Bash command.
	maxCommandBytes = 4 << 10
	// maxExcerptBytes bounds every other forwarded content excerpt.
	maxExcerptBytes = 2 << 10
)

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func inputString(in map[string]any, key string) string {
	v, _ := in[key].(string)
	return v
}

// summarizeToolInput reduces a tool_input to a bounded, human-readable pair.
//
// TRUNCATION HAPPENS HERE, IN THE HOOK, ON PURPOSE. An earlier version
// forwarded the whole tool_input, and a large Write blew the route's body cap,
// failed to decode, and DENIED an explicitly allowlisted tool — a permission
// decision that depended on payload size. Bounding it at the source is what
// makes that impossible. Never forward the raw input, and never let the server
// reject on size.
func summarizeToolInput(toolName string, in map[string]any) (summary, excerpt string) {
	if in == nil {
		return "", ""
	}
	switch toolName {
	case "Bash":
		return clip(inputString(in, "command"), maxCommandBytes), ""
	case "Write":
		return inputString(in, "file_path"), clip(inputString(in, "content"), maxExcerptBytes)
	case "Edit", "NotebookEdit":
		return inputString(in, "file_path"), clip(inputString(in, "new_string"), maxExcerptBytes)
	case "WebFetch":
		return inputString(in, "url"), ""
	}
	summary = inputString(in, "file_path")
	if summary == "" {
		summary = inputString(in, "url")
	}
	b, err := json.Marshal(in)
	if err != nil {
		return summary, ""
	}
	return summary, clip(string(b), maxExcerptBytes)
}
```

In the `hook` struct in `runClaudeApprove`, add the field:

```go
		ToolInput map[string]any `json:"tool_input"`
```

Replace the `body, err := json.Marshal(...)` block and the comment above it with:

```go
	// tool_input is forwarded as a BOUNDED SUMMARY, never raw. See
	// summarizeToolInput for why the truncation lives here rather than on the
	// server: permission must never depend on payload size.
	summary, excerpt := summarizeToolInput(hook.ToolName, hook.ToolInput)
	body, err := json.Marshal(map[string]any{
		"tool_name":   hook.ToolName,
		"tool_use_id": hook.ToolUseID,
		"session_id":  hook.SessionID,
		"cwd":         hook.CWD,
		"summary":     summary,
		"excerpt":     excerpt,
	})
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test . -run 'TestSummarize|TestRunClaudeApprove' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd_claude_approve.go cmd_claude_approve_test.go internal/claudecode/hooks.go
git commit -m "feat(claudecode): forward a bounded tool_input and raise the hook timeout to 300s"
```

---

### Task 3: Block the approval handler on the store

**Files:**
- Modify: `internal/server/handlers_claude_approve.go`
- Modify: `internal/server/server.go` (add the store field to `Server`)
- Test: `internal/server/handlers_claude_approve_test.go` (add to the existing file)

**Interfaces:**
- Consumes: `approvals.New`, `Register`, `Wait`, `Deliver`, `Remembered`, `Decision`, `ErrTooManyPending` from Task 1. The `summary` / `excerpt` POST fields from Task 2.
- Produces: `Server.approvals *approvals.Store`, populated by `New`. `s.approvals` may be nil in tests that do not set it, and nil MUST deny.

- [ ] **Step 1: Write the failing handler tests**

Add to `internal/server/handlers_claude_approve_test.go`:

```go
func TestApproveBlocksThenAllowsOnDeliver(t *testing.T) {
	s := newTestServer(t)
	s.approvals = approvals.New(2 * time.Second)
	defer s.approvals.Close()
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess", ClaudeAllowedTools: []string{"Read"},
		}}}, nil
	}

	done := make(chan string, 1)
	go func() {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/claude/approve",
			strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"ls"}`))
		req.RemoteAddr = "127.0.0.1:1234"
		s.handleClaudeApprove(rr, req)
		var got map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &got)
		done <- got["decision"]
	}()

	var id string
	for i := 0; i < 200; i++ {
		if v := s.approvals.List(); len(v) == 1 {
			id = v[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("handler never registered a pending approval")
	}
	if !s.approvals.Deliver(id, approvals.Allow) {
		t.Fatal("Deliver failed")
	}
	if got := <-done; got != "allow" {
		t.Fatalf("decision = %q, want allow", got)
	}
}

func TestApproveDeniesOnDeadline(t *testing.T) {
	s := newTestServer(t)
	// Non-zero: a zero deadline denies instantly for the wrong reason and this
	// test would pass against a handler that never blocks at all.
	s.approvals = approvals.New(80 * time.Millisecond)
	defer s.approvals.Close()
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess",
		}}}, nil
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"ls"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	start := time.Now()
	s.handleClaudeApprove(rr, req)
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Fatalf("handler returned after %v; it did not block", elapsed)
	}
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["decision"] != "deny" {
		t.Fatalf("decision = %q, want deny", got["decision"])
	}
}

func TestApproveAllowlistedToolNeverRegisters(t *testing.T) {
	s := newTestServer(t)
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess", ClaudeAllowedTools: []string{"Bash"},
		}}}, nil
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	s.handleClaudeApprove(rr, req)
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["decision"] != "allow" {
		t.Fatalf("decision = %q, want allow", got["decision"])
	}
	if v := s.approvals.List(); len(v) != 0 {
		t.Fatalf("allowlisted tool registered %d pending approvals, want 0", len(v))
	}
}

func TestApproveRememberedCommandNeverRegisters(t *testing.T) {
	s := newTestServer(t)
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	s.approvals.Remember("codey", "Bash", "ls -la")
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess",
		}}}, nil
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess","summary":"ls -la"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	s.handleClaudeApprove(rr, req)
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["decision"] != "allow" {
		t.Fatalf("decision = %q, want allow", got["decision"])
	}
	if v := s.approvals.List(); len(v) != 0 {
		t.Fatalf("remembered command registered %d pending approvals, want 0", len(v))
	}
}

func TestApproveNilStoreDenies(t *testing.T) {
	s := newTestServer(t)
	s.approvals = nil // production misconfiguration: must fail CLOSED
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeSessionID: "sess",
		}}}, nil
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/claude/approve",
		strings.NewReader(`{"tool_name":"Bash","session_id":"sess"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	s.handleClaudeApprove(rr, req)
	var got map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["decision"] != "deny" {
		t.Fatalf("decision = %q, want deny", got["decision"])
	}
}
```

If `newTestServer(t)` does not already exist in this package, use whatever constructor the surrounding tests use; do not invent a new one.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/server/ -run TestApprove -v`
Expected: FAIL — `s.approvals` undefined.

- [ ] **Step 3: Add the store field to Server**

In `internal/server/server.go`, beside the `auditLog *auditLogger` field (around line 208), add:

```go
	// approvals holds Claude Code tool-approval requests waiting on a human.
	// Nil means the feature is unwired, and a nil store DENIES — never allow
	// because the store is missing.
	approvals *approvals.Store
```

Import `"github.com/scrypster/huginn/internal/claudecode/approvals"`.

In the `Server` constructor, initialise it:

```go
	s.approvals = approvals.New(approvalDeadline)
```

and add near the other server constants:

```go
// approvalDeadline is how long a tool approval waits for a human.
//
// It MUST stay below the hook's client timeout (claudeApproveTimeout, derived
// as ClaudeHookTimeoutSecs-10 = 290s) so the server answers before the hook
// gives up, and that in turn stays below ClaudeHookTimeoutSecs = 300s so the
// hook prints an explicit deny before Claude Code kills it. Ordering:
// 285 < 290 < 300. A hook killed by Claude Code fails OPEN, so this ordering
// is a security property, not a nicety.
const approvalDeadline = 285 * time.Second
```

- [ ] **Step 4: Rewrite the handler**

In `internal/server/handlers_claude_approve.go`, replace this paragraph of the function doc comment:

```
// This handler must never block on anything unbounded — see cmd_claude_approve.go
// for the client-side timing constraint (claudeApproveTimeout, currently
// ClaudeHookTimeoutSecs-10 = 20s) that this response has to beat. Everything
// here is in-memory config lookups, so there is nothing to bound.
```

with:

```
// This handler DOES block, for up to approvalDeadline (285s), while a human
// decides. That is the point of the feature and it inverts this function's
// previous contract. The bound is what keeps it safe: 285s is below the hook's
// own client timeout of 290s, which is below Claude Code's 300s hook timeout.
// A hook killed by Claude Code fails OPEN, so the handler must always answer
// first. Never introduce a wait here that is not bounded by approvalDeadline.
//
// Consequence worth knowing: the agent's session semaphore is held for the
// whole wait. One agent can be frozen for five minutes; others are unaffected.
```

Add `summary` and `excerpt` to the decoded request struct:

```go
	var req struct {
		ToolName  string `json:"tool_name"`
		ToolUseID string `json:"tool_use_id"`
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		Summary   string `json:"summary"`
		Excerpt   string `json:"excerpt"`
	}
```

Replace the final deny block (the `slog.Info("claudecode: tool call requires approval", ...)` call and the `respondApprove(w, "deny", ...)` after it) with:

```go
	if s.approvals == nil {
		slog.Error("claudecode: approval store is not wired; denying",
			"agent", agent.Name, "tool", req.ToolName)
		respondApprove(w, "deny", "Huginn: approvals are unavailable; denying "+req.ToolName)
		return
	}

	if s.approvals.Remembered(agent.Name, req.ToolName, req.Summary) {
		slog.Info("claudecode: tool call allowed by a remembered command",
			"agent", agent.Name, "tool", req.ToolName)
		respondApprove(w, "allow", "Huginn: you previously always-allowed this exact command")
		return
	}

	pending, err := s.approvals.Register(approvals.Request{
		AgentName: agent.Name,
		ToolName:  req.ToolName,
		Summary:   req.Summary,
		Excerpt:   req.Excerpt,
		CWD:       req.CWD,
		ToolUseID: req.ToolUseID,
	})
	if err != nil {
		slog.Warn("claudecode: cannot register approval; denying",
			"agent", agent.Name, "tool", req.ToolName, "err", err)
		respondApprove(w, "deny", "Huginn: too many approvals are already pending")
		return
	}

	slog.Info("claudecode: tool call awaiting approval",
		"agent", agent.Name, "tool", req.ToolName, "id", pending.ID)
	s.broadcastApprovalChange()

	decision := s.approvals.Wait(r.Context(), pending)
	s.broadcastApprovalChange()

	if decision == approvals.AllowCommand {
		s.approvals.Remember(agent.Name, req.ToolName, req.Summary)
	}

	if !decision.Allowed() {
		if s.auditLog != nil {
			s.auditLog.Log("tool_permission", req.ToolName, false, "claude_approval_denied")
		}
		slog.Info("claudecode: tool call denied",
			"agent", agent.Name, "tool", req.ToolName, "id", pending.ID)
		respondApprove(w, "deny", "Huginn: "+req.ToolName+" was not approved")
		return
	}

	slog.Info("claudecode: tool call approved by a human",
		"agent", agent.Name, "tool", req.ToolName, "id", pending.ID)
	respondApprove(w, "allow", "Huginn: "+req.ToolName+" was approved")
```

Note: `AllowTool` (config promotion) is handled in Task 4, on the decide endpoint, because it needs to write agent config. Here it is simply an allow.

- [ ] **Step 5: Add the broadcast helper**

In `internal/server/handlers_claude_approve.go`, add:

```go
// broadcastApprovalChange tells every connected client that the pending set
// changed. The message is a HINT, not the data: clients respond by re-fetching
// GET /api/v1/claude/approvals, which is authoritative.
//
// This matters because WSHub.broadcast DROPS on a full channel. If the message
// carried the card itself, a drop would lose a card until the next reconnect.
// As a hint, any later message heals the drift, and a dropped hint only costs
// a delayed card — which then ages out to a deny, the safe direction.
func (s *Server) broadcastApprovalChange() {
	if s.hub == nil {
		return
	}
	s.hub.broadcast(WSMessage{Type: "claude_approvals_changed"})
}
```

If the `Server`'s hub field is not named `hub`, use its actual name; check `internal/server/server.go`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/server/ -run TestApprove -race -v`
Expected: PASS.

- [ ] **Step 7: Run the whole server package**

Run: `go test ./internal/server/`
Expected: PASS. This package is safe to run whole — its `TestMain` sandboxes `HOME`.

- [ ] **Step 8: Commit**

```bash
git add internal/server/handlers_claude_approve.go internal/server/handlers_claude_approve_test.go internal/server/server.go
git commit -m "feat(server): block tool approvals on a human decision"
```

---

### Task 4: Decision and listing endpoints

**Files:**
- Create: `internal/server/handlers_claude_approvals.go`
- Create: `internal/server/handlers_claude_approvals_test.go`
- Modify: `internal/server/server.go:1062` (routes and body cap)

**Interfaces:**
- Consumes: `s.approvals`, `approvals.Decision`, `s.broadcastApprovalChange` from Task 3.
- Produces: `GET /api/v1/claude/approvals` returning `{"approvals":[PendingView...]}`; `POST /api/v1/claude/approve/decide` taking `{"id":"...","decision":"allow"|"deny"|"allow_command"|"allow_tool"}`.

- [ ] **Step 1: Write the failing endpoint tests**

Create `internal/server/handlers_claude_approvals_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/claudecode/approvals"
)

func TestListApprovalsReturnsPending(t *testing.T) {
	s := newTestServer(t)
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	if _, err := s.approvals.Register(approvals.Request{
		AgentName: "codey", ToolName: "Bash", Summary: "ls",
	}); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.handleListClaudeApprovals(rr, httptest.NewRequest("GET", "/api/v1/claude/approvals", nil))
	var got struct {
		Approvals []approvals.PendingView `json:"approvals"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unparseable body: %v", err)
	}
	if len(got.Approvals) != 1 || got.Approvals[0].Summary != "ls" {
		t.Fatalf("approvals = %+v, want one entry summarised %q", got.Approvals, "ls")
	}
	if got.Approvals[0].RemainingMS <= 0 {
		t.Fatal("RemainingMS must be positive and computed server-side")
	}
}

func TestListApprovalsNilStoreReturnsEmpty(t *testing.T) {
	s := newTestServer(t)
	s.approvals = nil
	rr := httptest.NewRecorder()
	s.handleListClaudeApprovals(rr, httptest.NewRequest("GET", "/api/v1/claude/approvals", nil))
	var got struct {
		Approvals []approvals.PendingView `json:"approvals"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unparseable body: %v", err)
	}
	if len(got.Approvals) != 0 {
		t.Fatalf("want empty list, got %+v", got.Approvals)
	}
}

func TestDecideDeliversDecision(t *testing.T) {
	s := newTestServer(t)
	s.approvals = approvals.New(2 * time.Second)
	defer s.approvals.Close()
	p, _ := s.approvals.Register(approvals.Request{AgentName: "codey", ToolName: "Bash", Summary: "ls"})

	got := make(chan approvals.Decision, 1)
	go func() { got <- s.approvals.Wait(newTestContext(t), p) }()

	rr := httptest.NewRecorder()
	s.handleDecideClaudeApproval(rr, httptest.NewRequest("POST", "/api/v1/claude/approve/decide",
		strings.NewReader(`{"id":"`+p.ID+`","decision":"allow"}`)))
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if d := <-got; d != approvals.Allow {
		t.Fatalf("waiter got %v, want Allow", d)
	}
}

func TestDecideUnknownIDIs404(t *testing.T) {
	s := newTestServer(t)
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	rr := httptest.NewRecorder()
	s.handleDecideClaudeApproval(rr, httptest.NewRequest("POST", "/api/v1/claude/approve/decide",
		strings.NewReader(`{"id":"missing","decision":"allow"}`)))
	if rr.Code != 404 {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestDecideRejectsUnknownDecisionString(t *testing.T) {
	s := newTestServer(t)
	s.approvals = approvals.New(time.Minute)
	defer s.approvals.Close()
	p, _ := s.approvals.Register(approvals.Request{AgentName: "codey", ToolName: "Bash"})
	rr := httptest.NewRecorder()
	s.handleDecideClaudeApproval(rr, httptest.NewRequest("POST", "/api/v1/claude/approve/decide",
		strings.NewReader(`{"id":"`+p.ID+`","decision":"sudo"}`)))
	if rr.Code != 400 {
		t.Fatalf("status = %d, want 400 for an unrecognised decision", rr.Code)
	}
	if len(s.approvals.List()) != 1 {
		t.Fatal("a bad decision string consumed the pending entry")
	}
}

func TestDecideNilStoreIs503(t *testing.T) {
	s := newTestServer(t)
	s.approvals = nil
	rr := httptest.NewRecorder()
	s.handleDecideClaudeApproval(rr, httptest.NewRequest("POST", "/api/v1/claude/approve/decide",
		strings.NewReader(`{"id":"x","decision":"allow"}`)))
	if rr.Code != 503 {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}
```

If the package has no `newTestContext` helper, replace `newTestContext(t)` with `context.Background()` and import `"context"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestListApprovals|TestDecide' -v`
Expected: FAIL — `undefined: s.handleListClaudeApprovals`.

- [ ] **Step 3: Implement the handlers**

Create `internal/server/handlers_claude_approvals.go`:

```go
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/scrypster/huginn/internal/claudecode/approvals"
)

// handleListClaudeApprovals returns every approval waiting on a human.
//
// This is the AUTHORITATIVE source for the browser. Clients replace their list
// with this response rather than merging, so a client that was disconnected,
// backgrounded, or freshly opened converges without reconciliation logic.
//
// RemainingMS is computed server-side on every call. An absolute expiry would
// let client clock skew make a card look expired when it is not.
func (s *Server) handleListClaudeApprovals(w http.ResponseWriter, r *http.Request) {
	list := []approvals.PendingView{}
	if s.approvals != nil {
		list = s.approvals.List()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"approvals": list})
}

// decisionFromString maps the wire value to a Decision.
//
// Unknown strings are an ERROR, never a default. Defaulting an unrecognised
// value to Deny would silently discard a pending entry on a typo, and
// defaulting to Allow would be a vulnerability. Reject and leave the entry
// alone so the human can try again.
func decisionFromString(v string) (approvals.Decision, bool) {
	switch v {
	case "deny":
		return approvals.Deny, true
	case "allow":
		return approvals.Allow, true
	case "allow_command":
		return approvals.AllowCommand, true
	case "allow_tool":
		return approvals.AllowTool, true
	}
	return approvals.Deny, false
}

// handleDecideClaudeApproval delivers a human decision to a waiting hook.
//
// SECURITY: this is the endpoint that turns "denied" into "allowed". It is
// registered in the AUTHENTICATED block in server.go and must stay there.
// Unlike /claude/approve — which is unauthenticated because the hook is a
// local child process with no token — this one is reached by a browser and
// carries the user's session.
func (s *Server) handleDecideClaudeApproval(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	decision, ok := decisionFromString(req.Decision)
	if !ok {
		http.Error(w, "unrecognised decision", http.StatusBadRequest)
		return
	}

	// Capture the request before delivering: Deliver removes the entry, and
	// AllowTool needs the agent and tool names to promote.
	var agentName, toolName string
	for _, v := range s.approvals.List() {
		if v.ID == req.ID {
			agentName, toolName = v.AgentName, v.ToolName
			break
		}
	}

	if !s.approvals.Deliver(req.ID, decision) {
		http.Error(w, "no such pending approval", http.StatusNotFound)
		return
	}

	if decision == approvals.AllowTool && agentName != "" && toolName != "" {
		if err := s.promoteClaudeTool(agentName, toolName); err != nil {
			// The tool call itself is already allowed — only the persistence
			// failed. Log loudly; do not fail the request, because the human's
			// decision has already been delivered and cannot be recalled.
			slog.Error("claudecode: could not promote tool into agent config",
				"agent", agentName, "tool", toolName, "err", err)
		}
	}

	s.broadcastApprovalChange()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Write the failing promotion test**

Add to `internal/server/handlers_claude_approvals_test.go`:

```go
func TestPromoteClaudeToolAppendsOnce(t *testing.T) {
	s := newTestServer(t)
	saved := [][]string{}
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeAllowedTools: []string{"Read"},
		}}}, nil
	}
	s.agentSaver = func(cfg *agents.AgentsConfig) error {
		saved = append(saved, cfg.Agents[0].ClaudeAllowedTools)
		return nil
	}
	if err := s.promoteClaudeTool("codey", "Bash"); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || len(saved[0]) != 2 || saved[0][1] != "Bash" {
		t.Fatalf("saved = %v, want Read+Bash", saved)
	}
}

func TestPromoteClaudeToolIsIdempotent(t *testing.T) {
	s := newTestServer(t)
	calls := 0
	s.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{
			Name: "codey", ClaudeAllowedTools: []string{"Bash"},
		}}}, nil
	}
	s.agentSaver = func(cfg *agents.AgentsConfig) error { calls++; return nil }
	if err := s.promoteClaudeTool("codey", "Bash"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("saver called %d times for an already-allowed tool, want 0", calls)
	}
}
```

Add `"github.com/scrypster/huginn/internal/agents"` to the test imports.

- [ ] **Step 5: Implement promotion**

Add to `internal/server/handlers_claude_approvals.go`:

```go
// promoteClaudeTool permanently adds tool to an agent's ClaudeAllowedTools.
//
// This is a PRIVILEGE ESCALATION performed by a browser click: afterwards the
// tool is never gated for this agent again, and no card ever appears for it.
// The UI must confirm it separately from a one-click Allow. In Phase 1 the
// only undo is editing the agent config file.
//
// It is idempotent: an already-allowed tool writes nothing.
func (s *Server) promoteClaudeTool(agentName, tool string) error {
	loader := s.agentLoader
	if loader == nil {
		loader = agents.LoadAgents
	}
	cfg, err := loader()
	if err != nil {
		return err
	}
	for i := range cfg.Agents {
		if cfg.Agents[i].Name != agentName {
			continue
		}
		if toolAllowed(cfg.Agents[i].ClaudeAllowedTools, tool) {
			return nil
		}
		cfg.Agents[i].ClaudeAllowedTools = append(cfg.Agents[i].ClaudeAllowedTools, tool)
		saver := s.agentSaver
		if saver == nil {
			saver = agents.SaveAgents
		}
		return saver(cfg)
	}
	return nil
}
```

Add `"github.com/scrypster/huginn/internal/agents"` to the file's imports.

Add an `agentSaver` field beside `agentLoader` in `internal/server/server.go`:

```go
	// agentSaver is nil in production — only tests set it — so callers fall
	// back to agents.SaveAgents, mirroring agentLoader.
	agentSaver func(*agents.AgentsConfig) error
```

If `agents.SaveAgents` does not exist with that exact signature, run `grep -rn "func Save" internal/agents/` and use the real one; adjust the field type to match.

- [ ] **Step 6: Register the routes**

In `internal/server/server.go`, lower the existing approval route's body cap from `1<<20` to `64<<10` — with hook-side truncation, a large body is now a bug signal rather than a legitimate request:

```go
	mux.HandleFunc("POST /api/v1/claude/approve",
		loggingMiddleware(requestIDMiddleware(withMaxBody(64<<10, s.handleClaudeApprove))))
```

In the **authenticated** Claude block (beside `GET /api/v1/claude/status`), add:

```go
	mux.HandleFunc("GET /api/v1/claude/approvals", api(s.handleListClaudeApprovals))
	mux.HandleFunc("POST /api/v1/claude/approve/decide", api(withMaxBody(4<<10, s.handleDecideClaudeApproval)))
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestListApprovals|TestDecide|TestPromote' -race -v`
Expected: PASS.

- [ ] **Step 8: Run the whole server package**

Run: `go test ./internal/server/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/server/handlers_claude_approvals.go internal/server/handlers_claude_approvals_test.go internal/server/server.go
git commit -m "feat(server): add approval listing and decision endpoints"
```

---

### Task 5: Frontend approvals composable

**Files:**
- Create: `web/src/composables/useClaudeApprovals.ts`
- Test: `web/src/composables/__tests__/useClaudeApprovals.test.ts`

**Interfaces:**
- Consumes: `GET /api/v1/claude/approvals`, `POST /api/v1/claude/approve/decide`, WS message type `claude_approvals_changed` from Tasks 3-4.
- Produces:

```ts
export interface ClaudeApproval {
  id: string; agent_name: string; tool_name: string
  summary: string; excerpt: string; cwd: string
  remaining_ms: number; can_remember: boolean
}
export function useClaudeApprovals(): {
  approvals: Ref<ClaudeApproval[]>
  pendingCount: ComputedRef<number>
  approvalsFor: (agentName: string) => ClaudeApproval[]
  refresh: () => Promise<void>
  decide: (id: string, decision: 'allow'|'deny'|'allow_command'|'allow_tool') => Promise<void>
  handleApprovalsChanged: () => void
}
```

- [ ] **Step 1: Write the failing composable tests**

Create `web/src/composables/__tests__/useClaudeApprovals.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useClaudeApprovals } from '../useClaudeApprovals'

function card(over: Partial<any> = {}) {
  return {
    id: 'a1', agent_name: 'codey', tool_name: 'Bash',
    summary: 'go test ./...', excerpt: '', cwd: '/tmp',
    remaining_ms: 285000, can_remember: true, ...over,
  }
}

beforeEach(() => {
  vi.restoreAllMocks()
})

describe('useClaudeApprovals', () => {
  it('refresh replaces the list wholesale rather than merging', async () => {
    const { approvals, refresh } = useClaudeApprovals()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ approvals: [card(), card({ id: 'a2' })] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ approvals: [card({ id: 'a2' })] }) }) as any
    await refresh()
    expect(approvals.value.map(a => a.id)).toEqual(['a1', 'a2'])
    await refresh()
    // a1 resolved server-side while we were away: it must disappear, which a
    // merge would not do.
    expect(approvals.value.map(a => a.id)).toEqual(['a2'])
  })

  it('a card resolved while disconnected leaves the count at zero', async () => {
    const { approvals, pendingCount, refresh } = useClaudeApprovals()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ approvals: [card()] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ approvals: [] }) }) as any
    await refresh()
    expect(pendingCount.value).toBe(1)
    await refresh()
    expect(pendingCount.value).toBe(0)
    expect(approvals.value).toEqual([])
  })

  it('a failed refresh does not strand a stale list', async () => {
    const { approvals, refresh } = useClaudeApprovals()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ approvals: [card()] }) })
      .mockRejectedValueOnce(new Error('offline')) as any
    await refresh()
    expect(approvals.value).toHaveLength(1)
    await refresh()
    // Keep the last known list on a transient failure rather than blanking the
    // UI; the server remains authoritative on the next success.
    expect(approvals.value).toHaveLength(1)
  })

  it('approvalsFor filters by agent', async () => {
    const { approvalsFor, refresh } = useClaudeApprovals()
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ approvals: [card(), card({ id: 'a2', agent_name: 'other' })] }),
    }) as any
    await refresh()
    expect(approvalsFor('codey').map(a => a.id)).toEqual(['a1'])
    expect(approvalsFor('other').map(a => a.id)).toEqual(['a2'])
  })

  it('decide posts the decision and refreshes', async () => {
    const { decide } = useClaudeApprovals()
    const f = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ status: 'ok' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ approvals: [] }) })
    globalThis.fetch = f as any
    await decide('a1', 'allow')
    const [url, init] = f.mock.calls[0]
    expect(String(url)).toContain('/api/v1/claude/approve/decide')
    expect(JSON.parse(init.body)).toEqual({ id: 'a1', decision: 'allow' })
    expect(f).toHaveBeenCalledTimes(2)
  })

  it('handleApprovalsChanged triggers a refresh', async () => {
    const { handleApprovalsChanged } = useClaudeApprovals()
    const f = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ approvals: [] }) })
    globalThis.fetch = f as any
    handleApprovalsChanged()
    await Promise.resolve()
    expect(f).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/composables/__tests__/useClaudeApprovals.test.ts`
Expected: FAIL — cannot resolve `../useClaudeApprovals`.

- [ ] **Step 3: Implement the composable**

Create `web/src/composables/useClaudeApprovals.ts`:

```ts
import { ref, computed, type Ref, type ComputedRef } from 'vue'

export interface ClaudeApproval {
  id: string
  agent_name: string
  tool_name: string
  summary: string
  excerpt: string
  cwd: string
  /** Remaining time computed server-side. Never an absolute timestamp: client
   *  clock skew must not be able to make a card look expired when it is not. */
  remaining_ms: number
  /** True only for Bash — the one gated tool with a stable identifying
   *  argument, so the only one that can carry exact-command memory. */
  can_remember: boolean
}

export type ApprovalDecision = 'allow' | 'deny' | 'allow_command' | 'allow_tool'

// Module-level singletons so the nav badge and the chat cards read the same
// list. A second reactive copy is how badge counts drift from what they count
// — see unseenSessions.ts for the bug that behaviour caused.
const approvals: Ref<ClaudeApproval[]> = ref([])

export function useClaudeApprovals() {
  /**
   * refresh pulls the authoritative pending set and REPLACES the local list.
   * It never merges: a card resolved while this client was disconnected must
   * disappear, and merging would keep it forever.
   */
  async function refresh(): Promise<void> {
    try {
      const res = await fetch('/api/v1/claude/approvals')
      if (!res.ok) return
      const body = await res.json()
      approvals.value = Array.isArray(body?.approvals) ? body.approvals : []
    } catch {
      // Keep the last known list on a transient failure rather than blanking
      // the UI. The server stays authoritative on the next success.
    }
  }

  async function decide(id: string, decision: ApprovalDecision): Promise<void> {
    try {
      await fetch('/api/v1/claude/approve/decide', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id, decision }),
      })
    } finally {
      await refresh()
    }
  }

  /**
   * handleApprovalsChanged responds to the `claude_approvals_changed` websocket
   * message. That message is a HINT with no payload — the server's list is the
   * data — so any hint heals drift left by an earlier dropped broadcast.
   */
  function handleApprovalsChanged(): void {
    void refresh()
  }

  const pendingCount: ComputedRef<number> = computed(() => approvals.value.length)

  function approvalsFor(agentName: string): ClaudeApproval[] {
    return approvals.value.filter(a => a.agent_name === agentName)
  }

  return { approvals, pendingCount, approvalsFor, refresh, decide, handleApprovalsChanged }
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/composables/__tests__/useClaudeApprovals.test.ts`
Expected: PASS.

Because `approvals` is a module-level singleton, tests that assert on list contents must call `refresh()` first; the provided tests all do.

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useClaudeApprovals.ts web/src/composables/__tests__/useClaudeApprovals.test.ts
git commit -m "feat(web): add the claude approvals composable"
```

---

### Task 6: Approval card component

**Files:**
- Create: `web/src/components/ClaudeApprovalCard.vue`
- Test: `web/src/components/__tests__/ClaudeApprovalCard.test.ts`

**Interfaces:**
- Consumes: the `ClaudeApproval` interface from Task 5.
- Produces: a component taking `:approval="ClaudeApproval"` and emitting `decide` with an `ApprovalDecision`.

- [ ] **Step 1: Write the failing component tests**

Create `web/src/components/__tests__/ClaudeApprovalCard.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ClaudeApprovalCard from '../ClaudeApprovalCard.vue'

const base = {
  id: 'a1', agent_name: 'codey', tool_name: 'Bash',
  summary: 'go test ./...', excerpt: '', cwd: '/tmp/huginn',
  remaining_ms: 285000, can_remember: true,
}

describe('ClaudeApprovalCard', () => {
  it('shows the tool, command and cwd', () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    expect(w.text()).toContain('Bash')
    expect(w.text()).toContain('go test ./...')
    expect(w.text()).toContain('/tmp/huginn')
  })

  it('emits allow and deny', async () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    await w.get('[data-testid="approval-allow"]').trigger('click')
    await w.get('[data-testid="approval-deny"]').trigger('click')
    expect(w.emitted('decide')).toEqual([['allow'], ['deny']])
  })

  it('offers exact-command memory only when can_remember is true', () => {
    const yes = mount(ClaudeApprovalCard, { props: { approval: base } })
    expect(yes.find('[data-testid="approval-allow-command"]').exists()).toBe(true)
    const no = mount(ClaudeApprovalCard, {
      props: { approval: { ...base, tool_name: 'Write', can_remember: false } },
    })
    expect(no.find('[data-testid="approval-allow-command"]').exists()).toBe(false)
  })

  it('requires a second click before emitting allow_tool', async () => {
    // Promotion permanently ungates the tool for this agent. It must never be
    // reachable by the same single click that grants one call.
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    await w.get('[data-testid="approval-allow-tool"]').trigger('click')
    expect(w.emitted('decide')).toBeUndefined()
    await w.get('[data-testid="approval-allow-tool-confirm"]').trigger('click')
    expect(w.emitted('decide')).toEqual([['allow_tool']])
  })

  it('labels command memory as process-scoped', () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    expect(w.get('[data-testid="approval-allow-command"]').text().toLowerCase())
      .toContain('this session')
  })

  it('renders a countdown from remaining_ms', () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: { ...base, remaining_ms: 252000 } } })
    expect(w.text()).toContain('4:12')
  })

  it('shows the excerpt when present', () => {
    const w = mount(ClaudeApprovalCard, {
      props: { approval: { ...base, tool_name: 'Write', can_remember: false, summary: '/tmp/a.ts', excerpt: 'import x' } },
    })
    expect(w.text()).toContain('import x')
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/components/__tests__/ClaudeApprovalCard.test.ts`
Expected: FAIL — cannot resolve `../ClaudeApprovalCard.vue`.

- [ ] **Step 3: Implement the component**

Create `web/src/components/ClaudeApprovalCard.vue`:

```vue
<template>
  <div
    class="rounded-lg px-3 py-2 text-xs"
    style="background:rgba(210,153,34,0.10);border:1px solid rgba(210,153,34,0.35)"
    data-testid="approval-card"
  >
    <div class="flex items-center gap-2">
      <span class="font-semibold">{{ approval.tool_name }}</span>
      <span class="opacity-60">·</span>
      <span class="font-mono truncate">{{ approval.summary }}</span>
      <span class="ml-auto font-mono opacity-70" data-testid="approval-countdown">{{ countdown }}</span>
    </div>

    <div v-if="approval.cwd" class="mt-1 opacity-60 font-mono">cwd {{ approval.cwd }}</div>

    <pre
      v-if="approval.excerpt"
      class="mt-2 max-h-32 overflow-auto whitespace-pre-wrap font-mono opacity-80"
    >{{ approval.excerpt }}</pre>

    <div class="mt-2 flex items-center gap-2">
      <button
        class="px-2 py-1 rounded font-semibold"
        style="background:rgba(46,160,67,0.20)"
        data-testid="approval-allow"
        @click="emit('decide', 'allow')"
      >Allow</button>
      <button
        class="px-2 py-1 rounded font-semibold"
        style="background:rgba(248,81,73,0.18)"
        data-testid="approval-deny"
        @click="emit('decide', 'deny')"
      >Deny</button>
    </div>

    <div class="mt-2 flex flex-col gap-1">
      <button
        v-if="approval.can_remember"
        class="text-left opacity-70 hover:opacity-100"
        data-testid="approval-allow-command"
        @click="emit('decide', 'allow_command')"
      >⤷ Always allow this command (this session)</button>

      <button
        v-if="!confirmingTool"
        class="text-left opacity-70 hover:opacity-100"
        data-testid="approval-allow-tool"
        @click="confirmingTool = true"
      >⤷ Always allow {{ approval.tool_name }} for {{ approval.agent_name }}…</button>

      <button
        v-else
        class="text-left font-semibold"
        style="color:rgba(248,81,73,0.95)"
        data-testid="approval-allow-tool-confirm"
        @click="emit('decide', 'allow_tool')"
      >⤷ Confirm: {{ approval.tool_name }} is never gated for {{ approval.agent_name }} again</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import type { ClaudeApproval, ApprovalDecision } from '../composables/useClaudeApprovals'

const props = defineProps<{ approval: ClaudeApproval }>()
const emit = defineEmits<{ (e: 'decide', d: ApprovalDecision): void }>()

// Promotion permanently ungates a tool for an agent — after it, no card ever
// appears for that tool again, and Phase 1's only undo is editing the config
// file. It therefore must not share the one-click path that grants a single
// call.
const confirmingTool = ref(false)

const countdown = computed(() => {
  const total = Math.max(0, Math.floor(props.approval.remaining_ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
})
</script>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/__tests__/ClaudeApprovalCard.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ClaudeApprovalCard.vue web/src/components/__tests__/ClaudeApprovalCard.test.ts
git commit -m "feat(web): add the claude approval card component"
```

---

### Task 7: Wire cards into chat and the nav badge

**Files:**
- Modify: `web/src/views/ChatView.vue`
- Modify: `web/src/App.vue`

**Interfaces:**
- Consumes: `useClaudeApprovals` from Task 5, `ClaudeApprovalCard` from Task 6, WS type `claude_approvals_changed` from Task 3.
- Produces: no new exports.

- [ ] **Step 1: Render cards in ChatView**

In `web/src/views/ChatView.vue`, immediately after the `autoApproveNotices` `v-for` block (around line 838, inside the same wrapper `div` that closes at line 839), add:

```vue
        <ClaudeApprovalCard
          v-for="approval in visibleApprovals"
          :key="approval.id"
          :approval="approval"
          @decide="d => onApprovalDecide(approval.id, d)"
        />
```

In the `<script setup>` block, add the import beside the other component imports:

```ts
import ClaudeApprovalCard from '../components/ClaudeApprovalCard.vue'
import { useClaudeApprovals } from '../composables/useClaudeApprovals'
```

and near the `autoApproveNotices` declaration (around line 1196):

```ts
const { approvals: claudeApprovals, decide: decideApproval, refresh: refreshApprovals, handleApprovalsChanged } =
  useClaudeApprovals()

// Cards belong to the conversation whose agent raised them. The hook only
// knows the Claude session id, and backend.ChatRequest carries no Huginn
// session id, so the server broadcasts by AGENT NAME and the routing happens
// here rather than threading a session id through every provider's request
// struct.
const visibleApprovals = computed(() =>
  claudeApprovals.value.filter(a => a.agent_name === currentAgentName.value)
)

async function onApprovalDecide(id: string, d: 'allow' | 'deny' | 'allow_command' | 'allow_tool') {
  await decideApproval(id, d)
}
```

If this component has no `currentAgentName` ref, find how it identifies the agent for the open conversation (search for `agentName` in the file) and use that expression; do not invent a new source of truth.

- [ ] **Step 2: Subscribe to the websocket hint**

Find where `ChatView.vue` registers websocket handlers (search for `useHuginnWS` around line 1089 and its `on(...)` registrations) and add a registration for the new type, following the exact shape of the neighbouring handlers:

```ts
ws.on('claude_approvals_changed', () => handleApprovalsChanged())
```

Also call `void refreshApprovals()` in the component's `onMounted`, so a freshly opened tab shows cards raised before it connected.

- [ ] **Step 2b: Refresh on websocket reconnect**

Mounting is not enough. A dropped-and-restored websocket does NOT remount the
component, so without this the client keeps a stale list until the next hint —
and after a server restart there may be no further hint at all, leaving cards on
screen for waiters that no longer exist.

`useHuginnWS` exposes a `ConnectionState` (`'connecting' | 'connected' |
'disconnected' | 'reconnecting'`) and a `connected` ref (`useHuginnWS.ts:31,75`).
Watch it and refetch on every transition into connected:

```ts
watch(() => ws.connected.value, (isConnected) => {
  if (isConnected) void refreshApprovals()
})
```

Use whichever of `connected` / `connectionState` the `ws` object in this
component actually exposes; do not add a new one.

- [ ] **Step 3: Add the nav badge**

In `web/src/App.vue`, add beside the existing `useDeliveryQueue()` destructure (line 926):

```ts
import { useClaudeApprovals } from './composables/useClaudeApprovals'
const { pendingCount: approvalCount, refresh: refreshApprovals, handleApprovalsChanged: onApprovalsChanged } =
  useClaudeApprovals()
```

Register the same websocket handler here so the badge updates while the user is on another view, call `void refreshApprovals()` on mount, and render the count using the existing badge markup at line 90 as the template — a separate badge element with `v-if="approvalCount > 0"` showing `{{ approvalCount > 9 ? '9+' : approvalCount }}`.

The badge MUST read `approvalCount` from the composable rather than keeping its own counter. `web/src/composables/unseenSessions.ts` exists because a badge that tracked its own count drifted from what it counted and stayed positive forever.

- [ ] **Step 4: Fire a desktop notification**

In `web/src/App.vue`, import the existing composable and notify when the count rises:

```ts
import { useBrowserNotifications } from './composables/useBrowserNotifications'
const { notify } = useBrowserNotifications()
watch(approvalCount, (now, before) => {
  if (now > (before ?? 0)) notify('Huginn: a tool call needs approval')
})
```

- [ ] **Step 5: Run the frontend test suite**

Run: `cd web && npx vitest run src/composables/__tests__/useClaudeApprovals.test.ts src/components/__tests__/ClaudeApprovalCard.test.ts`
Expected: PASS.

Then run any existing ChatView and App tests to confirm nothing regressed:

Run: `cd web && npx vitest run src/views/__tests__ src/components/__tests__`
Expected: PASS.

- [ ] **Step 6: Type-check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors in the files this task touched. If the repo has pre-existing errors elsewhere, ignore those and confirm none are in `useClaudeApprovals.ts`, `ClaudeApprovalCard.vue`, `ChatView.vue` or `App.vue`.

- [ ] **Step 7: Commit**

```bash
git add web/src/views/ChatView.vue web/src/App.vue
git commit -m "feat(web): show approval cards in chat and a pending badge in the nav"
```

---

### Task 8: Update the feature documentation

**Files:**
- Modify: `docs/features/claude-code-agents.md`

**Interfaces:**
- Consumes: the behaviour built in Tasks 1-7.
- Produces: nothing.

- [ ] **Step 1: Correct the now-false claims**

`docs/features/claude-code-agents.md` currently states outright that a gated-but-not-allowlisted tool is denied every time with no human prompt. That is the exact behaviour this work replaces.

Read the file, then update it so it says:

- A gated tool that is not allowlisted now raises an approval card in the agent's conversation and waits up to 285 seconds for a human.
- No answer within that window denies, and the agent continues.
- The agent's session semaphore is held for the whole wait: that one agent is frozen, others are unaffected.
- A human Allow grants a tool that is not in `--allowedTools` (verified 2026-08-27).
- "Always allow this command" is byte-exact, `Bash`-only, and lasts until Huginn restarts.
- "Always allow this tool" writes to the agent's `claude_allowed_tools` permanently; the only undo in this release is editing the agent config file.

Keep the existing both-lists audit guidance intact — it is unchanged by this work.

- [ ] **Step 2: Verify no other doc contradicts the new behaviour**

Run: `grep -rn "denied every time\|no human prompt\|never prompts" docs/`
Expected: no remaining hits describing the old behaviour. Fix any that appear.

- [ ] **Step 3: Commit**

```bash
git add docs/features/claude-code-agents.md
git commit -m "docs: describe interactive tool approval for claude-code agents"
```

---

## Manual verification (after Task 8)

Automated tests cannot cover the real CLI. Once the branch is built, verify by hand:

1. Start Huginn with a `claude-code` agent whose `claude_gated_tools` includes `Bash` and whose `claude_allowed_tools` does not.
2. Ask the agent to run a shell command.
3. A card appears in that conversation and the nav badge shows 1.
4. Click Allow. The command runs and the card disappears.
5. Repeat, click Deny. The command is blocked and the agent says so.
6. Repeat, wait 285s without clicking. The tool is denied and the agent continues.
