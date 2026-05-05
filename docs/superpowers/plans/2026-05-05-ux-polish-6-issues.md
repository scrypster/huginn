# UX Polish — 6 Issues Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Six targeted UX fixes: toolbar collapse, auto-approve notification, View Thread fallback, self-delegation label, delegated-agent thinking indicator, and agent config hot-reload.

**Architecture:** Isolated changes — 4 are Vue-only frontend fixes in ChatView/ChatToolbar, 1 adds a Go polling watcher for agent config, 1 is a composable tweak. No new API endpoints needed.

**Tech Stack:** Vue 3 (Composition API), TypeScript, Go 1.22+, TipTap editor

---

### Task 1: Collapsible Chat Toolbar

**Spec:** Issue 1 — hide formatting buttons by default behind a toggle; show only Format toggle + keyboard hint + Send by default.

**Files:**
- Modify: `web/src/components/ChatEditor/ChatToolbar.vue`

- [ ] **Step 1: Write the test**

Create `web/src/components/ChatEditor/__tests__/ChatToolbar.test.ts`:

```typescript
import { mount } from '@vue/test-utils'
import { describe, it, expect } from 'vitest'
import ChatToolbar from '../ChatToolbar.vue'
import { createEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'

function makeEditor() {
  return createEditor({ extensions: [StarterKit] })
}

describe('ChatToolbar', () => {
  it('hides formatting buttons by default', () => {
    const editor = makeEditor()
    const wrapper = mount(ChatToolbar, { props: { editor } })
    // Bold button should not be visible initially
    expect(wrapper.find('[title="Bold (⌘B)"]').isVisible()).toBe(false)
  })

  it('shows formatting buttons after toggle', async () => {
    const editor = makeEditor()
    const wrapper = mount(ChatToolbar, { props: { editor } })
    await wrapper.find('[data-testid="format-toggle"]').trigger('mousedown')
    expect(wrapper.find('[title="Bold (⌘B)"]').isVisible()).toBe(true)
  })

  it('persists expanded state in localStorage', async () => {
    localStorage.clear()
    const editor = makeEditor()
    const wrapper = mount(ChatToolbar, { props: { editor } })
    await wrapper.find('[data-testid="format-toggle"]').trigger('mousedown')
    expect(localStorage.getItem('huginn_toolbar_expanded')).toBe('true')
  })
})
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vitest run src/components/ChatEditor/__tests__/ChatToolbar.test.ts
```

Expected: 3 test failures.

- [ ] **Step 3: Implement collapsible toolbar**

Replace `web/src/components/ChatEditor/ChatToolbar.vue` with:

