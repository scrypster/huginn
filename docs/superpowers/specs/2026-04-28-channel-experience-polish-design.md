# Channel Experience Polish — Design Spec

**Date:** 2026-04-28
**Status:** Draft
**Phase:** 3 — Desktop UX Polish

**Prerequisites:** Phase 2 (PR #63) must be merged. Provides `GET /api/v1/memory/replication-status` used by the replication chip.

---

## Problem

Channels (spaces where multiple agents collaborate) feel like a raw multi-agent chatlog. Three gaps:

1. The member list is buried in a modal (`AgentRosterModal`). You have to click a button to see who's in the channel. There's no persistent visual sense of "who's here."
2. When an agent posts in a channel its attribution is minimal — name and color hash, but no description or indication of role (lead vs. member).
3. Memory replication (Phase 2) is invisible. When Alice's memory fans out to Bob and Carol, nothing surfaces in the UI. Users don't know the feature is working.

---

## Scope

**In scope:**
- Persistent member panel: collapsible right sidebar in channel views showing agent name, role, description, and memory status
- Agent description tooltip: hover an agent name in the message stream → see their one-line description
- Memory replication chip: polling-based status indicator in channel header showing "Memory synced · N agents"

**Out of scope:**
- Real-time `memory_replicated` WS event (requires backend broadcast wiring — Phase 4)
- Typing indicators or presence (online/offline) — not meaningful for AI agents
- DM-mode changes (covered in DM Experience Polish spec)
- Mobile/responsive changes

---

## Architecture

### 1. Persistent Member Panel (frontend only)

The channel header already shows a row of agent avatar chips (lines 169-181 in `ChatView.vue`). The existing `AgentRosterModal` is a full modal with good detail but requires a deliberate click to open.

**Change:** Add a collapsible right sidebar panel to the channel layout. Default state: collapsed (shows a narrow strip with agent avatar initials stacked vertically). Expanded state: shows a list of agents with name, role badge, and description.

**Layout:** The channel view becomes a three-column layout when expanded:
```
[message list] [member panel]
```
The panel is 220px wide when open, hidden when closed. A toggle button (chevron) sits at the panel edge.

**Member panel content per agent:**
```html
<div class="flex items-start gap-2 px-3 py-2">
  <!-- Color dot / avatar initial -->
  <div class="w-6 h-6 rounded-full flex-shrink-0 flex items-center justify-center text-[10px] font-bold text-white"
       :style="{ background: agentColor(member) }">
    {{ member.name[0].toUpperCase() }}
  </div>
  <div class="min-w-0">
    <div class="flex items-center gap-1">
      <span class="text-xs font-medium text-[var(--color-text)] truncate">{{ member.name }}</span>
      <span v-if="member.isLead" class="text-[10px] px-1 py-0.5 rounded bg-[var(--color-accent)]/10 text-[var(--color-accent)]">Lead</span>
    </div>
    <p class="text-[11px] text-[var(--color-text-muted)] line-clamp-2 mt-0.5">{{ member.description || 'No description' }}</p>
    <p v-if="member.vaultName" class="text-[10px] text-[var(--color-text-muted)] mt-0.5 truncate">
      🧠 {{ member.vaultName }}
    </p>
  </div>
</div>
```

**State:** `memberPanelOpen: boolean` stored in `localStorage` keyed by `huginn:memberPanel:${spaceId}`, so each channel remembers its panel state independently.

**Data source:** `spaceAgents` computed already exists in `ChatView.vue` (line 1187) and returns `Agent[]` from the agents list. The `Agent` interface includes `description?: string` and `vault_name?: string` (both serialized from `AgentDef` on the backend). Replace `spaceAgents` with a `spaceMemberCards` computed that produces an enriched type:

```ts
interface SpaceMemberCard {
  name: string
  description: string
  vaultName: string      // from agent.vault_name
  isLead: boolean
  color: string
}

const spaceMemberCards = computed<SpaceMemberCard[]>(() => {
  const space = activeSpace.value
  if (!space) return []
  const leadName = space.leadAgent
  const names = [leadName, ...space.memberAgents]   // Space uses memberAgents, not members
  return names.map(n => {
    const agent = agentsList.value.find(a => a.name === n)
    return {
      name: n,
      description: agent?.description ?? '',
      vaultName: agent?.vault_name ?? '',
      isLead: n === leadName,
      color: agentColor(n),
    }
  })
})
```

Use `spaceMemberCards` in `ChannelMemberPanel.vue` (replace `member.vaultName` with `member.vaultName`, `member.description` with `member.description` — fields already named correctly above).

No backend changes.

---

### 2. Agent Description Tooltip (frontend only)

In the message stream, when an agent posts a message, `AgentMessageHeader.vue` already shows the agent's name and color. Add a tooltip that appears on hover over the agent name showing their `description` field.

**Change in `AgentMessageHeader.vue`:** Wrap the agent name in a `title` attribute (native browser tooltip) or a lightweight custom tooltip component if the design system has one:

```html
<span
  class="text-xs font-semibold text-[var(--color-text)] cursor-default"
  :title="agentDescription || undefined"
>{{ agentName }}</span>
```

`agentDescription` prop: passed from `ChatView.vue` → looked up from the agents map by `msg.agent`. The agents map is already loaded in `ChatView.vue` (grep for `agentMap` or the agent list fetch). If the agent name isn't in the map, `agentDescription` is undefined and no tooltip renders.

No backend changes.

---

### 3. Memory Replication Chip (frontend + polling)

Show a subtle chip in the channel header when memory replication is active for the channel. Uses the existing `GET /api/v1/memory/replication-status` endpoint (Phase 2).

**Polling:** When a space view is active (i.e., `spaceId` prop is set), poll the replication status endpoint every 30 seconds. Stop polling when the view is unmounted.

**API response shape** (from Phase 2 `GET /api/v1/memory/replication-status` — `handlers_memory.go`):
```json
{
  "pending": 0,
  "failed": 0,
  "dead": 0,
  "connected": true
}
```
- `pending`: memories queued and waiting to replicate
- `failed`: replication attempts that have failed but will retry (exponential backoff)
- `dead`: retries exhausted — permanent failures
- `connected`: whether MuninnDB is reachable at all

**Chip logic:**
- If `pending > 0`: show "🧠 Memory syncing…" (active/pulsing)
- If `pending === 0 && failed === 0 && dead === 0 && connected`: show "🧠 Memory synced · N agents" where N = `spaceAgents.length` — only shown after the first successful poll, not on initial page load
- If `failed > 0 || dead > 0`: show "🧠 Memory sync issues" in amber
- If `!connected` or poll hasn't completed yet: show nothing (MuninnDB not configured is not an error state for the channel view)

**Chip placement:** In the channel header bar, to the right of the space name and member chips, left of the roster button.

```html
<span v-if="replChipText" class="text-[10px] px-2 py-0.5 rounded-full"
  :class="replChipClass">
  {{ replChipText }}
</span>
```

`replChipText` and `replChipClass` are computed refs derived from the polled status.

**Composable:** Extract polling logic into `useReplicationStatus(spaceId: Ref<string | undefined>)` in `web/src/composables/useReplicationStatus.ts`. Returns `{ chipText, chipClass }`.

Use `watch(spaceId, ...)` to reactively start/stop the interval — this handles navigation between DMs (no spaceId) and channels (spaceId set) without remounting `ChatView`:

```ts
export function useReplicationStatus(spaceId: Ref<string | undefined>) {
  const status = ref<ReplicationStatus | null>(null)
  let timer: ReturnType<typeof setInterval> | null = null

  async function poll() {
    try {
      status.value = await apiFetch('/api/v1/memory/replication-status')
    } catch { /* swallow — chip just stays hidden */ }
  }

  watch(spaceId, (id) => {
    if (timer) clearInterval(timer)
    timer = null
    if (id) {
      poll()                          // immediate first fetch
      timer = setInterval(poll, 30_000)
    } else {
      status.value = null             // clear chip when leaving channel
    }
  }, { immediate: true })

  onScopeDispose(() => { if (timer) clearInterval(timer) })

  const chipText = computed(() => { /* logic above */ })
  const chipClass = computed(() => { /* ... */ })
  return { chipText, chipClass }
}
```

No backend changes.

---

### 4. Remove Roster Modal Redundancy

With the persistent member panel now available, the `AgentRosterModal` triggered by the agents chip row (lines 752-756 in `ChatView.vue`) becomes redundant for channel views. Keep the modal for now but change the chip row click behavior:

- In channel mode: clicking the agent chips row **opens/closes the member panel** instead of the modal
- In DM mode: chips row keeps its current behavior (opens the modal, since there's no sidebar in DM view)

This reduces the number of ways to see member info from two (chips → modal, sidebar) to one per mode.

**Thread panel coexistence:** `ChatView.vue` already has a right-side thread panel (`threadPanelOpen`). The member panel is also on the right. When both would be open simultaneously, the member panel yields: if `threadPanelOpen` becomes `true`, the member panel collapses. When the thread panel closes, the member panel restores to its previous state. This keeps the layout from getting crowded. Implementation: `watch(threadPanelOpen, open => { if (open) storedPanelOpen = memberPanelOpen.value; memberPanelOpen.value = false; else memberPanelOpen.value = storedPanelOpen })`.

---

## File Changes

| File | Change |
|------|--------|
| `web/src/composables/useReplicationStatus.ts` | New composable: polling logic, chip text/class computation |
| `web/src/views/ChatView.vue` | Add `spaceMemberCards` computed (replaces `spaceAgents` for panel); collapsible member panel integration; chip row click routing (channel → panel, DM → modal); thread panel coexistence watch; pass `agentDescription` to `AgentMessageHeader` |
| `web/src/components/AgentMessageHeader.vue` | Add `agentDescription` prop; render as tooltip on agent name |
| `web/src/components/ChannelMemberPanel.vue` | New component: collapsible member sidebar |
| `web/src/composables/useApi.ts` | Add `description?: string` to `Agent` interface (field exists on backend but missing from TS type) |

No backend changes. No DB migrations.

---

## Tests

**Frontend (Vitest):**
- `useReplicationStatus polls every 30s and stops on scope dispose`
- `useReplicationStatus starts polling when spaceId changes from null to a value`
- `useReplicationStatus stops polling and clears chip when spaceId changes to null`
- `useReplicationStatus swallows fetch errors silently (chip stays hidden)`
- `chipText is "Memory syncing…" when pending > 0`
- `chipText is "Memory synced · N agents" when pending/failed/dead all 0 and connected`
- `chipText is empty when not connected`
- `chipClass includes amber styles when failed > 0 or dead > 0`
- `ChannelMemberPanel renders lead badge on lead agent, not on members`
- `memberPanelOpen state persists in localStorage by space ID`

---

## Success Criteria

- Opening a channel shows agent chips in the header; clicking them opens/closes the member panel (not modal)
- Member panel shows each agent's name, Lead/Member badge, description, and vault name (if configured)
- Member panel open/collapsed state is remembered per channel across page reloads
- Hovering an agent name in the message stream shows their description in a tooltip
- When memory replication has been active in the last 10 minutes, the chip is visible in the channel header
- When `pending > 0`, chip shows "Memory syncing…" with pulsing indicator
- `npm run build` clean; no TypeScript errors
