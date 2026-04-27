# Multi-Agent Channels + Frontend Reactivity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hydrate delegation chain when the thread detail panel opens, and remove dead `BuildChannelContext` code.

**Architecture:** Enrich `handleGetMessageThread` response to include `thread_id`, `session_id`, and `delegation_chain`. Frontend `open()` sets `delegationChain.value` from the API response. Delete `BuildChannelContext` and its four test callers.

**Tech Stack:** Go 1.23, SQLite (database/sql), TypeScript/Vue 3, Vitest

---

## Context for all tasks

**Spec:** `docs/superpowers/specs/2026-04-27-multi-agent-channels-reactivity-design.md`

**Key files:**
- `internal/server/handlers_threads.go` — main Go changes
- `internal/server/handlers_threads_test.go` — Go tests
- `internal/spaces/store.go` — BuildChannelContext lives here (lines 542–561)
- `internal/spaces/store_test.go` — has 2 BuildChannelContext test functions
- `internal/spaces/open_dm_concurrent_test.go` — has 2 BuildChannelContext test functions
- `web/src/composables/useThreadDetail.ts` — main frontend changes
- `web/src/composables/__tests__/useThreadDetail.test.ts` — frontend tests

**Critical code facts:**
- `handleGetMessageThread` currently returns a bare `[]threadMessageRow` slice via `jsonOK(w, msgs)` (line 87 of handlers_threads.go)
- The `threads` table SQL schema: `id`, `parent_type`, `parent_id` (=session_id when parent_type='session'), `parent_msg_id`
- `s.delegationStore` is already a field on the Server struct (type `session.DelegationStore`, may be nil)
- `ListDelegationsBySession(sessionID string, limit, offset int) ([]DelegationRecord, error)` is the correct existing signature
- `DelegationRecord.ToAgent string` is the field to extract for the chain
- The frontend `fetchThreadMessages` already handles both `Array` and `{ messages: [...] }` shapes

---

### Task 1: Add `MessageThreadResponse` struct and enrich `handleGetMessageThread`

**Files:**
- Modify: `internal/server/handlers_threads.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/handlers_threads_test.go`:

```go
func TestHandleGetMessageThread_ResponseShape(t *testing.T) {
    // Server with no DB and nil delegationStore (fast path).
    srv := newTestServer(t)
    req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/msg-1/thread", nil)
    req.SetPathValue("id", "msg-1")
    w := httptest.NewRecorder()
    srv.handleGetMessageThread(w, req)

    require.Equal(t, http.StatusOK, w.Code)
    var resp struct {
        Messages        []any    `json:"messages"`
        DelegationChain []string `json:"delegation_chain"`
    }
    require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
    // delegation_chain must be [] not null
    require.NotNil(t, resp.DelegationChain)
    require.Equal(t, 0, len(resp.DelegationChain))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run TestHandleGetMessageThread_ResponseShape -v`
Expected: FAIL — current handler returns bare array, decode into struct field `Messages` will be empty and `DelegationChain` will be nil

- [ ] **Step 3: Add `MessageThreadResponse` struct to `handlers_threads.go`**

After the `threadMessageRow` struct (around line 30), add:

```go
// MessageThreadResponse is the JSON body returned by GET /api/v1/messages/:id/thread.
// DelegationChain is always a non-nil slice (empty array in JSON when no delegations).
type MessageThreadResponse struct {
	Messages        []threadMessageRow `json:"messages"`
	ThreadID        string             `json:"thread_id,omitempty"`
	SessionID       string             `json:"session_id,omitempty"`
	DelegationChain []string           `json:"delegation_chain"`
}
```

- [ ] **Step 4: Update the nil-DB fast path in `handleGetMessageThread`**

Change (line 45):
```go
// Before:
jsonOK(w, []threadMessageRow{})

// After:
jsonOK(w, MessageThreadResponse{Messages: []threadMessageRow{}, DelegationChain: []string{}})
```

