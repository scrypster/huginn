package artifact_test

// bench_r5_test.go — benchmarks for internal/artifact package.

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/workforce"
)

// newBenchStore creates a SQLiteStore suitable for benchmarks by opening a real
// file-backed DB (b.TempDir() provides a unique directory per benchmark run).
func newBenchStore(b *testing.B) (*artifact.SQLiteStore, string) {
	b.Helper()
	db, err := sqlitedb.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		b.Fatalf("ApplySchema: %v", err)
	}
	// Pre-insert sessions used by benchmarks.
	for _, id := range []string{"sess-001", "session-A", "session-B", "sess-del"} {
		if _, err := db.Write().Exec(`INSERT OR IGNORE INTO sessions (id) VALUES (?)`, id); err != nil {
			b.Fatalf("insert bench session %s: %v", id, err)
		}
	}
	b.Cleanup(func() { db.Close() })
	dir := b.TempDir()
	return artifact.NewStore(db.Write(), dir), dir
}

// BenchmarkWrite_SmallArtifact measures Write throughput for a 1 KB inline artifact.
func BenchmarkWrite_SmallArtifact(b *testing.B) {
	s, _ := newBenchStore(b)
	ctx := context.Background()
	content := bytes.Repeat([]byte("a"), 1024) // 1 KB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := &workforce.Artifact{
			Kind:      workforce.KindDocument,
			Title:     fmt.Sprintf("bench-small-%d", i),
			AgentName: "bench-agent",
			SessionID: "sess-001",
			Content:   content,
		}
		if err := s.Write(ctx, a); err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
}

// BenchmarkWrite_LargeArtifact measures Write throughput for a 300 KB disk-spill artifact.
func BenchmarkWrite_LargeArtifact(b *testing.B) {
	s, _ := newBenchStore(b)
	ctx := context.Background()
	content := bytes.Repeat([]byte("b"), 300*1024) // 300 KB — exceeds 256 KB threshold

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := &workforce.Artifact{
			Kind:      workforce.KindFileBundle,
			Title:     fmt.Sprintf("bench-large-%d", i),
			AgentName: "bench-agent",
			SessionID: "sess-001",
			Content:   content,
		}
		if err := s.Write(ctx, a); err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
}

// BenchmarkListBySession_50Items measures ListBySession with 50 pre-seeded artifacts.
func BenchmarkListBySession_50Items(b *testing.B) {
	s, _ := newBenchStore(b)
	ctx := context.Background()

	// Pre-seed 50 artifacts.
	for i := 0; i < 50; i++ {
		a := &workforce.Artifact{
			Kind:      workforce.KindDocument,
			Title:     fmt.Sprintf("item-%d", i),
			AgentName: "bench-agent",
			SessionID: "sess-001",
			Content:   []byte(fmt.Sprintf("content %d", i)),
		}
		if err := s.Write(ctx, a); err != nil {
			b.Fatalf("seed Write[%d]: %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		list, err := s.ListBySession(ctx, "sess-001", 50, "")
		if err != nil {
			b.Fatalf("ListBySession: %v", err)
		}
		_ = list
	}
}

// BenchmarkRead_InlineContent measures Read throughput for an artifact with inline content.
func BenchmarkRead_InlineContent(b *testing.B) {
	s, _ := newBenchStore(b)
	ctx := context.Background()

	a := &workforce.Artifact{
		Kind:      workforce.KindDocument,
		Title:     "bench-read",
		AgentName: "bench-agent",
		SessionID: "sess-001",
		Content:   bytes.Repeat([]byte("r"), 1024),
	}
	if err := s.Write(ctx, a); err != nil {
		b.Fatalf("Write: %v", err)
	}
	id := a.ID

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := s.Read(ctx, id)
		if err != nil {
			b.Fatalf("Read: %v", err)
		}
		_ = got
	}
}

// BenchmarkUpdateStatus measures UpdateStatus throughput by pre-creating all artifacts
// before starting the timer, then updating each one's status during the timed portion.
func BenchmarkUpdateStatus(b *testing.B) {
	s, _ := newBenchStore(b)
	ctx := context.Background()

	// Pre-create b.N artifacts so the timer only covers UpdateStatus.
	ids := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		a := &workforce.Artifact{
			Kind:      workforce.KindDocument,
			Title:     fmt.Sprintf("status-%d", i),
			AgentName: "bench-agent",
			SessionID: "sess-001",
			Content:   []byte("data"),
		}
		if err := s.Write(ctx, a); err != nil {
			b.Fatalf("seed Write[%d]: %v", i, err)
		}
		ids[i] = a.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.UpdateStatus(ctx, ids[i], workforce.StatusAccepted, ""); err != nil {
			b.Fatalf("UpdateStatus[%d]: %v", i, err)
		}
	}
}

// BenchmarkWrite_ConcurrentSessions measures concurrent Write throughput with 4 goroutines
// each writing to a different session.
func BenchmarkWrite_ConcurrentSessions(b *testing.B) {
	s, _ := newBenchStore(b)
	ctx := context.Background()
	content := bytes.Repeat([]byte("c"), 1024)

	sessions := []string{"sess-001", "session-A", "session-B", "sess-del"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sessID := sessions[i%len(sessions)]
			a := &workforce.Artifact{
				Kind:      workforce.KindDocument,
				Title:     fmt.Sprintf("concurrent-%d", i),
				AgentName: "bench-agent",
				SessionID: sessID,
				Content:   content,
			}
			if err := s.Write(ctx, a); err != nil {
				b.Errorf("Write: %v", err)
			}
			i++
		}
	})
}
