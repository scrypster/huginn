# Agent Cards — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the blank `/agents` state with a card grid showing all agents, each with avatar, name, description, heartbeat badge, and memory badge. Clicking a card navigates to the agent's DM.

**Architecture:** Three changes. `AgentSummary` in `useAgents.ts` gains three explicit fields (`description`, `heartbeat_enabled`, `vault_name`). A new `AgentCard.vue` component renders one card. `AgentsView.vue` replaces its blank-state div with a card grid and adds an `openDM` function.

**Tech Stack:** Vue 3, Vitest, Tailwind CSS.

---

## File Changes

| File | Change |
|------|--------|
| `web/src/composables/useAgents.ts` | Add `description?`, `heartbeat_enabled?`, `vault_name?` to `AgentSummary` interface |
| `web/src/components/AgentCard.vue` | New component: card with avatar, name, description, heartbeat + memory badges, Edit button |
| `web/src/views/AgentsView.vue` | Replace blank state with card grid; add `openDM()` and `loading` destructure |

---

### Task 1: Update `AgentSummary` Interface

**Files:**
- Modify: `web/src/composables/useAgents.ts`

Current `AgentSummary` (lines 4–11):
```ts
export interface AgentSummary {
  name: string
  color: string
  icon: string
  model: string
  is_default?: boolean
  [key: string]: unknown
}
```

- [ ] **Step 1: Update the interface**

Replace the `AgentSummary` interface with:

```ts
export interface AgentSummary {
  name: string
  color: string
  icon: string
  model: string
  is_default?: boolean
  description?: string          // agent's one-line description
  heartbeat_enabled?: boolean   // whether a heartbeat cron is active
  vault_name?: string           // MuninnDB vault name if memory is configured
  [key: string]: unknown
}
```

- [ ] **Step 2: Build check**

```bash
cd web && npm run build 2>&1 | grep -E "error TS|Error" | head -10
```
Expected: no TypeScript errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/composables/useAgents.ts
git commit -m "feat(ui): add description, heartbeat_enabled, vault_name to AgentSummary"
```

---

### Task 2: Create `AgentCard.vue`

**Files:**
- Create: `web/src/components/AgentCard.vue`
- Create: `web/src/components/__tests__/AgentCard.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/__tests__/AgentCard.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentCard from '../AgentCard.vue'
import type { AgentSummary } from '../../composables/useAgents'

function makeAgent(overrides: Partial<AgentSummary> = {}): AgentSummary {
  return {
    name: 'TestAgent',
    color: '#58a6ff',
    icon: 'T',
    model: 'claude-3',
    ...overrides,
  }
}

