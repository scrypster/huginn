# Agent Toolbelt

**Files**: `internal/agents/`, `internal/tools/registry.go`
**Related**: [connections.md](connections.md), [permissions-and-safety.md](permissions-and-safety.md)

---

## Overview

The agent toolbelt is a per-agent list of allowed connections. It answers the
question: which external providers is this agent permitted to use?

Without the toolbelt, every agent that runs on a machine can see and call every
connected provider — AWS, GitHub, Google, Slack, and all the rest. That is
fine for a personal assistant with a single user, but it becomes a liability
the moment you have specialized agents with different trust levels. A coding
assistant should never know your AWS production credentials exist. A DevOps
agent should not be able to open Jira tickets on behalf of a product agent
that happens to be in the same session.

The toolbelt enforces production isolation and the principle of least privilege
at the agent level. Each agent declares exactly which connections it may use.
Enforcement happens at three independent layers so that no single point of
failure can grant unauthorized access.

---

## Data Model

```go
// internal/agents/types.go

type ToolbeltEntry struct {
    ConnectionID string `json:"connection_id"`
    Provider     string `json:"provider"`
    Profile      string `json:"profile,omitempty"`
    ApprovalGate bool   `json:"approval_gate,omitempty"`
}
```

`ConnectionID` — the identifier of a specific configured connection (e.g.,
`"aws-prod"`, `"github-myorg"`). Matches a key in the user's connection store.

`Provider` — the provider type (e.g., `"aws"`, `"github"`, `"google"`). Used
for schema filtering and provider tagging (see Enforcement Layers below).

`Profile` — optional. For providers that support multiple named profiles (e.g.,
AWS named profiles), this pins the entry to a specific profile. Empty means
the default profile for that connection.

`ApprovalGate` — when `true`, write and exec operations for this provider
always prompt the user even when `--dangerously-skip-permissions` is active.
See Approval Gate below.

The toolbelt lives as a slice on each agent definition:

```go
type AgentDef struct {
    // ...
    Toolbelt []ToolbeltEntry `json:"toolbelt,omitempty"`
}
```

---

## Three Enforcement Layers

The toolbelt enforces access control at three independent points. All three
must pass for a tool call to succeed.

### Layer 1 — Schema Filtering

At session start, when the tool registry assembles the list of tool descriptions
to send to the LLM, it filters by the providers declared in the agent's toolbelt:

```go
reg.AllSchemasForProviders(agents.ToolbeltProviders(ag.Toolbelt))
```

`ToolbeltProviders` extracts the set of provider strings from the toolbelt
entries. `AllSchemasForProviders` returns only the tool schemas whose registered
provider is in that set.

The result: **the model never receives descriptions for tools it is not
permitted to use.** It cannot ask to call `aws_s3_list_buckets` if the AWS
provider is not in the toolbelt, because that tool's schema is never included
in the system context. The model has no knowledge that the tool exists.

This layer prevents the model from even attempting unauthorized tool calls. It
is the first and most efficient gate.

### Layer 2 — Provider Tagging

Every tool registered in the tool registry carries a provider tag set at
registration time. When a tool call arrives from the model, the registry looks
up the provider for the requested tool name:

```go
ProviderFor(toolName) → provider string
```

The executor then checks whether that provider is present in the agent's
toolbelt. If the provider is not in the toolbelt, the call is denied
immediately with a permission error — the tool implementation is never invoked.

This layer is a defense-in-depth backstop. Even if schema filtering were
somehow bypassed (e.g., a prompt injection that references a tool name the
model learned externally), provider tagging catches the unauthorized call at
execution time.

### Layer 3 — Approval Gate

For each toolbelt entry, `ApprovalGate` controls whether write and exec
operations require explicit user approval, regardless of the session's
permission mode.

When `approval_gate: true`, the gate for that provider's tools is always set
to prompt — even when the session was started with `--dangerously-skip-permissions`.
Read operations (`PermRead`) are never gated and are always allowed freely.

See the Approval Gate section below for full behavioral details.

---

## Enforcement Flow

```
LLM emits tool call: { "tool": "aws_s3_put_object", args: {...} }
          |
          v
  ToolbeltProviders(ag.Toolbelt)
  → {"aws", "github"}          <-- set of allowed providers
          |
          v
  ProviderFor("aws_s3_put_object")
  → "aws"
          |
          v
  "aws" in allowed providers?
          |
         NO → deny immediately, return permission error to LLM
          |
         YES
          |
          v
  tool.PermissionLevel == PermRead?
          |
         YES → execute immediately (no gate check)
          |
          NO (PermWrite or PermExec)
          |
          v
  toolbelt entry for "aws": approval_gate?
          |
         NO → delegate to standard Gate.Check()
              (respects --dangerously-skip-permissions and session allow-list)
          |
         YES → force prompt regardless of Gate.skipAll
              (--dangerously-skip-permissions has no effect here)
          |
          v
  User responds: Allow / AllowOnce / AllowAll / Deny
          |
          v
  Execute tool or return denial
```

---

## Approval Gate

