# Upgrade Lifecycle — Enterprise-Grade Design Spec

## Overview

Three targeted fixes to make every upgrade path seamless: daemons always stop and restart correctly, the server detects when it's been replaced on disk by any mechanism, and the UI lets users activate the new version with one click — no manual restart required.

---

## Fix 1 — Homebrew Upgrade Path Health Check

### Problem
`upgradeViaHomebrew()` restarts the server daemon but does not wait to confirm it came up. `selfUpdateWithURLs` already has `waitForServerHealth()` wired; the Homebrew path is missing the same call.

### Fix
After `u.step("Restarting server...", ...)` succeeds in `upgradeViaHomebrew`, add:

```go
port := 8421
if u.ServerPortFn != nil {
    port = u.ServerPortFn()
}
u.waitForServerHealth(port)
```

**Files to change:**
- `upgrade.go` — `upgradeViaHomebrew()`, 5 lines

---

## Fix 2 — Stale Binary Detection + One-Click Restart

### Problem
When a user runs `brew upgrade huginn` directly, Homebrew replaces the binary on disk but the running server process continues on the old binary. There is no feedback to the user and no automatic recovery.

### Backend — `StaleWatcher`

New file: `internal/server/stale_watcher.go`

`StaleWatcher` records the mtime of the running binary (`os.Executable()` resolved through symlinks) at construction time. A background goroutine checks every 60 seconds. If the mtime changes, it sets an atomic `stale` flag via `sync/atomic`.

```go
type StaleWatcher struct {
    exePath  string
    baseMtime time.Time
    stale    atomic.Bool
}

func NewStaleWatcher() (*StaleWatcher, error)  // resolves exe path + records initial mtime
func (w *StaleWatcher) Start(ctx context.Context)  // launches background goroutine
func (w *StaleWatcher) IsStale() bool
```

The 60-second poll interval is a package-level `var` so tests can override it without a constructor param.

**Startup log:** If `IsStale()` is true immediately at server startup (binary was already replaced before `huginn serve` ran), print to stderr:
```
⚠  huginn binary has been updated on disk — run: huginn restart
```
This surfaces the case where the server is started manually after a `brew upgrade`.

### Backend — Health endpoint

`handleHealth` in `handlers.go` gains `"stale": bool` from `s.staleWatcher.IsStale()`. Nil-safe (returns false if watcher not set).

```json
{
  "status": "ok",
  "version": "v0.3.1",
  "stale": true,
  ...
}
```

### Backend — Restart endpoint

New route: `POST /api/v1/restart`

Requires bearer token (same auth middleware as all other endpoints).

Handler:
1. Resolves `os.Executable()` (follows symlinks to get the current on-disk path — the new binary)
2. Calls `syscall.Exec(exePath, os.Args, os.Environ())` — replaces the running process in-place with the new binary, same PID, same args
3. If `syscall.Exec` fails (e.g. on Windows where it's not supported), return `503` with `{"error": "restart not supported on this platform"}`

`syscall.Exec` is Unix-only. The Windows path (`upgrade_windows.go`) should define a `platformExec` stub that returns an error. Unix defines the real implementation in `upgrade_unix.go` or a new `restart_unix.go`.

**Files to change/create:**
- `internal/server/stale_watcher.go` — new file, `StaleWatcher`
- `internal/server/stale_watcher_test.go` — new file, tests
- `internal/server/server.go` — add `staleWatcher *StaleWatcher` field; wire `NewStaleWatcher()` + `Start()` in server startup
- `internal/server/handlers.go` — `handleHealth` adds `stale` field; add `handleRestart` handler
- `internal/server/server.go` — register `POST /api/v1/restart` route (no auth needed for the mux entry — auth middleware applied inline like other routes)

### Frontend — Stale banner

`useVersion` currently fetches health once at startup. Extend it to:
1. Poll `GET /api/v1/health` every **60 seconds** after the initial load
2. Expose `stale: Ref<boolean>` from `useVersion()`

`App.vue` renders a full-width dismissible banner at the top of the layout when `stale === true`:

```
[⬆] huginn has been updated in the background.    [Restart Now]  [×]
```

Clicking **Restart Now**:
1. Calls `POST /api/v1/restart`
2. Banner transitions to "Restarting…" (spinner, non-dismissible)
3. Polls `GET /api/v1/health` every 500ms
4. When health responds with `stale: false` (new binary running), reloads the page via `window.location.reload()`

Clicking **×** dismisses the banner for the current session (does not suppress the restart — just hides the prompt; the user can always manually restart).

**Files to change:**
- `web/src/composables/useApi.ts` — add `stale?: boolean` to health response type; add `api.restart()` calling `POST /api/v1/restart`
- `web/src/composables/useVersion.ts` — add 60s polling, expose `stale` ref
- `web/src/App.vue` — stale banner component (inline, ~30 lines of template)
- `web/src/composables/__tests__/useVersion.test.ts` — test polling and stale propagation

---

## Fix 3 — Homebrew `service` Block

### Problem
The `scrypster/homebrew-tap` formula has no `service do` block. `brew services` doesn't know how to manage huginn, so `brew upgrade huginn` does not trigger a daemon restart even for users who installed via Homebrew.

### Fix
Add a `service do` block to `Formula/huginn.rb` in `scrypster/homebrew-tap`:

```ruby
service do
  run [opt_bin/"huginn", "serve", "--foreground"]
  keep_alive true
  log_path    var/"log/huginn.log"
  error_log_path var/"log/huginn.log"
  working_dir var
end
```

`huginn serve --foreground` already exists (flag wired in `main.go` line 148). Homebrew's `service` integration uses launchd on macOS and systemd on Linux.

With this in place, users who opted into `brew services start huginn` get automatic restart on `brew upgrade huginn`. The stale watcher (Fix 2) remains as a safety net for users who manage the daemon manually.

**Repo:** `scrypster/homebrew-tap` (separate PR)

**Files to change:**
- `Formula/huginn.rb` — add `service do` block

---

## Coverage After All Three Fixes

| Upgrade path | Stop old | Install | Start new | Verified up | User notified |
|---|---|---|---|---|---|
| `huginn upgrade` (direct binary) | ✅ | ✅ | ✅ | ✅ | ✅ |
| `huginn upgrade` (Homebrew) | ✅ | ✅ | ✅ | ✅ Fix 1 | ✅ |
| `brew upgrade huginn` (brew services) | ✅ Fix 3 | ✅ | ✅ Fix 3 | ✅ Fix 2 | ✅ Fix 2 |
| `brew upgrade huginn` (manual serve) | — | ✅ | — | ✅ Fix 2 | ✅ Fix 2 |

---

## Out of Scope

- Cosign/GPG binary signing (tracked separately)
- Windows in-place restart (syscall.Exec not available; banner shows 503 gracefully)
- Rollback UI (backup exists on disk; restore is manual)
