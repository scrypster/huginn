# Resilient Delivery Queue — Design Spec

**Date:** 2026-04-27
**Status:** Approved
**Scope:** Notification delivery engine for workflows (webhook + email channels)

---

## Problem

The current delivery system has two meaningful gaps:

1. **Email has no dead-letter coverage.** Webhook failures are written to a JSONL dead-letter file after 3 exhausted attempts. Email failures are logged and silently dropped — no recovery path.
2. **The dead-letter file is not surfaced in the UI.** API endpoints (`GET/POST /api/v1/delivery-failures`) exist but nothing calls them from the frontend. Users have no visibility into delivery failures.

The goal is to make delivery fully self-healing with zero user attention required under normal conditions, while being completely transparent when persistent failures need human action.

---

## Scope

**In scope:**
- Webhook and email delivery channels (external, transient failures)
- Durable SQLite-backed retry queue
- Per-(workflow, endpoint) circuit breaker
- Cron-proportional retry window
- Inbox escalation on exhaustion
- UI: Deliveries tab in run detail, global nav badge, drawer

**Out of scope:**
- Internal channels: `inbox`, `space`, `agent_dm` — these fail fast and surface immediately; no retry needed
- Distributed coordination (single process, local app)

---

## Architecture: Three Layers

### Layer 1 — Fast Send-Time Retries (existing, unchanged)

When a workflow completes and fires a delivery, the current system makes up to 3 immediate attempts with exponential backoff and jitter:

- Webhook: 2s → 5s → 15s, 15s per-request timeout, 5xx retried / 4xx permanent
- Email: 2s → 8s → 30s, PlainAuth SMTP

On success: delivery record written, done.
On exhaustion after 3 attempts: hand off to Layer 2.

SSRF protection (webhook) is unchanged: save-time DNS-free check + runtime DNS-resolved check against RFC 1918 / RFC 6598 ranges.

### Layer 2 — Durable Queue with Auto-Retry (new)

A background goroutine picks up deliveries that failed Layer 1 and retries them using a SQLite-backed queue. The system is invisible to the user while it works.

#### SQLite Schema

**`delivery_queue` table** — one row per pending delivery:

```sql
CREATE TABLE IF NOT EXISTS delivery_queue (
    id              TEXT PRIMARY KEY,         -- UUID
    workflow_id     TEXT NOT NULL,
    run_id          TEXT NOT NULL,
    endpoint        TEXT NOT NULL,            -- webhook URL or "smtp://user@host"
    channel         TEXT NOT NULL,            -- "webhook" | "email"
    payload         TEXT NOT NULL,            -- JSON blob (full NotificationDelivery + notification)
    status          TEXT NOT NULL DEFAULT 'pending',
                                              -- pending | retrying | delivered | failed | superseded
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 5,
    retry_window_s  INTEGER NOT NULL,         -- derived from workflow cron interval
    next_retry_at   TEXT NOT NULL,            -- RFC3339; worker fires when now >= this
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    last_attempt_at TEXT,
    last_error      TEXT
);

CREATE INDEX IF NOT EXISTS idx_delivery_queue_work
    ON delivery_queue (status, next_retry_at)
    WHERE status IN ('pending', 'retrying');

CREATE INDEX IF NOT EXISTS idx_delivery_queue_workflow
    ON delivery_queue (workflow_id, run_id);
```

**`endpoint_health` table** — circuit breaker state, one row per (workflow_id, endpoint):

```sql
CREATE TABLE IF NOT EXISTS endpoint_health (
    workflow_id          TEXT NOT NULL,
    endpoint             TEXT NOT NULL,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    circuit_state        TEXT NOT NULL DEFAULT 'closed', -- "closed" | "open"
    opened_at            TEXT,
    last_probe_at        TEXT,
    PRIMARY KEY (workflow_id, endpoint)
);
```

#### Deduplication

Before inserting a new queued delivery, the worker marks any existing `pending` or `retrying` row for the same `(workflow_id, endpoint)` as `superseded`. This caps the queue at one active delivery per workflow-endpoint pair regardless of cron frequency. Superseded rows are retained for the audit trail.

