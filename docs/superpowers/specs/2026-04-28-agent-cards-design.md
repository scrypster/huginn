# Agent Cards — Design Spec

**Date:** 2026-04-28
**Status:** Draft
**Phase:** 3 — Desktop UX Polish

---

## Problem

When a user navigates to `/agents` without selecting a specific agent, they see a blank state: a robot icon and "Select an agent" with a "+ New agent" button. This tells the user nothing about their agents — their descriptions, whether heartbeats are running, whether memory is connected. It feels like an empty filing cabinet, not a team of active collaborators.

The sidebar agent list is a compact navigation aid (color dot + name), not a showcase. It doesn't surface descriptions, heartbeat state, or memory status.

---

## Scope

**In scope:**
- Replace the `/agents` blank state with a card grid showing all agents
- Each card: avatar, name, description, heartbeat badge, memory badge
- Card click: navigate to DM with that agent (not the agent editor)
- Card "Edit" button: navigate to the agent editor as before
- Update `AgentSummary` interface in `useAgents.ts` to add `description`, `heartbeat_enabled`, and `vault_name` as explicit typed fields

**Out of scope:**
- Replacing the sidebar list with cards (sidebar is too narrow; compact list stays)
- "Last active" timestamp (requires new backend query — defer to Phase 4)
- Card-to-card drag-to-reorder (YAGNI)
- Agent status indicators (idle/running/scheduled) — requires WS subscription to all agent sessions simultaneously

---

## Architecture

### Card Grid in AgentsView

The current blank state in `AgentsView.vue` (`v-if="!agentName"` block, approximately lines 4-22) is replaced with a card grid when agents exist.

**Layout:**
```html
<div v-if="!agentName" class="flex-1 overflow-y-auto p-6">
  <!-- Empty state (no agents; guard with !loading to prevent flash while App.vue fetches) -->
  <div v-if="agents.length === 0 && !loading" class="flex flex-col items-center justify-center h-full gap-5 pb-16">
    <!-- existing empty state -->
  </div>

  <!-- Card grid -->
  <div v-else class="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-4">
    <AgentCard
      v-for="agent in agents"
      :key="agent.name"
      :agent="agent"
      @click="openDM(agent)"
      @edit="router.push('/agents/' + agent.name)"
    />
  </div>

  <!-- New agent button below grid -->
  <div class="mt-6 flex justify-center">
    <button @click="router.push('/agents/new')" ...>+ New agent</button>
  </div>
</div>
```

The `agents` list is already fetched in `AgentsView.vue` via `useAgents()` (same composable used by `App.vue`).

---

### AgentCard Component

New file: `web/src/components/AgentCard.vue`

**Props:** `agent: AgentSummary` (imported from `useAgents.ts`)
**Emits:** `click`, `edit`

```html
<template>
  <div
    @click="$emit('click')"
    class="relative flex flex-col gap-3 p-4 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] hover:bg-[var(--color-hover)] cursor-pointer transition-colors group"
  >
    <!-- Edit button (top-right, shows on hover) -->
    <button
      @click.stop="$emit('edit')"
      class="absolute top-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity text-[10px] px-2 py-1 rounded bg-[var(--color-tag-bg)] text-[var(--color-text-muted)] hover:text-[var(--color-text)]"
    >Edit</button>

    <!-- Avatar -->
    <div
      class="w-12 h-12 rounded-xl flex items-center justify-center text-lg font-bold text-white flex-shrink-0"
      :style="{ background: agent.color || '#58a6ff' }"
    >
      {{ agent.icon || agent.name?.[0]?.toUpperCase() }}
    </div>

    <!-- Name + description -->
    <div class="min-w-0">
      <p class="text-sm font-semibold text-[var(--color-text)] truncate">{{ agent.name }}</p>
      <p class="text-xs text-[var(--color-text-muted)] mt-0.5 line-clamp-2 leading-relaxed">
        {{ agent.description || 'No description' }}
      </p>
    </div>

    <!-- Badge row -->
    <div class="flex items-center gap-1.5 flex-wrap">
      <!-- Heartbeat badge -->
      <span
        v-if="agent.heartbeat_enabled"
        class="text-[10px] px-1.5 py-0.5 rounded-full bg-green-500/10 text-green-400 flex items-center gap-1"
      >
        <span class="w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse" />
        Heartbeat
      </span>

      <!-- Memory badge -->
      <span
        v-if="agent.vault_name"
        class="text-[10px] px-1.5 py-0.5 rounded-full bg-[var(--color-accent)]/10 text-[var(--color-accent)]"
      >
        🧠 Memory
      </span>
      <span
        v-else
        class="text-[10px] px-1.5 py-0.5 rounded-full bg-[var(--color-border)] text-[var(--color-text-muted)]"
      >
        No memory
      </span>
    </div>
  </div>
</template>
```

