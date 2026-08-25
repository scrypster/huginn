# Claude Code Bridge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ingest every Claude Code terminal session on the machine into Huginn live, and let a Huginn agent delegate a one-shot task to Claude Code.

**Architecture:** An fsnotify watcher tails Claude Code's append-only JSONL transcripts under `~/.claude/projects` and maps them into Huginn sessions marked `external_kind = "claude-code"`. A `claude_code` tool shells out to `claude -p --session-id <uuid> --output-format stream-json` for delegation; because Huginn assigns the session ID, the same watcher ingests delegated runs, so both directions share one mapper. Nothing is installed into Claude Code's configuration.

**Tech Stack:** Go 1.25, `github.com/fsnotify/fsnotify` (new dependency), SQLite via `internal/sqlitedb`, existing `internal/session` store, existing `internal/pricing`.

**Spec:** `docs/planning/2026-08-25-claude-code-bridge-design.md`

## Deviation from the spec

The spec specifies `Manifest.Source = "claude-code"`. **Do not implement it that way.** The `sessions` table declares `source TEXT NOT NULL DEFAULT '' CHECK (source IN ('', 'routine'))`. SQLite cannot drop a CHECK constraint without a full table rebuild, and `sessions` has two inbound foreign keys (`huginn-sqlite-schema.sql:223` and `:554`) while `PRAGMA foreign_keys = ON` (`internal/sqlitedb/db.go:23`) and `runMigration` wraps every migration in a transaction where that pragma cannot be toggled.

This plan adds two new unconstrained columns instead — `external_id` and `external_kind` — and leaves `source` alone. Task 14 amends the spec to match.

## Global Constraints

- Go 1.25.0 (`go.mod`). Do not raise the floor.
- All new code lives under `internal/claudecode/` except where a task names another path.
- `PRAGMA foreign_keys = ON` is set globally; migrations run inside a transaction and MUST NOT toggle pragmas.
- Every migration MUST be idempotent — reuse `isColumnAlreadyExistsError` from `internal/session/migrations.go:193`.
- Tests MUST NOT touch the network, spawn a real `claude`, or read the developer's real `~/.claude`. Use `testdata/` fixtures and `t.TempDir()`.
- Run tests with `go test ./internal/claudecode/ -run <Name> -v`. Full suite: `make test`.
- The feature defaults to **off** (`claude_code.enabled = false`). When off, no watcher starts and the tool is not registered.
- Never pass `--dangerously-skip-permissions` unless config explicitly sets it.
- Redact absolute home paths from any fixture committed to `testdata/`.

---

### Task 1: Schema and manifest support for external sessions

**Files:**
- Modify: `internal/sqlitedb/schema/huginn-sqlite-schema.sql:122-152` (sessions table)
- Modify: `internal/sqlitedb/schema/huginn-sqlite-schema.sql` (append `claude_ingest` table)
- Modify: `internal/session/types.go:9-31` (Manifest)
- Modify: `internal/session/migrations.go:15-17` (Migrations)
- Modify: `internal/session/sqlite_store.go:86-105` (SaveManifest)
- Test: `internal/session/external_id_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Manifest.ExternalID string`, `Manifest.ExternalKind string`; `(*SQLiteSessionStore).LoadByExternalID(kind, externalID string) (*Session, error)` returning `(nil, nil)` when absent; SQL table `claude_ingest`.

- [ ] **Step 1: Write the failing test**

Create `internal/session/external_id_test.go`:

```go
package session

import "testing"

func TestSaveManifestRoundTripsExternalFields(t *testing.T) {
	s := newTestSQLiteStore(t)

	sess := s.New("claude session", "/tmp/proj", "claude-opus-5")
	sess.Manifest.ExternalKind = "claude-code"
	sess.Manifest.ExternalID = "11111111-2222-3333-4444-555555555555"
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	got, err := s.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Manifest.ExternalKind != "claude-code" {
		t.Errorf("ExternalKind = %q, want %q", got.Manifest.ExternalKind, "claude-code")
	}
	if got.Manifest.ExternalID != sess.Manifest.ExternalID {
		t.Errorf("ExternalID = %q, want %q", got.Manifest.ExternalID, sess.Manifest.ExternalID)
	}
}

func TestLoadByExternalID(t *testing.T) {
	s := newTestSQLiteStore(t)

	sess := s.New("claude session", "/tmp/proj", "claude-opus-5")
	sess.Manifest.ExternalKind = "claude-code"
	sess.Manifest.ExternalID = "abc-123"
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	got, err := s.LoadByExternalID("claude-code", "abc-123")
	if err != nil {
		t.Fatalf("LoadByExternalID: %v", err)
	}
	if got == nil || got.ID != sess.ID {
		t.Fatalf("LoadByExternalID returned %v, want session %s", got, sess.ID)
	}

	missing, err := s.LoadByExternalID("claude-code", "nope")
	if err != nil {
		t.Fatalf("LoadByExternalID(missing): unexpected error %v", err)
	}
	if missing != nil {
		t.Errorf("LoadByExternalID(missing) = %v, want nil", missing)
	}
}

func TestSourceCheckConstraintUntouched(t *testing.T) {
	s := newTestSQLiteStore(t)
	sess := s.New("routine session", "/tmp/proj", "m")
	sess.Manifest.Source = "routine"
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("routine source must still be accepted: %v", err)
	}
}
```

If `newTestSQLiteStore` does not already exist in the package's test files, add it to this file:

```go
func newTestSQLiteStore(t *testing.T) *SQLiteSessionStore {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if err := db.Migrate(Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewSQLiteSessionStore(db)
}
```

Check the existing test files first — `grep -rn "func newTestSQLiteStore\|func testStore" internal/session/*_test.go` — and reuse the existing helper rather than duplicating it. Match its actual constructor name.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run 'TestSaveManifestRoundTripsExternalFields|TestLoadByExternalID' -v`
Expected: FAIL — `sess.Manifest.ExternalKind undefined` and `s.LoadByExternalID undefined`.

- [ ] **Step 3: Add the Manifest fields**

In `internal/session/types.go`, inside `type Manifest struct`, after the Space fields:

```go
	// External bridge fields. Empty strings mean a native Huginn session.
	// ExternalKind identifies the foreign system ("claude-code"); ExternalID
	// is that system's own session identifier.
	ExternalKind string `json:"external_kind,omitempty"`
	ExternalID   string `json:"external_id,omitempty"`
```

- [ ] **Step 4: Add the columns to the base schema**

In `internal/sqlitedb/schema/huginn-sqlite-schema.sql`, inside `CREATE TABLE IF NOT EXISTS sessions (...)`, after the `space_id` line (add a comma to `space_id`'s line):

```sql
    -- External bridge linkage. Empty strings mean a native Huginn session.
    external_kind   TEXT    NOT NULL DEFAULT '',   -- e.g. "claude-code"
    external_id     TEXT    NOT NULL DEFAULT ''    -- foreign system's session id
```

Immediately after the sessions table, add:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS uq_sessions_external
    ON sessions(external_kind, external_id)
    WHERE external_kind <> '';

CREATE TABLE IF NOT EXISTS claude_ingest (
    external_id       TEXT    NOT NULL PRIMARY KEY,  -- Claude Code session UUID
    huginn_session_id TEXT    NOT NULL,
    path              TEXT    NOT NULL,
    size              INTEGER NOT NULL DEFAULT 0,
    byte_offset       INTEGER NOT NULL DEFAULT 0,
    last_uuid         TEXT    NOT NULL DEFAULT '',
    updated_at        INTEGER NOT NULL DEFAULT 0
);
```

`byte_offset` rather than `offset` because `OFFSET` is a reserved word in SQLite.

- [ ] **Step 5: Add rolling migrations for existing databases**

`ApplySchema` uses `CREATE TABLE IF NOT EXISTS`, so an existing `huginn.db` will not gain the new columns. Replace the body of `Migrations()` in `internal/session/migrations.go`:

```go
func Migrations() []sqlitedb.Migration {
	return []sqlitedb.Migration{
		{Name: "sessions_external_columns_v1", Up: migrateSessionsExternalColumnsV1},
		{Name: "claude_ingest_v1", Up: migrateClaudeIngestV1},
	}
}

// migrateSessionsExternalColumnsV1 adds external_kind/external_id to the
// sessions table on databases created before the Claude Code bridge, and
// indexes the pair for reverse lookup. Uses ALTER TABLE ADD COLUMN rather
// than a table rebuild: sessions has inbound foreign keys and migrations run
// inside a transaction where PRAGMA foreign_keys cannot be toggled.
func migrateSessionsExternalColumnsV1(tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE sessions ADD COLUMN external_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN external_id TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.Exec(stmt); err != nil && !isColumnAlreadyExistsError(err) {
			return err
		}
	}
	_, err := tx.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS uq_sessions_external
			ON sessions(external_kind, external_id)
			WHERE external_kind <> ''`)
	return err
}

