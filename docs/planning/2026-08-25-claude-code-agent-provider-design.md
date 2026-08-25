# Claude Code as a Huginn Agent Provider — Design

**Date:** 2026-08-25
**Status:** Approved, not implemented
**Depends on:** the Claude Code bridge (`feat/claude-code-bridge`) — watcher, mapper, tailer,
ingester, session linkage and `internal/claudecode/delegate.go` are all reused.
**Companion project (separate spec, not this one):** a Huginn toolbelt MCP server.

## Goal

Let a Huginn agent BE a Claude Code session. You create an agent, chat with it in Huginn, and
Huginn drives a Claude Code session underneath — while the agent's Huginn configuration (system
prompt, skills, notepad, tool restrictions, approval gates) shapes every turn.

The bridge that already exists is an observer: it mirrors Claude Code sessions into Huginn
read-only, and offers a `claude_code` tool for one-shot delegation. This is different — Claude
Code becomes an execution backend for a first-class Huginn agent.

## Scope decision: this is one of two subsystems

Full parity would also give a Claude Code agent Huginn's toolbelt connections (GitHub, Slack,
AWS, Jira). Those are Huginn-native OAuth integrations executed in Huginn's process; Claude Code
runs in its own process and cannot reach them without an MCP server fronting them.

That MCP server is a substantial subsystem with its own auth model, and it is independently
useful — any MCP client could consume Huginn's connections, not just this feature. It gets its
own spec. **This design defines the seam and leaves the slot empty:** sessions launch with
`--mcp-config`, which is empty in v1.

Without it, a Claude Code agent still gets its system prompt, skills, notepad, tool restrictions,
per-call approval, and any MCP servers Claude Code is already configured with — including
MuninnDB, which is configured on this machine today.

## Verified facts this design rests on

Each was established empirically against the installed CLI, not assumed:

- `claude --resume <session-id> -p "…"` **carries context across separate processes.** Probe:
  turn 1 stored the number 4271 in one process; turn 2, a separate `--resume` invocation,
  answered "4271". This is the mechanism that makes an agent a durable session.
- `--permission-mode manual` **does NOT produce an interactive permission request** on the
  stream-json channel in `-p` mode. A probe asked it to write a file and it wrote the file. There
  is no `control_request` line type. Per-call approval is therefore NOT reachable through the
  stream protocol.
- **A `PreToolUse` hook CAN block a tool call**, and is the mechanism that makes approval work.
  A probe hook returning `{"hookSpecificOutput":{"permissionDecision":"deny"}}` prevented the
  write entirely, and Claude Code reported it to the user as *"a hook ('Huginn') denied the Write
  call. Nothing was written."* The denial also appears in the result's `permission_denials`.
- The hook receives `tool_name`, `tool_input`, `tool_use_id`, `session_id`, `cwd`,
  `transcript_path`, `permission_mode`, `prompt_id` — everything needed to correlate and decide.
- **`--settings` accepts inline JSON**, so hooks are per-session. Huginn never writes to the
  user's `~/.claude/settings.json`.
- Agents already resolve their backend per-agent: `main.go:3126` calls
  `serveCache.For(ag.Provider, ag.Endpoint, ag.APIKey, ag.GetModelID())`.

## Approach, and what was rejected

**Chosen: implement `backend.Backend`.** `BackendCache.For` returns a `ClaudeCodeBackend` when
the agent's provider is `claude-code`. Everything downstream — chat, spaces, routines, workflows,
the dispatcher — keeps working unchanged, because they all route through that provider-keyed
cache. One seam for a large capability.

**Rejected: a distinct agent kind with its own chat path.** More honest about the semantics, but
there are 64 backend construction sites and six `ChatRequest` builders across `chat_engine.go`
and `agent_dispatcher.go`. Every one is a chance to miss a path, and a missed path means an agent
that works in chat but silently fails in a routine.

**Rejected: replay Huginn's full history each turn with no `--resume`.** Keeps Huginn
authoritative, but re-sends the whole conversation every message, discards Claude Code's session
continuity and its compaction, and reduces it to a dumb model endpoint. It contradicts the core
idea that the agent *is* a session.

