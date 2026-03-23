# Integration Layer

**Files**: `internal/mcp/`, `internal/relay/`, `internal/skills/`
**Related**: [swarm.md](swarm.md)

---

## Overview

Huginn connects to the outside world through three distinct integration mechanisms.
Each solves a different extensibility problem:

| Mechanism | Problem solved | Status |
|---|---|---|
| **MCP** (Model Context Protocol) | Consume tools from any MCP-compatible server | Active |
| **WebSocket Relay** | Route agent output and permission prompts to remote clients | Scaffolded |
| **Skills** | Let users add prompt fragments and executable tools without touching Go code | Active |

Use MCP when you need to expose a well-defined tool to the LLM and that tool already
speaks the MCP protocol. Use the relay when the agent is running headlessly on a
server and a remote UI needs to observe or control it. Use skills when you want to
extend the assistant's behavior with domain-specific instructions or lightweight
scripts — without writing Go.

---

## MCP (Model Context Protocol)

### What MCP Is and Why Huginn Implements It

MCP is an open standard for tool-calling between AI assistants and external servers.
An MCP server exposes a list of tools; a client discovers them at startup and
dispatches calls on demand. Because MCP is language-agnostic and already supported
by Claude, Cursor, and other tools, implementing it means Huginn can consume the
same tool servers without any custom adapter work.

### Client Architecture

```
  Agent goroutine
       │
       │  client.CallTool(ctx, "name", args)
       ▼
  ┌─────────────┐
  │  MCPClient  │  nextID: atomic.Int64 (request IDs)
  └──────┬──────┘
         │  JSON-RPC 2.0 over Transport
         ▼
  ┌──────────────────────┐
  │  Transport interface │
  │  Send(ctx, []byte)   │
  │  Receive(ctx) []byte │
  │  Close()             │
  └──────────┬───────────┘
             │  concrete implementation
             ▼
  ┌────────────────────────────────────────┐
  │  StdioTransport                        │
  │  cmd: exec.Cmd  (the server process)   │
  │  stdin → newline-delimited JSON writes │
  │  stdout → bufio.Reader line reads      │
  └────────────────────────────────────────┘
             │
             ▼
      MCP server process
      (any language, e.g. npx @modelcontextprotocol/server-filesystem)
```

The `Transport` interface exists as the only seam between the client and the wire.
Swapping in an HTTP transport or a mock for testing requires only a new type that
implements three methods.

### Request / Response Flow

1. `Initialize`: send capabilities, receive server capabilities, send
   `notifications/initialized`.
2. `ListTools`: retrieve the tool schema list; the manager registers each as a
   `MCPToolAdapter` in the global `tools.Registry`.
3. `CallTool`: serialize args as JSON-RPC params, send, receive, deserialize
   `MCPToolCallResult`.

All three methods share the same pattern: atomically increment `nextID`, marshal the
request, call `transport.Send`, call `transport.Receive`, unmarshal the response,
check `resp.Error`.

### Error Handling and EOF Wrapping

Raw `io.EOF` errors from a subprocess pipe are ambiguous — they could mean the
server process exited cleanly, crashed, or was killed by the OS. Huginn wraps them
at both the transport and client layers:

```go
// wrapReceiveErr converts io.EOF and io.ErrUnexpectedEOF to a descriptive
// "server disconnected" error so callers get an actionable message.
func wrapReceiveErr(err error) error {
    if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
        return fmt.Errorf("mcp: server disconnected: %w", err)
    }
    return err
}
```

The `%w` verb preserves the original error in the chain, so callers can still use
`errors.Is(err, io.EOF)` if they need to distinguish disconnect from other I/O
errors. The human-readable prefix `"mcp: server disconnected:"` surfaces in logs
and TUI error panels without requiring callers to decode a bare `"EOF"` string.

The `StdioTransport.Receive` method also wraps independently, so disconnects are
caught whether the error surfaces through the transport directly or through the
client's `wrapReceiveErr` call.

### Server Lifecycle and Restart

`ServerManager` manages N server configs. At `StartAll`, it connects to each server,
registers its tools, and spawns a `watchServer` goroutine that health-checks every
30 seconds via `ListTools`. On failure it restarts with exponential backoff (initial
1s, max 30s):

```go
func (m *ServerManager) watchServer(ctx context.Context, ms *managedServer, reg *tools.Registry) {
    backoff := m.initBackoff
    for {
        _, err := ms.client.ListTools(ctx)
        if err == nil {
            // healthy — wait 30s and re-check
            ...
            continue
        }
        // unhealthy — restart after backoff
        newClient, mcpTools, err := m.factory(ms.cfg)
        ...
        backoff = m.initBackoff  // reset on success
    }
}
```

---

## WebSocket Relay

### What It Enables

