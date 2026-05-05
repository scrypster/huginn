# Upgrade Lifecycle — Enterprise-Grade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every upgrade path — direct binary, `huginn upgrade` on Homebrew, and raw `brew upgrade huginn` — cleanly stops the old server, installs the new binary, starts the new server, and confirms it's up; if a silent upgrade happens, the UI detects it and offers one-click restart.

**Architecture:** Fix 1 closes the missing health-check in the Homebrew upgrade path (one call in `upgradeViaHomebrew`). Fix 2 adds a background `StaleWatcher` that detects on-disk binary replacement, exposes `stale: true` on `/api/v1/health`, and handles `POST /api/v1/restart` via `syscall.Exec` (Unix) — the UI polls health, shows a banner, and calls restart. Fix 3 adds a `service do` block to the Homebrew formula so `brew upgrade` auto-restarts when running as a brew service.

**Tech Stack:** Go 1.22 (`sync/atomic`, `syscall.Exec`, `os.Executable`), Vue 3 Composition API, TypeScript, Homebrew Ruby DSL.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `upgrade.go` | Modify | Add `waitForServerHealth` call to `upgradeViaHomebrew` |
| `internal/server/stale_watcher.go` | Create | `StaleWatcher` — polls binary mtime, exposes `IsStale()` |
| `internal/server/stale_watcher_test.go` | Create | Tests for `StaleWatcher` |
| `internal/server/restart_unix.go` | Create | `platformExec` — `syscall.Exec` implementation |
| `internal/server/restart_windows.go` | Create | `platformExec` — unsupported stub |
| `internal/server/server.go` | Modify | Add `staleWatcher` field; start watcher in `Start()`; register restart route |
| `internal/server/handlers.go` | Modify | Add `stale` to health response; add `handleRestart` |
| `web/src/composables/useApi.ts` | Modify | Add `stale` to health type; add `api.restart()` |
| `web/src/composables/useVersion.ts` | Modify | Add 60s health polling; expose `stale` ref |
| `web/src/composables/__tests__/useVersion.test.ts` | Modify | Add tests for polling and stale ref |
| `web/src/App.vue` | Modify | Add stale banner with Restart Now button |
| `scrypster/homebrew-tap: Formula/huginn.rb` | Modify | Add `service do` block (separate repo, separate PR) |

---

## Task 1: Fix Homebrew Upgrade Path Health Check

**Files:**
- Modify: `upgrade.go` — `upgradeViaHomebrew()` around line 729

The Homebrew upgrade path restarts the server daemon but never polls health. This task adds the same health check already present in `selfUpdateWithURLs`.

- [ ] **Step 1: Locate the restart block in `upgradeViaHomebrew`**

Read `upgrade.go` around line 729. You will find:
```go
if state.serveRunning {
    if err := u.step("Restarting server...", func() error {
        cmd := exec.Command(exePath, "serve")
        cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
        return u.DetachStart(cmd)
    }); err != nil {
        fmt.Fprintf(u.out(), "\n  Warning: server did not restart: %v\n  Run: huginn serve\n\n", err)
    }
}
```

- [ ] **Step 2: Add health check after successful restart**

Replace the entire `if state.serveRunning` block in `upgradeViaHomebrew` with:
```go
if state.serveRunning {
    if err := u.step("Restarting server...", func() error {
        cmd := exec.Command(exePath, "serve")
        cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
        return u.DetachStart(cmd)
    }); err != nil {
        fmt.Fprintf(u.out(), "\n  Warning: server did not restart: %v\n  Run: huginn serve\n\n", err)
    } else {
        port := 8421
        if u.ServerPortFn != nil {
            port = u.ServerPortFn()
        }
        u.waitForServerHealth(port)
    }
}
```

- [ ] **Step 3: Build**

```bash
go build ./...
```
Expected: no output (clean build).

- [ ] **Step 4: Run upgrade tests**

```bash
go test -run "TestUpgradeViaHomebrew" -v -timeout 30s
```
Expected: all `TestUpgradeViaHomebrew_*` tests pass.

- [ ] **Step 5: Commit**

