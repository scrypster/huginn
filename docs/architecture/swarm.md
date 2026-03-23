# Swarm Orchestrator

**File**: `internal/swarm/swarm.go`
**Related**: [integrations.md](integrations.md)

---

## The Problem

A naive sequential agent pipeline — run agent A, wait, run agent B, wait — leaves
most of the wall-clock time idle. When a user kicks off a multi-agent task (e.g.
plan + implement + review), the agents could be doing independent work in parallel.

But unbounded parallelism has its own costs: hammering a local Ollama instance with
ten simultaneous LLM streams degrades throughput for all of them. The swarm needs
a concurrency ceiling it can enforce dynamically, without requiring every caller to
know about that ceiling up front.

A second concern is observability. The TUI needs a live stream of what every agent
is doing — tokens arriving, tool calls starting, status changes. If the observability
channel is synchronous, a slow TUI render blocks the agents. If it is a callback, the
callback must be threadsafe and the agent goroutine owns the locking burden.

The swarm solves both problems: semaphore-based throttling for the concurrency budget,
and a buffered event channel for zero-coupling observability.

---

## Architecture Diagram

```
  Caller (TUI / headless)
        |
        | []SwarmTask
        v
  ┌─────────────┐
  │  Swarm.Run  │  registers all agents → agents map
  └──────┬──────┘
         │  one goroutine per task (all launched immediately)
         ▼
  ┌──────────────────────────────────────────┐
  │   goroutine pool (unbounded goroutines)  │
  │                                          │
  │  task N ──► acquire semaphore slot ──────┤
  │               (blocks if maxParallel     │
  │                slots are full)           │
  │                                          │
  │  acquired ──► runTask                    │
  │                 │                        │
  │                 ├─ emit(StatusThinking)  │
  │                 ├─ task.Run(ctx, emit)   │──► agent logic
  │                 ├─ emit(StatusDone /     │    (LLM calls, tool use)
  │                 │       StatusError /    │
  │                 │       StatusCancelled) │
  │                 └─ release semaphore     │
  └──────────────────────────────────────────┘
         │
         │  SwarmEvent (non-blocking send)
         ▼
  ┌────────────────────────┐
  │  eventsCh  chan [512]  │◄── all goroutines write here (emitMu guards close)
  └───────────┬────────────┘
              │
              ▼
       TUI consumer
       (reads Events())
```

---

## Key Types

```go
// AgentStatus represents the current state of an agent in the swarm.
type AgentStatus int

const (
    StatusQueued    AgentStatus = iota // registered but waiting for semaphore
    StatusThinking                     // semaphore acquired, LLM is running
    StatusTooling                      // agent is executing a tool call
    StatusDone                         // task completed successfully
    StatusError                        // task.Run returned a non-nil error
    StatusCancelled                    // context was cancelled before or during run
)

// SwarmTask is the unit of work submitted to the swarm.
type SwarmTask struct {
    ID    string
    Name  string
    Color string
    Run   func(ctx context.Context, emit func(SwarmEvent)) error
}

// SwarmAgent tracks live state for a running task.
type SwarmAgent struct {
    ID     string
    Name   string
    Color  string
    Status AgentStatus
    Cancel context.CancelFunc // call to cancel this agent individually
    Err    error              // non-nil after StatusError
    Output []string
    mu     sync.Mutex         // guards Status, Cancel, Err
}

// SwarmEvent is the observable unit emitted by every state transition.
type SwarmEvent struct {
    AgentID   string
    AgentName string
    Type      EventType
    Payload   any       // StatusChange → AgentStatus; Error → error; SwarmReady → []SwarmTaskSpec
    At        time.Time
}
```

---

## Task Lifecycle

```
StatusQueued
    │
    │  goroutine spawned immediately, blocks on semaphore
    │
    ├── ctx cancelled while waiting ──► StatusCancelled (EventStatusChange emitted)
    │
    ▼
[semaphore acquired]
StatusThinking  (EventStatusChange emitted)
    │
    │  task.Run(agentCtx, emit) executing
    │
    ├── task calls emit(EventToolStart / EventToken / ...) during run
    │
    ├── run returns error AND agentCtx.Err() != nil ──► StatusCancelled
    ├── run returns error (non-cancel) ──────────────► StatusError  (EventError emitted)
    └── run returns nil ──────────────────────────────► StatusDone  (EventComplete emitted)
                                                        [semaphore released]
```

