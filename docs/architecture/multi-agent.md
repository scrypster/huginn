# Multi-Agent Thread System

**Package**: `internal/threadmgr`
**Related**: [swarm.md](swarm.md), [session-and-memory.md](session-and-memory.md), [backend.md](backend.md)

---

## Overview

The multi-agent system lets a primary agent (e.g. Mark) delegate work to specialized
sub-agents (e.g. Steve) in real time, within the same user session. Each delegation
creates a **thread** — an independent LLM loop with its own tool access, history,
and lifecycle. Threads run concurrently; the main chat stays responsive throughout.

This system is distinct from the TUI-era `internal/swarm` package. Swarm is a
batch runner for headless tasks; ThreadManager is the live, session-aware concurrency
layer that drives the web UI.

---

## Architecture Diagram

```
User (browser)
     │  chat message
     ▼
Primary Agent (Mark) ─── LLM call ─── delegate_to_agent(steve, task)
     │                                        │
     │                                        ▼
     │                               ThreadManager.Create()
     │                               ThreadManager.SpawnThread()
     │                                        │
     │                              ┌─────────────────────┐
     │                              │  Thread goroutine   │
     │                              │  (runOnce loop)     │
     │                              │  • LLM calls        │
     │                              │  • tool execution   │
     │                              │  • status updates   │
     │                              └──────────┬──────────┘
     │                                         │
     │       WS broadcast per event            │
     │  ◄──────────────────────────────────────┤
     │  thread_started, thread_status,         │
     │  thread_token, thread_tool_call,        │
     │  thread_tool_done, thread_done          │
     │                                         │
     │  ◄──────────── notify_start/token/done ─┘
     │         (CompletionNotifier)
     ▼
  Browser (ThreadPanel + main chat)
```

---

## Key Types

### Thread

```go
type Thread struct {
    ID        string
    SessionID string
    AgentID   string
    Task      string
    Status    ThreadStatus    // queued → thinking → tooling → done/error/cancelled
    StartedAt time.Time
    // ...
    InputCh   chan string      // receives injected input when blocked for help
}
```

### ThreadManager

```go
type ThreadManager struct {
    threads   map[string]*Thread  // threadID → Thread (guarded by mu RWMutex)
    fileLocks map[string]string   // filePath → owning threadID (for file-lease system)

    MaxThreadsPerSession int       // default: 20 active threads per session

    helpResolver        HelpResolver        // nil = human input fallback
    completionNotifier  *CompletionNotifier // nil = no completion messages
}
```

---

## Thread Lifecycle

```
queued
  │  SpawnThread called
  ▼
thinking   ◄────────────────────────────────────────────────────────┐
  │                                                                  │
  ├── LLM calls tools ──► tooling ──► (tool result) ──► thinking    │
  │                                                                  │
  ├── LLM calls request_help() ──► blocked ──────────────────────────┤
  │       │                          │ (human input or AutoHelpResolver)
  │       │                          └── resolving ──► thinking (if AutoHelpResolver)
  │       │                          └── blocked    (if human input required)
  │
  ├── LLM calls finish() ──► done (ErrFinish)
  ├── LLM stops with no tools ──► done (implicit finish)
  ├── context length exceeded ──► done (completed-with-timeout)
  ├── max turns reached ──► done (completed-with-timeout)
  ├── cancelled ──► cancelled
  └── LLM error ──► error
```

Terminal statuses: `done`, `completed`, `completed-with-timeout`, `cancelled`, `error`

---

## Concurrency Model

### Per-thread goroutine

`SpawnThread` launches a goroutine per thread that runs `runOnce` in a loop (for
re-entry after blocked help resolution). The goroutine holds no global locks during
LLM calls — it only acquires `tm.mu` briefly to snapshot resolver references at
startup and to update thread state on transitions.

### File lease system

When a sub-agent writes a file, it acquires a file lease:

```go
tm.leaseMu ──► fileLocks map[filePath → threadID]
```

Prevents two concurrent threads from writing the same file. Leases are released
automatically when the thread finishes.