```bash
git add upgrade.go
git commit -m "fix(upgrade): add health check after Homebrew daemon restart"
```

---

## Task 2: StaleWatcher — Backend

**Files:**
- Create: `internal/server/stale_watcher.go`
- Create: `internal/server/stale_watcher_test.go`

`StaleWatcher` records the mtime of the running binary at construction time and polls every 60 seconds. If the mtime changes (a new binary was installed), it sets an atomic flag.

- [ ] **Step 1: Write the failing test**

Create `internal/server/stale_watcher_test.go`:

```go
package server

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"
)

func TestStaleWatcher_NotStaleInitially(t *testing.T) {
    // Point at a real file; don't modify it — should not be stale.
    exe, err := os.Executable()
    if err != nil {
        t.Fatal(err)
    }
    w, err := newStaleWatcher(exe)
    if err != nil {
        t.Fatal(err)
    }
    if w.IsStale() {
        t.Error("expected IsStale()=false immediately after construction")
    }
}

func TestStaleWatcher_DetectsChange(t *testing.T) {
    // Write a temp file; record its mtime; touch it; watcher should go stale.
    dir := t.TempDir()
    f := filepath.Join(dir, "huginn")
    if err := os.WriteFile(f, []byte("v1"), 0755); err != nil {
        t.Fatal(err)
    }

    staleCheckInterval = 50 * time.Millisecond
    t.Cleanup(func() { staleCheckInterval = 60 * time.Second })

    w, err := newStaleWatcher(f)
    if err != nil {
        t.Fatal(err)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    w.Start(ctx)

    // Ensure at least one poll has run before modifying the file.
    time.Sleep(100 * time.Millisecond)

    // Advance mtime by touching the file.
    now := time.Now().Add(2 * time.Second)
    if err := os.Chtimes(f, now, now); err != nil {
        t.Fatal(err)
    }

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        if w.IsStale() {
            return // pass
        }
        time.Sleep(20 * time.Millisecond)
    }
    t.Error("expected IsStale()=true after binary mtime changed")
}

func TestStaleWatcher_NoChangeNoStale(t *testing.T) {
    dir := t.TempDir()
    f := filepath.Join(dir, "huginn")
    if err := os.WriteFile(f, []byte("v1"), 0755); err != nil {
        t.Fatal(err)
    }

    staleCheckInterval = 50 * time.Millisecond
    t.Cleanup(func() { staleCheckInterval = 60 * time.Second })

    w, err := newStaleWatcher(f)
    if err != nil {
        t.Fatal(err)
    }
    ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
    defer cancel()
    w.Start(ctx)

    time.Sleep(250 * time.Millisecond)
    if w.IsStale() {
        t.Error("expected IsStale()=false when file is not modified")
    }
}
```

- [ ] **Step 2: Run failing test**

```bash
go test ./internal/server/ -run "TestStaleWatcher" -v -timeout 15s
```
Expected: FAIL — `newStaleWatcher` undefined.

- [ ] **Step 3: Implement `stale_watcher.go`**

Create `internal/server/stale_watcher.go`:

```go
package server

import (
    "context"
    "log/slog"
    "os"
    "sync/atomic"
    "time"
)

// staleCheckInterval is the poll cadence. Package-level var so tests can
// override it without modifying the constructor.
var staleCheckInterval = 60 * time.Second

// StaleWatcher detects when the on-disk binary has been replaced (e.g. by
// `brew upgrade huginn`) while the server is running. It records the binary's
// mtime at construction time and polls periodically. Once stale, it stays stale
// until the process is restarted.
type StaleWatcher struct {
    exePath   string
    baseMtime time.Time
    stale     atomic.Bool
}

// newStaleWatcher creates a StaleWatcher for the binary at exePath.
// exePath should already be resolved through symlinks (call
// filepath.EvalSymlinks before passing).
func newStaleWatcher(exePath string) (*StaleWatcher, error) {
    info, err := os.Stat(exePath)
    if err != nil {
        return nil, err
    }
    return &StaleWatcher{
        exePath:   exePath,
        baseMtime: info.ModTime(),
    }, nil
}

// Start launches the background polling goroutine. It stops when ctx is done.
func (w *StaleWatcher) Start(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(staleCheckInterval)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                w.check()
            }
        }
    }()
}

// IsStale reports whether the on-disk binary has been replaced since startup.
func (w *StaleWatcher) IsStale() bool {
    return w.stale.Load()
}

func (w *StaleWatcher) check() {
    info, err := os.Stat(w.exePath)
    if err != nil {
        slog.Debug("stale watcher: stat error", "path", w.exePath, "err", err)
        return
    }
    if !info.ModTime().Equal(w.baseMtime) {
        if w.stale.CompareAndSwap(false, true) {
            slog.Warn("huginn binary has been updated on disk — restart to activate",
                "path", w.exePath)
        }
    }
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/server/ -run "TestStaleWatcher" -v -timeout 15s
```
Expected: all 3 `TestStaleWatcher_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/stale_watcher.go internal/server/stale_watcher_test.go
git commit -m "feat(server): StaleWatcher detects on-disk binary replacement"
```

