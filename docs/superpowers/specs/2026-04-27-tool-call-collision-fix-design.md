# Tool-Call Collision Fix — Design Spec

**Date:** 2026-04-27
**Status:** Approved
**Branch:** feature/close-gaps

---

## Problem

`OnToolCall` and `OnToolDone` callbacks in `RunLoopConfig` are keyed by tool name. When an agent calls the same tool twice in one turn (which is valid — parallel `read_file` calls, two `search` queries, etc.), the second `OnToolCall` overwrites the first entry in both capture maps. When `OnToolDone` fires for the first call, it retrieves the wrong args and the wrong `callID`, corrupting the `tool_call` / `tool_result` event pairs emitted to the frontend.

This is a **silent data corruption bug**: no crash, no error, wrong data delivered to the frontend and wrong args logged for debugging.

The same bug exists in `chat_engine.go`'s `ChatForSession`, which has an identical name-keyed capture pattern.

Additionally, a **map entry leak** exists: if a tool panics, `executeSingle`'s `defer recover()` returns early without calling `OnToolDone`, leaving a `toolArgsCapture[callID]` entry orphaned for the lifetime of the turn.

---

## Scope

**In scope:**
- Change `RunLoopConfig.OnToolCall` and `OnToolDone` signatures to include `callID string` as first parameter
- Generate `callID` from `tc.ID` (LLM-provided tool call ID) in `executeSingle`, with fallback for empty IDs
- Key capture maps in `agent_dispatcher.go` and `chat_engine.go` by `callID`
- Eliminate `toolCallIDCapture` map (redundant once callID flows as a parameter)
- Delete capture map entries after `OnToolDone` to prevent leak
- Call `OnToolDone` from inside the panic recovery path in `executeSingle`
- Update synthetic prefetch callbacks in `TaskWithAgent` for signature consistency
- Update all tests; add `TestRunLoop_SameToolTwiceInOneTurn`

**Out of scope:**
- No behavioral changes for the common case (one call per tool per turn)
- No API changes, no schema changes, no database changes
- No frontend changes (the frontend already uses the `id` field correctly)

---

## Architecture

### Why `tc.ID` instead of `time.Now().UnixNano()`

`executeSingle` receives `tc backend.ToolCall`, which carries an `ID` field populated by the LLM provider (OpenAI: `call_abc123`, Anthropic: `toolu_xyz`). This ID is:
- Guaranteed unique per turn by the LLM provider
- Already correlated with the tool result message sent back to the model
- Already present in the code — `tc.ID` is available at the call site

Using a nanosecond timestamp has a real collision risk: two goroutines dispatched from the same `dispatchTools` call can get the same nanosecond on a fast machine. An atomic counter would fix that but adds unnecessary complexity when the LLM already provides a unique ID.

**Fallback for empty IDs** (some Ollama models omit `tc.ID`):
```go
callID := tc.ID
if callID == "" {
    callID = fmt.Sprintf("tc-%d-%d-%s", time.Now().UnixNano(), idx, tc.Function.Name)
}
```
The `idx` (positional index within the batch) eliminates timestamp collision in the fallback path.

---

## Blast Radius — All Files With Signature Changes

The `OnToolCall`/`OnToolDone` signatures thread through more files than just the primary bug sites. The Go compiler will catch all of them, but they are documented here to prevent surprises during review:

| File | Change type |
|------|-------------|
| `internal/agent/loop.go` | Struct definition + call sites |
| `internal/agent/agent_dispatcher.go` | `ChatWithAgent` closures (bug fix), `TaskWithAgent` prefetch, `Dispatch()` method signature |
| `internal/agent/chat_engine.go` | `ChatForSession` closures (bug fix) |
| `internal/agent/orchestrator.go` | Passthrough signature update |
| `internal/agent/mcp_agent_chat.go` | Passthrough signature update |
| `internal/agent/debug_loop.go` | `DebugLoop` accepts old signatures as params — update |
| `internal/server/ws.go` | Caller that passes closures — update closure signatures |
| `internal/tui/stream_handler.go` | Caller that passes closures — update closure signatures |
| `main.go` | Caller — update closure signatures if any |
| `init_relay.go` | Caller — update closure signatures if any |
| Test files (loop_test.go, observability_test.go, agent_e2e_test.go, others) | Update callback signatures |

---

## File Changes

### `internal/agent/loop.go`

**1. Update `RunLoopConfig` struct** (lines 41–42):
```go
// Before:
OnToolCall func(name string, args map[string]any)
OnToolDone func(name string, result tools.ToolResult)

// After:
OnToolCall func(callID string, name string, args map[string]any)
OnToolDone func(callID string, name string, result tools.ToolResult)
```

**2. Resolve `callID` at top of `executeSingle`**, before the panic defer so the defer can also use it. Add after line 105 (`toolName := tc.Function.Name`):
```go
callID := tc.ID
if callID == "" {
    callID = fmt.Sprintf("tc-%d-%d-%s", time.Now().UnixNano(), idx, toolName)
}
```

**3. Update `OnToolCall` call** (line 156–158):
```go
if cfg.OnToolCall != nil {
    cfg.OnToolCall(callID, toolName, argsMap)
}
```

**4. Update `OnToolDone` call** (line 200–202):
```go
if cfg.OnToolDone != nil {
    cfg.OnToolDone(callID, toolName, toolResult)
}
```

