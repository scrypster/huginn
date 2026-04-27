# Multi-Agent Channels + Frontend Reactivity — Design Spec

**Date:** 2026-04-27
**Status:** Approved (post-Opus review)
**Branch:** feature/close-gaps

---

## Problem

Two independent gaps in the multi-agent channel experience:

1. **Delegation chain not hydrated on panel open.** `delegationChain` in `useThreadDetail` is
   populated only by live WS `thread_started` events. When a user opens the thread detail panel for
   a completed (or older) thread, `delegationChain` is always empty. The panel shows no agent
   breadcrumb.

2. **`BuildChannelContext` is dead code.** The function in `internal/spaces/store.go` was
   superseded by `BuildSpaceContextBlock` in `ws.go`. It is never called from production paths —
   only from four test functions that test the dead function directly. It should be deleted along
   with those tests.

---

## Scope

**In scope:**
- Enrich `handleGetMessageThread` response to include `thread_id`, `session_id`, and
  `delegation_chain`
- Update `fetchThreadMessages` in `useThreadDetail.ts` to extract `delegation_chain` from the
  response and populate `delegationChain.value` on panel open
- Delete `BuildChannelContext` from `internal/spaces/store.go` and the four test functions that
  call it

**Out of scope:**
- `ThreadResponse` / `handleGetThread` changes — `Status` is already serialized at
  `handlers_threads.go:184`; no field changes needed there
- Schema changes — no new columns, no migrations
- New `DelegationStore` interface methods — existing `ListDelegationsBySession` is sufficient
- Any UI redesign of the delegation chain display
- Live WS chain accumulation (already works; this spec fixes the stale-on-open case only)

---

## Architecture

### Why enrich `handleGetMessageThread` instead of `handleGetThread`

`useThreadDetail.open(messageId)` fetches from `GET /api/v1/messages/{id}/thread`
(`handleGetMessageThread`). It does **not** call `GET /api/v1/sessions/{id}/threads/{thread_id}`
(`handleGetThread`). The frontend has only `messageId` at open time — it does not know
`sessionId` or `threadId` until after a fetch completes.

Enriching `handleGetMessageThread` lets the panel get delegation chain, messages, and metadata in
**one request** — no second fetch, no session/thread ID pre-knowledge required.

`threadToResponse` remains pure (no delegation chain injected there). The delegation chain is
composed in `handleGetMessageThread` only.

---

## File Changes

### `internal/server/handlers_threads.go`

**1. New response wrapper**

Replace the current bare-array return from `handleGetMessageThread` with a struct:

```go
// MessageThreadResponse is the JSON body returned by GET /api/v1/messages/:id/thread.
type MessageThreadResponse struct {
    Messages       []threadMessageRow `json:"messages"`
    ThreadID       string             `json:"thread_id,omitempty"`
    SessionID      string             `json:"session_id,omitempty"`
    DelegationChain []string          `json:"delegation_chain"`
}
```

**2. Query thread metadata alongside messages**

The handler already runs:
```sql
SELECT id FROM threads WHERE parent_msg_id = ?
```

Extend to also fetch `session_id` (stored as `parent_id` in the `threads` table when
`parent_type = 'session'`):

```sql
SELECT id, CASE WHEN parent_type = 'session' THEN parent_id ELSE '' END AS session_id
FROM threads
WHERE parent_msg_id = ?
LIMIT 1
```

Capture `threadID` and `sessionID` for inclusion in the response.

**3. Populate delegation chain**

After building the messages list, if `s.delegationStore != nil && sessionID != ""`:

```go
delegationChain := []string{}
recs, err := s.delegationStore.ListDelegationsBySession(sessionID, 50, 0)
if err == nil {
    for _, r := range recs {
        delegationChain = append(delegationChain, r.ToAgent)
    }
}
```

**4. Return `MessageThreadResponse` instead of bare array**

```go
jsonOK(w, MessageThreadResponse{
    Messages:        rows,
    ThreadID:        threadID,
    SessionID:       sessionID,
    DelegationChain: delegationChain,
})
```

The empty-DB fast-path (when `s.db == nil`) returns:
```go
jsonOK(w, MessageThreadResponse{Messages: []threadMessageRow{}, DelegationChain: []string{}})
```

---

### `internal/spaces/store.go`

Delete lines 542–561 (the `BuildChannelContext` function) entirely.

---

### `internal/spaces/store_test.go`

Delete:
- `TestBuildChannelContext_WithMembers` (lines ~83–97)
- `TestBuildChannelContext_Empty` (lines ~98–103)

---

### `internal/spaces/open_dm_concurrent_test.go`

Delete:
- `TestBuildChannelContext_NilDescriptions` (lines ~384–390)
- `TestBuildChannelContext_MissingDescription` (lines ~391–398)

Also remove the comment header `// ── BuildChannelContext edge cases ───────` if present.

---

### `web/src/composables/useThreadDetail.ts`

**1. New response type**

```ts
interface MessageThreadAPIResponse {
  messages: ThreadMessage[]
  thread_id?: string
  session_id?: string
  delegation_chain?: string[]
}
```

**2. Update `fetchThreadMessages`**

Change return type and body extraction:

