# Glossary

Definitions for Huginn-specific terms. When documentation uses these terms, they carry the meanings defined here.

---

## Agent

A named AI persona with its own model, system prompt, color, icon, and memory vault. Huginn ships with three default agents — Chris (planner), Steve (coder), and Mark (reviewer) — but you can create as many custom agents as you need. Each agent maintains its own conversation history and can have different tool access, skills, and provider configuration.

See: [Custom Agents](../features/custom-agents.md)

---

## Artifact

A structured output an agent produces during its work, separate from the chat message stream. Artifacts have a type (code_patch, document, structured_data, file_bundle, timeline), a status lifecycle (draft → accepted/rejected/superseded), and can be reviewed before being applied. Large artifacts are stored on disk rather than inline.

See: [Artifacts](../features/artifacts.md)

---

## BM25

A text-search algorithm Huginn uses to find relevant code chunks by keyword matching. BM25 excels at finding exact symbol names, function names, and specific identifiers. Used in hybrid search alongside HNSW (vector search).

See: [Code Intelligence](../features/code-intelligence.md)

---

## Channel

A multi-agent collaboration space where a lead agent orchestrates and multiple member agents contribute. Channels have persistent conversation history, icons, colors, and can have space-specific Workstreams. Channel memory is replicated to all member agent vaults.

See: [Spaces](../features/spaces.md)

---

## Compaction

The process of summarizing old conversation history to free up context window space. Triggered when the context fill ratio reaches the `compact_trigger` threshold. Two strategies: LLM (planner model writes a summary) and Extractive (BM25 selects the most relevant chunks).

See: [Config Reference](config.md) → Context & Memory

---

## Context Window

The total amount of text (prompt + conversation + tool results) that fits in a single model request. Huginn manages the context window automatically, compacting history when it fills up. Controlled via `context_limit_kb` in config.

---

## Cron

Standard 5-field time expression used to schedule Routines and Workflows. Example: `0 9 * * 1-5` = every weekday at 9am. Huginn uses `robfig/cron/v3` — no OS-level cron required.

See: [Routines](../features/routines.md)

---

## DM (Direct Message)

A private conversation space between you and a single named agent. DMs persist across sessions with full history, giving you an ongoing relationship with a specific agent. Separate from the main chat sessions.

See: [Spaces](../features/spaces.md)

---

## Delegation

When a primary agent hands off a sub-task to another named agent. The delegating agent provides a task description and rationale; the sub-agent runs it independently and posts a completion summary back. Delegation creates a Thread.

See: [Multi-Agent](../features/multi-agent.md)

---

## Gate

The permission enforcement point for tool calls. Every tool invocation passes through the Gate, which decides whether to auto-approve it (read tools), prompt the user for approval (write/exec tools), or deny it. The Gate maintains a per-session cache of approved tools so you are not re-prompted for the same tool in the same session.

See: [Permissions](../features/permissions.md)

---

## HNSW

Hierarchical Navigable Small World — the vector index algorithm Huginn uses for semantic code search. When `semantic_search` is enabled, code chunks are embedded as vectors and stored in an HNSW index. At query time, HNSW finds semantically similar code even when it uses different words than the query.

See: [Code Intelligence](../features/code-intelligence.md)

---

## Impact Analysis

A bounded graph traversal that finds which files would be affected by changing a given file or symbol. Huginn walks the import graph up to 4 hops deep (max 2,000 nodes) using the `/impact` slash command or the Proactive Impact Radar.

See: [Code Intelligence](../features/code-intelligence.md)

---

## Inbox

The notification center where completed Workflow and Routine results are delivered. Unlike chat sessions, Inbox notifications are separate from your active conversations. A live badge counter updates in real time over WebSocket.

See: [Routines](../features/routines.md), [Workflows](../features/workflows.md)

---

## Lead Agent

In a Channel space, the lead agent is the designated orchestrator. It directs work, delegates to member agents, and synthesizes results. The lead agent is set when creating a channel and can be changed via the web UI.

See: [Spaces](../features/spaces.md)

---

## Local Tools

The set of built-in Huginn tools (bash, read_file, write_file, grep, git, etc.) that an agent is permitted to use. Configured per-agent via the `local_tools` field — either `["*"]` (all built-ins) or an explicit list of tool names.

See: [Local Tools](../features/local-tools.md)

---

## Machine ID

A stable 8-character hex identifier for your Huginn installation. Generated on first run and persisted. Used for HuginnCloud relay registration and agent memory segmentation. Not tied to hostname — survives hostname changes.

---

## MCP (Model Context Protocol)

A protocol for connecting external tool servers to agents. MCP servers expose custom tools that agents can call alongside built-in tools. Configured in `~/.huginn/config.json` under `mcp_servers`. Huginn uses stdio transport to communicate with MCP servers.

See: [Connections](../features/connections.md), [Config Reference](config.md)

---

## MuninnDB