func migrateClaudeIngestV1(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS claude_ingest (
			external_id       TEXT    NOT NULL PRIMARY KEY,
			huginn_session_id TEXT    NOT NULL,
			path              TEXT    NOT NULL,
			size              INTEGER NOT NULL DEFAULT 0,
			byte_offset       INTEGER NOT NULL DEFAULT 0,
			last_uuid         TEXT    NOT NULL DEFAULT '',
			updated_at        INTEGER NOT NULL DEFAULT 0
		)`)
	return err
}
```

Keep the existing unused `migrate*` helpers in the file; do not delete them.

- [ ] **Step 6: Persist and read the new columns**

In `internal/session/sqlite_store.go`, update the `SaveManifest` upsert (around line 86). Add the two columns to the insert list, add two `?` placeholders, add them to the `DO UPDATE SET` clause, and pass the values:

```go
		INSERT INTO sessions
			(id, title, model, agent, created_at, updated_at, message_count,
			 last_message_id, workspace_root, workspace_name, status, version,
			 source, routine_id, run_id, space_id, external_kind, external_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, model=excluded.model, agent=excluded.agent,
			updated_at=excluded.updated_at, message_count=excluded.message_count,
			last_message_id=excluded.last_message_id,
			workspace_root=excluded.workspace_root, workspace_name=excluded.workspace_name,
			status=excluded.status, version=excluded.version,
			source=excluded.source, routine_id=excluded.routine_id, run_id=excluded.run_id,
			space_id=excluded.space_id,
			external_kind=excluded.external_kind, external_id=excluded.external_id`,
```

and append `m.ExternalKind, m.ExternalID,` to the argument list after `spaceID`.

Then find every `SELECT` in `sqlite_store.go` that scans a full manifest (`grep -n "SELECT id, title, model, agent" internal/session/sqlite_store.go`) and add `external_kind, external_id` to both the column list and the `rows.Scan` destinations, in the same position.

- [ ] **Step 7: Add LoadByExternalID**

Append to `internal/session/sqlite_store.go`:

```go
// LoadByExternalID returns the session bridged from an external system, or
// (nil, nil) if no such session exists. kind is a value like "claude-code".
func (s *SQLiteSessionStore) LoadByExternalID(kind, externalID string) (*Session, error) {
	rdb := s.db.Read()
	if rdb == nil {
		return nil, fmt.Errorf("session sqlite: no read connection")
	}
	var id string
	err := rdb.QueryRow(
		`SELECT id FROM sessions WHERE external_kind = ? AND external_id = ?`,
		kind, externalID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("session sqlite: load by external id: %w", err)
	}
	return s.Load(id)
}
```

Add `"database/sql"` and `"errors"` to the imports if not already present.

Do **not** add `LoadByExternalID` to `StoreInterface` — the filesystem-backed `*Store` does not implement it and the bridge only ever runs against SQLite.

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/session/ -v`
Expected: PASS, including the three new tests and every pre-existing session test.

- [ ] **Step 9: Commit**

```bash
git add internal/session/types.go internal/session/migrations.go \
        internal/session/sqlite_store.go internal/session/external_id_test.go \
        internal/sqlitedb/schema/huginn-sqlite-schema.sql
git commit -m "feat(session): external_kind/external_id columns for bridged sessions"
```

---

### Task 2: Transcript line types and tolerant parser

**Files:**
- Create: `internal/claudecode/transcript.go`
- Create: `internal/claudecode/transcript_test.go`
- Create: `internal/claudecode/testdata/basic.jsonl`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Line struct` with fields `Type, UUID, ParentUUID, SessionID, CWD, Timestamp, GitBranch, CustomTitle string`, `IsSidechain bool`, `Message json.RawMessage`, `ToolUseResult json.RawMessage`
  - `func ParseLine(b []byte) (Line, bool)` — returns `ok=false` for malformed JSON or a filtered type
  - `func IsContentType(t string) bool`

- [ ] **Step 1: Create the fixture**

Create `internal/claudecode/testdata/basic.jsonl`. Every path is fake; no real home directory content.

```
{"type":"custom-title","customTitle":"Fix the auth bug","sessionId":"11111111-2222-3333-4444-555555555555"}
{"type":"mode","mode":"default","sessionId":"11111111-2222-3333-4444-555555555555"}
{"type":"atis-latch","sessionId":"11111111-2222-3333-4444-555555555555"}
{"type":"user","uuid":"u1","parentUuid":null,"isSidechain":false,"sessionId":"11111111-2222-3333-4444-555555555555","cwd":"/tmp/proj","gitBranch":"main","timestamp":"2026-08-25T10:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"read main.go"}]}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","isSidechain":false,"sessionId":"11111111-2222-3333-4444-555555555555","cwd":"/tmp/proj","timestamp":"2026-08-25T10:00:02.000Z","message":{"role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"Reading it now."},{"type":"tool_use","id":"tu1","name":"Read","input":{"file_path":"/tmp/proj/main.go"}}],"usage":{"input_tokens":120,"output_tokens":45}}}
{"type":"user","uuid":"u2","parentUuid":"a1","isSidechain":false,"sessionId":"11111111-2222-3333-4444-555555555555","timestamp":"2026-08-25T10:00:03.000Z","toolUseResult":{"stdout":"package main"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"package main"}]}}
{"type":"assistant","uuid":"a2","parentUuid":"u2","isSidechain":false,"sessionId":"11111111-2222-3333-4444-555555555555","timestamp":"2026-08-25T10:00:05.000Z","message":{"role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"It is a main package."}],"usage":{"input_tokens":200,"output_tokens":12}}}
```

- [ ] **Step 2: Write the failing test**

Create `internal/claudecode/transcript_test.go`:

```go
package claudecode

import (
	"os"
	"strings"
	"testing"
)

func TestParseLineAcceptsContentTypes(t *testing.T) {
	raw := []byte(`{"type":"assistant","uuid":"a1","sessionId":"s1","message":{"role":"assistant"}}`)
	got, ok := ParseLine(raw)
	if !ok {
		t.Fatal("ParseLine returned ok=false for an assistant line")
	}
	if got.Type != "assistant" || got.UUID != "a1" || got.SessionID != "s1" {
		t.Errorf("ParseLine = %+v, want type=assistant uuid=a1 sessionId=s1", got)
	}
}

func TestParseLineRejectsMetadataTypes(t *testing.T) {
	for _, typ := range []string{
		"last-prompt", "custom-title", "agent-name", "mode",
		"permission-mode", "atis-latch", "attachment",
		"file-history-snapshot", "system", "totally-new-future-type",
	} {
		raw := []byte(`{"type":"` + typ + `","uuid":"x"}`)
		if _, ok := ParseLine(raw); ok {
			t.Errorf("ParseLine(%q) returned ok=true, want false", typ)
		}
	}
}

func TestParseLineRejectsMalformedJSON(t *testing.T) {
	if _, ok := ParseLine([]byte(`{"type":"user"`)); ok {
		t.Error("ParseLine returned ok=true for truncated JSON")
	}
	if _, ok := ParseLine([]byte(``)); ok {
		t.Error("ParseLine returned ok=true for an empty line")
	}
}

func TestParseLineFixtureYieldsFourContentLines(t *testing.T) {
	b, err := os.ReadFile("testdata/basic.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var n int
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if _, ok := ParseLine([]byte(l)); ok {
			n++
		}
	}
	if n != 4 {
		t.Errorf("accepted %d lines, want 4 (2 user + 2 assistant)", n)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -v`
Expected: FAIL — the package does not exist yet (`no Go files` or `undefined: ParseLine`).

- [ ] **Step 4: Write the implementation**

Create `internal/claudecode/transcript.go`:

```go
// Package claudecode bridges Claude Code terminal sessions into Huginn.
//
// Claude Code appends each session to a JSONL transcript under
// ~/.claude/projects/<slugified-cwd>/<session-uuid>.jsonl. This package
// parses those transcripts, maps them onto Huginn session messages, and
// tails them live.
package claudecode

import "encoding/json"

// Line is one parsed transcript line. Only fields the bridge consumes are
// modelled; Claude Code adds fields freely and unknown ones are ignored.
type Line struct {
	Type          string          `json:"type"`
	UUID          string          `json:"uuid"`
	ParentUUID    string          `json:"parentUuid"`
	SessionID     string          `json:"sessionId"`
	CWD           string          `json:"cwd"`
	Timestamp     string          `json:"timestamp"`
	GitBranch     string          `json:"gitBranch"`
	CustomTitle   string          `json:"customTitle"`
	IsSidechain   bool            `json:"isSidechain"`
	Message       json.RawMessage `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

// contentTypes is an allowlist. Claude Code interleaves many metadata line
// types with conversation content, and new ones appear across versions.
// Allowlisting means an unrecognised future type is dropped rather than
// leaking into the timeline as a junk message.
var contentTypes = map[string]bool{
	"user":      true,
	"assistant": true,
}

// IsContentType reports whether a transcript line type carries conversation
// content the bridge should ingest.
func IsContentType(t string) bool { return contentTypes[t] }

// ParseLine parses one transcript line. ok is false when the line is not
// valid JSON or is not a content-bearing type; callers skip those.
// A parse failure is never fatal to the rest of the file.
func ParseLine(b []byte) (Line, bool) {
	var l Line
	if len(b) == 0 {
		return Line{}, false
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return Line{}, false
	}
	if !IsContentType(l.Type) {
		return Line{}, false
	}
	return l, true
}

// ParseTitle extracts a custom title from a line if it carries one. It is
// separate from ParseLine because "custom-title" is a metadata type that
// ParseLine deliberately rejects, but whose payload the ingester still wants.
func ParseTitle(b []byte) (string, bool) {
	var l Line
	if err := json.Unmarshal(b, &l); err != nil {
		return "", false
	}
	if l.Type != "custom-title" || l.CustomTitle == "" {
		return "", false
	}
	return l.CustomTitle, true
}
```

Note `ParentUUID` is `string` but the fixture has `"parentUuid":null`. `encoding/json` unmarshals a JSON `null` into a `string` as the zero value without error, so this is correct as written — the `TestParseLineFixtureYieldsFourContentLines` test covers it.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -v`
Expected: PASS — all four tests.

- [ ] **Step 6: Commit**

```bash
git add internal/claudecode/
git commit -m "feat(claudecode): transcript line types and allowlist parser"
```

---

### Task 3: Mapper — transcript lines to session messages

**Files:**
- Create: `internal/claudecode/mapper.go`
- Create: `internal/claudecode/mapper_test.go`

**Interfaces:**
- Consumes: `Line`, `ParseLine` (Task 2); `session.SessionMessage`, `session.PersistedToolCall` (Task 1).
- Produces:
  - `type Mapped struct { Msg session.SessionMessage; ThreadID string }` — `ThreadID` non-empty means the message belongs on a thread, not the main timeline
  - `type Mapper struct` with `func NewMapper() *Mapper`
  - `func (m *Mapper) Add(l Line) []Mapped` — may return zero, one, or (when completing a prior tool call) an amended message
  - `func (m *Mapper) Model() string`

- [ ] **Step 1: Write the failing test**

Create `internal/claudecode/mapper_test.go`:

```go
package claudecode

import (
	"os"
	"strings"
	"testing"
)

func mapFixture(t *testing.T, path string) []Mapped {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	m := NewMapper()
	var out []Mapped
	for _, raw := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		l, ok := ParseLine([]byte(raw))
		if !ok {
			continue
		}
		out = append(out, m.Add(l)...)
	}
	return out
}

func TestMapperProducesThreeMessages(t *testing.T) {
	got := mapFixture(t, "testdata/basic.jsonl")
	// u1 (user), a1 (assistant + tool_use), a2 (assistant).
	// u2 is tool_result-only and must NOT become a message.
	if len(got) != 3 {
		for i, g := range got {
			t.Logf("[%d] role=%q content=%q tools=%d", i, g.Msg.Role, g.Msg.Content, len(g.Msg.ToolCalls))
		}
		t.Fatalf("got %d messages, want 3", len(got))
	}
	if got[0].Msg.Role != "user" || got[0].Msg.Content != "read main.go" {
		t.Errorf("msg[0] = %q/%q, want user/read main.go", got[0].Msg.Role, got[0].Msg.Content)
	}
	if got[1].Msg.Role != "assistant" || got[1].Msg.Content != "Reading it now." {
		t.Errorf("msg[1] = %q/%q", got[1].Msg.Role, got[1].Msg.Content)
	}
	if got[2].Msg.Content != "It is a main package." {
		t.Errorf("msg[2] content = %q", got[2].Msg.Content)
	}
}

func TestMapperAttachesToolCallAndResult(t *testing.T) {
	got := mapFixture(t, "testdata/basic.jsonl")
	if len(got) < 2 {
		t.Fatal("not enough messages")
	}
	calls := got[1].Msg.ToolCalls
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(calls))
	}
	if calls[0].ID != "tu1" || calls[0].Name != "Read" {
		t.Errorf("tool call = %+v, want id=tu1 name=Read", calls[0])
	}
	if calls[0].Args["file_path"] != "/tmp/proj/main.go" {
		t.Errorf("tool args = %v", calls[0].Args)
	}
	if calls[0].Result != "package main" {
		t.Errorf("tool result = %q, want %q", calls[0].Result, "package main")
	}
}

func TestMapperRecordsUsageAndModel(t *testing.T) {
	got := mapFixture(t, "testdata/basic.jsonl")
	if got[1].Msg.PromptTok != 120 || got[1].Msg.CompTok != 45 {
		t.Errorf("usage = %d/%d, want 120/45", got[1].Msg.PromptTok, got[1].Msg.CompTok)
	}
	if got[1].Msg.ModelName != "claude-opus-5" {
		t.Errorf("model = %q", got[1].Msg.ModelName)
	}
}

func TestMapperRoutesSidechainToThread(t *testing.T) {
	m := NewMapper()
	parent, _ := ParseLine([]byte(`{"type":"assistant","uuid":"p1","sessionId":"s","message":{"role":"assistant","content":[{"type":"text","text":"delegating"}]}}`))
	m.Add(parent)

	side, _ := ParseLine([]byte(`{"type":"assistant","uuid":"s1","parentUuid":"p1","isSidechain":true,"sessionId":"s","message":{"role":"assistant","content":[{"type":"text","text":"subagent says hi"}]}}`))
	got := m.Add(side)

	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	if got[0].ThreadID != "p1" {
		t.Errorf("ThreadID = %q, want %q", got[0].ThreadID, "p1")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestMapper -v`
Expected: FAIL — `undefined: NewMapper`, `undefined: Mapped`.

- [ ] **Step 3: Write the implementation**

Create `internal/claudecode/mapper.go`:

```go
package claudecode

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// Mapped is one Huginn message derived from a transcript line. A non-empty
// ThreadID means the message belongs on that thread rather than on the main
// timeline; the thread id is the parent message's transcript uuid.
type Mapped struct {
	Msg      session.SessionMessage
	ThreadID string
}

// contentBlock is one element of a Claude Code message.content array.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     map[string]any  `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type innerMessage struct {
	Role    string         `json:"role"`
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Mapper converts a stream of transcript lines into Huginn messages.
//
// It is stateful by necessity: an assistant line declares tool_use blocks
// whose results only arrive on the *following* user line, keyed by
// tool_use_id. The mapper keeps a pointer to each emitted message so a later
// tool_result can be written back into it.
type Mapper struct {
	// openCalls maps tool_use_id to the emitted message index and the index
	// of the tool call within that message.
	openCalls map[string]callRef
	emitted   []*session.SessionMessage
	model     string
}

type callRef struct {
	msgIdx  int
	callIdx int
}

// NewMapper returns a Mapper with empty state.
func NewMapper() *Mapper {
	return &Mapper{openCalls: map[string]callRef{}}
}

// Model returns the most recent assistant model seen, or "" if none.
func (m *Mapper) Model() string { return m.model }

// Add feeds one transcript line to the mapper.
//
// It returns the messages that became newly available. A user line carrying
// only tool_result blocks completes earlier tool calls and returns nothing —
// the amended message was already returned when its assistant line arrived,
// and the caller is expected to persist the update.
func (m *Mapper) Add(l Line) []Mapped {
	var inner innerMessage
	if len(l.Message) > 0 {
		if err := json.Unmarshal(l.Message, &inner); err != nil {
			return nil
		}
	}
	if inner.Model != "" {
		m.model = inner.Model
	}

	// Complete any tool calls this line reports results for, regardless of role.
	completed := m.applyToolResults(inner)

	text := collectText(inner.Content)
	calls := collectToolCalls(inner.Content)

	// A line with no text and no new tool calls carries only results.
	if text == "" && len(calls) == 0 {
		return nil
	}
	// A tool_result-only user line must not become a message even if the
	// result content happened to be collected as text.
	if l.Type == "user" && len(calls) == 0 && onlyToolResults(inner.Content) {
		_ = completed
		return nil
	}

	msg := session.SessionMessage{
		ID:        l.UUID,
		Ts:        parseTS(l.Timestamp),
		Role:      inner.Role,
		Content:   text,
		ModelName: inner.Model,
		PromptTok: inner.Usage.InputTokens,
		CompTok:   inner.Usage.OutputTokens,
		ToolCalls: calls,
	}
	if msg.Role == "" {
		msg.Role = l.Type
	}

	m.emitted = append(m.emitted, &msg)
	idx := len(m.emitted) - 1
	for i, c := range calls {
		m.openCalls[c.ID] = callRef{msgIdx: idx, callIdx: i}
	}

	out := Mapped{Msg: msg}
	if l.IsSidechain {
		out.ThreadID = l.ParentUUID
	}
	return []Mapped{out}
}

// applyToolResults writes tool_result payloads back into the messages that
// declared the corresponding tool_use. Returns the number applied.
func (m *Mapper) applyToolResults(inner innerMessage) int {
	var n int
	for _, b := range inner.Content {
		if b.Type != "tool_result" || b.ToolUseID == "" {
			continue
		}
		ref, ok := m.openCalls[b.ToolUseID]
		if !ok || ref.msgIdx >= len(m.emitted) {
			continue
		}
		msg := m.emitted[ref.msgIdx]
		if ref.callIdx >= len(msg.ToolCalls) {
			continue
		}
		msg.ToolCalls[ref.callIdx].Result = rawToString(b.Content)
		delete(m.openCalls, b.ToolUseID)
		n++
	}
	return n
}

func collectText(blocks []contentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

func collectToolCalls(blocks []contentBlock) []session.PersistedToolCall {
	var out []session.PersistedToolCall
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		out = append(out, session.PersistedToolCall{
			ID:   b.ID,
			Name: b.Name,
			Args: b.Input,
		})
	}
	return out
}

func onlyToolResults(blocks []contentBlock) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			return false
		}
	}
	return true
}

// rawToString renders a tool_result content payload, which Claude Code emits
// either as a plain JSON string or as an array of content blocks.
func rawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return collectText(blocks)
	}
	return string(raw)
}

func parseTS(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now().UTC()
	}
	return t.UTC()
}
```

Note: the fixture's `content` for the user's `tool_result` is a plain string, and `collectText` ignores `tool_result` blocks, so the tool-result-only user line produces `text == ""` and returns `nil` at the first guard. The `onlyToolResults` guard is the belt-and-braces path for transcripts where a `tool_result` block also carries text.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run TestMapper -v`
Expected: PASS — all four mapper tests.

- [ ] **Step 5: Commit**

```bash
git add internal/claudecode/mapper.go internal/claudecode/mapper_test.go
git commit -m "feat(claudecode): map transcript lines to session messages"
```

---

### Task 4: Tailer — offsets, partial lines, truncation

This is the task most likely to produce silent data loss. Do not shortcut the tests.

**Files:**
- Create: `internal/claudecode/tail.go`
- Create: `internal/claudecode/tail_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type TailState struct { Path string; Size int64; ByteOffset int64; LastUUID string }`
  - `func ReadNew(path string, st TailState) (lines [][]byte, next TailState, err error)`

- [ ] **Step 1: Write the failing test**

Create `internal/claudecode/tail_test.go`:

```go
package claudecode

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func TestReadNewSkipsPartialTrailingLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, "{\"a\":1}\n{\"b\":2}\n{\"c\":3")

	lines, st, err := ReadNew(p, TailState{Path: p})
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (the third is incomplete)", len(lines))
	}
	if st.ByteOffset != 16 {
		t.Errorf("ByteOffset = %d, want 16 (two complete lines)", st.ByteOffset)
	}

	// Completing the line makes it available without re-emitting the first two.
	appendFile(t, p, "}\n")
	lines2, _, err := ReadNew(p, st)
	if err != nil {
		t.Fatalf("ReadNew(2): %v", err)
	}
	if len(lines2) != 1 || string(lines2[0]) != `{"c":3}` {
		t.Errorf("second read = %q, want [{\"c\":3}]", lines2)
	}
}

func TestReadNewByteAtATimeMatchesSingleShot(t *testing.T) {
	content := "{\"uuid\":\"1\"}\n{\"uuid\":\"2\"}\n{\"uuid\":\"3\"}\n{\"uuid\":\"4\"}\n"

	dir := t.TempDir()
	whole := filepath.Join(dir, "whole.jsonl")
	writeFile(t, whole, content)
	want, _, err := ReadNew(whole, TailState{Path: whole})
	if err != nil {
		t.Fatalf("single shot: %v", err)
	}

	drip := filepath.Join(dir, "drip.jsonl")
	writeFile(t, drip, "")
	st := TailState{Path: drip}
	var got [][]byte
	for i := 0; i < len(content); i++ {
		appendFile(t, drip, string(content[i]))
		lines, next, err := ReadNew(drip, st)
		if err != nil {
			t.Fatalf("drip read at %d: %v", i, err)
		}
		got = append(got, lines...)
		st = next
	}

	if len(got) != len(want) {
		t.Fatalf("drip produced %d lines, single shot produced %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != string(want[i]) {
			t.Errorf("line %d: drip %q != single %q", i, got[i], want[i])
		}
	}
}

func TestReadNewResetsOnTruncation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, "{\"uuid\":\"1\"}\n{\"uuid\":\"2\"}\n")

	_, st, err := ReadNew(p, TailState{Path: p})
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	st.LastUUID = "2"

	// Same session, file rewritten from scratch with more content appended.
	writeFile(t, p, "{\"uuid\":\"1\"}\n{\"uuid\":\"2\"}\n{\"uuid\":\"3\"}\n")
	lines, _, err := ReadNew(p, st)
	if err != nil {
		t.Fatalf("ReadNew after truncation: %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"uuid":"3"}` {
		t.Fatalf("after truncation got %q, want only uuid 3", lines)
	}
}