The relay is designed for scenarios where the Huginn agent runs on a remote machine
(a server, a CI runner, a Raspberry Pi) and a human needs to observe or approve
actions from another device — phone, tablet, or a second laptop. Rather than
requiring a full TUI session over SSH, the relay streams agent events and permission
prompts over WebSocket.

### Hub Interface

The relay's extensibility point is the `Hub` interface:

```go
// Hub routes messages to remote machines.
type Hub interface {
    Send(machineID string, msg Message) error
    Close(machineID string)
}
```

Two implementations exist:

| Type | Behavior |
|---|---|
| `InProcessHub` | No-op. `Send` always returns `nil`. Default when no relay is configured. |
| `WebSocketHub` | Returns `ErrNotActivated` until `huginn relay start` wires it up. |

The `Orchestrator` stores a `relay.Hub` field (nil defaults to `InProcessHub`
behavior via the nil check). This lets the orchestrator be tested without a real
WebSocket server — inject `InProcessHub` and all relay calls become no-ops.

### Message Envelope Format

All relay messages share one envelope:

```go
type Message struct {
    Type      MessageType    `json:"type"`
    MachineID string         `json:"machine_id,omitempty"`
    Payload   map[string]any `json:"payload,omitempty"`
}
```

Defined message types:

| Constant | Value | Purpose |
|---|---|---|
| `MsgToken` | `"token"` | A streaming LLM token |
| `MsgToolCall` | `"tool_call"` | Agent is about to invoke a tool |
| `MsgToolResult` | `"tool_result"` | Tool execution completed |
| `MsgPermissionReq` | `"permission_request"` | Agent needs human approval |
| `MsgPermissionResp` | `"permission_response"` | Human responded to permission prompt |
| `MsgDone` | `"done"` | Agent loop finished |

### Current Status

The relay is scaffolded. `InProcessHub` is the active implementation. `WebSocketHub`
compiles and satisfies the `Hub` interface but returns `ErrNotActivated` on every
`Send`. The command `huginn relay register` is where activation would be wired.
No mobile client has been built yet.

---

## Skills System

### The Problem Skills Solve

Core tools (read_file, bash, grep, etc.) are low-level primitives implemented in Go.
Adding a new tool means writing Go, recompiling, and shipping a new binary. That is
the right bar for primitives that run on every task.

But many useful extensions are domain-specific: a team might want a `summarize_pr`
tool, a `run_tests` shortcut, or a `code_review_checklist` injected into every
planning session. These should be user-owned, version-controlled alongside the
project, and require zero Go knowledge to create.

Skills are the answer: a directory on disk with a `skill.json` manifest and optional
Markdown files becomes a first-class extension point.

### Phase 1: Prompt-Only Skills

The simplest skill is a prompt fragment. Create a directory under `~/.huginn/skills/`:

```
~/.huginn/skills/go-style/
    skill.json    ← required: name, version
    prompt.md     ← optional: injected into agent system prompt
    rules.md      ← optional: injected as rule content
```

`skill.json` format:

```json
{
  "name": "go-style",
  "version": "1.0.0",
  "prompt_file": "prompt.md",
  "rules_file": "rules.md"
}
```

At startup the `Loader` scans `~/.huginn/skills/`, loads each subdirectory that
contains a valid `skill.json`, and builds a `SkillRegistry`. The registry's
`CombinedPromptFragment()` and `CombinedRuleContent()` methods concatenate all
loaded fragments with double-newline separators. The result is passed to
`Orchestrator.SetSkillsFragment` and injected into the context builder — it appears
in the system prompt for every agent call without any further wiring.

The loader also scans the workspace root for known rule file patterns:

```go
var knownRuleFiles = []string{
    ".cursorrules",
    ".cursor/rules",
    "CLAUDE.md",
    ".claude/CLAUDE.md",
    ".huginn/rules.md",
    ".github/copilot-instructions.md",
}
```

These are concatenated with headers and passed through the same skills fragment
path, so existing Cursor or Claude project rules work in Huginn automatically.

### Phase 2: Executable Skills

Phase 2 skills expose tools — callable functions the LLM can invoke during a task.
Tools are defined as Markdown files inside a `tools/` subdirectory of the skill:

```
~/.huginn/skills/my-skill/
    skill.json
    tools/
        run_tests.md
        summarize_pr.md
```

Each tool file is a Markdown document with YAML frontmatter:

```markdown
---
tool: run_tests
description: Run the project test suite and return the output
mode: shell
shell: make
args: ["test"]
timeout: 60
max_output_kb: 32
---
Run `make test` and return the combined output.
```

The `mode` field controls execution:

| Mode | Behavior |
|---|---|
| `template` (default) | `{{var}}` substitution in the body; returns rendered text as tool output |
| `shell` | Executes `shell` binary with `args`; captures stdout+stderr; caps at `max_output_kb` (default 64 KB) |
| `agent` | Stub — returns an informational message; real LLM invocation would require an orchestrator reference (circular dep) |