- [ ] **Step 5: Update the main query to also fetch thread metadata**

Replace the current SQL query (lines 60–74) with:

```go
// First, resolve the thread_id and session_id for this message.
var threadID, sessionID string
threadRow := rdb.QueryRowContext(r.Context(), `
    SELECT id, CASE WHEN parent_type = 'session' THEN parent_id ELSE '' END
    FROM threads WHERE parent_msg_id = ? LIMIT 1`,
    messageID,
)
// Ignore error — thread may not exist yet (in-flight or no-DB case).
_ = threadRow.Scan(&threadID, &sessionID)

rows, err := rdb.QueryContext(r.Context(), `
    SELECT id, container_id, seq, ts, role, content,
           COALESCE(agent, ''),
           COALESCE(tool_name, ''),
           COALESCE(parent_message_id, ''),
           COALESCE(triggering_message_id, ''),
           COALESCE(thread_reply_count, 0)
    FROM messages
    WHERE container_type = 'thread'
      AND container_id IN (
          SELECT id FROM threads WHERE parent_msg_id = ?
      )
      AND role NOT IN ('cost', 'system')
    ORDER BY seq ASC`,
    messageID,
)
```

- [ ] **Step 6: Build delegation chain and return `MessageThreadResponse`**

After `scanThreadMessageRows` succeeds, add:

```go
delegationChain := []string{}
if s.delegationStore != nil && sessionID != "" {
    recs, err2 := s.delegationStore.ListDelegationsBySession(sessionID, 50, 0)
    if err2 == nil {
        for _, r := range recs {
            delegationChain = append(delegationChain, r.ToAgent)
        }
    }
}
jsonOK(w, MessageThreadResponse{
    Messages:        msgs,
    ThreadID:        threadID,
    SessionID:       sessionID,
    DelegationChain: delegationChain,
})
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/server/ -run TestHandleGetMessageThread_ResponseShape -v`
Expected: PASS

- [ ] **Step 8: Run full server test suite**

Run: `go test ./internal/server/... -count=1`
Expected: all pass (fix any tests that asserted bare-array response from this endpoint)

- [ ] **Step 9: Commit**

```bash
git add internal/server/handlers_threads.go internal/server/handlers_threads_test.go
git commit -m "feat(server): enrich handleGetMessageThread with thread_id, session_id, delegation_chain"
```

---

### Task 2: Add delegation chain test with real delegation store mock

**Files:**
- Modify: `internal/server/handlers_threads_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/server/handlers_threads_test.go`:

```go
// stubDelegationStore is a minimal DelegationStore for tests.
type stubDelegationStore struct {
    listBySession []session.DelegationRecord
}

func (s *stubDelegationStore) InsertDelegation(d session.DelegationRecord) error { return nil }
func (s *stubDelegationStore) GetDelegation(id string) (*session.DelegationRecord, error) {
    return nil, sql.ErrNoRows
}
func (s *stubDelegationStore) FindDelegationByThread(threadID string) (*session.DelegationRecord, error) {
    return nil, sql.ErrNoRows
}
func (s *stubDelegationStore) ListDelegationsBySession(sessionID string, limit, offset int) ([]session.DelegationRecord, error) {
    return s.listBySession, nil
}
func (s *stubDelegationStore) UpdateDelegationStatus(id, status, result string, startedAt, completedAt *time.Time) error {
    return nil
}
func (s *stubDelegationStore) ReconcileOrphanDelegations() error { return nil }

func TestHandleGetMessageThread_IncludesDelegationChain(t *testing.T) {
    // Build a server with a real in-memory SQLite DB.
    // Insert a thread row: id='t-1', parent_type='session', parent_id='sess-1',
    //   parent_msg_id='msg-1', agent_name='Atlas'
    // Wire delegationStore returning one record: ToAgent='Coder', SessionID='sess-1'.
    //
    // GET /api/v1/messages/msg-1/thread
    // Assert:
    //   resp.thread_id == "t-1"
    //   resp.session_id == "sess-1"
    //   resp.delegation_chain == ["Coder"]
    //   resp.messages != nil (empty or populated)
    //
    // NOTE: requires an in-memory SQLite DB wired; if test infra doesn't support that
    // easily, use a table-driven integration approach by creating the DB via
    // sqlitedb.OpenMemory() and wiring it to the server.
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run TestHandleGetMessageThread_IncludesDelegationChain -v`
Expected: FAIL (test not implemented yet — verify it compiles at minimum)

