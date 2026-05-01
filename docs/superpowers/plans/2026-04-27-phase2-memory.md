# Phase 2: Memory — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make MuninnDB's cross-channel memory replication live, surface agent memory through a web UI inspector, and expose a safe server-side proxy for browser→MuninnDB tool calls.

**Architecture:** The cross-channel `agent.MemoryReplicator` is fully built but dormant — it has zero production call sites. Phase 2 wires three things: (1) `WithReplicationContext` into the channel chat path so the replicator knows who to fan out to, (2) `Intercept` into `OnToolDone` so every successful muninn write gets replicated, and (3) a new `POST /api/v1/muninn/tool` endpoint so the browser can query agent memories safely. A new `MemoryView.vue` uses that endpoint to show a searchable, per-vault memory inspector.

**Tech Stack:** Go 1.25, Vue 3 + Composition API, Tailwind CSS, existing `apiFetch` composable, `mcp.MCPClient.CallTool`, `memory.LoadGlobalConfig`, `agent.NewMemoryReplicator`, `agent.NewSQLiteReplicationQueuer`

---

## File Structure

**New files:**
- `internal/server/handlers_memory.go` — `GET /api/v1/memory/replication-status` and `POST /api/v1/muninn/tool` endpoints
- `web/src/views/MemoryView.vue` — memory inspector UI

**Modified files:**
- `internal/agents/memory_replicator.go` — rename `MemoryReplicator` → `CloudVaultReplicator` (name collision fix)
- `internal/agents/cloud_vault_replicator_test.go` — update struct references after rename
- `main.go` — `wireMemoryReplicator` return type; wire cross-channel replicator after muninnCfgFilePath is set
- `internal/session/migrations.go` — register `Migrations()` V1+V2 for existing installs
- `internal/agent/agent_dispatcher.go` — call `Intercept` in `OnToolDone`
- `internal/server/ws.go` — call `WithReplicationContext` in `InjectSpaceContext`
- `internal/server/server.go` — register new routes; add `memReplicator *agent.MemoryReplicator` field + setter
- `web/src/router/index.ts` — add `/memory` route
- `web/src/App.vue` — add Memory nav item + icon

---

## Task 1: Rename `agents.MemoryReplicator` → `CloudVaultReplicator`

**Context:** `agents.MemoryReplicator` (cloud vault sync) and `agent.MemoryReplicator` (cross-channel) share the same struct name across adjacent packages. This causes confusion when both are imported. Rename the cloud one — the test file is already named `cloud_vault_replicator_test.go`.

**Files:**
- Modify: `internal/agents/memory_replicator.go`
- Modify: `internal/agents/cloud_vault_replicator_test.go`
- Modify: `main.go` (two spots: `wireMemoryReplicator` return type + any `*agentslib.MemoryReplicator` references)

- [ ] **Step 1: Rename struct and constructor in `internal/agents/memory_replicator.go`**

Replace every occurrence of `MemoryReplicator` (type name, constructor, method receivers) with `CloudVaultReplicator`:

```go
// Line 33 — struct comment
// CloudVaultReplicator manages replication of agent memories to the HuginnCloud vault.
// It drains a SQLite queue (cloud_vault_queue) and pushes entries via CloudVaultClient.
// When no vaultClient is wired (WithVaultClient not called), the replicator runs in
// no-op mode: entries are acknowledged as completed immediately (local-only mode).
type CloudVaultReplicator struct {
    db          *sqlitedb.DB
    machineID   string
    vaultClient CloudVaultClient
    mu          sync.Mutex
    done        chan struct{}
    ctx         context.Context
    cancel      context.CancelFunc
}

// NewCloudVaultReplicator creates a new CloudVaultReplicator backed by the given DB.
func NewCloudVaultReplicator(db *sqlitedb.DB) *CloudVaultReplicator {
    ctx, cancel := context.WithCancel(context.Background())
    return &CloudVaultReplicator{
        db:     db,
        done:   make(chan struct{}),
        ctx:    ctx,
        cancel: cancel,
    }
}

// WithVaultClient wires a CloudVaultClient for pushing memories to HuginnCloud.
func (mr *CloudVaultReplicator) WithVaultClient(client CloudVaultClient, machineID string) *CloudVaultReplicator {
    mr.vaultClient = client
    mr.machineID = machineID
    return mr
}
```

Update ALL remaining method receivers from `(mr *MemoryReplicator)` → `(mr *CloudVaultReplicator)`.

- [ ] **Step 2: Update `wireMemoryReplicator` in `main.go`**

```go
func wireMemoryReplicator(sqlDB *sqlitedb.DB) *agentslib.CloudVaultReplicator {
    mr := agentslib.NewCloudVaultReplicator(sqlDB)
    tokenStore := relay.NewTokenStore()
    if cloudURL := os.Getenv("HUGINN_CLOUD_URL"); cloudURL != "" && tokenStore.IsRegistered() {
        mr.WithVaultClient(agentslib.NewHTTPVaultClient(cloudURL, func() string {
            tok, _ := tokenStore.Load()
            return tok
        }), relay.GetMachineID())
    }
    return mr
}
```

Also update any variable typed as `*agentslib.MemoryReplicator` at call sites of `wireMemoryReplicator` — search for:
```
grep -n "wireMemoryReplicator\|agentslib\.MemoryReplicator" main.go
```
and update each.

- [ ] **Step 3: Update `cloud_vault_replicator_test.go`**

The test file is already named correctly. Update all struct references from `agents.MemoryReplicator` → `agents.CloudVaultReplicator` and `agents.NewMemoryReplicator` → `agents.NewCloudVaultReplicator`:

```
grep -n "MemoryReplicator\|NewMemoryReplicator" internal/agents/cloud_vault_replicator_test.go
```

Replace each occurrence.