---

## Task 3: Platform-Specific Exec

**Files:**
- Create: `internal/server/restart_unix.go`
- Create: `internal/server/restart_windows.go`

`platformExec` replaces the current process with the on-disk binary. On Unix this is `syscall.Exec`; on Windows it is unsupported.

- [ ] **Step 1: Create `restart_unix.go`**

```go
//go:build !windows

package server

import (
    "os"
    "syscall"
)

// platformExec replaces the current process image with the binary at exePath,
// passing the original args and environment. On success it never returns.
// This is the Unix exec(2) system call.
func platformExec(exePath string, args []string, env []string) error {
    return syscall.Exec(exePath, args, env)
}

// execSupported reports whether in-place restart is available on this platform.
const execSupported = true

// currentExePath returns the resolved path of the running binary,
// following symlinks (important for Homebrew installs).
func currentExePath() (string, error) {
    exe, err := os.Executable()
    if err != nil {
        return "", err
    }
    return realPath(exe)
}
```

- [ ] **Step 2: Create `restart_windows.go`**

```go
//go:build windows

package server

import (
    "errors"
    "os"
    "path/filepath"
)

// platformExec is not supported on Windows; callers should use the
// browser-redirect upgrade path instead.
func platformExec(_ string, _ []string, _ []string) error {
    return errors.New("in-place restart is not supported on Windows")
}

const execSupported = false

func currentExePath() (string, error) {
    exe, err := os.Executable()
    if err != nil {
        return "", err
    }
    return filepath.EvalSymlinks(exe)
}
```

> **Note:** `realPath` on Unix is `filepath.EvalSymlinks`. Add a tiny helper in `restart_unix.go`:

- [ ] **Step 3: Add `realPath` helper to `restart_unix.go`**

Append to `restart_unix.go`:
```go

import "path/filepath"

func realPath(p string) (string, error) {
    return filepath.EvalSymlinks(p)
}
```

Wait — imports can't be added in the middle. Replace the entire `restart_unix.go` content with the correct single file:

```go
//go:build !windows

package server

import (
    "os"
    "path/filepath"
    "syscall"
)

// platformExec replaces the current process image with the binary at exePath,
// passing the original args and environment. On success it never returns.
func platformExec(exePath string, args []string, env []string) error {
    return syscall.Exec(exePath, args, env)
}

// execSupported reports whether in-place restart is available on this platform.
const execSupported = true

// currentExePath returns the resolved path of the running binary,
// following symlinks (important for Homebrew installs).
func currentExePath() (string, error) {
    exe, err := os.Executable()
    if err != nil {
        return "", err
    }
    return filepath.EvalSymlinks(exe)
}
```

And replace the entire `restart_windows.go` with:

```go
//go:build windows

package server

import (
    "errors"
    "os"
    "path/filepath"
)

func platformExec(_ string, _ []string, _ []string) error {
    return errors.New("in-place restart is not supported on Windows")
}

const execSupported = false

func currentExePath() (string, error) {
    exe, err := os.Executable()
    if err != nil {
        return "", err
    }
    return filepath.EvalSymlinks(exe)
}
```

