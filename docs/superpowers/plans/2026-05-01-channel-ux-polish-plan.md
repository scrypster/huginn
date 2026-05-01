# Channel UX Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 6 compounding UX failures (horizontal scroll, "0 replies" badge, wildcard save-block, no permission denial visibility, raw JSON tool output, ThreadDetail overflow) to deliver an enterprise-grade agent delegation experience.

**Architecture:** Pure frontend for Issues 1, 2, 5, 6. Frontend + backend WS event for Issue 4. Frontend save-flow fix for Issue 3+6. All changes are isolated and independently testable.

**Tech Stack:** Vue 3, TypeScript, Go, WebSocket event bus, CSS (Tailwind), Vitest, Playwright

---

## File Map

| File | Change |
|------|--------|
| `web/src/style.css` | Add overflow containment to `.md-content`, `.md-content pre`, `.md-content table` |
| `web/src/views/ChatView.vue` | Fix delegation strip reply count; add `min-w-0 overflow-hidden`; add permission denial cards; register WS handler for `thread_permission_denied` |
| `web/src/composables/useSessions.ts` | Add `permissionDenials` field to `ChatMessage`; add `DelegatedThread.done` as source of truth |
| `web/src/views/agents/useAgentsViewState.ts` | Strip wildcard entries on `loadAgent()`; decouple toolbelt validation from LOCAL ACCESS save |
| `web/src/views/agents/useAgentCapabilityMatrix.ts` | Update wildcard error copy to be actionable |
| `web/src/views/AgentsView.vue` | Update save button `:disabled` logic |
| `internal/threadmgr/spawn.go` | Broadcast `thread_permission_denied` WS event after `RequireApprovalToken` check fails |
| `web/src/components/ThreadDetail.vue` | Add `INTERNAL_TOOLS` set; classify tool groups; render memory ops as human-readable summary |

---

## Task 1 — CSS Overflow Containment

**Files:**
- Modify: `web/src/style.css`

- [ ] **Step 1: Add overflow rules to `.md-content`**

Open `web/src/style.css`. The current `.md-content` rule at line 14 is:
```css
.md-content { @apply text-huginn-text; }
```
Replace it with:
```css
.md-content { @apply text-huginn-text; overflow-x: auto; max-width: 100%; }
```

- [ ] **Step 2: Add overflow to `.md-content pre`**

The current `.md-content pre` rule at line 28 is:
```css
.md-content pre { @apply mb-3; }
```
Replace it with:
```css
.md-content pre { @apply mb-3; overflow-x: auto; }
```

- [ ] **Step 3: Add overflow to `.md-content table`**

The current `.md-content table` rule at line 36 is:
```css
.md-content table { @apply w-full text-sm mb-3 border-collapse; }
```
Replace it with:
```css
.md-content table { @apply w-full text-sm mb-3 border-collapse; overflow-x: auto; display: block; }
```

- [ ] **Step 4: Add `min-w-0 overflow-hidden` to assistant message wrapper in `ChatView.vue`**

In `web/src/views/ChatView.vue`, at line 393, the assistant message inner wrapper is:
```html
<div class="flex-1 min-w-0 pt-0.5">
```
This already has `min-w-0`. Now check line 429: the delegation strips parent:
```html
<div v-if="msg.delegatedThreads?.length" class="mt-1.5 space-y-1">
```
No change needed here — the child button at line 433 already has `overflow-hidden min-w-0`.

- [ ] **Step 5: Verify build compiles**

```bash
cd web && npm run build 2>&1 | tail -20
```
Expected: No errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/style.css
git commit -m "fix(ui): add overflow containment to md-content, pre, and table"
```

---

## Task 2 — Fix "0 Replies" Display

**Files:**
- Modify: `web/src/views/ChatView.vue`

**Context:** At line 447-454, the reply count label uses `(d.replyCount ?? 1)`. When `replyCount` is explicitly `0`, the `??` fallback doesn't fire and it shows "0 replies". The fix uses `d.done` as single source of truth, consistent with how `getThreadById` status is checked.

- [ ] **Step 1: Update the reply count template block**

In `web/src/views/ChatView.vue`, lines 447–454 currently read:
```html
<span class="text-xs font-medium" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
  <template v-if="['running','thinking','queued'].includes(getThreadById(d.threadId)?.Status ?? '')">
    working…
  </template>
  <template v-else>
    {{ (d.replyCount ?? 1) === 1 ? '1 reply' : `${d.replyCount} replies` }}
  </template>