The distinction between `StatusError` and `StatusCancelled` is intentional: callers
inspecting the agents map can tell whether a task failed because of a real error or
because a parent context was cancelled (e.g. the user hit Ctrl-C).

---

## Concurrency Model

### Semaphore-based throttling

`Swarm` uses a buffered channel as a counting semaphore:

```go
sem: make(chan struct{}, maxParallel)
```

Every goroutine tries to push a token into `sem` before running its task, and pops
the token when done. If `maxParallel` slots are occupied, the goroutine blocks. This
is standard Go semaphore idiom and requires no worker-pool bookkeeping.

All task goroutines are launched immediately from `Run`. The semaphore, not the
goroutine count, is the throttle.

### Per-task context

Each task gets its own `context.WithCancel` derived from the parent context passed
to `Run`. The cancel func is stored in `SwarmAgent.Cancel`, enabling individual
cancellation via `CancelAll` or a future `CancelOne`.

### Event emission

Every emit goes through `s.emit`, which holds `emitMu` for the duration:

```go
func (s *Swarm) emit(ev SwarmEvent) {
    s.emitMu.Lock()
    defer s.emitMu.Unlock()
    if s.closed {
        return
    }
    select {
    case s.eventsCh <- ev:
    default:
        s.droppedEvents.Add(1)
    }
}
```

The `emitMu` lock exists solely to prevent a send on a closed channel (which panics
in Go). Once `Run` calls `close(eventsCh)` at the end, subsequent `emit` calls see
`s.closed == true` and return early.

---

## Event Channel

| Property | Value |
|---|---|
| Buffer size | 512 events |
| Type | `chan SwarmEvent` (unbuffered writes blocked by default pattern) |
| Drop policy | Non-blocking send; increment `droppedEvents` counter on full |
| Consumer API | `swarm.Events() <-chan SwarmEvent` |
| Lifecycle | Closed by `closeOnce` after all tasks complete |

**Why 512?** A fast-running agent can emit dozens of token events per second. At
512 events the buffer absorbs a multi-second TUI render stall without dropping events
for typical workloads.

**What happens when it fills?** The `default` branch of the select increments
`s.droppedEvents` atomically. Callers can check `DroppedEvents()` to detect consumer
lag. The agent goroutine is never blocked by TUI slowness — it continues its LLM
calls regardless.

**Channel close race.** After all tasks finish, `Run` closes the channel exactly
once via `closeOnce`. Concurrent `emit` calls that happen to execute between the
wg.Wait() and the close are prevented from sending on a closed channel by the
`emitMu` / `s.closed` guard.

---

## Panic Recovery

Each task's `Run` function is wrapped in a deferred recover:

```go
err := func() (runErr error) {
    defer func() {
        if r := recover(); r != nil {
            runErr = fmt.Errorf("panic: %v", r)
        }
    }()
    return task.Run(agentCtx, emit)
}()
```

A panicking agent does not crash the process or leave the semaphore stuck. The
panic is converted to an error, the agent transitions to `StatusError`, and the
semaphore token is released by the outer `defer func() { <-s.sem }()`.

---

## Agent Personas (`agents` package)

The `agents` package (`internal/agents/`) defines named, persona-bearing agents
that plug into swarm tasks at a higher level. Each `Agent` carries:

- A **name** and **icon** (displayed in the TUI)
- A **system prompt** defining its personality and focus
- A **model slot** (`planner`, `coder`, `reasoner`) resolved to a model ID at startup
- A **local history** (capped at `MaxAgentHistory = 20` messages)

```go
// Default agents loaded from ~/.huginn/agents.json:
{
    Name: "Chris", Slot: "planner", Model: "qwen3-coder:30b",
    SystemPrompt: "You are Chris, a meticulous software architect...",
}
{
    Name: "Steve", Slot: "coder", Model: "qwen2.5-coder:14b",
    SystemPrompt: "You are Steve, a pragmatic senior engineer...",
}
{
    Name: "Mark", Slot: "reasoner", Model: "deepseek-r1:14b",
    SystemPrompt: "You are Mark, a deep thinker and meticulous reviewer...",
}
```