- [ ] **Step 4: Build**

```bash
go build ./internal/server/...
```
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/server/restart_unix.go internal/server/restart_windows.go
git commit -m "feat(server): platform-specific exec for in-place restart"
```

---

## Task 4: Wire StaleWatcher + Restart Handler + Routes

**Files:**
- Modify: `internal/server/server.go` — add `staleWatcher` field, start in `Start()`; register route
- Modify: `internal/server/handlers.go` — add `stale` to health; add `handleRestart`

### Part A: handlers.go

- [ ] **Step 1: Write failing test for stale field in health**

Add to `internal/server/handlers_gaps_test.go` (or create `internal/server/handlers_restart_test.go`):

```go
package server

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestHandleHealth_StaleField(t *testing.T) {
    s := newTestServer(t)

    // No stale watcher — stale should default false.
    req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
    rr := httptest.NewRecorder()
    s.handleHealth(rr, req)

    var body map[string]any
    if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
        t.Fatal(err)
    }
    if _, ok := body["stale"]; !ok {
        t.Error("expected 'stale' key in health response")
    }
    if body["stale"] != false {
        t.Errorf("expected stale=false, got %v", body["stale"])
    }
}

func TestHandleRestart_UnsupportedPlatform(t *testing.T) {
    if execSupported {
        t.Skip("skipping unsupported-platform test on this platform")
    }
    s := newTestServer(t)
    req := httptest.NewRequest(http.MethodPost, "/api/v1/restart", nil)
    req.Header.Set("Authorization", "Bearer "+s.token)
    rr := httptest.NewRecorder()
    s.handleRestart(rr, req)
    if rr.Code != http.StatusServiceUnavailable {
        t.Errorf("expected 503, got %d", rr.Code)
    }
}
```

- [ ] **Step 2: Run failing test**

```bash
go test ./internal/server/ -run "TestHandleHealth_StaleField|TestHandleRestart" -v -timeout 15s
```
Expected: FAIL — `handleRestart` undefined.

- [ ] **Step 3: Add `stale` to `handleHealth` in `handlers.go`**

Find the `health` map in `handleHealth` (around line 84):
```go
health := map[string]any{
    "status":              "ok",
    "version":             ver,
    "satellite_connected": satConnected,
    "relay":               relayInfo,
}
```

Replace with:
```go
stale := false
if s.staleWatcher != nil {
    stale = s.staleWatcher.IsStale()
}
health := map[string]any{
    "status":              "ok",
    "version":             ver,
    "stale":               stale,
    "satellite_connected": satConnected,
    "relay":               relayInfo,
}
```

- [ ] **Step 4: Add `handleRestart` to `handlers.go`**

Add after `handleHealth`:

```go
// handleRestart handles POST /api/v1/restart.
// It replaces the current process in-place with the on-disk binary via
// syscall.Exec (Unix only). On Windows it returns 503.
// The response is only sent when exec fails — on success the process is
// replaced before the handler returns.
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
    if !execSupported {
        jsonError(w, http.StatusServiceUnavailable,
            "in-place restart is not supported on this platform; restart huginn manually")
        return
    }
    exePath, err := currentExePath()
    if err != nil {
        jsonError(w, http.StatusInternalServerError, "could not resolve binary path: "+err.Error())
        return
    }
    // Acknowledge before exec so the client sees a response on slow paths.
    // In the success case, exec replaces us before the goroutine finishes —
    // that's fine; the response is already flushed.
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusAccepted)
    w.Write([]byte(`{"status":"restarting"}` + "\n")) //nolint:errcheck
    if f, ok := w.(http.Flusher); ok {
        f.Flush()
    }
    if err := platformExec(exePath, os.Args, os.Environ()); err != nil {
        // exec failed — log but we can't write to w anymore (headers sent).
        slog.Error("restart: exec failed", "err", err)
    }
}
```

Add `"os"` to the imports in `handlers.go` if not already present, and add `"log/slog"`.

- [ ] **Step 5: Add `staleWatcher` field to `Server` struct in `server.go`**

In `server.go`, find the `Server` struct (line 46). Add after the existing fields, before the closing `}`:

```go
staleWatcher *StaleWatcher // nil if binary is a symlink we can't stat, or tests
```

Add it after `satellite *relay.Satellite` (around line 121) to keep related infrastructure together:
```go
satellite    *relay.Satellite // nil if not registered with HuginnCloud
outbox       *relay.Outbox    // nil if outbox not wired (no store path)
staleWatcher *StaleWatcher    // nil if stat failed at startup; detects silent binary replacement
```

- [ ] **Step 6: Start the StaleWatcher in `Start()` in `server.go`**

In `Start()` (line 352), add after `go s.evictSwarmSnapshots(ctx)`:

```go
// Start stale-binary watcher so the UI can prompt for restart after
// `brew upgrade huginn` or any silent binary replacement.
if exePath, err := currentExePath(); err == nil {
    if sw, err := newStaleWatcher(exePath); err == nil {
        s.staleWatcher = sw
        s.staleWatcher.Start(ctx)
        // If already stale before the server started (binary replaced while
        // the server was stopped then restarted with the old binary), warn.
        if s.staleWatcher.IsStale() {
            slog.Warn("huginn binary on disk differs from running binary — restart to activate")
        }
    }
}
```

- [ ] **Step 7: Register restart route in `registerRoutes()` in `server.go`**

In `registerRoutes`, after the health route (line 984):
```go
mux.HandleFunc("GET /api/v1/health", loggingMiddleware(requestIDMiddleware(s.handleHealth)))
```

Add:
```go
mux.HandleFunc("POST /api/v1/restart", api(s.handleRestart))
```

- [ ] **Step 8: Build**

```bash
go build ./...
```
Expected: no output.

- [ ] **Step 9: Run tests**

```bash
go test ./internal/server/ -run "TestHandleHealth_StaleField|TestHandleRestart|TestStaleWatcher" -v -timeout 30s
```
Expected: all pass.

- [ ] **Step 10: Run full server test suite**

```bash
go test ./internal/server/... -timeout 120s
```
Expected: all pass.

- [ ] **Step 11: Commit**

```bash
git add internal/server/server.go internal/server/handlers.go internal/server/handlers_restart_test.go
git commit -m "feat(server): stale watcher, health stale field, POST /api/v1/restart"
```

---

## Task 5: Frontend — API Types + useVersion Polling

**Files:**
- Modify: `web/src/composables/useApi.ts` — add `stale` to health type; add `api.restart()`
- Modify: `web/src/composables/useVersion.ts` — add 60s polling; expose `stale` ref
- Modify: `web/src/composables/__tests__/useVersion.test.ts` — add tests

- [ ] **Step 1: Write failing tests for `useVersion` polling and stale**

Add to `web/src/composables/__tests__/useVersion.test.ts`:

```typescript
describe('useVersion polling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('stale is false initially', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ version: 'v0.3.1', stale: false }),
    } as Response)
    const { stale, loadVersion } = useVersion()
    await loadVersion()
    expect(stale.value).toBe(false)
  })

  it('stale becomes true when health returns stale:true', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue({
      ok: true,
      json: async () => ({ version: 'v0.3.1', stale: true }),
    } as Response)
    const { stale, loadVersion } = useVersion()
    await loadVersion()
    expect(stale.value).toBe(true)
  })

  it('polls health every 60 seconds and updates stale', async () => {
    let callCount = 0
    vi.spyOn(global, 'fetch').mockImplementation(async () => {
      callCount++
      return {
        ok: true,
        json: async () => ({ version: 'v0.3.1', stale: callCount > 1 }),
      } as Response
    })
    const { stale, loadVersion, startPolling } = useVersion()
    await loadVersion()
    startPolling()
    expect(stale.value).toBe(false)

    // Advance 60 seconds — second poll fires
    await vi.advanceTimersByTimeAsync(60_000)
    expect(stale.value).toBe(true)
  })
})
```

- [ ] **Step 2: Run failing test**

```bash
cd web && npx vitest run src/composables/__tests__/useVersion.test.ts 2>&1 | tail -20
```
Expected: FAIL — `stale` and `startPolling` not exported from `useVersion`.

- [ ] **Step 3: Update `useApi.ts` health type and add `api.restart()`**

In `web/src/composables/useApi.ts`, line 267, change:
```typescript
health: () => apiFetch<{ status: string; version: string; satellite_connected: boolean; backend_status: string }>('/api/v1/health'),
```
to:
```typescript
health: () => apiFetch<{ status: string; version: string; stale?: boolean; satellite_connected: boolean; backend_status: string }>('/api/v1/health'),
restart: () => apiFetch<{ status: string }>('/api/v1/restart', { method: 'POST' }),
```

- [ ] **Step 4: Update `useVersion.ts` to add polling and stale ref**

Replace the full contents of `web/src/composables/useVersion.ts` with:

```typescript
import { ref, computed } from 'vue'
import { api } from './useApi'