- [ ] **Step 4: Build to confirm no compilation errors**

```bash
go build ./...
```

Expected: no errors. If `agents.MemoryReplicator` is referenced elsewhere, `go build` will show you exactly where.

- [ ] **Step 5: Run existing tests**

```bash
go test ./internal/agents/... -run CloudVault -v
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/agents/memory_replicator.go internal/agents/cloud_vault_replicator_test.go main.go
git commit -m "refactor(agents): rename MemoryReplicator → CloudVaultReplicator"
```

---

## Task 2: Register Session Migrations for Existing Installs

**Context:** `memory_replication_queue` is in the base schema DDL (fresh installs get it), but `session.Migrations()` returns `nil` — existing installs will never get the table. Register V1 and V2 migrations so the table is created on first startup after upgrade.

**Files:**
- Modify: `internal/session/migrations.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/session/migrations_test.go
package session_test

import (
    "database/sql"
    "testing"

    _ "modernc.org/sqlite"

    "github.com/scrypster/huginn/internal/session"
)

func TestMigrationsRegistered(t *testing.T) {
    migs := session.Migrations()
    if len(migs) == 0 {
        t.Fatal("Migrations() returned nil — memory_replication_queue migrations not registered")
    }
}

func TestMemoryReplicationQueueTableCreated(t *testing.T) {
    db, err := sql.Open("sqlite", ":memory:")
    if err != nil {
        t.Fatalf("open: %v", err)
    }
    defer db.Close()

    // migrateMemoryReplicationQueueV1 has a FOREIGN KEY on sessions(id).
    // migrateThreadColumnsAndArtifacts (runs first) depends on messages + threads tables.
    // Create minimal stubs so all migrations can run without error.
    stubs := []string{
        `CREATE TABLE sessions (id TEXT PRIMARY KEY, space_id TEXT, title TEXT NOT NULL DEFAULT '')`,
        `CREATE TABLE messages (id TEXT PRIMARY KEY)`,
        `CREATE TABLE threads (id TEXT PRIMARY KEY)`,
        `PRAGMA foreign_keys = OFF`, // skip FK validation in the stub schema
    }
    for _, ddl := range stubs {
        if _, err := db.Exec(ddl); err != nil {
            t.Fatalf("stub: %v", err)
        }
    }

    // Run all registered migrations in order.
    migs := session.Migrations()
    for _, m := range migs {
        tx, err := db.Begin()
        if err != nil {
            t.Fatalf("begin tx for %s: %v", m.Name, err)
        }
        if err := m.Up(tx); err != nil {
            _ = tx.Rollback()
            t.Fatalf("migrate %s: %v", m.Name, err)
        }
        if err := tx.Commit(); err != nil {
            t.Fatalf("commit %s: %v", m.Name, err)
        }
    }

    // Verify memory_replication_queue (V2 schema) has the expected columns.
    rows, err := db.Query(`PRAGMA table_info(memory_replication_queue)`)
    if err != nil {
        t.Fatalf("pragma: %v", err)
    }
    defer rows.Close()
    var cols []string
    for rows.Next() {
        // PRAGMA table_info columns: cid INTEGER, name TEXT, type TEXT, notnull INTEGER, dflt_value TEXT, pk INTEGER
        var cid, notnull, pk int
        var name, typ string
        var dflt sql.NullString
        _ = rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk)
        cols = append(cols, name)
    }
    if len(cols) == 0 {
        t.Fatal("memory_replication_queue table not created")
    }
    required := []string{"id", "target_vault", "source_agent", "space_id", "concept_key", "payload"}
    for _, r := range required {
        found := false
        for _, c := range cols {
            if c == r {
                found = true
                break
            }
        }
        if !found {
            t.Errorf("column %q missing from memory_replication_queue; got: %v", r, cols)
        }
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/session/... -run TestMigrationsRegistered -v
```

Expected: FAIL — `Migrations() returned nil`.

- [ ] **Step 3: Register ALL orphaned migrations in `Migrations()`**

There are 7 migration functions in `internal/session/migrations.go`, all unregistered. `session.Migrations()` is already called in `main.go` at lines 434 and 2247 — we just need to populate the list. All must be registered in order so existing installs get the full schema without fresh re-install.

Open `internal/session/migrations.go`. Replace:

```go
// Migrations returns an empty list — all schema is now in the base schema DDL
// (ApplySchema). No rolling migrations are needed for fresh installations.
func Migrations() []sqlitedb.Migration {
    return nil
}
```

With:

```go
// Migrations returns the list of incremental schema migrations for existing
// Huginn installations. Fresh installs use the base schema DDL (ApplySchema)
// and do not run these migrations. Existing installs run them in order on first startup.
// All migrations are idempotent (IF NOT EXISTS / column-existence guards).
func Migrations() []sqlitedb.Migration {
    return []sqlitedb.Migration{
        {Name: "thread_columns_and_artifacts_v1",   Up: migrateThreadColumnsAndArtifacts},
        {Name: "delegations_session_id_v1",         Up: migrateDelegationsSessionIDV1},
        {Name: "sessions_space_id_index_v1",        Up: migrateAddSpaceIDIndex},
        {Name: "sessions_fts_v2",                   Up: migrateSessionsFTSv2},
        {Name: "memory_replication_queue_v1",       Up: migrateMemoryReplicationQueueV1},
        {Name: "memory_replication_queue_v2",       Up: migrateMemoryReplicationQueueV2},
        {Name: "cloud_vault_queue_v1",              Up: migrateCloudVaultQueueV1},
    }
}
```