```ts
async function fetchThreadMessages(messageId: string): Promise<MessageThreadAPIResponse> {
  // ...fetch as before...
  const data = await res.json()
  // Legacy bare-array shape (tests / old server):
  if (Array.isArray(data)) {
    return { messages: data as ThreadMessage[], delegation_chain: [] }
  }
  return {
    messages: Array.isArray(data.messages) ? (data.messages as ThreadMessage[]) : [],
    thread_id: data.thread_id as string | undefined,
    session_id: data.session_id as string | undefined,
    delegation_chain: Array.isArray(data.delegation_chain)
      ? (data.delegation_chain as string[])
      : [],
  }
}
```

**3. Update `open()` to hydrate `delegationChain`**

```ts
async function open(messageId: string, agentName = ''): Promise<void> {
  // ...reset state as before...
  try {
    const result = await fetchThreadMessages(messageId)
    messages.value = result.messages
    delegationChain.value = result.delegation_chain ?? []
    const firstAgent = agentName || result.messages.find(m => m.role === 'assistant')?.agent || ''
    if (firstAgent) {
      await loadArtifactForThread(firstAgent, messageId)
    }
  } catch (e) {
    error.value = (e as Error).message ?? 'Failed to load thread'
  } finally {
    loading.value = false
  }
}
```

**4. Update `scheduleRefetch`**

`scheduleRefetch` currently calls `fetchThreadMessages(id)` and updates `messages.value`. Update it
to also refresh `delegationChain.value`:

```ts
const result = await fetchThreadMessages(id)
messages.value = result.messages
if (result.delegation_chain && result.delegation_chain.length > 0) {
  delegationChain.value = result.delegation_chain
}
```

---

## Tests

### `internal/server/handlers_threads_test.go`

Add `TestHandleGetMessageThread_IncludesDelegationChain`:

```go
func TestHandleGetMessageThread_IncludesDelegationChain(t *testing.T) {
    // Setup: server with delegationStore that returns one delegation record
    // (FromAgent: "Atlas", ToAgent: "Coder", SessionID: "sess-1")
    // and a DB with one thread row (parent_msg_id = "msg-1", parent_type = "session",
    // parent_id = "sess-1") and one message in that thread.
    //
    // Request: GET /api/v1/messages/msg-1/thread
    //
    // Assert:
    // - resp.messages has length >= 0 (not nil)
    // - resp.delegation_chain == ["Coder"]
    // - resp.thread_id != ""
    // - resp.session_id == "sess-1"
}
```

Add `TestHandleGetMessageThread_EmptyDelegationChain_WhenNilStore`:

```go
func TestHandleGetMessageThread_EmptyDelegationChain_WhenNilStore(t *testing.T) {
    // Server with delegationStore = nil.
    // Assert: resp.delegation_chain == [] (not null in JSON)
}
```

Update any existing test that asserts on a bare-array response shape from this endpoint:
- Change `[]threadMessageRow{}` assertion to `MessageThreadResponse{Messages: [...], DelegationChain: []string{}}`

### `web/src/composables/__tests__/useThreadDetail.test.ts`

Add `hydrates delegationChain from API response on open`:

```ts
it('hydrates delegationChain from API response on open', async () => {
  // Mock fetch to return { messages: [], delegation_chain: ['Atlas', 'Coder'] }
  global.fetch = vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => ({
      messages: [],
      thread_id: 't-1',
      session_id: 's-1',
      delegation_chain: ['Atlas', 'Coder'],
    }),
  } as Response)

  const { delegationChain, open } = useThreadDetail()
  await open('msg-1')

  expect(delegationChain.value).toEqual(['Atlas', 'Coder'])
})
```

Add `delegationChain is empty when API returns bare array (legacy)`:

```ts
it('delegationChain is empty when API returns bare array (legacy)', async () => {
  global.fetch = vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => [],
  } as Response)

  const { delegationChain, open } = useThreadDetail()
  await open('msg-1')

  expect(delegationChain.value).toEqual([])
})
```

---

## File Layout

```
internal/
  server/
    handlers_threads.go      ← MODIFIED (enrich response, add MessageThreadResponse)
    handlers_threads_test.go ← MODIFIED (new delegation chain tests, fix array assertions)
  spaces/
    store.go                 ← MODIFIED (delete BuildChannelContext)
    store_test.go            ← MODIFIED (delete 2 BuildChannelContext tests)
    open_dm_concurrent_test.go ← MODIFIED (delete 2 BuildChannelContext tests + comment)
web/
  src/
    composables/
      useThreadDetail.ts     ← MODIFIED (enrich fetch, hydrate delegationChain)
      __tests__/
        useThreadDetail.test.ts ← MODIFIED (new delegation chain tests)
```

---

## API Changes

| Method | Path | Before | After |
|--------|------|--------|-------|
| `GET` | `/api/v1/messages/{id}/thread` | `[]threadMessageRow` bare array | `MessageThreadResponse` object |

Response is backwards-compatible for existing code that accesses `.messages` — the frontend
already handles both bare-array and `{ messages: [...] }` shapes (the new shape extends the object
form). Any code that type-asserts the response as a bare array will need updating.

---

## Success Criteria

- Opening the thread detail panel for a completed thread shows the correct delegation chain (agents
  that ran in the session) without requiring any live WS events
- The `delegation_chain` field is `[]` (not null) when no delegations exist or when the store is
  nil
- Deletion of `BuildChannelContext` and its four test callers — `go build ./...` and
  `go test ./internal/spaces/...` both pass
- No change to `threadToResponse` — it remains pure
- `go test -race ./internal/server/... ./internal/spaces/...` is clean
- Frontend unit tests pass: `npm run test -- useThreadDetail`
