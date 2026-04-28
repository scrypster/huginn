# Thread Tool-Calls Aggregation — Design Spec

**Date:** 2026-04-27
**Status:** Approved
**Branch:** feature/web-chat

---

## Problem

The thread detail panel (slide-out drawer that appears when you click the "thread" indicator on a message) renders tool interactions differently from the main chat. In the main chat, tool calls are aggregated into a compact chip on the assistant message (e.g., "3 tool calls · done") with click-to-expand. In the thread panel, raw `tool_call` / `tool_result` role rows appear as separate "Agent" bubbles showing raw JSON blobs from MuninnDB.

Root cause (two layers):

1. **Write-path bug:** `AppendToThread` in `internal/session/sqlite_store.go` inserts thread messages but omits the `tool_calls_json` column. This column is correctly populated by the session-path `Append` method. All thread assistant messages therefore have `NULL tool_calls_json`, so the aggregated chip data is never persisted.

2. **Read-path gap:** `handleGetMessageThread` in `internal/server/handlers_threads.go` does not SELECT or deserialize `tool_calls_json`, so even if the write path were fixed the data would not reach the client.

---

## Scope

**In scope:**
- Fix `AppendToThread` to persist `tool_calls_json` on assistant messages (same pattern as session `Append`)
- Fix `handleGetMessageThread` SELECT + scanner to return `tool_calls` array in the JSON response
- Fix `ThreadMessage` interface in `useThreadDetail.ts` to carry `toolCalls?: ToolCallRecord[]`
- Map `tool_calls` API field → `toolCalls` in `fetchThreadMessages`
- Add chip + expand UX in `ThreadDetail.vue` on assistant messages with `toolCalls`, matching ChatView exactly
- Keep existing consecutive-group fallback in `ThreadDetail.vue` for (a) old rows with NULL `tool_calls_json` and (b) live WS synthetic `tool_call`/`tool_result` role messages during streaming
- Unit tests: Go write-path + API shape; frontend chip render + legacy fallback

**Out of scope:**
- Schema changes (columns already exist)
- Changes to live WS streaming path (`onToolCall` in `useThreadDetail.ts`) — it already works correctly; `scheduleRefetch` after `thread_done` will replace synthetic rows with persisted chip data
- ChatView changes
- Tool call re-execution or any interaction beyond view/expand

---

## Architecture

### Write path fix (`sqlite_store.go` `AppendToThread`)

The thread INSERT currently:
```sql
INSERT OR IGNORE INTO messages
    (id, container_type, container_id, seq, ts, role, content,
     agent, tool_name, tool_call_id, type,
     prompt_tokens, completion_tokens, cost_usd, model)
VALUES (?, 'thread', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
```

Fix: add `tool_calls_json` as the 16th column (nullable `TEXT`), serialize with same pattern as `Append`:
```go
var toolCallsJSON *string
if len(msg.ToolCalls) > 0 {
    b, jsonErr := json.Marshal(msg.ToolCalls)
    if jsonErr == nil {
        s := string(b)
        toolCallsJSON = &s
    }
}
```
Then pass `toolCallsJSON` as the 16th bind param.

### Read path fix (`handlers_threads.go`)

`threadMessageRow` gains:
```go
ToolCalls []session.PersistedToolCall `json:"tool_calls,omitempty"`
```

SELECT adds: `COALESCE(m.tool_calls_json, '') AS tool_calls_json`

Scanner deserializes if non-empty:
```go
var toolCallsJSON string
// ...scan into toolCallsJSON...
if toolCallsJSON != "" {
    _ = json.Unmarshal([]byte(toolCallsJSON), &row.ToolCalls)
}
```

### Frontend `useThreadDetail.ts`

`ThreadMessage` interface gains:
```ts
toolCalls?: ToolCallRecord[]
```

`ToolCallRecord` is imported from `useSessions.ts` (already exported).

`fetchThreadMessages` maps:
```ts
toolCalls: Array.isArray(m.tool_calls)
  ? (m.tool_calls as ToolCallRecord[])
  : undefined,
```

### Frontend `ThreadDetail.vue` chip rendering

On assistant message items, after the markdown content block, add:

```html
<!-- Chip for persisted tool calls (new path) -->
<div v-if="item.msg.toolCalls?.length" class="mt-2">
  <button @click="toggleMsgToolCalls(item.msg.id)" ...>
    <WrenchIcon class="w-3.5 h-3.5 text-huginn-green" />
    <span class="text-xs text-huginn-text">
      {{ item.msg.toolCalls.length }} tool call{{ item.msg.toolCalls.length === 1 ? '' : 's' }}
    </span>
    <span class="text-[11px] text-huginn-green">· done</span>
    <ChevronDownIcon ... />
  </button>
  <div v-if="expandedMsgCalls.has(item.msg.id)" class="mt-1.5 space-y-1.5">
    <div v-for="tc in item.msg.toolCalls" :key="tc.id" ...>
      <!-- name + args + result -->
    </div>
  </div>
</div>
```

State: `expandedMsgCalls` is a `ref<Set<string>>` (same pattern as ChatView).

The existing consecutive-group `toolgroup` rendering is KEPT for:
1. Legacy rows with `NULL tool_calls_json` (rendered as old-style bubbles)
2. Live WS synthetic `tool_call`/`tool_result` rows during active streaming

Both paths can co-exist. Once `thread_done` fires and `scheduleRefetch` completes, the synthetic rows are replaced by the real persisted rows which carry `toolCalls` — so the chip appears naturally.

---

## File Changes

```
internal/session/
  sqlite_store.go       — MODIFIED: AppendToThread adds tool_calls_json column
internal/server/
  handlers_threads.go   — MODIFIED: threadMessageRow.ToolCalls + SELECT + scanner
web/src/
  composables/useThreadDetail.ts   — MODIFIED: ThreadMessage.toolCalls + map in fetch
  components/ThreadDetail.vue      — MODIFIED: chip rendering + expandedMsgCalls state
```

---

## Tests

### Go

- **Write path:** Call `AppendToThread` with a `SessionMessage` that has `ToolCalls` populated. Then query the DB and assert `tool_calls_json` is non-NULL and parseable.
- **API shape:** Call `handleGetMessageThread` after inserting a thread message with `tool_calls_json`. Assert response JSON has `tool_calls` array with correct `name` and `args`.
- **Nil/empty guard:** Message with no tool calls → `tool_calls` omitted or empty in response (no panic).

### Frontend (Vitest)

- **Chip renders:** Mount `ThreadDetail.vue` with an assistant message carrying `toolCalls: [{id,name,args:{},done:true}]`. Assert chip button is visible.
- **Expand:** Click chip → tool call details visible; click again → hidden.
- **Legacy fallback:** Mount with a message with `role: 'tool_call'` and no `toolCalls` on the assistant. Assert toolgroup bubble renders (backward compat).
- **No chip without toolCalls:** Assistant message with empty/undefined `toolCalls` → no chip.

---

## Success Criteria

- After a thread completes, opening the detail panel shows a chip ("N tool calls · done") on assistant messages instead of raw JSON bubbles
- Clicking the chip expands individual tool call cards with name, args, and result
- Old thread data (NULL `tool_calls_json`) still renders via the existing consecutive-group fallback
- Live streaming still shows synthetic tool_call bubbles during execution; after `thread_done` + refetch they become chips
- `go test ./internal/session/... ./internal/server/...` passes
- `vitest run` passes
- `go build ./...` passes
