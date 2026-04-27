# Delegation Record Wiring — Design Spec

**Date:** 2026-04-27
**Status:** Approved
**Branch:** feature/memory-wiring

---

## Problem

`DelegationStore.InsertDelegation` is implemented but never called from any production path. The `delegations` SQLite table is perpetually empty, so the delegation chain feature from WS2 (`GET /api/v1/messages/:id/thread` returning `delegation_chain`) always returns `[]` unless the client happens to have received live WebSocket `thread_started` events. This makes the backend enrichment from WS2 useless in practice.

The root cause: `DelegateFn` in `main.go` (the closure passed to `DelegateToAgentTool`) creates threads via `tm.Create()` but never calls `InsertDelegation`. Additionally, when `DelegateFn` runs, it does not know the *calling* agent's name — only the *target* agent name comes through `DelegateParams`.

---

## Scope

**In scope:**
- New file `internal/threadmgr/context.go` with `SetCallingAgent` / `GetCallingAgent` context helpers
- Set calling agent on context in `internal/server/ws.go` (primary agent path)
- Set calling agent on context in `internal/threadmgr/spawn.go` (spawned sub-thread path)
- Call `InsertDelegation` in `main.go`'s `DelegateFn` after `tm.Create()`
- Tests: unit test for context helpers, `TestDelegateFn_InsertsRecord`

**Out of scope:**
- Schema changes — `delegations` table already exists
- `UpdateDelegationStatus` / `FindDelegationByThread` behavior — already called by `MakeThreadEventEmitter`, no changes needed
- UI changes — WS2 already handles delegation chain display
- Any changes to `DelegateParams` struct

---

## Architecture

### Why a context key in `threadmgr`

When `DelegateFn` fires, the call stack is:

```
ws.go: ChatWithAgent(chatCtx, ...)
  → agent/loop.go: RunLoop
    → executeSingle(ctx, ...)
      → tryExecuteTool(ctx, ...)
        → tool.Execute(ctx, args)   ← DelegateToAgentTool.Execute
          → d.Fn(ctx, DelegateParams{AgentName, Task, ...})  ← DelegateFn in main.go
```

`DelegateParams` carries only the *target* agent name. The *calling* agent name must travel via context.

For spawned sub-threads the path is:

```
threadmgr/spawn.go: runOnce(ctx, agentID, ...)
  → runtime.ExecuteTool(ctx, toolName, args)  ← DelegateFn
```

`agentID` is already a parameter of `runOnce`.

The calling-agent context key must live in the `threadmgr` package because:
1. `ws.go` can import `threadmgr` (no cycle)
2. `spawn.go` is already in `threadmgr` (trivially no cycle)
3. `main.go` can import `threadmgr`
4. The `agent` package cannot be imported by `threadmgr` (would be circular — `agent` already imports `threadmgr`)

---

## File Changes

### `internal/threadmgr/context.go` (NEW)

```go
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

---

### `internal/server/ws.go`

In the primary agent chat path (around line 929), after `agent.SetParentMessageID`:

```go
chatCtx = agent.SetParentMessageID(chatCtx, userMsgID)
// Set calling agent so DelegateFn can record FromAgent.
if ag != nil {
    chatCtx = threadmgr.SetCallingAgent(chatCtx, ag.Name)
}
```

Add `"github.com/scrypster/huginn/internal/threadmgr"` to the import block.

---

### `internal/threadmgr/spawn.go`

In `runOnce`, before calling `runtime.ExecuteTool` (around line 694), enrich the context:

```go
case runtime != nil && runtime.ExecuteTool != nil:
    toolCtx := SetCallingAgent(ctx, agentID)
    result, execErr = runtime.ExecuteTool(toolCtx, tc.Function.Name, tc.Function.Arguments)
```

`SetCallingAgent` is in the same package, so no import needed.

---

### `main.go`

In `DelegateFn` (lines 2737–2831), after `tm.Create()` succeeds (currently around line 2778):

```go
// Record delegation so the frontend can hydrate delegation_chain on panel open.
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

`session.NewID()` is the ULID generator already used elsewhere. `Status: "pending"` matches the initial state expected by `UpdateDelegationStatus`.

---

## Tests

### `internal/threadmgr/context_test.go` (NEW)

```go
func TestSetGetCallingAgent_RoundTrip(t *testing.T) {
    ctx := SetCallingAgent(context.Background(), "Atlas")
    got := GetCallingAgent(ctx)
    if got != "Atlas" { t.Errorf("want Atlas, got %q", got) }
}

func TestGetCallingAgent_Empty(t *testing.T) {
    got := GetCallingAgent(context.Background())
    if got != "" { t.Errorf("want empty, got %q", got) }
}
```

### `TestDelegateFn_InsertsRecord` (new test file or in existing integration)

Use a stub/fake `delegationStore` (same pattern as `stubDelegationStore` in `handlers_threads_test.go`).
Set up a `DelegateFn` closure with:
- A mock `threadmgr.Manager` that returns a known `Thread`
- A fake `delegationStore` that captures `InsertDelegation` calls

Call `DelegateFn` with a context enriched via `threadmgr.SetCallingAgent(ctx, "Atlas")` and params `{AgentName: "Coder", Task: "write code"}`.

Assert:
1. `InsertDelegation` was called once
2. `rec.FromAgent == "Atlas"`
3. `rec.ToAgent == "Coder"`
4. `rec.SessionID` and `rec.ThreadID` are non-empty

---

## File Layout

```
internal/
  threadmgr/
    context.go         ← NEW (SetCallingAgent / GetCallingAgent)
    context_test.go    ← NEW (round-trip tests)
    spawn.go           ← MODIFIED (enrich toolCtx before ExecuteTool)
  server/
    ws.go              ← MODIFIED (set calling agent on chatCtx)
main.go                ← MODIFIED (InsertDelegation after tm.Create)
```

---

## Success Criteria

- After a delegation, a row exists in the `delegations` table with correct `from_agent`, `to_agent`, `session_id`, `thread_id`
- `GET /api/v1/messages/:id/thread` returns a non-empty `delegation_chain` for threads that involved delegation (without requiring any live WS events)
- `fromAgent == ""` does NOT insert a delegation record (guard condition)
- `go test ./internal/threadmgr/... ./internal/server/...` passes
- `go build ./...` passes