func TestReadNewFullResetWhenLastUUIDAbsent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, "{\"uuid\":\"9\"}\n")

	// Offset is past the new (shorter) file and LastUUID is not in it:
	// this is different content at the same path.
	st := TailState{Path: p, Size: 999, ByteOffset: 500, LastUUID: "gone"}
	lines, next, err := ReadNew(p, st)
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"uuid":"9"}` {
		t.Fatalf("got %q, want the whole file replayed", lines)
	}
	if next.LastUUID != "9" {
		t.Errorf("LastUUID = %q, want 9", next.LastUUID)
	}
}

func TestReadNewNoChangeReturnsNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, "{\"uuid\":\"1\"}\n")

	_, st, err := ReadNew(p, TailState{Path: p})
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	lines, _, err := ReadNew(p, st)
	if err != nil {
		t.Fatalf("ReadNew(2): %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("got %d lines on an unchanged file, want 0", len(lines))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestReadNew -v`
Expected: FAIL — `undefined: ReadNew`, `undefined: TailState`.

- [ ] **Step 3: Write the implementation**

Create `internal/claudecode/tail.go`:

```go
package claudecode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// TailState is the resume point for one transcript file. It is persisted per
// Claude Code session so ingestion survives restarts.
type TailState struct {
	Path       string
	Size       int64
	ByteOffset int64
	// LastUUID is the transcript uuid of the most recently consumed line. It
	// is the re-sync anchor after a truncation: uuids are unordered, so this
	// is used for positional matching, never comparison.
	LastUUID string
}

// ReadNew returns the complete lines appended to path since st, along with
// the state to pass on the next call.
//
// Three rules make this safe against a file Claude Code is actively writing:
//
//  1. A trailing fragment with no newline is never consumed, and ByteOffset
//     never advances past the last '\n'. Consuming it would corrupt the
//     stream; advancing past it would drop a message.
//  2. If the file shrank below ByteOffset it was truncated or replaced.
//     Replay from zero and discard lines up to and including LastUUID.
//  3. If LastUUID is not found during that replay, the path holds different
//     content entirely. Emit the whole file.
func ReadNew(path string, st TailState) ([][]byte, TailState, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, st, fmt.Errorf("claudecode: stat %s: %w", path, err)
	}
	size := fi.Size()

	next := st
	next.Path = path
	next.Size = size

	replaying := false
	start := st.ByteOffset
	if size < st.ByteOffset {
		// Rule 2: truncated or replaced.
		replaying = true
		start = 0
	}
	if size == st.ByteOffset {
		return nil, next, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, st, fmt.Errorf("claudecode: open %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, st, fmt.Errorf("claudecode: seek %s: %w", path, err)
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, st, fmt.Errorf("claudecode: read %s: %w", path, err)
	}

	// Rule 1: keep only whole lines.
	nl := bytes.LastIndexByte(buf, '\n')
	if nl < 0 {
		return nil, next, nil
	}
	whole := buf[:nl+1]
	next.ByteOffset = start + int64(len(whole))

	raw := bytes.Split(bytes.TrimSuffix(whole, []byte("\n")), []byte("\n"))

	var out [][]byte
	if replaying && st.LastUUID != "" {
		out = afterUUID(raw, st.LastUUID)
	} else {
		out = raw
	}

	for _, l := range out {
		if u := uuidOf(l); u != "" {
			next.LastUUID = u
		}
	}
	return out, next, nil
}

// afterUUID returns the lines following the one whose uuid is target. If the
// target is not present the whole slice is returned (rule 3).
func afterUUID(lines [][]byte, target string) [][]byte {
	for i, l := range lines {
		if uuidOf(l) == target {
			return lines[i+1:]
		}
	}
	return lines
}

func uuidOf(line []byte) string {
	var probe struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return ""
	}
	return probe.UUID
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run TestReadNew -v`
Expected: PASS — all five tailer tests.

- [ ] **Step 5: Run the race detector on the tailer**

Run: `go test -race ./internal/claudecode/ -v`
Expected: PASS with no race warnings.

- [ ] **Step 6: Commit**

```bash
git add internal/claudecode/tail.go internal/claudecode/tail_test.go
git commit -m "feat(claudecode): transcript tailer with partial-line and truncation safety"
```

---

### Task 5: Ingest state store

**Files:**
- Create: `internal/claudecode/ingest_store.go`
- Create: `internal/claudecode/ingest_store_test.go`

**Interfaces:**
- Consumes: `TailState` (Task 4); `claude_ingest` table (Task 1); `*sqlitedb.DB`.
- Produces:
  - `type IngestStore struct` with `func NewIngestStore(db *sqlitedb.DB) *IngestStore`
  - `func (s *IngestStore) Get(externalID string) (huginnSessionID string, st TailState, found bool, err error)`
  - `func (s *IngestStore) Put(externalID, huginnSessionID string, st TailState) error`
  - `func (s *IngestStore) Delete(externalID string) error`

- [ ] **Step 1: Write the failing test**

Create `internal/claudecode/ingest_store_test.go`:

```go
package claudecode

import (
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func newTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestIngestStoreRoundTrip(t *testing.T) {
	s := NewIngestStore(newTestDB(t))

	if _, _, found, err := s.Get("nope"); err != nil || found {
		t.Fatalf("Get(missing) = found %v, err %v; want false, nil", found, err)
	}

	want := TailState{Path: "/tmp/a.jsonl", Size: 42, ByteOffset: 40, LastUUID: "u9"}
	if err := s.Put("ext-1", "SESS1", want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	sid, got, found, err := s.Get("ext-1")
	if err != nil || !found {
		t.Fatalf("Get = found %v, err %v", found, err)
	}
	if sid != "SESS1" {
		t.Errorf("session id = %q, want SESS1", sid)
	}
	if got != want {
		t.Errorf("state = %+v, want %+v", got, want)
	}
}

func TestIngestStorePutIsUpsert(t *testing.T) {
	s := NewIngestStore(newTestDB(t))

	if err := s.Put("ext-1", "SESS1", TailState{Path: "/a", ByteOffset: 10}); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	if err := s.Put("ext-1", "SESS1", TailState{Path: "/a", ByteOffset: 99}); err != nil {
		t.Fatalf("Put 2: %v", err)
	}

	_, got, _, err := s.Get("ext-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ByteOffset != 99 {
		t.Errorf("ByteOffset = %d, want 99", got.ByteOffset)
	}
}

func TestIngestStoreDelete(t *testing.T) {
	s := NewIngestStore(newTestDB(t))
	if err := s.Put("ext-1", "SESS1", TailState{Path: "/a"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete("ext-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, found, _ := s.Get("ext-1"); found {
		t.Error("Get after Delete found the row")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestIngestStore -v`
Expected: FAIL — `undefined: NewIngestStore`.

- [ ] **Step 3: Write the implementation**

Create `internal/claudecode/ingest_store.go`:

