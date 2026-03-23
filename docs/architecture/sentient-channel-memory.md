# Huginn Memory Replication Strategy

> **Version:** 1.0 — Final Design
> **Status:** Validated across 5 Opus reasoning sessions (2026-03-17)
> **Purpose:** Enterprise-grade multi-agent memory architecture for Huginn channels

---

## 1. The Mental Model

When agents work together in a Huginn channel, they operate like colleagues in the same room. When someone makes a decision, states a fact, or establishes a constraint, everyone hears it. Each person stores that information in their own brain and carries it with them when they leave the room. Huginn implements this by replicating Tier 1 memories (decisions, facts, constraints) from the producing agent's personal MuninnDB vault to every other agent's personal vault. Recipients never run an LLM to receive knowledge — the system writes directly to their vaults. They use the knowledge naturally the next time their LLM runs and their memory prefetch surfaces it.

**The key principle:** Agents carry their knowledge everywhere. There is no "room-dependent" intelligence. Alice in the Bug Fix channel knows the Stripe decision Tom made in Software Team because it's in her personal vault — not because she's querying a shared room-level database.

---

## 2. Architecture Decision: Per-Agent Replication (NOT Shared Channel Vault)

**Rejected:** Shared `huginn:space:{spaceID}` vault that all agents read from.

**Problem with shared vault:** Creates location-dependent knowledge. Alice's intelligence fluctuates based on which channel she's in. She can't take the Stripe decision to the Bug Fix channel because it's locked in the Software Team vault. Agents become stateless workers consulting a shared notebook — not people who remember.

**Chosen:** Per-agent replication. The system replicates Tier 1 memories to all channel member personal vaults via server-side admin MCP writes. Zero LLM cost for recipients. Agents carry knowledge to every channel they're in.

---

## 3. What Replicates (and What Doesn't)

### Replication Policy Table

| MuninnDB Tool | `type` Field | Replicates? | Rationale |
|---|---|---|---|
| `muninn_decide` | any | ✅ Always | Decisions affect all team members |
| `muninn_remember` | `decision` | ✅ Always | Team knowledge by definition |
| `muninn_remember` | `constraint` | ✅ Always | Constraints are universal |
| `muninn_remember` | `fact` | ✅ Yes | Objective shared knowledge |
| `muninn_remember` | `procedure` | ✅ Yes | Processes apply to the team |
| `muninn_remember` | `preference` | ✅ Yes | Tagged with `source_agent` for attribution |
| `muninn_remember` | `issue` | ✅ Yes | Issues affect the whole team |
| `muninn_remember` | `observation` | ❌ Never | Personal cognition — not team knowledge |
| `muninn_remember_tree` | any | ✅ Root + first children | Preserves structure without deep recursion |
| `muninn_evolve` | any | ✅ Replace-on-evolve | See Section 5 |
| All read/recall/link/traverse/feedback tools | — | ❌ Never | Read operations produce no new knowledge |
| Thread `FinishSummary` (on completion) | `event` | ✅ All members | Work output is team knowledge |

### Hard Rules

1. **Never replicate a memory already tagged `replicated:true`.** Breaks circular echo chains.
2. **Never replicate in DM spaces.** One agent, no recipients.
3. **Exclude the producing agent.** Tom doesn't need a copy of his own memory.
4. **Include the lead agent for FinishSummary.** The `CompletionNotifier` only creates a transient chat message — it does NOT persist to Tom's vault. Replication adds durable cross-session memory.

---

## 4. The Zero-LLM-Cost Flow

Step-by-step execution when Tom stores a decision and Alice receives it.

### Step 1: Tom's LLM calls `muninn_decide` (LLM cost paid here — only here)

Tom's `RunLoop` in `loop.go` executes the tool via `executeSingle()`. The vault-locked MCP adapter writes to `huginn:agent:user:tom`. This is Tom's normal LLM turn.

### Step 2: `OnToolDone` fires (no LLM)

At `loop.go` after `tool.Execute()` returns, the `OnToolDone` callback fires. The replication interceptor inspects: tool name = `muninn_decide` → always replicate. Resolves channel members from the space context (`workforce.GetSpaceContext`).

### Step 3: Fan out async goroutines (no LLM)

For each member except Tom:
```go
go replicateToVault(adminClient, targetVault, concept, content, memType, tags)
```
Tom's LLM turn continues immediately. Replication is a non-blocking side-effect.

### Step 4: Admin MCP client writes to Alice's vault (no LLM)