```vue
<template>
  <div class="flex items-center gap-0.5 px-2 py-1.5 border-t border-huginn-border/50">
    <!-- Format toggle button -->
    <button
      type="button"
      data-testid="format-toggle"
      @mousedown.prevent="toggleFormat"
      :title="expanded ? 'Hide formatting' : 'Show formatting'"
      :class="[
        'p-1.5 rounded transition-all duration-100 flex items-center justify-center',
        expanded
          ? 'bg-huginn-blue/20 text-huginn-blue'
          : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface',
      ]"
    >
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M4 7V4h16v3"/><path d="M9 20h6"/><path d="M12 4v16"/>
      </svg>
    </button>

    <!-- Formatting buttons — hidden when collapsed -->
    <template v-if="expanded">
      <ToolbarBtn :active="editor.isActive('bold')" @click="editor.chain().focus().toggleBold().run()" title="Bold (⌘B)">
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <path d="M6 4h7a4 4 0 0 1 0 8H6V4z"/><path d="M6 12h8a4 4 0 0 1 0 8H6v-8z"/>
        </svg>
      </ToolbarBtn>
      <ToolbarBtn :active="editor.isActive('italic')" @click="editor.chain().focus().toggleItalic().run()" title="Italic (⌘I)">
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="19" y1="4" x2="10" y2="4"/><line x1="14" y1="20" x2="5" y2="20"/><line x1="15" y1="4" x2="9" y2="20"/></svg>
      </ToolbarBtn>
      <ToolbarBtn :active="editor.isActive('code')" @click="editor.chain().focus().toggleCode().run()" title="Inline code (⌘E)">
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
      </ToolbarBtn>

      <div class="w-px h-3.5 bg-huginn-border mx-1 flex-shrink-0" />

      <ToolbarBtn :active="editor.isActive('bulletList')" @click="editor.chain().focus().toggleBulletList().run()" title="Bullet list">
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="9" y1="6" x2="20" y2="6"/><line x1="9" y1="12" x2="20" y2="12"/><line x1="9" y1="18" x2="20" y2="18"/><circle cx="4" cy="6" r="1" fill="currentColor"/><circle cx="4" cy="12" r="1" fill="currentColor"/><circle cx="4" cy="18" r="1" fill="currentColor"/></svg>
      </ToolbarBtn>
      <ToolbarBtn :active="editor.isActive('orderedList')" @click="editor.chain().focus().toggleOrderedList().run()" title="Numbered list">
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="10" y1="6" x2="21" y2="6"/><line x1="10" y1="12" x2="21" y2="12"/><line x1="10" y1="18" x2="21" y2="18"/><path d="M4 6h1v4"/><path d="M4 10H6"/><path d="M6 18H4c0-1 2-2 2-3s-1-1.5-2-1"/></svg>
      </ToolbarBtn>
      <ToolbarBtn :active="editor.isActive('blockquote')" @click="editor.chain().focus().toggleBlockquote().run()" title="Blockquote">
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 21c3 0 7-1 7-8V5c0-1.25-.756-2.017-2-2H4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2 1 0 1 0 1 1v1c0 1-1 2-2 2s-1 .008-1 1.031V20c0 1 0 1 1 1z"/><path d="M15 21c3 0 7-1 7-8V5c0-1.25-.757-2.017-2-2h-4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2h.75c0 2.25.25 4-2.75 4v3c0 1 0 1 1 1z"/></svg>
      </ToolbarBtn>

      <div class="w-px h-3.5 bg-huginn-border mx-1 flex-shrink-0" />

      <ToolbarBtn :active="editor.isActive('codeBlock')" @click="insertCodeBlock" title="Code block">
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/><line x1="12" y1="3" x2="12" y2="21"/></svg>
      </ToolbarBtn>
    </template>

    <span class="ml-auto text-[11px] mr-1" style="color:rgba(139,148,158,0.4)">⏎ send &nbsp;·&nbsp; ⇧⏎ newline</span>

    <!-- Send button -->
    <button
      type="button"
      @mousedown.prevent="$emit('send')"
      class="w-7 h-7 rounded-xl flex items-center justify-center text-white transition-all duration-150 hover:opacity-80 active:scale-90 flex-shrink-0"
      style="background:rgba(88,166,255,0.9)"
      title="Send (⏎)"
    >
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <line x1="22" y1="2" x2="11" y2="13" />
        <polygon points="22 2 15 22 11 13 2 9 22 2" />
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref, defineComponent, h } from 'vue'
import type { Editor } from '@tiptap/vue-3'

const props = defineProps<{ editor: Editor }>()
defineEmits<{ (e: 'send'): void }>()

const LS_KEY = 'huginn_toolbar_expanded'
const expanded = ref(localStorage.getItem(LS_KEY) === 'true')

function toggleFormat() {
  expanded.value = !expanded.value
  localStorage.setItem(LS_KEY, String(expanded.value))
}

function insertCodeBlock() {
  const { editor } = props
  if (editor.isActive('codeBlock')) {
    editor.chain().focus().toggleCodeBlock().run()
    return
  }
  const { $from } = editor.state.selection
  if ($from.parent.content.size === 0) {
    editor.chain().focus().setCodeBlock().run()
    return
  }
  const endOfNode = $from.after($from.depth)
  editor.chain().focus().insertContentAt(endOfNode, { type: 'codeBlock', content: [] }).run()
}

const ToolbarBtn = defineComponent({
  props: {
    active: { type: Boolean, default: false },
    title: { type: String, default: '' },
  },
  emits: ['click'],
  setup(p, { slots, emit }) {
    return () => h('button', {
      type: 'button',
      title: p.title,
      onMousedown: (e: MouseEvent) => { e.preventDefault(); emit('click') },
      class: [
        'p-1.5 rounded transition-all duration-100 flex items-center justify-center',
        p.active
          ? 'bg-huginn-blue/20 text-huginn-blue'
          : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface',
      ],
    }, slots.default?.())
  },
})
</script>
```