- [ ] **Step 3: Implement the test**

Look at existing tests that use in-memory SQLite in `handlers_threads_test.go` (search for `sqlitedb.Open` or `TestHandleGetMessageThread` pattern). Mirror that setup to:
1. Open an in-memory SQLite DB
2. Insert a row into `threads` table: `(id, parent_type, parent_id, parent_msg_id, agent_name)` = `('t-1', 'session', 'sess-1', 'msg-1', 'Atlas')`
3. Create server with that DB and a `stubDelegationStore` returning `[]session.DelegationRecord{{ToAgent: "Coder", SessionID: "sess-1"}}`
4. Make the request and decode the response

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/server/ -run TestHandleGetMessageThread_IncludesDelegationChain -v`
Expected: PASS

- [ ] **Step 5: Run full server tests**

Run: `go test ./internal/server/... -count=1`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers_threads_test.go
git commit -m "test(server): add delegation chain tests for handleGetMessageThread"
```

---

### Task 3: Delete `BuildChannelContext` and its tests

**Files:**
- Modify: `internal/spaces/store.go`
- Modify: `internal/spaces/store_test.go`
- Modify: `internal/spaces/open_dm_concurrent_test.go`

- [ ] **Step 1: Verify `BuildChannelContext` has no production callers**

Run: `grep -rn "BuildChannelContext" /path/to/repo --include="*.go"`

Expected output: only matches in `store.go`, `store_test.go`, `open_dm_concurrent_test.go` (no matches in non-test production files).

Run: `go build ./...`
Expected: succeeds (confirms no unresolved references from non-test code)

- [ ] **Step 2: Delete `BuildChannelContext` from `store.go`**

Remove lines 542–561 from `internal/spaces/store.go`:

```go
// BuildChannelContext returns a system prompt addendum for the lead agent
// listing member agents and their purpose, enabling intelligent delegation.
// Returns empty string for non-channel spaces or spaces with no members.
func BuildChannelContext(leadAgent string, members []string, agentDescriptions map[string]string) string {
	if len(members) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[Team Context]\nYou are the lead agent in a multi-agent channel. ")
	sb.WriteString("You can delegate tasks to the following team members:\n")
	for _, m := range members {
		desc := agentDescriptions[m]
		if desc == "" {
			desc = "specialist agent"
		}
		fmt.Fprintf(&sb, "- %s: %s\n", m, desc)
	}
	sb.WriteString("\nDelegate specialized subtasks to appropriate team members and synthesize their results.")
	return sb.String()
}
```

After deletion, run: `go build ./internal/spaces/...`
Expected: compile error listing the test files referencing `BuildChannelContext`

- [ ] **Step 3: Delete the two test functions in `store_test.go`**

Remove from `internal/spaces/store_test.go`:
- `TestBuildChannelContext_WithMembers`
- `TestBuildChannelContext_Empty`

Both functions call `spaces.BuildChannelContext(...)`.

- [ ] **Step 4: Delete the two test functions in `open_dm_concurrent_test.go`**

Remove from `internal/spaces/open_dm_concurrent_test.go`:
- The comment `// ── BuildChannelContext edge cases ───────────────────────────────────────────`
- `TestBuildChannelContext_NilDescriptions`
- `TestBuildChannelContext_MissingDescription`

- [ ] **Step 5: Verify `fmt` and `strings` imports are still needed in `store.go`**

