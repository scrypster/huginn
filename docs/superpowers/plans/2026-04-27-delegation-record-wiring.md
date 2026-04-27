# Delegation Record Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire `InsertDelegation` so delegation records are created whenever `DelegateFn` fires, enabling the WS2 delegation chain feature to return real data from the backend.

**Architecture:** New `threadmgr.SetCallingAgent`/`GetCallingAgent` context helpers carry the calling agent's name through the tool-call stack to `DelegateFn` in `main.go`. `DelegateFn` calls `InsertDelegation` after `tm.Create()`. Two set-points: `ws.go` (primary agent path) and `spawn.go:runOnce` (spawned sub-thread path).

**Tech Stack:** Go, SQLite, existing `session.DelegationStore` interface, `session.NewID()` for ULID generation.

---

### Task 1: Add `SetCallingAgent`/`GetCallingAgent` to `threadmgr`

**Files:**
- Create: `internal/threadmgr/context.go`
- Create: `internal/threadmgr/context_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/threadmgr/context_test.go
package threadmgr

import (
	"context"
	"testing"
)

func TestSetGetCallingAgent_RoundTrip(t *testing.T) {
	ctx := SetCallingAgent(context.Background(), "Atlas")
	got := GetCallingAgent(ctx)
	if got != "Atlas" {
		t.Errorf("want Atlas, got %q", got)
	}
}

func TestGetCallingAgent_Empty(t *testing.T) {
	got := GetCallingAgent(context.Background())
	if got != "" {
		t.Errorf("want empty string when not set, got %q", got)
	}
}

func TestSetCallingAgent_Overwrite(t *testing.T) {
	ctx := SetCallingAgent(context.Background(), "Atlas")
	ctx = SetCallingAgent(ctx, "Coder")
	got := GetCallingAgent(ctx)
	if got != "Coder" {
		t.Errorf("want Coder after overwrite, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/threadmgr/ -run TestSetGetCallingAgent -v 2>&1 | head -20`
Expected: compilation error — `SetCallingAgent undefined`

- [ ] **Step 3: Implement context helpers**

```go
// internal/threadmgr/context.go
package threadmgr

import "context"

type callingAgentKey struct{}

// SetCallingAgent stores the calling agent's name in ctx so that tools
// (e.g. DelegateToAgentTool) can record which agent initiated the delegation.
func SetCallingAgent(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, callingAgentKey{}, name)
}

// GetCallingAgent retrieves the calling agent's name from ctx.
// Returns "" if not set.
func GetCallingAgent(ctx context.Context) string {
	name, _ := ctx.Value(callingAgentKey{}).(string)
	return name
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/threadmgr/ -run "TestSetGetCallingAgent|TestGetCallingAgent_Empty|TestSetCallingAgent_Overwrite" -v`
Expected: PASS all 3 tests

- [ ] **Step 5: Verify build is clean**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./...`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add internal/threadmgr/context.go internal/threadmgr/context_test.go
git commit -m "feat(threadmgr): add SetCallingAgent/GetCallingAgent context helpers"
```

---

### Task 2: Set calling agent in `ws.go` and `spawn.go`

**Files:**
- Modify: `internal/server/ws.go`
- Modify: `internal/threadmgr/spawn.go`

Context: `ws.go` handles the primary agent chat path. After setting `ParentMessageID` (around line 929), we add `SetCallingAgent`. In `spawn.go:runOnce`, the tool dispatch is around line 694 — enrich `ctx` before passing to `ExecuteTool`.

- [ ] **Step 1: Read the relevant sections**

Read `internal/server/ws.go` lines 920–945 to confirm the exact location and variable names.
Read `internal/threadmgr/spawn.go` lines 685–710 to confirm the ExecuteTool call site.

- [ ] **Step 2: Update `ws.go`**

In `internal/server/ws.go`, find the block that sets `chatCtx`:
```go
chatCtx = agent.SetParentMessageID(chatCtx, userMsgID)
```

Add immediately after it:
```go
// Set calling agent so DelegateFn can record the delegation's FromAgent.
if ag != nil {
    chatCtx = threadmgr.SetCallingAgent(chatCtx, ag.Name)
}
```