</span>
```
Replace with:
```html
<span class="text-xs font-medium" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
  <template v-if="!d.done">
    working…
  </template>
  <template v-else-if="d.done && (d.replyCount ?? 0) < 1">
    completed
  </template>
  <template v-else>
    {{ d.replyCount === 1 ? '1 reply' : `${d.replyCount} replies` }}
  </template>
</span>
```

- [ ] **Step 2: Verify build**

```bash
cd web && npm run build 2>&1 | tail -20
```
Expected: No errors.

- [ ] **Step 3: Run unit tests**

```bash
cd web && npx vitest run src/composables/__tests__/ src/views/__tests__/ 2>&1 | tail -30
```
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add web/src/views/ChatView.vue
git commit -m "fix(ui): use d.done as source of truth for delegation strip reply count"
```

---

## Task 3 — Wildcard Auto-Clean + Save Decoupling

**Files:**
- Modify: `web/src/views/agents/useAgentsViewState.ts`
- Modify: `web/src/views/agents/useAgentCapabilityMatrix.ts`
- Modify: `web/src/views/AgentsView.vue`

**Context:** The `save()` function at line 852 of `useAgentsViewState.ts` runs `capabilityMatrix.hasIssues()` unconditionally, which blocks the save button when a `provider: "*"` wildcard entry exists — even for LOCAL ACCESS-only changes. Root cause: `localReason()` in `useAgentCapabilityMatrix.ts` line 55 flags wildcard entries. Fix: strip wildcards on `loadAgent()`, update error copy, decouple LOCAL ACCESS save from toolbelt validation.

- [ ] **Step 1: Strip wildcards on loadAgent() and expose a ref for the banner**

In `web/src/views/agents/useAgentsViewState.ts`, add a new ref near the other refs (after line 188):
```typescript
const wildcardStripped = ref(false)
```

In the `loadAgent()` function at line 809, after `toolbelt: (data as any).toolbelt || [],`, change the toolbelt line and add stripping logic. The full `loadAgent()` function currently sets `form.value = { ... }` and then `original.value = JSON.stringify(form.value)`. Update the toolbelt assignment inside `form.value = { ... }`:

Change this line inside `loadAgent()`:
```typescript
toolbelt: (data as any).toolbelt || [],
```
To:
```typescript
toolbelt: ((data as any).toolbelt || []).filter((e: any) => e.provider !== '*'),
```

And after the closing `}` of `form.value = { ... }` assignment (before `original.value = JSON.stringify(form.value)`), add:
```typescript
const hadWildcards = ((data as any).toolbelt || []).some((e: any) => e.provider === '*')
wildcardStripped.value = hadWildcards
```

- [ ] **Step 2: Expose wildcardStripped from useAgentsViewState**

In `useAgentsViewState.ts`, find the `return { ... }` statement at the bottom. Add `wildcardStripped` to the returned object.

- [ ] **Step 3: Decouple toolbelt validation from save() when only local_tools changed**

In `useAgentsViewState.ts`, the `save()` function at line 852 currently:
```typescript
async function save() {
  const validationError = validateAgentForm()
  if (validationError) {
    saveMsg.value = validationError
    saveError.value = true
    return
  }
  const toolbeltValid = await capabilityMatrix.validateToolbelt(form.value.toolbelt)
  if (!toolbeltValid || capabilityMatrix.hasIssues(form.value.toolbelt)) {
    saveMsg.value = capabilityMatrix.firstReason(form.value.toolbelt) || 'Invalid connection assignment.'
    saveError.value = true
    return
  }
  ...
```

Add a parameter to allow bypassing toolbelt validation:
```typescript
async function save(options?: { skipToolbeltValidation?: boolean }) {
  const validationError = validateAgentForm()
  if (validationError) {
    saveMsg.value = validationError
    saveError.value = true
    return
  }
  if (!options?.skipToolbeltValidation) {
    const toolbeltValid = await capabilityMatrix.validateToolbelt(form.value.toolbelt)
    if (!toolbeltValid || capabilityMatrix.hasIssues(form.value.toolbelt)) {
      saveMsg.value = capabilityMatrix.firstReason(form.value.toolbelt) || 'Invalid connection assignment.'
      saveError.value = true
      return
    }
  }
  ...
```

