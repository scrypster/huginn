# Thread Tool-Calls Aggregation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the thread detail panel show tool calls as a compact chip (matching the main chat), instead of raw JSON bubbles, by fixing the write path (missing `tool_calls_json` column in `AppendToThread`) and the read path (handler doesn't return it), then wiring the frontend chip UX.

**Architecture:** Three-layer fix: (1) Go write path — `AppendToThread` in `sqlite_store.go` now persists `tool_calls_json`; (2) Go read path — `handleGetMessageThread` SELECTs and deserializes it into the response; (3) Frontend — `useThreadDetail.ts` maps it into `ThreadMessage.toolCalls`, `ThreadDetail.vue` renders it as a chip identical to ChatView. Legacy `tool_call`/`tool_result` role row rendering is kept as fallback for old data and live streaming.

**Tech Stack:** Go 1.21+, SQLite (mattn/go-sqlite3), Vue 3 + TypeScript, Vitest, `@vue/test-utils`

---

### Task 1: Fix write path — `AppendToThread` persists `tool_calls_json`

**Files:**
- Modify: `internal/session/sqlite_store.go` (lines 587–601)

**Context:**
The session `Append` method (line 305–328) correctly serializes `msg.ToolCalls` to JSON and passes it as the `tool_calls_json` column. `AppendToThread` (line 587–601) performs the same INSERT but omits `tool_calls_json` entirely, so all thread messages have `NULL` in that column. The fix mirrors the session path exactly.