const version = ref<string>('')
const stale = ref<boolean>(false)
let inflight: Promise<void> | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null

export function useVersion() {
  async function loadVersion(): Promise<void> {
    if (version.value && !stale.value) return
    if (inflight) return inflight

    inflight = (async () => {
      try {
        const h = await api.health()
        if (typeof h.version === 'string' && h.version.length > 0) {
          version.value = h.version
        }
        stale.value = h.stale ?? false
      } catch {
        // swallow — missing version label is preferable to a noisy error
      } finally {
        inflight = null
      }
    })()

    return inflight
  }

  function startPolling(): void {
    if (pollTimer !== null) return
    pollTimer = setInterval(async () => {
      try {
        const h = await api.health()
        stale.value = h.stale ?? false
        if (typeof h.version === 'string' && h.version.length > 0) {
          version.value = h.version
        }
      } catch {
        // ignore — server may be restarting
      }
    }, 60_000)
  }

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  const versionLabel = computed(() => version.value || '…')

  return { version, versionLabel, stale, loadVersion, startPolling, stopPolling }
}
```

- [ ] **Step 5: Run tests**

```bash
cd web && npx vitest run src/composables/__tests__/useVersion.test.ts 2>&1 | tail -20
```
Expected: all tests pass.

- [ ] **Step 6: Run full frontend test suite**

```bash
cd web && npx vitest run 2>&1 | tail -20
```
Expected: all pass (no regressions from useVersion changes).

- [ ] **Step 7: Commit**

```bash
git add web/src/composables/useApi.ts web/src/composables/useVersion.ts web/src/composables/__tests__/useVersion.test.ts
git commit -m "feat(frontend): health stale type, api.restart(), useVersion polling"
```

---

## Task 6: Frontend — Stale Banner in App.vue

**Files:**
- Modify: `web/src/App.vue` — import `stale`, `startPolling`, `stopPolling`; add banner template + logic

**Context:** `App.vue` already imports `useVersion` at line 864 and calls `loadVersion()` in `onMounted`. The banner goes at the very top of the `<template>` layout, above all other content.

- [ ] **Step 1: Add `stale`, `startPolling`, `stopPolling` to the `useVersion` destructuring in `App.vue`**

Find in `App.vue` (around line 1179):
```typescript
const { versionLabel, loadVersion } = useVersion()
```

Replace with:
```typescript
const { versionLabel, stale, loadVersion, startPolling, stopPolling } = useVersion()
```

- [ ] **Step 2: Start polling and add restart logic in the `onMounted` block**

Find the existing call `void loadVersion()` in `onMounted` (around line 1393). Add `startPolling()` after it:

```typescript
void loadVersion()
startPolling()
```

Also add `onUnmounted` import if not present, and call `stopPolling` on unmount. Find the imports section (around line 864) and ensure `onUnmounted` is imported from `vue`. Then find or add an `onUnmounted` call:
```typescript
onUnmounted(() => {
  stopPolling()
})
```

- [ ] **Step 3: Add restart banner refs and logic to the script section**

Add near the other `ref` declarations in the `<script setup>` block (after the `useVersion` destructuring):

```typescript
const restartDismissed = ref(false)
const restarting = ref(false)