- [ ] **Step 4: Update saveLocalAccessModal() to skip toolbelt validation**

In `useAgentsViewState.ts`, the `saveLocalAccessModal()` function at line 710 calls `save()`:
```typescript
async function saveLocalAccessModal() {
  form.value.local_tools = [...modalLocalTools.value]
  showLocalAccessModal.value = false
  if (agentName.value && agentName.value !== 'new') {
    await save()
  } else {
    markDirty()
  }
}
```
Change to:
```typescript
async function saveLocalAccessModal() {
  form.value.local_tools = [...modalLocalTools.value]
  showLocalAccessModal.value = false
  if (agentName.value && agentName.value !== 'new') {
    await save({ skipToolbeltValidation: true })
  } else {
    markDirty()
  }
}
```

- [ ] **Step 5: Update wildcard error copy in useAgentCapabilityMatrix.ts**

In `web/src/views/agents/useAgentCapabilityMatrix.ts`, line 8:
```typescript
wildcard_provider_forbidden: 'Wildcard provider assignment is not allowed.',
```
Replace with:
```typescript
wildcard_provider_forbidden: 'Legacy wildcard connection — click Remove to fix and unlock save.',
```

- [ ] **Step 6: Show info banner in AgentsView.vue when wildcards were stripped**

In `web/src/views/AgentsView.vue`, find the import of `useAgentsViewState` (it destructures many items). Add `wildcardStripped` to the destructured items.

Then in the template, find the existing banners section near line 56. Add an info banner after the existing banners:
```html
<!-- Wildcard strip info banner -->
<div v-if="wildcardStripped"
  class="flex items-center gap-2 px-4 py-2.5 border-b border-huginn-amber/20 bg-huginn-amber/8 text-huginn-amber text-xs"
>
  <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
    <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
  </svg>
  Removed legacy wildcard connection. Save to persist.
  <button @click="wildcardStripped = false" class="ml-auto text-huginn-amber/60 hover:text-huginn-amber transition-colors">✕</button>
</div>
```

- [ ] **Step 7: Verify build and unit tests**

```bash
cd web && npm run build 2>&1 | tail -20 && npx vitest run 2>&1 | tail -30
```
Expected: No errors, all tests pass.

- [ ] **Step 8: Commit**

```bash
git add web/src/views/agents/useAgentsViewState.ts web/src/views/agents/useAgentCapabilityMatrix.ts web/src/views/AgentsView.vue
git commit -m "fix(agents): strip wildcard toolbelt entries on load, decouple local access save from toolbelt validation"
```

---

## Task 4 — Backend: thread_permission_denied WS Event

**Files:**
- Modify: `internal/threadmgr/spawn.go`

**Context:** In `spawn.go`, the `runOnce` function at line 731–736 handles permission checks:
```go
if provider, action, needsApproval := delegatedToolRisk(tc.Function.Name); needsApproval {
    token, _ := tc.Function.Arguments["_approval_token"].(string)
    if err := tm.RequireApprovalToken(threadID, token, provider, action); err != nil {
        appendToolResult("tool error: permission denied: " + err.Error())
        continue
    }
}
```
After the `appendToolResult(...)` line, broadcast a `thread_permission_denied` WS event. Use a per-`runOnce`-call dedup map keyed by `tc.Function.Name` so each tool is only broadcast once per turn.

- [ ] **Step 1: Add dedup map before the tool loop**

In `spawn.go`, find the outer `for` loop that iterates `tc.Function.Name` tool calls. It starts just above line 721 (`switch tc.Function.Name {`). Before that loop, add a dedup map. The loop header typically looks like:
```go
for _, tc := range resp.Message.ToolCalls {
```
Add before the loop:
```go
deniedTools := map[string]bool{}
```

- [ ] **Step 2: Broadcast after permission denial**

