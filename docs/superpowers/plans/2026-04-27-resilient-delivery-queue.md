# Resilient Delivery Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Model preferences (per user):** Sonnet for planning/implementation, Haiku for R&D/file reading, Opus for architectural reasoning.

**Goal:** Replace the JSONL dead-letter file with a SQLite-backed delivery queue that auto-retries webhook and email failures with cron-proportional backoff, per-(workflow,endpoint) circuit breaking, and inbox escalation on exhaustion — all invisible to the user until persistence is required.

**Architecture:** Three-layer system: Layer 1 (existing fast send-time retries unchanged), Layer 2 (new durable SQLite queue with background worker, deduplication, circuit breaker, cron-proportional retry window), Layer 3 (inbox escalation + WS badge broadcast when all retries exhausted). Circuit breaker is scoped per `(workflow_id, endpoint)` pair so one buggy workflow cannot suspend delivery for other workflows using the same URL.

**Tech Stack:** Go 1.22+, SQLite via `github.com/scrypster/huginn/internal/sqlitedb`, robfig/cron v3, Vue 3 + TypeScript

---

## File Map

**New files:**
- `internal/scheduler/delivery_queue_types.go` — DeliveryQueueEntry, EndpointHealth, DeliveryQueuePayload types, endpointKey helper
- `internal/scheduler/delivery_queue_store.go` — SQLite CRUD for delivery_queue and endpoint_health tables
- `internal/scheduler/delivery_queue.go` — DeliveryQueue struct, Enqueue (with dedup), background worker, circuit breaker, badge count
- `internal/scheduler/delivery_queue_types_test.go` — unit tests for endpointKey, ComputeRetryWindow, nextRetryDelay
- `internal/scheduler/delivery_queue_store_test.go` — CRUD integration tests
- `internal/scheduler/delivery_queue_test.go` — enqueue/dedup/worker/circuit breaker tests
- `web/src/composables/useDeliveryQueue.ts` — Vue composable for delivery queue API + WS

**Modified files:**
- `internal/scheduler/cron_preview.go` — add `ComputeRetryWindow(schedule string) int`
- `internal/scheduler/migrations.go` — add v3 migration creating delivery_queue + endpoint_health
- `internal/scheduler/runner_options.go` — add `WithDeliveryQueue` option + field to runnerConfig
- `internal/scheduler/workflow_runner.go` — wire queue into dispatchNotification; remove WriteDeliveryFailure call
- `internal/scheduler/scheduler.go` — add deliveryQueue field, SetDeliveryQueue, start worker in Start()
- `internal/server/handlers_workflows.go` — add delivery-queue handlers; retire delivery-failures handlers
- `internal/server/server.go` — add deliveryQueue field, setter, register new routes
- `main.go` — create DeliveryQueue, wire to runner + scheduler + server
- `web/src/views/WorkflowsView.vue` — add Deliveries tab to run detail
- `web/src/App.vue` — add delivery badge to nav, wire WS event

---

## Task 1: SQLite migration — delivery_queue + endpoint_health tables

**Files:**
- Modify: `internal/scheduler/migrations.go`
- Modify: `internal/sqlitedb/schema/huginn-sqlite-schema.sql`

- [ ] **Step 1: Add SQL to schema file**

Open `internal/sqlitedb/schema/huginn-sqlite-schema.sql` and append after the `workflow_runs` block:

```sql
-- delivery_queue: durable retry queue for failed webhook/email deliveries.
CREATE TABLE IF NOT EXISTS delivery_queue (
    id              TEXT    NOT NULL PRIMARY KEY,
    workflow_id     TEXT    NOT NULL,
    run_id          TEXT    NOT NULL,
    endpoint        TEXT    NOT NULL,
    channel         TEXT    NOT NULL CHECK (channel IN ('webhook', 'email')),
    payload         TEXT    NOT NULL DEFAULT '{}',
    status          TEXT    NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'retrying', 'delivered', 'failed', 'superseded')),
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 5,
    retry_window_s  INTEGER NOT NULL DEFAULT 3600,
    next_retry_at   TEXT    NOT NULL,
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    last_attempt_at TEXT,
    last_error      TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_delivery_queue_work
    ON delivery_queue (status, next_retry_at)
    WHERE status IN ('pending', 'retrying');

CREATE INDEX IF NOT EXISTS idx_delivery_queue_workflow
    ON delivery_queue (workflow_id, run_id);

-- endpoint_health: per-(workflow_id, endpoint) circuit breaker state.
CREATE TABLE IF NOT EXISTS endpoint_health (
    workflow_id          TEXT    NOT NULL,
    endpoint             TEXT    NOT NULL,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    circuit_state        TEXT    NOT NULL DEFAULT 'closed'
                             CHECK (circuit_state IN ('closed', 'open')),
    opened_at            TEXT,
    last_probe_at        TEXT,
    PRIMARY KEY (workflow_id, endpoint)
);
```

- [ ] **Step 2: Add migration to migrations.go**

In `internal/scheduler/migrations.go`, add to the `Migrations()` slice and add the function:

```go
func Migrations() []sqlitedb.Migration {
    return []sqlitedb.Migration{
        {
            Name: "scheduler_v2_workflow_runs_add_replay_columns",
            Up:   migrateWorkflowRunsV2AddReplayColumns,
        },
        {
            Name: "scheduler_v3_delivery_queue",
            Up:   migrateV3DeliveryQueue,
        },
    }
}

func migrateV3DeliveryQueue(tx *sql.Tx) error {
    stmts := []string{
        `CREATE TABLE IF NOT EXISTS delivery_queue (
            id              TEXT    NOT NULL PRIMARY KEY,
            workflow_id     TEXT    NOT NULL,
            run_id          TEXT    NOT NULL,
            endpoint        TEXT    NOT NULL,
            channel         TEXT    NOT NULL CHECK (channel IN ('webhook','email')),
            payload         TEXT    NOT NULL DEFAULT '{}',
            status          TEXT    NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending','retrying','delivered','failed','superseded')),
            attempt_count   INTEGER NOT NULL DEFAULT 0,
            max_attempts    INTEGER NOT NULL DEFAULT 5,
            retry_window_s  INTEGER NOT NULL DEFAULT 3600,
            next_retry_at   TEXT    NOT NULL,
            created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
            last_attempt_at TEXT,
            last_error      TEXT    NOT NULL DEFAULT ''
        )`,
        `CREATE INDEX IF NOT EXISTS idx_delivery_queue_work
            ON delivery_queue (status, next_retry_at)
            WHERE status IN ('pending','retrying')`,
        `CREATE INDEX IF NOT EXISTS idx_delivery_queue_workflow
            ON delivery_queue (workflow_id, run_id)`,
        `CREATE TABLE IF NOT EXISTS endpoint_health (
            workflow_id          TEXT    NOT NULL,
            endpoint             TEXT    NOT NULL,
            consecutive_failures INTEGER NOT NULL DEFAULT 0,
            circuit_state        TEXT    NOT NULL DEFAULT 'closed'
                                     CHECK (circuit_state IN ('closed','open')),
            opened_at            TEXT,
            last_probe_at        TEXT,
            PRIMARY KEY (workflow_id, endpoint)
        )`,
    }
    for _, s := range stmts {
        if _, err := tx.Exec(s); err != nil {
            return err
        }
    }
    return nil
}
```

- [ ] **Step 3: Verify migration runs**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/scheduler/migrations.go internal/sqlitedb/schema/huginn-sqlite-schema.sql
git commit -m "feat(delivery): add delivery_queue and endpoint_health SQLite migration"
```

---

## Task 2: ComputeRetryWindow + nextRetryDelay helpers

**Files:**
- Modify: `internal/scheduler/cron_preview.go`
- Create: `internal/scheduler/delivery_queue_types_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/scheduler/delivery_queue_types_test.go`:

```go
package scheduler

import (
    "testing"
    "time"
)

func TestComputeRetryWindow(t *testing.T) {
    tests := []struct {
        name     string
        schedule string
        wantMin  int // seconds
        wantMax  int
    }{
        {"empty schedule = 1 hour default", "", 3600, 3600},
        {"every 10 min", "*/10 * * * *", 400, 500},   // 8 min = 480s ± jitter range
        {"every hour", "0 * * * *", 2700, 2900},       // 48 min = 2880s
        {"daily", "0 9 * * *", 60000, 70000},          // ~19hr
        {"weekly capped at 24h", "0 9 * * 1", 86390, 86401}, // 24h cap
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            got := ComputeRetryWindow(tc.schedule)
            if got < tc.wantMin || got > tc.wantMax {
                t.Errorf("ComputeRetryWindow(%q) = %d, want [%d, %d]", tc.schedule, got, tc.wantMin, tc.wantMax)
            }
        })
    }
}

func TestNextRetryDelay(t *testing.T) {
    window := 480 * int(time.Second) // 480s window (10-min workflow)
    tests := []struct {
        attempt  int
        wantMin  time.Duration
        wantMax  time.Duration
    }{
        {0, 0, 1 * time.Second},                        // immediate: ≈ 0
        {1, 18 * time.Second, 30 * time.Second},        // 0.05 × 480s = 24s ±10%
        {2, 55 * time.Second, 85 * time.Second},        // 0.15 × 480s = 72s ±10%
        {3, 170 * time.Second, 215 * time.Second},      // 0.40 × 480s = 192s ±10%
        {4, 345 * time.Second, 425 * time.Second},      // 0.80 × 480s = 384s ±10%
    }
    for _, tc := range tests {
        t.Run("", func(t *testing.T) {
            got := nextRetryDelay(window, tc.attempt)
            if got < tc.wantMin || got > tc.wantMax {
                t.Errorf("nextRetryDelay(attempt=%d) = %v, want [%v, %v]", tc.attempt, got, tc.wantMin, tc.wantMax)
            }
        })
    }
}
```

- [ ] **Step 2: Run tests — expect FAIL (functions not defined)**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestComputeRetryWindow|TestNextRetryDelay" -v 2>&1 | head -20
```

