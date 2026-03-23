package integration_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/workforce"
)

// openGranularDB opens a fresh SQLite DB with full schema and workstream
// migrations applied. Returns the DB; the caller must NOT close it directly —
// a t.Cleanup is registered.
func openGranularDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "granular.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("Migrate workstreams: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertGranularSession pre-populates a sessions row so artifact foreign-key
// constraints are satisfied. Uses INSERT OR IGNORE so it is safe to call
// multiple times with the same ID.
func insertGranularSession(t *testing.T, db *sqlitedb.DB, sessionID string) {
	t.Helper()
	if _, err := db.Write().Exec(
		`INSERT OR IGNORE INTO sessions (id) VALUES (?)`, sessionID,
	); err != nil {
		t.Fatalf("insertGranularSession %s: %v", sessionID, err)
	}
}

// ─── 1. ArtifactStore Write and Read ─────────────────────────────────────────

// TestIntegration_ArtifactStore_WriteAndRead opens a DB, writes an artifact,
// reads it back, and verifies every field is correctly round-tripped.
func TestIntegration_ArtifactStore_WriteAndRead(t *testing.T) {
	ctx := context.Background()
	db := openGranularDB(t)
	dir := t.TempDir()
	store := artifact.NewStore(db.Write(), dir)

	const sessID = "art-read-sess"
	insertGranularSession(t, db, sessID)

	a := &workforce.Artifact{
		Kind:      workforce.KindDocument,
		Title:     "write-and-read test artifact",
		MimeType:  "text/plain",
		AgentName: "test-agent",
		SessionID: sessID,
		Content:   []byte("hello integration test"),
	}
	if err := store.Write(ctx, a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if a.ID == "" {
		t.Fatal("Write did not set artifact ID")
	}

	got, err := store.Read(ctx, a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.ID != a.ID {
		t.Errorf("ID mismatch: want %q got %q", a.ID, got.ID)
	}
	if got.Title != a.Title {
		t.Errorf("Title mismatch: want %q got %q", a.Title, got.Title)
	}
	if got.Kind != a.Kind {
		t.Errorf("Kind mismatch: want %q got %q", a.Kind, got.Kind)
	}
	if got.AgentName != a.AgentName {
		t.Errorf("AgentName mismatch: want %q got %q", a.AgentName, got.AgentName)
	}
	if got.SessionID != a.SessionID {
		t.Errorf("SessionID mismatch: want %q got %q", a.SessionID, got.SessionID)
	}
	if string(got.Content) != string(a.Content) {
		t.Errorf("Content mismatch: want %q got %q", a.Content, got.Content)
	}
	if got.Status != workforce.StatusDraft {
		t.Errorf("expected initial status draft, got %q", got.Status)
	}
}

// ─── 2. ArtifactStore Status Lifecycle ───────────────────────────────────────

// TestIntegration_ArtifactStore_StatusLifecycle exercises the
// draft → accepted → superseded chain through UpdateStatus and Supersede.
func TestIntegration_ArtifactStore_StatusLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openGranularDB(t)
	dir := t.TempDir()
	store := artifact.NewStore(db.Write(), dir)

	const sessID = "art-lifecycle-sess"
	insertGranularSession(t, db, sessID)

	writeArtifact := func(title string) *workforce.Artifact {
		a := &workforce.Artifact{
			Kind:      workforce.KindDocument,
			Title:     title,
			AgentName: "lifecycle-agent",
			SessionID: sessID,
			Content:   []byte(title),
		}
		if err := store.Write(ctx, a); err != nil {
			t.Fatalf("Write %q: %v", title, err)
		}
		return a
	}

	// draft → accepted
	a1 := writeArtifact("v1 artifact")
	if err := store.UpdateStatus(ctx, a1.ID, workforce.StatusAccepted, ""); err != nil {
		t.Fatalf("UpdateStatus accepted: %v", err)
	}
	got1, _ := store.Read(ctx, a1.ID)
	if got1.Status != workforce.StatusAccepted {
		t.Errorf("expected accepted, got %q", got1.Status)
	}

	// accepted → superseded via Supersede
	a2 := writeArtifact("v2 artifact")
	if err := store.Supersede(ctx, a1.ID, a2.ID); err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	superseded, _ := store.Read(ctx, a1.ID)
	if superseded.Status != workforce.StatusSuperseded {
		t.Errorf("expected superseded, got %q", superseded.Status)
	}
	current, _ := store.Read(ctx, a2.ID)
	if current.Status != workforce.StatusDraft {
		// v2 was just written; Supersede marks v1, v2 stays draft
		t.Logf("v2 status: %q (expected draft)", current.Status)
	}
}

// ─── 3. WorkstreamStore CRUD ──────────────────────────────────────────────────

// TestIntegration_WorkstreamStore_CRUD exercises create, get, list, and delete.
func TestIntegration_WorkstreamStore_CRUD(t *testing.T) {
	ctx := context.Background()
	db := openGranularDB(t)
	store := spaces.NewWorkstreamStore(db)

	// Create
	ws, err := store.Create(ctx, "crud-workstream", "CRUD integration test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.ID == "" {
		t.Fatal("Create did not assign an ID")
	}
	if ws.Name != "crud-workstream" {
		t.Errorf("Name mismatch: want %q got %q", "crud-workstream", ws.Name)
	}
	if ws.Description != "CRUD integration test" {
		t.Errorf("Description mismatch: want %q got %q", "CRUD integration test", ws.Description)
	}

	// Get
	fetched, err := store.Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.ID != ws.ID {
		t.Errorf("Get returned wrong ID: want %q got %q", ws.ID, fetched.ID)
	}

	// List
	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("List returned empty, expected at least 1")
	}
	found := false
	for _, w := range list {
		if w.ID == ws.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("workstream %s not found in List output", ws.ID)
	}

	// Delete
	if err := store.Delete(ctx, ws.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, ws.ID)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

// ─── 4. WorkstreamStore Session Tagging ──────────────────────────────────────

// TestIntegration_WorkstreamStore_SessionTagging tags 3 sessions to a
// workstream, lists them, and verifies all 3 are present.
func TestIntegration_WorkstreamStore_SessionTagging(t *testing.T) {
	ctx := context.Background()
	db := openGranularDB(t)
	store := spaces.NewWorkstreamStore(db)

	ws, err := store.Create(ctx, "tag-workstream", "")
	if err != nil {
		t.Fatalf("Create workstream: %v", err)
	}

	sessionIDs := []string{"tag-sess-A", "tag-sess-B", "tag-sess-C"}
	for _, sid := range sessionIDs {
		if err := store.TagSession(ctx, ws.ID, sid); err != nil {
			t.Fatalf("TagSession %s: %v", sid, err)
		}
	}

	tagged, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(tagged) != 3 {
		t.Fatalf("expected 3 tagged sessions, got %d", len(tagged))
	}
	set := make(map[string]bool, len(tagged))
	for _, sid := range tagged {
		set[sid] = true
	}
	for _, sid := range sessionIDs {
		if !set[sid] {
			t.Errorf("session %s missing from tagged list", sid)
		}
	}
}

// ─── 5. Artifact + Workstream Cross-Package ───────────────────────────────────

// TestIntegration_ArtifactAndWorkstream_CrossPackage creates a workstream,
// tags a session to it, writes an artifact for that session, and verifies both
// stores see consistent data via their respective query APIs.
func TestIntegration_ArtifactAndWorkstream_CrossPackage(t *testing.T) {
	ctx := context.Background()
	db := openGranularDB(t)
	wsStore := spaces.NewWorkstreamStore(db)
	artStore := artifact.NewStore(db.Write(), t.TempDir())

	// Create a workstream.
	ws, err := wsStore.Create(ctx, "cross-package-ws", "cross-package test")
	if err != nil {
		t.Fatalf("Create workstream: %v", err)
	}

	// Insert session and tag it.
	const sessID = "cross-pkg-sess"
	insertGranularSession(t, db, sessID)
	if err := wsStore.TagSession(ctx, ws.ID, sessID); err != nil {
		t.Fatalf("TagSession: %v", err)
	}

	// Write an artifact for the session.
	a := &workforce.Artifact{
		Kind:      workforce.KindStructuredData,
		Title:     "cross-package artifact",
		AgentName: "cross-agent",
		SessionID: sessID,
		Content:   []byte(`{"cross":true}`),
	}
	if err := artStore.Write(ctx, a); err != nil {
		t.Fatalf("Write artifact: %v", err)
	}

	// Workstream store: session is tagged.
	sessions, err := wsStore.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0] != sessID {
		t.Errorf("workstream session list mismatch: %v", sessions)
	}

	// Artifact store: artifact is queryable by session.
	arts, err := artStore.ListBySession(ctx, sessID, 0, "")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].ID != a.ID {
		t.Errorf("artifact ID mismatch: want %q got %q", a.ID, arts[0].ID)
	}
}

