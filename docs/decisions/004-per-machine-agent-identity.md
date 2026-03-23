# ADR-004: Per-Machine Agent Identity

**Date:** 2026-03-03
**Status:** Accepted

## Context

In multi-agent deployments, Huginn instances on different machines need stable, distinguishable identities so that the relay hub can route messages, and so that memory summaries and audit logs can be attributed to a specific machine instance.

## Decision

Each Huginn installation auto-generates a `MachineID` string on first run, stored in `~/.huginn/config.json` as `machine_id`. The format is `<hostname>-<4 random hex bytes>`. A `SessionID` is generated at each orchestrator startup (not persisted — ephemeral per run).

Agents are **per-machine, not global**: each machine has its own `agents.json`, Pebble store, and session summaries. If the user wants to "continue a conversation" across machines, session summaries (ADR-002) provide the cross-machine context — not raw message syncing.

The relay hub routes by `MachineID`. Memory summaries in Pebble are namespaced as `agent:summary:<machineID>:<agentName>:<sessionID>`.

## Consequences

- Agent identity is stable across restarts and upgrades (MachineID persists in config).
- Agents on different machines are independent; no synchronization needed.
- Wiping `~/.huginn/` resets identity — acceptable for dev machines.
- Phase 1 (current): MachineID is generated and stored but not yet used for relay routing.
- Future: `huginn identity` command to view and label the machine identity.
