package session_test

import (
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openSessTestDBNoMigrate opens a DB with schema applied but no migrations run.
// This simulates a pre-migration database for testing migration data integrity.
func openSessTestDBNoMigrate(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestMigrationFTS_SessionsSavedViaStoreAreSearchable verifies that sessions
// saved through SaveManifest are immediately indexed in sessions_fts and
// findable via SearchSessions. (The old contentless-FTS migration is squashed
// into the base schema — sessions_fts is a standard FTS5 table from day 1.)
func TestMigrationFTS_SessionsSavedViaStoreAreSearchable(t *testing.T) {
	t.Parallel()
	db := openSessTestDBNoMigrate(t)
	store := session.NewSQLiteSessionStore(db)

	for _, title := range []string{"Alpha Project", "Beta Deployment", "Gamma Analysis"} {
		sess := store.New(title, "/tmp", "test-model")
		if err := store.SaveManifest(sess); err != nil {
			t.Fatalf("SaveManifest %q: %v", title, err)
		}
	}

	tests := []struct {
		query string
		want  string
	}{
		{"Alpha", "Alpha Project"},
		{"Beta", "Beta Deployment"},
		{"Gamma", "Gamma Analysis"},
	}
	for _, tc := range tests {
		results, err := store.SearchSessions(tc.query)
		if err != nil {
			t.Errorf("SearchSessions(%q): %v", tc.query, err)
			continue
		}
		found := false
		for _, m := range results {
			if m.Title == tc.want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SearchSessions(%q): %q not found in results %v", tc.query, tc.want, results)
		}
	}
}

// TestMigrationFTS_FreshInstallSearchable verifies that on a fresh install
// (no pre-migration data) sessions saved after migration are immediately
// searchable.
func TestMigrationFTS_FreshInstallSearchable(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	sess := store.New("Unique Needle Title", "/ws", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	results, err := store.SearchSessions("Needle")
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("SearchSessions returned 0 results; FTS index not populated")
	}
	if results[0].Title != "Unique Needle Title" {
		t.Errorf("got title %q, want %q", results[0].Title, "Unique Needle Title")
	}
}

// TestMigrationFTS_DeletedSessionRemovedFromFTS verifies that deleting a session
// also removes it from the FTS index so it no longer appears in search results.
func TestMigrationFTS_DeletedSessionRemovedFromFTS(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	sess := store.New("DeleteMe Session", "/ws", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Verify it appears in search.
	before, err := store.SearchSessions("DeleteMe")
	if err != nil {
		t.Fatalf("SearchSessions before delete: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("session not found before delete")
	}

	// Delete it.
	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it no longer appears.
	after, err := store.SearchSessions("DeleteMe")
	if err != nil {
		t.Fatalf("SearchSessions after delete: %v", err)
	}
	for _, m := range after {
		if m.ID == sess.ID {
			t.Errorf("deleted session %q still appears in FTS search results", sess.ID)
		}
	}
}

// TestSession_ListIncludesAllStatuses verifies that List() returns both active
// and archived sessions. Session status is a display concern — the store
// returns all sessions and the UI/caller decides what to show.
func TestSession_ListIncludesAllStatuses(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	active := store.New("Active Session", "/ws", "m")
	active.Manifest.Status = "active"
	if err := store.SaveManifest(active); err != nil {
		t.Fatalf("SaveManifest active: %v", err)
	}

	archived := store.New("Archived Session", "/ws", "m")
	archived.Manifest.Status = "archived"
	if err := store.SaveManifest(archived); err != nil {
		t.Fatalf("SaveManifest archived: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var foundActive, foundArchived bool
	for _, m := range list {
		if m.ID == active.ID {
			foundActive = true
			if m.Status != "active" {
				t.Errorf("active session status: want active, got %q", m.Status)
			}
		}
		if m.ID == archived.ID {
			foundArchived = true
			if m.Status != "archived" {
				t.Errorf("archived session status: want archived, got %q", m.Status)
			}
		}
	}
	if !foundActive {
		t.Error("active session not found in List()")
	}
	if !foundArchived {
		t.Error("archived session not found in List() — archive status must be preserved")
	}
}

// TestSession_ArchiveStatus_Roundtrip verifies that setting a session status to
// "archived" persists correctly and can be read back.
func TestSession_ArchiveStatus_Roundtrip(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	sess := store.New("To Archive", "/ws", "m")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("initial SaveManifest: %v", err)
	}

	// Archive it.
	sess.Manifest.Status = "archived"
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest archive: %v", err)
	}

	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Manifest.Status != "archived" {
		t.Errorf("status after archive: want archived, got %q", loaded.Manifest.Status)
	}
}

// insertSessionRaw inserts a session row directly into the DB, bypassing the store.
// Used to simulate pre-migration data scenarios.
func insertSessionRaw(t *testing.T, db *sqlitedb.DB, id, title string) {
	t.Helper()
	_, err := db.Write().Exec(`
		INSERT INTO sessions (id, title, model, agent, created_at, updated_at,
		                      message_count, workspace_root, workspace_name, status, version)
		VALUES (?, ?, 'model', '', strftime('%Y-%m-%dT%H:%M:%fZ','now'),
		        strftime('%Y-%m-%dT%H:%M:%fZ','now'), 0, '/ws', 'ws', 'active', 1)`,
		id, title,
	)
	if err != nil {
		t.Fatalf("insertSessionRaw %q: %v", title, err)
	}
}

// TestMigration_MemoryQueueV2_Idempotent verifies the memory replication queue
// migration leaves a usable table with correct schema constraints.
func TestMigration_MemoryQueueV2_Idempotent(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)

	// Table should exist after migrations.
	var tableCount int
	err := db.Read().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='memory_replication_queue'`,
	).Scan(&tableCount)
	if err != nil {
		t.Fatalf("check table exists: %v", err)
	}
	if tableCount != 1 {
		t.Fatal("memory_replication_queue table not created by migration")
	}

	// UNIQUE constraint (target_vault, concept_key, space_id) should be enforced.
	wdb := db.Write()
	ins := func() error {
		_, err := wdb.Exec(`
			INSERT INTO memory_replication_queue
			    (target_vault, source_agent, space_id, concept_key, payload, next_retry_at)
			VALUES ('vault1', 'agent1', 'space1', 'key1', '{}', 0)`)
		return err
	}
	if err := ins(); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Second insert with same (vault, key, space) must fail with UNIQUE constraint.
	if err := ins(); err == nil {
		t.Error("expected UNIQUE constraint violation on duplicate (target_vault, concept_key, space_id)")
	}

	// Insert with different key should succeed.
	_, err = wdb.Exec(`
		INSERT INTO memory_replication_queue
		    (target_vault, source_agent, space_id, concept_key, payload, next_retry_at)
		VALUES ('vault1', 'agent1', 'space1', 'key2', '{}', 0)`)
	if err != nil {
		t.Errorf("insert with different key: %v", err)
	}

	// status column must default to 'pending'.
	var status string
	err = db.Read().QueryRow(
		`SELECT status FROM memory_replication_queue WHERE concept_key = 'key2'`,
	).Scan(&status)
	if err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != "pending" {
		t.Errorf("default status: want pending, got %q", status)
	}
}

