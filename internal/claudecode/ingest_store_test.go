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