### Thread limit

`Create()` enforces `MaxThreadsPerSession = 20` active (non-terminal) threads
per session, returning `ErrThreadLimitExceeded` if exceeded. This caps goroutine
and memory usage when a primary agent runs amok.

### RWMutex usage

| Operation | Lock |
|-----------|------|
| Create / status update / complete | `tm.mu` write |
| Get / list threads | `tm.mu` read |
| Resolver snapshot in SpawnThread | `tm.mu` read |
| File lease acquire/release | `tm.leaseMu` write |

---

## WebSocket Events

All events are scoped to a `session_id` and routed only to clients subscribed to
that session.

| Event | Payload | When |
|-------|---------|------|
| `thread_started` | `thread_id`, `agent_id`, `task` | Thread created and running |
| `thread_status` | `thread_id`, `status` | Status transition |
| `thread_token` | `thread_id`, `token` | LLM streaming token |
| `thread_tool_call` | `thread_id`, `tool`, `args` | Tool call begins |
| `thread_tool_done` | `thread_id`, `tool`, `result_summary` | Tool call complete |
| `thread_done` | `thread_id`, `status`, `summary`, `elapsed_ms` | Thread finished |
| `thread_help` | `thread_id`, `message` | Sub-agent needs human input (fallback) |
| `thread_help_resolving` | `thread_id` | AutoHelpResolver started LLM call |
| `thread_help_resolved` | `thread_id` | AutoHelpResolver answered successfully |
| `thread_inject` (inbound) | `thread_id`, `content` | Human input injected |
| `thread_cancel` (inbound) | `thread_id` | Cancel request |
| `notify_start` | `thread_id`, `agent_id` | CompletionNotifier streaming begins |
| `notify_token` | `content` | CompletionNotifier streaming token |
| `notify_done` | `thread_id` | CompletionNotifier streaming complete |

---

## Auto-Help Resolver

When a sub-agent calls `request_help(message)`, the thread raises `ErrHelp` and
blocks. The `HelpResolver` interface handles resolution:

```go
type HelpResolver interface {
    Resolve(ctx, sessionID, threadID, agentID, helpMessage string) (string, error)
}
```

**`AutoHelpResolver`** is the production implementation:

1. Looks up the primary agent for the session (`PrimaryAgent(sessionID)`)
2. Broadcasts `thread_help_resolving` (UI shows "consulting Mark...")
3. Builds a focused prompt: primary agent's system prompt + up to 3,000 tokens
   of session context + the sub-agent's question
4. Makes a single-turn LLM call (no tools, no history loop)
5. On success: broadcasts `thread_help_resolved`, feeds answer to `thread.InputCh`
6. On error: falls back — broadcasts `thread_help` so the user can answer manually

```
Sub-agent raises ErrHelp
        │
        ▼
AutoHelpResolver.Resolve()
        │
        ├── PrimaryAgent(sessionID) == nil ──► error → human fallback
        │
        ├── LLM call succeeds ──► send answer to thread.InputCh
        │                         thread resumes at thinking
        │
        └── LLM call fails ──► broadcast thread_help
                                thread stays blocked, UI shows input form
```

The resolver runs in a goroutine while the outer loop blocks on `waitForInputOnce`
(select on `ctx.Done()` and `thread.InputCh`). No deadlock is possible because
the goroutine either sends to `InputCh` or the context is cancelled.

---

## Completion Notifier

After a sub-agent thread finishes, `CompletionNotifier.Notify` posts a natural-language
summary to the main session chat on behalf of the primary agent:

1. Looks up primary agent; returns silently if nil
2. Builds prompt: primary agent system prompt + up to 2,000 tokens of session context
   + `"Sub-agent Steve has completed. Their summary: <...>. Briefly let the user know."`
3. Broadcasts `notify_start` (frontend creates a streaming assistant message)
4. Streams LLM response as `notify_token` events (live streaming in main chat)
5. Persists the completed answer to session history via `Store.Append`
6. Broadcasts `notify_done` (frontend finalizes the message)
7. On LLM error: logs, broadcasts `notify_done` with empty content (silent degradation)