## Architecture

```
Huginn chat  ──►  agent (Provider: "claude-code")
                    │
                    ├─ BackendCache.For("claude-code", …)
                    │        └─► ClaudeCodeBackend.ChatCompletion
                    │                 │
                    │                 ├─ claude --resume <session> -p <newest message>
                    │                 │     --append-system-prompt <agent prompt+skills+notepad>
                    │                 │     --settings <inline JSON: PreToolUse hooks>
                    │                 │     --allowedTools <pre-authorised set>
                    │                 │     --mcp-config <empty in v1 — seam for subsystem B>
                    │                 │
                    │                 └─ stream-json ──► OnEvent (live) ──► Content + ExecutedTools
                    │
                    └─ PreToolUse hook ──► `huginn claude-approve`
                                              └─► POST /api/v1/claude/approve ──► Huginn approval UI
```

## Agent model

`Agent` already carries `Provider`, so no new concept is needed. Two new persisted fields:

| Field | Meaning |
|---|---|
| `ClaudeSessionID` | The Claude Code session this agent is bound to. One agent, one session. |
| `ClaudeCWD` | The session's working directory, fixed when the agent is created. |

Everything else is reused as-is: `SystemPrompt`, `Skills`, `LocalTools`, `MemoryEnabled`,
`VaultName`, `Toolbelt` (recorded but inert until subsystem B exists).

## Session lifecycle

- **Creation** — Huginn generates a session UUID and records it on the agent. The first turn runs
  with `--session-id <uuid>`; every later turn uses `--resume <uuid>`.
- **Adoption** — instead of generating, the user picks an existing session the bridge has already
  discovered. Only sessions that are not currently open in a terminal may be adopted; see
  "Two writers" below.
- **Context growth** — Claude Code's own auto-compaction handles the context window. Huginn must
  DISABLE its own compaction for these agents: compacting a history that is not the model's
  context would silently rewrite the record while changing nothing the model sees. Its history is
  a record, not a context buffer.
- **Session lost** — if `--resume` fails because the transcript is gone, Huginn surfaces the
  error and offers to bind a fresh session rather than silently starting one.

## The backend

`ClaudeCodeBackend` implements the four-method `Backend` interface.

`ChatCompletion` takes **only the newest message** from `req.Messages`, because Claude Code owns
the conversation. It reuses `BuildArgs` and `applyStreamLine` from
`internal/claudecode/delegate.go` — code already validated against the live CLI's stream.

Two interface leaks, documented rather than hidden:

- `req.Messages` is ignored past the newest turn.
- `req.Tools` is ignored; Claude Code has its own tools.

Streaming maps stream-json `assistant` events to `req.OnEvent` so the UI updates live. `system`
and `rate_limit_event` lines are dropped by the existing default switch arm, which is what keeps
new CLI line types from breaking a turn.

## Persisting tool calls: a two-line change to `ChatResponse`

`internal/agent/loop.go` uses one field for two jobs: line 441 persists
`chatResult.ToolCalls` into history, and lines 468/475 decide whether to dispatch them. Returning
Claude Code's already-executed calls through that field would make Huginn run them a second time.

Separate the jobs by adding one field:

```go
// ExecutedTool is a tool call the BACKEND already ran. Claude Code executes its
// own tools, so these must be persisted into history but never dispatched.
type ExecutedTool struct {
	Call   ToolCall
	Result string
}

// ChatResponse gains:
ExecutedTools []ExecutedTool
```

`loop.go` then builds the assistant message with `ExecutedTools`' calls when `ToolCalls` is empty,
and appends the matching `role:"tool"` messages carrying results — mirroring exactly what
`dispatchTools` already does at line 477, minus the dispatching. `ToolCalls` stays empty, so the
loop still terminates at line 468 and nothing is executed.

The effect: Claude Code's tool activity is indistinguishable from native tool activity in
Huginn's history — same message shapes, same rendering, results included.