The current broken INSERT (line 587–598):
```go
if _, err := tx.Exec(`
    INSERT OR IGNORE INTO messages
        (id, container_type, container_id, seq, ts, role, content,
         agent, tool_name, tool_call_id, type,
         prompt_tokens, completion_tokens, cost_usd, model)
    VALUES (?, 'thread', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    msg.ID, threadID, seq,
    msg.Ts.UTC().Format(time.RFC3339Nano),
    roleOrDefault(msg.Role), msg.Content,
    msg.Agent, msg.ToolName, msg.ToolCallID,
    msg.Type, msg.PromptTok, msg.CompTok, msg.CostUSD, msg.ModelName,
); err != nil {
```

- [ ] **Step 1: Write the failing test**

Create `internal/session/sqlite_store_thread_tool_calls_test.go`:

```go
package session

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAppendToThread_PersistsToolCallsJSON(t *testing.T) {
	db := openTestDB(t)
	store := NewSQLiteSessionStore(db)
	sess := store.New("agent-test", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	msg := SessionMessage{
		ID:      "msg-with-tools",
		Role:    "assistant",
		Content: "I used tools",
		Agent:   "Atlas",
		Ts:      time.Now().UTC(),
		ToolCalls: []PersistedToolCall{
			{ID: "tc-1", Name: "bash", Args: map[string]any{"cmd": "echo hi"}, Result: "hi"},
		},
	}

	if err := store.AppendToThread(sess.ID, "thread-1", msg); err != nil {
		t.Fatalf("AppendToThread: %v", err)
	}

	var raw sql.NullString
	err := db.Read().QueryRow(
		`SELECT tool_calls_json FROM messages WHERE id = ?`, "msg-with-tools",
	).Scan(&raw)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !raw.Valid || raw.String == "" {
		t.Fatal("expected tool_calls_json to be non-NULL, got NULL/empty")
	}
	var calls []PersistedToolCall
	if err := json.Unmarshal([]byte(raw.String), &calls); err != nil {
		t.Fatalf("unmarshal tool_calls_json: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "bash" {
		t.Errorf("expected tool name 'bash', got %q", calls[0].Name)
	}
	if calls[0].Result != "hi" {
		t.Errorf("expected result 'hi', got %q", calls[0].Result)
	}
}

func TestAppendToThread_NilToolCalls_NoColumn(t *testing.T) {
	db := openTestDB(t)
	store := NewSQLiteSessionStore(db)
	sess := store.New("agent-test2", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	msg := SessionMessage{
		ID:      "msg-no-tools",
		Role:    "assistant",
		Content: "No tools here",
		Agent:   "Atlas",
		Ts:      time.Now().UTC(),
	}

	if err := store.AppendToThread(sess.ID, "thread-2", msg); err != nil {
		t.Fatalf("AppendToThread: %v", err)
	}

	var raw sql.NullString
	err := db.Read().QueryRow(
		`SELECT tool_calls_json FROM messages WHERE id = ?`, "msg-no-tools",
	).Scan(&raw)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// NULL or empty is fine for a message with no tool calls
	if raw.Valid && raw.String != "" {
		t.Errorf("expected NULL/empty tool_calls_json for message with no tool calls, got %q", raw.String)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /path/to/repo && go test ./internal/session/ -run TestAppendToThread -v 2>&1 | head -30
```
Expected: FAIL — `expected tool_calls_json to be non-NULL, got NULL/empty`

- [ ] **Step 3: Implement the fix**

In `internal/session/sqlite_store.go`, replace the thread INSERT (lines 587–601) with one that includes `tool_calls_json`. The full replacement block:

```go
var toolCallsJSON *string
if len(msg.ToolCalls) > 0 {
    b, jsonErr := json.Marshal(msg.ToolCalls)
    if jsonErr == nil {
        s := string(b)
        toolCallsJSON = &s
    }
}

if _, err := tx.Exec(`
    INSERT OR IGNORE INTO messages
        (id, container_type, container_id, seq, ts, role, content,
         agent, tool_name, tool_call_id, type,
         prompt_tokens, completion_tokens, cost_usd, model,
         tool_calls_json)
    VALUES (?, 'thread', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    msg.ID, threadID, seq,
    msg.Ts.UTC().Format(time.RFC3339Nano),
    roleOrDefault(msg.Role), msg.Content,
    msg.Agent, msg.ToolName, msg.ToolCallID,
    msg.Type, msg.PromptTok, msg.CompTok, msg.CostUSD, msg.ModelName,
    toolCallsJSON,
); err != nil {
    tx.Rollback()
    return fmt.Errorf("session sqlite: append thread message: %w", err)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/session/ -run TestAppendToThread -v
```
Expected: PASS (both TestAppendToThread_PersistsToolCallsJSON and TestAppendToThread_NilToolCalls_NoColumn)

- [ ] **Step 5: Run full session package tests to confirm no regression**

```bash
go test ./internal/session/... -v 2>&1 | tail -20
```
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/session/sqlite_store.go internal/session/sqlite_store_thread_tool_calls_test.go
git commit -m "fix(session): persist tool_calls_json in AppendToThread"
```

---

### Task 2: Fix read path — `handleGetMessageThread` returns `tool_calls`

**Files:**
- Modify: `internal/server/handlers_threads.go` (lines 18–30, 83–98, 185–210)

**Context:**
`threadMessageRow` has no `ToolCalls` field. The SELECT (lines 83–98) does not include `tool_calls_json`. `scanThreadMessageRows` (lines 185–210) scans 11 columns and doesn't deserialize tool call JSON.

The `session` package import is NOT currently in `handlers_threads.go`. It already imports `encoding/json` and `database/sql`. We need to add `"github.com/scrypster/huginn/internal/session"` for the `session.PersistedToolCall` type.

- [ ] **Step 1: Write the failing test**

Add to `internal/server/handlers_threads_test.go`:

```go
func TestGetMessageThread_ReturnsToolCalls(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	sqliteStore := session.NewSQLiteSessionStore(db)
	sess := sqliteStore.New("tc-test", "/tmp", "model")
	if err := sqliteStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	parentMsg := session.SessionMessage{
		ID:      "parent-tc-1",
		Role:    "user",
		Content: "do work",
		Ts:      time.Now().UTC(),
	}
	if err := sqliteStore.Append(sess, parentMsg); err != nil {
		t.Fatalf("append parent: %v", err)
	}

	wdb := db.Write()
	if wdb == nil {
		t.Fatal("write db is nil")
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	_, err := wdb.Exec(`
		INSERT OR IGNORE INTO threads
			(id, parent_type, parent_id, agent_name, task, status,
			 parent_msg_id, created_at, files_modified, key_decisions, artifacts)
		VALUES (?, 'session', ?, 'Atlas', 'test', 'done',
		        'parent-tc-1', ?, '[]', '[]', '[]')`,
		"t-tc-1", sess.ID, ts,
	)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}

	toolCallsJSON := `[{"id":"tc-1","name":"bash","args":{"cmd":"echo hi"},"result":"hi"}]`
	_, err = wdb.Exec(`
		INSERT OR IGNORE INTO messages
			(id, container_type, container_id, seq, ts, role, content,
			 agent, tool_name, tool_call_id, type,
			 prompt_tokens, completion_tokens, cost_usd, model,
			 tool_calls_json)
		VALUES (?, 'thread', ?, 1, ?, 'assistant', 'I ran bash', 'Atlas',
		        '', '', '', 0, 0, 0.0, '', ?)`,
		"reply-tc-1", "t-tc-1", ts, toolCallsJSON,
	)
	if err != nil {
		t.Fatalf("insert reply: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/parent-tc-1/thread", nil)
	req.SetPathValue("id", "parent-tc-1")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result MessageThreadResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if len(result.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call on message, got %d", len(result.Messages[0].ToolCalls))
	}
	if result.Messages[0].ToolCalls[0].Name != "bash" {
		t.Errorf("expected tool call name 'bash', got %q", result.Messages[0].ToolCalls[0].Name)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/server/ -run TestGetMessageThread_ReturnsToolCalls -v 2>&1 | head -30
```
Expected: compile error or FAIL — `ToolCalls` field does not exist on `threadMessageRow`

- [ ] **Step 3: Implement the fix**

**3a. Add import for session package** in `internal/server/handlers_threads.go` imports block:
```go
"github.com/scrypster/huginn/internal/session"
```

**3b. Add `ToolCalls` to `threadMessageRow` struct** (after `ThreadReplyCount`):
```go
ToolCalls []session.PersistedToolCall `json:"tool_calls,omitempty"`
```

**3c. Update the SELECT query** in `handleGetMessageThread` to add `COALESCE(tool_calls_json, '')` as the 12th column:

Replace:
```go
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

With:
```go
rows, err := rdb.QueryContext(r.Context(), `
    SELECT id, container_id, seq, ts, role, content,
           COALESCE(agent, ''),
           COALESCE(tool_name, ''),
           COALESCE(parent_message_id, ''),
           COALESCE(triggering_message_id, ''),
           COALESCE(thread_reply_count, 0),
           COALESCE(tool_calls_json, '')
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

**3d. Update `scanThreadMessageRows`** to scan and deserialize the 12th column:

Replace the existing `scanThreadMessageRows` function with:
```go
func scanThreadMessageRows(rows *sql.Rows) ([]threadMessageRow, error) {
	var out []threadMessageRow
	for rows.Next() {
		var m threadMessageRow
		var tsStr string
		var toolCallsJSON string
		if err := rows.Scan(
			&m.ID, &m.ContainerID, &m.Seq, &tsStr,
			&m.Role, &m.Content, &m.Agent, &m.ToolName,
			&m.ParentMessageID, &m.TriggeringMessageID,
			&m.ThreadReplyCount, &toolCallsJSON,
		); err != nil {
			return nil, err
		}
		if t, e := time.Parse(time.RFC3339, tsStr); e == nil {
			m.Ts = t.UTC()
		}
		if toolCallsJSON != "" {
			_ = json.Unmarshal([]byte(toolCallsJSON), &m.ToolCalls)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []threadMessageRow{}
	}
	return out, nil
}
```

**IMPORTANT:** `handleGetContainerThreads` also calls `scanThreadMessageRows` with an 11-column SELECT (no `tool_calls_json`). That SELECT must also be updated to add `COALESCE(m.tool_calls_json, '')` as the 12th column — otherwise the scanner will fail (column count mismatch).

Update `handleGetContainerThreads` SELECT from:
```go
rows, err := rdb.QueryContext(r.Context(), `
    SELECT m.id, m.container_id, m.seq, m.ts, m.role, m.content,
           COALESCE(t.agent_name, COALESCE(m.agent, '')),
           COALESCE(m.tool_name, ''),
           COALESCE(m.parent_message_id, ''),
           COALESCE(m.triggering_message_id, ''),
           COALESCE(m.thread_reply_count, 0)
    FROM messages m
    LEFT JOIN threads t ON t.parent_msg_id = m.id
    WHERE m.container_type = 'session' AND m.container_id = ?
      AND (m.parent_message_id IS NULL OR m.parent_message_id = '')
      AND COALESCE(m.thread_reply_count, 0) > 0
    ORDER BY m.seq ASC`,
    containerID,
)
```

To:
```go
rows, err := rdb.QueryContext(r.Context(), `
    SELECT m.id, m.container_id, m.seq, m.ts, m.role, m.content,
           COALESCE(t.agent_name, COALESCE(m.agent, '')),
           COALESCE(m.tool_name, ''),
           COALESCE(m.parent_message_id, ''),
           COALESCE(m.triggering_message_id, ''),
           COALESCE(m.thread_reply_count, 0),
           COALESCE(m.tool_calls_json, '')
    FROM messages m
    LEFT JOIN threads t ON t.parent_msg_id = m.id
    WHERE m.container_type = 'session' AND m.container_id = ?
      AND (m.parent_message_id IS NULL OR m.parent_message_id = '')
      AND COALESCE(m.thread_reply_count, 0) > 0
    ORDER BY m.seq ASC`,
    containerID,
)
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/server/ -run TestGetMessageThread_ReturnsToolCalls -v
```
Expected: PASS

- [ ] **Step 5: Run full server package tests**

```bash
go test ./internal/server/... -v 2>&1 | tail -20
```
Expected: all PASS

- [ ] **Step 6: Build check**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add internal/server/handlers_threads.go internal/server/handlers_threads_test.go
git commit -m "fix(server): return tool_calls in GET /messages/:id/thread response"
```

---

### Task 3: Frontend types — add `toolCalls` to `ThreadMessage`

**Files:**
- Modify: `web/src/composables/useThreadDetail.ts`

**Context:**
`ThreadMessage` interface (line 5–16) has no `toolCalls` field. `ToolCallRecord` is defined and exported from `useSessions.ts`:
```ts
export interface ToolCallRecord {
  id: string
  name: string
  args: Record<string, unknown>
  result?: string
  done: boolean
}
```

`fetchThreadMessages` returns the raw API JSON. Each message in `data.messages` may now carry a `tool_calls` array (array of `{id, name, args, result}`). We map it to `toolCalls: ToolCallRecord[]` with `done: true` (all persisted tool calls are done).

- [ ] **Step 1: Write the failing test**

The existing composable tests are in `web/src/composables/__tests__/useWorkflows.test.ts`. Add a new file `web/src/composables/__tests__/useThreadDetail.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock fetch globally
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

// Mock useApi
vi.mock('../useApi', () => ({ getToken: () => 'test-token' }))

// Import AFTER mocks
const { useThreadDetail } = await import('../useThreadDetail')

function makeApiMessage(overrides = {}) {
  return {
    id: 'msg-1',
    role: 'assistant',
    content: 'Hello',
    agent: 'Atlas',
    seq: 1,
    created_at: new Date().toISOString(),
    ...overrides,
  }
}

describe('fetchThreadMessages maps tool_calls to toolCalls', () => {
  beforeEach(() => {
    mockFetch.mockReset()
  })

  it('maps tool_calls array from API response to toolCalls on ThreadMessage', async () => {
    const apiMsg = makeApiMessage({
      tool_calls: [
        { id: 'tc-1', name: 'bash', args: { cmd: 'echo hi' }, result: 'hi' },
      ],
    })
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ messages: [apiMsg], delegation_chain: [] }),
    })

    const { open, messages } = useThreadDetail()
    await open('msg-1', 'Atlas')

    expect(messages.value).toHaveLength(1)
    const msg = messages.value[0]!
    expect(msg.toolCalls).toBeDefined()
    expect(msg.toolCalls).toHaveLength(1)
    expect(msg.toolCalls![0]!.name).toBe('bash')
    expect(msg.toolCalls![0]!.result).toBe('hi')
    expect(msg.toolCalls![0]!.done).toBe(true)
  })

  it('leaves toolCalls undefined when tool_calls is absent from API response', async () => {
    const apiMsg = makeApiMessage() // no tool_calls
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ messages: [apiMsg], delegation_chain: [] }),
    })

    const { open, messages } = useThreadDetail()
    await open('msg-2', 'Atlas')

    expect(messages.value).toHaveLength(1)
    expect(messages.value[0]!.toolCalls).toBeUndefined()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run src/composables/__tests__/useThreadDetail.test.ts 2>&1 | tail -20
```
Expected: FAIL — `toolCalls` is undefined when it should be defined, or TypeScript compile error

- [ ] **Step 3: Implement the fix**

**3a. Add import** at top of `useThreadDetail.ts` after existing imports:
```ts
import type { ToolCallRecord } from './useSessions'
```

**3b. Add `toolCalls` to `ThreadMessage` interface**:
```ts
export interface ThreadMessage {
  id: string
  role: 'user' | 'assistant' | 'tool_call' | 'tool_result'
  content: string
  agent: string
  seq: number
  created_at: string
  tool_name?: string
  toolName?: string
  type?: string
  streaming?: boolean
  toolCalls?: ToolCallRecord[]
}
```

**3c. Map `tool_calls` in `fetchThreadMessages`**:

In the `return` block at the end of `fetchThreadMessages`, add mapping. Replace:
```ts
return {
  messages: Array.isArray(data.messages) ? (data.messages as ThreadMessage[]) : [],
  thread_id: data.thread_id as string | undefined,
  session_id: data.session_id as string | undefined,
  delegation_chain: Array.isArray(data.delegation_chain)
    ? (data.delegation_chain as string[])
    : [],
}
```

With:
```ts
const rawMsgs: unknown[] = Array.isArray(data.messages) ? data.messages : []
const messages: ThreadMessage[] = rawMsgs.map((m: unknown) => {
  const msg = m as Record<string, unknown>
  const rawToolCalls = Array.isArray(msg['tool_calls']) ? msg['tool_calls'] : undefined
  const toolCalls: ToolCallRecord[] | undefined = rawToolCalls?.length
    ? (rawToolCalls as Record<string, unknown>[]).map(tc => ({
        id: String(tc['id'] ?? ''),
        name: String(tc['name'] ?? ''),
        args: (tc['args'] as Record<string, unknown>) ?? {},
        result: tc['result'] != null ? String(tc['result']) : undefined,
        done: true,
      }))
    : undefined
  return {
    ...(msg as ThreadMessage),
    toolCalls,
  }
})
return {
  messages,
  thread_id: data.thread_id as string | undefined,
  session_id: data.session_id as string | undefined,
  delegation_chain: Array.isArray(data.delegation_chain)
    ? (data.delegation_chain as string[])
    : [],
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run src/composables/__tests__/useThreadDetail.test.ts 2>&1 | tail -20
```
Expected: PASS

- [ ] **Step 5: Run all frontend tests to confirm no regression**

```bash
cd web && npx vitest run 2>&1 | tail -20
```
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/composables/useThreadDetail.ts web/src/composables/__tests__/useThreadDetail.test.ts
git commit -m "feat(composable): add toolCalls to ThreadMessage, map from API tool_calls"
```

---

### Task 4: Frontend UX — chip rendering in `ThreadDetail.vue`

**Files:**
- Modify: `web/src/components/ThreadDetail.vue`
- Modify: `web/src/components/__tests__/ThreadDetail.test.ts`

**Context:**
The chip must match ChatView exactly. The key template from ChatView (lines 544–585):
- Chip button: wrench icon + "N tool call(s)" label + "· done" + chevron
- Expand: per-tool-call card with name + Input (args) + Output (result)
- State: `expandedMsgCalls = ref<Set<string>>(new Set())` + `expandedToolCalls = ref<Set<string>>(new Set())`
- The chip goes inside the assistant message block, after the content div, before the streaming cursor

The existing `toolgroup` consecutive-group rendering (lines 64–130) is KEPT unchanged for legacy rows.

- [ ] **Step 1: Write the failing tests**

Add to `web/src/components/__tests__/ThreadDetail.test.ts` (append to end of file):

```ts
import type { ToolCallRecord } from '../../composables/useSessions'

// Helper: make a ToolCallRecord
function makeToolCall(overrides: Partial<ToolCallRecord> = {}): ToolCallRecord {
  return {
    id: 'tc-1',
    name: 'bash',
    args: { cmd: 'echo hi' },
    result: 'hi',
    done: true,
    ...overrides,
  }
}

describe('ThreadDetail — tool call chip (persisted toolCalls)', () => {
  it('renders chip on assistant message when toolCalls is present', () => {
    const msg = makeMessage({
      role: 'assistant',
      agent: 'Atlas',
      content: 'I ran some tools',
      toolCalls: [makeToolCall()],
    })
    const wrapper = mountComponent({ messages: [msg] })
    expect(wrapper.html()).toContain('tool call')
    expect(wrapper.html()).toContain('done')
  })

  it('shows correct count for multiple tool calls', () => {
    const msg = makeMessage({
      role: 'assistant',
      toolCalls: [
        makeToolCall({ id: 'tc-1', name: 'bash' }),
        makeToolCall({ id: 'tc-2', name: 'read_file' }),
      ],
    })
    const wrapper = mountComponent({ messages: [msg] })
    expect(wrapper.html()).toContain('2 tool calls')
  })

  it('does not render chip when toolCalls is undefined', () => {
    const msg = makeMessage({ role: 'assistant', content: 'No tools' })
    const wrapper = mountComponent({ messages: [msg] })
    // No "· done" text should appear (which is unique to the chip)
    expect(wrapper.html()).not.toContain('· done')
  })

  it('does not render chip when toolCalls is empty array', () => {
    const msg = makeMessage({ role: 'assistant', toolCalls: [] })
    const wrapper = mountComponent({ messages: [msg] })
    expect(wrapper.html()).not.toContain('· done')
  })

  it('expands tool call details when chip is clicked', async () => {
    const msg = makeMessage({
      id: 'msg-chip-1',
      role: 'assistant',
      toolCalls: [makeToolCall({ name: 'bash', args: { cmd: 'ls' }, result: 'file.txt' })],
    })
    const wrapper = mountComponent({ messages: [msg] })

    // Find the chip button (contains "done" and "tool call")
    const buttons = wrapper.findAll('button')
    const chipBtn = buttons.find(b => b.text().includes('done'))
    expect(chipBtn).toBeDefined()
    await chipBtn!.trigger('click')

    // After expanding, tool call name should be visible
    expect(wrapper.html()).toContain('bash')
  })

  it('legacy tool_call role rows still render as toolgroup (backward compat)', () => {
    const wrapper = mountComponent({
      messages: [
        makeMessage({ id: 'm1', role: 'tool_call', content: JSON.stringify({ name: 'read_file' }) }),
        makeMessage({ id: 'm2', role: 'tool_result', content: 'file contents' }),
      ],
    })
    expect(wrapper.html()).toContain('tool call')
    // Should NOT show "· done" (the new chip marker) — the old path uses a different template
    // The toolgroup uses a wrench icon but not the "· done" text
    expect(wrapper.html()).not.toContain('· done')
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/components/__tests__/ThreadDetail.test.ts 2>&1 | tail -30
```
Expected: FAIL on the new chip tests

- [ ] **Step 3: Add `expandedMsgCalls` and `expandedToolCalls` state to `ThreadDetail.vue`**

In the `<script setup>` section, after the existing `expandedGroups` ref (line 320):

```ts
// ── Persisted tool call chip state ───────────────────────────────────
// Mirrors ChatView state for the persisted-tool-call chip UX.
const expandedMsgCalls = ref<Set<string>>(new Set())
const expandedToolCalls = ref<Set<string>>(new Set())

function toggleMsgToolCalls(msgId: string) {
  if (expandedMsgCalls.value.has(msgId)) expandedMsgCalls.value.delete(msgId)
  else expandedMsgCalls.value.add(msgId)
  expandedMsgCalls.value = new Set(expandedMsgCalls.value)
}

function toggleToolCall(tcId: string) {
  if (expandedToolCalls.value.has(tcId)) expandedToolCalls.value.delete(tcId)
  else expandedToolCalls.value.add(tcId)
  expandedToolCalls.value = new Set(expandedToolCalls.value)
}
```

- [ ] **Step 4: Add chip template to the assistant message block**

Inside the `<!-- Assistant message -->` block in `<template>`, after the streaming cursor `<span>` (line 172), add the chip:

```html
              <!-- Tool call chip (persisted — from tool_calls_json) -->
              <div v-if="item.msg.toolCalls?.length" class="mt-2">
                <button @click="toggleMsgToolCalls(item.msg.id)"
                  class="flex items-center gap-2 px-3 py-1.5 rounded-xl border border-huginn-border hover:bg-huginn-surface/80 transition-colors duration-100">
                  <svg class="w-3.5 h-3.5 text-huginn-yellow flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                    <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
                  </svg>
                  <span class="text-xs text-huginn-text">{{ item.msg.toolCalls!.length }} tool call{{ item.msg.toolCalls!.length === 1 ? '' : 's' }}</span>
                  <span class="text-[11px] text-huginn-green">· done</span>
                  <svg class="w-3 h-3 text-huginn-muted transition-transform duration-150 flex-shrink-0"
                    :class="expandedMsgCalls.has(item.msg.id) ? 'rotate-180' : ''"
                    viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                    <polyline points="6 9 12 15 18 9" />
                  </svg>
                </button>
                <div v-if="expandedMsgCalls.has(item.msg.id)" class="mt-1.5 space-y-1.5">
                  <div v-for="tc in item.msg.toolCalls" :key="tc.id"
                    class="rounded-xl overflow-hidden border border-huginn-border">
                    <button @click="toggleToolCall(tc.id)"
                      class="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-huginn-surface/80 transition-colors duration-100">
                      <span class="text-xs font-medium text-huginn-text flex-1">{{ tc.name }}</span>
                      <svg class="w-3 h-3 text-huginn-muted transition-transform duration-150 flex-shrink-0"
                        :class="expandedToolCalls.has(tc.id) ? 'rotate-180' : ''"
                        viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                        <polyline points="6 9 12 15 18 9" />
                      </svg>
                    </button>
                    <div v-if="expandedToolCalls.has(tc.id)"
                      class="border-t border-huginn-border px-3 py-2.5 space-y-2 bg-huginn-surface/30">
                      <div v-if="tc.args && Object.keys(tc.args).length">
                        <p class="text-[10px] text-huginn-muted uppercase tracking-wider mb-1.5">Input</p>
                        <pre class="text-xs text-huginn-muted overflow-x-auto leading-relaxed">{{ JSON.stringify(tc.args, null, 2) }}</pre>
                      </div>
                      <div v-if="tc.result">
                        <p class="text-[10px] text-huginn-muted uppercase tracking-wider mb-1.5">Output</p>
                        <pre class="text-xs text-huginn-muted overflow-x-auto max-h-40 leading-relaxed">{{ tc.result }}</pre>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
```

Also add the import for `ToolCallRecord` at the top of the script section (the import of `ThreadMessage` already covers this via `useThreadDetail`, but the type needs to be available in the template for `item.msg.toolCalls`). No additional import is needed since `toolCalls?: ToolCallRecord[]` is on `ThreadMessage` and TypeScript resolves the template types through the prop.

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd web && npx vitest run src/components/__tests__/ThreadDetail.test.ts 2>&1 | tail -30
```
Expected: all PASS

- [ ] **Step 6: Run all frontend tests**

```bash
cd web && npx vitest run 2>&1 | tail -20
```
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add web/src/components/ThreadDetail.vue web/src/components/__tests__/ThreadDetail.test.ts
git commit -m "feat(ui): thread panel tool call chip — matches main chat UX"
```

---

### Task 5: Final verification

**Files:** None new — verify + optional race test

- [ ] **Step 1: Full Go test suite with race detector**

```bash
go test -race ./internal/session/... ./internal/server/... 2>&1 | tail -20
```
Expected: all PASS, no data races

- [ ] **Step 2: Full frontend test suite**

```bash
cd web && npx vitest run 2>&1 | tail -20
```
Expected: all PASS

- [ ] **Step 3: Build**

```bash
go build ./...
```
Expected: no errors

- [ ] **Step 4: Commit if any cleanup needed, otherwise confirm done**

If all green, the implementation is complete. No final commit needed unless there are fixup changes.
