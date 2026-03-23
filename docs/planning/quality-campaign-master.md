# Huginn Quality Campaign — Target: 95%+ on All Features

**Status:** ROUND 2 COMPLETE
**Goal:** All features 95%+ (target 98%)
**Workflow:** Haiku research → Sonnet proposal → Opus gate → Implement

---

## Issue Registry

### Round 1 — P0 CRITICAL

| ID | Area | File | Issue | Status |
|----|------|------|-------|--------|
| C1 | Workflows | `handlers_workflows.go:331` | `_ = sched.RegisterWorkflow(&updates)` silently swallows errors on update | ✅ FIXED |
| C2 | Agents UI | `AgentsView.vue:1614` | Tool loading errors logged but never shown to user | ✅ FIXED |
| C3 | Search | `hybrid.go` | Embedding errors silently `continue` with no log | ✅ FIXED |

### Round 1 — P1 HIGH

| ID | Area | File | Issue | Status |
|----|------|------|-------|--------|
| H1 | Workflows | `handlers_workflows.go` | Template endpoint `GET /api/v1/workflows/templates` not implemented | ✅ FALSE POSITIVE — already implemented |
| H2 | Agents | `mcp_agent_chat.go` | MCP vault is one-shot — no mid-session reconnect | ✅ FIXED (Option A: graceful degradation + StreamWarning) |
| H3 | Search | `bm25.go` | O(n²) bubble sort degrades at corpus scale | ✅ FIXED |

### Round 1 — P2 MEDIUM

| ID | Area | File | Issue | Status |
|----|------|------|-------|--------|
| M1 | Chat | `threadmgr/manager.go` | `statusChangeHooks` called without panic recovery | ✅ FIXED |
| M2 | Workflows UI | `WorkflowsView.vue` | No validation error toasts on save failure | ✅ FIXED |
| M3 | Web UI | `AgentsView.vue` | General error propagation to user missing in multiple spots | ✅ FIXED |
| M4 | E2E Tests | `web/e2e/` | Error states and edge cases not tested | DEFERRED |

### Round 2 — All Issues

| ID | Area | File | Issue | Status |
|----|------|------|-------|--------|
| W1 | Workflows | `handlers_workflows.go` | `validateWorkflow()` missing `step.Validate()` + `ValidateCronSchedule()` | ✅ FIXED |
| A1 | Agents UI | `AgentsView.vue` | `deleteAgent()` no `resp.ok` check — removes from UI even on 500 | ✅ FIXED |
| A2 | Agents UI | `AgentsView.vue` | `save()` no pre-flight name validation before API call | ✅ FIXED |
| I1 | Infrastructure | `handlers.go:853,875` | `http.Error()` sends text/plain instead of JSON for two error paths | ✅ FIXED |
| I2 | Infrastructure | `logger.go` | `rotate()` silently ignores `os.Rename()` failures — critical data loss risk | ✅ FIXED |
| U1 | Web UI | `InboxView.vue` | Bulk actions (`markAllSeen`, `dismissAll`) have no try/catch or processing guard | ✅ FIXED |
| U2 | Web UI | `App.vue` | WS degradation/reconnecting state never surfaced to user | ✅ FIXED |
| U3 | Web UI | `SkillsView.vue` | `installSkill()` / `installCollection()` silently drop errors | ✅ FIXED |
| U4 | Web UI | `ModelsView.vue` | `deleteOllamaModel()` empty catch — silent failure on delete | ✅ FIXED |
| C1 | Chat | `useThreads.ts:256` | Silent catch in `loadThreads()` — no error observable at any layer | ✅ FIXED |

---

## Feature Score Tracker

| Feature | Round 0 | Round 1 | Round 2 | Round 3 | Delta R3 |
|---------|---------|---------|---------|---------|----------|
| Chat | 85% | 93% | 96% | 98% | +2% |
| Workflows/Automation | 85% | 93% | 97% | 97% | — |
| Agents | 83% | 92% | 96% | 96% | — |
| Skills | 95% | 95% | 97% | 97% | — |
| Connections | 95% | 95% | 95% | 98% | +3% |
| Infrastructure | 89% | 93% | 97% | 98% | +1% |
| Web UI | 85% | 92% | 97% | 98% | +1% |
| **Overall** | **86%** | **93%** | **96%** | **98%** | **+2%** |

All features now 95%+. Round 2 target achieved.

---

## Round 3 Issues

| ID | Area | File | Issue | Status |
|----|------|------|-------|--------|
| Con1 | Connections | `ConnectionsView.vue:416` | `loadMuninnStatus()` silent catch — errors invisible, state not reset | ✅ FIXED |
| Chat1 | Chat | `ChatView.vue:977` | `threadsError` exported but never imported or rendered | ✅ FIXED |
| Stats1 | Stats | `StatsView.vue:250` | `error.value = ''` runs unconditionally — blank dashboard when server unreachable | ✅ FIXED |
| Logs1 | Logs | `LogsView.vue:200` | `rawLines.value = data.lines` without null guard — type becomes `undefined` | ✅ FIXED |
| Api1 | Infrastructure | `useApi.ts:182` | `res.json()` throws opaque `SyntaxError` when server returns HTML on 200 OK | ✅ FIXED |