The server-level admin MCP client (NOT vault-locked, using global admin token) calls `muninn_remember` on Alice's vault:
```json
{
  "vault": "huginn:agent:user:alice",
  "concept": "payment processor decision",
  "content": "Using Stripe for payments. Webhook-based architecture.",
  "type": "decision",
  "tags": ["replicated:true", "source:tom", "channel:software-team",
           "replicated_concept:payment-processor-decision"]
}
```
Cost: one HTTP POST. Zero LLM tokens.

### Step 5: Alice's next turn surfaces the memory (Alice's normal turn cost)

When MJ next messages Alice (in any channel or DM), her `prefetchMemoryContext()` calls `muninn_where_left_off` against her personal vault. The Stripe decision appears. Her LLM reads it as `## Memory Context`. Alice knows about the decision without being explicitly told.

### LLM Cost Summary

| Step | LLM Tokens | Who Pays |
|---|---|---|
| Tom stores decision | Normal turn cost | Tom's session |
| Replication to Alice | **0** | System (MCP write only) |
| Replication to Bob | **0** | System (MCP write only) |
| Alice's next turn (memory surfaces) | Normal turn cost | Alice's session |

---

## 5. Replace-on-Evolve: The Decision Lifecycle

**Design Principle:** *Replicated memories are projections, not copies. Each vault is a materialized view of current state. History belongs at the source (Tom's vault), not at every consumer.*

**Rule: One decision, one memory. Replace on evolve. Never append.**

### Creation
Tom calls `muninn_decide("use Stripe")`. System replicates to Alice and Bob with tags including `replicated_concept:payment-processor-decision`.

### Evolution
Tom calls `muninn_evolve` → "Switching to Square, Stripe too expensive."

For each member vault, the interceptor:
```
1. Write ONE new self-documenting memory:
   concept: "payment processor decision"
   content: "Decision updated [2026-03-17]: Switching to Square (previously Stripe).
             Reason: Stripe too expensive ($0.029/tx vs Square $0.026/tx).
             This supersedes the earlier Stripe decision."
   type: "decision"
   tags: ["replicated:true", "source:tom", "channel:software-team",
          "replicated_concept:payment-processor-decision"]

2. Find previous version using TAG-BASED lookup (deterministic):
   muninn_find_by_entity(entity="replicated_concept:payment-processor-decision")
   (Fallback: muninn_recall filtered to tags=["replicated:true","source:tom"], threshold=0.7)

3. If found → muninn_forget(old_id)   ← soft-delete, recoverable 7 days

4. Done. No SQLite tracking table. No graph edge needed. No stale conflict.
```

**Why tag-based lookup (not semantic recall):** Semantic recall at threshold 0.8 could return Alice's own "payment processor research notes" and accidentally delete her personal work. The `replicated_concept:` tag is a deterministic key. Primary lookup must be deterministic.

**Why self-documenting content:** Insurance. If soft-delete fails or races, Alice's LLM reads "this supersedes the Stripe decision" and reasons correctly from the text alone — no graph edge or recall ranking needed.

**Why NO SQLite tracking table:** The table's own failure mode is worse — if the initial replication write failed, the table has no row and evolution silently misses Alice. Tag-based lookup is self-healing.

**All edge cases follow the same rule:**
- Tom deletes a decision → `muninn_forget` from all member vaults
- Tom splits one decision into two → write two new, delete old
- New agent joins channel → replicate current state only (historical backfill in Phase 5)
- Agent removed from channel → they keep what they already learned (they were in the room)

---

## 6. Provenance-Aware Recall Formatting

When `muninn_where_left_off` returns results, post-process in `prefetchMemoryContext()` to partition by source:

```
## Memory Context

### Your observations
- [Alice's self-authored memories]

### Team knowledge (from channel)
- [decision] "Switching to Square for payments" — from Tom, #software-team, 2026-03-15
- [constraint] "Stripe rate limit: 100 webhooks/sec" — from Tom, 2026-03-14
```

Gives the LLM clear epistemic framing: *what I know* vs *what the team decided*. This prevents Alice from treating Tom's replicated decisions as her own observations.

---

## 7. Failure Modes and Mitigations

| Failure | Impact | Mitigation |
|---|---|---|
| MuninnDB unreachable during replication | Low — Tom's turn completes normally | SQLite retry queue, 5 retries, drain on startup |
| Admin MCP token expired | Medium — all replications fail | Health check on startup; log on first failure |
| Race: evolve fires before initial replication completes | Low | Self-documenting content resolves LLM-level; retry queue can version-check |
| Tag-based lookup returns false positive | Medium — wrong memory soft-deleted | Soft-delete is recoverable 7 days via `muninn_restore` |
| New member joins after decisions were made | Low | Phase 5 backfill on member-add |
| Prefetch cache serves stale `where_left_off` (5min TTL) | Low | Reduce to 60s TTL (Phase 4) |
| `channelRecent` is the safety net during race window | — | Last 20 channel messages always injected regardless of vault state |

**The acceptable race:** Tom stores a decision at t=0. MJ asks Alice about it at t=3s. If replication hasn't landed yet, Alice doesn't have the structured memory — but `channelRecent` still shows Tom's raw message. Alice sees the context. If Alice is in a *different* channel, neither apply; she'll get it on her next turn after replication completes. This is acceptable.

---

## 8. Technical Architecture Details

### Vault Write Authorization — Security Decision

> **Rejected:** Global admin MDB token (`mdb_*`) that can write to any vault.
>
> **Why rejected:** An admin key turns vault isolation into a suggestion. If a replication bug targets the wrong vault, an admin key succeeds silently. A per-vault key fails with an auth error. More importantly, the principle "each agent owns their memory" should be enforced at the credential layer, not just in application code.

**Chosen: Per-Agent Vault Keys (Option A)**

Each agent already has an `mk_*` vault-scoped token stored in Huginn's agent config — `connectAgentVault()` uses it every time Alice starts a session. Replication uses that same token. No new secrets. No new surface area.

```
- For each replication target: open MCP connection using THAT AGENT'S mk_* token
- Alice's vault gets written with Alice's credentials, not God credentials
- If a bug targets the wrong vault → auth error, not silent corruption
- If Alice's token is compromised → only Alice's vault is at risk
- Lazy-initialize per-agent MCP connections from stored config when not already cached
- Reuse active connections when agent session is live
```

**Long-term:** File a MuninnDB feature request for explicit cross-vault trust relationships (`mk_tom` authorized to push to `mk_alice`'s vault for specific memory types). This makes authorization explicit at the DB layer. Use Option A until then.

**The security principle:**
> *A vault write must be authorized by credentials scoped to that vault — never by credentials that could write to a vault they weren't intended for.*

### SQLite Retry Queue Schema

```sql
CREATE TABLE memory_replication_queue (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    target_vault  TEXT NOT NULL,
    payload       TEXT NOT NULL,  -- JSON: {concept, content, type, tags, ...}
    operation     TEXT NOT NULL,  -- "remember" | "forget"
    attempts      INTEGER NOT NULL DEFAULT 0,
    next_retry_at TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
```

Retry schedule: 5s → 30s → 2min → 10min → 1hr. Max 5 attempts. Drain goroutine on startup.

### Intercept Point

`OnToolDone` callback in `loop.go`, after `executeSingle()` returns successfully. Replication is fire-and-forget — Tom's `RunLoop` does not wait.

### Classifier Logic (pseudocode)

```go
func shouldReplicate(toolName, memType string, tags []string) bool {
    // Never replicate a replica
    if contains(tags, "replicated:true") { return false }

    switch toolName {
    case "muninn_decide":
        return true
    case "muninn_remember", "muninn_remember_batch":
        return memType == "decision" || memType == "constraint" ||
               memType == "fact" || memType == "procedure" ||
               memType == "preference" || memType == "issue"
    case "muninn_remember_tree":
        return true  // root + first children only
    case "muninn_evolve":
        return true  // triggers replace-on-evolve flow
    }
    return false
}
```

---

## 9. Token Budget

### Current Per-Message (lead agent, before full implementation)

| Component | Tokens | Source |
|---|---|---|
| `baseBriefing` (persona + tools + skills) | ~1,500 | 5min cache |
| `spaceContext` | ~100 | Per message |
| `channelRecent` (last 20 messages) | ~2,000 | 30s cache, channels only |
| `memoryContext` (`where_left_off`) | ~500 | 2s timeout, 5min cache |
| `ctxText` (semantic search) | ~500 | Per message |
| `history(50)` | ~5,000 | Session |
| User message | ~200 | Variable |
| **Total** | **~9,800** | |

### After Full Implementation

| Component | Delta | New Total |
|---|---|---|
| All existing | 0 | ~9,800 |
| `muninn_recall(userMessage)` — Phase 4 | +500 | +500 |
| Replicated memories in `where_left_off` (amortized) | +200 | +200 |
| **Total** | **+700** | **~10,500** |

**~7% prompt token increase for a system that feels like a real team.** Zero increase for the producing agent's message.

---

## 10. Implementation Phases

### Phase 1: Core Replication — CREATE

**Output:** Tom stores a decision → Alice and Bob have it within seconds, zero LLM cost.

- [ ] Admin MCP client singleton (startup, connection-pooled, not vault-locked)
- [ ] `memory_replication_queue` SQLite table + drain-on-startup goroutine
- [ ] `OnToolDone` replication interceptor (tool name + type classifier)
- [ ] Provenance tags on all replicas (`replicated:true`, `source:`, `channel:`, `replicated_concept:`)
- [ ] Anti-echo rule (skip memories tagged `replicated:true`)
- [ ] Channel membership resolver (space ID → list of agent vault names)

### Phase 2: Replace-on-Evolve — UPDATE

**Prerequisite:** Phase 1 complete.
**Output:** Decision updates propagate cleanly. Alice always has current state.

- [ ] Evolve interceptor in `OnToolDone`
- [ ] Tag-based old-version lookup (`replicated_concept:` tag)
- [ ] Write new self-documenting memory + soft-delete old
- [ ] Fallback to semantic recall if tag lookup unavailable

### Phase 3: Thread FinishSummary Replication

**Prerequisite:** Phase 1 complete.
**Output:** Completed delegation work persists in all members' vaults permanently.

- [ ] Hook into `spawn.go` ErrFinish handler after `tm.Complete()`
- [ ] Replicate `FinishSummary` to all channel members (including lead agent, excluding producing agent)
- [ ] Format: `type="event"`, tagged `thread:completion`

### Phase 4: Enhanced Prefetch

**Prerequisite:** None (independent).
**Output:** Agents surface topically relevant memories proactively, not just "where we left off."

- [ ] Add `muninn_recall(userMessage, mode=balanced, limit=5, threshold=0.6)` to `prefetchMemoryContext()`
- [ ] Cache 60s by SHA-256 of message text
- [ ] Inject as `## Relevant Memory` block after `## Memory Context`
- [ ] Reduce prefetch cache TTL from 5min to 60s

### Phase 5: Member-Add Backfill

**Prerequisite:** Phase 1 complete.
**Output:** New channel members receive historical team decisions.

- [ ] On `UpdateSpace` adding new members, trigger backfill goroutine
- [ ] Query lead agent vault for all memories tagged `channel:{space-name}` + `replicated:true`
- [ ] Replicate missing memories to new member vaults

---

## 11. MuninnDB Tool Usage Reference for Agents

### Lead Agent Prompt Fragment (memory instructions)

```
## Memory & Team Awareness

You operate in two contexts — your personal memory and this channel.

### When to store memories
- A decision is made → muninn_decide (decision + rationale + alternatives)
- A fact is established → muninn_remember (type: "fact")
- A constraint is identified → muninn_remember (type: "constraint")
- A user preference is expressed → muninn_remember (type: "preference")
- Starting a multi-step project → muninn_remember_tree for the project structure
- A previously stored fact changes → muninn_evolve (NOT a new memory — update the old one)
- An internal thought or working note → muninn_remember (type: "observation") — STAYS PRIVATE

### When to recall
- Before answering about past work → muninn_recall (topic)
- Unsure if something was discussed → muninn_recall (mode: "deep")
- "Why did we do X?" → muninn_recall (profile: "causal")
- Reviewing a plan for risks → muninn_recall (profile: "adversarial")
- Information already visible in conversation → DO NOT recall what's above you

### Session return
Synthesize a natural 2-3 sentence status update from your Memory Context.
Focus on: what was in progress, key decisions, open questions.
Speak as a colleague who remembers — not a system listing entries.
```

### Subagent Prompt Fragment

```
## Memory

Channel context and recent team decisions are already in your Memory Context above.
Use them to work independently without asking the lead for repetition.

When completing your task, include a thorough summary in the finish tool:
- What you found/built/decided
- Key recommendations with data points
- Any caveats or open questions

Your summary becomes permanent team knowledge.
```

---

## 12. What This Feels Like

When you're chatting with Tom in the Software Team channel and he decides to use Square for payments, every agent on the team immediately absorbs that decision — silently, at zero cost. Ten minutes later, when you open a DM with Alice and ask her to build the checkout page, she already knows it's Square. You never repeated yourself. If Tom later changes his mind to PayPal, Alice's Square decision is cleanly replaced — she never argues from outdated context. When a new agent joins the channel, they receive the complete history of current decisions as if they'd been in the room all along.

That's not a feature. That's a team.

---

*Document maintained at `docs/architecture/sentient-channel-memory.md`*
*Validated by Claude Opus 4.6 across 5 reasoning sessions — 2026-03-17*