**Template mode example** — a tool that formats a PR summary prompt:

```markdown
---
tool: pr_context
description: Format a pull-request summary for the agent
schema:
  properties:
    pr_number: {type: string, description: "PR number"}
  required: [pr_number]
---
You are reviewing PR #{{pr_number}}. Focus on correctness and test coverage.
```

When the LLM calls `pr_context(pr_number="42")`, the `{{pr_number}}` placeholder
is replaced and the rendered text is returned as the tool result.

**Shell mode** runs a subprocess. The shell binary path and static args come from
frontmatter (`shell`, `args`). The LLM's call arguments are currently unused in
shell execution — the tool runs the same command every time. Output exceeding
`max_output_kb * 1024` bytes is truncated post-collection.

### Discovery Flow

```
startup
  │
  ▼
skills.DefaultLoader()          ← points to ~/.huginn/skills/
  │
  ├── Loader.LoadAll()
  │     │
  │     ├── os.ReadDir(skillsDir)
  │     ├── for each subdirectory:
  │     │     LoadFromDir(dir)
  │     │       ├── read skill.json  (required)
  │     │       ├── read prompt.md   (optional)
  │     │       └── read rules.md    (optional)
  │     └── []Skill
  │
  ├── SkillRegistry.Register(skill) × N
  │
  ├── FilesystemSkill.Tools()
  │     └── LoadToolsFromDir(skillDir)
  │           └── for each tools/*.md:
  │                 parseToolMD → PromptTool
  │
  └── tools.Registry.Register(promptTool) × M
```

Invalid skill directories are skipped with a log warning, not a fatal error. The
absence of `~/.huginn/skills/` is not an error — `LoadAll` returns an empty slice.

---

## Why This Design?

### Why MCP over a custom protocol?

MCP is already the de-facto standard for tool exchange in the LLM ecosystem. Claude
Desktop, Cursor, and Continue all implement MCP clients. An MCP server written for
any of those tools works with Huginn for free. A custom protocol would require
adapter shims for every external tool server, forever.

The cost of MCP is that the current transport is stdio-only. HTTP/SSE transport
(the other MCP transport type) is not yet implemented. This is acceptable for local
development but will matter for shared server deployments.

### Why a Hub interface for the relay instead of a concrete WebSocket type?

Testability. `InProcessHub` lets any test that exercises the orchestrator omit
WebSocket setup entirely — no ports, no goroutines, no cleanup. `WebSocketHub` is
the production implementation, activated only via explicit configuration. The
interface boundary also means a future gRPC or MQTT hub can be dropped in without
touching orchestrator code.

### Why are skills separate from tools?

Tools are low-level primitives defined in Go: they have typed arguments, permission
levels, and direct OS access. They are correct by construction (the compiler checks
them) and safe by policy (the permission gate).

Skills are composed behaviors defined in Markdown by users. They operate at a higher
abstraction level — prompt fragments that shape the agent's reasoning, or shell
scripts that run a well-known command. Users can create and share skills without
understanding Go or Huginn's internals. Core developers add tools; users add skills.

The separation also avoids a trust conflation: a skill's `shell` mode runs an
arbitrary binary, which requires the user to trust the skill author. A tool's
permission level is auditable in code. Mixing the two would make the permission
model much harder to reason about.

---

## Limitations

- **MCP is stdio-only.** The `defaultClientFactory` only handles `transport: "stdio"`.
  HTTP/SSE transport (used by remote MCP servers) is not implemented. All MCP
  servers must run as local subprocesses.
- **No MCP reconnect on restart with tool re-registration.** When `watchServer`
  reconnects a crashed server, it re-registers the new client's tools. If a tool
  schema changed between crashes, the old tool adapter remains registered with the
  stale schema.
- **Relay mobile client not implemented.** `WebSocketHub.Send` returns
  `ErrNotActivated`. There is no handshake, authentication, or client-side mobile
  application yet.
- **Skills shell mode ignores LLM arguments.** The shell command is defined entirely
  in frontmatter. The LLM can call the tool with any arguments, but they are not
  passed to the subprocess. This is intentional for the current phase but limits
  dynamic shell tools.
- **Agent mode in skills is a stub.** `mode: agent` returns an informational message
  rather than making a real LLM call. Wiring it requires an orchestrator reference,
  which would create a circular dependency between `skills` and `agent` packages.

---

## See Also

- [swarm.md](swarm.md) — how agents that use MCP tools are scheduled and run concurrently
- `internal/mcp/bridge.go` — `MCPToolAdapter` that wraps an `MCPTool` as a `tools.Tool`
- `internal/tools/registry.go` — the central tool registry that MCP and skills both register into
- `internal/agent/loop.go` — `isIndependentTool` classifies MCP tools as serial (state-dependent)
- `internal/agent/orchestrator.go` — `SetRelayHub` wires the relay hub into the orchestrator
