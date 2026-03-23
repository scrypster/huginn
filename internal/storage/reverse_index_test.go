package storage

import (
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// GetEdgesTo with reverse index
// ---------------------------------------------------------------------------

func TestGetEdgesTo_ReverseIndex_MultipleSourcesSameDest(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	dest := "internal/core/service.go"
	sources := []string{"cmd/main.go", "cmd/cli.go", "internal/handler/api.go"}

	for _, src := range sources {
		e := Edge{From: src, To: dest, Kind: "Import", Confidence: "HIGH", Symbol: "NewService"}
		if err := s.SetEdge(src, dest, e); err != nil {
			t.Fatalf("SetEdge(%s -> %s): %v", src, dest, err)
		}
	}

	// Add unrelated edge that should not appear
	if err := s.SetEdge("cmd/main.go", "internal/other.go", Edge{
		From: "cmd/main.go", To: "internal/other.go", Kind: "Import",
	}); err != nil {
		t.Fatalf("SetEdge unrelated: %v", err)
	}

	edges := s.GetEdgesTo(dest)
	if len(edges) != len(sources) {
		t.Fatalf("GetEdgesTo: got %d edges, want %d", len(edges), len(sources))
	}
	for _, e := range edges {
		if e.To != dest {
			t.Errorf("unexpected To=%q, want %q", e.To, dest)
		}
	}
}

func TestDeleteEdge_RemovesBothKeys(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	from, to := "cmd/main.go", "internal/svc.go"
	e := Edge{From: from, To: to, Kind: "Import", Confidence: "HIGH"}

	if err := s.SetEdge(from, to, e); err != nil {
		t.Fatalf("SetEdge: %v", err)
	}

	// Verify edge exists in both directions
	if got := s.GetEdgesFrom(from); len(got) != 1 {
		t.Fatalf("before delete: GetEdgesFrom got %d, want 1", len(got))
	}
	if got := s.GetEdgesTo(to); len(got) != 1 {
		t.Fatalf("before delete: GetEdgesTo got %d, want 1", len(got))
	}

	if err := s.DeleteEdge(from, to); err != nil {
		t.Fatalf("DeleteEdge: %v", err)
	}

	if got := s.GetEdgesFrom(from); len(got) != 0 {
		t.Errorf("after delete: GetEdgesFrom got %d, want 0", len(got))
	}
	if got := s.GetEdgesTo(to); len(got) != 0 {
		t.Errorf("after delete: GetEdgesTo got %d, want 0", len(got))
	}
}

func TestDeleteEdge_NonexistentIsNoOp(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	if err := s.DeleteEdge("ghost_from.go", "ghost_to.go"); err != nil {
		t.Fatalf("DeleteEdge nonexistent: %v", err)
	}
}

func TestDeleteEdge_OnlyRemovesTargetEdge(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	dest := "internal/svc.go"
	// Two edges pointing to same dest
	e1 := Edge{From: "a.go", To: dest, Kind: "Import", Confidence: "HIGH"}
	e2 := Edge{From: "b.go", To: dest, Kind: "Import", Confidence: "HIGH"}
	_ = s.SetEdge("a.go", dest, e1)
	_ = s.SetEdge("b.go", dest, e2)

	if err := s.DeleteEdge("a.go", dest); err != nil {
		t.Fatalf("DeleteEdge: %v", err)
	}

	// b.go -> dest should still exist
	edges := s.GetEdgesTo(dest)
	if len(edges) != 1 {
		t.Fatalf("after partial delete: GetEdgesTo got %d, want 1", len(edges))
	}
	if edges[0].From != "b.go" {
		t.Errorf("remaining edge From=%q, want b.go", edges[0].From)
	}
}

// ---------------------------------------------------------------------------
// Compact
// ---------------------------------------------------------------------------

func TestCompact_EmptyDB(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	if err := s.Compact(); err != nil {
		t.Fatalf("Compact on empty DB: %v", err)
	}
}

func TestCompact_PopulatedDB(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	// Populate with some data
	for i := 0; i < 50; i++ {
		path := fmt.Sprintf("file_%d.go", i)
		e := Edge{From: path, To: "target.go", Kind: "Import", Confidence: "HIGH"}
		if err := s.SetEdge(path, "target.go", e); err != nil {
			t.Fatalf("SetEdge: %v", err)
		}
	}

	if err := s.Compact(); err != nil {
		t.Fatalf("Compact on populated DB: %v", err)
	}

	// Verify data is still accessible after compaction
	edges := s.GetEdgesTo("target.go")
	if len(edges) != 50 {
		t.Errorf("after Compact: GetEdgesTo got %d, want 50", len(edges))
	}
}

func TestCompact_ClosedStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = s.Close()

	if err := s.Compact(); err == nil {
		t.Error("expected error on closed store, got nil")
	}
}

// ---------------------------------------------------------------------------
// Reverse index key builders
// ---------------------------------------------------------------------------

func TestKeyEdgeTo(t *testing.T) {
	t.Parallel()
	got := string(keyEdgeTo("internal/svc.go", "cmd/main.go"))
	want := "edgeto:internal/svc.go:cmd/main.go"
	if got != want {
		t.Errorf("keyEdgeTo: got %q, want %q", got, want)
	}
}

func TestKeyEdgeToPrefix(t *testing.T) {
	t.Parallel()
	got := string(keyEdgeToPrefix("internal/svc.go"))
	want := "edgeto:internal/svc.go:"
	if got != want {
		t.Errorf("keyEdgeToPrefix: got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Benchmark: GetEdgesTo O(k) prefix scan
// ---------------------------------------------------------------------------

func BenchmarkGetEdgesTo(b *testing.B) {
	dir := b.TempDir()
	s, err := Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Create 100 edges to different targets plus 10 edges to our target
	target := "target.go"
	for i := 0; i < 100; i++ {
		from := fmt.Sprintf("src_%d.go", i)
		to := fmt.Sprintf("other_%d.go", i)
		_ = s.SetEdge(from, to, Edge{From: from, To: to, Kind: "Import"})
	}
	for i := 0; i < 10; i++ {
		from := fmt.Sprintf("caller_%d.go", i)
		_ = s.SetEdge(from, target, Edge{From: from, To: target, Kind: "Import"})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		edges := s.GetEdgesTo(target)
		if len(edges) != 10 {
			b.Fatalf("got %d edges, want 10", len(edges))
		}
	}
}