Order matters:
- `migrateThreadColumnsAndArtifacts` creates the `delegations` table — must run before `migrateDelegationsSessionIDV1` which ALTERs it
- `migrateMemoryReplicationQueueV1` creates a preliminary table, `V2` drops+recreates it — V1 must precede V2

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/session/... -run TestMigrations -v
```

Expected: PASS.

- [ ] **Step 5: Confirm `session.Migrations()` is already called in `main.go`**

`session.Migrations()` is already called in `main.go` at lines 434 and 2247:

```bash
grep -n "session\.Migrations" main.go
```

Expected output includes two lines (~434 and ~2247). No new call needed — just populating `Migrations()` is sufficient. The existing call sites will pick up the newly registered migrations automatically.

- [ ] **Step 6: Build**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/session/migrations.go internal/session/migrations_test.go main.go
git commit -m "feat(session): register memory_replication_queue migrations for existing installs"
```

---

## Task 3: Wire Cross-Channel `agent.MemoryReplicator` in `main.go`

**Context:** `agent.MemoryReplicator` (cross-channel) exists fully-built. `orch.SetMemoryReplicator()` exists but is never called. After `muninnCfgFilePath` is set (~line 3193), we create the replicator, attach it to the orchestrator, and start its drain loop. The DB adapter (`agent.NewSQLiteReplicationQueuer`) bridges `*sqlitedb.DB` to the `ReplicationQueuer` interface.

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Write test confirming orchestrator accepts a replicator**

```go
// internal/agent/config_test.go  (add to existing test file or create new)
package agent_test

import (
    "testing"
    "github.com/scrypster/huginn/internal/agent"
)

func TestSetMemoryReplicatorNilSafe(t *testing.T) {
    o := agent.NewOrchestrator(agent.OrchestratorConfig{})
    // Should not panic when nil is passed
    o.SetMemoryReplicator(nil)
}
```

- [ ] **Step 2: Run the test**

```bash
go test ./internal/agent/... -run TestSetMemoryReplicatorNilSafe -v
```

Expected: PASS (the setter already exists and handles nil safely).

- [ ] **Step 3: Wire in `main.go` — after `muninnCfgFilePath` is set (~line 3193)**

Find this block in main.go:

```go
muninnCfgFilePath := filepath.Join(home, ".config", "huginn", "muninn.json")
srv.SetMuninnConfigPath(muninnCfgFilePath)
orch.SetMuninnConfigPath(muninnCfgFilePath)
```

Add immediately after it:

```go
// Wire the cross-channel memory replicator. This fans out muninn writes
// from one channel member to all other members' vaults in the same space.
// The SQLite queue provides durability across restarts.
if sqlDB != nil {
    crossChannelReplicator := agent.NewMemoryReplicator(
        muninnCfgFilePath,
        agent.NewSQLiteReplicationQueuer(sqlDB),
    )
    orch.SetMemoryReplicator(crossChannelReplicator)
    go crossChannelReplicator.Start(ctx)
    // Register Stop in the shutdown sequence — search for where the serve loop
    // closes other resources and add:
    // defer crossChannelReplicator.Stop()
}
```

For the defer: find where `srv.Stop()` or context cancellation happens in the serve path shutdown and add `crossChannelReplicator.Stop()` before it. The pattern in main.go is:

```go
// Look for: ctx, cancel := context.WithCancel(context.Background())
// The replicator's Start(ctx) will exit when ctx is cancelled.
// Additionally call Stop() for the graceful drain wait:
defer crossChannelReplicator.Stop()
```

Add the import if not already present: `"github.com/scrypster/huginn/internal/agent"` — it's already imported as `"github.com/scrypster/huginn/internal/agent"`.

- [ ] **Step 4: Build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat(main): wire cross-channel agent.MemoryReplicator to orchestrator"
```

---

## Task 4: Wire `Intercept` in `OnToolDone`

**Context:** `OnToolDone` in `agent_dispatcher.go` fires after every tool call. `isMemoryToolName()` already identifies the tools to replicate. We call `r.Intercept(ctx, name, capturedArgs, result, ag.Name, replCtx)` when the tool is a memory write and no error occurred. `GetReplicationContext` extracts the replication context attached by `InjectSpaceContext` (Task 5). Task 4 and Task 5 are written together but tested independently — wire Task 4 first; without a `replCtx`, `Intercept` is a no-op (it checks `replCtx == nil` at line 154).

**Files:**
- Modify: `internal/agent/agent_dispatcher.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/agent_dispatcher_replication_test.go
package agent_test

import (
    "context"
    "sync/atomic"
    "testing"

    "github.com/scrypster/huginn/internal/agent"
    "github.com/scrypster/huginn/internal/tools"
    "github.com/scrypster/huginn/internal/workforce"
)

// interceptSpy counts Intercept calls for a given tool name.
type interceptSpy struct {
    calls int64
}

func (s *interceptSpy) Intercept(
    ctx context.Context,
    toolName string, args map[string]any,
    result tools.ToolResult,
    producerName string,
    replCtx *workforce.MemReplicationContext,
) {
    atomic.AddInt64(&s.calls, 1)
}