An optional external memory service that gives agents persistent, cross-session memory. When configured, agents can recall decisions, facts, and context from previous sessions. Memory is stored per-vault and scoped per workspace.

See: [Memory](../features/memory.md)

---

## Notepad

A persistent markdown file injected into every agent's system prompt. Used for standing project context, coding conventions, and recurring instructions. Notepads live in `~/.huginn/notepads/` (global) or `.huginn/notepads/` (project-local). Sorted by priority; project notepads override global ones.

See: [Notepad](../features/notepad.md)

---

## Outbox

A durable SQLite-backed message queue used by the Satellite relay. Ensures messages to HuginnCloud are not lost if the connection drops — they are retried in order when connectivity is restored.

---

## Permissions

The three-tier system controlling what agents can do: **Read** (auto-approved), **Write** (requires user approval), **Exec** (requires user approval). The Gate enforces permissions on every tool call.

See: [Permissions](../features/permissions.md)

---

## Planner / Coder / Reasoner

The three default agent slots in Huginn:
- **Planner** (Chris): `qwen3-coder:30b` — architectural planning and design
- **Coder** (Steve): `qwen2.5-coder:14b` — implementation
- **Reasoner** (Mark): `deepseek-r1:14b` — analysis, debugging, review

Each slot can be pointed at any model via config or the `/model` command.

See: [Multi-Agent](../features/multi-agent.md)

---

## Proactive Impact Radar

A background analysis that watches for files that may be affected by recent edits. Results surface via `/radar` in the TUI or in the web UI. Powered by the BFS impact analysis system.

See: [Code Intelligence](../features/code-intelligence.md)

---

## Radar

See Proactive Impact Radar.

---

## Relay

See Satellite.

---

## Routine

A single YAML-defined agent task that runs on a cron schedule. Routines live in `~/.huginn/routines/`. When they complete, results are posted to the Inbox. A Routine with `trigger.mode: manual` can be referenced as a step inside a Workflow.

See: [Routines](../features/routines.md)

---

## Satellite

The local HuginnCloud client running inside the Huginn process. The Satellite maintains a persistent WebSocket connection to `api.huginncloud.com`, enabling remote access to your agents from any browser. Uses a durable Outbox to survive connection drops.

See: [HuginnCloud](../features/huginncloud.md)

---

## Session

A named conversation with full persistent message history. Sessions are stored in SQLite. Each session has its own context, active agent, and space association. Sessions can be resumed, renamed, archived, and searched.

See: [Sessions](../features/sessions.md)

---

## Skill

A markdown file that injects instructions into agent system prompts. Skills extend agent behavior without modifying Huginn itself. Enabled skills are merged into the system prompt at session start. Skills can also expose custom tools via a `tools/` directory.

See: [Skills](../features/skills.md), [Skills Registry](../features/skills-registry.md)

---

## Space

A persistent collaborative conversation room. Two types: DM (one agent) and Channel (multiple agents with a lead). Spaces retain full history, support workstreams, and can replicate memory to all member agents.

See: [Spaces](../features/spaces.md)

---

## Swarm

The parallel agent execution system. A Swarm runs multiple independent agent tasks concurrently, each in its own goroutine, up to a configurable maximum (default 16). Used by `/parallel` and `--agent` multi-agent patterns.

See: [Multi-Agent](../features/multi-agent.md)

---

## Thread

A delegated sub-task running inside a session. Threads have a status lifecycle: queued → thinking → tooling → done/blocked/cancelled/error. The Thread Panel in the web UI shows live streaming output from active threads. Threads can have token budgets and timeouts.

See: [Multi-Agent](../features/multi-agent.md)

---

## Thread Panel

The right-side panel in the web UI that shows live streaming output from active sub-agent threads. Each thread appears as a card with status, token count, and token stream. When a thread completes, its summary is posted into the main chat.

---

## Toolbelt

A per-agent allowlist of external connection tools (GitHub, Slack, Jira, etc.) that the agent is permitted to use. An empty toolbelt means no connection tools are available to the agent. The toolbelt is separate from Local Tools (built-in tools).

See: [Agent Toolbelt](../features/agent-toolbelt.md)

---

## Vault

A named namespace in MuninnDB where an agent stores its memories. The default vault pattern is `huginn:agent:<username>:<agentname>`. Vaults can be shared between agents or scoped per workspace. Configured via the `vault_name` field in agent configuration.

See: [Memory](../features/memory.md)

---

## Workstream

A Workflow scoped to a specific Space (DM or Channel). Workstreams run independently of global Workflows and post results back into the space. Created and managed from within the space in the web UI.

See: [Spaces](../features/spaces.md), [Workflows](../features/workflows.md)

---

## Workspace

The project root directory where Huginn was launched. Huginn auto-detects the workspace from `huginn.workspace.json`, Git repo root, or the current directory. The workspace determines which files are indexed and which project-local notepads and skills are loaded.

See: [Code Intelligence](../features/code-intelligence.md)