async function handleRestartNow(): Promise<void> {
  restarting.value = true
  try {
    await api.restart()
  } catch {
    // 202 Accepted triggers a fetch error in some clients — that's fine,
    // the server is restarting. Continue polling.
  }
  // Poll health every 500ms until server responds with stale:false,
  // then reload the page to load the new frontend assets.
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    await new Promise(resolve => setTimeout(resolve, 500))
    try {
      const h = await api.health()
      if (!h.stale) {
        window.location.reload()
        return
      }
    } catch {
      // server mid-restart — keep polling
    }
  }
  // Timed out — reload anyway to show whatever is up
  window.location.reload()
}
```

- [ ] **Step 4: Add the banner template**

Find the root element of the template in `App.vue`. It will look something like `<div id="app" ...>` or `<div class="...">`. Add the banner as the very first child:

```html
<!-- Stale binary banner: shown when brew upgrade installed a new binary -->
<Transition name="slide-down">
  <div
    v-if="stale && !restartDismissed && !restarting"
    class="fixed top-0 left-0 right-0 z-[9999] flex items-center justify-between gap-3 bg-blue-600 px-4 py-2 text-sm text-white shadow-md"
  >
    <div class="flex items-center gap-2">
      <svg class="h-4 w-4 shrink-0" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1M12 12V4m0 0L8 8m4-4l4 4"/>
      </svg>
      <span>A new version of huginn has been installed.</span>
    </div>
    <div class="flex items-center gap-2 shrink-0">
      <button
        class="rounded bg-white/20 px-3 py-1 font-medium hover:bg-white/30 transition-colors"
        @click="handleRestartNow"
      >
        Restart Now
      </button>
      <button
        class="rounded p-1 hover:bg-white/20 transition-colors"
        aria-label="Dismiss"
        @click="restartDismissed = true"
      >
        <svg class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/>
        </svg>
      </button>
    </div>
  </div>