After:
```go
appendToolResult("tool error: permission denied: " + err.Error())
continue
```
Add:
```go
if !deniedTools[tc.Function.Name] {
    deniedTools[tc.Function.Name] = true
    broadcast(sess.ID, "thread_permission_denied", map[string]any{
        "thread_id":  threadID,
        "agent_id":   agentID,
        "tool":       tc.Function.Name,
        "session_id": sess.ID,
    })
}
```

The full block after the change should read:
```go
if provider, action, needsApproval := delegatedToolRisk(tc.Function.Name); needsApproval {
    token, _ := tc.Function.Arguments["_approval_token"].(string)
    if err := tm.RequireApprovalToken(threadID, token, provider, action); err != nil {
        appendToolResult("tool error: permission denied: " + err.Error())
        if !deniedTools[tc.Function.Name] {
            deniedTools[tc.Function.Name] = true
            broadcast(sess.ID, "thread_permission_denied", map[string]any{
                "thread_id":  threadID,
                "agent_id":   agentID,
                "tool":       tc.Function.Name,
                "session_id": sess.ID,
            })
        }
        continue
    }
}
```

- [ ] **Step 3: Run Go tests**

```bash
go test ./internal/threadmgr/... -v 2>&1 | tail -40
```
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add internal/threadmgr/spawn.go
git commit -m "feat(backend): broadcast thread_permission_denied WS event on tool permission denial"
```

---

## Task 5 — Frontend: Permission Denial Cards

**Files:**
- Modify: `web/src/composables/useSessions.ts`
- Modify: `web/src/views/ChatView.vue`

**Context:** Add `permissionDenials` to `ChatMessage`. Register a WS handler for `thread_permission_denied` in `ChatView.vue`. Render amber "🔒 Agent needs X access to continue [Grant]" cards below delegation strips, with frontend dedup and fade-out after thread completes.

- [ ] **Step 1: Add permissionDenials to ChatMessage interface**

In `web/src/composables/useSessions.ts`, the `ChatMessage` interface is at lines 44-55:
```typescript
export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  agent?: string
  createdAt?: string
  streaming?: boolean
  toolCalls?: ToolCallRecord[]
  delegatedThreads?: DelegatedThread[]
  threadReplies?: ThreadReply[]
  replyCount?: number
}
```
Add `permissionDenials` after `replyCount`:
```typescript
export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  agent?: string
  createdAt?: string
  streaming?: boolean
  toolCalls?: ToolCallRecord[]
  delegatedThreads?: DelegatedThread[]
  threadReplies?: ThreadReply[]
  replyCount?: number
  permissionDenials?: Array<{ agentId: string; tool: string; threadId: string }>
}
```

- [ ] **Step 2: Register WS handler for thread_permission_denied in ChatView.vue**

In `web/src/views/ChatView.vue`, find where other WS event handlers are registered (search for `thread_done` or `thread_started` handler registration). Add a new handler in the same location:

```typescript
ws.on('thread_permission_denied', (payload: Record<string, unknown>) => {
  const threadId = payload.thread_id as string
  const agentId = payload.agent_id as string
  const tool = payload.tool as string
  if (!threadId || !agentId || !tool) return

  const sessionId = currentSessionId.value
  if (!sessionId) return

  if (queueIfHydrating(sessionId, () => {
    applyPermissionDenied(sessionId, threadId, agentId, tool)
  })) return

  applyPermissionDenied(sessionId, threadId, agentId, tool)
})