## Prompt assembly

`--append-system-prompt` carries the agent's `SystemPrompt`, its skills text, and the notepad,
**reassembled every turn** so edits to rules take effect on the next message rather than only at
session creation. It appends to Claude Code's own system prompt rather than replacing it.

## Approval

Huginn passes an inline `--settings` JSON containing one `PreToolUse` hook entry **per gated
tool**, each invoking a new `huginn claude-approve` subcommand. That command POSTs the hook's
payload to `/api/v1/claude/approve` on the local server and returns the decision as JSON.

Huginn correlates `session_id` to the agent, applies that agent's `LocalTools` and toolbelt
`approval_gate`, and either auto-allows or surfaces the request in the existing approval UI.

**One hook entry per gated tool, not a combined matcher.** The probe only established that
single-tool matchers work; per-tool entries need no assumption about regex support.

**Scoping is what makes fail-closed safe.** Tools the agent is pre-authorised for are passed via
`--allowedTools` and are NOT in any hook matcher, so they never call Huginn. A Huginn outage
therefore blocks only the tools that always required a human, and the agent degrades to its
pre-authorised capability instead of stopping dead.

**Fail closed.** Hook timeout or unreachable Huginn returns `deny`, with the reason
`"Huginn unreachable — <Tool> requires approval"`. A bridge that is down must never become a
bridge that approves everything, and Claude Code surfaces the reason to the user verbatim.

No approval cache in v1. Caching a security decision turns a boundary into a race.

## Two writers, and the session-mirroring wrinkle

A Claude Code session must have exactly one writer at a time.

- The agent's Huginn session is created with `external_kind = "claude-code"` and
  `external_id = <session uuid>`, reusing the bridge's existing linkage.
- The ingester recognises agent-owned transcripts and **skips them**: the chat path is already
  persisting those turns, and double-writing would duplicate every message.
- **Takeover** — an "Open in terminal" action hands the user `claude --resume <session-id>` and
  marks the agent paused. While paused Huginn refuses to send. This is the only supported way to
  work in the same session from a terminal.
- **Adoption of a live session is refused.** If a candidate transcript has been modified in the
  last 5 minutes, Huginn warns rather than binding it, because a terminal may still own it. This
  is a heuristic, not a guarantee — there is no way to ask Claude Code whether a session is open.

## Failure modes

| Condition | Behaviour |
|---|---|
| `claude` binary missing | Agent unusable; clear error naming the configured binary path. |
| Session no longer resumable | Error surfaced; offer to bind a fresh session. Never silently start one. |
| Hook cannot reach Huginn | Deny, with a reason naming the tool. Pre-authorised tools are unaffected. |
| Hook times out | Deny after 60s (configurable). Implementation MUST first verify Claude Code's own PreToolUse hook timeout and set this below it — if Claude Code gives up first, the decision is lost rather than denied. |
| Concurrent turns to one agent | Serialised per agent; a second turn waits rather than racing the transcript. |
| Claude Code exits mid-turn | Partial content returned with an error; the transcript remains the record. |
| Huginn restarts mid-session | The session is unaffected — it lives in the transcript, not in Huginn's memory. |

## Testing

- `internal/claudecode/testdata/fake-claude.sh` already exists and is reused for backend tests.
- Approval is tested by invoking `huginn claude-approve` against a test server, asserting
  allow, deny, timeout-denies, and unreachable-denies.
- `ExecutedTools` is tested in `loop.go`: a response carrying them must persist the calls and
  their results into history and must NOT dispatch anything.
- Prompt assembly is tested by asserting the `--append-system-prompt` argument contains the
  agent's prompt, its skills and its notepad.
- **No test invokes the real `claude` CLI, touches the network, or spends money.**

## Out of scope

- The toolbelt MCP server (its own spec).
- Attaching to a session currently open in a terminal — no local control surface exists, and it
  would mean two writers. Takeover is the supported path.
- Huginn-side compaction for these agents; Claude Code compacts its own context.
- Multiple concurrent conversations with one agent — one agent is one session by design.
