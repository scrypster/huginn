# ADR-001: Skills as Packaging, Not Execution

**Date:** 2026-03-03
**Status:** Accepted

## Context

Huginn needs a mechanism for bundling domain-specific context (prompt fragments, workspace rules, and eventually tools) that can be loaded at startup and injected into every agent context. The question was whether a "skill" should be an executable unit (a runnable agent or plugin) or a passive bundle of prompt and rule content consumed by the existing Orchestrator/ContextBuilder pipeline.

## Decision

A Skill is a passive data bundle, not an execution unit. Phase 1 Skills expose:
- `SystemPromptFragment()` — text injected into the system prompt
- `RuleContent()` — workspace rule text merged into context
- `Tools() []tools.Tool` — always nil in Phase 1; reserved for Phase 2

Skills are loaded once at startup by the Loader, registered in a SkillRegistry, and their combined content is set on the ContextBuilder via `SetSkillsFragment()`. The Orchestrator needs no per-call awareness of skills — the injection is transparent through `Build()`.

## Consequences

- Phase 1 is simpler: no dynamic skill dispatch, no skill-scoped tool sandboxing.
- Phase 2 tool injection is forward-compatible — the `Tools()` method is already on the interface.
- Skills cannot react to user queries in Phase 1 (static injection only).
- Skill content contributes to token budget; very large skills may crowd out repo context.
