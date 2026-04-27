# Tool-Call Collision Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Key in-flight tool call tracking maps by `callID` (from the LLM's `tc.ID`) instead of tool name, so the same tool called twice in one turn emits correct, non-colliding `tool_call`/`tool_result` event pairs.

**Architecture:** Move `callID` resolution into `executeSingle` in `loop.go` (using `tc.ID` with a positional fallback), thread it through the `OnToolCall`/`OnToolDone` callback signatures, and update both inline-closure bug sites (`agent_dispatcher.go` `ChatWithAgent` and `chat_engine.go` `ChatForSession`) plus all pass-through callers. The compiler enforces complete coverage — a build failure means a missed caller.

**Tech Stack:** Go, `internal/agent` package, `sync.Mutex`, `backend.ToolCall.ID`

---

## File Map

| File | Change |
|------|--------|
| `internal/agent/loop.go` | Struct signatures, `executeSingle` callID resolution, panic-path `OnToolDone`, call sites |
| `internal/agent/agent_dispatcher.go` | `ChatWithAgent` inline closures (bug fix + map cleanup), `TaskWithAgent` prefetch synthetic callID, pass-through param types |
| `internal/agent/chat_engine.go` | `ChatForSession` inline closures (bug fix) |
| `internal/agent/orchestrator.go` | Pass-through param types |
| `internal/agent/mcp_agent_chat.go` | `AgentChat` param types |
| `internal/agent/debug_loop.go` | `DebugLoop` param types (lines 27–28) |
| `internal/agent/loop_test.go` | New tests + update existing callback signatures |
| `internal/agent/observability_test.go` | Update callback signatures |
| `internal/integration/agent_e2e_test.go` | Update callback signatures |

---

## Task 1: Write failing tests for the new behavior

**Files:**
- Modify: `internal/agent/loop_test.go`

These tests use the new `func(callID string, ...)` signatures so they will **not compile** until Task 2 is done. Write them first so TDD drives the implementation.

- [ ] **Step 1: Add `TestRunLoop_SameToolTwiceInOneTurn` to `internal/agent/loop_test.go`**

Locate the end of the file (after the last test function) and add:

```go
// TestRunLoop_SameToolTwiceInOneTurn verifies that when the LLM returns two calls
// to the same tool in a single turn, each OnToolCall/OnToolDone pair carries the
// correct callID and args — no last-write-wins collision.
func TestRunLoop_SameToolTwiceInOneTurn(t *testing.T) {
	t.Parallel()

	tool := &mockTool{
		name:   "echo_tool",
		result: tools.ToolResult{Output: "echoed"},
	}
	// Backend returns two calls to echo_tool with distinct IDs and distinct args.
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_use",
				ToolCalls: []backend.ToolCall{
					{ID: "call-aaa", Function: backend.ToolCallFunction{Name: "echo_tool", Arguments: map[string]any{"msg": "first"}}},
					{ID: "call-bbb", Function: backend.ToolCallFunction{Name: "echo_tool", Arguments: map[string]any{"msg": "second"}}},
				},
			},
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	type callPair struct {
		callID string
		args   map[string]any
	}
	var mu sync.Mutex
	var calls []callPair
	var dones []callPair

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "run echo_tool twice"}},
		OnToolCall: func(callID string, name string, args map[string]any) {
			mu.Lock()
			calls = append(calls, callPair{callID: callID, args: args})
			mu.Unlock()
		},
		OnToolDone: func(callID string, name string, result tools.ToolResult) {
			mu.Lock()
			dones = append(dones, callPair{callID: callID})
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 OnToolCall invocations, got %d", len(calls))
	}
	if len(dones) != 2 {
		t.Fatalf("expected 2 OnToolDone invocations, got %d", len(dones))
	}

	// callIDs must be distinct.
	if calls[0].callID == calls[1].callID {
		t.Errorf("expected distinct callIDs, both were %q", calls[0].callID)
	}

	// Args must match the original call, not the last-write-wins value.
	seenArgs := make(map[string]string) // callID → msg
	for _, c := range calls {
		if msg, ok := c.args["msg"].(string); ok {
			seenArgs[c.callID] = msg
		}
	}
	if seenArgs["call-aaa"] != "first" {
		t.Errorf("expected call-aaa args msg=first, got %q", seenArgs["call-aaa"])
	}
	if seenArgs["call-bbb"] != "second" {
		t.Errorf("expected call-bbb args msg=second, got %q", seenArgs["call-bbb"])
	}

	// OnToolDone callIDs must match what was announced in OnToolCall.
	doneIDs := make(map[string]bool)
	for _, d := range dones {
		doneIDs[d.callID] = true
	}
	if !doneIDs["call-aaa"] {
		t.Error("expected OnToolDone for call-aaa")
	}
	if !doneIDs["call-bbb"] {
		t.Error("expected OnToolDone for call-bbb")
	}
}
```