```sql
-- Step 1: supersede any existing active row FIRST
UPDATE delivery_queue
   SET status = 'superseded'
 WHERE workflow_id = ? AND endpoint = ? AND status IN ('pending', 'retrying');

-- Step 2: insert the new row
INSERT INTO delivery_queue (...) VALUES (...);
```

#### Cron-Proportional Retry Window

The retry window is derived from the workflow's cron schedule so retries are bounded to the interval where they are still relevant:

```
retry_window = min(cron_interval × 0.8, 24h)
```

| Cron | Interval | Window |
|------|----------|--------|
| `*/10 * * * *` | 10 min | 8 min |
| `0 * * * *` | 1 hr | 48 min |
| `0 9 * * *` | 24 hr | ~19 hr |
| `0 9 * * 1` | 7 days | 24 hr (capped) |

For manual (ad-hoc) runs with no cron schedule: use a 1-hour default window.

For irregular cron expressions: compute the minimum gap between the next 5 scheduled fire times and use that as the interval.

5 attempts are spread exponentially within the window:

```
attempt 1: next_retry_at = now  (fired on the next 30s poll tick, no artificial delay)
attempt 2: next_retry_at = now + window × 0.05
attempt 3: next_retry_at = now + window × 0.15
attempt 4: next_retry_at = now + window × 0.40
attempt 5: next_retry_at = now + window × 0.80
```

Each delay gets ±10% jitter to prevent thundering-herd on restart.

#### Circuit Breaker

Scope: **per `(workflow_id, endpoint)` pair** — isolated per workflow. A buggy payload in one workflow cannot suspend delivery for another workflow using the same endpoint.

State machine:

```
closed → open:   after 5 consecutive failures
open → closed:   on successful probe OR manual "retry now" in UI
```

While `open`:
- Worker skips sending but still allows new deliveries to be enqueued (dedup keeps only the latest)
- Probes once per cron interval: worker checks `now - last_probe_at >= retry_window_s` on the queued item; if true, fires one attempt. If it succeeds, circuit closes and delivery resumes.
- UI shows circuit as suspended

Note: The UI can aggregate `endpoint_health` rows by endpoint URL at read time to surface "this URL appears down across N workflows" without sharing mutable state.

#### Background Worker

A single goroutine started at scheduler init time. Poll interval: 30 seconds.

```
loop every 30s:
  SELECT rows WHERE status IN ('pending','retrying') AND next_retry_at <= now
  for each row:
    check endpoint_health for (workflow_id, endpoint)
    if circuit = open AND not time for probe: skip
    attempt delivery
    if success:
      mark status = delivered
      reset consecutive_failures = 0, circuit = closed
    if failure:
      increment consecutive_failures
      if consecutive_failures >= 5: open circuit
      if attempt_count >= max_attempts: → Layer 3 escalation
      else: update next_retry_at, status = retrying
```

The worker is context-aware: it respects `ctx.Done()` for clean shutdown. No attempt is made mid-flight when the context is cancelled.

### Layer 3 — Inbox Escalation (new)

When all retry attempts are exhausted (attempt_count == max_attempts and delivery still failed):

1. Mark queue row `status = 'failed'`
2. Write a `notification.Notification` to the notification store with:
   - Severity: `error`
   - Summary: `"Delivery to {endpoint_display} permanently failed"`
   - Detail: `"Workflow '{workflow_name}' run {run_id} could not deliver to {endpoint} after {N} attempts. Last error: {last_error}. Open the run to retry manually."`
   - A deep-link action to the specific workflow run
3. Increment the global delivery failure badge count (via WS broadcast `delivery_badge_update` event)

The inbox notification is permanent until the user explicitly dismisses it. Badge stays visible until all failed deliveries are either retried successfully or dismissed.

---

## UI Surfaces

### 1. Deliveries Tab in Run Detail

Each workflow run detail view gains a **Deliveries** tab alongside the existing Steps view.