- [ ] **Step 4: Run test to confirm it passes**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vitest run src/components/ChatEditor/__tests__/ChatToolbar.test.ts
```

Expected: 3 tests passing.

- [ ] **Step 5: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add web/src/components/ChatEditor/ChatToolbar.vue web/src/components/ChatEditor/__tests__/ChatToolbar.test.ts
git commit -m "feat(ui): collapsible chat toolbar — formatting buttons hidden by default"
```

---

### Task 2: Auto-Approve Notification Toast

**Spec:** Issue 2 — when server auto-approves a delegation, show a transient chip so users know what happened. (The countdown timer is already implemented.)

**Files:**
- Modify: `web/src/views/ChatView.vue`

- [ ] **Step 1: Locate the `onDelegationPreviewAckResult` handler**

In `web/src/composables/useThreads.ts` the handler just calls `removePendingPreview`. In `ChatView.vue`, look for how WS events are consumed and where delegation preview UI lives (around line 1145 for `previewNowMs`, line 752 for the banner template).

Find where the `delegation_preview_timeout` WS event is consumed in `ChatView.vue`. The auto-approve notification belongs there — `delegation_preview_timeout` fires when the server auto-approves.

```bash
grep -n "delegation_preview_timeout\|previewNowMs\|autoApprove" web/src/views/ChatView.vue
```

- [ ] **Step 2: Add `autoApproveNotices` ref near `previewNowMs`**

Find the line `const previewNowMs = ref(Date.now())` (around line 1145) and add immediately after it:

```typescript
const autoApproveNotices = ref<{ id: string; agentName: string }[]>([])
const autoApproveTimers = new Map<string, ReturnType<typeof setTimeout>>()

function showAutoApproveNotice(agentName: string) {
  const id = `aa-${Date.now()}`
  autoApproveNotices.value.push({ id, agentName })
  const t = setTimeout(() => {
    autoApproveNotices.value = autoApproveNotices.value.filter(n => n.id !== id)
    autoApproveTimers.delete(id)
  }, 4000)
  autoApproveTimers.set(id, t)
}
```

- [ ] **Step 3: Call `showAutoApproveNotice` when `delegation_preview_timeout` fires**