**5. Call `OnToolDone` in panic recovery** (lines 90–103) so map entries are not leaked:
```go
defer func() {
    if r := recover(); r != nil {
        slog.Error("tool: panic in executeSingle", ...)
        panicResult := dispatchedResult{...}
        result = panicResult
        // Ensure OnToolDone fires so callers can clean up in-flight state.
        if cfg.OnToolDone != nil {
            cfg.OnToolDone(callID, toolName, tools.ToolResult{
                Output:  fmt.Sprintf("error: tool %s panicked: %v", toolName, r),
                IsError: true,
                Error:   fmt.Sprintf("tool %s panicked: %v", toolName, r),
            })
        }
    }
}()
```

Note: `callID` and `toolName` must be resolved before the defer (move them before it).

---

### `internal/agent/agent_dispatcher.go`

**In `ChatWithAgent`** (lines 716–781):

Remove `toolCallIDCapture` map entirely. Change `toolArgsCapture` key from tool name to callID.

```go
var toolArgsMu sync.Mutex
// toolArgsCapture stores args keyed by callID (from RunLoopConfig.OnToolCall).
// Entries are deleted in OnToolDone to prevent unbounded growth per turn.
toolArgsCapture := make(map[string]map[string]any)

// ...

OnToolCall: func(callID string, name string, args map[string]any) {
    slog.Info("tool call started", "agent", ag.Name, "tool", name,
        "session_id", sessionID, "call_id", callID)
    toolArgsMu.Lock()
    toolArgsCapture[callID] = args
    toolArgsMu.Unlock()
    if onToolEvent != nil {
        onToolEvent("tool_call", map[string]any{"tool": name, "args": args})
    } else if onEvent != nil {
        onEvent(backend.StreamEvent{
            Type:    backend.StreamToolCall,
            Payload: map[string]any{"id": callID, "tool": name, "args": args},
        })
    }
},
OnToolDone: func(callID string, name string, result tools.ToolResult) {
    toolArgsMu.Lock()
    capturedArgs := toolArgsCapture[callID]
    delete(toolArgsCapture, callID)
    toolArgsMu.Unlock()
    slog.Info("tool call done", "agent", ag.Name, "tool", name,
        "session_id", sessionID, "call_id", callID, "success", result.Error == "")
    if onToolEvent != nil {
        onToolEvent("tool_result", map[string]any{"tool": name, "result": result.Output})
    } else if onEvent != nil {
        onEvent(backend.StreamEvent{
            Type: backend.StreamToolResult,
            Payload: map[string]any{
                "id":      callID,
                "tool":    name,
                "success": result.Error == "",
                "result":  result.Output,
                "args":    capturedArgs,
            },
        })
    }
},
```

**In `TaskWithAgent`** prefetch callbacks (lines ~460–469): add synthetic `callID`:
```go
callID := fmt.Sprintf("prefetch-%s-%d", toolName, time.Now().UnixNano())
onToolCall(callID, toolName, args)
// ...
onToolDone(callID, toolName, tools.ToolResult{Output: output})
```

---

### `internal/agent/chat_engine.go`

`ChatForSession` has the same name-keyed capture pattern (lines 207–216). Apply identical fix:
- Add `callID` as first param to both closures
- Key any capture maps by `callID`
- Delete entries in `OnToolDone`

---

### `internal/agent/orchestrator.go`, `mcp_agent_chat.go`

These pass through outer callbacks — they don't have inline capture logic. Update the closure signatures to accept `callID string` as first param (threaded through to callers):

```go
// Before:
onToolCall = func(name string, args map[string]any) { ... }
onToolDone = func(name string, result tools.ToolResult) { ... }

// After:
onToolCall = func(callID string, name string, args map[string]any) { ... }
onToolDone = func(callID string, name string, result tools.ToolResult) { ... }
```

---

## Tests

### Existing tests to update

| File | Change |
|------|--------|
| `internal/agent/loop_test.go` | Add `callID string` first param to `OnToolCall`/`OnToolDone` closures |
| `internal/agent/observability_test.go` | Same |
| `internal/integration/agent_e2e_test.go` | Same |

### New test: `TestRunLoop_PanicPath_OnToolDoneStillFires`

**File:** `internal/agent/loop_test.go`

Register a tool that panics. Verify:
1. `OnToolDone` is still called (panic recovery path)
2. The `callID` matches what was emitted by `OnToolCall`
3. The result has `IsError: true` and a message containing "panicked"
4. No map entry is leaked (capture map is empty after the call)

### New test: `TestRunLoop_SameToolTwiceInOneTurn`

**File:** `internal/agent/loop_test.go`

The mock backend returns two calls to the same tool (`echo_tool`) in one turn's response:
```go
ToolCalls: []backend.ToolCall{
    {ID: "call-1", Function: backend.ToolCallFunction{Name: "echo_tool", Arguments: map[string]any{"msg": "first"}}},
    {ID: "call-2", Function: backend.ToolCallFunction{Name: "echo_tool", Arguments: map[string]any{"msg": "second"}}},
},
```

Then verify:
1. `OnToolCall` fires twice with distinct `callID` values (`"call-1"`, `"call-2"`)
2. `OnToolDone` fires twice with the correct corresponding `callID` and `args`:
   - `callID="call-1"` → `args["msg"] == "first"`
   - `callID="call-2"` → `args["msg"] == "second"`
3. Neither result's `capturedArgs` is the last-write-wins value

---

## Success Criteria

- The same tool called twice in one LLM turn emits two distinct `tool_call` / `tool_result` event pairs, each with correct `id` and `args`
- All existing tests pass
- `go test -race ./internal/agent/... ./internal/integration/...` is clean
- No map entry leaks: `toolArgsCapture` is empty after all `OnToolDone` callbacks fire
- Panic in a tool still fires `OnToolDone` with an error result (no orphaned entry)