describe('AgentCard', () => {
  it('renders agent name', () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    expect(wrapper.text()).toContain('TestAgent')
  })

  it('renders description when provided', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ description: 'My helpful agent' }) },
    })
    expect(wrapper.text()).toContain('My helpful agent')
  })

  it('shows "No description" when description is absent', () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    expect(wrapper.text()).toContain('No description')
  })

  it('shows heartbeat badge when heartbeat_enabled is true', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ heartbeat_enabled: true }) },
    })
    expect(wrapper.text()).toContain('Heartbeat')
  })

  it('hides heartbeat badge when heartbeat_enabled is false', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ heartbeat_enabled: false }) },
    })
    expect(wrapper.text()).not.toContain('Heartbeat')
  })

  it('shows memory badge when vault_name is set', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ vault_name: 'my-vault' }) },
    })
    expect(wrapper.text()).toContain('Memory')
    expect(wrapper.text()).not.toContain('No memory')
  })

  it('shows "No memory" when vault_name is absent', () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    expect(wrapper.text()).toContain('No memory')
  })

  it('emits "edit" on edit button click without bubbling to card click', async () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    const editBtn = wrapper.find('[data-testid="agent-card-edit"]')
    await editBtn.trigger('click')
    expect(wrapper.emitted('edit')).toHaveLength(1)
    expect(wrapper.emitted('click')).toBeFalsy()
  })

  it('emits "click" when card body is clicked', async () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    await wrapper.find('[data-testid="agent-card"]').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/components/__tests__/AgentCard.test.ts 2>&1 | tail -15
```
Expected: FAIL — component not found.

- [ ] **Step 3: Create `AgentCard.vue`**

Create `web/src/components/AgentCard.vue`:

```vue
<template>
  <div
    data-testid="agent-card"
    @click="$emit('click')"
    class="relative flex flex-col gap-3 p-4 rounded-xl border border-huginn-border bg-huginn-surface hover:bg-huginn-surface/80 cursor-pointer transition-colors group"
  >
    <!-- Edit button: top-right, appears on hover, stops propagation so it doesn't trigger card click -->
    <button
      data-testid="agent-card-edit"
      @click.stop="$emit('edit')"
      class="absolute top-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity text-[10px] px-2 py-1 rounded"
      style="background:rgba(255,255,255,0.06);color:var(--color-text-muted, #8b949e)"
    >Edit</button>

    <!-- Avatar -->
    <div
      class="w-12 h-12 rounded-xl flex items-center justify-center text-lg font-bold text-white flex-shrink-0 select-none"
      :style="{ background: agent.color || '#58a6ff' }"
    >
      {{ agent.icon || (agent.name?.[0]?.toUpperCase() ?? '?') }}
    </div>

    <!-- Name + description -->
    <div class="min-w-0">
      <p class="text-sm font-semibold truncate" style="color:var(--color-text, #e6edf3)">{{ agent.name }}</p>
      <p class="text-xs mt-0.5 leading-relaxed" style="color:var(--color-text-muted, #8b949e);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden">
        {{ agent.description || 'No description' }}
      </p>
    </div>

    <!-- Badges -->
    <div class="flex items-center gap-1.5 flex-wrap">
      <!-- Heartbeat -->
      <span
        v-if="agent.heartbeat_enabled"
        class="text-[10px] px-1.5 py-0.5 rounded-full flex items-center gap-1"
        style="background:rgba(63,185,80,0.1);color:#3fb950"
      >
        <span class="w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse inline-block" />
        Heartbeat
      </span>

      <!-- Memory -->
      <span
        v-if="agent.vault_name"
        class="text-[10px] px-1.5 py-0.5 rounded-full"
        style="background:rgba(88,166,255,0.1);color:#58a6ff"
      >🧠 Memory</span>
      <span
        v-else
        class="text-[10px] px-1.5 py-0.5 rounded-full"
        style="background:rgba(255,255,255,0.06);color:#8b949e"
      >No memory</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { AgentSummary } from '../composables/useAgents'

defineProps<{ agent: AgentSummary }>()
defineEmits<{ (e: 'click'): void; (e: 'edit'): void }>()
</script>
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/components/__tests__/AgentCard.test.ts
```
Expected: all 9 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/AgentCard.vue web/src/components/__tests__/AgentCard.test.ts
git commit -m "feat(ui): add AgentCard component with heartbeat and memory badges"
```

---

### Task 3: Update `AgentsView.vue`

**Files:**
- Modify: `web/src/views/AgentsView.vue`

Current blank state (lines 4–22 of template):
```html
<div v-if="!agentName" class="flex flex-col items-center justify-center h-full gap-5 pb-16">
  <!-- robot icon, "Select an agent", "New agent" button -->
</div>
```

Current destructure (line 1267):
```ts
const { updateAgent, removeAgent: removeFromList, fetchAgents } = useAgents()
```

- [ ] **Step 1: Write the failing tests**

Create `web/src/views/__tests__/AgentsView.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'

// We need to stub useAgents so we can control the agents list.
vi.mock('../../composables/useAgents', () => {
  const { ref } = require('vue')
  const agents = ref<any[]>([])
  const loading = ref(false)
  return {
    useAgents: () => ({
      agents,
      loading,
      fetchAgents: vi.fn(),
      updateAgent: vi.fn(),
      removeAgent: vi.fn(),
      getAgentByName: vi.fn(),
      wireWS: vi.fn(),
    }),
    get _agents() { return agents },
    get _loading() { return loading },
  }
})

// Stub apiFetch used by openDM
vi.mock('../../composables/useApi', async (importOriginal) => {
  const orig = await importOriginal<any>()
  return {
    ...orig,
    apiFetch: vi.fn().mockResolvedValue({ id: 'space-123' }),
  }
})

import AgentsView from '../AgentsView.vue'
import { useAgents as _useAgents } from '../../composables/useAgents'
import { apiFetch } from '../../composables/useApi'

const router = createRouter({ history: createMemoryHistory(), routes: [
  { path: '/agents/:agentName?', component: AgentsView, props: true },
  { path: '/space/:id', component: { template: '<div />' } },
] })

describe('AgentsView', () => {
  beforeEach(() => {
    ((_useAgents() as any).agents as any).value = []
    ;((_useAgents() as any).loading as any).value = false
  })

  it('shows empty state when agents list is empty and not loading', async () => {
    const wrapper = mount(AgentsView, {
      global: { plugins: [router] },
      props: { agentName: undefined },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Select an agent')
    expect(wrapper.find('[data-testid="agent-card"]').exists()).toBe(false)
  })

  it('renders card grid when agents exist', async () => {
    ;((_useAgents() as any).agents as any).value = [
      { name: 'Alpha', color: '#ff0', icon: 'A', model: 'gpt-4' },
      { name: 'Beta',  color: '#0ff', icon: 'B', model: 'gpt-4' },
    ]
    const wrapper = mount(AgentsView, {
      global: { plugins: [router] },
      props: { agentName: undefined },
    })
    await flushPromises()
    expect(wrapper.findAll('[data-testid="agent-card"]')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('Select an agent')
  })

  it('openDM navigates to /space/:id on success', async () => {
    ;((_useAgents() as any).agents as any).value = [
      { name: 'Alpha', color: '#ff0', icon: 'A', model: 'gpt-4' },
    ]
    const wrapper = mount(AgentsView, {
      global: { plugins: [router] },
      props: { agentName: undefined },
    })
    await flushPromises()
    await wrapper.find('[data-testid="agent-card"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/space/space-123')
  })

  it('openDM falls back to /agents/:name if DM fetch fails', async () => {
    vi.mocked(apiFetch).mockRejectedValueOnce(new Error('fail'))
    ;((_useAgents() as any).agents as any).value = [
      { name: 'Alpha', color: '#ff0', icon: 'A', model: 'gpt-4' },
    ]
    const wrapper = mount(AgentsView, {
      global: { plugins: [router] },
      props: { agentName: undefined },
    })
    await flushPromises()
    await wrapper.find('[data-testid="agent-card"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/agents/Alpha')
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/views/__tests__/AgentsView.test.ts 2>&1 | tail -20
```
Expected: FAIL.

- [ ] **Step 3: Update the import and destructure in `AgentsView.vue`**

At line 1263 (the `useAgents` import):
```ts
import { useAgents } from '../composables/useAgents'
```
No change needed — already imported.

At line 1267, replace:
```ts
const { updateAgent, removeAgent: removeFromList, fetchAgents } = useAgents()
```
with:
```ts
const { agents, loading, updateAgent, removeAgent: removeFromList, fetchAgents } = useAgents()
```

Also add import for `apiFetch` near the existing imports:
```ts
import { apiFetch } from '../composables/useApi'
```

And import `AgentCard` component (add after existing component imports):
```ts
import AgentCard from '../components/AgentCard.vue'
```

- [ ] **Step 4: Add the `openDM` function**

In the script setup, after the `fetchAgents` section, add:

```ts
async function openDM(agent: AgentSummary) {
  try {
    const space = await apiFetch<{ id: string }>(`/api/v1/spaces/dm/${encodeURIComponent(agent.name)}`)
    router.push(`/space/${space.id}`)
  } catch {
    router.push(`/agents/${agent.name}`)
  }
}
```

Also add the import for `AgentSummary` type:
```ts
import { useAgents, type AgentSummary } from '../composables/useAgents'
```

- [ ] **Step 5: Replace the blank state in the template**

Replace the entire `v-if="!agentName"` div (lines 4–22 of the template):

```html
<!-- No agent selected -->
<div v-if="!agentName" class="flex-1 overflow-y-auto p-6">

  <!-- Empty state: no agents at all (guard with !loading to prevent flash) -->
  <div v-if="agents.length === 0 && !loading" class="flex flex-col items-center justify-center h-full gap-5 pb-16">
    <div class="w-16 h-16 rounded-2xl flex items-center justify-center select-none"
      style="background:rgba(88,166,255,0.08);border:1px solid rgba(88,166,255,0.2)">
      <svg class="w-8 h-8 text-huginn-blue opacity-60" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
        <circle cx="12" cy="8" r="4" /><path d="M6 21v-2a4 4 0 014-4h4a4 4 0 014 4v2" />
      </svg>
    </div>
    <div class="text-center space-y-1">
      <p class="text-huginn-text text-sm font-medium">Select an agent</p>
      <p class="text-huginn-muted text-xs">Choose from the sidebar or create a new one</p>
    </div>
    <button data-testid="new-agent-btn" @click="createNew"
      class="flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium text-huginn-blue border border-huginn-blue/30 hover:bg-huginn-blue/10 transition-all duration-150 active:scale-95">
      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
        <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
      </svg>
      New agent
    </button>
  </div>

  <!-- Card grid: agents exist -->
  <template v-else>
    <div class="grid gap-4" style="grid-template-columns:repeat(auto-fill,minmax(220px,1fr))">
      <AgentCard
        v-for="agent in agents"
        :key="agent.name"
        :agent="agent"
        @click="openDM(agent)"
        @edit="router.push('/agents/' + agent.name)"
      />
    </div>

    <div class="mt-6 flex justify-center">
      <button @click="createNew"
        class="flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium text-huginn-blue border border-huginn-blue/30 hover:bg-huginn-blue/10 transition-all duration-150 active:scale-95">
        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
          <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
        </svg>
        New agent
      </button>
    </div>
  </template>

</div>
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
cd web && npx vitest run src/views/__tests__/AgentsView.test.ts
```
Expected: all 4 tests PASS.

- [ ] **Step 7: Build check**

```bash
cd web && npm run build 2>&1 | grep -E "error TS|Error" | head -10
```
Expected: no TypeScript errors.

- [ ] **Step 8: Commit**

```bash
git add web/src/views/AgentsView.vue web/src/views/__tests__/AgentsView.test.ts
git commit -m "feat(ui): replace agents blank state with card grid; add openDM navigation"
```