Add `"github.com/scrypster/huginn/internal/threadmgr"` to the import block in `ws.go`.

- [ ] **Step 3: Update `spawn.go`**

In `internal/threadmgr/spawn.go`, find the case that dispatches `runtime.ExecuteTool`. It looks like:
```go
case runtime != nil && runtime.ExecuteTool != nil:
    result, execErr = runtime.ExecuteTool(ctx, tc.Function.Name, tc.Function.Arguments)
```

Change it to:
```go
case runtime != nil && runtime.ExecuteTool != nil:
    toolCtx := SetCallingAgent(ctx, agentID)
    result, execErr = runtime.ExecuteTool(toolCtx, tc.Function.Name, tc.Function.Arguments)
```

`SetCallingAgent` is in the same package (`threadmgr`) — no import needed.
`agentID` is a parameter of `runOnce` — already available.

- [ ] **Step 4: Verify build is clean**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./...`
Expected: no errors

- [ ] **Step 5: Run threadmgr and server tests**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/threadmgr/... ./internal/server/... -count=1 2>&1 | tail -20`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add internal/server/ws.go internal/threadmgr/spawn.go
git commit -m "feat(delegation): set calling agent on context in ws.go and spawn.go"
```

---

### Task 3: Call `InsertDelegation` in `main.go:DelegateFn`

**Files:**
- Modify: `main.go`

Context: `DelegateFn` is the closure passed to `DelegateToAgentTool`. It's defined around lines 2737–2831. After `tm.Create()` (around line 2778), we insert the delegation record. `srv.delegationStore` is already used by `MakeThreadEventEmitter` in `server.go`.

- [ ] **Step 1: Read the relevant section**

Read `main.go` lines 2737–2800 to locate the exact position of `tm.Create()` and what variables are in scope (`sessionID`, `t`, `srv`, `p`).

- [ ] **Step 2: Add `InsertDelegation` call after `tm.Create()`**

After the successful `tm.Create()` call and thread variable `t` is set, add:

```go
// Record the delegation so delegation_chain is populated for panel-open hydration.
fromAgent := threadmgr.GetCallingAgent(ctx)
if srv.delegationStore != nil && fromAgent != "" {
    rec := session.DelegationRecord{
        ID:        session.NewID(),
        SessionID: sessionID,
        ThreadID:  t.ID,
        FromAgent: fromAgent,
        ToAgent:   p.AgentName,
        Task:      p.Task,
        Status:    "pending",
    }
    if err := srv.delegationStore.InsertDelegation(rec); err != nil {
        logger.Warn("delegate_to_agent: failed to insert delegation record",
            "err", err, "from", fromAgent, "to", p.AgentName)
    }
}
```

Verify that `session.NewID()` is already imported (it will be — `session` is already imported in `main.go`).
Verify that `threadmgr` is already imported (it will be — `tm` is of type `*threadmgr.Manager`).

- [ ] **Step 3: Verify build is clean**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add main.go
git commit -m "feat(delegation): call InsertDelegation in DelegateFn so records are actually created"
```

---

### Task 4: Add `TestDelegateFn_InsertsRecord`

**Files:**
- Create: `internal/threadmgr/delegate_integration_test.go`

Context: `DelegateFn` is defined in `main.go` so we can't directly import it from a test. Instead we test the contract: a closure with the same shape as `DelegateFn` using a fake `delegationStore`. This test validates the wiring pattern — that `SetCallingAgent`/`GetCallingAgent` round-trips correctly and that a `DelegationStore` mock gets called with the right data.

The test lives in `internal/threadmgr` because that's where the context helpers live and where `DelegateFn` signature types live.

- [ ] **Step 1: Write the failing test**

