package session_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openSessBenchDB(b *testing.B) *sqlitedb.DB {
	b.Helper()
	db, err := sqlitedb.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		b.Fatalf("ApplySchema: %v", err)
	}
	b.Cleanup(func() { db.Close() })
	return db
}

func BenchmarkSQLiteStoreSave(b *testing.B) {
	db := openSessBenchDB(b)
	store := session.NewSQLiteSessionStore(db)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sess := store.New(fmt.Sprintf("session-%d", i), "/workspace", "model-a")
		if err := store.SaveManifest(sess); err != nil {
			b.Fatalf("SaveManifest: %v", err)
		}
	}
}

func BenchmarkSQLiteStoreLoad(b *testing.B) {
	db := openSessBenchDB(b)
	store := session.NewSQLiteSessionStore(db)
	sess := store.New("bench-load", "/workspace", "model-a")
	if err := store.SaveManifest(sess); err != nil {
		b.Fatalf("SaveManifest: %v", err)
	}
	id := sess.ID
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Load(id); err != nil {
			b.Fatalf("Load: %v", err)
		}
	}
}

func BenchmarkSQLiteStoreAppendMessage(b *testing.B) {
	db := openSessBenchDB(b)
	store := session.NewSQLiteSessionStore(db)
	sess := store.New("bench-append", "/workspace", "model-a")
	if err := store.SaveManifest(sess); err != nil {
		b.Fatalf("SaveManifest: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := session.SessionMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("message content %d", i),
		}
		if err := store.Append(sess, msg); err != nil {
			b.Fatalf("Append: %v", err)
		}
	}
}

func BenchmarkSQLiteStoreTailMessages(b *testing.B) {
	db := openSessBenchDB(b)
	store := session.NewSQLiteSessionStore(db)
	sess := store.New("bench-tail", "/workspace", "model-a")
	if err := store.SaveManifest(sess); err != nil {
		b.Fatalf("SaveManifest: %v", err)
	}
	for i := 0; i < 100; i++ {
		msg := session.SessionMessage{
			Role:    "user",
			Content: fmt.Sprintf("message %d", i),
		}
		if err := store.Append(sess, msg); err != nil {
			b.Fatalf("Append: %v", err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.TailMessages(sess.ID, 20); err != nil {
			b.Fatalf("TailMessages: %v", err)
		}
	}
}