</Transition>

<!-- Restarting overlay -->
<Transition name="fade">
  <div
    v-if="restarting"
    class="fixed top-0 left-0 right-0 z-[9999] flex items-center justify-center gap-2 bg-blue-600 px-4 py-2 text-sm text-white shadow-md"
  >
    <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
      <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"/>
    </svg>
    <span>Restarting huginn…</span>
  </div>
</Transition>
```

Add CSS transitions at the end of the `<style>` block (or in the scoped styles):

```css
.slide-down-enter-active,
.slide-down-leave-active {
  transition: transform 0.2s ease, opacity 0.2s ease;
}
.slide-down-enter-from,
.slide-down-leave-to {
  transform: translateY(-100%);
  opacity: 0;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
```

- [ ] **Step 5: Verify frontend compiles**

```bash
cd web && npx vue-tsc --noEmit 2>&1 | tail -20
```
Expected: no type errors.

- [ ] **Step 6: Run full frontend tests**

```bash
cd web && npx vitest run 2>&1 | tail -20
```
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add web/src/App.vue
git commit -m "feat(ui): stale binary banner with one-click restart"
```

---

## Task 7: Full Build + Test

- [ ] **Step 1: Build everything**

```bash
go build ./... && echo "Go build OK"
cd web && npm run build 2>&1 | tail -10
```
Expected: `Go build OK` and Vite build completes.

- [ ] **Step 2: Run Go tests with race detector**

```bash
go test -race ./... -timeout 120s 2>&1 | grep -E "^(ok|FAIL|---)" | tail -30
```
Expected: all packages `ok`.

- [ ] **Step 3: Run frontend tests**

```bash
cd web && npx vitest run 2>&1 | tail -10
```
Expected: all pass.

- [ ] **Step 4: Commit if anything unstaged**

```bash
git status
```
If clean: proceed. Otherwise commit stragglers.

---

## Task 8: Homebrew Tap — Service Block

> **This task targets the `scrypster/homebrew-tap` repo, not this worktree.**
> Clone or edit the tap repo separately and open a companion PR.

**Files:**
- Modify: `Formula/huginn.rb`

- [ ] **Step 1: Clone the tap (or navigate to it)**

```bash
gh repo clone scrypster/homebrew-tap /tmp/homebrew-tap
cd /tmp/homebrew-tap
git checkout -b feat/huginn-service-block
```

- [ ] **Step 2: Add the `service do` block to `Formula/huginn.rb`**

Open `Formula/huginn.rb`. After the `def install` block and before `test do`, add:

```ruby
service do
  run          [opt_bin/"huginn", "serve", "--foreground"]
  keep_alive   true
  log_path     var/"log/huginn.log"
  error_log_path var/"log/huginn.log"
  working_dir  var
end
```

Full resulting file should look like:
```ruby
class Huginn < Formula
  desc "Local AI agent platform — multi-agent, skills, cloud sync"
  homepage "https://huginn.sh"
  version "0.3.1"

  on_macos do
    on_arm do
      url "https://github.com/scrypster/huginn/releases/download/v#{version}/huginn-darwin-arm64"
      sha256 "44da81a9e93691c9583cb61d8b8a356335baff2013e32dc3f2d048d7120d82c7"
    end
    on_intel do
      url "https://github.com/scrypster/huginn/releases/download/v#{version}/huginn-darwin-amd64"
      sha256 "31f5ee8b30daf27a6d2e093ad0feb07033f969d50a84ea6858c15f1dba6f965e"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/scrypster/huginn/releases/download/v#{version}/huginn-linux-amd64"
      sha256 "7f45042b986673e8fd98bdae3111a32c51693a9b5c199cd77c00c46593989320"
    end
    on_arm do
      url "https://github.com/scrypster/huginn/releases/download/v#{version}/huginn-linux-arm64"
      sha256 "a0b17bc58824bc87481d68ce0ea3056960f8a68810db1b085a56d75e96d6869b"
    end
  end

  def install
    os   = OS.mac? ? "darwin" : "linux"
    arch = Hardware::CPU.arm? ? "arm64" : "amd64"
    bin.install "huginn-#{os}-#{arch}" => "huginn"
  end

  service do
    run          [opt_bin/"huginn", "serve", "--foreground"]
    keep_alive   true
    log_path     var/"log/huginn.log"
    error_log_path var/"log/huginn.log"
    working_dir  var
  end

  test do
    assert_match "huginn", shell_output("#{bin}/huginn version")
  end
end
```

- [ ] **Step 3: Commit and push**

```bash
cd /tmp/homebrew-tap
git add Formula/huginn.rb
git commit -m "feat(huginn): add service block for brew services integration"
git push -u origin feat/huginn-service-block
```

- [ ] **Step 4: Open PR**

```bash
gh pr create \
  --repo scrypster/homebrew-tap \
  --title "feat(huginn): add service block for brew services" \
  --body "Adds a \`service do\` block so \`brew services start huginn\` works and \`brew upgrade huginn\` auto-restarts the daemon when managed by brew services." \
  --base main
```

---

## Self-Review

**Spec coverage:**
- Fix 1 (Homebrew health check): Task 1 ✅
- Fix 2 (StaleWatcher): Task 2 ✅
- Fix 2 (platform exec): Task 3 ✅
- Fix 2 (health stale field + restart handler + routes): Task 4 ✅
- Fix 2 (startup log warn): Task 4 Step 6 — `slog.Warn` in `Start()` ✅
- Fix 2 (frontend polling + stale ref): Task 5 ✅
- Fix 2 (banner + restart UX + reload): Task 6 ✅
- Fix 3 (Homebrew service block): Task 8 ✅

**Placeholder scan:** No TBDs. All code blocks are complete.

**Type consistency:**
- `newStaleWatcher(exePath string)` defined in Task 2, used in Task 4 ✅
- `currentExePath() (string, error)` defined in Task 3, used in Task 4 ✅
- `platformExec(exePath, args, env)` defined in Task 3, used in Task 4 ✅
- `execSupported` const defined in Task 3, used in Task 4 ✅
- `w.IsStale() bool` defined in Task 2, used in Task 4 ✅
- `api.restart()` defined in Task 5, used in Task 6 ✅
- `stale`, `startPolling`, `stopPolling` exported from `useVersion` in Task 5, used in Task 6 ✅
