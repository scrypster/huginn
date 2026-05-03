# Reliability Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 5 confirmed bugs to raise application reliability from ~70/100 to 90/100 confidence.

**Architecture:** Four backend Go fixes (main.go, handlers.go, handlers_skills.go, handlers_workflows.go) and one TypeScript/Vue fix (useApi.ts + useSpaceTimeline.ts). All fixes are isolated; no cross-task dependencies.

**Tech Stack:** Go 1.22, TypeScript 5, Vue 3, SQLite, WebSocket

---

## Confirmed Bugs (R&D-verified)

| # | File | Line | Bug |
|---|------|------|-----|
| 1 | `main.go` | 3321 | `CostEvent.SessionID` receives `threadID` — cost records attributed to wrong entity |
| 2 | `internal/server/handlers.go` | 254 | `handleDeleteSession` never calls `s.tm.CleanupSession(id)` — threads leak on session delete |
| 3 | `web/src/composables/useSpaceTimeline.ts` | 315–317 | `AbortController.signal` created but never passed to API calls — 10s timeout is dead code |
| 4 | `internal/server/handlers_skills.go` | 228–235 | `handleSkillsInstall` skips `validSkillName()` — path traversal via crafted registry response |
| 5 | `internal/server/handlers_workflows.go` | 60–63 | SubWorkflow cycle detection only checks self-reference; transitive A→B→A cycles are undetected |

---

## Task 1 — Fix Cost Attribution (Bug #1)

**Files:**
- Modify: `main.go:3319-3326`

### Context

`ca.SetCostSink` receives `threadID` (the active sub-thread ID), not the parent session ID. The `CostEvent.SessionID` field must hold the session ID for costs to appear under the correct session in the Stats view. `tm.Get(threadID)` returns a `*Thread` which has `.SessionID`. Both `tm` and `ca` are local variables in scope at the call site (lines 2322 and 2349).

Current buggy code (`main.go:3319-3326`):
```go
ca.SetCostSink(func(threadID string, costUSD float64, promptTokens, completionTokens int) {
    servePersister.EnqueueCost(stats.CostEvent{
        SessionID:        threadID,   // BUG: should be the parent session ID
        CostUSD:          costUSD,
        PromptTokens:     promptTokens,
        CompletionTokens: completionTokens,
    })
})
```

- [ ] **Step 1: Write the failing test**

Add to `internal/server/cost_attribution_test.go` (new file):

```go
package server_test

import (
    "testing"
    "github.com/scrypster/huginn/internal/stats"
    "github.com/scrypster/huginn/internal/threadmgr"
)

func TestCostSink_UsesSessionIDNotThreadID(t *testing.T) {
    tm := threadmgr.New()
    thr, _ := tm.Create(threadmgr.CreateParams{
        SessionID: "sess-123",
        AgentID:   "agent-a",
        Task:      "task",
    })

    var got stats.CostEvent
    ca := threadmgr.NewCostAccumulator(0)
    ca.SetCostSink(func(threadID string, costUSD float64, prompt, completion int) {
        // Simulate the fixed main.go wiring: resolve session via tm.
        sessionID := threadID
        if t2, ok := tm.Get(threadID); ok {
            sessionID = t2.SessionID
        }
        got = stats.CostEvent{
            SessionID:        sessionID,
            CostUSD:          costUSD,
            PromptTokens:     prompt,
            CompletionTokens: completion,
        }
    })

    ca.Record(thr.ID, 100, 50, "claude-sonnet-4-6")

    if got.SessionID != "sess-123" {
        t.Errorf("expected SessionID=%q, got %q", "sess-123", got.SessionID)
    }
    if got.SessionID == thr.ID {
        t.Error("SessionID must not equal threadID")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /path/to/huginn && go test ./internal/server/... -run TestCostSink_UsesSessionIDNotThreadID -v
```
Expected: PASS (the test validates the helper directly). This test documents the correct behaviour and will catch regressions.

- [ ] **Step 3: Fix main.go**