---

### DM Navigation

`openDM(agent: AgentSummary)` in `AgentsView.vue`:

```ts
async function openDM(agent: AgentSummary) {
  try {
    const space = await apiFetch<{ id: string }>(`/api/v1/spaces/dm/${encodeURIComponent(agent.name)}`)
    router.push(`/space/${space.id}`)
  } catch {
    // Fallback: open agent editor if DM creation fails
    router.push(`/agents/${agent.name}`)
  }
}
```

`GET /api/v1/spaces/dm/{agent}` already exists and is idempotent — creates the DM space on first call, returns the existing one thereafter.

---

### Agent Interface Updates

The agents list composable (`useAgents.ts`) uses `AgentSummary`, not the `Agent` type from `useApi.ts`. `AgentCard` receives an `AgentSummary`. Add explicit fields to `AgentSummary` in `web/src/composables/useAgents.ts`:

```ts
export interface AgentSummary {
  name: string
  color: string
  icon: string
  model: string
  is_default?: boolean
  description?: string          // add: was flowing through index signature as unknown
  heartbeat_enabled?: boolean   // add: was flowing through index signature as unknown
  vault_name?: string           // add: already present on backend, needed for memory badge
  [key: string]: unknown
}
```

`AgentsView.vue` currently destructures `{ updateAgent, removeFromList, fetchAgents }` from `useAgents()` but NOT `agents`. Also destructure `agents` and `loading`:

```ts
const { agents, loading, updateAgent, removeAgent: removeFromList, fetchAgents } = useAgents()
```

Note: `agents` is a module-level singleton populated by `App.vue` during WS initialization — it is already populated by the time `AgentsView` renders in normal usage. The `loading` guard prevents the empty state from flashing before the initial fetch completes.

`agents` is already populated by `fetchAgents()` called in `onMounted`. The card grid uses the same list.

All three fields are already serialized by the backend. This is a TypeScript type fix only.

---

## File Changes

| File | Change |
|------|--------|
| `web/src/composables/useAgents.ts` | Add `description?: string`, `heartbeat_enabled?: boolean`, `vault_name?: string` to `AgentSummary` interface |
| `web/src/views/AgentsView.vue` | Replace blank state with card grid; add `openDM()` function |
| `web/src/components/AgentCard.vue` | New component: agent card (avatar, name, description, badges, edit button) |

No backend changes. No DB migrations.

---

## Tests

**Frontend (Vitest):**
- `AgentCard renders agent name, description, and avatar initial`
- `AgentCard shows heartbeat badge when heartbeat_enabled is true`
- `AgentCard hides heartbeat badge when heartbeat_enabled is false/undefined`
- `AgentCard shows memory badge when vault_name is set, "No memory" when not`
- `AgentCard emits "edit" on edit button click without bubbling to card click`
- `AgentsView renders card grid when agents.length > 0`
- `AgentsView shows blank state when agents.length === 0`
- `openDM navigates to /space/{id} on successful GET /api/v1/spaces/dm/{agent}`
- `openDM falls back to /agents/{name} if DM fetch fails`

---

## Success Criteria

- Navigating to `/agents` with agents configured shows the card grid (not blank state)
- Each card shows avatar, name, description (or "No description"), heartbeat badge (if enabled), and memory badge
- Clicking a card navigates to the DM with that agent, creating the DM space if it doesn't exist
- The "Edit" button on hover navigates to the agent editor
- Navigating to `/agents` with no agents configured still shows the empty state
- All tests pass; `npm run build` clean; no TypeScript errors on `description` or `heartbeat_enabled`