In `ChatView.vue`, find where `delegation_preview_timeout` WS events are handled (it's wired via `useThreads`; the event removes the preview). The `delegation_preview_timeout` event payload has `agent_id`. In the WS event handler or by watching the removal, call `showAutoApproveNotice`.

The cleanest hook: search for `ws.on('delegation_preview_timeout'` in the file or find where the WS `delegation_preview_timeout` message is consumed. If it's only handled in `useThreads`, add a callback prop:

In `ChatView.vue`, where the WS instance is available (search for `ws.on` usage or `onDelegationPreview`), add:

```typescript
ws.on('delegation_preview_timeout', (msg: WSMessage) => {
  const p = msg.payload as Record<string, unknown>
  const agentName = typeof p.agent_id === 'string' ? p.agent_id : 'agent'
  showAutoApproveNotice(agentName)
})
```

Look for the spot in `ChatView.vue` where other WS event listeners are registered (search for `ws.on('thread_started'` or `ws.on('delegation_preview'`) to find the right location.

- [ ] **Step 4: Render the notice chips in the template**

Find the delegation preview banner section in the template (around line 752, the `v-for="preview in sessionPreviews"` block). After the last preview chip and before the closing wrapper, add:

```html
<!-- Auto-approve notices — shown briefly when server auto-approves a delegation -->
<div
  v-for="notice in autoApproveNotices"
  :key="notice.id"
  class="flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs"
  style="background:rgba(46,160,67,0.12);border:1px solid rgba(46,160,67,0.3);color:rgba(46,160,67,0.9)"
>
  <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
  Auto-approved — <span class="font-semibold ml-1">@{{ notice.agentName }}</span> took over
</div>
```

- [ ] **Step 5: Verify manually** — start the dev server, trigger a delegation with a short auto-approve timeout, observe the notice appears and fades after 4s.

- [ ] **Step 6: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add web/src/views/ChatView.vue
git commit -m "feat(ui): show transient notice when delegation is auto-approved"
```

---

### Task 3: Fix "View Thread" Button

**Spec:** Issue 3 — `openThreadDetailById` falls back to a generic panel when `parentMessageId` is missing from live state. Fix: async-fetch the thread from REST API.

**Files:**
- Modify: `web/src/views/ChatView.vue`

- [ ] **Step 1: Write the test**

In `web/src/views/__tests__/ChatView.test.ts` (or a new file), add a test that verifies `openThreadDetailById` works when the thread is NOT in live state:

```typescript
it('openThreadDetailById fetches thread when parentMessageId missing', async () => {
  // Setup: thread not in threadsBySession, mock the API
  server.use(
    http.get('/api/v1/sessions/:sessionId/threads/:threadId', () =>
      HttpResponse.json({ ID: 'thread-1', parentMessageId: 'msg-abc', AgentID: 'elena' })
    )
  )
  // ... trigger openThreadDetailById('thread-1')
  // ... assert threadDetail.open was called with 'msg-abc'
})
```

Run to confirm failure:
```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vitest run src/views/__tests__/ChatView.test.ts -t "openThreadDetailById"
```

- [ ] **Step 2: Update `openThreadDetailById` to async-fetch when needed**

Find `openThreadDetailById` in `ChatView.vue` (line ~1352) and replace:

```typescript
function openThreadDetailById(threadId: string) {
  const thread = getThreadById(threadId)
  if (thread?.parentMessageId) {
    threadPanelOpen.value = false
    openThreadLiveId.value = threadId
    threadDetail.open(thread.parentMessageId, thread.AgentID)
  } else {
    threadPanelOpen.value = true
  }
}
```

With:

```typescript
async function openThreadDetailById(threadId: string) {
  const thread = getThreadById(threadId)
  if (thread?.parentMessageId) {
    threadPanelOpen.value = false
    openThreadLiveId.value = threadId
    threadDetail.open(thread.parentMessageId, thread.AgentID)
    return
  }
  // Thread not in live state — fetch from API to get parentMessageId
  if (!props.sessionId) {
    threadPanelOpen.value = true
    return
  }
  try {
    const fetched = await api.sessions.getThread(props.sessionId, threadId)
    if (fetched?.parentMessageId) {
      threadPanelOpen.value = false
      openThreadLiveId.value = threadId
      threadDetail.open(fetched.parentMessageId, fetched.AgentID ?? '')
      return
    }
  } catch {
    // fall through to panel
  }
  threadPanelOpen.value = true
}
```

- [ ] **Step 3: Add `getThread` to the API composable**

Find `web/src/composables/useApi.ts` and look for `sessions` API object. Add:

```typescript
getThread: (sessionId: string, threadId: string) =>
  apiFetch<{ ID: string; parentMessageId?: string; AgentID?: string }>(
    `/api/v1/sessions/${sessionId}/threads/${threadId}`
  ),
```

(Find the existing `sessions` object in `useApi.ts` and add this alongside similar session methods.)

- [ ] **Step 4: Run test**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vitest run src/views/__tests__/ChatView.test.ts -t "openThreadDetailById"
```

Expected: passing.

- [ ] **Step 5: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add web/src/views/ChatView.vue web/src/composables/useApi.ts
git commit -m "fix(ui): view thread falls back to API fetch when parentMessageId missing from live state"
```

---

### Task 4: Fix Self-Delegation Label

**Spec:** Issue 4 — replace "Delegated to @elena" with "Handling directly" when `d.agentId === msg.agent`.

**Files:**
- Modify: `web/src/views/ChatView.vue`

- [ ] **Step 1: Find the delegation label template**

The delegation activity rows are at line ~560. The current label is:

```html
<span class="text-xs text-huginn-text/90 truncate">
  Delegated to
  <span class="font-semibold" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
    @{{ d.agentId || 'agent' }}
  </span>
</span>
```

This is inside a `v-for="d in msg.delegatedThreads"` loop, where `msg` is the parent assistant message.

- [ ] **Step 2: Add computed helper for delegation label**

Near the other helper functions (e.g., near `delegatedThreadStatusLabel`), add:

```typescript
function delegationLabel(d: { agentId: string }, parentAgentId?: string): string {
  if (d.agentId && d.agentId === parentAgentId) return 'Handling directly'
  return `@${d.agentId || 'agent'}`
}
```

- [ ] **Step 3: Update the template**

Replace the delegation label span with:

```html
<span class="text-xs text-huginn-text/90 truncate">
  <template v-if="d.agentId === msg.agent">Handling directly</template>
  <template v-else>
    Delegated to
    <span class="font-semibold" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
      @{{ d.agentId || 'agent' }}
    </span>
  </template>
</span>
```

Note: There are multiple delegation activity row locations in the template (lines ~416, ~451, ~545, ~585). Apply the same change to all of them. Search for `Delegated to` in the file and update each instance.

- [ ] **Step 4: Verify no test regressions**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vitest run src/views/__tests__/ChatView.test.ts
```

Expected: all existing tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add web/src/views/ChatView.vue
git commit -m "fix(ui): show 'Handling directly' instead of self-delegation label"
```

---

### Task 5: Delegated-Agent Thinking Indicator

**Spec:** Issue 5 — show a "thinking" row after each delegation activity row when the thread is running but has produced no output yet (`isRunning && streamingContent === ''`).

**Files:**
- Modify: `web/src/views/ChatView.vue`
- Modify: `web/src/composables/useThreads.ts` (export `getThreadById` if not already exported)

- [ ] **Step 1: Confirm `getThreadById` is exported from `useThreads`**

```bash
grep -n "getThreadById\|return {" web/src/composables/useThreads.ts | tail -30
```

If `getThreadById` is in the return object, it's available in `ChatView.vue` via destructuring. Note its name for use in template.

- [ ] **Step 2: Confirm `isRunning` is importable**

```bash
grep -n "^export function isRunning\|^export.*isRunning" web/src/composables/useThreads.ts
```

If not exported, add `export` to its declaration. Import it in `ChatView.vue` alongside other imports from `useThreads`.

- [ ] **Step 3: Add the thinking indicator to the delegation activity row loop**

Find the delegation activity row in the template (the `v-for="d in msg.delegatedThreads"` block, around line 543). After the closing `</button>` of each delegation row and before the closing `</div>`, add:

```html
<!-- Delegated agent thinking indicator — shown when thread is active but no output yet -->
<div
  v-if="(() => { const t = getThreadById(d.threadId); return t && isRunning(t) && !t.streamingContent })()"
  class="flex items-center gap-2 pl-2 py-1"
>
  <div class="w-4 h-4 rounded flex-shrink-0 text-[9px] font-bold flex items-center justify-center"
    :style="`background:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.2)'}33;color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
    {{ agentIconMap[d.agentId] || d.agentId?.[0]?.toUpperCase() || '?' }}
  </div>
  <span class="text-[11px] text-huginn-muted/70">thinking</span>
  <span class="flex gap-0.5 ml-0.5">
    <span class="w-1 h-1 rounded-full bg-huginn-muted/50 animate-bounce" style="animation-delay:0ms" />
    <span class="w-1 h-1 rounded-full bg-huginn-muted/50 animate-bounce" style="animation-delay:75ms" />
    <span class="w-1 h-1 rounded-full bg-huginn-muted/50 animate-bounce" style="animation-delay:150ms" />
  </span>
</div>
```

Note: The IIFE `(() => { ... })()` avoids template variable pollution. Alternatively, add a computed helper:

```typescript
function isThreadThinking(threadId: string): boolean {
  const t = getThreadById(threadId)
  return !!t && isRunning(t) && !t.streamingContent
}
```

Then use `v-if="isThreadThinking(d.threadId)"` instead — cleaner.

Apply the thinking indicator after **each** delegation row location in the template (lines ~416, ~451, ~545, ~585).

- [ ] **Step 4: Import `isRunning` in ChatView.vue if not already imported**

At the top of `ChatView.vue`'s `<script setup>`, find the import from `useThreads` and add `isRunning`:

```typescript
import { useThreads, isRunning } from '../composables/useThreads'
```

- [ ] **Step 5: Verify no test regressions**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vitest run src/views/__tests__/ChatView.test.ts
```

- [ ] **Step 6: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add web/src/views/ChatView.vue web/src/composables/useThreads.ts
git commit -m "feat(ui): show delegated agent thinking indicator before first token"
```

---

### Task 6: Agent Config Hot-Reload

**Spec:** Issue 6 — poll `~/.huginn/agents/` for changes and call `orch.SetAgentRegistry` with fresh config on change.

**Files:**
- Create: `internal/agent/agents_watcher.go`
- Create: `internal/agent/agents_watcher_test.go`
- Modify: `main.go`

- [ ] **Step 1: Write the test**

Create `internal/agent/agents_watcher_test.go`:

```go
package agent_test

import (
    "context"
    "os"
    "path/filepath"
    "sync/atomic"
    "testing"
    "time"

    "github.com/scrypster/huginn/internal/agent"
    "github.com/scrypster/huginn/internal/agents"
)

func TestAgentsWatcher_CallbackOnChange(t *testing.T) {
    dir := t.TempDir()
    agentsDir := filepath.Join(dir, "agents")
    if err := os.MkdirAll(agentsDir, 0o700); err != nil {
        t.Fatal(err)
    }

    var calls atomic.Int32
    w := agent.NewAgentsWatcher(dir, func(reg *agents.AgentRegistry) {
        calls.Add(1)
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go w.Start(ctx)

    // Write an agent file
    agentYAML := `name: TestAgent
model: test-model
system_prompt: "hello"
`
    if err := os.WriteFile(filepath.Join(agentsDir, "test.yaml"), []byte(agentYAML), 0o644); err != nil {
        t.Fatal(err)
    }

    // Wait up to 5s for callback to fire
    deadline := time.Now().Add(5 * time.Second)
    for time.Now().Before(deadline) {
        if calls.Load() > 0 {
            break
        }
        time.Sleep(100 * time.Millisecond)
    }

    if calls.Load() == 0 {
        t.Error("expected callback to fire after agent file change, got 0 calls")
    }
}

func TestAgentsWatcher_NoCallbackWhenUnchanged(t *testing.T) {
    dir := t.TempDir()
    _ = os.MkdirAll(filepath.Join(dir, "agents"), 0o700)

    var calls atomic.Int32
    w := agent.NewAgentsWatcher(dir, func(reg *agents.AgentRegistry) {
        calls.Add(1)
    })

    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    go w.Start(ctx)
    <-ctx.Done()

    // No files changed; may fire once on initial load if dir non-empty — that's OK.
    // The important thing is it didn't fire repeatedly for no reason.
    if calls.Load() > 1 {
        t.Errorf("expected at most 1 call (initial), got %d", calls.Load())
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test ./internal/agent/... -run TestAgentsWatcher -v
```

Expected: compilation error (NewAgentsWatcher undefined).

- [ ] **Step 3: Implement `agents_watcher.go`**

Create `internal/agent/agents_watcher.go`:

```go
package agent

import (
    "context"
    "hash/fnv"
    "io/fs"
    "log/slog"
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/scrypster/huginn/internal/agents"
    "github.com/scrypster/huginn/internal/modelconfig"
    "github.com/scrypster/huginn/internal/memory"
)

const (
    agentsWatcherPollInterval = 2 * time.Second
    agentsWatcherDebounce     = 500 * time.Millisecond
)

// AgentsWatcher polls the agents directory for changes to *.yaml/*.json agent
// files and calls the provided callback with a freshly-built AgentRegistry on
// each detected change. Uses FNV-64a hashing of path+size+mtime to detect
// changes without reading file contents on every poll.
type AgentsWatcher struct {
    baseDir  string
    callback func(*agents.AgentRegistry)

    mu       sync.Mutex
    lastHash uint64
    debounce *time.Timer
}

// NewAgentsWatcher creates an AgentsWatcher. callback is called (in a new
// goroutine, after the mutex is released) with a rebuilt AgentRegistry each
// time the agents directory changes.
func NewAgentsWatcher(baseDir string, callback func(*agents.AgentRegistry)) *AgentsWatcher {
    return &AgentsWatcher{
        baseDir:  baseDir,
        callback: callback,
    }
}

// Start begins polling. Blocks until ctx is cancelled. Call in a goroutine.
// Performs an initial hash seed before polling begins (does NOT fire callback
// for the initial state — the caller already loaded the registry at startup).
func (w *AgentsWatcher) Start(ctx context.Context) {
    w.mu.Lock()
    w.lastHash = w.computeHash()
    w.mu.Unlock()

    ticker := time.NewTicker(agentsWatcherPollInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.check()
        }
    }
}

func (w *AgentsWatcher) check() {
    h := w.computeHash()
    w.mu.Lock()
    defer w.mu.Unlock()
    if h == w.lastHash {
        return
    }
    w.lastHash = h
    if w.debounce != nil {
        w.debounce.Stop()
    }
    w.debounce = time.AfterFunc(agentsWatcherDebounce, func() {
        w.reload()
    })
}

func (w *AgentsWatcher) reload() {
    cfg, err := agents.LoadAgentsFromBase(w.baseDir)
    if err != nil {
        slog.Warn("agents watcher: reload failed", "err", err)
        return
    }
    models := modelconfig.DefaultModels()
    username := memory.ResolveUsername("")
    reg := agents.BuildRegistryWithUsername(cfg, models, username)
    slog.Info("agents watcher: registry reloaded", "count", len(cfg.Agents))
    go w.callback(reg)
}

func (w *AgentsWatcher) computeHash() uint64 {
    h := fnv.New64a()
    agentsDir := filepath.Join(w.baseDir, "agents")
    _ = filepath.WalkDir(agentsDir, func(path string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() {
            return nil
        }
        ext := filepath.Ext(path)
        if ext != ".yaml" && ext != ".json" {
            return nil
        }
        info, statErr := os.Stat(path)
        if statErr != nil {
            return nil
        }
        _, _ = h.Write([]byte(path))
        buf := make([]byte, 16)
        size := info.Size()
        mod := info.ModTime().UnixNano()
        for i := 0; i < 8; i++ {
            buf[i] = byte(size >> (i * 8))
            buf[i+8] = byte(mod >> (i * 8))
        }
        _, _ = h.Write(buf)
        return nil
    })
    return h.Sum64()
}
```

- [ ] **Step 4: Run test**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test ./internal/agent/... -run TestAgentsWatcher -v
```

Expected: both tests pass.

- [ ] **Step 5: Wire watcher in `main.go`**

In `main.go`, find `startServer` function (line ~2177). After the block where `orch.SetAgentRegistry(agentReg)` is called (around line 2747), add:

```go
// Start agents directory watcher — hot-reloads agent config without restart.
{
    agentsWatcher := agentlib.NewAgentsWatcher(huginnHome, func(reg *agentslib.AgentRegistry) {
        orch.SetAgentRegistry(reg)
        logger.Info("agent registry hot-reloaded")
    })
    go agentsWatcher.Start(serverCtx) // serverCtx is the context used for server lifetime
}
```

Find the correct context variable name (`serverCtx`, `ctx`, or however the server's lifetime context is named in `startServer`). Search for `context.WithCancel` or `context.Background()` in `startServer`.

- [ ] **Step 6: Verify Go build**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go build ./...
```

Expected: no errors.

- [ ] **Step 7: Run all agent tests**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
go test ./internal/agent/... -v -count=1
```

Expected: all pass.

- [ ] **Step 8: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn
git add internal/agent/agents_watcher.go internal/agent/agents_watcher_test.go main.go
git commit -m "feat(server): hot-reload agent registry when agent config files change"
```

---

## Final verification

```bash
# Go tests
cd /Users/mjbonanno/github.com/scrypster/huginn
go test ./internal/agent/... -race -count=1

# Frontend unit tests
cd web
npx vitest run

# Full build
cd ..
go build ./...
```