function applyPermissionDenied(sessionId: string, threadId: string, agentId: string, tool: string) {
  const msgs = getMessages(sessionId)
  const msg = msgs.find(m =>
    m.delegatedThreads?.some(d => d.threadId === threadId)
  )
  if (!msg) return
  if (!msg.permissionDenials) msg.permissionDenials = []
  // Frontend dedup: one entry per threadId:tool pair
  const key = `${threadId}:${tool}`
  if (msg.permissionDenials.some(d => `${d.threadId}:${d.tool}` === key)) return
  msg.permissionDenials.push({ agentId, tool, threadId })
}
```

- [ ] **Step 3: Render permission denial cards in ChatView.vue**

In `web/src/views/ChatView.vue`, find the section after the delegation error chips (around line 503, after the `delegationErrors` div). Add the permission denial cards block after the delegation warnings block (after line 520):

```html
<!-- Permission denial cards: shown when a delegated agent was blocked by permissions -->
<div v-if="msg.permissionDenials?.length" class="mt-1.5 space-y-1">
  <div
    v-for="denial in msg.permissionDenials"
    :key="denial.threadId + ':' + denial.tool"
    class="flex items-center gap-2 px-2.5 py-2 rounded-lg border text-xs transition-opacity"
    :class="getDelegatedThread(msg, denial.threadId)?.done
      ? 'border-huginn-amber/15 bg-huginn-amber/4 opacity-50'
      : 'border-huginn-amber/30 bg-huginn-amber/8'"
  >
    <span class="text-huginn-amber flex-shrink-0">🔒</span>
    <span class="text-huginn-amber font-medium flex-1 min-w-0">
      {{ denial.agentId }} needs {{ formatToolName(denial.tool) }} access to continue
    </span>
    <router-link
      v-if="!getDelegatedThread(msg, denial.threadId)?.done"
      :to="`/agents/${denial.agentId}`"
      class="flex-shrink-0 px-2 py-0.5 rounded-md text-[11px] font-medium border border-huginn-amber/40 text-huginn-amber hover:bg-huginn-amber/15 transition-colors"
    >
      Grant
    </router-link>
  </div>
</div>
```

- [ ] **Step 4: Add helper functions**

In `ChatView.vue`, in the `<script setup>` section, add these two helpers near the other utility functions:

```typescript
function getDelegatedThread(msg: ChatMessage, threadId: string): DelegatedThread | undefined {
  return msg.delegatedThreads?.find(d => d.threadId === threadId)
}

function formatToolName(tool: string): string {
  // bash → bash shell, read_file → file read, etc.
  const map: Record<string, string> = {
    bash: 'bash shell',
    run_tests: 'test runner',
    read_file: 'file read',
    write_file: 'file write',
    edit_file: 'file edit',
    list_dir: 'directory list',
    search_files: 'file search',
    grep: 'grep',
    fetch_url: 'web fetch',
    web_search: 'web search',
  }
  return map[tool] ?? tool.replace(/_/g, ' ')
}
```

- [ ] **Step 5: Import DelegatedThread type if needed**

At the top of `ChatView.vue` `<script setup>`, ensure `DelegatedThread` is imported from `useSessions`:
```typescript
import { useSessions, type ChatMessage, type DelegatedThread } from '../composables/useSessions'
```
(Check the existing import — add `DelegatedThread` if missing.)

- [ ] **Step 6: Verify build**

```bash
cd web && npm run build 2>&1 | tail -20
```
Expected: No errors.

- [ ] **Step 7: Run unit tests**

```bash
cd web && npx vitest run 2>&1 | tail -30
```
Expected: All pass.

- [ ] **Step 8: Commit**

```bash
git add web/src/composables/useSessions.ts web/src/views/ChatView.vue
git commit -m "feat(ui): show permission denial cards below delegation strips when agent is blocked"
```

---

## Task 6 — Filter Internal Tool Results in Thread Panel

**Files:**
- Modify: `web/src/components/ThreadDetail.vue`

**Context:** muninn_* memory tool calls appear in every thread as raw JSON. Add an `INTERNAL_TOOLS` set, mark tool groups that consist entirely of internal calls, and render them as `🧠 Memory: checked context` (italic, muted) instead of the expandable chip.

- [ ] **Step 1: Add INTERNAL_TOOLS set and isInternal logic**

In `web/src/components/ThreadDetail.vue`, after the `PALETTE` constant (around line 301), add:

```typescript
const INTERNAL_TOOLS = new Set([
  'muninn_recall', 'muninn_remember', 'muninn_remember_batch', 'muninn_remember_tree',
  'muninn_read', 'muninn_session', 'muninn_where_left_off', 'muninn_state',
  'muninn_entities', 'muninn_entity', 'muninn_find_by_entity', 'muninn_link',
  'muninn_forget', 'muninn_consolidate', 'muninn_evolve', 'muninn_feedback',
  'muninn_guide', 'muninn_status', 'muninn_traverse', 'muninn_recall_tree',
])

function isInternalTool(toolName: string): boolean {
  return INTERNAL_TOOLS.has(toolName) || toolName.startsWith('muninn_')
}

