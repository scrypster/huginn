# Upgrade UX — Design Spec

**Date:** 2026-04-27
**Status:** Approved
**Branch:** feature/upgrade-ux

---

## Problem

`huginn upgrade` has two UX bugs:

1. **Self-update path:** The confirmation prompt says only `Install v0.3.1? [y/N]` — no mention that huginn will stop and restart. Users with an active agent run are blindsided when their server goes down mid-task.

2. **Homebrew path:** `upgradeViaHomebrew()` runs `brew upgrade` and exits. Running daemons (`serve`, `tray`) are never stopped before the upgrade or restarted after. The new binary is installed but the old process keeps running — users must restart manually with no indication that they need to.

---

## Scope

**In scope:**
- Self-update path: move PID detection to `Run()`, pass state struct down, update confirmation prompt to surface running daemons
- `--yes` flag: skip prompt but print a notice when daemons are running
- Homebrew path: pass `latest` + `yes` into `upgradeViaHomebrew`, add prompt, add stop/restart/verify around `brew upgrade`
- `--version` and `--force` flags with Homebrew: error out clearly

**Out of scope:**
- Windows path (opens browser — no change)
- PID reuse signal race (pre-existing, not introduced here)
- Cosign/GPG binary signing (tracked separately)

---

## Architecture

### Running-state detection: detect once, pass down

PID detection currently happens inside `selfUpdateWithURLs`. Moving it to `Run()` lets the confirmation prompt surface running daemons. To avoid a TOCTOU gap (a daemon starts or dies between two detection calls), detect once and pass a struct:

```go
type runningState struct {
    serveRunning bool
    servePID     int
    trayRunning  bool
    trayPID      int
}
```

`Run()` calls `u.PIDIsLiveFn` for both PID files, builds the struct, uses it for the prompt, and passes it to `selfUpdateWithURLs` (and `upgradeViaHomebrew`). `selfUpdateWithURLs` no longer re-detects — it uses the passed struct directly.

### Self-update path (`Run()` changes)

**New prompt flow:**

1. Detect `runningState`
2. Build a daemon description: `"server + tray"` / `"server"` / `"tray"` / `""` based on which PIDs are live
3. If `--yes`: skip prompt; if daemons running, print notice line
4. If interactive: show combined prompt (version + running warning if applicable)

**Prompt when daemons are running:**
```
  huginn upgrade

  current: v0.3.0 → latest: v0.3.1
  Release notes: https://github.com/scrypster/huginn/releases/tag/v0.3.1

  huginn server + tray are running and will be stopped and
  restarted automatically during the upgrade.

  Upgrade to v0.3.1? [y/N]
```

**Prompt when nothing is running:**
```
  huginn upgrade

  current: v0.3.0 → latest: v0.3.1
  Release notes: https://github.com/scrypster/huginn/releases/tag/v0.3.1

  Upgrade to v0.3.1? [y/N]
```

**`--yes` with running daemons:**
```
  Note: huginn server + tray are running and will be stopped and restarted.
```
(then proceeds immediately)

**User says N:**
```
  Aborted.
```
Clean exit. Nothing stopped, nothing touched.

### Homebrew path (`upgradeViaHomebrew` changes)

Restructure `upgradeViaHomebrew` to accept `latest string`, `yes bool`, and `state runningState`:

**`--version` or `--force` with Homebrew:** error out immediately:
```
  --version and --force are not supported with Homebrew installs.
  Run: brew upgrade scrypster/tap/huginn
```

**Otherwise — same prompt + stop/restart flow as self-update:**

```
  Homebrew install detected.
  huginn server + tray are running and will be stopped
  and restarted automatically during the upgrade.

  Upgrade to v0.3.1? [y/N] y

  Stopping server...                   ✓
  Stopping tray...                     ✓
  Running brew upgrade...
  ==> Upgrading scrypster/tap/huginn
  ...brew output streams live...
  Verifying binary...                  ✓
  Restarting server...                 ✓
  Restarting tray...                   ✓

  huginn updated to v0.3.1
```

**If brew fails:** restart previously-running daemons (best-effort), then surface brew's error.

**Post-brew verification:** run `verifyHuginnBinary(exePath, latest)` after `brew upgrade`. If it fails:
```
  Warning: new binary failed verification.
  Run: brew reinstall scrypster/tap/huginn
```
Then attempt to restart daemons anyway (user can decide what to do).

---

## File Changes

| File | Change |
|------|--------|
| `upgrade.go` | Add `runningState` struct; move PID detection to `Run()`; update prompt logic; update `selfUpdateWithURLs` signature; restructure `upgradeViaHomebrew` |

No new files. All changes in `upgrade.go`.

---

## Tests

Existing `Upgrader` tests use injected `PIDIsLiveFn`, `StopProcess`, `DetachStart` — all new code paths are testable through the same injection points.

New tests:
- `TestRun_PromptMentionsRunningDaemons` — serveRunning=true, trayRunning=true → prompt contains "server + tray"
- `TestRun_PromptOmitsRestartWhenNotRunning` — both false → prompt does not contain "running"
- `TestRun_YesFlagPrintsNoticeWhenRunning` — `--yes` + running daemons → notice printed, no prompt
- `TestRun_YesFlagSilentWhenNotRunning` — `--yes` + nothing running → no notice
- `TestUpgradeViaHomebrew_StopsAndRestartsDaemons` — brew succeeds → stop called, restart called
- `TestUpgradeViaHomebrew_RestartsOnBrewFailure` — brew fails → daemons restarted, error returned
- `TestUpgradeViaHomebrew_VersionFlagErrors` — `--version` with Homebrew → error, no brew call
- `TestUpgradeViaHomebrew_ForceFlagErrors` — `--force` with Homebrew → error, no brew call
- `TestUpgradeViaHomebrew_VerifiesNewBinary` — post-brew verify fails → warning printed

---

## Success Criteria

- Running `huginn upgrade` with active server shows the daemon names before asking
- Saying N leaves everything untouched
- `--yes` is still non-interactive but prints a notice when daemons will be restarted
- `huginn upgrade` on a Homebrew install stops daemons, upgrades, verifies, restarts — no manual restart required
- `--version`/`--force` with Homebrew produce a clear error
- All new tests pass; existing upgrade tests still pass
- `go build ./...` clean