```go
package claudecode

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// IngestStore persists per-transcript resume state in the claude_ingest table.
type IngestStore struct {
	db *sqlitedb.DB
}

// NewIngestStore returns a store backed by db.
func NewIngestStore(db *sqlitedb.DB) *IngestStore { return &IngestStore{db: db} }

// Get returns the Huginn session id and resume state for a Claude Code
// session. found is false when the session has never been ingested.
func (s *IngestStore) Get(externalID string) (string, TailState, bool, error) {
	rdb := s.db.Read()
	if rdb == nil {
		return "", TailState{}, false, fmt.Errorf("claudecode: no read connection")
	}
	var (
		sid string
		st  TailState
	)
	err := rdb.QueryRow(`
		SELECT huginn_session_id, path, size, byte_offset, last_uuid
		FROM claude_ingest WHERE external_id = ?`, externalID,
	).Scan(&sid, &st.Path, &st.Size, &st.ByteOffset, &st.LastUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", TailState{}, false, nil
	}
	if err != nil {
		return "", TailState{}, false, fmt.Errorf("claudecode: get ingest state: %w", err)
	}
	return sid, st, true, nil
}

// Put upserts the resume state for a Claude Code session.
func (s *IngestStore) Put(externalID, huginnSessionID string, st TailState) error {
	wdb := s.db.Write()
	if wdb == nil {
		return fmt.Errorf("claudecode: no write connection")
	}
	_, err := wdb.Exec(`
		INSERT INTO claude_ingest
			(external_id, huginn_session_id, path, size, byte_offset, last_uuid, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(external_id) DO UPDATE SET
			huginn_session_id=excluded.huginn_session_id,
			path=excluded.path, size=excluded.size,
			byte_offset=excluded.byte_offset, last_uuid=excluded.last_uuid,
			updated_at=excluded.updated_at`,
		externalID, huginnSessionID, st.Path, st.Size, st.ByteOffset, st.LastUUID,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("claudecode: put ingest state: %w", err)
	}
	return nil
}

// Delete removes the resume state for a Claude Code session.
func (s *IngestStore) Delete(externalID string) error {
	wdb := s.db.Write()
	if wdb == nil {
		return fmt.Errorf("claudecode: no write connection")
	}
	if _, err := wdb.Exec(`DELETE FROM claude_ingest WHERE external_id = ?`, externalID); err != nil {
		return fmt.Errorf("claudecode: delete ingest state: %w", err)
	}
	return nil
}
```

Confirm the accessor names on `*sqlitedb.DB` before writing this — run `grep -n "func (d \*DB) \(Read\|Write\)" internal/sqlitedb/db.go` and match exactly.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run TestIngestStore -v`
Expected: PASS — all three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/claudecode/ingest_store.go internal/claudecode/ingest_store_test.go
git commit -m "feat(claudecode): persist per-transcript ingest offsets"
```

---

### Task 6: Ingester — wire tailer, mapper and session store together

**Files:**
- Create: `internal/claudecode/ingest.go`
- Create: `internal/claudecode/ingest_test.go`

**Interfaces:**
- Consumes: `ReadNew`, `TailState` (Task 4); `NewMapper`, `Mapped` (Task 3); `ParseLine`, `ParseTitle` (Task 2); `IngestStore` (Task 5); `LoadByExternalID` (Task 1).
- Produces:
  - `type SessionSink interface` — the narrow slice of the session store the ingester needs, so tests can fake it
  - `type Broadcaster interface { BroadcastToSession(sessionID, msgType string, payload map[string]any) }`
  - `type Ingester struct` with `func NewIngester(sink SessionSink, state *IngestStore, bc Broadcaster) *Ingester`
  - `func (i *Ingester) IngestFile(path string) (int, error)` — returns the number of messages appended

- [ ] **Step 1: Write the failing test**

Create `internal/claudecode/ingest_test.go`:

```go
package claudecode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

// fakeSink records what the ingester writes.
type fakeSink struct {
	sessions map[string]*session.Session // keyed by external id
	msgs     map[string][]session.SessionMessage
	threads  map[string][]session.SessionMessage
	nextID   int
}

func newFakeSink() *fakeSink {
	return &fakeSink{
		sessions: map[string]*session.Session{},
		msgs:     map[string][]session.SessionMessage{},
		threads:  map[string][]session.SessionMessage{},
	}
}

func (f *fakeSink) LoadByExternalID(kind, extID string) (*session.Session, error) {
	return f.sessions[extID], nil
}

func (f *fakeSink) New(title, workspaceRoot, model string) *session.Session {
	f.nextID++
	id := "SESS" + string(rune('0'+f.nextID))
	return &session.Session{ID: id, Manifest: session.Manifest{
		ID: id, SessionID: id, Title: title,
		WorkspaceRoot: workspaceRoot, Model: model, Status: "active",
	}}
}

func (f *fakeSink) SaveManifest(s *session.Session) error {
	f.sessions[s.Manifest.ExternalID] = s
	return nil
}

func (f *fakeSink) Append(s *session.Session, m session.SessionMessage) error {
	f.msgs[s.ID] = append(f.msgs[s.ID], m)
	return nil
}

func (f *fakeSink) AppendToThread(sessionID, threadID string, m session.SessionMessage) error {
	f.threads[sessionID+"/"+threadID] = append(f.threads[sessionID+"/"+threadID], m)
	return nil
}

type fakeBroadcaster struct{ calls int }

func (b *fakeBroadcaster) BroadcastToSession(string, string, map[string]any) { b.calls++ }

func newTestIngester(t *testing.T) (*Ingester, *fakeSink, *fakeBroadcaster) {
	t.Helper()
	sink := newFakeSink()
	bc := &fakeBroadcaster{}
	return NewIngester(sink, NewIngestStore(newTestDB(t)), bc), sink, bc
}

// copyFixture copies testdata/basic.jsonl into a temp dir under the file name
// Claude Code would use, so the ingester derives the session UUID from it.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	p := filepath.Join(t.TempDir(), "11111111-2222-3333-4444-555555555555.jsonl")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestIngestFileCreatesSessionAndMessages(t *testing.T) {
	ing, sink, bc := newTestIngester(t)
	p := copyFixture(t, "basic.jsonl")

	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if n != 3 {
		t.Errorf("appended %d messages, want 3", n)
	}

	sess := sink.sessions["11111111-2222-3333-4444-555555555555"]
	if sess == nil {
		t.Fatal("no session created for the transcript's session id")
	}
	if sess.Manifest.ExternalKind != "claude-code" {
		t.Errorf("ExternalKind = %q", sess.Manifest.ExternalKind)
	}
	if sess.Manifest.WorkspaceRoot != "/tmp/proj" {
		t.Errorf("WorkspaceRoot = %q, want /tmp/proj", sess.Manifest.WorkspaceRoot)
	}
	if sess.Manifest.Title != "Fix the auth bug" {
		t.Errorf("Title = %q, want the custom-title value", sess.Manifest.Title)
	}
	if sess.Manifest.Model != "claude-opus-5" {
		t.Errorf("Model = %q", sess.Manifest.Model)
	}
	if bc.calls == 0 {
		t.Error("expected at least one broadcast")
	}
}

func TestIngestFileIsIdempotent(t *testing.T) {
	ing, sink, _ := newTestIngester(t)
	p := copyFixture(t, "basic.jsonl")

	if _, err := ing.IngestFile(p); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if n != 0 {
		t.Errorf("second ingest appended %d messages, want 0", n)
	}

	sess := sink.sessions["11111111-2222-3333-4444-555555555555"]
	if got := len(sink.msgs[sess.ID]); got != 3 {
		t.Errorf("total messages = %d, want 3", got)
	}
}

func TestIngestFileIgnoresNonTranscriptNames(t *testing.T) {
	ing, _, _ := newTestIngester(t)
	p := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if n != 0 {
		t.Errorf("appended %d messages for a non-transcript file, want 0", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestIngestFile -v`
Expected: FAIL — `undefined: NewIngester`, `undefined: Ingester`.

- [ ] **Step 3: Write the implementation**

Create `internal/claudecode/ingest.go`:

```go
package claudecode

import (
	"bytes"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/scrypster/huginn/internal/session"
)

// ExternalKind is the value written to Manifest.ExternalKind for sessions
// bridged from Claude Code.
const ExternalKind = "claude-code"

// SessionSink is the slice of the session store the ingester needs. It is an
// interface so tests can substitute a fake without a database.
type SessionSink interface {
	LoadByExternalID(kind, externalID string) (*session.Session, error)
	New(title, workspaceRoot, model string) *session.Session
	SaveManifest(s *session.Session) error
	Append(s *session.Session, m session.SessionMessage) error
	AppendToThread(sessionID, threadID string, m session.SessionMessage) error
}

// Broadcaster pushes live updates to connected web UI clients. *server.Server
// satisfies this.
type Broadcaster interface {
	BroadcastToSession(sessionID, msgType string, payload map[string]any)
}

// Ingester converts Claude Code transcripts into Huginn sessions.
//
// Two pieces of per-session state make tool results work. Both exist because a
// tool_use block and the tool_result that completes it routinely arrive in
// DIFFERENT reads: a tool takes longer to run than the watcher's 150ms debounce.
//
//   - mappers retains each session's Mapper across IngestFile calls. A fresh
//     Mapper per call would forget every open tool call between reads.
//   - pending holds messages whose tool calls are not yet resolved. Messages are
//     appended to the store one batch LATE, after the following read has had a
//     chance to fill in their results. Appending eagerly writes the message to
//     storage before its results exist, and the Mapper's later in-place fill-in
//     never reaches the row that was already written.
type Ingester struct {
	sink  SessionSink
	state *IngestStore
	bc    Broadcaster

	mu      sync.Mutex
	mappers map[string]*Mapper
	pending map[string][]Mapped
}

// NewIngester wires an ingester. bc may be nil (backfill runs without a web UI).
func NewIngester(sink SessionSink, state *IngestStore, bc Broadcaster) *Ingester {
	return &Ingester{
		sink:    sink,
		state:   state,
		bc:      bc,
		mappers: map[string]*Mapper{},
		pending: map[string][]Mapped{},
	}
}

// mapperFor returns the retained Mapper for a Claude session, creating it on
// first use. Backfill calls IngestFile concurrently across distinct sessions,
// so the map is mutex-guarded.
func (i *Ingester) mapperFor(externalID string) *Mapper {
	i.mu.Lock()
	defer i.mu.Unlock()
	m, ok := i.mappers[externalID]
	if !ok {
		m = NewMapper()
		i.mappers[externalID] = m
	}
	return m
}

// takePending removes and returns the messages held back from the previous read.
func (i *Ingester) takePending(externalID string) []Mapped {
	i.mu.Lock()
	defer i.mu.Unlock()
	held := i.pending[externalID]
	delete(i.pending, externalID)
	return held
}

// holdPending stores messages whose tool calls are still unresolved.
func (i *Ingester) holdPending(externalID string, held []Mapped) {
	if len(held) == 0 {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.pending[externalID] = append(i.pending[externalID], held...)
}

// resolved reports whether every tool call on a message has a result, so the
// message is safe to persist.
func resolved(m Mapped) bool {
	for _, c := range m.Msg.ToolCalls {
		if c.Result == "" {
			return false
		}
	}
	return true
}

// FlushIdle appends any messages still held back for sessions that have stopped
// producing output, so a conversation that ends on an unanswered tool call is
// not left unpersisted. The watcher calls this on its rescan tick.
func (i *Ingester) FlushIdle() {
	i.mu.Lock()
	ids := make([]string, 0, len(i.pending))
	for id := range i.pending {
		ids = append(ids, id)
	}
	i.mu.Unlock()

	for _, id := range ids {
		sessionID, _, found, err := i.state.Get(id)
		if err != nil || !found {
			continue
		}
		sess, err := i.sink.LoadByExternalID(ExternalKind, id)
		if err != nil || sess == nil {
			_ = sessionID
			continue
		}
		for _, m := range i.takePending(id) {
			i.appendOne(sess, m)
		}
	}
}

// IngestFile consumes everything appended to a transcript since the last call
// and returns the number of messages appended. It is safe to call repeatedly;
// an unchanged file appends nothing.
func (i *Ingester) IngestFile(path string) (int, error) {
	externalID := sessionIDFromPath(path)
	if externalID == "" {
		return 0, nil
	}

	sessionID, st, found, err := i.state.Get(externalID)
	if err != nil {
		return 0, err
	}
	if !found {
		st = TailState{Path: path}
	}

	lines, next, err := ReadNew(path, st)
	if err != nil {
		return 0, err
	}
	if len(lines) == 0 {
		return 0, nil
	}

	sess, err := i.resolveSession(externalID, sessionID, lines)
	if err != nil {
		return 0, err
	}

	mapper := i.mapperFor(externalID)
	var fresh []Mapped
	var appended int
	for _, raw := range lines {
		if title, ok := ParseTitle(raw); ok && sess.Manifest.Title != title {
			sess.Manifest.Title = title
		}
		l, ok := ParseLine(raw)
		if !ok {
			continue
		}
		if sess.Manifest.WorkspaceRoot == "" && l.CWD != "" {
			sess.Manifest.WorkspaceRoot = l.CWD
			sess.Manifest.WorkspaceName = filepath.Base(l.CWD)
		}
		fresh = append(fresh, mapper.Add(l)...)
	}

	// Order matters. Every line in this batch has now been through the Mapper,
	// so tool_results in this batch have already filled in the results of
	// messages held from the previous batch (the Mapper mutates the tool-call
	// slice those Mapped values share). Only now is it safe to persist them,
	// and they must go in before this batch's own messages to keep chronology.
	for _, m := range i.takePending(externalID) {
		if i.appendOne(sess, m) {
			appended++
		}
	}

	var hold []Mapped
	for _, m := range fresh {
		if !resolved(m) {
			hold = append(hold, m)
			continue
		}
		if i.appendOne(sess, m) {
			appended++
		}
	}
	i.holdPending(externalID, hold)

	if mdl := mapper.Model(); mdl != "" {
		sess.Manifest.Model = mdl
	}
	if err := i.sink.SaveManifest(sess); err != nil {
		return appended, fmt.Errorf("claudecode: save manifest: %w", err)
	}
	if err := i.state.Put(externalID, sess.ID, next); err != nil {
		return appended, err
	}
	return appended, nil
}

// resolveSession finds or creates the Huginn session for a Claude Code
// session id, seeding metadata from the first content line.
func (i *Ingester) resolveSession(externalID, knownID string, lines [][]byte) (*session.Session, error) {
	existing, err := i.sink.LoadByExternalID(ExternalKind, externalID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	// knownID without a row means the session was deleted from under us;
	// fall through and create a fresh one.
	_ = knownID

	title, cwd := seedMetadata(lines)
	s := i.sink.New(title, cwd, "")
	s.Manifest.ExternalKind = ExternalKind
	s.Manifest.ExternalID = externalID
	if cwd != "" {
		s.Manifest.WorkspaceRoot = cwd
		s.Manifest.WorkspaceName = filepath.Base(cwd)
	}
	if err := i.sink.SaveManifest(s); err != nil {
		return nil, fmt.Errorf("claudecode: create session: %w", err)
	}
	return s, nil
}

// seedMetadata scans the first lines for a title and a working directory.
// The custom-title line wins; otherwise the first user text becomes the title.
func seedMetadata(lines [][]byte) (title, cwd string) {
	for _, raw := range lines {
		if t, ok := ParseTitle(raw); ok && title == "" {
			title = t
		}
		l, ok := ParseLine(raw)
		if !ok {
			continue
		}
		if cwd == "" && l.CWD != "" {
			cwd = l.CWD
		}
		if title == "" && l.Type == "user" {
			m := NewMapper()
			for _, mapped := range m.Add(l) {
				title = firstLine(mapped.Msg.Content)
			}
		}
		if title != "" && cwd != "" {
			return title, cwd
		}
	}
	return title, cwd
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// appendOne persists a single mapped message and broadcasts it. It returns
// false if the store rejected it, which is logged but never fatal to the file.
func (i *Ingester) appendOne(sess *session.Session, m Mapped) bool {
	if m.ThreadID != "" {
		if err := i.sink.AppendToThread(sess.ID, m.ThreadID, m.Msg); err != nil {
			slog.Warn("claudecode: append to thread failed", "session", sess.ID, "err", err)
			return false
		}
	} else if err := i.sink.Append(sess, m.Msg); err != nil {
		slog.Warn("claudecode: append failed", "session", sess.ID, "err", err)
		return false
	}
	i.broadcast(sess.ID, m)
	return true
}

func (i *Ingester) broadcast(sessionID string, m Mapped) {
	if i.bc == nil {
		return
	}
	i.bc.BroadcastToSession(sessionID, "message_new", map[string]any{
		"role":      m.Msg.Role,
		"content":   m.Msg.Content,
		"thread_id": m.ThreadID,
		"source":    ExternalKind,
	})
}

// sessionIDFromPath extracts the Claude Code session UUID from a transcript
// filename. Returns "" for any file that is not a .jsonl transcript.
func sessionIDFromPath(path string) string {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".jsonl") {
		return ""
	}
	return strings.TrimSuffix(base, ".jsonl")
}

var _ = bytes.MinRead // keep the bytes import if unused after edits; remove if not needed
```

Remove the trailing `var _ = bytes.MinRead` line and the `"bytes"` import if the compiler reports them unused.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run TestIngestFile -v`
Expected: PASS — all three tests.

- [ ] **Step 5: Run the whole package**

Run: `go test ./internal/claudecode/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/claudecode/ingest.go internal/claudecode/ingest_test.go
git commit -m "feat(claudecode): ingest transcripts into Huginn sessions"
```

---

### Task 7: Filesystem watcher

**Files:**
- Create: `internal/claudecode/watcher.go`
- Create: `internal/claudecode/watcher_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `Ingester.IngestFile` (Task 6).
- Produces:
  - `type Watcher struct` with `func NewWatcher(root string, ing *Ingester) *Watcher`
  - `func (w *Watcher) Run(ctx context.Context) error` — blocks until ctx is cancelled
  - `func DefaultRoot() string` — `~/.claude/projects`

- [ ] **Step 1: Add the dependency**

`fsnotify` is not currently a dependency.

```bash
go get github.com/fsnotify/fsnotify@latest
go mod tidy
```

- [ ] **Step 2: Make `fakeSink` race-safe**

Tasks 2-6 only ever called `fakeSink` from the test goroutine. From this task on, the WATCHER's
goroutine writes to it while the test polls — `go test -race` will flag that unless the fake is
guarded. In `internal/claudecode/ingest_test.go`, add a mutex to `fakeSink` and take it in every
method:

```go
type fakeSink struct {
	mu       sync.Mutex
	sessions map[string]*session.Session
	msgs     map[string][]session.SessionMessage
	threads  map[string][]session.SessionMessage
	nextID   int
}
```

Lock and defer-unlock in `LoadByExternalID`, `New`, `SaveManifest`, `Append` and
`AppendToThread`. Add accessor helpers the tests can poll safely:

```go
func (f *fakeSink) session(extID string) *session.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[extID]
}

func (f *fakeSink) messageCount(sessionID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs[sessionID])
}
```

Update the Task 6 tests to use these accessors wherever they currently touch the maps directly.
`fakeBroadcaster.calls` needs the same treatment — guard it with its own mutex and read it
through a method.

- [ ] **Step 3: Write the failing test**

Create `internal/claudecode/watcher_test.go`:

```go
package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherIngestsNewTranscript(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-tmp-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ing, sink, _ := newTestIngester(t)
	w := NewWatcher(root, ing)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(ctx) }()

	// Give the watcher a moment to register its directory watches.
	time.Sleep(200 * time.Millisecond)

	fixture, err := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(proj, "11111111-2222-3333-4444-555555555555.jsonl")
	if err := os.WriteFile(dst, fixture, 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		if sink.sessions["11111111-2222-3333-4444-555555555555"] != nil {
			return // success
		}
		select {
		case <-deadline:
			t.Fatal("watcher did not ingest the transcript within 5s")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestWatcherStopsOnContextCancel(t *testing.T) {
	ing, _, _ := newTestIngester(t)
	w := NewWatcher(t.TempDir(), ing)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("Run returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3s of cancellation")
	}
}

func TestWatcherMissingRootIsNotFatal(t *testing.T) {
	ing, _, _ := newTestIngester(t)
	w := NewWatcher(filepath.Join(t.TempDir(), "does-not-exist"), ing)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := w.Run(ctx); err != nil && err != context.Canceled {
		t.Errorf("Run on a missing root returned %v, want nil or context.Canceled", err)
	}
}
```

ALSO add this test to the same file. Without it, a watcher that reacts ONLY to `Create` events
passes every other test in this task — and would never ingest an ongoing conversation, because
Claude Code creates a transcript once and then appends to it for the rest of the session. This
is the live case; the create-only case is the rare one.

```go
func TestWatcherIngestsLinesAppendedAfterCreate(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-tmp-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ing, sink, _ := newTestIngester(t)
	w := NewWatcher(root, ing)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()
	time.Sleep(200 * time.Millisecond)

	const extID = "33333333-3333-3333-4444-555555555555"
	dst := filepath.Join(proj, extID+".jsonl")

	u1 := `{"type":"user","uuid":"y1","sessionId":"` + extID + `","cwd":"/tmp/p3","timestamp":"2026-08-25T10:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"first"}]}}` + "\n"
	a1 := `{"type":"assistant","uuid":"y2","parentUuid":"y1","sessionId":"` + extID + `","timestamp":"2026-08-25T10:00:02.000Z","message":{"role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"second"}]}}` + "\n"

	if err := os.WriteFile(dst, []byte(u1), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	sess := waitForSession(t, sink, extID, 5*time.Second)
	waitForCount(t, sink, sess.ID, 1, 5*time.Second)

	// The session continues: Claude Code APPENDS to the same file.
	appendFile(t, dst, a1)
	waitForCount(t, sink, sess.ID, 2, 5*time.Second)
}

func waitForSession(t *testing.T, sink *fakeSink, extID string, d time.Duration) *session.Session {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if s := sink.session(extID); s != nil {
			return s
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %s never appeared within %s", extID, d)
	return nil
}

func waitForCount(t *testing.T, sink *fakeSink, sessionID string, want int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	var got int
	for time.Now().Before(deadline) {
		got = sink.messageCount(sessionID)
		if got >= want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("message count reached %d, want %d, within %s", got, want, d)
}
```

Rewrite `TestWatcherIngestsNewTranscript` to poll via `waitForSession` rather than reading
`sink.sessions` directly, for the same race reason.

Note: this test's assistant line has NO tool calls, so the ingester's deferred-append hold-back
does not apply and the message lands on the first read after the append. If you find yourself
needing `FlushIdle` to make it pass, something is wrong — report it rather than adding the call.

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestWatcher -v`
Expected: FAIL — `undefined: NewWatcher`.

- [ ] **Step 5: Write the implementation**

Create `internal/claudecode/watcher.go`:

```go
package claudecode

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounceWindow coalesces the burst of write events Claude Code produces
// while streaming a single turn, so we re-read a transcript at most this often.
const debounceWindow = 150 * time.Millisecond

// Watcher tails every Claude Code transcript under a root directory.
//
// It watches the root and each project subdirectory. Claude Code creates a new
// subdirectory the first time it is run in a given working directory, so newly
// created directories are picked up and watched as they appear.
type Watcher struct {
	root string
	ing  *Ingester

	mu      sync.Mutex
	pending map[string]*time.Timer
}

// NewWatcher returns a watcher over root (typically DefaultRoot()).
func NewWatcher(root string, ing *Ingester) *Watcher {
	return &Watcher{root: root, ing: ing, pending: map[string]*time.Timer{}}
}

// DefaultRoot is where Claude Code stores session transcripts.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// Run watches until ctx is cancelled. A missing root is not an error: Claude
// Code may not have been run on this machine yet. Watcher errors are logged
// and retried rather than returned, so a transient failure never stops serve.
func (w *Watcher) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("claudecode: cannot create watcher, bridge disabled", "err", err)
		<-ctx.Done()
		return nil
	}
	defer watcher.Close()

	w.addTree(watcher)

	// Re-scan periodically so project directories created while the watcher
	// was running (or a root that appears later) are picked up.
	rescan := time.NewTicker(30 * time.Second)
	defer rescan.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-rescan.C:
			w.addTree(watcher)
			// Persist messages held back for sessions that have gone quiet,
			// so a conversation ending on an unanswered tool call is not
			// left unwritten.
			w.ing.FlushIdle()

		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			w.handle(watcher, ev)

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			slog.Warn("claudecode: watcher error", "err", err)
		}
	}
}

// addTree registers the root and every immediate subdirectory. Adding a path
// that is already watched is a no-op in fsnotify.
func (w *Watcher) addTree(watcher *fsnotify.Watcher) {
	if w.root == "" {
		return
	}
	if err := watcher.Add(w.root); err != nil {
		slog.Debug("claudecode: cannot watch root", "root", w.root, "err", err)
		return
	}
	entries, err := os.ReadDir(w.root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := watcher.Add(filepath.Join(w.root, e.Name())); err != nil {
			slog.Debug("claudecode: cannot watch project dir", "dir", e.Name(), "err", err)
		}
	}
}

func (w *Watcher) handle(watcher *fsnotify.Watcher, ev fsnotify.Event) {
	// A new project directory: start watching it.
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			if err := watcher.Add(ev.Name); err != nil {
				slog.Debug("claudecode: cannot watch new dir", "dir", ev.Name, "err", err)
			}
			return
		}
	}
	if !strings.HasSuffix(ev.Name, ".jsonl") {
		return
	}
	if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return
	}
	w.schedule(ev.Name)
}

// schedule debounces repeated writes to the same transcript.
func (w *Watcher) schedule(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.pending[path]; ok {
		t.Reset(debounceWindow)
		return
	}
	w.pending[path] = time.AfterFunc(debounceWindow, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()

		if _, err := w.ing.IngestFile(path); err != nil {
			slog.Warn("claudecode: ingest failed", "path", path, "err", err)
		}
	})
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run TestWatcher -v`
Expected: PASS — all four tests.

- [ ] **Step 7: Run with the race detector**

Run: `go test -race ./internal/claudecode/ -v`
Expected: PASS. The watcher's `pending` map is guarded by `w.mu`, and `fakeSink`/`fakeBroadcaster`
were guarded in Step 2. A race report naming `fakeSink` means Step 2 was incomplete; one naming
`watcher.go` means the lock discipline in the debounce path is wrong.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/claudecode/watcher.go internal/claudecode/watcher_test.go \
        internal/claudecode/ingest_test.go
git commit -m "feat(claudecode): fsnotify watcher over ~/.claude/projects"
```

---

### Task 8: Backfill

**Files:**
- Create: `internal/claudecode/backfill.go`
- Create: `internal/claudecode/backfill_test.go`

**Interfaces:**
- Consumes: `Ingester.IngestFile` (Task 6).
- Produces:
  - `type BackfillResult struct { Files, Messages, Skipped int }`
  - `func Backfill(ctx context.Context, root string, ing *Ingester, maxFileMB int) (BackfillResult, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/claudecode/backfill_test.go`:

```go
package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedProjects(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, "-tmp-proj"+string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// Give each project a distinct session uuid.
		content := strings.ReplaceAll(string(fixture),
			"11111111-2222-3333-4444-555555555555",
			"1111111"+string(rune('a'+i))+"-2222-3333-4444-555555555555")
		name := "1111111" + string(rune('a'+i)) + "-2222-3333-4444-555555555555.jsonl"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

func TestBackfillImportsEveryTranscript(t *testing.T) {
	root := seedProjects(t, 3)
	ing, sink, _ := newTestIngester(t)

	res, err := Backfill(context.Background(), root, ing, 50)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if res.Files != 3 {
		t.Errorf("Files = %d, want 3", res.Files)
	}
	if res.Messages != 9 {
		t.Errorf("Messages = %d, want 9 (3 per transcript)", res.Messages)
	}
	if len(sink.sessions) != 3 {
		t.Errorf("created %d sessions, want 3", len(sink.sessions))
	}
}

func TestBackfillIsIdempotent(t *testing.T) {
	root := seedProjects(t, 2)
	ing, sink, _ := newTestIngester(t)

	if _, err := Backfill(context.Background(), root, ing, 50); err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	res, err := Backfill(context.Background(), root, ing, 50)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if res.Messages != 0 {
		t.Errorf("second backfill appended %d messages, want 0", res.Messages)
	}

	total := 0
	for _, msgs := range sink.msgs {
		total += len(msgs)
	}
	if total != 6 {
		t.Errorf("total messages = %d, want 6", total)
	}
}

func TestBackfillSkipsOversizedFiles(t *testing.T) {
	root := seedProjects(t, 1)
	ing, _, _ := newTestIngester(t)

	// maxFileMB of 0 means every file exceeds the limit.
	res, err := Backfill(context.Background(), root, ing, 0)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	if res.Messages != 0 {
		t.Errorf("Messages = %d, want 0", res.Messages)
	}
}

func TestBackfillMissingRootIsNotAnError(t *testing.T) {
	ing, _, _ := newTestIngester(t)
	res, err := Backfill(context.Background(), filepath.Join(t.TempDir(), "nope"), ing, 50)
	if err != nil {
		t.Errorf("Backfill on a missing root returned %v, want nil", err)
	}
	if res.Files != 0 {
		t.Errorf("Files = %d, want 0", res.Files)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestBackfill -v`
Expected: FAIL — `undefined: Backfill`.

- [ ] **Step 3: Write the implementation**

Create `internal/claudecode/backfill.go`:

```go
package claudecode

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// backfillConcurrency bounds parallel transcript imports so a large history
// does not saturate the SQLite write connection.
const backfillConcurrency = 4

// BackfillResult summarises a historical import.
type BackfillResult struct {
	Files    int // transcripts read
	Messages int // messages appended
	Skipped  int // transcripts skipped for exceeding maxFileMB
}

// Backfill imports every Claude Code transcript under root that has not
// already been fully consumed. It is idempotent: transcripts already at their
// full offset append nothing.
//
// A missing root is not an error — Claude Code may never have run here.
// Transcripts larger than maxFileMB are skipped and logged by name and size;
// truncation is never silent.
func Backfill(ctx context.Context, root string, ing *Ingester, maxFileMB int) (BackfillResult, error) {
	var res BackfillResult
	if root == "" {
		return res, nil
	}

	paths, err := transcriptPaths(root)
	if err != nil {
		return res, err
	}

	maxBytes := int64(maxFileMB) * 1024 * 1024

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, backfillConcurrency)
	)

	for _, p := range paths {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}

		fi, statErr := os.Stat(p)
		if statErr != nil {
			continue
		}
		if fi.Size() > maxBytes {
			slog.Warn("claudecode: skipping oversized transcript",
				"path", p, "size_bytes", fi.Size(), "limit_bytes", maxBytes)
			mu.Lock()
			res.Skipped++
			mu.Unlock()
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			n, ingErr := ing.IngestFile(path)
			mu.Lock()
			defer mu.Unlock()
			if ingErr != nil {
				slog.Warn("claudecode: backfill ingest failed", "path", path, "err", ingErr)
				return
			}
			res.Files++
			res.Messages += n
		}(p)
	}

	wg.Wait()
	return res, nil
}

// transcriptPaths lists every .jsonl file directly inside a project directory
// under root. A missing root yields an empty list, not an error.
func transcriptPaths(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			// An unreadable subdirectory should not abort the whole walk.
			slog.Debug("claudecode: skipping unreadable path", "path", path, "err", err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return out, err
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run TestBackfill -v`
Expected: PASS — all four tests.

`TestBackfillIsIdempotent` is the load-bearing one: it proves the offset store and the tailer agree. If it fails, the bug is in Task 4 or Task 5, not here.

- [ ] **Step 5: Run with the race detector**

Run: `go test -race ./internal/claudecode/ -run TestBackfill -v`
Expected: PASS. `res` is mutated from goroutines under `mu`.

- [ ] **Step 6: Commit**

```bash
git add internal/claudecode/backfill.go internal/claudecode/backfill_test.go
git commit -m "feat(claudecode): backfill historical Claude Code transcripts"
```

---

### Task 9: Configuration

**Files:**
- Create: `internal/claudecode/config.go`
- Create: `internal/claudecode/config_test.go`
- Modify: `internal/config/` (the file declaring the top-level config struct)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Config struct` with nested `Watch` and `Delegate` structs
  - `func DefaultConfig() Config`
  - `func (c Config) WatchEnabled() bool`, `func (c Config) DelegateEnabled() bool`

- [ ] **Step 1: Understand the config conventions (already located for you)**

VERIFIED locations in `internal/config/config.go`:
- `Config` struct — the `WebUI WebUIConfig \`json:"web_ui"\`` field is at **line 113**.
- `Default()` — the `WebUI: WebUIConfig{...}` literal is at **line ~146**.
- `const currentConfigVersion = 13` at **line 181**.
- The migration chain is a slice of `func(cfg *Config)` ending `migrateV12toV13,` at **line 197**.
- `internal/config` imports only `internal/mcp`, and `internal/claudecode` does not import
  `internal/config` — so importing `claudecode` here creates NO cycle. The plan's earlier
  fallback (a duplicate struct plus a converter) is dead; do not take it.

This repo versions its config and migrates it forward. Adding a block means four coordinated
edits, not one. Read `migrateV11toV12` (which "adds ActiveSessionID") as your model — it is
essentially a documented no-op, because `LoadFrom` unmarshals the file OVER `Default()`, so
absent JSON keys retain their defaults through merging.

CONFIRM THAT MERGE BEHAVIOUR YOURSELF before relying on it: read `LoadFrom` and check it starts
from `Default()` (the comment at `config.go:201-203` claims it does). If it does NOT merge over
defaults, your `migrateV13toV14` must instead fill each zero-valued field from
`claudecode.DefaultConfig()` individually — and say so in your report.

- [ ] **Step 2: Write the failing test**

Create `internal/claudecode/config_test.go`:

```go
package claudecode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultConfigIsDisabled(t *testing.T) {
	c := DefaultConfig()
	if c.Enabled {
		t.Error("DefaultConfig().Enabled = true, want false — the bridge reads every transcript on the machine and must be opt-in")
	}
	if c.WatchEnabled() {
		t.Error("WatchEnabled() = true while Enabled is false")
	}
	if c.DelegateEnabled() {
		t.Error("DelegateEnabled() = true while Enabled is false")
	}
}

func TestSubsystemsRequireTopLevelEnable(t *testing.T) {
	c := DefaultConfig()
	c.Enabled = true
	if !c.WatchEnabled() {
		t.Error("WatchEnabled() = false with Enabled=true and Watch.Enabled default true")
	}
	c.Watch.Enabled = false
	if c.WatchEnabled() {
		t.Error("WatchEnabled() = true with Watch.Enabled=false")
	}
}

func TestDefaultsMatchTheSpec(t *testing.T) {
	c := DefaultConfig()
	if c.Binary != "claude" {
		t.Errorf("Binary = %q, want claude", c.Binary)
	}
	if c.Watch.MaxFileMB != 50 {
		t.Errorf("MaxFileMB = %d, want 50", c.Watch.MaxFileMB)
	}
	if c.Delegate.PermissionMode != "acceptEdits" {
		t.Errorf("PermissionMode = %q, want acceptEdits", c.Delegate.PermissionMode)
	}
	if c.Delegate.MaxTurns != 30 {
		t.Errorf("MaxTurns = %d, want 30", c.Delegate.MaxTurns)
	}
	if c.Delegate.TimeoutSecs != 900 {
		t.Errorf("TimeoutSecs = %d, want 900", c.Delegate.TimeoutSecs)
	}
	if c.Delegate.SkipPermissions {
		t.Error("SkipPermissions must default to false")
	}
}

func TestConfigJSONTags(t *testing.T) {
	b, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"enabled"`, `"binary"`, `"watch"`, `"delegate"`,
		`"max_file_mb"`, `"permission_mode"`, `"timeout_secs"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("marshalled config missing %s: %s", want, b)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run 'TestDefaultConfig|TestSubsystems|TestDefaultsMatch|TestConfigJSON' -v`
Expected: FAIL — `undefined: DefaultConfig`.

- [ ] **Step 4: Write the implementation**

Create `internal/claudecode/config.go`:

```go
package claudecode

// Config controls the Claude Code bridge. It is stored under the
// "claude_code" key in ~/.huginn/config.json.
//
// Enabled defaults to false: turning the bridge on means Huginn reads every
// Claude Code transcript on the machine, which is a deliberate opt-in.
type Config struct {
	Enabled bool   `json:"enabled"`
	Binary  string `json:"binary"`

	Watch    WatchConfig    `json:"watch"`
	Delegate DelegateConfig `json:"delegate"`
}

// WatchConfig controls transcript ingestion.
type WatchConfig struct {
	Enabled bool `json:"enabled"`
	// Backfill imports existing transcripts on startup.
	Backfill bool `json:"backfill"`
	// MaxFileMB skips transcripts larger than this, with a log line.
	MaxFileMB int `json:"max_file_mb"`
}

// DelegateConfig controls the claude_code tool.
type DelegateConfig struct {
	Enabled        bool   `json:"enabled"`
	DefaultModel   string `json:"default_model"`
	PermissionMode string `json:"permission_mode"`
	MaxTurns       int    `json:"max_turns"`
	TimeoutSecs    int    `json:"timeout_secs"`
	// SkipPermissions passes --dangerously-skip-permissions. It must be set
	// explicitly and is never inferred.
	SkipPermissions bool `json:"skip_permissions"`
}

// DefaultConfig returns the bridge defaults: off, with sensible values for
// when it is switched on.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Binary:  "claude",
		Watch: WatchConfig{
			Enabled:   true,
			Backfill:  true,
			MaxFileMB: 50,
		},
		Delegate: DelegateConfig{
			Enabled:         true,
			DefaultModel:    "sonnet",
			PermissionMode:  "acceptEdits",
			MaxTurns:        30,
			TimeoutSecs:     900,
			SkipPermissions: false,
		},
	}
}

// WatchEnabled reports whether the transcript watcher should run.
func (c Config) WatchEnabled() bool { return c.Enabled && c.Watch.Enabled }

// DelegateEnabled reports whether the claude_code tool should be registered.
func (c Config) DelegateEnabled() bool { return c.Enabled && c.Delegate.Enabled }
```

- [ ] **Step 5: Wire the block into Huginn's config (four coordinated edits)**

Import `"github.com/scrypster/huginn/internal/claudecode"` in `internal/config/config.go`, then:

**(a)** Add the field to the `Config` struct beside `WebUI` (line 113):

```go
	ClaudeCode claudecode.Config `json:"claude_code"`
```

**(b)** Add it to `Default()` beside the `WebUI:` literal (line ~146):

```go
		ClaudeCode: claudecode.DefaultConfig(),
```

**(c)** Bump the version constant (line 181):

```go
const currentConfigVersion = 14
```

**(d)** Append a migration to the chain (after `migrateV12toV13,` at line 197) and define it
alongside the other `migrateVNtoVN+1` functions:

```go
	migrateV13toV14,
```

```go
// migrateV13toV14 adds the ClaudeCode bridge block. No field renames or removals: LoadFrom
// unmarshals the persisted file over Default(), so a config predating this block simply keeps
// claudecode.DefaultConfig() — which is opt-out (Enabled: false). The migration exists to
// record the version bump, matching migrateV11toV12's treatment of ActiveSessionID.
func migrateV13toV14(_ *Config) {}
```

Do NOT switch the bridge on for existing installs. A user upgrading must opt in deliberately —
enabling it means Huginn reads every Claude Code transcript on the machine.

REAL-WORLD CHECK: the developer's own `~/.huginn/config.json` is at `"version": 13` with no
`claude_code` key, and has a customised `web_ui.port` of 9200. Your change must leave that file
loadable and must not reset their port.

- [ ] **Step 5b: Test the migration, not just the defaults**

The tests in Step 2 only assert `DefaultConfig()`'s values. Nothing there covers the version
bump or the migration-chain entry — which is the part that can actually break a real install.
VERIFIED mechanics you are testing against: `LoadFrom` does `cfg := Default()` then
`json.Unmarshal(data, cfg)`, so absent keys keep their defaults; it then runs forward-only
migrations while `cfg.Version < currentConfigVersion`, indexing `configMigrations[cfg.Version]`.
`migrateV12toV13` sits at index 12, so `migrateV13toV14` MUST land at index 13 or a v13 config
never migrates.

Add to `internal/config/config_test.go` (or a new `claude_code_config_test.go` in the same
package):

```go
func TestClaudeCodeMigrationRegisteredAtRightIndex(t *testing.T) {
	if currentConfigVersion != 14 {
		t.Fatalf("currentConfigVersion = %d, want 14", currentConfigVersion)
	}
	if len(configMigrations) != 14 {
		t.Fatalf("len(configMigrations) = %d, want 14 — a v13 config indexes configMigrations[13]", len(configMigrations))
	}
}

func TestV13ConfigMigratesAndKeepsCustomPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// A config shaped like a real pre-bridge install: version 13, a CUSTOMISED
	// web_ui port, and no claude_code key at all.
	const v13 = `{"version":13,"web_ui":{"enabled":true,"port":9200,"auto_open":true,"bind":"127.0.0.1"}}`
	if err := os.WriteFile(path, []byte(v13), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	if cfg.Version != 14 {
		t.Errorf("Version = %d, want 14", cfg.Version)
	}
	if cfg.WebUI.Port != 9200 {
		t.Errorf("WebUI.Port = %d, want 9200 — migration must not reset a customised port", cfg.WebUI.Port)
	}
	if cfg.ClaudeCode.Enabled {
		t.Error("ClaudeCode.Enabled = true after upgrade; the bridge must stay opt-in for existing installs")
	}
	if cfg.ClaudeCode.Binary != "claude" {
		t.Errorf("ClaudeCode.Binary = %q, want \"claude\" — defaults must fill in for an absent block", cfg.ClaudeCode.Binary)
	}
	if cfg.ClaudeCode.Watch.MaxFileMB != 50 {
		t.Errorf("Watch.MaxFileMB = %d, want 50", cfg.ClaudeCode.Watch.MaxFileMB)
	}
	if cfg.ClaudeCode.Delegate.TimeoutSecs != 900 {
		t.Errorf("Delegate.TimeoutSecs = %d, want 900", cfg.ClaudeCode.Delegate.TimeoutSecs)
	}
}
```

That second test is the one that matters: it is shaped exactly like the developer's real
`~/.huginn/config.json` (version 13, `web_ui.port` 9200, no `claude_code` key). If it fails,
upgrading would break or silently reconfigure a live install.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ ./internal/config/ -v`
Expected: PASS.

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/claudecode/config.go internal/claudecode/config_test.go internal/config/
git commit -m "feat(claudecode): opt-in configuration block"
```

---

### Task 10: Delegation — run `claude -p` and parse stream-json

**Files:**
- Create: `internal/claudecode/delegate.go`
- Create: `internal/claudecode/delegate_unix.go` (`//go:build !windows`)
- Create: `internal/claudecode/delegate_windows.go` (`//go:build windows`)
- Create: `internal/claudecode/delegate_test.go`
- Create: `internal/claudecode/testdata/fake-claude.sh`

**Interfaces:**
- Consumes: `DelegateConfig` (Task 9).
- Produces:
  - `type DelegateRequest struct { Prompt, CWD, Model, PermissionMode string; MaxTurns int; AllowedTools []string }`
  - `type DelegateResult struct { SessionID, Text string; CostUSD float64; DurationMS, NumTurns int; IsError bool; ErrorText string }`
  - `type StreamEvent struct { Type, Text, ToolName string }`
  - `func Delegate(ctx context.Context, cfg DelegateConfig, binary string, req DelegateRequest, sessionID string, onEvent func(StreamEvent)) (DelegateResult, error)`
  - `func BuildArgs(cfg DelegateConfig, req DelegateRequest, sessionID string) []string`

- [ ] **Step 1: Create the fake claude binary**

Create `internal/claudecode/testdata/fake-claude.sh` and make it executable (`chmod +x`). It emits canned stream-json so no test ever runs a real model.

```bash
#!/bin/sh
# Stand-in for the `claude` CLI in tests. Echoes its own argv on the first
# line so argument-construction tests can assert against it, then emits a
# canned stream-json conversation.
echo "{\"type\":\"argv\",\"argv\":\"$*\"}"
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Working on it."}]}}'
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/x"}}]}}'
echo '{"type":"result","subtype":"success","result":"Done: renamed the function.","total_cost_usd":0.0123,"duration_ms":4210,"num_turns":3,"session_id":"FAKE-SESSION"}'
exit 0
```

- [ ] **Step 2: Write the failing test**

Create `internal/claudecode/delegate_test.go`:

```go
package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeClaude(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "fake-claude.sh"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return p
}

func TestBuildArgsUsesAssignedSessionID(t *testing.T) {
	cfg := DefaultConfig().Delegate
	args := BuildArgs(cfg, DelegateRequest{Prompt: "hi"}, "SID-1")
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-p", "hi",
		"--session-id SID-1",
		"--output-format stream-json",
		"--verbose",
		"--permission-mode acceptEdits",
		"--max-turns 30",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
}

func TestBuildArgsNeverSkipsPermissionsByDefault(t *testing.T) {
	cfg := DefaultConfig().Delegate
	args := BuildArgs(cfg, DelegateRequest{Prompt: "hi"}, "SID-1")
	if strings.Contains(strings.Join(args, " "), "dangerously-skip-permissions") {
		t.Fatal("default args include --dangerously-skip-permissions")
	}

	cfg.SkipPermissions = true
	args = BuildArgs(cfg, DelegateRequest{Prompt: "hi"}, "SID-1")
	if !strings.Contains(strings.Join(args, " "), "--dangerously-skip-permissions") {
		t.Error("SkipPermissions=true did not add the flag")
	}
}

func TestDelegateParsesResult(t *testing.T) {
	var events []StreamEvent
	res, err := Delegate(
		context.Background(),
		DefaultConfig().Delegate,
		fakeClaude(t),
		DelegateRequest{Prompt: "rename the function"},
		"SID-1",
		func(e StreamEvent) { events = append(events, e) },
	)
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if res.Text != "Done: renamed the function." {
		t.Errorf("Text = %q", res.Text)
	}
	if res.CostUSD != 0.0123 {
		t.Errorf("CostUSD = %v, want 0.0123", res.CostUSD)
	}
	if res.NumTurns != 3 {
		t.Errorf("NumTurns = %d, want 3", res.NumTurns)
	}
	if res.DurationMS != 4210 {
		t.Errorf("DurationMS = %d, want 4210", res.DurationMS)
	}
	if res.IsError {
		t.Error("IsError = true for a successful run")
	}

	var sawText, sawTool bool
	for _, e := range events {
		if e.Type == "text" && e.Text == "Working on it." {
			sawText = true
		}
		if e.Type == "tool_use" && e.ToolName == "Read" {
			sawTool = true
		}
	}
	if !sawText {
		t.Error("no text stream event")
	}
	if !sawTool {
		t.Error("no tool_use stream event")
	}
}

func TestDelegateMissingBinary(t *testing.T) {
	res, err := Delegate(
		context.Background(),
		DefaultConfig().Delegate,
		filepath.Join(t.TempDir(), "no-such-claude"),
		DelegateRequest{Prompt: "hi"},
		"SID-1",
		nil,
	)
	if err == nil {
		t.Fatal("Delegate with a missing binary returned nil error")
	}
	if !res.IsError {
		t.Error("IsError = false for a missing binary")
	}
}

func TestDelegateTimeoutReturnsPartialOutput(t *testing.T) {
	dir := t.TempDir()
	slow := filepath.Join(dir, "slow-claude.sh")
	script := "#!/bin/sh\n" +
		"echo '{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"starting\"}]}}'\n" +
		"sleep 30\n"
	if err := os.WriteFile(slow, []byte(script), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	start := time.Now()
	res, err := Delegate(ctx, DefaultConfig().Delegate, slow,
		DelegateRequest{Prompt: "hi"}, "SID-1", nil)
	if err == nil {
		t.Fatal("Delegate returned nil error after a timeout")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Delegate took %v after a 700ms deadline — the process was not killed", elapsed)
	}
	if !res.IsError {
		t.Error("IsError = false after a timeout")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run 'TestBuildArgs|TestDelegate' -v`
Expected: FAIL — `undefined: BuildArgs`, `undefined: Delegate`.

- [ ] **Step 4: Write the implementation**

Create `internal/claudecode/delegate.go`:

```go
package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// DelegateRequest is one delegated Claude Code task.
type DelegateRequest struct {
	Prompt         string
	CWD            string
	Model          string
	PermissionMode string
	MaxTurns       int
	AllowedTools   []string
}

// DelegateResult is the outcome of a delegated run.
type DelegateResult struct {
	SessionID  string
	Text       string
	CostUSD    float64
	DurationMS int
	NumTurns   int
	IsError    bool
	ErrorText  string
}

// StreamEvent is a live update emitted while the delegated run proceeds.
// Type is "text" or "tool_use".
type StreamEvent struct {
	Type     string
	Text     string
	ToolName string
}

// BuildArgs assembles the claude CLI arguments for a delegated run.
//
// The session id is assigned by Huginn rather than read back afterwards, so
// the transcript watcher can correlate the run to a session without racing
// the CLI.
func BuildArgs(cfg DelegateConfig, req DelegateRequest, sessionID string) []string {
	mode := req.PermissionMode
	if mode == "" {
		mode = cfg.PermissionMode
	}
	model := req.Model
	if model == "" {
		model = cfg.DefaultModel
	}
	turns := req.MaxTurns
	if turns <= 0 {
		turns = cfg.MaxTurns
	}

	args := []string{
		"-p", req.Prompt,
		"--session-id", sessionID,
		"--output-format", "stream-json",
		"--verbose",
	}
	if mode != "" {
		args = append(args, "--permission-mode", mode)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	// --max-turns is absent from `claude --help` but IS accepted by the CLI.
	// Verified empirically: this build rejects genuinely unknown flags with
	// "error: unknown option", and `--max-turns 3` was accepted and ran
	// normally. It is undocumented, not nonexistent — do not remove it, and do
	// not assume other undocumented flags behave the same way without testing.
	if turns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(turns))
	}
	if len(req.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, req.AllowedTools...)
	}
	// Only ever set from explicit configuration, never inferred.
	if cfg.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	return args
}

// Delegate runs a one-shot Claude Code task and returns its result.
//
// onEvent, if non-nil, is called for each assistant text and tool_use event as
// it streams, so callers can mirror progress into a thread panel. It is called
// from the reading goroutine and must not block for long.
//
// On timeout the process group is killed and the partial result is returned
// alongside a non-nil error.
func Delegate(
	ctx context.Context,
	cfg DelegateConfig,
	binary string,
	req DelegateRequest,
	sessionID string,
	onEvent func(StreamEvent),
) (DelegateResult, error) {
	timeout := time.Duration(cfg.TimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binary, BuildArgs(cfg, req, sessionID)...)
	if req.CWD != "" {
		cmd.Dir = req.CWD
	}
	// Put the child in its own process group so a timeout kills any tools it
	// spawned, not just the CLI itself. Platform-specific; see delegate_unix.go.
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return DelegateResult{IsError: true, ErrorText: err.Error()},
			fmt.Errorf("claudecode: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return DelegateResult{IsError: true, ErrorText: err.Error()},
			fmt.Errorf("claudecode: start %s: %w", binary, err)
	}

	res := DelegateResult{SessionID: sessionID}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		applyStreamLine(sc.Bytes(), &res, onEvent)
	}

	waitErr := cmd.Wait()
	if runCtx.Err() != nil {
		res.IsError = true
		res.ErrorText = "claude run timed out after " + timeout.String()
		return res, fmt.Errorf("claudecode: delegate timed out: %w", runCtx.Err())
	}
	if waitErr != nil {
		res.IsError = true
		if res.ErrorText == "" {
			res.ErrorText = waitErr.Error()
		}
		return res, fmt.Errorf("claudecode: delegate failed: %w", waitErr)
	}
	return res, nil
}

// streamLine is one line of claude's stream-json output.
//
// VERIFIED against the real CLI, not just the test fixture: a live
// `claude -p ... --output-format json` run emits exactly these keys —
// result, total_cost_usd, duration_ms, num_turns, subtype ("success"),
// session_id, type ("result"). Field names here match that output.
type streamLine struct {
	Type     string  `json:"type"`
	Subtype  string  `json:"subtype"`
	Result   string  `json:"result"`
	IsError  bool    `json:"is_error"`
	Cost     float64 `json:"total_cost_usd"`
	Duration int     `json:"duration_ms"`
	NumTurns int     `json:"num_turns"`
	Message  struct {
		Content []contentBlock `json:"content"`
	} `json:"message"`
}

func applyStreamLine(raw []byte, res *DelegateResult, onEvent func(StreamEvent)) {
	var sl streamLine
	if err := json.Unmarshal(raw, &sl); err != nil {
		return
	}
	// VERIFIED against a live `claude -p --output-format stream-json` run: the
	// stream also carries `system` (hook events) and `rate_limit_event` lines.
	// Both are deliberately ignored here — the default switch arm drops any
	// line type this bridge does not consume, so new line types in future CLI
	// versions cannot break a delegation.
	switch sl.Type {
	case "assistant":
		if onEvent == nil {
			return
		}
		for _, b := range sl.Message.Content {
			switch b.Type {
			case "text":
				onEvent(StreamEvent{Type: "text", Text: b.Text})
			case "tool_use":
				onEvent(StreamEvent{Type: "tool_use", ToolName: b.Name})
			}
		}
	case "result":
		res.Text = sl.Result
		res.CostUSD = sl.Cost
		res.DurationMS = sl.Duration
		res.NumTurns = sl.NumTurns
		// `is_error` is the CLI's canonical failure signal; `subtype` carries
		// the reason. Both are checked: a run can report is_error with a
		// subtype we do not recognise, and older builds may set only subtype.
		if sl.IsError || (sl.Subtype != "" && sl.Subtype != "success") {
			res.IsError = true
			res.ErrorText = sl.Subtype
			if res.ErrorText == "" {
				res.ErrorText = "claude reported is_error"
			}
		}
	}
}
```

`syscall.SysProcAttr{Setpgid: true}` is Unix-only. VERIFIED: the repo already uses this split —
`upgrade_unix.go` / `upgrade_windows.go` exist at the repo root, and `upgrade_unix.go:46` already
sets `syscall.SysProcAttr{Setsid: true}`. Follow that precedent from the START rather than
conditionally: put the two `cmd.SysProcAttr` / `cmd.Cancel` lines behind a helper
`setProcessGroup(cmd *exec.Cmd)` declared in `delegate_unix.go` (`//go:build !windows`, the
Setpgid + SIGKILL-to-process-group version) and `delegate_windows.go` (`//go:build windows`, a
no-op body relying on `exec.CommandContext`'s default kill). `delegate.go` calls
`setProcessGroup(cmd)` and imports no `syscall`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run 'TestBuildArgs|TestDelegate' -v`
Expected: PASS — all five tests.

- [ ] **Step 6: Commit**

```bash
git add internal/claudecode/delegate.go internal/claudecode/delegate_test.go \
        internal/claudecode/testdata/fake-claude.sh
git commit -m "feat(claudecode): one-shot delegation to the claude CLI"
```

---

### Task 11: The `claude_code` tool

**Files:**
- Create: `internal/tools/claude_code.go`
- Create: `internal/tools/claude_code_test.go`
- Modify: `init_tools.go`

**Interfaces:**
- Consumes: `Delegate`, `DelegateRequest`, `DelegateConfig` (Task 10); `Config` (Task 9); `tools.Tool`, `tools.ToolResult`, `tools.PermExec` (`internal/tools/types.go:27-42`).
- Produces:
  - `type ClaudeCodeTool struct` implementing `tools.Tool`
  - `func NewClaudeCodeTool(cfg claudecode.Config, defaultCWD string, onEvent func(claudecode.StreamEvent)) *ClaudeCodeTool`
  - `func RegisterClaudeCodeTool(reg *Registry, cfg claudecode.Config, cwd string, onEvent func(claudecode.StreamEvent))`

- [ ] **Step 1: Write the failing test**

Create `internal/tools/claude_code_test.go`:

```go
package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/claudecode"
)

func enabledCfg(t *testing.T) claudecode.Config {
	t.Helper()
	cfg := claudecode.DefaultConfig()
	cfg.Enabled = true
	p, err := filepath.Abs(filepath.Join("..", "claudecode", "testdata", "fake-claude.sh"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	cfg.Binary = p
	return cfg
}

func TestClaudeCodeToolMetadata(t *testing.T) {
	tool := NewClaudeCodeTool(enabledCfg(t), "/tmp", nil)
	if tool.Name() != "claude_code" {
		t.Errorf("Name() = %q, want claude_code", tool.Name())
	}
	if tool.Permission() != PermExec {
		t.Errorf("Permission() = %v, want PermExec — delegation runs arbitrary tools", tool.Permission())
	}
	if tool.Description() == "" {
		t.Error("Description() is empty")
	}
	if tool.Schema().Function.Name != "claude_code" {
		t.Errorf("Schema function name = %q", tool.Schema().Function.Name)
	}
}

func TestClaudeCodeToolExecute(t *testing.T) {
	tool := NewClaudeCodeTool(enabledCfg(t), t.TempDir(), nil)
	res := tool.Execute(context.Background(), map[string]any{
		"prompt": "rename the function",
	})
	if res.IsError {
		t.Fatalf("Execute errored: %s", res.Error)
	}
	if !strings.Contains(res.Output, "Done: renamed the function.") {
		t.Errorf("Output = %q", res.Output)
	}
	if res.Metadata["cost_usd"] != 0.0123 {
		t.Errorf("cost_usd = %v, want 0.0123", res.Metadata["cost_usd"])
	}
	if res.Metadata["num_turns"] != 3 {
		t.Errorf("num_turns = %v, want 3", res.Metadata["num_turns"])
	}
	if res.Metadata["claude_session_id"] == "" {
		t.Error("claude_session_id missing from metadata")
	}
}

func TestClaudeCodeToolRequiresPrompt(t *testing.T) {
	tool := NewClaudeCodeTool(enabledCfg(t), "/tmp", nil)
	res := tool.Execute(context.Background(), map[string]any{})
	if !res.IsError {
		t.Error("Execute with no prompt should be an error")
	}
}

func TestRegisterClaudeCodeToolRespectsConfig(t *testing.T) {
	reg := NewRegistry()
	off := claudecode.DefaultConfig() // Enabled=false
	RegisterClaudeCodeTool(reg, off, "/tmp", nil)
	if _, ok := reg.Get("claude_code"); ok {
		t.Error("claude_code registered while the bridge is disabled")
	}

	RegisterClaudeCodeTool(reg, enabledCfg(t), "/tmp", nil)
	if _, ok := reg.Get("claude_code"); !ok {
		t.Error("claude_code not registered while the bridge is enabled")
	}
}
```

Before writing the implementation, confirm the registry constructor and lookup names: `grep -n "func NewRegistry\|func (r \*Registry) Get\|func (r \*Registry) Register" internal/tools/registry.go`. Match them exactly; adjust the test if the lookup returns something other than `(Tool, bool)`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestClaudeCode -v`
Expected: FAIL — `undefined: NewClaudeCodeTool`.

- [ ] **Step 3: Write the implementation**

Create `internal/tools/claude_code.go`. Match `backend.Tool`'s actual shape first — run `grep -n -A 20 "type Tool struct" internal/backend/backend.go` and mirror how `internal/tools/bash.go` builds its `Schema()`.

```go
package tools

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/claudecode"
)

// ClaudeCodeTool delegates a one-shot task to the Claude Code CLI.
//
// It is PermExec: the delegated session can run arbitrary tools in the target
// directory, so it goes through the same approval path as bash.
type ClaudeCodeTool struct {
	cfg        claudecode.Config
	defaultCWD string
	onEvent    func(claudecode.StreamEvent)
}

// NewClaudeCodeTool returns the tool. onEvent may be nil; when set it receives
// live stream events so the caller can mirror them into a thread panel.
func NewClaudeCodeTool(cfg claudecode.Config, defaultCWD string, onEvent func(claudecode.StreamEvent)) *ClaudeCodeTool {
	return &ClaudeCodeTool{cfg: cfg, defaultCWD: defaultCWD, onEvent: onEvent}
}

func (t *ClaudeCodeTool) Name() string { return "claude_code" }

func (t *ClaudeCodeTool) Description() string {
	return "Delegate a self-contained coding task to a Claude Code session and return its result."
}

func (t *ClaudeCodeTool) Permission() PermissionLevel { return PermExec }

func (t *ClaudeCodeTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "claude_code",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"prompt"},
				Properties: map[string]backend.ToolProperty{
					"prompt":          {Type: "string", Description: "The task for Claude Code. Must be self-contained: the session has no access to this conversation."},
					"cwd":             {Type: "string", Description: "Working directory for the session. Defaults to the current workspace root."},
					"model":           {Type: "string", Description: "Model alias, e.g. opus or sonnet. Defaults to the configured model."},
					"max_turns":       {Type: "integer", Description: "Maximum agentic turns before the session stops."},
					"permission_mode": {Type: "string", Description: "One of acceptEdits, auto, manual, plan, dontAsk. Defaults to acceptEdits."},
				},
			},
		},
	}
}

func (t *ClaudeCodeTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return ToolResult{
			IsError: true,
			Error:   "claude_code: prompt is required and must be a non-empty string",
		}
	}

	req := claudecode.DelegateRequest{
		Prompt:         prompt,
		CWD:            stringArg(args, "cwd", t.defaultCWD),
		Model:          stringArg(args, "model", ""),
		PermissionMode: stringArg(args, "permission_mode", ""),
		MaxTurns:       intArgOr(args, "max_turns", 0),
	}

	sessionID := uuid.NewString()
	res, err := claudecode.Delegate(ctx, t.cfg.Delegate, t.cfg.Binary, req, sessionID, t.onEvent)

	out := ToolResult{
		Output: res.Text,
		Metadata: map[string]any{
			"claude_session_id": res.SessionID,
			"cost_usd":          res.CostUSD,
			"duration_ms":       res.DurationMS,
			"num_turns":         res.NumTurns,
		},
	}
	if err != nil {
		out.IsError = true
		out.Error = fmt.Sprintf("claude_code: %v", err)
		if res.ErrorText != "" {
			out.Error = fmt.Sprintf("claude_code: %s (%v)", res.ErrorText, err)
		}
	}
	return out
}

// RegisterClaudeCodeTool registers the tool only when delegation is enabled.
func RegisterClaudeCodeTool(reg *Registry, cfg claudecode.Config, cwd string, onEvent func(claudecode.StreamEvent)) {
	if !cfg.DelegateEnabled() {
		return
	}
	reg.Register(NewClaudeCodeTool(cfg, cwd, onEvent))
}

func stringArg(args map[string]any, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// intArgOr is named to avoid colliding with the package's existing
// intArg(args, key) (int, bool) in github.go.
func intArgOr(args map[string]any, key string, fallback int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64: // JSON numbers arrive as float64
		return int(v)
	}
	return fallback
}
```

VERIFIED: `intArg` ALREADY EXISTS at `internal/tools/github.go:364` as
`func intArg(args map[string]any, key string) (int, bool)` — a different signature in the same
package, so redeclaring it is a hard compile error. That is why the helper above is named
`intArgOr`. `stringArg` does NOT exist and is safe to add as written.

VERIFIED: `backend.ToolFunction.Parameters` is the typed `backend.ToolParameters`
(`internal/backend/backend.go:67-78`), NOT `map[string]any`. `ToolProperty` carries only
`{Type, Description string}` — no nested schema. `internal/tools/bash.go:34-50` is the pattern
to follow. `github.com/google/uuid` is already a direct dependency (`go.mod`, v1.6.0) — no `go get` needed.

- [ ] **Step 4: Register the tool at startup**

In `init_tools.go`, after the existing `tools.Register*` calls (around line 72), add:

```go
	tools.RegisterClaudeCodeTool(toolReg, cfg.ClaudeCode, cwd, nil)
```

using whatever variable already holds the loaded config in that function. `onEvent` is `nil` here; Task 12 wires the streaming callback once the server is available.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tools/ -run TestClaudeCode -v`
Expected: PASS — all four tests.

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/claude_code.go internal/tools/claude_code_test.go init_tools.go
git commit -m "feat(tools): claude_code delegation tool"
```

---

### Task 12: Server wiring and HTTP endpoints

**Files:**
- Create: `internal/server/handlers_claude.go`
- Create: `internal/server/handlers_claude_test.go`
- Modify: `internal/server/server.go:820` area (route registration)

**Interfaces:**
- Consumes: `Ingester`, `Backfill`, `Watcher`, `Config`, `DefaultRoot` (Tasks 6-9); `(*Server).BroadcastToSession` (`internal/server/server.go:467`).
- Produces:
  - `GET /api/v1/claude/status` → `{"enabled":bool,"watching":bool,"root":string,"sessions":int}`
  - `POST /api/v1/claude/backfill` → `{"files":int,"messages":int,"skipped":int,"failed":int}`
    (`skipped` = deliberately not imported for exceeding max_file_mb; `failed` = could not be
    read at all. They are separate on purpose — a user needs to tell those outcomes apart.)
  - `func (s *Server) StartClaudeBridge(ctx context.Context, cfg claudecode.Config, db *sqlitedb.DB) error`

- [ ] **Step 1: Write the failing test**

Create `internal/server/handlers_claude_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/claudecode"
)

func TestClaudeStatusReportsDisabled(t *testing.T) {
	s := &Server{claudeCfg: claudecode.DefaultConfig()}

	rec := httptest.NewRecorder()
	s.handleClaudeStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/claude/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Enabled  bool   `json:"enabled"`
		Watching bool   `json:"watching"`
		Root     string `json:"root"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enabled {
		t.Error("enabled = true for the default config")
	}
	if body.Watching {
		t.Error("watching = true while disabled")
	}
}

func TestClaudeBackfillRefusesWhenDisabled(t *testing.T) {
	s := &Server{claudeCfg: claudecode.DefaultConfig()}

	rec := httptest.NewRecorder()
	s.handleClaudeBackfill(rec, httptest.NewRequest(http.MethodPost, "/api/v1/claude/backfill", nil))

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 when the bridge is disabled", rec.Code)
	}
}
```

ALSO add these. The two tests above BOTH exercise only the disabled path — a pair of handlers
that unconditionally reported "disabled" would pass them, and nothing would prove the routes are
even reachable:

```go
func TestClaudeStatusReportsEnabledState(t *testing.T) {
	cfg := claudecode.DefaultConfig()
	cfg.Enabled = true
	s := &Server{claudeCfg: cfg, claudeWatching: true, claudeRoot: "/tmp/projects"}

	rec := httptest.NewRecorder()
	s.handleClaudeStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/claude/status", nil))

	var body struct {
		Enabled  bool   `json:"enabled"`
		Watching bool   `json:"watching"`
		Root     string `json:"root"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Enabled || !body.Watching {
		t.Errorf("enabled=%v watching=%v, want both true", body.Enabled, body.Watching)
	}
	if body.Root != "/tmp/projects" {
		t.Errorf("root = %q, want /tmp/projects", body.Root)
	}
}

func TestClaudeRoutesAreRegistered(t *testing.T) {
	s := &Server{claudeCfg: claudecode.DefaultConfig()}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/claude/status"},
		{http.MethodPost, "/api/v1/claude/backfill"},
	} {
		h, pattern := mux.Handler(httptest.NewRequest(tc.method, tc.path, nil))
		if h == nil || pattern == "" {
			t.Errorf("%s %s is not routed (pattern=%q)", tc.method, tc.path, pattern)
		}
	}
}
```

IF `registerRoutes` panics on a minimally-constructed `&Server{}` (it wires many subsystems),
do NOT force it with elaborate fakes and do NOT delete the test. Report that in your report and
leave `TestClaudeRoutesAreRegistered` out, keeping the other three. I would rather know the
route table is untestable in isolation than have a test that lies.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestClaude -v`
Expected: FAIL — `s.claudeCfg undefined`, `s.handleClaudeStatus undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/server/handlers_claude.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/scrypster/huginn/internal/claudecode"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// StartClaudeBridge starts transcript ingestion if the bridge is enabled.
// It returns immediately; the watcher runs until ctx is cancelled. A failure
// to start is logged and swallowed — the bridge is never allowed to prevent
// the server from serving.
func (s *Server) StartClaudeBridge(ctx context.Context, cfg claudecode.Config, db *sqlitedb.DB) error {
	s.claudeCfg = cfg
	if !cfg.WatchEnabled() {
		slog.Info("claudecode: bridge disabled")
		return nil
	}
	if db == nil {
		slog.Warn("claudecode: no database, bridge disabled")
		return nil
	}

	root := claudecode.DefaultRoot()
	if root == "" {
		slog.Warn("claudecode: cannot determine home directory, bridge disabled")
		return nil
	}

	sink, ok := s.store.(claudecode.SessionSink)
	if !ok {
		slog.Warn("claudecode: session store does not support external sessions, bridge disabled")
		return nil
	}

	ing := claudecode.NewIngester(sink, claudecode.NewIngestStore(db), s)
	s.claudeIngester = ing
	s.claudeRoot = root

	if cfg.Watch.Backfill {
		go func() {
			res, err := claudecode.Backfill(ctx, root, ing, cfg.Watch.MaxFileMB)
			if err != nil {
				slog.Warn("claudecode: backfill failed", "err", err)
				return
			}
			slog.Info("claudecode: backfill complete",
				"files", res.Files, "messages", res.Messages, "skipped", res.Skipped)
		}()
	}

	go func() {
		if err := claudecode.NewWatcher(root, ing).Run(ctx); err != nil {
			slog.Warn("claudecode: watcher stopped", "err", err)
		}
	}()

	s.claudeWatching = true
	slog.Info("claudecode: bridge started", "root", root)
	return nil
}

func (s *Server) handleClaudeStatus(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{
		"enabled":  s.claudeCfg.Enabled,
		"watching": s.claudeWatching,
		"root":     s.claudeRoot,
	})
}

func (s *Server) handleClaudeBackfill(w http.ResponseWriter, r *http.Request) {
	if !s.claudeCfg.WatchEnabled() || s.claudeIngester == nil {
		jsonError(w, http.StatusConflict,
			"claude code bridge is disabled; set claude_code.enabled in config.json")
		return
	}

	res, err := claudecode.Backfill(r.Context(), s.claudeRoot, s.claudeIngester, s.claudeCfg.Watch.MaxFileMB)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"files": res.Files, "messages": res.Messages,
		"skipped": res.Skipped, "failed": res.Failed,
	})
}

var _ = json.Marshal // remove if unused
```

Delete the trailing `var _ = json.Marshal` and the `encoding/json` import if the compiler flags them.

The response helpers are `jsonOK(w http.ResponseWriter, v any)` and `jsonError(w http.ResponseWriter, code int, msg string)`, both already in the package — see `handlers_muninn.go` for the idiom. There is no `writeJSON`.

- [ ] **Step 4: Add the Server fields**

In `internal/server/server.go`, add to `type Server struct`:

```go
	claudeCfg      claudecode.Config
	claudeIngester *claudecode.Ingester
	claudeRoot     string
	claudeWatching bool
```

Import `"github.com/scrypster/huginn/internal/claudecode"`.

`*Server` already has `BroadcastToSession(sessionID, msgType string, payload map[string]any)` at `server.go:467`, which satisfies `claudecode.Broadcaster` with no adapter.

- [ ] **Step 5: Register the routes**

In `server.go`, alongside the other `mux.HandleFunc` calls, following the same auth-wrapping pattern used by neighbouring routes:

VERIFIED: routes are registered in `func (s *Server) registerRoutes(mux *http.ServeMux)` at
`internal/server/server.go:805`, which defines a local helper `api(h http.HandlerFunc)` (line
818) composing `loggingMiddleware(requestIDMiddleware(s.authMiddleware(...)))` plus a 10 MiB
body cap. Do NOT hand-compose the middleware — use `api`, exactly as the neighbouring
`muninn` routes do at `server.go:977` and `:979`. Add beside them:

```go
	mux.HandleFunc("GET /api/v1/claude/status",    api(s.handleClaudeStatus))
	mux.HandleFunc("POST /api/v1/claude/backfill", api(s.handleClaudeBackfill))
```

- [ ] **Step 6: Call StartClaudeBridge at server startup**

VERIFIED: there is an exact precedent for handing the server a `*sqlitedb.DB` after
construction — `func (s *Server) StartAuditLog(db *sqlitedb.DB)` at `internal/server/server.go:680`,
called from `main.go:2846` as `srv.StartAuditLog(sqlDB)`. Follow it: call `StartClaudeBridge`
from `main.go` immediately after that line, reusing the same `sqlDB` variable and the `ctx`
in scope there.

```go
	srv.StartAuditLog(sqlDB)
	if err := srv.StartClaudeBridge(ctx, cfg.ClaudeCode, sqlDB); err != nil {
		slog.Warn("claudecode: bridge did not start", "err", err)
	}
```

Use whichever variable at that point in `main.go` holds the loaded `config.Config`. Do NOT add
the call inside `(*Server).Start` — the DB is not in scope there.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/server/ -run TestClaude -v`
Expected: PASS — both tests.

Run: `make test`
Expected: the full suite passes.

- [ ] **Step 8: Commit**

```bash
git add internal/server/handlers_claude.go internal/server/handlers_claude_test.go internal/server/server.go
git commit -m "feat(server): start the Claude Code bridge and expose status/backfill"
```

---

### Task 13: Web UI badge for bridged sessions

**Files:**
- Modify: `web/src/composables/useSessions.ts` (the `Session` interface, line ~10)
- Modify: `web/src/views/SessionsView.vue` (the session list item)

**Interfaces:**
- Consumes: `external_kind` on the session manifest JSON (Task 1).
- Produces: no Go interface.

VERIFIED FOR YOU (do not re-derive): the frontend is Vue 3 + Tailwind. The session list lives in
`web/src/views/SessionsView.vue`, which does `v-for="sess in sessions"` and renders a title, a
date, a `model: {{ sess.model }}` line, and a JSON dump. Its type is `interface Session` in
`web/src/composables/useSessions.ts:10`. Styling uses Tailwind tokens already in the file:
`text-huginn-muted`, `text-huginn-blue`, `text-xs`, `border-huginn-border`.

There is NO existing badge keyed on `source === 'routine'` anywhere in `web/src` — do not go
looking for one to copy. Match the classes already used in `SessionsView.vue` instead.

- [ ] **Step 1: Confirm the field reaches the frontend — WITHOUT starting a server**

DO NOT run `go run . serve`, `huginn serve`, `huginn tray`, or any other command that starts a
real Huginn process. Earlier today a repo-wide test run overwrote this developer's real
`~/.huginn/config.json`, and a `huginn relay` process is currently live on this machine.
Starting another server risks writing to their real `~/.huginn` and colliding with it. There is
nothing here that requires a running server.

Verify statically instead — the question is only whether `external_kind` survives JSON
marshalling of the session manifest:

```bash
grep -n "external_kind" internal/session/types.go
go test ./internal/session/ -run TestSaveManifestRoundTripsExternalFields -v
```

Expected: `Manifest.ExternalKind` carries the tag `json:"external_kind,omitempty"`, and the
round-trip test passes. Because the field is `omitempty`, a native Huginn session omits the key
entirely and a bridged one emits `"external_kind":"claude-code"` — which is exactly what the
badge's `v-if` tests for.

If you want stronger proof, add a tiny assertion to an EXISTING session test that marshals a
`Manifest` with `ExternalKind` set and checks the JSON contains `"external_kind":"claude-code"`.
Do not stand up a server to answer a serialisation question.

- [ ] **Step 2: Add the field to the Session interface**

In `web/src/composables/useSessions.ts`, add to `interface Session`:

```ts
  external_kind?: string
```

Optional, because native Huginn sessions omit it (the Go side has `omitempty`).

- [ ] **Step 3: Render the badge**

In `web/src/views/SessionsView.vue`, beside the existing `model:` line, add:

```html
<span
  v-if="sess.external_kind === 'claude-code'"
  class="text-huginn-blue text-xs border border-huginn-border rounded px-1 ml-2"
>claude code</span>
```

Place it so it reads as part of the session's metadata row, consistent with the surrounding
markup. Do not restructure the list item or introduce new CSS files.

- [ ] **Step 4: Type-check only — do NOT run `make build-frontend`**

`make build-frontend` runs `cd web && pnpm install && pnpm build`. The developer has UNCOMMITTED
changes to `web/package.json` (adding `@tiptap/core` and `@tiptap/suggestion`) and two UNTRACKED
lockfiles (`pnpm-lock.yaml`, `web/pnpm-lock.yaml`). `pnpm install` would regenerate that lockfile
and install against their work-in-progress dependency changes. Do not run it, and do not run
`pnpm install`, `npm install` or `pnpm build` by any other route.

`web/node_modules` is already present (189M) with `vue-tsc` and `vite` installed, so you can
type-check without installing anything:

```bash
cd web && ./node_modules/.bin/vue-tsc --noEmit 2>&1 | head -30; cd ..
```

Expected: no NEW errors mentioning `external_kind`, `Session`, or `SessionsView`. This codebase
may already emit unrelated type errors — only errors traceable to YOUR two edits matter. If you
see pre-existing noise, say so in your report and name which errors you judged pre-existing.

Do not treat a non-zero exit as failure by itself; read the actual messages.

DO NOT start a server for a visual check. The badge is a single `v-if` on a string comparison;
its correctness is readable from the template. If `web/` has a component test setup (there is a
`vitest.config.ts` at `web/vitest.config.ts`), a small test asserting the badge renders for
`external_kind: 'claude-code'` and not otherwise is welcome but optional — say in your report
whether you added one.

Leave BOTH the production build and the visual confirmation to the developer — building requires
`pnpm install`, which is theirs to run against their own in-progress dependency changes. Note in
your report that a human should build and eyeball it, and that their `web_ui.port` is 9200 so
they know where to look.

- [ ] **Step 5: Commit — EXPLICIT PATHS ONLY**

The working tree contains unrelated uncommitted work belonging to the developer:
`web/package.json` (modified), `pnpm-lock.yaml` and `web/pnpm-lock.yaml` (untracked).
`git add web/` WOULD SWEEP ALL OF IT onto this branch. Never do that. Add exactly the two files
you edited:

```bash
git add web/src/composables/useSessions.ts web/src/views/SessionsView.vue
git commit -m "feat(web): badge sessions bridged from Claude Code"
```

Then run `git status --porcelain` and confirm `web/package.json` and both lockfiles are still
listed as uncommitted. If they are not, you have committed the developer's work — say so
immediately in your report.

### Task 14: Documentation and spec amendment

**Files:**
- Create: `docs/features/claude-code-bridge.md`
- Modify: `docs/planning/2026-08-25-claude-code-bridge-design.md`
- Modify: `docs/index.md` (if it indexes the features directory)

**Interfaces:**
- Consumes: everything.
- Produces: no code.

- [ ] **Step 1: Amend the spec — SIX divergences, not one**

The spec (`docs/planning/2026-08-25-claude-code-bridge-design.md`) was written before
implementation and is now wrong in six places. A spec that misdescribes the code is worse than
no spec, because the next reader trusts it. Fix ALL of these:

**(a) Session identity — line ~91 and the table at ~95-98.** The spec specifies
`Source: "claude-code"`. The implementation uses NEW `external_kind` / `external_id` columns and
leaves `source` untouched, because `sessions` declares
`CHECK (source IN ('', 'routine'))` and SQLite cannot drop a CHECK without rebuilding the table —
which here would mean rebuilding under two inbound foreign keys with `PRAGMA foreign_keys = ON`,
inside the transaction that wraps every migration. Replace the mechanism, and in the identity
table delete the `Source` row and add:

```
| `ExternalKind`  | literal `"claude-code"`                             |
| `ExternalID`    | transcript `sessionId`                              |
```

**(b) The unique index is NOT in the base schema — add this, it is not currently stated at all.**
`uq_sessions_external` is created by `migrateSessionsExternalColumnsV1`, deliberately NOT by the
base schema DDL, because `ApplySchema()` runs BEFORE `Migrate()`: on an existing database the
`CREATE TABLE IF NOT EXISTS` is a no-op, so an index there would reference
`external_kind`/`external_id` before the ALTER TABLE had added them and every existing install
would fail to start with `no such column: external_kind`. This actually happened during
implementation. Document it in the Session identity section.

**(c) Delegation does NOT stream into the thread panel — line ~229.** The spec says each
assistant and tool_use event "is emitted as a thread reply on the delegating message, in-process,
so the thread panel streams live." That is not implemented: the `claude_code` tool is registered
with a nil `onEvent` callback. What IS true is that a delegated run is launched with an assigned
`--session-id`, so the watcher ingests its transcript and it appears as a full session. Rewrite
that step to say exactly that, and note the thread-panel streaming as deliberate future work.

**(d) `BackfillResult` gained a fourth field.** The spec describes
`BackfillResult{Files, Messages, Skipped}`. It is now `{Files, Messages, Skipped, Failed}`.
`Skipped` means "deliberately not imported, exceeded max_file_mb"; `Failed` means "could not be
read at all". They are separate on purpose — a user must be able to tell those outcomes apart.
Update the Backfill section and the `POST /api/v1/claude/backfill` response shape.

**(e) The frontend file is wrong — line ~75.** The architecture block says
`internal/server/dist   sidebar badge for source="claude-code"`. The actual edits are
`web/src/views/SessionsView.vue` and `web/src/composables/useSessions.ts`; `internal/server/dist`
is gitignored build output. Correct it.

**(f) Add a "What was not built" note** to the Out of scope section: live thread-panel streaming
of delegated runs (see (c)), and the phase-2 hooks, which remain specified and deliberately
unimplemented.

Do NOT rewrite the spec wholesale. Make these six targeted edits and leave the rest — the
approach comparison, the failure-mode table and the testing section are all still accurate.

- [ ] **Step 2: Write the feature doc**

Create `docs/features/claude-code-bridge.md`.

VERIFIED house style, from `docs/features/routines.md` — follow it exactly:
- `# Title` (plain feature name, no "Guide"/"Reference" suffix)
- `## What it is` — two or three paragraphs of prose, no bullet dump
- a `---` rule between top-level sections
- `## How to use it` with `###` subsections
- fenced config blocks in the real format (JSON here, since this feature is configured in
  `~/.huginn/config.json`, not YAML)

Content it must cover:
- what the bridge does, in both directions (watching every Claude Code session; delegating a
  one-shot task via the `claude_code` tool)
- that it is **opt-in**: `claude_code.enabled` defaults to `false`, and that enabling it means
  Huginn reads **every** Claude Code transcript on the machine. Say that plainly — it is the
  fact a user most needs before switching it on.
- the full config block with its defaults
- the `claude_code` tool and each of its arguments
- `GET /api/v1/claude/status` and `POST /api/v1/claude/backfill`
- that Huginn writes **nothing** into `~/.claude/settings.json` and installs no Claude Code
  hooks, so a stopped or broken Huginn cannot affect a Claude Code session
- the known limitation: a delegated run appears as a full session (the watcher ingests its
  transcript) but does **not** stream into the thread panel, because the tool is registered with
  a nil stream callback

Do NOT document the phase-2 hooks as if they exist. They are specified in the design doc and
deliberately not built.

- [ ] **Step 2b: Add a warning comment to the schema file**

A future reader diffing `internal/sqlitedb/schema/huginn-sqlite-schema.sql` against
`internal/session/migrations.go` will notice the migration creates a `uq_sessions_external`
index that the base schema does not, and may "fix" the apparent omission by adding it back.
Doing so reintroduces a Critical bug: `ApplySchema()` runs BEFORE `Migrate()`, so on an existing
database the index would reference `external_kind`/`external_id` before the ALTER TABLE has
added them, and every existing install would fail to start.

Add this comment in BOTH schema copies, immediately after the `sessions` table definition, then
re-sync them so they stay byte-identical:

```sql
-- NOTE: the uq_sessions_external index is deliberately NOT defined here. It is
-- created by migrateSessionsExternalColumnsV1 instead, because ApplySchema()
-- runs BEFORE Migrate(): on an existing database this CREATE TABLE is a no-op,
-- so an index here would reference external_kind/external_id before the
-- ALTER TABLE has added them and startup would fail. Do not move it back.
```

Verify `diff docs/schema/huginn-sqlite-schema.sql internal/sqlitedb/schema/huginn-sqlite-schema.sql`
prints nothing, and that `go test ./internal/sqlitedb/ -run TestSchemaSyncWithDocs` passes.

- [ ] **Step 3: Add it to the docs index**

VERIFIED: `docs/index.md` indexes the features directory with one line per page in this exact
format (see lines 11-34):

```
→ [Routines](features/routines.md) — YAML Routines, cron scheduling, Inbox
```

Add a matching entry, placed near the Sessions and Memory lines since it is session-related:

```
→ [Claude Code Bridge](features/claude-code-bridge.md) — watch Claude Code sessions live, delegate tasks to `claude -p`
```

VERIFIED: there is no docs target in the Makefile and no docs build to run — adding the index
line is the whole of this step.

- [ ] **Step 4: Full verification**

```bash
make test
go test -race ./internal/claudecode/ ./internal/session/ ./internal/tools/
go build ./...
gofmt -l internal/claudecode internal/tools internal/server internal/session
```

Expected: tests pass, no race warnings, build clean, `gofmt -l` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs: Claude Code bridge feature guide and spec amendment"
```

---

## Verification checklist

Before considering the plan complete, confirm each spec requirement has a task:

| Spec requirement | Task |
|---|---|
| Session identity via external id | 1 |
| `claude_ingest` offset table | 1, 5 |
| Line filtering allowlist | 2 |
| Tool call state machine | 3 |
| Sidechain to thread routing | 3, 6 |
| Usage and model capture | 3 |
| Partial trailing line rule | 4 |
| Truncation reset and re-sync | 4 |
| Live WebSocket push | 6, 12 |
| Workspace root from `cwd` | 6 |
| fsnotify watcher | 7 |
| Backfill with bounded concurrency | 8 |
| Oversized files skipped, logged | 8 |
| Opt-in config, default off | 9 |
| Delegation via `claude -p` | 10 |
| Assigned `--session-id` | 10 |
| Never skip permissions by default | 10 |
| Timeout kills the process group | 10 |
| `claude_code` tool at PermExec | 11 |
| Status and backfill endpoints | 12 |
| Sidebar badge | 13 |
| Feature documentation | 14 |

**Cost computed via `internal/pricing`:** the spec calls for this. Tasks 3 and 6 record `PromptTok`, `CompTok` and `ModelName` on every message but do not compute `CostUSD`. Huginn's existing cost path derives spend from those fields, so wiring `internal/pricing` into the mapper would double-count. If, after Task 12, bridged sessions show no cost in the UI, add a follow-up task calling the pricing package from `Ingester.IngestFile` — do not add it speculatively.