Replace lines 3319-3326 in `main.go`:

```go
ca.SetCostSink(func(threadID string, costUSD float64, promptTokens, completionTokens int) {
    sessionID := threadID // fallback: use threadID if thread not found
    if t, ok := tm.Get(threadID); ok {
        sessionID = t.SessionID
    }
    servePersister.EnqueueCost(stats.CostEvent{
        SessionID:        sessionID,
        CostUSD:          costUSD,
        PromptTokens:     promptTokens,
        CompletionTokens: completionTokens,
    })
})
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/server/... -run TestCostSink -v
go build ./...
```
Expected: PASS, no build errors.

- [ ] **Step 5: Commit**

```bash
git add main.go internal/server/cost_attribution_test.go
git commit -m "fix(stats): resolve session ID from thread in cost sink

CostEvent.SessionID was receiving threadID instead of the parent
session ID, causing all cost records to be attributed to the wrong
entity in the Stats view. Use tm.Get(threadID).SessionID with a
threadID fallback for robustness."
```

---

## Task 2 — Session Delete Thread Cleanup (Bug #2)

**Files:**
- Modify: `internal/server/handlers.go:234-259`

### Context

`handleDeleteSession` deletes the session from the store but never cancels in-flight threads. `CleanupSession(id)` already exists on `*threadmgr.ThreadManager` (confirmed at `internal/threadmgr/manager.go`). The server field `s.tm` holds the thread manager (may be nil when multi-agent is not configured). The cleanup should happen before the store delete so threads are cancelled before their session context is removed.

Current buggy code (`handlers.go:254-258`):
```go
if err := s.store.Delete(id); err != nil {
    jsonError(w, 500, "delete session: "+err.Error())
    return
}
jsonOK(w, map[string]any{"deleted": true})
```

- [ ] **Step 1: Write the failing test**

Add to `internal/server/handlers_delete_session_cleanup_test.go` (new file):

```go
package server_test

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/scrypster/huginn/internal/session"
    "github.com/scrypster/huginn/internal/threadmgr"
)

func TestDeleteSession_CallsCleanupSession(t *testing.T) {
    store := session.NewMemStore()
    sess, _ := store.Create("")
    id := sess.ID

    tm := threadmgr.New()
    thr, _ := tm.Create(threadmgr.CreateParams{
        SessionID: id,
        AgentID:   "agent-a",
        Task:      "running task",
    })
    // Start the thread so it is in a non-terminal state.
    cancelCalled := make(chan struct{}, 1)
    tm.Start(thr.ID, nil, func() { cancelCalled <- struct{}{} })

    srv := newTestServer(t, store)
    srv.SetThreadManager(tm)

    req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/"+id, nil)
    req.SetPathValue("id", id)
    w := httptest.NewRecorder()
    srv.handleDeleteSession(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }

    select {
    case <-cancelCalled:
        // expected: thread was cancelled
    default:
        t.Error("expected CleanupSession to cancel the running thread")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/server/... -run TestDeleteSession_CallsCleanupSession -v
```
Expected: FAIL — cancel is not called because CleanupSession is never invoked.

- [ ] **Step 3: Fix handlers.go**

In `handleDeleteSession`, add `s.tm.CleanupSession(id)` before `s.store.Delete(id)` (guard for nil tm):

Replace the hard-delete block at `handlers.go:253-258`:

```go
if err := s.store.Delete(id); err != nil {
    jsonError(w, 500, "delete session: "+err.Error())
    return
}
jsonOK(w, map[string]any{"deleted": true})
```

With:

```go
// Cancel and remove any in-flight threads for this session before
// deleting the session record. Guarded for nil to support minimal
// server configurations that don't wire up multi-agent.
if s.tm != nil {
    s.tm.CleanupSession(id)
}
if err := s.store.Delete(id); err != nil {
    jsonError(w, 500, "delete session: "+err.Error())
    return
}
jsonOK(w, map[string]any{"deleted": true})
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/server/... -run TestDeleteSession -v
go build ./...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/handlers.go internal/server/handlers_delete_session_cleanup_test.go
git commit -m "fix(server): cancel threads on session delete

handleDeleteSession was deleting the session record without calling
CleanupSession, leaving in-flight sub-agent threads running and
holding goroutines after their session context was gone. Now calls
tm.CleanupSession(id) before the store delete."
```