After deletion, check if `fmt` and `strings` are still used elsewhere in `store.go`. If not, remove the unused imports.

Run: `go build ./internal/spaces/...`
Expected: PASS (no compile errors)

- [ ] **Step 6: Run spaces tests**

Run: `go test ./internal/spaces/... -count=1`
Expected: all pass

- [ ] **Step 7: Commit**

```bash
git add internal/spaces/store.go internal/spaces/store_test.go internal/spaces/open_dm_concurrent_test.go
git commit -m "refactor(spaces): remove dead BuildChannelContext and its tests"
```

---

### Task 4: Update `useThreadDetail.ts` to hydrate delegation chain on open

**Files:**
- Modify: `web/src/composables/useThreadDetail.ts`
- Modify: `web/src/composables/__tests__/useThreadDetail.test.ts`

- [ ] **Step 1: Write the failing tests**

Add to `web/src/composables/__tests__/useThreadDetail.test.ts`:

```ts
describe('delegationChain hydration', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    // Reset module state between tests if composable uses module-level refs
  })

  it('hydrates delegationChain from API response on open', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValueOnce({
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

  it('delegationChain is empty when API returns bare array (legacy shape)', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => [],
    } as Response)

    const { delegationChain, open } = useThreadDetail()
    await open('msg-1')

    expect(delegationChain.value).toEqual([])
  })

  it('delegationChain is empty when API returns object without delegation_chain field', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ messages: [] }),
    } as Response)

    const { delegationChain, open } = useThreadDetail()
    await open('msg-1')

    expect(delegationChain.value).toEqual([])
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npm run test -- useThreadDetail`
Expected: the new tests FAIL because `delegationChain.value` is `[]` regardless of API response

- [ ] **Step 3: Update `fetchThreadMessages` to return enriched shape**

Replace the current `fetchThreadMessages` function with:

```ts
interface MessageThreadAPIResponse {
  messages: ThreadMessage[]
  thread_id?: string
  session_id?: string
  delegation_chain?: string[]
}

async function fetchThreadMessages(messageId: string): Promise<MessageThreadAPIResponse> {
  const res = await fetch(`/api/v1/messages/${encodeURIComponent(messageId)}/thread`, {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getToken()}`,
    },
  })
  if (res.status === 401) {
    throw new Error('Unauthorized: please refresh the page')
  }
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    throw new Error(`Failed to load thread: ${res.status} ${body}`)
  }
  const data = await res.json()
  // Legacy bare-array shape (old server / some tests):
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

- [ ] **Step 4: Update `open()` to set `delegationChain.value`**

In the `open` function, update the try block to:

```ts
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
```

- [ ] **Step 5: Update `scheduleRefetch` to refresh delegation chain**

In `scheduleRefetch`'s setTimeout callback, update:

```ts
const result = await fetchThreadMessages(id)
messages.value = result.messages
if (result.delegation_chain && result.delegation_chain.length > 0) {
  delegationChain.value = result.delegation_chain
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd web && npm run test -- useThreadDetail`
Expected: all pass including the 3 new delegation chain tests

- [ ] **Step 7: Run full frontend test suite**

Run: `cd web && npm run test`
Expected: all pass

- [ ] **Step 8: Commit**

```bash
git add web/src/composables/useThreadDetail.ts web/src/composables/__tests__/useThreadDetail.test.ts
git commit -m "feat(ui): hydrate delegationChain from API on thread panel open"
```

---

### Task 5: Final integration check

**Files:** None (verification only)

- [ ] **Step 1: Run the full Go test suite with race detector**

Run: `go test -race ./internal/server/... ./internal/spaces/... -count=1`
Expected: all pass, no race conditions

- [ ] **Step 2: Build the Go binary**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 3: Run full frontend tests**

Run: `cd web && npm run test`
Expected: all pass

- [ ] **Step 4: Commit if any fixups needed**

If any fixups were required: commit them with descriptive message. Otherwise skip.