---

## Round 2 Changes (Opus-approved, all tests passing)

### W1 — handlers_workflows.go
- Added per-step `step.Validate()` call inside the step iteration loop
- Errors accumulated (all steps checked) — returns all failures in one response
- Step errors prefixed with step name/index for debuggability
- Added `ValidateCronSchedule()` call when `wf.Schedule != ""`
- Note: `step.Validate()` pre-populates `retryDelayParsed`/`timeoutParsed` cache fields — intentional and beneficial

### A1 — AgentsView.vue (deleteAgent)
- Capture `resp` from `fetch()`; check `resp.ok` before any UI mutation
- Non-ok: parse body with `.json().catch(() => ({error:'Delete failed'}))` fallback; set error refs; return
- `removeFromList()` now only called on confirmed success
- Catch handles network errors separately (`'Network error'` message)

### A2 — AgentsView.vue (save pre-flight)
- Added `validateAgentForm()` helper: checks empty name and path-unsafe chars (`/`, `\`, `:`, null, control chars)
- `save()` calls it first; returns early with error state if invalid
- Prevents confusing 404/500 from empty-name or slash-containing API calls

### I1 — handlers.go
- Replaced `http.Error(w, '{"error":"..."}', ...)` with `jsonError(w, status, "...")` at lines 853 and 875
- `jsonError()` (middleware.go) sets Content-Type: application/json before WriteHeader — verified correct
- Ensures all error responses are JSON regardless of code path

### I2 — logger.go
- Added error handling to all three `os.Rename()` calls in `rotate()`
- `.2→.3` and `.1→.2` failures: log to stderr, tolerable, continue
- `current→.1` failure: CRITICAL — log to stderr, reopen existing file in append mode, return without creating new file
- Prevents silent write loss when log rotation fails on full disk or permission errors

### U1 — InboxView.vue
- Added `inboxError = ref<string|null>(null)` and `isBulkProcessing = ref(false)`
- All four async functions (`handleAction`, `handleChat`, `markAllSeen`, `dismissAll`) wrapped in try/catch
- `markAllSeen` and `dismissAll` guard against double-submit via `isBulkProcessing`
- Buttons disabled during bulk processing (`disabled:opacity-40 disabled:cursor-not-allowed`)
- Error banner renders below header with dismiss button

### U2 — App.vue
- Added `wsConnectionState` computed from `wsRef.value?.connectionState.value`
- `showDegradedBanner` ref + 4-second debounce timer — avoids flicker on brief blips
- Banner auto-clears when `connectionState` returns to `'connected'`
- `onUnmounted()` cleanup prevents stale ref writes on destroyed component
- Banner uses `<Teleport to="body">` with `z-[9999]` for reliable overlay above all UI

### U3 — SkillsView.vue
- Added `installError = ref<string|null>(null)` and `installLoading = ref(false)`
- `installSkill()`: full try/catch + loading state; error set on failure
- `installCollection()`: sequential install loop with per-skill failure tracking; names of failed skills reported (`"Failed to install: skill-a, skill-b"`); `installed.load()` called once after loop
- Install button disabled and shows "Installing…" text during operation
- Error banner rendered in collection detail panel

### U4 — ModelsView.vue
- Added `deleteError = ref<string|null>(null)`
- `deleteOllamaModel()`: clears `deleteError` at top; sets on catch with descriptive message
- Error banner rendered below pull feedback section with dismiss button

### C1 — useThreads.ts
- Added `threadsError = ref<string|null>(null)` at module level
- `loadThreads()` catch: `console.warn('huginn: loadThreads failed', e)` for DevTools observability
- Sets `threadsError.value = 'Could not load threads'` for views to consume
- Sets `threadsError.value = null` on successful load (self-healing)
- `threadsError` exported from `useThreads()` return value

---

## Architecture Decisions

### Round 1 Opus Review
- Round 1: C2a rejected (shared state race), H2 rejected (underspecified)
- Round 2: C2a revised (separate loadError refs), H2 revised (full spec) → ARCHITECTURE_APPROVED

### Round 2 Opus Review
- Round 1 proposal: rejected on 6 of 10 items (error accumulation, resp.ok control flow, char validation, I2 abort logic, bulk guard, banner debounce, named failure report)
- Round 2 revised proposal: all 6 addressed + 4 additional refinements (char set widened, onUnmounted cleanup, l.size note addressed, C1 committed approach)
- Opus conditionally approved with 4 final surgical changes (step name prefix, char set, I2 size reset n/a, onUnmounted) → implemented → ARCHITECTURE_APPROVED

### Server-side Agent Name Validation
- Client-side: validates empty, slash, backslash, colon, null, control chars
- Server-side: only checks `name == ""` (no character-level validation)
- Mitigation: Go `http.ServeMux` PathValue prevents slash injection at transport layer
- **TODO**: Add regex validation to `handleUpdateAgent` and `handleDeleteAgent` for null bytes and control chars (separate ticket)