// ─── 6. SQLiteSchema All Tables Exist ────────────────────────────────────────

// TestIntegration_SQLiteSchema_AllTablesExist opens a fresh DB, applies the
// schema, and verifies all expected tables are present via sqlite_master.
func TestIntegration_SQLiteSchema_AllTablesExist(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "schema_tables.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	expectedTables := []string{
		"sessions",
		"messages",
		"threads",
		"thread_deps",
		"routines",
		"routine_runs",
		"artifacts",
		"workstreams",
		"workstream_sessions",
		"connections",
		"notifications",
		"_migrations",
	}

	rows, err := db.Read().Query(
		`SELECT name FROM sqlite_master WHERE type = 'table'`,
	)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("sqlite_master rows: %v", err)
	}

	for _, tbl := range expectedTables {
		if !existing[tbl] {
			t.Errorf("expected table %q not found in schema", tbl)
		}
	}
}

// ─── 7. SQLiteSchema Foreign Keys Enabled ────────────────────────────────────

// TestIntegration_SQLiteSchema_ForeignKeys_Enabled verifies that the DB
// connection has foreign_keys = ON (as required by the schema conventions).
func TestIntegration_SQLiteSchema_ForeignKeys_Enabled(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "fk_check.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var fkEnabled int
	if err := db.Read().QueryRow(`PRAGMA foreign_keys`).Scan(&fkEnabled); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("expected foreign_keys = 1, got %d", fkEnabled)
	}
}