Expected: `undefined: ComputeRetryWindow`, `undefined: nextRetryDelay`

- [ ] **Step 3: Implement in cron_preview.go**

Append to `internal/scheduler/cron_preview.go`:

```go
// ComputeRetryWindow returns the retry window in seconds for a workflow's
// delivery queue. Derived as min(cron_interval × 0.8, 86400).
// Empty schedule (ad-hoc runs) returns 3600 (1 hour).
// For irregular expressions the minimum gap between the next 6 fire times is used.
func ComputeRetryWindow(schedule string) int {
    const maxWindowS = 86400 // 24 hours
    if schedule == "" {
        return 3600
    }
    times, err := CronPreview(schedule, 6, time.Now())
    if err != nil || len(times) < 2 {
        return 3600
    }
    minGap := times[1].Sub(times[0])
    for i := 2; i < len(times); i++ {
        if d := times[i].Sub(times[i-1]); d < minGap {
            minGap = d
        }
    }
    window := time.Duration(float64(minGap) * 0.8)
    if int(window.Seconds()) > maxWindowS {
        return maxWindowS
    }
    return int(window.Seconds())
}

// nextRetryDelay returns the delay before attempt number attemptCount
// (0-indexed) using exponential spacing within the retry window.
// Attempt 0 returns 0 (fire immediately on next poll tick).
// Each delay gets ±10% jitter.
func nextRetryDelay(retryWindowS, attemptCount int) time.Duration {
    ratios := []float64{0.0, 0.05, 0.15, 0.40, 0.80}
    if attemptCount >= len(ratios) {
        attemptCount = len(ratios) - 1
    }
    base := time.Duration(float64(retryWindowS) * ratios[attemptCount] * float64(time.Second))
    if base == 0 {
        return 0
    }
    jitterRange := float64(base) * 0.10
    offset := time.Duration(rand.Int63n(int64(2*jitterRange+1)) - int64(jitterRange)) //nolint:gosec
    return base + offset
}
```