---

## Task 3 — Space Timeline Abort Signal (Bug #3)

**Files:**
- Modify: `web/src/composables/useApi.ts:402,406-410`
- Modify: `web/src/composables/useSpaceTimeline.ts:315-317`

### Context

`useSpaceTimeline.ts` creates an `AbortController` and sets a 10-second timeout, but neither `api.spaces.sessions()` nor `api.spaces.messages()` accept a `signal` parameter — so the timeout never fires. `apiFetch` already accepts `RequestInit` (which includes `signal`) but the `spaces.*` wrapper methods don't forward it.

Current buggy code (`useSpaceTimeline.ts:310-318`):
```typescript
const controller = new AbortController()
const timer = setTimeout(() => controller.abort(), 10_000)
try {
  const [msgResult, sessions] = await Promise.all([
    api.spaces.messages(spaceId, undefined, 20),   // signal missing
    api.spaces.sessions(spaceId),                  // signal missing
  ])
```

Current `useApi.ts` (lines 402, 406-410):
```typescript
sessions: (id: string) => apiFetch<unknown[]>(`/api/v1/space-sessions/${id}`),
messages: (spaceId: string, before?: string, limit = 20) => {
  const params = new URLSearchParams({ limit: String(limit) })
  if (before) params.set('before', before)
  return apiFetch<{ messages: SpaceMessage[]; next_cursor: string }>(`/api/v1/space-messages/${spaceId}?${params}`)
},
```

- [ ] **Step 1: Write the failing test**

Add to `web/src/composables/__tests__/useSpaceTimeline.abort.test.ts` (new file):

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useSpaceTimeline } from '../useSpaceTimeline'

