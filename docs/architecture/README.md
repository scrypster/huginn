# Architecture Documentation

This directory contains deep-dive documents for each major subsystem of Huginn.

| Document | Subsystem | What it covers |
|---|---|---|
| [agent-toolbelt.md](agent-toolbelt.md) | Agent Toolbelt | Per-agent connection scoping and approval gate enforcement |
| [backend.md](backend.md) | LLM Backends | Backend interface, provider implementations, factory, health checks |
| [code-intelligence.md](code-intelligence.md) | Code Intelligence | File chunking, BM25+HNSW search, AST symbol extraction |
| [cloud-connections.md](cloud-connections.md) | Cloud Connections | Cloud OAuth broker flow, relay_key scheme, token security model |
| [connections.md](connections.md) | Connections | Third-party service connection management (AWS, GitHub, etc.) |
| [integrations.md](integrations.md) | MCP | Model Context Protocol client, tool routing |
| [multi-agent.md](multi-agent.md) | Multi-Agent Threads | ThreadManager, AutoHelpResolver, CompletionNotifier, WS events |
| [permissions-and-safety.md](permissions-and-safety.md) | Permissions | Three-tier permission gate, session allow-list, sandbox checks |
| [relay-protocol.md](relay-protocol.md) | Relay | WebSocket relay for remote agent control |
| [scheduler.md](scheduler.md) | Scheduler | YAML Routines, cron triggers, headless sessions, Inbox notifications |
| [session-and-memory.md](session-and-memory.md) | Session + Memory | JSONL message store, MuninnDB integration, session compaction |
| [swarm.md](swarm.md) | Swarm Orchestrator | Parallel agent scheduling, semaphore throttle, SwarmEvent channel |
| [tui.md](tui.md) | TUI | bubbletea terminal UI, event consumer, onboarding wizard |
| [workflows.md](workflows.md) | Workflows | Ordered Routine sequences, WorkflowRunner, run history |

## Subsystem Docs

Additional subsystem-level documentation lives in `../subsystems/`:

| Document | What it covers |
|---|---|
| [storage.md](../subsystems/storage.md) | Pebble KV storage layer, key schema, indexing |
| [streaming-and-runtime.md](../subsystems/streaming-and-runtime.md) | Token streaming, SSE, runtime lifecycle |

---

## Where to Start

**New to Huginn?** Read [swarm.md](swarm.md) and [multi-agent.md](multi-agent.md) —
they explain the two orchestration systems that drive most of what Huginn does.

**Adding an agent tool?** See [permissions-and-safety.md](permissions-and-safety.md)
and `docs/CONTRIBUTING.md`.

**Wiring a new backend?** See [backend.md](backend.md).

**Setting up scheduled tasks?** See [scheduler.md](scheduler.md) and
[workflows.md](workflows.md).