function summarizeMemoryOp(calls: ThreadMessage[]): string {
  const names = calls.map(c => extractToolName(c.content))
  if (names.some(n => n === 'muninn_remember' || n === 'muninn_remember_batch' || n === 'muninn_remember_tree')) {
    return 'saved to memory'
  }
  if (names.some(n => n === 'muninn_session' || n === 'muninn_where_left_off')) {
    return 'resumed session'
  }
  return 'checked context'
}
```

- [ ] **Step 2: Add isInternal to ToolGroup type**

The `ToolGroup` type at line 293 currently:
```typescript
type ToolGroup = {
  type: 'toolgroup'
  key: string
  calls: ThreadMessage[]
  results: ThreadMessage[]
}
```
Replace with:
```typescript
type ToolGroup = {
  type: 'toolgroup'
  key: string
  calls: ThreadMessage[]
  results: ThreadMessage[]
  isInternal: boolean
}
```

- [ ] **Step 3: Set isInternal when building groupedMessages**

In the `groupedMessages` computed at line 405, the push currently is:
```typescript
result.push({
  type: 'toolgroup',
  key: `tg-${startIdx}`,
  calls,
  results,
})
```
Replace with:
```typescript
result.push({
  type: 'toolgroup',
  key: `tg-${startIdx}`,
  calls,
  results,
  isInternal: calls.length > 0 && calls.every(c => isInternalTool(extractToolName(c.content))),
})
```

- [ ] **Step 4: Update the template to render internal groups differently**

In the template, find the `<div v-if="item.type === 'toolgroup'">` block (line 68). Currently it renders a button with toggle. Replace the entire `<div v-if="item.type === 'toolgroup'">` block (lines 68–130) with:

```html
<div v-if="item.type === 'toolgroup'">
  <!-- Internal memory ops: compact, non-interactive summary -->
  <div v-if="item.isInternal"
    class="flex items-center gap-1.5 py-0.5 text-huginn-muted/40 select-none"
  >
    <span class="text-[11px] italic">🧠 Memory: {{ summarizeMemoryOp(item.calls) }}</span>
  </div>

  <!-- Regular tool groups: collapsible chip -->
  <template v-else>
    <!-- Collapsed summary row -->
    <button
      @click="toggleGroup(item.key)"
      class="flex items-center gap-1.5 py-1 text-huginn-muted/50 hover:text-huginn-muted/80 transition-colors w-full text-left"
    >
      <svg class="w-3 h-3 flex-shrink-0 text-huginn-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
      </svg>
      <span class="text-[11px]">
        {{ item.calls.length }} tool call{{ item.calls.length !== 1 ? 's' : '' }}
      </span>
      <svg class="w-3 h-3 ml-auto flex-shrink-0 transition-transform" :class="{'rotate-180': expandedGroups[item.key]}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <polyline points="6 9 12 15 18 9" />
      </svg>
    </button>

    <!-- Expanded detail: each tool call + its result -->
    <div v-if="expandedGroups[item.key]" class="mt-1 space-y-1 pl-4 border-l-2" style="border-color:rgba(255,255,255,0.08)">
      <template v-for="call in item.calls" :key="call.id">
        <!-- Tool call row -->
        <div class="flex items-center gap-2 px-2 py-1.5 rounded-lg border border-huginn-border bg-huginn-surface/30 text-xs">
          <svg class="w-3 h-3 text-huginn-yellow flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
          </svg>
          <span class="text-huginn-text font-medium">{{ extractToolName(call.content) }}</span>
        </div>
        <!-- Matching tool result (if any) -->
        <template v-if="item.results.find(r => r.tool_name === call.tool_name)">
          <template v-for="cr in [call.tool_name === 'consult_agent' ? parseConsultResult(item.results.find(r => r.tool_name === call.tool_name)?.content ?? '') : null]" :key="call.tool_name + '-cr'">
            <!-- Consultation card: special rendering for consult_agent results -->
            <div v-if="cr" class="rounded-lg border overflow-hidden"
              :style="`border-color:${agentColor(cr.agentName)}33`">
              <div class="flex items-center gap-1.5 px-2 py-1.5"
                :style="`background:${agentColor(cr.agentName)}10`">
                <div class="w-3.5 h-3.5 rounded text-[8px] font-bold flex items-center justify-center flex-shrink-0"
                  :style="`background:${agentColor(cr.agentName)}22;color:${agentColor(cr.agentName)}`">
                  {{ cr.agentName[0]?.toUpperCase() }}
                </div>
                <span class="text-[11px] font-medium"
                  :style="`color:${agentColor(cr.agentName)}`">
                  {{ cr.agentName }}
                </span>
                <span class="text-[10px] text-huginn-muted/50 ml-auto">consulted</span>
              </div>
              <div class="px-2 py-1.5 bg-huginn-surface/20">
                <div class="md-content text-[11px] text-huginn-muted leading-relaxed break-words"
                  v-html="renderMarkdown(cr.answer)" />
              </div>
            </div>
            <!-- Generic tool result -->
            <div v-else class="px-2 py-1.5 rounded-lg border border-huginn-border bg-huginn-surface/20">
              <pre class="text-[11px] text-huginn-muted overflow-x-auto max-h-24 leading-relaxed whitespace-pre-wrap break-words">{{ item.results.find(r => r.tool_name === call.tool_name)?.content }}</pre>
            </div>
          </template>
        </template>
      </template>
    </div>
  </template>