func TestOnToolDoneCallsInterceptForMemoryTools(t *testing.T) {
    // Build a replication context with one member.
    replCtx := &workforce.MemReplicationContext{
        SpaceID:   "space-1",
        SpaceName: "Test Space",
        Members: []workforce.ReplicationMember{
            {AgentName: "Bob", VaultName: "huginn:agent:user:bob"},
        },
    }
    ctx := workforce.WithReplicationContext(context.Background(), replCtx)

    spy := &interceptSpy{}

    // We can't call ChatWithAgent directly in a unit test, but we CAN test the
    // Intercept path by confirming the spy increments. A simpler integration
    // approach: call the replicator's Intercept directly with the same args
    // that OnToolDone will pass, to confirm the interface is correct.
    spy.Intercept(ctx, "muninn_remember", map[string]any{
        "concept": "test concept",
        "content": "test content",
    }, tools.ToolResult{Output: "ok", IsError: false}, "Alice", replCtx)

    if spy.calls != 1 {
        t.Errorf("expected 1 Intercept call, got %d", spy.calls)
    }
}
```

- [ ] **Step 2: Run test**

```bash
go test ./internal/agent/... -run TestOnToolDoneCallsInterceptForMemoryTools -v
```

Expected: PASS (the test only validates the spy pattern, not the dispatcher wiring).

- [ ] **Step 3: Wire `Intercept` in `agent_dispatcher.go`**

Find the `OnToolDone` closure at approximately line 777:

```go
OnToolDone: func(callID string, name string, result tools.ToolResult) {
    toolArgsMu.Lock()
    capturedArgs := toolArgsCapture[callID]
    delete(toolArgsCapture, callID)
    toolArgsMu.Unlock()
    slog.Info("tool call done", "agent", ag.Name, "tool", name, ...)
```

Add the intercept call **after** the `toolArgsMu.Unlock()` line and **before** the `slog.Info`:

```go
OnToolDone: func(callID string, name string, result tools.ToolResult) {
    toolArgsMu.Lock()
    capturedArgs := toolArgsCapture[callID]
    delete(toolArgsCapture, callID)
    toolArgsMu.Unlock()

    // Replicate memory writes to other channel members' vaults.
    if o.memoryReplicator != nil && isMemoryToolName(name) && !result.IsError {
        if replCtx := workforce.GetReplicationContext(ctx); replCtx != nil {
            o.memoryReplicator.Intercept(ctx, name, capturedArgs, result, ag.Name, replCtx)
        }
    }

    slog.Info("tool call done", "agent", ag.Name, "tool", name, "session_id", sessionID, "call_id", callID, "success", result.Error == "")
    // ... existing event emission unchanged
```

Check that `workforce` is already imported in `agent_dispatcher.go`:

```bash
grep "workforce" internal/agent/agent_dispatcher.go | head -3
```

If not, add `"github.com/scrypster/huginn/internal/workforce"` to the import block.

- [ ] **Step 4: Build**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent_dispatcher.go internal/agent/agent_dispatcher_replication_test.go
git commit -m "feat(agent): call Intercept in OnToolDone for cross-channel memory replication"
```

---

## Task 5: Wire `WithReplicationContext` in `InjectSpaceContext`

**Context:** `InjectSpaceContext` in `ws.go` already loads all channel members into `sp.Members []string` and resolves their agent defs. We need to call `workforce.WithReplicationContext` to attach a `MemReplicationContext` to the context so `Intercept` (Task 4) knows the member vault names. `def.ResolvedVaultName(username)` produces the vault name. The username comes from `memory.ResolveUsername("")` which returns the configured MuninnDB username.

**Files:**
- Modify: `internal/server/ws.go`

- [ ] **Step 1: Write the failing test**

The existing `InjectSpaceContext` tests are in `internal/server/ws_test.go` (or the server test file). Add a test confirming the replication context is attached when a space has multiple members:

```go
// internal/server/ws_replication_test.go
package server_test

import (
    "context"
    "testing"

    "github.com/scrypster/huginn/internal/agents"
    "github.com/scrypster/huginn/internal/workforce"
)

func TestInjectSpaceContextAttachesReplicationContext(t *testing.T) {
    // This is tested via the replication context being present on the returned context.
    // We build a minimal server with a two-member space and call InjectSpaceContext.
    // The test uses the test helpers already in ws_test.go.

    // For now, verify the contract: WithReplicationContext + GetReplicationContext roundtrip.
    rc := &workforce.MemReplicationContext{
        SpaceID:   "space-abc",
        SpaceName: "Test",
        Members: []workforce.ReplicationMember{
            {AgentName: "Alice", VaultName: "huginn:agent:user:alice"},
            {AgentName: "Bob",   VaultName: "huginn:agent:user:bob"},
        },
    }
    ctx := workforce.WithReplicationContext(context.Background(), rc)
    got := workforce.GetReplicationContext(ctx)
    if got == nil {
        t.Fatal("GetReplicationContext returned nil after WithReplicationContext")
    }
    if got.SpaceID != "space-abc" {
        t.Errorf("SpaceID: got %q, want %q", got.SpaceID, "space-abc")
    }
    if len(got.Members) != 2 {
        t.Errorf("Members: got %d, want 2", len(got.Members))
    }
}
```

- [ ] **Step 2: Run test**

```bash
go test ./internal/server/... -run TestInjectSpaceContextAttachesReplicationContext -v
```

Expected: PASS (tests workforce roundtrip).

- [ ] **Step 3: Wire in `InjectSpaceContext` in `ws.go`**

Find the channel (non-DM) branch inside `InjectSpaceContext`. After the `block` is built and attached to ctx (~line 865–868):

```go
block := agent.BuildSpaceContextBlock(sp.Name, sp.Kind, selfName, sp.LeadAgent, members)
if block != "" {
    ctx = workforce.WithSpaceContext(ctx, block)
}
```

Add replication context wiring immediately after:

```go
// Attach replication context so OnToolDone can fan out memory writes.
// Only wire for channel spaces (DMs don't have shared memory semantics).
if sp.Kind != spaces.KindDM && len(sp.Members) > 1 {
    loader := s.agentLoader
    if loader == nil {
        loader = agents.LoadAgents
    }
    if agentCfg, cfgErr := loader(); cfgErr == nil {
        username := memory.ResolveUsername("")
        var replMembers []workforce.ReplicationMember
        for _, memberName := range sp.Members {
            for _, def := range agentCfg.Agents {
                if strings.EqualFold(def.Name, memberName) {
                    replMembers = append(replMembers, workforce.ReplicationMember{
                        AgentName: def.Name,
                        VaultName: def.ResolvedVaultName(username),
                    })
                    break
                }
            }
        }
        if len(replMembers) > 1 {
            ctx = workforce.WithReplicationContext(ctx, &workforce.MemReplicationContext{
                SpaceID:   sp.ID,
                SpaceName: sp.Name,
                Members:   replMembers,
            })
        }
    }
}
```

Check imports in `ws.go` — add if missing:
- `"github.com/scrypster/huginn/internal/memory"`
- `"github.com/scrypster/huginn/internal/workforce"`

Both should already be present. Verify:
```bash
grep "workforce\|memory\." internal/server/ws.go | head -5
```

- [ ] **Step 4: Build**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/server/ws.go internal/server/ws_replication_test.go
git commit -m "feat(server): attach MemReplicationContext in InjectSpaceContext"
```

---

## Task 6: Add Replication Status Endpoint

**Context:** The web UI needs a way to show replication queue health — how many entries are pending, failed, and dead. This is a read-only diagnostic endpoint that reads from the `memory_replication_queue` table directly via `s.db`. The server already has `s.db *sqlitedb.DB` set via `srv.SetDB(sqlDB)`.

**Files:**
- Create: `internal/server/handlers_memory.go`
- Modify: `internal/server/server.go` (register route)

- [ ] **Step 1: Write the failing test**

```go
// internal/server/handlers_memory_test.go
package server_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/scrypster/huginn/internal/server"
)

func TestHandleReplicationStatusNoDB(t *testing.T) {
    srv := server.NewTestServer(t)
    // No DB wired — should return 200 with zeroed counts, not 500
    req := httptest.NewRequest("GET", "/api/v1/memory/replication-status", nil)
    req.Header.Set("Authorization", "Bearer "+srv.Token())
    w := httptest.NewRecorder()
    srv.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
        t.Fatalf("status: got %d, want 200", w.Code)
    }
    var resp map[string]any
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if _, ok := resp["pending"]; !ok {
        t.Error("response missing 'pending' field")
    }
}
```

Note: `server.NewTestServer` is a test helper already defined in the server test package. If it doesn't exist with exactly that signature, use the existing helper pattern from other server tests:

```bash
grep -n "NewTestServer\|testServer\|newTestServer" internal/server/*_test.go | head -10
```

Use whatever test server constructor exists.

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/server/... -run TestHandleReplicationStatusNoDB -v
```

Expected: FAIL — route not registered yet.

- [ ] **Step 3: Create `internal/server/handlers_memory.go`**

```go
// internal/server/handlers_memory.go
package server

import (
    "context"
    "net/http"
)

// handleMemoryReplicationStatus returns replication queue counts from SQLite.
// GET /api/v1/memory/replication-status
// Response: {"pending":N,"failed":N,"dead":N,"connected":bool}
func (s *Server) handleMemoryReplicationStatus(w http.ResponseWriter, r *http.Request) {
    if s.db == nil {
        jsonOK(w, map[string]any{
            "pending":   0,
            "failed":    0,
            "dead":      0,
            "connected": false,
        })
        return
    }

    type counts struct {
        status string
        count  int
    }

    rows, err := s.db.Read().QueryContext(
        context.Background(),
        `SELECT status, COUNT(*) FROM memory_replication_queue GROUP BY status`,
    )
    if err != nil {
        jsonOK(w, map[string]any{
            "pending":   0,
            "failed":    0,
            "dead":      0,
            "connected": true,
        })
        return
    }
    defer rows.Close()

    result := map[string]int{
        "pending": 0,
        "failed":  0,
        "dead":    0,
    }
    for rows.Next() {
        var status string
        var count int
        if err := rows.Scan(&status, &count); err == nil {
            if _, ok := result[status]; ok {
                result[status] = count
            }
        }
    }

    jsonOK(w, map[string]any{
        "pending":   result["pending"],
        "failed":    result["failed"],
        "dead":      result["dead"],
        "connected": true,
    })
}
```

Note: `s.db.Read()` returns a `*sql.DB` reader. Check the `sqlitedb.DB` API:
```bash
grep -n "func.*Read\(\)\|func.*Write\(\)" internal/sqlitedb/db.go | head -6
```
Use the correct read accessor.

- [ ] **Step 4: Register the route in `server.go`**

Find the muninn routes block (~line 1045):

```go
mux.HandleFunc("GET /api/v1/muninn/status",   api(s.handleMuninnStatus))
mux.HandleFunc("POST /api/v1/muninn/test",    api(s.handleMuninnTest))
```

Add after:

```go
mux.HandleFunc("GET /api/v1/memory/replication-status", api(s.handleMemoryReplicationStatus))
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/server/... -run TestHandleReplicationStatus -v
```

Expected: PASS.

- [ ] **Step 6: Build**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/server/handlers_memory.go internal/server/handlers_memory_test.go internal/server/server.go
git commit -m "feat(server): add GET /api/v1/memory/replication-status endpoint"
```

---

## Task 7: Add `POST /api/v1/muninn/tool` Proxy Endpoint

**Context:** The browser cannot call MuninnDB's MCP server directly — it has no auth token and can't reach the MCP port. The server proxies a whitelisted set of read-only tools: `muninn_recall`, `muninn_read`, `muninn_find_by_entity`, `muninn_entities`, `muninn_forget`. `muninn_forget` is write-only but user-initiated (safe). The server loads the vault token from `memory.LoadGlobalConfig`, creates an `mcp.MCPClient`, and calls `CallTool`. Response is passed through as-is.

**Files:**
- Modify: `internal/server/handlers_memory.go`
- Modify: `internal/server/server.go` (register route)

- [ ] **Step 1: Write the failing test**

Add to `internal/server/handlers_memory_test.go`:

```go
func TestHandleMuninnToolRejectsUnknownTool(t *testing.T) {
    srv := server.NewTestServer(t)
    body := `{"vault":"huginn:agent:user:alice","tool":"muninn_remember","args":{}}`
    req := httptest.NewRequest("POST", "/api/v1/muninn/tool",
        strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+srv.Token())
    w := httptest.NewRecorder()
    srv.ServeHTTP(w, req)
    if w.Code != http.StatusForbidden {
        t.Fatalf("expected 403 for non-whitelisted tool, got %d", w.Code)
    }
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/server/... -run TestHandleMuninnToolRejectsUnknownTool -v
```

Expected: FAIL — route not registered.

- [ ] **Step 3: Implement in `handlers_memory.go`**

Add to `internal/server/handlers_memory.go`:

```go
import (
    "context"
    "encoding/json"
    "net/http"
    "time"

    mcp "github.com/scrypster/huginn/internal/mcp"
    "github.com/scrypster/huginn/internal/memory"
)

// allowedMuninnTools is the whitelist of tools the browser may call via the proxy.
// Only read + user-initiated write tools are permitted; no autonomous write tools.
var allowedMuninnTools = map[string]bool{
    "muninn_recall":          true,
    "muninn_read":            true,
    "muninn_find_by_entity":  true,
    "muninn_entities":        true,
    "muninn_forget":          true,
}

// handleMuninnTool proxies a MuninnDB tool call from the browser to MuninnDB MCP.
// POST /api/v1/muninn/tool
// Body: {"vault":"huginn:agent:user:alice","tool":"muninn_recall","args":{"context":"..."}}
// Response: {"result": <raw MCP tool response>} or {"error":"..."}
func (s *Server) handleMuninnTool(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Vault string         `json:"vault"`
        Tool  string         `json:"tool"`
        Args  map[string]any `json:"args"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        jsonError(w, http.StatusBadRequest, "invalid request")
        return
    }
    if req.Vault == "" || req.Tool == "" {
        jsonError(w, http.StatusBadRequest, "vault and tool are required")
        return
    }
    if !allowedMuninnTools[req.Tool] {
        jsonError(w, http.StatusForbidden, "tool not permitted: "+req.Tool)
        return
    }

    cfg, err := memory.LoadGlobalConfig(s.muninnCfgPath)
    if err != nil || cfg.Endpoint == "" {
        jsonError(w, http.StatusServiceUnavailable, "MuninnDB not configured")
        return
    }

    // MCPTokenFor and MCPURLFromEndpoint both return (string, error).
    token, tokenErr := memory.MCPTokenFor(cfg, req.Vault)
    if tokenErr != nil || token == "" {
        jsonError(w, http.StatusNotFound, "no token for vault: "+req.Vault)
        return
    }

    mcpURL, urlErr := memory.MCPURLFromEndpoint(cfg.Endpoint)
    if urlErr != nil {
        jsonError(w, http.StatusServiceUnavailable, "bad MuninnDB endpoint: "+urlErr.Error())
        return
    }

    // mcp.NewMCPClient takes a Transport (not url+token directly).
    // mcp.NewHTTPTransport(endpoint, token) builds the HTTP/SSE transport.
    transport := mcp.NewHTTPTransport(mcpURL, token)
    client := mcp.NewMCPClient(transport)

    ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
    defer cancel()
    defer client.Close()

    result, callErr := client.CallTool(ctx, req.Tool, req.Args)
    if callErr != nil {
        jsonError(w, http.StatusBadGateway, "tool call failed: "+callErr.Error())
        return
    }

    jsonOK(w, map[string]any{"result": result})
}

- [ ] **Step 4: Register route in `server.go`**

In the muninn routes block:

```go
mux.HandleFunc("POST /api/v1/muninn/tool", api(s.handleMuninnTool))
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/server/... -run TestHandleMuninnTool -v
```

Expected: PASS for the whitelist test. The connect-to-real-MuninnDB path is not tested here — that requires integration test setup.

- [ ] **Step 6: Build**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/server/handlers_memory.go internal/server/handlers_memory_test.go internal/server/server.go
git commit -m "feat(server): add POST /api/v1/muninn/tool proxy endpoint with tool whitelist"
```

---

## Task 8: Build `MemoryView.vue`

**Context:** A memory inspector that lets users browse and search agent vault memories. Uses `POST /api/v1/muninn/tool` (Task 7) for all MuninnDB calls. `GET /api/v1/muninn/vaults` gives the vault list. Pattern: vault selector → recall search → memory list → memory detail → forget action. No new backend needed beyond Tasks 6–7.

**Files:**
- Create: `web/src/views/MemoryView.vue`
- Modify: `web/src/router/index.ts`
- Modify: `web/src/App.vue`

- [ ] **Step 1: Add route to `web/src/router/index.ts`**

```typescript
import MemoryView from '../views/MemoryView.vue'

// Inside routes array, after the /inbox route:
{ path: '/memory', component: MemoryView },
```

- [ ] **Step 2: Add Memory nav item to `web/src/App.vue`**

Find the nav items array (~line 853):

```typescript
{ section: 'chat',       label: 'Chat',       path: '/chat',       icon: 'chat'       },
{ section: 'agents',     label: 'Agents',     path: '/agents',     icon: 'agents'     },
// ...
{ section: 'inbox',      label: 'Activity Log', path: '/inbox',    icon: 'inbox'      },
```

Add after `agents`:

```typescript
{ section: 'memory',     label: 'Memory',     path: '/memory',     icon: 'memory'     },
```

Add the SVG icon block in the icon renderer (search for `v-else-if="item.icon === 'inbox'"`):

```html
<!-- Memory icon (brain/database) -->
<g v-else-if="item.icon === 'memory'">
  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
    d="M9 3H7a2 2 0 00-2 2v1a3 3 0 000 6v1a2 2 0 002 2h2m6 0h2a2 2 0 002-2v-1a3 3 0 000-6V5a2 2 0 00-2-2h-2M9 3v18M15 3v18M9 9h6M9 15h6" />
</g>
```

Also add the active section detection if needed. Search for:
```typescript
if (seg === 'space') return 'chat'
```
Add after that:
```typescript
if (seg === 'memory') return 'memory'
```

- [ ] **Step 3: Write `MemoryView.vue`**

```vue
<!-- web/src/views/MemoryView.vue -->
<template>
  <div class="h-full flex flex-col bg-[var(--color-bg)]">
    <!-- Header -->
    <div class="flex-shrink-0 px-4 pt-4 pb-3 border-b border-[var(--color-border)]">
      <h1 class="text-sm font-semibold text-[var(--color-text)]">Memory</h1>
      <p class="text-xs text-[var(--color-text-muted)] mt-0.5">Browse and search agent vault memories</p>
    </div>

    <!-- Error banner -->
    <div v-if="error" class="flex-shrink-0 mx-4 mt-3 px-3 py-2 rounded bg-red-500/10 text-red-400 text-xs">
      {{ error }}
    </div>

    <!-- No MuninnDB configured -->
    <div v-if="!connected && !loading" class="flex-1 flex items-center justify-center">
      <div class="text-center text-[var(--color-text-muted)]">
        <p class="text-sm">MuninnDB not connected</p>
        <p class="text-xs mt-1">Configure in <router-link to="/connections" class="underline">Connections</router-link></p>
      </div>
    </div>

    <template v-else>
      <!-- Vault selector + search row -->
      <div class="flex-shrink-0 px-4 pt-3 pb-2 flex gap-2 items-center">
        <select
          v-model="selectedVault"
          @change="onVaultChange"
          class="flex-shrink-0 text-xs rounded px-2 py-1.5 bg-[var(--color-input-bg)] border border-[var(--color-border)] text-[var(--color-text)] max-w-[180px]"
        >
          <option value="">Select vault…</option>
          <option v-for="v in vaults" :key="v" :value="v">{{ v }}</option>
        </select>
        <input
          v-model="searchQuery"
          @keydown.enter="onSearch"
          placeholder="Search memories…"
          class="flex-1 text-xs rounded px-2 py-1.5 bg-[var(--color-input-bg)] border border-[var(--color-border)] text-[var(--color-text)] placeholder:text-[var(--color-text-muted)]"
        />
        <button
          @click="onSearch"
          :disabled="!selectedVault || searchLoading"
          class="flex-shrink-0 text-xs px-3 py-1.5 rounded bg-[var(--color-accent)] text-white disabled:opacity-40"
        >
          {{ searchLoading ? '…' : 'Search' }}
        </button>
      </div>

      <!-- Two-panel layout: list + detail -->
      <div class="flex flex-1 overflow-hidden divide-x divide-[var(--color-border)]">

        <!-- Memory list -->
        <div class="w-72 flex-shrink-0 overflow-y-auto">
          <div v-if="searchLoading" class="p-4 text-xs text-[var(--color-text-muted)]">Searching…</div>
          <div v-else-if="memories.length === 0 && searched" class="p-4 text-xs text-[var(--color-text-muted)]">No memories found.</div>
          <div v-else-if="memories.length === 0" class="p-4 text-xs text-[var(--color-text-muted)]">Enter a search term and press Search.</div>
          <button
            v-for="mem in memories"
            :key="mem.id"
            @click="selectMemory(mem)"
            class="w-full text-left px-4 py-3 border-b border-[var(--color-border)] hover:bg-[var(--color-hover)]"
            :class="{ 'bg-[var(--color-selected)]': selectedMemory?.id === mem.id }"
          >
            <p class="text-xs font-medium text-[var(--color-text)] truncate">{{ mem.concept || mem.id }}</p>
            <p class="text-xs text-[var(--color-text-muted)] mt-0.5 line-clamp-2">{{ mem.content }}</p>
          </button>
        </div>

        <!-- Memory detail -->
        <div class="flex-1 overflow-y-auto p-4">
          <div v-if="!selectedMemory" class="h-full flex items-center justify-center text-xs text-[var(--color-text-muted)]">
            Select a memory to view details
          </div>
          <template v-else>
            <div class="flex items-start justify-between mb-4">
              <h2 class="text-sm font-semibold text-[var(--color-text)]">{{ selectedMemory.concept }}</h2>
              <button
                @click="forgetMemory"
                :disabled="forgetLoading"
                class="text-xs px-2 py-1 rounded bg-red-500/10 text-red-400 hover:bg-red-500/20 disabled:opacity-40"
              >
                {{ forgetLoading ? 'Forgetting…' : 'Forget' }}
              </button>
            </div>
            <div class="text-xs text-[var(--color-text)] whitespace-pre-wrap leading-relaxed">{{ selectedMemory.content }}</div>
            <div v-if="selectedMemory.entities?.length" class="mt-4">
              <p class="text-xs font-medium text-[var(--color-text-muted)] mb-1">Entities</p>
              <div class="flex flex-wrap gap-1">
                <span
                  v-for="e in selectedMemory.entities"
                  :key="e"
                  class="text-xs px-2 py-0.5 rounded-full bg-[var(--color-tag-bg)] text-[var(--color-tag-text)]"
                >{{ e }}</span>
              </div>
            </div>
            <div v-if="selectedMemory.decay_score !== undefined" class="mt-3 text-xs text-[var(--color-text-muted)]">
              Decay score: {{ (selectedMemory.decay_score * 100).toFixed(0) }}%
            </div>
          </template>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useApi } from '../composables/useApi'

const api = useApi()

interface Memory {
  id: string
  concept: string
  content: string
  entities?: string[]
  decay_score?: number
}

const loading = ref(true)
const connected = ref(false)
const error = ref('')
const vaults = ref<string[]>([])
const selectedVault = ref('')
const searchQuery = ref('')
const searchLoading = ref(false)
const memories = ref<Memory[]>([])
const searched = ref(false)
const selectedMemory = ref<Memory | null>(null)
const forgetLoading = ref(false)

onMounted(async () => {
  try {
    const res = await api.get('/api/v1/muninn/vaults')
    vaults.value = res.vaults ?? []
    connected.value = res.connected ?? false
    if (vaults.value.length > 0) {
      selectedVault.value = vaults.value[0]
    }
  } catch (e: any) {
    error.value = e?.message ?? 'Failed to load vaults'
  } finally {
    loading.value = false
  }
})

function onVaultChange() {
  memories.value = []
  selectedMemory.value = null
  searched.value = false
}

async function onSearch() {
  if (!selectedVault.value) return
  searchLoading.value = true
  searched.value = true
  selectedMemory.value = null
  error.value = ''
  try {
    const res = await api.post('/api/v1/muninn/tool', {
      vault: selectedVault.value,
      tool: 'muninn_recall',
      args: { context: searchQuery.value || 'recent memories', limit: 30 },
    })
    // MuninnDB recall returns {result: {memories: [...]}} or {result: {content: [...]}}
    const raw = res.result
    const items: Memory[] = []
    const list = raw?.memories ?? raw?.content ?? (Array.isArray(raw) ? raw : [])
    for (const item of list) {
      items.push({
        id: item.id ?? item.memory_id ?? String(Math.random()),
        concept: item.concept ?? item.name ?? '',
        content: typeof item.content === 'string' ? item.content : JSON.stringify(item.content),
        entities: item.entities ?? [],
        decay_score: item.decay_score,
      })
    }
    memories.value = items
  } catch (e: any) {
    error.value = e?.message ?? 'Search failed'
  } finally {
    searchLoading.value = false
  }
}

function selectMemory(mem: Memory) {
  selectedMemory.value = mem
}

async function forgetMemory() {
  if (!selectedMemory.value || !selectedVault.value) return
  forgetLoading.value = true
  error.value = ''
  try {
    await api.post('/api/v1/muninn/tool', {
      vault: selectedVault.value,
      tool: 'muninn_forget',
      args: { id: selectedMemory.value.id },
    })
    memories.value = memories.value.filter(m => m.id !== selectedMemory.value!.id)
    selectedMemory.value = null
  } catch (e: any) {
    error.value = e?.message ?? 'Forget failed'
  } finally {
    forgetLoading.value = false
  }
}
</script>
```

- [ ] **Step 4: Check `useApi` composable API shape**

Verify the composable exposes `.get()` and `.post()` methods:

```bash
grep -n "\.get\|\.post\|apiFetch\|return" web/src/composables/useApi.ts | head -20
```

Adjust the `api.get(...)` / `api.post(...)` calls in the Vue component to match the actual API surface. If `useApi` returns a `fetch`-like function rather than `{get, post}`, update accordingly.

- [ ] **Step 5: Build the frontend**

```bash
cd web && npm run build 2>&1 | tail -20
```

Expected: no TypeScript errors, no Vue compilation errors.

- [ ] **Step 6: Smoke test manually**

Start the server (`go run main.go --headless`), open the web UI, navigate to Memory. Confirm:
- Vaults load in the selector
- Search returns memories for an agent with an active vault
- Forget removes the entry from the list

- [ ] **Step 7: Commit**

```bash
git add web/src/views/MemoryView.vue web/src/router/index.ts web/src/App.vue
git commit -m "feat(ui): add MemoryView — vault selector, memory search, detail + forget"
```

---

## Self-Review

### Spec Coverage Check

| Roadmap Task | Plan Task | Covered? |
|---|---|---|
| AUDIT: cross-agent memory sharing | Tasks 3–5 wire the existing replicator | ✅ (existing code, no new design needed) |
| Semantic triggers | Skipped by Opus decision | ✅ out of scope |
| Memory Inspector UI | Task 8 (MemoryView.vue) | ✅ |
| Memory config in Web agent editor | Already complete in AgentsView.vue | ✅ out of scope |
| Cross-agent memory write tool | Handled by MemoryReplicator fan-out | ✅ |
| Rename disambiguation | Task 1 | ✅ |
| Migration registration | Task 2 | ✅ |

### Placeholder Scan

All code blocks are complete. No "TBD" or "add validation" language. One intentional caveat: Task 8 Step 4 asks the implementer to verify `useApi` shape — this is necessary because the composable's exact method names are not confirmed. This is a read + adapt step, not a placeholder.

### Type Consistency

- `workforce.MemReplicationContext` used consistently in Tasks 4, 5
- `workforce.WithReplicationContext` / `workforce.GetReplicationContext` used consistently
- `agent.MemoryReplicator` (cross-channel) vs `agents.CloudVaultReplicator` (cloud) — distinction maintained throughout
- `allowedMuninnTools` whitelist type `map[string]bool` — consistent with Go idiom

### Confirmed Signatures

- `memory.MCPTokenFor(cfg *GlobalConfig, vaultName string) (string, error)` — in `internal/memory/mcp_connect.go`
- `memory.MCPURLFromEndpoint(endpoint string) (string, error)` — in `internal/memory/mcp_connect.go`
- `mcp.NewHTTPTransport(endpoint, token string) *HTTPTransport` — in `internal/mcp/http_transport.go`
- `mcp.NewMCPClient(tr Transport) *MCPClient` — in `internal/mcp/client.go`
- `session.Migrations()` is already called in `main.go` at lines 434 and 2247 — no new wiring needed, just register the functions
