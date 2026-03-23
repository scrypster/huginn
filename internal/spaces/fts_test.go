package spaces_test

import (
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openFTSTestDB opens a fresh SQLite DB with both the core schema and the
// spaces migrations applied. It registers a cleanup on t to close the DB.
func openFTSTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dir, "fts_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("Migrate spaces: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestFTS_SearchFindsSession verifies that after a session manifest is saved via
// SQLiteSessionStore.SaveManifest, a full-text search for a word in the session
// title returns that session.
//
// Note: FTS5 MATCH treats hyphens as token separators / operators. Titles and
// search terms in these tests use plain words to exercise the FTS tokenizer
// without triggering FTS5 operator parsing.
func TestFTS_SearchFindsSession(t *testing.T) {
	db := openFTSTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	// Create and persist a session with a distinctive title (plain words only).
	sess := store.New("huginn fts uniqueterm alpha", "/workspace", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Search for a distinctive word present in the title.
	results, err := store.SearchSessions("uniqueterm")
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result, got none")
	}

	found := false
	for _, m := range results {
		if m.ID == sess.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("session %s not found in FTS results (got %d results)", sess.ID, len(results))
	}
}

// TestFTS_SearchNoMatch verifies that searching for a term not present in any
// session title returns an empty (non-nil) slice without error.
func TestFTS_SearchNoMatch(t *testing.T) {
	db := openFTSTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	// Persist a session so the FTS table is non-empty.
	sess := store.New("ordinary session title", "/workspace", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Search for a term that does not appear in any session title.
	// Use a plain alphanumeric string with no FTS5 operator characters.
	results, err := store.SearchSessions("xyzzynomatch42")
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if results == nil {
		t.Error("expected empty (non-nil) slice, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestFTS_SearchUpdatesOnSaveManifest verifies that when a session title is
// updated and SaveManifest is called again, the FTS index reflects the new
// title and the old title no longer matches.
//
// Note: FTS5 treats hyphens as operators; titles and search terms use plain
// words only.
func TestFTS_SearchUpdatesOnSaveManifest(t *testing.T) {
	db := openFTSTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	// Create and persist a session with an initial title.
	sess := store.New("initialftssearchword beta", "/workspace", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest (initial): %v", err)
	}

	// Verify initial title is findable.
	initial, err := store.SearchSessions("initialftssearchword")
	if err != nil {
		t.Fatalf("SearchSessions initial: %v", err)
	}
	if len(initial) == 0 {
		t.Fatal("expected to find session by initial title, got none")
	}

	// Update the title and re-save.
	sess.Manifest.Title = "updatedftssearchword gamma"
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest (updated): %v", err)
	}

	// Old title should no longer match.
	oldResults, err := store.SearchSessions("initialftssearchword")
	if err != nil {
		t.Fatalf("SearchSessions old title: %v", err)
	}
	if len(oldResults) != 0 {
		t.Errorf("expected 0 results for old title after update, got %d", len(oldResults))
	}

	// New title should be findable.
	newResults, err := store.SearchSessions("updatedftssearchword")
	if err != nil {
		t.Fatalf("SearchSessions new title: %v", err)
	}
	if len(newResults) == 0 {
		t.Fatal("expected to find session by updated title, got none")
	}
}