</div>
```

- [ ] **Step 5: Verify build**

```bash
cd web && npm run build 2>&1 | tail -20
```
Expected: No errors.

- [ ] **Step 6: Run unit tests**

```bash
cd web && npx vitest run 2>&1 | tail -30
```
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/ThreadDetail.vue
git commit -m "feat(ui): render muninn memory tool calls as compact 'Memory: checked context' summary"
```

---

## Task 7 — ThreadDetail Overflow + Final Polish

**Files:**
- Modify: `web/src/components/ThreadDetail.vue`
- Modify: `web/src/views/ChatView.vue`

**Context:** ThreadDetail needs the same overflow cascade as ChatView. The panel content container and message content wrappers need `min-w-0 overflow-hidden`.

- [ ] **Step 1: Add overflow containment to ThreadDetail message content containers**

In `web/src/components/ThreadDetail.vue`, find the main scrollable message list container. It's the outer `<div class="flex-1 overflow-y-auto ...">` that wraps the message list. Look for the template loop at line 65:
```html
<template v-for="(item, idx) in groupedMessages" ...>
```
The parent container above it needs `min-w-0 overflow-x-hidden`. Find the enclosing scrollable div and ensure it has `min-w-0 overflow-x-hidden`.

In the message template area at line 133, the regular message `<template v-else-if="item.type === 'message'">` block contains message content divs. For each assistant message content div that renders markdown with `v-html`, ensure the div has `min-w-0 overflow-hidden`. Look for `md-content` divs in ThreadDetail and ensure their parent has `min-w-0`.

Search for the main message content wrapper in ThreadDetail (around line 140–200) and add `min-w-0 overflow-hidden` to the outer flex container of each message row.

Specifically, find:
```html
<div class="flex gap-2 py-1">
```
or similar message row containers and add `min-w-0 overflow-hidden`.

- [ ] **Step 2: Verify no horizontal scroll in ThreadDetail panel**

```bash
cd web && npm run build 2>&1 | tail -10
```
Expected: No errors.

- [ ] **Step 3: Ensure assistant message bubble in ChatView has proper overflow cascade**

In `web/src/views/ChatView.vue`, line 393: `<div class="flex-1 min-w-0 pt-0.5">` — already correct.

Line 402: `<div v-if="msg.content" class="md-content text-sm text-huginn-text leading-relaxed break-words"` — already has `break-words`, no change needed.

- [ ] **Step 4: Run full test suite**

```bash
cd web && npx vitest run 2>&1 | tail -40
```
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ThreadDetail.vue web/src/views/ChatView.vue
git commit -m "fix(ui): add overflow containment to ThreadDetail message containers"
```

---

## Final Verification

- [ ] **Run all Go tests**

```bash
go test ./... 2>&1 | tail -40
```
Expected: All pass.

- [ ] **Run all frontend tests**

```bash
cd web && npx vitest run 2>&1 | tail -40
```
Expected: All pass.

- [ ] **Run E2E smoke test**

```bash
cd web && npx playwright test e2e/chat.spec.ts --headed 2>&1 | tail -20
```
Expected: Pass or known skip.