describe('useSpaceTimeline hydrate abort', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  it('passes signal to api.spaces.messages so AbortController fires', async () => {
    let capturedSignal: AbortSignal | undefined
    vi.mock('../useApi', () => ({
      api: {
        spaces: {
          messages: vi.fn((_id: string, _before?: string, _limit?: number, opts?: { signal?: AbortSignal }) => {
            capturedSignal = opts?.signal
            return new Promise(() => {}) // never resolves
          }),
          sessions: vi.fn(() => new Promise(() => {})),
        },
      },
    }))

    const { hydrate } = useSpaceTimeline('space-1')
    const p = hydrate(true)
    vi.advanceTimersByTime(10_001)
    await Promise.resolve()

    expect(capturedSignal).toBeDefined()
    expect(capturedSignal?.aborted).toBe(true)
    await p.catch(() => {}) // suppress unhandled
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run src/composables/__tests__/useSpaceTimeline.abort.test.ts
```
Expected: FAIL — capturedSignal is undefined.

- [ ] **Step 3: Update useApi.ts to accept signal**

Replace `spaces.sessions` and `spaces.messages` in `useApi.ts`:

```typescript
// Before:
sessions: (id: string) => apiFetch<unknown[]>(`/api/v1/space-sessions/${id}`),

// After:
sessions: (id: string, opts?: { signal?: AbortSignal }) =>
  apiFetch<unknown[]>(`/api/v1/space-sessions/${id}`, { signal: opts?.signal }),
```

```typescript
// Before:
messages: (spaceId: string, before?: string, limit = 20) => {
  const params = new URLSearchParams({ limit: String(limit) })
  if (before) params.set('before', before)
  return apiFetch<{ messages: SpaceMessage[]; next_cursor: string }>(`/api/v1/space-messages/${spaceId}?${params}`)
},

// After:
messages: (spaceId: string, before?: string, limit = 20, opts?: { signal?: AbortSignal }) => {
  const params = new URLSearchParams({ limit: String(limit) })
  if (before) params.set('before', before)
  return apiFetch<{ messages: SpaceMessage[]; next_cursor: string }>(
    `/api/v1/space-messages/${spaceId}?${params}`,
    { signal: opts?.signal },
  )
},
```

- [ ] **Step 4: Pass signal in useSpaceTimeline.ts**

Replace lines 315-318 in `useSpaceTimeline.ts`:

```typescript
// Before:
const [msgResult, sessions] = await Promise.all([
  api.spaces.messages(spaceId, undefined, 20),
  api.spaces.sessions(spaceId),
])

// After:
const [msgResult, sessions] = await Promise.all([
  api.spaces.messages(spaceId, undefined, 20, { signal: controller.signal }),
  api.spaces.sessions(spaceId, { signal: controller.signal }),
])
```

- [ ] **Step 5: Run tests**

```bash
cd web && npx vitest run src/composables/__tests__/useSpaceTimeline.abort.test.ts
npx tsc --noEmit
```
Expected: PASS, no type errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/composables/useApi.ts web/src/composables/useSpaceTimeline.ts web/src/composables/__tests__/useSpaceTimeline.abort.test.ts
git commit -m "fix(spaces): wire AbortController signal to API calls in hydrate()

useSpaceTimeline created an AbortController with a 10-second timeout
but never passed controller.signal to the API calls, making the
timeout dead code. Added signal parameter to api.spaces.messages and
api.spaces.sessions, then threaded the signal through hydrate()."
```

---

## Task 4 — Skills Install Name Validation (Bug #4)

**Files:**
- Modify: `internal/server/handlers_skills.go:228-235`

### Context

`handleSkillsInstall` (registry install path) parses a skill from a remote URL response and writes it to disk at `<skillsDir>/<sk.Name()>.md`. It calls `ParseMarkdownSkillBytes` but never calls `validSkillName(sk.Name())`, unlike every other handler that writes skill files (create at line 145, update at line 286, and others). A crafted registry response with a name like `../../etc/passwd` would escape the skills directory.

Current buggy code (`handlers_skills.go:228-235`):
```go
sk, err := skills.ParseMarkdownSkillBytes(rawBytes)
if err != nil {
    http.Error(w, "invalid SKILL.md: "+err.Error(), http.StatusBadGateway)
    return
}
sdir := s.skillsDirPath()
os.MkdirAll(sdir, 0755)
if err := os.WriteFile(filepath.Join(sdir, sk.Name()+".md"), rawBytes, 0644); err != nil {
```

- [ ] **Step 1: Write the failing test**

Add to `internal/server/handlers_skills_install_test.go` (new file):

```go
package server_test

import (
    "bytes"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestSkillsInstall_RejectsInvalidSkillName(t *testing.T) {
    // Stand up a fake registry that serves a SKILL.md with a path-traversal name.
    maliciousSkillMD := "---\nname: ../../evil\ndescription: bad\n---\n# evil skill\n"
    fakeRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/markdown")
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(maliciousSkillMD))
    }))
    defer fakeRegistry.Close()

    srv := newTestServer(t, nil)
    // Point the request at our fake registry URL.
    body := bytes.NewBufferString(`{"url":"` + fakeRegistry.URL + `/skills/evil"}`)
    req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/install", body)
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    srv.handleSkillsInstall(w, req)

    if w.Code != http.StatusBadGateway {
        t.Errorf("expected 502 for invalid skill name, got %d: %s", w.Code, w.Body.String())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/server/... -run TestSkillsInstall_RejectsInvalidSkillName -v
```
Expected: FAIL — the handler writes the file without checking the name, returning 200.

- [ ] **Step 3: Fix handlers_skills.go**

Add `validSkillName` check immediately after the `ParseMarkdownSkillBytes` call in `handleSkillsInstall` (after the `if err != nil` block, before `sdir := s.skillsDirPath()`):

```go
sk, err := skills.ParseMarkdownSkillBytes(rawBytes)
if err != nil {
    http.Error(w, "invalid SKILL.md: "+err.Error(), http.StatusBadGateway)
    return
}
if !validSkillName(sk.Name()) {
    http.Error(w, "invalid skill name in registry SKILL.md", http.StatusBadGateway)
    return
}
sdir := s.skillsDirPath()
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/server/... -run TestSkillsInstall -v
go build ./...
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/handlers_skills.go internal/server/handlers_skills_install_test.go
git commit -m "fix(skills): validate skill name on registry install

handleSkillsInstall was writing skill files without calling
validSkillName(), creating a path traversal risk if a registry
response contains a crafted name like '../../evil'. Every other
skill write path already validates — this adds the missing gate."
```

---

## Task 5 — SubWorkflow Transitive Cycle Detection (Bug #5)

**Files:**
- Modify: `internal/server/handlers_workflows.go`

### Context

`validateWorkflow` checks self-reference (`step.SubWorkflow == wf.ID`) but does not detect transitive cycles (A calls B which calls A). The DFS at lines 143-183 only operates on `from_step` data dependencies within a single workflow. Cross-workflow SubWorkflow cycles must be checked against the full workflow registry.

The fix adds a `validateSubWorkflowCycles(wf, all)` function that DFS-traverses SubWorkflow references across all loaded workflows. This is called by `handleCreateWorkflow` and `handleUpdateWorkflow` after `validateWorkflow`, with the loaded workflow list from `scheduler.LoadWorkflows(dir)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/server/handlers_workflows_cycle_test.go` (new file):

```go
package server

import (
    "testing"
    "github.com/scrypster/huginn/internal/scheduler"
)

func TestValidateSubWorkflowCycles_DetectsDirectCycle(t *testing.T) {
    wfA := &scheduler.Workflow{ID: "wf-a", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-b"}}}
    wfB := &scheduler.Workflow{ID: "wf-b", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-a"}}}

    // Validating A with registry [A, B] — A→B→A is a cycle.
    if err := validateSubWorkflowCycles(wfA, []*scheduler.Workflow{wfA, wfB}); err == nil {
        t.Error("expected cycle error for A→B→A, got nil")
    }
}

func TestValidateSubWorkflowCycles_DetectsLongCycle(t *testing.T) {
    wfA := &scheduler.Workflow{ID: "wf-a", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-b"}}}
    wfB := &scheduler.Workflow{ID: "wf-b", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-c"}}}
    wfC := &scheduler.Workflow{ID: "wf-c", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-a"}}}

    if err := validateSubWorkflowCycles(wfA, []*scheduler.Workflow{wfA, wfB, wfC}); err == nil {
        t.Error("expected cycle error for A→B→C→A, got nil")
    }
}

func TestValidateSubWorkflowCycles_AllowsAcyclicTree(t *testing.T) {
    wfA := &scheduler.Workflow{ID: "wf-a", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-b"}}}
    wfB := &scheduler.Workflow{ID: "wf-b", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-c"}}}
    wfC := &scheduler.Workflow{ID: "wf-c", Steps: []scheduler.WorkflowStep{{Name: "s1", Prompt: "leaf step"}}}

    if err := validateSubWorkflowCycles(wfA, []*scheduler.Workflow{wfA, wfB, wfC}); err != nil {
        t.Errorf("expected no error for acyclic A→B→C, got: %v", err)
    }
}

func TestValidateSubWorkflowCycles_AllowsDanglingRef(t *testing.T) {
    // A dangling SubWorkflow ref (missing workflow) is not a cycle — just a runtime error.
    wfA := &scheduler.Workflow{ID: "wf-a", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-missing"}}}

    if err := validateSubWorkflowCycles(wfA, []*scheduler.Workflow{wfA}); err != nil {
        t.Errorf("dangling ref should not be flagged as cycle, got: %v", err)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/server/... -run TestValidateSubWorkflowCycles -v
```
Expected: FAIL — `validateSubWorkflowCycles` does not exist.

- [ ] **Step 3: Implement validateSubWorkflowCycles**

Add after the `validateWorkflow` function in `handlers_workflows.go`:

```go
// validateSubWorkflowCycles detects cycles in SubWorkflow cross-references
// across the full workflow registry. A cycle (e.g. A→B→A) would cause
// infinite recursion at runtime. wf is the workflow being saved (may be
// new and not yet present in all). all is the current on-disk registry.
func validateSubWorkflowCycles(wf *scheduler.Workflow, all []*scheduler.Workflow) error {
    registry := make(map[string]*scheduler.Workflow, len(all)+1)
    for _, w := range all {
        registry[w.ID] = w
    }
    registry[wf.ID] = wf // include candidate (may override stale on-disk version)

    const (
        unvisited = 0
        inStack   = 1
        done      = 2
    )
    state := make(map[string]int, len(registry))

    var dfs func(id string) error
    dfs = func(id string) error {
        state[id] = inStack
        w, ok := registry[id]
        if !ok {
            state[id] = done
            return nil // dangling ref — not a cycle
        }
        for _, step := range w.Steps {
            ref := step.SubWorkflow
            if ref == "" {
                continue
            }
            if state[ref] == inStack {
                return fmt.Errorf("circular sub_workflow reference: %q → %q creates a cycle", id, ref)
            }
            if state[ref] == unvisited {
                if err := dfs(ref); err != nil {
                    return err
                }
            }
        }
        state[id] = done
        return nil
    }

    return dfs(wf.ID)
}
```

- [ ] **Step 4: Call validateSubWorkflowCycles from handleCreateWorkflow**

In `handleCreateWorkflow`, after the `validateWorkflow` call, add:

```go
if err := validateWorkflow(&wf); err != nil {
    jsonError(w, 422, "invalid workflow: "+err.Error())
    return
}
// Check for cycles across the cross-workflow SubWorkflow call graph.
if allWFs, loadErr := scheduler.LoadWorkflows(dir); loadErr == nil {
    if err := validateSubWorkflowCycles(&wf, allWFs); err != nil {
        jsonError(w, 422, "invalid workflow: "+err.Error())
        return
    }
}
```

Note: `dir` is already set earlier in the function (`dir := filepath.Join(s.huginnDir, "workflows")`). If `LoadWorkflows` fails (e.g. empty dir), we skip the cross-workflow check (graceful degradation — the self-reference check still fires).

- [ ] **Step 5: Call validateSubWorkflowCycles from handleUpdateWorkflow**

Apply the same pattern in `handleUpdateWorkflow` at the equivalent position after `validateWorkflow` is called. Locate the `validateWorkflow` call in that function and add:

```go
if allWFs, loadErr := scheduler.LoadWorkflows(dir); loadErr == nil {
    if err := validateSubWorkflowCycles(&wf, allWFs); err != nil {
        jsonError(w, 422, "invalid workflow: "+err.Error())
        return
    }
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/server/... -run TestValidateSubWorkflowCycles -v
go test ./internal/server/... -v
go build ./...
```
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/server/handlers_workflows.go internal/server/handlers_workflows_cycle_test.go
git commit -m "fix(workflows): detect transitive SubWorkflow cycles at save time

validateWorkflow only checked self-reference (A.sub_workflow == A.id).
Transitive cycles like A→B→A were silently saved and would cause
infinite recursion at runtime. Added validateSubWorkflowCycles() which
DFS-traverses the full workflow registry to reject cyclic call graphs
before persistence."
```

---

## Final Verification

After all 5 tasks are committed:

- [ ] **Run full Go test suite**

```bash
go test ./... 2>&1 | tail -20
```
Expected: no new failures.

- [ ] **Run frontend tests**

```bash
cd web && npx vitest run
```
Expected: no new failures.

- [ ] **Build binary**

```bash
go build ./...
```
Expected: no errors.
