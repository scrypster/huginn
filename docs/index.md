# Huginn — Documentation Index

Find what you need by what you want to do.

---

## If you want to get up and running fast
→ [Getting Started](getting-started.md)

## If you want to use the web UI
→ [Web UI](features/web-ui.md) — all views, thread panel, session list, spaces, inbox, profile popover

## If you want to run multiple agents in parallel
→ [Multi-Agent](features/multi-agent.md) — Chris, Steve, Mark, delegation, thread panel

## If you want to create your own agents
→ [Custom Agents](features/custom-agents.md) — name, model, persona, memory, tool access

## If you want ongoing conversations with agents
→ [Spaces](features/spaces.md) — DMs and Channels, team rooms, workstreams, memory replication

## If you want agents to remember things across sessions
→ [Sessions](features/sessions.md) — persistent history, session naming, compaction
→ [Memory](features/memory.md) — context notes and MuninnDB cross-session memory
→ [Notepad](features/notepad.md) — inject standing project context into every session

## If you want to teach agents new behaviors
→ [Skills](features/skills.md) — SKILL.md files, system prompt injection, skill tools

## If you want skills from the community
→ [Skills Registry](features/skills-registry.md) — browse, install, and publish community skills

## If you want to automate recurring tasks
→ [Routines](features/routines.md) — YAML Routines, cron scheduling, Inbox

## If you want to chain automations into pipelines
→ [Workflows](features/workflows.md) — ordered sequences of steps, variables, failure handling, notifications

## If you want to connect external tools
→ [Connections](features/connections.md) — GitHub, Jira, Slack, MCP servers, OAuth

## If you want agents to understand your codebase
→ [Code Intelligence](features/code-intelligence.md) — BM25 index, vector search, impact analysis

## If you want to review what agents produce
→ [Artifacts](features/artifacts.md) — structured outputs, review lifecycle, accept/reject, download

## If you want to run Huginn on a server or in Docker
→ [Headless Mode](features/headless.md) — `--server` flag, CI/CD, Docker, no-TTY

## If you want to access your agents from multiple machines
→ [HuginnCloud](features/huginncloud.md) — connect, WebSocket relay, fleet deployments

## If you want to understand what agents can and cannot do
→ [Permissions](features/permissions.md) — three-tier gate, approval flow, auto-run, headless mode

## If you need CLI flags and environment variables
→ [CLI Reference](reference/cli.md) — all flags, all subcommands, environment variables

## If you need config file options
→ [Config Reference](reference/config.md) — complete config schema with defaults and examples

## If you need TUI keybindings and slash commands
→ [TUI Reference](reference/tui.md) — keyboard shortcuts, slash commands, app states

## If you need a glossary of terms
→ [Glossary](reference/glossary.md) — definitions for Huginn-specific terms

## If something is broken
→ [Troubleshooting](troubleshooting.md) — common problems and fixes

## If you want to contribute to Huginn
→ [Contributing](CONTRIBUTING.md)

---

## Architecture & Internals

These docs go deeper for contributors and people building on top of Huginn:

- [Backend Architecture](architecture/README.md)
- [Multi-Agent Internals](architecture/multi-agent.md)
- [Relay Protocol](architecture/relay-protocol.md)
- [Session & Memory](architecture/session-and-memory.md)
- [Swarm](architecture/swarm.md)
- [Workflows](architecture/workflows.md)
- [Scheduler](architecture/scheduler.md)
- [Connections](architecture/connections.md)
- [Permissions & Safety](architecture/permissions-and-safety.md)
- [TUI Architecture](architecture/tui.md)
- [Agent Toolbelt](architecture/agent-toolbelt.md)
- [Code Intelligence](architecture/code-intelligence.md)
- [Integrations & MCP](architecture/integrations.md)
