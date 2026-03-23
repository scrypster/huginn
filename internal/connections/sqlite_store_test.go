package connections_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openTestDB opens an isolated sqlitedb.DB with schema applied.
func openTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestConn(id string) connections.Connection {
	return connections.Connection{
		ID:           id,
		Provider:     connections.ProviderGitHub,
		Type:         connections.ConnectionTypeOAuth,
		AccountLabel: "test-label",
		AccountID:    "test-account",
		Scopes:       []string{"repo", "read:org"},
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
		Metadata:     map[string]string{"key": "value"},
	}
}

func TestSQLiteStore_AddAndList(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	s := connections.NewSQLiteConnectionStore(db)

	conn := newTestConn("id-001")
	if err := s.Add(conn); err != nil {
		t.Fatalf("Add: %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}
	if list[0].ID != "id-001" {
		t.Errorf("ID = %q, want %q", list[0].ID, "id-001")
	}
	if list[0].AccountLabel != "test-label" {
		t.Errorf("AccountLabel = %q, want %q", list[0].AccountLabel, "test-label")
	}
}

func TestSQLiteStore_Get(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	s := connections.NewSQLiteConnectionStore(db)

	conn := newTestConn("id-get")
	s.Add(conn)

	got, ok := s.Get("id-get")
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.Provider != connections.ProviderGitHub {
		t.Errorf("Provider = %q, want %q", got.Provider, connections.ProviderGitHub)
	}

	_, ok = s.Get("nonexistent")
	if ok {
		t.Fatal("Get nonexistent: expected not found")
	}
}

func TestSQLiteStore_Remove(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	s := connections.NewSQLiteConnectionStore(db)

	s.Add(newTestConn("id-rm"))
	if err := s.Remove("id-rm"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	_, ok := s.Get("id-rm")
	if ok {
		t.Fatal("expected not found after Remove")
	}
	// Remove non-existent is a no-op (no error)
	if err := s.Remove("id-rm"); err != nil {
		t.Fatalf("Remove non-existent: %v", err)
	}
}

func TestSQLiteStore_ListByProvider(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	s := connections.NewSQLiteConnectionStore(db)

	conn1 := newTestConn("id-gh")
	conn1.Provider = connections.ProviderGitHub
	s.Add(conn1)

	conn2 := newTestConn("id-slack")
	conn2.Provider = connections.ProviderSlack
	s.Add(conn2)

	list, err := s.ListByProvider(connections.ProviderGitHub)
	if err != nil {
		t.Fatalf("ListByProvider: %v", err)
	}
	if len(list) != 1 || list[0].ID != "id-gh" {
		t.Fatalf("ListByProvider returned wrong results: %v", list)
	}
}

func TestSQLiteStore_UpdateExpiry(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	s := connections.NewSQLiteConnectionStore(db)

	conn := newTestConn("id-exp")
	conn.ExpiresAt = time.Time{} // zero
	s.Add(conn)

	newExpiry := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	if err := s.UpdateExpiry("id-exp", newExpiry); err != nil {
		t.Fatalf("UpdateExpiry: %v", err)
	}

	got, _ := s.Get("id-exp")
	if !got.ExpiresAt.Equal(newExpiry) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, newExpiry)
	}
}

func TestSQLiteStore_ScopesRoundTrip(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	s := connections.NewSQLiteConnectionStore(db)

	conn := newTestConn("id-scopes")
	conn.Scopes = []string{"read", "write", "admin"}
	s.Add(conn)

	got, _ := s.Get("id-scopes")
	if len(got.Scopes) != 3 || got.Scopes[0] != "read" {
		t.Errorf("Scopes round-trip failed: %v", got.Scopes)
	}
}

func TestSQLiteStore_NilMetadata(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	s := connections.NewSQLiteConnectionStore(db)

	conn := newTestConn("id-nometa")
	conn.Metadata = nil
	if err := s.Add(conn); err != nil {
		t.Fatalf("Add with nil Metadata: %v", err)
	}
	got, _ := s.Get("id-nometa")
	// Nil or empty map are both acceptable
	if len(got.Metadata) != 0 {
		t.Errorf("expected nil/empty Metadata, got %v", got.Metadata)
	}
}

func TestSQLiteStore_AddDuplicateID(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	s := connections.NewSQLiteConnectionStore(db)

	conn := newTestConn("id-dup")
	s.Add(conn)
	if err := s.Add(conn); err == nil {
		t.Fatal("expected error on duplicate ID, got nil")
	}
}

func TestSQLiteStore_ConcurrentAdd(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	s := connections.NewSQLiteConnectionStore(db)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-%d", n)
			conn := newTestConn(id)
			s.Add(conn) // ignore errors — some may collide
		}(i)
	}
	wg.Wait()

	list, err := s.List()
	if err != nil {
		t.Fatalf("List after concurrent adds: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("expected at least 1 connection after concurrent adds")
	}
}