// ─── 8. ArtifactStore Archive Old Artifacts ──────────────────────────────────

// TestIntegration_ArtifactStore_ArchiveOldArtifacts writes terminal-state
// artifacts, back-dates them beyond the cutoff, calls Archive, and verifies
// they are removed while active artifacts remain.
func TestIntegration_ArtifactStore_ArchiveOldArtifacts(t *testing.T) {
	ctx := context.Background()
	db := openGranularDB(t)
	dir := t.TempDir()
	store := artifact.NewStore(db.Write(), dir)

	const sessID = "archive-granular-sess"
	insertGranularSession(t, db, sessID)

	terminalStatuses := []workforce.ArtifactStatus{
		workforce.StatusRejected,
		workforce.StatusFailed,
		workforce.StatusSuperseded,
	}
	terminalIDs := make([]string, len(terminalStatuses))

	for i, status := range terminalStatuses {
		a := &workforce.Artifact{
			Kind:      workforce.KindDocument,
			Title:     fmt.Sprintf("terminal %d", i),
			AgentName: "archive-agent",
			SessionID: sessID,
			Content:   []byte("terminal"),
		}
		if err := store.Write(ctx, a); err != nil {
			t.Fatalf("Write terminal %d: %v", i, err)
		}
		if err := store.UpdateStatus(ctx, a.ID, status, "archive test"); err != nil {
			t.Fatalf("UpdateStatus %d: %v", i, err)
		}
		terminalIDs[i] = a.ID
	}

	// Write one active draft that must NOT be archived.
	activeDraft := &workforce.Artifact{
		Kind:      workforce.KindDocument,
		Title:     "active draft",
		AgentName: "archive-agent",
		SessionID: sessID,
		Content:   []byte("active"),
	}
	if err := store.Write(ctx, activeDraft); err != nil {
		t.Fatalf("Write active draft: %v", err)
	}

	// Back-date terminal artifacts to 3 hours ago.
	oldTime := time.Now().UTC().Add(-3 * time.Hour).Format(time.RFC3339Nano)
	for _, id := range terminalIDs {
		if _, err := db.Write().ExecContext(ctx,
			`UPDATE artifacts SET updated_at = ? WHERE id = ?`, oldTime, id,
		); err != nil {
			t.Fatalf("back-date %s: %v", id, err)
		}
	}

	// Archive with a 2-hour cutoff.
	n, err := store.Archive(ctx, 2*time.Hour)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if n != len(terminalStatuses) {
		t.Errorf("expected %d archived, got %d", len(terminalStatuses), n)
	}

	// Terminal artifacts should be gone.
	for _, id := range terminalIDs {
		if _, err := store.Read(ctx, id); err == nil {
			t.Errorf("terminal artifact %s should be deleted after archive", id)
		}
	}

	// Active draft should still exist.
	if _, err := store.Read(ctx, activeDraft.ID); err != nil {
		t.Errorf("active draft should still exist, got: %v", err)
	}
}

// ─── 9. WorkstreamStore Concurrent Creates ───────────────────────────────────

// TestIntegration_WorkstreamStore_ConcurrentCreates launches 5 goroutines that
// each create a workstream concurrently. All creates must succeed and List must
// return exactly 5 workstreams.
func TestIntegration_WorkstreamStore_ConcurrentCreates(t *testing.T) {
	ctx := context.Background()
	db := openGranularDB(t)
	store := spaces.NewWorkstreamStore(db)

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = store.Create(ctx,
				fmt.Sprintf("concurrent-ws-%d", idx),
				fmt.Sprintf("concurrent test workstream %d", idx),
			)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d Create error: %v", i, err)
		}
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List after concurrent creates: %v", err)
	}
	if len(list) != n {
		t.Errorf("expected %d workstreams, got %d", n, len(list))
	}
}

// ─── 10. SchemaVersion Idempotent ────────────────────────────────────────────

// TestIntegration_SchemaVersion_Idempotent calls ApplySchema twice against the
// same database and verifies the second call does not return an error and the
// expected tables still exist correctly.
func TestIntegration_SchemaVersion_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.db")

	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("Open (first): %v", err)
	}

	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema (first): %v", err)
	}
	db.Close()

	// Re-open and apply schema again — must be idempotent.
	db2, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("Open (second): %v", err)
	}
	defer db2.Close()

	if err := db2.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema (second) returned error: %v", err)
	}

	// Verify key tables still exist.
	for _, tbl := range []string{"sessions", "messages", "artifacts"} {
		var count int
		if err := db2.Read().QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&count); err != nil {
			t.Fatalf("check table %q: %v", tbl, err)
		}
		if count != 1 {
			t.Errorf("table %q missing after second ApplySchema", tbl)
		}
	}
}