The approval gate is a per-provider override of the standard permission system.
It exists for a specific production safety scenario: you want an agent to
operate autonomously in most contexts, but you want a human checkpoint before
it makes changes to a critical system.

### Behavior when `approval_gate: false`

The agent uses the connection freely. Read operations never prompt. Write and
exec operations follow the standard `Gate` logic — they can be allowed by the
session allow-list or by `--dangerously-skip-permissions`.

This is appropriate for connections where the risk of writes is acceptable in
headless or autonomous operation: a staging GitHub org, a read-heavy data
provider, or a sandbox AWS account.

### Behavior when `approval_gate: true`

Read operations (`PermRead`) are always allowed without prompting.

Write and exec operations (`PermWrite`, `PermExec`) always prompt the user,
with no exceptions:

- `--dangerously-skip-permissions` does **not** bypass this gate.
- A session-level `AllowAll` decision for the tool name does **not** bypass
  this gate.
- Headless mode with `promptFunc == nil` causes the call to be denied (same
  behavior as the standard gate when no prompt is available).

The approval gate is appropriate for any connection to a production system:
AWS production accounts, the GitHub organization that owns production
repositories, the Slack workspace used for incident channels.

### Why `--dangerously-skip-permissions` doesn't bypass it

`--dangerously-skip-permissions` is a process-level flag that the user or CI
operator sets when starting Huginn. The approval gate is an agent-level policy
set by the person who configured the agent. An operator running a headless
pipeline cannot know in advance which connections a particular agent declared
as approval-gated. Letting the process flag override the agent policy would
silently defeat the protection at exactly the moment it is most needed — in
automation.

---

## Empty Toolbelt

When `AgentDef.Toolbelt` is nil or empty, **no external providers are
granted**. Schema filtering sends no connection tools to the model, and
`AllowedProviders` returns an empty map so the permission gate fails closed
even if the model hallucinates a tool name that exists in the global registry
(for example `aws_ec2_terminate_instance`).

Built-in tools from `local_tools` and always-injected delegation / vault tools
(`builtin`, `muninndb`) stay available exactly as they do for a named toolbelt.

`Gate.skipAll` (server oneshot/WebSocket auto-approve, or
`--dangerously-skip-permissions`) does **not** override an empty toolbelt.
Auto-approve skips the human prompt; it is not allow-all-providers.

**Explicit allow-all** is a wildcard entry: `provider: "*"` and/or
`connection_id: "*"`. Agents created before default-deny were migrated to that
wildcard so existing full-access assistants (for example Astra on MJ Mac) keep
working. Review wildcard toolbelts regularly.

---

## Key Code Locations

| Location | What it does |
|---|---|
| `internal/agents/types.go` | `ToolbeltEntry` and `AgentDef` definitions |
| `internal/agents/toolbelt.go` | `ToolbeltProviders()` / `AllowedProviders()` — empty = deny; `*` = allow-all |
| `internal/tools/registry.go` | `AllSchemasForProviders()`, `ProviderFor()`, provider tag at registration |
| `internal/permissions/permissions.go` | `Gate` type, approval gate integration |
| `internal/server/handlers_agents.go` | Agent config loading and toolbelt hydration |

---

## Threat Model

### What the toolbelt protects against

**Lateral movement between providers.** An agent compromised through prompt
injection cannot use a provider that is not in its toolbelt. The attacker
cannot pivot from a GitHub tool call to AWS even if both connections are
configured on the machine.

**Overprivileged automation.** A scheduled or headless agent cannot silently
write to production systems it was not explicitly granted access to. The schema
filtering means the model never plans actions against those systems.

**Accidental misconfiguration.** An agent defined for one purpose (e.g.,
drafting Jira tickets) cannot be accidentally pointed at production
infrastructure because the infrastructure tools are not in its schema set.

**Unsupervised writes to production.** The approval gate ensures that even
when an agent is running in fully autonomous mode, writes to critical systems
surface a prompt. A human is always in the loop for production mutations.

### What the toolbelt does not protect against

**Compromised tool implementations.** If a tool implementation itself is
malicious or contains a vulnerability, the toolbelt does not prevent it from
misusing the connection it was granted.

**Session state exfiltration.** The toolbelt controls which tool schemas are
shown to the model and which tool calls are permitted, but it does not prevent
an agent from reading session history or memory that contains data from other
providers.

**Provider collusion.** If two agents share a connection (same `connection_id`)
and one agent is compromised, the toolbelt on the other agent does not protect
the shared connection's underlying credentials.

**Physical credential access.** The toolbelt is a runtime enforcement layer.
It does not prevent a process running as the same OS user from reading CLI
credentials (`~/.aws/credentials`, `~/.config/gh/`) directly. Defense against
that threat requires OS-level isolation.

---

## See Also

- [connections.md](connections.md) — how connections are established and stored
- [permissions-and-safety.md](permissions-and-safety.md) — the three-tier
  permission gate that the toolbelt integrates with
- [multi-agent.md](multi-agent.md) — how agent definitions are loaded and
  assigned to sessions
