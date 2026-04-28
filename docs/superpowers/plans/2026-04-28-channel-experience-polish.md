# Channel Experience Polish — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a collapsible member sidebar to channel views, agent description tooltips on message headers, and a polling memory-replication status chip in the channel header.

**Architecture:** Five changes. `useApi.ts` adds `description?: string` to `Agent`. `AgentMessageHeader.vue` adds an optional `agentDescription` prop rendered as a native `title` tooltip. A new `useReplicationStatus.ts` composable polls `GET /api/v1/memory/replication-status` reactively. A new `ChannelMemberPanel.vue` renders the collapsible sidebar. `ChatView.vue` wires everything: `spaceMemberCards` computed, panel open/close with localStorage persistence, thread-panel coexistence watch, chip-row click routing, and the replication chip.

**Tech Stack:** Vue 3, Vitest, localStorage.

**Prerequisites:** Phase 2 (PR #63) must be merged — provides `GET /api/v1/memory/replication-status`.

---

## File Changes

| File | Change |
|------|--------|
| `web/src/composables/useApi.ts` | Add `description?: string` to `Agent` interface |
| `web/src/components/AgentMessageHeader.vue` | Add `agentDescription?: string` prop; render as `title` on name span |
| `web/src/composables/useReplicationStatus.ts` | New: polling composable returning `chipText` and `chipClass` |
| `web/src/components/ChannelMemberPanel.vue` | New: collapsible member sidebar |
| `web/src/views/ChatView.vue` | `spaceMemberCards`, panel state + coexistence watch, chip routing, replication chip, `agentDescription` prop pass-through |

---

### Task 1: Add `description` to `Agent` Interface

**Files:**
- Modify: `web/src/composables/useApi.ts`

Current `Agent` interface (lines 42–59) — `description` is missing.

- [ ] **Step 1: Add the field**

In `web/src/composables/useApi.ts`, add `description?: string` to the `Agent` interface:

```ts
export interface Agent {
  name: string
  model: string
  system_prompt: string
  color: string
  icon: string
  memory_type?: string
  memory_enabled?: boolean
  context_notes_enabled?: boolean
  vault_name?: string
  memory_mode?: string
  vault_description?: string
  description?: string          // one-line description shown in member panels and tooltips
  toolbelt?: ToolbeltEntry[]
  local_tools?: string[]
  skills?: unknown[]
  is_default?: boolean
  [key: string]: unknown
}
```

- [ ] **Step 2: Build check**

```bash
cd web && npm run build 2>&1 | grep "error TS" | head -5
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/composables/useApi.ts
git commit -m "feat(ui): add description field to Agent interface"
```

---

### Task 2: Agent Description Tooltip in `AgentMessageHeader.vue`

**Files:**
- Modify: `web/src/components/AgentMessageHeader.vue`

Current props (line ~26–29):
```ts
const props = defineProps<{
  agentName: string
  createdAt?: string
}>()
```

Current agent name span (line ~9):
```html
<span class="text-xs font-semibold" :style="`color:${color}`">{{ agentName }}</span>
```

- [ ] **Step 1: Add prop and title attribute**

Update `AgentMessageHeader.vue`:

```vue
<template>
  <div class="flex items-center gap-1.5 mb-1">
    <!-- Agent initial chip -->
    <span
      class="w-4 h-4 rounded text-[10px] font-bold flex items-center justify-center flex-shrink-0 select-none"
      :style="`background:${color}22;color:${color}`"
    >{{ initial }}</span>
    <!-- Agent name — title shows description on hover when available -->
    <span
      class="text-xs font-semibold"
      :style="`color:${color}`"
      :title="agentDescription || undefined"
    >{{ agentName }}</span>
    <!-- Timestamp -->
    <span class="text-[11px] text-huginn-muted/60">{{ formattedTime }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const PALETTE = ['#58A6FF', '#3FB950', '#FF7B72', '#D2A8FF', '#FFA657', '#79C0FF']

function agentColor(name: string): string {
  let h = 0
  for (const c of name) h = (Math.imul(31, h) + c.charCodeAt(0)) | 0
  return PALETTE[Math.abs(h) % PALETTE.length]!
}

const props = defineProps<{
  agentName: string
  createdAt?: string
  agentDescription?: string
}>()

const color = computed(() => agentColor(props.agentName))

const initial = computed(() => (props.agentName?.[0] ?? '?').toUpperCase())

const formattedTime = computed(() => {
  if (!props.createdAt) return 'just now'
  const d = new Date(props.createdAt)
  if (isNaN(d.getTime())) return 'just now'
  const now = Date.now()
  const diffMs = now - d.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60) return 'just now'
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  return d.toLocaleDateString()
})
</script>
```

- [ ] **Step 2: Build check**

```bash
cd web && npm run build 2>&1 | grep "error TS" | head -5
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/AgentMessageHeader.vue
git commit -m "feat(ui): add agentDescription tooltip prop to AgentMessageHeader"
```

---

### Task 3: `useReplicationStatus` Composable

**Files:**
- Create: `web/src/composables/useReplicationStatus.ts`
- Create: `web/src/composables/__tests__/useReplicationStatus.test.ts`

The API endpoint returns: `{ pending: number, failed: number, dead: number, connected: boolean }`.

- [ ] **Step 1: Write the failing tests**

Create `web/src/composables/__tests__/useReplicationStatus.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'

// Mock apiFetch so tests don't need a real server
vi.mock('../useApi', () => ({
  apiFetch: vi.fn(),
  setToken: vi.fn(),
}))

import { apiFetch } from '../useApi'

async function fresh() {
  vi.resetModules()
  // Re-apply the mock after module reset
  vi.mock('../useApi', () => ({ apiFetch: vi.fn(), setToken: vi.fn() }))
  const mod = await import('../useReplicationStatus')
  return mod.useReplicationStatus
}

function mockFetch(response: unknown) {
  vi.mocked(apiFetch).mockResolvedValue(response)
}

describe('useReplicationStatus', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })

  it('chipText is empty string initially (before first poll)', async () => {
    const useReplicationStatus = await fresh()
    const spaceId = ref<string | undefined>(undefined)
    const { chipText } = useReplicationStatus(spaceId)
    expect(chipText.value).toBe('')
  })

  it('chipText is empty when not connected', async () => {
    mockFetch({ pending: 0, failed: 0, dead: 0, connected: false })
    const useReplicationStatus = await fresh()
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.runAllTimersAsync()
    await Promise.resolve()
    expect(chipText.value).toBe('')
  })

  it('chipText shows syncing when pending > 0', async () => {
    mockFetch({ pending: 3, failed: 0, dead: 0, connected: true })
    const useReplicationStatus = await fresh()
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.runAllTimersAsync()
    await Promise.resolve()
    expect(chipText.value).toContain('syncing')
  })

  it('chipText shows synced when pending=0, failed=0, dead=0, connected=true', async () => {
    mockFetch({ pending: 0, failed: 0, dead: 0, connected: true })
    const useReplicationStatus = await fresh()
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.runAllTimersAsync()
    await Promise.resolve()
    expect(chipText.value).toContain('synced')
  })

  it('chipText shows issues when failed > 0', async () => {
    mockFetch({ pending: 0, failed: 2, dead: 0, connected: true })
    const useReplicationStatus = await fresh()
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.runAllTimersAsync()
    await Promise.resolve()
    expect(chipText.value).toContain('issues')
  })

  it('swallows fetch errors silently — chip stays empty', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network fail'))
    const useReplicationStatus = await fresh()
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.runAllTimersAsync()
    await Promise.resolve()
    expect(chipText.value).toBe('')
  })

  it('clears chip when spaceId changes to undefined', async () => {
    mockFetch({ pending: 0, failed: 0, dead: 0, connected: true })
    const useReplicationStatus = await fresh()
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.runAllTimersAsync()
    await Promise.resolve()
    spaceId.value = undefined
    await Promise.resolve()
    expect(chipText.value).toBe('')
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/composables/__tests__/useReplicationStatus.test.ts 2>&1 | tail -15
```
Expected: FAIL — module not found.

- [ ] **Step 3: Create the composable**

Create `web/src/composables/useReplicationStatus.ts`:

```ts
import { ref, computed, watch, onScopeDispose } from 'vue'
import type { Ref } from 'vue'
import { apiFetch } from './useApi'

interface ReplicationStatus {
  pending: number
  failed: number
  dead: number
  connected: boolean
}

export function useReplicationStatus(spaceId: Ref<string | undefined>) {
  const status = ref<ReplicationStatus | null>(null)
  let timer: ReturnType<typeof setInterval> | null = null

  async function poll() {
    try {
      status.value = await apiFetch<ReplicationStatus>('/api/v1/memory/replication-status')
    } catch { /* swallow — chip stays hidden */ }
  }

  watch(spaceId, (id) => {
    if (timer) { clearInterval(timer); timer = null }
    if (id) {
      poll()                                    // immediate first fetch
      timer = setInterval(poll, 30_000)
    } else {
      status.value = null                       // clear chip when leaving channel
    }
  }, { immediate: true })

  onScopeDispose(() => { if (timer) clearInterval(timer) })

  const chipText = computed<string>(() => {
    const s = status.value
    if (!s || !s.connected) return ''
    if (s.pending > 0) return '🧠 Memory syncing…'
    if (s.failed > 0 || s.dead > 0) return '🧠 Memory sync issues'
    return '🧠 Memory synced'
  })

  const chipClass = computed<string>(() => {
    const s = status.value
    if (!s) return ''
    if (s.failed > 0 || s.dead > 0) return 'text-amber-400 bg-amber-400/10'
    if (s.pending > 0) return 'text-huginn-blue bg-huginn-blue/10 animate-pulse'
    return 'text-huginn-muted bg-huginn-surface/60'
  })

  return { chipText, chipClass }
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/composables/__tests__/useReplicationStatus.test.ts
```
Expected: all 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useReplicationStatus.ts web/src/composables/__tests__/useReplicationStatus.test.ts
git commit -m "feat(ui): add useReplicationStatus composable with polling"
```

---

### Task 4: `ChannelMemberPanel.vue`

**Files:**
- Create: `web/src/components/ChannelMemberPanel.vue`

- [ ] **Step 1: Write the failing test**

Create `web/src/components/__tests__/ChannelMemberPanel.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ChannelMemberPanel from '../ChannelMemberPanel.vue'

interface SpaceMemberCard {
  name: string
  description: string
  vaultName: string
  isLead: boolean
  color: string
}

function makeMembers(): SpaceMemberCard[] {
  return [
    { name: 'Alice', description: 'Lead agent', vaultName: 'alice-vault', isLead: true, color: '#58a6ff' },
    { name: 'Bob',   description: 'Helper',      vaultName: '',            isLead: false, color: '#3fb950' },
  ]
}

describe('ChannelMemberPanel', () => {
  it('renders Lead badge on the lead agent', () => {
    const wrapper = mount(ChannelMemberPanel, {
      props: { members: makeMembers(), open: true },
    })
    const text = wrapper.text()
    expect(text).toContain('Lead')
    // Only one Lead badge
    const leads = wrapper.findAll('[data-testid="lead-badge"]')
    expect(leads).toHaveLength(1)
  })

  it('does not show Lead badge on non-lead members', () => {
    const wrapper = mount(ChannelMemberPanel, {
      props: { members: makeMembers(), open: true },
    })
    // Bob should not have a lead badge
    expect(wrapper.text()).not.toMatch(/Bob.*Lead|Lead.*Bob/)
  })

  it('shows vault name when vaultName is set', () => {
    const wrapper = mount(ChannelMemberPanel, {
      props: { members: makeMembers(), open: true },
    })
    expect(wrapper.text()).toContain('alice-vault')
  })

  it('emits toggle when chevron button is clicked', async () => {
    const wrapper = mount(ChannelMemberPanel, {
      props: { members: makeMembers(), open: true },
    })
    await wrapper.find('[data-testid="panel-toggle"]').trigger('click')
    expect(wrapper.emitted('toggle')).toHaveLength(1)
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/components/__tests__/ChannelMemberPanel.test.ts 2>&1 | tail -10
```
Expected: FAIL.

- [ ] **Step 3: Create `ChannelMemberPanel.vue`**

Create `web/src/components/ChannelMemberPanel.vue`:

```vue
<template>
  <div class="flex flex-shrink-0 transition-all duration-200"
       :style="open ? 'width:220px' : 'width:28px'">
    <!-- Toggle button (chevron on left edge of panel) -->
    <button
      data-testid="panel-toggle"
      @click="$emit('toggle')"
      class="flex-shrink-0 w-7 flex flex-col items-center justify-center gap-1 py-3 hover:bg-huginn-surface/60 transition-colors"
      :title="open ? 'Collapse member panel' : 'Expand member panel'"
    >
      <svg class="w-3 h-3 text-huginn-muted transition-transform" :class="open ? 'rotate-0' : 'rotate-180'"
           viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <polyline points="15 18 9 12 15 6" />
      </svg>
    </button>

    <!-- Panel content — only rendered when open -->
    <div v-if="open" class="flex-1 overflow-y-auto border-l border-huginn-border">
      <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-widest px-3 pt-3 pb-2">Members</p>

      <div v-for="member in members" :key="member.name" class="flex items-start gap-2 px-3 py-2">
        <!-- Avatar initial -->
        <div
          class="w-6 h-6 rounded-full flex-shrink-0 flex items-center justify-center text-[10px] font-bold text-white select-none"
          :style="{ background: member.color }"
        >{{ member.name[0]?.toUpperCase() }}</div>

        <!-- Info -->
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1">
            <span class="text-xs font-medium truncate" style="color:var(--color-text,#e6edf3)">{{ member.name }}</span>
            <span
              v-if="member.isLead"
              data-testid="lead-badge"
              class="text-[9px] px-1 py-0.5 rounded flex-shrink-0"
              style="background:rgba(88,166,255,0.1);color:#58a6ff"
            >Lead</span>
          </div>
          <p class="text-[11px] mt-0.5 leading-snug"
             style="color:#8b949e;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden">
            {{ member.description || 'No description' }}
          </p>
          <p v-if="member.vaultName" class="text-[10px] mt-0.5 truncate" style="color:#8b949e">
            🧠 {{ member.vaultName }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
interface SpaceMemberCard {
  name: string
  description: string
  vaultName: string
  isLead: boolean
  color: string
}

defineProps<{
  members: SpaceMemberCard[]
  open: boolean
}>()

defineEmits<{ (e: 'toggle'): void }>()
</script>
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/components/__tests__/ChannelMemberPanel.test.ts
```
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ChannelMemberPanel.vue web/src/components/__tests__/ChannelMemberPanel.test.ts
git commit -m "feat(ui): add ChannelMemberPanel collapsible sidebar component"
```

---

### Task 5: Wire Everything into `ChatView.vue`

**Files:**
- Modify: `web/src/views/ChatView.vue`

This is the integration task. Five sub-steps, each testable by build.

- [ ] **Step 1: Add imports**

After the existing composable imports, add:

```ts
import { useReplicationStatus } from '../composables/useReplicationStatus'
import ChannelMemberPanel from '../components/ChannelMemberPanel.vue'
```

- [ ] **Step 2: Add `spaceMemberCards` computed and member panel state**

After the existing `spaceAgents` computed (line ~1187) and `spaceAgentPreviews` (line ~1193), add:

```ts
interface SpaceMemberCard {
  name: string
  description: string
  vaultName: string
  isLead: boolean
  color: string
}

const spaceMemberCards = computed<SpaceMemberCard[]>(() => {
  const space = activeSpace.value
  if (!space) return []
  const leadName = space.leadAgent
  const names = [leadName, ...space.memberAgents]
  return names.map(n => {
    const agent = agentsList.value.find(a => a.name === n)
    return {
      name: n,
      description: agent?.description ?? '',
      vaultName: (agent?.vault_name as string) ?? '',
      isLead: n === leadName,
      color: agentColorMap.value[n] ?? '#58a6ff',
    }
  })
})
```

Add member panel state (after `rosterOpen` and `threadPanelOpen` declarations, ~lines 998 and 1014):

```ts
// Member panel (channel view right sidebar)
const memberPanelOpen = ref(false)
const memberPanelStoredState = ref(false) // preserves state during thread panel override

// Initialize from localStorage keyed by spaceId
watch(() => props.spaceId, (id) => {
  if (id) {
    memberPanelOpen.value = localStorage.getItem(`huginn:memberPanel:${id}`) === 'true'
  }
}, { immediate: true })

function toggleMemberPanel() {
  if (!props.spaceId) return
  memberPanelOpen.value = !memberPanelOpen.value
  localStorage.setItem(`huginn:memberPanel:${props.spaceId}`, String(memberPanelOpen.value))
}
```

- [ ] **Step 3: Thread panel coexistence watch**

After the `memberPanelOpen` state, add:

```ts
// When thread panel opens, collapse member panel; restore when it closes.
watch(threadPanelOpen, (open) => {
  if (open) {
    memberPanelStoredState.value = memberPanelOpen.value
    memberPanelOpen.value = false
  } else {
    memberPanelOpen.value = memberPanelStoredState.value
  }
})
```

- [ ] **Step 4: Replication status chip**

After the `spaceMemberCards` computed, set up the replication composable:

```ts
const spaceIdRef = computed(() => props.spaceId)
const { chipText: replChipText, chipClass: replChipClass } = useReplicationStatus(spaceIdRef)
```

- [ ] **Step 5: Pass `agentDescription` to `AgentMessageHeader`**

Find where `AgentMessageHeader` is used in the template (search for `<AgentMessageHeader`). It currently receives `:agent-name` and `:created-at`. Add the description prop:

```html
<AgentMessageHeader
  :agent-name="msg.agent ?? ''"
  :created-at="msg.createdAt"
  :agent-description="msg.agent ? (agentsList.value.find(a => a.name === msg.agent)?.description as string | undefined) : undefined"
/>
```

- [ ] **Step 6: Update agent chips click handler**

Find the agents chip button in the template (line ~162, `@click="rosterOpen = true"`). Change it to route to member panel in channel mode:

```html
<button v-if="activeSpace"
  @click="activeSpace ? toggleMemberPanel() : (rosterOpen = true)"
  ...
>
```

Since `activeSpace` is already the `v-if` condition, simplify to:
```html
<button v-if="activeSpace"
  @click="toggleMemberPanel()"
  ...
>
```

- [ ] **Step 7: Add replication chip to channel header**

Find the channel header area (near the agents chip button, ~line 162). After the agents button and before any roster button, add:

```html
<span v-if="replChipText" :class="['text-[10px] px-2 py-0.5 rounded-full', replChipClass]">
  {{ replChipText }}
</span>
```

- [ ] **Step 8: Add `ChannelMemberPanel` to the layout**

Find the main chat layout wrapper in the template. The chat view uses a flex column or row layout. Wrap the message area + input in a flex row to accommodate the panel. Find the outermost flex container that holds the message list and add the panel alongside it:

```html
<!-- Member panel (channel view only, right side) -->
<ChannelMemberPanel
  v-if="activeSpace"
  :members="spaceMemberCards"
  :open="memberPanelOpen"
  @toggle="toggleMemberPanel"
/>
```

Place this immediately after the message list + input container, inside the same flex row container.

- [ ] **Step 9: Build check**

```bash
cd web && npm run build 2>&1 | grep -E "error TS|Error" | head -15
```
Expected: no TypeScript errors.

- [ ] **Step 10: Run all new frontend tests**

```bash
cd web && npx vitest run \
  src/composables/__tests__/useReplicationStatus.test.ts \
  src/components/__tests__/ChannelMemberPanel.test.ts
```
Expected: all tests PASS.

- [ ] **Step 11: Commit**

```bash
git add web/src/views/ChatView.vue
git commit -m "feat(ui): channel polish — member panel, description tooltips, replication chip"
```