```go
// internal/threadmgr/delegate_integration_test.go
package threadmgr_test

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// fakeDelegationStore captures InsertDelegation calls for assertion.
type fakeDelegationStore struct {
	inserted []session.DelegationRecord
}

func (f *fakeDelegationStore) InsertDelegation(d session.DelegationRecord) error {
	f.inserted = append(f.inserted, d)
	return nil
}

func (f *fakeDelegationStore) FindDelegationByThread(threadID string) (*session.DelegationRecord, error) {
	return nil, nil
}

func (f *fakeDelegationStore) UpdateDelegationStatus(threadID, status, result string) error {
	return nil
}

func (f *fakeDelegationStore) ListDelegationsBySession(sessionID string, limit, offset int) ([]session.DelegationRecord, error) {
	return nil, nil
}

func (f *fakeDelegationStore) ListDelegationsByThread(threadID string) ([]session.DelegationRecord, error) {
	return nil, nil
}

func (f *fakeDelegationStore) DeleteDelegation(id string) error {
	return nil
}

func TestCallingAgentContext_DelegateFnPattern(t *testing.T) {
	store := &fakeDelegationStore{}

	// Simulate the DelegateFn pattern: context carries calling agent name,
	// and a delegation record is inserted after thread creation.
	simulateDelegateFn := func(ctx context.Context, agentName, task, sessionID, threadID string) {
		fromAgent := threadmgr.GetCallingAgent(ctx)
		if store != nil && fromAgent != "" {
			rec := session.DelegationRecord{
				ID:        session.NewID(),
				SessionID: sessionID,
				ThreadID:  threadID,
				FromAgent: fromAgent,
				ToAgent:   agentName,
				Task:      task,
				Status:    "pending",
			}
			if err := store.InsertDelegation(rec); err != nil {
				t.Fatalf("InsertDelegation failed: %v", err)
			}
		}
	}

	ctx := threadmgr.SetCallingAgent(context.Background(), "Atlas")
	simulateDelegateFn(ctx, "Coder", "write the handler", "sess-1", "thread-1")

	if len(store.inserted) != 1 {
		t.Fatalf("expected 1 inserted record, got %d", len(store.inserted))
	}

	rec := store.inserted[0]
	if rec.FromAgent != "Atlas" {
		t.Errorf("FromAgent: want Atlas, got %q", rec.FromAgent)
	}
	if rec.ToAgent != "Coder" {
		t.Errorf("ToAgent: want Coder, got %q", rec.ToAgent)
	}
	if rec.SessionID != "sess-1" {
		t.Errorf("SessionID: want sess-1, got %q", rec.SessionID)
	}
	if rec.ThreadID != "thread-1" {
		t.Errorf("ThreadID: want thread-1, got %q", rec.ThreadID)
	}
	if rec.Task != "write the handler" {
		t.Errorf("Task: want 'write the handler', got %q", rec.Task)
	}
	if rec.ID == "" {
		t.Error("ID should be non-empty (ULID)")
	}
}

func TestCallingAgentContext_NoInsertWhenEmpty(t *testing.T) {
	store := &fakeDelegationStore{}

	// When no calling agent is set, no record should be inserted.
	simulateDelegateFn := func(ctx context.Context) {
		fromAgent := threadmgr.GetCallingAgent(ctx)
		if store != nil && fromAgent != "" {
			store.InsertDelegation(session.DelegationRecord{}) //nolint
		}
	}

	simulateDelegateFn(context.Background()) // no SetCallingAgent called

	if len(store.inserted) != 0 {
		t.Errorf("expected 0 inserts when FromAgent is empty, got %d", len(store.inserted))
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/threadmgr/... -run "TestCallingAgentContext" -v`
Expected: PASS

- [ ] **Step 3: Run full threadmgr suite**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/threadmgr/... -count=1 2>&1 | tail -10`
Expected: all pass

- [ ] **Step 4: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add internal/threadmgr/delegate_integration_test.go
git commit -m "test(delegation): validate InsertDelegation wiring pattern with fake store"
```

---

### Task 5: Final verification

- [ ] **Step 1: Full build**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./...`
Expected: no errors

- [ ] **Step 2: Race-safe test run**

Run: `cd /Users/mjbonanno/github.com/scrypster/huginn && go test -race ./internal/threadmgr/... ./internal/server/... -count=1 2>&1 | tail -20`
Expected: all pass, no race conditions

- [ ] **Step 3: Commit if any remaining changes**

If no uncommitted changes, skip. Otherwise:
```bash
git add -p
git commit -m "chore: final cleanup for delegation record wiring"
```