- [ ] **Step 2: Add `TestRunLoop_PanicPath_OnToolDoneStillFires` to `internal/agent/loop_test.go`**

Add immediately after the test above:

```go
// TestRunLoop_PanicPath_OnToolDoneStillFires verifies that when a tool panics,
// OnToolDone is still called with an error result and the correct callID.
// This ensures callers can clean up in-flight state even on panic.
func TestRunLoop_PanicPath_OnToolDoneStillFires(t *testing.T) {
	t.Parallel()

	panicTool := &mockTool{
		name:       "panic_tool",
		shouldPanic: true,
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_use",
				ToolCalls: []backend.ToolCall{
					{ID: "call-panic-1", Function: backend.ToolCallFunction{Name: "panic_tool", Arguments: map[string]any{}}},
				},
			},
			stopResponse("recovered"),
		},
	}
	reg := newRegistryWith(panicTool)

	var doneCalled bool
	var doneCallID string
	var doneIsError bool

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "trigger panic"}},
		OnToolCall: func(callID string, name string, args map[string]any) {},
		OnToolDone: func(callID string, name string, result tools.ToolResult) {
			doneCalled = true
			doneCallID = callID
			doneIsError = result.IsError
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !doneCalled {
		t.Fatal("expected OnToolDone to be called after tool panic")
	}
	if doneCallID != "call-panic-1" {
		t.Errorf("expected doneCallID=call-panic-1, got %q", doneCallID)
	}
	if !doneIsError {
		t.Error("expected OnToolDone result.IsError=true after panic")
	}
}
```

- [ ] **Step 3: Add `shouldPanic` field to `mockTool` in `loop_test.go`**

Find the `mockTool` struct definition in `loop_test.go` and add the `shouldPanic` field, and handle it in `Execute`:

```go
type mockTool struct {
	name        string
	result      tools.ToolResult
	shouldPanic bool     // add this field
}

func (m *mockTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	if m.shouldPanic {
		panic(fmt.Sprintf("mockTool %s intentional panic", m.name))
	}
	return m.result
}
```

- [ ] **Step 4: Verify tests do not compile yet**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test ./internal/agent/... 2>&1 | head -30
```

Expected: compile errors about `func(callID string, name string, ...)` vs `func(name string, ...)` mismatches. This confirms the tests are driving the implementation correctly.

---

## Task 2: Update `loop.go` — struct signatures, `executeSingle`, call sites

**Files:**
- Modify: `internal/agent/loop.go`

- [ ] **Step 1: Update `RunLoopConfig` callback signatures** (lines 41–42)

Find:
```go
	OnToolCall         func(name string, args map[string]any)
	OnToolDone         func(name string, result tools.ToolResult)
```

Replace with:
```go
	OnToolCall         func(callID string, name string, args map[string]any)
	OnToolDone         func(callID string, name string, result tools.ToolResult)
```

- [ ] **Step 2: Reorder declarations and add `callID` resolution in `executeSingle`**

The current `executeSingle` starts like this (lines 89–110):
```go
func (cfg *RunLoopConfig) executeSingle(ctx context.Context, idx int, tc backend.ToolCall, writeMu *sync.Mutex) (result dispatchedResult) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("tool: panic in executeSingle",
				"tool", tc.Function.Name,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			result = dispatchedResult{
				index:   idx,
				tc:      tc,
				content: fmt.Sprintf("error: tool %s panicked: %v", tc.Function.Name, r),
			}
		}
	}()

	toolName := tc.Function.Name
	argsMap := tc.Function.Arguments

	makeResult := func(content string) dispatchedResult {
		return dispatchedResult{index: idx, tc: tc, content: content}
	}