`Notify` is always called with `context.Background()` because the thread context is
cancelled by the time the goroutine runs. The message appears in the main chat as a
normal assistant turn and survives page reloads.

```
Thread finishes (ErrFinish, stop, length, maxTurns)
        │
        └── go completionNotifier.Notify(context.Background(), ...)
                │
                ├── no primary agent ──► return (silent)
                ├── LLM error ──► notify_done, return (silent)
                └── success ──► notify_start → stream notify_token → persist → notify_done
```

---

## Frontend Integration

The frontend (`web/src/`) has two layers:

**`useThreads.ts`** — composable that maps WS events to `LiveThread` reactive state:
- `thread_started` → creates entry, starts elapsed timer
- `thread_status` → updates `Status`
- `thread_token` → appends to `streamingContent` (capped at 600 chars)
- `thread_tool_call/done` → maintains `toolCalls[]` list
- `thread_done` → terminal state, stops timer
- `thread_help` → status `'blocked'` (shows input form in ThreadCard)
- `thread_help_resolving` → status `'resolving'` ("consulting Mark...")
- `thread_help_resolved` → status `'thinking'`

**`ChatView.vue`** — main chat handles completion notifications:
- `notify_start` → pushes streaming assistant message, sets `notifyStreaming = true`
- `notify_token` → appends `payload.content` to last streaming message
- `notify_done` → clears `streaming` flag, sets `notifyStreaming = false`

`notifyStreaming` is kept separate from `streaming` so the user can type while
a completion notification is rendering.

---

## Delegation Approval Gate

`DelegationPreviewGate` is an optional middleware that intercepts thread spawning
and requires explicit frontend approval before the thread starts. When enabled:

1. Primary agent calls `delegate_to_agent(...)`
2. ThreadManager broadcasts a `delegation_preview` event with the proposed task
3. Frontend shows an approval prompt
4. On `delegation_ack` (approve/deny), the gate unblocks
5. Thread spawns (approved) or call returns with error (denied)

When disabled (default), threads spawn immediately without approval.

---

## REST API

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/v1/sessions/{id}/threads` | List threads for a session |

Thread state is primarily delivered over WebSocket. The REST endpoint seeds the
frontend state on initial load or reconnect.

---

## Wiring (main.go)

```go
// Thread manager is created and wired after agent config is loaded:
tm := threadmgr.New()
srv.SetThreadManager(tm)

// Auto-help: primary agent answers sub-agent questions autonomously
helpResolver := &threadmgr.AutoHelpResolver{
    Backend:      b,
    Store:        sessStore,
    Broadcast:    srv.BroadcastToSession,
    PrimaryAgent: srv.ResolveAgent,
}
tm.SetHelpResolver(helpResolver)

// Completion notify: primary agent posts a summary when sub-agent finishes
completionNotifier := &threadmgr.CompletionNotifier{
    Backend:      b,
    Store:        sessStore,
    Broadcast:    srv.BroadcastToSession,
    PrimaryAgent: srv.ResolveAgent,
}
tm.SetCompletionNotifier(completionNotifier)
```

Both can be disabled by passing `nil` (or by not calling the setter). When the
help resolver is nil, `thread_help` is broadcast and the user must respond via
the ThreadCard input form. When the completion notifier is nil, no summary is
posted after sub-agent threads finish.

---

## See Also

- [swarm.md](swarm.md) — the TUI-era batch orchestrator (separate system)
- [backend.md](backend.md) — LLM backend abstraction used by resolvers
- [session-and-memory.md](session-and-memory.md) — session store used for context snapshots
- `internal/threadmgr/spawn.go` — `runOnce` loop: the core execution engine
- `internal/threadmgr/context.go` — `buildSnapshotMessages` for session context injection
- `internal/server/handlers.go` — `handleListThreads`, `handleThreadCancel`, `handleThreadInject`
- `web/src/components/ThreadPanel/` — thread panel UI components