**Contents:**
- One row per delivery attempt (channel, endpoint, status, attempt count, next retry countdown or final error)
- Status indicators: `delivered` (green check), `retrying` (amber with countdown), `failed` (red), `superseded` (grey, dimmed), `suspended` (orange circuit icon)
- "Retry now" button on `failed` and `suspended` rows — calls `POST /api/v1/delivery-queue/{id}/retry`
- If circuit is open: inline callout "Delivery to this endpoint is suspended after 5 consecutive failures. Other runs are unaffected."

A small badge appears on the run card in the run list if any delivery for that run is in a non-terminal non-success state.

### 2. Global Nav Badge

A persistent badge in the main navigation, visible only when there are actionable issues (status = `failed` OR circuit_state = `open`). Count reflects distinct (workflow_id, endpoint) pairs with issues — not raw row count.

Zero badge = all clear. No badge rendered at all when count is zero.

### 3. Delivery Drawer

Clicking the nav badge opens a lightweight slide-in drawer (not a full page navigation). Contents:

- List of actionable issues grouped by workflow
- Each item: workflow name, endpoint (truncated), status, last error snippet
- "Retry now" button per item
- "View run" link → jumps to the run detail Deliveries tab
- Aggregated view: if multiple workflows share the same endpoint URL and all have open circuits, show a grouped callout: "hooks.slack.com appears down — affecting 3 workflows"

---

## API Changes

### New endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/delivery-queue` | List active queue entries (pending, retrying, failed, open circuits) |
| `POST` | `/api/v1/delivery-queue/{id}/retry` | Force immediate retry; closes circuit if open |
| `DELETE` | `/api/v1/delivery-queue/{id}` | Dismiss a failed entry (removes from badge count) |
| `GET` | `/api/v1/delivery-queue/badge` | Returns `{"count": N}` for badge polling |

### Retired endpoints

`GET /api/v1/delivery-failures` and `POST /api/v1/delivery-failures/retry` are removed. The JSONL dead-letter file is retired. SQLite is the single source of truth.

### WebSocket event

```json
{
  "type": "delivery_badge_update",
  "payload": { "count": 2 }
}
```

Broadcast on any change to the actionable failure count so the badge updates in real time without polling.

---

## Migration

1. On startup, run a one-time migration: read any existing JSONL dead-letter files from `~/.huginn/delivery-failures/` and insert unretried records into `delivery_queue` with `status = 'failed'` and `attempt_count = max_attempts`. This preserves history from the old system.
2. Rename/archive the JSONL files after migration (do not delete — audit trail).
3. The migration is idempotent: skip rows already present in `delivery_queue` (match on workflow_id + run_id + endpoint).

---

## Email Parity

Email failures are treated identically to webhook failures through Layers 2 and 3. The `endpoint` key for email entries is `smtp://{smtp_user}@{smtp_host}` — unique enough to identify the account while avoiding storing passwords in the endpoint column. The full credentials remain in the `payload` JSON blob (encrypted at rest is out of scope for this iteration).

The inline `smtp_pass` deprecation warning (already present) is unchanged. Connection-based SMTP continues to be recommended.

---

## What Doesn't Change

- Layer 1 fast retries (webhook: 2s→5s→15s, email: 2s→8s→30s) — unchanged
- SSRF protection for webhooks — unchanged
- Internal channels (`inbox`, `space`, `agent_dm`) — fail fast, no queue
- Notification store (inbox) — unchanged
- Workflow scheduling — never blocked by delivery state
- Per-step and workflow-level notify config schema — unchanged

---

## Success Criteria

- A webhook endpoint that goes down for 8 minutes and recovers causes zero user-visible events for a 10-minute workflow
- A permanently dead endpoint results in exactly one inbox notification and one nav badge increment — not a flood
- A workflow bug (bad payload causing 4xx) trips only that workflow's circuit; other workflows delivering to the same URL are unaffected
- Email failures and webhook failures are indistinguishable in UX — same queue, same UI, same retry path
- Dismissing all issues sets badge to zero with no page reload required