```

Replace the entire block from the function signature through `makeResult` with:
```go
func (cfg *RunLoopConfig) executeSingle(ctx context.Context, idx int, tc backend.ToolCall, writeMu *sync.Mutex) (result dispatchedResult) {
	// Resolve tool name and call ID before the panic defer so the defer
	// can reference them safely. tc.ID is the LLM-provider-assigned call ID
	// (e.g. "call_abc123" from OpenAI, "toolu_xyz" from Anthropic).
	// Fall back to a positional ID when the provider omits it (some Ollama models).
	toolName := tc.Function.Name
	argsMap := tc.Function.Arguments
	callID := tc.ID
	if callID == "" {
		callID = fmt.Sprintf("tc-%d-%d-%s", time.Now().UnixNano(), idx, toolName)
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("tool: panic in executeSingle",
				"tool", toolName,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			result = dispatchedResult{
				index:   idx,
				tc:      tc,
				content: fmt.Sprintf("error: tool %s panicked: %v", toolName, r),
			}
			// Fire OnToolDone so callers can clean up in-flight state (e.g.
			// remove the entry from their capture map). Without this, a panic
			// would leave an orphaned map entry for the lifetime of the turn.
			if cfg.OnToolDone != nil {
				cfg.OnToolDone(callID, toolName, tools.ToolResult{
					Output:  fmt.Sprintf("error: tool %s panicked: %v", toolName, r),
					IsError: true,
					Error:   fmt.Sprintf("tool %s panicked: %v", toolName, r),
				})
			}
		}
	}()

	makeResult := func(content string) dispatchedResult {
		return dispatchedResult{index: idx, tc: tc, content: content}
	}
```

- [ ] **Step 3: Update `OnToolCall` and `OnToolDone` invocations in `executeSingle`**

Find (lines 156–158):
```go
	if cfg.OnToolCall != nil {
		cfg.OnToolCall(toolName, argsMap)
	}
```
Replace with:
```go
	if cfg.OnToolCall != nil {
		cfg.OnToolCall(callID, toolName, argsMap)
	}
```

Find (lines 200–202):
```go
	if cfg.OnToolDone != nil {
		cfg.OnToolDone(toolName, toolResult)
	}
```
Replace with:
```go
	if cfg.OnToolDone != nil {
		cfg.OnToolDone(callID, toolName, toolResult)
	}
```

- [ ] **Step 4: Run build to verify loop.go itself is consistent**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go build ./internal/agent/... 2>&1 | grep "loop.go"
```

Expected: errors only from OTHER files (they use the old signature). No errors in `loop.go` itself.

---

## Task 3: Fix `agent_dispatcher.go` — `ChatWithAgent` inline closures (main bug site)

**Files:**
- Modify: `internal/agent/agent_dispatcher.go` (lines 716–781)

This is the primary bug site. Remove the name-keyed capture pattern and replace with callID-keyed.

- [ ] **Step 1: Replace the `toolArgsMu` + capture maps + closures block**

Find this entire block (lines 716–781):
```go
		// toolArgsMu guards toolArgsCapture against concurrent writes from parallel
		// tool dispatches. dispatchTools spawns one goroutine per tool call, so
		// OnToolCall/OnToolDone can fire concurrently.
		var toolArgsMu sync.Mutex
		// toolArgsCapture stores args keyed by tool name so OnToolDone can include
		// them in the tool_result event. Last-write-wins when the same tool is called
		// multiple times in one turn (a known limitation).
		// TODO(tool-call-id): key by call ID instead of tool name to fix same-tool collision.
		toolArgsCapture := make(map[string]map[string]any)
		// toolCallIDCapture stores a correlation ID per tool name so that
		// tool_call and tool_result events carry the same id. The frontend
		// uses this id to match results back to the pending call chip.
		toolCallIDCapture := make(map[string]string)
		loopCfg := RunLoopConfig{
			MaxTurns:      50,
			ModelName:     ag.GetModelID(),
			Messages:      msgs,
			Tools:         vr.sessionReg,
			ToolSchemas:   schemas,
			Gate:          agentGate,
			Backend:       agChatBackend,
			OnToken:       onToken,
			OnEvent:          onEvent,
			VaultWarnOnce:    &sync.Once{},
			VaultReconnector: vr.reconnector,
			OnToolCall: func(name string, args map[string]any) {
				callID := fmt.Sprintf("tc-%d-%s", time.Now().UnixNano(), name)
				slog.Info("tool call started", "agent", ag.Name, "tool", name, "session_id", sessionID, "call_id", callID)
				toolArgsMu.Lock()
				toolArgsCapture[name] = args
				toolCallIDCapture[name] = callID
				toolArgsMu.Unlock()
				if onToolEvent != nil {
					onToolEvent("tool_call", map[string]any{"tool": name, "args": args})
				} else if onEvent != nil {
					// Emit full tool_call event with id+args so the frontend can show
					// a "running…" chip with context before the result arrives.
					onEvent(backend.StreamEvent{
						Type:    backend.StreamToolCall,
						Payload: map[string]any{"id": callID, "tool": name, "args": args},
					})
				}
			},
			OnToolDone: func(name string, result tools.ToolResult) {
				toolArgsMu.Lock()
				capturedArgs := toolArgsCapture[name]
				callID := toolCallIDCapture[name]
				toolArgsMu.Unlock()
				slog.Info("tool call done", "agent", ag.Name, "tool", name, "session_id", sessionID, "call_id", callID, "success", result.Error == "")
				if onToolEvent != nil {
					onToolEvent("tool_result", map[string]any{"tool": name, "result": result.Output})
				} else if onEvent != nil {
					// Emit full tool_result event with matching id so the frontend
					// can attach the result to the correct pending chip.
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
		}
```

Replace with:
```go
		// toolArgsMu guards toolArgsCapture against concurrent writes from parallel
		// tool dispatches. dispatchTools spawns one goroutine per tool call, so
		// OnToolCall/OnToolDone can fire concurrently.
		var toolArgsMu sync.Mutex
		// toolArgsCapture stores args keyed by callID (the LLM-assigned tool call ID).
		// Entries are deleted in OnToolDone to prevent unbounded growth per turn.
		// Keying by callID (not tool name) fixes the same-tool-twice collision.
		toolArgsCapture := make(map[string]map[string]any)
		loopCfg := RunLoopConfig{
			MaxTurns:      50,
			ModelName:     ag.GetModelID(),
			Messages:      msgs,
			Tools:         vr.sessionReg,
			ToolSchemas:   schemas,
			Gate:          agentGate,
			Backend:       agChatBackend,
			OnToken:       onToken,
			OnEvent:          onEvent,
			VaultWarnOnce:    &sync.Once{},
			VaultReconnector: vr.reconnector,
			OnToolCall: func(callID string, name string, args map[string]any) {
				slog.Info("tool call started", "agent", ag.Name, "tool", name, "session_id", sessionID, "call_id", callID)
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
				slog.Info("tool call done", "agent", ag.Name, "tool", name, "session_id", sessionID, "call_id", callID, "success", result.Error == "")
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
		}
```

- [ ] **Step 2: Update `TaskWithAgent` prefetch callback** (lines ~460–469)

Find:
```go
	taskPrefetchCallback := func(toolName string, args map[string]any, output string, cached bool) {
		if cached {
			return
		}
		if onToolCall != nil {
			onToolCall(toolName, args)
		}
		if onToolDone != nil {
			onToolDone(toolName, tools.ToolResult{Output: output})
		}
	}
```

Replace with:
```go
	taskPrefetchCallback := func(toolName string, args map[string]any, output string, cached bool) {
		if cached {
			return
		}
		callID := fmt.Sprintf("prefetch-%s-%d", toolName, time.Now().UnixNano())
		if onToolCall != nil {
			onToolCall(callID, toolName, args)
		}
		if onToolDone != nil {
			onToolDone(callID, toolName, tools.ToolResult{Output: output})
		}
	}
```

---

## Task 4: Fix `chat_engine.go` — `ChatForSession` inline closures

**Files:**
- Modify: `internal/agent/chat_engine.go` (lines 205–214)

This is the second bug site — same pattern as ChatWithAgent but simpler (no capture map, just forwarding to `onToolEvent`).

- [ ] **Step 1: Update the inline closures**

Find (lines 205–214):
```go
		OnToolCall: func(name string, args map[string]any) {
			if onToolEvent != nil {
				onToolEvent("tool_call", map[string]any{"tool": name, "args": args})
			}
		},
		OnToolDone: func(name string, result tools.ToolResult) {
			if onToolEvent != nil {
				onToolEvent("tool_result", map[string]any{"tool": name, "result": result.Output})
			}
		},
```

Replace with:
```go
		OnToolCall: func(callID string, name string, args map[string]any) {
			if onToolEvent != nil {
				onToolEvent("tool_call", map[string]any{"tool": name, "args": args})
			}
		},
		OnToolDone: func(callID string, name string, result tools.ToolResult) {
			if onToolEvent != nil {
				onToolEvent("tool_result", map[string]any{"tool": name, "result": result.Output})
			}
		},
```

---

## Task 5: Update all pass-through function signatures

**Files:**
- Modify: `internal/agent/debug_loop.go` (lines 27–28, 57)
- Modify: `internal/agent/orchestrator.go` (wherever `onToolCall`/`onToolDone` params are declared)
- Modify: `internal/agent/mcp_agent_chat.go` (wherever `onToolCall`/`onToolDone` params are declared)
- Modify: `internal/agent/agent_dispatcher.go` (`Dispatch` and `TaskWithAgent` function signatures)

These files pass the callbacks through without creating inline closures. All changes are type signature updates only — no logic changes.

- [ ] **Step 1: Update `debug_loop.go`** (lines 27–28)

Find:
```go
	onToolCall func(string, map[string]any),
	onToolDone func(string, tools.ToolResult),
```

Replace with:
```go
	onToolCall func(string, string, map[string]any),
	onToolDone func(string, string, tools.ToolResult),
```

- [ ] **Step 2: Run `go build ./...` to find all remaining compile errors**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go build ./... 2>&1 | grep -v "_test.go"
```

Expected: compile errors pointing to the remaining files with old-style signatures in `orchestrator.go`, `mcp_agent_chat.go`, `agent_dispatcher.go`. Fix each error by changing `func(string, ...)` to `func(string, string, ...)` for the relevant parameters. The pattern is always the same:

```go
// OLD:
onToolCall func(string, map[string]any),
onToolDone func(string, tools.ToolResult),

// NEW:
onToolCall func(string, string, map[string]any),
onToolDone func(string, string, tools.ToolResult),
```

- [ ] **Step 3: Re-run `go build ./...` (non-test files) until clean**

```bash
go build ./... 2>&1 | grep -v "_test.go"
```

Expected: no output (no errors).

---

## Task 6: Update existing tests

**Files:**
- Modify: `internal/agent/loop_test.go`
- Modify: `internal/agent/observability_test.go`
- Modify: `internal/integration/agent_e2e_test.go`

- [ ] **Step 1: Update `loop_test.go` — `TestRunLoop_OnToolCallAndDoneCallbacks`** (lines 476–481)

Find:
```go
		OnToolCall: func(name string, args map[string]any) {
			calledNames = append(calledNames, name)
		},
		OnToolDone: func(name string, result tools.ToolResult) {
			doneCalled = true
		},
```

Replace with:
```go
		OnToolCall: func(callID string, name string, args map[string]any) {
			calledNames = append(calledNames, name)
		},
		OnToolDone: func(callID string, name string, result tools.ToolResult) {
			doneCalled = true
		},
```

- [ ] **Step 2: Update `observability_test.go`** — two tests (lines 116–117, 146–150)

Find first occurrence:
```go
		OnToolDone: func(name string, _ tools.ToolResult) {
			toolsDone = append(toolsDone, name)
		},
```
Replace with:
```go
		OnToolDone: func(_ string, name string, _ tools.ToolResult) {
			toolsDone = append(toolsDone, name)
		},
```

Find second occurrence:
```go
		OnToolDone: func(_ string, result tools.ToolResult) {
			if result.IsError {
				errorSeen = true
			}
		},
```
Replace with:
```go
		OnToolDone: func(_ string, _ string, result tools.ToolResult) {
			if result.IsError {
				errorSeen = true
			}
		},
```

- [ ] **Step 3: Update `agent_e2e_test.go`** (lines 168–174)

Find:
```go
	cfg.OnToolCall = func(name string, _ map[string]any) {
		toolCallName = name
	}
	cfg.OnToolDone = func(name string, result tools.ToolResult) {
		toolDoneName = name
		toolDoneResult = result
	}
```

Replace with:
```go
	cfg.OnToolCall = func(_ string, name string, _ map[string]any) {
		toolCallName = name
	}
	cfg.OnToolDone = func(_ string, name string, result tools.ToolResult) {
		toolDoneName = name
		toolDoneResult = result
	}
```

- [ ] **Step 4: Run `go build ./...` to find any remaining test compile errors**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go build ./... 2>&1
```

Expected: no output. If there are remaining compile errors, apply the same signature pattern (`func(_ string, name string, ...)`) to each one.

---

## Task 7: Run tests and verify

- [ ] **Step 1: Run the agent package tests with the race detector**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test -race ./internal/agent/... -v -run "TestRunLoop" 2>&1 | tail -40
```

Expected: all `TestRunLoop_*` tests pass. Specifically look for:
- `PASS: TestRunLoop_SameToolTwiceInOneTurn`
- `PASS: TestRunLoop_PanicPath_OnToolDoneStillFires`
- `PASS: TestRunLoop_OnToolCallAndDoneCallbacks`

- [ ] **Step 2: Run observability and integration tests**

```bash
go test -race ./internal/agent/... ./internal/integration/... -v 2>&1 | grep -E "^(PASS|FAIL|---)"
```

Expected: all PASS, no FAIL.

- [ ] **Step 3: Run full test suite**

```bash
go test ./... 2>&1 | grep -E "^(ok|FAIL)"
```

Expected: all `ok`, no `FAIL`.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/loop.go \
        internal/agent/agent_dispatcher.go \
        internal/agent/chat_engine.go \
        internal/agent/orchestrator.go \
        internal/agent/mcp_agent_chat.go \
        internal/agent/debug_loop.go \
        internal/agent/loop_test.go \
        internal/agent/observability_test.go \
        internal/integration/agent_e2e_test.go

git commit -m "fix(agent): key tool call tracking by callID to fix same-tool collision

OnToolCall/OnToolDone capture maps were keyed by tool name, causing
last-write-wins corruption when the same tool was called twice in one
turn. Move callID resolution to executeSingle (using tc.ID from the
LLM response, with positional fallback for empty IDs), thread it through
the callback signatures, and key capture maps by callID.

Also fires OnToolDone from the panic recovery path so callers can clean
up in-flight state on tool panic."
```

---

## Self-Review

**Spec coverage:**
- ✅ `tc.ID` with fallback — Task 2 Step 2
- ✅ `OnToolCall`/`OnToolDone` signatures changed — Task 2 Step 1
- ✅ Capture maps keyed by callID — Task 3 Step 1
- ✅ `toolCallIDCapture` eliminated — Task 3 Step 1 (replaced block omits it)
- ✅ `delete(toolArgsCapture, callID)` in `OnToolDone` — Task 3 Step 1
- ✅ Panic-path `OnToolDone` call — Task 2 Step 2
- ✅ `chat_engine.go` full fix (not just signature) — Task 4
- ✅ `debug_loop.go` signature — Task 5 Step 1
- ✅ `orchestrator.go`, `mcp_agent_chat.go`, `agent_dispatcher.go` pass-throughs — Task 5 Step 2
- ✅ `TaskWithAgent` prefetch synthetic callID — Task 3 Step 2
- ✅ `TestRunLoop_SameToolTwiceInOneTurn` — Task 1 Step 1
- ✅ `TestRunLoop_PanicPath_OnToolDoneStillFires` — Task 1 Step 2
- ✅ Existing test updates — Task 6
- ✅ Race detector — Task 7 Step 1

**No placeholders found.**

**Type consistency:** `callID string` is always first param in both callbacks throughout all tasks. ✅