When the orchestrator runs a swarm, it wraps each agent's `CodeWithAgent` or
`ChatWithAgent` call inside a `SwarmTask.Run` closure. The persona becomes the
system prompt; the swarm handles scheduling and event routing.

Cross-session memory is injected via `BuildPersonaPromptWithMemory`, which prepends
up to 3 recent `SessionSummary` entries (date, summary, files touched, decisions,
open questions) into the system prompt before each run.

---

## Why This Design?

### Why a semaphore instead of a fixed worker pool?

A worker-pool model requires all tasks to exist at pool-creation time, or complex
job-queue machinery to inject tasks later. The swarm's `Run` function is deliberately
simple: every task goroutine launches immediately and self-throttles via the semaphore.
This makes it trivial to add tasks dynamically in future — a `Submit` method would
just spawn one more goroutine that races for a semaphore slot.

### Why a buffered event channel instead of callbacks?

Callbacks couple the producer (agent goroutine) to the consumer (TUI). If the
callback takes a lock the TUI owns, or if it blocks on I/O, the agent stalls. A
channel decouples them: the producer writes and moves on; the consumer processes at
its own pace. The channel also provides a natural fan-out point — multiple consumers
could `tee` the channel without changes to agent code.

### Why non-blocking emit with a drop counter?

The alternative — blocking emit — means a frozen TUI freezes all agents. That is
an unacceptable failure mode: the user sees a hung UI and can not interrupt. With
non-blocking emit, agents always make forward progress; the counter surfaces the
problem without causing it.

---

## Thread Safety

| Resource | Mutex | Safe operations |
|---|---|---|
| `s.agents` map | `s.mu` (RWMutex) | Register agents in `Run` (write lock); read status in `setStatus` (write lock) |
| `s.eventsCh` close | `s.emitMu` (Mutex) | Guards sends vs. the `closeOnce` close; prevents send-on-closed-channel panic |
| `s.closed` flag | `s.emitMu` (Mutex) | Read and write always under `emitMu` |
| `s.droppedEvents` | atomic.Int64 | Lock-free increment and read |
| `ag.Status` | `ag.mu` (Mutex per agent) | `setStatus` acquires `s.mu` (write) then `ag.mu` |
| `ag.Cancel` | `ag.mu` (Mutex per agent) | Set under `s.mu` write lock in `runTask`; called under `ag.mu` in `CancelAll` |

Note the lock ordering: `s.mu` (outer) → `ag.mu` (inner). Never acquire `ag.mu`
while holding only a read lock on `s.mu` if you also need to write `ag.Status`,
because `setStatus` always takes a write lock on `s.mu` first.

---

## Limitations

- **No task priorities.** All tasks are equal; there is no way to express "run this
  agent before that one" short of ordering the `tasks` slice and setting `maxParallel=1`.
- **No retry.** A task that returns `StatusError` stays in that state. The caller
  must re-submit a new `SwarmTask` to retry.
- **No work stealing.** If one agent finishes quickly and another is slow, the fast
  goroutine exits rather than helping with the slow task.
- **No partial results.** `Run` returns only after all tasks complete. There is no
  API to drain results incrementally while tasks are still running (use `Events()`
  for that).
- **Single run per Swarm.** After `Run` returns, `eventsCh` is closed. A second
  call to `Run` on the same `Swarm` would attempt to send on a closed channel and
  be dropped. Create a new `Swarm` for each batch of tasks.

---

## See Also

- [multi-agent.md](multi-agent.md) — **ThreadManager**: the web UI multi-agent system (separate from Swarm)
- [integrations.md](integrations.md) — MCP client that runs inside `SwarmTask.Run` closures
- `internal/agent/orchestrator.go` — `Orchestrator.CodeWithAgent` that wraps agent logic into tasks
- `internal/agents/agent.go` — `Agent` type and `AgentRegistry`
- `internal/tui/` — the event consumer that reads `swarm.Events()`
