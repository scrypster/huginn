# DM Experience Polish — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Polish DM conversations with a thinking bubble, seen indicator, user message timestamps, and a hover action bar (copy, retry, save to memory).

**Architecture:** One backend change: add `muninn_remember` to `allowedMuninnTools`. Two frontend additions: `agentThinking`/`lastSeenMessageId` state in `useSessions.ts` drives the bubble and seen indicator in `ChatView.vue`; a new `MessageActions.vue` component provides the hover action bar. `ChatView.vue` wires WS events to set the new state and renders the new UI.

**Tech Stack:** Vue 3, Go, Vitest.

**Prerequisites:** Phase 2 (PR #63 — `POST /api/v1/muninn/tool` endpoint) must be merged to the target branch.

---

## File Changes

| File | Change |
|------|--------|
| `internal/server/handlers_memory.go` | Add `"muninn_remember": true` to `allowedMuninnTools` |
| `web/src/composables/useSessions.ts` | Add `agentThinking` and `lastSeenMessageId` to session state; expose setters |
| `web/src/components/MessageActions.vue` | New component: copy, retry, save-to-memory action bar |
| `web/src/views/ChatView.vue` | Thinking bubble, seen indicator, user timestamps on hover, wire `MessageActions` |

---

### Task 1: Backend — Allow `muninn_remember` via Proxy

**Files:**
- Modify: `internal/server/handlers_memory.go`

Current `allowedMuninnTools` (lines 70–78):
```go
var allowedMuninnTools = map[string]bool{
    "muninn_recall":         true,
    "muninn_read":           true,
    "muninn_find_by_entity": true,
    "muninn_entities":       true,
    "muninn_forget":         true,
}
```

- [ ] **Step 1: Write the failing test**

Find the test file for this handler. Look for `handlers_memory_test.go`:
```bash
ls internal/server/handlers_memory_test.go
```

If it exists, append this test. If not, create it with the package declaration `package server` first.

Append to `internal/server/handlers_memory_test.go`:

```go
func TestAllowedMuninnTools_IncludesMuninnRemember(t *testing.T) {
	if !allowedMuninnTools["muninn_remember"] {
		t.Error("allowedMuninnTools must include muninn_remember for user-initiated Save to Memory")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/server/... -run TestAllowedMuninnTools -v
```
Expected: FAIL.

- [ ] **Step 3: Add `muninn_remember` to the whitelist**

In `handlers_memory.go`, update `allowedMuninnTools` and the comment:

```go
// allowedMuninnTools is the whitelist of tools the browser may call via the proxy.
// Read tools and user-initiated write tools are permitted; no agent-autonomous write tools.
// muninn_remember is allowed here for explicit user-initiated "Save to Memory" actions in the UI.
var allowedMuninnTools = map[string]bool{
    "muninn_recall":         true,
    "muninn_read":           true,
    "muninn_find_by_entity": true,
    "muninn_entities":       true,
    "muninn_forget":         true,
    "muninn_remember":       true, // user-initiated only; not called autonomously via this endpoint
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/server/... -run TestAllowedMuninnTools -v
```
Expected: PASS.

- [ ] **Step 5: Build check**

```bash
go build ./...
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers_memory.go internal/server/handlers_memory_test.go
git commit -m "feat(server): allow muninn_remember in MuninnDB proxy whitelist"
```

---

### Task 2: Session State — `agentThinking` and `lastSeenMessageId`

**Files:**
- Modify: `web/src/composables/useSessions.ts`

The module-level `messagesBySession` stores `ChatMessage[]` per session ID. We need per-session `agentThinking` and `lastSeenMessageId` state following the same pattern.

- [ ] **Step 1: Write the failing tests**

Create `web/src/composables/__tests__/useSessions.test.ts` (or append if it exists):

```ts
import { describe, it, expect, vi } from 'vitest'

async function fresh() {
  vi.resetModules()
  const mod = await import('../useSessions')
  return mod.useSessions
}

describe('useSessions — agentThinking', () => {
  it('agentThinking starts false for a new session', async () => {
    const useSessions = await fresh()
    const { getAgentThinking } = useSessions()
    expect(getAgentThinking('sess-1')).toBe(false)
  })

  it('setAgentThinking sets and clears thinking state', async () => {
    const useSessions = await fresh()
    const { setAgentThinking, getAgentThinking } = useSessions()
    setAgentThinking('sess-1', true)
    expect(getAgentThinking('sess-1')).toBe(true)
    setAgentThinking('sess-1', false)
    expect(getAgentThinking('sess-1')).toBe(false)
  })
})

describe('useSessions — lastSeenMessageId', () => {
  it('lastSeenMessageId starts null', async () => {
    const useSessions = await fresh()
    const { getLastSeenMessageId } = useSessions()
    expect(getLastSeenMessageId('sess-1')).toBeNull()
  })

  it('setLastSeenMessageId stores and returns the id', async () => {
    const useSessions = await fresh()
    const { setLastSeenMessageId, getLastSeenMessageId } = useSessions()
    setLastSeenMessageId('sess-1', 'msg-42')
    expect(getLastSeenMessageId('sess-1')).toBe('msg-42')
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/composables/__tests__/useSessions.test.ts 2>&1 | tail -15
```
Expected: FAIL — functions not defined.

- [ ] **Step 3: Add state and functions to `useSessions.ts`**

After the `messagesBySession` declaration (~line 59), add:

```ts
// agentThinking: true from message send until first token/status/done/error
const agentThinkingBySession = ref<Record<string, boolean>>({})
// lastSeenMessageId: set when agent starts streaming to mark the last user message as "seen"
const lastSeenMessageIdBySession = ref<Record<string, string | null>>({})
```

Then in the `useSessions()` function return (and implementation), add:

```ts
function getAgentThinking(sessionId: string): boolean {
  return agentThinkingBySession.value[sessionId] ?? false
}

function setAgentThinking(sessionId: string, value: boolean) {
  agentThinkingBySession.value[sessionId] = value
}

function getLastSeenMessageId(sessionId: string): string | null {
  return lastSeenMessageIdBySession.value[sessionId] ?? null
}

function setLastSeenMessageId(sessionId: string, id: string | null) {
  lastSeenMessageIdBySession.value[sessionId] = id
}
```

Add all four to the return value of `useSessions()`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/composables/__tests__/useSessions.test.ts
```
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useSessions.ts web/src/composables/__tests__/useSessions.test.ts
git commit -m "feat(ui): add agentThinking and lastSeenMessageId state to useSessions"
```

---

### Task 3: `MessageActions.vue` Component

**Files:**
- Create: `web/src/components/MessageActions.vue`
- Create: `web/src/components/__tests__/MessageActions.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/__tests__/MessageActions.test.ts`:

```ts
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MessageActions from '../MessageActions.vue'
import type { ChatMessage, ToolCallRecord } from '../../composables/useSessions'

function makeMsg(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 'msg-1',
    role: 'assistant',
    content: 'Hello world',
    ...overrides,
  }
}

describe('MessageActions', () => {
  it('shows copy button for all messages', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg(), agentVaultName: '' },
    })
    expect(wrapper.find('[data-testid="msg-copy"]').exists()).toBe(true)
  })

  it('shows retry button for user messages', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'user' }), agentVaultName: '' },
    })
    expect(wrapper.find('[data-testid="msg-retry"]').exists()).toBe(true)
  })

  it('hides retry button for assistant messages', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant' }), agentVaultName: '' },
    })
    expect(wrapper.find('[data-testid="msg-retry"]').exists()).toBe(false)
  })

  it('hides save-to-memory when agent has no vault', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant' }), agentVaultName: '' },
    })
    expect(wrapper.find('[data-testid="msg-save-memory"]').exists()).toBe(false)
  })

  it('shows "Saved ✓" when agent already called muninn_remember', () => {
    const toolCalls: ToolCallRecord[] = [
      { id: 't1', name: 'muninn_remember', args: {}, done: true },
    ]
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant', toolCalls }), agentVaultName: 'my-vault' },
    })
    expect(wrapper.text()).toContain('Saved')
    expect(wrapper.find('[data-testid="msg-save-memory"]').exists()).toBe(false)
  })

  it('shows save button when vault set and agent did not call memory tools', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant', toolCalls: [] }), agentVaultName: 'my-vault' },
    })
    expect(wrapper.find('[data-testid="msg-save-memory"]').exists()).toBe(true)
  })

  it('emits retry with message content on retry click', async () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'user', content: 'retry me' }), agentVaultName: '' },
    })
    await wrapper.find('[data-testid="msg-retry"]').trigger('click')
    expect(wrapper.emitted('retry')?.[0]).toEqual(['retry me'])
  })

  it('emits save-memory with vault and content on save click', async () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant', content: 'remember this' }), agentVaultName: 'vault-x' },
    })
    await wrapper.find('[data-testid="msg-save-memory"]').trigger('click')
    expect(wrapper.emitted('save-memory')?.[0]).toEqual([{ vault: 'vault-x', content: 'remember this' }])
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/components/__tests__/MessageActions.test.ts 2>&1 | tail -15
```
Expected: FAIL — component not found.

- [ ] **Step 3: Create `MessageActions.vue`**

Create `web/src/components/MessageActions.vue`:

```vue
<template>
  <div class="flex items-center gap-1">
    <!-- Copy -->
    <button
      data-testid="msg-copy"
      @click="handleCopy"
      class="text-[10px] px-2 py-0.5 rounded transition-colors"
      style="background:rgba(255,255,255,0.06);color:#8b949e"
      :title="copied ? 'Copied!' : 'Copy'"
    >{{ copied ? 'Copied!' : 'Copy' }}</button>

    <!-- Retry (user messages only) -->
    <button
      v-if="msg.role === 'user'"
      data-testid="msg-retry"
      @click="$emit('retry', msg.content)"
      class="text-[10px] px-2 py-0.5 rounded transition-colors"
      style="background:rgba(255,255,255,0.06);color:#8b949e"
    >Retry</button>

    <!-- Save to Memory (assistant messages, vault configured) -->
    <template v-if="msg.role === 'assistant' && agentVaultName">
      <!-- Agent already saved — show indicator, no button -->
      <span
        v-if="agentAlreadySaved"
        class="text-[10px] px-2 py-0.5 rounded"
        style="color:#3fb950"
      >Saved ✓</span>
      <!-- User can save -->
      <button
        v-else
        data-testid="msg-save-memory"
        @click="handleSaveMemory"
        class="text-[10px] px-2 py-0.5 rounded transition-colors"
        style="background:rgba(255,255,255,0.06);color:#8b949e"
        :disabled="saving"
      >{{ saveLabel }}</button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChatMessage } from '../composables/useSessions'

const MEMORY_TOOLS = ['muninn_remember', 'muninn_decide', 'muninn_evolve']

const props = defineProps<{
  msg: ChatMessage
  agentVaultName: string
}>()

const emit = defineEmits<{
  (e: 'retry', content: string): void
  (e: 'save-memory', payload: { vault: string; content: string }): void
}>()

const copied = ref(false)
const saving = ref(false)
const saved = ref(false)

const saveLabel = computed(() => {
  if (saving.value) return 'Saving…'
  if (saved.value) return 'Saved ✓'
  return 'Save to Memory'
})

// True when the agent already called a memory tool during this turn.
const agentAlreadySaved = computed(() =>
  props.msg.toolCalls?.some(tc => MEMORY_TOOLS.includes(tc.name)) ?? false
)

async function handleCopy() {
  try {
    await navigator.clipboard.writeText(props.msg.content)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch { /* clipboard API not available */ }
}

function handleSaveMemory() {
  emit('save-memory', { vault: props.agentVaultName, content: props.msg.content })
}
</script>
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/components/__tests__/MessageActions.test.ts
```
Expected: all 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/MessageActions.vue web/src/components/__tests__/MessageActions.test.ts
git commit -m "feat(ui): add MessageActions component (copy, retry, save-to-memory)"
```

---

### Task 4: Wire Everything into `ChatView.vue`

**Files:**
- Modify: `web/src/views/ChatView.vue`

- [ ] **Step 1: Add imports**

In the imports section (~line 809–828), add:

```ts
import MessageActions from '../components/MessageActions.vue'
import { apiFetch } from '../composables/useApi'
```

- [ ] **Step 2: Destructure new session functions**

Find the existing `useSessions()` destructure (line ~914):
```ts
const { sessions, getMessages, fetchMessages, queueIfHydrating, formatSessionLabel, renameSession } = useSessions()
```

Add the new functions:
```ts
const {
  sessions, getMessages, fetchMessages, queueIfHydrating, formatSessionLabel, renameSession,
  getAgentThinking, setAgentThinking, getLastSeenMessageId, setLastSeenMessageId,
} = useSessions()
```

- [ ] **Step 3: Add `agentThinking` and `lastSeenMessageId` computed helpers**

After the `messages` computed (line ~1097), add:

```ts
const agentThinking = computed(() =>
  props.sessionId ? getAgentThinking(props.sessionId) : false
)

const lastSeenMessageId = computed(() =>
  props.sessionId ? getLastSeenMessageId(props.sessionId) : null
)
```

- [ ] **Step 4: Set `agentThinking = true` on send**

In the send function (the block ending with `ws.send({ type: 'chat', ... })` around line 1334), immediately before the `ws.send` call, add:

```ts
  if (props.sessionId) setAgentThinking(props.sessionId, true)
```

- [ ] **Step 5: Clear `agentThinking` in WS handlers**

In the `token` handler (line ~1395, the `registerWS(ws, 'token', ...)` block), at the very start of the handler body:

```ts
  if (sid) setAgentThinking(sid, false)
```

In the `status` handler (find `registerWS(ws, 'status', ...)`), add at the start:

```ts
  if (props.sessionId) setAgentThinking(props.sessionId, false)
```

In the `done` handler (line ~1445), after `streaming.value = false`:

```ts
  if (props.sessionId) setAgentThinking(props.sessionId, false)
```

In the `error` handler (find `registerWS(ws, 'error', ...)`), add:

```ts
  if (props.sessionId) setAgentThinking(props.sessionId, false)
```

- [ ] **Step 6: Set `lastSeenMessageId` in the `token` handler**

In the `token` handler, after setting `agentThinking` to false (step 5), add:

```ts
  if (sid && !getLastSeenMessageId(sid)) {
    // Mark the last user message as "seen" on first token
    const msgs = getMessages(sid)
    const lastUser = [...msgs].reverse().find(m => m.role === 'user')
    if (lastUser) setLastSeenMessageId(sid, lastUser.id)
  }
```

Clear `lastSeenMessageId` when a new user message is sent. In the send function, before `ws.send`:

```ts
  if (props.sessionId) setLastSeenMessageId(props.sessionId, null)
```

- [ ] **Step 7: Add thinking bubble and seen indicator to the template**

In the message list template, after the last rendered message in the `v-for` loop, add (after the closing `</div>` of each message item, before the list's closing `</div>`):

```html
<!-- Thinking bubble: shown while waiting for first token -->
<div v-if="agentThinking" class="flex items-end gap-2 px-4 py-2">
  <div class="flex gap-1 px-3 py-2 rounded-2xl rounded-bl-sm" style="background:rgba(255,255,255,0.06)">
    <span v-for="i in 3" :key="i"
      class="w-1.5 h-1.5 rounded-full bg-huginn-muted/60 animate-bounce"
      :style="`animation-delay:${(i-1)*150}ms`"
    />
  </div>
</div>
```

For the seen indicator, inside the `v-for` message loop, after rendering each user message (`v-else-if="msg.role === 'user'"`), add immediately after its closing `</div>`:

```html
<!-- Seen indicator: shows below the last user message the agent has "seen" -->
<p v-if="msg.id === lastSeenMessageId"
   class="text-[10px] text-right pr-3 -mt-1"
   style="color:#8b949e">
  Seen
</p>
```

- [ ] **Step 8: Add user message timestamp on hover**

Current user message template (line ~382–386):
```html
<div v-else-if="msg.role === 'user'" class="flex justify-end" :class="msg.showHeader ? 'mt-4' : 'mt-1'">
  <div class="md-content max-w-[75%] px-4 py-3 rounded-2xl rounded-tr-sm ...">
    ...
  </div>
</div>
```

Wrap the outer div with `group` class and add the timestamp:

```html
<div v-else-if="msg.role === 'user'" class="flex justify-end group" :class="msg.showHeader ? 'mt-4' : 'mt-1'">
  <div class="flex flex-col items-end gap-0.5">
    <div class="md-content max-w-[75%] px-4 py-3 rounded-2xl rounded-tr-sm text-sm text-huginn-text leading-relaxed break-words"
      style="background:rgba(88,166,255,0.12);border:1px solid rgba(88,166,255,0.22)"
      v-html="renderWithMentions(msg.content)" />
    <span class="text-[10px] opacity-0 group-hover:opacity-100 transition-opacity pr-1"
      style="color:#8b949e">
      {{ msg.createdAt ? new Date(msg.createdAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '' }}
    </span>
  </div>
</div>
```

- [ ] **Step 9: Wire `MessageActions` into both user and assistant messages**

The `MessageActions` component should appear on message hover. For user messages, wrap the existing message div with `group` (already done in step 8) and add `MessageActions` after the content:

For user messages, inside the `flex flex-col items-end gap-0.5` div, add:
```html
<MessageActions
  class="opacity-0 group-hover:opacity-100 transition-opacity"
  :msg="msg"
  :agent-vault-name="''"
  @retry="handleRetry"
/>
```

For assistant messages, find the assistant message content area (around line 389+) and add inside the message container with `group`:
```html
<MessageActions
  class="mt-1 opacity-0 group-hover:opacity-100 transition-opacity"
  :msg="msg"
  :agent-vault-name="activeAgentVaultName"
  @save-memory="handleSaveMemory"
/>
```

- [ ] **Step 10: Add `activeAgentVaultName` computed and handlers**

Add a computed for the current agent's vault name:

```ts
const activeAgentVaultName = computed(() => {
  const name = selectedAgentName.value
  if (!name) return ''
  const agent = agentsList.value.find(a => a.name === name)
  return (agent?.vault_name as string) ?? ''
})
```

Add the `handleRetry` and `handleSaveMemory` functions:

```ts
function handleRetry(content: string) {
  // Re-send the message content as a new chat message
  if (!props.sessionId || !wsRef.value) return
  const runId = `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`
  currentRunId.value = runId
  streaming.value = true
  startStreamingWatchdog()
  const msgs = getMessages(props.sessionId)
  msgs.push({ id: `u-${Date.now()}`, role: 'user', content })
  msgs.push({ id: `h-${Date.now()}`, role: 'assistant', content: '', streaming: true,
    agent: selectedAgentName.value || undefined, createdAt: new Date().toISOString() })
  setAgentThinking(props.sessionId, true)
  wsRef.value.send({ type: 'chat', content, session_id: props.sessionId, run_id: runId })
  scrollToBottom()
}

async function handleSaveMemory({ vault, content }: { vault: string; content: string }) {
  if (!vault) return
  try {
    await apiFetch('/api/v1/muninn/tool', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        vault,
        tool: 'muninn_remember',
        args: {
          concept: content.trim().slice(0, 60),
          content,
        },
      }),
    })
  } catch { /* error surface to MessageActions via emitted state if needed */ }
}
```

- [ ] **Step 11: Build check**

```bash
cd web && npm run build 2>&1 | grep -E "error TS|Error" | head -15
```
Expected: no TypeScript errors.

- [ ] **Step 12: Run all frontend tests**

```bash
cd web && npx vitest run src/composables/__tests__/useSessions.test.ts src/components/__tests__/MessageActions.test.ts
```
Expected: all tests PASS.

- [ ] **Step 13: Commit**

```bash
git add web/src/views/ChatView.vue
git commit -m "feat(ui): DM polish — thinking bubble, seen indicator, user timestamps, message actions"
```