Add `"math/rand"` to the imports in `cron_preview.go` if not already present.

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestComputeRetryWindow|TestNextRetryDelay" -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/cron_preview.go internal/scheduler/delivery_queue_types_test.go
git commit -m "feat(delivery): add ComputeRetryWindow and nextRetryDelay helpers"
```

---

## Task 3: DeliveryQueueEntry types + endpointKey helper

**Files:**
- Create: `internal/scheduler/delivery_queue_types.go`
- Modify: `internal/scheduler/delivery_queue_types_test.go`

- [ ] **Step 1: Write failing tests for endpointKey**

Append to `internal/scheduler/delivery_queue_types_test.go`:

```go
func TestEndpointKey(t *testing.T) {
    tests := []struct {
        target NotificationDelivery
        want   string
    }{
        {
            NotificationDelivery{Type: "webhook", To: "https://hooks.slack.com/abc"},
            "https://hooks.slack.com/abc",
        },
        {
            NotificationDelivery{Type: "email", SMTPUser: "bot", SMTPHost: "smtp.gmail.com"},
            "smtp://bot@smtp.gmail.com",
        },
        {
            NotificationDelivery{Type: "email", Connection: "my-gmail"},
            "smtp-connection://my-gmail",
        },
    }
    for _, tc := range tests {
        got := endpointKey(tc.target)
        if got != tc.want {
            t.Errorf("endpointKey(%+v) = %q, want %q", tc.target, got, tc.want)
        }
    }
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run TestEndpointKey -v 2>&1 | head -10
```

Expected: `undefined: endpointKey`

- [ ] **Step 3: Create delivery_queue_types.go**

```go
// internal/scheduler/delivery_queue_types.go
package scheduler

import (
    "fmt"
    "time"

    "github.com/scrypster/huginn/internal/notification"
)

// DeliveryQueuePayload is the JSON blob stored in delivery_queue.payload.
// It captures everything needed to re-attempt a delivery without re-running
// the workflow. SMTPPass is stored here when inline credentials are used
// (deprecated but supported for backward compatibility).
type DeliveryQueuePayload struct {
    Notification notification.Notification `json:"notification"`
    Target       NotificationDelivery      `json:"target"`
    WorkflowName string                    `json:"workflow_name,omitempty"`
}

// DeliveryQueueEntry is one row in the delivery_queue table.
type DeliveryQueueEntry struct {
    ID            string     `json:"id"`
    WorkflowID    string     `json:"workflow_id"`
    RunID         string     `json:"run_id"`
    Endpoint      string     `json:"endpoint"`
    Channel       string     `json:"channel"`        // "webhook" | "email"
    Payload       string     `json:"payload"`        // JSON-encoded DeliveryQueuePayload
    Status        string     `json:"status"`         // pending|retrying|delivered|failed|superseded
    AttemptCount  int        `json:"attempt_count"`
    MaxAttempts   int        `json:"max_attempts"`
    RetryWindowS  int        `json:"retry_window_s"`
    NextRetryAt   time.Time  `json:"next_retry_at"`
    CreatedAt     time.Time  `json:"created_at"`
    LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
    LastError     string     `json:"last_error,omitempty"`
}

// EndpointHealth is one row in the endpoint_health table.
// Scoped per (WorkflowID, Endpoint) — isolated per workflow.
type EndpointHealth struct {
    WorkflowID          string     `json:"workflow_id"`
    Endpoint            string     `json:"endpoint"`
    ConsecutiveFailures int        `json:"consecutive_failures"`
    CircuitState        string     `json:"circuit_state"` // "closed" | "open"
    OpenedAt            *time.Time `json:"opened_at,omitempty"`
    LastProbeAt         *time.Time `json:"last_probe_at,omitempty"`
}

const (
    circuitBreakThreshold = 5 // open circuit after this many consecutive failures
)

// endpointKey returns a stable, credential-free string that uniquely identifies
// a delivery target for circuit-breaker and dedup keying.
func endpointKey(target NotificationDelivery) string {
    switch target.Type {
    case "webhook":
        return target.To
    case "email":
        if target.Connection != "" {
            return fmt.Sprintf("smtp-connection://%s", target.Connection)
        }
        return fmt.Sprintf("smtp://%s@%s", target.SMTPUser, target.SMTPHost)
    default:
        return fmt.Sprintf("%s:%s", target.Type, target.To)
    }
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestEndpointKey|TestComputeRetryWindow|TestNextRetryDelay" -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/delivery_queue_types.go internal/scheduler/delivery_queue_types_test.go
git commit -m "feat(delivery): add DeliveryQueueEntry, EndpointHealth types and endpointKey helper"
```

---

## Task 4: DeliveryQueueStore — SQLite CRUD

**Files:**
- Create: `internal/scheduler/delivery_queue_store.go`
- Create: `internal/scheduler/delivery_queue_store_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/scheduler/delivery_queue_store_test.go`:

```go
package scheduler

import (
    "testing"
    "time"

    "github.com/scrypster/huginn/internal/sqlitedb"
)

func newTestDeliveryQueueStore(t *testing.T) *DeliveryQueueStore {
    t.Helper()
    db, err := sqlitedb.Open(":memory:")
    if err != nil {
        t.Fatalf("open db: %v", err)
    }
    if err := sqlitedb.Migrate(db, Migrations()); err != nil {
        t.Fatalf("migrate: %v", err)
    }
    t.Cleanup(func() { db.Close() })
    return NewDeliveryQueueStore(db)
}

func TestDeliveryQueueStore_InsertAndGet(t *testing.T) {
    s := newTestDeliveryQueueStore(t)
    entry := DeliveryQueueEntry{
        ID:           "entry-1",
        WorkflowID:   "wf-1",
        RunID:        "run-1",
        Endpoint:     "https://hooks.slack.com/abc",
        Channel:      "webhook",
        Payload:      `{"notification":{},"target":{}}`,
        Status:       "pending",
        AttemptCount: 0,
        MaxAttempts:  5,
        RetryWindowS: 480,
        NextRetryAt:  time.Now().UTC().Truncate(time.Second),
    }
    if err := s.Insert(entry); err != nil {
        t.Fatalf("Insert: %v", err)
    }
    got, err := s.Get("entry-1")
    if err != nil {
        t.Fatalf("Get: %v", err)
    }
    if got.WorkflowID != "wf-1" || got.Status != "pending" {
        t.Errorf("unexpected entry: %+v", got)
    }
}

func TestDeliveryQueueStore_SupersedeAndInsert(t *testing.T) {
    s := newTestDeliveryQueueStore(t)
    base := DeliveryQueueEntry{
        ID: "old-1", WorkflowID: "wf-1", RunID: "run-1",
        Endpoint: "https://hooks.slack.com/abc", Channel: "webhook",
        Payload: `{}`, Status: "pending", MaxAttempts: 5, RetryWindowS: 480,
        NextRetryAt: time.Now().UTC(),
    }
    if err := s.Insert(base); err != nil {
        t.Fatalf("Insert old: %v", err)
    }
    // Supersede old, insert new
    newEntry := base
    newEntry.ID = "new-1"
    newEntry.RunID = "run-2"
    if err := s.SupersedeAndInsert(newEntry); err != nil {
        t.Fatalf("SupersedeAndInsert: %v", err)
    }
    old, _ := s.Get("old-1")
    if old.Status != "superseded" {
        t.Errorf("old entry not superseded, got status=%q", old.Status)
    }
    newGot, _ := s.Get("new-1")
    if newGot.Status != "pending" {
        t.Errorf("new entry wrong status=%q", newGot.Status)
    }
}

func TestDeliveryQueueStore_ListDue(t *testing.T) {
    s := newTestDeliveryQueueStore(t)
    past := time.Now().Add(-1 * time.Minute).UTC()
    future := time.Now().Add(1 * time.Hour).UTC()
    due := DeliveryQueueEntry{ID: "due-1", WorkflowID: "wf-1", RunID: "r1", Endpoint: "x", Channel: "webhook", Payload: "{}", Status: "pending", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: past}
    notDue := DeliveryQueueEntry{ID: "future-1", WorkflowID: "wf-1", RunID: "r2", Endpoint: "y", Channel: "webhook", Payload: "{}", Status: "pending", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: future}
    _ = s.Insert(due)
    _ = s.Insert(notDue)
    rows, err := s.ListDue(time.Now().UTC(), 10)
    if err != nil {
        t.Fatalf("ListDue: %v", err)
    }
    if len(rows) != 1 || rows[0].ID != "due-1" {
        t.Errorf("ListDue returned %d rows, want 1 with id=due-1", len(rows))
    }
}

func TestDeliveryQueueStore_UpdateStatus(t *testing.T) {
    s := newTestDeliveryQueueStore(t)
    e := DeliveryQueueEntry{ID: "e1", WorkflowID: "w1", RunID: "r1", Endpoint: "x", Channel: "webhook", Payload: "{}", Status: "pending", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: time.Now().UTC()}
    _ = s.Insert(e)
    next := time.Now().Add(5 * time.Minute).UTC()
    if err := s.UpdateAttempt("e1", "retrying", 1, "conn refused", &next); err != nil {
        t.Fatalf("UpdateAttempt: %v", err)
    }
    got, _ := s.Get("e1")
    if got.Status != "retrying" || got.AttemptCount != 1 {
        t.Errorf("unexpected after update: %+v", got)
    }
}

func TestDeliveryQueueStore_BadgeCount(t *testing.T) {
    s := newTestDeliveryQueueStore(t)
    now := time.Now().UTC()
    _ = s.Insert(DeliveryQueueEntry{ID: "f1", WorkflowID: "w1", RunID: "r1", Endpoint: "x", Channel: "webhook", Payload: "{}", Status: "failed", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: now})
    _ = s.Insert(DeliveryQueueEntry{ID: "f2", WorkflowID: "w1", RunID: "r2", Endpoint: "y", Channel: "email", Payload: "{}", Status: "delivered", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: now})
    count, err := s.BadgeCount()
    if err != nil {
        t.Fatalf("BadgeCount: %v", err)
    }
    if count != 1 {
        t.Errorf("BadgeCount = %d, want 1", count)
    }
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestDeliveryQueueStore" -v 2>&1 | head -15
```

Expected: `undefined: DeliveryQueueStore`

- [ ] **Step 3: Create delivery_queue_store.go**

```go
// internal/scheduler/delivery_queue_store.go
package scheduler

import (
    "database/sql"
    "fmt"
    "time"

    "github.com/scrypster/huginn/internal/sqlitedb"
)

// DeliveryQueueStore provides CRUD access to the delivery_queue and
// endpoint_health SQLite tables.
type DeliveryQueueStore struct {
    db *sqlitedb.DB
}

// NewDeliveryQueueStore wraps an existing sqlitedb.DB connection.
func NewDeliveryQueueStore(db *sqlitedb.DB) *DeliveryQueueStore {
    return &DeliveryQueueStore{db: db}
}

// Insert writes a new queue entry. Does NOT supersede existing rows.
// Use SupersedeAndInsert for the normal enqueue path.
func (s *DeliveryQueueStore) Insert(e DeliveryQueueEntry) error {
    _, err := s.db.Exec(`
        INSERT INTO delivery_queue
            (id, workflow_id, run_id, endpoint, channel, payload, status,
             attempt_count, max_attempts, retry_window_s, next_retry_at,
             created_at, last_attempt_at, last_error)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
        e.ID, e.WorkflowID, e.RunID, e.Endpoint, e.Channel, e.Payload, e.Status,
        e.AttemptCount, e.MaxAttempts, e.RetryWindowS,
        e.NextRetryAt.UTC().Format(time.RFC3339),
        time.Now().UTC().Format(time.RFC3339),
        nullTime(e.LastAttemptAt),
        e.LastError,
    )
    return err
}

// SupersedeAndInsert marks any existing pending/retrying row for the same
// (workflow_id, endpoint) as superseded, then inserts the new entry.
// This caps the queue at one active delivery per workflow-endpoint pair.
func (s *DeliveryQueueStore) SupersedeAndInsert(e DeliveryQueueEntry) error {
    tx, err := s.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback() //nolint:errcheck
    _, err = tx.Exec(`
        UPDATE delivery_queue SET status = 'superseded'
         WHERE workflow_id = ? AND endpoint = ? AND status IN ('pending','retrying')`,
        e.WorkflowID, e.Endpoint)
    if err != nil {
        return err
    }
    _, err = tx.Exec(`
        INSERT INTO delivery_queue
            (id, workflow_id, run_id, endpoint, channel, payload, status,
             attempt_count, max_attempts, retry_window_s, next_retry_at,
             created_at, last_attempt_at, last_error)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
        e.ID, e.WorkflowID, e.RunID, e.Endpoint, e.Channel, e.Payload, e.Status,
        e.AttemptCount, e.MaxAttempts, e.RetryWindowS,
        e.NextRetryAt.UTC().Format(time.RFC3339),
        time.Now().UTC().Format(time.RFC3339),
        nullTime(e.LastAttemptAt),
        e.LastError,
    )
    if err != nil {
        return err
    }
    return tx.Commit()
}

// Get retrieves a single entry by ID.
func (s *DeliveryQueueStore) Get(id string) (DeliveryQueueEntry, error) {
    row := s.db.QueryRow(`SELECT `+deliveryQueueCols+` FROM delivery_queue WHERE id = ?`, id)
    return scanDeliveryQueueEntry(row)
}

// ListDue returns entries with status pending/retrying where next_retry_at <= now,
// up to limit rows, ordered by next_retry_at ascending.
func (s *DeliveryQueueStore) ListDue(now time.Time, limit int) ([]DeliveryQueueEntry, error) {
    rows, err := s.db.Query(`
        SELECT `+deliveryQueueCols+` FROM delivery_queue
         WHERE status IN ('pending','retrying') AND next_retry_at <= ?
         ORDER BY next_retry_at ASC LIMIT ?`,
        now.UTC().Format(time.RFC3339), limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    return scanDeliveryQueueEntries(rows)
}

// ListActionable returns entries with status=failed plus open-circuit entries,
// up to limit rows. Used by the API badge and drawer.
func (s *DeliveryQueueStore) ListActionable(limit int) ([]DeliveryQueueEntry, error) {
    rows, err := s.db.Query(`
        SELECT `+deliveryQueueCols+` FROM delivery_queue
         WHERE status = 'failed'
         ORDER BY created_at DESC LIMIT ?`, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    return scanDeliveryQueueEntries(rows)
}

// UpdateAttempt updates status, attempt_count, last_error, and next_retry_at
// for an entry after an attempt (success or failure).
func (s *DeliveryQueueStore) UpdateAttempt(id, status string, attemptCount int, lastError string, nextRetryAt *time.Time) error {
    now := time.Now().UTC().Format(time.RFC3339)
    var nextStr *string
    if nextRetryAt != nil {
        v := nextRetryAt.UTC().Format(time.RFC3339)
        nextStr = &v
    }
    _, err := s.db.Exec(`
        UPDATE delivery_queue
           SET status = ?, attempt_count = ?, last_error = ?,
               next_retry_at = COALESCE(?, next_retry_at), last_attempt_at = ?
         WHERE id = ?`,
        status, attemptCount, lastError, nextStr, now, id)
    return err
}

// BadgeCount returns the count of distinct (workflow_id, endpoint) pairs
// with status=failed. Used for the nav badge.
func (s *DeliveryQueueStore) BadgeCount() (int, error) {
    var count int
    err := s.db.QueryRow(`
        SELECT COUNT(DISTINCT workflow_id || '|' || endpoint)
          FROM delivery_queue WHERE status = 'failed'`).Scan(&count)
    return count, err
}

// MarkDelivered sets status=delivered and clears last_error for an entry.
func (s *DeliveryQueueStore) MarkDelivered(id string) error {
    _, err := s.db.Exec(`
        UPDATE delivery_queue
           SET status = 'delivered', last_error = '', last_attempt_at = ?
         WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
    return err
}

// Dismiss sets status=failed and marks it as acknowledged (removes from badge).
// Actually deletes from queue — audit trail lives in the inbox notification.
func (s *DeliveryQueueStore) Dismiss(id string) error {
    _, err := s.db.Exec(`DELETE FROM delivery_queue WHERE id = ?`, id)
    return err
}

// --- EndpointHealth ---

// GetHealth retrieves circuit breaker state for (workflowID, endpoint).
// Returns a zero-value EndpointHealth with CircuitState=closed if not found.
func (s *DeliveryQueueStore) GetHealth(workflowID, endpoint string) (EndpointHealth, error) {
    var h EndpointHealth
    var openedAt, lastProbeAt sql.NullString
    err := s.db.QueryRow(`
        SELECT workflow_id, endpoint, consecutive_failures, circuit_state, opened_at, last_probe_at
          FROM endpoint_health WHERE workflow_id = ? AND endpoint = ?`,
        workflowID, endpoint).Scan(
        &h.WorkflowID, &h.Endpoint, &h.ConsecutiveFailures, &h.CircuitState,
        &openedAt, &lastProbeAt)
    if err == sql.ErrNoRows {
        return EndpointHealth{WorkflowID: workflowID, Endpoint: endpoint, CircuitState: "closed"}, nil
    }
    if err != nil {
        return h, err
    }
    if openedAt.Valid {
        t, _ := time.Parse(time.RFC3339, openedAt.String)
        h.OpenedAt = &t
    }
    if lastProbeAt.Valid {
        t, _ := time.Parse(time.RFC3339, lastProbeAt.String)
        h.LastProbeAt = &t
    }
    return h, nil
}

// UpsertHealth writes circuit breaker state for (workflowID, endpoint).
func (s *DeliveryQueueStore) UpsertHealth(h EndpointHealth) error {
    _, err := s.db.Exec(`
        INSERT INTO endpoint_health (workflow_id, endpoint, consecutive_failures, circuit_state, opened_at, last_probe_at)
        VALUES (?,?,?,?,?,?)
        ON CONFLICT(workflow_id, endpoint) DO UPDATE SET
            consecutive_failures = excluded.consecutive_failures,
            circuit_state        = excluded.circuit_state,
            opened_at            = excluded.opened_at,
            last_probe_at        = excluded.last_probe_at`,
        h.WorkflowID, h.Endpoint, h.ConsecutiveFailures, h.CircuitState,
        nullTime(h.OpenedAt), nullTime(h.LastProbeAt))
    return err
}

// --- helpers ---

const deliveryQueueCols = `id, workflow_id, run_id, endpoint, channel, payload, status,
    attempt_count, max_attempts, retry_window_s, next_retry_at,
    created_at, last_attempt_at, last_error`

func scanDeliveryQueueEntry(row *sql.Row) (DeliveryQueueEntry, error) {
    var e DeliveryQueueEntry
    var nextRetry, created string
    var lastAttempt sql.NullString
    err := row.Scan(
        &e.ID, &e.WorkflowID, &e.RunID, &e.Endpoint, &e.Channel, &e.Payload,
        &e.Status, &e.AttemptCount, &e.MaxAttempts, &e.RetryWindowS,
        &nextRetry, &created, &lastAttempt, &e.LastError)
    if err != nil {
        return e, fmt.Errorf("scan delivery queue entry: %w", err)
    }
    e.NextRetryAt, _ = time.Parse(time.RFC3339, nextRetry)
    e.CreatedAt, _ = time.Parse(time.RFC3339, created)
    if lastAttempt.Valid {
        t, _ := time.Parse(time.RFC3339, lastAttempt.String)
        e.LastAttemptAt = &t
    }
    return e, nil
}

func scanDeliveryQueueEntries(rows *sql.Rows) ([]DeliveryQueueEntry, error) {
    var out []DeliveryQueueEntry
    for rows.Next() {
        var e DeliveryQueueEntry
        var nextRetry, created string
        var lastAttempt sql.NullString
        if err := rows.Scan(
            &e.ID, &e.WorkflowID, &e.RunID, &e.Endpoint, &e.Channel, &e.Payload,
            &e.Status, &e.AttemptCount, &e.MaxAttempts, &e.RetryWindowS,
            &nextRetry, &created, &lastAttempt, &e.LastError); err != nil {
            return nil, err
        }
        e.NextRetryAt, _ = time.Parse(time.RFC3339, nextRetry)
        e.CreatedAt, _ = time.Parse(time.RFC3339, created)
        if lastAttempt.Valid {
            t, _ := time.Parse(time.RFC3339, lastAttempt.String)
            e.LastAttemptAt = &t
        }
        out = append(out, e)
    }
    if out == nil {
        out = []DeliveryQueueEntry{}
    }
    return out, rows.Err()
}

func nullTime(t *time.Time) *string {
    if t == nil {
        return nil
    }
    s := t.UTC().Format(time.RFC3339)
    return &s
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestDeliveryQueueStore" -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/delivery_queue_store.go internal/scheduler/delivery_queue_store_test.go
git commit -m "feat(delivery): add DeliveryQueueStore SQLite CRUD"
```

---

## Task 5: DeliveryQueue struct — Enqueue with deduplication

**Files:**
- Create: `internal/scheduler/delivery_queue.go`
- Create: `internal/scheduler/delivery_queue_test.go`

- [ ] **Step 1: Write failing enqueue tests**

Create `internal/scheduler/delivery_queue_test.go`:

```go
package scheduler

import (
    "context"
    "encoding/json"
    "testing"
    "time"

    "github.com/scrypster/huginn/internal/notification"
    "github.com/scrypster/huginn/internal/sqlitedb"
)

func newTestDeliveryQueue(t *testing.T) *DeliveryQueue {
    t.Helper()
    db, err := sqlitedb.Open(":memory:")
    if err != nil {
        t.Fatalf("open db: %v", err)
    }
    if err := sqlitedb.Migrate(db, Migrations()); err != nil {
        t.Fatalf("migrate: %v", err)
    }
    t.Cleanup(func() { db.Close() })
    store := NewDeliveryQueueStore(db)
    return NewDeliveryQueue(store, NewDelivererRegistry(nil), nil, nil)
}

func TestDeliveryQueue_Enqueue_CreatesEntry(t *testing.T) {
    q := newTestDeliveryQueue(t)
    n := &notification.Notification{ID: "n1", WorkflowID: "wf1", RunID: "run1", Summary: "test"}
    target := NotificationDelivery{Type: "webhook", To: "https://hooks.slack.com/abc"}
    if err := q.Enqueue(context.Background(), "wf1", "run1", "*/10 * * * *", n, target); err != nil {
        t.Fatalf("Enqueue: %v", err)
    }
    entries, err := q.store.ListDue(time.Now().Add(time.Minute), 10)
    if err != nil {
        t.Fatalf("ListDue: %v", err)
    }
    if len(entries) != 1 {
        t.Fatalf("want 1 entry, got %d", len(entries))
    }
    if entries[0].WorkflowID != "wf1" || entries[0].Channel != "webhook" {
        t.Errorf("unexpected entry: %+v", entries[0])
    }
}

func TestDeliveryQueue_Enqueue_Deduplicates(t *testing.T) {
    q := newTestDeliveryQueue(t)
    n := &notification.Notification{ID: "n1", WorkflowID: "wf1", Summary: "first"}
    target := NotificationDelivery{Type: "webhook", To: "https://hooks.slack.com/abc"}
    _ = q.Enqueue(context.Background(), "wf1", "run1", "*/10 * * * *", n, target)

    n2 := &notification.Notification{ID: "n2", WorkflowID: "wf1", Summary: "second"}
    _ = q.Enqueue(context.Background(), "wf1", "run2", "*/10 * * * *", n2, target)

    // Only the second (newest) should be active
    due, _ := q.store.ListDue(time.Now().Add(time.Minute), 10)
    if len(due) != 1 {
        t.Fatalf("want 1 active entry after dedup, got %d", len(due))
    }
    var payload DeliveryQueuePayload
    _ = json.Unmarshal([]byte(due[0].Payload), &payload)
    if payload.Notification.Summary != "second" {
        t.Errorf("wrong entry kept, want 'second', got %q", payload.Notification.Summary)
    }
}

func TestDeliveryQueue_Enqueue_RetryWindowFromSchedule(t *testing.T) {
    q := newTestDeliveryQueue(t)
    n := &notification.Notification{ID: "n1", WorkflowID: "wf1"}
    target := NotificationDelivery{Type: "webhook", To: "https://x.com"}
    _ = q.Enqueue(context.Background(), "wf1", "run1", "*/10 * * * *", n, target)
    due, _ := q.store.ListDue(time.Now().Add(time.Minute), 10)
    if len(due) == 0 {
        t.Fatal("no entry")
    }
    // 10-min cron → retry_window = ~480s
    if due[0].RetryWindowS < 400 || due[0].RetryWindowS > 500 {
        t.Errorf("RetryWindowS = %d, want ~480", due[0].RetryWindowS)
    }
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestDeliveryQueue_Enqueue" -v 2>&1 | head -10
```

Expected: `undefined: DeliveryQueue`, `undefined: NewDeliveryQueue`

- [ ] **Step 3: Create delivery_queue.go with Enqueue**

```go
// internal/scheduler/delivery_queue.go
package scheduler

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "sync"
    "time"

    "github.com/google/uuid"
    "github.com/scrypster/huginn/internal/notification"
)

// DeliveryQueue manages the durable retry queue for failed webhook/email
// deliveries. It is safe for concurrent use.
type DeliveryQueue struct {
    store       *DeliveryQueueStore
    deliverers  *DelivererRegistry
    notifStore  notification.StoreInterface // may be nil
    broadcastFn WorkflowBroadcastFunc       // may be nil; used for badge WS events
    mu          sync.Mutex
}

// NewDeliveryQueue constructs a DeliveryQueue. deliverers and notifStore may
// be nil (delivery and escalation are skipped respectively).
func NewDeliveryQueue(
    store *DeliveryQueueStore,
    deliverers *DelivererRegistry,
    notifStore notification.StoreInterface,
    broadcastFn WorkflowBroadcastFunc,
) *DeliveryQueue {
    return &DeliveryQueue{
        store:       store,
        deliverers:  deliverers,
        notifStore:  notifStore,
        broadcastFn: broadcastFn,
    }
}

// Enqueue adds a failed delivery to the durable queue. Any existing
// pending/retrying entry for the same (workflowID, target endpoint) is
// superseded first (dedup). schedule is the workflow's cron expression and
// is used to derive retry_window_s.
func (q *DeliveryQueue) Enqueue(
    ctx context.Context,
    workflowID, runID, schedule string,
    n *notification.Notification,
    target NotificationDelivery,
) error {
    payload := DeliveryQueuePayload{
        Notification: *n,
        Target:       target,
    }
    payloadJSON, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("delivery queue: marshal payload: %w", err)
    }
    retryWindow := ComputeRetryWindow(schedule)
    entry := DeliveryQueueEntry{
        ID:           uuid.New().String(),
        WorkflowID:   workflowID,
        RunID:        runID,
        Endpoint:     endpointKey(target),
        Channel:      target.Type,
        Payload:      string(payloadJSON),
        Status:       "pending",
        AttemptCount: 0,
        MaxAttempts:  5,
        RetryWindowS: retryWindow,
        NextRetryAt:  time.Now().UTC(), // attempt 0: fire on next poll tick
    }
    if err := q.store.SupersedeAndInsert(entry); err != nil {
        return fmt.Errorf("delivery queue: enqueue: %w", err)
    }
    slog.Info("delivery queue: enqueued entry",
        "workflow_id", workflowID, "run_id", runID,
        "channel", target.Type, "endpoint", entry.Endpoint,
        "retry_window_s", retryWindow)
    return nil
}

// BadgeCount returns the number of distinct (workflow_id, endpoint) pairs
// with permanently failed deliveries. Used by the nav badge API.
func (q *DeliveryQueue) BadgeCount() (int, error) {
    return q.store.BadgeCount()
}

// ListActionable returns entries needing user attention (status=failed).
func (q *DeliveryQueue) ListActionable(limit int) ([]DeliveryQueueEntry, error) {
    return q.store.ListActionable(limit)
}

// Dismiss removes a failed entry from the queue (and the badge count).
func (q *DeliveryQueue) Dismiss(id string) error {
    if err := q.store.Dismiss(id); err != nil {
        return err
    }
    q.broadcastBadge()
    return nil
}

// ForceRetry resets a failed/exhausted entry to pending with next_retry_at=now,
// closes its circuit breaker if open, then triggers an immediate worker sweep.
// Returns an error if the entry does not exist.
func (q *DeliveryQueue) ForceRetry(ctx context.Context, id string) error {
    entry, err := q.store.Get(id)
    if err != nil {
        return fmt.Errorf("entry not found: %w", err)
    }
    next := time.Now().UTC()
    if err := q.store.UpdateAttempt(entry.ID, "pending", entry.AttemptCount, "", &next); err != nil {
        return fmt.Errorf("reset entry: %w", err)
    }
    health, _ := q.store.GetHealth(entry.WorkflowID, entry.Endpoint)
    if health.CircuitState == "open" {
        health.CircuitState = "closed"
        health.ConsecutiveFailures = 0
        _ = q.store.UpsertHealth(health)
    }
    go q.RunOnce(ctx)
    return nil
}

// broadcastBadge emits a delivery_badge_update WS event with the current count.
func (q *DeliveryQueue) broadcastBadge() {
    if q.broadcastFn == nil {
        return
    }
    count, err := q.store.BadgeCount()
    if err != nil {
        slog.Warn("delivery queue: badge count error", "err", err)
        return
    }
    q.broadcastFn("delivery_badge_update", map[string]any{"count": count})
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestDeliveryQueue_Enqueue" -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/delivery_queue.go internal/scheduler/delivery_queue_test.go
git commit -m "feat(delivery): add DeliveryQueue.Enqueue with deduplication"
```

---

## Task 6: Background worker + circuit breaker

**Files:**
- Modify: `internal/scheduler/delivery_queue.go`
- Modify: `internal/scheduler/delivery_queue_test.go`

- [ ] **Step 1: Write failing worker tests**

Append to `internal/scheduler/delivery_queue_test.go`:

```go
func TestDeliveryQueue_Worker_DeliverSuccess(t *testing.T) {
    q := newTestDeliveryQueue(t)
    // Swap in a registry with a mock webhook deliverer that always succeeds
    q.deliverers = &DelivererRegistry{m: map[string]Deliverer{
        "webhook": &mockDeliverer{status: "sent"},
    }}
    n := &notification.Notification{ID: "n1", WorkflowID: "wf1"}
    target := NotificationDelivery{Type: "webhook", To: "https://x.com"}
    _ = q.Enqueue(context.Background(), "wf1", "run1", "*/10 * * * *", n, target)

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    q.RunOnce(ctx)

    due, _ := q.store.ListDue(time.Now().Add(time.Minute), 10)
    if len(due) != 0 {
        t.Errorf("entry still pending after successful delivery")
    }
    got, _ := q.store.Get(q.lastEnqueuedID)
    if got.Status != "delivered" {
        t.Errorf("want status=delivered, got %q", got.Status)
    }
}

func TestDeliveryQueue_Worker_CircuitOpensAfter5Failures(t *testing.T) {
    q := newTestDeliveryQueue(t)
    q.deliverers = &DelivererRegistry{m: map[string]Deliverer{
        "webhook": &mockDeliverer{status: "failed", errMsg: "connection refused"},
    }}
    n := &notification.Notification{ID: "n1", WorkflowID: "wf1"}
    target := NotificationDelivery{Type: "webhook", To: "https://x.com"}
    _ = q.Enqueue(context.Background(), "wf1", "run1", "", n, target)
    // Get the entry
    due, _ := q.store.ListDue(time.Now().Add(time.Minute), 10)
    if len(due) == 0 {
        t.Fatal("no entry")
    }
    entryID := due[0].ID
    endpoint := due[0].Endpoint

    ctx := context.Background()
    for i := 0; i < circuitBreakThreshold; i++ {
        q.attemptDelivery(ctx, due[0])
        // re-fetch for next iteration
        due[0], _ = q.store.Get(entryID)
    }
    health, _ := q.store.GetHealth("wf1", endpoint)
    if health.CircuitState != "open" {
        t.Errorf("circuit should be open after %d failures, got %q", circuitBreakThreshold, health.CircuitState)
    }
}

// mockDeliverer is a test double for Deliverer.
type mockDeliverer struct {
    status string
    errMsg string
}

func (m *mockDeliverer) Deliver(_ context.Context, _ *notification.Notification, _ NotificationDelivery) notification.DeliveryRecord {
    return notification.DeliveryRecord{Status: m.status, Error: m.errMsg}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestDeliveryQueue_Worker" -v 2>&1 | head -15
```

Expected: `q.runOnce undefined`, `q.attemptDelivery undefined`, `q.lastEnqueuedID undefined`

- [ ] **Step 3: Add worker + circuit breaker to delivery_queue.go**

Add these methods to `DeliveryQueue` in `delivery_queue.go`:

```go
// lastEnqueuedID is set during Enqueue for testability.
// In production this is always overwritten; only tests read it.
// guarded by mu.
var lastEnqueuedID string // package-level for test access; set in Enqueue

// StartWorker launches the background retry goroutine. Call once at startup.
// The goroutine stops when ctx is cancelled.
func (q *DeliveryQueue) StartWorker(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()
        // Run immediately on startup to pick up any pending entries.
        q.RunOnce(ctx)
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                q.RunOnce(ctx)
            }
        }
    }()
}

// RunOnce processes all due entries once. Exported so the retry API handler
// can trigger an immediate sweep after resetting an entry's next_retry_at.
func (q *DeliveryQueue) RunOnce(ctx context.Context) {
    entries, err := q.store.ListDue(time.Now().UTC(), 50)
    if err != nil {
        slog.Error("delivery queue: list due entries", "err", err)
        return
    }
    for _, e := range entries {
        if ctx.Err() != nil {
            return
        }
        q.attemptDelivery(ctx, e)
    }
}

// attemptDelivery runs one delivery attempt for the given entry.
func (q *DeliveryQueue) attemptDelivery(ctx context.Context, e DeliveryQueueEntry) {
    // Check circuit breaker.
    health, err := q.store.GetHealth(e.WorkflowID, e.Endpoint)
    if err != nil {
        slog.Error("delivery queue: get health", "err", err)
        return
    }
    if health.CircuitState == "open" {
        // Probe once per retry_window_s when circuit is open.
        if health.LastProbeAt != nil {
            sinceProbe := time.Since(*health.LastProbeAt)
            if sinceProbe < time.Duration(e.RetryWindowS)*time.Second {
                return // too soon to probe
            }
        }
        slog.Info("delivery queue: circuit open — probing", "workflow_id", e.WorkflowID, "endpoint", e.Endpoint)
    }

    // Decode payload.
    var payload DeliveryQueuePayload
    if err := json.Unmarshal([]byte(e.Payload), &payload); err != nil {
        slog.Error("delivery queue: decode payload", "id", e.ID, "err", err)
        return
    }

    // Attempt delivery.
    d := q.deliverers.get(e.Channel)
    if d == nil {
        slog.Warn("delivery queue: no deliverer for channel", "channel", e.Channel)
        return
    }
    rec := d.Deliver(ctx, &payload.Notification, payload.Target)
    now := time.Now().UTC()
    e.AttemptCount++

    if rec.Status == "sent" {
        // Success: mark delivered, close circuit.
        _ = q.store.MarkDelivered(e.ID)
        health.ConsecutiveFailures = 0
        health.CircuitState = "closed"
        health.OpenedAt = nil
        _ = q.store.UpsertHealth(health)
        slog.Info("delivery queue: delivered", "id", e.ID, "workflow_id", e.WorkflowID)
        q.broadcastBadge()
        return
    }

    // Failure: update health.
    health.ConsecutiveFailures++
    health.LastProbeAt = &now
    if health.CircuitState == "open" {
        // Already open — just update probe time.
        _ = q.store.UpsertHealth(health)
    } else if health.ConsecutiveFailures >= circuitBreakThreshold {
        // Open the circuit.
        health.CircuitState = "open"
        health.OpenedAt = &now
        _ = q.store.UpsertHealth(health)
        slog.Warn("delivery queue: circuit opened",
            "workflow_id", e.WorkflowID, "endpoint", e.Endpoint,
            "consecutive_failures", health.ConsecutiveFailures)
    } else {
        _ = q.store.UpsertHealth(health)
    }

    // Check exhaustion.
    if e.AttemptCount >= e.MaxAttempts {
        _ = q.store.UpdateAttempt(e.ID, "failed", e.AttemptCount, rec.Error, nil)
        slog.Warn("delivery queue: exhausted", "id", e.ID, "workflow_id", e.WorkflowID, "last_error", rec.Error)
        q.escalate(e, payload, rec.Error)
        q.broadcastBadge()
        return
    }

    // Schedule next retry.
    delay := nextRetryDelay(e.RetryWindowS, e.AttemptCount)
    next := now.Add(delay)
    _ = q.store.UpdateAttempt(e.ID, "retrying", e.AttemptCount, rec.Error, &next)
    slog.Info("delivery queue: scheduled retry",
        "id", e.ID, "attempt", e.AttemptCount, "next_retry_at", next)
}

// escalate fires an inbox notification when all retries are exhausted.
func (q *DeliveryQueue) escalate(e DeliveryQueueEntry, payload DeliveryQueuePayload, lastError string) {
    if q.notifStore == nil {
        return
    }
    endpointDisplay := e.Endpoint
    if len(endpointDisplay) > 50 {
        endpointDisplay = endpointDisplay[:47] + "..."
    }
    n := notification.Notification{
        ID:         fmt.Sprintf("dlq-escalation-%s", e.ID),
        WorkflowID: e.WorkflowID,
        RunID:      e.RunID,
        Summary:    fmt.Sprintf("Delivery to %s permanently failed", endpointDisplay),
        Detail:     fmt.Sprintf("Workflow run %s could not deliver to %s after %d attempts. Last error: %s", e.RunID, e.Endpoint, e.AttemptCount, lastError),
        Severity:   notification.Severity("urgent"),
        Status:     notification.Status("pending"),
        CreatedAt:  time.Now().UTC(),
        UpdatedAt:  time.Now().UTC(),
    }
    if err := q.notifStore.Put(&n); err != nil {
        slog.Error("delivery queue: escalation notification failed", "err", err)
    }
}
```

Also update `Enqueue` to set `lastEnqueuedID` (for test inspection):

```go
// At the end of Enqueue, before returning nil:
q.mu.Lock()
lastEnqueuedID = entry.ID
q.mu.Unlock()
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestDeliveryQueue" -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/delivery_queue.go internal/scheduler/delivery_queue_test.go
git commit -m "feat(delivery): add background worker, circuit breaker, inbox escalation"
```

---

## Task 7: Wire delivery queue into the workflow runner

**Files:**
- Modify: `internal/scheduler/runner_options.go`
- Modify: `internal/scheduler/workflow_runner.go`

- [ ] **Step 1: Add WithDeliveryQueue runner option**

In `internal/scheduler/runner_options.go`, add to the `runnerConfig` struct and add the option function:

```go
// in runnerConfig struct, add:
deliveryQueue *DeliveryQueue

// new option function:

// WithDeliveryQueue wires the durable delivery queue. When set, failed
// webhook and email deliveries are enqueued for automatic retry instead of
// being written to the JSONL dead-letter file. Pass nil to disable (default).
func WithDeliveryQueue(q *DeliveryQueue) RunnerOption {
    return func(c *runnerConfig) {
        c.deliveryQueue = q
    }
}
```

- [ ] **Step 2: Wire queue into dispatchNotification**

In `internal/scheduler/workflow_runner.go`, find `dispatchNotification` (around line 995). Add `deliveryQueue *DeliveryQueue` and `schedule string` parameters, and replace the `WriteDeliveryFailure` call:

Change the function signature from:
```go
func dispatchNotification(
    n *notification.Notification,
    targets []NotificationDelivery,
    notifStore notification.StoreInterface,
    spaceDeliveryFn SpaceDeliveryFunc,
    agentDMFn AgentDMDeliveryFunc,
    stepAgent string,
    huginnDir string,
    deliverers *DelivererRegistry,
    onDeliveryFailure DeliveryFailureFunc,
) []notification.DeliveryRecord {
```

To:
```go
func dispatchNotification(
    n *notification.Notification,
    targets []NotificationDelivery,
    notifStore notification.StoreInterface,
    spaceDeliveryFn SpaceDeliveryFunc,
    agentDMFn AgentDMDeliveryFunc,
    stepAgent string,
    huginnDir string,
    deliverers *DelivererRegistry,
    onDeliveryFailure DeliveryFailureFunc,
    deliveryQueue *DeliveryQueue,
    schedule string,
) []notification.DeliveryRecord {
```

Then find the failure block (around line 1072-1079):

```go
// BEFORE (remove these lines):
if rec.Status != "sent" && huginnDir != "" {
    WriteDeliveryFailure(huginnDir, n.WorkflowID, n.RunID, t.To, 3, rec.Error)
}
```

Replace with:

```go
// AFTER:
if rec.Status != "sent" {
    if deliveryQueue != nil {
        if err := deliveryQueue.Enqueue(context.Background(), n.WorkflowID, n.RunID, schedule, n, t); err != nil {
            slog.Error("scheduler: enqueue delivery failure", "err", err)
        }
    } else if huginnDir != "" {
        // Fallback: legacy dead-letter file when queue not configured.
        WriteDeliveryFailure(huginnDir, n.WorkflowID, n.RunID, t.To, 3, rec.Error)
    }
}
```

- [ ] **Step 3: Update the two dispatchNotification call sites**

In `workflow_runner.go`, find the two calls to `dispatchNotification` (around lines 587 and 699) and add the two new trailing arguments. Both calls are inside `MakeWorkflowRunner`'s closure where `w` (the Workflow) is in scope:

```go
// Step-level call (around line 587):
deliveries := dispatchNotification(n, step.Notify.DeliverTo, notifStore, spaceDeliveryFn,
    cfg.agentDM, step.Agent, huginnDir, deliverers, onDeliveryFailure,
    cfg.deliveryQueue, w.Schedule)

// Workflow-level call (around line 699):
deliveries := dispatchNotification(n, w.Notification.DeliverTo, notifStore, spaceDeliveryFn,
    cfg.agentDM, "", huginnDir, deliverers, onDeliveryFailure,
    cfg.deliveryQueue, w.Schedule)
```

- [ ] **Step 4: Build to verify no compile errors**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./internal/scheduler/...
```

Expected: no errors.

- [ ] **Step 5: Run existing workflow runner tests**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -run "TestWorkflow" -v -count=1 2>&1 | tail -20
```

Expected: all PASS (no regressions).

- [ ] **Step 6: Commit**

```bash
git add internal/scheduler/runner_options.go internal/scheduler/workflow_runner.go
git commit -m "feat(delivery): wire delivery queue into dispatchNotification"
```

---

## Task 8: Wire delivery queue into Scheduler

**Files:**
- Modify: `internal/scheduler/scheduler.go`

- [ ] **Step 1: Add deliveryQueue field and SetDeliveryQueue**

In `scheduler.go`, add to the `Scheduler` struct:

```go
deliveryQueue *DeliveryQueue // optional; started in Start() if set
```

Add a setter method after `SetBroadcastFunc`:

```go
// SetDeliveryQueue wires the durable delivery queue and starts its background
// worker when Start() is called (or immediately if already started).
func (s *Scheduler) SetDeliveryQueue(q *DeliveryQueue) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.deliveryQueue = q
}
```

- [ ] **Step 2: Start worker in Start()**

Change `Start()` from:

```go
func (s *Scheduler) Start() {
    s.cron.Start()
}
```

To:

```go
func (s *Scheduler) Start() {
    s.cron.Start()
    s.mu.Lock()
    q := s.deliveryQueue
    s.mu.Unlock()
    if q != nil {
        q.StartWorker(context.Background())
    }
}
```

Add `"context"` to imports if not present.

- [ ] **Step 3: Build**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./internal/scheduler/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/scheduler/scheduler.go
git commit -m "feat(delivery): add SetDeliveryQueue to Scheduler, start worker in Start()"
```

---

## Task 9: JSONL dead-letter migration on startup

**Files:**
- Modify: `internal/scheduler/migrate.go`

- [ ] **Step 1: Add migration function**

Append to `internal/scheduler/migrate.go`:

```go
// MigrateDeadLetterToQueue reads existing JSONL dead-letter files from
// <huginnDir>/delivery-failures/ and imports unretried records into the
// SQLite delivery_queue as status=failed (exhausted). This is a one-time
// migration; files are renamed to .migrated after import.
// The function is idempotent: already-imported records (matched by workflow_id+run_id+endpoint)
// are skipped via INSERT OR IGNORE.
func MigrateDeadLetterToQueue(huginnDir string, store *DeliveryQueueStore) error {
    dir := filepath.Join(huginnDir, "delivery-failures")
    entries, err := os.ReadDir(dir)
    if err != nil {
        if os.IsNotExist(err) {
            return nil // nothing to migrate
        }
        return fmt.Errorf("migrate dead-letter: read dir: %w", err)
    }
    for _, entry := range entries {
        name := entry.Name()
        if !strings.HasSuffix(name, ".jsonl") {
            continue
        }
        path := filepath.Join(dir, name)
        recs, err := readFailureFile(path)
        if err != nil {
            slog.Warn("migrate dead-letter: skip unreadable file", "path", path, "err", err)
            continue
        }
        for _, rec := range recs {
            if rec.RetriedAt != "" {
                continue // already retried; skip
            }
            e := DeliveryQueueEntry{
                ID:           fmt.Sprintf("migrated-%s-%s", rec.WorkflowID, rec.RunID),
                WorkflowID:   rec.WorkflowID,
                RunID:        rec.RunID,
                Endpoint:     rec.URL,
                Channel:      "webhook",
                Payload:      `{}`,
                Status:       "failed",
                AttemptCount: rec.Attempts,
                MaxAttempts:  rec.Attempts,
                RetryWindowS: 3600,
                NextRetryAt:  time.Now().UTC(),
                LastError:    rec.LastError,
            }
            if insertErr := store.Insert(e); insertErr != nil {
                // Ignore duplicate key — already migrated on a previous run.
                if !strings.Contains(insertErr.Error(), "UNIQUE constraint") {
                    slog.Warn("migrate dead-letter: insert entry", "err", insertErr)
                }
            }
        }
        // Rename file to mark as migrated.
        _ = os.Rename(path, path+".migrated")
    }
    return nil
}
```

Add required imports to the file if not present: `"fmt"`, `"os"`, `"path/filepath"`, `"strings"`, `"time"`.

- [ ] **Step 2: Build**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./internal/scheduler/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/scheduler/migrate.go
git commit -m "feat(delivery): add MigrateDeadLetterToQueue for JSONL→SQLite migration"
```

---

## Task 10: API handlers for delivery queue

**Files:**
- Modify: `internal/server/handlers_workflows.go`
- Modify: `internal/server/server.go`

- [ ] **Step 1: Add deliveryQueue field + setter to Server**

In `internal/server/server.go`, add to the `Server` struct:

```go
deliveryQueue *scheduler.DeliveryQueue // optional
```

Add a setter after `SetWorkflowRunStore`:

```go
// SetDeliveryQueue wires the durable delivery queue for API access.
func (s *Server) SetDeliveryQueue(q *scheduler.DeliveryQueue) {
    s.deliveryQueue = q
}
```

- [ ] **Step 2: Replace old routes with new delivery-queue routes**

In `server.go`, find and replace:

```go
// REMOVE:
mux.HandleFunc("GET /api/v1/delivery-failures",      api(s.handleListDeliveryFailures))
mux.HandleFunc("POST /api/v1/delivery-failures/retry", api(s.handleRetryDeliveryFailure))
```

With:

```go
// ADD:
mux.HandleFunc("GET /api/v1/delivery-queue",              api(s.handleListDeliveryQueue))
mux.HandleFunc("GET /api/v1/delivery-queue/badge",        api(s.handleDeliveryQueueBadge))
mux.HandleFunc("POST /api/v1/delivery-queue/{id}/retry",  api(s.handleRetryDeliveryQueueEntry))
mux.HandleFunc("DELETE /api/v1/delivery-queue/{id}",      api(s.handleDismissDeliveryQueueEntry))
```

- [ ] **Step 3: Add new handlers to handlers_workflows.go**

Replace the bodies of `handleListDeliveryFailures` and `handleRetryDeliveryFailure` with the new implementations. Add the new handlers at the end of the file:

```go
// handleListDeliveryQueue returns actionable (failed) delivery queue entries.
//
//	GET /api/v1/delivery-queue
func (s *Server) handleListDeliveryQueue(w http.ResponseWriter, r *http.Request) {
    if s.deliveryQueue == nil {
        jsonOK(w, []any{})
        return
    }
    entries, err := s.deliveryQueue.ListActionable(100)
    if err != nil {
        jsonError(w, 500, "list delivery queue: "+err.Error())
        return
    }
    jsonOK(w, entries)
}

// handleDeliveryQueueBadge returns the badge count for the nav indicator.
//
//	GET /api/v1/delivery-queue/badge
func (s *Server) handleDeliveryQueueBadge(w http.ResponseWriter, r *http.Request) {
    if s.deliveryQueue == nil {
        jsonOK(w, map[string]int{"count": 0})
        return
    }
    count, err := s.deliveryQueue.BadgeCount()
    if err != nil {
        jsonError(w, 500, "badge count: "+err.Error())
        return
    }
    jsonOK(w, map[string]int{"count": count})
}

// handleRetryDeliveryQueueEntry forces immediate retry of a queue entry.
//
//	POST /api/v1/delivery-queue/{id}/retry
func (s *Server) handleRetryDeliveryQueueEntry(w http.ResponseWriter, r *http.Request) {
    if s.deliveryQueue == nil {
        jsonError(w, 503, "delivery queue not configured")
        return
    }
    id := r.PathValue("id")
    if id == "" {
        jsonError(w, 400, "missing id")
        return
    }
    if err := s.deliveryQueue.ForceRetry(r.Context(), id); err != nil {
        jsonError(w, 404, "retry failed: "+err.Error())
        return
    }
    jsonOK(w, map[string]string{"status": "retrying", "id": id})
}

// handleDismissDeliveryQueueEntry removes a failed entry from the queue.
//
//	DELETE /api/v1/delivery-queue/{id}
func (s *Server) handleDismissDeliveryQueueEntry(w http.ResponseWriter, r *http.Request) {
    if s.deliveryQueue == nil {
        jsonError(w, 503, "delivery queue not configured")
        return
    }
    id := r.PathValue("id")
    if id == "" {
        jsonError(w, 400, "missing id")
        return
    }
    if err := s.deliveryQueue.Dismiss(id); err != nil {
        jsonError(w, 500, "dismiss: "+err.Error())
        return
    }
    jsonOK(w, map[string]string{"status": "dismissed", "id": id})
}
```

Verify `handlers_workflows.go` imports include `"context"` if needed; `"time"` is no longer required by these handlers directly.

- [ ] **Step 4: Delete old handler functions**

In `handlers_workflows.go`, delete the bodies of `handleRetryDeliveryFailure` (lines ~479-531) and `handleListDeliveryFailures` (lines ~533-544). These are now replaced by the four new handlers above.

- [ ] **Step 5: Build**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./internal/server/...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers_workflows.go internal/server/server.go
git commit -m "feat(delivery): replace delivery-failures API with delivery-queue endpoints"
```

---

## Task 11: Wire everything in main.go

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Create DeliveryQueue and wire to scheduler + runner + server**

In `main.go`, find the block where `workflowRunStore` is created (around line 2498). After it, add:

```go
// Delivery queue (requires SQLite; falls back to nil = legacy JSONL mode).
var deliveryQueue *scheduler.DeliveryQueue
if sqlDB != nil {
    dqStore := scheduler.NewDeliveryQueueStore(sqlDB)
    if err := scheduler.MigrateDeadLetterToQueue(huginnHome, dqStore); err != nil {
        logger.Warn("huginn: dead-letter migration", "err", err)
    }
    deliveryQueue = scheduler.NewDeliveryQueue(dqStore, wfDeliverers, notifStore,
        func(eventType string, payload map[string]any) {
            srv.BroadcastWS(server.WSMessage{Type: eventType, Payload: payload})
        })
    sched.SetDeliveryQueue(deliveryQueue)
    srv.SetDeliveryQueue(deliveryQueue)
}
```

- [ ] **Step 2: Add WithDeliveryQueue to MakeWorkflowRunner call**

Find the `MakeWorkflowRunner(...)` call (around line 2519) and add:

```go
scheduler.WithDeliveryQueue(deliveryQueue),
```

as an additional option.

- [ ] **Step 3: Build the whole app**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Run all scheduler tests**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./internal/scheduler/... -v -count=1 2>&1 | tail -30
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat(delivery): wire DeliveryQueue into main.go — full backend integration"
```

---

## Task 12: Vue composable — useDeliveryQueue

**Files:**
- Create: `web/src/composables/useDeliveryQueue.ts`

- [ ] **Step 1: Create composable**

```typescript
// web/src/composables/useDeliveryQueue.ts
import { ref, computed } from 'vue'

export interface DeliveryQueueEntry {
  id: string
  workflow_id: string
  run_id: string
  endpoint: string
  channel: 'webhook' | 'email'
  status: 'pending' | 'retrying' | 'delivered' | 'failed' | 'superseded'
  attempt_count: number
  max_attempts: number
  retry_window_s: number
  next_retry_at: string
  created_at: string
  last_attempt_at?: string
  last_error?: string
}

const badgeCount = ref(0)
const actionableEntries = ref<DeliveryQueueEntry[]>([])
const loading = ref(false)

export function useDeliveryQueue() {
  async function fetchBadge(): Promise<void> {
    try {
      const res = await fetch('/api/v1/delivery-queue/badge')
      if (!res.ok) return
      const data = await res.json()
      badgeCount.value = data.count ?? 0
    } catch {
      // Non-critical — badge stays at last value
    }
  }

  async function fetchActionable(): Promise<void> {
    loading.value = true
    try {
      const res = await fetch('/api/v1/delivery-queue')
      if (!res.ok) return
      actionableEntries.value = await res.json()
      badgeCount.value = actionableEntries.value.length
    } finally {
      loading.value = false
    }
  }

  async function retryEntry(id: string): Promise<void> {
    const res = await fetch(`/api/v1/delivery-queue/${id}/retry`, { method: 'POST' })
    if (!res.ok) throw new Error(`retry failed: ${res.status}`)
    await fetchActionable()
  }

  async function dismissEntry(id: string): Promise<void> {
    const res = await fetch(`/api/v1/delivery-queue/${id}`, { method: 'DELETE' })
    if (!res.ok) throw new Error(`dismiss failed: ${res.status}`)
    actionableEntries.value = actionableEntries.value.filter(e => e.id !== id)
    badgeCount.value = actionableEntries.value.length
  }

  // Call this from the WS message handler when a delivery_badge_update event arrives.
  function handleBadgeUpdate(count: number): void {
    badgeCount.value = count
    if (count > 0) fetchActionable()
  }

  const hasIssues = computed(() => badgeCount.value > 0)

  return {
    badgeCount,
    actionableEntries,
    loading,
    hasIssues,
    fetchBadge,
    fetchActionable,
    retryEntry,
    dismissEntry,
    handleBadgeUpdate,
  }
}
```

- [ ] **Step 2: Add unit test**

Create `web/src/composables/__tests__/useDeliveryQueue.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useDeliveryQueue } from '../useDeliveryQueue'

describe('useDeliveryQueue', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('fetchBadge updates badgeCount', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ count: 3 }),
    } as Response)
    const { badgeCount, fetchBadge } = useDeliveryQueue()
    await fetchBadge()
    expect(badgeCount.value).toBe(3)
  })

  it('dismissEntry removes entry from list', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true } as Response)
    const { actionableEntries, dismissEntry } = useDeliveryQueue()
    actionableEntries.value = [
      { id: 'e1', workflow_id: 'w1', run_id: 'r1', endpoint: 'x', channel: 'webhook',
        status: 'failed', attempt_count: 5, max_attempts: 5, retry_window_s: 480,
        next_retry_at: '', created_at: '' }
    ]
    await dismissEntry('e1')
    expect(actionableEntries.value).toHaveLength(0)
  })

  it('handleBadgeUpdate sets count', () => {
    const { badgeCount, handleBadgeUpdate } = useDeliveryQueue()
    handleBadgeUpdate(7)
    expect(badgeCount.value).toBe(7)
  })
})
```

- [ ] **Step 3: Run frontend tests**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web && npm run test -- --testPathPattern="useDeliveryQueue" 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/composables/useDeliveryQueue.ts web/src/composables/__tests__/useDeliveryQueue.test.ts
git commit -m "feat(delivery): add useDeliveryQueue Vue composable"
```

---

## Task 13: Deliveries tab in WorkflowsView run detail

**Files:**
- Modify: `web/src/views/WorkflowsView.vue`

- [ ] **Step 1: Import and wire useDeliveryQueue**

In `WorkflowsView.vue`, in the `<script setup>` block, add:

```typescript
import { useDeliveryQueue } from '@/composables/useDeliveryQueue'
const { actionableEntries, retryEntry, dismissEntry } = useDeliveryQueue()

// Tab state for run detail view
const runDetailTab = ref<'steps' | 'deliveries'>('steps')

// Entries for the currently selected run
const runDeliveries = computed(() =>
  actionableEntries.value.filter(e => e.run_id === selectedRun.value?.id)
)
```

- [ ] **Step 2: Add Deliveries tab to the run detail template**

Find the section that renders step results in the run detail (look for the steps tab or run detail panel in the template). Add a tab bar and the deliveries panel. Insert after the existing steps tab header:

```html
<!-- Tab bar -->
<div class="flex gap-4 border-b border-huginn-border mb-3 text-xs">
  <button
    @click="runDetailTab = 'steps'"
    :class="runDetailTab === 'steps'
      ? 'text-huginn-text border-b-2 border-huginn-blue pb-1'
      : 'text-huginn-muted pb-1'"
  >Steps</button>
  <button
    @click="runDetailTab = 'deliveries'"
    :class="runDetailTab === 'deliveries'
      ? 'text-huginn-text border-b-2 border-huginn-blue pb-1'
      : 'text-huginn-muted pb-1'"
  >
    Deliveries
    <span v-if="runDeliveries.length > 0"
      class="ml-1 bg-huginn-red text-white text-[8px] font-bold rounded-full px-1">
      {{ runDeliveries.length }}
    </span>
  </button>
</div>

<!-- Steps panel (existing content) — wrap with v-if -->
<div v-if="runDetailTab === 'steps'">
  <!-- existing steps rendering here -->
</div>

<!-- Deliveries panel -->
<div v-else class="flex flex-col gap-2">
  <div v-if="runDeliveries.length === 0" class="text-huginn-muted text-xs py-4 text-center">
    All deliveries successful
  </div>
  <div v-for="entry in runDeliveries" :key="entry.id"
    class="bg-huginn-surface rounded-lg p-3 border border-huginn-border text-xs">
    <div class="flex items-center justify-between gap-2">
      <div class="flex items-center gap-2 min-w-0">
        <span :class="entry.channel === 'webhook' ? 'text-huginn-blue' : 'text-purple-400'">
          {{ entry.channel }}
        </span>
        <span class="text-huginn-muted truncate">{{ entry.endpoint }}</span>
      </div>
      <div class="flex items-center gap-2 flex-shrink-0">
        <span class="text-huginn-red">failed after {{ entry.attempt_count }} attempts</span>
        <button @click="retryEntry(entry.id)"
          class="px-2 py-0.5 bg-huginn-blue/20 text-huginn-blue rounded hover:bg-huginn-blue/30 text-xs">
          Retry
        </button>
        <button @click="dismissEntry(entry.id)"
          class="px-2 py-0.5 bg-huginn-surface text-huginn-muted rounded hover:text-huginn-text text-xs">
          Dismiss
        </button>
      </div>
    </div>
    <div v-if="entry.last_error" class="text-huginn-muted mt-1 truncate">
      {{ entry.last_error }}
    </div>
  </div>
</div>
```

- [ ] **Step 3: Build and verify no TS errors**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web && npm run type-check 2>&1 | tail -10
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/views/WorkflowsView.vue
git commit -m "feat(delivery): add Deliveries tab to workflow run detail"
```

---

## Task 14: Global nav badge + delivery drawer in App.vue

**Files:**
- Modify: `web/src/App.vue`

- [ ] **Step 1: Import useDeliveryQueue and wire badge**

In `App.vue` `<script setup>`, add:

```typescript
import { useDeliveryQueue } from '@/composables/useDeliveryQueue'
const { badgeCount, actionableEntries, hasIssues, fetchBadge, fetchActionable, retryEntry, dismissEntry, handleBadgeUpdate } = useDeliveryQueue()

const drawerOpen = ref(false)

// Fetch badge on mount
onMounted(() => { fetchBadge() })

// Wire WS event — find the existing WS message handler and add this case:
// case 'delivery_badge_update':
//   handleBadgeUpdate(msg.payload.count)
//   break
```

Find the existing WS `onmessage` / message handler in App.vue and add the `delivery_badge_update` case to it.

- [ ] **Step 2: Add badge to the Automation nav item**

Find the nav badge block (around line 43 where inbox badge is). Add after the chat badge block:

```html
<!-- Delivery queue badge — shown only on automation section or always visible -->
<span v-if="hasIssues"
  class="absolute -top-0.5 -right-0.5 w-3.5 h-3.5 rounded-full bg-huginn-red text-white text-[8px] font-bold flex items-center justify-center leading-none"
  @click.stop="drawerOpen = true">
  {{ badgeCount > 9 ? '9+' : badgeCount }}
</span>
```

Place this inside the nav button for `item.section === 'automation'` (similar to how `inbox` and `chat` badges are positioned).

- [ ] **Step 3: Add the delivery drawer**

At the end of `App.vue` template, before the closing `</div>`, add:

```html
<!-- Delivery issues drawer -->
<Transition name="slide-right">
  <div v-if="drawerOpen"
    class="fixed right-0 top-0 h-full w-80 bg-huginn-bg border-l border-huginn-border z-50 flex flex-col shadow-xl">
    <div class="flex items-center justify-between p-4 border-b border-huginn-border">
      <span class="text-sm font-semibold text-huginn-text">Delivery Issues</span>
      <button @click="drawerOpen = false" class="text-huginn-muted hover:text-huginn-text">✕</button>
    </div>
    <div class="flex-1 overflow-y-auto p-3 flex flex-col gap-2">
      <div v-if="actionableEntries.length === 0"
        class="text-huginn-muted text-xs text-center py-8">
        No delivery issues
      </div>
      <div v-for="entry in actionableEntries" :key="entry.id"
        class="bg-huginn-surface rounded-lg p-3 border border-huginn-border text-xs">
        <div class="text-huginn-muted mb-1 truncate">{{ entry.workflow_id }}</div>
        <div class="font-medium text-huginn-text truncate mb-1">{{ entry.endpoint }}</div>
        <div class="text-huginn-red mb-2">Failed after {{ entry.attempt_count }} attempts</div>
        <div v-if="entry.last_error" class="text-huginn-muted truncate mb-2">{{ entry.last_error }}</div>
        <div class="flex gap-2">
          <button @click="retryEntry(entry.id)"
            class="flex-1 py-1 bg-huginn-blue/20 text-huginn-blue rounded hover:bg-huginn-blue/30">
            Retry
          </button>
          <button @click="dismissEntry(entry.id)"
            class="px-2 py-1 text-huginn-muted hover:text-huginn-text rounded border border-huginn-border">
            Dismiss
          </button>
        </div>
      </div>
    </div>
    <div class="p-3 border-t border-huginn-border">
      <button @click="fetchActionable()"
        class="w-full text-xs text-huginn-muted hover:text-huginn-text">
        Refresh
      </button>
    </div>
  </div>
</Transition>

<!-- Drawer backdrop -->
<div v-if="drawerOpen"
  class="fixed inset-0 z-40 bg-black/20"
  @click="drawerOpen = false" />
```

Add slide-right transition to `<style>` in App.vue:

```css
.slide-right-enter-active,
.slide-right-leave-active {
  transition: transform 0.2s ease;
}
.slide-right-enter-from,
.slide-right-leave-to {
  transform: translateX(100%);
}
```

Also: when `drawerOpen` is set to `true`, call `fetchActionable()` to load the entries:

```typescript
watch(drawerOpen, (open) => {
  if (open) fetchActionable()
})
```

- [ ] **Step 4: Build and type-check**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web && npm run type-check 2>&1 | tail -10
```

Expected: no errors.

- [ ] **Step 5: Run existing App tests**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web && npm run test -- --testPathPattern="App.test" 2>&1 | tail -20
```

Expected: all PASS (no regressions).

- [ ] **Step 6: Commit**

```bash
git add web/src/App.vue
git commit -m "feat(delivery): add delivery badge and drawer to App.vue nav"
```

---

## Task 15: Full build + test verification

- [ ] **Step 1: Full Go test suite**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go test ./... -count=1 2>&1 | tail -30
```

Expected: all PASS.

- [ ] **Step 2: Full frontend test suite**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web && npm run test 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 3: Final build**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn && go build ./... && echo "BUILD OK"
```

Expected: `BUILD OK`

- [ ] **Step 4: Commit final state**

```bash
git add -A
git commit -m "feat(delivery): resilient delivery queue — full implementation complete"
```
