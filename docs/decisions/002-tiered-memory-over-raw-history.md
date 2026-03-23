# ADR-002: Tiered Memory Over Raw Conversation History

**Date:** 2026-03-03
**Status:** Accepted

## Context

As Huginn agents are used across long sessions, raw conversation history grows unboundedly. Sending the full history on every request increases latency, cost, and risks exceeding the context window. The naive fix — a sliding window — discards semantically important older turns.

## Decision

Huginn uses a three-tier memory model:
1. **Hot history** — last `maxHistoryMessages` messages (currently 20), kept in RAM, sent verbatim.
2. **Warm summary** — at session close, an LLM-generated structured summary is persisted to Pebble KV and injected into the agent's system prompt on next session start.
3. **Delegation log** — last 10 consultation exchanges per agent pair, persisted to Pebble, providing context for inter-agent referrals.

Skills and workspace rules are injected via ContextBuilder and do not count against history — they are always current and always present.

## Consequences

- Conversations can span arbitrarily long sessions without OOM or context overflow.
- Old details may be lost unless the summarizer captures them (summarizer quality matters).
- The tiered model requires the summarizer agent (Track B) to run at session close.
- Per-machine agent identity (ADR-004) is stored in cold storage, not history.
